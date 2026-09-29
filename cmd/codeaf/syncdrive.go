package main

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
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
	drive map[string]*syncsetup.Drive
}

// driveOf is the drive side of c, made on first use. It answers nil, and the
// chat runs exactly as it did before sync existed, when sync is off, or when it
// cannot start: a relay setting must never stop a conversation from opening.
// A nil book answers nil, which is how a door that never syncs asks.
func (b *driveBook) driveOf(c cell.Cell, engine cellstore.Engine, report func(error)) *syncsetup.Drive {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if d := b.drive[c.ID]; d != nil {
		return d
	}
	d := startDrive(c, engine, report)
	if d != nil {
		if b.drive == nil {
			b.drive = map[string]*syncsetup.Drive{}
		}
		b.drive[c.ID] = d
	}
	return d
}

func startDrive(c cell.Cell, engine cellstore.Engine, report func(error)) *syncsetup.Drive {
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil || !ok {
		report(err)
		return nil
	}
	name, _ := os.Hostname()
	d, err := s.Drive(context.Background(), engine, c, syncsetup.DriveOptions{
		DeviceName: name,
		// The surface owns the terminal, so a line goes where the logger does.
		OnNotice: func(line string) { log.Print("sync: " + line) },
	})
	report(err)
	return d
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
func (b *driveBook) take() map[string]*syncsetup.Drive {
	b.mu.Lock()
	defer b.mu.Unlock()
	drives := b.drive
	b.drive = nil
	return drives
}

// driveGate is what a tool call meets before it runs: the superseded line once
// another machine has taken the chat, and nothing while this one drives it.
func driveGate(d *syncsetup.Drive) func() error {
	if d == nil {
		return nil
	}
	return d.Gate
}

// driveStore is how the seat's seals reach the drive side; nil seals on the
// engine itself.
func driveStore(d *syncsetup.Drive) func(cellstore.Engine) cellstore.Store {
	if d == nil {
		return nil
	}
	return d.Store
}
