package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// sweepLogName is where a failed sweep says so.
//
// IT IS A FILE AND NEVER THE SCREEN. The pass runs while a surface is taking
// over the terminal, so a line on stderr is either scrolled past before anybody
// reads it or painted through a frame the surface has already drawn. Nothing
// here is a person's problem either — a log that could not be expired is a log
// that will be expired next week — so the honest destination is a file an
// operator can read afterwards and nobody else ever has to.
const sweepLogName = "sweep.log"

// startPlaceSweep runs the session-folder sweep once per process, in the
// background, and NEVER BLOCKS THE LAUNCH.
//
// A person opening a conversation is waiting for a prompt, not for housekeeping:
// the pass walks every session folder on the machine and stats every dropping in
// them, which is milliseconds on a laptop that has held ten conversations and a
// visible pause on one that has held a thousand. So it is a goroutine, it is
// started and forgotten, and a launch that exits before it finishes has simply
// swept nothing this time.
//
// The once is for the doors, not for a schedule: `aforge chat` and `aforge
// resume` are two entrances to one launch, and a process that came through both
// should still sweep once.
//
// STUB(place/layout): the layout lane owns chatv3.go, so the call sits in
// main.go's v3 doors for now. It belongs inside openV3Launch, beside the rest of
// what a v3 launch resolves, and moving it there is one line at the merge.
func startPlaceSweep() {
	sweepOnce.Do(func() { go session.SweepHome(noteSweep) })
}

var sweepOnce sync.Once

// noteSweep writes one line, and opens the file only when there is a line to
// write: a clean sweep — which is every sweep on a machine that is behaving —
// leaves nothing behind at all.
func noteSweep(line string) {
	path := home.Join("v3", sweepLogName)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), line)
}
