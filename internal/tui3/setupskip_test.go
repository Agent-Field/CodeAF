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
// controls screen never opens again. So a foot may not say `not now`, and it may
// not say `skips` about an esc that goes BACK either — the controls screen with a
// connection behind it returns to it rather than leaving.
func TestEveryStepOfSetupNamesEscForWhatItDoes(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	a.routerConnect = func(context.Context) (OpenRouterFlow, error) { return nil, nil }
	a.setup.open = true
	a.setup.steps = []setupStep{setupKey, setupControls}

	for at := range a.setup.steps {
		a.setup.at = at
		a.setup.text = ""
		foot := a.setupKeysWord()
		if strings.Contains(foot, "not now") {
			t.Fatalf("step %d's foot reads %q — `not now` promises the question comes back, and it does not", at, foot)
		}
		// The first step leaves the setup; a step with something before it goes
		// back to it. Both are said in the words that are true of them.
		want := setupSkipKeysWord
		if at > 0 {
			want = "esc back"
		}
		if !strings.Contains(foot, want) {
			t.Fatalf("step %d's foot reads %q, want it to name esc as %q", at, foot, want)
		}
	}
	// AND THE CONTROLS SCREEN STANDING ALONE — a key already in the shell — SKIPS
	// rather than promising a back that has nowhere to go.
	a.setup.steps = []setupStep{setupControls}
	a.setup.at = 0
	if foot := a.setupKeysWord(); !strings.Contains(foot, setupSkipKeysWord) {
		t.Fatalf("the lone controls screen's foot reads %q, want %q", foot, setupSkipKeysWord)
	}
	// AND THE BROWSER-CONNECT FOOT IS THE ONE THIS ROW IS ABOUT: it is the first
	// screen of a fresh install, and it is the branch that disagreed.
	a.setup.steps = []setupStep{setupKey, setupControls}
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
		{"esc on the very first step", 0, []string{"/crew", "/budget", "/model"}, nil},
		// The controls screen is ON SCREEN and unanswered, so its doors are
		// named: esc walked past the step it was standing on.
		{"esc on the controls screen itself", 1, []string{"/crew", "/budget", "/model"}, nil},
		// And here every step has been answered, so naming a door back would be
		// the screen offering somebody a question they just closed.
		{"esc past the last step", 2, nil, []string{"/budget", "/crew"}},
	} {
		a := newTestApp(&fakeAgent{model: "m"})
		a.setup.open = true
		a.setup.steps = []setupStep{setupKey, setupControls}
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
		if len(c.want) > 0 && !strings.Contains(note, setupLaterWord) {
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
	a.setup.steps = []setupStep{setupKey, setupControls}
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
