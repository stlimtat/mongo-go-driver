// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package topology

import (
	"encoding/json"
	"io/ioutil"
	"path"
	"testing"
	"time"

	"github.com/stlimtat/mongo-go-driver/internal/testutil/assert"
	testhelpers "github.com/stlimtat/mongo-go-driver/internal/testutil/helpers"
)

// Test case for all server selection rtt spec tests.
func TestServerSelectionRTTSpec(t *testing.T) {

	type testCase struct {
		// AvgRttMs is either "NULL" or float
		AvgRttMs  interface{} `json:"avg_rtt_ms"`
		NewRttMs  float64     `json:"new_rtt_ms"`
		NewAvgRtt float64     `json:"new_avg_rtt"`
	}

	const testsDir string = "../../../../data/server-selection/rtt"

	for _, file := range testhelpers.FindJSONFilesInDir(t, testsDir) {
		func(t *testing.T, filename string) {
			filepath := path.Join(testsDir, filename)
			content, err := ioutil.ReadFile(filepath)
			assert.Nil(t, err, "ReadFile error for %s: %v", filepath, err)

			// Remove ".json" from filename.
			testName := filename[:len(filename)-5]

			t.Run(testName, func(t *testing.T) {
				var test testCase
				err = json.Unmarshal(content, &test)
				assert.Nil(t, err, "Unmarshal error: %v", err)

				var monitor rttMonitor
				if test.AvgRttMs != "NULL" {
					// If not "NULL", then must be a number, so typecast to float64
					monitor.addSample(time.Duration(test.AvgRttMs.(float64) * float64(time.Millisecond)))
				}

				monitor.addSample(time.Duration(test.NewRttMs * float64(time.Millisecond)))
				expectedRTT := time.Duration(test.NewAvgRtt * float64(time.Millisecond))
				actualRTT := monitor.getRTT()
				assert.Equal(t, expectedRTT, actualRTT, "expected average RTT %s, got %s", expectedRTT, actualRTT)
			})
		}(t, file)
	}
}
