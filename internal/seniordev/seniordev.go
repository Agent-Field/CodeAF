//go:build !windows

// Package seniordev is senior-dev: an autonomous coding agent codeaf carries
// and runs, and nothing else can. It takes one brief, works in a working copy
// under a model it reaches only through codeaf, submits a frozen candidate,
// checks it with the project's own build and tests, and ends with one record
// that keeps what its model claimed apart from what it saw
// (internal/seniordev/app; its own account of the run is ARCHITECTURE.md in
// the repository it came from, swe-pro-go at the tag codeaf-absorb).
//
// IT HAS NO ENTRY POINT OF ITS OWN. What codeaf needs of it is a
// delegate.Delegate value, and its one command's body takes a delegate.Host,
// which only codeaf makes: `/senior-dev <brief>` in the chat, and
// `codeaf senior-dev <brief>` at a shell. There is no binary, no key it reads
// and no stdout it writes to but the host's records.
//
// ON WINDOWS IT IS ABSENT. Its engine leans on process groups, file locks and
// a bash shell it has never had a Windows form of, so every file under this
// tree carries a !windows constraint and the build's list carries nothing
// there (internal/delegate/builtin/carried_windows.go).
package seniordev

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/seniordev/app"
)

// Program is senior-dev as codeaf carries it.
var Program = delegate.Delegate{
	Name:    "senior-dev",
	Summary: "an autonomous agent for one large, well-specified code change",
	// What the chat's model reads before it names senior-dev in `via`. The
	// brief is copied word for word into .senior-dev/spec.md and is all it ever
	// knows of the work, so the guide says what that brief must settle; and
	// its recorder is git unless it runs --in-place, which the chat's line
	// never passes, so the guide says what its folder must be.
	Guide: "For one large code change worth an hour: a rewrite across a package, a migration, " +
		"a feature with its tests. Its brief names the files and commands, what done means and " +
		"how to check it, and what must not change. Its folder must be a git repository with a commit.",
	Lands:    delegate.LandsTree,
	Default:  "run",
	Page:     "senior-dev",
	Commands: []delegate.Command{runCommand},
}

// runCommand is senior-dev's one verb: the whole run, from the brief to the
// terminal record. codeaf owns --dir, --max-cost, --max-hours and --json; the
// flags here are senior-dev's own.
var runCommand = delegate.Command{
	Name:    "run",
	Usage:   "[flags] -- <brief>",
	Summary: "does one change start to finish: works, submits, checks its work",
	Bind:    bindRun,
}

// bindRun declares the run's own flags and answers the body that reads them.
func bindRun(fs *flag.FlagSet) delegate.Body {
	variant := fs.String("variant", "", "reasoning effort per call: low, medium, high or xhigh")
	inPlace := fs.Bool("in-place", false, "work without git: no commits; checkpoints kept outside")
	high := fs.String("high", app.DefaultHighModels, "models the coder routes among, comma-separated")
	low := fs.String("low", "", "models for the history summary (default: --high)")
	frontier := fs.String("frontier", "", "models for the frontier tier (default: --high)")
	return func(ctx context.Context, host delegate.Host, args []string) error {
		run(ctx, host, app.Options{
			Goal:     strings.Join(args, " "),
			High:     *high,
			Low:      *low,
			Frontier: *frontier,
			Variant:  *variant,
			InPlace:  *inPlace,
		}, os.Stderr)
		return nil
	}
}

// run is the body: hello first, the run, and exactly one terminal.
//
// A PANIC IS AN ENDING TOO. The host reads a missing terminal as work that did
// not finish and can say nothing more; a panic in the run's own goroutine is
// caught here and written as the crash it is, with its stack on stderr for
// whoever opens the task. (A panic on another of the run's goroutines ends the
// process, and the missing terminal says so.)
func run(ctx context.Context, host delegate.Host, options app.Options, notes io.Writer) {
	host.Hello(app.Stages)
	defer func() {
		if recovered := recover(); recovered != nil {
			_, _ = fmt.Fprintf(notes, "[senior-dev] panic: %v\n%s", recovered, debug.Stack())
			host.Terminal(delegate.Ending{
				Status:  delegate.StatusCrashed,
				Message: fmt.Sprintf("senior-dev broke: %v", recovered),
			})
		}
	}()
	host.Terminal(app.Run(ctx, host, options, notes))
}
