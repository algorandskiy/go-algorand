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
	"fmt"
	"sort"

	"github.com/algorand/go-algorand/data/basics"
	"github.com/algorand/go-algorand/protocol"
)

// MaxHoldingGroupSize specifies maximum size of AssetsHoldingGroup
const MaxHoldingGroupSize = 256

// AssetGroupDesc is asset group descriptor
type AssetGroupDesc struct {
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
}

// AssetsHoldingGroup is a metadata for asset group data (AssetsHoldingGroupData)
// that is stored separately
type AssetsHoldingGroup struct {
	AssetGroupDesc
	// groupData is an actual group data
	groupData AssetsHoldingGroupData
	// loaded indicates either groupData loaded or not
	loaded bool
}

// AssetsParamGroup is a metadata for asset group data (AssetsParamGroupData)
// that is stored separately
type AssetsParamGroup struct {
	AssetGroupDesc
	// groupData is an actual group data
	groupData AssetsParamGroupData
	// loaded indicates either groupData loaded or not
	loaded bool
}

type AssetsCommonGroupData struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	// offset relative to MinAssetIndex and differential afterward
	// assetId1 = AssetOffsets[0] + MinAssetIndex and assetIdx1 == MinAssetIndex
	// assetId2 = AssetOffsets[1] + assetIdx1
	// assetId3 = AssetOffsets[2] + assetIdx2
	AssetOffsets []basics.AssetIndex `codec:"ao,allocbound=MaxHoldingGroupSize"`
}

// AssetsParamGroupData is an actual asset param data
type AssetsParamGroupData struct {
	AssetsCommonGroupData

	// Param Total
	// same number of elements as in AssetOffsets
	Totals []uint64 `codec:"t,allocbound=MaxHoldingGroupSize"`

	// Param DefaultFrozen
	// same number of elements as in AssetOffsets
	DefaultFrozens []bool `codec:"f,allocbound=MaxHoldingGroupSize"`

	UnitNames    []string         `codec:"u,allocbound=MaxHoldingGroupSize"`
	AssetNames   []string         `codec:"n,allocbound=MaxHoldingGroupSize"`
	URLs         []string         `codec:"l,allocbound=MaxHoldingGroupSize"`
	MetadataHash [][32]byte       `codec:"h,allocbound=MaxHoldingGroupSize"`
	Managers     []basics.Address `codec:"m,allocbound=MaxHoldingGroupSize"`
	Reserves     []basics.Address `codec:"r,allocbound=MaxHoldingGroupSize"`
	Freezes      []basics.Address `codec:"z,allocbound=MaxHoldingGroupSize"`
	Clawbacks    []basics.Address `codec:"c,allocbound=MaxHoldingGroupSize"`
}

// AssetsHoldingGroupData is an actual asset holding data
type AssetsHoldingGroupData struct {
	AssetsCommonGroupData

	// Holding amount
	// same number of elements as in AssetOffsets
	Amounts []uint64 `codec:"a,allocbound=MaxHoldingGroupSize"`

	// Holding "frozen" flag
	// same number of elements as in AssetOffsets
	Frozens []bool `codec:"f,allocbound=MaxHoldingGroupSize"`
}

const maxEncodedGroupsSize = 4096

// ExtendedAssetHolding is AccountData's extension for storing asset holdings
type ExtendedAssetHolding struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	Count  uint32               `codec:"c"`
	Groups []AssetsHoldingGroup `codec:"gs,allocbound=maxEncodedGroupsSize"` // 1M asset holdings
}

// ExtendedAssetParam is AccountData's extension for storing asset params
type ExtendedAssetParam struct {
	_struct struct{}           `codec:",omitempty,omitemptyarray"`
	Count   uint32             `codec:"c"`
	Groups  []AssetsParamGroup `codec:"gs,allocbound=4096"` // 1M asset holdings
}

// PersistedAccountData represents actual data stored in DB
type PersistedAccountData struct {
	_struct struct{} `codec:",omitempty,omitemptyarray"`

	basics.AccountData
	ExtendedAssetHolding ExtendedAssetHolding `codec:"eah"`
	ExtendedAssetParam   ExtendedAssetParam   `codec:"eap"`
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

type AbstractAssetGroupData interface {
	Find(aidx basics.AssetIndex, base basics.AssetIndex) int
}

type AbstractAssetGroup interface {
	MinAsset() basics.AssetIndex
	MaxAsset() basics.AssetIndex
	HasSpace() bool
	Loaded() bool
	GroupData() AbstractAssetGroupData
	AssetCount() uint32
	Update(idx int, data interface{})
	Encode() []byte
	Key() int64
	SetKey(key int64)
}
type AbstractAssetGroupList interface {
	// Get returns abstract group
	Get(idx int) AbstractAssetGroup
	// Len returns number of groups in the list
	Len() int
	// Totals returns number of assets inside all the the groups
	Total() uint32
	// Reset initializes group to hold count assets in length groups
	Reset(count uint32, length int)
	// Assign assigns to value group to group[idx]
	Assign(idx int, group interface{})
}

type GroupBuilder interface {
	NewGroup(size int)
	NewElement(offset basics.AssetIndex, data interface{})
	Build(desc AssetGroupDesc) interface{}
}

type Flattener interface {
	Count() uint32
	Data(idx int) interface{}
	AssetIndex(idx int) basics.AssetIndex
}

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

// NumAssetParams returns number of assets in the account
func (pad PersistedAccountData) NumAssetParams() int {
	if pad.ExtendedAssetParam.Count > 0 {
		return int(pad.ExtendedAssetParam.Count)
	}
	return len(pad.AccountData.AssetParams)
}

func (gd *AssetsHoldingGroupData) update(ai int, hodl basics.AssetHolding) {
	gd.Amounts[ai] = hodl.Amount
	gd.Frozens[ai] = hodl.Frozen
}

func (gd *AssetsParamGroupData) update(ai int, params basics.AssetParams) {
	gd.Totals[ai] = params.Total
	gd.DefaultFrozens[ai] = params.DefaultFrozen
	gd.UnitNames[ai] = params.UnitName
	gd.AssetNames[ai] = params.AssetName
	gd.URLs[ai] = params.URL
	copy(gd.MetadataHash[ai][:], params.MetadataHash[:])
	copy(gd.Managers[ai][:], params.Manager[:])
	copy(gd.Reserves[ai][:], params.Reserve[:])
	copy(gd.Freezes[ai][:], params.Freeze[:])
	copy(gd.Clawbacks[ai][:], params.Clawback[:])
}

func (gd *AssetsHoldingGroupData) delete(ai int) {
	if ai == 0 {
		gd.AssetOffsets = gd.AssetOffsets[1:]
		gd.AssetOffsets[0] = 0
		gd.Amounts = gd.Amounts[1:]
		gd.Frozens = gd.Frozens[1:]
	} else if ai == len(gd.AssetOffsets)-1 {
		gd.AssetOffsets = gd.AssetOffsets[:len(gd.AssetOffsets)-1]
		gd.Amounts = gd.Amounts[:len(gd.Amounts)-1]
		gd.Frozens = gd.Frozens[:len(gd.Frozens)-1]
	} else {
		gd.AssetOffsets[ai+1] += gd.AssetOffsets[ai]
		copy(gd.AssetOffsets[ai:], gd.AssetOffsets[ai+1:])
		gd.AssetOffsets = gd.AssetOffsets[:len(gd.AssetOffsets)-1]

		copy(gd.Amounts[ai:], gd.Amounts[ai+1:])
		gd.Amounts = gd.Amounts[:len(gd.Amounts)-1]
		copy(gd.Frozens[ai:], gd.Frozens[ai+1:])
		gd.Frozens = gd.Frozens[:len(gd.Frozens)-1]
	}
}

// GetHolding returns AssetHolding from group data by asset index ai
func (gd AssetsHoldingGroupData) GetHolding(ai int) basics.AssetHolding {
	return basics.AssetHolding{Amount: gd.Amounts[ai], Frozen: gd.Frozens[ai]}
}

// GetHolding returns AssetHolding from group data by asset index ai
func (g AssetsHoldingGroup) GetHolding(ai int) basics.AssetHolding {
	return g.groupData.GetHolding(ai)
}

// Encode returns msgp-encoded group data
func (g AssetsHoldingGroup) Encode() []byte {
	// TODO: use GetEncodingBuf/PutEncodingBuf
	return protocol.Encode(&g.groupData)
}

// Encode returns msgp-encoded group data
func (g AssetsParamGroup) Encode() []byte {
	// TODO: use GetEncodingBuf/PutEncodingBuf
	return protocol.Encode(&g.groupData)
}

// TestGetGroupData returns group data. Used in tests only
func (g AssetsHoldingGroup) TestGetGroupData() AssetsHoldingGroupData {
	return g.groupData
}

// Update an asset holding by index
func (g *AssetsHoldingGroup) update(ai int, holdings basics.AssetHolding) {
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

// delete an asset at position ai in this group
func (g *AssetsHoldingGroup) delete(ai int) {
	// although a group with only one element is handled by a caller
	// add a safety check here
	if g.Count == 1 {
		*g = AssetsHoldingGroup{}
		return
	}

	if ai == 0 {
		// when deleting the first element, update MinAssetIndex and DeltaMaxAssetIndex
		g.MinAssetIndex += g.groupData.AssetOffsets[1]
		g.DeltaMaxAssetIndex -= uint64(g.groupData.AssetOffsets[1])
	} else if uint32(ai) == g.Count-1 {
		// when deleting the last element, update DeltaMaxAssetIndex
		g.DeltaMaxAssetIndex -= uint64(g.groupData.AssetOffsets[len(g.groupData.AssetOffsets)-1])
	}
	g.groupData.delete(ai)
	g.Count--
}

// insert asset aidx into current group. It should not exist in the group
func (g *AssetsHoldingGroup) insert(aidx basics.AssetIndex, holding basics.AssetHolding) {
	if aidx < g.MinAssetIndex {
		// prepend
		g.groupData.Amounts = append([]uint64{holding.Amount}, g.groupData.Amounts...)
		g.groupData.Frozens = append([]bool{holding.Frozen}, g.groupData.Frozens...)
		g.groupData.AssetOffsets[0] = g.MinAssetIndex - aidx
		g.groupData.AssetOffsets = append([]basics.AssetIndex{0}, g.groupData.AssetOffsets...)
		g.DeltaMaxAssetIndex += uint64(g.MinAssetIndex - aidx)
		g.MinAssetIndex = aidx
	} else if aidx > g.MinAssetIndex+basics.AssetIndex(g.DeltaMaxAssetIndex) {
		// append
		g.groupData.Amounts = append(g.groupData.Amounts, holding.Amount)
		g.groupData.Frozens = append(g.groupData.Frozens, holding.Frozen)
		lastAssetIndex := g.MinAssetIndex + basics.AssetIndex(g.DeltaMaxAssetIndex)
		delta := aidx - lastAssetIndex
		g.groupData.AssetOffsets = append(g.groupData.AssetOffsets, delta)
		g.DeltaMaxAssetIndex = uint64(aidx - g.MinAssetIndex)
	} else {
		// find position and insert
		cur := g.MinAssetIndex
		for ai, d := range g.groupData.AssetOffsets {
			cur += d
			if aidx < cur {
				g.groupData.AssetOffsets = append(g.groupData.AssetOffsets, 0)
				copy(g.groupData.AssetOffsets[ai:], g.groupData.AssetOffsets[ai-1:])
				prev := cur - d
				g.groupData.AssetOffsets[ai] = aidx - prev
				g.groupData.AssetOffsets[ai+1] = cur - aidx

				g.groupData.Amounts = append(g.groupData.Amounts, 0)
				copy(g.groupData.Amounts[ai:], g.groupData.Amounts[ai-1:])
				g.groupData.Amounts[ai] = holding.Amount

				g.groupData.Frozens = append(g.groupData.Frozens, false)
				copy(g.groupData.Frozens[ai:], g.groupData.Frozens[ai-1:])
				g.groupData.Frozens[ai] = holding.Frozen

				break
			}
		}
	}
	g.Count++
}

// Loaded return a boolean flag indicated if the group loaded or not
func (g AssetsParamGroup) Loaded() bool {
	return g.loaded
}

func (g *AssetsParamGroup) update(ai int, params basics.AssetParams) {
	g.groupData.update(ai, params)
}

func (g *AssetsCommonGroupData) Find(aidx basics.AssetIndex, base basics.AssetIndex) int {
	// linear search because AssetOffsets is delta-encoded, not values
	cur := base
	for ai, d := range g.AssetOffsets {
		cur = d + cur
		if aidx == cur {
			return ai
		}
	}
	return -1
}

func (g *AssetGroupDesc) HasSpace() bool {
	return g.Count < MaxHoldingGroupSize
}

func (g *AssetGroupDesc) MinAsset() basics.AssetIndex {
	return g.MinAssetIndex
}

func (g *AssetGroupDesc) MaxAsset() basics.AssetIndex {
	return g.MinAssetIndex + basics.AssetIndex(g.DeltaMaxAssetIndex)
}

func (g *AssetGroupDesc) AssetCount() uint32 {
	return g.Count
}

func (g *AssetGroupDesc) SetKey(key int64) {
	g.AssetGroupKey = key
}

func (g *AssetGroupDesc) Key() int64 {
	return g.AssetGroupKey
}

func (g *AssetsHoldingGroup) GroupData() AbstractAssetGroupData {
	return &g.groupData.AssetsCommonGroupData
}

func (g *AssetsParamGroup) GroupData() AbstractAssetGroupData {
	return &g.groupData.AssetsCommonGroupData
}

func (g *AssetsHoldingGroup) Update(ai int, data interface{}) {
	g.update(ai, data.(basics.AssetHolding))
}

func (g *AssetsParamGroup) Update(ai int, data interface{}) {
	g.update(ai, data.(basics.AssetParams))
}

type AssetDataGetter interface {
	Get(aidx basics.AssetIndex) interface{}
}

type AssetHoldingGetter struct {
	assets map[basics.AssetIndex]basics.AssetHolding
}

func (g AssetHoldingGetter) Get(aidx basics.AssetIndex) interface{} {
	return g.assets[aidx]
}

type AssetParamsGetter struct {
	assets map[basics.AssetIndex]basics.AssetParams
}

func (g AssetParamsGetter) Get(aidx basics.AssetIndex) interface{} {
	return g.assets[aidx]
}

// Update an asset holding by index
func (e *ExtendedAssetHolding) Update(updated []basics.AssetIndex, assets map[basics.AssetIndex]basics.AssetHolding) error {
	g := AssetHoldingGetter{assets}
	return update(updated, e, &g)
}

func (e *ExtendedAssetParam) Update(updated []basics.AssetIndex, assets map[basics.AssetIndex]basics.AssetParams) error {
	g := AssetParamsGetter{assets}
	return update(updated, e, &g)
}

func update(updated []basics.AssetIndex, agl AbstractAssetGroupList, assets AssetDataGetter) error {
	sort.SliceStable(updated, func(i, j int) bool { return updated[i] < updated[j] })
	gi, ai := 0, 0
	for _, aidx := range updated {
		gi, ai = findAsset(aidx, gi, agl)
		if gi == -1 || ai == -1 {
			return fmt.Errorf("failed to find asset group for %d: (%d, %d)", aidx, gi, ai)
		}
		agl.Get(gi).Update(ai, assets.Get(aidx))
	}
	return nil
}

// Delete asset holdings identified by asset indexes in assets list
// Function returns list of group keys that needs to be removed from DB
func (e *ExtendedAssetHolding) Delete(assets []basics.AssetIndex) (deleted []int64, err error) {
	// TODO: possible optimizations:
	// 1. pad.NumAssetHoldings() == len(deleted)
	// 2. deletion of entire group
	sort.SliceStable(assets, func(i, j int) bool { return assets[i] < assets[j] })
	gi, ai := 0, 0
	for _, aidx := range assets {
		gi, ai = e.FindAsset(aidx, gi)
		if gi == -1 || ai == -1 {
			err = fmt.Errorf("failed to find asset group for %d: (%d, %d)", aidx, gi, ai)
			return
		}
		// group data is loaded in accountsLoadOld
		key := e.Groups[gi].AssetGroupKey
		if e.delete(gi, ai) {
			deleted = append(deleted, key)
		}
	}
	return
}

func (e *ExtendedAssetHolding) delete(gi int, ai int) bool {
	if e.Groups[gi].Count == 1 {
		if gi < len(e.Groups)-1 {
			copy(e.Groups[gi:], e.Groups[gi+1:])
		}
		e.Groups[len(e.Groups)-1] = AssetsHoldingGroup{} // release AssetsHoldingGroup data
		e.Groups = e.Groups[:len(e.Groups)-1]
		e.Count--
		return true
	}
	e.Groups[gi].delete(ai)
	e.Count--
	return false
}

// splitInsert splits the group identified by gi
// and inserts a new asset into appropriate left or right part of the split.
func (e *ExtendedAssetHolding) splitInsert(gi int, aidx basics.AssetIndex, holding basics.AssetHolding) {
	g := e.Groups[gi]
	pos := g.Count / 2
	asset := g.MinAssetIndex
	for i := 0; i < int(pos); i++ {
		asset += g.groupData.AssetOffsets[i]
	}
	rgCount := g.Count - g.Count/2
	rgMinAssetIndex := asset + g.groupData.AssetOffsets[pos]
	rgDeltaMaxIndex := g.MinAssetIndex + basics.AssetIndex(g.DeltaMaxAssetIndex) - rgMinAssetIndex
	lgMinAssetIndex := g.MinAssetIndex
	lgCount := g.Count - rgCount
	lgDeltaMaxIndex := asset - g.MinAssetIndex

	rgCap := rgCount
	if aidx >= lgMinAssetIndex+lgDeltaMaxIndex {
		// if new asset goes into right group, reserve space
		rgCap++
	}
	rgd := AssetsHoldingGroupData{
		Amounts:               make([]uint64, rgCount, rgCap),
		Frozens:               make([]bool, rgCount, rgCap),
		AssetsCommonGroupData: AssetsCommonGroupData{AssetOffsets: make([]basics.AssetIndex, rgCount, rgCap)},
	}
	copy(rgd.Amounts, g.groupData.Amounts[pos:])
	copy(rgd.Frozens, g.groupData.Frozens[pos:])
	copy(rgd.AssetOffsets, g.groupData.AssetOffsets[pos:])
	rightGroup := AssetsHoldingGroup{
		AssetGroupDesc: AssetGroupDesc{
			Count:              rgCount,
			MinAssetIndex:      rgMinAssetIndex,
			DeltaMaxAssetIndex: uint64(rgDeltaMaxIndex),
		},
		groupData: rgd,
		loaded:    true,
	}
	rightGroup.groupData.AssetOffsets[0] = 0

	e.Groups[gi].Count = lgCount
	e.Groups[gi].DeltaMaxAssetIndex = uint64(lgDeltaMaxIndex)
	e.Groups[gi].groupData = AssetsHoldingGroupData{
		Amounts:               g.groupData.Amounts[:pos],
		Frozens:               g.groupData.Frozens[:pos],
		AssetsCommonGroupData: AssetsCommonGroupData{AssetOffsets: g.groupData.AssetOffsets[:pos]},
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
	sort.SliceStable(input, func(i, j int) bool { return input[i] < input[j] })
	gi := 0
	for _, aidx := range input {
		result := findGroup(aidx, gi, e)
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
				AssetGroupDesc: AssetGroupDesc{
					Count:              1,
					MinAssetIndex:      aidx,
					DeltaMaxAssetIndex: 0,
					AssetGroupKey:      0,
				},
				groupData: AssetsHoldingGroupData{
					AssetsCommonGroupData: AssetsCommonGroupData{AssetOffsets: []basics.AssetIndex{0}},
					Amounts:               []uint64{holding.Amount},
					Frozens:               []bool{holding.Frozen},
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

// func (e ExtendedAssetHolding) findGroup(aidx basics.AssetIndex, startIdx int) fgres {
func findGroup(aidx basics.AssetIndex, startIdx int, agl AbstractAssetGroupList) fgres {
	if agl.Total() == 0 {
		return fgres{false, -1, false}
	}
	for i := startIdx; i < agl.Len(); i++ {
		g := agl.Get(i)
		// check exact boundaries
		if aidx >= g.MinAsset() && aidx <= g.MaxAsset() {
			// found a group that is a right place for the asset
			// if it has space, insert into it
			if g.HasSpace() {
				return fgres{found: true, gi: i, split: false}
			}
			// otherwise split into two groups
			return fgres{found: true, gi: i, split: true}
		}
		// check upper bound
		if aidx >= g.MinAsset() && aidx > g.MaxAsset() {
			// the asset still might fit into a group if it has space and does not break groups order
			if g.HasSpace() {
				// ensure next group starts with the asset greater than current one
				if i < agl.Len()-1 && aidx < agl.Get(i+1).MinAsset() {
					return fgres{found: true, gi: i, split: false}
				}
				// the last group, ok to add more
				if i == agl.Len()-1 {
					return fgres{found: true, gi: i, split: false}
				}
			}
		}

		// check bottom bound
		if aidx < g.MinAsset() {
			// found a group that is a right place for the asset
			// if it has space, insert into it
			if g.HasSpace() {
				return fgres{found: true, gi: i, split: false}
			}
			// otherwise insert group before the current one
			return fgres{found: false, gi: i - 1, split: false}
		}
	}

	// no matching groups then add a new group at the end
	return fgres{found: false, gi: agl.Len() - 1, split: false}
}

// FindGroup returns a group suitable for asset insertion
func (e ExtendedAssetHolding) FindGroup(aidx basics.AssetIndex, startIdx int) int {
	res := findGroup(aidx, startIdx, &e)
	if res.found {
		return res.gi
	}
	return -1
}

// FindAsset returns group index and asset index if found and (-1, -1) otherwise.
// If a matching group found but the group is not loaded yet, it returns (groupIdx, -1)
func (e ExtendedAssetHolding) FindAsset(aidx basics.AssetIndex, startIdx int) (int, int) {
	return findAsset(aidx, startIdx, &e)
}

// FindGroup returns a group suitable for asset insertion
func (e ExtendedAssetParam) FindGroup(aidx basics.AssetIndex, startIdx int) int {
	res := findGroup(aidx, startIdx, &e)
	if res.found {
		return res.gi
	}
	return -1
}

// FindAsset returns group index and asset index if found and (-1, -1) otherwise.
// If a matching group found but the group is not loaded yet, it returns (groupIdx, -1)
func (e ExtendedAssetParam) FindAsset(aidx basics.AssetIndex, startIdx int) (int, int) {
	return findAsset(aidx, startIdx, &e)
}

// findAsset returns group index and asset index if found and (-1, -1) otherwise.
// If a matching group found but the group is not loaded yet, it returns (groupIdx, -1).
// It is suitable for searchin within AbstractAssetGroupList that is either holdings or params types.
func findAsset(aidx basics.AssetIndex, startIdx int, agl AbstractAssetGroupList) (int, int) {
	if agl.Total() == 0 {
		return -1, -1
	}

	for i := startIdx; i < agl.Len(); i++ {
		g := agl.Get(i)
		if aidx >= g.MinAsset() && aidx <= g.MaxAsset() {
			if !g.Loaded() {
				// groupData not loaded, but the group boundaries match
				// return group match and -1 as asset index indicating loading is need
				return i, -1
			}

			if ai := g.GroupData().Find(aidx, g.MinAsset()); ai != -1 {
				return i, ai
			}

			// the group is loaded and the asset not found
			return -1, -1
		}
	}
	return -1, -1
}

type AssetHoldingGroupBuilder struct {
	gd  AssetsHoldingGroupData
	idx int
}

func (b *AssetHoldingGroupBuilder) NewGroup(size int) {
	b.gd = AssetsHoldingGroupData{
		AssetsCommonGroupData: AssetsCommonGroupData{AssetOffsets: make([]basics.AssetIndex, size, size)},
		Amounts:               make([]uint64, size, size),
		Frozens:               make([]bool, size, size),
	}
	b.idx = 0
}
func (b *AssetHoldingGroupBuilder) Build(desc AssetGroupDesc) interface{} {
	defer func() {
		b.gd = AssetsHoldingGroupData{}
		b.idx = 0
	}()

	return AssetsHoldingGroup{
		AssetGroupDesc: desc,
		groupData:      b.gd,
		loaded:         true,
	}
}

func (b *AssetHoldingGroupBuilder) NewElement(offset basics.AssetIndex, data interface{}) {
	b.gd.AssetOffsets[b.idx] = offset
	holding := data.(basics.AssetHolding)
	b.gd.Amounts[b.idx] = holding.Amount
	b.gd.Frozens[b.idx] = holding.Frozen
	b.idx++
}

type AssetFlattener struct {
	assets []flattenAsset
}

type flattenAsset struct {
	aidx basics.AssetIndex
	data interface{}
}

func newAssetHoldingFlattener(assets map[basics.AssetIndex]basics.AssetHolding) *AssetFlattener {
	flatten := make([]flattenAsset, len(assets), len(assets))
	i := 0
	for k, v := range assets {
		flatten[i] = flattenAsset{k, v}
		i++
	}
	sort.SliceStable(flatten, func(i, j int) bool { return flatten[i].aidx < flatten[j].aidx })
	return &AssetFlattener{flatten}
}

func newAssetParamFlattener(assets map[basics.AssetIndex]basics.AssetParams) *AssetFlattener {
	flatten := make([]flattenAsset, len(assets), len(assets))
	i := 0
	for k, v := range assets {
		flatten[i] = flattenAsset{k, v}
		i++
	}
	sort.SliceStable(flatten, func(i, j int) bool { return flatten[i].aidx < flatten[j].aidx })
	return &AssetFlattener{flatten}
}

func (f *AssetFlattener) Count() uint32 {
	return uint32(len(f.assets))
}

func (f *AssetFlattener) AssetIndex(idx int) basics.AssetIndex {
	return f.assets[idx].aidx
}

func (f *AssetFlattener) Data(idx int) interface{} {
	return f.assets[idx].data
}

func (e *ExtendedAssetHolding) ConvertToGroups(assets map[basics.AssetIndex]basics.AssetHolding) {
	if len(assets) == 0 {
		return
	}
	b := AssetHoldingGroupBuilder{}
	flt := newAssetHoldingFlattener(assets)
	convertToGroups(e, flt, &b)
}

func (e *ExtendedAssetParam) ConvertToGroups(assets map[basics.AssetIndex]basics.AssetParams) {
	if len(assets) == 0 {
		return
	}
	b := AssetHoldingGroupBuilder{}
	flt := newAssetParamFlattener(assets)
	convertToGroups(e, flt, &b)
}

// convertToGroups converts data from Flattener into groups produced by GroupBuilder and assigns into AbstractAssetGroupList
func convertToGroups(agl AbstractAssetGroupList, flt Flattener, builder GroupBuilder) {
	min := func(a, b int) int {
		if a < b {
			return a
		}
		return b
	}

	numGroups := int(flt.Count()+MaxHoldingGroupSize-1) / MaxHoldingGroupSize
	agl.Reset(flt.Count(), numGroups)

	for i := 0; i < numGroups; i++ {
		start := i * MaxHoldingGroupSize
		end := min((i+1)*MaxHoldingGroupSize, int(flt.Count()))
		size := end - start
		builder.NewGroup(size)

		first := flt.AssetIndex(start)
		prev := first
		for j, di := start, 0; j < end; j, di = j+1, di+1 {
			offset := flt.AssetIndex(j) - prev
			builder.NewElement(offset, flt.Data(j))
			prev = flt.AssetIndex(j)
		}

		desc := AssetGroupDesc{
			Count:              uint32(size),
			MinAssetIndex:      first,
			DeltaMaxAssetIndex: uint64(prev - first),
		}
		agl.Assign(i, builder.Build(desc))
	}
}

// continuosRange describes range of groups that can be merged
type continuosRange struct {
	start int // group start index
	size  int // number of groups
	count int // total holdings
}

func findLoadedSiblings(agl AbstractAssetGroupList) (loaded []int, crs []continuosRange) {
	// find candidates for merging
	loaded = make([]int, 0, agl.Len())
	for i := 0; i < agl.Len(); i++ {
		g := agl.Get(i)
		if !g.Loaded() {
			continue
		}
		if len(loaded) > 0 && loaded[len(loaded)-1] == i-1 {
			// found continuos range
			exists := false
			if len(crs) != 0 {
				last := &crs[len(crs)-1]
				if last.start+last.size == i {
					last.size++
					last.count += int(g.AssetCount())
					exists = true
				}
			}
			if !exists {
				pg := agl.Get(i - 1)
				count := int(pg.AssetCount() + g.AssetCount())
				crs = append(crs, continuosRange{i - 1, 2, count})
			}
		}
		loaded = append(loaded, i)
	}
	if len(loaded) == 0 {
		return nil, nil
	}

	return
}

// merge merges groups [start, start+size) and returns keys of deleted group data entries
func (e *ExtendedAssetHolding) merge(start int, size int, hint int) (deleted []int64) {
	deleted = make([]int64, 0, hint)
	// process i and i + 1 groups at once => size-1 iterations
	i := 0
	for i < size-1 {
		li := start + i     // left group index, destination
		ri := start + i + 1 // right group index, source
		lg := &e.Groups[li]
		rg := &e.Groups[ri]

		num := int(MaxHoldingGroupSize - lg.Count)
		if num == 0 { // group is full, skip
			i++
			continue
		}
		if num > int(rg.Count) { // source group is shorter than dest capacity, adjust
			num = int(rg.Count)
		}
		groupDelta := rg.MinAssetIndex - (lg.MinAssetIndex + basics.AssetIndex(lg.DeltaMaxAssetIndex))
		delta := basics.AssetIndex(0)
		lg.groupData.AssetOffsets = append(lg.groupData.AssetOffsets, rg.groupData.AssetOffsets[0]+groupDelta)
		for j := 1; j < num; j++ {
			lg.groupData.AssetOffsets = append(lg.groupData.AssetOffsets, rg.groupData.AssetOffsets[j])
			delta += rg.groupData.AssetOffsets[j]
		}
		lg.DeltaMaxAssetIndex += uint64(delta + groupDelta)
		lg.groupData.Amounts = append(lg.groupData.Amounts, rg.groupData.Amounts[:num]...)
		lg.groupData.Frozens = append(lg.groupData.Frozens, rg.groupData.Frozens[:num]...)
		lg.Count += uint32(num)
		if num != int(rg.Count) {
			// src group survived, update it and repeat
			rg.Count -= uint32(num)
			rg.groupData.AssetOffsets = rg.groupData.AssetOffsets[num:]
			delta += rg.groupData.AssetOffsets[0]
			rg.groupData.AssetOffsets[0] = 0
			rg.groupData.Amounts = rg.groupData.Amounts[num:]
			rg.groupData.Frozens = rg.groupData.Frozens[num:]
			rg.MinAssetIndex += delta
			rg.DeltaMaxAssetIndex -= uint64(delta)
			i++
		} else {
			// entire src group gone: save the key and delete from Groups
			deleted = append(deleted, e.Groups[ri].AssetGroupKey)
			if ri == len(e.Groups) {
				// last group, cut and exit
				e.Groups = e.Groups[:len(e.Groups)-1]
				return
			}
			e.Groups = append(e.Groups[:ri], e.Groups[ri+1:]...)
			// restart merging with the same index but decrease size
			size--
		}
	}
	return
}

// Merge attempts to re-merge loaded groups by squashing small loaded sibling groups together
// Returns:
// - loaded list group indices that are loaded and needs to flushed
// - deleted list of group data keys that needs to be deleted
func (e *ExtendedAssetHolding) Merge() (loaded []int, deleted []int64) {
	loaded, crs := findLoadedSiblings(e)
	if len(crs) == 0 {
		return
	}

	someGroupDeleted := false
	offset := 0 // difference in group indexes that happens after deleteion some groups from e.Groups array
	for _, cr := range crs {
		minGroupsRequired := (cr.count + MaxHoldingGroupSize - 1) / MaxHoldingGroupSize
		if minGroupsRequired == cr.size {
			// no gain in merging, skip
			continue
		}
		del := e.merge(cr.start-offset, cr.size, cr.size-minGroupsRequired)
		offset += len(del)
		for _, key := range del {
			someGroupDeleted = true
			if key != 0 { // 0 key means a new group that exist only in memory
				deleted = append(deleted, key)
			}
		}
	}

	if someGroupDeleted {
		// rebuild loaded list since indices changed after merging
		loaded = make([]int, 0, len(loaded)-len(deleted))
		for i := 0; i < len(e.Groups); i++ {
			if e.Groups[i].Loaded() {
				loaded = append(loaded, i)
			}
		}
	}
	return
}

// TestClearGroupData removes all the groups, used in tests only
func (e *ExtendedAssetHolding) TestClearGroupData() {
	for i := 0; i < len(e.Groups); i++ {
		// ignored on serialization
		e.Groups[i].groupData = AssetsHoldingGroupData{}
		e.Groups[i].loaded = false
	}
}

func (e *ExtendedAssetHolding) Get(idx int) AbstractAssetGroup {
	return &(e.Groups[idx])
}

func (e *ExtendedAssetHolding) Len() int {
	return len(e.Groups)
}

func (e *ExtendedAssetHolding) Total() uint32 {
	return e.Count
}

func (e *ExtendedAssetHolding) Reset(count uint32, length int) {
	e.Count = count
	e.Groups = make([]AssetsHoldingGroup, length)
}

func (e *ExtendedAssetHolding) Assign(idx int, group interface{}) {
	e.Groups[idx] = group.(AssetsHoldingGroup)
}

func (e *ExtendedAssetParam) Get(idx int) AbstractAssetGroup {
	return &(e.Groups[idx])
}

func (e *ExtendedAssetParam) Len() int {
	return len(e.Groups)
}

func (e *ExtendedAssetParam) Total() uint32 {
	return e.Count
}

func (e *ExtendedAssetParam) Reset(count uint32, length int) {
	e.Count = count
	e.Groups = make([]AssetsParamGroup, length)
}

func (e *ExtendedAssetParam) Assign(idx int, group interface{}) {
	e.Groups[idx] = group.(AssetsParamGroup)
}
