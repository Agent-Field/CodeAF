//go:build !windows

// Package seniordev is senior-dev: an autonomous coding agent codeaf carries
// and runs, and nothing else can. It takes one brief, works in the folder it is
// handed — on the branch codeaf cut for it there when the folder is a git
// repository (internal/session's programfolder.go) — under a model it reaches
// only through codeaf, submits a frozen candidate,
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
// stageWords is senior-dev's stage names (app.Stages) in a person's words.
//
// `landing` IS ITS LAST CHECKS. senior-dev reports it once as it starts, and
// `implement` replaces that within milliseconds; every other time it is the
// end-of-run turn that brings its work to a state that stands and the build
// and tests it runs on the tree it leaves. Read as `starting`, the row went
// back to `starting` for the last minutes of a run.
var stageWords = map[string]string{
	"bootstrap":           "starting",
	"run-contract":        "starting",
	"intake":              "reading the brief",
	"landing":             "checking its work",
	"implement":           "working",
	"agent-runtime":       "working",
	"compaction-capacity": "working",
	"compaction":          "working",
	"router-cancellation": "working",
	"model-switch":        "working",
	"submit":              "handing in its work",
	"verification":        "checking its work",
	"ship":                "finishing",
	"patch-summary":       "finishing",
	"agent-summary":       "finishing",
}

var Program = delegate.Delegate{
	Name:    "senior-dev",
	Summary: "an autonomous agent for one large, well-specified code change",
	// What the chat's model reads before it names senior-dev in `via`. The
	// brief is copied word for word into .senior-dev/spec.md and is all it ever
	// knows of the work, so the guide says what that brief must settle. Its
	// folder is codeaf's to choose and to read: a plain one is handed over
	// with PlainFolder on the line, so the guide says nothing about git.
	//
	// IT CLAIMS THE HARD CODING WORK, BECAUSE THAT IS WHAT IT IS FOR. It said
	// "one large code change worth an hour", a price no piece of work seems to
	// clear before it is opened: an issue in a mature project whose cause runs
	// through several files reads, from its report, as a few edits the chat
	// could make itself. The owner asked on 2026-09-24 that codeaf reach for
	// senior-dev by itself on complicated, many-sided coding work, the kind a
	// hard software-engineering benchmark is made of, so the guide names that
	// work, and the paragraph it is printed under says codeaf prefers a program
	// for whatever its guide claims (internal/session/delegate_door.go). THE
	// BRIEF CARRIES THE ISSUE IN FULL: a summary of a bug report is the one
	// thing senior-dev cannot check against, since there is nobody it can ask
	// what the report said.
	Guide: "For complex, multi-part coding work: fixing an issue in a mature codebase whose cause " +
		"spans files, a feature with its tests, a rewrite across a package, a migration. Its brief " +
		"carries the issue or ask in full, what done means and how to check it, and what must not change.",
	Lands: delegate.LandsTree,
	// Its recorder is git unless it is told --in-place, which keeps its
	// checkpoints outside the folder and commits nothing. codeaf passes it for
	// every folder it works in without git — no history, or a repository at
	// the home folder — because the recorder's own reading climbs to any
	// repository around the folder.
	PlainFolder: []string{"--in-place"},
	// Where it keeps its records in the folder it works in: the brief, the
	// checklist, the pinned command, its session database and its model
	// conversation (app's seniorDevDataDirectory, which git never sees).
	Notes:     ".senior-dev",
	CrewFlags: crewFlags,
	// What a person reads for its stages, one plain word per phase: getting
	// ready, doing the work (every inner stage of a model turn included),
	// handing it in, checking it, wrapping up. Its task's row reads them only
	// until a record has named a step of its process — which its first,
	// `bootstrap`, already does — so they are the words for a run whose records
	// say no step. A test holds every stage in app.Stages to a word.
	StageWords: stageWords,
	// What each line of its action log reads as on its task's page, under the
	// step of its process it served, and the step's word its task's row reads
	// while it is in it (actions.go).
	Present:  presentActions,
	Default:  "run",
	Page:     "senior-dev",
	Commands: []delegate.Command{runCommand},
}

// crewFlags is the conversation's crew as senior-dev's own flags: the working
// seat is the pool the coder routes on (--high), and the light seat the
// history summaries (--low). --crew says the pools came from a crew, so a
// model senior-dev's catalog cannot size is left out rather than failing the
// run. A seat the crew leaves unset keeps senior-dev's own default for it.
//
// THE PLANNING SEAT IS NOT HANDED OVER, BECAUSE NOTHING WOULD USE IT. It went
// to senior-dev's frontier tier, and senior-dev routes exactly two kinds of
// call: the coder's turns on the high pool and the history summary on the low
// one (baked/tier.go). No call rides the frontier tier, so the flag changed
// nothing, while the manual told the person their mastermind model handled
// senior-dev's hardest calls.
//
// MODELS THE PERSON ASKED FOR ARE THE WORKING POOL, AND ARE KEPT AS ASKED.
// They go on --high in place of the crew's working seat, with --asked, which
// takes that pool out of the crew's leniency: a model the person named that
// senior-dev cannot size ends the run before its first call, naming it,
// rather than being quietly swapped for its own list.
func crewFlags(crew delegate.Crew) []string {
	flags := []string{"--crew"}
	high := app.CrewModel(crew.Hands)
	if len(crew.Asked) > 0 {
		asked := make([]string, 0, len(crew.Asked))
		for _, model := range crew.Asked {
			if model = app.CrewModel(model); model != "" {
				asked = append(asked, model)
			}
		}
		high = strings.Join(asked, ",")
		flags = append(flags, "--asked")
	}
	for _, seat := range []struct{ flag, model string }{
		{"--high", high}, {"--low", app.CrewModel(crew.Light)},
	} {
		if seat.model != "" {
			flags = append(flags, seat.flag, seat.model)
		}
	}
	return flags
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
	frontier := fs.String("frontier", "", "models for the frontier tier (no call uses it)")
	crew := fs.Bool("crew", false, "the models came from codeaf's crew: skip any it cannot size")
	asked := fs.Bool("asked", false, "the --high models were asked for by name; none is skipped")
	return func(ctx context.Context, host delegate.Host, args []string) error {
		run(ctx, host, app.Options{
			Goal:     strings.Join(args, " "),
			High:     *high,
			Low:      *low,
			Frontier: *frontier,
			Variant:  *variant,
			InPlace:  *inPlace,
			Crew:     *crew,
			Asked:    *asked,
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
