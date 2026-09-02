package session

// THE WRITE SEAM, PROVED (issue #272, part 2, on the owner's ruling).
//
// The measured run is one sentence: a chat turn made forty-eight tool calls and
// edited a person's live checkout with `sed -i` for seven minutes and forty-six
// seconds before anything noticed. Reads are free here, exactly as the ruling
// says; a turn that has written past the small allowance is moved to a task
// through the road the ceiling already takes.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// THE RULING'S OWN SHAPE: a small edit runs inline, and the write that would
// cross the allowance is the task's to make. A task exists before the turn ends,
// and the person reads the line that says why.
func TestATurnThatWritesPastTheAllowanceIsMovedToATask(t *testing.T) {
	const asked = "rename the parser and fix everything that calls it"
	const brief = "Finish the rename\nwhat is left, and everything this turn already found out"

	completer := &scriptedCompleter{steps: writingSteps(12, checkpointChainSketch, brief)}
	agent, workspace := writeSeamAgent(t, completer)
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	events, err := agent.Submit(context.Background(), asked)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)
	ran.await(t)

	// EXACTLY ONE TASK, on the one road.
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
	// THE LINE, EXACTLY, and it is in the transcript as well as on the screen —
	// the next turn opens on a turn that says what happened to it.
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the seam never said its line; notices were %q", noticeTexts(collected))
	}
	if last := lastMessage(agent); last.Role != "assistant" || !strings.Contains(messageText(last), writeSeamNote) {
		t.Fatalf("the turn did not end on its own line; the transcript ends with a %s saying %q",
			last.Role, messageText(last))
	}
	// AND THE ALLOWANCE WAS A SMALL EDIT AND NOT A SESSION. The script writes one
	// file per round; what is on the disk when the turn ends is the allowance and
	// the round that crossed it, never the twelve the script would have run.
	wrote := 0
	for round := range 12 {
		if _, err := os.Stat(filepath.Join(workspace, fmt.Sprintf("file%d.txt", round))); err == nil {
			wrote++
		}
	}
	if wrote > writeAllowanceFiles+1 {
		t.Fatalf("%d files were written inline, want the allowance (%d) and the one that crossed it",
			wrote, writeAllowanceFiles)
	}
}

// READS STAY FREE, WHICH IS HALF THE RULING. A turn that spends every one of its
// rounds looking is governed by the ceiling and by nothing this file added.
func TestATurnThatOnlyReadsIsNeverMovedByTheWriteSeam(t *testing.T) {
	completer := &scriptedCompleter{steps: readingSteps(8)}
	agent, _ := writeSeamAgent(t, completer)
	stubbedGraph(agent, func(*TaskNode) {})

	events, err := agent.Submit(context.Background(), "read the four files and tell me what they do")
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	collected := collect(t, events)

	if saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("a turn that wrote nothing was moved; notices were %q", noticeTexts(collected))
	}
	if meter := agent.writeMeterNow(); meter != nil && meter.calls != 0 {
		t.Fatalf("the counter charged a reading turn %d calls", meter.calls)
	}
}

// AND ITS LINE IS IN THE REGISTER every line in this house is held to, which is
// the same assertion the ceiling's and the split's lines carry: one line,
// lowercase, no full stop, a middle dot, and not one word of machinery.
func TestTheWriteSeamLineIsTheLineAndCarriesNoMachinery(t *testing.T) {
	inTheHouseRegister(t, writeSeamNote)
}

// THE COUNTER ITSELF: either half of the allowance spends it, and the door opens
// once.
func TestTheAllowanceIsSpentByEitherFilesOrCalls(t *testing.T) {
	byFiles := newWriteMeter()
	for index := range writeAllowanceFiles {
		byFiles.wrote([]string{fmt.Sprintf("/w/file%d.go", index)})
	}
	if !byFiles.pastAllowance() {
		t.Fatalf("%d files did not spend the allowance", writeAllowanceFiles)
	}
	if byFiles.pastAllowance() {
		t.Fatal("the door opened twice in one turn")
	}

	byCalls := newWriteMeter()
	for range writeAllowanceCalls {
		byCalls.wrote([]string{"/w/one.go"})
	}
	if !byCalls.pastAllowance() {
		t.Fatalf("%d calls against one file did not spend the allowance", writeAllowanceCalls)
	}

	under := newWriteMeter()
	under.wrote([]string{"/w/one.go"})
	if under.pastAllowance() {
		t.Fatal("one edit spent the allowance; a small bounded edit is what the ruling protects")
	}
}

// AND WHAT COUNTS AS A WRITE. The hands with a named destination come from the
// one predicate that knows them; the shell ones are the shape the measured run
// was made of, and a read is not one of them.
func TestTheSeamCountsTheWritesAPersonWouldCallWrites(t *testing.T) {
	workspace := t.TempDir()
	elsewhere := t.TempDir()

	for _, probe := range []struct {
		why     string
		call    ai.ToolCall
		wanted  bool
		nothing bool
	}{
		{why: "an edit under the workspace", call: seamCall("edit", map[string]any{"path": "parser.go"}), wanted: true},
		{why: "a write under the workspace", call: seamCall("write", map[string]any{"path": "notes.md"}), wanted: true},
		{why: "a read", call: seamCall("read", map[string]any{"path": "parser.go"}), nothing: true},
		{why: "a listing", call: seamCall("ls", map[string]any{"path": "."}), nothing: true},
		{why: "an edit outside the workspace",
			call: seamCall("edit", map[string]any{"path": filepath.Join(elsewhere, "theirs.go")}), nothing: true},
		{why: "the shape the measured run was made of",
			call: seamBash("sed -i 's/old/new/' internal/session/task_run.go"), wanted: true},
		{why: "a redirection", call: seamBash("echo hi > notes.md"), wanted: true},
		{why: "a move", call: seamBash("mv one.go two.go"), wanted: true},
		{why: "a copy, which writes only its last operand",
			call: seamBash("cp one.go two.go"), wanted: true},
		{why: "a grep", call: seamBash("grep -rn parser ."), nothing: true},
		{why: "a stream edit that is not in place", call: seamBash("sed 's/old/new/' one.go"), nothing: true},
		{why: "a read of somebody else's repository", call: seamBash("git -C " + elsewhere + " log"), nothing: true},
		{why: "a write into somebody else's directory",
			call: seamBash("rm " + filepath.Join(elsewhere, "theirs.go")), nothing: true},
	} {
		wrote := workspaceWrites(workspace, probe.call)
		switch {
		case probe.nothing && len(wrote) != 0:
			t.Errorf("%s was counted as a write: %v", probe.why, wrote)
		case probe.wanted && len(wrote) == 0:
			t.Errorf("%s was not counted as a write", probe.why)
		}
	}

	// A `cd` is carried through the command exactly as the shell reads it, so a
	// write after one is judged where it actually lands.
	if wrote := workspaceWrites(workspace, seamBash("cd "+elsewhere+" && rm theirs.go")); len(wrote) != 0 {
		t.Errorf("a write after a cd out of the workspace was counted: %v", wrote)
	}
}

// ── the fixture ─────────────────────────────────────────────────────────────

// writeSeamAgent is [checkpointAgent] with the tools allowed to run, because
// this file is about what a turn DOES to the disk and a prompt nobody answers
// would leave it doing nothing.
func writeSeamAgent(t *testing.T, completer Completer) (*Agent, string) {
	t.Helper()
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
	})
	return agent, workspace
}

// writingSteps is [grindingSteps] with the one difference this file is about:
// every round writes a file of its own under the workspace.
func writingSteps(count int, sketch, brief string) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(sketch), nil
			}
			if askedForHandoff(messages) {
				return textResponse(brief), nil
			}
			if askedToWriteHandoff(messages) {
				return toolResponse("no-writer", "ls", `{"path":"."}`), nil
			}
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
				Text string `json:"content"`
			}{Path: fmt.Sprintf("file%d.txt", round), Text: fmt.Sprintf("round %d\n", round)})
			return toolResponseWithText(fmt.Sprintf("write-%d", round), "write", string(arguments),
				"Writing the next file."), nil
		}
	}
	return steps
}

// readingSteps is the same script with its hands off the disk: a turn that only
// looks, which the ruling leaves alone.
func readingSteps(count int) []step {
	steps := make([]step, count)
	for index := range steps {
		round := index
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				return textResponse(checkpointDoneSketch), nil
			}
			if askedForRemains(messages) {
				return textResponse(checkpointNothingLeft), nil
			}
			if round >= count-2 {
				return textResponse("They are four files that parse the same format."), nil
			}
			arguments, _ := json.Marshal(struct {
				Path string `json:"path"`
			}{Path: fmt.Sprintf("./%d", round)})
			return toolResponseWithText(fmt.Sprintf("read-%d", round), "ls", string(arguments),
				"Looking at the next path."), nil
		}
	}
	return steps
}

func seamCall(name string, args map[string]any) ai.ToolCall {
	raw, _ := json.Marshal(args)
	return ai.ToolCall{Function: ai.ToolCallFunction{Name: name, Arguments: string(raw)}}
}

func seamBash(command string) ai.ToolCall {
	return seamCall("bash", map[string]any{"command": command})
}
