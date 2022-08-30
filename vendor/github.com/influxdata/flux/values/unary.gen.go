// Generated by tmpl
// https://github.com/benbjohnson/tmpl
//
// DO NOT EDIT!
// Source: unary.gen.go.tmpl

package values

import (
	"github.com/apache/arrow/go/v7/arrow/memory"
	fluxarray "github.com/influxdata/flux/array"
	"github.com/influxdata/flux/codes"
	"github.com/influxdata/flux/internal/errors"
	"github.com/influxdata/flux/semantic"
)

func VectorUnarySub(v Vector, mem memory.Allocator) (Value, error) {
	elemType := v.ElementType()
	switch elemType.Nature() {

	case semantic.Int:

		var (
			x   *fluxarray.Int
			err error
		)
		x, err = fluxarray.IntUnarySub(v.Arr().(*fluxarray.Int), mem)
		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicInt), nil

	case semantic.Float:

		var (
			x   *fluxarray.Float
			err error
		)
		x, err = fluxarray.FloatUnarySub(v.Arr().(*fluxarray.Float), mem)
		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicFloat), nil

	default:
		return nil, errors.Newf(codes.Invalid, "unsupported type for vector UnarySub: %v", elemType)
	}
}

func VectorExists(v Vector, mem memory.Allocator) (Value, error) {
	elemType := v.ElementType()
	switch elemType.Nature() {

	case semantic.Int:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.IntExists(v.Arr().(*fluxarray.Int), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	case semantic.UInt:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.UintExists(v.Arr().(*fluxarray.Uint), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	case semantic.Float:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.FloatExists(v.Arr().(*fluxarray.Float), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	case semantic.String:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.StringExists(v.Arr().(*fluxarray.String), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	case semantic.Bool:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.BooleanExists(v.Arr().(*fluxarray.Boolean), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	case semantic.Time:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.IntExists(v.Arr().(*fluxarray.Int), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	default:
		return nil, errors.Newf(codes.Invalid, "unsupported type for vector Exists: %v", elemType)
	}
}

func VectorNot(v Vector, mem memory.Allocator) (Value, error) {
	elemType := v.ElementType()
	switch elemType.Nature() {

	case semantic.Bool:

		var (
			x   *fluxarray.Boolean
			err error
		)

		x, err = fluxarray.BooleanNot(v.Arr().(*fluxarray.Boolean), mem)

		if err != nil {
			return nil, err
		}
		return NewVectorValue(x, semantic.BasicBool), nil

	default:
		return nil, errors.Newf(codes.Invalid, "unsupported type for vector Not: %v", elemType)
	}
}
