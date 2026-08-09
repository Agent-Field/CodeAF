package steploop

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/orclient"
	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
	"github.com/Agent-Field/swe-pro-go/internal/plandb"
	"github.com/Agent-Field/swe-pro-go/internal/session/instruction"
)

func stringPtr(value string) *string { return &value }
func boolPtr(value bool) *bool       { return &value }

func baseUser(id, agent string, parts msgmodel.Parts) msgmodel.WithParts {
	return msgmodel.WithParts{
		Info: msgmodel.User{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: "ses_1"},
			Time:        msgmodel.TimeCreated{Created: 1},
			Agent:       agent,
			Model:       msgmodel.UserModel{ProviderID: "openrouter", ModelID: "model-1"},
		},
		Parts: parts,
	}
}

func baseAssistant(id, parent, agent, finish string, parts msgmodel.Parts) msgmodel.WithParts {
	return msgmodel.WithParts{
		Info: msgmodel.Assistant{
			MessageBase: msgmodel.MessageBase{ID: id, SessionID: "ses_1"},
			Time:        msgmodel.AssistantTime{Created: 2},
			ParentID:    parent,
			ModelID:     "model-1",
			ProviderID:  "openrouter",
			Mode:        agent,
			Agent:       agent,
			Path:        msgmodel.AssistantPath{Cwd: "/work", Root: "/work"},
			Tokens:      msgmodel.Tokens{Cache: msgmodel.TokenCache{}},
			Finish:      stringPtr(finish),
		},
		Parts: parts,
	}
}

func TestPromptLines1612To1620ExitTruthTable(t *testing.T) {
	finishes := []*string{
		nil,
		stringPtr(""),
		stringPtr(orclient.FinishStop),
		stringPtr(orclient.FinishLength),
		stringPtr(orclient.FinishContentFilter),
		stringPtr(orclient.FinishToolCalls),
		stringPtr(orclient.FinishError),
		stringPtr(orclient.FinishOther),
		stringPtr("unknown"),
	}
	for _, finish := range finishes {
		label := "unset"
		if finish != nil {
			label = *finish
			if label == "" {
				label = "empty"
			}
		}
		for _, pending := range []bool{false, true} {
			for _, ordering := range []string{"lt", "eq", "gt"} {
				t.Run("prompt.ts:1612-1620/"+label+"/tool="+jscompat.FormatNumber(boolNumber(pending))+"/"+ordering, func(t *testing.T) {
					userID := "msg_2"
					switch ordering {
					case "lt":
						userID = "msg_1"
					case "gt":
						userID = "msg_3"
					}
					user := msgmodel.User{MessageBase: msgmodel.MessageBase{ID: userID}}
					assistant := msgmodel.Assistant{MessageBase: msgmodel.MessageBase{ID: "msg_2"}, Finish: finish}
					parts := msgmodel.Parts{}
					if pending {
						parts = append(parts, msgmodel.ToolPart{
							PartBase: msgmodel.PartBase{ID: "prt_1", MessageID: assistant.ID},
							CallID:   "call_1", Tool: "fake", State: msgmodel.PendingToolState(),
						})
					}
					msgs := []msgmodel.WithParts{{Info: assistant, Parts: parts}}
					got := ShouldExit(&user, &assistant, msgs)
					want := finish != nil && *finish != "" && *finish != orclient.FinishToolCalls && !pending && ordering == "lt"
					if got != want {
						t.Fatalf("ShouldExit=%v want %v", got, want)
					}
				})
			}
		}
	}
}

func boolNumber(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

type memoryStore struct {
	mu       sync.Mutex
	messages []msgmodel.WithParts
	events   []string
}

func (s *memoryStore) Messages(_ context.Context, sessionID string) ([]msgmodel.WithParts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw, err := jscompat.Stringify(s.messages)
	if err != nil {
		return nil, err
	}
	var copied []msgmodel.WithParts
	if err := json.Unmarshal(raw, &copied); err != nil {
		return nil, err
	}
	return copied, nil
}

func (s *memoryStore) UpdateMessage(_ context.Context, info msgmodel.Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "message:"+info.MessageRole()+":"+info.MessageID())
	for index := range s.messages {
		if s.messages[index].Info.MessageID() == info.MessageID() {
			s.messages[index].Info = info
			return nil
		}
	}
	s.messages = append(s.messages, msgmodel.WithParts{Info: info, Parts: msgmodel.Parts{}})
	return nil
}

func (s *memoryStore) UpdatePart(_ context.Context, part msgmodel.Part) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	base := part.Base()
	s.events = append(s.events, "part:"+part.PartType()+":"+base.ID)
	for mi := range s.messages {
		if s.messages[mi].Info.MessageID() != base.MessageID {
			continue
		}
		for pi := range s.messages[mi].Parts {
			if s.messages[mi].Parts[pi].Base().ID == base.ID {
				s.messages[mi].Parts[pi] = part
				return nil
			}
		}
		s.messages[mi].Parts = append(s.messages[mi].Parts, part)
		return nil
	}
	return errors.New("part message not found: " + base.MessageID)
}

func (s *memoryStore) snapshot() []msgmodel.WithParts {
	msgs, err := s.Messages(context.Background(), "ses_1")
	if err != nil {
		panic(err)
	}
	return msgs
}

func (s *memoryStore) rawSnapshot() []msgmodel.WithParts {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]msgmodel.WithParts(nil), s.messages...)
}

type scriptedClient struct {
	mu       sync.Mutex
	scripts  [][]orclient.StreamPart
	requests []orclient.RequestParams
}

func (c *scriptedClient) Stream(_ context.Context, params orclient.RequestParams) (PartStream, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, params)
	if len(c.scripts) == 0 {
		return nil, errors.New("unexpected extra LLM turn")
	}
	parts := c.scripts[0]
	c.scripts = c.scripts[1:]
	return &SliceStream{Parts: parts}, nil
}

func finishPart(reason string) orclient.FinishPart {
	return orclient.FinishPart{
		FinishReason: orclient.FinishReason{Unified: reason},
		Usage:        calc.LanguageModelV3Usage{},
	}
}

type immediateTool struct {
	mu    sync.Mutex
	calls []ToolCall
}

type historyTool struct {
	loaded chan []string
}

func (tool *historyTool) Execute(ctx context.Context, _ ToolCall) (ToolResult, error) {
	paths := instruction.Loaded(ToolMessagesFromContext(ctx)).Values()
	tool.loaded <- paths
	return ToolResult{Output: "nested reminder re-injected", Metadata: msgmodel.RawObject(`{}`)}, nil
}

type taskControllerSpy struct {
	mu             sync.Mutex
	overflowChecks []OverflowOptions
	creates        int
	pruned         chan string
}

func (*taskControllerSpy) HandleSubtask(context.Context, TaskInput, msgmodel.SubtaskPart) error {
	return nil
}

func (*taskControllerSpy) ProcessCompaction(
	context.Context, TaskInput, msgmodel.CompactionPart,
) (Result, error) {
	return ResultContinue, nil
}

func (s *taskControllerSpy) IsOverflow(
	_ context.Context, _ msgmodel.Assistant, _ Model, options OverflowOptions,
) (bool, error) {
	s.mu.Lock()
	s.overflowChecks = append(s.overflowChecks, options)
	s.mu.Unlock()
	return false, nil
}

func (s *taskControllerSpy) CreateCompaction(
	context.Context, string, msgmodel.User, bool,
) error {
	s.mu.Lock()
	s.creates++
	s.mu.Unlock()
	return nil
}

func (s *taskControllerSpy) Prune(_ context.Context, sessionID string) error {
	s.pruned <- sessionID
	return nil
}

func (e *immediateTool) Execute(_ context.Context, call ToolCall) (ToolResult, error) {
	e.mu.Lock()
	e.calls = append(e.calls, call)
	e.mu.Unlock()
	return ToolResult{Title: "ok", Metadata: msgmodel.RawObject(`{"source":"fake"}`), Output: "value=2"}, nil
}

func fixedSeams(t *testing.T) {
	t.Helper()
	restoreNow := SetNowForTesting(func() uint64 { return 1000 })
	sequence := 0
	restoreID := SetIDFactoryForTesting(func(prefix string) string {
		sequence++
		return prefix + "_1" + leftPad(sequence, 4)
	})
	t.Cleanup(restoreID)
	t.Cleanup(restoreNow)
}

func leftPad(value, width int) string {
	text := jscompat.FormatNumber(float64(value))
	for len(text) < width {
		text = "0" + text
	}
	return text
}

func testResolver() ModelResolver {
	return StaticModelResolver{Calc: calc.Model{Limit: calc.ModelLimit{Context: 128000, Output: 32000}}}
}

func TestScriptedMultiTurnToolSequenceAndReminderDrainOrdering(t *testing.T) {
	fixedSeams(t)
	store := &memoryStore{messages: []msgmodel.WithParts{baseUser("msg_0000", "build", msgmodel.Parts{
		msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0000", SessionID: "ses_1", MessageID: "msg_0000"}, Text: "double one"},
	})}}
	client := &scriptedClient{scripts: [][]orclient.StreamPart{
		{
			orclient.ToolInputStartPart{ID: "call_1", ToolName: "double"},
			orclient.ToolCallPart{ToolCallID: "call_1", ToolName: "double", Input: `{"x":1}`},
			finishPart(orclient.FinishToolCalls),
		},
		{
			orclient.TextStartPart{ID: "text_1"},
			orclient.TextDeltaPart{ID: "text_1", Delta: "done"},
			orclient.TextEndPart{ID: "text_1"},
			finishPart(orclient.FinishStop),
		},
	}}
	executor := &immediateTool{}
	queue := NewReminderQueue()
	queue.QueueReminder("ses_1", "  observer correction  ")
	cleared := []string{}

	loop := Loop{Store: store, Client: client, Models: testResolver(), Executor: executor}
	final, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
		Tools: []ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "double", Description: "double", InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
		AfterAssistant: func(_ context.Context, messageID string) {
			cleared = append(cleared, messageID)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if final.Finish == nil || *final.Finish != orclient.FinishStop {
		t.Fatalf("final finish = %#v", final.Finish)
	}
	if len(client.requests) != 2 {
		t.Fatalf("LLM turns = %d, want 2", len(client.requests))
	}
	if got := modelRoles(client.requests[0].Prompt); strings.Join(got, ",") != "user" {
		t.Fatalf("first prompt roles = %v", got)
	}
	if got := modelRoles(client.requests[1].Prompt); strings.Join(got, ",") != "user,assistant,tool" {
		t.Fatalf("second prompt roles = %v", got)
	}
	if strings.Contains(fixtureStringify(t, client.requests), "observer correction") {
		t.Fatal("Run drained the observer reminder during an internal turn")
	}

	messages := store.rawSnapshot()
	if len(messages) != 3 {
		t.Fatalf("persisted messages = %d, want user + two assistants", len(messages))
	}
	assertPartTypes(t, messages[1].Parts, "step-start,tool,step-finish")
	assertPartTypes(t, messages[2].Parts, "step-start,text,step-finish")
	tool := messages[1].Parts[1].(msgmodel.ToolPart)
	if tool.State.ToolStatus() != msgmodel.ToolStatusCompleted {
		t.Fatalf("tool status = %s", tool.State.ToolStatus())
	}
	if len(executor.calls) != 1 || string(executor.calls[0].Input) != `{"x":1}` {
		t.Fatalf("tool calls = %#v", executor.calls)
	}
	if len(cleared) != 2 || cleared[0] == cleared[1] {
		t.Fatalf("assistant instruction claims cleared = %v", cleared)
	}

	user := messages[0].Info.(msgmodel.User)
	bootstrap := syntheticTextPart("ses_1", user.ID, "PlanDB bootstrap")
	withReminders := queue.DrainIntoUserMessage(user, msgmodel.Parts{bootstrap})
	if len(withReminders) != 2 {
		t.Fatalf("drained parts = %d", len(withReminders))
	}
	if got := withReminders[0].(msgmodel.TextPart).Text; got != "PlanDB bootstrap" {
		t.Fatalf("bootstrap order changed: %q", got)
	}
	if got := withReminders[1].(msgmodel.TextPart).Text; got != "observer correction" {
		t.Fatalf("reminder = %q", got)
	}
}

func TestToolExecutionUsesPostCompactionHistory(t *testing.T) {
	// Round-2 compacted-history contract: read metadata before the completed
	// compaction is absent from the next read's tool context, so its nested
	// instruction reminder can be injected again.
	fixedSeams(t)
	summary := true
	compactionUser := baseUser("msg_0002", "coder", msgmodel.Parts{msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "compact", SessionID: "ses_1", MessageID: "msg_0002"},
		Auto:     true,
	}})
	summaryAssistant := baseAssistant("msg_0003", "msg_0002", "compaction", orclient.FinishStop, msgmodel.Parts{
		msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "summary", SessionID: "ses_1", MessageID: "msg_0003"}, Text: "summary"},
	})
	summaryInfo := summaryAssistant.Info.(msgmodel.Assistant)
	summaryInfo.Summary = &summary
	summaryAssistant.Info = summaryInfo
	oldRead := msgmodel.ToolPart{
		PartBase: msgmodel.PartBase{ID: "old-read", SessionID: "ses_1", MessageID: "msg_0001"},
		CallID:   "old", Tool: "read",
		State: msgmodel.CompletedToolState(
			msgmodel.RawObject(`{}`), "old reminder", "read", msgmodel.RawObject(`{"loaded":["/work/src/AGENTS.md"]}`),
			1, 2, nil,
		),
	}
	store := &memoryStore{messages: []msgmodel.WithParts{
		baseUser("msg_0000", "coder", msgmodel.Parts{msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "initial", SessionID: "ses_1", MessageID: "msg_0000"}, Text: "first read",
		}}),
		baseAssistant("msg_0001", "msg_0000", "coder", orclient.FinishToolCalls, msgmodel.Parts{oldRead}),
		compactionUser, summaryAssistant,
		baseUser("msg_0004", "coder", msgmodel.Parts{msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "again", SessionID: "ses_1", MessageID: "msg_0004"}, Text: "read again",
		}}),
	}}
	client := &scriptedClient{scripts: [][]orclient.StreamPart{
		{
			orclient.ToolInputStartPart{ID: "call_2", ToolName: "read"},
			orclient.ToolCallPart{ToolCallID: "call_2", ToolName: "read", Input: `{}`},
			finishPart(orclient.FinishToolCalls),
		},
		{finishPart(orclient.FinishStop)},
	}}
	executor := &historyTool{loaded: make(chan []string, 1)}
	loop := Loop{Store: store, Client: client, Models: testResolver(), Executor: executor}
	if _, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
		Tools: []ToolDefinition{{Provider: orclient.Tool{
			Type: "function", Name: "read", InputSchema: json.RawMessage(`{"type":"object"}`),
		}}},
	}); err != nil {
		t.Fatal(err)
	}
	if loaded := <-executor.loaded; len(loaded) != 0 {
		t.Fatalf("post-compaction read saw stale loaded paths: %v", loaded)
	}
}

func TestSummaryTurnSkipsRecursiveCompactionAndPrunesOnExit(t *testing.T) {
	// Validation contracts 2 and 3: a finished summary bypasses the pre-turn
	// overflow check, while a wired controller is pruned after natural exit.
	fixedSeams(t)
	summary := true
	store := &memoryStore{messages: []msgmodel.WithParts{
		baseUser("msg_0001", "coder", msgmodel.Parts{
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0001", SessionID: "ses_1", MessageID: "msg_0001"}, Text: "original"},
		}),
		baseAssistant("msg_0002", "msg_0001", "compaction", orclient.FinishStop, msgmodel.Parts{
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0002", SessionID: "ses_1", MessageID: "msg_0002"}, Text: "summary"},
		}),
		baseUser("msg_0003", "coder", msgmodel.Parts{
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0003", SessionID: "ses_1", MessageID: "msg_0003"}, Text: "continue"},
		}),
	}}
	assistant := store.messages[1].Info.(msgmodel.Assistant)
	assistant.Summary = &summary
	store.messages[1].Info = assistant
	client := &scriptedClient{scripts: [][]orclient.StreamPart{{
		orclient.TextStartPart{ID: "text_1"},
		orclient.TextDeltaPart{ID: "text_1", Delta: "done"},
		orclient.TextEndPart{ID: "text_1"},
		finishPart(orclient.FinishStop),
	}}}
	tasks := &taskControllerSpy{pruned: make(chan string, 1)}
	loop := Loop{Store: store, Client: client, Models: testResolver(), Tasks: tasks}
	if _, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case sessionID := <-tasks.pruned:
		if sessionID != "ses_1" {
			t.Fatalf("pruned session = %q", sessionID)
		}
	case <-time.After(time.Second):
		t.Fatal("prune fork did not run")
	}
	tasks.mu.Lock()
	defer tasks.mu.Unlock()
	if len(tasks.overflowChecks) != 1 {
		t.Fatalf("overflow checks = %d, want only the post-response check", len(tasks.overflowChecks))
	}
	if tasks.overflowChecks[0].Agent == nil || *tasks.overflowChecks[0].Agent != "coder" {
		t.Fatalf("overflow agent = %#v", tasks.overflowChecks[0].Agent)
	}
	if tasks.creates != 0 {
		t.Fatalf("recursive compactions = %d", tasks.creates)
	}
}

func modelRoles(messages []msgmodel.ModelMessage) []string {
	out := make([]string, len(messages))
	for i, message := range messages {
		out[i] = message.Role
	}
	return out
}

func assertPartTypes(t *testing.T, parts msgmodel.Parts, want string) {
	t.Helper()
	got := make([]string, len(parts))
	for index, part := range parts {
		got[index] = part.PartType()
	}
	if strings.Join(got, ",") != want {
		t.Fatalf("part types = %v, want %s", got, want)
	}
}

type oneShotGuard struct {
	mu     sync.Mutex
	marked []string
}

func (g *oneShotGuard) FindOpenCapExhaustFailures(_ context.Context, _ ExitGuardQuery) ([]OpenFailure, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.marked) > 0 {
		return []OpenFailure{}, nil
	}
	return []OpenFailure{{
		TaskID: "t-failed", Title: "failed leaf", Reason: "cap exhausted",
		Bugs: []FailureBug{}, RepairHints: []string{"split it"}, BlockedDescendants: 2,
	}}, nil
}

func (g *oneShotGuard) MarkFailureNudged(rootTaskID, taskID string) {
	g.mu.Lock()
	g.marked = append(g.marked, rootTaskID+"/"+taskID)
	g.mu.Unlock()
}

func TestExitGuardPersistsRecoveryAndContinuesWithoutIncrementingStep(t *testing.T) {
	fixedSeams(t)
	bootstrap := "Project: p-one\nRoot task: t-root"
	store := &memoryStore{messages: []msgmodel.WithParts{
		baseUser("msg_0000", "build", msgmodel.Parts{
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0000", SessionID: "ses_1", MessageID: "msg_0000"}, Text: bootstrap},
		}),
		baseAssistant("msg_0001", "msg_0000", "build", orclient.FinishStop, msgmodel.Parts{}),
	}}
	client := &scriptedClient{scripts: [][]orclient.StreamPart{{finishPart(orclient.FinishStop)}}}
	guard := &oneShotGuard{}
	maxSteps := float64(2)
	loop := Loop{Store: store, Client: client, Models: testResolver(), ExitGuard: guard}
	if _, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work", MaxSteps: &maxSteps,
	}); err != nil {
		t.Fatal(err)
	}
	if len(client.requests) != 1 {
		t.Fatalf("turns = %d", len(client.requests))
	}
	for _, message := range client.requests[0].Prompt {
		if message.Role == "assistant" && message.Content == MaxStepsPrompt {
			t.Fatal("exit guard incremented step before continuing")
		}
	}
	if strings.Join(guard.marked, ",") != "t-root/t-failed" {
		t.Fatalf("marks = %v", guard.marked)
	}
	messages := store.snapshot()
	var recovery *msgmodel.WithParts
	for index := range messages {
		if user, ok := messages[index].Info.(msgmodel.User); ok && user.ID != "msg_0000" {
			recovery = &messages[index]
			break
		}
	}
	if recovery == nil || len(recovery.Parts) != 1 {
		t.Fatal("recovery user was not persisted")
	}
	text := recovery.Parts[0].(msgmodel.TextPart)
	if !boolValue(text.Synthetic) || text.Text != RenderRecoveryReminder([]OpenFailure{{
		TaskID: "t-failed", Title: "failed leaf", Reason: "cap exhausted",
		Bugs: []FailureBug{}, RepairHints: []string{"split it"}, BlockedDescendants: 2,
	}}) {
		t.Fatalf("recovery text mismatch: %q", text.Text)
	}
}

type scriptedScheduler struct {
	mu      sync.Mutex
	summary string
	err     error
	calls   int
}

func (s *scriptedScheduler) Pump(_ context.Context, _ SchedulerInput) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.calls == 1 {
		return s.summary, s.err
	}
	return "", nil
}

func TestSchedulerErrorStopsRootLoop(t *testing.T) {
	fixedSeams(t)
	store := &memoryStore{messages: []msgmodel.WithParts{
		baseUser("msg_0000", "orchestrator", msgmodel.Parts{
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0000", SessionID: "ses_1", MessageID: "msg_0000"}, Text: "Project: p-one\nRoot task: t-root"},
		}),
	}}
	client := &scriptedClient{scripts: [][]orclient.StreamPart{{finishPart(orclient.FinishToolCalls)}}}
	wantErr := errors.New("scheduler stalled with open descendants")
	scheduler := &scriptedScheduler{err: wantErr}
	loop := Loop{Store: store, Client: client, Models: testResolver(), Scheduler: scheduler}

	_, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run error = %v, want %v", err, wantErr)
	}
	if scheduler.calls != 1 || len(client.requests) != 1 {
		t.Fatalf("scheduler calls=%d model turns=%d, want 1/1", scheduler.calls, len(client.requests))
	}
}

func TestSchedulerSummaryPersistsBetweenTurns(t *testing.T) {
	fixedSeams(t)
	bootstrap := "Project: p-one\nRoot task: t-root"
	store := &memoryStore{messages: []msgmodel.WithParts{
		baseUser("msg_0000", "orchestrator", msgmodel.Parts{
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "prt_0000", SessionID: "ses_1", MessageID: "msg_0000"}, Text: bootstrap},
		}),
	}}
	client := &scriptedClient{scripts: [][]orclient.StreamPart{
		{finishPart(orclient.FinishToolCalls)},
		{finishPart(orclient.FinishStop)},
	}}
	scheduler := &scriptedScheduler{summary: "scheduler cycle summary"}
	loop := Loop{Store: store, Client: client, Models: testResolver(), Scheduler: scheduler}
	if _, err := loop.Run(context.Background(), RunOptions{
		SessionID: "ses_1", Workspace: "/work", Worktree: "/work",
	}); err != nil {
		t.Fatal(err)
	}
	if scheduler.calls != 2 {
		t.Fatalf("scheduler calls = %d, want one before turn 2 and one before natural exit", scheduler.calls)
	}
	if len(client.requests) != 2 || !strings.Contains(fixtureStringify(t, client.requests[1].Prompt), scheduler.summary) {
		t.Fatal("second turn did not receive persisted scheduler summary")
	}
	messages := store.snapshot()
	if len(messages) != 4 {
		t.Fatalf("messages = %d, want user/assistant/scheduler-user/assistant", len(messages))
	}
	summaryUser, ok := messages[2].Info.(msgmodel.User)
	if !ok || summaryUser.Agent != "orchestrator" || len(messages[2].Parts) != 1 {
		t.Fatalf("scheduler message = %#v", messages[2])
	}
	text := messages[2].Parts[0].(msgmodel.TextPart)
	if text.Text != scheduler.summary || !boolValue(text.Synthetic) {
		t.Fatalf("scheduler part = %#v", text)
	}
}

type blockingTool struct {
	release <-chan struct{}
}

func (e blockingTool) Execute(_ context.Context, _ ToolCall) (ToolResult, error) {
	<-e.release
	return ToolResult{Metadata: msgmodel.RawObject("{}")}, nil
}

func TestCleanupWaitsOutstandingToolsConcurrentlyThenAbortsSpreadStates(t *testing.T) {
	// The TS AI SDK settles every execute() inside streamText, so the 250ms
	// force-abort drain (processor.ts:634-656) only ever fires on an aborted
	// turn. The Go equivalent: a cancelled ctx grants the grace window, then
	// force-writes the spread aborted state.
	fixedSeams(t)
	store := &memoryStore{}
	assistant := baseAssistant("msg_0001", "msg_0000", "build", "", nil).Info.(msgmodel.Assistant)
	assistant.Finish = nil
	if err := store.UpdateMessage(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	defer close(release)
	processor := NewProcessor(ProcessorOptions{
		Store: store, Assistant: assistant, Model: Model{},
		Tools: []ToolDefinition{
			{Provider: orclient.Tool{Type: "function", Name: "a"}},
			{Provider: orclient.Tool{Type: "function", Name: "b"}},
		},
		Executor: blockingTool{release: release}, WaitTimeout: 40 * time.Millisecond,
	})
	stream := &SliceStream{Parts: []orclient.StreamPart{
		orclient.ToolInputStartPart{ID: "a", ToolName: "a"},
		orclient.ToolCallPart{ToolCallID: "a", ToolName: "a", Input: `{}`},
		orclient.ToolInputStartPart{ID: "b", ToolName: "b"},
		orclient.ToolCallPart{ToolCallID: "b", ToolName: "b", Input: `{}`},
		finishPart(orclient.FinishToolCalls),
	}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := processor.Process(ctx, stream); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= 75*time.Millisecond {
		t.Fatalf("two 40ms waits were serial: %s", elapsed)
	}
	messages := store.rawSnapshot()
	parts := messages[0].Parts
	assertPartTypes(t, parts, "step-start,tool,tool,step-finish")
	for _, raw := range parts {
		tool, ok := raw.(msgmodel.ToolPart)
		if !ok {
			continue
		}
		if tool.State.ToolStatus() != msgmodel.ToolStatusError {
			t.Fatalf("%s status = %s", tool.CallID, tool.State.ToolStatus())
		}
		state := fixtureStringify(t, tool.State)
		for _, fragment := range []string{`"raw":""`, `"error":"Tool execution aborted"`, `"interrupted":true`} {
			if !strings.Contains(state, fragment) {
				t.Fatalf("%s state %s missing %s", tool.CallID, state, fragment)
			}
		}
	}
}

func TestCleanupBoundsDispatchedToolAfterErrorPart(t *testing.T) {
	testCleanupBoundsDispatchedToolAfterAbnormalPart(t, orclient.ErrorPart{
		Error: json.RawMessage(`{"message":"provider failed"}`),
	})
}

func TestCleanupBoundsDispatchedToolAfterAbortPart(t *testing.T) {
	testCleanupBoundsDispatchedToolAfterAbnormalPart(t, orclient.AbortPart{
		Reason: "user stopped the stream", HasReason: true,
	})
}

func testCleanupBoundsDispatchedToolAfterAbnormalPart(t *testing.T, terminal orclient.StreamPart) {
	t.Helper()
	fixedSeams(t)
	store := &memoryStore{}
	assistant := baseAssistant("msg_0001", "msg_0000", "build", "", nil).Info.(msgmodel.Assistant)
	assistant.Finish = nil
	if err := store.UpdateMessage(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseTool := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseTool()
	processor := NewProcessor(ProcessorOptions{
		Store: store, Assistant: assistant, Model: Model{},
		Tools: []ToolDefinition{
			{Provider: orclient.Tool{Type: "function", Name: "a"}},
		},
		Executor: blockingTool{release: release},
	})
	stream := &SliceStream{Parts: []orclient.StreamPart{
		orclient.ToolInputStartPart{ID: "a", ToolName: "a"},
		orclient.ToolCallPart{ToolCallID: "a", ToolName: "a", Input: `{}`},
		terminal,
	}}
	type processResult struct {
		result Result
		err    error
	}
	completed := make(chan processResult, 1)
	start := time.Now()
	go func() {
		result, err := processor.Process(context.Background(), stream)
		completed <- processResult{result: result, err: err}
	}()
	var got processResult
	select {
	case got = <-completed:
	case <-time.After(time.Second):
		releaseTool()
		<-completed
		t.Fatal("Process did not bound cleanup after an abnormal stream end")
	}
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.result != ResultStop {
		t.Fatalf("Process result = %v, want ResultStop", got.result)
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("Process took %s, want bounded abnormal cleanup", elapsed)
	}
	messages := store.rawSnapshot()
	var tool *msgmodel.ToolPart
	for _, part := range messages[0].Parts {
		if value, ok := part.(msgmodel.ToolPart); ok {
			value := value
			tool = &value
			break
		}
	}
	if tool == nil {
		t.Fatal("tool part was not persisted")
	}
	state := fixtureStringify(t, tool.State)
	for _, fragment := range []string{`"error":"Tool execution aborted"`, `"interrupted":true`} {
		if !strings.Contains(state, fragment) {
			t.Fatalf("state %s missing %s", state, fragment)
		}
	}
}

func TestCleanupBoundsUndispatchedToolRegistrationOnHealthyTurn(t *testing.T) {
	// Contract (adversarial-verify finding): a tool-input-start whose
	// tool-call part never arrives (length-truncated or errored stream) has no
	// executor and can never settle itself. On a HEALTHY turn cleanup must
	// settle it via the bounded drain — exactly TS's unconditional 250ms
	// timeout — not wait unbounded (which deadlocks the loop forever).
	fixedSeams(t)
	store := &memoryStore{}
	assistant := baseAssistant("msg_0001", "msg_0000", "build", "", nil).Info.(msgmodel.Assistant)
	assistant.Finish = nil
	if err := store.UpdateMessage(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(ProcessorOptions{
		Store: store, Assistant: assistant, Model: Model{},
		Tools: []ToolDefinition{
			{Provider: orclient.Tool{Type: "function", Name: "write"}},
		},
		Executor:    &immediateTool{},
		WaitTimeout: 40 * time.Millisecond,
	})
	stream := &SliceStream{Parts: []orclient.StreamPart{
		orclient.ToolInputStartPart{ID: "a", ToolName: "write"},
		finishPart(orclient.FinishLength),
	}}
	completed := make(chan error, 1)
	go func() {
		_, err := processor.Process(context.Background(), stream)
		completed <- err
	}()
	select {
	case err := <-completed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup hung on an undispatched tool registration (healthy ctx)")
	}
	messages := store.rawSnapshot()
	parts := messages[0].Parts
	assertPartTypes(t, parts, "step-start,tool,step-finish")
	tool := parts[1].(msgmodel.ToolPart)
	if tool.State.ToolStatus() != msgmodel.ToolStatusError {
		t.Fatalf("undispatched registration status = %s, want aborted error", tool.State.ToolStatus())
	}
	state := fixtureStringify(t, tool.State)
	for _, fragment := range []string{`"error":"Tool execution aborted"`, `"interrupted":true`} {
		if !strings.Contains(state, fragment) {
			t.Fatalf("state %s missing %s", state, fragment)
		}
	}
}

type slowTool struct {
	delay  time.Duration
	output string
}

func (e slowTool) Execute(_ context.Context, _ ToolCall) (ToolResult, error) {
	time.Sleep(e.delay)
	return ToolResult{Output: e.output, Metadata: msgmodel.RawObject("{}")}, nil
}

func TestCleanupWaitsForSlowToolOnHealthyTurnInsteadOfAborting(t *testing.T) {
	// Contract: a tool whose execution outlives the provider stream still
	// completes with its real output on a healthy (non-cancelled) turn, no
	// matter how far past WaitTimeout it runs. Spurious "Tool execution
	// aborted" errors here were live-observed (runs R/T) burning model
	// retries and leaking task-tool bookkeeping packages.
	fixedSeams(t)
	store := &memoryStore{}
	assistant := baseAssistant("msg_0001", "msg_0000", "build", "", nil).Info.(msgmodel.Assistant)
	assistant.Finish = nil
	if err := store.UpdateMessage(context.Background(), assistant); err != nil {
		t.Fatal(err)
	}
	processor := NewProcessor(ProcessorOptions{
		Store: store, Assistant: assistant, Model: Model{},
		Tools: []ToolDefinition{
			{Provider: orclient.Tool{Type: "function", Name: "a"}},
		},
		Executor:    slowTool{delay: 120 * time.Millisecond, output: "slow but real"},
		WaitTimeout: 5 * time.Millisecond,
	})
	stream := &SliceStream{Parts: []orclient.StreamPart{
		orclient.ToolInputStartPart{ID: "a", ToolName: "a"},
		orclient.ToolCallPart{ToolCallID: "a", ToolName: "a", Input: `{}`},
		finishPart(orclient.FinishToolCalls),
	}}
	if _, err := processor.Process(context.Background(), stream); err != nil {
		t.Fatal(err)
	}
	messages := store.rawSnapshot()
	parts := messages[0].Parts
	assertPartTypes(t, parts, "step-start,tool,step-finish")
	tool := parts[1].(msgmodel.ToolPart)
	if tool.State.ToolStatus() != msgmodel.ToolStatusCompleted {
		t.Fatalf("slow tool status = %s, want completed; state = %s",
			tool.State.ToolStatus(), fixtureStringify(t, tool.State))
	}
	if !strings.Contains(fixtureStringify(t, tool.State), "slow but real") {
		t.Fatalf("slow tool output missing: %s", fixtureStringify(t, tool.State))
	}
}

func TestReminderQueueCapDuplicateAndLastDrainedRules(t *testing.T) {
	queue := NewReminderQueue()
	queue.QueueReminder("s", "\u00a0")
	queue.QueueReminder("s", " one ")
	queue.QueueReminder("s", "one")
	for index := 2; index <= 10; index++ {
		queue.QueueReminder("s", "r"+jscompat.FormatNumber(float64(index)))
	}
	first := queue.DrainReminders("s")
	if len(first) != ReminderQueueCap || first[0] != "one" || first[7] != "r8" {
		t.Fatalf("first drain = %#v", first)
	}
	queue.QueueReminder("s", "r8") // last drained: dropped
	queue.QueueReminder("s", "one")
	second := queue.DrainReminders("s")
	if strings.Join(second, ",") != "one" {
		t.Fatalf("second drain = %#v", second)
	}
	if got := queue.DrainReminders("s"); got == nil || len(got) != 0 {
		t.Fatalf("empty drain must be [], got %#v", got)
	}
}

func TestPlanDBExitGuardUsesLatestFailureAndDependencyDescendants(t *testing.T) {
	db := plandb.NewPlanDB()
	db.Restore(plandb.Snapshot{
		Tasks: []*plandb.Task{
			{ID: "t-failed", Status: plandb.StatusFailed},
			{ID: "t-open", Status: plandb.StatusReady},
			{ID: "t-done", Status: plandb.StatusDone},
			{ID: "t-closed-failure", Status: plandb.StatusFailed},
			{ID: "t-cancelled", Status: plandb.StatusCancelled},
		},
		Dependencies: []plandb.Dependency{
			{FromTask: "t-failed", ToTask: "t-open", Kind: plandb.DepBlocks},
			{FromTask: "t-open", ToTask: "t-done", Kind: plandb.DepFeedsInto},
			{FromTask: "t-closed-failure", ToTask: "t-cancelled", Kind: plandb.DepBlocks},
		},
	})
	ledger := NewMemoryFailureLedger()
	ledger.RecordLeafFailure("t-root", LeafFailure{TaskID: "t-failed", Title: "old", Reason: "old"})
	ledger.RecordLeafFailure("t-root", LeafFailure{
		TaskID: "t-failed", Title: "latest", Reason: "cap", RepairHints: []string{"split"},
	})
	ledger.RecordLeafFailure("t-root", LeafFailure{TaskID: "t-closed-failure", Title: "closed"})
	guard := PlanDBExitGuard{DB: db, Ledger: ledger}
	open, err := guard.FindOpenCapExhaustFailures(context.Background(), ExitGuardQuery{RootTaskID: "t-root"})
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].TaskID != "t-failed" || open[0].Title != "latest" || open[0].BlockedDescendants != 1 {
		t.Fatalf("open = %#v", open)
	}
	guard.MarkFailureNudged("t-root", "t-failed")
	open, err = guard.FindOpenCapExhaustFailures(context.Background(), ExitGuardQuery{RootTaskID: "t-root"})
	if err != nil || len(open) != 0 {
		t.Fatalf("after nudge: %#v, %v", open, err)
	}
}
