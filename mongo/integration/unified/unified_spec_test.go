// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package unified

import (
	"path"
	"testing"

	"github.com/stlimtat/mongo-go-driver/mongo/integration/mtest"
)

var (
	directories = []string{
		"unified-test-format/valid-pass",
		"versioned-api",
		"crud/unified",
		"change-streams/unified",
		"transactions/unified",
		"load-balancers",
	}
)

const (
	dataDirectory = "../../../data"
)

func TestUnifiedSpec(t *testing.T) {
	// Ensure the cluster is in a clean state before test execution begins.
	if err := terminateOpenSessions(mtest.Background); err != nil {
		t.Fatalf("error terminating open transactions: %v", err)
	}

	for _, testDir := range directories {
		t.Run(testDir, func(t *testing.T) {
			runTestDirectory(t, path.Join(dataDirectory, testDir))
		})
	}
}
