// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package integration

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/stlimtat/mongo-go-driver/bson"
	"github.com/stlimtat/mongo-go-driver/bson/bsontype"
	"github.com/stlimtat/mongo-go-driver/event"
	"github.com/stlimtat/mongo-go-driver/internal/testutil/assert"
	"github.com/stlimtat/mongo-go-driver/mongo/integration/mtest"
)

// Helper functions to compare BSON values and command monitoring expectations.

func numberFromValue(mt *mtest.T, val bson.RawValue) int64 {
	mt.Helper()

	switch val.Type {
	case bson.TypeInt32:
		return int64(val.Int32())
	case bson.TypeInt64:
		return val.Int64()
	case bson.TypeDouble:
		return int64(val.Double())
	default:
		mt.Fatalf("unexpected type for number: %v", val.Type)
	}

	return 0
}

func compareNumberValues(mt *mtest.T, key string, expected, actual bson.RawValue) error {
	eInt := numberFromValue(mt, expected)
	if eInt == 42 {
		if actual.Type == bson.TypeNull {
			return fmt.Errorf("expected non-null value for key %s, got null", key)
		}
		return nil
	}

	aInt := numberFromValue(mt, actual)
	if eInt != aInt {
		return fmt.Errorf("value mismatch for key %s; expected %s, got %s", key, expected, actual)
	}
	return nil
}

// compare BSON values and fail if they are not equal. the key parameter is used for error strings.
// if the expected value is a numeric type (int32, int64, or double) and the value is 42, the function only asserts that
// the actual value is non-null.
func compareValues(mt *mtest.T, key string, expected, actual bson.RawValue) error {
	mt.Helper()

	switch expected.Type {
	case bson.TypeInt32, bson.TypeInt64, bson.TypeDouble:
		if err := compareNumberValues(mt, key, expected, actual); err != nil {
			return err
		}
		return nil
	case bson.TypeString:
		val := expected.StringValue()
		if val == "42" {
			if actual.Type == bson.TypeNull {
				return fmt.Errorf("expected non-null value for key %s, got null", key)
			}
			return nil
		}
		break // compare bytes for expected.Value and actual.Value outside of the switch
	case bson.TypeEmbeddedDocument:
		e := expected.Document()
		if typeVal, err := e.LookupErr("$$type"); err == nil {
			// $$type represents a type assertion
			// for example {field: {$$type: "binData"}} should assert that "field" is an element with a binary value
			return checkValueType(mt, key, actual.Type, typeVal.StringValue())
		}

		a := actual.Document()
		return compareDocsHelper(mt, e, a, key)
	case bson.TypeArray:
		e := expected.Array()
		a := actual.Array()
		return compareDocsHelper(mt, e, a, key)
	}

	if expected.Type != actual.Type {
		return fmt.Errorf("type mismatch for key %s; expected %s, got %s", key, expected.Type, actual.Type)
	}
	if !bytes.Equal(expected.Value, actual.Value) {
		return fmt.Errorf("value mismatch for key %s; expected %s, got %s", key, expected.Value, actual.Value)
	}
	return nil
}

// helper for $$type assertions
func checkValueType(mt *mtest.T, key string, actual bsontype.Type, typeStr string) error {
	mt.Helper()

	var expected bsontype.Type
	switch typeStr {
	case "double":
		expected = bsontype.Double
	case "string":
		expected = bsontype.String
	case "object":
		expected = bsontype.EmbeddedDocument
	case "array":
		expected = bsontype.Array
	case "binData":
		expected = bsontype.Binary
	case "undefined":
		expected = bsontype.Undefined
	case "objectId":
		expected = bsontype.ObjectID
	case "boolean":
		expected = bsontype.Boolean
	case "date":
		expected = bsontype.DateTime
	case "null":
		expected = bsontype.Null
	case "regex":
		expected = bsontype.Regex
	case "dbPointer":
		expected = bsontype.DBPointer
	case "javascript":
		expected = bsontype.JavaScript
	case "symbol":
		expected = bsontype.Symbol
	case "javascriptWithScope":
		expected = bsontype.CodeWithScope
	case "int":
		expected = bsontype.Int32
	case "timestamp":
		expected = bsontype.Timestamp
	case "long":
		expected = bsontype.Int64
	case "decimal":
		expected = bsontype.Decimal128
	case "minKey":
		expected = bsontype.MinKey
	case "maxKey":
		expected = bsontype.MaxKey
	default:
		mt.Fatalf("unrecognized type string: %v", typeStr)
	}

	if expected != actual {
		return fmt.Errorf("BSON type mismatch for key %s; expected %s, got %s", key, expected, actual)
	}
	return nil
}

// compare expected and actual BSON documents. comparison succeeds if actual contains each element in expected.
func compareDocsHelper(mt *mtest.T, expected, actual bson.Raw, prefix string) error {
	mt.Helper()

	eElems, err := expected.Elements()
	assert.Nil(mt, err, "error getting expected elements: %v", err)

	for _, e := range eElems {
		eKey := e.Key()
		fullKeyName := eKey
		if prefix != "" {
			fullKeyName = prefix + "." + eKey
		}

		aVal, err := actual.LookupErr(eKey)
		if err != nil {
			return fmt.Errorf("key %s not found in result", fullKeyName)
		}

		if err := compareValues(mt, fullKeyName, e.Value(), aVal); err != nil {
			return err
		}
	}
	return nil
}

func compareDocs(mt *mtest.T, expected, actual bson.Raw) error {
	mt.Helper()
	return compareDocsHelper(mt, expected, actual, "")
}

func checkExpectations(mt *mtest.T, expectations *[]*expectation, id0, id1 bson.Raw) {
	mt.Helper()

	// If the expectations field in the test JSON is null, we want to skip all command monitoring assertions.
	if expectations == nil {
		return
	}

	// Filter out events that shouldn't show up in monitoring expectations.
	ignoredEvents := map[string]struct{}{
		"configureFailPoint": {},
	}
	mt.FilterStartedEvents(func(evt *event.CommandStartedEvent) bool {
		// ok is true if the command should be ignored, so return !ok
		_, ok := ignoredEvents[evt.CommandName]
		return !ok
	})
	mt.FilterSucceededEvents(func(evt *event.CommandSucceededEvent) bool {
		// ok is true if the command should be ignored, so return !ok
		_, ok := ignoredEvents[evt.CommandName]
		return !ok
	})
	mt.FilterFailedEvents(func(evt *event.CommandFailedEvent) bool {
		// ok is true if the command should be ignored, so return !ok
		_, ok := ignoredEvents[evt.CommandName]
		return !ok
	})

	// If the expectations field in the test JSON is non-null but is empty, we want to assert that no events were
	// emitted.
	if len(*expectations) == 0 {
		// One of the bulkWrite spec tests expects update and updateMany to be grouped together into a single batch,
		// but this isn't the case because of GODRIVER-1157. To work around this, we expect one event to be emitted for
		// that test rather than 0. This assertion should be changed when GODRIVER-1157 is done.
		numExpectedEvents := 0
		bulkWriteTestName := "BulkWrite_on_server_that_doesn't_support_arrayFilters_with_arrayFilters_on_second_op"
		if strings.HasSuffix(mt.Name(), bulkWriteTestName) {
			numExpectedEvents = 1
		}

		numActualEvents := len(mt.GetAllStartedEvents())
		assert.Equal(mt, numExpectedEvents, numActualEvents, "expected %d events to be sent, but got %d events",
			numExpectedEvents, numActualEvents)
		return
	}

	for idx, expectation := range *expectations {
		var err error

		if expectation.CommandStartedEvent != nil {
			err = compareStartedEvent(mt, expectation, id0, id1)
		}
		if expectation.CommandSucceededEvent != nil {
			err = compareSucceededEvent(mt, expectation)
		}
		if expectation.CommandFailedEvent != nil {
			err = compareFailedEvent(mt, expectation)
		}

		assert.Nil(mt, err, "expectation comparison error at index %v: %s", idx, err)
	}
}

func compareStartedEvent(mt *mtest.T, expectation *expectation, id0, id1 bson.Raw) error {
	mt.Helper()

	expected := expectation.CommandStartedEvent
	evt := mt.GetStartedEvent()
	if evt == nil {
		return errors.New("expected CommandStartedEvent, got nil")
	}

	if expected.CommandName != "" && expected.CommandName != evt.CommandName {
		return fmt.Errorf("command name mismatch; expected %s, got %s", expected.CommandName, evt.CommandName)
	}
	if expected.DatabaseName != "" && expected.DatabaseName != evt.DatabaseName {
		return fmt.Errorf("database name mismatch; expected %s, got %s", expected.DatabaseName, evt.DatabaseName)
	}

	eElems, err := expected.Command.Elements()
	if err != nil {
		return fmt.Errorf("error getting expected command elements: %s", err)
	}

	for _, elem := range eElems {
		key := elem.Key()
		val := elem.Value()

		actualVal, err := evt.Command.LookupErr(key)

		// Keys that may be nil
		if val.Type == bson.TypeNull {
			assert.NotNil(mt, err, "expected key %q to be omitted but got %q", key, actualVal)
			continue
		}
		assert.Nil(mt, err, "expected command to contain key %q", key)

		if key == "batchSize" {
			// Some command monitoring tests expect that the driver will send a lower batch size if the required batch
			// size is lower than the operation limit. We only do this for legacy servers <= 3.0 because those server
			// versions do not support the limit option, but not for 3.2+. We've already validated that the command
			// contains a batchSize field above and we can skip the actual value comparison below.
			continue
		}

		switch key {
		case "lsid":
			sessName := val.StringValue()
			var expectedID bson.Raw
			actualID := actualVal.Document()

			switch sessName {
			case "session0":
				expectedID = id0
			case "session1":
				expectedID = id1
			default:
				return fmt.Errorf("unrecognized session identifier in command document: %s", sessName)
			}

			if !bytes.Equal(expectedID, actualID) {
				return fmt.Errorf("session ID mismatch for session %s; expected %s, got %s", sessName, expectedID,
					actualID)
			}
		default:
			if err := compareValues(mt, key, val, actualVal); err != nil {
				return err
			}
		}
	}
	return nil
}

func compareWriteErrors(mt *mtest.T, expected, actual bson.Raw) error {
	mt.Helper()

	expectedErrors, _ := expected.Values()
	actualErrors, _ := actual.Values()

	for i, expectedErrVal := range expectedErrors {
		expectedErr := expectedErrVal.Document()
		actualErr := actualErrors[i].Document()

		eIdx := expectedErr.Lookup("index").Int32()
		aIdx := actualErr.Lookup("index").Int32()
		if eIdx != aIdx {
			return fmt.Errorf("write error index mismatch at index %d; expected %d, got %d", i, eIdx, aIdx)
		}

		eCode := expectedErr.Lookup("code").Int32()
		aCode := actualErr.Lookup("code").Int32()
		if eCode != 42 && eCode != aCode {
			return fmt.Errorf("write error code mismatch at index %d; expected %d, got %d", i, eCode, aCode)
		}

		eMsg := expectedErr.Lookup("errmsg").StringValue()
		aMsg := actualErr.Lookup("errmsg").StringValue()
		if eMsg == "" {
			if aMsg == "" {
				return fmt.Errorf("write error message mismatch at index %d; expected non-empty message, got empty", i)
			}
			return nil
		}
		if eMsg != aMsg {
			return fmt.Errorf("write error message mismatch at index %d, expected %s, got %s", i, eMsg, aMsg)
		}
	}
	return nil
}

func compareSucceededEvent(mt *mtest.T, expectation *expectation) error {
	mt.Helper()

	expected := expectation.CommandSucceededEvent
	evt := mt.GetSucceededEvent()
	if evt == nil {
		return errors.New("expected CommandSucceededEvent, got nil")
	}

	if expected.CommandName != "" && expected.CommandName != evt.CommandName {
		return fmt.Errorf("command name mismatch; expected %s, got %s", expected.CommandName, evt.CommandName)
	}

	eElems, err := expected.Reply.Elements()
	if err != nil {
		return fmt.Errorf("error getting expected reply elements: %s", err)
	}

	for _, elem := range eElems {
		key := elem.Key()
		val := elem.Value()
		actualVal := evt.Reply.Lookup(key)

		switch key {
		case "writeErrors":
			if err = compareWriteErrors(mt, val.Array(), actualVal.Array()); err != nil {
				return err
			}
		default:
			if err := compareValues(mt, key, val, actualVal); err != nil {
				return err
			}
		}
	}
	return nil
}

func compareFailedEvent(mt *mtest.T, expectation *expectation) error {
	mt.Helper()

	expected := expectation.CommandFailedEvent
	evt := mt.GetFailedEvent()
	if evt == nil {
		return errors.New("expected CommandFailedEvent, got nil")
	}

	if expected.CommandName != "" && expected.CommandName != evt.CommandName {
		return fmt.Errorf("command name mismatch; expected %s, got %s", expected.CommandName, evt.CommandName)
	}
	return nil
}
