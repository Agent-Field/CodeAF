// aforge-replay judges a chooser by REGRET ON TEN DAYS OF LOG rather than by a
// screenshot.
//
// IT EXISTS BECAUSE WE WERE TUNING A BANDIT BY ANECDOTE. Every change to the
// machine chooser in the week to 2026-09-11 — #850, #853, #873 and two lanes
// still in flight — was argued from one person's screenshot and a ten-row grep,
// and every one of them moved a number nobody then measured. The model-call log
// already holds about a thousand finished requests a day: which machine was
// asked for, which one answered, how long its first token took, how fast it
// wrote, whether it refused, and what the call was for. That is a bandit replay
// dataset, and this is the instrument that reads it as one.
//
// WHAT IT DOES, in one paragraph. It walks the log in time order. Every finished
// request becomes a question — "on this model, at this moment, for this role,
// which machine would you have demanded?" — put to each candidate policy, whose
// beliefs have been fed exactly the sightings and refusals that had arrived by
// then and not one that had not. The answer is priced against what that machine
// measurably did around that moment (world.go), the best machine available is
// priced the same way, and the difference is the regret. The table it prints is
// per role class, because a second of a watched answer and a second of an
// unattended errand are not the same second.
//
// IT IS A DEVELOPER'S BINARY AND NEVER A VERB ON aforge, for the reason
// cmd/aforge-census and cmd/aforge-changes are: the shipped binary is on a
// checked-in byte budget (SIZE-BUDGET) and a measuring tool must not spend the
// product's weight.
//
// Usage:
//
//	aforge-replay [flags]
//
//	  -log PATH         the model-call log (default: this machine's own)
//	  -sightings PATH   the lane observation journal (default: beside the belief file)
//	  -since DATE       ignore rows before this RFC3339 prefix
//	  -window D         how far either side of a request a machine's own answers
//	                    are read for what it was doing then (default 15m)
//	  -min N            the fewest machines a model needs before its requests are
//	                    scored at all (default 2 — nothing to choose between)
//	  -incidents N      how many of the worst moments to show every candidate at
//	  -out PATH         write the report to a file instead of stdout
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/calllog"
	"github.com/Agent-Field/aforge-v2/internal/callrows"
	"github.com/Agent-Field/aforge-v2/internal/lane"
)

func main() {
	log := flag.String("log", "", "the model-call log to replay (default: this machine's own)")
	sightings := flag.String("sightings", "", "the lane observation journal (default: beside the belief file)")
	since := flag.String("since", "", "ignore rows whose stamp sorts before this prefix")
	window := flag.Duration("window", defaultWindow, "how far either side of a request a machine's own answers are read")
	min := flag.Int("min", 2, "the fewest machines a model needs before its requests are scored")
	incidents := flag.Int("incidents", 3, "how many of the worst moments to show every candidate at")
	out := flag.String("out", "", "write the report here instead of to stdout")
	flag.Usage = usage
	flag.Parse()

	path := strings.TrimSpace(*log)
	if path == "" {
		path = calllog.PathFor("")
	}
	if path == "" {
		fmt.Fprintln(os.Stderr, "aforge-replay: no call log to read (the log is switched off; name one with -log)")
		os.Exit(2)
	}
	rows, err := callrows.Read(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aforge-replay: %v\n", err)
		os.Exit(1)
	}
	rows = from(rows, *since)

	journal := strings.TrimSpace(*sightings)
	if journal == "" {
		journal = journalBeside(lane.StorePath())
	}
	seen, missing := readJournal(journal)

	writer := os.Stdout
	if strings.TrimSpace(*out) != "" {
		file, err := os.Create(*out)
		if err != nil {
			fmt.Fprintf(os.Stderr, "aforge-replay: %v\n", err)
			os.Exit(1)
		}
		defer file.Close()
		writer = file
	}
	buffered := bufio.NewWriter(writer)
	defer buffered.Flush()

	look := settings{window: *window, min: *min, incidents: *incidents, log: path, journal: journal, journalMissing: missing}
	report(buffered, look, rows, seen)
	if strings.TrimSpace(*out) != "" {
		fmt.Fprintf(os.Stderr, "the replay is in %s\n", *out)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: aforge-replay [flags]

Replays the model-call log through every candidate chooser and prints, as
markdown, the regret each one would have paid (docs/design/recovery/DESIGN.md §8).

flags:
`)
	flag.PrintDefaults()
}

// settings is what the reader asked for, gathered so that the report and its
// sections take one argument rather than six positional ones nobody can tell
// apart at a call site.
type settings struct {
	// window is how far either side of a request this program is willing to read
	// a machine's own answers as evidence of what it was doing then. It is the
	// one free parameter of the whole instrument and it is REPORTED beside every
	// number it produced, because a regret is a statement about a counterfactual
	// and the width of the window is the whole of what makes it estimable.
	window time.Duration
	// min is the fewest machines a model must have been seen to have before its
	// requests are scored. Below two there is nothing to choose between and a
	// regret of zero would be arithmetic rather than a finding.
	min int
	// incidents is how many of the worst moments the report shows every
	// candidate at.
	incidents int

	log            string
	journal        string
	journalMissing bool
}

// defaultWindow is fifteen minutes, and it is the belief's own forgetting rather
// than a round number: [lane.HalfLife] is ten minutes, so evidence a window's
// width away from a request has already been discounted to about a third of its
// weight by the time the request is made. A window much wider than that is
// reading a machine's afternoon as though it were its minute.
const defaultWindow = 15 * time.Minute

// from drops every row that sorts before a prefix the reader named. The
// comparison is on the stamp as it is SPELLED, so `-since 2026-09-11` and
// `-since 2026-09-11T14:` are both legal and neither needs a layout.
func from(rows []callrows.Row, since string) []callrows.Row {
	since = strings.TrimSpace(since)
	if since == "" {
		return rows
	}
	kept := rows[:0]
	for _, row := range rows {
		if row.Time >= since {
			kept = append(kept, row)
		}
	}
	return kept
}

// journalBeside is the observation log next to a belief file — `lanes.json` and
// `lanes.log` in one directory, which is the pairing internal/lane's journal.go
// makes and this reads back.
func journalBeside(state string) string {
	if strings.TrimSpace(state) == "" {
		return ""
	}
	if trimmed := strings.TrimSuffix(state, ".json"); trimmed != state {
		return trimmed + ".log"
	}
	return ""
}
