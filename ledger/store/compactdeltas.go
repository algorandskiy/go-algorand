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
	"database/sql"

	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/ledger/ledgercore"
	"github.com/algorand/go-algorand/protocol"
)

// CompactAccountDeltas and accountDelta is an extension to ledgercore.AccountDeltas that is being used by the commitRound function for counting the
// number of changes we've made per account. The ndeltas is used exclusively for consistency checking - making sure that
// all the pending changes were written and that there are no outstanding writes missing.
type CompactAccountDeltas struct {
	// actual data
	deltas []accountDelta
	// addresses for deltas
	addresses []basics.Address
	// cache for addr to deltas index resolution
	cache map[basics.Address]int
	// misses holds indices of addresses for which old portion of delta needs to be loaded from disk
	misses []int

	// iteration related stuff
	iterStarted bool
	iterPos     int
}

type accountDelta struct {
	old     PersistedAccountData
	new     basics.AccountData
	ndeltas int
}

type accountRCache interface {
	Read(basics.Address) (PersistedAccountData, bool)
}

// MakeCompactAccountDeltas takes an array of account AccountDeltas ( one array entry per round ), and compacts the arrays into a single
// data structure that contains all the account deltas changes. While doing that, the function eliminate any intermediate account changes.
// It counts the number of changes per round by specifying it in the ndeltas field of the accountDeltaCount/modifiedCreatable.
func MakeCompactAccountDeltas(accountDeltas []ledgercore.AccountDeltas, baseAccounts accountRCache) (outAccountDeltas CompactAccountDeltas) {
	if len(accountDeltas) == 0 {
		return
	}

	// the sizes of the maps here aren't super accurate, but would hopefully be a rough estimate for a reasonable starting point.
	size := accountDeltas[0].Len()*len(accountDeltas) + 1
	outAccountDeltas.cache = make(map[basics.Address]int, size)
	outAccountDeltas.deltas = make([]accountDelta, 0, size)
	outAccountDeltas.misses = make([]int, 0, size)

	for _, roundDelta := range accountDeltas {
		for i := 0; i < roundDelta.Len(); i++ {
			addr, acctDelta := roundDelta.GetByIdx(i)
			if prev, idx := outAccountDeltas.get(addr); idx != -1 {
				outAccountDeltas.update(idx, accountDelta{ // update instead of upsert economizes one map lookup
					old:     prev.old,
					new:     acctDelta,
					ndeltas: prev.ndeltas + 1,
				})
			} else {
				// it's a new entry.
				newEntry := accountDelta{
					new:     acctDelta,
					ndeltas: 1,
				}
				if baseAccountData, has := baseAccounts.Read(addr); has {
					newEntry.old = baseAccountData
					outAccountDeltas.insert(addr, newEntry) // insert instead of upsert economizes one map lookup
				} else {
					outAccountDeltas.insertMissing(addr, newEntry)
				}
			}
		}
	}
	return
}

type CompactDeltaItem interface {
	HasOld() bool
	HasNew() bool
	Old() basics.AccountData
	New() basics.AccountData
}

type CompactDeltasIter struct {
	pos int
	cd  *CompactAccountDeltas
}

func (it *CompactDeltasIter) HasNext() bool {
	if it.pos < it.cd.len() {
		return true
	}
	return false
}

func (it *CompactDeltasIter) GetNext() (basics.Address, CompactDeltaItem) {
	if it.pos < it.cd.len() {
		addr, delta := it.cd.getByIdx(it.pos)
		it.pos++
		return addr, &delta
	}
	return basics.Address{}, nil
}

func (d *accountDelta) HasOld() bool {
	return !d.old.AccountData.IsZero()
}

func (d *accountDelta) HasNew() bool {
	return !d.new.IsZero()
}

func (d *accountDelta) Old() basics.AccountData {
	return d.old.AccountData
}

func (d *accountDelta) New() basics.AccountData {
	return d.new
}

func (a *CompactAccountDeltas) Iter() *CompactDeltasIter {
	return &CompactDeltasIter{pos: 0, cd: a}
}

func (a CompactAccountDeltas) hasMisses() bool {
	return len(a.misses) != 0
}

// accountsLoadOld updates the entries on the deltas.old map that matches the provided addresses.
// The round number of the persistedAccountData is not updated by this function, and the caller is responsible
// for populating this field.
func (a *CompactAccountDeltas) accountsLoadOld(stmt *sql.Stmt) (err error) {
	defer func() {
		a.misses = nil
	}()

	var rowid sql.NullInt64
	var acctDataBuf []byte
	for _, idx := range a.misses {
		addr := a.addresses[idx]
		err = stmt.QueryRow(addr[:]).Scan(&rowid, &acctDataBuf)
		switch err {
		case nil:
			if len(acctDataBuf) > 0 {
				persistedAcctData := &PersistedAccountData{Addr: addr, rowid: rowid.Int64}
				err = protocol.Decode(acctDataBuf, &persistedAcctData.AccountData)
				if err != nil {
					return err
				}
				a.updateOld(idx, *persistedAcctData)
			} else {
				// to retain backward compatibility, we will treat this condition as if we don't have the account.
				a.updateOld(idx, PersistedAccountData{Addr: addr, rowid: rowid.Int64})
			}
		case sql.ErrNoRows:
			// we don't have that account, just return an empty record.
			a.updateOld(idx, PersistedAccountData{Addr: addr})
			err = nil
		default:
			// unexpected error - let the caller know that we couldn't complete the operation.
			return err
		}
	}
	return
}

// get returns accountDelta by address and its position.
// if no such entry -1 returned
func (a *CompactAccountDeltas) get(addr basics.Address) (accountDelta, int) {
	idx, ok := a.cache[addr]
	if !ok {
		return accountDelta{}, -1
	}
	return a.deltas[idx], idx
}

func (a *CompactAccountDeltas) len() int {
	return len(a.deltas)
}

func (a *CompactAccountDeltas) getByIdx(i int) (basics.Address, accountDelta) {
	return a.addresses[i], a.deltas[i]
}

// upsert updates existing or inserts a new entry
func (a *CompactAccountDeltas) upsert(addr basics.Address, delta accountDelta) {
	if idx, exist := a.cache[addr]; exist { // nil map lookup is OK
		a.deltas[idx] = delta
		return
	}
	a.insert(addr, delta)
}

// update replaces specific entry by idx
func (a *CompactAccountDeltas) update(idx int, delta accountDelta) {
	a.deltas[idx] = delta
}

func (a *CompactAccountDeltas) insert(addr basics.Address, delta accountDelta) int {
	last := len(a.deltas)
	a.deltas = append(a.deltas, delta)
	a.addresses = append(a.addresses, addr)

	if a.cache == nil {
		a.cache = make(map[basics.Address]int)
	}
	a.cache[addr] = last
	return last
}

func (a *CompactAccountDeltas) insertMissing(addr basics.Address, delta accountDelta) {
	idx := a.insert(addr, delta)
	a.misses = append(a.misses, idx)
}

// upsertOld updates existing or inserts a new partial entry with only old field filled
func (a *CompactAccountDeltas) upsertOld(old PersistedAccountData) {
	addr := old.Addr
	if idx, exist := a.cache[addr]; exist {
		a.deltas[idx].old = old
		return
	}
	a.insert(addr, accountDelta{old: old})
}

// updateOld updates existing or inserts a new partial entry with only old field filled
func (a *CompactAccountDeltas) updateOld(idx int, old PersistedAccountData) {
	a.deltas[idx].old = old
}
