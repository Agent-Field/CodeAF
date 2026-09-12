package tui3

// THE LAW: THE CHROME NAMES THE MODEL THE ENGINE IS ON.
//
// The reported line is the whole of the argument. A person sent a message at
// 22:48:15, opened the picker at 22:48:18 and chose kimi-k3 at 22:48:21. The
// turn had latched glm-5.3-flash and ran there for seventeen minutes and
// forty-one calls, which is what [session.Agent.SetModel] promises. The seam said
// `moonshotai/kimi-k3 · auto · via wafer` throughout — the model cell from the
// picker's answer, the `via` cell from the endpoint of the request in flight.
// Two cells, two worlds, one line.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/effort"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// wireModelApp is that session at 22:48:22: the dial on the model just picked,
// a turn in flight on the model it started on, and a machine answering for it.
//
// THE TURN'S MODEL COMES OFF THE ENGINE AND THE MACHINE OFF THE NEWS, which is
// the division the surface itself keeps. A fixture that posted a phase and let
// the surface read the model out of it would be testing the inference this
// change deleted: a phase entry is gone at the end of every request and every
// tool batch, so that reading went empty between two steps of one turn.
func wireModelApp(t *testing.T) (*app, *fakeAgent, time.Time) {
	t.Helper()
	t.Cleanup(forgetPhases)
	now := time.Date(2026, 9, 11, 22, 48, 22, 0, time.UTC)
	engine := &fakeAgent{model: wireDial, turnModel: wireLatched}
	a := newTestApp(engine)
	a.width, a.height = 120, 24
	a.model, a.title = wireDial, "porting the parser"
	a.file = "/tmp/lab/conv-one/session.jsonl"
	a.state = stateWorking
	a.clock = func() time.Time { return now }
	PostPhaseNews(PhaseNews{
		Phase:   provider.PhaseWriting,
		Since:   now.Add(-7 * time.Minute),
		Model:   wireLatched,
		Lane:    "wafer",
		Role:    lane.RoleTalk,
		Session: a.taskSheetSelfID(),
		At:      now,
	})
	return a, engine, now
}

const (
	// wireDial is what the picker was last told, and wireLatched is what the turn
	// in flight is really talking to.
	wireDial    = "moonshotai/kimi-k3"
	wireLatched = "zhipu/glm-5.3-flash"
)

// ONE READING FEEDS BOTH CELLS, so the model cell and the `via` cell cannot
// contradict each other: the name and the machine come off the same piece of
// news about the same request.
func TestTheSeamNamesTheModelTheTurnIsOnAndNotTheDial(t *testing.T) {
	a, _, _ := wireModelApp(t)

	line := seamLine(t, a)
	if !strings.Contains(line, "glm-5.3-flash") {
		t.Fatalf("the seam does not name the model the turn is on: %q", line)
	}
	if strings.Contains(line, "kimi-k3") {
		t.Fatalf("the seam still names the dial while another model is answering: %q", line)
	}
	if !strings.Contains(line, "via wafer") {
		t.Fatalf("the seam lost the machine that is answering: %q", line)
	}
}

// AND AN IDLE CONVERSATION NAMES THE DIAL, which is the honest answer to what
// the next request will use. The fallback is a model, never nothing: the
// emptiness law is about a figure nobody has.
func TestAnIdleSeamNamesTheDial(t *testing.T) {
	a, engine, _ := wireModelApp(t)
	forgetPhases()
	engine.turnModel = ""
	a.state = stateIdle

	if line := seamLine(t, a); !strings.Contains(line, "kimi-k3") {
		t.Fatalf("an idle seam does not name the dial: %q", line)
	}
}

// AND THE SWITCH SAYS WHAT IT DID NOT TOUCH, the way it already does for a
// running task. A person who picks a model mid-turn and then watches the answer
// go on arriving in the old voice has been told nothing unless it is said at the
// moment they acted.
func TestSwitchingModelMidTurnSaysWhatTheTurnKeeps(t *testing.T) {
	a, _, _ := wireModelApp(t)

	a.switchModel("openai/gpt-5.4", 0)

	want := "model · openai/gpt-5.4 — " + wireLatched + " is answering now, and " + roomModelNextWord
	if got := plain(frame(a)); !strings.Contains(got, want) {
		t.Fatalf("the switch said nothing about the turn in flight, want %q:\n%s", want, got)
	}
}

// AND IT SAYS NOTHING WHEN THERE IS NOTHING TO SAY. A switch on an idle
// conversation, and a switch onto the model the turn is already on, both leave
// the note as the id alone — a sentence about work that is not running is
// furniture a person has to read past.
func TestSwitchingModelSaysNothingExtraWhenNothingIsRunning(t *testing.T) {
	for _, c := range []struct {
		what string
		idle bool
		pick string
	}{
		{what: "an idle conversation", idle: true, pick: "openai/gpt-5.4"},
		{what: "a pick of the model already running", pick: wireLatched},
	} {
		a, engine, _ := wireModelApp(t)
		if c.idle {
			forgetPhases()
			engine.turnModel = ""
			a.state = stateIdle
		}
		a.switchModel(c.pick, 0)
		if got := plain(frame(a)); strings.Contains(got, "is answering now") {
			t.Fatalf("%s was told about a turn it has no business hearing about:\n%s", c.what, got)
		}
	}
}

// AND THE RUNG BESIDE THE NAME IS THAT MODEL'S RUNG. The levels a person sets
// with the picker's ctrl+t live per model id, so a rung resolved for the dial
// while the cell names the model in flight puts one model's name next to another
// model's level — two facts about two models, read as one sentence.
func TestTheSeamsRungIsTheRungOfTheModelItNames(t *testing.T) {
	t.Cleanup(forgetPhases)
	engine := &effortAgent{
		fakeAgent: &fakeAgent{model: wireDial, turnModel: wireLatched},
		perModel:  map[string]effort.Rung{wireDial: effort.Max, wireLatched: effort.Low},
	}
	a := newTestApp(engine)
	a.width, a.height = 120, 24
	a.model, a.title = wireDial, "porting the parser"
	a.state = stateWorking

	line := seamLine(t, a)
	if !strings.Contains(line, effortLadderWord+" low") {
		t.Fatalf("the rung is not the rung of the model the cell names: %q", line)
	}
	if strings.Contains(line, effortLadderWord+" max") {
		t.Fatalf("the seam drew the dial's rung beside the model in flight: %q", line)
	}

	// And an idle conversation's cell is the dial's, on both halves.
	engine.turnModel = ""
	a.state = stateIdle
	if line := seamLine(t, a); !strings.Contains(line, effortLadderWord+" max") {
		t.Fatalf("an idle seam does not draw the dial's own rung: %q", line)
	}
}
