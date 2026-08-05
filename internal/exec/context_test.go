package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

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
