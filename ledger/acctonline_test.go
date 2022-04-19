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

package ledger

import (
	"testing"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/bookkeeping"
	"github.com/algorand/go-algorand/ledger/ledgercore"
	ledgertesting "github.com/algorand/go-algorand/ledger/testing"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/stretchr/testify/require"
)

// TestAcctOnline checks the online accounts tracker correctly stores accont change history
// 1. Start with 1000 online accounts
// 2. Every round set one of them offline
// 3. Ensure the DB and the base cache are up to date (report them offline)
// 4. Ensure expiration works
func TestAcctOnline(t *testing.T) {
	partitiontest.PartitionTest(t)

	const seedLookback = 1
	const seedInteval = 1
	const maxBalLookback = 2 * seedLookback * seedInteval
	const maxDeltaLookback = maxBalLookback // TODO: change

	const numAccts = maxBalLookback * 10
	allAccts := make([]basics.BalanceRecord, numAccts)
	genesisAccts := []map[basics.Address]basics.AccountData{{}}
	genesisAccts[0] = make(map[basics.Address]basics.AccountData, numAccts)
	for i := 0; i < numAccts; i++ {
		allAccts[i] = basics.BalanceRecord{
			Addr:        ledgertesting.RandomAddress(),
			AccountData: ledgertesting.RandomOnlineAccountData(0),
		}
		genesisAccts[0][allAccts[i].Addr] = allAccts[i].AccountData
	}

	pooldata := basics.AccountData{}
	pooldata.MicroAlgos.Raw = 100 * 1000 * 1000 * 1000 * 1000
	pooldata.Status = basics.NotParticipating
	genesisAccts[0][testPoolAddr] = pooldata

	sinkdata := basics.AccountData{}
	sinkdata.MicroAlgos.Raw = 1000 * 1000 * 1000 * 1000
	sinkdata.Status = basics.NotParticipating
	genesisAccts[0][testSinkAddr] = sinkdata

	testProtocolVersion := protocol.ConsensusVersion("test-protocol-TestAcctOnline")
	protoParams := config.Consensus[protocol.ConsensusCurrentVersion]
	protoParams.MaxBalLookback = maxBalLookback
	protoParams.SeedLookback = seedLookback
	protoParams.SeedRefreshInterval = seedInteval
	config.Consensus[testProtocolVersion] = protoParams
	defer func() {
		delete(config.Consensus, testProtocolVersion)
	}()

	ml := makeMockLedgerForTracker(t, true, 1, testProtocolVersion, genesisAccts)
	defer ml.Close()

	conf := config.GetDefaultLocal()
	au, oa := newAcctUpdates(t, ml, conf, ".")
	defer oa.close()

	_, totals, err := au.LatestTotals()
	require.NoError(t, err)

	for _, bal := range allAccts {
		data, err := oa.accountsq.lookupOnline(bal.Addr, 0)
		require.NoError(t, err)
		require.Equal(t, bal.Addr, data.addr)
		require.Equal(t, basics.Round(0), data.round)
		require.Equal(t, bal.AccountData.MicroAlgos, data.accountData.MicroAlgos)
		require.Equal(t, bal.AccountData.RewardsBase, data.accountData.RewardsBase)
		require.Equal(t, bal.AccountData.VoteFirstValid, data.accountData.VoteFirstValid)
		require.Equal(t, bal.AccountData.VoteLastValid, data.accountData.VoteLastValid)

		oad, err := oa.lookupOnlineAccountData(0, bal.Addr)
		require.NoError(t, err)
		require.NotEmpty(t, oad)
	}

	commitSync := func(rnd basics.Round) {
		_, maxLookback := oa.committedUpTo(rnd)
		dcc := &deferredCommitContext{
			deferredCommitRange: deferredCommitRange{
				lookback: maxLookback,
			},
		}
		cdr := ml.trackers.produceCommittingTask(rnd, ml.trackers.dbRound, &dcc.deferredCommitRange)
		if cdr != nil {
			func() {
				dcc.deferredCommitRange = *cdr
				ml.trackers.accountsWriting.Add(1)

				// do not take any locks since all operations are synchronous
				newBase := basics.Round(dcc.offset) + dcc.oldBase
				dcc.newBase = newBase
				err = ml.trackers.commitRound(dcc)
				require.NoError(t, err)
			}()

		}
	}

	newBlock := func(rnd basics.Round, base map[basics.Address]basics.AccountData, updates ledgercore.AccountDeltas, prevTotals ledgercore.AccountTotals) (newTotals ledgercore.AccountTotals) {
		rewardLevel := uint64(0)
		newTotals = ledgertesting.CalculateNewRoundAccountTotals(t, updates, rewardLevel, protoParams, base, prevTotals)

		blk := bookkeeping.Block{
			BlockHeader: bookkeeping.BlockHeader{
				Round: basics.Round(rnd),
			},
		}
		blk.RewardsLevel = rewardLevel
		blk.CurrentProtocol = testProtocolVersion
		delta := ledgercore.MakeStateDelta(&blk.BlockHeader, 0, updates.Len(), 0)
		delta.Accts.MergeAccounts(updates)
		delta.Totals = totals

		ml.trackers.newBlock(blk, delta)

		return newTotals
	}

	// the test 1 requires 2 blocks with different resource state,
	// oa requires MaxBalLookback block to start persisting
	// TODO: change MaxBalLookback to the actual lookback parameter
	const numAcctsStage1 = 10
	numConsumedStage1 := basics.Round(maxDeltaLookback) + numAcctsStage1
	targetRound := numConsumedStage1
	for i := basics.Round(1); i <= targetRound; i++ {
		var updates ledgercore.AccountDeltas
		acctIdx := int(i) - 1

		updates.Upsert(allAccts[acctIdx].Addr, ledgercore.AccountData{AccountBaseData: ledgercore.AccountBaseData{Status: basics.Offline}, VotingData: ledgercore.VotingData{}})

		base := genesisAccts[i-1]
		newAccts := applyPartialDeltas(base, updates)
		genesisAccts = append(genesisAccts, newAccts)

		// prepare block
		totals = newBlock(i, base, updates, totals)

		// commit changes synchroniously
		commitSync(i)

		// check the table data and the cache
		// data gets committed after maxDeltaLookback
		if i > maxDeltaLookback {
			rnd := i - maxDeltaLookback
			acctIdx := int(rnd) - 1
			bal := allAccts[acctIdx]
			data, err := oa.accountsq.lookupOnline(bal.Addr, rnd)
			require.NoError(t, err)
			require.Equal(t, bal.Addr, data.addr)
			require.NotEmpty(t, data.rowid)
			require.Equal(t, oa.cachedDBRoundOnline, data.round)
			require.Empty(t, data.accountData)

			data, has := oa.baseOnlineAccounts.read(bal.Addr)
			require.True(t, has)
			require.NotEmpty(t, data.rowid)
			require.Empty(t, data.accountData)

			oad, err := oa.lookupOnlineAccountData(rnd, bal.Addr)
			require.NoError(t, err)
			require.Empty(t, oad)

			// check the prev original row is still there
			data, err = oa.accountsq.lookupOnline(bal.Addr, rnd-1)
			require.NoError(t, err)
			require.Equal(t, bal.Addr, data.addr)
			require.NotEmpty(t, data.rowid)
			require.Equal(t, oa.cachedDBRoundOnline, data.round)
			require.NotEmpty(t, data.accountData)
		}

		// check data gets expired and removed from the DB
		// account 0 is set to Offline at round 1
		// and set expired at X = 1 + MaxBalLookback (= 3)
		// actual removal happens when X is committed i.e. at round X + maxDeltaLookback (= 5)
		if i > maxDeltaLookback+maxDeltaLookback {
			rnd := i - (maxDeltaLookback + maxDeltaLookback)
			acctIdx := int(rnd) - 1
			bal := allAccts[acctIdx]
			data, err := oa.accountsq.lookupOnline(bal.Addr, rnd)
			require.NoError(t, err)
			require.Equal(t, bal.Addr, data.addr)
			require.Empty(t, data.rowid)
			require.Equal(t, oa.cachedDBRoundOnline, data.round)
			require.Empty(t, data.accountData)

			data, has := oa.baseOnlineAccounts.read(bal.Addr)
			require.True(t, has)
			require.Empty(t, data.rowid)
			require.Empty(t, data.accountData)

			// TODO: restore after introducing lookback
			// roundOffset fails with round 1 before dbRound 3
			// oad, err := oa.lookupOnlineAccountData(rnd, bal.Addr)
			// require.NoError(t, err)
			// require.Empty(t, oad)
		}
	}

	// ensure rounds
	require.Equal(t, targetRound, au.latest())
	require.Equal(t, basics.Round(numAcctsStage1), oa.cachedDBRoundOnline)

	// at this point we should have maxBalLookback last accounts of numAcctsStage1
	// to be in the DB and in the cache and not yet removed
	for i := numAcctsStage1 - maxBalLookback; i < numAcctsStage1; i++ {
		bal := allAccts[i]
		// we expire account i at round i+1
		data, err := oa.accountsq.lookupOnline(bal.Addr, basics.Round(i+1))
		require.NoError(t, err)
		require.Equal(t, bal.Addr, data.addr)
		require.NotEmpty(t, data.rowid)
		require.Equal(t, oa.cachedDBRoundOnline, data.round)
		require.Empty(t, data.accountData)

		data, has := oa.baseOnlineAccounts.read(bal.Addr)
		require.True(t, has)
		require.NotEmpty(t, data.rowid)
		require.Empty(t, data.accountData)

		// TODO: restore after introducing lookback
		// oad, err := oa.lookupOnlineAccountData(basics.Round(i+1), bal.Addr)
		// require.NoError(t, err)
		// require.Empty(t, oad)

		// ensure the online entry is still in the DB for the round i
		data, err = oa.accountsq.lookupOnline(bal.Addr, basics.Round(i))
		require.NoError(t, err)
		require.Equal(t, bal.Addr, data.addr)
		require.NotEmpty(t, data.rowid)
		require.Equal(t, oa.cachedDBRoundOnline, data.round)
		require.NotEmpty(t, data.accountData)
	}

	// check maxDeltaLookback accounts in in-memory deltas, check it
	for i := numAcctsStage1; i < numAcctsStage1+maxDeltaLookback; i++ {
		bal := allAccts[i]
		oad, err := oa.lookupOnlineAccountData(basics.Round(i+1), bal.Addr)
		require.NoError(t, err)
		require.Empty(t, oad)

		// the table has old values b/c not committed yet
		data, err := oa.accountsq.lookupOnline(bal.Addr, basics.Round(i))
		require.NoError(t, err)
		require.Equal(t, bal.Addr, data.addr)
		require.NotEmpty(t, data.rowid)
		require.Equal(t, oa.cachedDBRoundOnline, data.round)
		require.NotEmpty(t, data.accountData)

		// the base cache also does not have such entires
		data, has := oa.baseOnlineAccounts.read(bal.Addr)
		require.False(t, has)
		require.Empty(t, data)
	}
}
