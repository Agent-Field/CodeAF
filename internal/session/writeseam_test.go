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
	"reflect"
	"strings"
	"testing"
	"time"

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

// THE CALLS THREE HANDS LAND ARE THE CALLER'S OWN WRITES. A hand writes in the
// caller's directory by construction, so crossing the same unchanged allowance
// opens the same one road and says the same one line.
func TestTheWritesThreeHandsLandAreTheCallersAndMoveItToOneTask(t *testing.T) {
	const asked = "change the three independent settings and finish the integration"
	paths := []string{"one.txt", "two.txt", "three.txt"}
	completer := newForkCompleter(len(paths))
	var callerMeter *writeMeter
	handsMayWrite := make(chan struct{})
	releaseHands := func() {
		select {
		case <-handsMayWrite:
		default:
			close(handsMayWrite)
		}
	}
	t.Cleanup(releaseHands)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("fork-three", "fork", forkCall(
				forkPartJSON("the first setting", paths[0]),
				forkPartJSON("the second setting", paths[1]),
				forkPartJSON("the third setting", paths[2]),
			)), nil
		},
	}
	completer.callerTail = keepWorkingThroughHandReports(completer, "stitched", releaseHands, func(agent *Agent) {
		callerMeter = agent.writeMeterNow()
	})
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn == 1 {
			<-handsMayWrite
			return toolResponse(fmt.Sprintf("hand-%d-write", index), "write",
				fmt.Sprintf(`{"path":%q,"content":%q}`, paths[index-1], fmt.Sprintf("hand %d\n", index))), nil
		}
		return textResponse(fmt.Sprintf("hand %d finished its setting", index)), nil
	}

	agent, workspace := writeSeamAgent(t, completer)
	completer.drive(agent)
	ran := make(ranNodes, 2)
	releaseTask := make(chan struct{})
	t.Cleanup(func() { close(releaseTask) })
	graph := stubbedGraph(agent, func(node *TaskNode) {
		ran <- node
		<-releaseTask
	})

	events := collect(t, mustSubmit(t, agent, asked))
	ran.await(t)

	calls, files := writeMeterSnapshot(callerMeter)
	if calls != len(paths) || len(files) != len(paths) {
		t.Fatalf("the caller's meter has %d calls over %d files, want %d of each: %v",
			calls, len(files), len(paths), files)
	}
	for _, path := range paths {
		absolute := filepath.Join(workspace, path)
		if !files[absolute] {
			t.Errorf("the caller's meter is missing %s: %v", absolute, files)
		}
	}
	if !saidSomething(noticeTexts(events), writeSeamNote) {
		t.Fatalf("the seam never said its line; notices were %q", noticeTexts(events))
	}
	lineInTranscript := false
	for _, message := range agent.snapshot() {
		if message.Role == "assistant" && strings.Contains(messageText(message), writeSeamNote) {
			lineInTranscript = true
			break
		}
	}
	if !lineInTranscript {
		t.Fatal("the seam's line is absent from the transcript")
	}
	if count := admitted(graph); count != 1 {
		t.Fatalf("%d tasks were admitted, want exactly one", count)
	}
}

// A REFUSED WRITE CHANGED NOTHING AND COSTS NOTHING, which is [writeSeam.PostFeedback]'s
// own rule read from the other side: the hand's next write, the one inside its
// own slice, reaches the caller's meter as the single call it actually was.
func TestAHandsRefusedWriteCountsNothingAndTheOneThatLandedCounts(t *testing.T) {
	completer := newForkCompleter(2)
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("fork-two", "fork", forkCall(
				forkPartJSON("the adapters", "adapters"),
				forkPartJSON("the docs", "docs"),
			)), nil
		},
	}
	completer.callerTail = keepWorkingThroughHandReports(completer, "done", nil, nil)
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if index != 1 {
			return textResponse("nothing to change"), nil
		}
		switch turn {
		case 1:
			return toolResponse("refused", "write", `{"path":"docs/stolen.md","content":"x"}`), nil
		case 2:
			return toolResponse("landed", "write", `{"path":"adapters/mine.md","content":"x"}`), nil
		default:
			return textResponse("the allowed change is done"), nil
		}
	}

	agent, workspace := newTestAgent(t, completer, nil)
	completer.drive(agent)
	collect(t, mustSubmit(t, agent, "split the adapters and docs changes"))

	calls, files := writeMeterSnapshot(agent.writeMeterNow())
	landed := filepath.Join(workspace, "adapters", "mine.md")
	refused := filepath.Join(workspace, "docs", "stolen.md")
	if calls != 1 || len(files) != 1 || !files[landed] {
		t.Fatalf("the meter has %d calls and files %v, want only %s", calls, files, landed)
	}
	if files[refused] {
		t.Fatalf("the refused path reached the caller's meter: %v", files)
	}
	if _, err := os.Stat(refused); !os.IsNotExist(err) {
		t.Fatalf("the refused path changed on disk: %v", err)
	}
}

// A HAND'S FINISHED CALL IS READ BY EXACTLY THE READING AN INLINE CALL GETS
// ([workspaceWrites]), so the same writes count under the workspace and the same
// non-writes count nothing. A fork held to a looser rule would be the way around
// the seam all over again.
func TestAFinishedHandCallIsReadExactlyAsAnInlineOneIs(t *testing.T) {
	workspace := t.TempDir()
	elsewhere := t.TempDir()

	for _, probe := range []struct {
		why  string
		kind EventKind
		call ai.ToolCall
	}{
		{why: "an edit under the workspace", kind: EventToolEnd,
			call: seamCall("edit", map[string]any{"path": "parser.go"})},
		{why: "a read", kind: EventToolEnd,
			call: seamCall("read", map[string]any{"path": "parser.go"})},
		{why: "an edit outside the workspace", kind: EventToolEnd,
			call: seamCall("edit", map[string]any{"path": filepath.Join(elsewhere, "theirs.go")})},
		{why: "an in-place shell edit under the workspace", kind: EventToolEnd,
			call: seamBash("sed -i 's/old/new/' parser.go")},
		{why: "a generated image", kind: EventToolEnd,
			call: seamCall("generate_image", map[string]any{"path": "image.png"})},
		{why: "a failed edit", kind: EventToolFailed,
			call: seamCall("edit", map[string]any{"path": "failed.go"})},
	} {
		event := Event{Kind: probe.kind, Tool: probe.call.Function.Name, Args: probe.call.Function.Arguments}
		want := workspaceWrites(workspace, probe.call)
		if probe.kind != EventToolEnd {
			want = nil
		}
		if wrote := landedHandWrites(workspace, event); !reflect.DeepEqual(wrote, want) {
			t.Errorf("%s counted %v from a hand, want the inline count %v", probe.why, wrote, want)
		}
	}
}

// A HAND OUTLIVES THE TURN THAT FORKED IT, so what it brings home between turns
// opens the next meter, what it brings home inside one lands at that turn's next
// boundary, and neither of them is counted a second time.
func TestAHandsWritesCarryToTheNextBoundaryAndAreCountedOnce(t *testing.T) {
	agent := &Agent{config: Config{Workspace: t.TempDir()}}
	seam := &writeSeam{agent: agent}
	first := filepath.Join(agent.config.Workspace, "first.go")
	second := filepath.Join(agent.config.Workspace, "second.go")

	agent.stashHandWrites([][]string{{first}, {second}})
	seam.EpisodeInit(nil)
	if calls, files := writeMeterSnapshot(agent.writeMeterNow()); calls != 2 || len(files) != 2 {
		t.Fatalf("episode-init carried %d calls over %d files, want two of each: %v", calls, len(files), files)
	}
	seam.PostFeedback(context.Background(), nil, nil, nil, nil, false)
	if calls, files := writeMeterSnapshot(agent.writeMeterNow()); calls != 2 || len(files) != 2 {
		t.Fatalf("a second drain counted the opening stash again: %d calls over %v", calls, files)
	}

	seam.EpisodeInit(nil)
	agent.stashHandWrites([][]string{{first}, {second}})
	seam.PostFeedback(context.Background(), nil, nil, nil, nil, false)
	if calls, files := writeMeterSnapshot(agent.writeMeterNow()); calls != 2 || len(files) != 2 {
		t.Fatalf("post-feedback carried %d calls over %d files, want two of each: %v", calls, len(files), files)
	}
	seam.PostFeedback(context.Background(), nil, nil, nil, nil, false)
	if calls, files := writeMeterSnapshot(agent.writeMeterNow()); calls != 2 || len(files) != 2 {
		t.Fatalf("a second boundary counted the live stash again: %d calls over %v", calls, files)
	}
}

// AND READS STAY FREE IN A HAND TOO, which is the same half of the ruling that
// leaves an inline reading turn alone: two hands that only look leave the meter
// at nothing, say no line, and start no task.
func TestAForkWhoseHandsOnlyReadIsNeverMovedByTheWriteSeam(t *testing.T) {
	paths := []string{"one.txt", "two.txt"}
	completer := newForkCompleter(len(paths))
	completer.caller = []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("fork-readers", "fork", forkCall(
				forkPartJSON("read the first", paths[0]),
				forkPartJSON("read the second", paths[1]),
			)), nil
		},
	}
	completer.callerTail = keepWorkingThroughHandReports(completer, "both readings folded in", nil, nil)
	completer.hand = func(index, turn int, _ []ai.Message) (*ai.Response, error) {
		if turn == 1 {
			return toolResponse(fmt.Sprintf("hand-%d-read", index), "read",
				fmt.Sprintf(`{"path":%q}`, paths[index-1])), nil
		}
		return textResponse(fmt.Sprintf("hand %d read its file", index)), nil
	}

	agent, workspace := writeSeamAgent(t, completer)
	for _, path := range paths {
		if err := os.WriteFile(filepath.Join(workspace, path), []byte(path+"\n"), 0o600); err != nil {
			t.Fatalf("write read fixture: %v", err)
		}
	}
	completer.drive(agent)
	graph := stubbedGraph(agent, func(*TaskNode) {})
	events := collect(t, mustSubmit(t, agent, "read these two files in parallel and report"))

	if calls, files := writeMeterSnapshot(agent.writeMeterNow()); calls != 0 || len(files) != 0 {
		t.Fatalf("the read-only fork charged %d calls over %v", calls, files)
	}
	if saidSomething(noticeTexts(events), writeSeamNote) {
		t.Fatalf("the read-only fork said the seam's line; notices were %q", noticeTexts(events))
	}
	for _, message := range agent.snapshot() {
		if strings.Contains(messageText(message), writeSeamNote) {
			t.Fatalf("the read-only fork put the seam's line in the transcript: %q", messageText(message))
		}
	}
	if count := admitted(graph); count != 0 {
		t.Fatalf("the read-only fork admitted %d tasks", count)
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

// keepWorkingThroughHandReports gives the caller one real step boundary after
// every hand is home. The production ordering is what the tests exercise; this
// wait only removes scheduler timing from whether all stashed calls meet that
// boundary together.
func keepWorkingThroughHandReports(completer *forkCompleter, closing string, release func(), arrived func(*Agent)) step {
	keepGoing := keepWorking(completer, closing)
	first := true
	return func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
		response, err := keepGoing(ctx, messages)
		if err != nil || !first {
			return response, err
		}
		first = false
		if release != nil {
			release()
		}
		agent := completer.driven()
		for agent != nil && agent.jobs.handsOutstanding() {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Millisecond):
			}
		}
		if arrived != nil {
			arrived(agent)
		}
		return toolResponse("read-after-hands", "ls", `{"path":"."}`), nil
	}
}

// writeMeterSnapshot reads a meter under its own lock so assertions made while
// background hand goroutines are settling cannot race the account they inspect.
func writeMeterSnapshot(meter *writeMeter) (int, map[string]bool) {
	if meter == nil {
		return 0, nil
	}
	meter.mu.Lock()
	defer meter.mu.Unlock()
	files := make(map[string]bool, len(meter.files))
	for path := range meter.files {
		files[path] = true
	}
	return meter.calls, files
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
