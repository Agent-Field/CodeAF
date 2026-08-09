package observer

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/swe-pro-go/internal/bus"
	"github.com/Agent-Field/swe-pro-go/internal/engine/msgmodel"
)

type reminderRecorder struct {
	mu    sync.Mutex
	items []string
}

func (r *reminderRecorder) QueueReminder(sessionID, text string) {
	r.mu.Lock()
	r.items = append(r.items, sessionID+":"+text)
	r.mu.Unlock()
}

type historyStub struct {
	items []msgmodel.WithParts
	err   error
}

func (h historyStub) Messages(context.Context, string) ([]msgmodel.WithParts, error) {
	return h.items, h.err
}

func TestFireAppliesThresholdAndTrimsReminder(t *testing.T) {
	reminders := &reminderRecorder{}
	service := &Service{
		Reminders: reminders,
		Now:       func() time.Time { return time.UnixMilli(900_000) },
	}
	confidence := 0.9
	reminder := "  stop rereading and inspect the caller  "
	result, err := service.Fire(context.Background(), FireInput{
		Observed:  ObservedSession{SessionID: "ses", AgentRole: "auditor"},
		Trigger:   TriggerKind{Kind: "time", ElapsedMS: 900_000},
		StartedAt: 0,
		Override: func(view ObserverContext) (Verdict, error) {
			if view.ElapsedMinutes != 15 || view.RecentToolCalls == nil {
				t.Fatalf("view = %#v", view)
			}
			return Verdict{Intervene: true, Reasoning: "loop", Reminder: &reminder, Confidence: &confidence}, nil
		},
	})
	if err != nil || !result.Intervened || result.Reason != "queued" {
		t.Fatalf("Fire = %#v, %v", result, err)
	}
	reminders.mu.Lock()
	defer reminders.mu.Unlock()
	if len(reminders.items) != 1 || reminders.items[0] != "ses:stop rereading and inspect the caller" {
		t.Fatalf("reminders = %v", reminders.items)
	}
}

func TestFireBelowThresholdDoesNotQueue(t *testing.T) {
	reminders := &reminderRecorder{}
	service := &Service{Reminders: reminders}
	confidence := 0.699
	reminder := "nudge"
	result, err := service.applyVerdict(ObservedSession{SessionID: "s"}, Verdict{
		Intervene: true, Reasoning: "maybe", Reminder: &reminder, Confidence: &confidence,
	}, "test")
	if err != nil || result.Intervened || result.Reason != "confidence 0.699 < threshold 0.7" {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestRecentActivityProjectionOrderAndBounds(t *testing.T) {
	state := msgmodel.ToolStateCompleted{
		Input:    msgmodel.RawObject(`{"file_path":"a<&.go"}`),
		Output:   strings.Repeat("x", 250),
		Metadata: msgmodel.RawObject(`{}`),
		Title:    "read",
		Time:     msgmodel.ToolTimeCompleted{Start: 1, End: 2},
	}
	assistant := msgmodel.Assistant{
		MessageBase: msgmodel.MessageBase{ID: "m", SessionID: "s"},
		Time:        msgmodel.AssistantTime{Created: 1}, ParentID: "u",
		ModelID: "model", ProviderID: "provider", Mode: "coder", Agent: "coder",
	}
	service := &Service{History: historyStub{items: []msgmodel.WithParts{{
		Info: assistant,
		Parts: msgmodel.Parts{
			msgmodel.StepFinishPart{PartBase: msgmodel.PartBase{ID: "f", MessageID: "m", SessionID: "s"}},
			msgmodel.TextPart{PartBase: msgmodel.PartBase{ID: "t", MessageID: "m", SessionID: "s"}, Text: "progress"},
			msgmodel.ToolPart{PartBase: msgmodel.PartBase{ID: "p", MessageID: "m", SessionID: "s"}, Tool: "read", State: state},
		},
	}}}}
	recent := service.summarizeRecentActivity(context.Background(), "s")
	if recent.StepCount != 1 || len(recent.ToolCalls) != 1 || recent.ToolTargets[0] != "a<&.go" {
		t.Fatalf("recent = %#v", recent)
	}
	if recent.ToolCalls[0].Output[len(recent.ToolCalls[0].Output)-3:] != "..." {
		t.Fatalf("output not compacted: %q", recent.ToolCalls[0].Output)
	}
}

func TestSupervisorAndRegistryAreRaceSafe(t *testing.T) {
	reminders := &reminderRecorder{}
	service := &Service{Reminders: reminders, Now: func() time.Time { return time.UnixMilli(900_000) }}
	registry := NewRegistry(service)
	spec := ObservedSession{SessionID: "s", StartedAt: 0}
	var group sync.WaitGroup
	for i := 0; i < 20; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			registry.Track(spec)
			registry.List()
		}()
	}
	group.Wait()
	if len(registry.List()) != 1 {
		t.Fatalf("list = %d", len(registry.List()))
	}
	confidence := 0.0
	results := registry.PollOnce(context.Background(), TickOptions{
		Override: func(ObserverContext) (Verdict, error) {
			return Verdict{Reasoning: "progress", Confidence: &confidence}, nil
		},
	})
	if len(results) != 1 || !results[0].Fired {
		t.Fatalf("poll = %#v", results)
	}
	registry.Stop()
}

func TestNDJSONTapIsByteStableAndOrdered(t *testing.T) {
	var output bytes.Buffer
	source := bus.New(bus.Context{}, bus.WithIDGenerator(func() string { return "evt_1" }))
	tap := NewTap(source, &output)
	def := bus.Define("observer.test", nil)
	source.Publish(def, struct {
		SessionID string `json:"sessionID"`
		Text      string `json:"text"`
	}{"ses", "<&"})
	tap.Close()
	want := "{\"id\":\"evt_1\",\"type\":\"observer.test\",\"properties\":{\"sessionID\":\"ses\",\"text\":\"<&\"}}\n"
	if output.String() != want {
		t.Fatalf("got  %q\nwant %q", output.String(), want)
	}
	source.Publish(def, nil)
	if output.String() != want {
		t.Fatal("tap wrote after Close")
	}
}

func TestBuildObserverPromptExactFraming(t *testing.T) {
	prompt, err := BuildObserverPrompt(ObserverContext{
		AgentRole: "coder", TaskSummary: "fix", RecentToolCalls: []RecentToolCall{},
		RecentMessages: []string{}, CurrentArtifact: "n/a",
		PriorInterventions: []ContextIntervention{}, Trigger: TriggerKind{Kind: "none"},
	}, "/tmp/out.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(prompt, "# Observer evaluation\n\nRead the JSON below.") ||
		!strings.HasSuffix(prompt, "Write your verdict as JSON to: /tmp/out.json") {
		t.Fatalf("prompt = %q", prompt)
	}
}

func TestVerdictSchemaMatchesOptionalNullableConfidence(t *testing.T) {
	valid := (VerdictSchema{}).SafeParse([]byte(
		`{"intervene":true,"reasoning":"loop","reminder":"nudge","confidence":0.7,"extra":"stripped"}`,
	))
	if !valid.Success() || valid.Data.Confidence == nil || *valid.Data.Confidence != 0.7 ||
		valid.Data.Reminder == nil || *valid.Data.Reminder != "nudge" {
		t.Fatalf("valid=%#v", valid)
	}
	nullable := (VerdictSchema{}).SafeParse([]byte(
		`{"intervene":false,"reasoning":"progress","reminder":null,"confidence":null}`,
	))
	if !nullable.Success() || nullable.Data.Reminder != nil || nullable.Data.Confidence != nil {
		t.Fatalf("nullable=%#v", nullable)
	}
	for _, raw := range []string{
		`{"intervene":false,"reasoning":"x"}`,
		`{"intervene":"no","reasoning":"x","reminder":null}`,
		`{"intervene":false,"reasoning":"x","reminder":null,"confidence":1.1}`,
	} {
		if parsed := (VerdictSchema{}).SafeParse([]byte(raw)); parsed.Success() {
			t.Fatalf("invalid verdict accepted: %s", raw)
		}
	}
}
