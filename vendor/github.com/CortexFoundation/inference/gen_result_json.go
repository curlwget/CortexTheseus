// Code generated by github.com/fjl/gencodec. DO NOT EDIT.

package inference

import (
	"encoding/json"
	"errors"

	"github.com/CortexFoundation/CortexTheseus/common/hexutil"
)

// MarshalJSON marshals as JSON.
func (i InferResult) MarshalJSON() ([]byte, error) {
	type InferResult struct {
		Data hexutil.Bytes `json:"data" gencodec:"required"`
		Info string        `json:"info" gencodec:"required"`
	}
	var enc InferResult
	enc.Data = i.Data
	enc.Info = i.Info
	return json.Marshal(&enc)
}

// UnmarshalJSON unmarshals from JSON.
func (i *InferResult) UnmarshalJSON(input []byte) error {
	type InferResult struct {
		Data *hexutil.Bytes `json:"data" gencodec:"required"`
		Info *string        `json:"info" gencodec:"required"`
	}
	var dec InferResult
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.Data == nil {
		return errors.New("missing required field 'data' for InferResult")
	}
	i.Data = *dec.Data
	if dec.Info == nil {
		return errors.New("missing required field 'info' for InferResult")
	}
	i.Info = *dec.Info
	return nil
}
