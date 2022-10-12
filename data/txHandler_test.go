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
	"encoding/binary"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/components/mocks"
	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/data/pools"
	"github.com/algorand/go-algorand/data/transactions"
	"github.com/algorand/go-algorand/data/transactions/verify"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/network"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/algorand/go-algorand/util/execpool"
)

func BenchmarkTxHandlerProcessing(b *testing.B) {
	const numUsers = 100
	log := logging.TestingLog(b)
	log.SetLevel(logging.Warn)
	secrets := make([]*crypto.SignatureSecrets, numUsers)
	addresses := make([]basics.Address, numUsers)

	genesis := make(map[basics.Address]basics.AccountData)
	for i := 0; i < numUsers; i++ {
		secret := keypair()
		addr := basics.Address(secret.SignatureVerifier)
		secrets[i] = secret
		addresses[i] = addr
		genesis[addr] = basics.AccountData{
			Status:     basics.Online,
			MicroAlgos: basics.MicroAlgos{Raw: 10000000000000},
		}
	}

	genesis[poolAddr] = basics.AccountData{
		Status:     basics.NotParticipating,
		MicroAlgos: basics.MicroAlgos{Raw: config.Consensus[protocol.ConsensusCurrentVersion].MinBalance},
	}

	require.Equal(b, len(genesis), numUsers+1)
	genBal := bookkeeping.MakeGenesisBalances(genesis, sinkAddr, poolAddr)
	ledgerName := fmt.Sprintf("%s-mem-%d", b.Name(), b.N)
	const inMem = true
	cfg := config.GetDefaultLocal()
	cfg.Archival = true
	ledger, err := LoadLedger(log, ledgerName, inMem, protocol.ConsensusCurrentVersion, genBal, genesisID, genesisHash, nil, cfg)
	require.NoError(b, err)

	l := ledger

	cfg.TxPoolSize = 75000
	cfg.EnableProcessBlockStats = false
	tp := pools.MakeTransactionPool(l.Ledger, cfg, logging.Base())
	backlogPool := execpool.MakeBacklog(nil, 0, execpool.LowPriority, nil)
	txHandler := MakeTxHandler(tp, l, &mocks.MockNetwork{}, "", crypto.Digest{}, backlogPool)

	makeTxns := func(N int) [][]transactions.SignedTxn {
		ret := make([][]transactions.SignedTxn, 0, N)
		for u := 0; u < N; u++ {
			// generate transactions
			tx := transactions.Transaction{
				Type: protocol.PaymentTx,
				Header: transactions.Header{
					Sender:     addresses[u%numUsers],
					Fee:        basics.MicroAlgos{Raw: proto.MinTxnFee * 2},
					FirstValid: 0,
					LastValid:  basics.Round(proto.MaxTxnLife),
					Note:       make([]byte, 2),
				},
				PaymentTxnFields: transactions.PaymentTxnFields{
					Receiver: addresses[(u+1)%numUsers],
					Amount:   basics.MicroAlgos{Raw: mockBalancesMinBalance + (rand.Uint64() % 10000)},
				},
			}
			signedTx := tx.Sign(secrets[u%numUsers])
			ret = append(ret, []transactions.SignedTxn{signedTx})
		}
		return ret
	}

	b.Run("processDecoded", func(b *testing.B) {
		signedTransactionGroups := makeTxns(b.N)
		b.ResetTimer()
		for i := range signedTransactionGroups {
			txHandler.processDecoded(signedTransactionGroups[i])
		}
	})
	b.Run("verify.TxnGroup", func(b *testing.B) {
		signedTransactionGroups := makeTxns(b.N)
		b.ResetTimer()
		// make a header including only the fields needed by PrepareGroupContext
		hdr := bookkeeping.BlockHeader{}
		hdr.FeeSink = basics.Address{}
		hdr.RewardsPool = basics.Address{}
		hdr.CurrentProtocol = protocol.ConsensusCurrentVersion
		vtc := vtCache{}
		b.Logf("verifying %d signedTransactionGroups", len(signedTransactionGroups))
		b.ResetTimer()
		for i := range signedTransactionGroups {
			verify.TxnGroup(signedTransactionGroups[i], hdr, vtc, l)
		}
	})
}

func BenchmarkIncomingTxHandlerProcessing(b *testing.B) {
	const numUsers = 100
	log := logging.TestingLog(b)
	log.SetLevel(logging.Warn)
	secrets := make([]*crypto.SignatureSecrets, numUsers)
	addresses := make([]basics.Address, numUsers)

	genesis := make(map[basics.Address]basics.AccountData)
	for i := 0; i < numUsers; i++ {
		secret := keypair()
		addr := basics.Address(secret.SignatureVerifier)
		secrets[i] = secret
		addresses[i] = addr
		genesis[addr] = basics.AccountData{
			Status:     basics.Online,
			MicroAlgos: basics.MicroAlgos{Raw: 10000000000000},
		}
	}

	genesis[poolAddr] = basics.AccountData{
		Status:     basics.NotParticipating,
		MicroAlgos: basics.MicroAlgos{Raw: config.Consensus[protocol.ConsensusCurrentVersion].MinBalance},
	}

	require.Equal(b, len(genesis), numUsers+1)
	genBal := bookkeeping.MakeGenesisBalances(genesis, sinkAddr, poolAddr)
	ledgerName := fmt.Sprintf("%s-mem-%d", b.Name(), b.N)
	const inMem = true
	cfg := config.GetDefaultLocal()
	cfg.Archival = true
	ledger, err := LoadLedger(log, ledgerName, inMem, protocol.ConsensusCurrentVersion, genBal, genesisID, genesisHash, nil, cfg)
	require.NoError(b, err)

	l := ledger

	cfg.TxPoolSize = 75000
	cfg.EnableProcessBlockStats = false
	tp := pools.MakeTransactionPool(l.Ledger, cfg, logging.Base())
	backlogPool := execpool.MakeBacklog(nil, 0, execpool.LowPriority, nil)
	txHandler := MakeTxHandler(tp, l, &mocks.MockNetwork{}, "", crypto.Digest{}, backlogPool)

	var outChan chan *txBacklogMsg
	
	// Make a test backlog worker, which is simiar to backlogWorker, but sends the results
	// through the outChan instead of passing it to postprocessCheckedTxn
	go func() {
		defer handler.backlogWg.Done()
		for {
			// prioritize the postVerificationQueue
			select {
			case wi, ok := <-handler.postVerificationQueue:
				if !ok {
					return
				}
				outChan <- wi
				// restart the loop so that we could empty out the post verification queue.
				continue
			default:
			}

			// we have no more post verification items. wait for either backlog queue item or post verification item.
			select {
			case wi, ok := <-handler.backlogQueue:
				if !ok {
					return
				}
				if handler.checkAlreadyCommitted(wi) {
					continue
				}

				handler.streamVerifierChan <- verify.VerificationElement{TxnGroup: wi.unverifiedTxGroup, Context: wi}

			case wi, ok := <-handler.postVerificationQueue:
				if !ok {
					return
				}
				outChan <- wi

			case <-handler.ctx.Done():
				return
			}
		}
	}()

	goodTxnGroups := make(map[Uint64], interface{})
	badTxnGroups := make(map[Uint64], interface{})
	makeSignedTxnGroups := func(N int) [][]transactions.SignedTxn {
		maxGrpSize := proto.MaxTxGroupSize
		ret := make([][]transactions.SignedTxn, 0, N)
		for u := 0; u < N; u++ {
			grpSize := rand.Intn(maxGrpSize)
			var txGroup transactions.TxGroup
			txns := make([]transactions.Transaction, grpSize)
			for g := 0; g < grpSize; g++ {
				// generate transactions
				noteField := make([]byte, binary.MaxVarintLen64)
				binary.PutUvarint(noteField, u)
				tx := transactions.Transaction{
					Type: protocol.PaymentTx,
					Header: transactions.Header{
						Sender:     addresses[(u+g)%numUsers],
						Fee:        basics.MicroAlgos{Raw: proto.MinTxnFee * 2},
						FirstValid: 0,
						LastValid:  basics.Round(proto.MaxTxnLife),
						Note:       noteField,
					},
					PaymentTxnFields: transactions.PaymentTxnFields{
						Receiver: addresses[(u+g+1)%numUsers],
						Amount:   basics.MicroAlgos{Raw: mockBalancesMinBalance + (rand.Uint64() % 10000)},
					},
				}
				txGroup.TxGroupHashes = append(txGroup.TxGroupHashes, crypto.HashObj(tx))
				txns = append(txns, tx)
			}
			groupHash := crypto.HashObj(txGroup)
			signedTxGroup = make([]transactions.SignedTxn, 0, grpSize)
			for g, txn := range txns {
				txn.Group = groupHash
				signedTx := txn.Sign(secrets[(u+g)%numUsers])
				signedTxGroup = append(signedTxGroup, signedTx)
			}
			// randomly make bad signatures
			if rand.Float32() < 0.3 {
				signedTxGroup[rand.Intn(grpSize)].Sig[0] = signedTxGroup[rand.Intn(grpSize)].Sig[0] + 1
				badTxnGroups[u] = struct{}{}
			} else {
				goodTxnGroups[u] = struct{}{}
			}
			ret = append(ret, signedTxGroup)
		}
		return ret
	}

	b.Run("handleIncomingTxns", func(b *testing.B) {
		signedTransactionGroups := makeSignedTxnGroups(b.N)
		encodedSignedTransactionGroups := make([]IncomingMessage, b.N)
		for _, stxngrp := range signedTransactionGroups {
			encodedSignedTransactionGroups =
				append(encodedSignedTransactionGroups, IncomingMessage{Data: protocol.Encode(&stxngrp)})
		}
		b.ResetTimer()
		for i := range encodedSignedTransactionGroups {
			txHandler.processIncomingTxn(encodedSignedTransactionGroups[i])
		}
	})
	b.Run("verifyIncomingTxns", func(b *testing.B) {
		signedTransactionGroups := makeSignedTxnGroups(b.N)
		b.ResetTimer()
		for wi := range outChan {
			
		}
	})
}

// vtCache is a noop VerifiedTransactionCache
type vtCache struct{}

func (vtCache) Add(txgroup []transactions.SignedTxn, groupCtx *verify.GroupContext) {}
func (vtCache) AddPayset(txgroup [][]transactions.SignedTxn, groupCtxs []*verify.GroupContext) error {
	return nil
}
func (vtCache) GetUnverifiedTransactionGroups(payset [][]transactions.SignedTxn, CurrSpecAddrs transactions.SpecialAddresses, CurrProto protocol.ConsensusVersion) [][]transactions.SignedTxn {
	return nil
}
func (vtCache) UpdatePinned(pinnedTxns map[transactions.Txid]transactions.SignedTxn) error {
	return nil
}
func (vtCache) Pin(txgroup []transactions.SignedTxn) error { return nil }

func BenchmarkTimeAfter(b *testing.B) {
	b.StopTimer()
	b.ResetTimer()
	deadline := time.Now().Add(5 * time.Second)
	after := 0
	before := 0
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		if time.Now().After(deadline) {
			after++
		} else {
			before++
		}
	}
}

func makeRandomTransactions(num int) ([]transactions.SignedTxn, []byte) {
	stxns := make([]transactions.SignedTxn, num)
	result := make([]byte, 0, num*200)
	for i := 0; i < num; i++ {
		var sig crypto.Signature
		crypto.RandBytes(sig[:])
		var addr basics.Address
		crypto.RandBytes(addr[:])
		stxns[i] = transactions.SignedTxn{
			Sig:      sig,
			AuthAddr: addr,
			Txn: transactions.Transaction{
				Header: transactions.Header{
					Sender: addr,
					Fee:    basics.MicroAlgos{Raw: crypto.RandUint64()},
					Note:   sig[:],
				},
				PaymentTxnFields: transactions.PaymentTxnFields{
					Receiver: addr,
					Amount:   basics.MicroAlgos{Raw: crypto.RandUint64()},
				},
			},
		}

		d2 := protocol.Encode(&stxns[i])
		result = append(result, d2...)
	}
	return stxns, result
}

func TestTxHandlerProcessIncomingTxn(t *testing.T) {
	partitiontest.PartitionTest(t)

	const numTxns = 11
	handler := TxHandler{
		backlogQueue: make(chan *txBacklogMsg, 1),
	}
	stxns, blob := makeRandomTransactions(numTxns)
	action := handler.processIncomingTxn(network.IncomingMessage{Data: blob})
	require.Equal(t, network.OutgoingMessage{Action: network.Ignore}, action)

	require.Equal(t, 1, len(handler.backlogQueue))
	msg := <-handler.backlogQueue
	require.Equal(t, numTxns, len(msg.unverifiedTxGroup))
	for i := 0; i < numTxns; i++ {
		require.Equal(t, stxns[i], msg.unverifiedTxGroup[i])
	}
}

const benchTxnNum = 25_000

func BenchmarkTxHandlerDecoder(b *testing.B) {
	_, blob := makeRandomTransactions(benchTxnNum)
	var err error
	stxns := make([]transactions.SignedTxn, benchTxnNum+1)
	for i := 0; i < b.N; i++ {
		dec := protocol.NewDecoderBytes(blob)
		var idx int
		for {
			err = dec.Decode(&stxns[idx])
			if err == io.EOF {
				break
			}
			require.NoError(b, err)
			idx++
		}
		require.Equal(b, benchTxnNum, idx)
	}
}

func BenchmarkTxHandlerDecoderMsgp(b *testing.B) {
	_, blob := makeRandomTransactions(benchTxnNum)
	var err error
	stxns := make([]transactions.SignedTxn, benchTxnNum+1)
	for i := 0; i < b.N; i++ {
		dec := protocol.NewMsgpDecoderBytes(blob)
		var idx int
		for {
			err = dec.Decode(&stxns[idx])
			if err == io.EOF {
				break
			}
			require.NoError(b, err)
			idx++
		}
		require.Equal(b, benchTxnNum, idx)
	}
}
