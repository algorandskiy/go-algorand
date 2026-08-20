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

type legacyPartkeyOptions struct {
	version        int  // schema version to record; the table shape is always v3
	nullStateProof bool // v3 file with a NULL stateProof column (upgraded from v1/v2 by old code)
}

// makeLegacyPartkeyFile creates an old-schema participation key file, as an
// old algokey would have written it, and returns the key.
func makeLegacyPartkeyFile(t *testing.T, keyfile string, opts legacyPartkeyOptions) account.Participation {
	t.Helper()
	a := require.New(t)

	const first, last, dilution = 1, 200, 10
	firstID := basics.OneTimeIDForRound(first, dilution)
	lastID := basics.OneTimeIDForRound(last, dilution)
	votingSecrets := crypto.GenerateOneTimeSignatureSecrets(firstID.Batch, lastID.Batch-firstID.Batch+1)
	// make the key mid-life so both batch and offset subkeys exist
	votingSecrets.DeleteBeforeFineGrained(basics.OneTimeIDForRound(42, dilution), dilution)

	part := account.Participation{
		FirstValid:  first,
		LastValid:   last,
		KeyDilution: dilution,
		Voting:      votingSecrets,
		VRF:         crypto.GenerateVRFSecrets(),
	}
	crypto.RandBytes(part.Parent[:])
	if !opts.nullStateProof {
		stateProofSecrets, err := merklesignature.New(first, last, (last+1)/2)
		a.NoError(err)
		part.StateProofSecrets = stateProofSecrets
	}

	voting := part.Voting.Snapshot()
	rawVoting := protocol.Encode(&voting)

	partdb, err := db.MakeErasableAccessor(keyfile)
	a.NoError(err)
	defer partdb.Close()

	err = partdb.Atomic(func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE schema (tablename TEXT PRIMARY KEY, version INTEGER);`); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO schema (tablename, version) VALUES (?, ?)", account.PartTableSchemaName, opts.version); err != nil {
			return err
		}
		if _, err := tx.Exec(`CREATE TABLE ParticipationAccount (
			parent BLOB, vrf BLOB, voting BLOB,
			firstValid INTEGER, lastValid INTEGER,
			keyDilution INTEGER NOT NULL DEFAULT 0, stateProof BLOB);`); err != nil {
			return err
		}
		var rawStateProof []byte
		if part.StateProofSecrets != nil {
			rawStateProof = protocol.Encode(&part.StateProofSecrets.SignerContext)
		}
		_, err := tx.Exec("INSERT INTO ParticipationAccount (parent, vrf, voting, firstValid, lastValid, keyDilution, stateProof) VALUES (?, ?, ?, ?, ?, ?, ?)",
			part.Parent[:], protocol.Encode(part.VRF), rawVoting, part.FirstValid, part.LastValid, part.KeyDilution,
			rawStateProof)
		return err
	})
	a.NoError(err)

	// real v3 files carry the state proof secret keys in their own table
	if part.StateProofSecrets != nil {
		a.NoError(part.StateProofSecrets.Persist(partdb))
	}
	return part
}

func makeV3PartkeyFile(t *testing.T, keyfile string) account.Participation {
	t.Helper()
	return makeLegacyPartkeyFile(t, keyfile, legacyPartkeyOptions{version: 3})
}

func TestPartMigrate(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	original := makeV3PartkeyFile(t, keyfile)

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

// TestPartMigrateNilStateProof covers v3 files whose stateProof column is
// NULL (upgraded from v1/v2 by old releases). Validation must not crash on
// them.
func TestPartMigrateNilStateProof(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "v3null.partkey")
	makeLegacyPartkeyFile(t, keyfile, legacyPartkeyOptions{version: 3, nullStateProof: true})

	var out bytes.Buffer
	partkey, migrated, err := runPartMigrate(keyfile, false, &out)
	a.NoError(err)
	a.True(migrated)
	a.Nil(partkey.StateProofSecrets)
	a.Contains(out.String(), "Validation PASSED")

	// validation reads the original without migrating it
	origdb, err := db.MakeErasableAccessor(keyfile)
	a.NoError(err)
	version, err := account.PartkeySchemaVersion(origdb)
	origdb.Close()
	a.NoError(err)
	a.Equal(3, version)
}

// TestPartMigrateRejectsPreStateProofVersions verifies schema versions 1 and
// 2 (whose keys expired years ago) are refused rather than migrated.
func TestPartMigrateRejectsPreStateProofVersions(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	for _, version := range []int{1, 2} {
		keyfile := filepath.Join(t.TempDir(), "old.partkey")
		makeLegacyPartkeyFile(t, keyfile, legacyPartkeyOptions{version: version})

		var out bytes.Buffer
		_, _, err := runPartMigrate(keyfile, false, &out)
		a.ErrorContains(err, "unsupported schema version", "version %d", version)
		_, statErr := os.Stat(keyfile + ".new")
		a.True(os.IsNotExist(statErr), "version %d", version)
	}
}

// TestPartMigratePreservesPermissions verifies the snapshot is not more
// permissive than the original key file.
func TestPartMigratePreservesPermissions(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	keyfile := filepath.Join(t.TempDir(), "test.partkey")
	makeV3PartkeyFile(t, keyfile)
	a.NoError(os.Chmod(keyfile, 0600))

	var out bytes.Buffer
	_, migrated, err := runPartMigrate(keyfile, false, &out)
	a.NoError(err)
	a.True(migrated)

	info, err := os.Stat(keyfile + ".new")
	a.NoError(err)
	a.Equal(os.FileMode(0600), info.Mode().Perm())
}

// TestComparePartkeys covers the validation comparator: whole-key and
// metadata mismatches, and a copy whose state proof secret keys are missing
// (the Participation encoding itself covers only the SignerContext).
func TestComparePartkeys(t *testing.T) {
	partitiontest.PartitionTest(t)
	t.Parallel()
	a := require.New(t)

	dir := t.TempDir()
	p1 := makeV3PartkeyFile(t, filepath.Join(dir, "a.partkey"))
	p2 := makeV3PartkeyFile(t, filepath.Join(dir, "b.partkey"))

	a.NoError(comparePartkeys(p1, p1))
	a.Error(comparePartkeys(p1, p2))

	tweaked := p1
	tweaked.KeyDilution++
	a.ErrorContains(comparePartkeys(p1, tweaked), "metadata")

	// state proof secret keys live in their own table and are compared only
	// when loaded; a copy missing them must be detected
	partdb, err := db.MakeErasableAccessor(filepath.Join(dir, "a.partkey"))
	a.NoError(err)
	defer partdb.Close()

	withKeys, err := account.RestoreParticipationUnmigrated(partdb)
	a.NoError(err)
	a.NoError(withKeys.StateProofSecrets.RestoreAllSecrets(partdb))
	a.NotEmpty(withKeys.StateProofSecrets.GetAllKeys())

	withoutKeys, err := account.RestoreParticipationUnmigrated(partdb)
	a.NoError(err)
	a.Empty(withoutKeys.StateProofSecrets.GetAllKeys())

	a.ErrorContains(comparePartkeys(withKeys.Participation, withoutKeys.Participation), "state proof key count mismatch")
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
	makeV3PartkeyFile(t, keyfile)
	a.NoError(os.WriteFile(keyfile+".new", []byte("occupied"), 0600))

	var out bytes.Buffer
	_, _, err := runPartMigrate(keyfile, false, &out)
	a.ErrorContains(err, "already exists")
}
