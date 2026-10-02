package main

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// driveCloseWithin bounds the quit path's last publish and lease release, so a
// silent relay cannot hold the terminal.
const driveCloseWithin = 10 * time.Second

// syncDrives is the drive side of every conversation the chat door opened in
// this process. It exists at all only for a chat: a headless run never syncs.
var syncDrives = &driveBook{}

// driveBook keeps one drive side per cell, so a conversation opened again in the
// same process (a resume, a switch back) goes on with the drive it had instead
// of taking a second lease.
type driveBook struct {
	mu    sync.Mutex
	drive map[string]*liveDrive
}

// driveOf is the drive side of c, made on first use. It answers nil, and the
// chat runs exactly as it did before sync existed, when sync is off, or when it
// cannot start: a relay setting must never stop a conversation from opening.
// A computer with no identity yet gets a drive side that starts by itself the
// moment one is made (see liveDrive). A nil book answers nil, which is how a
// door that never syncs asks.
func (b *driveBook) driveOf(c cell.Cell, engine cellstore.Engine, report func(error)) *liveDrive {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if d := b.drive[c.ID]; d != nil && !d.superseded() {
		return d
	}
	// A drive side that no longer holds the chat's lease (another machine took it, or it was moved
	// back here) never drives again: the chat opened here needs a drive side of its own, or every
	// tool call in it would be refused as superseded.
	delete(b.drive, c.ID)
	d := newLiveDrive(func() (*syncsetup.Drive, error) { return startDrive(c, engine, report) })
	if d != nil {
		if b.drive == nil {
			b.drive = map[string]*liveDrive{}
		}
		b.drive[c.ID] = d
	}
	return d
}

// startDrive opens the drive side of c. The error it answers is already
// reported; it is returned so a caller can tell a computer that has no identity
// yet (which /pair will change) from one that cannot sync at all.
func startDrive(c cell.Cell, engine cellstore.Engine, report func(error)) (*syncsetup.Drive, error) {
	s, ok, err := syncsetup.Open(home.Dir())
	if errors.Is(err, syncsetup.ErrNoIdentity) {
		// Waiting for an identity is not a seal failing: the live drive starts by itself once one
		// is made, and until then nothing is owed to the relay. With the hosted default every
		// fresh computer is in this state, so reporting it would mark every first launch as
		// failing to seal.
		return nil, err
	}
	if err != nil || !ok {
		report(err)
		return nil, err
	}
	if syncsetup.Quiet(home.Dir()) {
		return nil, syncsetup.ErrQuiet // a fleet of one has nothing to sync with yet
	}
	if line, first := s.FirstRun(); first {
		surfaceNotices.Say(line)
	}
	d, err := s.Drive(context.Background(), engine, c, syncsetup.DriveOptions{
		DeviceName: deviceName(),
		// The title the chat has now, sealed on its way out: the other machine's
		// list names the chat by it.
		Title: func() string { return chatTitle(c) },
		// A line is said by the surface the moment it happens (tui3/notices.go).
		OnNotice: surfaceNotices.Say,
	})
	if sayRefusal(err) {
		return nil, err
	}
	report(err)
	return d, err
}

// sayRefusal tells the person what the relay refused when it refused this
// device outright (removed from the fleet, replaced, over its limit) and
// answers true, so the chat runs unsynced with the reason on screen instead
// of opening as if nothing were wrong. Any other failure is not its business.
func sayRefusal(err error) bool { return sayRefusalTo(surfaceNotices.SayOnce, err) }

func sayRefusalTo(say func(string), err error) bool {
	r, ok := cellsync.RefusalOf(err)
	if ok {
		say(r.Say(err))
	}
	return ok
}

// chatTitle is the chat's title as its session record has it now: the name it
// earned, or the person's opening words until it has one.
func chatTitle(c cell.Cell) string {
	m, _ := session.LoadMeta(c.Root)
	return m.Title
}

// closeAll ends every drive side: what is sealed is published and each lease is
// given back, so another machine can take the chat at once. It runs on the
// door's way out, after the conversations closed, so their last seal is in.
func (b *driveBook) closeAll() {
	drives := b.take()
	ctx, cancel := context.WithTimeout(context.Background(), driveCloseWithin)
	defer cancel()
	for _, d := range drives {
		if err := d.Close(ctx); err != nil {
			log.Print("sync: closing a chat: ", err)
		}
	}
}

// take hands over every drive side and forgets them.
func (b *driveBook) take() map[string]*liveDrive {
	b.mu.Lock()
	defer b.mu.Unlock()
	drives := b.drive
	b.drive = nil
	return drives
}

// driveGate is what a tool call meets before it runs: the superseded line once
// another machine has taken the chat, and nothing while this one drives it.
func driveGate(d *liveDrive) func() error {
	if d == nil {
		return nil
	}
	return d.Gate
}

// driveStore is how the seat's seals reach the drive side; nil seals on the
// engine itself.
func driveStore(d *liveDrive) func(cellstore.Engine) cellstore.Store {
	if d == nil {
		return nil
	}
	return d.Store
}
