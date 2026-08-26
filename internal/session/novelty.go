package session

// Line-level novelty: how much a result actually told the node, and how long it
// has been since the work itself moved.
//
// ── WHY A LINE AND NOT A RESULT ──
//
// The no-progress counter (task_run.go) asks one question of every step: did
// this teach the node anything it did not already know? The answer used to be a
// hash of the WHOLE result, which makes the question binary and makes it easy to
// answer yes by accident. One line of a sixteen-line result changing — a
// timestamp, a run counter, an elapsed time — makes the other fifteen new again,
// and the counter that exists to notice a node getting nowhere resets.
//
// MEASURED (a peer harness on the same benchmark family, and the same shape is
// reachable here): for two hours a worker rewrote its own measuring script four
// times and re-ran it six times against a deliverable it never touched. Every
// run printed `Starting at <date>` on its first line, so a whole-result detector
// saw six productive steps in a row while the score under it did not move by a
// point. A per-line estimator sees two new lines in sixteen and calls that what
// it is.
//
// So novelty is a RATIO: how many of a result's lines the node has never been
// told, over how many lines it has. It needs no list of volatile tokens, no rule
// about timestamps, and no idea what kind of work is being done — a line either
// came back before or it did not.
//
// ── AND WHY THE WORK'S OWN CLOCK IS BESIDE IT ──
//
// The ratio says how much the node is learning. It says nothing about whether
// the DELIVERABLE is moving, and the loop this file was written for is one where
// both answers are needed: the node was learning a little (new timings every
// run) about a thing it had stopped changing. [workClock] holds that structural
// fact — the step at which the work last changed, and what has happened since —
// and it is stated in words rather than acted on. NOTHING STOPS ON IT: the only
// threshold that ends anything is still the no-progress counter, now fed by the
// honest estimator above.

import (
	"fmt"
	"hash/fnv"
	"strings"
)

// taughtLineThreshold is how much of a result must be new before the step that
// fetched it counts as having taught the node something.
//
// ONE HALF, and the number is a judgement rather than a measurement, so here is
// the judgement. The two failures it sits between are not symmetric:
//
//   - TOO LOW (a tenth) and the estimator is the old one with extra arithmetic.
//     A sixteen-line result with a clock in it clears a tenth on the clock line
//     alone, which is exactly the defect above.
//   - TOO HIGH (nine tenths) and a real discovery is called a spin. `go test`
//     after a fix reprints most of its output and changes the four lines that
//     matter; a directory listing grows by one file; a log is tailed and has
//     three new lines at the end. All of those are the node finding out what it
//     went to find out, and all of them are mostly old bytes.
//
// A half says: MORE OF THIS ANSWER IS NEW THAN IS OLD. It is the one point on
// the scale that needs no argument about how much repetition is acceptable,
// because it is the point where the result stops being mostly a repeat. A
// one-line result that is new clears it outright (1 of 1), which is the case
// that must not be lost — a single new line is information.
//
// IT IS A CONSTANT AND NOT A SETTING. A threshold a task could raise would be a
// threshold a stuck task raises.
const taughtLineThreshold = 0.5

// noveltyGeneration is how many line hashes one generation of the memory holds
// before it is rotated. The memory keeps two generations, so a node remembers
// between one and two of these and never more.
//
// BOUNDED BECAUSE A LONG RUN IS THE ORDINARY CASE. A node at its step limit can
// read a hundred thousand lines, and a set that grew with every one of them
// would be a leak whose size is decided by how much output the world happens to
// produce. Four thousand lines is many times the window any of this reasons
// over — a repeat that comes back four thousand distinct lines later is not the
// repetition the counter is looking for.
const noveltyGeneration = 4096

// lineNovelty is one node's memory of the lines it has been told.
//
// TWO GENERATIONS AND NOT AN EVICTION QUEUE. The exact identity of the oldest
// remembered line does not matter — what matters is that recent lines are all
// there and that the memory has a ceiling. A rotation gives both for two maps
// and no bookkeeping; an LRU would give the same answer at several times the
// cost, to a question nobody asks.
//
// THE TOOL IS PART OF THE KEY, exactly as it was when this was a whole-result
// hash (task_run.go's [taughtSomething]): two hands that happen to answer the
// same bytes are two ways of learning the same thing, and the counter has always
// judged them per hand.
type lineNovelty struct {
	recent map[uint64]bool
	older  map[uint64]bool
}

func newLineNovelty() *lineNovelty {
	return &lineNovelty{recent: make(map[uint64]bool, 64)}
}

// measure reports how many of this result's lines the memory has never held, and
// how many lines it weighed — and RECORDS THEM AS IT ANSWERS, so a line cannot
// be spent as new twice.
//
// NORMALISATION IS TRAILING WHITESPACE AND NOTHING ELSE. A shell that pads its
// columns and one that does not are printing the same line; every other
// difference is a difference in what was said, and a harness that decided which
// of them were unimportant would be back to keeping a list of the tokens it
// thinks are noise.
//
// BLANK LINES ARE NOT WEIGHED AT ALL. They carry nothing, they are the same
// blank line everywhere, and counting them would let a result of one new line
// and three blanks read as a quarter new.
func (m *lineNovelty) measure(tool, text string) (fresh, lines int) {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t\r\v\f")
		if line == "" {
			continue
		}
		lines++
		if m.remember(tool, line) {
			fresh++
		}
	}
	return fresh, lines
}

// remember records one line and reports whether it was new.
func (m *lineNovelty) remember(tool, line string) bool {
	if m == nil {
		return true
	}
	digest := fnv.New64a()
	_, _ = digest.Write([]byte(tool))
	_, _ = digest.Write([]byte{0})
	_, _ = digest.Write([]byte(line))
	key := digest.Sum64()

	if m.recent == nil {
		m.recent = make(map[uint64]bool, 64)
	}
	if m.recent[key] || m.older[key] {
		return false
	}
	if len(m.recent) >= noveltyGeneration {
		m.older, m.recent = m.recent, make(map[uint64]bool, 64)
	}
	m.recent[key] = true
	return true
}

// informative reports whether one result carried more new lines than old ones,
// and records every line either way.
//
// AN EMPTY RESULT IS NOT INFORMATION. `(no output)` is a real answer the first
// time — it has a line — but a result with no lines at all told the node
// nothing, and reading it as a discovery is how a silent poll used to look
// productive.
func (m *lineNovelty) informative(tool, text string) bool {
	fresh, lines := m.measure(tool, text)
	if lines == 0 {
		return false
	}
	return float64(fresh)/float64(lines) >= taughtLineThreshold
}

// ── the work's own clock ─────────────────────────────────────────────────────

// workClock is the structural fact, kept per turn: THE STEP AT WHICH THE
// DELIVERABLE LAST CHANGED, and what has happened since.
//
// It is a description and never a rule. Nothing here stops anything, nothing
// here is a threshold, and the two places that read it both put it into words a
// person or a model reads (looped.go's [stuck] note, checkpoint.go's digest).
// The measure→measure loop is visible in one sentence — the work has not moved
// since step 12, and the nine results since brought nothing new — without the
// harness having any opinion about which kinds of work are allowed to look like
// that.
type workClock struct {
	// steps is how many tool calls have been counted.
	steps int
	// changedAt is the step at which the work last changed, 0 for never.
	changedAt int
	// since is how many results have come back since then, and fresh/lines are
	// how much of them was new.
	since int
	fresh int
	lines int
}

// step counts one tool call.
func (c *workClock) step() {
	c.steps++
}

// wrote records that this step changed the deliverable, which resets everything
// measured since the last one: the question is always "since the work last
// moved".
func (c *workClock) wrote() {
	c.changedAt = c.steps
	c.since, c.fresh, c.lines = 0, 0, 0
}

// read records what one result brought back.
func (c *workClock) read(fresh, lines int) {
	c.since++
	c.fresh += fresh
	c.lines += lines
}

// newPercent is how much of what came back since the work last moved was new,
// rounded down. A stretch with no lines in it at all is 0.
func (c *workClock) newPercent() int {
	if c.lines <= 0 {
		return 0
	}
	return c.fresh * 100 / c.lines
}

// note is the fact in plain words, for a model that is being told what it has
// been doing. It says nothing when there is nothing to say — a turn that has
// just changed the work is not a turn with a story about standing still.
func (c *workClock) note() string {
	if c == nil || c.steps == 0 || c.since == 0 {
		return ""
	}
	var out strings.Builder
	if c.changedAt == 0 {
		out.WriteString("The work has not changed yet")
	} else {
		fmt.Fprintf(&out, "The work has not changed since step %d", c.changedAt)
	}
	results := "results"
	if c.since == 1 {
		results = "result"
	}
	if c.fresh == 0 {
		fmt.Fprintf(&out, "; %d %s since brought nothing new.", c.since, results)
		return out.String()
	}
	fmt.Fprintf(&out, "; %d %s since were %d%% new lines.", c.since, results, c.newPercent())
	return out.String()
}

// digestLine is the same fact on one line, in the shape the rest of a digest is
// written in (checkpoint.go). It is written even when the work has just moved,
// because a reader of a digest is being handed the state of the work rather than
// being told something about itself.
func (c *workClock) digestLine() string {
	if c == nil || c.steps == 0 {
		return ""
	}
	last := "never"
	if c.changedAt > 0 {
		last = fmt.Sprintf("step %d", c.changedAt)
	}
	return fmt.Sprintf("work last changed: %s · results since: %d · new lines since: %d%%",
		last, c.since, c.newPercent())
}
