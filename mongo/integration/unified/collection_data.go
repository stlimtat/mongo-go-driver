// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package unified

import (
	"bytes"
	"context"
	"fmt"

	"github.com/stlimtat/mongo-go-driver/bson"
	testhelpers "github.com/stlimtat/mongo-go-driver/internal/testutil/helpers"
	"github.com/stlimtat/mongo-go-driver/mongo/integration/mtest"
	"github.com/stlimtat/mongo-go-driver/mongo/options"
	"github.com/stlimtat/mongo-go-driver/mongo/readconcern"
	"github.com/stlimtat/mongo-go-driver/mongo/readpref"
)

type collectionData struct {
	DatabaseName   string     `bson:"databaseName"`
	CollectionName string     `bson:"collectionName"`
	Documents      []bson.Raw `bson:"documents"`
}

// createCollection configures the collection represented by the receiver using the internal client. This function
// first drops the collection and then creates it and inserts the seed data if needed.
func (c *collectionData) createCollection(ctx context.Context) error {
	db := mtest.GlobalClient().Database(c.DatabaseName)
	coll := db.Collection(c.CollectionName, options.Collection().SetWriteConcern(mtest.MajorityWc))
	if err := coll.Drop(ctx); err != nil {
		return fmt.Errorf("error dropping collection: %v", err)
	}

	// If no data is given, create the collection with write concern "majority".
	if len(c.Documents) == 0 {
		// The write concern has to be manually specified in the command document because RunCommand does not honor
		// the database's write concern.
		create := bson.D{
			{"create", coll.Name()},
			{"writeConcern", bson.D{
				{"w", "majority"},
			}},
		}
		if err := db.RunCommand(ctx, create).Err(); err != nil {
			return fmt.Errorf("error creating collection: %v", err)
		}
		return nil
	}

	docs := testhelpers.RawSliceToInterfaceSlice(c.Documents)
	if _, err := coll.InsertMany(ctx, docs); err != nil {
		return fmt.Errorf("error inserting data: %v", err)
	}
	return nil
}

// verifyContents asserts that the collection on the server represented by this collectionData instance contains the
// expected documents.
func (c *collectionData) verifyContents(ctx context.Context) error {
	collOpts := options.Collection().
		SetReadPreference(readpref.Primary()).
		SetReadConcern(readconcern.Local())
	coll := mtest.GlobalClient().Database(c.DatabaseName).Collection(c.CollectionName, collOpts)

	cursor, err := coll.Find(ctx, bson.D{}, options.Find().SetSort(bson.M{"_id": 1}))
	if err != nil {
		return fmt.Errorf("Find error: %v", err)
	}
	defer cursor.Close(ctx)

	var docs []bson.Raw
	if err := cursor.All(ctx, &docs); err != nil {
		return fmt.Errorf("cursor iteration error: %v", err)
	}

	// Verify the slice lengths are equal. This also covers the case of asserting that the collection is empty if
	// c.Documents is an empty slice.
	if len(c.Documents) != len(docs) {
		return fmt.Errorf("expected %d documents but found %d: %v", len(c.Documents), len(docs), docs)
	}

	// We can't use verifyValuesMatch here because the rules for evaluating matches (e.g. flexible numeric comparisons
	// and special $$ operators) do not apply when verifying collection outcomes. We have to permit variations in key
	// order, though, so we sort documents before doing a byte-wise comparison.
	for idx, expected := range c.Documents {
		expected = sortDocument(expected)
		actual := sortDocument(docs[idx])

		if !bytes.Equal(expected, actual) {
			return fmt.Errorf("document comparison error at index %d: expected %s, got %s", idx, expected, actual)
		}
	}
	return nil
}

func (c *collectionData) namespace() string {
	return fmt.Sprintf("%s.%s", c.DatabaseName, c.CollectionName)
}
