// STUB(connect): replaced by the owner branch on merge.

// Package connect holds the accounts a person has connected to aforge, and the
// sign-in that connects them. This file is the shape the chat surface reads and
// nothing else: the owner branch replaces it with the engine.
package connect

import (
	"context"
	"errors"
)

// Service is one thing a person can connect — Google first.
type Service struct {
	ID    string
	Name  string
	Blurb string

	Scopes []string
}

// Status is a service and whether this profile has it, with the account it is
// held as. An empty Account is a service connected under a name nobody
// reported, and a surface draws nothing rather than an empty parenthetical.
//
// It is NOT comparable — Service carries a slice — so callers compare the fields
// they care about rather than the value.
type Status struct {
	Service

	Connected bool
	Account   string
}

// Flow is one sign-in in progress: where it happens, and what it comes to.
type Flow struct {
	// The stub's own state. The owner branch replaces it with the exchange;
	// these are exported so a surface test can stand a finished one up.
	Link   string
	Result Status
	Err    error
}

// URL is where the sign-in happens.
func (f *Flow) URL() string {
	if f == nil {
		return ""
	}
	return f.Link
}

// Cancel abandons the sign-in. It is what a surface calls when the person walked
// away from it — the conversation was replaced, or a second attempt at the same
// service started — so the listener behind it does not outlive the reason for it.
func (f *Flow) Cancel() {}

// Wait blocks until the person finishes in their browser, or the context dies.
func (f *Flow) Wait(ctx context.Context) (Status, error) {
	if f == nil {
		return Status{}, errors.New("no sign-in is running")
	}
	select {
	case <-ctx.Done():
		return Status{}, ctx.Err()
	default:
	}
	return f.Result, f.Err
}
