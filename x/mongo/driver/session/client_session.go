// Copyright (C) MongoDB, Inc. 2017-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package session // import "github.com/stlimtat/mongo-go-driver/x/mongo/driver/session"

import (
	"context"
	"errors"
	"time"

	"github.com/stlimtat/mongo-go-driver/bson"
	"github.com/stlimtat/mongo-go-driver/bson/primitive"
	"github.com/stlimtat/mongo-go-driver/mongo/address"
	"github.com/stlimtat/mongo-go-driver/mongo/description"
	"github.com/stlimtat/mongo-go-driver/mongo/readconcern"
	"github.com/stlimtat/mongo-go-driver/mongo/readpref"
	"github.com/stlimtat/mongo-go-driver/mongo/writeconcern"
	"github.com/stlimtat/mongo-go-driver/x/mongo/driver/uuid"
)

// ErrSessionEnded is returned when a client session is used after a call to endSession().
var ErrSessionEnded = errors.New("ended session was used")

// ErrNoTransactStarted is returned if a transaction operation is called when no transaction has started.
var ErrNoTransactStarted = errors.New("no transaction started")

// ErrTransactInProgress is returned if startTransaction() is called when a transaction is in progress.
var ErrTransactInProgress = errors.New("transaction already in progress")

// ErrAbortAfterCommit is returned when abort is called after a commit.
var ErrAbortAfterCommit = errors.New("cannot call abortTransaction after calling commitTransaction")

// ErrAbortTwice is returned if abort is called after transaction is already aborted.
var ErrAbortTwice = errors.New("cannot call abortTransaction twice")

// ErrCommitAfterAbort is returned if commit is called after an abort.
var ErrCommitAfterAbort = errors.New("cannot call commitTransaction after calling abortTransaction")

// ErrUnackWCUnsupported is returned if an unacknowledged write concern is supported for a transaciton.
var ErrUnackWCUnsupported = errors.New("transactions do not support unacknowledged write concerns")

// Type describes the type of the session
type Type uint8

// These constants are the valid types for a client session.
const (
	Explicit Type = iota
	Implicit
)

// TransactionState indicates the state of the transactions FSM.
type TransactionState uint8

// Client Session states
const (
	None TransactionState = iota
	Starting
	InProgress
	Committed
	Aborted
)

// String implements the fmt.Stringer interface.
func (s TransactionState) String() string {
	switch s {
	case None:
		return "none"
	case Starting:
		return "starting"
	case InProgress:
		return "in progress"
	case Committed:
		return "committed"
	case Aborted:
		return "aborted"
	default:
		return "unknown"
	}
}

// LoadBalancedTransactionConnection represents a connection that's pinned by a ClientSession because it's being used
// to execute a transaction when running against a load balancer. This interface is a copy of driver.PinnedConnection
// and exists to be able to pin transactions to a connection without causing an import cycle.
type LoadBalancedTransactionConnection interface {
	// Functions copied over from driver.Connection.
	WriteWireMessage(context.Context, []byte) error
	ReadWireMessage(ctx context.Context, dst []byte) ([]byte, error)
	Description() description.Server
	Close() error
	ID() string
	Address() address.Address
	Stale() bool

	// Functions copied over from driver.Expirable.
	Alive() bool
	Expire() error

	// Functions copied over from driver.PinnedConnection that are not part of Connection or Expirable.
	PinToCursor() error
	PinToTransaction() error
	UnpinFromCursor() error
	UnpinFromTransaction() error
}

// Client is a session for clients to run commands.
type Client struct {
	*Server
	ClientID       uuid.UUID
	ClusterTime    bson.Raw
	Consistent     bool // causal consistency
	OperationTime  *primitive.Timestamp
	SessionType    Type
	Terminated     bool
	RetryingCommit bool
	Committing     bool
	Aborting       bool
	RetryWrite     bool
	RetryRead      bool

	// options for the current transaction
	// most recently set by transactionopt
	CurrentRc  *readconcern.ReadConcern
	CurrentRp  *readpref.ReadPref
	CurrentWc  *writeconcern.WriteConcern
	CurrentMct *time.Duration

	// default transaction options
	transactionRc            *readconcern.ReadConcern
	transactionRp            *readpref.ReadPref
	transactionWc            *writeconcern.WriteConcern
	transactionMaxCommitTime *time.Duration

	pool             *Pool
	TransactionState TransactionState
	PinnedServer     *description.Server
	RecoveryToken    bson.Raw
	PinnedConnection LoadBalancedTransactionConnection
}

func getClusterTime(clusterTime bson.Raw) (uint32, uint32) {
	if clusterTime == nil {
		return 0, 0
	}

	clusterTimeVal, err := clusterTime.LookupErr("$clusterTime")
	if err != nil {
		return 0, 0
	}

	timestampVal, err := bson.Raw(clusterTimeVal.Value).LookupErr("clusterTime")
	if err != nil {
		return 0, 0
	}

	return timestampVal.Timestamp()
}

// MaxClusterTime compares 2 clusterTime documents and returns the document representing the highest cluster time.
func MaxClusterTime(ct1, ct2 bson.Raw) bson.Raw {
	epoch1, ord1 := getClusterTime(ct1)
	epoch2, ord2 := getClusterTime(ct2)

	if epoch1 > epoch2 {
		return ct1
	} else if epoch1 < epoch2 {
		return ct2
	} else if ord1 > ord2 {
		return ct1
	} else if ord1 < ord2 {
		return ct2
	}

	return ct1
}

// NewClientSession creates a Client.
func NewClientSession(pool *Pool, clientID uuid.UUID, sessionType Type, opts ...*ClientOptions) (*Client, error) {
	c := &Client{
		Consistent:  true, // set default
		ClientID:    clientID,
		SessionType: sessionType,
		pool:        pool,
	}

	mergedOpts := mergeClientOptions(opts...)
	if mergedOpts.CausalConsistency != nil {
		c.Consistent = *mergedOpts.CausalConsistency
	}
	if mergedOpts.DefaultReadPreference != nil {
		c.transactionRp = mergedOpts.DefaultReadPreference
	}
	if mergedOpts.DefaultReadConcern != nil {
		c.transactionRc = mergedOpts.DefaultReadConcern
	}
	if mergedOpts.DefaultWriteConcern != nil {
		c.transactionWc = mergedOpts.DefaultWriteConcern
	}
	if mergedOpts.DefaultMaxCommitTime != nil {
		c.transactionMaxCommitTime = mergedOpts.DefaultMaxCommitTime
	}

	servSess, err := pool.GetSession()
	if err != nil {
		return nil, err
	}

	c.Server = servSess

	return c, nil
}

// AdvanceClusterTime updates the session's cluster time.
func (c *Client) AdvanceClusterTime(clusterTime bson.Raw) error {
	if c.Terminated {
		return ErrSessionEnded
	}
	c.ClusterTime = MaxClusterTime(c.ClusterTime, clusterTime)
	return nil
}

// AdvanceOperationTime updates the session's operation time.
func (c *Client) AdvanceOperationTime(opTime *primitive.Timestamp) error {
	if c.Terminated {
		return ErrSessionEnded
	}

	if c.OperationTime == nil {
		c.OperationTime = opTime
		return nil
	}

	if opTime.T > c.OperationTime.T {
		c.OperationTime = opTime
	} else if (opTime.T == c.OperationTime.T) && (opTime.I > c.OperationTime.I) {
		c.OperationTime = opTime
	}

	return nil
}

// UpdateUseTime sets the session's last used time to the current time. This must be called whenever the session is
// used to send a command to the server to ensure that the session is not prematurely marked expired in the driver's
// session pool. If the session has already been ended, this method will return ErrSessionEnded.
func (c *Client) UpdateUseTime() error {
	if c.Terminated {
		return ErrSessionEnded
	}
	c.updateUseTime()
	return nil
}

// UpdateRecoveryToken updates the session's recovery token from the server response.
func (c *Client) UpdateRecoveryToken(response bson.Raw) {
	if c == nil {
		return
	}

	token, err := response.LookupErr("recoveryToken")
	if err != nil {
		return
	}

	c.RecoveryToken = token.Document()
}

// ClearPinnedServer sets the PinnedServer to nil.
func (c *Client) ClearPinnedServer() {
	if c != nil {
		c.PinnedServer = nil
	}
}

// UnpinConnection gracefully unpins the connection associated with the session if there is one. This is done via
// the pinned connection's UnpinFromTransaction function.
func (c *Client) UnpinConnection() error {
	if c == nil || c.PinnedConnection == nil {
		return nil
	}

	err := c.PinnedConnection.UnpinFromTransaction()
	closeErr := c.PinnedConnection.Close()
	if err == nil && closeErr != nil {
		err = closeErr
	}
	c.PinnedConnection = nil
	return err
}

// ExpirePinnedConnection forcefully unpins the connection assocated with the session if there is one. This is done via
// the pinned connection's Expire function.
func (c *Client) ExpirePinnedConnection() error {
	if c == nil || c.PinnedConnection == nil {
		return nil
	}

	err := c.PinnedConnection.Expire()
	c.PinnedConnection = nil
	return err
}

// EndSession ends the session.
func (c *Client) EndSession() {
	if c.Terminated {
		return
	}

	c.Terminated = true
	c.pool.ReturnSession(c.Server)

	return
}

// TransactionInProgress returns true if the client session is in an active transaction.
func (c *Client) TransactionInProgress() bool {
	return c.TransactionState == InProgress
}

// TransactionStarting returns true if the client session is starting a transaction.
func (c *Client) TransactionStarting() bool {
	return c.TransactionState == Starting
}

// TransactionRunning returns true if the client session has started the transaction
// and it hasn't been committed or aborted
func (c *Client) TransactionRunning() bool {
	return c != nil && (c.TransactionState == Starting || c.TransactionState == InProgress)
}

// TransactionCommitted returns true of the client session just committed a transaciton.
func (c *Client) TransactionCommitted() bool {
	return c.TransactionState == Committed
}

// CheckStartTransaction checks to see if allowed to start transaction and returns
// an error if not allowed
func (c *Client) CheckStartTransaction() error {
	if c.TransactionState == InProgress || c.TransactionState == Starting {
		return ErrTransactInProgress
	}
	return nil
}

// StartTransaction initializes the transaction options and advances the state machine.
// It does not contact the server to start the transaction.
func (c *Client) StartTransaction(opts *TransactionOptions) error {
	err := c.CheckStartTransaction()
	if err != nil {
		return err
	}

	c.IncrementTxnNumber()
	c.RetryingCommit = false

	if opts != nil {
		c.CurrentRc = opts.ReadConcern
		c.CurrentRp = opts.ReadPreference
		c.CurrentWc = opts.WriteConcern
		c.CurrentMct = opts.MaxCommitTime
	}

	if c.CurrentRc == nil {
		c.CurrentRc = c.transactionRc
	}

	if c.CurrentRp == nil {
		c.CurrentRp = c.transactionRp
	}

	if c.CurrentWc == nil {
		c.CurrentWc = c.transactionWc
	}

	if c.CurrentMct == nil {
		c.CurrentMct = c.transactionMaxCommitTime
	}

	if !writeconcern.AckWrite(c.CurrentWc) {
		c.clearTransactionOpts()
		return ErrUnackWCUnsupported
	}

	c.TransactionState = Starting
	c.PinnedServer = nil
	return nil
}

// CheckCommitTransaction checks to see if allowed to commit transaction and returns
// an error if not allowed.
func (c *Client) CheckCommitTransaction() error {
	if c.TransactionState == None {
		return ErrNoTransactStarted
	} else if c.TransactionState == Aborted {
		return ErrCommitAfterAbort
	}
	return nil
}

// CommitTransaction updates the state for a successfully committed transaction and returns
// an error if not permissible.  It does not actually perform the commit.
func (c *Client) CommitTransaction() error {
	err := c.CheckCommitTransaction()
	if err != nil {
		return err
	}
	c.TransactionState = Committed
	return nil
}

// UpdateCommitTransactionWriteConcern will set the write concern to majority and potentially set  a
// w timeout of 10 seconds. This should be called after a commit transaction operation fails with a
// retryable error or after a successful commit transaction operation.
func (c *Client) UpdateCommitTransactionWriteConcern() {
	wc := c.CurrentWc
	timeout := 10 * time.Second
	if wc != nil && wc.GetWTimeout() != 0 {
		timeout = wc.GetWTimeout()
	}
	c.CurrentWc = wc.WithOptions(writeconcern.WMajority(), writeconcern.WTimeout(timeout))
}

// CheckAbortTransaction checks to see if allowed to abort transaction and returns
// an error if not allowed.
func (c *Client) CheckAbortTransaction() error {
	if c.TransactionState == None {
		return ErrNoTransactStarted
	} else if c.TransactionState == Committed {
		return ErrAbortAfterCommit
	} else if c.TransactionState == Aborted {
		return ErrAbortTwice
	}
	return nil
}

// AbortTransaction updates the state for a successfully aborted transaction and returns
// an error if not permissible.  It does not actually perform the abort.
func (c *Client) AbortTransaction() error {
	err := c.CheckAbortTransaction()
	if err != nil {
		return err
	}
	c.TransactionState = Aborted
	c.clearTransactionOpts()
	return nil
}

// ApplyCommand advances the state machine upon command execution.
func (c *Client) ApplyCommand(desc description.Server) {
	if c.Committing {
		// Do not change state if committing after already committed
		return
	}
	if c.TransactionState == Starting {
		c.TransactionState = InProgress
		// If this is in a transaction and the server is a mongos, pin it
		if desc.Kind == description.Mongos {
			c.PinnedServer = &desc
		}
	} else if c.TransactionState == Committed || c.TransactionState == Aborted {
		c.clearTransactionOpts()
		c.TransactionState = None
	}
}

func (c *Client) clearTransactionOpts() {
	c.RetryingCommit = false
	c.Aborting = false
	c.Committing = false
	c.CurrentWc = nil
	c.CurrentRp = nil
	c.CurrentRc = nil
	c.PinnedServer = nil
	c.RecoveryToken = nil
}
