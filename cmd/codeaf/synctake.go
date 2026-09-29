package main

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui3"
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
	s, err := syncOf()
	if err != nil {
		return
	}
	if o.Machines == nil {
		o.Machines = s
	}
	if o.Takeover == nil {
		o.Takeover = takeover{s.Continuer(cellstore.EngineFor(""), syncsetup.TakeOptions{
			DeviceName: deviceName(),
			RootFor:    takeRootFor,
			Notify:     surfaceNotices.Say,
		})}
	}
	// THERE IS NO MERGE: a branch row says `discard` alone (cell_branch.go).
	if o.Branches.Discard == nil {
		o.Branches = tui3.BranchActions{Discard: s.Discard}
	}
}

// takeover is the surface's Taker over the take side: what a takeover reports,
// in the shape the surface knows.
type takeover struct{ c *syncsetup.Continuer }

var _ tui3.Taker = takeover{}

func (t takeover) Take(ctx context.Context, id string) (tui3.Taken, error) {
	got, err := t.c.Take(ctx, id)
	if err != nil {
		return tui3.Taken{}, err
	}
	return tui3.Taken{Kept: got.Taken.Kept, KeptTurns: got.KeptTurns, Device: got.Device}, nil
}

// takeRootFor is where this machine keeps the chat with an id: the folder it
// already has, found where a chat keeps it, and otherwise a folder in the
// bucket of chats that belong to no project, where a chat that arrives from
// another machine is at home.
func takeRootFor(id string) string {
	if c, err := openCellByID(id); err == nil {
		return c.Root
	}
	here, _ := os.UserHomeDir()
	return filepath.Join(home.Join("v3", "projects", encodeWorkspace(here)), id)
}

// deviceName is what this machine is called in a sentence about it.
func deviceName() string {
	name, _ := os.Hostname()
	return name
}
