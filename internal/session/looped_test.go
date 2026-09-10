package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

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

// freshTool answers each execution with a fresh line, so a test aimed at one of
// the identity rules does not accidentally exercise the ledger rule beside it.
func freshTool(name string) bare.Tool {
	run := 0
	return bare.Tool{
		Name: name, Description: "returns a fresh line",
		Schema: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			run++
			return fmt.Sprintf("%s result %d", name, run), false, nil
		},
	}
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

// ── the call rule ───────────────────────────────────────────────────────────

// Three identical calls in a row earn one nudge; the fourth earns nothing, and
// the fifth — the second repetition since the nudge — earns the next rung.
func TestTheSameCallIsNamedOnceThenNeedsRepeatedEvidence(t *testing.T) {
	completer := &scriptedCompleter{steps: repeatedCalls("touch", `{"path":"a.md"}`, 5)}
	agent := loopAgent(t, completer, freshTool("touch"))

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
	agent := loopAgent(t, completer, freshTool("touch"))

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

// ── repeated observations ──────────────────────────────────────────────────

// Distinct calls can still repeat one fact. Empty answers make the ledger's
// judgement unambiguous: none of the six rounds brought back a fresh line.
func TestDistinctCallsThatReadNothingNewAreNudged(t *testing.T) {
	watch := newLoopWatch()
	var notes []string
	for round := 0; round < noNewInformationLimit+1; round++ {
		call := ai.ToolCall{ID: fmt.Sprintf("r%d", round), Function: ai.ToolCallFunction{
			Name: "bash", Arguments: fmt.Sprintf(`{"command":"git show file | grep pattern-%d"}`, round)}}
		if looping, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: ""}}); fired {
			notes = append(notes, nudgeNote(looping))
		}
	}
	if len(notes) != 1 {
		t.Fatalf("no-new-information notes = %d, want 1: %v", len(notes), notes)
	}
	if !strings.Contains(notes[0], fmt.Sprintf("last %d rounds read nothing new", noNewInformationLimit)) ||
		!strings.Contains(notes[0], "already in the transcript") {
		t.Fatalf("note does not name the ledger fact: %q", notes[0])
	}
}

func TestAFreshWriteResetsNoNewInformationStreaks(t *testing.T) {
	watch := newLoopWatch()
	read := func(round int) bool {
		call := ai.ToolCall{ID: fmt.Sprintf("r%d", round), Function: ai.ToolCallFunction{
			Name: "bash", Arguments: fmt.Sprintf(`{"command":"empty-%d"}`, round)}}
		_, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: ""}})
		return fired
	}
	for round := 0; round < noNewInformationLimit-1; round++ {
		if read(round) {
			t.Fatalf("nudged before the write at round %d", round+1)
		}
	}
	write := ai.ToolCall{ID: "w", Function: ai.ToolCallFunction{
		Name: "write", Arguments: `{"path":"notes.md","content":"new"}`}}
	if _, fired := watch.observe([]ai.ToolCall{write}, []toolResult{{text: "wrote notes.md"}}); fired {
		t.Fatal("the fresh write nudged")
	}
	for round := noNewInformationLimit - 1; round < 2*(noNewInformationLimit-1); round++ {
		if read(round) {
			t.Fatalf("a streak survived the write and nudged at round %d", round+1)
		}
	}
}

func TestHarnessMadeBatchesDoNotAdvanceAnyStreak(t *testing.T) {
	watch := newLoopWatch()
	for round := 0; round < 30; round++ {
		call := ai.ToolCall{ID: fmt.Sprintf("h%d", round), Function: ai.ToolCallFunction{
			Name: "bash", Arguments: fmt.Sprintf(`{"command":"refused-%d"}`, round)}}
		if _, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: "refused", harness: true}}); fired {
			t.Fatalf("the harness's own answer nudged at round %d", round+1)
		}
	}
	if watch.noNewStreak != 0 || watch.nudges != 0 {
		t.Fatalf("harness batches changed the watch: no-new=%d nudges=%d",
			watch.noNewStreak, watch.nudges)
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

// Past two nudges the turn ends. This fixture has no consent surface, so it
// exercises the honest fallback instead of asking the scripted model for a
// checkpoint brief: the tenth scripted response must never be requested.
func TestTheThirdNudgeEndsTheTurnAndSaysWhatWasLeft(t *testing.T) {
	var steps []step
	for _, name := range []string{"alpha", "beta", "gamma"} {
		steps = append(steps, repeatedCalls(name, `{"path":"a.md"}`, 3)[:3]...)
	}
	steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("done"), nil
	})

	completer := &scriptedCompleter{steps: steps}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = false })
	for _, name := range []string{"alpha", "beta", "gamma"} {
		agent.tools = append(agent.tools, countingTool(name, make(chan string, 8), nil))
	}

	events, err := agent.Submit(context.Background(), "go")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if fired := nudgeEvents(collected); len(fired) != 3 {
		t.Fatalf("nudges: got %d, want 3", len(fired))
	}
	if completer.requests() != 3*loopRepeats {
		t.Fatalf("provider requests = %d, want %d; the turn did not end at the ceiling",
			completer.requests(), 3*loopRepeats)
	}
	left, ok := firstOfKind(collected, EventNotice)
	if !ok || left.Text != loopLeftUndoneNote {
		t.Fatalf("left-undone notice = %q, present=%v", left.Text, ok)
	}
	if collected[len(collected)-1].Kind != EventTurnDone {
		t.Fatalf("last event = %v, want EventTurnDone", collected[len(collected)-1].Kind)
	}
	if notes := transcriptNotes(agent); len(notes) != loopNudgeCeiling {
		t.Fatalf("notes: got %d, want only the %d warnings before the ceiling", len(notes), loopNudgeCeiling)
	}
}

// ── the watch itself ────────────────────────────────────────────────────────

func TestLoopWatchNamesASignatureThenClimbsOnRepeatedEvidence(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a"}`}}
	result := 0
	ok := func(ai.ToolCall) []toolResult {
		result++
		return []toolResult{{text: fmt.Sprintf("result %d", result)}}
	}

	for attempt := 1; attempt <= 2; attempt++ {
		if _, fired := watch.observe([]ai.ToolCall{call}, ok(call)); fired {
			t.Fatalf("nudged after %d identical calls, want %d", attempt, loopRepeats)
		}
	}
	looping, fired := watch.observe([]ai.ToolCall{call}, ok(call))
	if !fired || looping.count != 3 || looping.nth != 1 {
		t.Fatalf("third identical call: fired=%v count=%d nth=%d", fired, looping.count, looping.nth)
	}

	// SAME ONCE IS NOT ENOUGH. The model has been told; one more repetition is
	// not yet evidence that the telling failed.
	if _, fired := watch.observe([]ai.ToolCall{call}, ok(call)); fired {
		t.Fatal("one repetition after a nudge escalated; the ladder needs repeated evidence")
	}
	// SAME TWICE MORE IS. And the streak resets with the escalation, so the next
	// rung costs the same evidence again rather than firing every step.
	second, fired := watch.observe([]ai.ToolCall{call}, ok(call))
	if !fired || second.nth != 2 {
		t.Fatalf("the second rung: fired=%v nth=%d, want true/2", fired, second.nth)
	}
	if _, fired := watch.observe([]ai.ToolCall{call}, ok(call)); fired {
		t.Fatal("the ladder climbed on a single repetition after escalating")
	}
	third, fired := watch.observe([]ai.ToolCall{call}, ok(call))
	if !fired || third.nth != 3 {
		t.Fatalf("the third rung: fired=%v nth=%d, want true/3", fired, third.nth)
	}
	for attempt := 0; attempt < 3; attempt++ {
		if _, fired := watch.observe([]ai.ToolCall{call}, ok(call)); fired {
			t.Fatal("the watch produced a fourth nudge after its terminal signal")
		}
	}
}

// A second, different loop in the same turn is its own signature and earns its
// own first nudge — the hysteresis is per signature, not a turn-wide budget.
func TestLoopWatchGivesADifferentLoopItsOwnNudge(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a"}`}}
	other := ai.ToolCall{ID: "2", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"b"}`}}
	result := 0
	ok := func(ai.ToolCall) []toolResult {
		result++
		return []toolResult{{text: fmt.Sprintf("result %d", result)}}
	}

	for attempt := 1; attempt <= 3; attempt++ {
		watch.observe([]ai.ToolCall{call}, ok(call))
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if _, fired := watch.observe([]ai.ToolCall{other}, ok(other)); fired {
			t.Fatalf("nudged after %d calls of the second loop", attempt)
		}
	}
	second, fired := watch.observe([]ai.ToolCall{other}, ok(other))
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
		return watch.observe(calls, results)
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
	watch.observe([]ai.ToolCall{progress}, []toolResult{{text: "the file"}})

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
		watch.observe([]ai.ToolCall{call}, ok)
	}

	progress := ai.ToolCall{ID: "2", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"x"}`}}
	both := []ai.ToolCall{call, progress}
	results := []toolResult{{text: "fine"}, {text: "the file"}}
	for attempt := 1; attempt <= 3; attempt++ {
		if _, fired := watch.observe(both, results); fired {
			t.Fatalf("a batch that also progressed escalated (batch %d)", attempt)
		}
	}
}

// One batch produces at most one nudge, even when both rules fire on it.
func TestOneBatchSaysOneThing(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "build", Arguments: "{}"}}
	failed := []toolResult{{text: "undefined: Frobnicate", isError: true}}

	watch.observe([]ai.ToolCall{call}, failed)
	watch.observe([]ai.ToolCall{call}, failed)
	looping, fired := watch.observe([]ai.ToolCall{call}, failed)
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

// ── silence is hygiene, not stuckness ───────────────────────────────────────

// Distinct successful observations do not spend the repetition budget.
func TestUsefulObservationsDoNotSpendTheLoopBudget(t *testing.T) {
	watch := newLoopWatch()
	for round := 0; round < 30; round++ {
		call := ai.ToolCall{ID: fmt.Sprintf("s%d", round), Function: ai.ToolCallFunction{Name: "bash", Arguments: fmt.Sprintf(`{"command":"git show %d"}`, round)}}
		if looping, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: fmt.Sprintf("commit %d", round)}}); fired {
			t.Fatalf("useful work nudged: %q", loopRule(looping))
		}
	}
	if watch.nudges != 0 {
		t.Fatalf("nudges=%d", watch.nudges)
	}
}

// AND THE RULES THAT DO MEAN STUCK STILL END A TURN, after any amount of
// silence. This is the guard on the change above: making silence free must not
// make a real loop free with it.
func TestARealLoopStillHandsOverAfterASilentRun(t *testing.T) {
	watch := newLoopWatch()
	for round := range 18 {
		call := ai.ToolCall{ID: fmt.Sprintf("q%d", round), Function: ai.ToolCallFunction{
			Name: "bash", Arguments: fmt.Sprintf(`{"command":"git show %d"}`, round)}}
		watch.observe([]ai.ToolCall{call}, []toolResult{{text: fmt.Sprintf("commit %d", round)}})
	}
	if watch.nudges != 0 {
		t.Fatalf("the silent run spent %d of the count before the loop began", watch.nudges)
	}

	// Three identical failing calls, three times over: the ordinary ladder.
	highest := 0
	for round := range 12 {
		call := ai.ToolCall{ID: fmt.Sprintf("b%d", round), Function: ai.ToolCallFunction{
			Name: "bash", Arguments: `{"command":"make build"}`}}
		looping, fired := watch.observe([]ai.ToolCall{call},
			[]toolResult{{text: "undefined: Frobnicate", isError: true}})
		if fired && looping.nth > highest {
			highest = looping.nth
		}
	}
	if highest <= loopNudgeCeiling {
		t.Fatalf("the highest nudge was %d; a real loop after silence must still pass %d",
			highest, loopNudgeCeiling)
	}
}

// ── the tree is the third kind of progress ──────────────────────────────────

// A commit phase is pure bash, so edit-and-write alone reads it as silence. The
// tree does not: a command that left something behind is the one claim nobody
// can argue with, and it breaks the ladder exactly as a write does.
func TestAShellCommandThatMovedTheTreeIsProgress(t *testing.T) {
	repo := newTestRepo(t)
	watch := newLoopWatch()
	watch.dir = repo

	round := 0
	shell := func(wrote bool) bool {
		if wrote {
			writeFile(t, filepath.Join(repo, fmt.Sprintf("made-%d.txt", round)), "landed")
		}
		call := ai.ToolCall{ID: fmt.Sprintf("c%d", round), Function: ai.ToolCallFunction{
			Name: "bash", Arguments: fmt.Sprintf(`{"command":"land %d"}`, round)}}
		result := fmt.Sprintf("landed step %d", round)
		round++
		_, fired := watch.observe([]ai.ToolCall{call}, []toolResult{{text: result}})
		return fired
	}

	// The first reading is the baseline: a tree that is already dirty is not work
	// this batch did, so it may not read as progress, and neither may a second
	// reading of a tree nothing has touched since.
	writeFile(t, filepath.Join(repo, "already-there.txt"), "before the turn")
	if watch.treeMoved() {
		t.Fatal("the baseline reading counted the tree's existing dirt as this turn's work")
	}
	if watch.treeMoved() {
		t.Fatal("a second reading of an unchanged tree claimed it had moved")
	}

	for cycle := range 3 {
		for range 6 - 1 {
			if shell(false) {
				t.Fatalf("nudged before the landing command in cycle %d", cycle+1)
			}
		}
		if shell(true) {
			t.Fatalf("the command that moved the tree nudged in cycle %d", cycle+1)
		}
	}
	if watch.nudges != 0 {
		t.Fatalf("nudges=%d after three landings", watch.nudges)
	}
}

// A watch with no directory behind it, and one pointed at a plain folder, both
// answer "nothing moved" forever — stable, so they never move a counter either
// way, and the small unit tests above are not quietly running git.
func TestAWatchWithNoRepositoryNeverSeesTheTreeMove(t *testing.T) {
	for name, dir := range map[string]string{"no workspace": "", "not a repository": t.TempDir()} {
		watch := newLoopWatch()
		watch.dir = dir
		for round := range 3 {
			if watch.treeMoved() {
				t.Fatalf("%s: reading %d claimed the tree moved", name, round+1)
			}
		}
	}
}

// ── the count comes back ────────────────────────────────────────────────────

// Two loops earn two notes. Then the turn gets something done, and the third
// loop is a note rather than the end of the turn — the count stepped down by
// one. It is a step and not a reset: a second real loop after that reaches the
// ceiling, so the turn is forgiven quickly and still remembered.
func TestMaterialProgressGivesOneSpentNoteBack(t *testing.T) {
	watch := newLoopWatch()
	loop := func(name string) (nudge, bool) {
		call := ai.ToolCall{ID: name, Function: ai.ToolCallFunction{
			Name: name, Arguments: `{"path":"a"}`}}
		var last nudge
		var fired bool
		for range loopRepeats {
			last, fired = watch.observe([]ai.ToolCall{call}, []toolResult{{text: "fine"}})
		}
		return last, fired
	}
	wrote := func() {
		call := ai.ToolCall{ID: "w", Function: ai.ToolCallFunction{
			Name: "write", Arguments: `{"path":"out.md","content":"landed"}`}}
		watch.observe([]ai.ToolCall{call}, []toolResult{{text: "wrote out.md"}})
	}

	if first, fired := loop("alpha"); !fired || first.nth != 1 {
		t.Fatalf("first loop: fired=%v nth=%d", fired, first.nth)
	}
	if second, fired := loop("beta"); !fired || second.nth != 2 {
		t.Fatalf("second loop: fired=%v nth=%d", fired, second.nth)
	}
	wrote()
	if watch.nudges != 1 {
		t.Fatalf("nudges after progress = %d, want 1 — a step down, not a reset", watch.nudges)
	}
	third, fired := loop("gamma")
	if !fired || third.nth != loopNudgeCeiling {
		t.Fatalf("third loop after progress: fired=%v nth=%d, want %d", fired, third.nth, loopNudgeCeiling)
	}
	fourth, fired := loop("delta")
	if !fired || fourth.nth <= loopNudgeCeiling {
		t.Fatalf("fourth loop: fired=%v nth=%d, want past %d", fired, fourth.nth, loopNudgeCeiling)
	}
}

// And the WEAK evidence does not buy the count back. The first calls of a
// turn's next loop are, by construction, calls nobody has been nudged about
// yet; a budget those refunded would be no budget at all.
func TestASuccessfulUnnamedCallDoesNotGiveTheCountBack(t *testing.T) {
	watch := newLoopWatch()
	call := ai.ToolCall{ID: "1", Function: ai.ToolCallFunction{Name: "touch", Arguments: `{"path":"a"}`}}
	for range loopRepeats {
		watch.observe([]ai.ToolCall{call}, []toolResult{{text: "fine"}})
	}
	if watch.nudges != 1 {
		t.Fatalf("nudges = %d after one loop, want 1", watch.nudges)
	}
	read := ai.ToolCall{ID: "2", Function: ai.ToolCallFunction{Name: "read", Arguments: `{"path":"x"}`}}
	watch.observe([]ai.ToolCall{read}, []toolResult{{text: "a line nobody has read"}})
	if watch.nudges != 1 {
		t.Fatalf("a successful read gave the count back: nudges = %d, want 1", watch.nudges)
	}
}
