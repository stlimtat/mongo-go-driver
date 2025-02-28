// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package mongo

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/stlimtat/mongo-go-driver/mongo/options"
	"github.com/stlimtat/mongo-go-driver/mongo/readconcern"
	"github.com/stlimtat/mongo-go-driver/mongo/readpref"
	"github.com/stlimtat/mongo-go-driver/x/bsonx/bsoncore"
)

const (
	defaultServerSelectionTimeout = 10 * time.Second
	defaultURI                    = "mongodb://localhost:27020"
	defaultPath                   = "mongocryptd"
	serverSelectionTimeoutStr     = "server selection error"
)

var defaultTimeoutArgs = []string{"--idleShutdownTimeoutSecs=60"}
var databaseOpts = options.Database().SetReadConcern(readconcern.New()).SetReadPreference(readpref.Primary())

type mcryptClient struct {
	bypassSpawn bool
	client      *Client
	path        string
	spawnArgs   []string
}

func newMcryptClient(opts *options.AutoEncryptionOptions) (*mcryptClient, error) {
	// create mcryptClient instance and spawn process if necessary
	var bypassSpawn bool
	var bypassAutoEncryption bool
	if bypass, ok := opts.ExtraOptions["mongocryptdBypassSpawn"]; ok {
		bypassSpawn = bypass.(bool)
	}
	if opts.BypassAutoEncryption != nil {
		bypassAutoEncryption = *opts.BypassAutoEncryption
	}

	mc := &mcryptClient{
		// mongocryptd should not be spawned if mongocryptdBypassSpawn is passed or if bypassAutoEncryption is
		// specified because it is not used during decryption
		bypassSpawn: bypassSpawn || bypassAutoEncryption,
	}

	if !mc.bypassSpawn {
		mc.path, mc.spawnArgs = createSpawnArgs(opts.ExtraOptions)
		if err := mc.spawnProcess(); err != nil {
			return nil, err
		}
	}

	// get connection string
	uri := defaultURI
	if u, ok := opts.ExtraOptions["mongocryptdURI"]; ok {
		uri = u.(string)
	}

	// create client
	client, err := NewClient(options.Client().ApplyURI(uri).SetServerSelectionTimeout(defaultServerSelectionTimeout))
	if err != nil {
		return nil, err
	}
	mc.client = client

	return mc, nil
}

// markCommand executes the given command on mongocryptd.
func (mc *mcryptClient) markCommand(ctx context.Context, dbName string, cmd bsoncore.Document) (bsoncore.Document, error) {
	db := mc.client.Database(dbName, databaseOpts)

	res, err := db.RunCommand(ctx, cmd).DecodeBytes()
	// propagate original result
	if err == nil {
		return bsoncore.Document(res), nil
	}
	// wrap original error
	if mc.bypassSpawn || !strings.Contains(err.Error(), serverSelectionTimeoutStr) {
		return nil, MongocryptdError{Wrapped: err}
	}

	// re-spawn and retry
	if err = mc.spawnProcess(); err != nil {
		return nil, err
	}
	res, err = db.RunCommand(ctx, cmd).DecodeBytes()
	if err != nil {
		return nil, MongocryptdError{Wrapped: err}
	}
	return bsoncore.Document(res), nil
}

// connect connects the underlying Client instance. This must be called before performing any mark operations.
func (mc *mcryptClient) connect(ctx context.Context) error {
	return mc.client.Connect(ctx)
}

// disconnect disconnects the underlying Client instance. This should be called after all operations have completed.
func (mc *mcryptClient) disconnect(ctx context.Context) error {
	return mc.client.Disconnect(ctx)
}

func (mc *mcryptClient) spawnProcess() error {
	cmd := exec.Command(mc.path, mc.spawnArgs...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Start()
}

// createSpawnArgs creates arguments to spawn mcryptClient. It returns the path and a slice of arguments.
func createSpawnArgs(opts map[string]interface{}) (string, []string) {
	var spawnArgs []string

	// get command path
	path := defaultPath
	if p, ok := opts["mongocryptdPath"]; ok {
		path = p.(string)
	}

	// add specified options
	if sa, ok := opts["mongocryptdSpawnArgs"]; ok {
		spawnArgs = append(spawnArgs, sa.([]string)...)
	}

	// add timeout options if necessary
	var foundTimeout bool
	for _, arg := range spawnArgs {
		// need to use HasPrefix instead of doing an exact equality check because both
		// mongocryptd supports both [--idleShutdownTimeoutSecs, 0] and [--idleShutdownTimeoutSecs=0]
		if strings.HasPrefix(arg, "--idleShutdownTimeoutSecs") {
			foundTimeout = true
			break
		}
	}
	if !foundTimeout {
		spawnArgs = append(spawnArgs, defaultTimeoutArgs...)
	}

	return path, spawnArgs
}
