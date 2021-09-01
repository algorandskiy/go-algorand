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

package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto/merkletrie"
	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/ledger/ledgercore"
	"github.com/algorand/go-algorand/logging"
	"github.com/algorand/go-algorand/protocol"
	"github.com/algorand/go-algorand/util/db"
	"github.com/mattn/go-sqlite3"
)

var accountsSchema = []string{
	`CREATE TABLE IF NOT EXISTS acctrounds (
		id string primary key,
		rnd integer)`,
	`CREATE TABLE IF NOT EXISTS accounttotals (
		id string primary key,
		online integer,
		onlinerewardunits integer,
		offline integer,
		offlinerewardunits integer,
		notparticipating integer,
		notparticipatingrewardunits integer,
		rewardslevel integer)`,
	`CREATE TABLE IF NOT EXISTS accountbase (
		address blob primary key,
		data blob)`,
	`CREATE TABLE IF NOT EXISTS assetcreators (
		asset integer primary key,
		creator blob)`,
	`CREATE TABLE IF NOT EXISTS storedcatchpoints (
		round integer primary key,
		filename text NOT NULL,
		catchpoint text NOT NULL,
		filesize size NOT NULL,
		pinned integer NOT NULL)`,
	`CREATE TABLE IF NOT EXISTS accounthashes (
		id integer primary key,
		data blob)`,
	`CREATE TABLE IF NOT EXISTS catchpointstate (
		id string primary key,
		intval integer,
		strval text)`,
}

// TODO: Post applications, rename assetcreators -> creatables and rename
// 'asset' column -> 'creatable'
var creatablesMigration = []string{
	`ALTER TABLE assetcreators ADD COLUMN ctype INTEGER DEFAULT 0`,
}

// createNormalizedOnlineBalanceIndex handles accountbase/catchpointbalances tables
func createNormalizedOnlineBalanceIndex(idxname string, tablename string) string {
	return fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s
		ON %s ( normalizedonlinebalance, address, data )
		WHERE normalizedonlinebalance>0`, idxname, tablename)
}

var createOnlineAccountIndex = []string{
	`ALTER TABLE accountbase
		ADD COLUMN normalizedonlinebalance INTEGER`,
	createNormalizedOnlineBalanceIndex("onlineaccountbals", "accountbase"),
}

var accountsResetExprs = []string{
	`DROP TABLE IF EXISTS acctrounds`,
	`DROP TABLE IF EXISTS accounttotals`,
	`DROP TABLE IF EXISTS accountbase`,
	`DROP TABLE IF EXISTS assetcreators`,
	`DROP TABLE IF EXISTS storedcatchpoints`,
	`DROP TABLE IF EXISTS catchpointstate`,
	`DROP TABLE IF EXISTS accounthashes`,
}

// accountDBVersion is the database version that this binary would know how to support and how to upgrade to.
// details about the content of each of the versions can be found in the upgrade functions upgradeDatabaseSchemaXXXX
// and their descriptions.
var accountDBVersion = int32(5)

// createCatchpointStagingHashesIndex creates an index on catchpointpendinghashes to allow faster scanning according to the hash order
func createCatchpointStagingHashesIndex(ctx context.Context, tx *sql.Tx) (err error) {
	_, err = tx.ExecContext(ctx, "CREATE INDEX IF NOT EXISTS catchpointpendinghashesidx ON catchpointpendinghashes(data)")
	if err != nil {
		return
	}
	return
}

func resetCatchpointStagingBalances(ctx context.Context, tx *sql.Tx, newCatchup bool) (err error) {
	s := []string{
		"DROP TABLE IF EXISTS catchpointbalances",
		"DROP TABLE IF EXISTS catchpointassetcreators",
		"DROP TABLE IF EXISTS catchpointaccounthashes",
		"DROP TABLE IF EXISTS catchpointpendinghashes",
		"DELETE FROM accounttotals where id='catchpointStaging'",
	}

	if newCatchup {
		// SQLite has no way to rename an existing index.  So, we need
		// to cook up a fresh name for the index, which will be kept
		// around after we rename the table from "catchpointbalances"
		// to "accountbase".  To construct a unique index name, we
		// use the current time.
		// Apply the same logic to
		idxnameBalances := fmt.Sprintf("onlineaccountbals_idx_%d", time.Now().UnixNano())

		s = append(s,
			"CREATE TABLE IF NOT EXISTS catchpointassetcreators (asset integer primary key, creator blob, ctype integer)",
			"CREATE TABLE IF NOT EXISTS catchpointbalances (address blob primary key, data blob, normalizedonlinebalance integer)",
			"CREATE TABLE IF NOT EXISTS catchpointpendinghashes (data blob)",
			"CREATE TABLE IF NOT EXISTS catchpointaccounthashes (id integer primary key, data blob)",
			createNormalizedOnlineBalanceIndex(idxnameBalances, "catchpointbalances"),
		)
	}

	for _, stmt := range s {
		_, err = tx.Exec(stmt)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyCatchpointStagingBalances switches the staged catchpoint catchup tables onto the actual
// tables and update the correct balance round. This is the final step in switching onto the new catchpoint round.
func applyCatchpointStagingBalances(ctx context.Context, tx *sql.Tx, balancesRound basics.Round) (err error) {
	stmts := []string{
		"ALTER TABLE accountbase RENAME TO accountbase_old",
		"ALTER TABLE assetcreators RENAME TO assetcreators_old",
		"ALTER TABLE accounthashes RENAME TO accounthashes_old",

		"ALTER TABLE catchpointbalances RENAME TO accountbase",
		"ALTER TABLE catchpointassetcreators RENAME TO assetcreators",
		"ALTER TABLE catchpointaccounthashes RENAME TO accounthashes",

		"DROP TABLE IF EXISTS accountbase_old",
		"DROP TABLE IF EXISTS assetcreators_old",
		"DROP TABLE IF EXISTS accounthashes_old",
	}

	for _, stmt := range stmts {
		_, err = tx.Exec(stmt)
		if err != nil {
			return err
		}
	}

	_, err = tx.Exec("INSERT OR REPLACE INTO acctrounds(id, rnd) VALUES('acctbase', ?)", balancesRound)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT OR REPLACE INTO acctrounds(id, rnd) VALUES('hashbase', ?)", balancesRound)
	if err != nil {
		return err
	}
	return
}

// accountsAddNormalizedBalance adds the normalizedonlinebalance column
// to the accountbase table.
func accountsAddNormalizedBalance(tx *sql.Tx, proto config.ConsensusParams) error {
	var exists bool
	err := tx.QueryRow("SELECT 1 FROM pragma_table_info('accountbase') WHERE name='normalizedonlinebalance'").Scan(&exists)
	if err == nil {
		// Already exists.
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}

	for _, stmt := range createOnlineAccountIndex {
		_, err := tx.Exec(stmt)
		if err != nil {
			return err
		}
	}

	rows, err := tx.Query("SELECT address, data FROM accountbase")
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var addrbuf []byte
		var buf []byte
		err = rows.Scan(&addrbuf, &buf)
		if err != nil {
			return err
		}

		var data basics.AccountData
		err = protocol.Decode(buf, &data)
		if err != nil {
			return err
		}

		normBalance := data.NormalizedOnlineBalance(proto)
		if normBalance > 0 {
			_, err = tx.Exec("UPDATE accountbase SET normalizedonlinebalance=? WHERE address=?", normBalance, addrbuf)
			if err != nil {
				return err
			}
		}
	}

	return rows.Err()
}

// removeEmptyAccountData removes empty AccountData msgp-encoded entries from accountbase table
// and optionally returns list of addresses that were eliminated
func removeEmptyAccountData(tx *sql.Tx, returnAddresses bool) (num int64, addresses []basics.Address, err error) {
	if returnAddresses {
		rows, err := tx.Query("SELECT address FROM accountbase where length(data) = 1 and data = x'80'") // empty AccountData is 0x80
		if err != nil {
			return 0, nil, err
		}
		defer rows.Close()

		for rows.Next() {
			var addrbuf []byte
			err = rows.Scan(&addrbuf)
			if err != nil {
				return 0, nil, err
			}
			var addr basics.Address
			if len(addrbuf) != len(addr) {
				err = fmt.Errorf("Account DB address length mismatch: %d != %d", len(addrbuf), len(addr))
				return 0, nil, err
			}
			copy(addr[:], addrbuf)
			addresses = append(addresses, addr)
		}

		// if the above loop was abrupted by an error, test it now.
		if err = rows.Err(); err != nil {
			return 0, nil, err
		}
	}

	result, err := tx.Exec("DELETE from accountbase where length(data) = 1 and data = x'80'")
	if err != nil {
		return 0, nil, err
	}
	num, err = result.RowsAffected()
	if err != nil {
		// something wrong on getting rows count but data deleted, ignore the error
		num = int64(len(addresses))
		err = nil
	}
	return num, addresses, err
}

func accountsReset(tx *sql.Tx) error {
	for _, stmt := range accountsResetExprs {
		_, err := tx.Exec(stmt)
		if err != nil {
			return err
		}
	}
	_, err := db.SetUserVersion(context.Background(), tx, 0)
	return err
}

// reencodeAccounts reads all the accounts in the accountbase table, decode and reencode the account data.
// if the account data is found to have a different encoding, it would update the encoded account on disk.
// on return, it returns the number of modified accounts as well as an error ( if we had any )
func reencodeAccounts(ctx context.Context, tx *sql.Tx) (modifiedAccounts uint, err error) {
	modifiedAccounts = 0
	scannedAccounts := 0

	updateStmt, err := tx.PrepareContext(ctx, "UPDATE accountbase SET data = ? WHERE address = ?")
	if err != nil {
		return 0, err
	}

	rows, err := tx.QueryContext(ctx, "SELECT address, data FROM accountbase")
	if err != nil {
		return
	}
	defer rows.Close()

	var addr basics.Address
	for rows.Next() {
		// once every 1000 accounts we scan through, update the warning deadline.
		// as long as the last "chunk" takes less than one second, we should be good to go.
		// note that we should be quite liberal on timing here, since it might perform much slower
		// on low-power devices.
		if scannedAccounts%1000 == 0 {
			// The return value from ResetTransactionWarnDeadline can be safely ignored here since it would only default to writing the warning
			// message, which would let us know that it failed anyway.
			db.ResetTransactionWarnDeadline(ctx, tx, time.Now().Add(time.Second))
		}

		var addrbuf []byte
		var preencodedAccountData []byte
		err = rows.Scan(&addrbuf, &preencodedAccountData)
		if err != nil {
			return
		}

		if len(addrbuf) != len(addr) {
			err = fmt.Errorf("Account DB address length mismatch: %d != %d", len(addrbuf), len(addr))
			return
		}
		copy(addr[:], addrbuf[:])
		scannedAccounts++

		// decode and re-encode:
		var decodedAccountData basics.AccountData
		err = protocol.Decode(preencodedAccountData, &decodedAccountData)
		if err != nil {
			return
		}
		reencodedAccountData := protocol.Encode(&decodedAccountData)
		if bytes.Compare(preencodedAccountData, reencodedAccountData) == 0 {
			// these are identical, no need to store re-encoded account data
			continue
		}

		// we need to update the encoded data.
		result, err := updateStmt.ExecContext(ctx, reencodedAccountData, addrbuf)
		if err != nil {
			return 0, err
		}
		rowsUpdated, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if rowsUpdated != 1 {
			return 0, fmt.Errorf("failed to update account %v, number of rows updated was %d instead of 1", addr, rowsUpdated)
		}
		modifiedAccounts++
	}

	err = rows.Err()
	updateStmt.Close()
	return
}

type trackerParams interface {
	GetInitAccounts() map[basics.Address]basics.AccountData
	GetInitProto() config.ConsensusParams
	Logger() logging.Logger
	CatchpointConfig() (bool, merkletrie.MemoryConfig)
	Dir() string
}

type schemaUpdater struct {
	schemaVersion   int32
	newDatabase     bool
	vacuumOnStartup bool // TODO: report back to au to perform "maintenance"
	params          trackerParams
}

func (su *schemaUpdater) version() int32 {
	return su.schemaVersion
}

func (su *schemaUpdater) setVersion(ctx context.Context, tx *sql.Tx, version int32) (err error) {
	oldVersion := su.schemaVersion
	su.schemaVersion = version
	_, err = db.SetUserVersion(ctx, tx, su.schemaVersion)
	if err != nil {
		return fmt.Errorf("accountsInitialize unable to update database schema version from %d to %d: %v", oldVersion, version, err)
	}
	return nil
}

func (su *schemaUpdater) NeedMaintenance() bool {
	return su.vacuumOnStartup
}

func makeSchemaUpdater(ctx context.Context, tx *sql.Tx, params trackerParams) (schemaUpdater, error) {
	dbVersion, err := db.GetUserVersion(ctx, tx)
	if err != nil {
		return schemaUpdater{}, fmt.Errorf("accountsInitialize unable to read database schema version : %v", err)
	}
	return schemaUpdater{schemaVersion: dbVersion, params: params}, nil
}

func InitializeStore(ctx context.Context, tx *sql.Tx, params trackerParams) (*schemaUpdater, error) {
	su, err := makeSchemaUpdater(ctx, tx, params)
	if err != nil {
		return &su, fmt.Errorf("InitializeStore unable to read database schema version : %v", err)
	}

	// check current database version.
	// if database version is greater than supported by current binary, write a warning. This would keep the existing
	// fallback behavior where we could use an older binary iff the schema happen to be backward compatible.
	if su.version() > accountDBVersion {
		params.Logger().Warnf("InitializeStore database schema version is %d, but algod supports only %d", su.version(), accountDBVersion)
	}

	if su.version() < accountDBVersion {
		params.Logger().Infof("InitializeStore upgrading database schema from version %d to version %d", su.version(), accountDBVersion)

		// newDatabase is determined during the tables creations. If we're filling the database with accounts,
		// then we set this variable to true, allowing some of the upgrades to be skipped.
		for su.version() < accountDBVersion {
			params.Logger().Infof("InitializeStore performing upgrade from version %d", su.version())
			// perform the initialization/upgrade
			switch su.version() {
			case 0:
				err = su.upgradeDatabaseSchema0(ctx, tx)
				if err != nil {
					params.Logger().Warnf("accountsInitialize failed to upgrade accounts database (ledger.tracker.sqlite) from schema 0 : %v", err)
					return &su, err
				}
			case 1:
				err = su.upgradeDatabaseSchema1(ctx, tx)
				if err != nil {
					params.Logger().Warnf("accountsInitialize failed to upgrade accounts database (ledger.tracker.sqlite) from schema 1 : %v", err)
					return &su, err
				}
			case 2:
				err = su.upgradeDatabaseSchema2(ctx, tx)
				if err != nil {
					params.Logger().Warnf("accountsInitialize failed to upgrade accounts database (ledger.tracker.sqlite) from schema 2 : %v", err)
					return &su, err
				}
			case 3:
				err = su.upgradeDatabaseSchema3(ctx, tx)
				if err != nil {
					params.Logger().Warnf("accountsInitialize failed to upgrade accounts database (ledger.tracker.sqlite) from schema 3 : %v", err)
					return &su, err
				}
			case 4:
				err = su.upgradeDatabaseSchema4(ctx, tx)
				if err != nil {
					params.Logger().Warnf("accountsInitialize failed to upgrade accounts database (ledger.tracker.sqlite) from schema 4 : %v", err)
					return &su, err
				}
			default:
				return &su, fmt.Errorf("accountsInitialize unable to upgrade database from schema version %d", su.version())
			}

			params.Logger().Infof("accountsInitialize database schema upgrade complete")
		}
	}

	return &su, err
}

// accountsInit fills the database using tx with initAccounts if the
// database has not been initialized yet.
//
// accountsInit returns nil if either it has initialized the database
// correctly, or if the database has already been initialized.
func accountsInit(tx *sql.Tx, initAccounts map[basics.Address]basics.AccountData, proto config.ConsensusParams) (newDatabase bool, err error) {
	for _, tableCreate := range accountsSchema {
		_, err = tx.Exec(tableCreate)
		if err != nil {
			return
		}
	}

	// Run creatables migration if it hasn't run yet
	var creatableMigrated bool
	err = tx.QueryRow("SELECT 1 FROM pragma_table_info('assetcreators') WHERE name='ctype'").Scan(&creatableMigrated)
	if err == sql.ErrNoRows {
		// Run migration
		for _, migrateCmd := range creatablesMigration {
			_, err = tx.Exec(migrateCmd)
			if err != nil {
				return
			}
		}
	} else if err != nil {
		return
	}

	_, err = tx.Exec("INSERT INTO acctrounds (id, rnd) VALUES ('acctbase', 0)")
	if err == nil {
		var ot basics.OverflowTracker
		var totals ledgercore.AccountTotals

		for addr, data := range initAccounts {
			_, err = tx.Exec("INSERT INTO accountbase (address, data) VALUES (?, ?)",
				addr[:], protocol.Encode(&data))
			if err != nil {
				return true, err
			}

			totals.AddAccount(proto, data, &ot)
		}

		if ot.Overflowed {
			return true, fmt.Errorf("overflow computing totals")
		}

		err = accountsPutTotals(tx, totals, false)
		if err != nil {
			return true, err
		}
		newDatabase = true
	} else {
		serr, ok := err.(sqlite3.Error)
		// serr.Code is sqlite.ErrConstraint if the database has already been initialized;
		// in that case, ignore the error and return nil.
		if !ok || serr.Code != sqlite3.ErrConstraint {
			return
		}

	}

	return newDatabase, nil
}

func resetAccountHashes(tx *sql.Tx) (err error) {
	_, err = tx.Exec(`DELETE FROM accounthashes`)
	return
}

// upgradeDatabaseSchema0 upgrades the database schema from version 0 to version 1
//
// Schema of version 0 is expected to be aligned with the schema used on version 2.0.8 or before.
// Any database of version 2.0.8 would be of version 0. At this point, the database might
// have the following tables : ( i.e. a newly created database would not have these )
// * acctrounds
// * accounttotals
// * accountbase
// * assetcreators
// * storedcatchpoints
// * accounthashes
// * catchpointstate
//
// As the first step of the upgrade, the above tables are being created if they do not already exists.
// Following that, the assetcreators table is being altered by adding a new column to it (ctype).
// Last, in case the database was just created, it would get initialized with the following:
// The accountbase would get initialized with the au.initAccounts
// The accounttotals would get initialized to align with the initialization account added to accountbase
// The acctrounds would get updated to indicate that the balance matches round 0
//
func (su *schemaUpdater) upgradeDatabaseSchema0(ctx context.Context, tx *sql.Tx) (err error) {
	su.params.Logger().Infof("accountsInitialize initializing schema")
	su.newDatabase, err = accountsInit(tx, su.params.GetInitAccounts(), su.params.GetInitProto())
	if err != nil {
		return fmt.Errorf("accountsInitialize unable to initialize schema : %v", err)
	}
	return su.setVersion(ctx, tx, 1)
}

// upgradeDatabaseSchema1 upgrades the database schema from version 1 to version 2
//
// The schema updated to version 2 intended to ensure that the encoding of all the accounts data is
// both canonical and identical across the entire network. On release 2.0.5 we released an upgrade to the messagepack.
// the upgraded messagepack was decoding the account data correctly, but would have different
// encoding compared to it's predecessor. As a result, some of the account data that was previously stored
// would have different encoded representation than the one on disk.
// To address this, this startup procedure would attempt to scan all the accounts data. for each account data, we would
// see if it's encoding aligns with the current messagepack encoder. If it doesn't we would update it's encoding.
// then, depending if we found any such account data, we would reset the merkle trie and stored catchpoints.
// once the upgrade is complete, the accountsInitialize would (if needed) rebuild the merkle trie using the new
// encoded accounts.
//
// This upgrade doesn't change any of the actual database schema ( i.e. tables, indexes ) but rather just performing
// a functional update to it's content.
//
func (su *schemaUpdater) upgradeDatabaseSchema1(ctx context.Context, tx *sql.Tx) (err error) {
	var modifiedAccounts uint
	if su.newDatabase {
		goto schemaUpdateComplete
	}

	// update accounts encoding.
	su.params.Logger().Infof("upgradeDatabaseSchema1 verifying accounts data encoding")
	modifiedAccounts, err = reencodeAccounts(ctx, tx)
	if err != nil {
		return err
	}

	if modifiedAccounts > 0 {
		su.params.Logger().Infof("upgradeDatabaseSchema1 reencoded %d accounts", modifiedAccounts)

		su.params.Logger().Infof("upgradeDatabaseSchema1 resetting account hashes")
		// reset the merkle trie
		err = resetAccountHashes(tx)
		if err != nil {
			return fmt.Errorf("upgradeDatabaseSchema1 unable to reset account hashes : %v", err)
		}

		su.params.Logger().Infof("upgradeDatabaseSchema1 preparing queries")
		// initialize a new accountsq with the incoming transaction.
		catchpointq, err := catchpointDbInit(tx, tx)
		if err != nil {
			return fmt.Errorf("upgradeDatabaseSchema1 unable to prepare queries : %v", err)
		}

		// close the prepared statements when we're done with them.
		defer catchpointq.close()

		su.params.Logger().Infof("upgradeDatabaseSchema1 resetting prior catchpoints")
		// delete the last catchpoint label if we have any.
		_, err = catchpointq.writeCatchpointStateString(ctx, catchpointStateLastCatchpoint, "")
		if err != nil {
			return fmt.Errorf("upgradeDatabaseSchema1 unable to clear prior catchpoint : %v", err)
		}

		su.params.Logger().Infof("upgradeDatabaseSchema1 deleting stored catchpoints")
		// delete catchpoints.
		err = deleteStoredCatchpoints(ctx, catchpointq, su.params.Dir())
		if err != nil {
			return fmt.Errorf("upgradeDatabaseSchema1 unable to delete stored catchpoints : %v", err)
		}
	} else {
		su.params.Logger().Infof("upgradeDatabaseSchema1 found that no accounts needed to be reencoded")
	}

schemaUpdateComplete:
	return su.setVersion(ctx, tx, 2)
}

// upgradeDatabaseSchema2 upgrades the database schema from version 2 to version 3
//
// This upgrade only enables the database vacuuming which will take place once the upgrade process is complete.
// If the user has already specified the OptimizeAccountsDatabaseOnStartup flag in the configuration file, this
// step becomes a no-op.
//
func (su *schemaUpdater) upgradeDatabaseSchema2(ctx context.Context, tx *sql.Tx) (err error) {
	if !su.newDatabase {
		su.vacuumOnStartup = true
	}

	return su.setVersion(ctx, tx, 3)
}

// upgradeDatabaseSchema3 upgrades the database schema from version 3 to version 4,
// adding the normalizedonlinebalance column to the accountbase table.
func (su *schemaUpdater) upgradeDatabaseSchema3(ctx context.Context, tx *sql.Tx) (err error) {
	err = accountsAddNormalizedBalance(tx, su.params.GetInitProto())
	if err != nil {
		return err
	}

	return su.setVersion(ctx, tx, 4)
}

// upgradeDatabaseSchema4 does not change the schema but migrates data:
// remove empty AccountData entries from accountbase table
func (su *schemaUpdater) upgradeDatabaseSchema4(ctx context.Context, tx *sql.Tx) (err error) {
	catchpointEnabled, mtConfig := su.params.CatchpointConfig()
	var numDeleted int64
	var addresses []basics.Address

	if su.newDatabase {
		goto done
	}

	numDeleted, addresses, err = removeEmptyAccountData(tx, catchpointEnabled)
	if err != nil {
		return err
	}

	if catchpointEnabled && len(addresses) > 0 {
		mc, err := MakeMerkleCommitter(tx, false)
		if err != nil {
			// at this point record deleted and DB is pruned for account data
			// if hash deletion fails just log it and do not abort startup
			su.params.Logger().Errorf("upgradeDatabaseSchema4: failed to create merkle committer: %v", err)
			goto done
		}
		// TrieMemoryConfig
		trie, err := merkletrie.MakeTrie(mc, mtConfig)
		if err != nil {
			su.params.Logger().Errorf("upgradeDatabaseSchema4: failed to create merkle trie: %v", err)
			goto done
		}

		var totalHashesDeleted int
		for _, addr := range addresses {
			hash := ledgercore.AccountHashBuilder(addr, 0, []byte{0x80})
			deleted, err := trie.Delete(hash)
			if err != nil {
				su.params.Logger().Errorf("upgradeDatabaseSchema4: failed to delete hash '%s' from merkle trie for account %v: %v", hex.EncodeToString(hash), addr, err)
			} else {
				if !deleted {
					su.params.Logger().Warnf("upgradeDatabaseSchema4: failed to delete hash '%s' from merkle trie for account %v", hex.EncodeToString(hash), addr)
				} else {
					totalHashesDeleted++
				}
			}
		}

		if _, err = trie.Commit(); err != nil {
			su.params.Logger().Errorf("upgradeDatabaseSchema4: failed to commit changes to merkle trie: %v", err)
		}

		su.params.Logger().Infof("upgradeDatabaseSchema4: deleted %d hashes", totalHashesDeleted)
	}

done:
	su.params.Logger().Infof("upgradeDatabaseSchema4: deleted %d rows", numDeleted)

	return su.setVersion(ctx, tx, 5)
}

// deleteStoredCatchpoints iterates over the storedcatchpoints table and deletes all the files stored on disk.
// once all the files have been deleted, it would go ahead and remove the entries from the table.
func deleteStoredCatchpoints(ctx context.Context, dbQueries *catchpointDbQueries, dbDirectory string) (err error) {
	catchpointsFilesChunkSize := 50
	for {
		fileNames, err := dbQueries.getOldestCatchpointFiles(ctx, catchpointsFilesChunkSize, 0)
		if err != nil {
			return err
		}
		if len(fileNames) == 0 {
			break
		}

		for round, fileName := range fileNames {
			absCatchpointFileName := filepath.Join(dbDirectory, fileName)
			err = os.Remove(absCatchpointFileName)
			if err == nil || os.IsNotExist(err) {
				// it's ok if the file doesn't exist. just remove it from the database and we'll be good to go.
			} else {
				// we can't delete the file, abort -
				return fmt.Errorf("unable to delete old catchpoint file '%s' : %v", absCatchpointFileName, err)
			}
			// clear the entry from the database
			err = dbQueries.storeCatchpoint(ctx, round, "", "", 0)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
