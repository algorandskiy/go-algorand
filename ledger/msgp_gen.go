package ledger

// Code generated by github.com/algorand/msgp DO NOT EDIT.

import (
	"github.com/algorand/msgp/msgp"

	"github.com/algorand/go-algorand/ledger/encoded"
)

// The following msgp objects are implemented in this file:
// CatchpointCatchupState
//            |-----> MarshalMsg
//            |-----> CanMarshalMsg
//            |-----> (*) UnmarshalMsg
//            |-----> (*) CanUnmarshalMsg
//            |-----> Msgsize
//            |-----> MsgIsZero
//
// CatchpointFileHeader
//           |-----> (*) MarshalMsg
//           |-----> (*) CanMarshalMsg
//           |-----> (*) UnmarshalMsg
//           |-----> (*) CanUnmarshalMsg
//           |-----> (*) Msgsize
//           |-----> (*) MsgIsZero
//
// catchpointFileBalancesChunkV5
//               |-----> (*) MarshalMsg
//               |-----> (*) CanMarshalMsg
//               |-----> (*) UnmarshalMsg
//               |-----> (*) CanUnmarshalMsg
//               |-----> (*) Msgsize
//               |-----> (*) MsgIsZero
//
// catchpointFileChunkV6
//           |-----> (*) MarshalMsg
//           |-----> (*) CanMarshalMsg
//           |-----> (*) UnmarshalMsg
//           |-----> (*) CanUnmarshalMsg
//           |-----> (*) Msgsize
//           |-----> (*) MsgIsZero
//
// encodedBalanceRecordV5
//            |-----> (*) MarshalMsg
//            |-----> (*) CanMarshalMsg
//            |-----> (*) UnmarshalMsg
//            |-----> (*) CanUnmarshalMsg
//            |-----> (*) Msgsize
//            |-----> (*) MsgIsZero
//

// MarshalMsg implements msgp.Marshaler
func (z CatchpointCatchupState) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	o = msgp.AppendInt32(o, int32(z))
	return
}

func (_ CatchpointCatchupState) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(CatchpointCatchupState)
	if !ok {
		_, ok = (z).(*CatchpointCatchupState)
	}
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *CatchpointCatchupState) UnmarshalMsg(bts []byte) (o []byte, err error) {
	{
		var zb0001 int32
		zb0001, bts, err = msgp.ReadInt32Bytes(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		(*z) = CatchpointCatchupState(zb0001)
	}
	o = bts
	return
}

func (_ *CatchpointCatchupState) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*CatchpointCatchupState)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z CatchpointCatchupState) Msgsize() (s int) {
	s = msgp.Int32Size
	return
}

// MsgIsZero returns whether this is a zero value
func (z CatchpointCatchupState) MsgIsZero() bool {
	return z == 0
}

// MarshalMsg implements msgp.Marshaler
func (z *CatchpointFileHeader) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0001Len := uint32(9)
	var zb0001Mask uint16 /* 10 bits */
	if (*z).Totals.MsgIsZero() {
		zb0001Len--
		zb0001Mask |= 0x2
	}
	if (*z).TotalAccounts == 0 {
		zb0001Len--
		zb0001Mask |= 0x4
	}
	if (*z).BalancesRound.MsgIsZero() {
		zb0001Len--
		zb0001Mask |= 0x8
	}
	if (*z).BlockHeaderDigest.MsgIsZero() {
		zb0001Len--
		zb0001Mask |= 0x10
	}
	if (*z).BlocksRound.MsgIsZero() {
		zb0001Len--
		zb0001Mask |= 0x20
	}
	if (*z).Catchpoint == "" {
		zb0001Len--
		zb0001Mask |= 0x40
	}
	if (*z).TotalChunks == 0 {
		zb0001Len--
		zb0001Mask |= 0x80
	}
	if (*z).TotalKVs == 0 {
		zb0001Len--
		zb0001Mask |= 0x100
	}
	if (*z).Version == 0 {
		zb0001Len--
		zb0001Mask |= 0x200
	}
	// variable map header, size zb0001Len
	o = append(o, 0x80|uint8(zb0001Len))
	if zb0001Len != 0 {
		if (zb0001Mask & 0x2) == 0 { // if not empty
			// string "accountTotals"
			o = append(o, 0xad, 0x61, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x54, 0x6f, 0x74, 0x61, 0x6c, 0x73)
			o = (*z).Totals.MarshalMsg(o)
		}
		if (zb0001Mask & 0x4) == 0 { // if not empty
			// string "accountsCount"
			o = append(o, 0xad, 0x61, 0x63, 0x63, 0x6f, 0x75, 0x6e, 0x74, 0x73, 0x43, 0x6f, 0x75, 0x6e, 0x74)
			o = msgp.AppendUint64(o, (*z).TotalAccounts)
		}
		if (zb0001Mask & 0x8) == 0 { // if not empty
			// string "balancesRound"
			o = append(o, 0xad, 0x62, 0x61, 0x6c, 0x61, 0x6e, 0x63, 0x65, 0x73, 0x52, 0x6f, 0x75, 0x6e, 0x64)
			o = (*z).BalancesRound.MarshalMsg(o)
		}
		if (zb0001Mask & 0x10) == 0 { // if not empty
			// string "blockHeaderDigest"
			o = append(o, 0xb1, 0x62, 0x6c, 0x6f, 0x63, 0x6b, 0x48, 0x65, 0x61, 0x64, 0x65, 0x72, 0x44, 0x69, 0x67, 0x65, 0x73, 0x74)
			o = (*z).BlockHeaderDigest.MarshalMsg(o)
		}
		if (zb0001Mask & 0x20) == 0 { // if not empty
			// string "blocksRound"
			o = append(o, 0xab, 0x62, 0x6c, 0x6f, 0x63, 0x6b, 0x73, 0x52, 0x6f, 0x75, 0x6e, 0x64)
			o = (*z).BlocksRound.MarshalMsg(o)
		}
		if (zb0001Mask & 0x40) == 0 { // if not empty
			// string "catchpoint"
			o = append(o, 0xaa, 0x63, 0x61, 0x74, 0x63, 0x68, 0x70, 0x6f, 0x69, 0x6e, 0x74)
			o = msgp.AppendString(o, (*z).Catchpoint)
		}
		if (zb0001Mask & 0x80) == 0 { // if not empty
			// string "chunksCount"
			o = append(o, 0xab, 0x63, 0x68, 0x75, 0x6e, 0x6b, 0x73, 0x43, 0x6f, 0x75, 0x6e, 0x74)
			o = msgp.AppendUint64(o, (*z).TotalChunks)
		}
		if (zb0001Mask & 0x100) == 0 { // if not empty
			// string "kvsCount"
			o = append(o, 0xa8, 0x6b, 0x76, 0x73, 0x43, 0x6f, 0x75, 0x6e, 0x74)
			o = msgp.AppendUint64(o, (*z).TotalKVs)
		}
		if (zb0001Mask & 0x200) == 0 { // if not empty
			// string "version"
			o = append(o, 0xa7, 0x76, 0x65, 0x72, 0x73, 0x69, 0x6f, 0x6e)
			o = msgp.AppendUint64(o, (*z).Version)
		}
	}
	return
}

func (_ *CatchpointFileHeader) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*CatchpointFileHeader)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *CatchpointFileHeader) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zb0001 int
	var zb0002 bool
	zb0001, zb0002, bts, err = msgp.ReadMapHeaderBytes(bts)
	if _, ok := err.(msgp.TypeError); ok {
		zb0001, zb0002, bts, err = msgp.ReadArrayHeaderBytes(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0001 > 0 {
			zb0001--
			(*z).Version, bts, err = msgp.ReadUint64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Version")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			bts, err = (*z).BalancesRound.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "BalancesRound")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			bts, err = (*z).BlocksRound.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "BlocksRound")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			bts, err = (*z).Totals.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Totals")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			(*z).TotalAccounts, bts, err = msgp.ReadUint64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "TotalAccounts")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			(*z).TotalChunks, bts, err = msgp.ReadUint64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "TotalChunks")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			(*z).TotalKVs, bts, err = msgp.ReadUint64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "TotalKVs")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			(*z).Catchpoint, bts, err = msgp.ReadStringBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Catchpoint")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			bts, err = (*z).BlockHeaderDigest.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "BlockHeaderDigest")
				return
			}
		}
		if zb0001 > 0 {
			err = msgp.ErrTooManyArrayFields(zb0001)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array")
				return
			}
		}
	} else {
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0002 {
			(*z) = CatchpointFileHeader{}
		}
		for zb0001 > 0 {
			zb0001--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "version":
				(*z).Version, bts, err = msgp.ReadUint64Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Version")
					return
				}
			case "balancesRound":
				bts, err = (*z).BalancesRound.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "BalancesRound")
					return
				}
			case "blocksRound":
				bts, err = (*z).BlocksRound.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "BlocksRound")
					return
				}
			case "accountTotals":
				bts, err = (*z).Totals.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "Totals")
					return
				}
			case "accountsCount":
				(*z).TotalAccounts, bts, err = msgp.ReadUint64Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "TotalAccounts")
					return
				}
			case "chunksCount":
				(*z).TotalChunks, bts, err = msgp.ReadUint64Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "TotalChunks")
					return
				}
			case "kvsCount":
				(*z).TotalKVs, bts, err = msgp.ReadUint64Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "TotalKVs")
					return
				}
			case "catchpoint":
				(*z).Catchpoint, bts, err = msgp.ReadStringBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Catchpoint")
					return
				}
			case "blockHeaderDigest":
				bts, err = (*z).BlockHeaderDigest.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "BlockHeaderDigest")
					return
				}
			default:
				err = msgp.ErrNoField(string(field))
				if err != nil {
					err = msgp.WrapError(err)
					return
				}
			}
		}
	}
	o = bts
	return
}

func (_ *CatchpointFileHeader) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*CatchpointFileHeader)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *CatchpointFileHeader) Msgsize() (s int) {
	s = 1 + 8 + msgp.Uint64Size + 14 + (*z).BalancesRound.Msgsize() + 12 + (*z).BlocksRound.Msgsize() + 14 + (*z).Totals.Msgsize() + 14 + msgp.Uint64Size + 12 + msgp.Uint64Size + 9 + msgp.Uint64Size + 11 + msgp.StringPrefixSize + len((*z).Catchpoint) + 18 + (*z).BlockHeaderDigest.Msgsize()
	return
}

// MsgIsZero returns whether this is a zero value
func (z *CatchpointFileHeader) MsgIsZero() bool {
	return ((*z).Version == 0) && ((*z).BalancesRound.MsgIsZero()) && ((*z).BlocksRound.MsgIsZero()) && ((*z).Totals.MsgIsZero()) && ((*z).TotalAccounts == 0) && ((*z).TotalChunks == 0) && ((*z).TotalKVs == 0) && ((*z).Catchpoint == "") && ((*z).BlockHeaderDigest.MsgIsZero())
}

// MarshalMsg implements msgp.Marshaler
func (z *catchpointFileBalancesChunkV5) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0002Len := uint32(1)
	var zb0002Mask uint8 /* 2 bits */
	if len((*z).Balances) == 0 {
		zb0002Len--
		zb0002Mask |= 0x2
	}
	// variable map header, size zb0002Len
	o = append(o, 0x80|uint8(zb0002Len))
	if zb0002Len != 0 {
		if (zb0002Mask & 0x2) == 0 { // if not empty
			// string "bl"
			o = append(o, 0xa2, 0x62, 0x6c)
			if (*z).Balances == nil {
				o = msgp.AppendNil(o)
			} else {
				o = msgp.AppendArrayHeader(o, uint32(len((*z).Balances)))
			}
			for zb0001 := range (*z).Balances {
				// omitempty: check for empty values
				zb0003Len := uint32(2)
				var zb0003Mask uint8 /* 3 bits */
				if (*z).Balances[zb0001].AccountData.MsgIsZero() {
					zb0003Len--
					zb0003Mask |= 0x2
				}
				if (*z).Balances[zb0001].Address.MsgIsZero() {
					zb0003Len--
					zb0003Mask |= 0x4
				}
				// variable map header, size zb0003Len
				o = append(o, 0x80|uint8(zb0003Len))
				if (zb0003Mask & 0x2) == 0 { // if not empty
					// string "ad"
					o = append(o, 0xa2, 0x61, 0x64)
					o = (*z).Balances[zb0001].AccountData.MarshalMsg(o)
				}
				if (zb0003Mask & 0x4) == 0 { // if not empty
					// string "pk"
					o = append(o, 0xa2, 0x70, 0x6b)
					o = (*z).Balances[zb0001].Address.MarshalMsg(o)
				}
			}
		}
	}
	return
}

func (_ *catchpointFileBalancesChunkV5) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*catchpointFileBalancesChunkV5)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *catchpointFileBalancesChunkV5) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zb0002 int
	var zb0003 bool
	zb0002, zb0003, bts, err = msgp.ReadMapHeaderBytes(bts)
	if _, ok := err.(msgp.TypeError); ok {
		zb0002, zb0003, bts, err = msgp.ReadArrayHeaderBytes(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0002 > 0 {
			zb0002--
			var zb0004 int
			var zb0005 bool
			zb0004, zb0005, bts, err = msgp.ReadArrayHeaderBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Balances")
				return
			}
			if zb0004 > BalancesPerCatchpointFileChunk {
				err = msgp.ErrOverflow(uint64(zb0004), uint64(BalancesPerCatchpointFileChunk))
				err = msgp.WrapError(err, "struct-from-array", "Balances")
				return
			}
			if zb0005 {
				(*z).Balances = nil
			} else if (*z).Balances != nil && cap((*z).Balances) >= zb0004 {
				(*z).Balances = ((*z).Balances)[:zb0004]
			} else {
				(*z).Balances = make([]encodedBalanceRecordV5, zb0004)
			}
			for zb0001 := range (*z).Balances {
				var zb0006 int
				var zb0007 bool
				zb0006, zb0007, bts, err = msgp.ReadMapHeaderBytes(bts)
				if _, ok := err.(msgp.TypeError); ok {
					zb0006, zb0007, bts, err = msgp.ReadArrayHeaderBytes(bts)
					if err != nil {
						err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001)
						return
					}
					if zb0006 > 0 {
						zb0006--
						bts, err = (*z).Balances[zb0001].Address.UnmarshalMsg(bts)
						if err != nil {
							err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001, "struct-from-array", "Address")
							return
						}
					}
					if zb0006 > 0 {
						zb0006--
						bts, err = (*z).Balances[zb0001].AccountData.UnmarshalMsg(bts)
						if err != nil {
							err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001, "struct-from-array", "AccountData")
							return
						}
					}
					if zb0006 > 0 {
						err = msgp.ErrTooManyArrayFields(zb0006)
						if err != nil {
							err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001, "struct-from-array")
							return
						}
					}
				} else {
					if err != nil {
						err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001)
						return
					}
					if zb0007 {
						(*z).Balances[zb0001] = encodedBalanceRecordV5{}
					}
					for zb0006 > 0 {
						zb0006--
						field, bts, err = msgp.ReadMapKeyZC(bts)
						if err != nil {
							err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001)
							return
						}
						switch string(field) {
						case "pk":
							bts, err = (*z).Balances[zb0001].Address.UnmarshalMsg(bts)
							if err != nil {
								err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001, "Address")
								return
							}
						case "ad":
							bts, err = (*z).Balances[zb0001].AccountData.UnmarshalMsg(bts)
							if err != nil {
								err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001, "AccountData")
								return
							}
						default:
							err = msgp.ErrNoField(string(field))
							if err != nil {
								err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001)
								return
							}
						}
					}
				}
			}
		}
		if zb0002 > 0 {
			err = msgp.ErrTooManyArrayFields(zb0002)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array")
				return
			}
		}
	} else {
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0003 {
			(*z) = catchpointFileBalancesChunkV5{}
		}
		for zb0002 > 0 {
			zb0002--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "bl":
				var zb0008 int
				var zb0009 bool
				zb0008, zb0009, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Balances")
					return
				}
				if zb0008 > BalancesPerCatchpointFileChunk {
					err = msgp.ErrOverflow(uint64(zb0008), uint64(BalancesPerCatchpointFileChunk))
					err = msgp.WrapError(err, "Balances")
					return
				}
				if zb0009 {
					(*z).Balances = nil
				} else if (*z).Balances != nil && cap((*z).Balances) >= zb0008 {
					(*z).Balances = ((*z).Balances)[:zb0008]
				} else {
					(*z).Balances = make([]encodedBalanceRecordV5, zb0008)
				}
				for zb0001 := range (*z).Balances {
					var zb0010 int
					var zb0011 bool
					zb0010, zb0011, bts, err = msgp.ReadMapHeaderBytes(bts)
					if _, ok := err.(msgp.TypeError); ok {
						zb0010, zb0011, bts, err = msgp.ReadArrayHeaderBytes(bts)
						if err != nil {
							err = msgp.WrapError(err, "Balances", zb0001)
							return
						}
						if zb0010 > 0 {
							zb0010--
							bts, err = (*z).Balances[zb0001].Address.UnmarshalMsg(bts)
							if err != nil {
								err = msgp.WrapError(err, "Balances", zb0001, "struct-from-array", "Address")
								return
							}
						}
						if zb0010 > 0 {
							zb0010--
							bts, err = (*z).Balances[zb0001].AccountData.UnmarshalMsg(bts)
							if err != nil {
								err = msgp.WrapError(err, "Balances", zb0001, "struct-from-array", "AccountData")
								return
							}
						}
						if zb0010 > 0 {
							err = msgp.ErrTooManyArrayFields(zb0010)
							if err != nil {
								err = msgp.WrapError(err, "Balances", zb0001, "struct-from-array")
								return
							}
						}
					} else {
						if err != nil {
							err = msgp.WrapError(err, "Balances", zb0001)
							return
						}
						if zb0011 {
							(*z).Balances[zb0001] = encodedBalanceRecordV5{}
						}
						for zb0010 > 0 {
							zb0010--
							field, bts, err = msgp.ReadMapKeyZC(bts)
							if err != nil {
								err = msgp.WrapError(err, "Balances", zb0001)
								return
							}
							switch string(field) {
							case "pk":
								bts, err = (*z).Balances[zb0001].Address.UnmarshalMsg(bts)
								if err != nil {
									err = msgp.WrapError(err, "Balances", zb0001, "Address")
									return
								}
							case "ad":
								bts, err = (*z).Balances[zb0001].AccountData.UnmarshalMsg(bts)
								if err != nil {
									err = msgp.WrapError(err, "Balances", zb0001, "AccountData")
									return
								}
							default:
								err = msgp.ErrNoField(string(field))
								if err != nil {
									err = msgp.WrapError(err, "Balances", zb0001)
									return
								}
							}
						}
					}
				}
			default:
				err = msgp.ErrNoField(string(field))
				if err != nil {
					err = msgp.WrapError(err)
					return
				}
			}
		}
	}
	o = bts
	return
}

func (_ *catchpointFileBalancesChunkV5) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*catchpointFileBalancesChunkV5)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *catchpointFileBalancesChunkV5) Msgsize() (s int) {
	s = 1 + 3 + msgp.ArrayHeaderSize
	for zb0001 := range (*z).Balances {
		s += 1 + 3 + (*z).Balances[zb0001].Address.Msgsize() + 3 + (*z).Balances[zb0001].AccountData.Msgsize()
	}
	return
}

// MsgIsZero returns whether this is a zero value
func (z *catchpointFileBalancesChunkV5) MsgIsZero() bool {
	return (len((*z).Balances) == 0)
}

// MarshalMsg implements msgp.Marshaler
func (z *catchpointFileChunkV6) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0003Len := uint32(2)
	var zb0003Mask uint8 /* 4 bits */
	if len((*z).Balances) == 0 {
		zb0003Len--
		zb0003Mask |= 0x2
	}
	if len((*z).KVs) == 0 {
		zb0003Len--
		zb0003Mask |= 0x4
	}
	// variable map header, size zb0003Len
	o = append(o, 0x80|uint8(zb0003Len))
	if zb0003Len != 0 {
		if (zb0003Mask & 0x2) == 0 { // if not empty
			// string "bl"
			o = append(o, 0xa2, 0x62, 0x6c)
			if (*z).Balances == nil {
				o = msgp.AppendNil(o)
			} else {
				o = msgp.AppendArrayHeader(o, uint32(len((*z).Balances)))
			}
			for zb0001 := range (*z).Balances {
				o = (*z).Balances[zb0001].MarshalMsg(o)
			}
		}
		if (zb0003Mask & 0x4) == 0 { // if not empty
			// string "kv"
			o = append(o, 0xa2, 0x6b, 0x76)
			if (*z).KVs == nil {
				o = msgp.AppendNil(o)
			} else {
				o = msgp.AppendArrayHeader(o, uint32(len((*z).KVs)))
			}
			for zb0002 := range (*z).KVs {
				o = (*z).KVs[zb0002].MarshalMsg(o)
			}
		}
	}
	return
}

func (_ *catchpointFileChunkV6) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*catchpointFileChunkV6)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *catchpointFileChunkV6) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zb0003 int
	var zb0004 bool
	zb0003, zb0004, bts, err = msgp.ReadMapHeaderBytes(bts)
	if _, ok := err.(msgp.TypeError); ok {
		zb0003, zb0004, bts, err = msgp.ReadArrayHeaderBytes(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0003 > 0 {
			zb0003--
			var zb0005 int
			var zb0006 bool
			zb0005, zb0006, bts, err = msgp.ReadArrayHeaderBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Balances")
				return
			}
			if zb0005 > BalancesPerCatchpointFileChunk {
				err = msgp.ErrOverflow(uint64(zb0005), uint64(BalancesPerCatchpointFileChunk))
				err = msgp.WrapError(err, "struct-from-array", "Balances")
				return
			}
			if zb0006 {
				(*z).Balances = nil
			} else if (*z).Balances != nil && cap((*z).Balances) >= zb0005 {
				(*z).Balances = ((*z).Balances)[:zb0005]
			} else {
				(*z).Balances = make([]encoded.BalanceRecordV6, zb0005)
			}
			for zb0001 := range (*z).Balances {
				bts, err = (*z).Balances[zb0001].UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "struct-from-array", "Balances", zb0001)
					return
				}
			}
		}
		if zb0003 > 0 {
			zb0003--
			var zb0007 int
			var zb0008 bool
			zb0007, zb0008, bts, err = msgp.ReadArrayHeaderBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "KVs")
				return
			}
			if zb0007 > BalancesPerCatchpointFileChunk {
				err = msgp.ErrOverflow(uint64(zb0007), uint64(BalancesPerCatchpointFileChunk))
				err = msgp.WrapError(err, "struct-from-array", "KVs")
				return
			}
			if zb0008 {
				(*z).KVs = nil
			} else if (*z).KVs != nil && cap((*z).KVs) >= zb0007 {
				(*z).KVs = ((*z).KVs)[:zb0007]
			} else {
				(*z).KVs = make([]encoded.KVRecordV6, zb0007)
			}
			for zb0002 := range (*z).KVs {
				bts, err = (*z).KVs[zb0002].UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "struct-from-array", "KVs", zb0002)
					return
				}
			}
		}
		if zb0003 > 0 {
			err = msgp.ErrTooManyArrayFields(zb0003)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array")
				return
			}
		}
	} else {
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0004 {
			(*z) = catchpointFileChunkV6{}
		}
		for zb0003 > 0 {
			zb0003--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "bl":
				var zb0009 int
				var zb0010 bool
				zb0009, zb0010, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Balances")
					return
				}
				if zb0009 > BalancesPerCatchpointFileChunk {
					err = msgp.ErrOverflow(uint64(zb0009), uint64(BalancesPerCatchpointFileChunk))
					err = msgp.WrapError(err, "Balances")
					return
				}
				if zb0010 {
					(*z).Balances = nil
				} else if (*z).Balances != nil && cap((*z).Balances) >= zb0009 {
					(*z).Balances = ((*z).Balances)[:zb0009]
				} else {
					(*z).Balances = make([]encoded.BalanceRecordV6, zb0009)
				}
				for zb0001 := range (*z).Balances {
					bts, err = (*z).Balances[zb0001].UnmarshalMsg(bts)
					if err != nil {
						err = msgp.WrapError(err, "Balances", zb0001)
						return
					}
				}
			case "kv":
				var zb0011 int
				var zb0012 bool
				zb0011, zb0012, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "KVs")
					return
				}
				if zb0011 > BalancesPerCatchpointFileChunk {
					err = msgp.ErrOverflow(uint64(zb0011), uint64(BalancesPerCatchpointFileChunk))
					err = msgp.WrapError(err, "KVs")
					return
				}
				if zb0012 {
					(*z).KVs = nil
				} else if (*z).KVs != nil && cap((*z).KVs) >= zb0011 {
					(*z).KVs = ((*z).KVs)[:zb0011]
				} else {
					(*z).KVs = make([]encoded.KVRecordV6, zb0011)
				}
				for zb0002 := range (*z).KVs {
					bts, err = (*z).KVs[zb0002].UnmarshalMsg(bts)
					if err != nil {
						err = msgp.WrapError(err, "KVs", zb0002)
						return
					}
				}
			default:
				err = msgp.ErrNoField(string(field))
				if err != nil {
					err = msgp.WrapError(err)
					return
				}
			}
		}
	}
	o = bts
	return
}

func (_ *catchpointFileChunkV6) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*catchpointFileChunkV6)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *catchpointFileChunkV6) Msgsize() (s int) {
	s = 1 + 3 + msgp.ArrayHeaderSize
	for zb0001 := range (*z).Balances {
		s += (*z).Balances[zb0001].Msgsize()
	}
	s += 3 + msgp.ArrayHeaderSize
	for zb0002 := range (*z).KVs {
		s += (*z).KVs[zb0002].Msgsize()
	}
	return
}

// MsgIsZero returns whether this is a zero value
func (z *catchpointFileChunkV6) MsgIsZero() bool {
	return (len((*z).Balances) == 0) && (len((*z).KVs) == 0)
}

// MarshalMsg implements msgp.Marshaler
func (z *encodedBalanceRecordV5) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0001Len := uint32(2)
	var zb0001Mask uint8 /* 3 bits */
	if (*z).AccountData.MsgIsZero() {
		zb0001Len--
		zb0001Mask |= 0x2
	}
	if (*z).Address.MsgIsZero() {
		zb0001Len--
		zb0001Mask |= 0x4
	}
	// variable map header, size zb0001Len
	o = append(o, 0x80|uint8(zb0001Len))
	if zb0001Len != 0 {
		if (zb0001Mask & 0x2) == 0 { // if not empty
			// string "ad"
			o = append(o, 0xa2, 0x61, 0x64)
			o = (*z).AccountData.MarshalMsg(o)
		}
		if (zb0001Mask & 0x4) == 0 { // if not empty
			// string "pk"
			o = append(o, 0xa2, 0x70, 0x6b)
			o = (*z).Address.MarshalMsg(o)
		}
	}
	return
}

func (_ *encodedBalanceRecordV5) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*encodedBalanceRecordV5)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *encodedBalanceRecordV5) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var field []byte
	_ = field
	var zb0001 int
	var zb0002 bool
	zb0001, zb0002, bts, err = msgp.ReadMapHeaderBytes(bts)
	if _, ok := err.(msgp.TypeError); ok {
		zb0001, zb0002, bts, err = msgp.ReadArrayHeaderBytes(bts)
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0001 > 0 {
			zb0001--
			bts, err = (*z).Address.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Address")
				return
			}
		}
		if zb0001 > 0 {
			zb0001--
			bts, err = (*z).AccountData.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "AccountData")
				return
			}
		}
		if zb0001 > 0 {
			err = msgp.ErrTooManyArrayFields(zb0001)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array")
				return
			}
		}
	} else {
		if err != nil {
			err = msgp.WrapError(err)
			return
		}
		if zb0002 {
			(*z) = encodedBalanceRecordV5{}
		}
		for zb0001 > 0 {
			zb0001--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "pk":
				bts, err = (*z).Address.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "Address")
					return
				}
			case "ad":
				bts, err = (*z).AccountData.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "AccountData")
					return
				}
			default:
				err = msgp.ErrNoField(string(field))
				if err != nil {
					err = msgp.WrapError(err)
					return
				}
			}
		}
	}
	o = bts
	return
}

func (_ *encodedBalanceRecordV5) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*encodedBalanceRecordV5)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *encodedBalanceRecordV5) Msgsize() (s int) {
	s = 1 + 3 + (*z).Address.Msgsize() + 3 + (*z).AccountData.Msgsize()
	return
}

// MsgIsZero returns whether this is a zero value
func (z *encodedBalanceRecordV5) MsgIsZero() bool {
	return ((*z).Address.MsgIsZero()) && ((*z).AccountData.MsgIsZero())
}
