// Copyright (C) 2019-2026 Algorand Foundation Ltd.
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

package account

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/algorand/go-algorand/util/db"
)

func registryCountRows(a *require.Assertions, registry *participationDB, table string) (n int) {
	err := registry.store.Rdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
	})
	a.NoError(err)
	return n
}

func registryReadVotingBlob(a *require.Assertions, registry *participationDB, id ParticipationID) (raw []byte) {
	err := registry.store.Rdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRow(selectRollingVotingByID, id[:]).Scan(new(int64), &raw)
	})
	a.NoError(err)
	return raw
}

// TestRegistryMigrationV1ToV2 hand-builds a version-1 registry (whole voting
// blob in Rolling.voting) and verifies opening it converts to per-subkey rows
// with identical restored secrets.
func TestRegistryMigrationV1ToV2(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	// mid-life key so both batch and offset rows exist
	p := makeTestParticipation(a, 1, 1, 200, 10)
	p.Voting.DeleteBeforeFineGrained(basics.OneTimeIDForRound(55, 10), 10)
	a.NotEmpty(p.Voting.Offsets)
	votingBlob := protocol.Encode(p.Voting)

	rootDB, err := db.OpenPair(t.Name(), true)
	a.NoError(err)

	// build the version-1 schema by hand and insert a record with the legacy blob
	err = rootDB.Wdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		if err := dbSchemaUpgrade0(ctx, tx, true); err != nil {
			return err
		}
		if _, err := db.SetUserVersion(ctx, tx, 1); err != nil {
			return err
		}
		id := p.ID()
		result, err := tx.Exec(insertKeysetQuery, id[:], p.Parent[:], p.FirstValid, p.LastValid, p.KeyDilution,
			protocol.Encode(p.VRF), protocol.Encode(&p.StateProofSecrets.SignerContext))
		if err != nil {
			return err
		}
		pk, err := result.LastInsertId()
		if err != nil {
			return err
		}
		_, err = tx.Exec(insertRollingQuery, pk, votingBlob)
		return err
	})
	a.NoError(err)

	// opening the registry runs dbSchemaUpgrade1
	registry, err := makeParticipationRegistry(rootDB, logging.TestingLog(t))
	a.NoError(err)
	defer registryCloseTest(t, registry, "")

	err = rootDB.Rdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		version, err := db.GetUserVersion(ctx, tx)
		a.Equal(int32(2), version)
		return err
	})
	a.NoError(err)

	a.Equal(len(p.Voting.Batches), registryCountRows(a, registry, "VotingBatches"))
	a.Equal(len(p.Voting.Offsets), registryCountRows(a, registry, "VotingOffsets"))

	// blob now holds scalars only
	var scalars crypto.OneTimeSignatureSecrets
	a.NoError(protocol.Decode(registryReadVotingBlob(a, registry, p.ID()), &scalars))
	a.Empty(scalars.Batches)
	a.Empty(scalars.Offsets)

	// cache built from the converted store equals the original secrets
	record := registry.Get(p.ID())
	a.False(record.IsZero())
	a.Equal(encodedVotingSnapshot(p.Voting), encodedVotingSnapshot(record.Voting))
}

// TestRegistryInsertMidLifeKey verifies inserting a key that already has
// expanded offsets stores and restores them.
func TestRegistryInsertMidLifeKey(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	p := makeTestParticipation(a, 1, 1, 200, 10)
	p.Voting.DeleteBeforeFineGrained(basics.OneTimeIDForRound(37, 10), 10)
	a.NotEmpty(p.Voting.Offsets)

	id, err := registry.Insert(p)
	a.NoError(err)
	a.NoError(registry.Flush(defaultTimeout))

	a.Equal(len(p.Voting.Batches), registryCountRows(a, registry, "VotingBatches"))
	a.Equal(len(p.Voting.Offsets), registryCountRows(a, registry, "VotingOffsets"))

	// reload from disk and compare
	a.NoError(registry.initializeCache())
	record := registry.Get(id)
	a.False(record.IsZero())
	a.Equal(encodedVotingSnapshot(p.Voting), encodedVotingSnapshot(record.Voting))
}

// TestRegistryDeleteExpiredPersistsReassembly walks rounds through
// DeleteExpired+Flush and verifies the store reassembles to exactly the
// cached secrets each round, including across batch rollovers.
func TestRegistryDeleteExpiredPersistsReassembly(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	p := makeTestParticipation(a, 1, 1, 200, 10)
	id, err := registry.Insert(p)
	a.NoError(err)
	a.NoError(registry.Register(id, 1))
	a.NoError(registry.Flush(defaultTimeout))

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	for round := basics.Round(2); round <= 60; round += 7 {
		a.NoError(registry.DeleteExpired(round, proto))
		a.NoError(registry.Flush(defaultTimeout))

		cached := registry.Get(id)
		a.False(cached.IsZero())

		a.NoError(registry.initializeCache())
		reloaded := registry.Get(id)
		a.False(reloaded.IsZero())
		a.Equal(encodedVotingSnapshot(cached.Voting), encodedVotingSnapshot(reloaded.Voting), "round %d", round)
	}
}

// TestRegistryEndOfLifeClearsRows verifies the registry erases every voting
// subkey row when a key on its final batch moves past its end while still
// within its validity window (the transition where the scalars don't move).
func TestRegistryEndOfLifeClearsRows(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	// LastValid 309 with dilution 10 puts the end of validity at the very end
	// of the final batch (30), so the end-of-life transition happens while the
	// record is still in the registry.
	p := makeTestParticipation(a, 1, 1, 309, 10)
	id, err := registry.Insert(p)
	a.NoError(err)
	a.NoError(registry.Register(id, 1))
	a.NoError(registry.Flush(defaultTimeout))

	proto := config.Consensus[protocol.ConsensusCurrentVersion]

	// expand the final batch
	a.NoError(registry.DeleteExpired(300, proto))
	a.NoError(registry.Flush(defaultTimeout))
	a.NotZero(registryCountRows(a, registry, "VotingOffsets"))

	// move past the end of the key: offsets are cleared without scalar movement
	a.NoError(registry.DeleteExpired(309, proto))
	a.NoError(registry.Flush(defaultTimeout))
	a.Zero(registryCountRows(a, registry, "VotingOffsets"), "retired offset subkeys survived in the registry")
	a.Zero(registryCountRows(a, registry, "VotingBatches"))

	// a cache rebuild must not resurrect any subkeys
	a.NoError(registry.initializeCache())
	record := registry.Get(id)
	a.False(record.IsZero())
	a.Empty(record.Voting.Offsets)
	a.Empty(record.Voting.Batches)
}

// TestRegistryExcludesCorruptRecord verifies a record whose subkey rows were
// lost is excluded from the cache with a warning instead of blocking the
// whole registry (and the node) from loading, while healthy records survive.
func TestRegistryExcludesCorruptRecord(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	pHealthy := makeTestParticipation(a, 1, 1, 200, 10)
	healthyID, err := registry.Insert(pHealthy)
	a.NoError(err)
	pCorrupt := makeTestParticipation(a, 2, 1, 200, 10)
	corruptID, err := registry.Insert(pCorrupt)
	a.NoError(err)
	a.NoError(registry.Flush(defaultTimeout))
	a.NoError(registry.initializeCache())

	// damage the second key's rows
	err = registry.store.Wdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM VotingBatches WHERE batch=(SELECT MAX(batch) FROM VotingBatches) AND pk=(SELECT pk FROM Keysets WHERE participationID=?)", corruptID[:])
		return err
	})
	a.NoError(err)

	a.NoError(registry.initializeCache())
	a.True(registry.Get(corruptID).IsZero(), "corrupt record not excluded")
	a.False(registry.Get(healthyID).IsZero(), "healthy record lost")

	// re-inserting the excluded key (as loadParticipationKeys does from the
	// .partkey file in the same startup) must replace the orphaned rows, not
	// create a duplicate Keysets row that would fail every flush with
	// ErrMultipleKeysForID
	reinsertedID, err := registry.Insert(pCorrupt)
	a.NoError(err)
	a.Equal(corruptID, reinsertedID)
	a.NoError(registry.Flush(defaultTimeout))

	var keysetRows int
	err = registry.store.Rdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRow("SELECT count(*) FROM Keysets WHERE participationID=?", corruptID[:]).Scan(&keysetRows)
	})
	a.NoError(err)
	a.Equal(1, keysetRows, "duplicate Keysets row after re-insert")

	// the next round's deletion flush works for every key
	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(registry.DeleteExpired(1, proto))
	a.NoError(registry.Flush(defaultTimeout))

	a.NoError(registry.initializeCache())
	a.False(registry.Get(corruptID).IsZero(), "re-inserted record not restored")
	a.False(registry.Get(healthyID).IsZero(), "healthy record lost after re-insert")
}

// TestInsertNeverRewindsCursor verifies re-inserting a lagging copy of a key
// (the .partkey file and the registry are independent stores) cannot rewind
// the persisted deletion cursor and resurrect retired rounds: the inserted
// copy is fast-forwarded to the stored cursor instead.
func TestInsertNeverRewindsCursor(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	const dilution = 10
	p := makeTestParticipation(a, 1, 1, 3000, dilution)
	// an independent copy of the same key, which will lag behind
	behind := p
	behindVoting := p.Voting.Snapshot()
	behind.Voting = &behindVoting

	id, err := registry.Insert(p)
	a.NoError(err)
	proto := config.Consensus[protocol.ConsensusCurrentVersion]

	// vote through round 999: the stored cursor advances to batch 101
	a.NoError(registry.DeleteExpired(999, proto))
	a.NoError(registry.Flush(defaultTimeout))

	// evict the key from the cache the way the corrupt-record exclusion does
	registry.mutex.Lock()
	delete(registry.cache, id)
	delete(registry.dirty, id)
	registry.mutex.Unlock()

	// the lagging copy only reached round 500; re-insert it
	behind.Voting.DeleteBeforeFineGrained(basics.OneTimeIDForRound(500, dilution), dilution)
	reinsertedID, err := registry.Insert(behind)
	a.NoError(err)
	a.Equal(id, reinsertedID)
	a.NoError(registry.Flush(defaultTimeout))

	// the persisted cursor did not rewind
	var storedScalars crypto.OneTimeSignatureSecrets
	a.NoError(protocol.Decode(registryReadVotingBlob(a, registry, id), &storedScalars))
	a.GreaterOrEqual(storedScalars.FirstBatch, uint64(101), "persisted deletion cursor rewound")

	// after a reload, retired rounds cannot produce valid signatures while
	// live rounds still can
	a.NoError(registry.initializeCache())
	record := registry.Get(id)
	a.False(record.IsZero())
	msg := crypto.OneTimeSignatureSubkeyBatchID{Batch: 1}
	retired := basics.OneTimeIDForRound(500, dilution)
	sig := record.Voting.Sign(retired, msg)
	a.False(p.Voting.OneTimeSignatureVerifier.Verify(retired, msg, sig), "retired round signed after re-inserting a lagging copy")
	live := basics.OneTimeIDForRound(1500, dilution)
	sig = record.Voting.Sign(live, msg)
	a.True(p.Voting.OneTimeSignatureVerifier.Verify(live, msg, sig), "live round unusable after fast-forward")
}

// TestFlushSelfHealsInconsistentRows verifies one key with rows inconsistent
// with its scalars does not block the flush for every key: the applier
// rebuilds the damaged key's rows from memory and the flush succeeds.
func TestFlushSelfHealsInconsistentRows(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	pA := makeTestParticipation(a, 1, 1, 200, 10)
	idA, err := registry.Insert(pA)
	a.NoError(err)
	pB := makeTestParticipation(a, 2, 1, 200, 10)
	idB, err := registry.Insert(pB)
	a.NoError(err)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(registry.DeleteExpired(20, proto))
	a.NoError(registry.Flush(defaultTimeout))

	// lose one of B's offset rows behind the registry's back
	err = registry.store.Wdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM VotingOffsets WHERE off=(SELECT MIN(off) FROM VotingOffsets) AND pk=(SELECT pk FROM Keysets WHERE participationID=?)", idB[:])
		return err
	})
	a.NoError(err)

	// the next flush trims offsets, detects the inconsistency, and rebuilds
	a.NoError(registry.DeleteExpired(23, proto))
	a.NoError(registry.Flush(defaultTimeout))

	// both keys reload consistent with the cache
	cachedA, cachedB := registry.Get(idA), registry.Get(idB)
	a.NoError(registry.initializeCache())
	a.Equal(encodedVotingSnapshot(cachedA.Voting), encodedVotingSnapshot(registry.Get(idA).Voting))
	a.Equal(encodedVotingSnapshot(cachedB.Voting), encodedVotingSnapshot(registry.Get(idB).Voting))
}

// TestFlushWritesDeltaOnly verifies a flush with no voting-key progress
// leaves the voting blob and subkey rows untouched.
func TestFlushWritesDeltaOnly(t *testing.T) {
	partitiontest.PartitionTest(t)
	a := require.New(t)

	registry, dbfile := getRegistry(t)
	defer registryCloseTest(t, registry, dbfile)

	p := makeTestParticipation(a, 1, 1, 200, 10)
	id, err := registry.Insert(p)
	a.NoError(err)
	a.NoError(registry.Register(id, 1))

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(registry.DeleteExpired(10, proto))
	a.NoError(registry.Flush(defaultTimeout))

	blobBefore := registryReadVotingBlob(a, registry, id)
	batchesBefore := registryCountRows(a, registry, "VotingBatches")
	offsetsBefore := registryCountRows(a, registry, "VotingOffsets")

	// dirty the record without advancing the voting keys
	a.NoError(registry.Record(p.Parent, 10, Vote))
	a.NoError(registry.Flush(defaultTimeout))

	a.Equal(blobBefore, registryReadVotingBlob(a, registry, id))
	a.Equal(batchesBefore, registryCountRows(a, registry, "VotingBatches"))
	a.Equal(offsetsBefore, registryCountRows(a, registry, "VotingOffsets"))

	// and the rolling fields did land
	record := registry.Get(id)
	a.Equal(basics.Round(10), record.LastVote)
}
