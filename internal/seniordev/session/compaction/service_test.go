//go:build !windows

package compaction

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/calc"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/msgmodel"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/overflow"
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
	// A zero tail budget keeps only the newest message verbatim, so every
	// older message lands in the summarized head and the tests can see the
	// summary path with a one-message tail.
	tail := float64(0)
	return Dependencies{
		Store: store,
		Config: ConfigProviderFunc(func(context.Context) (overflow.Config, error) {
			return overflow.Config{Compaction: &overflow.CompactionConfig{
				PreserveRecentTokens: &tail,
			}}, nil
		}),
		Agents: AgentProviderFunc(func(_ context.Context, name string) (Agent, error) {
			return Agent{Name: name}, nil
		}),
		NewID: newID, Now: now,
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
	// Two assistant messages: the newest one is always the verbatim tail, so
	// the goal and the first reply form the head that gets summarized.
	first := testAssistant("a0", "u0", textPart("a0", "Reading src/a.ts first."))
	newest := testAssistant("a1", "u0", textPart("a1", "The edit is in place."))
	parent := testUser("uc", parentPart)
	parentUser := parent.Info.(msgmodel.User)
	parentUser.Agent = agent
	parent.Info = parentUser
	return []msgmodel.WithParts{goal, first, newest, parent}
}

// promptOf returns the single user text block of a summary request, after
// proving it is the one shape the wire converter accepts.
func promptOf(t *testing.T, request SummaryRequest) string {
	t.Helper()
	if len(request.Messages) != 1 || request.Messages[0].Role != "user" {
		t.Fatalf("summary request shape = %#v", request.Messages)
	}
	parts, ok := request.Messages[0].Content.([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("summary content is not a canonical part list: %#v", request.Messages[0].Content)
	}
	text, ok := parts[0].(msgmodel.TextContent)
	if !ok {
		t.Fatalf("summary content part = %#v", parts[0])
	}
	body, err := orclient.BuildRequestBody(orclient.RequestParams{
		ModelID: "vendor/model", Prompt: request.Messages,
	})
	if err != nil {
		t.Fatalf("summary request does not build a request body: %v", err)
	}
	if !strings.Contains(string(body), `"content":"<conversation>\n`) {
		t.Fatalf("wire body lost the transcript: %s", body)
	}
	return text.Text
}

func TestProcessContinueInjectsSummaryEvidenceAutoContinueAndEvents(t *testing.T) {
	messages := compactionConversation("coder")
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
	provider := &fakeProvider{
		model: serviceModel(), provider: ProviderInfo{Source: "env", Options: "opts"},
	}
	plugin := &fakePlugin{
		compacting: CompactingResult{Context: []string{"PLUGIN CONTEXT"}},
		auto:       true,
	}
	events := &fakeEvents{}
	decisions := []CompactionDecision{}
	deps := baseDeps(store)
	deps.Provider = provider
	deps.Plugin = plugin
	deps.Events = events
	deps.Decisions = DecisionSinkFunc(func(decision CompactionDecision) {
		decisions = append(decisions, decision)
	})
	deps.Instance = InstanceContext{Directory: "/repo", Worktree: "/repo"}
	deps.Evidence = EvidenceSelectorFunc(func(_ context.Context, blocks []string) (*string, error) {
		// Evidence is harvested from the summarized head only: the goal and
		// the first reply, never the verbatim tail.
		if len(blocks) != 2 || blocks[0] != "Fix src/a.ts" || blocks[1] != "Reading src/a.ts first." {
			t.Fatalf("evidence blocks = %#v", blocks)
		}
		value := "EVIDENCE"
		return &value, nil
	})
	deps.ChangedFiles = func(context.Context) []string {
		return []string{" src/a.ts | 2 +-", "?? notes.txt"}
	}
	deps.Processors = ProcessorFactoryFunc(func(
		_ context.Context, assistant *msgmodel.Assistant, _ string, _ Model,
	) (SummaryProcessor, error) {
		return &fakeProcessor{
			message: assistant,
			process: func(ctx context.Context, request SummaryRequest) (steploop.Result, error) {
				if !plugin.transformed {
					t.Fatal("message transform did not run before processor")
				}
				prompt := promptOf(t, request)
				if !stringsContainsAll(
					prompt, SummaryTemplate, "PLUGIN CONTEXT",
					"[User]: Fix src/a.ts", "[Assistant]: Reading src/a.ts first.",
				) {
					t.Fatalf("summary prompt = %q", prompt)
				}
				if strings.Contains(prompt, "The edit is in place.") {
					t.Fatalf("the verbatim tail was summarized too: %q", prompt)
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
					Text: testValidSummary("Fix src/a.ts"),
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
		plugin.autoInput.Agent != "coder" {
		t.Fatalf("auto plugin/provider calls = %d/%d input=%#v", plugin.autoCalls, provider.providerCalls, plugin.autoInput)
	}
	if len(events.ended) != 1 ||
		!stringsContainsAll(
			events.ended[0],
			"ses_1:a1:", // the tail starts at the newest message
			testValidSummary("Fix src/a.ts"),
			"# AUTHORITATIVE TASK (verbatim — durable, not generated)",
			"Fix src/a.ts", "EVIDENCE",
			"# CHANGED FILES (computed by senior-dev at this compaction, not generated)",
			"src/a.ts | 2 +-", "?? notes.txt",
		) || len(events.published) != 1 {
		t.Fatalf("events = %#v %#v", events.ended, events.published)
	}
	if len(decisions) != 1 || decisions[0].Status != compactionStatusTarget ||
		decisions[0].After > decisions[0].Low || decisions[0].DroppedTail ||
		decisions[0].SummaryStatus != "valid" || decisions[0].SummaryError != "" {
		t.Fatalf("compaction decisions = %#v", decisions)
	}
	if d := decisions[0]; d.TranscriptMessages != 2 || d.TailMessages != 1 ||
		d.TranscriptChars == 0 || d.PromptChars <= d.TranscriptChars || d.TailTokens == 0 {
		t.Fatalf("decision sizes = %#v", d)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	tailPart := fresh[3].Parts[0].(msgmodel.CompactionPart)
	if tailPart.TailStartID == nil || *tailPart.TailStartID != "a1" {
		t.Fatalf("tail start = %#v", tailPart)
	}
	last := fresh[len(fresh)-1]
	autoUser, ok := last.Info.(msgmodel.User)
	if !ok || autoUser.Agent != "coder" || len(last.Parts) != 1 {
		t.Fatalf("auto continuation = %#v", last)
	}
	autoPart := last.Parts[0].(msgmodel.TextPart)
	if autoPart.Text != autoContinueText(false) ||
		string(autoPart.Metadata) != `{"compaction_continue":true}` ||
		autoPart.Synthetic == nil || !*autoPart.Synthetic {
		t.Fatalf("auto part = %#v", autoPart)
	}
}

func TestEnforceWatermarksRebuildsWhenFirstProjectionLacksHeadroom(t *testing.T) {
	tail := "u0"
	compactionPart := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
		Auto:     true, TailStartID: &tail,
	}
	parent := testUser("uc", compactionPart)
	finish := "stop"
	summaryFlag := true
	summary := testAssistant(
		"as", "uc",
		textPart("as", testValidSummary("Fix src/a.ts")),
		msgmodel.TextPart{
			PartBase:  msgmodel.PartBase{ID: "pin", SessionID: "ses_1", MessageID: "as"},
			Text:      BuildAuthoritativeTaskPin("Fix src/a.ts", "original user request"),
			Synthetic: boolAddress(true),
			Metadata:  msgmodel.RawObject(`{"compaction_role":"authoritative_task"}`),
		},
		msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "evidence", SessionID: "ses_1", MessageID: "as"},
			Text:     "large non-authoritative evidence", Synthetic: boolAddress(true),
			Metadata: msgmodel.RawObject(`{"compaction_role":"evidence"}`),
		},
	)
	assistant := summary.Info.(msgmodel.Assistant)
	assistant.Summary = &summaryFlag
	assistant.Finish = &finish
	summary.Info = assistant
	store := &memoryStore{messages: []msgmodel.WithParts{
		testUser("u0", textPart("u0", "Fix src/a.ts")), parent, summary,
	}}
	// Stage one only: dropping the tail brings the projection under the
	// watermark, so the VALID summary and its evidence are kept.
	sizes := []float64{58_000, 20_000}
	deps := baseDeps(store)
	deps.Sizer = ContextSizerFunc(func(
		context.Context, []msgmodel.WithParts, Model,
	) (float64, error) {
		value := sizes[0]
		sizes = sizes[1:]
		return value, nil
	})
	decision, err := NewService(deps).enforceWatermarks(
		context.Background(), "ses_1", 70_000, serviceModel(), watermarkTestConfig(),
		&compactionPart, "original user request",
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != compactionStatusRebuilt || !decision.DroppedTail || decision.StubbedSummary ||
		decision.After != 20_000 || len(sizes) != 0 {
		t.Fatalf("decision = %#v; remaining sizes = %#v", decision, sizes)
	}
	fresh, err := store.Messages(context.Background(), "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	updatedParent := fresh[1].Parts[0].(msgmodel.CompactionPart)
	if updatedParent.TailStartID != nil {
		t.Fatalf("retained tail survived the rebuild: %#v", updatedParent)
	}
	generated := generatedSummaryText(fresh[2])
	if generated == nil || !strings.Contains(*generated, "Fix src/a.ts") ||
		strings.Contains(*generated, "retained history did not fit") {
		t.Fatalf("the valid summary did not survive a tail-only rebuild: %v", generated)
	}
}

// watermarkTestConfig caps the capacity at 100K so the watermarks the
// enforcement tests reason about are high 60,000 / low 40,000.
func watermarkTestConfig() overflow.Config {
	capacity := 100_000.0
	return overflow.Config{Compaction: &overflow.CompactionConfig{CapacityTokens: &capacity}}
}

// Stage two: when the summary block alone still does not fit, it is replaced
// by the capacity stub and the evidence is tombstoned.
func TestEnforceWatermarksStubsTheSummaryOnlyWhenItAloneDoesNotFit(t *testing.T) {
	tail := "u0"
	compactionPart := msgmodel.CompactionPart{
		PartBase: msgmodel.PartBase{ID: "pc", SessionID: "ses_1", MessageID: "uc"},
		Auto:     true, TailStartID: &tail,
	}
	parent := testUser("uc", compactionPart)
	finish := "stop"
	summaryFlag := true
	summary := testAssistant(
		"as", "uc",
		textPart("as", testValidSummary("Fix src/a.ts")),
		msgmodel.TextPart{
			PartBase:  msgmodel.PartBase{ID: "pin", SessionID: "ses_1", MessageID: "as"},
			Text:      BuildAuthoritativeTaskPin("Fix src/a.ts", "original user request"),
			Synthetic: boolAddress(true),
			Metadata:  msgmodel.RawObject(`{"compaction_role":"authoritative_task"}`),
		},
		msgmodel.TextPart{
			PartBase: msgmodel.PartBase{ID: "evidence", SessionID: "ses_1", MessageID: "as"},
			Text:     "large non-authoritative evidence", Synthetic: boolAddress(true),
			Metadata: msgmodel.RawObject(`{"compaction_role":"evidence"}`),
		},
	)
	assistant := summary.Info.(msgmodel.Assistant)
	assistant.Summary = &summaryFlag
	assistant.Finish = &finish
	summary.Info = assistant
	store := &memoryStore{messages: []msgmodel.WithParts{
		testUser("u0", textPart("u0", "Fix src/a.ts")), parent, summary,
	}}
	sizes := []float64{90_000, 70_000, 20_000}
	deps := baseDeps(store)
	deps.Sizer = ContextSizerFunc(func(
		context.Context, []msgmodel.WithParts, Model,
	) (float64, error) {
		value := sizes[0]
		sizes = sizes[1:]
		return value, nil
	})
	decision, err := NewService(deps).enforceWatermarks(
		context.Background(), "ses_1", 95_000, serviceModel(), watermarkTestConfig(),
		&compactionPart, "original user request",
	)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != compactionStatusRebuilt || !decision.DroppedTail || !decision.StubbedSummary ||
		decision.After != 20_000 || len(sizes) != 0 {
		t.Fatalf("decision = %#v; remaining sizes = %#v", decision, sizes)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	generated := generatedSummaryText(fresh[2])
	if generated == nil || !strings.Contains(*generated, "retained history did not fit") {
		t.Fatalf("generated fallback = %v", generated)
	}
	evidenceRemoved := false
	for _, raw := range fresh[2].Parts {
		part, ok := raw.(msgmodel.TextPart)
		if !ok || part.ID != "evidence" {
			continue
		}
		if part.Ignored == nil || !*part.Ignored || part.Text != "" {
			t.Fatalf("evidence survived deterministic rebuild: %#v", part)
		}
		evidenceRemoved = true
	}
	if !evidenceRemoved {
		t.Fatal("deterministic rebuild did not retain an ignored evidence tombstone")
	}
}

func TestProcessReplacesInvalidSummaryBeforeActivatingBoundary(t *testing.T) {
	cases := []struct {
		name       string
		wantStatus string
		wantText   string
		parts      func(assistant *msgmodel.Assistant) msgmodel.Parts
	}{
		{
			name:       "empty",
			wantStatus: "fallback",
			parts:      func(*msgmodel.Assistant) msgmodel.Parts { return nil },
		},
		{
			name:       "malformed text",
			wantStatus: "fallback",
			parts: func(assistant *msgmodel.Assistant) msgmodel.Parts {
				return msgmodel.Parts{textPart(assistant.ID, "I will inspect src/a.ts next.")}
			},
		},
		{
			name:       "tool shaped",
			wantStatus: "fallback",
			parts: func(assistant *msgmodel.Assistant) msgmodel.Parts {
				return msgmodel.Parts{msgmodel.ToolPart{
					PartBase: msgmodel.PartBase{
						ID: "bad_tool", SessionID: "ses_1", MessageID: assistant.ID,
					},
					CallID: "call_1", Tool: "bash", State: msgmodel.PendingToolState(),
				}}
			},
		},
		{
			name:       "off-format state",
			wantStatus: "normalized",
			wantText:   "cargo build: error: could not compile",
			parts: func(assistant *msgmodel.Assistant) msgmodel.Parts {
				return msgmodel.Parts{textPart(
					assistant.ID, "## Summary of Changes\n- cargo build: error: could not compile",
				)}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			messages := compactionConversation("coder")
			store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}
			workspace := t.TempDir()
			if err := os.MkdirAll(filepath.Join(workspace, ".senior-dev"), 0o755); err != nil {
				t.Fatal(err)
			}
			const spec = "Fix src/a.ts exactly as requested.\nPreserve public behavior."
			if err := os.WriteFile(
				filepath.Join(workspace, ".senior-dev", "spec.md"), []byte(spec), 0o600,
			); err != nil {
				t.Fatal(err)
			}
			deps := baseDeps(store)
			var decisions []CompactionDecision
			deps.Decisions = DecisionSinkFunc(func(decision CompactionDecision) {
				decisions = append(decisions, decision)
			})
			deps.Provider = &fakeProvider{model: serviceModel()}
			deps.Instance = InstanceContext{Directory: workspace, Worktree: workspace}
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
						for _, part := range tc.parts(assistant) {
							if err := store.UpdatePart(ctx, part); err != nil {
								return steploop.ResultStop, err
							}
						}
						return steploop.ResultContinue, nil
					},
				}, nil
			})

			result, err := NewService(deps).Process(context.Background(), ProcessInput{
				ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: false,
			})
			if err != nil || result != steploop.ResultContinue {
				t.Fatalf("result=%s err=%v", result, err)
			}
			fresh, err := store.Messages(context.Background(), "ses_1")
			if err != nil {
				t.Fatal(err)
			}
			prior := completedCompactions(fresh)
			if len(prior) != 1 || prior[0].Summary == nil {
				t.Fatalf("completed compactions = %#v", prior)
			}
			if err := ValidateSummaryText(*prior[0].Summary); err != nil {
				t.Fatalf("fallback summary is invalid: %v\n%s", err, *prior[0].Summary)
			}
			accepted := fresh[prior[0].AssistantIndex]
			for _, part := range accepted.Parts {
				if _, ok := part.(msgmodel.ToolPart); ok {
					t.Fatalf("tool-shaped summary part survived fallback: %#v", accepted.Parts)
				}
			}
			full := summaryText(accepted)
			if len(decisions) != 1 || decisions[0].SummaryStatus != tc.wantStatus {
				t.Fatalf("compaction decisions = %#v", decisions)
			}
			if full == nil || !stringsContainsAll(*full, spec, "# AUTHORITATIVE TASK") {
				t.Fatalf("authoritative fallback = %v", full)
			}
			if tc.wantText != "" {
				if !strings.Contains(*full, tc.wantText) {
					t.Fatalf("normalized state was lost: %s", *full)
				}
			} else if !strings.Contains(*full, "no state record could be generated at this boundary") {
				t.Fatalf("deterministic record missing: %s", *full)
			}
		})
	}
}

// A summary request that itself overflows the model is one more way of having
// no generated summary: the deterministic record is installed and the run goes
// on with its tail intact.
func TestProcessSummaryOverflowInstallsDeterministicRecordAndContinues(t *testing.T) {
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
	var decisions []CompactionDecision
	deps.Decisions = DecisionSinkFunc(func(decision CompactionDecision) {
		decisions = append(decisions, decision)
	})
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
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}
	fresh, _ := store.Messages(context.Background(), "ses_1")
	prior := completedCompactions(fresh)
	if len(prior) != 1 || prior[0].Summary == nil ||
		!strings.Contains(*prior[0].Summary, "no state record could be generated") {
		t.Fatalf("completed compactions = %#v", prior)
	}
	if len(decisions) != 1 || decisions[0].SummaryStatus != "overflow" ||
		!strings.Contains(decisions[0].SummaryError, "exceeded the model context") {
		t.Fatalf("decisions = %#v", decisions)
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

// A run that loses the specification at a compaction boundary will later
// report that no task was given. The spec must therefore be pinned beside
// every summary, not only beside a rejected one.
//
// TestProcessContinueInjectsSummaryEvidenceAutoContinueAndEvents already covers
// the pin on a valid summary, and TestProcessReplacesInvalidSummaryBefore-
// ActivatingBoundary covers .senior-dev/spec.md as its source. Neither covers the
// combination that actually occurs in a long run: a summary that VALIDATES, a
// spec on disk, and an original request that is no longer recoverable from
// the message list. Without this, moving the pin into the invalid-summary
// branch would leave every test green and silently restore the defect.
func TestProcessPinsSpecOnValidSummaryWhenTheRequestIsUnrecoverable(t *testing.T) {
	messages := compactionConversation("coder")
	// The surviving user message says something the spec does not, standing in
	// for a first message compaction has already rewritten past recognition.
	// If the pin ever sources from here instead of the file, the assertions
	// below say so by name rather than by a missing substring.
	stale := "Fix src/a.ts"
	store := &memoryStore{messages: append([]msgmodel.WithParts(nil), messages...)}

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".senior-dev"), 0o755); err != nil {
		t.Fatal(err)
	}
	const spec = "Emit a sorted manifest of every record, ordered by source path.\nInclude derived records in the output."
	if err := os.WriteFile(
		filepath.Join(workspace, ".senior-dev", "spec.md"), []byte(spec), 0o600,
	); err != nil {
		t.Fatal(err)
	}

	deps := baseDeps(store)
	deps.Provider = &fakeProvider{model: serviceModel()}
	deps.Instance = InstanceContext{Directory: workspace, Worktree: workspace}
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
				// A summary that passes ValidateSummary, so the fallback path
				// is not what installs the pin.
				if err := store.UpdatePart(
					ctx, textPart(assistant.ID, testValidSummary("continue the refactor")),
				); err != nil {
					return steploop.ResultStop, err
				}
				return steploop.ResultContinue, nil
			},
		}, nil
	})

	result, err := NewService(deps).Process(context.Background(), ProcessInput{
		ParentID: "uc", Messages: messages, SessionID: "ses_1", Auto: false,
	})
	if err != nil || result != steploop.ResultContinue {
		t.Fatalf("result=%s err=%v", result, err)
	}

	fresh, err := store.Messages(context.Background(), "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	prior := completedCompactions(fresh)
	if len(prior) != 1 || prior[0].Summary == nil {
		t.Fatalf("completed compactions = %#v", prior)
	}
	// The generated summary was accepted on its own merits: if this run had
	// gone down the fallback path the pin would prove nothing about the path
	// a real run takes.
	if strings.Contains(*prior[0].Summary, "generated summary failed validation") {
		t.Fatalf("valid summary was replaced by the fallback:\n%s", *prior[0].Summary)
	}
	if !strings.Contains(*prior[0].Summary, "continue the refactor") {
		t.Fatalf("generated summary was not preserved:\n%s", *prior[0].Summary)
	}

	accepted := fresh[prior[0].AssistantIndex]
	var pin *msgmodel.TextPart
	for _, raw := range accepted.Parts {
		part, ok := raw.(msgmodel.TextPart)
		if !ok || !strings.Contains(string(part.Metadata), `"authoritative_task"`) {
			continue
		}
		pin = &part
	}
	if pin == nil {
		t.Fatal("no authoritative-task pin was installed beside a VALID summary")
	}
	if !stringsContainsAll(
		pin.Text, spec, "# AUTHORITATIVE TASK", "Source: .senior-dev/spec.md",
	) {
		t.Fatalf("pin did not carry the spec verbatim from disk:\n%s", pin.Text)
	}
	if strings.Contains(pin.Text, stale) {
		t.Fatalf("pin sourced the stale user message instead of the spec:\n%s", pin.Text)
	}
	if pin.Synthetic == nil || !*pin.Synthetic {
		t.Fatalf("pin must be synthetic so it is not mistaken for the summary: %#v", pin)
	}
}
