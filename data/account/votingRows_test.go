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

func TestPersistCreatesV4(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	part, partDB := makeSmallTestKey(t, a, 0, 300, 10)
	defer closeDBS(partDB)

	versions, err := getSchemaVersions(partDB)
	a.NoError(err)
	a.Equal(PartTableSchemaVersion, versions[PartTableSchemaName])

	// fresh keys have batch subkeys only, no offsets yet
	a.Equal(len(part.Voting.Batches), countTableRows(a, partDB, "OtsBatches"))
	a.NotZero(countTableRows(a, partDB, "OtsBatches"))
	a.Zero(countTableRows(a, partDB, "OtsOffsets"))

	var scalars crypto.OneTimeSignatureSecrets
	a.NoError(protocol.Decode(readVotingColumn(a, partDB), &scalars))
	a.Empty(scalars.Batches)
	a.Empty(scalars.Offsets)
}

func TestComputeVotingDelta(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 8

	secrets := crypto.GenerateOneTimeSignatureSecrets(0, 10)
	secrets.DeleteBeforeFineGrained(crypto.OneTimeSignatureIdentifier{Batch: 2, Offset: 3}, dilution)
	current, _, _ := secrets.PersistentState()

	// noop: persisted state matches memory
	d := computeVotingDelta(&current, secrets)
	a.True(d.noop)

	// same-batch advance
	older := current
	older.FirstOffset = current.FirstOffset - 2
	d = computeVotingDelta(&older, secrets)
	a.False(d.noop)
	a.False(d.fullRewrite)
	a.False(d.replaceAllOffsets)
	a.Equal(current.FirstOffset, d.deleteOffsetsBelow)
	a.Zero(d.deleteBatchesBelow)
	a.Empty(d.insertBatches)
	a.Empty(d.insertOffsets)
	a.NotNil(d.newScalars)

	// batch rollover (single and multi-batch jump behave identically)
	prevBatch := current
	prevBatch.FirstBatch = current.FirstBatch - 2
	prevBatch.FirstOffset = 5
	d = computeVotingDelta(&prevBatch, secrets)
	a.False(d.noop)
	a.False(d.fullRewrite)
	a.True(d.replaceAllOffsets)
	a.Equal(current.FirstBatch, d.deleteBatchesBelow)
	a.Empty(d.insertBatches)
	a.Equal(len(secrets.Offsets), len(d.insertOffsets))
	a.NotNil(d.newScalars)

	// nil old: full rewrite
	d = computeVotingDelta(nil, secrets)
	a.True(d.fullRewrite)
	a.Equal(len(secrets.Batches), len(d.insertBatches))
	a.Equal(len(secrets.Offsets), len(d.insertOffsets))
	a.NotNil(d.newScalars)

	// legacy whole-blob persisted state: full rewrite
	legacy := secrets.Snapshot().OneTimeSignatureSecretsPersistent
	a.NotEmpty(legacy.Batches)
	d = computeVotingDelta(&legacy, secrets)
	a.True(d.fullRewrite)

	// persisted state ahead of memory: full rewrite
	ahead := current
	ahead.FirstBatch = current.FirstBatch + 1
	d = computeVotingDelta(&ahead, secrets)
	a.True(d.fullRewrite)

	// end-of-key-life: DeleteBeforeFineGrained clears the remaining subkeys
	// of a key on its last batch without advancing either scalar; the delta
	// must clear the rows rather than report a noop
	spent := crypto.GenerateOneTimeSignatureSecrets(0, 4)
	spent.DeleteBeforeFineGrained(crypto.OneTimeSignatureIdentifier{Batch: 3, Offset: 2}, dilution)
	a.NotEmpty(spent.Offsets) // final batch expanded
	a.Empty(spent.Batches)
	persisted, _, _ := spent.PersistentState()
	spent.DeleteBeforeFineGrained(crypto.OneTimeSignatureIdentifier{Batch: 4, Offset: 0}, dilution)
	a.Empty(spent.Offsets)
	afterState, _, _ := spent.PersistentState()
	a.Equal(persisted.FirstBatch, afterState.FirstBatch) // scalars did not move
	a.Equal(persisted.FirstOffset, afterState.FirstOffset)
	d = computeVotingDelta(&persisted, spent)
	a.False(d.noop)
	a.True(d.clearAllRows)
	a.Nil(d.newScalars)
}

func TestDeleteOldKeysIncremental(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]

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

func TestDeleteOldKeysBatchRollover(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]

	// advance into batch 3 in one jump
	const round = 3*dilution + 4
	a.NoError(<-part.DeleteOldKeys(round, proto))

	a.Equal(len(part.Voting.Batches), countTableRows(a, partDB, "OtsBatches"))
	a.Equal(len(part.Voting.Offsets), countTableRows(a, partDB, "OtsOffsets"))
	a.NotZero(countTableRows(a, partDB, "OtsOffsets"))

	restored, err := RestoreParticipationUnmigrated(partDB)
	a.NoError(err)
	a.Equal(encodedVotingSnapshot(part.Voting), encodedVotingSnapshot(restored.Voting))
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

	// expand the final batch, then move past the end of the key
	a.NoError(<-part.DeleteOldKeys(basics.Round(305), proto))
	a.NotZero(countTableRows(a, partDB, "OtsOffsets"))
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

// TestDeleteOldKeysLegacyBlobSelfHeals plants a legacy whole-secrets blob in
// the voting column of a v4 file and verifies the next DeleteOldKeys converts
// it to rows.
func TestDeleteOldKeysLegacyBlobSelfHeals(t *testing.T) {
	partitiontest.PartitionTest(t)

	a := require.New(t)
	const dilution = 10
	part, partDB := makeSmallTestKey(t, a, 0, 300, dilution)
	defer closeDBS(partDB)

	// simulate stale state: full legacy blob in the voting column, no rows
	legacyBlob := encodedVotingSnapshot(part.Voting)
	err := partDB.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM OtsBatches"); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM OtsOffsets"); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE ParticipationAccount SET voting=?", legacyBlob)
		return err
	})
	a.NoError(err)

	proto := config.Consensus[protocol.ConsensusCurrentVersion]
	a.NoError(<-part.DeleteOldKeys(basics.Round(25), proto))

	a.Equal(len(part.Voting.Batches), countTableRows(a, partDB, "OtsBatches"))
	a.Equal(len(part.Voting.Offsets), countTableRows(a, partDB, "OtsOffsets"))

	restored, err := RestoreParticipationUnmigrated(partDB)
	a.NoError(err)
	a.Equal(encodedVotingSnapshot(part.Voting), encodedVotingSnapshot(restored.Voting))
}
