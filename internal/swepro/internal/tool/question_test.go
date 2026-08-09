package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/bus"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/question"
)

type questionPublication struct {
	definition bus.Definition
	properties any
}

type questionPublisher struct {
	events chan questionPublication
}

func (p *questionPublisher) Publish(definition bus.Definition, properties any, _ ...bus.PublishOptions) {
	p.events <- questionPublication{definition: definition, properties: properties}
}

func questionDefinition(t *testing.T, registry *Registry) steploop.ToolDefinition {
	t.Helper()
	for _, item := range registry.Definitions() {
		if item.Provider.Name == "question" {
			return item
		}
	}
	t.Fatal("question definition not registered")
	return steploop.ToolDefinition{}
}

func newQuestionRegistry(t *testing.T, service *question.Service) *Registry {
	t.Helper()
	t.Setenv("CODEAF_ENABLE_QUESTION_TOOL", "0")
	return NewWithOptions(t.TempDir(), RegistryOptions{
		ClientIdentity: "cli",
		Question:       service,
	})
}

func TestQuestionRegistrationDescriptionSchemaAndFilters(t *testing.T) {
	registry := newQuestionRegistry(t, question.NewService(nil, nil))
	definition := questionDefinition(t, registry)
	if !definition.WaitForResult {
		t.Fatal("question definition does not keep the provider stream open")
	}
	if definition.Provider.Description != questionDescription {
		t.Fatal("registered question description does not use embedded asset")
	}
	if pinned, err := os.ReadFile("/home/abir/swe-migration/swe-pro/src/tool/question.txt"); err == nil {
		if definition.Provider.Description != string(pinned) {
			t.Fatal("question description differs from frozen TypeScript bytes")
		}
	}

	valid := json.RawMessage(`{"questions":[{"question":"Continue?","header":"Confirm","options":[{"label":"Yes","description":"Continue","ignored":1}],"multiple":true,"custom":false}],"ignored":true}`)
	if err := definition.Validate(valid); err != nil {
		t.Fatalf("valid parameters rejected: %v", err)
	}
	for _, invalid := range []json.RawMessage{
		json.RawMessage(`{"questions":[{"header":"Missing question","options":[]}]}`),
		json.RawMessage(`{"questions":[{"question":"Continue?","header":"Confirm","options":null}]}`),
	} {
		if err := definition.Validate(invalid); err == nil {
			t.Fatalf("invalid parameters accepted: %s", invalid)
		}
	}

	coder := definitionNames(registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter", ModelID: "anthropic/claude-opus-4-6", AgentName: "coder",
	}))
	if want := []string{"question", "bash", "read", "glob", "grep", "edit", "write", "webfetch"}; !reflect.DeepEqual(coder, want) {
		t.Fatalf("coder definitions = %v, want %v", coder, want)
	}
	reviewer := definitionNames(registry.DefinitionsFor(FilterInput{
		ProviderID: "openrouter", ModelID: "anthropic/claude-opus-4-6", AgentName: "review-prover",
	}))
	if !containsName(reviewer, "question") || containsName(reviewer, "bash") {
		t.Fatalf("review definitions = %v", reviewer)
	}
}

func TestQuestionForceFlagEnablesNonClientRegistry(t *testing.T) {
	t.Setenv("CODEAF_ENABLE_QUESTION_TOOL", "1")
	registry := NewWithOptions(t.TempDir(), RegistryOptions{ClientIdentity: "server"})
	_ = questionDefinition(t, registry)
}

func TestHeadlessQuestionWaitsUntilRunCancellation(t *testing.T) {
	publisher := &questionPublisher{events: make(chan questionPublication, 1)}
	service := question.NewService(publisher, func() (question.QuestionID, error) {
		return "que_headless", nil
	})
	registry := newQuestionRegistry(t, service)
	ctx, cancel := context.WithCancel(context.Background())
	type execution struct {
		result steploop.ToolResult
		err    error
	}
	done := make(chan execution, 1)
	go func() {
		result, err := registry.Execute(ctx, steploop.ToolCall{
			ID: "call_1", Name: "question", SessionID: "ses_1", MessageID: "msg_1",
			Input: json.RawMessage(`{"questions":[{"question":"Continue?","header":"Confirm","options":[]}]}`),
		})
		done <- execution{result: result, err: err}
	}()

	published := <-publisher.events
	request, ok := published.properties.(question.Request)
	if published.definition.Type != question.Event.Asked.Type || !ok || request.ID != "que_headless" ||
		request.Tool == nil || request.Tool.MessageID != "msg_1" || request.Tool.CallID != "call_1" {
		t.Fatalf("asked publication = %#v", published)
	}
	select {
	case completed := <-done:
		t.Fatalf("headless question completed without an answer: %#v", completed)
	default:
	}
	if pending := service.List(); len(pending) != 1 || pending[0].ID != "que_headless" {
		t.Fatalf("pending questions = %#v", pending)
	}

	cancel()
	completed := <-done
	if !errors.Is(completed.err, context.Canceled) {
		t.Fatalf("question cancellation error = %v", completed.err)
	}
	if pending := service.List(); len(pending) != 0 {
		t.Fatalf("pending after cancellation = %#v", pending)
	}
}

func TestQuestionReplyFormatsToolResultLikeTypeScript(t *testing.T) {
	publisher := &questionPublisher{events: make(chan questionPublication, 1)}
	service := question.NewService(publisher, func() (question.QuestionID, error) {
		return "que_reply", nil
	})
	registry := newQuestionRegistry(t, service)
	done := make(chan struct {
		result steploop.ToolResult
		err    error
	}, 1)
	go func() {
		result, err := registry.Execute(context.Background(), steploop.ToolCall{
			ID: "call_1", Name: "question", SessionID: "ses_1", MessageID: "msg_1",
			Input: json.RawMessage(`{"questions":[{"question":"Color?","header":"Color","options":[{"label":"Blue","description":"Use blue"}]},{"question":"Size?","header":"Size","options":[]}]}`),
		})
		done <- struct {
			result steploop.ToolResult
			err    error
		}{result: result, err: err}
	}()
	published := <-publisher.events
	request := published.properties.(question.Request)
	service.Reply(question.ReplyInput{
		RequestID: request.ID,
		Answers:   []question.Answer{{"Blue"}, {}},
	})

	completed := <-done
	if completed.err != nil {
		t.Fatal(completed.err)
	}
	if completed.result.Title != "Asked 2 questions" {
		t.Fatalf("title = %q", completed.result.Title)
	}
	wantOutput := `User has answered your questions: "Color?"="Blue", "Size?"="Unanswered". You can now continue with the user's answers in mind.`
	if completed.result.Output != wantOutput {
		t.Fatalf("output = %q, want %q", completed.result.Output, wantOutput)
	}
	if string(completed.result.Metadata) != `{"answers":[["Blue"],[]]}` {
		t.Fatalf("metadata = %s", completed.result.Metadata)
	}
}

func TestQuestionRejectionsBecomeTerminalResultAfterThree(t *testing.T) {
	instanceBus := bus.New(bus.Context{})
	service := question.NewService(instanceBus, nil)
	instanceBus.SubscribeCallback(question.Event.Asked, func(payload bus.Payload) {
		service.Reject(payload.Properties.(question.Request).ID)
	})
	registry := newQuestionRegistry(t, service)
	call := steploop.ToolCall{
		Name: "question", SessionID: "ses-bounded",
		Input: json.RawMessage(`{"questions":[{"question":"Continue?","header":"Confirm","options":[]}]}`),
	}
	for attempt := 1; attempt <= 3; attempt++ {
		result, err := registry.Execute(context.Background(), call)
		if attempt < 3 {
			var rejected *question.RejectedError
			if !errors.As(err, &rejected) {
				t.Fatalf("attempt %d = (%#v, %v), want rejection", attempt, result, err)
			}
			continue
		}
		if err != nil || result.Title != "Questions unavailable" ||
			!strings.Contains(result.Output, "answers are final") ||
			!strings.Contains(result.Output, "best judgment") {
			t.Fatalf("terminal attempt = (%#v, %v)", result, err)
		}
	}
}

func TestQuestionRejectionCounterResetsAfterSuccessfulTool(t *testing.T) {
	instanceBus := bus.New(bus.Context{})
	service := question.NewService(instanceBus, nil)
	instanceBus.SubscribeCallback(question.Event.Asked, func(payload bus.Payload) {
		service.Reject(payload.Properties.(question.Request).ID)
	})
	registry := newQuestionRegistry(t, service)
	questionCall := steploop.ToolCall{
		Name: "question", SessionID: "ses-reset",
		Input: json.RawMessage(`{"questions":[{"question":"Continue?","header":"Confirm","options":[]}]}`),
	}
	for range 2 {
		if _, err := registry.Execute(context.Background(), questionCall); err == nil {
			t.Fatal("question unexpectedly succeeded before reset")
		}
	}
	resetFile := filepath.Join(registry.workDir, "reset-marker.txt")
	if err := os.WriteFile(resetFile, []byte("marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	readInput, err := json.Marshal(map[string]any{"filePath": resetFile})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Execute(context.Background(), steploop.ToolCall{
		Name: "read", SessionID: "ses-reset", Input: readInput,
	}); err != nil {
		t.Fatalf("interleaved read: %v", err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if result, err := registry.Execute(context.Background(), questionCall); err == nil {
			t.Fatalf("post-reset attempt %d unexpectedly terminal: %#v", attempt, result)
		}
	}
	result, err := registry.Execute(context.Background(), questionCall)
	if err != nil || result.Title != "Questions unavailable" {
		t.Fatalf("post-reset third attempt = (%#v, %v)", result, err)
	}
}

func TestAnsweredQuestionsNeverHitRejectionBound(t *testing.T) {
	instanceBus := bus.New(bus.Context{})
	service := question.NewService(instanceBus, nil)
	instanceBus.SubscribeCallback(question.Event.Asked, func(payload bus.Payload) {
		request := payload.Properties.(question.Request)
		service.Reply(question.ReplyInput{RequestID: request.ID, Answers: []question.Answer{{"Yes"}}})
	})
	registry := newQuestionRegistry(t, service)
	call := steploop.ToolCall{
		Name: "question", SessionID: "ses-embedded",
		Input: json.RawMessage(`{"questions":[{"question":"Continue?","header":"Confirm","options":[]}]}`),
	}
	for attempt := 1; attempt <= 5; attempt++ {
		result, err := registry.Execute(context.Background(), call)
		if err != nil || result.Title != "Asked 1 question" || strings.Contains(result.Output, "unavailable") {
			t.Fatalf("answered attempt %d = (%#v, %v)", attempt, result, err)
		}
	}
}
