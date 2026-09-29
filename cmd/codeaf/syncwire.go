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
func syncListSource() (chatlist.Source, error) {
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New(chatlist.SyncOff)
	}
	return s, nil
}

// syncMachines is the home screen's source for other machines' chats. It is
// nil, which draws nothing extra, when sync is off or cannot be opened: the
// home screen must not fail to start over a relay setting.
func syncMachines() chatlist.Source {
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		return nil
	}
	return s
}
