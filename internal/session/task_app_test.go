package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

func appNodeFixture(t *testing.T, quick bool) (*Agent, *TaskNode, string) {
	t.Helper()
	agent, dir := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.AskConsent = true; c.TaskAutoApproveSeconds = 12 })
	graph := agent.graph()
	node := &TaskNode{graph: graph, id: 1, state: TaskRunning, done: make(chan struct{}), Ground: dir, Mode: TaskModeInPlace, spec: taskSpec{title: "Edit a file", named: true, brief: "edit the file", acceptance: taskPersonAcceptance, model: agent.Model()}, room: newTaskRoom()}
	if quick {
		node.spec.quick = newQuickTaskSpec("edit the file", nil, nil)
		node.kind = TaskKindQuick
	}
	graph.mu.Lock()
	graph.nodes[1] = node
	graph.order = append(graph.order, 1)
	graph.seq = 1
	graph.mu.Unlock()
	agent.config.taskControl = node
	return agent, node, dir
}
func appWriteCall(path, content string) ai.ToolCall {
	raw, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return ai.ToolCall{ID: "write-test", Function: ai.ToolCallFunction{Name: "write", Arguments: string(raw)}}
}
func appBareTool(t *testing.T, dir, name string) bare.Tool {
	t.Helper()
	for _, tool := range bare.AllTools(dir) {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("missing tool %s", name)
	return bare.Tool{}
}

func TestAppQuickWritesKeepOriginalBytesAndUndoEveryFileAfterRestart(t *testing.T) {
	agent, node, dir := appNodeFixture(t, true)
	original := filepath.Join(dir, "original.txt")
	created := filepath.Join(dir, "created.txt")
	if err := os.WriteFile(original, []byte("keep\nold\n"), 0640); err != nil {
		t.Fatal(err)
	}
	for _, call := range []ai.ToolCall{appWriteCall(original, "keep\nfirst\n"), appWriteCall(original, "keep\nlast\n"), appWriteCall(created, "")} {
		result := agent.executeTool(context.Background(), agent.newEpisode(), nil, call, "")
		if result.isError {
			t.Fatalf("real dispatch: %s", result.text)
		}
	}
	node.graph.mu.Lock()
	node.state = TaskDone
	record := node.recordLocked()
	node.graph.mu.Unlock()
	if len(record.Quick.Captures) != 2 || string(record.Quick.Captures[0].Before) != "keep\nold\n" {
		t.Fatalf("captures: %+v", record.Quick)
	}
	node.graph.mu.Lock()
	restored := restoreNode(node.graph, record)
	node.graph.nodes[1] = restored
	node.graph.mu.Unlock()
	rows, err := agent.AppQuickTasks()
	if err != nil || len(rows) != 1 || rows[0].Added != 1 || rows[0].Removed != 1 || rows[0].Files[0].BeforeHash == rows[0].Files[0].AfterHash {
		t.Fatalf("read: %+v, %v", rows, err)
	}
	undone, changed, err := agent.UndoQuickTask(1)
	if err != nil || len(undone) != 2 || len(changed) != 0 {
		t.Fatalf("undo %v %v %v", undone, changed, err)
	}
	raw, _ := os.ReadFile(original)
	if string(raw) != "keep\nold\n" {
		t.Fatalf("original=%q", raw)
	}
	info, _ := os.Stat(original)
	if info.Mode().Perm() != 0640 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Fatalf("created file remains: %v", err)
	}
	rows, _ = agent.AppQuickTasks()
	if rows[0].State != "undone" {
		t.Fatal(rows)
	}
}

func TestAppQuickUndoMismatchNeverRestoresTheOtherFile(t *testing.T) {
	agent, node, dir := appNodeFixture(t, true)
	tool := appBareTool(t, dir, "write")
	for _, path := range []string{"one", "two"} {
		call := appWriteCall(path, "ours")
		if _, bad, err := agent.executeTaskTool(context.Background(), tool, call, json.RawMessage(call.Function.Arguments)); bad || err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	node.graph.mu.Lock()
	node.state = TaskDone
	node.graph.mu.Unlock()
	if err := os.WriteFile(filepath.Join(dir, "two"), []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	undone, changed, err := agent.UndoQuickTask(1)
	if err != nil || len(undone) != 0 || len(changed) != 1 {
		t.Fatalf("undo %v %v %v", undone, changed, err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "one"))
	if string(raw) != "ours" {
		t.Fatalf("partially restored %q", raw)
	}
}

func TestAppQuickCannotRunShellOrWriteFile31BeforePromotion(t *testing.T) {
	for _, kind := range []string{"shell", "file31"} {
		t.Run(kind, func(t *testing.T) {
			agent, node, dir := appNodeFixture(t, true)
			lane, stop := agent.WatchTaskUpdates()
			defer stop()
			call := ai.ToolCall{ID: "test", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"go test ./..."}`}}
			if kind == "file31" {
				tool := appBareTool(t, dir, "write")
				for i := 0; i < QuickTaskFileLimit; i++ {
					call := appWriteCall(strconv.Itoa(i), "ours")
					if _, bad, err := agent.executeTaskTool(context.Background(), tool, call, json.RawMessage(call.Function.Arguments)); bad || err != nil {
						t.Fatalf("file %d: %v", i, err)
					}
				}
				call = appWriteCall("31", "blocked")
			}
			ran := make(chan struct{}, 1)
			tool := bare.Tool{Name: call.Function.Name, Execute: func(context.Context, json.RawMessage) (string, bool, error) {
				ran <- struct{}{}
				return "ran", false, nil
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan string, 1)
			go func() {
				text, _, _ := agent.executeTaskTool(ctx, tool, call, json.RawMessage(call.Function.Arguments))
				done <- text
			}()
			var proposal TaskNotice
			for proposal.ID == 0 {
				select {
				case event := <-lane:
					if event.Kind == EventTaskProposal && event.Task != nil {
						proposal = *event.Task
					}
				case <-time.After(2 * time.Second):
					t.Fatal("promotion did not ask")
				}
			}
			if !proposal.Deadline.IsZero() {
				t.Fatal("promotion has a clock")
			}
			select {
			case <-ran:
				t.Fatal("action ran before answer")
			default:
			}
			if err := agent.AppTaskAction(AppTaskIntent{ID: strconv.FormatUint(proposal.ID, 10), Action: "decline"}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("decline did not release worker")
			}
			select {
			case <-ran:
				t.Fatal("declined action ran")
			default:
			}
			if kind == "file31" {
				if _, err := os.Stat(filepath.Join(dir, "31")); !os.IsNotExist(err) {
					t.Fatal("file 31 exists")
				}
			}
			node.graph.mu.Lock()
			node.state = TaskDone
			node.graph.mu.Unlock()
		})
	}
}

func TestAppProposalClockAndTypingHoldArePerTask(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.AskConsent = true; c.TaskAutoApproveSeconds = 12 })
	hub := newEventHub()
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	agent.taskNow = func() time.Time { return now }
	expiry := make(chan time.Time, 1)
	timerCalls := 0
	agent.taskTimer = func(after time.Duration) (<-chan time.Time, func()) {
		timerCalls++
		if after != 12*time.Second {
			t.Errorf("timer=%v", after)
		}
		return expiry, func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waits := []*taskWait{}
	for id := uint64(1); id <= 2; id++ {
		wait, err := agent.openTask(ctx, id, taskSpec{title: "Proposal", brief: "work", acceptance: taskPersonAcceptance}, "")
		if err != nil {
			t.Fatal(err)
		}
		waits = append(waits, wait)
	}
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "typing"}); err != nil {
		t.Fatal(err)
	}
	rows, err := agent.AppTasks()
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if rows[0].State != "held" || !rows[0].Deadline.IsZero() || !rows[1].Deadline.Equal(now.Add(12*time.Second)) {
		t.Fatalf("per-task clock %+v", rows)
	}
	irreversible, err := agent.openTask(ctx, 3, taskSpec{title: "Irreversible", irreversible: true}, "")
	if err != nil {
		t.Fatal(err)
	}
	if timerCalls != 2 {
		t.Fatalf("irreversible started timer: %d", timerCalls)
	}
	for _, id := range []uint64{1, 2, 3} {
		agent.ResolveTask(id, TaskAnswer{Approved: false})
	}
	for _, wait := range append(waits, irreversible) {
		_, _ = wait.answer()
	}
}

func TestAppTaskNotesReceiptMarksOnlyTheRequestThatReadIt(t *testing.T) {
	agent, node, _ := appNodeFixture(t, false)
	node.graph.mu.Lock()
	direction, _, _ := node.assignment.hear("read this", directionFromPerson, time.Now(), spokenSource{})
	node.app.Notes = []AppTaskNote{{Text: "read this", Direction: direction}}
	node.graph.mu.Unlock()
	row, found, err := agent.AppTask("1")
	if err != nil || !found || row.Notes[0].ReadAtStep != nil {
		t.Fatalf("premature receipt %+v %v", row, err)
	}
	agent.config.tasker = node.graph
	agent.config.taskID = node.id
	agent.markDirectionsCarried([]uint64{direction})
	row, _, _ = agent.AppTask("1")
	if row.Notes[0].ReadAtStep == nil || *row.Notes[0].ReadAtStep != 1 {
		t.Fatal(row.Notes)
	}
	node.graph.mu.Lock()
	record := node.recordLocked()
	node.graph.mu.Unlock()
	restored := restoreNode(node.graph, record)
	restored.graph.mu.Lock()
	notes := restored.appNotesLocked()
	restored.graph.mu.Unlock()
	if notes[0].ReadAtStep == nil || *notes[0].ReadAtStep != 1 {
		t.Fatal(notes)
	}
}

func TestAppTaskPauseAndStaleResumeCannotStartTheNextStep(t *testing.T) {
	agent, node, _ := appNodeFixture(t, false)
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "pause"}); err != nil {
		t.Fatal(err)
	}
	stale := uint64(1)
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "resume", Version: &stale}); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("resume: %v", err)
	}
	node.graph.mu.Lock()
	paused := node.app.Paused
	node.graph.mu.Unlock()
	if !paused {
		t.Fatal("stale resume released pause")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := node.appBoundary(ctx); err != context.Canceled {
		t.Fatalf("boundary: %v", err)
	}
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: "resume"}); err != nil {
		t.Fatal(err)
	}
	if err := node.appBoundary(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAppTaskCapStopsTheNextModelCall(t *testing.T) {
	agent, node, _ := appNodeFixture(t, false)
	agent.config.taskCapUSD = 2
	node.graph.mu.Lock()
	node.cost = 2
	node.graph.mu.Unlock()
	_, _, err := agent.completeWithRetry(context.Background(), nil, agent.Model(), "", nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "$2 limit") {
		t.Fatalf("cap: %v", err)
	}
}

func TestAppQuickApprovalAdmitsAnOrdinaryTaskWithoutExecutingTheBlockedCall(t *testing.T) {
	agent, node, dir := appNodeFixture(t, true)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	call := ai.ToolCall{ID: "test", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"go test ./..."}`}}
	ran := false
	tool := bare.Tool{Name: "bash", Execute: func(context.Context, json.RawMessage) (string, bool, error) { ran = true; return "ran", false, nil }}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _, _ = agent.executeTaskTool(context.Background(), tool, call, json.RawMessage(call.Function.Arguments))
	}()
	var proposal TaskNotice
	for proposal.ID == 0 {
		select {
		case event := <-lane:
			if event.Kind == EventTaskProposal && event.Task != nil {
				proposal = *event.Task
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no promotion")
		}
	}
	approved := make(chan error, 1)
	go func() {
		_, _, err := agent.AppQuickTaskAction(AppTaskIntent{ID: "1", Action: "promote"})
		approved <- err
	}()
	select {
	case err := <-approved:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Promote waited on its own worker")
	}

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("approval did not finish")
	}
	node.graph.mu.Lock()
	full := node.graph.nodes[proposal.ID]
	promoted := node.spec.quick.promoted
	node.graph.mu.Unlock()
	if ran || !promoted || full == nil || full.kind == TaskKindQuick || full.spec.quick != nil || full.Ground != dir || full.Mode != TaskModeInPlace || full.spec.acceptance == "" || len(full.dependsOn) != 1 || full.dependsOn[0] != node.id {
		t.Fatalf("promoted=%v ran=%v task=%+v", promoted, ran, full)
	}
	node.graph.mu.Lock()
	node.state = TaskDone
	node.graph.quitting = true
	node.graph.mu.Unlock()
}

func TestAppQuickCaptureFailureLeavesTheFileUntouched(t *testing.T) {
	agent, node, dir := appNodeFixture(t, true)
	blocked := filepath.Join(dir, "cannot-write-checkpoint")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	node.graph.mu.Lock()
	node.graph.store = &taskStore{path: blocked}
	node.graph.mu.Unlock()
	path := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	call := appWriteCall(path, "after")
	_, failed, err := agent.executeTaskTool(context.Background(), appBareTool(t, dir, "write"), call, json.RawMessage(call.Function.Arguments))
	if !failed || err != nil {
		t.Fatalf("capture failure: %v %v", failed, err)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "before" {
		t.Fatalf("file changed: %q", raw)
	}
	node.graph.mu.Lock()
	captures := len(node.spec.quick.captures)
	node.graph.store = nil
	node.graph.mu.Unlock()
	if captures != 0 {
		t.Fatalf("failed snapshot leaked %d captures", captures)
	}
}

func TestAppQuickStoppedTaskCanUndoItsCapturedWrites(t *testing.T) {
	agent, node, dir := appNodeFixture(t, true)
	call := appWriteCall("stopped.txt", "ours")
	if _, bad, err := agent.executeTaskTool(context.Background(), appBareTool(t, dir, "write"), call, json.RawMessage(call.Function.Arguments)); bad || err != nil {
		t.Fatalf("write: %v", err)
	}
	node.graph.mu.Lock()
	node.state = TaskFailed
	node.stopped = true
	node.graph.mu.Unlock()
	rows, _ := agent.AppQuickTasks()
	if rows[0].State != "done" {
		t.Fatal(rows)
	}
	undone, changed, err := agent.UndoQuickTask(1)
	if err != nil || len(undone) != 1 || len(changed) != 0 {
		t.Fatalf("undo stopped task: %v %v %v", undone, changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stopped.txt")); !os.IsNotExist(err) {
		t.Fatalf("captured file remains: %v", err)
	}
}

// The proposal card's Change model moves one proposal's model and nothing else.
func TestAppProposalModelChangesOnlyTheSelectedProposal(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.AskConsent = true; c.TaskAutoApproveSeconds = 12 })
	hub := newEventHub()
	agent.mu.Lock()
	agent.hub = hub
	agent.mu.Unlock()
	agent.config.TaskModels = func() []string { return []string{"alpha/one", "beta/two"} }
	before := agent.Model()
	agent.taskTimer = func(time.Duration) (<-chan time.Time, func()) { return make(chan time.Time), func() {} }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for id := uint64(1); id <= 2; id++ {
		if _, err := agent.openTask(ctx, id, taskSpec{title: "Proposal", brief: "work", acceptance: taskPersonAcceptance}, ""); err != nil {
			t.Fatal(err)
		}
	}
	if err := agent.AppTaskAction(AppTaskIntent{ID: "1", Action: AppActionProposalModel, Model: "beta/two"}); err != nil {
		t.Fatal(err)
	}
	rows, err := agent.AppTasks()
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	if rows[0].Model != "beta/two" || rows[1].Model == "beta/two" {
		t.Fatalf("model moved the wrong proposal: %+v", rows)
	}
	if agent.Model() != before {
		t.Fatalf("agent model changed %q -> %q", before, agent.Model())
	}
	// Unknown, empty, stale and not-a-proposal are refused and change nothing.
	stale := rows[0].Version + 9
	for name, intent := range map[string]AppTaskIntent{
		"unknown": {ID: "2", Model: "nope/none"},
		"empty":   {ID: "2", Model: " "},
		"stale":   {ID: "2", Model: "alpha/one", Version: &stale},
		"state":   {ID: "2", Model: "alpha/one", State: ptr("running")},
		"missing": {ID: "9", Model: "alpha/one"},
	} {
		intent.Action = AppActionProposalModel
		if err := agent.AppTaskAction(intent); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
	rows, _ = agent.AppTasks()
	if rows[1].Model == "alpha/one" {
		t.Fatalf("a refused change landed: %+v", rows[1])
	}
	for _, id := range []uint64{1, 2} {
		agent.ResolveTask(id, TaskAnswer{Approved: false})
	}
}

func ptr[T any](v T) *T { return &v }
