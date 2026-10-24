package main

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// syncListSource is the chat list's source for the headless verb. With no
// relay set it says sync is off, and with a relay but no identity it says so:
// two causes, two sentences, so the person is told what to do.
func syncListSource() (chatlist.Source, error) { return syncOf() }

// syncOf is the sync of this machine, or the sentence that says why there is
// none.
func syncOf() (*syncsetup.Sync, error) { return syncFrom(syncsetup.Open) }

// syncOfFirst is syncOf for the app's own launch, where a machine with a relay
// and no identity is the first machine and makes one.
func syncOfFirst() (*syncsetup.Sync, error) { return syncFrom(syncsetup.OpenFirst) }

func syncFrom(open func(string) (*syncsetup.Sync, bool, error)) (*syncsetup.Sync, error) {
	s, ok, err := open(home.Dir())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New(chatlist.SyncOff)
	}
	return s, nil
}
