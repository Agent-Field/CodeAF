package tui3

import (
	"context"
	"strings"
	"testing"
)

// ESC IS NAMED FOR WHAT IT DOES, ON EVERY STEP.
//
// Five of the flow's six feet said `esc skips setup` and the sixth — the browser
// connect step, which is the FIRST thing a new install sees — said `esc not
// now`. That is a promise about a later: esc stamps `setup_seen_at`, and the
// crew and budget questions never open again. One spelling, and it is the true
// one.
func TestEveryStepOfSetupNamesEscForWhatItDoes(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.setup.open = true
	a.setup.steps = []setupStep{setupKey, setupCrew, setupBudget}

	seen := 0
	for at := range a.setup.steps {
		a.setup.at = at
		a.setup.text = ""
		foot := a.setupKeysWord()
		if !strings.Contains(foot, setupSkipKeysWord) {
			t.Fatalf("step %d's foot reads %q, want it to name esc as %q", at, foot, setupSkipKeysWord)
		}
		if strings.Contains(foot, "not now") {
			t.Fatalf("step %d's foot reads %q — `not now` promises the question comes back, and it does not", at, foot)
		}
		seen++
	}
	if seen != 3 {
		t.Fatalf("only %d steps were read, want the whole flow", seen)
	}
	// AND THE BROWSER-CONNECT FOOT IS THE ONE THIS ROW IS ABOUT: it is the first
	// screen of a fresh install, and it is the branch that disagreed.
	a.setup.at, a.setup.text = 0, ""
	if foot := a.setupKeysWord(); !strings.Contains(foot, "enter connects in browser") {
		t.Fatalf("the connect step's foot reads %q, want the browser offer this test is about", foot)
	}
}

// AND THE QUESTIONS IT WALKED PAST ARE NAMED ON THE WAY OUT.
//
// Two of the three questions have no second door — `setup_seen_at` is stamped
// whichever way the screen ended and only the key-only form reopens — so a
// person who pressed a key called "skip" lost the crew and the day's limit with
// no sign that anything had gone. The line names them, and only the ones they
// were not asked.
func TestSkippingSetupNamesTheQuestionsItRetired(t *testing.T) {
	for _, c := range []struct {
		what  string
		at    int
		want  []string
		unfit []string
	}{
		{"esc on the very first step", 0, []string{"/crew", "/budget"}, nil},
		// The crew question is ON SCREEN and unanswered, so it is named: esc
		// walked past the step it was standing on as well as the ones under it.
		{"esc on the crew question itself", 1, []string{"/crew", "/budget"}, nil},
		// And here the crew HAS been answered, so naming it would be the screen
		// offering somebody a door back to a question they just closed.
		{"esc on the last step, the crew answered", 2, []string{"/budget"}, []string{"/crew"}},
	} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.setup.open = true
		a.setup.steps = []setupStep{setupKey, setupCrew, setupBudget}
		a.setup.at = c.at
		before := len(a.entries)
		a.endSetup(true)
		note := strings.Join(setupNotesAfter(a, before), "\n")
		for _, want := range c.want {
			if !strings.Contains(note, want) {
				t.Errorf("%s left %q, which never names %q", c.what, note, want)
			}
		}
		for _, gone := range c.unfit {
			if strings.Contains(note, gone) {
				t.Errorf("%s left %q, which offers %q over a question already answered", c.what, note, gone)
			}
		}
		if !strings.Contains(note, setupLaterWord) {
			t.Errorf("%s left %q, want it led by %q", c.what, note, setupLaterWord)
		}
	}
}

// AND FINISHING THE FLOW LEAVES NO SUCH LINE — every question was asked, so
// there is nothing still to set, and the emptiness law says a screen with
// nothing to report reports nothing.
func TestFinishingSetupLeavesNoLineAboutQuestionsItAsked(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.setup.open = true
	a.setup.steps = []setupStep{setupKey, setupCrew, setupBudget}
	a.setup.at = len(a.setup.steps)
	before := len(a.entries)
	a.endSetup(false)
	note := strings.Join(setupNotesAfter(a, before), "\n")
	if strings.Contains(note, setupLaterWord) || strings.Contains(note, "/crew") {
		t.Fatalf("a finished setup left %q, want nothing about questions it asked", note)
	}
}

// setupNotesAfter is every note this app wrote after the entry count it is
// given, which is how a test reads the line one call left behind rather than
// the pile a whole fixture has accumulated.
func setupNotesAfter(a *app, from int) []string {
	var out []string
	for _, e := range a.entries[from:] {
		if e.kind == entryNote {
			out = append(out, e.text)
		}
	}
	return out
}
