// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package bsoncodec

import (
	"fmt"
	"reflect"

	"github.com/stlimtat/mongo-go-driver/bson/bsonoptions"
	"github.com/stlimtat/mongo-go-driver/bson/bsonrw"
	"github.com/stlimtat/mongo-go-driver/bson/bsontype"
)

// StringCodec is the Codec used for struct values.
type StringCodec struct {
	DecodeObjectIDAsHex bool
}

var (
	defaultStringCodec = NewStringCodec()

	_ ValueCodec  = defaultStringCodec
	_ typeDecoder = defaultStringCodec
)

// NewStringCodec returns a StringCodec with options opts.
func NewStringCodec(opts ...*bsonoptions.StringCodecOptions) *StringCodec {
	stringOpt := bsonoptions.MergeStringCodecOptions(opts...)
	return &StringCodec{*stringOpt.DecodeObjectIDAsHex}
}

// EncodeValue is the ValueEncoder for string types.
func (sc *StringCodec) EncodeValue(ectx EncodeContext, vw bsonrw.ValueWriter, val reflect.Value) error {
	if val.Kind() != reflect.String {
		return ValueEncoderError{
			Name:     "StringEncodeValue",
			Kinds:    []reflect.Kind{reflect.String},
			Received: val,
		}
	}

	return vw.WriteString(val.String())
}

func (sc *StringCodec) decodeType(dc DecodeContext, vr bsonrw.ValueReader, t reflect.Type) (reflect.Value, error) {
	if t.Kind() != reflect.String {
		return emptyValue, ValueDecoderError{
			Name:     "StringDecodeValue",
			Kinds:    []reflect.Kind{reflect.String},
			Received: reflect.Zero(t),
		}
	}

	var str string
	var err error
	switch vr.Type() {
	case bsontype.String:
		str, err = vr.ReadString()
		if err != nil {
			return emptyValue, err
		}
	case bsontype.ObjectID:
		oid, err := vr.ReadObjectID()
		if err != nil {
			return emptyValue, err
		}
		if sc.DecodeObjectIDAsHex {
			str = oid.Hex()
		} else {
			byteArray := [12]byte(oid)
			str = string(byteArray[:])
		}
	case bsontype.Symbol:
		str, err = vr.ReadSymbol()
		if err != nil {
			return emptyValue, err
		}
	case bsontype.Binary:
		data, subtype, err := vr.ReadBinary()
		if err != nil {
			return emptyValue, err
		}
		if subtype != bsontype.BinaryGeneric && subtype != bsontype.BinaryBinaryOld {
			return emptyValue, decodeBinaryError{subtype: subtype, typeName: "string"}
		}
		str = string(data)
	case bsontype.Null:
		if err = vr.ReadNull(); err != nil {
			return emptyValue, err
		}
	case bsontype.Undefined:
		if err = vr.ReadUndefined(); err != nil {
			return emptyValue, err
		}
	default:
		return emptyValue, fmt.Errorf("cannot decode %v into a string type", vr.Type())
	}

	return reflect.ValueOf(str), nil
}

// DecodeValue is the ValueDecoder for string types.
func (sc *StringCodec) DecodeValue(dctx DecodeContext, vr bsonrw.ValueReader, val reflect.Value) error {
	if !val.CanSet() || val.Kind() != reflect.String {
		return ValueDecoderError{Name: "StringDecodeValue", Kinds: []reflect.Kind{reflect.String}, Received: val}
	}

	elem, err := sc.decodeType(dctx, vr, val.Type())
	if err != nil {
		return err
	}

	val.SetString(elem.String())
	return nil
}
