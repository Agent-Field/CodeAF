package session

// Issue #890's engine half. The belt refuses a `propose_task` whose declared
// check is a composed shell command, and THE SENTENCE IT ANSWERS WITH IS
// WRITTEN FOR THE MODEL: it names `checks`, quotes the offending command, and
// is one repair away from a working call — which is the stated reason the door
// refuses rather than dropping silently. The surface may no longer draw that
// sentence to the person (tui3's refusal_test.go holds that half), but nothing
// about the refusal itself may soften: the model still reads it verbatim as
// the tool's result, in the transcript, because that is the only place the
// repair can happen.

import (
	"strconv"
	"strings"
	"testing"
)

// TestAnArgumentRefusalStillReachesTheModelVerbatim is the acceptance line "the
// refusal still reaches the model verbatim as the tool result, asserted on the
// transcript": the belt's own sentence for a composed check, unclipped and
// unrewritten, carried in the result the model's next turn reads.
func TestAnArgumentRefusalStillReachesTheModelVerbatim(t *testing.T) {
	refusal := `Invalid arguments: checks must each be ONE command with no shell composition — "cd 1-check && ./run.sh" is not`

	// The direct answer the belt gives is the sentence whole.
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	composed, isError := runTool(t, agent, "propose_task",
		`{"title":"Fix the nil-map","summary":"s","brief":"b","deliverable":"d","acceptance":"a","checks":["cd 1-check && ./run.sh"]}`)
	if !isError {
		t.Fatalf("a composed check was accepted:\n%s", composed)
	}
	if !strings.Contains(composed, `composition character "&" is outside quotes`) ||
		!strings.Contains(composed, "A valid check is one rerunnable command") {
		t.Fatalf("the refusal does not name the repair:\n%s", composed)
	}

	// The same sentence, sent back through a refused call's result, is still
	// recognisably a refusal the loop guard and the surface can agree on —
	// that is what keeps the two halves of this change answering with one
	// pair of eyes.
	if !ArgumentRefusal(refusal) {
		t.Fatal("ArgumentRefusal does not see the belt's own composed-check refusal")
	}
	if ArgumentRefusal("make: *** no rule to make target 'test'") {
		t.Fatal("ArgumentRefusal took a bash that failed in the world for a refusal of the call's arguments")
	}
}

// A CHECK IS JUDGED BY ITS SHAPE, AND THE SCHEMA SAYS THE SHAPE IN ANY SETUP: one
// rerunnable command, no leading cd, no composition. It names no tool and no language.
func TestChecksSchemaSaysWhereAChecksRunsAndNamesNoTool(t *testing.T) {
	for _, want := range []string{"ONE rerunnable command", "composition characters", "only inside quoted arguments"} {
		if !strings.Contains(checksSchemaJSON, want) {
			t.Errorf("checks schema does not say %q:\n%s", want, checksSchemaJSON)
		}
	}
}

// A CHECK THAT LEADS WITH A DIRECTORY CHANGE IS REFUSED, NEVER REPAIRED, AND THE
// REFUSAL SAYS THE FORM THAT PASSES. Dropping the step is meaning-preserving only
// when the directory is the ground itself, which this door does not know: a
// check that changes into any other folder would be kept as a command run where
// its files are not. Any other composition keeps the plain refusal.
func TestACheckThatLeadsWithADirectoryChangeIsRefusedWithTheFormThatPasses(t *testing.T) {
	const form = "leave the directory change out and name each file by its path"
	for _, led := range []string{"cd /srv/checkout && ./run.sh --all", "cd /srv/elsewhere && ./run.sh report", "cd sub && ./run.sh"} {
		got, refusal := declaredCheckList([]string{led})
		if got != nil || !strings.Contains(refusal, "no shell composition") || !strings.HasSuffix(refusal, form) {
			t.Fatalf("%q: checks = %q, refusal = %q", led, got, refusal)
		}
	}
	for _, composed := range []string{"./build.sh && ./run.sh", "./run.sh | ./count.sh"} {
		if _, refusal := declaredCheckList([]string{composed}); !strings.HasSuffix(refusal, " is not") {
			t.Fatalf("%q: refusal = %q", composed, refusal)
		}
	}
	if got, refusal := declaredCheckList([]string{"./run.sh /srv/elsewhere/report"}); refusal != "" || len(got) != 1 {
		t.Fatalf("an absolute path as an argument: checks = %q, refusal = %q", got, refusal)
	}
}

func TestDeclaredChecksJudgeCompositionOutsideQuotesAndPreserveBytes(t *testing.T) {
	tests := []struct {
		name    string
		check   string
		wantBad string
	}{
		{name: "composition outside quotes", check: "test -s /tmp/wisp-ideation/walls.md && grep -c '^## ' /tmp/wisp-ideation/walls.md | awk '$1>=6'", wantBad: "&"},
		{name: "composition characters inside quotes", check: "grep -iE 'handoff|vault|wall' /tmp/wisp-ideation/walls.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, refusal := declaredCheckList([]string{tt.check})
			if tt.wantBad == "" {
				if refusal != "" || len(got) != 1 || got[0] != tt.check {
					t.Fatalf("checks = %q, refusal = %q; want the declared bytes unchanged", got, refusal)
				}
				return
			}
			if len(got) != 0 || !strings.Contains(refusal, strconv.Quote(tt.wantBad)) ||
				!strings.Contains(refusal, "A valid check is one rerunnable command with composition characters only inside quoted arguments.") {
				t.Fatalf("checks = %q, refusal = %q", got, refusal)
			}
		})
	}
}

func TestCommandLikePreservesQuotedArgumentSpacing(t *testing.T) {
	const check = `verify 'left  |  right' --label="x && y"`
	got, ok := commandLike(check)
	if !ok || got != check {
		t.Fatalf("commandLike(%q) = %q, %v", check, got, ok)
	}
}
