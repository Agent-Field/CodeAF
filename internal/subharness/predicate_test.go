package subharness

import (
	"strings"
	"testing"
)

func TestAConditionIsCheckedBeforeItIsEverEvaluated(t *testing.T) {
	for _, one := range []struct {
		condition string
		want      string
	}{
		{"always", ""},
		{"never", ""},
		{"ok", ""},
		{"failed", ""},
		{"empty", ""},
		{"nonempty", ""},
		{"contains flaky", ""},
		{"equals done", ""},
		{`matches ^ok\b`, ""},
		{"", "a condition is required"},
		{"ok please", `"ok" takes no argument`},
		{"contains", "needs the text to look for"},
		{"matches", "needs a regular expression"},
		{"matches (", "is not a regular expression"},
		{"the suite is green", `"the" is not a condition`},
	} {
		err := ValidCondition(one.condition)
		switch {
		case one.want == "" && err != nil:
			t.Errorf("%q was refused: %v", one.condition, err)
		case one.want != "" && err == nil:
			t.Errorf("%q was accepted and should not be", one.condition)
		case one.want != "" && err != nil && !strings.Contains(err.Error(), one.want):
			t.Errorf("%q gave %v, want %q", one.condition, err, one.want)
		}
	}
}

func TestEachConditionAsksAboutTheStepBefore(t *testing.T) {
	failed := State{Last: "3 of 20 runs failed", OK: false}
	passed := State{Last: "  Done  ", OK: true}
	empty := State{OK: true}

	for _, one := range []struct {
		condition string
		state     State
		want      bool
	}{
		{"always", failed, true},
		{"never", passed, false},
		{"ok", passed, true},
		{"ok", failed, false},
		{"failed", failed, true},
		{"empty", empty, true},
		{"empty", passed, false},
		{"nonempty", passed, true},
		{"contains FAILED", failed, true},
		{"contains green", failed, false},
		{"equals done", passed, true},
		{"equals don", passed, false},
		{`matches ^\d+ of \d+`, failed, true},
		{`matches ^green`, failed, false},
	} {
		got, err := MatchCondition(one.condition, one.state)
		if err != nil {
			t.Errorf("%q: %v", one.condition, err)
			continue
		}
		if got != one.want {
			t.Errorf("%q on %+v is %v, want %v", one.condition, one.state, got, one.want)
		}
	}
}

// A condition that does not parse is FALSE and an error, never a quiet true:
// the arm it guards is the one thing a person approved on the card.
func TestAConditionThatDoesNotParseIsFalseAndAnError(t *testing.T) {
	got, err := MatchCondition("contians flaky", State{Last: "flaky", OK: true})
	if err == nil {
		t.Fatal("a typo was accepted as a condition")
	}
	if got {
		t.Fatal("a condition that did not parse took its arm anyway")
	}
}
