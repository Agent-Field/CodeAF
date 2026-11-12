package main

import (
	"context"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
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

// foremanDoor is one way of naming the foreman: the open's or the fresh
// start's.
type foremanDoor func(st *store.Store, workspace, profileDir string, load func() (factory.Snapshot, error)) func(context.Context) (string, error)

// foremanOpened answers the foreman a door names, held open and the
// factory's.
//
// ONE AT A TIME. Two opens that both found no foreman, or both found one in a
// refused folder, each made one: the second was the store's and the first was
// left held, the factory's by its flag, with no floor behind it and no door
// that would ever reach it again. The local factory's gate makes the naming
// and the hold one act, so a second caller meets the first one's foreman.
//
// THE NEW ONE IS ANSWERED BEFORE THE OLD ONE IS PUT AWAY. A foreman the door
// REPLACED (a fresh start, or one moved out of a folder it may not work in) is
// stopped and archived on a goroutine of its own once the new one is held:
// closing a conversation whose turn will not let go waits out its graces, and
// the app's call would time out behind them with the new manager already made
// and nobody told.
func (b *appBackend) foremanOpened(ctx context.Context, p appwire.FactoryRef, door foremanDoor) (appwire.FactoryForeman, error) {
	host, err := b.localHostOf(p.Factory)
	if err != nil {
		return appwire.FactoryForeman{}, err
	}
	gate := &b.local.foreman
	gate.Lock()
	defer gate.Unlock()
	before, _ := host.floor.Foreman()
	transcript, err := door(host.floor, host.workspace, host.profile, foremanLoad(host))(ctx)
	if err != nil {
		return appwire.FactoryForeman{}, appwire.Errorf(appwire.CodeConflict, "the factory's manager could not be opened: %s", err)
	}
	id := appAgentID(transcript)
	a, err := b.find(id)
	if err != nil {
		return appwire.FactoryForeman{}, err
	}
	// The factory's before anything is said of it, so the app never draws the
	// manager among the person's own chats for the moment between the two.
	flipped := false
	b.locked(func() { flipped, a.view.FactoryOwned = !a.view.FactoryOwned, true })
	if b.tidied(id) {
		if _, err := b.Reopen(id); err != nil {
			return appwire.FactoryForeman{}, err
		}
	}
	if _, err := b.holdJobChat(id); err != nil {
		return appwire.FactoryForeman{}, err
	}
	// A CONVERSATION THE APP ALREADY HELD OPEN IS TOLD IT IS THE FACTORY'S. The
	// hold above says an agent only when it had to open it, and a tab brought
	// back by a relaunch had opened this one as an ordinary chat: until the
	// app heard otherwise it went on offering to archive the manager.
	if flipped {
		b.sayAgent(a)
	}
	if before != "" && !sameSessionFile(before, transcript) {
		guard.Go("app/factory foreman put away", func() { b.putAwayForeman(before) })
	}
	return appwire.FactoryForeman{Factory: p.Factory, Agent: id}, nil
}

// foremanLoad is the floor's read a foreman's making asks for its brief's
// policy lines: the host's own seam when it has one.
func foremanLoad(host *appFactoryHost) func() (factory.Snapshot, error) {
	if load := host.seam.Load; load != nil {
		return load
	}
	return factory.LocalSeam(host.floor, time.Now(), factoryPipelineOption(host.floor)).Load
}

// settleForeman puts the factory's foreman right AS THE FACTORY STARTS, before
// anybody opens it.
//
// THE OPEN IS NOT THE ONLY ROAD TO A FOREMAN. The app brings back the tabs it
// had, and resumes a reply the last quit cut: both reach the conversation by
// its id and never through factory.foreman_open. So a foreman still working in
// a folder the factory refuses was written to in that folder again, and one
// made before the foreman's mark existed came back as one of the person's own
// chats, with Archive offered on it. Here, once per start:
//
//   - a foreman in a refused folder is made again in its own, and the old one
//     stopped and archived, so the tab that comes back holds an archived chat
//     and the manager's button opens the new one;
//   - any other foreman takes the mark, and the app is told it is the
//     factory's if it had already read it as an ordinary chat;
//   - a conversation still marked the foreman's that the store no longer
//     names (a making cut short) is retired and put away.
//
// A store that names no foreman is left alone: one is never made by default.
//
// IT WRITES THE FLOOR ONLY UNDER THE HOST. The naming is a chore of the host
// ([appFactoryHost.chore]), so it never starts once a close has begun and a
// close waits for it; putting the replaced conversations away is not, since a
// conversation that will not let go would hold the close for its graces, and
// what it writes is the person's chats, never the floor.
func (b *appBackend) settleForeman(host *appFactoryHost) {
	host.chore("app/factory foreman settle", func() {
		away := b.settleForemanNow(host)
		if len(away) > 0 {
			guard.Go("app/factory foreman put away", func() {
				for _, old := range away {
					b.putAwayForeman(old)
				}
			})
		}
	})
}

// settleForemanNow is the naming half of [appBackend.settleForeman], under the
// foreman's gate: it answers the conversations that were retired and are the
// caller's to put away.
func (b *appBackend) settleForemanNow(host *appFactoryHost) (away []string) {
	gate := &b.local.foreman
	gate.Lock()
	defer gate.Unlock()
	away = foremanSweep(host.floor, host.profile)
	chat, err := host.floor.Foreman()
	if err != nil || chat == "" {
		return away
	}
	if _, err := os.Stat(chat); err != nil {
		return away
	}
	if foremanFolderRefusal(host.floor, chat) == nil {
		_ = markFactoryForeman(host.floor, chat)
		b.ownForeman(chat)
		return away
	}
	if _, err := foremanMint(host.floor, host.workspace, host.profile, foremanLoad(host)); err == nil {
		away = append(away, chat)
	}
	return away
}

// ownForeman tells the app a conversation it already knows is the factory's,
// when this engine had read it as an ordinary chat. One the engine has not
// read yet needs nothing said: its mark is on disk and its first reading
// carries it ([factoryOwnedConversation]).
func (b *appBackend) ownForeman(transcript string) {
	id := appAgentID(transcript)
	var a *appAgent
	flipped := false
	b.locked(func() {
		if a = b.agents[id]; a != nil && !a.view.FactoryOwned {
			a.view.FactoryOwned, flipped = true, true
		}
	})
	if flipped {
		b.sayAgent(a)
	}
}

// putAwayForeman stops a replaced foreman's turn and archives its
// conversation. It is no longer the factory's: its mark is gone and the app is
// told, so it reads as one of the person's own chats, in Archived with its
// history, under the name [foremanEarlierTitle] when it still carried the
// manager's. It is best effort. The new foreman is already the factory's, and
// a conversation that is gone or will not close is no reason to refuse that
// one; what could not be archived is left open and is the person's to close.
//
// THE NAME IS WRITTEN THROUGH THE CONVERSATION. A title lives in the journal,
// and an open reads it from there: the name [foremanRetire] leaves in the
// folder's row alone was lost the first time the conversation was opened
// again, which is exactly when it matters, since a tab the app brought back
// may still hold this conversation and a person may write to it there.
func (b *appBackend) putAwayForeman(transcript string) {
	if _, err := os.Stat(transcript); err != nil {
		return
	}
	id := appAgentID(transcript)
	a, err := b.find(id)
	if err != nil {
		return
	}
	rename := false
	b.locked(func() {
		a.view.FactoryOwned = false
		rename = a.view.Title == foremanTitle || a.view.Title == foremanOldTitle || a.view.Title == foremanEarlierTitle
	})
	if rename {
		if _, err := b.Rename(id, foremanEarlierTitle); err != nil {
			// It could not be opened to be renamed (no provider, another
			// window holds it): this engine still says the earlier name.
			b.locked(func() { a.view.Title = foremanEarlierTitle })
		}
	}
	_ = b.Stop(id)
	if err := b.Archive([]string{id}); err != nil {
		// Archive says the agent when it succeeds; a refusal still has to
		// say it is the person's now.
		b.sayAgent(a)
	}
}
