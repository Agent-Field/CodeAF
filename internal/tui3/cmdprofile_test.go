package tui3

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// THIS FILE IS A MEASUREMENT AND NOT A GATE. It exists so that a claim about
// where this package's wall clock goes is a number rather than a reading of the
// code: #399 was opened on a reading that turned out to be false (the note above
// [cmdBudget] records it), and the only reason that was caught is that somebody
// counted. It is off unless AFORGE_TUI3_CMDPROFILE names a file, and when it is
// off every hook in it is one nil compare.
//
// It measures the harness, not the surface, so it lives beside [runCmd] and
// records what [runCmd] itself decides: which command was run, whether the
// command answered inside its budget or was dropped at the end of it, and how
// long the answer took when it came. The last is the question the design turns
// on — a waiter that answers in microseconds can have its waiting overlapped
// with every other waiter's, and one that answers at 140ms cannot.
var (
	cmdProfileOn bool
	cmdProfileMu sync.Mutex
	// cmdProfile is keyed by the runtime symbol behind the command, which is the
	// same key [budgetFor] prices by, so the two readings line up.
	cmdProfile = map[string]*cmdStat{}
	// cmdBatches counts batches by how many commands they held, flattened —
	// [runCmd] runs a batch's members one after another, so a batch of n parked
	// waiters costs n budgets, and this is how much that is worth.
	cmdBatches = map[int]int{}
	// cmdProfileWall is the total time spent inside [runCmd] at the top level,
	// nested batch members excluded, which is the share of the package's wall
	// clock this harness is responsible for.
	cmdProfileWall time.Duration
	cmdProfileTop  int
)

// cmdStat is one symbol's account.
type cmdStat struct {
	answered, dropped      int
	answeredWall, dropWall time.Duration
	// latency buckets for answers, in the boundaries below: one per bound and
	// one for everything above the last.
	buckets [cmdLatencyBuckets]int
	slowest time.Duration
}

// cmdLatencyBounds are the edges of the answer-latency histogram. They are
// chosen around the decision: everything in the first bucket is an answer that
// was already sitting in a buffered channel, and everything in the last is an
// answer that really waited on a timer or a scheduler.
const cmdLatencyBuckets = 7

var cmdLatencyBounds = [cmdLatencyBuckets - 1]time.Duration{
	100 * time.Microsecond,
	time.Millisecond,
	5 * time.Millisecond,
	20 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
}

// startCmdProfile arms the profile if the environment asks for one.
func startCmdProfile() bool {
	cmdProfileOn = os.Getenv("AFORGE_TUI3_CMDPROFILE") != ""
	return cmdProfileOn
}

// noteCommand records one command [runCmd] ran to a decision.
func noteCommand(cmd tea.Cmd, took time.Duration, answered bool) {
	if !cmdProfileOn {
		return
	}
	symbol := cmdSymbol(cmd)
	cmdProfileMu.Lock()
	defer cmdProfileMu.Unlock()
	stat := cmdProfile[symbol]
	if stat == nil {
		stat = &cmdStat{}
		cmdProfile[symbol] = stat
	}
	if !answered {
		stat.dropped++
		stat.dropWall += took
		return
	}
	stat.answered++
	stat.answeredWall += took
	if took > stat.slowest {
		stat.slowest = took
	}
	at := sort.Search(len(cmdLatencyBounds), func(i int) bool { return took < cmdLatencyBounds[i] })
	stat.buckets[at]++
}

// noteBatch records that one command answered with a batch of n members.
func noteBatch(n int) {
	if !cmdProfileOn {
		return
	}
	cmdProfileMu.Lock()
	defer cmdProfileMu.Unlock()
	cmdBatches[n]++
}

// noteTopLevel records one whole top-level [runCmd], batch members included, so
// the total is the harness's own share of the package's wall clock.
func noteTopLevel(took time.Duration) {
	if !cmdProfileOn {
		return
	}
	cmdProfileMu.Lock()
	defer cmdProfileMu.Unlock()
	cmdProfileWall += took
	cmdProfileTop++
}

// writeCmdProfile dumps the account, worst first. TestMain calls it on the way
// out; a failure to write is printed and otherwise ignored, because a broken
// measurement must never fail a run.
func writeCmdProfile() {
	if !cmdProfileOn {
		return
	}
	path := os.Getenv("AFORGE_TUI3_CMDPROFILE")
	cmdProfileMu.Lock()
	defer cmdProfileMu.Unlock()

	type row struct {
		symbol string
		*cmdStat
	}
	rows := make([]row, 0, len(cmdProfile))
	var totalDropped, totalAnswered int
	var totalDropWall, totalAnsweredWall time.Duration
	for symbol, stat := range cmdProfile {
		rows = append(rows, row{symbol, stat})
		totalDropped += stat.dropped
		totalAnswered += stat.answered
		totalDropWall += stat.dropWall
		totalAnsweredWall += stat.answeredWall
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].dropWall != rows[j].dropWall {
			return rows[i].dropWall > rows[j].dropWall
		}
		return rows[i].symbol < rows[j].symbol
	})

	var out strings.Builder
	fmt.Fprintf(&out, "# tui3 command profile\n")
	fmt.Fprintf(&out, "commands\t%d\ttop-level\t%d\n", totalAnswered+totalDropped, cmdProfileTop)
	fmt.Fprintf(&out, "answered\t%d\twall\t%s\n", totalAnswered, totalAnsweredWall)
	fmt.Fprintf(&out, "dropped\t%d\twall\t%s\n", totalDropped, totalDropWall)
	fmt.Fprintf(&out, "runCmd-wall\t%s\n", cmdProfileWall)

	sizes := make([]int, 0, len(cmdBatches))
	for n := range cmdBatches {
		sizes = append(sizes, n)
	}
	sort.Ints(sizes)
	for _, n := range sizes {
		fmt.Fprintf(&out, "batch\t%d\t%d\n", n, cmdBatches[n])
	}

	fmt.Fprintf(&out, "\n%-64s %8s %12s %8s %12s %10s  %s\n",
		"symbol", "dropped", "drop-wall", "answered", "answer-wall", "slowest", "answer-latency-histogram")
	for _, r := range rows {
		hist := make([]string, 0, len(r.buckets))
		for i, n := range r.buckets {
			label := "+"
			if i < len(cmdLatencyBounds) {
				label = "<" + cmdLatencyBounds[i].String()
			}
			if n > 0 {
				hist = append(hist, fmt.Sprintf("%s=%d", label, n))
			}
		}
		fmt.Fprintf(&out, "%-64s %8d %12s %8d %12s %10s  %s\n",
			r.symbol, r.dropped, r.dropWall.Round(time.Millisecond), r.answered,
			r.answeredWall.Round(time.Millisecond), r.slowest.Round(time.Microsecond),
			strings.Join(hist, " "))
	}
	if err := os.WriteFile(path, []byte(out.String()), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "tui3: could not write the command profile: %v\n", err)
	}
}
