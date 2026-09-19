package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

// sweepLogName is where a failed sweep says so.
//
// IT IS A FILE AND NEVER THE SCREEN. The pass runs while a surface is taking
// over the terminal, so a line on stderr is either scrolled past before anybody
// reads it or painted through a frame the surface has already drawn. Nothing
// here is a person's problem either, a log that could not be expired is a log
// that will be expired next week, so the honest destination is a file an
// operator can read afterwards and nobody else ever has to.
const sweepLogName = "sweep.log"

// startPlaceSweep runs the session-folder sweep once per process, in the
// background, and NEVER BLOCKS THE LAUNCH.
//
// A person opening a conversation is waiting for a prompt, not for housekeeping:
// the pass walks every session folder on the machine and stats every dropping in
// them, which is milliseconds on a laptop that has held ten conversations and a
// visible pause on one that has held a thousand. So it is a goroutine and does
// not block launch. Each v3Process owns one pass. Its close cancels the contextual
// walk and joins the goroutine, whose cancellation checks bound the join to the
// filesystem operation already in flight.
//
// The call sits after successful openV3Process construction, so failed doors do
// not start housekeeping and every later process in the same binary gets a pass.
//
// THE STANDING ROOT IS HANDED OVER RATHER THAN FOUND. The same pass reaps the
// ambient side's own litter, an errand that came to nothing, a run that
// delivered nothing, and internal/session is given the directory to walk so
// that the rule can be pointed at a temp directory and proved. [v3StandingRoot]
// is the one answer to where that is.
var sweepHome func(context.Context, string, func(string)) = session.SweepHomeContext

func (p *v3Process) startPlaceSweep() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	p.sweepCancel = cancel
	p.sweepDone = done
	guard.Go("chatv3/sweep-home", func() {
		defer close(done)
		sweepHome(ctx, v3StandingRoot(), func(line string) {
			if ctx.Err() == nil {
				noteSweep(line)
			}
		})
	})
}

func (p *v3Process) stopPlaceSweep() {
	if p.sweepCancel == nil {
		return
	}
	p.sweepCancel()
	<-p.sweepDone
}

// noteSweep writes one line, and opens the file only when there is a line to
// write: a clean sweep, which is every sweep on a machine that is behaving,
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
