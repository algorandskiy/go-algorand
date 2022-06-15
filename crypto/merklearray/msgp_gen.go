package merklearray

// Code generated by github.com/algorand/msgp DO NOT EDIT.

import (
	"github.com/algorand/go-algorand/crypto"
	"github.com/algorand/msgp/msgp"
)

// The following msgp objects are implemented in this file:
// Layer
//   |-----> MarshalMsg
//   |-----> CanMarshalMsg
//   |-----> (*) UnmarshalMsg
//   |-----> (*) CanUnmarshalMsg
//   |-----> Msgsize
//   |-----> MsgIsZero
//
// Proof
//   |-----> (*) MarshalMsg
//   |-----> (*) CanMarshalMsg
//   |-----> (*) UnmarshalMsg
//   |-----> (*) CanUnmarshalMsg
//   |-----> (*) Msgsize
//   |-----> (*) MsgIsZero
//
// SingleLeafProof
//        |-----> (*) MarshalMsg
//        |-----> (*) CanMarshalMsg
//        |-----> (*) UnmarshalMsg
//        |-----> (*) CanUnmarshalMsg
//        |-----> (*) Msgsize
//        |-----> (*) MsgIsZero
//
// Tree
//   |-----> (*) MarshalMsg
//   |-----> (*) CanMarshalMsg
//   |-----> (*) UnmarshalMsg
//   |-----> (*) CanUnmarshalMsg
//   |-----> (*) Msgsize
//   |-----> (*) MsgIsZero
//

// MarshalMsg implements msgp.Marshaler
func (z Layer) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	if z == nil {
		o = msgp.AppendNil(o)
	} else {
		o = msgp.AppendArrayHeader(o, uint32(len(z)))
	}
	for za0001 := range z {
		o = z[za0001].MarshalMsg(o)
	}
	return
}

func (_ Layer) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(Layer)
	if !ok {
		_, ok = (z).(*Layer)
	}
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *Layer) UnmarshalMsg(bts []byte) (o []byte, err error) {
	var zb0002 int
	var zb0003 bool
	zb0002, zb0003, bts, err = msgp.ReadArrayHeaderBytes(bts)
	if err != nil {
		err = msgp.WrapError(err)
		return
	}
	if zb0002 > MaxNumLeavesOnEncodedTree {
		err = msgp.ErrOverflow(uint64(zb0002), uint64(MaxNumLeavesOnEncodedTree))
		err = msgp.WrapError(err)
		return
	}
	if zb0003 {
		(*z) = nil
	} else if (*z) != nil && cap((*z)) >= zb0002 {
		(*z) = (*z)[:zb0002]
	} else {
		(*z) = make(Layer, zb0002)
	}
	for zb0001 := range *z {
		bts, err = (*z)[zb0001].UnmarshalMsg(bts)
		if err != nil {
			err = msgp.WrapError(err, zb0001)
			return
		}
	}
	o = bts
	return
}

func (_ *Layer) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*Layer)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z Layer) Msgsize() (s int) {
	s = msgp.ArrayHeaderSize
	for za0001 := range z {
		s += z[za0001].Msgsize()
	}
	return
}

// MsgIsZero returns whether this is a zero value
func (z Layer) MsgIsZero() bool {
	return len(z) == 0
}

// MarshalMsg implements msgp.Marshaler
func (z *Proof) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0002Len := uint32(3)
	var zb0002Mask uint8 /* 4 bits */
	if (*z).HashFactory.MsgIsZero() {
		zb0002Len--
		zb0002Mask |= 0x2
	}
	if len((*z).Path) == 0 {
		zb0002Len--
		zb0002Mask |= 0x4
	}
	if (*z).TreeDepth == 0 {
		zb0002Len--
		zb0002Mask |= 0x8
	}
	// variable map header, size zb0002Len
	o = append(o, 0x80|uint8(zb0002Len))
	if zb0002Len != 0 {
		if (zb0002Mask & 0x2) == 0 { // if not empty
			// string "hsh"
			o = append(o, 0xa3, 0x68, 0x73, 0x68)
			o = (*z).HashFactory.MarshalMsg(o)
		}
		if (zb0002Mask & 0x4) == 0 { // if not empty
			// string "pth"
			o = append(o, 0xa3, 0x70, 0x74, 0x68)
			if (*z).Path == nil {
				o = msgp.AppendNil(o)
			} else {
				o = msgp.AppendArrayHeader(o, uint32(len((*z).Path)))
			}
			for zb0001 := range (*z).Path {
				o = (*z).Path[zb0001].MarshalMsg(o)
			}
		}
		if (zb0002Mask & 0x8) == 0 { // if not empty
			// string "td"
			o = append(o, 0xa2, 0x74, 0x64)
			o = msgp.AppendUint8(o, (*z).TreeDepth)
		}
	}
	return
}

func (_ *Proof) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*Proof)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *Proof) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
				err = msgp.WrapError(err, "struct-from-array", "Path")
				return
			}
			if zb0004 > MaxNumLeavesOnEncodedTree/2 {
				err = msgp.ErrOverflow(uint64(zb0004), uint64(MaxNumLeavesOnEncodedTree/2))
				err = msgp.WrapError(err, "struct-from-array", "Path")
				return
			}
			if zb0005 {
				(*z).Path = nil
			} else if (*z).Path != nil && cap((*z).Path) >= zb0004 {
				(*z).Path = ((*z).Path)[:zb0004]
			} else {
				(*z).Path = make([]crypto.GenericDigest, zb0004)
			}
			for zb0001 := range (*z).Path {
				bts, err = (*z).Path[zb0001].UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "struct-from-array", "Path", zb0001)
					return
				}
			}
		}
		if zb0002 > 0 {
			zb0002--
			bts, err = (*z).HashFactory.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "HashFactory")
				return
			}
		}
		if zb0002 > 0 {
			zb0002--
			(*z).TreeDepth, bts, err = msgp.ReadUint8Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "TreeDepth")
				return
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
			(*z) = Proof{}
		}
		for zb0002 > 0 {
			zb0002--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "pth":
				var zb0006 int
				var zb0007 bool
				zb0006, zb0007, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Path")
					return
				}
				if zb0006 > MaxNumLeavesOnEncodedTree/2 {
					err = msgp.ErrOverflow(uint64(zb0006), uint64(MaxNumLeavesOnEncodedTree/2))
					err = msgp.WrapError(err, "Path")
					return
				}
				if zb0007 {
					(*z).Path = nil
				} else if (*z).Path != nil && cap((*z).Path) >= zb0006 {
					(*z).Path = ((*z).Path)[:zb0006]
				} else {
					(*z).Path = make([]crypto.GenericDigest, zb0006)
				}
				for zb0001 := range (*z).Path {
					bts, err = (*z).Path[zb0001].UnmarshalMsg(bts)
					if err != nil {
						err = msgp.WrapError(err, "Path", zb0001)
						return
					}
				}
			case "hsh":
				bts, err = (*z).HashFactory.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "HashFactory")
					return
				}
			case "td":
				(*z).TreeDepth, bts, err = msgp.ReadUint8Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "TreeDepth")
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

func (_ *Proof) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*Proof)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *Proof) Msgsize() (s int) {
	s = 1 + 4 + msgp.ArrayHeaderSize
	for zb0001 := range (*z).Path {
		s += (*z).Path[zb0001].Msgsize()
	}
	s += 4 + (*z).HashFactory.Msgsize() + 3 + msgp.Uint8Size
	return
}

// MsgIsZero returns whether this is a zero value
func (z *Proof) MsgIsZero() bool {
	return (len((*z).Path) == 0) && ((*z).HashFactory.MsgIsZero()) && ((*z).TreeDepth == 0)
}

// MarshalMsg implements msgp.Marshaler
func (z *SingleLeafProof) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0002Len := uint32(3)
	var zb0002Mask uint8 /* 5 bits */
	if (*z).Proof.HashFactory.MsgIsZero() {
		zb0002Len--
		zb0002Mask |= 0x4
	}
	if len((*z).Proof.Path) == 0 {
		zb0002Len--
		zb0002Mask |= 0x8
	}
	if (*z).Proof.TreeDepth == 0 {
		zb0002Len--
		zb0002Mask |= 0x10
	}
	// variable map header, size zb0002Len
	o = append(o, 0x80|uint8(zb0002Len))
	if zb0002Len != 0 {
		if (zb0002Mask & 0x4) == 0 { // if not empty
			// string "hsh"
			o = append(o, 0xa3, 0x68, 0x73, 0x68)
			o = (*z).Proof.HashFactory.MarshalMsg(o)
		}
		if (zb0002Mask & 0x8) == 0 { // if not empty
			// string "pth"
			o = append(o, 0xa3, 0x70, 0x74, 0x68)
			if (*z).Proof.Path == nil {
				o = msgp.AppendNil(o)
			} else {
				o = msgp.AppendArrayHeader(o, uint32(len((*z).Proof.Path)))
			}
			for zb0001 := range (*z).Proof.Path {
				o = (*z).Proof.Path[zb0001].MarshalMsg(o)
			}
		}
		if (zb0002Mask & 0x10) == 0 { // if not empty
			// string "td"
			o = append(o, 0xa2, 0x74, 0x64)
			o = msgp.AppendUint8(o, (*z).Proof.TreeDepth)
		}
	}
	return
}

func (_ *SingleLeafProof) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*SingleLeafProof)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *SingleLeafProof) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
				err = msgp.WrapError(err, "struct-from-array", "Path")
				return
			}
			if zb0004 > MaxNumLeavesOnEncodedTree/2 {
				err = msgp.ErrOverflow(uint64(zb0004), uint64(MaxNumLeavesOnEncodedTree/2))
				err = msgp.WrapError(err, "struct-from-array", "Path")
				return
			}
			if zb0005 {
				(*z).Proof.Path = nil
			} else if (*z).Proof.Path != nil && cap((*z).Proof.Path) >= zb0004 {
				(*z).Proof.Path = ((*z).Proof.Path)[:zb0004]
			} else {
				(*z).Proof.Path = make([]crypto.GenericDigest, zb0004)
			}
			for zb0001 := range (*z).Proof.Path {
				bts, err = (*z).Proof.Path[zb0001].UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "struct-from-array", "Path", zb0001)
					return
				}
			}
		}
		if zb0002 > 0 {
			zb0002--
			bts, err = (*z).Proof.HashFactory.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "HashFactory")
				return
			}
		}
		if zb0002 > 0 {
			zb0002--
			(*z).Proof.TreeDepth, bts, err = msgp.ReadUint8Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "TreeDepth")
				return
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
			(*z) = SingleLeafProof{}
		}
		for zb0002 > 0 {
			zb0002--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "pth":
				var zb0006 int
				var zb0007 bool
				zb0006, zb0007, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Path")
					return
				}
				if zb0006 > MaxNumLeavesOnEncodedTree/2 {
					err = msgp.ErrOverflow(uint64(zb0006), uint64(MaxNumLeavesOnEncodedTree/2))
					err = msgp.WrapError(err, "Path")
					return
				}
				if zb0007 {
					(*z).Proof.Path = nil
				} else if (*z).Proof.Path != nil && cap((*z).Proof.Path) >= zb0006 {
					(*z).Proof.Path = ((*z).Proof.Path)[:zb0006]
				} else {
					(*z).Proof.Path = make([]crypto.GenericDigest, zb0006)
				}
				for zb0001 := range (*z).Proof.Path {
					bts, err = (*z).Proof.Path[zb0001].UnmarshalMsg(bts)
					if err != nil {
						err = msgp.WrapError(err, "Path", zb0001)
						return
					}
				}
			case "hsh":
				bts, err = (*z).Proof.HashFactory.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "HashFactory")
					return
				}
			case "td":
				(*z).Proof.TreeDepth, bts, err = msgp.ReadUint8Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "TreeDepth")
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

func (_ *SingleLeafProof) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*SingleLeafProof)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *SingleLeafProof) Msgsize() (s int) {
	s = 1 + 4 + msgp.ArrayHeaderSize
	for zb0001 := range (*z).Proof.Path {
		s += (*z).Proof.Path[zb0001].Msgsize()
	}
	s += 4 + (*z).Proof.HashFactory.Msgsize() + 3 + msgp.Uint8Size
	return
}

// MsgIsZero returns whether this is a zero value
func (z *SingleLeafProof) MsgIsZero() bool {
	return (len((*z).Proof.Path) == 0) && ((*z).Proof.HashFactory.MsgIsZero()) && ((*z).Proof.TreeDepth == 0)
}

// MarshalMsg implements msgp.Marshaler
func (z *Tree) MarshalMsg(b []byte) (o []byte) {
	o = msgp.Require(b, z.Msgsize())
	// omitempty: check for empty values
	zb0003Len := uint32(4)
	var zb0003Mask uint8 /* 5 bits */
	if (*z).Hash.MsgIsZero() {
		zb0003Len--
		zb0003Mask |= 0x2
	}
	if len((*z).Levels) == 0 {
		zb0003Len--
		zb0003Mask |= 0x4
	}
	if (*z).NumOfElements == 0 {
		zb0003Len--
		zb0003Mask |= 0x8
	}
	if (*z).IsVectorCommitment == false {
		zb0003Len--
		zb0003Mask |= 0x10
	}
	// variable map header, size zb0003Len
	o = append(o, 0x80|uint8(zb0003Len))
	if zb0003Len != 0 {
		if (zb0003Mask & 0x2) == 0 { // if not empty
			// string "hsh"
			o = append(o, 0xa3, 0x68, 0x73, 0x68)
			o = (*z).Hash.MarshalMsg(o)
		}
		if (zb0003Mask & 0x4) == 0 { // if not empty
			// string "lvls"
			o = append(o, 0xa4, 0x6c, 0x76, 0x6c, 0x73)
			if (*z).Levels == nil {
				o = msgp.AppendNil(o)
			} else {
				o = msgp.AppendArrayHeader(o, uint32(len((*z).Levels)))
			}
			for zb0001 := range (*z).Levels {
				if (*z).Levels[zb0001] == nil {
					o = msgp.AppendNil(o)
				} else {
					o = msgp.AppendArrayHeader(o, uint32(len((*z).Levels[zb0001])))
				}
				for zb0002 := range (*z).Levels[zb0001] {
					o = (*z).Levels[zb0001][zb0002].MarshalMsg(o)
				}
			}
		}
		if (zb0003Mask & 0x8) == 0 { // if not empty
			// string "nl"
			o = append(o, 0xa2, 0x6e, 0x6c)
			o = msgp.AppendUint64(o, (*z).NumOfElements)
		}
		if (zb0003Mask & 0x10) == 0 { // if not empty
			// string "vc"
			o = append(o, 0xa2, 0x76, 0x63)
			o = msgp.AppendBool(o, (*z).IsVectorCommitment)
		}
	}
	return
}

func (_ *Tree) CanMarshalMsg(z interface{}) bool {
	_, ok := (z).(*Tree)
	return ok
}

// UnmarshalMsg implements msgp.Unmarshaler
func (z *Tree) UnmarshalMsg(bts []byte) (o []byte, err error) {
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
				err = msgp.WrapError(err, "struct-from-array", "Levels")
				return
			}
			if zb0005 > MaxEncodedTreeDepth+1 {
				err = msgp.ErrOverflow(uint64(zb0005), uint64(MaxEncodedTreeDepth+1))
				err = msgp.WrapError(err, "struct-from-array", "Levels")
				return
			}
			if zb0006 {
				(*z).Levels = nil
			} else if (*z).Levels != nil && cap((*z).Levels) >= zb0005 {
				(*z).Levels = ((*z).Levels)[:zb0005]
			} else {
				(*z).Levels = make([]Layer, zb0005)
			}
			for zb0001 := range (*z).Levels {
				var zb0007 int
				var zb0008 bool
				zb0007, zb0008, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "struct-from-array", "Levels", zb0001)
					return
				}
				if zb0007 > MaxNumLeavesOnEncodedTree {
					err = msgp.ErrOverflow(uint64(zb0007), uint64(MaxNumLeavesOnEncodedTree))
					err = msgp.WrapError(err, "struct-from-array", "Levels", zb0001)
					return
				}
				if zb0008 {
					(*z).Levels[zb0001] = nil
				} else if (*z).Levels[zb0001] != nil && cap((*z).Levels[zb0001]) >= zb0007 {
					(*z).Levels[zb0001] = ((*z).Levels[zb0001])[:zb0007]
				} else {
					(*z).Levels[zb0001] = make(Layer, zb0007)
				}
				for zb0002 := range (*z).Levels[zb0001] {
					bts, err = (*z).Levels[zb0001][zb0002].UnmarshalMsg(bts)
					if err != nil {
						err = msgp.WrapError(err, "struct-from-array", "Levels", zb0001, zb0002)
						return
					}
				}
			}
		}
		if zb0003 > 0 {
			zb0003--
			(*z).NumOfElements, bts, err = msgp.ReadUint64Bytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "NumOfElements")
				return
			}
		}
		if zb0003 > 0 {
			zb0003--
			bts, err = (*z).Hash.UnmarshalMsg(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "Hash")
				return
			}
		}
		if zb0003 > 0 {
			zb0003--
			(*z).IsVectorCommitment, bts, err = msgp.ReadBoolBytes(bts)
			if err != nil {
				err = msgp.WrapError(err, "struct-from-array", "IsVectorCommitment")
				return
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
			(*z) = Tree{}
		}
		for zb0003 > 0 {
			zb0003--
			field, bts, err = msgp.ReadMapKeyZC(bts)
			if err != nil {
				err = msgp.WrapError(err)
				return
			}
			switch string(field) {
			case "lvls":
				var zb0009 int
				var zb0010 bool
				zb0009, zb0010, bts, err = msgp.ReadArrayHeaderBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "Levels")
					return
				}
				if zb0009 > MaxEncodedTreeDepth+1 {
					err = msgp.ErrOverflow(uint64(zb0009), uint64(MaxEncodedTreeDepth+1))
					err = msgp.WrapError(err, "Levels")
					return
				}
				if zb0010 {
					(*z).Levels = nil
				} else if (*z).Levels != nil && cap((*z).Levels) >= zb0009 {
					(*z).Levels = ((*z).Levels)[:zb0009]
				} else {
					(*z).Levels = make([]Layer, zb0009)
				}
				for zb0001 := range (*z).Levels {
					var zb0011 int
					var zb0012 bool
					zb0011, zb0012, bts, err = msgp.ReadArrayHeaderBytes(bts)
					if err != nil {
						err = msgp.WrapError(err, "Levels", zb0001)
						return
					}
					if zb0011 > MaxNumLeavesOnEncodedTree {
						err = msgp.ErrOverflow(uint64(zb0011), uint64(MaxNumLeavesOnEncodedTree))
						err = msgp.WrapError(err, "Levels", zb0001)
						return
					}
					if zb0012 {
						(*z).Levels[zb0001] = nil
					} else if (*z).Levels[zb0001] != nil && cap((*z).Levels[zb0001]) >= zb0011 {
						(*z).Levels[zb0001] = ((*z).Levels[zb0001])[:zb0011]
					} else {
						(*z).Levels[zb0001] = make(Layer, zb0011)
					}
					for zb0002 := range (*z).Levels[zb0001] {
						bts, err = (*z).Levels[zb0001][zb0002].UnmarshalMsg(bts)
						if err != nil {
							err = msgp.WrapError(err, "Levels", zb0001, zb0002)
							return
						}
					}
				}
			case "nl":
				(*z).NumOfElements, bts, err = msgp.ReadUint64Bytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "NumOfElements")
					return
				}
			case "hsh":
				bts, err = (*z).Hash.UnmarshalMsg(bts)
				if err != nil {
					err = msgp.WrapError(err, "Hash")
					return
				}
			case "vc":
				(*z).IsVectorCommitment, bts, err = msgp.ReadBoolBytes(bts)
				if err != nil {
					err = msgp.WrapError(err, "IsVectorCommitment")
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

func (_ *Tree) CanUnmarshalMsg(z interface{}) bool {
	_, ok := (z).(*Tree)
	return ok
}

// Msgsize returns an upper bound estimate of the number of bytes occupied by the serialized message
func (z *Tree) Msgsize() (s int) {
	s = 1 + 5 + msgp.ArrayHeaderSize
	for zb0001 := range (*z).Levels {
		s += msgp.ArrayHeaderSize
		for zb0002 := range (*z).Levels[zb0001] {
			s += (*z).Levels[zb0001][zb0002].Msgsize()
		}
	}
	s += 3 + msgp.Uint64Size + 4 + (*z).Hash.Msgsize() + 3 + msgp.BoolSize
	return
}

// MsgIsZero returns whether this is a zero value
func (z *Tree) MsgIsZero() bool {
	return (len((*z).Levels) == 0) && ((*z).NumOfElements == 0) && ((*z).Hash.MsgIsZero()) && ((*z).IsVectorCommitment == false)
}
