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
	"sync/atomic"
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
	// AND THE ALLOWANCE WAS A SMALL EDIT AND NOT A SESSION. The script makes one
	// write call per round; what is on the disk when the turn ends is the
	// allowance and the round that crossed it, never the twelve the script would
	// have run.
	wrote := 0
	for round := range 12 {
		if _, err := os.Stat(filepath.Join(workspace, fmt.Sprintf("file%d.txt", round))); err == nil {
			wrote++
		}
	}
	if wrote > writeAllowanceCalls+1 {
		t.Fatalf("%d writes landed inline, want the allowance (%d) and the one that crossed it",
			wrote, writeAllowanceCalls)
	}
}

// ── the two shapes the one count tells apart ────────────────────────────────

// A SCRIPT, THE FILE IT NEEDS, AND THEN RUNNING IT, FINISHES HERE.
//
// This is the ordinary small piece of work the allowance exists to leave alone,
// and it is the shape that used to be moved for its BREADTH alone: three writes
// land on three different paths, and the round after them runs what was written.
// Nothing about it is a grind — the turn reaches for the disk three times, under
// the allowance — so no line is said, no task is started, and the person gets the
// answer in the conversation they asked it in.
//
// IT IS DRIVEN THROUGH THE REAL LOOP and the assertions are taken off THE DISK,
// including the file the SHELL wrote rather than the model: a fixture that only
// counted calls could pass while the work never happened.
func TestAFewWritesAndRunningTheirResultFinishInline(t *testing.T) {
	const asked = "write the service list and a script that totals it, then run the script"

	completer := &scriptedCompleter{steps: scriptAndRunSteps(12)}
	agent, workspace := writeSeamAgent(t, completer)
	graph := stubbedGraph(agent, func(*TaskNode) {})

	collected := collect(t, mustSubmit(t, agent, asked))

	// NOTHING MOVED, and the person was not told anything had.
	if count := admitted(graph); count != 0 {
		t.Fatalf("%d tasks were admitted for a few writes and a run of them", count)
	}
	if saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the seam moved a small finished piece of work; notices were %q", noticeTexts(collected))
	}
	if text := transcriptText(agent); strings.Contains(text, writeSeamNote) {
		t.Fatalf("the seam's line reached the transcript of a turn that was never moved:\n%s", text)
	}
	// AND THE WORK IS ALL THERE: the three files the turn wrote, and the fourth
	// one the script it wrote produced when it was run.
	for _, name := range []string{"services.csv", "ports.csv", "totals.sh", "totals.txt"} {
		if _, err := os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Errorf("the inline turn did not deliver %s: %v", name, err)
		}
	}
	// AND THE COUNT IS WHAT SAYS WHY. Three reaches for the disk, under the
	// allowance; running the script names nothing it changes and costs nothing.
	if calls := writeMeterCalls(agent.writeMeterNow()); calls != 3 {
		t.Fatalf("the meter charged %d calls, want the three writes and nothing for the run", calls)
	}
}

// AND THE SAME NUMBER OF FILES WRITTEN OVER AND OVER IS STILL MOVED, at the
// unchanged call budget — which is the half of the ruling this file has always
// been about, and the one the measured run was made of.
func TestRepeatedWritesToOneFileStillHandOverAtTheCallBudget(t *testing.T) {
	const asked = "fix the totals in report.md"
	const brief = "Finish the report\nwhat is left, and everything this turn already found out"

	completer := &scriptedCompleter{steps: oneFileWritingSteps(12, "report.md",
		checkpointChainSketch, brief)}
	// The session's evidence belongs outside the deliverable directory, as it
	// does for a conversation with its own folder. Count only the worker's work.
	agent, workspace := writeSeamAgent(t, completer, func(c *Config) {
		c.droppings = Place{Dir: t.TempDir()}
	})
	ran := make(ranNodes, 2)
	graph := stubbedGraph(agent, func(node *TaskNode) { ran <- node })

	collected := collect(t, mustSubmit(t, agent, asked))
	ran.await(t)

	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
	if !saidSomething(noticeTexts(collected), writeSeamNote) {
		t.Fatalf("the seam never said its line; notices were %q", noticeTexts(collected))
	}
	// AND ONE PATH IS ALL THAT EVER CHANGED, so nothing about breadth can be what
	// moved this turn.
	entries, err := os.ReadDir(workspace)
	if err != nil {
		t.Fatalf("reading the workspace: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "report.md" {
		t.Fatalf("the workspace holds %v, want only the one file the turn wrote over", entries)
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

// THE COUNTER ITSELF: the allowance is spent by the CALLS and by nothing else,
// and the door opens once.
func TestTheAllowanceIsSpentByTheCallsAlone(t *testing.T) {
	byCalls := newWriteMeter()
	for range writeAllowanceCalls {
		byCalls.wrote([]string{"/w/one.go"})
	}
	if !byCalls.pastAllowance() {
		t.Fatalf("%d calls against one file did not spend the allowance", writeAllowanceCalls)
	}
	if byCalls.pastAllowance() {
		t.Fatal("the door opened twice in one turn")
	}

	// AND BREADTH ALONE SPENDS NOTHING. A turn that wrote a script and then the
	// file the script produced has touched two paths and done one thing; every
	// call under the allowance stays in the conversation however far apart the
	// paths are.
	across := newWriteMeter()
	for index := range writeAllowanceCalls - 1 {
		across.wrote([]string{fmt.Sprintf("/w/apart/file%d.go", index)})
	}
	if across.pastAllowance() {
		t.Fatalf("%d writes to %d different files spent the allowance; breadth is not the count",
			writeAllowanceCalls-1, writeAllowanceCalls-1)
	}
	// AND ONE CALL THAT CHANGED SEVERAL PATHS IS STILL ONE REACH FOR THE DISK.
	wide := newWriteMeter()
	wide.wrote([]string{"/w/a.go", "/w/b.go", "/w/c.go", "/w/d.go", "/w/e.go", "/w/f.go"})
	if wide.pastAllowance() {
		t.Fatal("a single call was charged once per path it named")
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

// writeMeterCalls reads a meter under its own lock so assertions made while
// background hand goroutines are settling cannot race the account they inspect.
func writeMeterCalls(meter *writeMeter) int {
	if meter == nil {
		return 0
	}
	meter.mu.Lock()
	defer meter.mu.Unlock()
	return meter.calls
}

// writeSeamAgent is [checkpointAgent] with the tools allowed to run, because
// this file is about what a turn DOES to the disk and a prompt nobody answers
// would leave it doing nothing.
//
// AND IT TAKES THE FIXTURE'S OWN MUTATORS AFTER ITS OWN, for the one test that
// needs a session file to read a decision back out of (writeseam_delivery_test.go).
func writeSeamAgent(t *testing.T, completer Completer, also ...func(*Config)) (*Agent, string) {
	t.Helper()
	// AND IT ANSWERS THE NAMER OFF THE QUEUE for the same reason checkpointAgent
	// does: the seam moves work to a task, the road asks for its name on a
	// goroutine of its own, and a namer taking one of this script's twelve rounds
	// is a test failing on the scheduler ([answerTheNamerOffTheQueue], #392).
	answerTheNamerOffTheQueue(completer)
	agent, workspace := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = true
		config.Divide = true
		config.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow}
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierMastermind): checkpointMarkModel,
		})
		for _, mutate := range also {
			mutate(config)
		}
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

// scriptAndRunSteps is the small finished piece of work: two data files, the
// script that reads them, the round that RUNS it, and then the answer in words.
//
// The run is a real `sh`, and it is what puts `totals.txt` on the disk — so the
// case above can assert that the work happened and not merely that a counter
// stayed low. A command that only NAMES a script is not a write
// ([bashWritesInside]), which is why the fourth round costs nothing.
func scriptAndRunSteps(count int) []step {
	const script = "#!/bin/sh\nwc -l < services.csv > totals.txt\n"
	written := map[string]string{
		"services.csv": "kestrel\nlinnet\n",
		"ports.csv":    "8431\n8432\n",
		"totals.sh":    script,
	}
	order := []string{"services.csv", "ports.csv", "totals.sh"}
	var done atomic.Int64
	steps := make([]step, count)
	for index := range steps {
		steps[index] = func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if askedForSketch(messages) {
				if done.Load() < int64(len(order)+1) {
					return textResponse(checkpointChainSketch), nil
				}
				return textResponse(checkpointDoneSketch), nil
			}
			if askedForHandoff(messages) || askedForRemains(messages) {
				if done.Load() < int64(len(order)+1) {
					return textResponse("The script still needs to run and produce totals.txt."), nil
				}
				return textResponse(checkpointNothingLeft), nil
			}
			if askedToWriteHandoff(messages) {
				return textResponse("Finish the totals."), nil
			}
			round := int(done.Add(1))
			switch {
			case round <= len(order):
				name := order[round-1]
				arguments, _ := json.Marshal(struct {
					Path string `json:"path"`
					Text string `json:"content"`
				}{Path: name, Text: written[name]})
				return toolResponseWithText(fmt.Sprintf("write-%s", name), "write", string(arguments),
					"Writing "+name+"."), nil
			case round == len(order)+1:
				arguments, _ := json.Marshal(struct {
					Command string `json:"command"`
				}{Command: "sh totals.sh"})
				return toolResponseWithText("run-totals", "bash", string(arguments),
					"Running the script."), nil
			}
			return textResponse("The list is written and the script has run; totals.txt has the count."), nil
		}
	}
	return steps
}

// oneFileWritingSteps is [writingSteps] aimed at ONE path, which is the turn that
// edits the same file over and over — the shape the call budget is for.
func oneFileWritingSteps(count int, path, sketch, brief string) []step {
	var round atomic.Int64
	steps := make([]step, count)
	for index := range steps {
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
			}{Path: path, Text: fmt.Sprintf("attempt %d\n", round.Add(1))})
			return toolResponseWithText(fmt.Sprintf("write-%d", round.Load()), "write", string(arguments),
				"Writing the file again."), nil
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
