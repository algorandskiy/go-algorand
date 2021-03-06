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
	"sort"

	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/protocol"
)

// MaxHoldingGroupSize specifies maximum size of AssetsHoldingGroup
const MaxHoldingGroupSize = 256

// AssetsHoldingGroup is a metadata for asset group data (AssetsHoldingGroupData)
// that is stored separately
type AssetsHoldingGroup struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	// assets count in the group
	Count uint32 `codec:"c"`

	// Smallest AssetIndex in the group
	MinAssetIndex basics.AssetIndex `codec:"m"`

	// The delta is relative to MinAssetIndex
	DeltaMaxAssetIndex uint64 `codec:"d"`

	// A foreign key to the accountext table to the appropriate AssetsHoldingGroupData entry
	// AssetGroupKey is 0 for newly created entries and filled after persisting to DB
	AssetGroupKey int64 `codec:"k"`

	// groupData is an actual group data
	groupData AssetsHoldingGroupData

	// loaded indicates either groupData loaded or not
	loaded bool
}

// AssetsHoldingGroupData is an actual asset holding data
type AssetsHoldingGroupData struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	Data []AssetsHoldingGroupDataElement `codec:"d,allocbound=MaxHoldingGroupSize"`
}

// AssetsHoldingGroupDataElement is a single holding element
type AssetsHoldingGroupDataElement struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	// offset relative to MinAssetIndex and differential afterward
	// assetId1 = data[0].AssetOffset + MinAssetIndex; assetIdx1 == MinAssetIndex
	// assetId2 = data[1].AssetOffset + assetIdx1
	// assetId3 = data[2].AssetOffset + assetIdx2
	AssetOffset basics.AssetIndex `codec:"o"`
	Amount      uint64            `codec:"a"`
	Frozen      bool              `codec:"f"`
}

// ExtendedAssetHolding is AccountData's extension for storing asset holdings
type ExtendedAssetHolding struct {
	_struct struct{}             `codec:",omitempty,omitemptyarray"`
	Count   uint32               `codec:"c"`
	Groups  []AssetsHoldingGroup `codec:"gs,allocbound=4096"` // 1M asset holdings
}

// PersistedAccountData represents actual data stored in DB
type PersistedAccountData struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`
	basics.AccountData
	ExtendedAssetHolding ExtendedAssetHolding `codec:"eash"`
}

// SortAssetIndex is a copy from data/basics/sort.go
//msgp:ignore SortAssetIndex
//msgp:sort basics.AssetIndex SortAssetIndex
type SortAssetIndex []basics.AssetIndex

func (a SortAssetIndex) Len() int           { return len(a) }
func (a SortAssetIndex) Less(i, j int) bool { return a[i] < a[j] }
func (a SortAssetIndex) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

// SortAppIndex is a copy from data/basics/sort.go
//msgp:ignore SortAppIndex
//msgp:sort basics.AppIndex SortAppIndex
type SortAppIndex []basics.AppIndex

func (a SortAppIndex) Len() int           { return len(a) }
func (a SortAppIndex) Less(i, j int) bool { return a[i] < a[j] }
func (a SortAppIndex) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

// EncodedMaxAssetsPerAccount is a copy from basics package to resolve deps in msgp-generated file
var EncodedMaxAssetsPerAccount = basics.EncodedMaxAssetsPerAccount

// EncodedMaxAppLocalStates is a copy from basics package to resolve deps in msgp-generated file
var EncodedMaxAppLocalStates = basics.EncodedMaxAppLocalStates

// EncodedMaxAppParams is a copy from basics package to resolve deps in msgp-generated file
var EncodedMaxAppParams = basics.EncodedMaxAppParams

// NumAssetHoldings returns number of assets in the account
func (pad PersistedAccountData) NumAssetHoldings() int {
	if pad.ExtendedAssetHolding.Count > 0 {
		return int(pad.ExtendedAssetHolding.Count)
	}
	return len(pad.AccountData.Assets)
}

func (gd *AssetsHoldingGroupData) update(ai int, hodl basics.AssetHolding) {
	gd.Data[ai].Amount = hodl.Amount
	gd.Data[ai].Frozen = hodl.Frozen
}

func (gd *AssetsHoldingGroupData) delete(ai int) {
	if ai == 0 {
		gd.Data = gd.Data[1:]
		gd.Data[0].AssetOffset = 0
	} else if ai == len(gd.Data)-1 {
		gd.Data = gd.Data[:len(gd.Data)-1]
	} else {
		gd.Data[ai+1].AssetOffset += gd.Data[ai].AssetOffset

		copy(gd.Data[ai:], gd.Data[ai+1:])
		gd.Data = gd.Data[:len(gd.Data)-1]
	}
}

// GetHolding returns AssetHolding from group data by asset index ai
func (gd AssetsHoldingGroupData) GetHolding(ai int) basics.AssetHolding {
	return basics.AssetHolding{Amount: gd.Data[ai].Amount, Frozen: gd.Data[ai].Frozen}
}

// GetHolding returns AssetHolding from group data by asset index ai
func (g AssetsHoldingGroup) GetHolding(ai int) basics.AssetHolding {
	return g.groupData.GetHolding(ai)
}

// Encode returns msgp-encoded group data
func (g AssetsHoldingGroup) Encode() []byte {
	return protocol.Encode(&g.groupData)
}

// TestGetGroupData returns group data. Used in tests only
func (g AssetsHoldingGroup) TestGetGroupData() AssetsHoldingGroupData {
	return g.groupData
}

// Update an asset holding by index
func (g *AssetsHoldingGroup) Update(ai int, holdings basics.AssetHolding) {
	g.groupData.update(ai, holdings)
}

// Loaded return a boolean flag indicated if the group loaded or not
func (g AssetsHoldingGroup) Loaded() bool {
	return g.loaded
}

// Load sets a group data value in the group
func (g *AssetsHoldingGroup) Load(gd AssetsHoldingGroupData) {
	g.groupData = gd
	g.loaded = true
}

func (g *AssetsHoldingGroup) delete(ai int) {
	// although a group with only one element is handled by a caller
	// add a safety check here
	if g.Count == 1 {
		*g = AssetsHoldingGroup{}
		return
	}

	if ai == 0 {
		// when deleting the first element, update MinAssetIndex and DeltaMaxAssetIndex
		g.MinAssetIndex += g.groupData.Data[1].AssetOffset
		g.DeltaMaxAssetIndex -= uint64(g.groupData.Data[1].AssetOffset)
	} else if uint32(ai) == g.Count-1 {
		// when deleting the last element, update DeltaMaxAssetIndex
		idx := len(g.groupData.Data) - 1
		g.DeltaMaxAssetIndex -= uint64(g.groupData.Data[idx].AssetOffset)
	}
	g.groupData.delete(ai)
	g.Count--
}

func (g *AssetsHoldingGroup) insert(aidx basics.AssetIndex, holding basics.AssetHolding) {
	if aidx < g.MinAssetIndex {
		// prepend
		g.groupData.Data[0].AssetOffset = g.MinAssetIndex - aidx
		g.groupData.Data = append(
			[]AssetsHoldingGroupDataElement{
				{AssetOffset: 0, Amount: holding.Amount, Frozen: holding.Frozen}},
			g.groupData.Data...)

		g.DeltaMaxAssetIndex += uint64(g.MinAssetIndex - aidx)
		g.MinAssetIndex = aidx
	} else if aidx >= g.MinAssetIndex+basics.AssetIndex(g.DeltaMaxAssetIndex) {
		// append
		lastAssetIndex := g.MinAssetIndex + basics.AssetIndex(g.DeltaMaxAssetIndex)
		delta := aidx - lastAssetIndex
		g.groupData.Data = append(
			g.groupData.Data,
			AssetsHoldingGroupDataElement{AssetOffset: delta, Amount: holding.Amount, Frozen: holding.Frozen})

		g.DeltaMaxAssetIndex = uint64(aidx - g.MinAssetIndex)
	} else {
		// find position and insert
		cur := g.MinAssetIndex
		for ai, d := range g.groupData.Data {
			cur = d.AssetOffset + cur
			if aidx < cur {
				prev := cur - d.AssetOffset

				g.groupData.Data = append(g.groupData.Data, AssetsHoldingGroupDataElement{})
				copy(g.groupData.Data[ai:], g.groupData.Data[ai-1:])
				g.groupData.Data[ai] = AssetsHoldingGroupDataElement{
					AssetOffset: aidx - prev,
					Amount:      holding.Amount,
					Frozen:      holding.Frozen,
				}
				g.groupData.Data[ai+1].AssetOffset = cur - aidx
				break
			}
		}
	}
	g.Count++
}

// Delete removes asset by index ai from group gi.
// It returns true if the group gone and needs to be removed from DB
func (e *ExtendedAssetHolding) Delete(gi int, ai int) bool {
	if e.Groups[gi].Count == 1 {
		copy(e.Groups[gi:], e.Groups[gi+1:])
		e.Groups = e.Groups[:len(e.Groups)-1]
		e.Count--
		return true
	}
	e.Groups[gi].delete(ai)
	e.Count--
	return false
}

// splitInsert splits the group identified by gi
// and inserts a new asset into appropriate left or right part of the split
func (e *ExtendedAssetHolding) splitInsert(gi int, aidx basics.AssetIndex, holding basics.AssetHolding) {
	g := e.Groups[gi]
	pos := g.Count / 2
	asset := g.MinAssetIndex
	for i := 0; i < int(pos); i++ {
		asset += g.groupData.Data[i].AssetOffset
	}
	rgCount := g.Count - g.Count/2
	rgMinAssetIndex := asset + g.groupData.Data[pos].AssetOffset
	rgDeltaMaxIndex := g.MinAssetIndex + basics.AssetIndex(g.DeltaMaxAssetIndex) - rgMinAssetIndex
	lgMinAssetIndex := g.MinAssetIndex
	lgCount := g.Count - rgCount
	lgDeltaMaxIndex := asset - g.MinAssetIndex

	rgCap := rgCount
	if aidx >= lgMinAssetIndex+lgDeltaMaxIndex {
		// if new asset goes into right group, reserve space
		rgCap++
	}
	rgd := AssetsHoldingGroupData{Data: make([]AssetsHoldingGroupDataElement, rgCount, rgCap)}

	copy(rgd.Data, g.groupData.Data[pos:])
	rightGroup := AssetsHoldingGroup{
		Count:              rgCount,
		MinAssetIndex:      rgMinAssetIndex,
		DeltaMaxAssetIndex: uint64(rgDeltaMaxIndex),
		groupData:          rgd,
		loaded:             true,
	}
	rightGroup.groupData.Data[0].AssetOffset = 0

	e.Groups[gi].Count = lgCount
	e.Groups[gi].DeltaMaxAssetIndex = uint64(lgDeltaMaxIndex)
	e.Groups[gi].groupData = AssetsHoldingGroupData{
		Data: g.groupData.Data[:pos],
	}
	if aidx < lgMinAssetIndex+lgDeltaMaxIndex {
		e.Groups[gi].insert(aidx, holding)
	} else {
		rightGroup.insert(aidx, holding)
	}

	e.Count++
	e.Groups = append(e.Groups, AssetsHoldingGroup{})
	copy(e.Groups[gi+1:], e.Groups[gi:])
	e.Groups[gi+1] = rightGroup
}

// Insert takes an array of asset holdings into ExtendedAssetHolding.
// The input sequence must be sorted.
func (e *ExtendedAssetHolding) Insert(input []basics.AssetIndex, data map[basics.AssetIndex]basics.AssetHolding) {
	gi := 0
	for _, aidx := range input {
		result := e.findGroup(aidx, gi)
		if result.found {
			if result.split {
				e.splitInsert(result.gi, aidx, data[aidx])
			} else {
				e.Groups[result.gi].insert(aidx, data[aidx])
				e.Count++
			}
			gi = result.gi // advance group search offset (input is ordered, it is safe to search from the last match)
		} else {
			insertAfter := result.gi
			holding := data[aidx]
			g := AssetsHoldingGroup{
				Count:              1,
				MinAssetIndex:      aidx,
				DeltaMaxAssetIndex: 0,
				AssetGroupKey:      0,
				groupData: AssetsHoldingGroupData{
					Data: []AssetsHoldingGroupDataElement{{
						AssetOffset: 0,
						Amount:      holding.Amount,
						Frozen:      holding.Frozen,
					}},
				},
				loaded: true,
			}
			if insertAfter == -1 {
				// special case, prepend
				e.Groups = append([]AssetsHoldingGroup{g}, e.Groups...)
			} else if insertAfter == len(e.Groups)-1 {
				// save on two copying compare to the default branch below
				e.Groups = append(e.Groups, g)
			} else {
				// insert after result.gi
				e.Groups = append(e.Groups, AssetsHoldingGroup{})
				copy(e.Groups[result.gi+1:], e.Groups[result.gi:])
				e.Groups[result.gi+1] = g
			}
			e.Count++
			gi = result.gi + 1
		}
	}
	return
}

// fgres structure describes result value of findGroup function
//
// +-------+-----------------------------+-------------------------------+
// | found | gi                          | split                         |
// |-------|-----------------------------|-------------------------------|
// | true  | target group index          | split the target group or not |
// | false | group index to insert after | not used                      |
// +-------+-----------------------------+-------------------------------+
type fgres struct {
	found bool
	gi    int
	split bool
}

// findGroup looks up for an appropriate group or position for insertion a new asset holdings entry
// Examples:
//   groups of size 4
//   [2, 3, 5], [7, 10, 12, 15]
//   aidx = 0 -> group 0
//   aidx = 4 -> group 0
//   aidx = 6 -> group 0
//   aidx = 8 -> group 1 split
//   aidx = 16 -> new group after 1
//
//   groups of size 4
//   [1, 2, 3, 5], [7, 10, 15]
//   aidx = 0 -> new group after -1
//   aidx = 4 -> group 0 split
//   aidx = 6 -> group 1
//   aidx = 16 -> group 1
//
//   groups of size 4
//   [1, 2, 3, 5], [7, 10, 12, 15]
//   aidx = 6 -> new group after 0

func (e ExtendedAssetHolding) findGroup(aidx basics.AssetIndex, startIdx int) fgres {
	if e.Count == 0 {
		return fgres{false, -1, false}
	}
	for i, g := range e.Groups[startIdx:] {
		// check exact boundaries
		if aidx >= g.MinAssetIndex && aidx <= g.MinAssetIndex+basics.AssetIndex(g.DeltaMaxAssetIndex) {
			// found a group that is a right place for the asset
			// if it has space, insert into it
			if g.Count < MaxHoldingGroupSize {
				return fgres{found: true, gi: i + startIdx, split: false}
			}
			// otherwise split into two groups
			return fgres{found: true, gi: i + startIdx, split: true}
		}
		// check upper bound
		if aidx >= g.MinAssetIndex && aidx > g.MinAssetIndex+basics.AssetIndex(g.DeltaMaxAssetIndex) {
			// the asset still might fit into a group if it has space and does not break groups order
			if g.Count < MaxHoldingGroupSize {
				// ensure next group starts with the asset greater than current one
				if i+startIdx < len(e.Groups)-1 && aidx < e.Groups[i+startIdx+1].MinAssetIndex {
					return fgres{found: true, gi: i + startIdx, split: false}
				}
				// the last group, ok to add more
				if i+startIdx == len(e.Groups)-1 {
					return fgres{found: true, gi: i + startIdx, split: false}
				}
			}
		}

		// check bottom bound
		if aidx < g.MinAssetIndex {
			// found a group that is a right place for the asset
			// if it has space, insert into it
			if g.Count < MaxHoldingGroupSize {
				return fgres{found: true, gi: i + startIdx, split: false}
			}
			// otherwise insert group before the current one
			return fgres{found: false, gi: i + startIdx - 1, split: false}
		}
	}

	// no matching groups then add a new group at the end
	return fgres{found: false, gi: len(e.Groups) - 1, split: false}
}

// FindGroup returns a group suitable for asset insertion
func (e ExtendedAssetHolding) FindGroup(aidx basics.AssetIndex, startIdx int) int {
	res := e.findGroup(aidx, startIdx)
	if res.found {
		return res.gi
	}
	return -1
}

// FindAsset returns group index and asset index if found and (-1, -1) otherwise.
// If a matching group found but the group is not loaded yet, it returns (gi, -1)
func (e ExtendedAssetHolding) FindAsset(aidx basics.AssetIndex, startIdx int) (int, int) {
	if e.Count == 0 {
		return -1, -1
	}

	for gi, g := range e.Groups[startIdx:] {
		if aidx >= g.MinAssetIndex && aidx <= g.MinAssetIndex+basics.AssetIndex(g.DeltaMaxAssetIndex) {
			if !g.loaded {
				// groupData not loaded, but the group boundaries match
				// return group match and -1 as asset index indicating loading is need
				return gi, -1
			}

			// linear search because AssetOffsets is delta-encoded, not values
			cur := g.MinAssetIndex
			for ai, d := range g.groupData.Data {
				cur = d.AssetOffset + cur
				if aidx == cur {
					return gi + startIdx, ai
				}
			}

			// the group is loaded and the asset not found
			return -1, -1
		}
	}

	return -1, -1
}

// ConvertToGroups converts asset holdings map to groups/group data
func (e *ExtendedAssetHolding) ConvertToGroups(assets map[basics.AssetIndex]basics.AssetHolding) {
	if len(assets) == 0 {
		return
	}

	type asset struct {
		aidx     basics.AssetIndex
		holdings basics.AssetHolding
	}
	flatten := make([]asset, len(assets), len(assets))
	i := 0
	for k, v := range assets {
		flatten[i] = asset{k, v}
		i++
	}
	// TODO: possible optimization
	// if max asset id - min asset id less than X then use counting/radix sort ?
	sort.SliceStable(flatten, func(i, j int) bool { return flatten[i].aidx < flatten[j].aidx })

	numGroups := len(assets) / MaxHoldingGroupSize
	if len(assets)%MaxHoldingGroupSize != 0 {
		numGroups++
	}
	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}

	e.Count = uint32(len(assets))
	e.Groups = make([]AssetsHoldingGroup, numGroups)

	size := min(MaxHoldingGroupSize, len(assets))
	currentGroup := 0
	e.Groups[currentGroup] = AssetsHoldingGroup{
		Count:         uint32(size),
		MinAssetIndex: flatten[0].aidx,
		groupData: AssetsHoldingGroupData{
			Data: make([]AssetsHoldingGroupDataElement, size, size),
		},
		loaded: true,
	}
	prevAssetIndex := e.Groups[currentGroup].MinAssetIndex
	prevGroupIndex := currentGroup
	for i, a := range flatten {
		currentGroup = i / MaxHoldingGroupSize
		if currentGroup != prevGroupIndex {
			e.Groups[prevGroupIndex].DeltaMaxAssetIndex = uint64(flatten[i-1].aidx - e.Groups[prevGroupIndex].MinAssetIndex)
			size := min(MaxHoldingGroupSize, len(assets)-i)
			e.Groups[currentGroup] = AssetsHoldingGroup{
				Count:         uint32(size),
				MinAssetIndex: flatten[i].aidx,
				groupData: AssetsHoldingGroupData{
					Data: make([]AssetsHoldingGroupDataElement, size, size),
				},
				loaded: true,
			}
			prevAssetIndex = e.Groups[currentGroup].MinAssetIndex
			prevGroupIndex = currentGroup
		}
		e.Groups[currentGroup].groupData.Data[i%MaxHoldingGroupSize] = AssetsHoldingGroupDataElement{
			AssetOffset: a.aidx - prevAssetIndex,
			Amount:      a.holdings.Amount,
			Frozen:      a.holdings.Frozen,
		}
		prevAssetIndex = a.aidx
	}
	e.Groups[currentGroup].DeltaMaxAssetIndex = uint64(flatten[len(flatten)-1].aidx - e.Groups[currentGroup].MinAssetIndex)
}

// TestClear removes all the groups, used in tests only
func (e *ExtendedAssetHolding) TestClear() {
	for i := 0; i < len(e.Groups); i++ {
		// ignored on serialization
		e.Groups[i].groupData = AssetsHoldingGroupData{}
		e.Groups[i].loaded = false
	}
}
