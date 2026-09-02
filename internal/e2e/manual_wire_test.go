//go:build e2e

// WHAT A STRANGER'S QUESTION MEETS ON THE WIRE.
//
// internal/manual measures its own retrieval for free and exactly: the
// twenty-five plain questions of [asked.Plain] and the cold [asked.HeldOut] set
// are ranked with no model in the loop, and the floors in plainquestions_test.go
// hold. THAT NUMBER IS NOT WHAT A PERSON MEETS. The model does not search what it
// was asked — it composes a query of its own, and on a corpus of a few dozen
// short sections two words nobody said move the ranking off the page (#307).
//
// So this lane asks the SAME forty-seven questions through the surface a person
// uses, one fresh conversation each, on `deepseek/deepseek-v4-flash`, and reads
// off transcript.jsonl three things for every one of them: the query the model
// composed, whether it opened the manual at all, and which pages came back. The
// free number and this one are then about the same questions, which is the whole
// reason [asked] is a package rather than a table in a test file.
//
//	go test -tags e2e -count=1 -timeout 120m -v -run TestManualOnTheWire ./internal/e2e/
package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/manual/asked"
)

const (
	// wireAttempts is the ask and one retry, and the retry is for ONE thing:
	// a turn where the model answered from memory and never opened the manual.
	// That is a fact about the model's appetite for tools, not about retrieval,
	// and a lane measuring retrieval that scored it would be reporting a number
	// half about something else. A lookup that HAPPENED and missed is the
	// finding and is never asked again.
	wireAttempts = 2
	// wireCap is what one question may spend before something is wrong with the
	// run rather than with the manual. One turn on this model is a fraction of
	// a cent; a nickel is a runaway.
	wireCap = 0.05
)

// ── the floors ──────────────────────────────────────────────────────────────
//
// THESE ARE MEASURED NUMBERS, ROUNDED DOWN, AND NOTHING ELSE. They are set from
// a real run of this file after #307 landed, and they are lower than that run:
// a live model composes a different query every time it is asked, so a floor at
// the measured number would go red on the model's own variance rather than on a
// regression. Read the run's log lines, not these constants, for what the wire
// actually does today.
//
// They are deliberately SEPARATE from plainquestions_test.go's floors. Those
// measure the corpus and are exact and free; these measure the corpus THROUGH a
// model and cost money, and folding the two together would let a model's mood
// look like a page becoming unreachable.
const (
	wirePlainFirstFloor  = 0
	wirePlainWithinFloor = 0
	wireHeldFirstFloor   = 0
	wireHeldWithinFloor  = 0
)

// TestManualOnTheWire is the lane itself.
func TestManualOnTheWire(t *testing.T) {
	w := newManualWorld(t)
	started := time.Now()

	plain := askOnTheWire(t, w, "the twenty-five", asked.Plain)
	held := askOnTheWire(t, w, "held out", asked.HeldOut)

	t.Logf("THE WIRE, in %s", time.Since(started).Round(time.Second))
	plain.report(t, "the twenty-five")
	held.report(t, "held out")

	plain.hold(t, "the twenty-five", wirePlainFirstFloor, wirePlainWithinFloor)
	held.hold(t, "held out", wireHeldFirstFloor, wireHeldWithinFloor)
}

// ── one question, through the surface ───────────────────────────────────────

// wireAsk is what one question met: the query the model wrote instead of it, and
// the pages the manual handed back, in rank order.
type wireAsk struct {
	question string
	want     string
	query    string
	pages    []string
	opened   bool
}

// first is whether the page the question is about came back FIRST, and within is
// whether it came back at all inside the four sections the model was handed.
func (a wireAsk) first() bool  { return a.hit(1) }
func (a wireAsk) within() bool { return a.hit(len(a.pages)) }

func (a wireAsk) hit(depth int) bool {
	for at, page := range a.pages {
		if at >= depth {
			return false
		}
		for _, want := range strings.Split(a.want, ",") {
			if page == want {
				return true
			}
		}
	}
	return false
}

func (a wireAsk) mark() string {
	switch {
	case !a.opened:
		return "SHUT"
	case a.first():
		return "1st "
	case a.within():
		return "top4"
	}
	return "MISS"
}

// wireRun is one set of questions, asked.
type wireRun []wireAsk

func (r wireRun) count() (opened, first, within int) {
	for _, one := range r {
		if one.opened {
			opened++
		}
		if one.first() {
			first++
		}
		if one.within() {
			within++
		}
	}
	return opened, first, within
}

// report prints the table a reader of this lane came for: every question with
// the words the model actually searched, so a miss can be read rather than
// guessed at.
func (r wireRun) report(t *testing.T, name string) {
	t.Helper()
	for _, one := range r {
		t.Logf("%s %-52s searched=%-52q got=%v", one.mark(), one.question, one.query, one.pages)
	}
	opened, first, within := r.count()
	t.Logf("%s ON THE WIRE: opened the manual %d/%d · first %d/%d · within four %d/%d",
		name, opened, len(r), first, len(r), within, len(r))
}

// hold is the floor. A drop here is a page a person's question no longer
// reaches through the model that answers it, which nothing free in this build
// would notice.
func (r wireRun) hold(t *testing.T, name string, firstFloor, withinFloor int) {
	t.Helper()
	_, first, within := r.count()
	if first < firstFloor {
		t.Errorf("%s: the right page came first %d times of %d on the wire; the floor is %d", name, first, len(r), firstFloor)
	}
	if within < withinFloor {
		t.Errorf("%s: the right page reached the model at all %d times of %d on the wire; the floor is %d", name, within, len(r), withinFloor)
	}
}

// askOnTheWire puts every question of one set to its own fresh conversation.
func askOnTheWire(t *testing.T, w *world, name string, set []asked.Question) wireRun {
	t.Helper()
	run := make(wireRun, 0, len(set))
	for at, question := range set {
		one := askOneOnTheWire(t, w, question)
		run = append(run, one)
		t.Logf("  %s %2d/%2d %s %-52s searched=%q got=%v",
			name, at+1, len(set), one.mark(), one.question, one.query, one.pages)
	}
	return run
}

// askOneOnTheWire is one question, one conversation, and the journal read back.
// A turn where the model never opened the manual is asked once more — see
// [wireAttempts] — and a lookup that happened stands whatever it returned.
func askOneOnTheWire(t *testing.T, w *world, question asked.Question) wireAsk {
	t.Helper()
	found := wireAsk{question: question.Ask, want: question.Want}
	for attempt := 1; attempt <= wireAttempts; attempt++ {
		started := time.Now()
		agent, place := w.open(aPlainWorkspace(t), manualConfig)
		out := w.say(agent, question.Ask, answerYes)
		usd, models := ledgerSince(t, started)
		if out.Err != nil {
			t.Errorf("%q ended in an error: %v", question.Ask, out.Err)
		}
		for _, model := range models {
			if model != e2eModel {
				t.Errorf("a call rode %q; every model row in this run is pinned at %q", model, e2eModel)
			}
		}
		if usd > wireCap {
			t.Errorf("%q spent $%.4f, past the $%.2f one turn costs on %s", question.Ask, usd, wireCap, e2eModel)
		}

		calls := manualCalls(t, place)
		found.query, found.pages = searchedFor(calls), pagesReturnedTo(calls)
		found.opened = len(calls) > 0
		if found.opened {
			return found
		}
		if attempt < wireAttempts {
			t.Logf("    %q was answered without opening the manual (%v); asking once more", question.Ask, out.names())
		}
	}
	return found
}

// searchedFor is the query the model composed, which is the whole subject of
// #307: it is written down for every question whether the lookup hit or missed,
// because a table of misses with no queries beside them says nothing about why.
func searchedFor(calls []manualCall) string {
	asked := make([]string, 0, len(calls))
	for _, one := range calls {
		switch {
		case strings.TrimSpace(one.Query) != "":
			asked = append(asked, one.Query)
		case strings.TrimSpace(one.Page) != "":
			asked = append(asked, "page:"+one.Page)
		}
	}
	return strings.Join(asked, " | ")
}

// pagesReturnedTo is the pages the manual put in front of the model this turn,
// in the order it ranked them and each named once — read off the journal, never
// off [session.Event]'s four-thousand-byte display copy ([manualCall] says why).
//
// A PAGE ASKED FOR BY NAME COUNTS AS THAT PAGE ARRIVING. It carries no `[page ·
// heading]` labels, because a page read is the page's own text; a reading that
// only understood labels would score the most direct route to a page as a miss.
func pagesReturnedTo(calls []manualCall) []string {
	seen := map[string]bool{}
	pages := make([]string, 0, 4)
	keep := func(page string) {
		if page != "" && !seen[page] {
			seen[page] = true
			pages = append(pages, page)
		}
	}
	for _, one := range calls {
		if name := pageName(one.Page); name != "" {
			keep(name)
		}
		for _, section := range renderedSections(one.Output) {
			keep(section.Page)
		}
	}
	return pages
}
