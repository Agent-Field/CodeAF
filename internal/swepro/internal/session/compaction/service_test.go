package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/engine/calc"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
	"github.com/Agent-Field/swe-pro-go/internal/engine/steploop"
	"github.com/Agent-Field/swe-pro-go/internal/session/overflow"
)

type memoryStore struct {
	mu       sync.Mutex
	messages []msgmodel.WithParts
	updates  []string
	err      error
}

func (s *memoryStore) Messages(_ context.Context, _ string) ([]msgmodel.WithParts, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, s.err
	}
	return append([]msgmodel.WithParts(nil), s.messages...), nil
}

func (s *memoryStore) UpdateMessage(_ context.Context, info msgmodel.Info) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].Info.MessageID() == info.MessageID() {
			s.messages[i].Info = info
			s.updates = append(s.updates, "message:"+info.MessageID())
			return nil
		}
	}
	s.messages = append(s.messages, msgmodel.WithParts{Info: info, Parts: msgmodel.Parts{}})
	s.updates = append(s.updates, "message:"+info.MessageID())
	return nil
}

func (s *memoryStore) UpdatePart(_ context.Context, part msgmodel.Part) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	base := part.Base()
	for messageIndex := range s.messages {
		if s.messages[messageIndex].Info.MessageID() != base.MessageID {
			continue
		}
		for partIndex, existing := range s.messages[messageIndex].Parts {
			if existing.Base().ID == base.ID {
				s.messages[messageIndex].Parts[partIndex] = part
				s.updates = append(s.updates, "part:"+base.ID)
				return nil
			}
		}
		s.messages[messageIndex].Parts = append(s.messages[messageIndex].Parts, part)
		s.updates = append(s.updates, "part:"+base.ID)
		return nil
	}
	return fmt.Errorf("message not found for part %s", base.ID)
}

func (s *memoryStore) find(id string) *msgmodel.WithParts {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.messages {
		if s.messages[i].Info.MessageID() == id {
			value := s.messages[i]
			return &value
		}
	}
	return nil
}

type fakeProvider struct {
	model         Model
	provider      ProviderInfo
	providerCalls int
}

func (p *fakeProvider) GetModel(_ context.Context, _, _ string) (Model, error) {
	return p.model, nil
}

func (p *fakeProvider) GetProvider(_ context.Context, _ string) (ProviderInfo, error) {
	p.providerCalls++
	return p.provider, nil
}

type fakePlugin struct {
	compacting  CompactingResult
	transformed bool
	auto        bool
	autoCalls   int
	autoInput   AutoContinueInput
}

func (p *fakePlugin) Compacting(context.Context, string) (CompactingResult, error) {
	return p.compacting, nil
}

func (p *fakePlugin) TransformMessages(_ context.Context, _ []msgmodel.WithParts) error {
	p.transformed = true
	return nil
}

func (p *fakePlugin) AutoContinue(_ context.Context, input AutoContinueInput) (bool, error) {
	p.autoCalls++
	p.autoInput = input
	return p.auto, nil
}

type fakeProcessor struct {
	message *msgmodel.Assistant
	process func(context.Context, SummaryRequest) (steploop.Result, error)
}

func (p *fakeProcessor) Process(
	ctx context.Context, request SummaryRequest,
) (steploop.Result, error) {
	return p.process(ctx, request)
}

func (p *fakeProcessor) Message() msgmodel.Assistant { return *p.message }

type fakeEvents struct {
	started   []string
	ended     []string
	published []string
}

func (e *fakeEvents) CompactionStarted(sessionID string, _ uint64, reason string) {
	e.started = append(e.started, sessionID+":"+reason)
}

func (e *fakeEvents) CompactionEnded(
	sessionID string, _ uint64, text string, include *string,
) {
	tail := ""
	if include != nil {
		tail = *include
	}
	e.ended = append(e.ended, sessionID+":"+tail+":"+text)
}

func (e *fakeEvents) PublishCompacted(_ context.Context, sessionID string) error {
	e.published = append(e.published, sessionID)
	return nil
}

func serviceModel() Model {
	return Model{
		Message: msgmodel.Model{
			ProviderID: "openrouter", ID: "vendor/model",
			API: msgmodel.ModelAPI{
				Npm: "@openrouter/ai-sdk-provider", ID: "vendor/model",
			},
		},
		Overflow: overflow.Model{
			Limit: calc.ModelLimit{Context: 131_072, Output: 8_192},
		},
	}
}

func deterministicRuntime() (func(string) string, func() uint64) {
	id := 0
	now := uint64(1000)
	return func(prefix string) string {
			id++
			return fmt.Sprintf("%s_%d", prefix, id)
		}, func() uint64 {
			now++
			return now
		}
}

func baseDeps(store *memoryStore) Dependencies {
	newID, now := deterministicRuntime()
	tail := float64(0)
	return Dependencies{
		Store: store,
		Config: ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return overflow.Config{Compaction: &overflow.CompactionConfig{
				TailTurns: &tail,
			}}, nil
		}),
		Agents: AgentProviderFunc(func(_ context.Context, name string) (Agent, error) {
			return Agent{Name: name}, nil
		}),
		NewID: newID, Now: now,
		Env: func(string) (string, bool) { return "", false },
	}
}

func compactionConversation(agent string) []msgmodel.WithParts {
	parentPart := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{
			ID: "pc", SessionID: "ses_1", MessageID: "uc",
		},
		Auto: true,
	}
	goal := testUser("u0", textPart("u0", "Fix src/a.ts"))
	parent := testUser("uc", parentPart)
	parentUser := parent.Info.(msgmodel.User)
	parentUser.Agent = agent
	parent.Info = parentUser
	return []msgmodel.WithParts{goal, parent}
}

func TestProcessContinueInjectsSummaryEvidenceAutoContinueAndEvents(t *testing.T) {
	messages := compactionConversation("auditor")
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	provider := &fakeProvider{
		model: serviceModel(), provider: ProviderInfo{Source: "env", Options: "opts"},
	}
	plugin := &fakePlugin{
		compacting: CompactingResult{Context: []string{"PLUGIN CONTEXT"}},
		auto:       true,
	}
	events := &fakeEvents{}
	deps := baseDeps(store)
	deps.Provider = provider
	deps.Plugin = plugin
	deps.Events = events
	deps.Instance = InstanceContext{Directory: "/repo", Worktree: "/repo"}
	deps.Evidence = EvidenceSelectorFunc(func(_ context.Context, blocks []string) (*string, error) {
		if len(blocks) != 1 || blocks[0] != "Fix src/a.ts" {
			t.Fatalf("evidence blocks = %#v", blocks)
		}
		value := "EVIDENCE"
		return &value, nil
	})
	deps.Processors = ProcessorFactoryFunc(func(
		_ context.Context, assistant *msgmodel.Assistant, _ string, _ Model,
	) (SummaryProcessor, error) {
		return &fakeProcessor{
			message: assistant,
			process: func(ctx context.Context, request SummaryRequest) (steploop.Result, error) {
				if !plugin.transformed {
					t.Fatal("message transform did not run before processor")
				}
				last := request.Messages[len(request.Messages)-1]
				content := last.Content.([]msgmodel.TextContent)
				prompt := content[0].Text
				if !stringsContainsAll(
					prompt, SummaryTemplate, "PLUGIN CONTEXT",
					"Rewrite /repo/.codeaf/auditor-verdict.json",
				) {
					t.Fatalf("summary prompt = %q", prompt)
				}
				finish := "stop"
				assistant.Finish = &finish
				if err := store.UpdateMessage(ctx, *assistant); err != nil {
					return steploop.ResultStop, err
				}
				if err := store.UpdatePart(ctx, msgmodel.TextPart{
					PartBase: msgmodel.PartBase{
						ID: "summary_part", SessionID: "ses_1", MessageID: assistant.ID,
					},
					Text: "summary body",
				}); err != nil {
					return steploop.ResultStop, err
				}
				return steploop.ResultContinue, nil
			},
		}, nil
	})
	service := NewService(deps)
	result, err := service.Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: true,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if plugin.autoCalls != 1 || provider.providerCalls != 1 ||
		plugin.autoInput.Agent != "auditor" {
		t.Fatalf("auto plugin/provider calls = %d/%d input=%#v", plugin.autoCalls, provider.providerCalls, plugin.autoInput)
	}
	if len(events.ended) != 1 ||
		events.ended[0] != "ses_1::summary body\n\nEVIDENCE" ||
		len(events.published) != 1 {
		t.Fatalf("events = %#v %#v", events.ended, events.published)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	last := fresh[len(fresh)-1]
	autoUser, ok := last.Info.(msgmodel.User)
	if !ok || autoUser.Agent != "auditor" || len(last.Parts) != 1 {
		t.Fatalf("auto continuation = %#v", last)
	}
	autoPart := last.Parts[0].(msgmodel.TextPart)
	if autoPart.Text != autoContinueText(false) ||
		string(autoPart.Metadata) != `{"compaction_continue":true}` ||
		autoPart.Synthetic == nil || !*autoPart.Synthetic {
		t.Fatalf("auto part = %#v", autoPart)
	}
}

func TestProcessCompactWritesExactContextError(t *testing.T) {
	imageName := "large.png"
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "old")),
		testAssistant("a0", "u0", textPart("a0", "reply")),
		testUser("u1", msgmodel.FilePart{
			PartBase: msgmodel.PartBase{ID: "img", SessionID: "ses_1", MessageID: "u1"},
			Mime:     "image/png", Filename: &imageName, URL: "data:image/png;base64,AA",
		}),
	}
	parentPart := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
		Auto:     true,
	}
	parent := testUser("uc", parentPart)
	messages = append(messages, parent)
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	deps := baseDeps(store)
	deps.Provider = &fakeProvider{model: serviceModel()}
	deps.Instance = InstanceContext{Directory: "/repo", Worktree: "/repo"}
	deps.Processors = ProcessorFactoryFunc(func(
		_ context.Context, assistant *msgmodel.Assistant, _ string, _ Model,
	) (SummaryProcessor, error) {
		return &fakeProcessor{
			message: assistant,
			process: func(context.Context, SummaryRequest) (steploop.Result, error) {
				return steploop.ResultCompact, nil
			},
		}, nil
	})
	overflowed := true
	result, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1",
		Auto: true, Overflow: &overflowed,
	})
	if err != nil || result != steploop.ResultStop {
		t.Fatalf("result=%s err=%v", result, err)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	var summary msgmodel.Assistant
	for _, message := range fresh {
		if value, ok := message.Info.(msgmodel.Assistant); ok && boolPointer(value.Summary) {
			summary = value
		}
	}
	if summary.Error == nil || summary.Error.Name != msgmodel.ErrNameContextOverflow ||
		summary.Finish == nil || *summary.Finish != "error" {
		t.Fatalf("summary error = %#v", summary)
	}
	var data msgmodel.ContextOverflowErrorData
	if err := json.Unmarshal(summary.Error.Data, &data); err != nil {
		t.Fatal(err)
	}
	want := "Conversation history too large to compact - exceeds model context limit"
	if data.Message != want {
		t.Fatalf("error message = %q", data.Message)
	}
}

func TestProcessOverflowReplayReplacesMediaAndSkipsAutoContinue(t *testing.T) {
	name := "big.pdf"
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "old")),
		testAssistant("a0", "u0", textPart("a0", "reply")),
		testUser("u1",
			textPart("u1", "inspect"),
			msgmodel.FilePart{
				PartBase: msgmodel.PartBase{ID: "pdf", SessionID: "ses_1", MessageID: "u1"},
				Mime:     "application/pdf", Filename: &name, URL: "data:application/pdf;base64,AA",
			},
		),
	}
	parentPart := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
		Auto:     true,
	}
	messages = append(messages, testUser("uc", parentPart))
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	plugin := &fakePlugin{auto: true}
	deps := baseDeps(store)
	deps.Provider = &fakeProvider{model: serviceModel()}
	deps.Plugin = plugin
	deps.Instance = InstanceContext{Directory: "/repo", Worktree: "/repo"}
	deps.Processors = ProcessorFactoryFunc(func(
		_ context.Context, assistant *msgmodel.Assistant, _ string, _ Model,
	) (SummaryProcessor, error) {
		return &fakeProcessor{
			message: assistant,
			process: func(ctx context.Context, _ SummaryRequest) (steploop.Result, error) {
				finish := "stop"
				assistant.Finish = &finish
				if err := store.UpdateMessage(ctx, *assistant); err != nil {
					return steploop.ResultStop, err
				}
				return steploop.ResultContinue, nil
			},
		}, nil
	})
	overflowed := true
	result, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1",
		Auto: true, Overflow: &overflowed,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	if plugin.autoCalls != 0 {
		t.Fatalf("autocontinue plugin called for replay: %d", plugin.autoCalls)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	last := fresh[len(fresh)-1]
	if _, ok := last.Info.(msgmodel.User); !ok || len(last.Parts) != 2 {
		t.Fatalf("replay = %#v", last)
	}
	if got := last.Parts[1].(msgmodel.TextPart).Text; got != "[Attached application/pdf: big.pdf]" {
		t.Fatalf("media placeholder = %q", got)
	}
}

func TestPruneThresholdProtectedToolAndConfigDisable(t *testing.T) {
	big := strings.Repeat("x", 260_000) // 65k estimated tokens
	bash := completedToolPart("bash_part", "a0", "bash", big)
	skill := completedToolPart("skill_part", "a0", "skill", big)
	messages := []msgmodel.WithParts{
		testUser("u0", textPart("u0", "old")),
		testAssistant("a0", "u0", bash, skill),
		testUser("u1", textPart("u1", "next")),
		testAssistant("a1", "u1", textPart("a1", "reply")),
		testUser("u2", textPart("u2", "latest")),
	}
	store := &memoryStore{messages: messages}
	deps := baseDeps(store)
	service := NewService(deps)
	if err := service.Prune(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
	old := store.find("a0")
	bashState := old.Parts[0].(msgmodel.ToolPart).State.(msgmodel.ToolStateCompleted)
	skillState := old.Parts[1].(msgmodel.ToolPart).State.(msgmodel.ToolStateCompleted)
	if bashState.Time.Compacted == nil || skillState.Time.Compacted != nil {
		t.Fatalf("prune states bash=%#v skill=%#v", bashState.Time, skillState.Time)
	}

	disabled := false
	deps.Config = ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
		return overflow.Config{Compaction: &overflow.CompactionConfig{Prune: &disabled}}, nil
	})
	store.updates = nil
	if err := NewService(deps).Prune(context.Background(), "ses_1"); err != nil {
		t.Fatal(err)
	}
	if len(store.updates) != 0 {
		t.Fatalf("disabled prune updates = %#v", store.updates)
	}
}

func TestPruneNotFoundDegradesOnlyThatError(t *testing.T) {
	store := &memoryStore{err: fmt.Errorf("%w: missing", msgmodel.ErrNotFound)}
	deps := baseDeps(store)
	if err := NewService(deps).Prune(context.Background(), "missing"); err != nil {
		t.Fatalf("not-found prune = %v", err)
	}
	store.err = errors.New("database failed")
	if err := NewService(deps).Prune(context.Background(), "ses"); err == nil ||
		err.Error() != "database failed" {
		t.Fatalf("other error = %v", err)
	}
}

func TestCreatePersistsCompactionAndStartedEvent(t *testing.T) {
	store := &memoryStore{}
	events := &fakeEvents{}
	deps := baseDeps(store)
	deps.Events = events
	overflowed := true
	err := NewService(deps).Create(context.Background(), CreateInput{
		SessionID: "ses_1", Agent: "coder",
		Model: ModelRef{ProviderID: "openrouter", ModelID: "m"},
		Auto:  true, Overflow: &overflowed,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(store.messages) != 1 || len(store.messages[0].Parts) != 1 {
		t.Fatalf("created state = %#v", store.messages)
	}
	part := store.messages[0].Parts[0].(msgmodel.CompactionPart)
	if !part.Auto || part.Overflow == nil || !*part.Overflow {
		t.Fatalf("part = %#v", part)
	}
	if len(events.started) != 1 || events.started[0] != "ses_1:auto" {
		t.Fatalf("started events = %#v", events.started)
	}
}

func completedToolPart(id, messageID, tool, output string) msgmodel.ToolPart {
	return msgmodel.ToolPart{
		PartBase: msgmodel.PartBase{ID: id, SessionID: "ses_1", MessageID: messageID},
		CallID:   "call_" + id, Tool: tool,
		State: msgmodel.ToolStateCompleted{
			Input: msgmodel.RawObject("{}"), Output: output, Title: tool,
			Metadata: msgmodel.RawObject("{}"),
			Time:     msgmodel.ToolTimeCompleted{Start: 1, End: 2},
		},
	}
}
