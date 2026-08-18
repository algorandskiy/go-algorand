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

	// newScalars is the encoded scalar state to store; nil only when noop.
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

	scalars := secrets.PersistentScalars()
	switch {
	case scalars.FirstBatch == old.FirstBatch && scalars.FirstOffset == old.FirstOffset:
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
		partScalars, _, offsets := secrets.PersistentParts()
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

// insertKeyedSubkeys bulk-inserts rows with a single prepared statement.
// insertSQL must take (prefixArgs..., index, data).
func insertKeyedSubkeys(tx *sql.Tx, insertSQL string, prefixArgs []interface{}, rows []crypto.KeyedSubkey) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return err
	}
	defer stmt.Close()
	args := make([]interface{}, len(prefixArgs)+2)
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

// applyVotingDeltaToPartkeyFile applies a delta to a .partkey database
// (tables OtsBatches/OtsOffsets, scalars in ParticipationAccount.voting).
func applyVotingDeltaToPartkeyFile(tx *sql.Tx, d votingDelta) error {
	if d.noop {
		return nil
	}

	if d.fullRewrite {
		if _, err := tx.Exec("DELETE FROM OtsBatches"); err != nil {
			return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to clear batches: %w", err)
		}
	} else if d.deleteBatchesBelow > 0 {
		if _, err := tx.Exec("DELETE FROM OtsBatches WHERE batch<?", d.deleteBatchesBelow); err != nil {
			return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to trim batches: %w", err)
		}
	}

	if d.fullRewrite || d.replaceAllOffsets {
		if _, err := tx.Exec("DELETE FROM OtsOffsets"); err != nil {
			return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to clear offsets: %w", err)
		}
	} else if d.deleteOffsetsBelow > 0 {
		if _, err := tx.Exec("DELETE FROM OtsOffsets WHERE off<?", d.deleteOffsetsBelow); err != nil {
			return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to trim offsets: %w", err)
		}
	}

	if err := insertKeyedSubkeys(tx, "INSERT INTO OtsBatches (batch, data) VALUES (?, ?)", nil, d.insertBatches); err != nil {
		return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to insert batches: %w", err)
	}
	if err := insertKeyedSubkeys(tx, "INSERT INTO OtsOffsets (off, data) VALUES (?, ?)", nil, d.insertOffsets); err != nil {
		return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to insert offsets: %w", err)
	}

	if _, err := tx.Exec("UPDATE ParticipationAccount SET voting=?", d.newScalars); err != nil {
		return fmt.Errorf("applyVotingDeltaToPartkeyFile: failed to update scalars: %w", err)
	}
	return nil
}

// applyVotingDeltaToRegistry applies a delta to the participation registry
// (tables VotingBatches/VotingOffsets keyed by pk, scalars in Rolling.voting).
func applyVotingDeltaToRegistry(tx *sql.Tx, pk int64, d votingDelta) error {
	if d.noop {
		return nil
	}

	if d.fullRewrite {
		if _, err := tx.Exec("DELETE FROM VotingBatches WHERE pk=?", pk); err != nil {
			return fmt.Errorf("applyVotingDeltaToRegistry: failed to clear batches: %w", err)
		}
	} else if d.deleteBatchesBelow > 0 {
		if _, err := tx.Exec("DELETE FROM VotingBatches WHERE pk=? AND batch<?", pk, d.deleteBatchesBelow); err != nil {
			return fmt.Errorf("applyVotingDeltaToRegistry: failed to trim batches: %w", err)
		}
	}

	if d.fullRewrite || d.replaceAllOffsets {
		if _, err := tx.Exec("DELETE FROM VotingOffsets WHERE pk=?", pk); err != nil {
			return fmt.Errorf("applyVotingDeltaToRegistry: failed to clear offsets: %w", err)
		}
	} else if d.deleteOffsetsBelow > 0 {
		if _, err := tx.Exec("DELETE FROM VotingOffsets WHERE pk=? AND off<?", pk, d.deleteOffsetsBelow); err != nil {
			return fmt.Errorf("applyVotingDeltaToRegistry: failed to trim offsets: %w", err)
		}
	}

	if err := insertKeyedSubkeys(tx, "INSERT INTO VotingBatches (pk, batch, data) VALUES (?, ?, ?)", []interface{}{pk}, d.insertBatches); err != nil {
		return fmt.Errorf("applyVotingDeltaToRegistry: failed to insert batches: %w", err)
	}
	if err := insertKeyedSubkeys(tx, "INSERT INTO VotingOffsets (pk, off, data) VALUES (?, ?, ?)", []interface{}{pk}, d.insertOffsets); err != nil {
		return fmt.Errorf("applyVotingDeltaToRegistry: failed to insert offsets: %w", err)
	}

	if _, err := tx.Exec("UPDATE Rolling SET voting=? WHERE pk=?", d.newScalars, pk); err != nil {
		return fmt.Errorf("applyVotingDeltaToRegistry: failed to update scalars: %w", err)
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

func readKeyedSubkeys(tx *sql.Tx, query string, args ...interface{}) ([]crypto.KeyedSubkey, error) {
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
