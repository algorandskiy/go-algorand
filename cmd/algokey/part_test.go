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

package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/crypto/merklesignature"
	"github.com/algorand/go-algorand/data/account"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/test/partitiontest"
	"github.com/algorand/go-algorand/util/db"
)

// makeV3PartkeyFile creates a legacy (schema version 3) participation key
// file, as an old algokey would have written it, and returns the key.
func makeV3PartkeyFile(t *testing.T, keyfile string, mangleVoting bool) account.Participation {
	t.Helper()
	a := require.New(t)

	const first, last, dilution = 1, 200, 10
	stateProofSecrets, err := merklesignature.New(first, last, (last+1)/2)
	a.NoError(err)

	firstID := basics.OneTimeIDForRound(first, dilution)
	lastID := basics.OneTimeIDForRound(last, dilution)
	votingSecrets := crypto.GenerateOneTimeSignatureSecrets(firstID.Batch, lastID.Batch-firstID.Batch+1)
	// make the key mid-life so both batch and offset subkeys exist
	votingSecrets.DeleteBeforeFineGrained(basics.OneTimeIDForRound(42, dilution), dilution)

	part := account.Participation{
		FirstValid:        first,
		LastValid:         last,
		KeyDilution:       dilution,
		Voting:            votingSecrets,
		VRF:               crypto.GenerateVRFSecrets(),
		StateProofSecrets: stateProofSecrets,
	}
	crypto.RandBytes(part.Parent[:])

	voting := part.Voting.Snapshot()
	rawVoting := protocol.Encode(&voting)
	if mangleVoting {
		rawVoting = rawVoting[:len(rawVoting)/2]
	}

	partdb, err := db.MakeErasableAccessor(keyfile)
	a.NoError(err)
	defer partdb.Close()

	err = partdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE ParticipationAccount (
			parent BLOB, vrf BLOB, voting BLOB,
			firstValid INTEGER, lastValid INTEGER,
			keyDilution INTEGER NOT NULL DEFAULT 0, stateProof BLOB);`); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE schema (tablename TEXT PRIMARY KEY, version INTEGER);`); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO schema (tablename, version) VALUES (?, ?)", account.PartTableSchemaName, 3); err != nil {
			return err
		}
		_, err := tx.Exec("INSERT INTO ParticipationAccount (parent, vrf, voting, firstValid, lastValid, keyDilution, stateProof) VALUES (?, ?, ?, ?, ?, ?, ?)",
			part.Parent[:], protocol.Encode(part.VRF), rawVoting, part.FirstValid, part.LastValid, part.KeyDilution,
			protocol.Encode(&part.StateProofSecrets.SignerContext))
		return err
	})
	a.NoError(err)
	return part
}

func TestPartMigrate(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	original := makeV3PartkeyFile(t, keyfile, false)

	bytesBefore, err := os.ReadFile(keyfile)
	a.NoError(err)

	var out bytes.Buffer
	partkey, migrated, err := runPartMigrate(keyfile, false, &out)
	a.NoError(err)
	a.True(migrated)

	// original untouched
	bytesAfter, err := os.ReadFile(keyfile)
	a.NoError(err)
	a.Equal(bytesBefore, bytesAfter)

	// the .new copy is at the latest version
	newFile := keyfile + ".new"
	newdb, err := db.MakeErasableAccessor(newFile)
	a.NoError(err)
	version, err := account.PartkeySchemaVersion(newdb)
	newdb.Close()
	a.NoError(err)
	a.Equal(account.PartTableSchemaVersion, version)

	// migrated key matches the original
	a.NoError(comparePartkeys(original, partkey))

	a.Contains(out.String(), "Pure migration time")
	a.Contains(out.String(), "Validation PASSED")
}

func TestPartMigrateNoValidation(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	makeV3PartkeyFile(t, keyfile, false)

	var out bytes.Buffer
	_, migrated, err := runPartMigrate(keyfile, true, &out)
	a.NoError(err)
	a.True(migrated)
	a.NotContains(out.String(), "Validation PASSED")
	a.NotContains(out.String(), "Validation FAILED")
}

func TestPartMigrateNoopOnLatest(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	partdb, err := db.MakeErasableAccessor(keyfile)
	a.NoError(err)
	var addr basics.Address
	crypto.RandBytes(addr[:])
	_, err = account.FillDBWithParticipationKeys(partdb, addr, 1, 200, 10)
	a.NoError(err)
	partdb.Close()

	var out bytes.Buffer
	_, migrated, err := runPartMigrate(keyfile, false, &out)
	a.NoError(err)
	a.False(migrated)
	a.Contains(out.String(), "nothing to do")
	_, err = os.Stat(keyfile + ".new")
	a.True(os.IsNotExist(err))
}

func TestPartMigrateRefusesExistingNew(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	makeV3PartkeyFile(t, keyfile, false)
	a.NoError(os.WriteFile(keyfile+".new", []byte("occupied"), 0600))

	var out bytes.Buffer
	_, _, err := runPartMigrate(keyfile, false, &out)
	a.ErrorContains(err, "already exists")
}

func TestPartMigrateCorruptVotingBlob(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	makeV3PartkeyFile(t, keyfile, true)

	var out bytes.Buffer
	_, _, err := runPartMigrate(keyfile, false, &out)
	a.ErrorContains(err, "migration")

	// original still untouched at version 3
	origdb, err := db.MakeErasableAccessor(keyfile)
	a.NoError(err)
	version, err := account.PartkeySchemaVersion(origdb)
	origdb.Close()
	a.NoError(err)
	a.Equal(3, version)
}

func TestComparePartkeysMismatch(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	dir := t.TempDir()
	p1 := makeV3PartkeyFile(t, filepath.Join(dir, "a.partkey"), false)
	p2 := makeV3PartkeyFile(t, filepath.Join(dir, "b.partkey"), false)

	a.NoError(comparePartkeys(p1, p1))
	a.Error(comparePartkeys(p1, p2))

	tweaked := p1
	tweaked.KeyDilution++
	a.ErrorContains(comparePartkeys(p1, tweaked), "metadata")
}
