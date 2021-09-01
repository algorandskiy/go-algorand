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

package ledgercore

import (
	"github.com/algorand/go-algorand/config"
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/go-algorand/data/basics"
)

// OnlineAccount corresponds to an account whose AccountData.Status
// is Online.  This is used for a Merkle tree commitment of online
// accounts, which is subsequently used to validate participants for
// a compact certificate.
type OnlineAccount struct {
	// These are a subset of the fields from the corresponding AccountData.
	Address                 basics.Address
	MicroAlgos              basics.MicroAlgos
	RewardsBase             uint64
	NormalizedOnlineBalance uint64
	VoteID                  crypto.OneTimeSignatureVerifier
	VoteFirstValid          basics.Round
	VoteLastValid           basics.Round
	VoteKeyDilution         uint64
}

// AccountDataToOnline returns the part of the AccountData that matters
// for online accounts (to answer top-N queries).  We store a subset of
// the full AccountData because we need to store a large number of these
// in memory (say, 1M), and storing that many AccountData could easily
// cause us to run out of memory.
func AccountDataToOnline(address basics.Address, ad *basics.AccountData, proto config.ConsensusParams) *OnlineAccount {
	return &OnlineAccount{
		Address:                 address,
		MicroAlgos:              ad.MicroAlgos,
		RewardsBase:             ad.RewardsBase,
		NormalizedOnlineBalance: ad.NormalizedOnlineBalance(proto),
		VoteID:                  ad.VoteID,
		VoteFirstValid:          ad.VoteFirstValid,
		VoteLastValid:           ad.VoteLastValid,
		VoteKeyDilution:         ad.VoteKeyDilution,
	}
}
