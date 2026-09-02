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
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/algorand/go-algorand/util/db"
)

// makeSmallTestKey creates a participation key with a small dilution so batch
// rollovers happen quickly in tests.
func makeSmallTestKey(t *testing.T, a *require.Assertions, first, last basics.Round, dilution uint64) (PersistedParticipation, db.Accessor) {
	partDB, err := db.MakeAccessor(t.Name()+"_part", false, true)
	a.NoError(err)

	var addr basics.Address
	crypto.RandBytes(addr[:])
	part, err := FillDBWithParticipationKeys(partDB, addr, first, last, dilution)
	a.NoError(err)
	return part, partDB
}

func countTableRows(a *require.Assertions, store db.Accessor, table string) (n int) {
	err := store.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRow("SELECT count(*) FROM " + table).Scan(&n)
	})
	a.NoError(err)
	return n
}

func readVotingColumn(a *require.Assertions, store db.Accessor) (raw []byte) {
	err := store.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		return tx.QueryRow("SELECT voting FROM ParticipationAccount").Scan(&raw)
	})
	a.NoError(err)
	return raw
}

func encodedVotingSnapshot(secrets *crypto.OneTimeSignatureSecrets) []byte {
	snap := secrets.Snapshot()
	return protocol.Encode(&snap)
}

func setupTestDBAtVer3(partDB db.Accessor, part Participation) error {
	rawVRF := protocol.Encode(part.VRF)
	voting := part.Voting.Snapshot()
	rawVoting := protocol.Encode(&voting)
	rawStateProof := protocol.Encode(part.StateProofSecrets)

	return partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TABLE ParticipationAccount (
		parent BLOB,

		vrf BLOB,
		voting BLOB,

		firstValid INTEGER,
		lastValid INTEGER,

		keyDilution INTEGER NOT NULL DEFAULT 0,
		stateProof BLOB
	);`)
		if err != nil {
			return err
		}

		if err := setupSchemaForTest(tx, 3); err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO ParticipationAccount (parent, vrf, voting, firstValid, lastValid, keyDilution, stateProof) VALUES (?, ?, ?, ?, ?, ?, ?)",
			part.Parent[:], rawVRF, rawVoting, part.FirstValid, part.LastValid, part.KeyDilution, rawStateProof)
		return err
	})
}

func TestMigrateFromVersion3(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)

	// build a mid-life key: offsets expanded, some batches consumed
	part, tmpDB := makeSmallTestKey(t, a, 0, 300, 10)
	defer closeDBS(tmpDB)
	part.Voting.DeleteBeforeFineGrained(basics.OneTimeIDForRound(55, 10), 10)
	a.NotEmpty(part.Voting.Offsets)
	a.NotZero(part.Voting.FirstBatch)

	partDB, err := db.MakeAccessor(t.Name()+"_v3", false, true)
	a.NoError(err)
	defer closeDBS(partDB)

	a.NoError(setupTestDBAtVer3(partDB, part.Participation))
	a.NoError(Migrate(partDB))

	versions, err := getSchemaVersions(partDB)
	a.NoError(err)
	a.Equal(PartTableSchemaVersion, versions[PartTableSchemaName])
	a.NoError(testDBContainsAllColumns(partDB))

	a.Equal(len(part.Voting.Batches), countTableRows(a, partDB, "OtsBatches"))
	a.Equal(len(part.Voting.Offsets), countTableRows(a, partDB, "OtsOffsets"))

	// voting column now holds scalars only
	var scalars crypto.OneTimeSignatureSecrets
	a.NoError(protocol.Decode(readVotingColumn(a, partDB), &scalars))
	a.Empty(scalars.Batches)
	a.Empty(scalars.Offsets)
	a.Equal(part.Voting.FirstBatch, scalars.FirstBatch)
	a.Equal(part.Voting.FirstOffset, scalars.FirstOffset)

	// full restore equals the original
	restored, err := RestoreParticipation(partDB)
	a.NoError(err)
	a.Equal(encodedVotingSnapshot(part.Voting), encodedVotingSnapshot(restored.Voting))
	a.Equal(part.Parent, restored.Parent)
	a.Equal(part.KeyDilution, restored.KeyDilution)
}

func TestComputeVotingDelta(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 8

	secrets := crypto.GenerateOneTimeSignatureSecrets(0, 10)
	secrets.DeleteBeforeFineGrained(crypto.OneTimeSignatureIdentifier{Batch: 2, Offset: 3}, dilution)
	current, _, _ := secrets.PersistentState()

	// noop: persisted state matches memory
	d, err := computeVotingDelta(&current, secrets)
	a.NoError(err)
	a.True(d.noop)

	// same-batch advance
	older := current
	older.FirstOffset = current.FirstOffset - 2
	d, err = computeVotingDelta(&older, secrets)
	a.NoError(err)
	a.False(d.noop)
	a.False(d.fullRewrite)
	a.False(d.replaceAllOffsets)
	a.Equal(current.FirstOffset, d.deleteOffsetsBelow)
	a.Equal(int64(2), d.expectedOffsetDeletes)
	a.Zero(d.deleteBatchesBelow)
	a.Empty(d.insertBatches)
	a.Empty(d.insertOffsets)
	a.NotNil(d.newScalars)

	// batch rollover (single and multi-batch jump behave identically)
	prevBatch := current
	prevBatch.FirstBatch = current.FirstBatch - 2
	prevBatch.FirstOffset = 5
	d, err = computeVotingDelta(&prevBatch, secrets)
	a.NoError(err)
	a.False(d.noop)
	a.False(d.fullRewrite)
	a.True(d.replaceAllOffsets)
	a.Equal(current.FirstBatch, d.deleteBatchesBelow)
	a.Equal(int64(2), d.expectedBatchDeletes)
	a.Empty(d.insertBatches)
	a.Equal(len(secrets.Offsets), len(d.insertOffsets))
	a.Equal(current.FirstBatch-1, d.offsetsBatch)
	a.NotNil(d.newScalars)

	// nil old: full rewrite
	d, err = computeVotingDelta(nil, secrets)
	a.NoError(err)
	a.True(d.fullRewrite)
	a.Equal(len(secrets.Batches), len(d.insertBatches))
	a.Equal(len(secrets.Offsets), len(d.insertOffsets))
	a.Equal(current.FirstBatch-1, d.offsetsBatch)
	a.NotNil(d.newScalars)

	// legacy whole-blob persisted state: full rewrite
	legacy := secrets.Snapshot().OneTimeSignatureSecretsPersistent
	a.NotEmpty(legacy.Batches)
	d, err = computeVotingDelta(&legacy, secrets)
	a.NoError(err)
	a.True(d.fullRewrite)

	// persisted state ahead of memory: forward security forbids moving the
	// deletion cursor backward, so this is an error rather than a rewrite
	ahead := current
	ahead.FirstBatch = current.FirstBatch + 1
	_, err = computeVotingDelta(&ahead, secrets)
	a.ErrorContains(err, "refusing to resurrect")
	aheadOffset := current
	aheadOffset.FirstOffset = current.FirstOffset + 1
	_, err = computeVotingDelta(&aheadOffset, secrets)
	a.ErrorContains(err, "refusing to resurrect")
	// a legacy blob ahead of memory is refused as well
	legacyAhead := legacy
	legacyAhead.FirstBatch = current.FirstBatch + 1
	_, err = computeVotingDelta(&legacyAhead, secrets)
	a.ErrorContains(err, "refusing to resurrect")

	// end-of-key-life: moving past the final batch consumes its remaining
	// offsets and advances FirstOffset to the batch end, so exhaustion is
	// distinguishable from a live final batch and persists as an exact trim
	spent := crypto.GenerateOneTimeSignatureSecrets(0, 4)
	spent.DeleteBeforeFineGrained(crypto.OneTimeSignatureIdentifier{Batch: 3, Offset: 2}, dilution)
	a.NotEmpty(spent.Offsets) // final batch expanded
	a.Empty(spent.Batches)
	persisted, _, numOffsets := spent.PersistentState()
	spent.DeleteBeforeFineGrained(crypto.OneTimeSignatureIdentifier{Batch: 4, Offset: 0}, dilution)
	a.Empty(spent.Offsets)
	afterState, _, _ := spent.PersistentState()
	a.Equal(persisted.FirstBatch, afterState.FirstBatch)
	a.Equal(uint64(dilution), afterState.FirstOffset)
	d, err = computeVotingDelta(&persisted, spent)
	a.NoError(err)
	a.Equal(uint64(dilution), d.deleteOffsetsBelow)
	a.Equal(int64(numOffsets), d.expectedOffsetDeletes)

	// an exhausted key whose stored cursor did not move (legacy encoding of
	// exhaustion) still gets its rows cleared rather than a noop
	d, err = computeVotingDelta(&afterState, spent)
	a.NoError(err)
	a.False(d.noop)
	a.True(d.clearAllRows)
	a.Nil(d.newScalars)
}

// TestDeleteOldKeysFailsClosedOnCorruptScalars verifies an undecodable
// persisted cursor refuses to write (a rewrite from possibly-stale memory
// could resurrect retired keys) and marks the file as corrupt for quarantine.
func TestDeleteOldKeysFailsClosedOnCorruptScalars(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(<-part.DeleteOldKeys(basics.Round(25), proto))
	batchRows := countTableRows(a, partDB, "OtsBatches")
	offsetRows := countTableRows(a, partDB, "OtsOffsets")

	err := partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE ParticipationAccount SET voting=?", []byte{0xff, 0x00})
		return err
	})
	a.NoError(err)

	err = <-part.DeleteOldKeys(basics.Round(26), proto)
	a.ErrorContains(err, "undecodable")
	a.Equal(batchRows, countTableRows(a, partDB, "OtsBatches"), "rows rewritten despite undecodable cursor")
	a.Equal(offsetRows, countTableRows(a, partDB, "OtsOffsets"))

	_, err = RestoreParticipationUnmigrated(partDB)
	a.ErrorIs(err, ErrCorruptedVotingData)
}

func TestDeleteOldKeysIncremental(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]

	// a fresh Persist stores one row per batch subkey, no offsets, and a
	// scalar-only voting column (a whole-secrets blob would silently defeat
	// the row-oriented format through the legacy tolerance)
	a.Equal(len(part.Voting.Batches), countTableRows(a, partDB, "OtsBatches"))
	a.Zero(countTableRows(a, partDB, "OtsOffsets"))
	var freshScalars crypto.OneTimeSignatureSecrets
	a.NoError(protocol.Decode(readVotingColumn(a, partDB), &freshScalars))
	a.Empty(freshScalars.Batches)
	a.Empty(freshScalars.Offsets)

	prevBatchRows := countTableRows(a, partDB, "OtsBatches")
	for r := basics.Round(1); r <= 120; r++ {
		firstBatchBefore := part.Voting.FirstBatch
		a.NoError(<-part.DeleteOldKeys(r, proto))

		// persisted state reconstructs to exactly the in-memory state
		restored, err := RestoreParticipationUnmigrated(partDB)
		a.NoError(err)
		a.Equal(encodedVotingSnapshot(part.Voting), encodedVotingSnapshot(restored.Voting), "round %d", r)

		// batch rows only churn when a batch is consumed (expanded into offsets)
		batchRows := countTableRows(a, partDB, "OtsBatches")
		if part.Voting.FirstBatch == firstBatchBefore {
			a.Equal(prevBatchRows, batchRows, "batch rows changed off-rollover at round %d", r)
		} else {
			a.Less(batchRows, prevBatchRows, "batch rows not trimmed at rollover round %d", r)
		}
		prevBatchRows = batchRows
	}
}

// TestDeleteOldKeysEndOfLife walks a key past its final batch and verifies
// every subkey row is erased from the file — the forward-security guarantee
// at the end-of-key-life transition, where DeleteBeforeFineGrained clears the
// remaining offsets without advancing the scalars.
func TestDeleteOldKeysEndOfLife(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution) // batches 0..30, coverage through round 309
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]

	// expand the final batch (a multi-batch jump from the fresh state), then
	// move past the end of the key
	a.NoError(<-part.DeleteOldKeys(basics.Round(305), proto))
	a.NotZero(countTableRows(a, partDB, "OtsOffsets"))
	midRestore, err := RestoreParticipationUnmigrated(partDB)
	a.NoError(err)
	a.Equal(encodedVotingSnapshot(part.Voting), encodedVotingSnapshot(midRestore.Voting))
	a.NoError(<-part.DeleteOldKeys(basics.Round(311), proto))

	a.Empty(part.Voting.Offsets)
	a.Zero(countTableRows(a, partDB, "OtsOffsets"), "retired offset subkeys survived on disk")
	a.Zero(countTableRows(a, partDB, "OtsBatches"))

	// a fresh restore must not resurrect any signing capability
	restored, err := RestoreParticipationUnmigrated(partDB)
	a.NoError(err)
	a.Empty(restored.Voting.Offsets)
	a.Empty(restored.Voting.Batches)
	id := basics.OneTimeIDForRound(305, dilution)
	msg := crypto.OneTimeSignatureSubkeyBatchID{Batch: 1}
	sig := restored.Voting.Sign(id, msg)
	a.False(part.Voting.OneTimeSignatureVerifier.Verify(id, msg, sig), "restored secrets signed a retired round")

	// subsequent rounds on the dead key stay cheap and consistent
	a.NoError(<-part.DeleteOldKeys(basics.Round(315), proto))
	a.Zero(countTableRows(a, partDB, "OtsOffsets"))
}

// TestDeleteOldKeysRefusesStaleDisk verifies the forward-security monotonic
// guard: when the persisted deletion cursor is ahead of memory, the write is
// refused instead of resurrecting deleted keys.
func TestDeleteOldKeysRefusesStaleDisk(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(<-part.DeleteOldKeys(basics.Round(25), proto))
	batchRows := countTableRows(a, partDB, "OtsBatches")
	offsetRows := countTableRows(a, partDB, "OtsOffsets")

	// plant a persisted cursor from the future
	future, _, _ := part.Voting.PersistentState()
	future.FirstBatch += 5
	err := partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE ParticipationAccount SET voting=?", protocol.Encode(&future))
		return err
	})
	a.NoError(err)

	err = <-part.DeleteOldKeys(basics.Round(26), proto)
	a.ErrorContains(err, "refusing to resurrect")

	// nothing was written
	a.Equal(batchRows, countTableRows(a, partDB, "OtsBatches"))
	a.Equal(offsetRows, countTableRows(a, partDB, "OtsOffsets"))
}

// TestRestoreDetectsCorruptRows verifies damaged subkey tables are reported
// as corruption instead of loading a key that silently cannot vote.
func TestRestoreDetectsCorruptRows(t *testing.T) {
	partitiontest.PartitionTest(t)

	cases := []struct {
		name      string
		tamperSQL string
		wantErr   string
	}{
		{"missingBatchRow", "DELETE FROM OtsBatches WHERE batch=(SELECT MAX(batch) FROM OtsBatches)", "missing or extra rows"},
		{"wrongOffsetBatch", "UPDATE OtsOffsets SET batch=batch+1 WHERE off=(SELECT MIN(off) FROM OtsOffsets)", "expected batch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := require.New(t)
			const dilution = 10
			part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
			defer closeDBS(partDB)

			proto := config.Consensus[protocol.ConsensusCurrentVersion]
			a.NoError(<-part.DeleteOldKeys(basics.Round(25), proto))

			// sanity: loads fine before the damage
			_, err := RestoreParticipationUnmigrated(partDB)
			a.NoError(err)

			err = partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
				_, err := tx.Exec(tc.tamperSQL)
				return err
			})
			a.NoError(err)
			_, err = RestoreParticipationUnmigrated(partDB)
			a.ErrorContains(err, tc.wantErr)
			// the sentinel lets the node quarantine the file as *.old
			a.ErrorIs(err, ErrCorruptedVotingData)
		})
	}
}

// TestDeleteOldKeysSelfHealsInconsistentRows verifies a file whose rows drift
// from its scalars is rebuilt from memory by the next deletion instead of
// failing every round.
func TestDeleteOldKeysSelfHealsInconsistentRows(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(<-part.DeleteOldKeys(basics.Round(25), proto))

	// lose one offset row behind the node's back
	err := partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.Exec("DELETE FROM OtsOffsets WHERE off=(SELECT MIN(off) FROM OtsOffsets)")
		return err
	})
	a.NoError(err)

	a.NoError(<-part.DeleteOldKeys(basics.Round(26), proto))

	restored, err := RestoreParticipationUnmigrated(partDB)
	a.NoError(err)
	a.Equal(encodedVotingSnapshot(part.Voting), encodedVotingSnapshot(restored.Voting))
}

// TestMigrationRollsBackOnFailure verifies a failing v3-to-v4 migration
// leaves the file at version 3 with none of the new tables behind.
func TestMigrationRollsBackOnFailure(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	part, tmpDB := makeSmallTestKey(t, a, 0, 300, 10)
	defer closeDBS(tmpDB)

	partDB, err := db.MakeAccessor(t.Name()+"_v3", false, true)
	a.NoError(err)
	defer closeDBS(partDB)
	a.NoError(setupTestDBAtVer3(partDB, part.Participation))

	// mangle the voting blob so the conversion fails mid-transaction
	err = partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		var raw []byte
		if err := tx.QueryRow("SELECT voting FROM ParticipationAccount").Scan(&raw); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE ParticipationAccount SET voting=?", raw[:len(raw)/2])
		return err
	})
	a.NoError(err)

	a.Error(Migrate(partDB))

	// the whole migration transaction rolled back
	versions, err := getSchemaVersions(partDB)
	a.NoError(err)
	a.Equal(3, versions[PartTableSchemaName])
	err = partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name IN ('OtsBatches', 'OtsOffsets')").Scan(&n); err != nil {
			return err
		}
		require.Zero(t, n, "migration tables survived the rollback")
		return nil
	})
	a.NoError(err)
}
