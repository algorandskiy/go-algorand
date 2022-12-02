// Copyright (C) 2019-2022 Algorand, Inc.
// This file is part of go-algorand
//
// go-algorand is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// go-algorand is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with go-algorand.  If not, see <https://www.gnu.org/licenses/>.

package data

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/data/pools"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/data/transactions/verify"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/network"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/util/execpool"
	"github.com/algorand/go-algorand/util/metrics"
)

// The size txBacklogSize used to determine the size of the backlog that is used to store incoming transaction messages before starting dropping them.
// It should be configured to be higher then the number of CPU cores, so that the execution pool get saturated, but not too high to avoid lockout of the
// execution pool for a long duration of time.
// Set backlog at 'approximately one block' by dividing block size by a typical transaction size.
var txBacklogSize = config.Consensus[protocol.ConsensusCurrentVersion].MaxTxnBytesPerBlock / 200

var transactionMessagesHandled = metrics.MakeCounter(metrics.TransactionMessagesHandled)
var transactionMessagesDroppedFromBacklog = metrics.MakeCounter(metrics.TransactionMessagesDroppedFromBacklog)
var transactionMessagesDroppedFromPool = metrics.MakeCounter(metrics.TransactionMessagesDroppedFromPool)
var transactionMessagesAlreadyCommitted = metrics.MakeCounter(metrics.TransactionMessagesAlreadyCommitted)
var transactionMessagesTxGroupInvalidFee = metrics.MakeCounter(metrics.TransactionMessagesTxGroupInvalidFee)
var transactionMessagesTxnNotWellFormed = metrics.MakeCounter(metrics.TransactionMessagesTxnNotWellFormed)
var transactionMessagesTxnSigNotWellFormed = metrics.MakeCounter(metrics.TransactionMessagesTxnSigNotWellFormed)
var transactionMessagesTxnMsigNotWellFormed = metrics.MakeCounter(metrics.TransactionMessagesTxnMsigNotWellFormed)
var transactionMessagesTxnLogicSig = metrics.MakeCounter(metrics.TransactionMessagesTxnLogicSig)
var transactionMessagesTxnSigVerificationFailed = metrics.MakeCounter(metrics.TransactionMessagesTxnSigVerificationFailed)
var transactionMessagesBacklogErr = metrics.MakeCounter(metrics.TransactionMessagesBacklogErr)
var transactionMessagesRemember = metrics.MakeCounter(metrics.TransactionMessagesRemember)
var transactionMessageTxGroupExcessive = metrics.MakeCounter(metrics.TransactionMessageTxGroupExcessive)

var transactionMessageTxGroupFull = metrics.MakeCounter(metrics.TransactionMessageTxGroupFull)
var transactionMessagesBacklogSizeGauge = metrics.MakeGauge(metrics.TransactionMessagesBacklogSize)

var transactionGroupTxSyncHandled = metrics.MakeCounter(metrics.TransactionGroupTxSyncHandled)
var transactionGroupTxSyncRemember = metrics.MakeCounter(metrics.TransactionGroupTxSyncRemember)
var transactionGroupTxSyncAlreadyCommitted = metrics.MakeCounter(metrics.TransactionGroupTxSyncAlreadyCommitted)

// The txBacklogMsg structure used to track a single incoming transaction from the gossip network,
type txBacklogMsg struct {
	rawmsg            *network.IncomingMessage // the raw message from the network
	unverifiedTxGroup []transactions.SignedTxn // the unverified ( and signed ) transaction group
	verificationErr   error                    // The verification error generated by the verification function, if any.
}

// TxHandler handles transaction messages
type TxHandler struct {
	txPool                *pools.TransactionPool
	ledger                *Ledger
	genesisID             string
	genesisHash           crypto.Digest
	txVerificationPool    execpool.BacklogPool
	backlogQueue          chan *txBacklogMsg
	postVerificationQueue chan *verify.VerificationResult
	backlogWg             sync.WaitGroup
	net                   network.GossipNode
	ctx                   context.Context
	ctxCancel             context.CancelFunc
	streamVerifier        *verify.StreamVerifier
	streamVerifierChan    chan *verify.UnverifiedElement
}

// MakeTxHandler makes a new handler for transaction messages
func MakeTxHandler(txPool *pools.TransactionPool, ledger *Ledger, net network.GossipNode, genesisID string, genesisHash crypto.Digest, executionPool execpool.BacklogPool) (*TxHandler, error) {

	if txPool == nil {
		return nil, errors.New("MakeTxHandler: txPool is nil on initialization")
	}

	if ledger == nil {
		return nil, errors.New("MakeTxHandler: ledger is nil on initialization")
	}

	handler := &TxHandler{
		txPool:                txPool,
		genesisID:             genesisID,
		genesisHash:           genesisHash,
		ledger:                ledger,
		txVerificationPool:    executionPool,
		backlogQueue:          make(chan *txBacklogMsg, txBacklogSize),
		postVerificationQueue: make(chan *verify.VerificationResult, txBacklogSize),
		net:                   net,
		streamVerifierChan:    make(chan *verify.UnverifiedElement),
	}

	// prepare the transaction stream verifer
	var err error
	handler.streamVerifier, err = verify.MakeStreamVerifier(handler.streamVerifierChan,
		handler.postVerificationQueue, handler.ledger, handler.txVerificationPool,
		handler.ledger.VerifiedTransactionCache(), transactionMessagesDroppedFromPool)
	if err != nil {
		return nil, err
	}
	return handler, nil
}

// Start enables the processing of incoming messages at the transaction handler
func (handler *TxHandler) Start() {
	handler.ctx, handler.ctxCancel = context.WithCancel(context.Background())
	handler.net.RegisterHandlers([]network.TaggedMessageHandler{
		{Tag: protocol.TxnTag, MessageHandler: network.HandlerFunc(handler.processIncomingTxn)},
	})
	handler.backlogWg.Add(2)
	go handler.backlogWorker()
	go handler.backlogGaugeThread()
	handler.streamVerifier.Start(handler.ctx)
}

// Stop suspends the processing of incoming messages at the transaction handler
func (handler *TxHandler) Stop() {
	handler.ctxCancel()
	handler.backlogWg.Wait()
}

func reencode(stxns []transactions.SignedTxn) []byte {
	var result [][]byte
	for i := range stxns {
		result = append(result, protocol.Encode(&stxns[i]))
	}
	return bytes.Join(result, nil)
}

func (handler *TxHandler) backlogGaugeThread() {
	defer handler.backlogWg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			transactionMessagesBacklogSizeGauge.Set(float64(len(handler.backlogQueue)))
		case <-handler.ctx.Done():
			return
		}
	}
}

// backlogWorker is the worker go routine that process the incoming messages from the postVerificationQueue and backlogQueue channels
// and dispatches them further.
func (handler *TxHandler) backlogWorker() {
	// Note: TestIncomingTxHandle and TestIncomingTxGroupHandle emulate this function.
	// Changes to the behavior in this function should be reflected in the test.
	defer handler.backlogWg.Done()
	for {
		// prioritize the postVerificationQueue
		select {
		case wi, ok := <-handler.postVerificationQueue:
			if !ok {
				return
			}
			txBLMsg := wi.BacklogMessage.(*txBacklogMsg)
			txBLMsg.verificationErr = wi.Err
			handler.postProcessCheckedTxn(txBLMsg)

			// restart the loop so that we could empty out the post verification queue.
			continue
		default:
		}

		// we have no more post verification items. wait for either backlog queue item or post verification item.
		select {
		case wi, ok := <-handler.backlogQueue:
			if !ok {
				// this is never happening since handler.backlogQueue is never closed
				return
			}
			if handler.checkAlreadyCommitted(wi) {
				transactionMessagesAlreadyCommitted.Inc(nil)
				continue
			}
			// handler.streamVerifierChan does not receive if ctx is cancled
			select {
			case handler.streamVerifierChan <- &verify.UnverifiedElement{TxnGroup: wi.unverifiedTxGroup, BacklogMessage: wi}:
			case <-handler.ctx.Done():
				transactionMessagesDroppedFromBacklog.Inc(nil)
				return
			}
		case wi, ok := <-handler.postVerificationQueue:
			if !ok {
				// this is never happening since handler.postVerificationQueue is never closed
				return
			}
			txBLMsg := wi.BacklogMessage.(*txBacklogMsg)
			txBLMsg.verificationErr = wi.Err
			handler.postProcessCheckedTxn(txBLMsg)

		case <-handler.ctx.Done():
			return
		}
	}
}

func (handler *TxHandler) postProcessReportErrors(err error) {
	if errors.Is(err, crypto.ErrBatchHasFailedSigs) {
		transactionMessagesTxnSigVerificationFailed.Inc(nil)
		return
	}

	var txGroupErr *verify.ErrTxGroupError
	if errors.As(err, &txGroupErr) {
		switch txGroupErr.Reason {
		case verify.TxGroupErrorReasonNotWellFormed:
			transactionMessagesTxnNotWellFormed.Inc(nil)
		case verify.TxGroupErrorReasonInvalidFee:
			transactionMessagesTxGroupInvalidFee.Inc(nil)
		case verify.TxGroupErrorReasonHasNoSig:
			fallthrough
		case verify.TxGroupErrorReasonSigNotWellFormed:
			transactionMessagesTxnSigNotWellFormed.Inc(nil)
		case verify.TxGroupErrorReasonMsigNotWellFormed:
			transactionMessagesTxnMsigNotWellFormed.Inc(nil)
		case verify.TxGroupErrorReasonLogicSigFailed:
			transactionMessagesTxnLogicSig.Inc(nil)
		default:
			transactionMessagesBacklogErr.Inc(nil)
		}
	} else {
		transactionMessagesBacklogErr.Inc(nil)
	}
}

func (handler *TxHandler) postProcessCheckedTxn(wi *txBacklogMsg) {
	if wi.verificationErr != nil {
		// disconnect from peer.
		handler.postProcessReportErrors(wi.verificationErr)
		logging.Base().Warnf("Received a malformed tx group %v: %v", wi.unverifiedTxGroup, wi.verificationErr)
		handler.net.Disconnect(wi.rawmsg.Sender)
		return
	}

	// we've processed this message, so increase the counter.
	transactionMessagesHandled.Inc(nil)

	// at this point, we've verified the transaction, so we can safely treat the transaction as a verified transaction.
	verifiedTxGroup := wi.unverifiedTxGroup

	// save the transaction, if it has high enough fee and not already in the cache
	err := handler.txPool.Remember(verifiedTxGroup)
	if err != nil {
		logging.Base().Debugf("could not remember tx: %v", err)
		return
	}

	transactionMessagesRemember.Inc(nil)

	// if we remembered without any error ( i.e. txpool wasn't full ), then we should pin these transactions.
	err = handler.ledger.VerifiedTransactionCache().Pin(verifiedTxGroup)
	if err != nil {
		logging.Base().Infof("unable to pin transaction: %v", err)
	}

	// We reencode here instead of using rawmsg.Data to avoid broadcasting non-canonical encodings
	handler.net.Relay(handler.ctx, protocol.TxnTag, reencode(verifiedTxGroup), false, wi.rawmsg.Sender)
}

func (handler *TxHandler) processIncomingTxn(rawmsg network.IncomingMessage) network.OutgoingMessage {
	dec := protocol.NewMsgpDecoderBytes(rawmsg.Data)
	ntx := 0
	unverifiedTxGroup := make([]transactions.SignedTxn, 1)
	for {
		if len(unverifiedTxGroup) == ntx {
			n := make([]transactions.SignedTxn, len(unverifiedTxGroup)*2)
			copy(n, unverifiedTxGroup)
			unverifiedTxGroup = n
		}

		err := dec.Decode(&unverifiedTxGroup[ntx])
		if err == io.EOF {
			break
		}
		if err != nil {
			logging.Base().Warnf("Received a non-decodable txn: %v", err)
			return network.OutgoingMessage{Action: network.Disconnect}
		}
		ntx++
	}
	if ntx == 0 {
		logging.Base().Warnf("Received empty tx group")
		return network.OutgoingMessage{Action: network.Disconnect}
	}
	unverifiedTxGroup = unverifiedTxGroup[:ntx]

	if ntx == config.MaxTxGroupSize {
		transactionMessageTxGroupFull.Inc(nil)
	} else if ntx > config.MaxTxGroupSize {
		transactionMessageTxGroupExcessive.Inc(nil)
	}

	select {
	case handler.backlogQueue <- &txBacklogMsg{
		rawmsg:            &rawmsg,
		unverifiedTxGroup: unverifiedTxGroup,
	}:
	default:
		// if we failed here we want to increase the corresponding metric. It might suggest that we
		// want to increase the queue size.
		transactionMessagesDroppedFromBacklog.Inc(nil)
	}

	return network.OutgoingMessage{Action: network.Ignore}
}

// checkAlreadyCommitted test to see if the given transaction ( in the txBacklogMsg ) was already committed, and
// whether it would qualify as a candidate for the transaction pool.
//
// Note that this also checks the consistency of the transaction's group hash,
// which is required for safe transaction signature caching behavior.
func (handler *TxHandler) checkAlreadyCommitted(tx *txBacklogMsg) (processingDone bool) {
	if logging.Base().IsLevelEnabled(logging.Debug) {
		txids := make([]transactions.Txid, len(tx.unverifiedTxGroup))
		for i := range tx.unverifiedTxGroup {
			txids[i] = tx.unverifiedTxGroup[i].ID()
		}
		logging.Base().Debugf("got a tx group with IDs %v", txids)
	}

	// do a quick test to check that this transaction could potentially be committed, to reject dup pending transactions
	err := handler.txPool.Test(tx.unverifiedTxGroup)
	if err != nil {
		logging.Base().Debugf("txPool rejected transaction: %v", err)
		return true
	}
	return false
}

func (handler *TxHandler) processDecoded(unverifiedTxGroup []transactions.SignedTxn) (outmsg network.OutgoingMessage, processingDone bool) {
	tx := &txBacklogMsg{
		unverifiedTxGroup: unverifiedTxGroup,
	}
	transactionGroupTxSyncHandled.Inc(nil)

	if handler.checkAlreadyCommitted(tx) {
		transactionGroupTxSyncAlreadyCommitted.Inc(nil)
		return network.OutgoingMessage{}, true
	}

	// build the transaction verification context
	latest := handler.ledger.Latest()
	latestHdr, err := handler.ledger.BlockHdr(latest)
	if err != nil {
		logging.Base().Warnf("Could not get header for previous block %v: %v", latest, err)
		return network.OutgoingMessage{}, true
	}

	unverifiedTxnGroups := bookkeeping.SignedTxnsToGroups(unverifiedTxGroup)
	err = verify.PaysetGroups(context.Background(), unverifiedTxnGroups, latestHdr, handler.txVerificationPool, handler.ledger.VerifiedTransactionCache(), handler.ledger)
	if err != nil {
		// transaction is invalid
		logging.Base().Warnf("One or more transactions were malformed: %v", err)
		return network.OutgoingMessage{Action: network.Disconnect}, true
	}

	// at this point, we've verified the transaction group,
	// so we can safely treat the transaction as a verified transaction.
	verifiedTxGroup := unverifiedTxGroup

	// save the transaction, if it has high enough fee and not already in the cache
	err = handler.txPool.Remember(verifiedTxGroup)
	if err != nil {
		logging.Base().Debugf("could not remember tx: %v", err)
		return network.OutgoingMessage{}, true
	}

	transactionGroupTxSyncRemember.Inc(nil)

	// if we remembered without any error ( i.e. txpool wasn't full ), then we should pin these transactions.
	err = handler.ledger.VerifiedTransactionCache().Pin(verifiedTxGroup)
	if err != nil {
		logging.Base().Warnf("unable to pin transaction: %v", err)
	}

	return network.OutgoingMessage{}, false
}

// SolicitedTxHandler handles messages received through channels other than the gossip network.
// It therefore circumvents the notion of incoming/outgoing messages
type SolicitedTxHandler interface {
	Handle(txgroup []transactions.SignedTxn) error
}

// SolicitedTxHandler converts a transaction handler to a SolicitedTxHandler
func (handler *TxHandler) SolicitedTxHandler() SolicitedTxHandler {
	return &solicitedTxHandler{txHandler: handler}
}

type solicitedTxHandler struct {
	txHandler *TxHandler
}

func (handler *solicitedTxHandler) Handle(txgroup []transactions.SignedTxn) error {
	outmsg, _ := handler.txHandler.processDecoded(txgroup)
	if outmsg.Action == network.Disconnect {
		return fmt.Errorf("invalid transaction")
	}
	return nil
}
