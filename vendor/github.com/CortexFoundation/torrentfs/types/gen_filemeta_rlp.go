// Code generated by rlpgen. DO NOT EDIT.

//go:build !norlpgen
// +build !norlpgen

package types

import "github.com/CortexFoundation/CortexTheseus/rlp"
import "io"

func (obj *FileMeta) EncodeRLP(_w io.Writer) error {
	w := rlp.NewEncoderBuffer(_w)
	_tmp0 := w.List()
	w.WriteString(obj.InfoHash)
	w.WriteUint64(obj.RawSize)
	w.ListEnd(_tmp0)
	return w.Flush()
}
