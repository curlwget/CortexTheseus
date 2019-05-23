// Code generated by github.com/fjl/gencodec. DO NOT EDIT.

package torrentfs

import (
	"encoding/json"

	"github.com/CortexFoundation/CortexTheseus/common"
)

// MarshalJSON marshals as JSON.
func (f FileMeta) MarshalJSON() ([]byte, error) {
	type FileMeta struct {
		AuthorAddr *common.Address
		URI        string
		RawSize    uint64
		BlockNum   uint64
	}
	var enc FileMeta
	enc.AuthorAddr = f.AuthorAddr
	enc.URI = f.URI
	enc.RawSize = f.RawSize
	enc.BlockNum = f.BlockNum
	return json.Marshal(&enc)
}

// UnmarshalJSON unmarshals from JSON.
func (f *FileMeta) UnmarshalJSON(input []byte) error {
	type FileMeta struct {
		AuthorAddr *common.Address
		URI        *string
		RawSize    *uint64
		BlockNum   *uint64
	}
	var dec FileMeta
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.AuthorAddr != nil {
		f.AuthorAddr = dec.AuthorAddr
	}
	if dec.URI != nil {
		f.URI = *dec.URI
	}
	if dec.RawSize != nil {
		f.RawSize = *dec.RawSize
	}
	if dec.BlockNum != nil {
		f.BlockNum = *dec.BlockNum
	}
	return nil
}
