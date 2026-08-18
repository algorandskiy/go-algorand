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
	"fmt"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/util/db"
)

// PartTableSchemaName is the name of the table in the Schema Versions table storing the table + version details
const PartTableSchemaName = "parttable"

// PartTableSchemaVersion is the latest version of the PartTable schema
const PartTableSchemaVersion = 4

// ErrUnsupportedSchema is the error returned when the PartTable schema version is wrong.
var ErrUnsupportedSchema = fmt.Errorf("unsupported participation file schema version (expected %d)", PartTableSchemaVersion)

func partInstallDatabase(tx *sql.Tx) error {
	var err error

	_, err = tx.Exec(`CREATE TABLE ParticipationAccount (
		parent BLOB,

		--* participation keys
		vrf BLOB,         --*  msgpack encoding of ParticipationAccount.vrf
		voting BLOB,      --*  msgpack encoding of the voting key scalars (whole secrets before schema v4)

		firstValid INTEGER,
		lastValid INTEGER,

		keyDilution INTEGER NOT NULL DEFAULT 0,
		stateProof BLOB  --*  msgpack encoding of ParticipationAccount.StateProof
	);`)
	if err != nil {
		return err
	}

	err = createVotingSubkeyTables(tx)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`CREATE TABLE schema (
		tablename TEXT PRIMARY KEY,
		version INTEGER
	);`)
	if err != nil {
		return err
	}

	_, err = tx.Exec("INSERT INTO schema (tablename, version) VALUES (?, ?)",
		PartTableSchemaName, PartTableSchemaVersion)
	if err != nil {
		return err
	}

	return nil
}

func partMigrate(tx *sql.Tx) (err error) {
	rows, err := tx.Query("SELECT tablename, version FROM schema")
	if err != nil {
		return ErrUnsupportedSchema
	}
	defer rows.Close()

	versions := make(map[string]int)
	for rows.Next() {
		var tableName string
		var version int
		err = rows.Scan(&tableName, &version)
		if err != nil {
			return err
		}
		versions[tableName] = version
	}

	err = rows.Err()
	if err != nil {
		return err
	}

	partVersion, has := versions[PartTableSchemaName]
	if !has {
		return ErrUnsupportedSchema
	}

	partVersion, err = updateDB(tx, partVersion)
	if err != nil {
		return err
	}

	if partVersion != PartTableSchemaVersion {
		return ErrUnsupportedSchema
	}

	return nil
}

func updateDB(tx *sql.Tx, partVersion int) (int, error) {
	if partVersion == 1 {
		_, err := tx.Exec("ALTER TABLE ParticipationAccount ADD keyDilution INTEGER NOT NULL DEFAULT 0")
		if err != nil {
			return 0, err
		}

		partVersion = 2
		_, err = tx.Exec("UPDATE schema SET version=? WHERE tablename=?", partVersion, PartTableSchemaName)
		if err != nil {
			return 0, err
		}
	}

	if partVersion == 2 {
		_, err := tx.Exec("ALTER TABLE ParticipationAccount ADD stateProof BLOB")
		if err != nil {
			return 0, err
		}

		partVersion = 3
		_, err = tx.Exec("UPDATE schema SET version=? WHERE tablename=?", partVersion, PartTableSchemaName)
		if err != nil {
			return 0, err
		}
	}

	if partVersion == 3 {
		err := migrateVotingBlobToRows(tx)
		if err != nil {
			return 0, err
		}

		partVersion = 4
		_, err = tx.Exec("UPDATE schema SET version=? WHERE tablename=?", partVersion, PartTableSchemaName)
		if err != nil {
			return 0, err
		}
	}
	return partVersion, nil
}

func createVotingSubkeyTables(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE OtsBatches (
		batch INTEGER PRIMARY KEY, --* absolute batch number
		data BLOB NOT NULL         --* msgpack encoding of the batch subkey
	);`)
	if err != nil {
		return err
	}

	_, err = tx.Exec(`CREATE TABLE OtsOffsets (
		off INTEGER PRIMARY KEY, --* absolute offset within batch FirstBatch-1
		data BLOB NOT NULL       --* msgpack encoding of the offset subkey
	);`)
	return err
}

// migrateVotingBlobToRows converts the whole-secrets voting blob into
// per-subkey rows, leaving only the scalar fields in the voting column.
func migrateVotingBlobToRows(tx *sql.Tx) error {
	err := createVotingSubkeyTables(tx)
	if err != nil {
		return err
	}

	var rawVoting []byte
	err = tx.QueryRow("SELECT voting FROM ParticipationAccount").Scan(&rawVoting)
	if err == sql.ErrNoRows {
		// no account row (partially initialized file); nothing to convert
		return nil
	}
	if err != nil {
		return err
	}
	if len(rawVoting) == 0 {
		return nil
	}

	voting := &crypto.OneTimeSignatureSecrets{}
	err = protocol.Decode(rawVoting, voting)
	if err != nil {
		return fmt.Errorf("migrateVotingBlobToRows: failed to decode voting blob: %w", err)
	}

	return applyVotingDeltaToPartkeyFile(tx, computeVotingDelta(nil, voting))
}

// PartkeySchemaVersion reads the participation file's schema version without
// migrating it.  Returns ErrUnsupportedSchema if no version is recorded.
func PartkeySchemaVersion(store db.Accessor) (version int, err error) {
	err = store.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		serr := tx.QueryRow("SELECT version FROM schema WHERE tablename=?", PartTableSchemaName).Scan(&version)
		if serr == sql.ErrNoRows {
			return ErrUnsupportedSchema
		}
		return serr
	})
	return version, err
}
