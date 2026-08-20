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
	"database/sql"
	"fmt"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/protocol"
)

// votingDelta describes the row operations needed to bring row-oriented
// persisted voting secrets (whose last-persisted scalars are known) up to the
// current in-memory state.  Voting subkeys advance monotonically: batch rows
// are written once when a key is stored and only ever deleted afterwards;
// offset rows are replaced wholesale on a batch rollover and deleted from the
// front as rounds pass.
type votingDelta struct {
	// noop means the persisted state already matches; skip all writes.
	noop bool

	// fullRewrite means the persisted state is unusable (ahead of memory,
	// undecodable, or a legacy whole-blob): delete every row and re-insert
	// insertBatches+insertOffsets.
	fullRewrite bool

	// clearAllRows means the in-memory secrets carry no subkeys at all while
	// the scalars are unchanged (the end-of-key-life transition): delete
	// every row, keep the stored scalars.  Idempotent, and once the tables
	// are empty it writes nothing.
	clearAllRows bool

	// deleteBatchesBelow deletes batch rows with index < this value
	// (rollover path; 0 means no batch deletion).
	deleteBatchesBelow uint64

	// deleteOffsetsBelow deletes offset rows with index < this value
	// (same-batch path; 0 means no offset range-deletion).
	deleteOffsetsBelow uint64

	// replaceAllOffsets clears the offsets table before inserting
	// insertOffsets.  Offsets restart at 0 in every batch, so a rollover must
	// clear rather than range-delete.
	replaceAllOffsets bool

	insertBatches []crypto.KeyedSubkey
	insertOffsets []crypto.KeyedSubkey

	// newScalars is the encoded scalar state to store; nil when the stored
	// scalars need no update (noop and clearAllRows).
	newScalars []byte
}

// computeVotingDelta compares the last-persisted scalars against the current
// in-memory secrets and returns the row operations to persist the difference.
// old == nil requests an unconditional full rewrite (undecodable or missing
// persisted state).
func computeVotingDelta(old *crypto.OneTimeSignatureSecretsPersistent, secrets *crypto.OneTimeSignatureSecrets) votingDelta {
	fullRewrite := func() votingDelta {
		scalars, batches, offsets := secrets.PersistentParts()
		return votingDelta{
			fullRewrite:   true,
			insertBatches: batches,
			insertOffsets: offsets,
			newScalars:    protocol.Encode(&scalars),
		}
	}

	// Legacy whole-blob or unusable persisted state: convert in place.
	if old == nil || len(old.Batches) != 0 || len(old.Offsets) != 0 {
		return fullRewrite()
	}

	scalars, numBatches, numOffsets := secrets.PersistentState()
	switch {
	case scalars.FirstBatch == old.FirstBatch && scalars.FirstOffset == old.FirstOffset:
		if numBatches == 0 && numOffsets == 0 {
			// End-of-key-life: when a key on its last batch moves past its
			// end, DeleteBeforeFineGrained clears the remaining offset
			// subkeys without advancing either scalar, so scalar equality
			// does not imply the stored rows are current.  A key with no
			// subkeys in memory must have no rows on disk (forward
			// security).
			return votingDelta{clearAllRows: true}
		}
		return votingDelta{noop: true}

	case scalars.FirstBatch == old.FirstBatch && scalars.FirstOffset > old.FirstOffset:
		// Common per-round path: offsets consumed from the front.
		return votingDelta{
			deleteOffsetsBelow: scalars.FirstOffset,
			newScalars:         protocol.Encode(&scalars),
		}

	case scalars.FirstBatch > old.FirstBatch:
		// Batch rollover: batch rows consumed, offset rows regenerated.
		// Re-capture scalars and offset rows in one consistent snapshot;
		// batch rows are already present in storage and never re-inserted.
		partScalars, offsets := secrets.PersistentScalarsAndOffsets()
		return votingDelta{
			deleteBatchesBelow: partScalars.FirstBatch,
			replaceAllOffsets:  true,
			insertOffsets:      offsets,
			newScalars:         protocol.Encode(&partScalars),
		}

	default:
		// Persisted state is ahead of memory; self-heal.
		return fullRewrite()
	}
}

// votingDeltaTarget names the SQL statements for one of the two row-oriented
// voting key stores: the .partkey file tables, or the registry tables scoped
// by pk.  Statements taking a threshold or row values receive prefixArgs
// first.
type votingDeltaTarget struct {
	deleteAllBatches   string
	deleteBatchesBelow string // args: (prefixArgs..., threshold)
	deleteAllOffsets   string
	deleteOffsetsBelow string // args: (prefixArgs..., threshold)
	insertBatch        string // args: (prefixArgs..., index, data)
	insertOffset       string // args: (prefixArgs..., index, data)
	updateScalars      string // args: (scalars, prefixArgs...)
	prefixArgs         []any
}

var partkeyFileVotingTarget = votingDeltaTarget{
	deleteAllBatches:   "DELETE FROM OtsBatches",
	deleteBatchesBelow: "DELETE FROM OtsBatches WHERE batch<?",
	deleteAllOffsets:   "DELETE FROM OtsOffsets",
	deleteOffsetsBelow: "DELETE FROM OtsOffsets WHERE off<?",
	insertBatch:        "INSERT INTO OtsBatches (batch, data) VALUES (?, ?)",
	insertOffset:       "INSERT INTO OtsOffsets (off, data) VALUES (?, ?)",
	updateScalars:      "UPDATE ParticipationAccount SET voting=?",
}

func registryVotingTarget(pk int64) votingDeltaTarget {
	return votingDeltaTarget{
		deleteAllBatches:   deleteVotingBatchesPK,
		deleteBatchesBelow: "DELETE FROM VotingBatches WHERE pk=? AND batch<?",
		deleteAllOffsets:   deleteVotingOffsetsPK,
		deleteOffsetsBelow: "DELETE FROM VotingOffsets WHERE pk=? AND off<?",
		insertBatch:        "INSERT INTO VotingBatches (pk, batch, data) VALUES (?, ?, ?)",
		insertOffset:       "INSERT INTO VotingOffsets (pk, off, data) VALUES (?, ?, ?)",
		updateScalars:      "UPDATE Rolling SET voting=? WHERE pk=?",
		prefixArgs:         []any{pk},
	}
}

// applyVotingDeltaToPartkeyFile applies a delta to a .partkey database.
func applyVotingDeltaToPartkeyFile(tx *sql.Tx, d votingDelta) error {
	return applyVotingDelta(tx, partkeyFileVotingTarget, d)
}

// applyVotingDeltaToRegistry applies a delta to the participation registry
// tables for one key.
func applyVotingDeltaToRegistry(tx *sql.Tx, pk int64, d votingDelta) error {
	return applyVotingDelta(tx, registryVotingTarget(pk), d)
}

func applyVotingDelta(tx *sql.Tx, target votingDeltaTarget, d votingDelta) error {
	if d.noop {
		return nil
	}

	if d.fullRewrite || d.clearAllRows {
		if _, err := tx.Exec(target.deleteAllBatches, target.prefixArgs...); err != nil {
			return fmt.Errorf("applyVotingDelta: failed to clear batches: %w", err)
		}
	} else if d.deleteBatchesBelow > 0 {
		if _, err := tx.Exec(target.deleteBatchesBelow, append(append([]any{}, target.prefixArgs...), d.deleteBatchesBelow)...); err != nil {
			return fmt.Errorf("applyVotingDelta: failed to trim batches: %w", err)
		}
	}

	if d.fullRewrite || d.clearAllRows || d.replaceAllOffsets {
		if _, err := tx.Exec(target.deleteAllOffsets, target.prefixArgs...); err != nil {
			return fmt.Errorf("applyVotingDelta: failed to clear offsets: %w", err)
		}
	} else if d.deleteOffsetsBelow > 0 {
		if _, err := tx.Exec(target.deleteOffsetsBelow, append(append([]any{}, target.prefixArgs...), d.deleteOffsetsBelow)...); err != nil {
			return fmt.Errorf("applyVotingDelta: failed to trim offsets: %w", err)
		}
	}

	if err := insertKeyedSubkeys(tx, target.insertBatch, target.prefixArgs, d.insertBatches); err != nil {
		return fmt.Errorf("applyVotingDelta: failed to insert batches: %w", err)
	}
	if err := insertKeyedSubkeys(tx, target.insertOffset, target.prefixArgs, d.insertOffsets); err != nil {
		return fmt.Errorf("applyVotingDelta: failed to insert offsets: %w", err)
	}

	if d.newScalars != nil {
		if _, err := tx.Exec(target.updateScalars, append([]any{d.newScalars}, target.prefixArgs...)...); err != nil {
			return fmt.Errorf("applyVotingDelta: failed to update scalars: %w", err)
		}
	}
	return nil
}

// insertKeyedSubkeys bulk-inserts rows with a single prepared statement.
// insertSQL must take (prefixArgs..., index, data).
func insertKeyedSubkeys(tx *sql.Tx, insertSQL string, prefixArgs []any, rows []crypto.KeyedSubkey) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	args := make([]any, len(prefixArgs)+2)
	copy(args, prefixArgs)
	for _, row := range rows {
		args[len(prefixArgs)] = row.Index
		args[len(prefixArgs)+1] = row.Key
		if _, err := stmt.Exec(args...); err != nil {
			return err
		}
	}
	return nil
}

// readVotingRowsFromPartkeyFile loads the subkey rows from a v4 .partkey
// database, ordered by index.
func readVotingRowsFromPartkeyFile(tx *sql.Tx) (batches, offsets []crypto.KeyedSubkey, err error) {
	batches, err = readKeyedSubkeys(tx, "SELECT batch, data FROM OtsBatches ORDER BY batch")
	if err != nil {
		return nil, nil, err
	}
	offsets, err = readKeyedSubkeys(tx, "SELECT off, data FROM OtsOffsets ORDER BY off")
	if err != nil {
		return nil, nil, err
	}
	return batches, offsets, nil
}

func readKeyedSubkeys(tx *sql.Tx, query string, args ...any) ([]crypto.KeyedSubkey, error) {
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []crypto.KeyedSubkey
	for rows.Next() {
		var row crypto.KeyedSubkey
		if err := rows.Scan(&row.Index, &row.Key); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
