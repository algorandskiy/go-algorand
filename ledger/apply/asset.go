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

package apply

import (
	"fmt"

	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/data/transactions"
)

func cloneAssetHoldings(m map[basics.AssetIndex]basics.AssetHolding) map[basics.AssetIndex]basics.AssetHolding {
	res := make(map[basics.AssetIndex]basics.AssetHolding, len(m))
	for id, val := range m {
		res[id] = val
	}
	return res
}

func cloneAssetParams(m map[basics.AssetIndex]basics.AssetParams) map[basics.AssetIndex]basics.AssetParams {
	res := make(map[basics.AssetIndex]basics.AssetParams, len(m))
	for id, val := range m {
		res[id] = val
	}
	return res
}

func getParams(balances Balances, aidx basics.AssetIndex) (resource basics.AccountDataResource, creator basics.Address, err error) {
	creator, exists, err := balances.GetCreator(basics.CreatableIndex(aidx), basics.AssetCreatable)
	if err != nil {
		return
	}

	// For assets, anywhere we're attempting to fetch parameters, we are
	// assuming that the asset should exist.
	if !exists {
		err = fmt.Errorf("asset %d does not exist or has been deleted", aidx)
		return
	}

	resource, found, err := balances.GetParams(creator, basics.CreatableIndex(aidx))
	if err != nil {
		return
	}

	if !found {
		err = fmt.Errorf("asset index %d not found in account %s", aidx, creator.String())
		return
	}

	return
}

// AssetConfig applies an AssetConfig transaction using the Balances interface.
func AssetConfig(cc transactions.AssetConfigTxnFields, header transactions.Header, balances Balances, spec transactions.SpecialAddresses, ad *transactions.ApplyData, txnCounter uint64) error {
	if cc.ConfigAsset == 0 {
		// Allocating an asset.

		// Ensure index is never zero
		newidx := basics.AssetIndex(txnCounter + 1)

		// Sanity check that there isn't an asset with this counter value.
		_, present, err := balances.GetParams(header.Sender, basics.CreatableIndex(newidx))
		if err != nil {
			return err
		}

		if present {
			return fmt.Errorf("already found asset with index %d", newidx)
		}

		record, err := balances.Get(header.Sender, false)
		if err != nil {
			return err
		}

		record.TotalAssets++
		if record.TotalAssets > uint32(balances.ConsensusParams().MaxAssetsPerAccount) {
			return fmt.Errorf("too many assets in account: %d > %d", record.TotalAssets, balances.ConsensusParams().MaxAssetsPerAccount)
		}

		// Tell the cow what asset we created
		info := AssetInfo{
			HasParams:  true,
			HasHolding: true,
			Params:     cc.AssetParams,
			Holding: basics.AssetHolding{
				Amount: cc.AssetParams.Total,
			},
		}

		err = balances.AllocateAsset(header.Sender, newidx, info)
		if err != nil {
			return err
		}

		return balances.Put(header.Sender, record)
	}

	// Re-configuration and destroying must be done by the manager key.
	resource, creator, err := getParams(balances, cc.ConfigAsset)
	if err != nil {
		return err
	}

	params := resource.AssetParams
	if params.Manager.IsZero() || (header.Sender != params.Manager) {
		return fmt.Errorf("this transaction should be issued by the manager. It is issued by %v, manager key %v", header.Sender, params.Manager)
	}

	record, err := balances.Get(creator, false)
	if err != nil {
		return err
	}

	holding := resource.AssetHolding

	// record.Assets = cloneAssetHoldings(record.Assets)
	// record.AssetParams = cloneAssetParams(record.AssetParams)

	if cc.AssetParams == (basics.AssetParams{}) {
		// Destroying an asset.  The creator account must hold
		// the entire outstanding asset amount.
		if holding.Amount != params.Total {
			return fmt.Errorf("cannot destroy asset: creator is holding only %d/%d", holding.Amount, params.Total)
		}

		// Tell the cow what asset we deleted
		err = balances.DeallocateAsset(creator, cc.ConfigAsset, AssetInfo{HasParams: true, HasHolding: true})
		if err != nil {
			return err
		}

		record.TotalAssets--
	} else {
		// Changing keys in an asset.
		if !params.Manager.IsZero() {
			params.Manager = cc.AssetParams.Manager
		}
		if !params.Reserve.IsZero() {
			params.Reserve = cc.AssetParams.Reserve
		}
		if !params.Freeze.IsZero() {
			params.Freeze = cc.AssetParams.Freeze
		}
		if !params.Clawback.IsZero() {
			params.Clawback = cc.AssetParams.Clawback
		}

		balances.UpdateAssetParams(cc.ConfigAsset, params)
	}

	return balances.Put(creator, record)
}

func takeOut(balances Balances, addr basics.Address, asset basics.AssetIndex, amount uint64, bypassFreeze bool) error {
	if amount == 0 {
		return nil
	}

	sndHolding, ok, err := balances.GetAssetHolding(addr, asset)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("asset %v missing from %v", asset, addr)
	}

	if sndHolding.Frozen && !bypassFreeze {
		return fmt.Errorf("asset %v frozen in %v", asset, addr)
	}

	newAmount, overflowed := basics.OSub(sndHolding.Amount, amount)
	if overflowed {
		return fmt.Errorf("underflow on subtracting %d from sender amount %d", amount, sndHolding.Amount)
	}
	sndHolding.Amount = newAmount
	return balances.UpdateAssetHolding(addr, asset, sndHolding)
}

func putIn(balances Balances, addr basics.Address, asset basics.AssetIndex, amount uint64, bypassFreeze bool) error {
	if amount == 0 {
		return nil
	}

	rcvHolding, ok, err := balances.GetAssetHolding(addr, asset)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("asset %v missing from %v", asset, addr)
	}

	if rcvHolding.Frozen && !bypassFreeze {
		return fmt.Errorf("asset frozen in recipient")
	}

	var overflowed bool
	rcvHolding.Amount, overflowed = basics.OAdd(rcvHolding.Amount, amount)
	if overflowed {
		return fmt.Errorf("overflow on adding %d to receiver amount %d", amount, rcvHolding.Amount)
	}

	return balances.UpdateAssetHolding(addr, asset, rcvHolding)
}

// AssetTransfer applies an AssetTransfer transaction using the Balances interface.
func AssetTransfer(ct transactions.AssetTransferTxnFields, header transactions.Header, balances Balances, spec transactions.SpecialAddresses, ad *transactions.ApplyData) error {
	// Default to sending from the transaction sender's account.
	source := header.Sender
	clawback := false

	if !ct.AssetSender.IsZero() {
		// Clawback transaction.  Check that the transaction sender
		// is the Clawback address for this asset.
		params, _, err := getParams(balances, ct.XferAsset)
		if err != nil {
			return err
		}

		if params.Clawback.IsZero() || (header.Sender != params.Clawback) {
			return fmt.Errorf("clawback not allowed: sender %v, clawback %v", header.Sender, params.Clawback)
		}

		// Transaction sent from the correct clawback address,
		// execute asset transfer from specified source.
		source = ct.AssetSender
		clawback = true
	}

	// Allocate a slot for asset (self-transfer of zero amount).
	if ct.AssetAmount == 0 && ct.AssetReceiver == source && !clawback {
		snd, err := balances.Get(source, false)
		if err != nil {
			return err
		}

		sndHolding, ok, err := balances.GetAssetHolding(source, ct.XferAsset)
		if err != nil {
			return err
		}
		if !ok {
			// Initialize holding with default Frozen value.
			params, _, err := getParams(balances, ct.XferAsset)
			if err != nil {
				return err
			}

			sndHolding.Frozen = params.DefaultFrozen
			snd.TotalAssets++

			if snd.TotalAssets > uint32(balances.ConsensusParams().MaxAssetsPerAccount) {
				return fmt.Errorf("too many assets in account: %d > %d", snd.TotalAssets, balances.ConsensusParams().MaxAssetsPerAccount)
			}

			info := AssetInfo{HasHolding: true, Holding: sndHolding}
			err = balances.AllocateAsset(source, ct.XferAsset, info)
			if err != nil {
				return err
			}

			return balances.Put(source, snd)
		}
	}

	// Actually move the asset.  Zero transfers return right away
	// without looking up accounts, so it's fine to have a zero transfer
	// to an all-zero address (e.g., when the only meaningful part of
	// the transaction is the close-to address). Similarly, takeOut and
	// putIn will succeed for zero transfers on frozen asset holdings
	err := takeOut(balances, source, ct.XferAsset, ct.AssetAmount, clawback)
	if err != nil {
		return err
	}

	err = putIn(balances, ct.AssetReceiver, ct.XferAsset, ct.AssetAmount, clawback)
	if err != nil {
		return err
	}

	if ct.AssetCloseTo != (basics.Address{}) {
		// Cannot close by clawback
		if clawback {
			return fmt.Errorf("cannot close asset by clawback")
		}

		// Fetch the sender balance record. We will use this to ensure
		// that the sender is not the creator of the asset, and to
		// figure out how much of the asset to move.
		snd, err := balances.Get(source, false)
		if err != nil {
			return err
		}

		// The creator of the asset cannot close their holding of the
		// asset. Check if we are the creator by seeing if there is an
		// AssetParams entry for the asset index.
		_, ok, err := balances.GetAssetParams(source, ct.XferAsset)
		if err != nil {
			return err
		}
		if ok {
			return fmt.Errorf("cannot close asset ID in allocating account")
		}

		// Fetch our asset holding, which should exist since we're
		// closing it out
		sndHolding, ok, err := balances.GetAssetHolding(source, ct.XferAsset)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("asset %v not present in account %v", ct.XferAsset, source)
		}

		// Fetch the destination balance record to check if we are
		// closing out to the creator

		// Allow closing out to the asset creator even when frozen.
		// If we are closing out 0 units of the asset, then takeOut
		// and putIn will short circuit (so bypassFreeze doesn't matter)
		_, bypassFreeze, err := balances.GetAssetParams(ct.AssetCloseTo, ct.XferAsset)
		if err != nil {
			return err
		}
		// AssetCloseAmount was a late addition, checking that the current protocol version supports it.
		if balances.ConsensusParams().EnableAssetCloseAmount {
			// Add the close amount to ApplyData.
			closeAmount := sndHolding.Amount
			ad.AssetClosingAmount = closeAmount
		}

		// Move the balance out.
		err = takeOut(balances, source, ct.XferAsset, sndHolding.Amount, bypassFreeze)
		if err != nil {
			return err
		}

		// Put the balance in.
		err = putIn(balances, ct.AssetCloseTo, ct.XferAsset, sndHolding.Amount, bypassFreeze)
		if err != nil {
			return err
		}

		sndHolding, _, err = balances.GetAssetHolding(source, ct.XferAsset)
		if err != nil {
			return err
		}
		if sndHolding.Amount != 0 {
			return fmt.Errorf("asset %v not zero (%d) after closing", ct.XferAsset, sndHolding.Amount)
		}

		info := AssetInfo{HasHolding: true}
		err = balances.DeallocateAsset(source, ct.XferAsset, info)
		if err != nil {
			return err
		}

		snd.TotalAssets--
		return balances.Put(source, snd)
	}

	return nil
}

// AssetFreeze applies an AssetFreeze transaction using the Balances interface.
func AssetFreeze(cf transactions.AssetFreezeTxnFields, header transactions.Header, balances Balances, spec transactions.SpecialAddresses, ad *transactions.ApplyData) error {
	// Only the Freeze address can change the freeze value.
	params, _, err := getParams(balances, cf.FreezeAsset)
	if err != nil {
		return err
	}

	if params.Freeze.IsZero() || (header.Sender != params.Freeze) {
		return fmt.Errorf("freeze not allowed: sender %v, freeze %v", header.Sender, params.Freeze)
	}

	// Get the account to be frozen/unfrozen.
	holding, ok, err := balances.GetAssetHolding(cf.FreezeAccount, cf.FreezeAsset)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("asset not found in account")
	}

	holding.Frozen = cf.AssetFrozen
	return balances.UpdateAssetHolding(cf.FreezeAccount, cf.FreezeAsset, holding)
}
