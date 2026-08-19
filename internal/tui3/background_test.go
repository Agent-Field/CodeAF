package tui3

// ctrl+g's acceptance tests. The key sends a running foreground command to the
// background through the engine's own seam — it never kills and never restarts —
// and it is ABSENT wherever that seam cannot answer.

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// promotingAgent is [fakeAgent] with the engine's promotion door on it
// (internal/session's PromoteCall). It records what it was asked to promote,
// because the one thing the surface must get right is WHICH call it sent away.
type promotingAgent struct {
	*fakeAgent
	asked  []string
	answer string
	refuse bool
}

func (p *promotingAgent) PromoteCall(callID string) (string, bool) {
	p.asked = append(p.asked, callID)
	if p.refuse {
		return "", false
	}
	return p.answer, true
}

// runningBash puts one foreground bash call on screen, still running, with an
// id the surface can address it by. The forming fragment is what carries the id
// — a row that never formed has none, and cannot be promoted.
func runningBash(t *testing.T, a *app, id, command string) {
	t.Helper()
	args := `{"command":"` + command + `"}`
	drive(t, a, streamEventMsg{gen: a.gen, ev: forming(id, "bash", "bash", args)})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolAnnounced, CallID: id, Tool: "bash", Hint: "bash", Args: args,
	}})
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, CallID: id, Tool: "bash", Hint: "bash", Args: args,
	}})
}

// promotingTurn is a surface mid-turn with the promotion door behind it.
func promotingTurn(t *testing.T) (*app, *promotingAgent) {
	t.Helper()
	agent := &promotingAgent{
		fakeAgent: &fakeAgent{model: "m"},
		answer:    "still running as job 3; log at /tmp/lab/.aforge-v3/jobs/3.log",
	}
	a := newTestApp(agent)
	typeLine(t, a, "build it")
	return a, agent
}

// THE WHOLE POINT: one key, and the command that was running is a job. The row
// stays one row, says which job it became, and the turn is untouched — nothing
// was interrupted and nothing was sent again.
func TestARunningCommandIsSentToTheBackgroundWithOneKey(t *testing.T) {
	a, agent := promotingTurn(t)
	runningBash(t, a, "c1", "go test ./...")

	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 1 || agent.asked[0] != "c1" {
		t.Fatalf("the key promoted %v, want exactly [c1]", agent.asked)
	}
	// THE TURN IS NOT STOPPED. That is the entire difference between this key
	// and esc, and it is the reason the key exists.
	if agent.stops != 0 {
		t.Fatalf("backgrounding interrupted the turn %d times", agent.stops)
	}
	if a.state != stateWorking {
		t.Fatalf("the surface left stateWorking for %v", a.state)
	}
	// The transcript stays legal: one row for one call, still the same call.
	if got := toolEntries(a); got != 1 {
		t.Fatalf("backgrounding drew %d tool rows, want 1", got)
	}
	if line := toolLineOf(t, a); !strings.Contains(line, "job 3") {
		t.Fatalf("the row does not say which job it became: %q", line)
	}
	if line := toolLineOf(t, a); !strings.Contains(line, "go test") {
		t.Fatalf("the row lost the command it is about: %q", line)
	}

	// And the key is spent: the same row cannot be sent away twice.
	drive(t, a, key("ctrl+g"))
	if len(agent.asked) != 1 {
		t.Fatalf("a backgrounded row was promoted again: %v", agent.asked)
	}
}

// A CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN. With nothing promotable
// running, ctrl+g does nothing at all — it does not refuse, and it does not
// leave a line in the conversation saying there was nothing to do.
func TestBackgroundingIsAbsentWithNothingToBackground(t *testing.T) {
	a, agent := promotingTurn(t)
	before := len(a.entries)

	// Nothing running.
	drive(t, a, key("ctrl+g"))
	// A call that is not bash.
	drive(t, a, streamEventMsg{gen: a.gen, ev: session.Event{
		Kind: session.EventToolBegin, CallID: "r1", Tool: "read", Hint: "read app.go",
		Args: `{"path":"app.go"}`,
	}})
	drive(t, a, key("ctrl+g"))
	// A bash call that asked to run in the background: it was a job from the
	// first instant, so there is nothing to promote.
	runningBash(t, a, "b1", "npm run dev")
	a.entries[len(a.entries)-1].detail.Args = `{"command":"npm run dev","background":true}`
	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 0 {
		t.Fatalf("the key reached the engine with nothing to promote: %v", agent.asked)
	}
	if got := len(a.entries) - before; got != 2 {
		t.Fatalf("the key added rows of its own: %d new entries", got)
	}
	for _, e := range a.entries {
		if e.bg != "" {
			t.Fatalf("a row was marked backgrounded with nothing to background: %q", e.bg)
		}
	}
}

// The key is absent on a surface whose agent has never heard of promotion — the
// same rule the stop card and the wake lane are held to.
func TestBackgroundingIsAbsentWithoutTheEnginesDoor(t *testing.T) {
	agent := &fakeAgent{model: "m"}
	a := newTestApp(agent)
	typeLine(t, a, "build it")
	runningBash(t, a, "c1", "go test ./...")

	drive(t, a, key("ctrl+g"))

	if at := a.promotableRow(); at >= 0 {
		t.Fatalf("a surface with no promotion door offered row %d", at)
	}
	if line := toolLineOf(t, a); strings.Contains(line, "job ") {
		t.Fatalf("the row claims a job nothing gave it: %q", line)
	}
}

// A call that finished between the frame and the keypress is refused by the
// engine, and the surface says nothing about it: the result is already on its
// way, and a row marked "job 3" for a job that does not exist would be a lie
// with an id on it.
func TestARefusedPromotionMarksNothing(t *testing.T) {
	a, agent := promotingTurn(t)
	agent.refuse = true
	runningBash(t, a, "c1", "go test ./...")

	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 1 {
		t.Fatalf("the key did not reach the engine: %v", agent.asked)
	}
	if line := toolLineOf(t, a); strings.Contains(line, "job ") {
		t.Fatalf("a refused promotion still marked the row: %q", line)
	}
}

// The oldest running call is the one a person is waiting on. A batch that
// started a `cd` a moment ago and a build two minutes ago must background the
// build.
func TestBackgroundingTakesTheCallThatHasBeenRunningLongest(t *testing.T) {
	a, agent := promotingTurn(t)
	runningBash(t, a, "old", "go test ./...")
	runningBash(t, a, "new", "git status")

	drive(t, a, key("ctrl+g"))

	if len(agent.asked) != 1 || agent.asked[0] != "old" {
		t.Fatalf("the key promoted %v, want exactly [old]", agent.asked)
	}
}

// The engine owns the id, and the row quotes it rather than composing one.
func TestTheRowQuotesTheEnginesOwnJobNumber(t *testing.T) {
	cases := []struct {
		line   string
		wanted string
	}{
		{"still running as job 3; log at /tmp/3.log", "job 3"},
		{"still running as job 17; log at /tmp/17.log", "job 17"},
		{"something nobody here can read", "backgrounded"},
	}
	for _, testCase := range cases {
		if got := backgroundWord(testCase.line); got != testCase.wanted {
			t.Fatalf("%q became %q, want %q", testCase.line, got, testCase.wanted)
		}
	}
}
