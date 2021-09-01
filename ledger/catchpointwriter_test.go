// Copyright (C) 2019-2021 Algorand, Inc.
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

package ledger

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/ledger/store"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
)

func makeString(len int) string {
	s := ""
	for i := 0; i < len; i++ {
		s += string(byte(i))
	}
	return s
}

func makeTestEncodedBalanceRecord(t *testing.T) store.EncodedBalanceRecord {
	er := store.EncodedBalanceRecord{}
	hash := crypto.Hash([]byte{1, 2, 3})
	copy(er.Address[:], hash[:])
	oneTimeSecrets := crypto.GenerateOneTimeSignatureSecrets(0, 1)
	vrfSecrets := crypto.GenerateVRFSecrets()
	ad := basics.AccountData{
		Status:             basics.NotParticipating,
		MicroAlgos:         basics.MicroAlgos{},
		RewardsBase:        0x1234123412341234,
		RewardedMicroAlgos: basics.MicroAlgos{},
		VoteID:             oneTimeSecrets.OneTimeSignatureVerifier,
		SelectionID:        vrfSecrets.PK,
		VoteFirstValid:     basics.Round(0x1234123412341234),
		VoteLastValid:      basics.Round(0x1234123412341234),
		VoteKeyDilution:    0x1234123412341234,
		AssetParams:        make(map[basics.AssetIndex]basics.AssetParams),
		Assets:             make(map[basics.AssetIndex]basics.AssetHolding),
		AuthAddr:           basics.Address(crypto.Hash([]byte{1, 2, 3, 4})),
	}
	currentConsensusParams := config.Consensus[protocol.ConsensusCurrentVersion]

	for assetCreatorAssets := 0; assetCreatorAssets < currentConsensusParams.MaxAssetsPerAccount; assetCreatorAssets++ {
		ap := basics.AssetParams{
			Total:         0x1234123412341234,
			Decimals:      0x12341234,
			DefaultFrozen: true,
			UnitName:      makeString(currentConsensusParams.MaxAssetUnitNameBytes),
			AssetName:     makeString(currentConsensusParams.MaxAssetNameBytes),
			URL:           makeString(currentConsensusParams.MaxAssetURLBytes),
			Manager:       basics.Address(crypto.Hash([]byte{1, byte(assetCreatorAssets)})),
			Reserve:       basics.Address(crypto.Hash([]byte{2, byte(assetCreatorAssets)})),
			Freeze:        basics.Address(crypto.Hash([]byte{3, byte(assetCreatorAssets)})),
			Clawback:      basics.Address(crypto.Hash([]byte{4, byte(assetCreatorAssets)})),
		}
		copy(ap.MetadataHash[:], makeString(32))
		ad.AssetParams[basics.AssetIndex(0x1234123412341234-assetCreatorAssets)] = ap
	}

	for assetHolderAssets := 0; assetHolderAssets < currentConsensusParams.MaxAssetsPerAccount; assetHolderAssets++ {
		ah := basics.AssetHolding{
			Amount: 0x1234123412341234,
			Frozen: true,
		}
		ad.Assets[basics.AssetIndex(0x1234123412341234-assetHolderAssets)] = ah
	}

	maxApps := currentConsensusParams.MaxAppsCreated
	maxOptIns := currentConsensusParams.MaxAppsOptedIn
	maxKeyBytesLen := currentConsensusParams.MaxAppKeyLen
	maxSumBytesLen := currentConsensusParams.MaxAppSumKeyValueLens

	genKey := func() (string, basics.TealValue) {
		len := int(crypto.RandUint64() % uint64(maxKeyBytesLen))
		if len == 0 {
			return "k", basics.TealValue{Type: basics.TealUintType, Uint: 0}
		}
		key := make([]byte, maxSumBytesLen-len)
		crypto.RandBytes(key)
		return string(key), basics.TealValue{Type: basics.TealUintType, Bytes: string(key)}
	}
	startIndex := crypto.RandUint64() % 100000
	ad.AppParams = make(map[basics.AppIndex]basics.AppParams, maxApps)
	for aidx := startIndex; aidx < startIndex+uint64(maxApps); aidx++ {
		ap := basics.AppParams{}
		ap.GlobalState = make(basics.TealKeyValue)
		for i := uint64(0); i < currentConsensusParams.MaxGlobalSchemaEntries/4; i++ {
			k, v := genKey()
			ap.GlobalState[k] = v
		}
		ad.AppParams[basics.AppIndex(aidx)] = ap
		optins := maxApps
		if maxApps > maxOptIns {
			optins = maxOptIns
		}
		ad.AppLocalStates = make(map[basics.AppIndex]basics.AppLocalState, optins)
		keys := currentConsensusParams.MaxLocalSchemaEntries / 4
		lkv := make(basics.TealKeyValue, keys)
		for i := 0; i < optins; i++ {
			for j := uint64(0); j < keys; j++ {
				k, v := genKey()
				lkv[k] = v
			}
		}
		ad.AppLocalStates[basics.AppIndex(aidx)] = basics.AppLocalState{KeyValue: lkv}
	}

	encodedAd := ad.MarshalMsg(nil)
	er.AccountData = encodedAd
	return er
}

func TestEncodedBalanceRecordEncoding(t *testing.T) {
	partitiontest.PartitionTest(t)

	er := makeTestEncodedBalanceRecord(t)
	encodedBr := er.MarshalMsg(nil)

	var er2 store.EncodedBalanceRecord
	_, err := er2.UnmarshalMsg(encodedBr)
	require.NoError(t, err)

	require.Equal(t, er, er2)
}

func TestCatchpointFileBalancesChunkEncoding(t *testing.T) {
	partitiontest.PartitionTest(t)

	fbc := catchpointFileBalancesChunk{}
	for i := 0; i < 512; i++ {
		fbc.Balances = append(fbc.Balances, makeTestEncodedBalanceRecord(t))
	}
	encodedFbc := fbc.MarshalMsg(nil)

	var fbc2 catchpointFileBalancesChunk
	_, err := fbc2.UnmarshalMsg(encodedFbc)
	require.NoError(t, err)

	require.Equal(t, fbc, fbc2)
}

func TestBasicCatchpointWriter(t *testing.T) {
	partitiontest.PartitionTest(t)

	// create new protocol version, which has lower lookback
	testProtocolVersion := protocol.ConsensusVersion("test-protocol-TestBasicCatchpointWriter")
	protoParams := config.Consensus[protocol.ConsensusCurrentVersion]
	protoParams.MaxBalLookback = 32
	protoParams.SeedLookback = 2
	protoParams.SeedRefreshInterval = 8
	config.Consensus[testProtocolVersion] = protoParams
	temporaryDirectroy, _ := ioutil.TempDir(os.TempDir(), "catchpoints")
	defer func() {
		delete(config.Consensus, testProtocolVersion)
		os.RemoveAll(temporaryDirectroy)
	}()

	ml := makeMockLedgerForTracker(t, true, 10, testProtocolVersion)
	defer ml.Close()
	accts := randomAccounts(300, false)

	au := &accountUpdates{}
	conf := config.GetDefaultLocal()
	conf.CatchpointInterval = 1
	conf.Archival = true
	au.initialize(conf, ".", protoParams, accts)
	defer au.close()
	err := au.loadFromDisk(ml)
	require.NoError(t, err)
	au.close()
	fileName := filepath.Join(temporaryDirectroy, "15.catchpoint")
	blocksRound := basics.Round(12345)
	blockHeaderDigest := crypto.Hash([]byte{1, 2, 3})
	catchpointLabel := fmt.Sprintf("%d#%v", blocksRound, blockHeaderDigest) // this is not a correct way to create a label, but it's good enough for this unit test

	readDb := ml.trackerDB().Rdb
	err = readDb.Atomic(func(ctx context.Context, tx *sql.Tx) (err error) {
		writer := makeCatchpointWriter(context.Background(), fileName, tx, blocksRound, blockHeaderDigest, catchpointLabel)
		for {
			more, err := writer.WriteStep(context.Background())
			require.NoError(t, err)
			if !more {
				break
			}
		}
		return
	})
	require.NoError(t, err)

	// load the file from disk.
	fileContent, err := ioutil.ReadFile(fileName)
	require.NoError(t, err)
	gzipReader, err := gzip.NewReader(bytes.NewBuffer(fileContent))
	require.NoError(t, err)
	tarReader := tar.NewReader(gzipReader)
	defer gzipReader.Close()
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			break
		}
		balancesBlockBytes := make([]byte, header.Size)
		readComplete := int64(0)

		for readComplete < header.Size {
			bytesRead, err := tarReader.Read(balancesBlockBytes[readComplete:])
			readComplete += int64(bytesRead)
			if err != nil {
				if err == io.EOF {
					if readComplete == header.Size {
						break
					}
					require.NoError(t, err)
				}
				break
			}
		}

		if header.Name == "content.msgpack" {
			var fileHeader CatchpointFileHeader
			err = protocol.Decode(balancesBlockBytes, &fileHeader)
			require.NoError(t, err)
			require.Equal(t, catchpointLabel, fileHeader.Catchpoint)
			require.Equal(t, blocksRound, fileHeader.BlocksRound)
			require.Equal(t, blockHeaderDigest, fileHeader.BlockHeaderDigest)
			require.Equal(t, uint64(len(accts)), fileHeader.TotalAccounts)
		} else if header.Name == "balances.1.1.msgpack" {
			var balances catchpointFileBalancesChunk
			err = protocol.Decode(balancesBlockBytes, &balances)
			require.NoError(t, err)
			require.Equal(t, uint64(len(accts)), uint64(len(balances.Balances)))
		} else {
			require.Failf(t, "unexpected tar chunk name", "tar chunk name %s", header.Name)
		}
	}
}

func TestFullCatchpointWriter(t *testing.T) {
	partitiontest.PartitionTest(t)

	// create new protocol version, which has lower lookback
	testProtocolVersion := protocol.ConsensusVersion("test-protocol-TestFullCatchpointWriter")
	protoParams := config.Consensus[protocol.ConsensusCurrentVersion]
	protoParams.MaxBalLookback = 32
	protoParams.SeedLookback = 2
	protoParams.SeedRefreshInterval = 8
	config.Consensus[testProtocolVersion] = protoParams
	temporaryDirectroy, _ := ioutil.TempDir(os.TempDir(), "catchpoints")
	defer func() {
		delete(config.Consensus, testProtocolVersion)
		os.RemoveAll(temporaryDirectroy)
	}()

	ml := makeMockLedgerForTracker(t, true, 10, testProtocolVersion)
	defer ml.Close()
	accts := randomAccounts(BalancesPerCatchpointFileChunk*3, false)

	au := &accountUpdates{}
	conf := config.GetDefaultLocal()
	conf.CatchpointInterval = 1
	conf.Archival = true
	au.initialize(conf, ".", protoParams, accts)
	defer au.close()
	err := au.loadFromDisk(ml)
	require.NoError(t, err)
	au.close()
	fileName := filepath.Join(temporaryDirectroy, "15.catchpoint")
	blocksRound := basics.Round(12345)
	blockHeaderDigest := crypto.Hash([]byte{1, 2, 3})
	catchpointLabel := fmt.Sprintf("%d#%v", blocksRound, blockHeaderDigest) // this is not a correct way to create a label, but it's good enough for this unit test
	readDb := ml.trackerDB().Rdb
	err = readDb.Atomic(func(ctx context.Context, tx *sql.Tx) (err error) {
		writer := makeCatchpointWriter(context.Background(), fileName, tx, blocksRound, blockHeaderDigest, catchpointLabel)
		for {
			more, err := writer.WriteStep(context.Background())
			require.NoError(t, err)
			if !more {
				break
			}
		}
		return
	})
	require.NoError(t, err)

	// create a ledger.
	var initState InitState
	initState.Block.CurrentProtocol = protocol.ConsensusCurrentVersion
	l, err := OpenLedger(ml.log, "TestFullCatchpointWriter", true, initState, conf)
	require.NoError(t, err)
	defer l.Close()
	accessor := MakeCatchpointCatchupAccessor(l, l.log)

	err = accessor.ResetStagingBalances(context.Background(), true)
	require.NoError(t, err)

	// load the file from disk.
	fileContent, err := ioutil.ReadFile(fileName)
	require.NoError(t, err)
	gzipReader, err := gzip.NewReader(bytes.NewBuffer(fileContent))
	require.NoError(t, err)
	tarReader := tar.NewReader(gzipReader)
	var catchupProgress CatchpointCatchupAccessorProgress
	defer gzipReader.Close()
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			break
		}
		balancesBlockBytes := make([]byte, header.Size)
		readComplete := int64(0)

		for readComplete < header.Size {
			bytesRead, err := tarReader.Read(balancesBlockBytes[readComplete:])
			readComplete += int64(bytesRead)
			if err != nil {
				if err == io.EOF {
					if readComplete == header.Size {
						break
					}
					require.NoError(t, err)
				}
				break
			}
		}
		err = accessor.ProgressStagingBalances(context.Background(), header.Name, balancesBlockBytes, &catchupProgress)
		require.NoError(t, err)
	}

	err = l.trackerDBs.Wdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		err := applyCatchpointStagingBalances(ctx, tx, 0)
		return err
	})
	require.NoError(t, err)

	// verify that the account data aligns with what we originally stored :
	for addr, acct := range accts {
		acctData, validThrough, err := l.LookupWithoutRewards(0, addr)
		require.NoError(t, err)
		require.Equal(t, acct, acctData)
		require.Equal(t, basics.Round(0), validThrough)
	}
}

func benchmarkWriteCatchpointStagingBalancesSub(b *testing.B, ascendingOrder bool) {
	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	genesisInitState, _ := testGenerateInitState(b, protocol.ConsensusCurrentVersion, 100)
	const inMem = false
	log := logging.TestingLog(b)
	cfg := config.GetDefaultLocal()
	cfg.Archival = false
	log.SetLevel(logging.Warn)
	dbBaseFileName := strings.Replace(b.Name(), "/", "_", -1)
	l, err := OpenLedger(log, dbBaseFileName, inMem, genesisInitState, cfg)
	require.NoError(b, err, "could not open ledger")
	defer func() {
		l.Close()
		os.Remove(dbBaseFileName + ".block.sqlite")
		os.Remove(dbBaseFileName + ".tracker.sqlite")
	}()
	catchpointAccessor := MakeCatchpointCatchupAccessor(l, log)
	catchpointAccessor.ResetStagingBalances(context.Background(), true)
	targetAccountsCount := uint64(b.N)
	accountsLoaded := uint64(0)
	var last64KStart time.Time
	last64KSize := uint64(0)
	last64KAccountCreationTime := time.Duration(0)
	accountsWritingStarted := time.Now()
	accountsGenerationDuration := time.Duration(0)
	b.ResetTimer()
	for accountsLoaded < targetAccountsCount {
		b.StopTimer()
		balancesLoopStart := time.Now()
		// generate a chunk;
		chunkSize := targetAccountsCount - accountsLoaded
		if chunkSize > BalancesPerCatchpointFileChunk {
			chunkSize = BalancesPerCatchpointFileChunk
		}
		last64KSize += chunkSize
		if accountsLoaded >= targetAccountsCount-64*1024 && last64KStart.IsZero() {
			last64KStart = time.Now()
			last64KSize = chunkSize
			last64KAccountCreationTime = time.Duration(0)
		}
		var balances catchpointFileBalancesChunk
		balances.Balances = make([]store.EncodedBalanceRecord, chunkSize)
		for i := uint64(0); i < chunkSize; i++ {
			var randomAccount store.EncodedBalanceRecord
			accountData := basics.AccountData{RewardsBase: accountsLoaded + i}
			accountData.MicroAlgos.Raw = crypto.RandUint63()
			randomAccount.AccountData = protocol.Encode(&accountData)
			crypto.RandBytes(randomAccount.Address[:])
			if ascendingOrder {
				binary.LittleEndian.PutUint64(randomAccount.Address[:], accountsLoaded+i)
			}
			balances.Balances[i] = randomAccount
		}
		balanceLoopDuration := time.Now().Sub(balancesLoopStart)
		last64KAccountCreationTime += balanceLoopDuration
		accountsGenerationDuration += balanceLoopDuration

		normalizedAccountBalances, err := prepareNormalizedBalances(balances.Balances, proto)
		b.StartTimer()
		err = l.trackerDBs.Wdb.Atomic(func(ctx context.Context, tx *sql.Tx) (err error) {
			err = writeCatchpointStagingBalances(ctx, tx, normalizedAccountBalances)
			return
		})

		require.NoError(b, err)
		accountsLoaded += chunkSize
	}
	if !last64KStart.IsZero() {
		last64KDuration := time.Now().Sub(last64KStart) - last64KAccountCreationTime
		fmt.Printf("%-82s%-7d (last 64k) %-6d ns/account       %d accounts/sec\n", b.Name(), last64KSize, (last64KDuration / time.Duration(last64KSize)).Nanoseconds(), int(float64(last64KSize)/float64(last64KDuration.Seconds())))
	}
	stats, err := l.trackerDBs.Wdb.Vacuum(context.Background())
	require.NoError(b, err)
	fmt.Printf("%-82sdb fragmentation   %.1f%%\n", b.Name(), float32(stats.PagesBefore-stats.PagesAfter)*100/float32(stats.PagesBefore))
	b.ReportMetric(float64(b.N)/float64((time.Now().Sub(accountsWritingStarted)-accountsGenerationDuration).Seconds()), "accounts/sec")
}

func BenchmarkWriteCatchpointStagingBalances(b *testing.B) {
	benchSizes := []int{1024 * 100, 1024 * 200, 1024 * 400}
	for _, size := range benchSizes {
		b.Run(fmt.Sprintf("RandomInsertOrder-%d", size), func(b *testing.B) {
			b.N = size
			benchmarkWriteCatchpointStagingBalancesSub(b, false)
		})
	}
	for _, size := range benchSizes {
		b.Run(fmt.Sprintf("AscendingInsertOrder-%d", size), func(b *testing.B) {
			b.N = size
			benchmarkWriteCatchpointStagingBalancesSub(b, true)
		})
	}
}
