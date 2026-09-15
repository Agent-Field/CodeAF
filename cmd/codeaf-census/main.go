// codeaf-census is the instrument the recovery design asks for: it reads the
// model-call log this build always writes (`~/.codeaf/logs/calls.jsonl`,
// internal/calllog) and prints, as markdown, the table
// docs/design/recovery/DESIGN.md §1 argues from.
//
// IT EXISTS BECAUSE EVERY WAVE OF THAT DESIGN MOVES A NUMBER, and without a
// committed instrument every one of those numbers is an argument about
// anecdotes. The first census (docs/design/recovery/census-20260910.md) was a
// throwaway pass over the same file; this is that pass, committed, tested on a
// fixture, and runnable nightly by a cron on the Spark (`make census`).
//
// It is a developer's binary and never a verb on codeaf, for the reason
// cmd/codeaf-changes and cmd/codeaf-demo-home are: the shipped binary is on a
// checked-in byte budget (SIZE-BUDGET) and a measuring tool must not spend the
// product's weight.
//
// IT READS OLD ROWS AS WELL AS NEW ONES. The log is a ten-day rolling record
// written by whichever build was running, so every reading below tolerates a
// field that is absent, and the one field this wave renamed is read under both
// of its spellings (row.go's hazardCeiling).
//
// Usage:
//
//	codeaf-census [flags] [path]
//
//	  -top N        how many error signatures to print (default 25)
//	  -days N       the window lane health is measured over (default 3)
//	  -min N        the fewest finishes a (model, machine) pair needs to be
//	                listed in lane health (default 25)
//	  -chains N     how many of the longest retry chains to print (default 10)
//
// With no path it reads the log this machine's profile writes.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/Agent-Field/codeaf/internal/calllog"
)

func main() {
	top := flag.Int("top", 25, "how many error signatures to print")
	days := flag.Int("days", 3, "the window lane health is measured over")
	min := flag.Int("min", 25, "the fewest finishes a (model, machine) pair needs to be listed")
	longest := flag.Int("chains", 10, "how many of the longest retry chains to print")
	flag.Usage = usage
	flag.Parse()

	path := flag.Arg(0)
	if strings.TrimSpace(path) == "" {
		path = calllog.PathFor("")
	}
	if strings.TrimSpace(path) == "" {
		fmt.Fprintln(os.Stderr, "codeaf-census: no call log to read (the log is switched off; name a file to read instead)")
		os.Exit(2)
	}
	rows, err := readLog(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeaf-census: %v\n", err)
		os.Exit(1)
	}
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()
	report(out, path, rows, settings{top: *top, days: *days, min: *min, longest: *longest})
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: codeaf-census [flags] [path to calls.jsonl]

Prints the recovery census (docs/design/recovery/DESIGN.md §1) as markdown.
With no path it reads this machine's own model-call log.

flags:
`)
	flag.PrintDefaults()
}

// settings is what the reader asked for, gathered so that report and its
// sections take one argument rather than four positional integers nobody can
// tell apart at a call site.
type settings struct {
	top     int
	days    int
	min     int
	longest int
}
