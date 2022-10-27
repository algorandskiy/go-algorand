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

package verify

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/algorand/go-deadlock"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/data/transactions/logic"
	"github.com/algorand/go-algorand/ledger/ledgercore"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/util/execpool"
	"github.com/algorand/go-algorand/util/metrics"
)

var logicGoodTotal = metrics.MakeCounter(metrics.MetricName{Name: "algod_ledger_logic_ok", Description: "Total transaction scripts executed and accepted"})
var logicRejTotal = metrics.MakeCounter(metrics.MetricName{Name: "algod_ledger_logic_rej", Description: "Total transaction scripts executed and rejected"})
var logicErrTotal = metrics.MakeCounter(metrics.MetricName{Name: "algod_ledger_logic_err", Description: "Total transaction scripts executed and errored"})

// ErrInvalidSignature is the error returned to report that at least one signature is invalid
var ErrInvalidSignature = errors.New("At least one signature didn't pass verification")

// The PaysetGroups is taking large set of transaction groups and attempt to verify their validity using multiple go-routines.
// When doing so, it attempts to break these into smaller "worksets" where each workset takes about 2ms of execution time in order
// to avoid context switching overhead while providing good validation cancelation responsiveness. Each one of these worksets is
// "populated" with roughly txnPerWorksetThreshold transactions. ( note that the real evaluation time is unknown, but benchmarks
// show that these are realistic numbers )
const txnPerWorksetThreshold = 64

// When the PaysetGroups is generating worksets, it enqueues up to concurrentWorksets entries to the execution pool. This serves several
// purposes :
// - if the verification task need to be aborted, there are only concurrentWorksets entries that are currently redundant on the execution pool queue.
// - that number of concurrent tasks would not get beyond the capacity of the execution pool back buffer.
// - if we were to "redundantly" execute all these during context cancelation, we would spent at most 2ms * 16 = 32ms time.
// - it allows us to linearly scan the input, and process elements only once we're going to queue them into the pool.
const concurrentWorksets = 16

// GroupContext is the set of parameters external to a transaction which
// stateless checks are performed against.
//
// For efficient caching, these parameters should either be constant
// or change slowly over time.
//
// Group data are omitted because they are committed to in the
// transaction and its ID.
type GroupContext struct {
	specAddrs        transactions.SpecialAddresses
	consensusVersion protocol.ConsensusVersion
	consensusParams  config.ConsensusParams
	minAvmVersion    uint64
	signedGroupTxns  []transactions.SignedTxn
	ledger           logic.LedgerForSignature
}

// PrepareGroupContext prepares a verification group parameter object for a given transaction
// group.
func PrepareGroupContext(group []transactions.SignedTxn, contextHdr bookkeeping.BlockHeader, ledger logic.LedgerForSignature) (*GroupContext, error) {
	if len(group) == 0 {
		return nil, nil
	}
	consensusParams, ok := config.Consensus[contextHdr.CurrentProtocol]
	if !ok {
		return nil, protocol.Error(contextHdr.CurrentProtocol)
	}
	return &GroupContext{
		specAddrs: transactions.SpecialAddresses{
			FeeSink:     contextHdr.FeeSink,
			RewardsPool: contextHdr.RewardsPool,
		},
		consensusVersion: contextHdr.CurrentProtocol,
		consensusParams:  consensusParams,
		minAvmVersion:    logic.ComputeMinAvmVersion(transactions.WrapSignedTxnsWithAD(group)),
		signedGroupTxns:  group,
		ledger:           ledger,
	}, nil
}

// Equal compares two group contexts to see if they would represent the same verification context for a given transaction.
func (g *GroupContext) Equal(other *GroupContext) bool {
	return g.specAddrs == other.specAddrs &&
		g.consensusVersion == other.consensusVersion &&
		g.minAvmVersion == other.minAvmVersion
}

// txnBatchPrep verifies a SignedTxn having no obviously inconsistent data.
// Block-assembly time checks of LogicSig and accounting rules may still block the txn.
// it is the caller responsibility to call batchVerifier.Verify()
func txnBatchPrep(s *transactions.SignedTxn, txnIdx int, groupCtx *GroupContext, verifier *crypto.BatchVerifier) error {
	if !groupCtx.consensusParams.SupportRekeying && (s.AuthAddr != basics.Address{}) {
		return errors.New("nonempty AuthAddr but rekeying is not supported")
	}

	if err := s.Txn.WellFormed(groupCtx.specAddrs, groupCtx.consensusParams); err != nil {
		return err
	}

	return stxnCoreChecks(s, txnIdx, groupCtx, verifier)
}

// TxnGroup verifies a []SignedTxn as being signed and having no obviously inconsistent data.
func TxnGroup(stxs []transactions.SignedTxn, contextHdr bookkeeping.BlockHeader, cache VerifiedTransactionCache, ledger logic.LedgerForSignature) (groupCtx *GroupContext, err error) {
	batchVerifier := crypto.MakeBatchVerifier()

	if groupCtx, err = txnGroupBatchPrep(stxs, contextHdr, ledger, batchVerifier); err != nil {
		return nil, err
	}

	if err := batchVerifier.Verify(); err != nil {
		return nil, err
	}

	if cache != nil {
		cache.Add(stxs, groupCtx)
	}

	return
}

// txnGroupBatchPrep verifies a []SignedTxn having no obviously inconsistent data.
// it is the caller responsibility to call batchVerifier.Verify()
func txnGroupBatchPrep(stxs []transactions.SignedTxn, contextHdr bookkeeping.BlockHeader, ledger logic.LedgerForSignature, verifier *crypto.BatchVerifier) (groupCtx *GroupContext, err error) {
	groupCtx, err = PrepareGroupContext(stxs, contextHdr, ledger)
	if err != nil {
		return nil, err
	}

	minFeeCount := uint64(0)
	feesPaid := uint64(0)
	for i, stxn := range stxs {
		err = txnBatchPrep(&stxn, i, groupCtx, verifier)
		if err != nil {
			err = fmt.Errorf("transaction %+v invalid : %w", stxn, err)
			return
		}
		if stxn.Txn.Type != protocol.StateProofTx {
			minFeeCount++
		}
		feesPaid = basics.AddSaturate(feesPaid, stxn.Txn.Fee.Raw)
	}
	feeNeeded, overflow := basics.OMul(groupCtx.consensusParams.MinTxnFee, minFeeCount)
	if overflow {
		err = fmt.Errorf("txgroup fee requirement overflow")
		return
	}
	// feesPaid may have saturated. That's ok. Since we know
	// feeNeeded did not overflow, simple comparison tells us
	// feesPaid was enough.
	if feesPaid < feeNeeded {
		err = fmt.Errorf("txgroup had %d in fees, which is less than the minimum %d * %d",
			feesPaid, minFeeCount, groupCtx.consensusParams.MinTxnFee)
		return
	}

	return
}

func stxnCoreChecks(s *transactions.SignedTxn, txnIdx int, groupCtx *GroupContext, batchVerifier *crypto.BatchVerifier) error {
	numSigs := 0
	hasSig := false
	hasMsig := false
	hasLogicSig := false
	if s.Sig != (crypto.Signature{}) {
		numSigs++
		hasSig = true
	}
	if !s.Msig.Blank() {
		numSigs++
		hasMsig = true
	}
	if !s.Lsig.Blank() {
		numSigs++
		hasLogicSig = true
	}
	if numSigs == 0 {
		// Special case: special sender address can issue special transaction
		// types (state proof txn) without any signature.  The well-formed
		// check ensures that this transaction cannot pay any fee, and
		// cannot have any other interesting fields, except for the state proof payload.
		if s.Txn.Sender == transactions.StateProofSender && s.Txn.Type == protocol.StateProofTx {
			return nil
		}

		return errors.New("signedtxn has no sig")
	}
	if numSigs > 1 {
		return errors.New("signedtxn should only have one of Sig or Msig or LogicSig")
	}

	if hasSig {
		batchVerifier.EnqueueSignature(crypto.SignatureVerifier(s.Authorizer()), s.Txn, s.Sig)
		return nil
	}
	if hasMsig {
		if err := crypto.MultisigBatchPrep(s.Txn, crypto.Digest(s.Authorizer()), s.Msig, batchVerifier); err != nil {
			return fmt.Errorf("multisig validation failed: %w", err)
		}
		return nil
	}
	if hasLogicSig {
		return logicSigVerify(s, txnIdx, groupCtx)
	}
	return errors.New("has one mystery sig. WAT?")
}

// LogicSigSanityCheck checks that the signature is valid and that the program is basically well formed.
// It does not evaluate the logic.
func LogicSigSanityCheck(txn *transactions.SignedTxn, groupIndex int, groupCtx *GroupContext) error {
	batchVerifier := crypto.MakeBatchVerifier()

	if err := logicSigSanityCheckBatchPrep(txn, groupIndex, groupCtx, batchVerifier); err != nil {
		return err
	}
	return batchVerifier.Verify()
}

// logicSigSanityCheckBatchPrep checks that the signature is valid and that the program is basically well formed.
// It does not evaluate the logic.
// it is the caller responsibility to call batchVerifier.Verify()
func logicSigSanityCheckBatchPrep(txn *transactions.SignedTxn, groupIndex int, groupCtx *GroupContext, batchVerifier *crypto.BatchVerifier) error {
	lsig := txn.Lsig

	if groupCtx.consensusParams.LogicSigVersion == 0 {
		return errors.New("LogicSig not enabled")
	}
	if len(lsig.Logic) == 0 {
		return errors.New("LogicSig.Logic empty")
	}
	version, vlen := binary.Uvarint(lsig.Logic)
	if vlen <= 0 {
		return errors.New("LogicSig.Logic bad version")
	}
	if version > groupCtx.consensusParams.LogicSigVersion {
		return errors.New("LogicSig.Logic version too new")
	}
	if uint64(lsig.Len()) > groupCtx.consensusParams.LogicSigMaxSize {
		return errors.New("LogicSig.Logic too long")
	}

	if groupIndex < 0 {
		return errors.New("Negative groupIndex")
	}
	txngroup := transactions.WrapSignedTxnsWithAD(groupCtx.signedGroupTxns)
	ep := logic.EvalParams{
		Proto:         &groupCtx.consensusParams,
		TxnGroup:      txngroup,
		MinAvmVersion: &groupCtx.minAvmVersion,
		SigLedger:     groupCtx.ledger, // won't be needed for CheckSignature
	}
	err := logic.CheckSignature(groupIndex, &ep)
	if err != nil {
		return err
	}

	hasMsig := false
	numSigs := 0
	if lsig.Sig != (crypto.Signature{}) {
		numSigs++
	}
	if !lsig.Msig.Blank() {
		hasMsig = true
		numSigs++
	}
	if numSigs == 0 {
		// if the txn.Authorizer() == hash(Logic) then this is a (potentially) valid operation on a contract-only account
		program := logic.Program(lsig.Logic)
		lhash := crypto.HashObj(&program)
		if crypto.Digest(txn.Authorizer()) == lhash {
			return nil
		}
		return errors.New("LogicNot signed and not a Logic-only account")
	}
	if numSigs > 1 {
		return errors.New("LogicSig should only have one of Sig or Msig but has more than one")
	}

	if !hasMsig {
		program := logic.Program(lsig.Logic)
		batchVerifier.EnqueueSignature(crypto.PublicKey(txn.Authorizer()), &program, lsig.Sig)
	} else {
		program := logic.Program(lsig.Logic)
		if err := crypto.MultisigBatchPrep(&program, crypto.Digest(txn.Authorizer()), lsig.Msig, batchVerifier); err != nil {
			return fmt.Errorf("logic multisig validation failed: %w", err)
		}
	}
	return nil
}

// logicSigVerify checks that the signature is valid, executing the program.
func logicSigVerify(txn *transactions.SignedTxn, groupIndex int, groupCtx *GroupContext) error {
	err := LogicSigSanityCheck(txn, groupIndex, groupCtx)
	if err != nil {
		return err
	}

	if groupIndex < 0 {
		return errors.New("Negative groupIndex")
	}
	ep := logic.EvalParams{
		Proto:         &groupCtx.consensusParams,
		TxnGroup:      transactions.WrapSignedTxnsWithAD(groupCtx.signedGroupTxns),
		MinAvmVersion: &groupCtx.minAvmVersion,
		SigLedger:     groupCtx.ledger,
	}
	pass, err := logic.EvalSignature(groupIndex, &ep)
	if err != nil {
		logicErrTotal.Inc(nil)
		return fmt.Errorf("transaction %v: rejected by logic err=%v", txn.ID(), err)
	}
	if !pass {
		logicRejTotal.Inc(nil)
		return fmt.Errorf("transaction %v: rejected by logic", txn.ID())
	}
	logicGoodTotal.Inc(nil)
	return nil

}

// PaysetGroups verifies that the payset have a good signature and that the underlying
// transactions are properly constructed.
// Note that this does not check whether a payset is valid against the ledger:
// a PaysetGroups may be well-formed, but a payset might contain an overspend.
//
// This version of verify is performing the verification over the provided execution pool.
func PaysetGroups(ctx context.Context, payset [][]transactions.SignedTxn, blkHeader bookkeeping.BlockHeader, verificationPool execpool.BacklogPool, cache VerifiedTransactionCache, ledger logic.LedgerForSignature) (err error) {
	if len(payset) == 0 {
		return nil
	}

	// prepare up to 16 concurrent worksets.
	worksets := make(chan struct{}, concurrentWorksets)
	worksDoneCh := make(chan interface{}, concurrentWorksets)
	processing := 0

	tasksCtx, cancelTasksCtx := context.WithCancel(ctx)
	defer cancelTasksCtx()
	builder := worksetBuilder{payset: payset}
	var nextWorkset [][]transactions.SignedTxn
	for processing >= 0 {
		// see if we need to get another workset
		if len(nextWorkset) == 0 && !builder.completed() {
			nextWorkset = builder.next()
		}

		select {
		case <-tasksCtx.Done():
			return tasksCtx.Err()
		case worksets <- struct{}{}:
			if len(nextWorkset) > 0 {
				err := verificationPool.EnqueueBacklog(ctx, func(arg interface{}) interface{} {
					var grpErr error
					// check if we've canceled the request while this was in the queue.
					if tasksCtx.Err() != nil {
						return tasksCtx.Err()
					}

					txnGroups := arg.([][]transactions.SignedTxn)
					groupCtxs := make([]*GroupContext, len(txnGroups))

					batchVerifier := crypto.MakeBatchVerifierWithHint(len(payset))
					for i, signTxnsGrp := range txnGroups {
						groupCtxs[i], grpErr = txnGroupBatchPrep(signTxnsGrp, blkHeader, ledger, batchVerifier)
						// abort only if it's a non-cache error.
						if grpErr != nil {
							return grpErr
						}
					}
					verifyErr := batchVerifier.Verify()
					if verifyErr != nil {
						return verifyErr
					}
					cache.AddPayset(txnGroups, groupCtxs)
					return nil
				}, nextWorkset, worksDoneCh)
				if err != nil {
					return err
				}
				processing++
				nextWorkset = nil
			}
		case processingResult := <-worksDoneCh:
			processing--
			<-worksets
			// if there is nothing in the queue, the nextWorkset doesn't contain any work and the builder has no more entries, then we're done.
			if processing == 0 && builder.completed() && len(nextWorkset) == 0 {
				// we're done.
				processing = -1
			}
			if processingResult != nil {
				err = processingResult.(error)
				if err != nil {
					return err
				}
			}
		}

	}
	return err
}

// worksetBuilder is a helper struct used to construct well sized worksets for the execution pool to process
type worksetBuilder struct {
	payset [][]transactions.SignedTxn
	idx    int
}

func (w *worksetBuilder) next() (txnGroups [][]transactions.SignedTxn) {
	txnCounter := 0 // how many transaction we already included in the current workset.
	// scan starting from the current position until we filled up the workset.
	for i := w.idx; i < len(w.payset); i++ {
		if txnCounter+len(w.payset[i]) > txnPerWorksetThreshold {
			if i == w.idx {
				i++
			}
			txnGroups = w.payset[w.idx:i]
			w.idx = i
			return
		}
		if i == len(w.payset)-1 {
			txnGroups = w.payset[w.idx:]
			w.idx = len(w.payset)
			return
		}
		txnCounter += len(w.payset[i])
	}
	// we can reach here only if w.idx >= len(w.payset). This is not really a usecase, but just
	// for code-completeness, we'll return an empty array here.
	return nil
}

// test to see if we have any more worksets we can extract from our payset.
func (w *worksetBuilder) completed() bool {
	return w.idx >= len(w.payset)
}

// UnverifiedElement is the element passed the Stream verifier
// Context is a reference associated with the txn group which is passed
// with the result
type UnverifiedElement struct {
	TxnGroup []transactions.SignedTxn
	Context  interface{}
}

// VerificationResult is the result of the txn group verification
// Context is a reference associated with the txn group which was
// initially passed to the stream verifier
type VerificationResult struct {
	TxnGroup []transactions.SignedTxn
	Context  interface{}
	Err      error
}

// StreamVerifier verifies txn groups received through the stxnChan channel, and returns the
// results through the resultChan
type StreamVerifier struct {
	seatReturnChan   chan interface{}
	resultChan       chan<- VerificationResult
	stxnChan         <-chan UnverifiedElement
	verificationPool execpool.BacklogPool
	ctx              context.Context
	cache            VerifiedTransactionCache
	pendingTasksWg   sync.WaitGroup
	nbw              *NewBlockWatcher
	ledger           logic.LedgerForSignature
}

// NewBlockWatcher is a struct used to provide a new block header to the
// stream verifier
type NewBlockWatcher struct {
	blkHeader bookkeeping.BlockHeader
	mu        deadlock.RWMutex
}

// MakeNewBlockWatcher construct a new block watcher with the initial blkHdr
func MakeNewBlockWatcher(blkHdr bookkeeping.BlockHeader) (nbw *NewBlockWatcher) {
	nbw = &NewBlockWatcher{
		blkHeader: blkHdr,
	}
	return nbw
}

// OnNewBlock implements the interface to subscribe to new block notifications from the ledger
func (nbw *NewBlockWatcher) OnNewBlock(block bookkeeping.Block, delta ledgercore.StateDelta) {
	if nbw.blkHeader.Round >= block.BlockHeader.Round {
		return
	}
	nbw.mu.Lock()
	defer nbw.mu.Unlock()
	nbw.blkHeader = block.BlockHeader
}

func (nbw *NewBlockWatcher) getBlockHeader() (bh bookkeeping.BlockHeader) {
	nbw.mu.RLock()
	defer nbw.mu.RUnlock()
	return nbw.blkHeader
}

type unverifiedElementList struct {
	elementList []UnverifiedElement
	nbw         *NewBlockWatcher
	ledger      logic.LedgerForSignature
}

func makeUnverifiedElementList(nbw *NewBlockWatcher, ledger logic.LedgerForSignature) (uel unverifiedElementList) {
	uel.nbw = nbw
	uel.ledger = ledger
	uel.elementList = make([]UnverifiedElement, 0)
	return
}

func makeSingleUnverifiedElement(nbw *NewBlockWatcher, ledger logic.LedgerForSignature, ue UnverifiedElement) (uel unverifiedElementList) {
	uel.nbw = nbw
	uel.ledger = ledger
	uel.elementList = []UnverifiedElement{ue}
	return
}

type batchLoad struct {
	txnGroups      [][]transactions.SignedTxn
	groupCtxs      []*GroupContext
	elementContext []interface{}
	messagesForTxn []int
}

func makeBatchLoad() (bl batchLoad) {
	bl.txnGroups = make([][]transactions.SignedTxn, 0)
	bl.groupCtxs = make([]*GroupContext, 0)
	bl.elementContext = make([]interface{}, 0)
	bl.messagesForTxn = make([]int, 0)
	return bl
}

func (bl *batchLoad) addLoad(txngrp []transactions.SignedTxn, gctx *GroupContext, eltctx interface{}, numBatchableSigs int) {
	bl.txnGroups = append(bl.txnGroups, txngrp)
	bl.groupCtxs = append(bl.groupCtxs, gctx)
	bl.elementContext = append(bl.elementContext, eltctx)
	bl.messagesForTxn = append(bl.messagesForTxn, numBatchableSigs)

}

// wait time for another txn should satisfy the following inequality:
// [validation time added to the group by one more txn] + [wait time] <= [validation time of a single txn]
// since these are difficult to estimate, the simplified version could be to assume:
// [validation time added to the group by one more txn] = [validation time of a single txn] / 2
// This gives us:
// [wait time] <= [validation time of a single txn] / 2
const waitForNextTxnDuration = 5 * time.Millisecond
const waitForFirstTxnDuration = 2000 * time.Millisecond

// MakeStreamVerifier creates a new stream verifier and returns the chans used to send txn groups
// to it and obtain the txn signature verification result from
func MakeStreamVerifier(ctx context.Context, stxnChan <-chan UnverifiedElement, resultChan chan<- VerificationResult,
	ledger logic.LedgerForSignature, nbw *NewBlockWatcher, verificationPool execpool.BacklogPool,
	cache VerifiedTransactionCache) (sv *StreamVerifier) {

	// limit the number of tasks queued to the execution pool
	// the purpose of this parameter is to create bigger batches
	// instead of having many smaller batching waiting in the execution pool
	numberOfExecPoolSeats := verificationPool.GetParallelism() * 2

	sv = &StreamVerifier{
		seatReturnChan:   make(chan interface{}, numberOfExecPoolSeats),
		resultChan:       resultChan,
		stxnChan:         stxnChan,
		verificationPool: verificationPool,
		ctx:              ctx,
		cache:            cache,
		nbw:              nbw,
		ledger:           ledger,
	}
	for x := 0; x < numberOfExecPoolSeats; x++ {
		sv.seatReturnChan <- struct{}{}
	}
	return sv
}

// Start is called when the verifier is created and whenever it needs to restart after
// the ctx is canceled
func (sv *StreamVerifier) Start() {
	go sv.batchingLoop()
}

func (sv *StreamVerifier) batchingLoop() {
	timer := time.NewTicker(waitForFirstTxnDuration)
	var added bool
	var numberOfSigsInCurrent uint64
	uel := makeUnverifiedElementList(sv.nbw, sv.ledger)
	for {
		select {
		case stx := <-sv.stxnChan:
			isFirstInBatch := numberOfSigsInCurrent == 0
			numberOfBatchableSigsInGroup, err := getNumberOfBatchableSigsInGroup(stx.TxnGroup)
			if err != nil {
				sv.sendResult(stx.TxnGroup, stx.Context, err)
				continue
			}

			// if no batchable signatures here, send this as a task of its own
			if numberOfBatchableSigsInGroup == 0 {
				// no point in blocking and waiting for a seat here, let it wait in the exec pool
				err := sv.addVerificationTaskToThePool(makeSingleUnverifiedElement(sv.nbw, sv.ledger, stx), false)
				if err != nil {
					sv.sendResult(stx.TxnGroup, stx.Context, err)
				}
				continue
			}

			// add this txngrp to the list of batchable txn groups
			numberOfSigsInCurrent = numberOfSigsInCurrent + numberOfBatchableSigsInGroup
			uel.elementList = append(uel.elementList, stx)
			if numberOfSigsInCurrent > txnPerWorksetThreshold {
				// enough transaction in the batch to efficiently verify

				if numberOfSigsInCurrent > 4*txnPerWorksetThreshold {
					// do not consider adding more txns to this batch.
					// bypass the seat count and block if the exec pool is busy
					// this is to prevent creation of very large batches
					err := sv.addVerificationTaskToThePool(uel, false)
					// if the pool is already terminated
					if err != nil {
						return
					}
					added = true
				} else {
					added = sv.processBatch(uel)
				}
				if added {
					numberOfSigsInCurrent = 0
					uel = makeUnverifiedElementList(sv.nbw, sv.ledger)
					// starting a new batch. Can wait long, since nothing is blocked
					timer.Reset(waitForFirstTxnDuration)
				} else {
					// was not added because no-available-seats. wait for some more txns
					timer.Reset(waitForNextTxnDuration)
				}
			} else {
				if isFirstInBatch {
					// an element is added and is waiting. shorten the waiting time
					timer.Reset(waitForNextTxnDuration)
				}
			}
		case <-timer.C:
			// timer ticked. it is time to send the batch even if it is not full
			if numberOfSigsInCurrent == 0 {
				// nothing batched yet... wait some more
				timer.Reset(waitForFirstTxnDuration)
				continue
			}
			added = sv.processBatch(uel)
			if added {
				numberOfSigsInCurrent = 0
				uel = makeUnverifiedElementList(sv.nbw, sv.ledger)
				// starting a new batch. Can wait long, since nothing is blocked
				timer.Reset(waitForFirstTxnDuration)
			} else {
				// was not added because no-available-seats. wait for some more txns
				timer.Reset(waitForNextTxnDuration)
			}
		case <-sv.ctx.Done():
			if numberOfSigsInCurrent > 0 {
				// send the current accumulated transactions to the pool
				err := sv.addVerificationTaskToThePool(uel, false)
				// if the pool is already terminated
				if err != nil {
					return
				}
			}
			// wait for the pending tasks, then close the result chan
			sv.pendingTasksWg.Wait()
			return
		}
	}
}

func (sv *StreamVerifier) sendResult(veTxnGroup []transactions.SignedTxn, veContext interface{}, err error) {
	vr := VerificationResult{
		TxnGroup: veTxnGroup,
		Context:  veContext,
		Err:      err,
	}
	// send the txn result out the pipe
	sv.resultChan <- vr
	/*
		select {
		case sv.resultChan <- vr:

				// if the channel is not accepting, should not block here
				// report dropped txn. caching is fine, if it comes back in the block
				default:
					fmt.Println("skipped!!")
					//TODO: report this

		}
	*/
}

func (sv *StreamVerifier) processBatch(uel unverifiedElementList) (added bool) {
	// if cannot find a seat, can go back and collect
	// more signatures instead of waiting here
	// more signatures to the batch do not harm performance (see crypto.BenchmarkBatchVerifierBig)
	select {
	case <-sv.seatReturnChan:
		err := sv.addVerificationTaskToThePool(uel, true)
		if err != nil {
			// An error is returned when the context of the pool expires
			// No need to report this
			return false
		}
		return true
	default:
		// if no free seats, wait some more for more txns
		return false
	}
}

func (sv *StreamVerifier) addVerificationTaskToThePool(uel unverifiedElementList, returnSeat bool) error {
	sv.pendingTasksWg.Add(1)
	function := func(arg interface{}) interface{} {
		defer sv.pendingTasksWg.Done()

		if sv.ctx.Err() != nil {
			return sv.ctx.Err()
		}

		uel := arg.(*unverifiedElementList)
		batchVerifier := crypto.MakeBatchVerifier()

		bl := makeBatchLoad()
		// TODO: separate operations here, and get the sig verification inside LogicSig outside
		for _, ue := range uel.elementList {
			groupCtx, err := txnGroupBatchPrep(ue.TxnGroup, uel.nbw.getBlockHeader(), uel.ledger, batchVerifier)
			if err != nil {
				// verification failed, cannot go to the batch. return here.
				sv.sendResult(ue.TxnGroup, ue.Context, err)
				continue
			}
			totalBatchCount := batchVerifier.GetNumberOfEnqueuedSignatures()
			bl.addLoad(ue.TxnGroup, groupCtx, ue.Context, totalBatchCount)
		}

		failed, err := batchVerifier.VerifyWithFeedback()
		if err != nil && err != crypto.ErrBatchHasFailedSigs {
			fmt.Println(err)
			// something bad happened
			// TODO:  report error and discard the batch
		}

		verifiedTxnGroups := make([][]transactions.SignedTxn, len(bl.txnGroups))
		verifiedGroupCtxs := make([]*GroupContext, len(bl.groupCtxs))
		failedSigIdx := 0
		for txgIdx := range bl.txnGroups {
			txGroupSigFailed := false
			// if err == nil, then all sigs are verified, no need to check for the failed
			for err != nil && failedSigIdx < bl.messagesForTxn[txgIdx] {
				if failed[failedSigIdx] {
					// if there is a failed sig check, then no need to check the rest of the
					// sigs for this txnGroup
					failedSigIdx = bl.messagesForTxn[txgIdx]
					txGroupSigFailed = true
				} else {
					// proceed to check the next sig belonging to this txnGroup
					failedSigIdx++
				}
			}
			var result error
			if !txGroupSigFailed {
				verifiedTxnGroups = append(verifiedTxnGroups, bl.txnGroups[txgIdx])
				verifiedGroupCtxs = append(verifiedGroupCtxs, bl.groupCtxs[txgIdx])
			} else {
				result = ErrInvalidSignature
			}
			sv.sendResult(bl.txnGroups[txgIdx], bl.elementContext[txgIdx], result)
		}
		// loading them all at once by locking the cache once
		err = sv.cache.AddPayset(verifiedTxnGroups, verifiedGroupCtxs)
		if err != nil {
			// TODO: handle the error
			fmt.Println(err)
		}
		return struct{}{}
	}
	var retChan chan interface{}
	if returnSeat {
		retChan = sv.seatReturnChan
	}
	// EnqueueBacklog returns an error when the context is canceled
	err := sv.verificationPool.EnqueueBacklog(sv.ctx, function, &uel, retChan)
	return err
}

func getNumberOfBatchableSigsInGroup(stxs []transactions.SignedTxn) (batchSigs uint64, err error) {
	batchSigs = 0
	for _, stx := range stxs {
		count, err := getNumberOfBatchableSigsInTxn(stx)
		if err != nil {
			return 0, err
		}
		batchSigs = batchSigs + count
	}
	return
}

func getNumberOfBatchableSigsInTxn(stx transactions.SignedTxn) (batchSigs uint64, err error) {
	var hasSig, hasMsig bool
	numSigs := 0
	if stx.Sig != (crypto.Signature{}) {
		numSigs++
		hasSig = true
	}
	if !stx.Msig.Blank() {
		numSigs++
		hasMsig = true
	}
	if !stx.Lsig.Blank() {
		numSigs++
	}

	if numSigs == 0 {
		// Special case: special sender address can issue special transaction
		// types (state proof txn) without any signature.  The well-formed
		// check ensures that this transaction cannot pay any fee, and
		// cannot have any other interesting fields, except for the state proof payload.
		if stx.Txn.Sender == transactions.StateProofSender && stx.Txn.Type == protocol.StateProofTx {
			return 0, nil
		}
		return 0, errors.New("signedtxn has no sig")
	}
	if numSigs != 1 {
		return 0, errors.New("signedtxn should only have one of Sig or Msig or LogicSig")
	}
	if hasSig {
		return 1, nil
	}
	if hasMsig {
		sig := stx.Msig
		for _, subsigi := range sig.Subsigs {
			if (subsigi.Sig != crypto.Signature{}) {
				batchSigs++
			}
		}
	}
	return
}
