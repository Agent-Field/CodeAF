package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// ── a planning model that thinks past its wall, through the real door ───────
//
// Issue #927 lost two grounding passes to the four-minute call wall on a
// request nobody would call dishonest: the model thought for the whole wall,
// the cut threw the thought away, and the person read that the model had
// stopped answering. The unit tests prove each half of the fix where it lives;
// these prove the chain through `codeaf do --json`, the door the issue names,
// against a model that never stops thinking. They fail if the structuring slots
// lose their wall (chat.go's WithCallWall), if the wall stops telling the model
// how long it has, or if the cut stops asking for the answer the thought
// reached.

// scriptedThought is what the thinking compiler says on the stream before it
// is cut. The answer ask carries it back, which is how the stub knows the ask.
const scriptedThought = "The ask is a release note with its migration steps; one worker writes it."

// stubLane is the one machine the scripted endpoint is believed to be.
const stubLane = "stub-lane"

// stubRate is what that machine is believed to write, in tokens a second —
// issue #927's own Friendli figure.
const stubRate = 210

// wallInTest is the structuring slots' wall in these runs: one a test reaches
// in a second, where the shipped wall is four minutes.
const wallInTest = time.Second

// planningStages is every pass of the planning pipeline this suite can answer
// for, each named by the opening line of its own prompt. The stage is read off
// the REQUEST, so a stub cannot agree with the wall by counting calls.
var planningStages = []struct{ stage, prompt string }{
	{"compile", "You are the intent compiler"},
	{"ground", "You settle what a goal leaves unsaid"},
	{"spine", "You break a goal into its ordered stages"},
	{"fanout", "You list the parts of one stage"},
	{"size", "You judge whether each node is the right size"},
	{"audit", "You check whether each node can actually be completed"},
	{"bind", "You decide what each node must wait for"},
	{"contracts", "You write the working method for one agent"},
	{"accept", "You read one request and list the behaviours it states"},
}

// stageOf names the planning pass one request body belongs to, empty for
// anything else the run asks about.
func stageOf(body string) string {
	for _, known := range planningStages {
		if strings.Contains(body, known.prompt) {
			return known.stage
		}
	}
	return ""
}

// nodesAskedAbout reads the plan node ids one size or bind ask lists, so the
// stub answers about the nodes it was actually shown. The ask renders them as
// "3. Title — Summary", and the prompt's newlines arrive as the two characters
// JSON spells them with.
func nodesAskedAbout(body string) []int {
	var ids []int
	for _, line := range strings.Split(strings.ReplaceAll(body, `\n`, "\n"), "\n") {
		var id int
		if _, err := fmt.Sscanf(strings.TrimSpace(line), "%d. ", &id); err == nil && id > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// thinkUntilCut is the model from the incident: its thought on the stream, and
// then nothing more until the caller gives up on it.
func (s *scriptedBrain) thinkUntilCut(writer http.ResponseWriter, request *http.Request) {
	s.tally("thought")
	writer.Header().Set("Content-Type", "text/event-stream")
	chunk, _ := json.Marshal(map[string]any{
		"id": "thinking", "model": "scripted",
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning": scriptedThought}}},
	})
	fmt.Fprintf(writer, "data: %s\n\n", chunk)
	if flusher, ok := writer.(http.Flusher); ok {
		flusher.Flush()
	}
	// Thinking, but not past the test: the wall ends this, and the handler
	// lets go with it so the server can close.
	select {
	case <-request.Context().Done():
	case <-time.After(30 * time.Second):
	}
}

// thinksAtMaxWhenNothingIsSent is GLM 5.3's published row: a thinking pass
// that cannot be switched off and runs at the top of its ladder when a request
// says nothing about it.
func thinksAtMaxWhenNothingIsSent(string) (provider.ReasoningProfile, bool) {
	return provider.ReasoningProfile{Mandatory: true, Default: "max"}, true
}

// oneLaneSheet says every model is served by one machine.
type oneLaneSheet struct{}

func (oneLaneSheet) Rows(model string) []lanes.Row {
	return []lanes.Row{{ID: lanes.ID{Model: model, Lane: stubLane}}}
}
func (oneLaneSheet) Refresh(context.Context, string) error { return nil }

// laneBelievedAt believes one thing: that machine's pace, known well.
type laneBelievedAt float64

func (laneBelievedAt) Note(lanes.Sighting)       {}
func (laneBelievedAt) NoteOutcome(lanes.Outcome) {}
func (laneBelievedAt) Prime(lanes.Row, float64)  {}
func (rate laneBelievedAt) Belief(id lanes.ID) (lanes.Belief, bool) {
	if id.Lane != stubLane {
		return lanes.Belief{}, false
	}
	return lanes.Belief{ID: id, Rate: lanes.Posterior{X: math.Log(float64(rate)), P: 0.01}}, true
}
func (rate laneBelievedAt) Beliefs(model string) []lanes.Belief {
	belief, _ := rate.Belief(lanes.ID{Model: model, Lane: stubLane})
	return []lanes.Belief{belief}
}

// believeTheStub installs what a shipped build would know by the second call
// of a process: the endpoint's one machine, and its pace.
func believeTheStub(t *testing.T) {
	t.Helper()
	registry := lanes.Default()
	registry.SetSheet(oneLaneSheet{})
	registry.SetLedger(laneBelievedAt(stubRate))
	t.Cleanup(func() {
		registry.SetLedger(nil)
		registry.SetSheet(nil)
	})
}

// planOnThinkingModel runs one errand through `codeaf do --json` and returns
// its outcome, its log and its error.
func planOnThinkingModel(t *testing.T, script *scriptedBrain) (settled bool, log string, err error) {
	t.Helper()
	script.reasoning = thinksAtMaxWhenNothingIsSent
	script.gatePasses = true
	believeTheStub(t)
	var stdout, stderr strings.Builder
	err = doErrand(doRequest{
		task: "write the release note and include the migration steps", asJSON: true,
		// Room for the whole run many times over; what cuts the thought is the
		// slots' wall, never this.
		timeout:  30 * time.Second,
		callWall: wallInTest,
		stdout:   &stdout, stderr: &stderr, newClient: script.client,
	})
	var outcome struct {
		Settled bool `json:"settled"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout.String()), &outcome); decodeErr != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s\n%s", decodeErr, stdout.String(), stderr.String())
	}
	return outcome.Settled, stdout.String() + stderr.String(), err
}

// reasoningBudget is the thinking budget one request body carried, zero when
// it carried none.
func reasoningBudget(t *testing.T, body string) float64 {
	t.Helper()
	var request struct {
		Reasoning struct {
			MaxTokens float64 `json:"max_tokens"`
		} `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(body), &request); err != nil {
		t.Fatalf("the request body is not JSON: %v", err)
	}
	return request.Reasoning.MaxTokens
}

// bodiesFor is every request one planning stage was sent, in order.
func (s *scriptedBrain) bodiesFor(stage string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.asked[stage]...)
}

// TestEveryPlanningStageThatThinksPastItsWallStillPlans is issue #927's
// acceptance, over the whole pipeline. Every pass that reaches a model on the
// planning road — the compile, and then ground, spine, fan-out, size, bind and
// the contracts — is served by a model that thinks and never stops, and the run
// still plans and settles. The second run on the issue (deepseek-v4.1-flash,
// 2026-09-11) lost size, bind and contracts, not only the compile, which is why
// this asks the same three questions of every stage rather than of the first.
func TestEveryPlanningStageThatThinksPastItsWallStillPlans(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.plansAPipeline = true
	script.thinksPastTheWallOn = map[string]bool{}
	for _, stage := range planningStages {
		script.thinksPastTheWallOn[stage.stage] = true
	}

	settled, log, err := planOnThinkingModel(t, script)
	if err != nil || !settled {
		t.Fatalf("a planner that thought past its wall at every stage cost the run its plan: settled=%v err=%v\n%s", settled, err, log)
	}
	// Every stage that ran is held to the whole contract. A stage the scripted
	// plan never reaches asks nothing and is not asserted about; the count of
	// those is pinned below so this cannot quietly become a one-stage test.
	ran := 0
	for _, known := range planningStages {
		bodies := script.bodiesFor(known.stage)
		if len(bodies) == 0 {
			continue
		}
		ran++
		// THE MODEL WAS TOLD HOW LONG IT HAD, on the first request of the stage.
		told := reasoningBudget(t, bodies[0])
		if most := 0.95 * stubRate * wallInTest.Seconds(); told <= 0 || told > math.Ceil(most) {
			t.Fatalf("the %s pass was told a thinking budget of %.0f, want the %.0f its wall and its machine imply",
				known.stage, told, most)
		}
		// AND WHAT IT THOUGHT WAS KEPT: every cut call is followed by an ask
		// carrying the thought back.
		if len(bodies) < 2 {
			t.Fatalf("the %s pass was asked once; a pass cut with a thought on the wire is asked for its answer", known.stage)
		}
		thoughtCarried := false
		for _, body := range bodies[1:] {
			thoughtCarried = thoughtCarried || strings.Contains(body, scriptedThought)
		}
		if !thoughtCarried {
			t.Fatalf("no ask on the %s pass carried the thought its cut kept", known.stage)
		}
	}
	if ran < 6 {
		t.Fatalf("only %d planning stages ran; this run is meant to exercise the pipeline, not the compile alone", ran)
	}
	if strings.Contains(log, "stopped answering") || strings.Contains(log, "context deadline exceeded") {
		t.Fatalf("a run that planned told the person machinery or a silence that never happened:\n%s", log)
	}
}

// TestAPlanningModelThatThinksPastItsWallStillPlans is the compile alone,
// through the door the issue names: the pass thinks until its wall, the wall
// asks once for the answer that thought reached, and the run settles.
func TestAPlanningModelThatThinksPastItsWallStillPlans(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.thinksPastTheWallOn = map[string]bool{"compile": true}

	settled, log, err := planOnThinkingModel(t, script)
	if err != nil || !settled {
		t.Fatalf("a model that thought past its wall cost the run its plan: settled=%v err=%v\n%s", settled, err, log)
	}
	compiles := script.bodiesFor("compile")
	if len(compiles) != 2 {
		t.Fatalf("the compiler was sent %d requests, want the one it thought through and the ask for its answer", len(compiles))
	}
	// THE MODEL WAS TOLD HOW LONG IT HAD. share(max) × the machine's pace ×
	// the wall is at most this; the wall's own share of a thirty-second rail
	// never exceeds the wall.
	told := reasoningBudget(t, compiles[0])
	if most := 0.95 * stubRate * wallInTest.Seconds(); told <= 0 || told > math.Ceil(most) {
		t.Fatalf("the first compile was told a thinking budget of %.0f, want the %.0f its wall and its machine imply", told, most)
	}
	if strings.Contains(compiles[0], scriptedThought) {
		t.Fatal("the first compile already carried the thought it had not yet had")
	}
	// AND WHAT IT THOUGHT WAS KEPT: the second request carries the thought back.
	if !strings.Contains(compiles[1], scriptedThought) {
		t.Fatalf("the answer ask did not carry the thought the wall cut:\n%s", compiles[1])
	}
	if strings.Contains(log, "stopped answering") {
		t.Fatalf("the run blamed a silence that never happened:\n%s", log)
	}
}

// TestAPlanningPipelineThatAnswersInTimeIsAskedOnce is the control for both
// tests above: the same model, the same wall, the same belief, and a planner
// whose every pass answers. Each pass is asked exactly once, carries the budget
// its wall implies, and nothing is asked for an answer it already gave.
func TestAPlanningPipelineThatAnswersInTimeIsAskedOnce(t *testing.T) {
	script := newScriptedBrain(t)
	defer script.close()
	script.plansAPipeline = true

	settled, log, err := planOnThinkingModel(t, script)
	if err != nil || !settled {
		t.Fatalf("the control run did not settle: settled=%v err=%v\n%s", settled, err, log)
	}
	ran := 0
	for _, known := range planningStages {
		bodies := script.bodiesFor(known.stage)
		if len(bodies) == 0 {
			continue
		}
		ran++
		for index, body := range bodies {
			if strings.Contains(body, scriptedThought) {
				t.Fatalf("the %s pass was asked for an answer it had already given", known.stage)
			}
			if reasoningBudget(t, body) <= 0 {
				t.Fatalf("request %d of the %s pass carried no thinking budget", index+1, known.stage)
			}
		}
	}
	if ran < 6 {
		t.Fatalf("only %d planning stages ran; the control is meant to run the same pipeline", ran)
	}
}
