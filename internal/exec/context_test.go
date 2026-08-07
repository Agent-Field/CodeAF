package exec

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// steadyLeaf replays what a leaf actually does to its transcript: every turn
// appends an assistant message and one tool result of a fixed size, then runs
// decay before the next call. It reports what each turn did to the messages
// that were already there, which is the only thing a prefix cache cares about.
type steadyLeaf struct {
	fade     *decayer
	messages []ai.Message
	previous map[int]string // index → content after the previous turn's decay
}

func newSteadyLeaf() *steadyLeaf {
	return &steadyLeaf{fade: newDecayer(map[string]string{}, nil), previous: map[int]string{}}
}

// turn appends one assistant/tool pair of the given result size and decays.
// rewrote is true when the pass changed a message that already existed — the
// event that invalidates the provider's cached prefix from that point on.
func (l *steadyLeaf) turn(t *testing.T, size, budget int) (rewrote, wasOver bool) {
	t.Helper()
	id := fmt.Sprintf("c%d", len(l.messages))
	call := ai.ToolCall{ID: id, Type: "function",
		Function: ai.ToolCallFunction{Name: "sh", Arguments: `{"cmd":"ls"}`}}
	l.fade.labels[id] = callLabel(call)
	l.messages = append(l.messages,
		ai.Message{Role: "assistant", Content: text("thinking " + id), ToolCalls: []ai.ToolCall{call}},
		ai.Message{Role: "tool", ToolCallID: id, Content: text(strings.Repeat("x", size))},
	)
	wasOver = liveObservationBytes(l.messages) > budget
	l.fade.decay(l.messages, budget)
	for index, before := range l.previous {
		if contentOf(l.messages[index]) != before {
			rewrote = true
			break
		}
	}
	for index := range l.messages {
		l.previous[index] = contentOf(l.messages[index])
	}
	return rewrote, wasOver
}

// stubbedIDs is the set of tool_call_ids currently carrying a stub.
func (l *steadyLeaf) stubbedIDs() map[string]bool {
	stubbed := map[string]bool{}
	for _, message := range l.messages {
		if message.Role == "tool" && strings.Contains(contentOf(message), "superseded") {
			stubbed[message.ToolCallID] = true
		}
	}
	return stubbed
}

// TestDecayBatchesRewrites is the cache-economics test. A trim-to-budget pass
// retires exactly what the newest result displaced, so once the window is full
// it rewrites an old message on every single turn and the transcript is never
// cached past the fade line. Batching to a low-water mark spends the same
// number of stubs but groups them: one invalidation per K turns instead of K.
//
// The old behaviour is not run, it is counted: a pass that always refills to
// exactly the budget holds the window pinned there, so every turn from the
// first crossing onward displaces something and rewrites. That is turns minus
// the crossing turn. The new count has to come out near turns-over-budget
// divided by the turns of headroom one batch buys.
func TestDecayBatchesRewrites(t *testing.T) {
	const (
		budget = observationBudget
		result = 2 << 10
		turns  = 40
	)
	lowWater := decayLowWater(budget)
	// Turns of headroom each batch buys: the gap it clears, divided by what a
	// turn adds.
	perBatch := (budget - lowWater) / result
	// The turn on which raw output first exceeds the budget, counting from one.
	crossing := budget/result + 1
	// What trimming to exactly the budget every turn would have cost: a rewrite
	// on the crossing turn and on every turn after it.
	trimEveryTurn := turns - crossing + 1

	leaf := newSteadyLeaf()
	rewrites, overBudget := 0, 0
	for turn := 0; turn < turns; turn++ {
		rewrote, wasOver := leaf.turn(t, result, budget)
		if rewrote {
			rewrites++
		}
		if wasOver {
			overBudget++
		}
	}

	if rewrites == 0 {
		t.Fatal("nothing was ever rewritten; decay stopped working")
	}
	if overBudget != rewrites {
		t.Errorf("decay fired on %d turns but %d turns were over budget; it must fire exactly when the window crosses the line, never otherwise", rewrites, overBudget)
	}
	// Batch math: the crossing turn, plus one batch for every perBatch turns
	// after it. Stub bytes left behind by earlier batches count against the
	// window too, which can pull one extra batch forward.
	expected := trimEveryTurn/perBatch + 1
	if rewrites < expected-1 || rewrites > expected+1 {
		t.Errorf("rewrote history on %d of %d turns, want about %d (%d turns over budget / %d turns of headroom per batch)",
			rewrites, turns, expected, trimEveryTurn, perBatch)
	}
	if rewrites*2 > trimEveryTurn {
		t.Errorf("rewrote on %d turns; trimming to the budget every turn would rewrite on all %d, and hysteresis must at least halve that",
			rewrites, trimEveryTurn)
	}
}

// TestDecayHoldsTheBudgetInvariant pins what may not change. The window may
// never sit above the budget once decay has run, whatever the low-water mark
// does; a firing pass must clear down to the mark, give or take the newest
// turn's own output, which is measured against the full budget because the
// model has not read it yet; and nothing that was stubbed may ever be un-stubbed
// or rewritten.
func TestDecayHoldsTheBudgetInvariant(t *testing.T) {
	const (
		budget = observationBudget
		result = 3 << 10
		turns  = 30
	)
	lowWater := decayLowWater(budget)

	leaf := newSteadyLeaf()
	stubbed := map[string]bool{}
	frozen := map[string]string{} // tool_call_id → the stub it was given
	fired := 0
	for turn := 0; turn < turns; turn++ {
		before := leaf.stubbedIDs()
		rewrote, _ := leaf.turn(t, result, budget)
		if rewrote {
			fired++
		}

		live := liveObservationBytes(leaf.messages)
		if live > budget {
			t.Fatalf("turn %d: %d live bytes against a %d-byte budget — decay must never leave the window over budget", turn, live, budget)
		}
		// A firing pass clears to the mark; the newest turn's result is held
		// back from the mark by design, so it is the allowance.
		if rewrote && live > lowWater+result {
			t.Errorf("turn %d: a firing decay left %d live bytes, want at most the %d-byte low-water mark plus the newest result", turn, live, lowWater+result)
		}

		now := leaf.stubbedIDs()
		for id := range before {
			if !now[id] {
				t.Fatalf("turn %d: %s was un-stubbed; decay is write-once", turn, id)
			}
		}
		for _, message := range leaf.messages {
			if message.Role != "tool" || !now[message.ToolCallID] {
				continue
			}
			body := contentOf(message)
			if was, seen := frozen[message.ToolCallID]; seen && was != body {
				t.Fatalf("turn %d: stub for %s changed from %q to %q; a stub is written once and then never touched again", turn, message.ToolCallID, was, body)
			}
			frozen[message.ToolCallID] = body
			stubbed[message.ToolCallID] = true
		}
	}
	if fired == 0 || len(stubbed) == 0 {
		t.Fatalf("decay fired %d times and stubbed %d results; the run never exercised the invariant", fired, len(stubbed))
	}
}

// TestDecayLowWaterRespectsTheFloor checks the two ends of the budget. The
// floor is what keeps a small task's window usable, and the mark has to stay
// under the budget it fired at or the invariant above is unenforceable.
func TestDecayLowWaterRespectsTheFloor(t *testing.T) {
	for _, budget := range []int{observationBudget, observationBudget * 3, maxObservationBudget} {
		mark := decayLowWater(budget)
		if mark >= budget {
			t.Errorf("low-water mark %d for budget %d does not retire anything", mark, budget)
		}
		if mark < budget/2 {
			t.Errorf("low-water mark %d for budget %d gives up more than half the window", mark, budget)
		}
	}
	if decayLowWater(observationBudget) != observationBudget*decayLowWaterPercent/100 {
		t.Error("the mark is no longer a fixed fraction of the budget")
	}
}

// TestDecayStubsAnOversizedResult is the degenerate case, unchanged by
// batching: one result larger than the entire budget cannot be kept by any
// mark, so it is stubbed on its own — and, being alone, it still leaves the
// window as small as it can be made.
func TestDecayStubsAnOversizedResult(t *testing.T) {
	huge := strings.Repeat("q", observationBudget*2)
	messages := []ai.Message{
		{Role: "user", Content: text("do the thing")},
		{Role: "assistant", Content: text("looking"), ToolCalls: []ai.ToolCall{{ID: "big"}}},
		{Role: "tool", ToolCallID: "big", Content: text(huge)},
	}
	fade := newDecayer(map[string]string{"big": "sh cat huge"}, nil)

	if decayed := fade.decay(messages, observationBudget); decayed != 1 {
		t.Fatalf("decayed = %d, want the one oversized result stubbed", decayed)
	}
	stub := contentOf(messages[2])
	if !strings.Contains(stub, "sh cat huge") || !strings.Contains(stub, "superseded") {
		t.Errorf("stub %q does not name what was retired", stub)
	}
	if again := fade.decay(messages, observationBudget); again != 0 {
		t.Errorf("a second pass decayed %d more; the stub must be left alone", again)
	}
	if contentOf(messages[2]) != stub {
		t.Error("the stub was rewritten on a later pass")
	}
}

// TestDecayPreservesPairing guards the one hard constraint. Every tool message
// answers an assistant tool_call by id; dropping one, or losing its id, is a
// provider-level rejection rather than a degraded prompt. Decay may only ever
// shorten content.
func TestDecayPreservesPairing(t *testing.T) {
	big := strings.Repeat("x", 20<<10)
	messages := []ai.Message{
		{Role: "system", Content: text("rules")},
		{Role: "user", Content: text("do the thing")},
	}
	labels := map[string]string{}
	for turn := 0; turn < 5; turn++ {
		id := string(rune('a' + turn))
		call := ai.ToolCall{ID: id, Type: "function",
			Function: ai.ToolCallFunction{Name: "sh", Arguments: `{"cmd":"ls"}`}}
		messages = append(messages,
			ai.Message{Role: "assistant", Content: text("thinking " + id), ToolCalls: []ai.ToolCall{call}},
			ai.Message{Role: "tool", ToolCallID: id, Content: text(big)},
		)
		labels[id] = callLabel(call)
	}
	before := len(messages)

	decayed := newDecayer(labels, nil).decay(messages, observationBudget)

	if len(messages) != before {
		t.Fatalf("message count changed from %d to %d — decay must never remove a message", before, len(messages))
	}
	if decayed == 0 {
		t.Fatal("nothing decayed despite 100KB of observations against a 24KB budget")
	}

	var toolMessages, stubbed, kept int
	for _, message := range messages {
		switch message.Role {
		case "assistant":
			if !strings.HasPrefix(contentOf(message), "thinking ") {
				t.Error("an assistant message was modified; those are the compressed state and must survive")
			}
		case "tool":
			toolMessages++
			if message.ToolCallID == "" {
				t.Error("a tool message lost its ToolCallID and can no longer be paired")
			}
			if strings.Contains(contentOf(message), "superseded") {
				stubbed++
			} else {
				kept++
			}
		}
	}
	if toolMessages != 5 {
		t.Errorf("tool messages = %d, want 5", toolMessages)
	}
	if kept == 0 {
		t.Error("everything was stubbed; the newest observations must survive in full")
	}
	if stubbed == 0 {
		t.Error("nothing was stubbed despite exceeding the budget")
	}
}

// TestDecayKeepsNewestInFull checks the direction of the walk. The most recent
// observation is the one the model is actually reasoning about.
func TestDecayKeepsNewestInFull(t *testing.T) {
	big := strings.Repeat("y", 20<<10)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: "old", Content: text(big)},
		{Role: "tool", ToolCallID: "new", Content: text(big)},
	}
	newDecayer(map[string]string{"old": "sh ls", "new": "sh pwd"}, nil).decay(messages, observationBudget)

	if strings.Contains(contentOf(messages[1]), "superseded") {
		t.Error("the newest observation was stubbed")
	}
	if !strings.Contains(contentOf(messages[0]), "superseded") {
		t.Error("the oldest observation survived past the budget")
	}
	if !strings.Contains(contentOf(messages[0]), "sh ls") {
		t.Error("the stub does not say what the result was, so the model cannot tell it already ran that")
	}
}

// TestDecayLeavesSmallResultsAlone confirms the budget is spent on what costs.
// Many small observations should all survive; stubbing them would lose detail
// for no saving.
func TestDecayLeavesSmallResultsAlone(t *testing.T) {
	var messages []ai.Message
	for index := 0; index < 30; index++ {
		messages = append(messages, ai.Message{
			Role: "tool", ToolCallID: string(rune('a' + index)), Content: text("ok"),
		})
	}
	if decayed := newDecayer(nil, nil).decay(messages, observationBudget); decayed != 0 {
		t.Errorf("%d small results were stubbed; they cost nothing to keep", decayed)
	}
}

// TestDecaySpillsLosslessly is the decay-to-pointer contract: a stubbed result
// must name the file its bytes went to, and that file must hold the original
// bytes exactly — decay defers detail, it never destroys it.
func TestDecaySpillsLosslessly(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, 2, nil)
	old := strings.Repeat("A", 20<<10)
	fresh := strings.Repeat("B", 20<<10)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: "c7", Content: text(old)},
		{Role: "tool", ToolCallID: "c8", Content: text(fresh)},
	}
	labels := map[string]string{"c7": "sh cat foo", "c8": "sh cat bar"}

	decayed := newDecayer(labels, tools.decaySpill).decay(messages, observationBudget)
	if decayed != 1 {
		t.Fatalf("decayed = %d, want exactly the older result", decayed)
	}

	stub := contentOf(messages[0])
	if !strings.Contains(stub, "sh cat foo") || !strings.Contains(stub, "20480 bytes") {
		t.Errorf("stub %q does not identify what was decayed", stub)
	}
	wantPath := filepath.Join(obsDir, "2-decay-c7.txt")
	if !strings.Contains(stub, "spilled to "+wantPath) {
		t.Fatalf("stub %q does not carry the spill path %q", stub, wantPath)
	}
	body, err := os.ReadFile(filepath.Join(space.Root(), wantPath))
	if err != nil {
		t.Fatalf("the spill file the stub points at cannot be read: %v", err)
	}
	if string(body) != old {
		t.Errorf("spill file holds %d bytes, want the original %d untouched", len(body), len(old))
	}
	if contentOf(messages[1]) != fresh {
		t.Error("the newest observation was touched; only older ones may decay")
	}
}

// TestDecayIsIdempotentAcrossTurns guards the every-turn re-run. Decay walks
// the same transcript before each model call; the second pass must not write
// the file again, grow the stub, or stub the stub.
func TestDecayIsIdempotentAcrossTurns(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, 4, nil)
	big := strings.Repeat("z", 20<<10)
	messages := []ai.Message{
		{Role: "tool", ToolCallID: "a1", Content: text(big)},
		{Role: "tool", ToolCallID: "a2", Content: text(big)},
	}
	fade := newDecayer(map[string]string{"a1": "sh ls", "a2": "sh pwd"}, tools.decaySpill)

	if first := fade.decay(messages, observationBudget); first != 1 {
		t.Fatalf("first pass decayed %d, want 1", first)
	}
	stub := contentOf(messages[0])
	path := filepath.Join(space.Root(), obsDir, "4-decay-a1.txt")
	written, err := os.Stat(path)
	if err != nil {
		t.Fatalf("spill file missing after first pass: %v", err)
	}

	for pass := 0; pass < 3; pass++ {
		if again := fade.decay(messages, observationBudget); again != 0 {
			t.Fatalf("repeat pass decayed %d more results; the same result must not decay twice", again)
		}
	}
	if got := contentOf(messages[0]); got != stub {
		t.Errorf("stub changed across passes:\n first %q\n later %q", stub, got)
	}
	rewritten, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !rewritten.ModTime().Equal(written.ModTime()) || rewritten.Size() != written.Size() {
		t.Error("the spill file was rewritten on a later pass; spilling must happen once")
	}
	entries, err := os.ReadDir(filepath.Join(space.Root(), obsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Errorf("obs dir holds %v, want exactly one spill file", names)
	}
}

// TestSpillLeavesReadableFile checks the other half of the context fix: a large
// result must be recoverable, or bounding it would be data loss rather than
// deferral.
func TestSpillLeavesReadableFile(t *testing.T) {
	space := workspace(t)
	tools := NewToolbox(space, 3, nil)

	result := tools.Execute(t.Context(), "sh", `{"cmd":"printf 'LINE%s\\n' 1 2 3 4 5 6 7 8 9 10 | awk '{for(i=0;i<200;i++) print}'"}`)
	if result.IsError {
		t.Fatalf("command failed: %s", result.Content)
	}
	if len(result.Content) > spillBytes+512 {
		t.Errorf("spilled result is still %d bytes in context", len(result.Content))
	}
	if !strings.Contains(result.Content, obsDir) {
		t.Fatalf("spilled result does not say where the full output went: %q", result.Content)
	}

	read := tools.Execute(t.Context(), "sh", `{"cmd":"wc -c .obs/3-1.txt"}`)
	if read.IsError {
		t.Errorf("the spilled file could not be read back: %s", read.Content)
	}
}
