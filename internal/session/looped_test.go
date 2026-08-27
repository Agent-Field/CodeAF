package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── the harness ─────────────────────────────────────────────────────────────

// repeatedCalls scripts one turn: the same call, times over, then an answer.
func repeatedCalls(name, arguments string, times int) []step {
	steps := make([]step, 0, times+1)
	for index := 0; index < times; index++ {
		id := fmt.Sprintf("%s-%d", name, index)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, name, arguments), nil
		})
	}
	return append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
}

// failingTool answers every call with the same error text, whatever it is asked.
func failingTool(name, message string) bare.Tool {
	return bare.Tool{
		Name:        name,
		Description: "always fails",
		Schema:      json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			return message, true, nil
		},
	}
}

// loopAgent builds an agent with the given belt tools and no approval policy —
// the ungated shape, so nothing but the loop detector can ask a question.
func loopAgent(t *testing.T, completer Completer, tools ...bare.Tool) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, nil)
	agent.tools = append(agent.tools, tools...)
	return agent
}

// nudges is every EventNudge in a drained turn.
func nudgeEvents(events []Event) []Event {
	var found []Event
	for _, event := range events {
		if event.Kind == EventNudge {
			found = append(found, event)
		}
	}
	return found
}

// transcriptNotes is every user-role message that is a nudge note.
func transcriptNotes(a *Agent) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var notes []string
	for _, message := range a.messages {
		if message.Role != "user" {
			continue
		}
		if text := messageContentText(message); strings.HasPrefix(text, "[stuck]") {
			notes = append(notes, text)
		}
	}
	return notes
}

// silentTranscriptNotes is every externalize-your-plan note the silent rule
// left in the transcript.
func silentTranscriptNotes(a *Agent) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var notes []string
	for _, message := range a.messages {
		if message.Role != "user" {
			continue
		}
		if text := messageContentText(message); strings.HasPrefix(text, "[silent]") {
			notes = append(notes, text)
		}
	}
	return notes
}

// ── the call rule ───────────────────────────────────────────────────────────

// Three identical calls in a row earn one nudge; the fourth earns nothing, and
// the fifth — the second repetition since the nudge — earns the next rung.
func TestTheSameCallIsNamedOnceThenNeedsRepeatedEvidence(t *testing.T) {
	completer := &scriptedCompleter{steps: repeatedCalls("touch", `{"path":"a.md"}`, 5)}
	agent := loopAgent(t, completer, countingTool("touch", make(chan string, 8), nil))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	fired := nudgeEvents(collected)
	if len(fired) != 2 {
		t.Fatalf("nudges: got %d, want 2 — one at the third call, one at the fifth (%v)", len(fired), fired)
	}
	if fired[0].Tool != "touch" || fired[0].Count != loopRepeats {
		t.Fatalf("nudge: tool %q ×%d, want touch ×%d", fired[0].Tool, fired[0].Count, loopRepeats)
	}
	// The fourth call is the hysteresis: one repetition after a nudge is not
	// enough evidence to say anything again.
	if fired[1].Count != loopRepeats+loopHysteresis {
		t.Fatalf("the second nudge fired at ×%d, want ×%d", fired[1].Count, loopRepeats+loopHysteresis)
	}

	notes := transcriptNotes(agent)
	if len(notes) != 2 {
		t.Fatalf("notes in the transcript: got %d, want 2 (%v)", len(notes), notes)
	}
	if !strings.Contains(notes[0], "repeated the same touch call 3 times") {
		t.Fatalf("the note does not say what happened: %q", notes[0])
	}

	// It rides into the NEXT request, the way a person's steering does — not as
	// an error, and not into the request that was already being assembled.
	fourth := completer.request(3)
	if len(fourth) == 0 {
		t.Fatal("the turn never made a fourth request")
	}
	last := fourth[len(fourth)-1]
	if last.Role != "user" || !strings.HasPrefix(messageContentText(last), "[stuck]") {
		t.Fatalf("the nudge did not ride into the next request; tail was %s: %q",
			last.Role, messageContentText(last))
	}
}

// A call repeated with other work between the repeats is a model working, not a
// model looping.
func TestARepeatWithWorkBetweenIsNotALoop(t *testing.T) {
	steps := []step{}
	for index := 0; index < 3; index++ {
		same, other := fmt.Sprintf("same-%d", index), fmt.Sprintf("other-%d", index)
		steps = append(steps,
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponseWithText(same, "touch", `{"path":"a.md"}`, "Checking a.md."), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponseWithText(other, "touch", `{"path":"b.md"}`, "Checking b.md."), nil
			})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, countingTool("touch", make(chan string, 8), nil))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if fired := nudgeEvents(collect(t, events)); len(fired) != 0 {
		t.Fatalf("nudged alternating work %d times, want 0", len(fired))
	}
}

// ── the error rule ──────────────────────────────────────────────────────────

// The same failure three times is a loop even when the model varies the call.
func TestTheSameErrorThreeTimesIsNudged(t *testing.T) {
	steps := []step{}
	for index := 0; index < 3; index++ {
		id, arguments := fmt.Sprintf("try-%d", index), fmt.Sprintf(`{"path":"file-%d.md"}`, index)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "build", arguments), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, failingTool("build", "undefined: Frobnicate"))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	fired := nudgeEvents(collect(t, events))
	if len(fired) != 1 {
		t.Fatalf("nudges: got %d, want 1", len(fired))
	}
	if fired[0].Tool != "build" || fired[0].Count != loopRepeats {
		t.Fatalf("nudge: tool %q ×%d, want build ×%d", fired[0].Tool, fired[0].Count, loopRepeats)
	}
	notes := transcriptNotes(agent)
	if len(notes) != 1 || !strings.Contains(notes[0], "failed the same way") {
		t.Fatalf("the note does not name the repeated failure: %v", notes)
	}
}

// ── the silent-streak rule ─────────────────────────────────────────────────

// Six distinct successful reads are exactly the case the identity rules cannot
// see: every call is new, but the model has carried no visible plan between
// steps. The sixth batch asks it to write that plan down.
func TestSixSilentToolBatchesAreNudged(t *testing.T) {
	steps := make([]step, 0, silentStreakLimit+1)
	for index := 0; index < silentStreakLimit; index++ {
		id, arguments := fmt.Sprintf("read-%d", index), fmt.Sprintf(`{"path":"file-%d.go"}`, index)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "read", arguments), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, countingTool("read", make(chan string, 8), nil))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	fired := nudgeEvents(collect(t, events))
	if len(fired) != 1 {
		t.Fatalf("nudges: got %d, want 1", len(fired))
	}
	if fired[0].Count != silentStreakLimit || !strings.HasPrefix(fired[0].Hint, "silent:") {
		t.Fatalf("silent nudge: count=%d hint=%q", fired[0].Count, fired[0].Hint)
	}
	notes := silentTranscriptNotes(agent)
	if len(notes) != 1 || !strings.Contains(notes[0], "reasoning between steps is not saved") {
		t.Fatalf("silent note does not explain the lost fact: %v", notes)
	}
}

func TestVisibleTextResetsTheSilentStreak(t *testing.T) {
	watch := newLoopWatch()
	observe := func(index int, visible bool) bool {
		call := ai.ToolCall{ID: fmt.Sprintf("r%d", index), Function: ai.ToolCallFunction{
			Name: "read", Arguments: fmt.Sprintf(`{"path":"%d"}`, index)}}
		_, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: "file"}}, visible)
		return fired
	}
	for index := 0; index < silentStreakLimit-1; index++ {
		if observe(index, false) {
			t.Fatalf("nudged on silent batch %d", index+1)
		}
	}
	if observe(silentStreakLimit-1, true) {
		t.Fatal("a batch with visible text nudged")
	}
	for index := silentStreakLimit; index < 2*silentStreakLimit-1; index++ {
		if observe(index, false) {
			t.Fatalf("the streak survived visible text and nudged on batch %d", index+1)
		}
	}
}

func TestFileWritingProgressResetsTheSilentStreak(t *testing.T) {
	watch := newLoopWatch()
	index := 0
	for cycle := 0; cycle < 2; cycle++ {
		for silent := 0; silent < silentStreakLimit-1; silent++ {
			call := ai.ToolCall{ID: fmt.Sprintf("r%d", index), Function: ai.ToolCallFunction{
				Name: "read", Arguments: fmt.Sprintf(`{"path":"%d"}`, index)}}
			index++
			if _, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: "file"}}, false); fired {
				t.Fatalf("nudged before write progress in cycle %d", cycle+1)
			}
		}
		write := ai.ToolCall{ID: fmt.Sprintf("w%d", cycle), Function: ai.ToolCallFunction{
			Name: "write", Arguments: fmt.Sprintf(`{"path":"%d","content":"done"}`, cycle)}}
		if _, fired := watch.observe([]ai.ToolCall{write}, []toolResult{{text: "wrote it"}}, false); fired {
			t.Fatalf("successful write nudged in cycle %d", cycle+1)
		}
	}
}

func TestTheSilentRuleNudgesOnlyOncePerTurn(t *testing.T) {
	watch := newLoopWatch()
	for index := 0; index < 2*silentStreakLimit; index++ {
		call := ai.ToolCall{ID: fmt.Sprintf("r%d", index), Function: ai.ToolCallFunction{
			Name: "read", Arguments: fmt.Sprintf(`{"path":"%d"}`, index)}}
		_, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: "file"}}, false)
		if fired != (index == silentStreakLimit-1) {
			t.Fatalf("batch %d fired=%v, want only batch %d", index+1, fired, silentStreakLimit)
		}
	}
	if watch.nudges != 1 {
		t.Fatalf("nudges: got %d, want 1", watch.nudges)
	}
}

func TestTheSilentNudgeCountsTowardConsentEscalation(t *testing.T) {
	var steps []step
	for _, name := range []string{"alpha", "beta"} {
		for index := 0; index < loopRepeats; index++ {
			id := fmt.Sprintf("%s-%d", name, index)
			steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponseWithText(id, name, `{"path":"same"}`, "Still working."), nil
			})
		}
	}
	for index := 0; index < silentStreakLimit; index++ {
		id, arguments := fmt.Sprintf("read-%d", index), fmt.Sprintf(`{"path":"file-%d"}`, index)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "read", arguments), nil
		})
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})

	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionPrompt,
			Tools: map[string]approval.Action{
				"alpha": approval.ActionAllow,
				"beta":  approval.ActionAllow,
				"read":  approval.ActionAllow,
			},
		}
		config.AskConsent = true
	})
	for _, name := range []string{"alpha", "beta", "read"} {
		agent.tools = append(agent.tools, countingTool(name, make(chan string, 16), nil))
	}

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var asked []Event
	collected := drainAnswering(t, events, func(event Event) {
		asked = append(asked, event)
		agent.ResolveConsent(event.ID, false)
	})
	if fired := nudgeEvents(collected); len(fired) != loopNudgeCeiling+1 {
		t.Fatalf("nudges: got %d, want %d", len(fired), loopNudgeCeiling+1)
	}
	if len(asked) != 1 || !strings.HasPrefix(asked[0].Rule, "silent:") {
		t.Fatalf("silent escalation questions: %+v", asked)
	}
}

// ── the reset ───────────────────────────────────────────────────────────────

// The window is a fact about one turn. The same loop in the next turn is nudged
// again, because the person's attention reset with the turn.
func TestTheWindowResetsEachTurn(t *testing.T) {
	steps := append(repeatedCalls("touch", `{"path":"a.md"}`, 3), repeatedCalls("touch", `{"path":"a.md"}`, 3)...)
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer, countingTool("touch", make(chan string, 8), nil))

	for turn := 1; turn <= 2; turn++ {
		events, err := agent.Submit(context.Background(), "go")
		if err != nil {
			t.Fatalf("Submit %d: %v", turn, err)
		}
		if fired := nudgeEvents(collect(t, events)); len(fired) != 1 {
			t.Fatalf("turn %d nudges: got %d, want 1", turn, len(fired))
		}
	}
}

// ── the escalation ──────────────────────────────────────────────────────────

// Past two nudges, in a session where the person is being asked about things,
// the third goes to the person instead of to the model.
func TestTheThirdNudgeAsksThePerson(t *testing.T) {
	var steps []step
	for _, name := range []string{"alpha", "beta", "gamma"} {
		steps = append(steps, repeatedCalls(name, `{"path":"a.md"}`, 3)[:3]...)
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})

	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		// Prompt mode — somebody is answering questions — with the three tools
		// themselves allowed, so the only question this turn can raise is the
		// loop detector's own.
		config.ApprovalPolicy = &approval.Policy{
			Default: approval.ActionPrompt,
			Tools: map[string]approval.Action{
				"alpha": approval.ActionAllow,
				"beta":  approval.ActionAllow,
				"gamma": approval.ActionAllow,
			},
		}
		config.AskConsent = true
	})
	for _, name := range []string{"alpha", "beta", "gamma"} {
		agent.tools = append(agent.tools, countingTool(name, make(chan string, 8), nil))
	}

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	var asked []Event
	collected := drainAnswering(t, events, func(event Event) {
		asked = append(asked, event)
		agent.ResolveConsent(event.ID, false)
	})

	if fired := nudgeEvents(collected); len(fired) != 3 {
		t.Fatalf("nudges: got %d, want 3", len(fired))
	}
	if len(asked) != 1 {
		t.Fatalf("the person was asked %d times, want 1 (the third nudge)", len(asked))
	}
	if !strings.HasPrefix(asked[0].Rule, "stuck:") || asked[0].Tool != "gamma" {
		t.Fatalf("the question does not name the loop: tool %q rule %q", asked[0].Tool, asked[0].Rule)
	}

	notes := transcriptNotes(agent)
	if len(notes) != 3 {
		t.Fatalf("notes: got %d, want 3 (two nudges and the person's answer)", len(notes))
	}
	if !strings.Contains(notes[2], "they said no") {
		t.Fatalf("the person's refusal did not reach the model: %q", notes[2])
	}
}

// With no policy at all — a headless caller, a test — there is nobody to ask, so
// the third nudge stays a note.
func TestWithoutPromptModeTheThirdNudgeIsStillANote(t *testing.T) {
	var steps []step
	for _, name := range []string{"alpha", "beta", "gamma"} {
		steps = append(steps, repeatedCalls(name, `{"path":"a.md"}`, 3)[:3]...)
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})
	completer := &scriptedCompleter{steps: steps}
	agent := loopAgent(t, completer,
		countingTool("alpha", make(chan string, 8), nil),
		countingTool("beta", make(chan string, 8), nil),
		countingTool("gamma", make(chan string, 8), nil))

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := drainAnswering(t, events, func(Event) {
		t.Error("a session with no policy asked the person a question")
	})
	if fired := nudgeEvents(collected); len(fired) != 3 {
		t.Fatalf("nudges: got %d, want 3", len(fired))
	}
	if notes := transcriptNotes(agent); len(notes) != 3 {
		t.Fatalf("notes: got %d, want 3", len(notes))
	}
}

// ── the watch itself ────────────────────────────────────────────────────────

func TestLoopWatchNamesASignatureThenClimbsOnRepeatedEvidence(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a"}`}}
	ok := func(ai.ToolCall) []toolResult { return []toolResult{{text: "fine"}} }

	for attempt := 1; attempt <= 2; attempt++ {
		if _, fired := watch.observe([]ai.ToolCall{call}, ok(call), true); fired {
			t.Fatalf("nudged after %d identical calls, want %d", attempt, loopRepeats)
		}
	}
	looping, fired := watch.observe([]ai.ToolCall{call}, ok(call), true)
	if !fired || looping.count != 3 || looping.nth != 1 {
		t.Fatalf("third identical call: fired=%v count=%d nth=%d", fired, looping.count, looping.nth)
	}

	// SAME ONCE IS NOT ENOUGH. The model has been told; one more repetition is
	// not yet evidence that the telling failed.
	if _, fired := watch.observe([]ai.ToolCall{call}, ok(call), true); fired {
		t.Fatal("one repetition after a nudge escalated; the ladder needs repeated evidence")
	}
	// SAME TWICE MORE IS. And the streak resets with the escalation, so the next
	// rung costs the same evidence again rather than firing every step.
	second, fired := watch.observe([]ai.ToolCall{call}, ok(call), true)
	if !fired || second.nth != 2 {
		t.Fatalf("the second rung: fired=%v nth=%d, want true/2", fired, second.nth)
	}
	if _, fired := watch.observe([]ai.ToolCall{call}, ok(call), true); fired {
		t.Fatal("the ladder climbed on a single repetition after escalating")
	}
	third, fired := watch.observe([]ai.ToolCall{call}, ok(call), true)
	if !fired || third.nth != 3 {
		t.Fatalf("the third rung: fired=%v nth=%d, want true/3", fired, third.nth)
	}
}

// A second, different loop in the same turn is its own signature and earns its
// own first nudge — the hysteresis is per signature, not a turn-wide budget.
func TestLoopWatchGivesADifferentLoopItsOwnNudge(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a"}`}}
	other := ai.ToolCall{ID: "2", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"b"}`}}
	ok := func(ai.ToolCall) []toolResult { return []toolResult{{text: "fine"}} }

	for attempt := 1; attempt <= 3; attempt++ {
		watch.observe([]ai.ToolCall{call}, ok(call), true)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, fired := watch.observe([]ai.ToolCall{other}, ok(other), true); fired {
			t.Fatalf("nudged after %d calls of the second loop", attempt)
		}
	}
	second, fired := watch.observe([]ai.ToolCall{other}, ok(other), true)
	if !fired || second.nth != 2 {
		t.Fatalf("second loop: fired=%v nth=%d, want true/2", fired, second.nth)
	}
}

// ── the hysteresis law ──────────────────────────────────────────────────────

// Forward progress kills the backward case immediately: a successful call the
// turn has not been nudged about empties the streak, so the evidence for the
// next rung has to be gathered again from nothing.
//
// The repeated ERROR rule is what this is tested through, because it is the one
// that counts across a turn rather than along a consecutive tail: a different
// call between two repeats already breaks the call rule's run, so only here can
// progress and repetition be told apart.
func TestForwardProgressResetsTheBackwardStreak(t *testing.T) {
	watch := newLoopWatch()
	failing := func(n int) ([]ai.ToolCall, []toolResult) {
		call := ai.ToolCall{ID: fmt.Sprintf("f%d", n), Function: ai.ToolCallFunction{
			Name: "build", Arguments: fmt.Sprintf(`{"target":%d}`, n)}}
		return []ai.ToolCall{call}, []toolResult{{text: "undefined: Frobnicate", isError: true}}
	}
	observe := func(n int) (nudge, bool) {
		calls, results := failing(n)
		return watch.observe(calls, results, true)
	}

	observe(1)
	observe(2)
	if _, fired := observe(3); !fired {
		t.Fatal("three identical failures did not nudge")
	}
	if _, fired := observe(4); fired {
		t.Fatal("one repetition after a nudge escalated")
	}

	// A successful call the turn has not been nudged about: forward evidence.
	progress := ai.ToolCall{ID: "ok", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"x"}`}}
	watch.observe([]ai.ToolCall{progress}, []toolResult{{text: "the file"}}, true)

	if _, fired := observe(5); fired {
		t.Fatal("the streak survived forward progress: the fifth failure escalated on one repetition")
	}
	if _, fired := observe(6); !fired {
		t.Fatal("the streak never rebuilt: two repetitions after progress did not escalate")
	}
}

// A batch that both repeated and got something done does not escalate. Forward
// evidence is read first, so the two facts in one batch resolve toward the
// model working rather than toward the harness interrupting.
func TestABatchThatAlsoProgressedDoesNotEscalate(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a"}`}}
	ok := []toolResult{{text: "fine"}}
	for attempt := 1; attempt <= 3; attempt++ {
		watch.observe([]ai.ToolCall{call}, ok, true)
	}

	progress := ai.ToolCall{ID: "2", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"x"}`}}
	both := []ai.ToolCall{call, progress}
	results := []toolResult{{text: "fine"}, {text: "the file"}}
	for attempt := 1; attempt <= 3; attempt++ {
		if _, fired := watch.observe(both, results, true); fired {
			t.Fatalf("a batch that also progressed escalated (batch %d)", attempt)
		}
	}
}

// One batch produces at most one nudge, even when both rules fire on it.
func TestOneBatchSaysOneThing(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "build", Arguments: "{}"}}
	failed := []toolResult{{text: "undefined: Frobnicate", isError: true}}

	watch.observe([]ai.ToolCall{call}, failed, true)
	watch.observe([]ai.ToolCall{call}, failed, true)
	looping, fired := watch.observe([]ai.ToolCall{call}, failed, true)
	if !fired {
		t.Fatal("three identical failing calls did not nudge")
	}
	if looping.failing {
		t.Fatal("the error rule spoke over the call rule for the same batch")
	}
	if watch.nudges != 1 {
		t.Fatalf("nudges: got %d, want 1", watch.nudges)
	}
}
