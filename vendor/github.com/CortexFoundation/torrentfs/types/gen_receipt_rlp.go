// Code generated by rlpgen. DO NOT EDIT.

//go:build !norlpgen
// +build !norlpgen

package types

import "github.com/CortexFoundation/CortexTheseus/rlp"
import "io"

func (obj *Receipt) EncodeRLP(_w io.Writer) error {
	w := rlp.NewEncoderBuffer(_w)
	_tmp0 := w.List()
	if obj.ContractAddr == nil {
		w.Write([]byte{0x80})
	} else {
		w.WriteBytes(obj.ContractAddr[:])
	}
	w.WriteUint64(obj.GasUsed)
	w.WriteUint64(obj.Status)
	w.ListEnd(_tmp0)
	return w.Flush()
}
