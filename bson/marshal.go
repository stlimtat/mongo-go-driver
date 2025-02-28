// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package bson

import (
	"github.com/stlimtat/mongo-go-driver/bson/bsoncodec"
	"github.com/stlimtat/mongo-go-driver/bson/bsonrw"
	"github.com/stlimtat/mongo-go-driver/bson/bsontype"
)

const defaultDstCap = 256

var bvwPool = bsonrw.NewBSONValueWriterPool()
var extjPool = bsonrw.NewExtJSONValueWriterPool()

// Marshaler is an interface implemented by types that can marshal themselves
// into a BSON document represented as bytes. The bytes returned must be a valid
// BSON document if the error is nil.
type Marshaler interface {
	MarshalBSON() ([]byte, error)
}

// ValueMarshaler is an interface implemented by types that can marshal
// themselves into a BSON value as bytes. The type must be the valid type for
// the bytes returned. The bytes and byte type together must be valid if the
// error is nil.
type ValueMarshaler interface {
	MarshalBSONValue() (bsontype.Type, []byte, error)
}

// Marshal returns the BSON encoding of val as a BSON document. If val is not a type that can be transformed into a
// document, MarshalValue should be used instead.
//
// Marshal will use the default registry created by NewRegistry to recursively
// marshal val into a []byte. Marshal will inspect struct tags and alter the
// marshaling process accordingly.
func Marshal(val interface{}) ([]byte, error) {
	return MarshalWithRegistry(DefaultRegistry, val)
}

// MarshalAppend will encode val as a BSON document and append the bytes to dst. If dst is not large enough to hold the
// bytes, it will be grown. If val is not a type that can be transformed into a document, MarshalValueAppend should be
// used instead.
func MarshalAppend(dst []byte, val interface{}) ([]byte, error) {
	return MarshalAppendWithRegistry(DefaultRegistry, dst, val)
}

// MarshalWithRegistry returns the BSON encoding of val as a BSON document. If val is not a type that can be transformed
// into a document, MarshalValueWithRegistry should be used instead.
func MarshalWithRegistry(r *bsoncodec.Registry, val interface{}) ([]byte, error) {
	dst := make([]byte, 0, 256) // TODO: make the default cap a constant
	return MarshalAppendWithRegistry(r, dst, val)
}

// MarshalWithContext returns the BSON encoding of val as a BSON document using EncodeContext ec. If val is not a type
// that can be transformed into a document, MarshalValueWithContext should be used instead.
func MarshalWithContext(ec bsoncodec.EncodeContext, val interface{}) ([]byte, error) {
	dst := make([]byte, 0, 256) // TODO: make the default cap a constant
	return MarshalAppendWithContext(ec, dst, val)
}

// MarshalAppendWithRegistry will encode val as a BSON document using Registry r and append the bytes to dst. If dst is
// not large enough to hold the bytes, it will be grown. If val is not a type that can be transformed into a document,
// MarshalValueAppendWithRegistry should be used instead.
func MarshalAppendWithRegistry(r *bsoncodec.Registry, dst []byte, val interface{}) ([]byte, error) {
	return MarshalAppendWithContext(bsoncodec.EncodeContext{Registry: r}, dst, val)
}

// MarshalAppendWithContext will encode val as a BSON document using Registry r and EncodeContext ec and append the
// bytes to dst. If dst is not large enough to hold the bytes, it will be grown. If val is not a type that can be
// transformed into a document, MarshalValueAppendWithContext should be used instead.
func MarshalAppendWithContext(ec bsoncodec.EncodeContext, dst []byte, val interface{}) ([]byte, error) {
	sw := new(bsonrw.SliceWriter)
	*sw = dst
	vw := bvwPool.Get(sw)
	defer bvwPool.Put(vw)

	enc := encPool.Get().(*Encoder)
	defer encPool.Put(enc)

	err := enc.Reset(vw)
	if err != nil {
		return nil, err
	}
	err = enc.SetContext(ec)
	if err != nil {
		return nil, err
	}

	err = enc.Encode(val)
	if err != nil {
		return nil, err
	}

	return *sw, nil
}

// MarshalValue returns the BSON encoding of val.
//
// MarshalValue will use bson.DefaultRegistry to transform val into a BSON value. If val is a struct, this function will
// inspect struct tags and alter the marshalling process accordingly.
func MarshalValue(val interface{}) (bsontype.Type, []byte, error) {
	return MarshalValueWithRegistry(DefaultRegistry, val)
}

// MarshalValueAppend will append the BSON encoding of val to dst. If dst is not large enough to hold the BSON encoding
// of val, dst will be grown.
func MarshalValueAppend(dst []byte, val interface{}) (bsontype.Type, []byte, error) {
	return MarshalValueAppendWithRegistry(DefaultRegistry, dst, val)
}

// MarshalValueWithRegistry returns the BSON encoding of val using Registry r.
func MarshalValueWithRegistry(r *bsoncodec.Registry, val interface{}) (bsontype.Type, []byte, error) {
	dst := make([]byte, 0, defaultDstCap)
	return MarshalValueAppendWithRegistry(r, dst, val)
}

// MarshalValueWithContext returns the BSON encoding of val using EncodeContext ec.
func MarshalValueWithContext(ec bsoncodec.EncodeContext, val interface{}) (bsontype.Type, []byte, error) {
	dst := make([]byte, 0, defaultDstCap)
	return MarshalValueAppendWithContext(ec, dst, val)
}

// MarshalValueAppendWithRegistry will append the BSON encoding of val to dst using Registry r. If dst is not large
// enough to hold the BSON encoding of val, dst will be grown.
func MarshalValueAppendWithRegistry(r *bsoncodec.Registry, dst []byte, val interface{}) (bsontype.Type, []byte, error) {
	return MarshalValueAppendWithContext(bsoncodec.EncodeContext{Registry: r}, dst, val)
}

// MarshalValueAppendWithContext will append the BSON encoding of val to dst using EncodeContext ec. If dst is not large
// enough to hold the BSON encoding of val, dst will be grown.
func MarshalValueAppendWithContext(ec bsoncodec.EncodeContext, dst []byte, val interface{}) (bsontype.Type, []byte, error) {
	// get a ValueWriter configured to write to dst
	sw := new(bsonrw.SliceWriter)
	*sw = dst
	vwFlusher := bvwPool.GetAtModeElement(sw)

	// get an Encoder and encode the value
	enc := encPool.Get().(*Encoder)
	defer encPool.Put(enc)
	if err := enc.Reset(vwFlusher); err != nil {
		return 0, nil, err
	}
	if err := enc.SetContext(ec); err != nil {
		return 0, nil, err
	}
	if err := enc.Encode(val); err != nil {
		return 0, nil, err
	}

	// flush the bytes written because we cannot guarantee that a full document has been written
	// after the flush, *sw will be in the format
	// [value type, 0 (null byte to indicate end of empty element name), value bytes..]
	if err := vwFlusher.Flush(); err != nil {
		return 0, nil, err
	}
	buffer := *sw
	return bsontype.Type(buffer[0]), buffer[2:], nil
}

// MarshalExtJSON returns the extended JSON encoding of val.
func MarshalExtJSON(val interface{}, canonical, escapeHTML bool) ([]byte, error) {
	return MarshalExtJSONWithRegistry(DefaultRegistry, val, canonical, escapeHTML)
}

// MarshalExtJSONAppend will append the extended JSON encoding of val to dst.
// If dst is not large enough to hold the extended JSON encoding of val, dst
// will be grown.
func MarshalExtJSONAppend(dst []byte, val interface{}, canonical, escapeHTML bool) ([]byte, error) {
	return MarshalExtJSONAppendWithRegistry(DefaultRegistry, dst, val, canonical, escapeHTML)
}

// MarshalExtJSONWithRegistry returns the extended JSON encoding of val using Registry r.
func MarshalExtJSONWithRegistry(r *bsoncodec.Registry, val interface{}, canonical, escapeHTML bool) ([]byte, error) {
	dst := make([]byte, 0, defaultDstCap)
	return MarshalExtJSONAppendWithContext(bsoncodec.EncodeContext{Registry: r}, dst, val, canonical, escapeHTML)
}

// MarshalExtJSONWithContext returns the extended JSON encoding of val using Registry r.
func MarshalExtJSONWithContext(ec bsoncodec.EncodeContext, val interface{}, canonical, escapeHTML bool) ([]byte, error) {
	dst := make([]byte, 0, defaultDstCap)
	return MarshalExtJSONAppendWithContext(ec, dst, val, canonical, escapeHTML)
}

// MarshalExtJSONAppendWithRegistry will append the extended JSON encoding of
// val to dst using Registry r. If dst is not large enough to hold the BSON
// encoding of val, dst will be grown.
func MarshalExtJSONAppendWithRegistry(r *bsoncodec.Registry, dst []byte, val interface{}, canonical, escapeHTML bool) ([]byte, error) {
	return MarshalExtJSONAppendWithContext(bsoncodec.EncodeContext{Registry: r}, dst, val, canonical, escapeHTML)
}

// MarshalExtJSONAppendWithContext will append the extended JSON encoding of
// val to dst using Registry r. If dst is not large enough to hold the BSON
// encoding of val, dst will be grown.
func MarshalExtJSONAppendWithContext(ec bsoncodec.EncodeContext, dst []byte, val interface{}, canonical, escapeHTML bool) ([]byte, error) {
	sw := new(bsonrw.SliceWriter)
	*sw = dst
	ejvw := extjPool.Get(sw, canonical, escapeHTML)
	defer extjPool.Put(ejvw)

	enc := encPool.Get().(*Encoder)
	defer encPool.Put(enc)

	err := enc.Reset(ejvw)
	if err != nil {
		return nil, err
	}
	err = enc.SetContext(ec)
	if err != nil {
		return nil, err
	}

	err = enc.Encode(val)
	if err != nil {
		return nil, err
	}

	return *sw, nil
}
