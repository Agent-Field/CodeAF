package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/automation"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// surfaceNotices is the one desk the sync side puts its sentences on for the
// surface to say at once: another machine took the chat over, the clock is off.
// The drive side and the take side both speak through it (tui3/notices.go).
var surfaceNotices = tui3.NewNotices()

// wireSync gives the surface everything that needs a relay: the chats on other
// machines, `continue here`, and what a branch row can do. With sync off, or
// when it cannot start, it adds nothing and the home screen must not fail to
// start over a relay setting: every seam stays nil, and a nil seam is absent.
func wireSync(o *tui3.Options) {
	if o.Notices == nil {
		o.Notices = surfaceNotices
	}
	s, err := syncOfFirst()
	if err != nil {
		return
	}
	if o.Machines == nil {
		o.Machines = s
	}
	if o.Fleet == nil {
		o.Fleet = s
	}
	take := takeover{s.Continuer(cellstore.EngineFor(""), syncsetup.TakeOptions{
		DeviceName: deviceName(),
		RootFor:    takeRootFor,
		BucketFor:  takeCopyBucket,
		Notify:     surfaceNotices.Say,
	})}
	if o.Takeover == nil {
		o.Takeover = take
	}
	if o.Branches.Open == nil {
		o.Branches.Open = take.OpenBranch
	}
	if o.Branches.Discard == nil {
		o.Branches.Discard = s.Discard
	}
	guard.Go("sync/standing", func() { checkStanding(s.Home, s.Dir, surfaceNotices.SayHomeOnce) })
	guard.Go("sync/standing-live", func() { watchStanding(s.Home, s.Follow, surfaceNotices.SayHomeOnce) })
}

// standingWithin bounds the launch-time check so a silent relay costs nothing.
const standingWithin = 15 * time.Second

// checkStanding asks the directory once at launch whether this device is still
// let in, and says the refusal (removed from the fleet, replaced) as soon as the
// surface is up. A conversation that is only resumed, and so has no drive side
// yet, would otherwise open as if nothing were wrong until its first message.
// A solo identity (made here, never shared) cannot have been stopped, so it
// asks nothing: a fresh install sends the sync service no request on its own.
func checkStanding(dir string, d directory.Client, say func(string)) {
	if !syncsetup.MayTalk(dir) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), standingWithin)
	defer cancel()
	_, err := d.List(ctx)
	sayRefusalTo(say, err)
}

// watchStanding keeps listening, for as long as the app runs, for the relay
// ending this device's socket as removed or replaced, and says it the moment it
// happens, in the open chat and on home alike, without waiting for a restart or
// the next refused request. The same quiet law as checkStanding: a home that
// cannot talk to anyone holds no socket. The feed stops reconnecting after a
// refusal, so the listener ends there too.
func watchStanding(dir string, follow func() dirwatch.Follower, say func(string)) {
	if !syncsetup.MayTalk(dir) {
		return
	}
	f := follow()
	if f == nil {
		return
	}
	defer f.Close()
	for refused(f) == nil {
		if _, open := <-f.Changes(); !open {
			return
		}
	}
	sayRefusalTo(say, standingRefusal(refused(f)))
}

// refused is the final refusal the feed ended with, nil while it is not ended.
func refused(f dirwatch.Follower) error { return f.State().Refused }

// standingRefusals maps the feed's final refusals to the wire's, so the one
// table of sentences (cellsync.refusals) is the only place they are written.
var standingRefusals = []struct{ feed, wire error }{
	{dirwatch.ErrRevoked, wireauth.ErrRevoked},
	{dirwatch.ErrRotated, wireauth.ErrRotated},
}

func standingRefusal(err error) error {
	for _, r := range standingRefusals {
		if errors.Is(err, r.feed) {
			return r.wire
		}
	}
	return err
}

// takeover is the surface's Taker over the take side: what a takeover reports,
// in the shape the surface knows.
type takeover struct{ c *syncsetup.Continuer }

var _ tui3.Taker = takeover{}

func (t takeover) Take(ctx context.Context, id string) (tui3.Taken, error) {
	return t.TakeSeen(ctx, id, nil)
}

// TakeSeen carries the card's observed holder through the wire's conditional
// acquire, so its consent cannot displace a holder that arrived meanwhile.
func (t takeover) TakeSeen(ctx context.Context, id string, seen *directory.LeaseExpectation) (tui3.Taken, error) {
	began := time.Now()
	got, err := t.c.TakeSeen(ctx, id, seen)
	elapsed := time.Since(began)
	if err != nil {
		return tui3.Taken{}, err
	}
	return surfaceTaken(got, elapsed), nil
}

func (t takeover) TakeSeenWithWorkspaceRoots(ctx context.Context, id string, seen *directory.LeaseExpectation, roots map[string]string) (tui3.Taken, error) {
	began := time.Now()
	got, err := t.c.TakeSeenWithWorkspaceRoots(ctx, id, seen, roots)
	if err != nil {
		return tui3.Taken{}, err
	}
	return surfaceTaken(got, time.Since(began)), nil
}

func (t takeover) OpenBranch(ctx context.Context, id string) (tui3.Taken, error) {
	began := time.Now()
	got, err := t.c.OpenBranch(ctx, id)
	if err != nil {
		return tui3.Taken{}, err
	}
	return surfaceTaken(got, time.Since(began)), nil
}

func surfaceTaken(got syncsetup.Continued, elapsed time.Duration) tui3.Taken {
	return tui3.Taken{Kept: got.Taken.Kept, KeptTurns: got.KeptTurns, Device: got.Device, TaskCopies: got.TaskCopies, Resume: got.Resume, Transcript: filepath.Join(got.Taken.Cell.Root, cell.TranscriptPath), Elapsed: elapsed}
}

// takeRootFor is where this machine keeps the chat with an id: the folder it
// already has, found where a chat keeps it, and otherwise a folder in the
// bucket of chats that belong to no project, where a chat that arrives from
// another machine is at home.
func takeRootFor(id string) string {
	if c, err := openCellByID(id); err == nil {
		return c.Root
	}
	return filepath.Join(home.Join("v3", "projects", "copies"), id)
}

// takeCopyBucket files each copy under the folder it came from, apart from
// conversations that work in this computer's own folder of the same name.
func takeCopyBucket(origin string) string {
	return home.Join("v3", "projects", "copy-"+encodeWorkspace(origin))
}

// deviceName is what this machine is called in a sentence about it: the name a
// person gave it, else its host name.
func deviceName() string { return devname.Name(home.Dir()) }

// SafeToContinue refuses an automatic move when any local window already holds
// a conversation or this chat has work that would need its own branch (C31.3).
func (t takeover) SafeToContinue(ctx context.Context, id, current string, automatic bool) (bool, error) {
	if automatic {
		if windows, err := automation.NewPresence(automationsRoot()).Count(); err != nil || windows > 1 {
			return false, err
		}
	}
	for _, row := range session.ReadHome().Sessions() {
		if row.Open && row.ID == id && row.Transcript != current {
			log.Printf("continue %s needs a choice: open in another window (%s)", id, row.Transcript)
			return false, nil
		}
	}
	c, err := openCellByID(id)
	if err != nil {
		return true, nil
	}
	meta, err := session.LoadMeta(c.Root)
	if err != nil {
		return false, err
	}
	e := cellstore.EngineFor(meta.Workspace)
	if meta.Owned {
		e.Workspace = session.Place{Dir: c.Root, Owned: true}.Work()
	}

	dirty, err := e.LocalWork(ctx, c)
	if dirty {
		log.Printf("continue %s needs a choice: local files, conversation or tool work changed", id)
	}
	return !dirty, err
}
