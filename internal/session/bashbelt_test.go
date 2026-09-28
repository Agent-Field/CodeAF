package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// THE WAVE'S PROOF. Each test names the acceptance line it stands for
// (docs/design/bash-task-loop/DESIGN.md, the W1 row): the belt composition
// (kept set present, the six file tools absent, the flag-off belt identical
// byte for byte to today's), the envelope (a two-call batch rejected without
// poisoning history, four consecutive invalids landing the node failed on the
// circling road), the truncation (head+tail+path, the cap following the
// window, the file beside the node log, the working copy clean), and a
// scripted task that reads, writes and edits a file through bash alone.

// bashToolOf finds the belt's one bash hand.
func bashToolOf(t *testing.T, belt []bare.Tool) bare.Tool {
	t.Helper()
	for _, tool := range belt {
		if tool.Name == "bash" {
			return tool
		}
	}
	t.Fatal("the belt carries no bash tool")
	return bare.Tool{}
}

// toolBytes is the fingerprint one tool shows the wire: its name, description
// and schema. Two tools that agree here are the same tool to the model.
func toolBytes(tool bare.Tool) string {
	return tool.Name + "\x00" + tool.Description + "\x00" + string(tool.Schema)
}

// beltBytes is the same fingerprint for a whole belt, in order.
func beltBytes(belt []bare.Tool) string {
	lines := make([]string, 0, len(belt))
	for _, tool := range belt {
		lines = append(lines, toolBytes(tool))
	}
	return strings.Join(lines, "\n")
}

// bashBeltWorkerConfig is a depth-one task node's config, the shape
// newTaskAgentOn builds, with the branch belt asked for directly.
func bashBeltWorkerConfig(t *testing.T) func(*Config) {
	t.Helper()
	return func(config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 1
		config.taskDepth = 1
		config.bashBelt = true
	}
}

func TestBashBeltCompositionKeepsTheKeptHandsAndDropsTheSix(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	belt := agent.beltTools()

	carried := map[string]bool{}
	for _, tool := range belt {
		carried[tool.Name] = true
	}
	// THE KEPT SET, on the belt and on its existing gates. `ask` is not among
	// them: this belt reaches the person through the plan CLI, not a gate.
	for _, kept := range []string{"bash", "read_document", "jobs", "manual"} {
		if !carried[kept] {
			t.Errorf("the branch belt is missing the kept hand %s", kept)
		}
	}
	if carried["ask"] {
		t.Error("the branch belt still carries ask, which this belt does not use")
	}
	// AND THE SIX FILE TOOLS ARE OFF. Each of them is a shell command wearing
	// a schema, and the branch worker spells what it did in bash.
	for _, gone := range []string{"read", "edit", "write", "grep", "find", "ls"} {
		if carried[gone] {
			t.Errorf("the branch belt still carries %s", gone)
		}
	}
	// AND THE KEPT FAMILIES THAT GATE THEMSELVES ride on, to the extent this
	// shape has the stores behind them: a fan-out worker carries the graph's
	// verbs, which is the same gate today's belt reads.
	if agent.config.mayProposeTask() && !carried["propose_task"] {
		t.Error("a fan-out bash worker lost propose_task")
	}

	// THE ONE HAND IS BARE'S, WITH THE SESSION'S BACKGROUND ARGUMENT AND THE
	// BRANCH CUT. The schema is bare's bash schema with the background boolean
	// added, and the description names the cut the branch applies.
	hand := bashToolOf(t, belt)
	var schema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(hand.Schema, &schema); err != nil {
		t.Fatalf("bash schema does not parse: %v", err)
	}
	if _, background := schema.Properties["background"]; !background {
		t.Error("the branch bash hand lost the background argument")
	}
	if !strings.Contains(hand.Description, "cut to its first half and its last half") {
		t.Errorf("the branch bash description does not name the branch cut: %s", hand.Description)
	}

	// AND THE FLAG-OFF BELT IS TODAY'S, BYTE FOR BYTE. A worker built without
	// the branch belt carries the six file tools, and its file-tool block is
	// exactly the one today's composition builds: bare's tools at this agent's
	// result caps, wrapped with the session's three wrappers and nothing else.
	// The first seven entries are that block; everything after is the appended
	// families this comparison is not about.
	offBelt := plainBeltAgent(t).beltTools()
	offBash := bashToolOf(t, offBelt)
	var offSchema struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(offBash.Schema, &offSchema); err != nil {
		t.Fatalf("flag-off bash schema does not parse: %v", err)
	}
	if _, background := offSchema.Properties["background"]; !background {
		t.Error("the flag-off bash hand lost the background argument")
	}
	for _, pi := range []string{"read", "edit", "write", "grep", "find", "ls"} {
		if !beltNameSet(offBelt)[pi] {
			t.Errorf("the flag-off belt is missing %s", pi)
		}
	}
}

// plainBeltAgent builds a task-node agent the way the flag-off world builds
// one, for the byte-identity assertions above.
func plainBeltAgent(t *testing.T) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 2
		config.taskDepth = 1
	})
	return agent
}

// TestBashBeltFlagOffChangesNothing is the both-arms law: a belt that does not
// read the switch is byte-identical whether the switch is set or unset, and a
// conversation never reads it at all. The flag-off task belt must equal a belt
// built exactly the way the pre-branch composition built it, and the
// conversation belt must not move one byte with the field set.
func TestBashBeltFlagOffChangesNothing(t *testing.T) {
	offAgent := plainBeltAgent(t)
	offBeltTools := offAgent.beltTools()

	// THE FLAG SET ON A SHAPE THE PREDICATE REFUSES (a conversation) leaves the
	// belt alone: mayBashBelt answers InTask first.
	withFlag, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.bashBelt = true
	})
	if beltBytes(withFlag.beltTools()) != beltBytes(plainConversationBelt(t)) {
		t.Error("the conversation belt moved with the branch field set")
	}

	// AND THE FLAG-OFF TASK BELT IS THE PRE-BRANCH COMPOSITION: recomputed here
	// the way tools.go composed it before this branch existed, byte for byte.
	recomputed := bare.AllToolsCapped(offAgent.config.Workspace, offAgent.resultCaps())
	for index, tool := range recomputed {
		switch tool.Name {
		case "bash":
			recomputed[index] = offAgent.backgroundBash(tool)
		case "read":
			recomputed[index] = offAgent.pdfRead(tool)
		case "write":
			recomputed[index] = offAgent.appendableWrite(tool)
		}
	}
	if beltBytes(recomputed) != beltBytes(offBeltTools[:len(recomputed)]) {
		t.Error("the flag-off file-tool block is not the composition that was there before this branch")
	}
}

func plainConversationBelt(t *testing.T) []bare.Tool {
	t.Helper()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	return agent.beltTools()
}

// beltWorkerAgent is one task worker built the way the runner builds it —
// [Agent.newTaskAgent] on a node the graph has admitted, with the belt asked
// through CODEAF_TASK_BELT as the runner asks it — and it answers that worker.
// The parent's dial and the spec's rung are handed in, because they are the two
// rungs the spawn places around the seat the worker thinks from.
func beltWorkerAgent(t *testing.T, dial, specRung effort.Rung) *Agent {
	t.Helper()
	session, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	// The graph's runner is stubbed off before anything is admitted, so the
	// node stays where the spawn left it and this test reads the spawn alone.
	stubbedGraph(session, func(*TaskNode) {})
	if dial.Valid() && !session.SetConversationEffort(dial.String()) {
		t.Fatalf("%q is not a rung the parent can be dialled to", dial)
	}
	id := session.graph().reserve()
	spec := taskSpec{title: "the job", brief: "b", acceptance: "a", depth: 1}
	if specRung.Valid() {
		spec.effort = specRung
	}
	session.graph().admit(id, spec)
	worker, err := session.newTaskAgent(context.Background(), t.TempDir(), session.graph().node(id), "")
	if err != nil {
		t.Fatalf("newTaskAgent: %v", err)
	}
	t.Cleanup(func() { _ = worker.Close() })
	return worker
}

// TestBashBeltWorkerThinksFromTheWorkSeat pins the effort half of the belt, at
// the spawn: a bash-belt worker's agent answers low when nothing more specific
// spoke, and a shipped-belt worker's agent answers what it always did — the
// parent's own resolved answer, which is absence on a session configured no
// further. The parent's dial rides in as the worker's default either way, so it
// reaches the shipped worker and does not lift the belt's seat; a rung set on
// the spec outranks the seat on both belts, which is what keeps
// [Agent.SetTaskEffort] worth having here.
func TestBashBeltWorkerThinksFromTheWorkSeat(t *testing.T) {
	t.Run("on the bash belt", func(t *testing.T) {
		t.Setenv("CODEAF_TASK_BELT", "bash")
		if got := beltWorkerAgent(t, effort.None, effort.None).ResolvedEffort(); got != "low" {
			t.Fatalf("the bash-belt worker resolves to %q, want low", got)
		}
		if got := beltWorkerAgent(t, effort.High, effort.None).ResolvedEffort(); got != "low" {
			t.Fatalf("a bash-belt worker under a parent dialled to high resolves to %q, want low", got)
		}
		if got := beltWorkerAgent(t, effort.None, effort.High).ResolvedEffort(); got != "high" {
			t.Fatalf("a bash-belt worker with a rung on its spec resolves to %q, want high", got)
		}
	})
	t.Run("on the shipped belt", func(t *testing.T) {
		t.Setenv("CODEAF_TASK_BELT", "node")
		if got := beltWorkerAgent(t, effort.None, effort.None).ResolvedEffort(); got != "" {
			t.Fatalf("the shipped-belt worker resolves to %q, want absence", got)
		}
		if got := beltWorkerAgent(t, effort.High, effort.None).ResolvedEffort(); got != "high" {
			t.Fatalf("a shipped-belt worker under a parent dialled to high resolves to %q, want high", got)
		}
		if got := beltWorkerAgent(t, effort.None, effort.High).ResolvedEffort(); got != "high" {
			t.Fatalf("a shipped-belt worker with a rung on its spec resolves to %q, want high", got)
		}
	})
}

// TestBashBeltTruncationWritesHeadTailAndPath pins Decision 3's shape: over
// the cap a bash result is its first half, a marker naming the file the whole
// output lives in, and its last half; the file is beside this agent's own
// journal, and the working copy it stood in is clean.
func TestBashBeltTruncationWritesHeadTailAndPath(t *testing.T) {
	sessionDir := t.TempDir()
	journalDir := filepath.Join(sessionDir, "journal")
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = workspace
		config.SessionFile = filepath.Join(journalDir, "s.jsonl")
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})
	hand := bashToolOf(t, agent.beltTools())

	line := strings.Repeat("x", 31) + "\n"
	payload := strings.Repeat(line, 1875) // exactly 60000 bytes
	command := "yes '" + strings.Repeat("x", 31) + "' | head -c 60000"
	arguments, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	text, isError, err := hand.Execute(context.Background(), arguments)
	if err != nil || isError {
		t.Fatalf("bash failed: %v (error=%v, text %.80s)", err, isError, text)
	}

	capBytes := agent.resultCaps().MaxBytes
	if len(text) > capBytes+512 {
		t.Fatalf("the result is %d bytes, want it cut near the %d-byte cap", len(text), capBytes)
	}
	if !strings.HasPrefix(text, payload[:capBytes/2]) {
		t.Error("the cut result does not open with the head of the output")
	}
	if !strings.HasSuffix(text, payload[len(payload)-(capBytes-capBytes/2):]) {
		t.Error("the cut result does not end with the tail of the output")
	}
	if !strings.Contains(text, "[output truncated; full output: ") {
		t.Error("the cut result carries no marker naming the full output")
	}

	// THE FILE, WHERE THE JOURNAL LIVES AND NOWHERE ELSE.
	spills := actionFiles(t, journalDir)
	if len(spills) != 1 {
		t.Fatalf("found %d spill files beside the journal, want exactly one", len(spills))
	}
	full, err := os.ReadFile(spills[0])
	if err != nil {
		t.Fatalf("read the spill: %v", err)
	}
	if len(full) != 60000 {
		t.Errorf("the full output is %d bytes, want the whole 60000", len(full))
	}
	if path := markerPath(text); path == "" || path != spills[0] {
		t.Errorf("the marker names %q, want the spill %q", path, spills[0])
	}
	if entries := actionFiles(t, workspace); len(entries) != 0 {
		t.Errorf("the working copy was dirtied by its own telemetry: %v", entries)
	}
}

// TestBashBeltTruncationCapFollowsTheWindow pins the half of Decision 3 a flat
// 20k would have blown: the cap is the same window-following pair the rest of
// the belt is cut with, so a small-window worker gets proportionally less.
func TestBashBeltTruncationCapFollowsTheWindow(t *testing.T) {
	sessionDir := t.TempDir()
	journalDir := filepath.Join(sessionDir, "journal")
	workspace := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = workspace
		config.SessionFile = filepath.Join(journalDir, "s.jsonl")
		config.ContextWindow = 16000
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})
	hand := bashToolOf(t, agent.beltTools())
	hag0 := "yes x | head -c 30000"
	arguments, err := json.Marshal(map[string]string{"command": hag0})
	if err != nil {
		t.Fatal(err)
	}
	text, isError, err := hand.Execute(context.Background(), arguments)
	if err != nil || isError {
		t.Fatalf("bash failed: %v (error=%v)", err, isError)
	}
	capBytes := agent.resultCaps().MaxBytes
	if capBytes >= 30000 {
		t.Fatalf("the test needs a cap under the output size, got %d", capBytes)
	}
	if got := len(text); got > capBytes+512 {
		t.Errorf("the result is %d bytes, want it bound by this window's %d-byte cap", got, capBytes)
	}
	spills := actionFiles(t, journalDir)
	if len(spills) != 1 {
		t.Fatalf("found %d spill files, want one beside the journal", len(spills))
	}
	if full := lenOf(t, spills[0]); full != 30000 {
		t.Errorf("the spill holds %d bytes, want the whole 30000", full)
	}
}

// TestBashBeltEnvelopeRejectsTwoCallBatchWithoutPoisoning pins the reject's
// two promises: the batch runs nothing, and what the model reads next is a
// transcript the corrected call can live in — the diagnostics recorded as tool
// results, the next request built, the valid call actually run.
func TestBashBeltEnvelopeRejectsTwoCallBatchWithoutPoisoning(t *testing.T) {
	workspace := t.TempDir()
	completer := &routedCompleter{parent: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{
					{ID: "one", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo first > one.txt"}`}},
					{ID: "two", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo second > two.txt"}`}},
				},
			}}}}, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("three", "bash", `{"command":"echo written > ok.txt"}`), nil
		},
		finalText("done"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = workspace
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})
	collect(t, mustSubmit(t, agent, "go"))

	// NOTHING FROM THE BATCH RAN; THE VALID CALL DID.
	if _, err := os.Stat(filepath.Join(workspace, "one.txt")); err == nil {
		t.Error("one.txt exists: a rejected batch ran")
	}
	if _, err := os.Stat(filepath.Join(workspace, "two.txt")); err == nil {
		t.Error("two.txt exists: a rejected batch ran")
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "ok.txt")); err != nil || strings.TrimSpace(string(data)) != "written" {
		t.Errorf("the corrected call did not run: %v", err)
	}

	// AND THE TRANSCRIPT IS THE SHAPE THE NEXT REQUEST NEEDS: the assistant
	// message kept, both calls answered, then the valid call and its result.
	agent.mu.Lock()
	defer agent.mu.Unlock()
	roles := make([]string, 0, len(agent.messages))
	for _, message := range agent.messages {
		roles = append(roles, message.Role)
	}
	want := []string{"system", "user", "assistant", "tool", "tool", "assistant", "tool", "assistant"}
	if len(roles) != len(want) {
		t.Fatalf("transcript roles = %v, want %v", roles, want)
	}
	for index := range want {
		if roles[index] != want[index] {
			t.Fatalf("transcript roles = %v, want %v", roles, want)
		}
	}
	if !strings.HasPrefix(messageContentText(agent.messages[3]), bashEnvelopeMark) {
		t.Errorf("the first rejection did not reach the model as a tool result: %.80s", messageContentText(agent.messages[3]))
	}
	if !strings.HasPrefix(messageContentText(agent.messages[4]), bashEnvelopeMark) {
		t.Errorf("the second call's rejection did not reach the model: %.80s", messageContentText(agent.messages[4]))
	}
}

// TestBashBeltEnvelopeFourInvalidsEndTheTurn walks four consecutive invalid
// submissions through the real turn loop: nothing runs anywhere, and the turn
// lands on the sentence the runner reads a stopped worker's last words for.
func TestBashBeltEnvelopeFourInvalidsEndTheTurn(t *testing.T) {
	workspace := t.TempDir()
	completer := &routedCompleter{parent: []step{
		// one: two calls, addressable, both refused
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{
					{ID: "a", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo a > a.txt"}`}},
					{ID: "b", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo b > b.txt"}`}},
				},
			}}}}, nil
		},
		// two: a hand the branch belt does not carry
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("c", "read", `{"path":"a.txt"}`), nil
		},
		// three: the one tool, malformed arguments
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("d", "bash", `{"command":""}`), nil
		},
		// four: another hand that is not here
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("e", "grep", `{"pattern":"x"}`), nil
		},
		finalText("never asked"),
	}}
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})
	events := collect(t, mustSubmit(t, agent, "go"))

	sawTurnDone := false
	for _, event := range events {
		if event.Kind == EventTurnDone {
			sawTurnDone = true
		}
	}
	if !sawTurnDone {
		t.Fatal("the turn did not end after four invalid actions")
	}
	// AND THE BUDGET WAS SPENT, NOT THE SCRIPT: four requests, no fifth.
	completer.mu.Lock()
	spent := completer.seen.parent
	completer.mu.Unlock()
	if spent != 4 {
		t.Fatalf("the model was asked %d times, want exactly four", spent)
	}
	// THE WORKER'S LAST WORDS are the sentence the landing reads, so the node
	// this worker serves settles on the circling road.
	last := lastMessage(agent)
	if last.Role != "assistant" || !strings.Contains(messageContentText(last), loopLeftUndoneNote) {
		t.Errorf("the turn did not end on the circling road: role %q, text %.120s", last.Role, messageContentText(last))
	}
	// AND NOTHING RAN, ANYWHERE, IN ANY REJECTED SHAPE.
	for _, stray := range []string{"a.txt", "b.txt"} {
		if _, err := os.Stat(filepath.Join(workspace, stray)); err == nil {
			t.Errorf("%s exists: a rejected action ran", stray)
		}
	}
}

// TestBashBeltEnvelopeDropsAnUnaddressableCall pins the other branch of the
// reject: a call with no id cannot be given a tool result, so the malformed
// assistant message is taken back out of what the model reads next and the
// diagnostic goes back as a user message instead.
func TestBashBeltEnvelopeDropsAnUnaddressableCall(t *testing.T) {
	workspace := t.TempDir()
	completer := &routedCompleter{parent: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{
				Role: "assistant",
				ToolCalls: []ai.ToolCall{ // no id: no tool result can be paired with it
					{Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo x > x.txt"}`}},
				},
			}}}}, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("ok", "bash", `{"command":"echo written > ok.txt"}`), nil
		},
		finalText("done"),
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = workspace
		config.bashBelt = true
		config.InTask = true
		config.taskID = 1
	})
	collect(t, mustSubmit(t, agent, "go"))

	// THE COMMAND NEVER RAN, AND THE REPLAY HISTORY IS CLEAN: the malformed
	// assistant message is gone, the diagnostic reached the model as a user
	// message, and the corrected call after it ran.
	if _, err := os.Stat(filepath.Join(workspace, "x.txt")); err == nil {
		t.Error("x.txt exists: a dropped call ran")
	}
	if data, err := os.ReadFile(filepath.Join(workspace, "ok.txt")); err != nil || strings.TrimSpace(string(data)) != "written" {
		t.Errorf("the corrected call did not run: %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	sawToolResult, sawDiagnostic := false, false
	for _, message := range agent.messages {
		if message.Role == "tool" && message.ToolCallID == "" {
			sawToolResult = true
		}
		if message.Role == "user" && strings.HasPrefix(messageContentText(message), bashEnvelopeMark) {
			sawDiagnostic = true
		}
	}
	if sawToolResult {
		t.Error("a tool result was recorded for a call no request carried")
	}
	if !sawDiagnostic {
		t.Error("the diagnostic did not reach the model as a user message")
	}
}

// TestBashBeltTaskReadsWritesAndEditsThroughBashAlone is the scripted task:
// one file made, read, appended and read again by a worker whose every action
// is a bash command, landing done through the ordinary gate.
// bashScriptSteps is the worker's script for the bash-only task: every step
// is one bash call, and every step is IDEMPOTENT AND CONVERGING - whatever the
// concurrent errand calls steal, and however many times a step repeats, the
// file the script leaves holds the same two lines, and the last step reads it.
func bashScriptSteps() []step {
	converge := "mkdir -p notes && printf 'first line\n' > notes/scratch.md && { grep -q appended notes/scratch.md || printf 'appended line\n' >> notes/scratch.md; } && cat notes/scratch.md"
	arguments, _ := json.Marshal(map[string]string{"command": converge})
	var steps []step
	for index := 0; index < 5; index++ {
		callID := fmt.Sprintf("b%d", index+1)
		args := string(arguments)
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(callID, "bash", args), nil
		})
	}
	steps = append(steps,
		finalText("notes/scratch.md holds the first line and the appended line.\nfiles: notes/scratch.md"),
		finalText("notes/scratch.md holds both lines.\nfiles: notes/scratch.md"),
	)
	return steps
}

// bashInvalidSteps is four consecutive invalid submissions, plus spares: a
// stolen step is a skipped step, never a run one, so the child's first four
// requests are all invalid whichever errand answered beside them.
func bashInvalidSteps() []step {
	invalid := []struct {
		id, name, arguments string
	}{
		{"c1", "read", `{"path":"x"}`},
		{"c2", "bash", `{"command":""}`},
		{"c3", "grep", `{"pattern":"x"}`},
		{"c4", "bash", `{"command":"echo hi"}`},
		{"c5", "read", `{"path":"y"}`},
		{"c6", "write", `{"path":"z","content":"w"}`},
		{"c7", "grep", `{"pattern":"y"}`},
		{"c8", "edit", `{"path":"w","edits":[]}`},
	}
	var steps []step
	for _, call := range invalid {
		call := call
		steps = append(steps, func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(call.id, call.name, call.arguments), nil
		})
	}
	steps = append(steps, finalText("never asked"), finalText("never asked"))
	return steps
}

func TestBashBeltTaskReadsWritesAndEditsThroughBashAlone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)

	brief, _ := json.Marshal(taskArguments{
		Title: "Scratch file", Summary: "s",
		Brief:       "make notes/scratch.md, read it, append a line, read it again\n" + taskBriefMark,
		Deliverable: "notes/scratch.md with both lines", Acceptance: "The file carries the first line and the appended line.",
		MaxSteps: 200, NoProgress: 6,
	})
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(brief)), nil
			},
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		child: bashScriptSteps(),
		audit: []step{finalText("VERIFIED — the file carries both lines")},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "make the scratch file"))
	node := agent.graph().node(1)
	waitDoneNode(t, node)

	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("the bash-only task landed %q (report: %s)", notice.State, notice.Report)
	}
	data, err := os.ReadFile(filepath.Join(repo, "notes", "scratch.md"))
	if err != nil {
		t.Fatalf("read the deliverable: %v", err)
	}
	if string(data) != "first line\nappended line\n" {
		t.Errorf("the file holds %q, want both lines", string(data))
	}
	// AND THE WORKER READ THE BRANCH'S OWN PAGE, not the file-tool guidance: it
	// opens on the loop policy.
	page := messageContentText(completer.childAsked()[0])
	if !strings.Contains(page, "Your FIRST action is the FRAME/PLAN") {
		t.Error("the bash worker did not read the branch doctrine page")
	}
	if strings.Contains(page, "## Specialized Tools") {
		t.Error("the bash worker was still handed the file-tool guidance")
	}
}

// TestBashBeltFourInvalidsLandTheNodeFailedOnTheCirclingRoad runs the same
// road as the envelope test above, at the task boundary: four invalid child
// submissions end the node failed, and the row reads the circling road the
// runner already had.
func TestBashBeltFourInvalidsLandTheNodeFailedOnTheCirclingRoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)

	brief, _ := json.Marshal(taskArguments{
		Title: "Never starts", Summary: "s", Brief: "do the thing\n" + taskBriefMark,
		Deliverable: "d", Acceptance: "a", MaxSteps: 200, NoProgress: 6,
	})
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(brief)), nil
			},
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		child: bashInvalidSteps(),
		audit: []step{finalText("REFUTED — the deliverable does not exist: the worker never ran a valid action")},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "do the thing"))
	node := agent.graph().node(1)
	waitDoneNode(t, node)

	notice := node.notice()
	if notice.State != TaskFailed {
		t.Fatalf("four invalid actions landed the node %q (report: %s)", notice.State, notice.Report)
	}
	if node.endingNow() != TaskEndingCircling {
		t.Errorf("the ending is %q, want the circling road", node.endingNow())
	}
	// NO AUDITOR ON THIS BELT: the runner's own road keeps the circling ending
	// and lands the node failed, which is what the audit used to answer.
	if calls := completer.auditCalls(); calls != 0 {
		t.Errorf("the auditor was asked %d times, want none", calls)
	}
}

// TestBashBeltWorkerPromptOpensOnTheLoopPolicy pins the page a bash-belt worker
// reads both ways: the planning policy opens it — unhedged, with the first-action
// law and the loop's own words — and a worker without the branch belt reads the
// composed page it always read. The old page's hedges and its two contradictions
// (a batch of calls, a planner on the belt) are gone.
func TestBashBeltWorkerPromptOpensOnTheLoopPolicy(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	bashConfig := Config{Workspace: t.TempDir(), Model: "test/model"}
	bashBeltWorkerConfig(t)(&bashConfig)
	bashPage := renderSystemAt(bashConfig, now)
	// THE POLICY LEADS, down to its first sentence.
	if !strings.HasPrefix(bashPage, "You own one task within a shared objective.") {
		t.Errorf("the bash worker's page does not open on the policy: %.160q", bashPage)
	}
	// AND THE FIRST-ACTION LAW IS RESTORED, with no hedge in front of it.
	if !strings.Contains(bashPage, "Your FIRST action is the FRAME/PLAN") {
		t.Error("the bash worker's page lost the first-action law")
	}
	// AND THE DIRECT LAW LEADS THE LOOP: a task with one owned output and no
	// unknown is done directly, and the plan is decided once in the first turn.
	if !strings.Contains(bashPage, "DONE DIRECTLY") {
		t.Error("the bash worker's page lost the done-directly law")
	}
	// AND THE REASONING ECONOMY IS TAUGHT: the observation is the record, and
	// what comes back is one decision rather than a replay.
	if !strings.Contains(bashPage, "Think once") {
		t.Error("the bash worker's page lost the think-once section")
	}
	// AND THE EDIT IDIOM NAMES THE DOOR THE BELT ACTUALLY CARRIES.
	if !strings.Contains(bashPage, "codeaf patch") {
		t.Error("the bash worker's page lost the codeaf patch idiom")
	}
	// AND THE PAGE'S OLD WEIGHT IS GONE: no there-is-no-planner, no batch law
	// the envelope refuses, no clean-restore check (this belt has no auditor),
	// and none of the hedges the page used to gate its own rules with.
	for _, gone := range []string{
		"THERE IS NO PLANNER",
		"ONE batch of calls",
		"clean restore",
		"on a wide brief",
		"when the work has nameable independence",
	} {
		if strings.Contains(bashPage, gone) {
			t.Errorf("the bash worker's page still carries %q", gone)
		}
	}
	// AND `ask` IS NOT TAUGHT: the verb is off this belt.
	if strings.Contains(bashPage, "Use `ask`") {
		t.Error("the bash worker's page still teaches ask")
	}
	if strings.Contains(bashPage, "## Specialized Tools") {
		t.Error("the bash worker's page still carries the file-tool section")
	}
	if strings.Contains(bashPage, "`read` takes a row's URIs exactly as printed") {
		t.Error("the page still names `read` in the citation fact")
	}

	// AND THE FLOOR OF THE TREE reads the same page: the policy leads and no
	// file-tool name survives at any depth.
	floorConfig := Config{Workspace: t.TempDir(), Model: "test/model"}
	func() {
		config := &floorConfig
		config.InTask = true
		config.tasker = graphForShape(t)
		config.taskID = 2
		config.taskDepth = taskDepthLimit
		config.bashBelt = true
	}()
	floorPage := renderSystemAt(floorConfig, now)
	if !strings.HasPrefix(floorPage, "You own one task within a shared objective.") {
		t.Error("a floor bash node's page does not open on the policy")
	}
	if strings.Contains(floorPage, "`read` takes one exactly as printed") {
		t.Error("a floor bash node's page still names `read` in the citation fact")
	}
	if strings.Contains(floorPage, "never `edit` or `write` a config file") {
		t.Error("a floor bash node's page still names `edit` and `write`")
	}

	// AND A WORKER WITHOUT THE BELT reads the composed page it always read.
	plainConfig := Config{Workspace: t.TempDir(), Model: "test/model"}
	plainPage := renderSystemAt(plainConfig, now)
	if !strings.Contains(plainPage, "## Specialized Tools") {
		t.Error("a worker without the branch belt lost the file-tool section")
	}
	if !strings.Contains(plainPage, "THERE IS NO PLANNER ON YOUR BELT") {
		t.Error("a worker without the branch belt lost the planner sentence")
	}
	if strings.Contains(plainPage, "You own one task within a shared objective.") {
		t.Error("a worker without the branch belt was handed the loop policy")
	}
	// AND THE BYTES THIS WAVE ADDED NEVER REACH THE FLAG-OFF PAGE: the law,
	// the reasoning economy and the patch idiom are the bash belt's own.
	for _, bashOnly := range []string{"DONE DIRECTLY", "Think once", "codeaf patch"} {
		if strings.Contains(plainPage, bashOnly) {
			t.Errorf("a worker without the branch belt was handed %q", bashOnly)
		}
	}
}

// TestBashWorkerPromptNamesOnlyWhatTheBeltCarries is the forward law of
// prompt_belt_test, run for the shape this wave adds: every tool the bash
// worker's page names is on the branch belt.
func TestBashWorkerPromptNamesOnlyWhatTheBeltCarries(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, bashBeltWorkerConfig(t))
	page := renderSystemAt(agent.config, time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC))
	carried := beltNameSet(agent.beltTools())
	// AND THE ABSENT-CASE FRAGMENTS ARE NOT PROMISES (prompt_belt_test's
	// reading): they say what is not here and are stripped before the names
	// are read.
	residue := page
	for _, fact := range allBeltFacts() {
		if !fact.holds(agent.config) && fact.absent != "" {
			residue = strings.Replace(residue, fact.absent, "", 1)
		}
	}
	universe := universeToolNames(t)
	for name := range namesIn(residue) {
		if !universe[name] || carried[name] {
			continue
		}
		if _, ledgered := promptNamesBeyondTheBelt[name]; ledgered {
			continue
		}
		t.Errorf("the bash worker's page names `%s` and the branch belt does not carry it", name)
	}
}

// actionFiles lists the spill files in one directory, sorted, and empty for a
// directory that is not there.
func actionFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "action-") && strings.HasSuffix(entry.Name(), ".txt") {
			found = append(found, filepath.Join(dir, entry.Name()))
		}
	}
	return found
}

// markerPath reads the path out of a truncation marker.
func markerPath(text string) string {
	start := strings.Index(text, "[output truncated; full output: ")
	if start < 0 {
		return ""
	}
	rest := text[start+len("[output truncated; full output: "):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// lenOf is one file's size in bytes.
func lenOf(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return len(data)
}

// universeToolNames is every name that is a tool on some belt this package
// builds — the bash shape's belt added to the shipped shapes' universe, so a
// name the branch belt retires is still read as a tool name and not as prose.
func universeToolNames(t *testing.T) map[string]bool {
	t.Helper()
	universe := map[string]bool{}
	for _, shape := range beltShapes {
		for name := range beltNameSet(beltShapeAgent(t, shape).belt) {
			universe[name] = true
		}
	}
	for name := range beltNameSet(beltShapeAgent(t, beltShape{name: "a bash worker", build: func(t *testing.T, config *Config) { bashBeltWorkerConfig(t)(config) }}).belt) {
		universe[name] = true
	}
	for _, fact := range allBeltFacts() {
		for _, name := range fact.tools {
			universe[name] = true
		}
	}
	for name := range promptNamesBeyondTheBelt {
		universe[name] = true
	}
	return universe
}

// TestBashBeltNodeKeepsNoAuditor is the experiment's second decision proved at
// the task boundary: a task on the bash belt IS the planner's loop and nothing
// else — one worker, one bash, one landing — so no auditor is ever asked, and
// the node's own account is what comes home, marked for what it is.
//
// The auditor could not have helped here anyway: it verifies a STAGED diff, and
// a shell worker's writes are never staged, so on every grid row it read "no
// diff, no staged change" against a tree that already carried the fix and
// refuted correct work (docs/design/bash-task-loop/INVESTIGATION.md).
func TestBashBeltNodeKeepsNoAuditor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	repo := newTestRepo(t)

	brief, _ := json.Marshal(taskArguments{
		Title: "Scratch file", Summary: "s",
		Brief:       "make notes/scratch.md, read it, append a line, read it again\n" + taskBriefMark,
		Deliverable: "notes/scratch.md with both lines", Acceptance: "The file carries the first line and the appended line.",
		MaxSteps: 200, NoProgress: 6,
	})
	completer := &routedCompleter{
		parent: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call-task", "propose_task", string(brief)), nil
			},
			finalText("handed off"), finalText("handed off"), finalText("handed off"),
		},
		child: bashScriptSteps(),
		audit: []step{finalText("VERIFIED — the file carries both lines")},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	collect(t, mustSubmit(t, agent, "make the scratch file"))
	node := agent.graph().node(1)
	waitDoneNode(t, node)

	notice := node.notice()
	if notice.State != TaskDone {
		t.Fatalf("the bash-only task landed %q (report: %s)", notice.State, notice.Report)
	}
	// THE DISCRIMINATOR: newTestAgent's own TaskAudit is on, so a belt that
	// still had an auditor would have asked it here. The landing above is the
	// unaudited one — the node's own account came home as `done`.
	if page := messageContentText(completer.childAsked()[0]); !strings.Contains(page, "Your FIRST action is the FRAME/PLAN") {
		t.Error("the belt did not engage: the worker read the file-tool page")
	}
	asked := completer.auditAsked()
	for i, m := range asked {
		t.Logf("AUDITASK %d: %.200s", i, messageContentText(m))
	}
	if len(asked) != 0 {
		t.Errorf("the bash belt asked an auditor %d times, want none", len(asked))
	}
}
