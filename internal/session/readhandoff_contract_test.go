package session

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/furrow"
)

// readingBatch reaches the real turn loop's hand-off without a changed trigger.
func readingBatch(context.Context, []ai.Message) (*ai.Response, error) {
	return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: []ai.ToolCall{
		readCall("read", `{"path":"one.txt"}`),
		readCall("read", `{"path":"two.txt"}`),
		readCall("read", `{"path":"three.txt"}`),
	}}}}}, nil
}

func readingAgent(t *testing.T, completer Completer, session string) (*Agent, *TaskGraph, string) {
	t.Helper()
	registerBeltRunEngine(t, &beltRunDouble{})
	agent, graph, workspace := quickAgent(t, completer, func(c *Config) {
		c.SessionFile = session
		c.Interactive = true
		c.AskConsent = true
	})
	for _, name := range []string{"one.txt", "two.txt", "three.txt"} {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte("INLINE-"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return agent, graph, workspace
}

func assertNoReadingNews(t *testing.T, agent *Agent, model *quickLanes) {
	t.Helper()
	for _, needle := range []string{"work you handed off landed", "say that word back"} {
		if transcriptCarries(agent, needle) || model.sawIn("", needle) {
			t.Errorf("the chat received a second reading result containing %q", needle)
		}
	}
	if got := agent.OpenQuestions(); len(got) != 0 {
		t.Fatalf("a reading left questions open: %+v", got)
	}
	if got := agent.PendingTasks(); len(got) != 0 {
		t.Fatalf("a reading proposed work: %+v", got)
	}
}

// Contract C1.a, C1.b, C1.c and C2.a: a real hand-off refuses every non-reader,
// keeps going on the same turn, and delivers only the first reader's answer.
// Landing subscribers receive its record once without another note to the chat.
func TestReadHandoffRefusesActionsAndReturnsItsAnswerWithoutAsking(t *testing.T) {
	forbidden := []ai.ToolCall{
		readCall("bash", `{"command":"printf changed > one.txt"}`),
		readCall("write", `{"path":"one.txt","content":"changed"}`),
		readCall("edit", `{"path":"two.txt","oldText":"INLINE","newText":"changed"}`),
		readCall("generate_image", `{}`), readCall("generate_music", `{}`),
		readCall("generate_video", `{}`), readCall("edit_video", `{}`), readCall("speak", `{}`),
		readCall(furrow.ToolRestore, `{}`), readCall(furrow.ToolFork, `{}`), readCall(furrow.ToolMerge, `{}`),
		readCall("ask", `{"question":"promote this reading?"}`),
	}
	model := newQuickLanes([]step{readingBatch, finalText("The distilled answer arrived.")})
	model.lane("READING-CONTRACT",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", ToolCalls: forbidden}}}}, nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			for _, call := range forbidden {
				found := false
				for _, message := range messages {
					if message.Role == "tool" && message.ToolCallID == call.ID {
						found = true
						text := messageText(message)
						for name := range earlyTools {
							if !strings.Contains(text, name) {
								t.Errorf("%s refusal omits %s: %q", call.Function.Name, name, text)
							}
						}
						if !strings.Contains(text, "nothing ran") {
							t.Errorf("%s refusal says %q", call.Function.Name, text)
						}
					}
				}
				if !found {
					t.Errorf("%s never returned to the worker", call.Function.Name)
				}
			}
			return toolResponse("allowed-read", "read", `{"path":"three.txt"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if !strings.Contains(messageText(messages[len(messages)-1]), "INLINE-three.txt") {
				t.Errorf("the allowed read did not run: %+v", messages[len(messages)-1])
			}
			return textResponse("DISTILLED-READING"), nil
		},
	)
	agent, graph, workspace := readingAgent(t, model, filepath.Join(t.TempDir(), "transcript.jsonl"))
	landings := make(chan TaskLanding, 4)
	agent.config.TaskLanded = func(landing TaskLanding) { landings <- landing }
	asks, stop := agent.WatchQuestions()
	defer stop()
	updates, stopUpdates := agent.WatchTaskUpdates()
	defer stopUpdates()
	collect(t, mustSubmit(t, agent, "READING-CONTRACT compare the files"))
	node := quickNodeSaying(t, graph, "READING-CONTRACT")
	waitDoneNode(t, node)
	if node.stateNow() != TaskDone || !node.spec.quick.reading {
		t.Fatalf("reading landed as %+v", node.notice())
	}
	select {
	case landing := <-landings:
		if landing.ID != node.id || landing.State != TaskDone {
			t.Fatalf("reading's landing hook received %+v", landing)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("reading never reached the TaskLanded hook")
	}
	select {
	case landing := <-landings:
		t.Fatalf("reading reached the TaskLanded hook twice: %+v", landing)
	default:
	}
	first, covered := 0, 0
	for _, request := range model.asksOf("") {
		for _, message := range request {
			if message.Role != "tool" {
				continue
			}
			text := messageText(message)
			if strings.Contains(text, "DISTILLED-READING") {
				first++
				if message.ToolCallID != readCall("read", `{"path":"one.txt"}`).ID || !strings.HasPrefix(text, sweepHandoffWord(node.id)) {
					t.Errorf("answer reached the wrong read: %+v", message)
				}
			}
			if strings.Contains(text, sweepCoveredWord(node.id)) {
				covered++
			}
		}
	}
	if first != 1 || covered != 2 {
		t.Fatalf("distilled answers = %d, covered results = %d", first, covered)
	}
	for _, name := range []string{"one.txt", "two.txt"} {
		raw, err := os.ReadFile(filepath.Join(workspace, name))
		if err != nil || string(raw) != "INLINE-"+name {
			t.Fatalf("reading changed %s: %q (%v)", name, raw, err)
		}
	}
	graph.mu.Lock()
	captures, promotion := len(node.spec.quick.captures), node.spec.quick.promotion
	graph.mu.Unlock()
	if captures != 0 || promotion != 0 {
		t.Fatalf("reading captured %d files or proposed %d", captures, promotion)
	}
	select {
	case event := <-asks:
		t.Fatalf("reading published a question: %+v", event)
	default:
	}
	for {
		select {
		case event := <-updates:
			if event.Kind == EventTaskProposal {
				t.Fatalf("reading published a proposal: %+v", event)
			}
		default:
			assertNoReadingNews(t, agent, model)
			return
		}
	}
}

// Contract C1.a, C1.b and C1.c: even a directly named tool that bypasses the
// belt returns a failure before execution, with a stable list of surviving hands.
func TestReadHandoffExecutionGuardAndBeltUseTheReadingTools(t *testing.T) {
	agent, node, _ := appNodeFixture(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	node.spec.quick.reading = true
	agent.config.InTask, agent.config.bashBelt = true, true
	if agent.config.mayBashBelt() {
		t.Fatal("reading still carries the shell worker's envelope")
	}
	var got, want []string
	for _, tool := range agent.belt() {
		got = append(got, tool.Name)
	}
	for name := range earlyTools {
		want = append(want, name)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reading belt = %v, want %v", got, want)
	}
	for _, name := range []string{"bash", "write", "edit", "generate_image", furrow.ToolRestore} {
		tool := bare.Tool{Name: name, Execute: func(context.Context, json.RawMessage) (string, bool, error) {
			t.Error("a forbidden tool ran")
			return "ran", false, nil
		}}
		call := readCall(name, `{}`)
		text, failed, err := agent.executeTaskTool(ctx, tool, call, json.RawMessage(`{}`))
		if err != nil || !failed || text != "this reading can only look — use "+strings.Join(want, ", ")+"; nothing ran" {
			t.Fatalf("%s = %q, failed %v, err %v", name, text, failed, err)
		}
	}
}

// Contract C1.a: a real quick worker's composed prompt promises only the
// reading tools, while an ordinary quick worker still learns how to fan out.
func TestReadHandoffPromptPromisesOnlyItsReadingTools(t *testing.T) {
	for _, reading := range []bool{true, false} {
		name := "ordinary"
		if reading {
			name = "reading"
		}
		t.Run(name, func(t *testing.T) {
			agent, node, workspace := appNodeFixture(t, true)
			node.spec.quick.reading = reading
			worker, err := agent.newTaskAgent(context.Background(), workspace, node, "")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = worker.Close() })
			page := systemTextOf(worker)
			if !reading {
				if !strings.Contains(page, fanoutPage(worker.config)) {
					t.Fatal("ordinary quick worker lost its fan-out page")
				}
				return
			}
			for tool := range namesIn(page) {
				if !earlyTools[tool] {
					t.Errorf("reading prompt names unavailable tool %q", tool)
				}
			}
			for tool := range earlyTools {
				if !strings.Contains(page, "`"+tool+"`") {
					t.Errorf("reading prompt omits its tool %q", tool)
				}
			}
			for _, tool := range []string{"propose_task", "quick_task", "tasks", "bash", "write", "edit", "ask"} {
				if strings.Contains(page, "`"+tool+"`") {
					t.Errorf("reading prompt promises %s", tool)
				}
			}
			for _, fact := range allBeltFacts() {
				if fact.holds(worker.config) {
					t.Errorf("reading holds a belt fact for unavailable tools %v", fact.tools)
				}
			}
		})
	}
}

// Contract C1.a and C1.b: choosing the shell worker's belt cannot force an
// engine-owned reading to use shell or reject its permitted read calls.
func TestReadHandoffStillReadsWhenTheShellWorkerBeltIsSelected(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	model := newQuickLanes([]step{readingBatch, finalText("answered")})
	model.lane("BELT-CONTRACT",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("read-file", "read", `{"path":"three.txt"}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			if !strings.Contains(messageText(messages[len(messages)-1]), "INLINE-three.txt") {
				t.Errorf("reading was forced through the shell envelope: %+v", messages[len(messages)-1])
			}
			return textResponse("BELT-DISTILLED"), nil
		},
	)
	agent, graph, _ := readingAgent(t, model, "")
	collect(t, mustSubmit(t, agent, "BELT-CONTRACT compare files"))
	node := quickNodeSaying(t, graph, "BELT-CONTRACT")
	waitDoneNode(t, node)
	if node.stateNow() != TaskDone || !model.sawIn("", "BELT-DISTILLED") {
		t.Fatalf("reading failed under the shell worker choice: %+v", node.notice())
	}
	assertNoReadingNews(t, agent, model)
}

// Contract C1.d: an ordinary quick worker still proposes, waits for the answer,
// and carries on with the declined tool result when the person says no.
func TestOrdinaryQuickStillWaitsForPromotionAndCarriesOnAfterDecline(t *testing.T) {
	continued, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	model := newQuickLanes([]step{quickCall("quick", "ordinary reading", "ORDINARY-CONTRACT read a file", nil, nil), finalText("started")})
	model.lane("ORDINARY-CONTRACT",
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("shell", "bash", `{"command":"cat one.txt"}`), nil
		},
		func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			result := messages[len(messages)-1]
			if result.Role != "tool" || result.ToolCallID != "shell" || messageText(result) != "This needs a full task before running a command or creating media · promotion declined; nothing ran" {
				t.Errorf("decline returned %+v", result)
			}
			close(continued)
			select {
			case <-release:
			case <-ctx.Done():
			}
			return textResponse("carried on"), nil
		},
	)
	agent, graph, _ := quickAgent(t, model)
	updates, stop := agent.WatchTaskUpdates()
	defer stop()
	collect(t, mustSubmit(t, agent, "read it"))
	var proposal TaskNotice
	for proposal.ID == 0 {
		select {
		case event := <-updates:
			if event.Kind == EventTaskProposal && event.Task != nil {
				proposal = *event.Task
			}
		case <-time.After(10 * time.Second):
			t.Fatal("ordinary quick work never asked to promote")
		}
	}
	select {
	case <-continued:
		t.Fatal("worker continued before the promotion was answered")
	default:
	}
	rows, _ := agent.AppQuickTasks()
	if len(rows) != 1 || rows[0].State != "grew" || !proposal.Deadline.IsZero() {
		t.Fatalf("waiting quick = %+v, proposal = %+v", rows, proposal)
	}
	agent.ResolveTask(proposal.ID, TaskAnswer{Approved: false})
	select {
	case <-continued:
	case <-time.After(10 * time.Second):
		t.Fatal("decline did not release the worker")
	}
	if quickNodeSaying(t, graph, "ORDINARY-CONTRACT").spec.quick.reading {
		t.Fatal("ordinary quick work was marked as a hand-off reading")
	}
}

// Contract C2.a and C2.b: all fallback roads keep the reading's own ending,
// while the real chat receives inline results and no landing instruction.
func TestReadHandoffFallbackHasNoLandingNote(t *testing.T) {
	for _, ending := range []string{"failure", "empty", "timeout", "held"} {
		t.Run(ending, func(t *testing.T) {
			model := newQuickLanes([]step{readingBatch, finalText("answered inline")})
			entered := make(chan struct{})
			worker := func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
				switch ending {
				case "failure":
					return nil, errors.New("provider returned error: 401 reading failed")
				case "timeout":
					close(entered)
					<-ctx.Done()
					return nil, ctx.Err()
				default:
					return textResponse(""), nil
				}
			}
			steps := make([]step, 32)
			for i := range steps {
				steps[i] = worker
			}
			model.lane("FALLBACK-CONTRACT", steps...)
			agent, graph, _ := readingAgent(t, model, filepath.Join(t.TempDir(), "transcript.jsonl"))
			onATestClock(t)
			clock := newFakeClock()
			armed := make(chan time.Duration, 8)
			agent.taskTimer = func(after time.Duration) (<-chan time.Time, func()) {
				ch, stop := clock.timer(after)
				armed <- after
				return ch, stop
			}
			if ending == "held" {
				graph.governor = heldGovernor()
			} else if ending == "empty" {
				// The model loop repairs blank replies. Supply the empty settled
				// result at the runner seam, retaining the real reporting hook.
				stubbedGraph(agent, func(node *TaskNode) {
					node.keepWorkerConclusion("", io.Discard)
					node.finish("it finished and said nothing", nil, "", "")
					node.graph.complete(node, TaskDone)
				})
			}
			events := mustSubmit(t, agent, "FALLBACK-CONTRACT compare files")
			if ending == "timeout" || ending == "held" {
				for count := 0; count < 2; count++ {
					select {
					case <-armed:
					case <-time.After(10 * time.Second):
						t.Fatal("reading did not arm its wait clocks")
					}
				}
				if ending == "timeout" {
					select {
					case <-entered:
					case <-time.After(10 * time.Second):
						t.Fatal("worker did not start")
					}
					clock.advance(readSweepWait)
				} else {
					clock.advance(readSweepStartWait)
				}
			}
			collect(t, events)
			node := quickNodeSaying(t, graph, "FALLBACK-CONTRACT")
			waitDoneNode(t, node)
			if !model.sawIn("", "INLINE-three.txt") {
				t.Fatal("fallback did not return the inline reading")
			}
			assertNoReadingNews(t, agent, model)
			if ending != "empty" && node.stateNow() != TaskFailed {
				t.Fatalf("abandoned reading concealed its ending: %+v", node.notice())
			}
			rows := ReadTaskIndex(agent.config.taskIndexFile())
			if len(rows) != 1 || rows[0].Kind != TaskKindQuick {
				t.Fatalf("reading lost its index record: %+v", rows)
			}
			if rows[0].Status != string(node.stateNow()) {
				t.Fatalf("index concealed the reading's ending: %+v", rows)
			}
			if ending != "empty" {
				quicks, err := agent.AppQuickTasks()
				if err != nil || len(quicks) != 1 || quicks[0].State != "stopped" || len(quicks[0].Files) != 0 {
					t.Fatalf("abandoned reading is finished work: %+v (%v)", quicks, err)
				}
			}
		})
	}
}

// Contract C2.a and C2.b: letting a reading go for a move returns the move
// result and keeps its stopped record without delivering a landing note.
func TestReadHandoffMoveHasNoLandingNote(t *testing.T) {
	entered := make(chan struct{})
	model := newQuickLanes(nil)
	model.lane("MOVE-CONTRACT", func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	agent, graph, _ := readingAgent(t, model, filepath.Join(t.TempDir(), "transcript.jsonl"))
	clock := newFakeClock()
	armed := make(chan time.Duration, 4)
	agent.taskTimer = func(after time.Duration) (<-chan time.Time, func()) {
		ch, stop := clock.timer(after)
		armed <- after
		return ch, stop
	}
	response, _ := readingBatch(context.Background(), nil)
	calls := response.Choices[0].Message.ToolCalls
	done := make(chan []toolResult, 1)
	go func() {
		done <- agent.handoffReadSweep(context.Background(), agent.newEpisode(), nil, userText("MOVE-CONTRACT compare files"), calls, &readSweep{}, nil)
	}()
	for count := 0; count < 3; count++ {
		select {
		case <-armed:
		case <-time.After(10 * time.Second):
			t.Fatal("reading did not arm its clocks")
		}
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("reading worker did not start")
	}
	agent.moveAsked.Store(true)
	clock.advance(readSweepMoveLook)
	select {
	case results := <-done:
		if len(results) != len(calls) {
			t.Fatalf("move returned %d results", len(results))
		}
		for _, result := range results {
			if result.text != sweepMovedWord {
				t.Fatalf("move returned %q", result.text)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("move did not let the reading go")
	}
	node := quickNodeSaying(t, graph, "MOVE-CONTRACT")
	waitDoneNode(t, node)
	if node.stateNow() != TaskFailed {
		t.Fatalf("moved reading concealed its ending: %+v", node.notice())
	}
	assertNoReadingNews(t, agent, model)
}

// Contract C1.e, C2.a and C4.d: a resumed turn sees the ordinary landing owed
// beside finished and interrupted readings, with no reading landing or replay.
func TestReadHandoffMarkAndQuietEndingSurviveAReopen(t *testing.T) {
	model := newQuickLanes([]step{readingBatch, finalText("answered")})
	model.lane("REOPEN-CONTRACT", finalText("distilled answer"))
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, graph, workspace := readingAgent(t, model, path)
	collect(t, mustSubmit(t, agent, "REOPEN-CONTRACT compare files"))
	node := quickNodeSaying(t, graph, "REOPEN-CONTRACT")
	waitDoneNode(t, node)
	before, _ := agent.AppQuickTasks()
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	checkpoint := taskCheckpointPath(path)
	document := readCheckpoint(t, checkpoint)
	finished := recordOf(t, document, node.id)
	if finished.Quick == nil || !finished.Quick.Reading || finished.State != TaskDone {
		t.Fatalf("finished reading was not checkpointed: %+v", finished)
	}
	// The saved node is real. Add the other two records a crash can leave:
	// a reading still running, and ordinary work whose landing nobody read.
	running := taskRecord{
		ID: document.Seq + 1, Kind: TaskKindQuick, State: TaskRunning,
		Title: "Read the remaining files", Brief: "INTERRUPTED-READING compare files",
		Ground: workspace, Mode: TaskModeInPlace, Model: finished.Model,
		Quick: &quickRecord{Reading: true, Line: "INTERRUPTED-READING compare files"},
	}
	ordinary := taskRecord{
		ID: running.ID + 1, Kind: TaskKindQuick, State: TaskDone,
		Title: "Collect the ordinary findings", Brief: "ORDINARY-OWED collect findings",
		Ground: workspace, Mode: TaskModeInPlace, Model: finished.Model,
		Quick: &quickRecord{Line: "ORDINARY-OWED collect findings"}, Report: "ORDINARY-LANDING-OWED",
	}
	document.Nodes = append(document.Nodes, running, ordinary)
	document.Seq = ordinary.ID
	writeCheckpoint(t, checkpoint, document)

	reopenedModel := newQuickLanes([]step{finalText("carried on after reopening")})
	for _, mark := range []string{"REOPEN-CONTRACT", "INTERRUPTED-READING"} {
		reopenedModel.lane(mark, func(context.Context, []ai.Message) (*ai.Response, error) {
			t.Error("a reading executed again after reopening")
			return textResponse("replayed reading"), nil
		})
	}
	fresh, _ := newTestAgent(t, reopenedModel, func(c *Config) { c.SessionFile, c.Workspace = path, workspace })
	after, _ := fresh.AppQuickTasks()
	if len(before) != 1 || len(after) != 3 || !reflect.DeepEqual(before[0], after[0]) || after[1].State != "stopped" || after[2].State != "done" {
		t.Fatalf("quick state on reopen: %+v -> %+v", before, after)
	}
	restored := fresh.graph().node(node.id)
	if restored == nil || !restored.spec.quick.reading {
		t.Fatal("checkpoint lost the reading mark")
	}
	interrupted := fresh.graph().node(running.ID)
	if interrupted == nil || !interrupted.spec.quick.reading || interrupted.stateNow() != TaskFailed {
		t.Fatal("checkpoint lost the interrupted reading's mark or ending")
	}
	collect(t, mustSubmit(t, fresh, "carry on"))
	requests := reopenedModel.asksOf("")
	if len(requests) != 1 {
		t.Fatalf("reopen submitted %d conversation requests, want one", len(requests))
	}
	recoveries := 0
	for _, message := range requests[0] {
		text := messageText(message)
		if _, recovery, found := strings.Cut(text, "recovered task graph: "); found {
			recoveries++
			if count := strings.SplitN(recovery, "\n", 2)[0]; count != "1 done" {
				t.Errorf("model received reading recovery counts: %q", count)
			}
			if !strings.Contains(recovery, "ORDINARY-LANDING-OWED") || strings.Count(recovery, "work you handed off landed") != 1 {
				t.Errorf("model did not receive exactly the ordinary landing: %s", recovery)
			}
			for _, reading := range []taskRecord{finished, running} {
				if strings.Contains(recovery, reading.Title) || strings.Contains(recovery, reading.Quick.Line) || strings.Contains(recovery, reading.Report) && reading.Report != "" {
					t.Errorf("model received a reading landing: %s", recovery)
				}
			}
		}
	}
	if recoveries != 1 {
		t.Fatalf("model received %d recovery notes, want the ordinary work's note", recoveries)
	}
	for _, mark := range []string{"REOPEN-CONTRACT", "INTERRUPTED-READING"} {
		if requests := reopenedModel.asksOf(mark); len(requests) != 0 {
			t.Errorf("reading %s executed again: %d requests", mark, len(requests))
		}
	}
	fresh.config.taskControl = restored
	if fresh.readingToolRefusal("write") == "" {
		t.Fatal("reopened reading may now write")
	}
}
