package main

import (
	"context"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// FactoryForemanOpen is factory.foreman_open: the foreman conversation of the
// factory this engine serves, made the first time ([foremanMaker]) and the same
// one every time after, held open so the app draws it at once. Its floor is
// this host's own store, the factory home it belongs to and no other; a factory
// this engine does not serve is refused, as its job chats are.
//
// THE DOOR IS THE PERSON'S OWN OPEN. They pressed the Factory manager's own
// button, so a manager they closed or archived comes out of Closed here: left
// as it was, the button opened a conversation that could not be written to,
// with no other door to a manager at all.
func (b *appBackend) FactoryForemanOpen(ctx context.Context, p appwire.FactoryRef) (appwire.FactoryForeman, error) {
	return b.foremanOpened(ctx, p, foremanMaker)
}

// FactoryForemanFresh is factory.foreman_fresh: the manager started over. A
// new conversation takes the manager's place ([foremanFresh]) and is answered
// held open, as an open answers it; the one it replaces is stopped and put
// away, its history kept on disk.
func (b *appBackend) FactoryForemanFresh(ctx context.Context, p appwire.FactoryRef) (appwire.FactoryForeman, error) {
	return b.foremanOpened(ctx, p, foremanFresh)
}

// foremanOpened answers the foreman a maker names, held open and the
// factory's. A foreman the maker REPLACED (a fresh start, or one moved out of
// a folder it may not work in) is stopped and put away once the new one is
// named, so nothing is left running behind a conversation no door opens.
func (b *appBackend) foremanOpened(ctx context.Context, p appwire.FactoryRef, maker func(*store.Store, string, string, func() (factory.Snapshot, error)) func(context.Context) (string, error)) (appwire.FactoryForeman, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryForeman{}, err
	}
	load := host.seam.Load
	if load == nil {
		load = factory.LocalSeam(host.floor, time.Now(), factoryPipelineOption(host.floor)).Load
	}
	before, _ := host.floor.Foreman()
	transcript, err := maker(host.floor, host.workspace, host.profile, load)(ctx)
	if err != nil {
		return appwire.FactoryForeman{}, appwire.Errorf(appwire.CodeConflict, "the factory's manager could not be opened: %s", err)
	}
	if before != "" && !sameSessionFile(before, transcript) {
		b.putAwayForeman(before)
	}
	id := appAgentID(transcript)
	a, err := b.find(id)
	if err != nil {
		return appwire.FactoryForeman{}, err
	}
	// The factory's before anything is said of it, so the app never draws the
	// manager among the person's own chats for the moment between the two.
	b.locked(func() { a.view.FactoryOwned = true })
	if b.tidied(id) {
		if _, err := b.Reopen(id); err != nil {
			return appwire.FactoryForeman{}, err
		}
	}
	if _, err := b.holdJobChat(id); err != nil {
		return appwire.FactoryForeman{}, err
	}
	return appwire.FactoryForeman{Factory: p.Factory, Agent: id}, nil
}

// putAwayForeman stops a replaced foreman's turn and archives its
// conversation: its history stays on disk and in Archived, and nothing of it
// runs on. It is best effort. The new foreman is already the factory's, and a
// conversation that is gone or cannot be closed is no reason to refuse it.
func (b *appBackend) putAwayForeman(transcript string) {
	if _, err := os.Stat(transcript); err != nil {
		return
	}
	id := appAgentID(transcript)
	if _, err := b.find(id); err != nil {
		return
	}
	_ = b.Stop(id)
	_ = b.Archive([]string{id})
}
