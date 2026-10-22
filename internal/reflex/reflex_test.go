package reflex

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// ── the fake ────────────────────────────────────────────────────────────────

// fake is a Completer that answers from a script and keeps every request it was
// handed. One entry per call: the second entry is what the repair retry gets.
type fake struct {
	replies   []string
	responses []*ai.Response
	err       error
	calls     []ai.Request
}

func (f *fake) CompleteWithMessages(_ context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := ai.Request{Messages: messages}
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	f.calls = append(f.calls, request)
	if f.err != nil {
		return nil, f.err
	}
	index := len(f.calls) - 1
	if index < len(f.responses) {
		return f.responses[index], nil
	}
	if index >= len(f.replies) {
		return nil, errors.New("fake: the script ran out of replies")
	}
	return reply(f.replies[index]), nil
}

func reply(text string) *ai.Response {
	return &ai.Response{Choices: []ai.Choice{{
		Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: text}}},
	}}}
}

// prompt is the text of one request, every message run together, which is what
// a test asking "was the index in there" actually wants to search.
// userMessage is the variable half of one reflex request — what this call was
// shown, as distinct from the standing instructions in front of it.
func userMessage(request ai.Request) string {
	var builder strings.Builder
	for _, message := range request.Messages {
		if message.Role != "user" {
			continue
		}
		for _, part := range message.Content {
			builder.WriteString(part.Text)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func prompt(request ai.Request) string {
	var builder strings.Builder
	for _, message := range request.Messages {
		for _, part := range message.Content {
			builder.WriteString(part.Text)
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

// ── notice ──────────────────────────────────────────────────────────────────

var kept = []string{"deploys on Fridays", "prefers dark themes"}

// A REPLY IS A REPLY WHATEVER IT IS WRAPPED IN. A small model fences its JSON,
// prefaces it, or apologises after it, and every one of those is the right
// answer with something around it.
func TestNoticeReadsTheAnswerThroughWhateverTheModelWrappedItIn(t *testing.T) {
	for _, wrapped := range []string{
		`{"keep":"uses tabs","scope":"user","replaces":0}`,
		"```json\n{\"keep\":\"uses tabs\",\"scope\":\"user\",\"replaces\":0}\n```",
		"Here you go: {\"keep\":\"uses tabs\",\"scope\":\"user\",\"replaces\":0} hope that helps",
	} {
		got, err := Notice(context.Background(), &fake{replies: []string{wrapped}}, "I use tabs everywhere", "noted", kept, true)
		if err != nil {
			t.Fatalf("Notice(%q): %v", wrapped, err)
		}
		if got.Keep != "uses tabs" || got.Scope != "user" {
			t.Fatalf("Notice(%q) = %+v, want the line and its scope", wrapped, got)
		}
	}
}

func TestNoticeRepairsOneUnreadableAnswerAndThenGivesUp(t *testing.T) {
	healed := &fake{replies: []string{"I think nothing", `{"keep":""}`}}
	if _, err := Notice(context.Background(), healed, "hi", "hello", nil, true); err != nil {
		t.Fatalf("one repair should heal: %v", err)
	}
	if len(healed.calls) != 2 || !strings.Contains(prompt(healed.calls[1]), repairInstruction) {
		t.Fatalf("the repair was not asked: %d calls", len(healed.calls))
	}
	broken := &fake{replies: []string{"nope", "still nope"}}
	if _, err := Notice(context.Background(), broken, "hi", "hello", nil, true); !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("two unreadable answers err = %v, want ErrReflexFailed", err)
	}
	if len(broken.calls) != 2 {
		t.Fatalf("a hopeless answer cost %d calls, want two", len(broken.calls))
	}
	failing := &fake{err: errors.New("provider down")}
	if _, err := Notice(context.Background(), failing, "hi", "hello", nil, true); !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("a provider failure err = %v, want ErrReflexFailed", err)
	}
}

func TestNoticeDoesNotCallTheModelForAnEmptyExchange(t *testing.T) {
	client := &fake{}
	got, err := Notice(context.Background(), client, "  ", "", kept, true)
	if err != nil || got.Keep != "" || len(client.calls) != 0 {
		t.Fatalf("empty exchange = %+v %v after %d calls, want nothing and no call", got, err, len(client.calls))
	}
}

// NOT ASKED IS NOT KEPT. With automatic saving off the call is a state-card
// reading, and a model that answers a line anyway has the line dropped.
func TestNoticeKeepsNothingWhenItWasNotAskedAbout(t *testing.T) {
	client := &fake{replies: []string{`{"keep":"uses tabs","scope":"user","state":{"goal":"tidy the build"}}`}}
	got, err := Notice(context.Background(), client, "I use tabs", "noted", kept, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Keep != "" {
		t.Fatalf("not asked about memory, kept %q", got.Keep)
	}
	if got.State == nil || got.State.Goal != "tidy the build" {
		t.Fatalf("state = %+v, want the card's delta read either way", got.State)
	}
	if !strings.Contains(userMessage(client.calls[0]), "KEPT:") || strings.Contains(prompt(client.calls[0]), "FIRST, keep") {
		t.Fatal("a call not saving memory lost use feedback or was asked to save")
	}
}

func TestNoticeDropsAReplacementOutsideTheListAndDefaultsTheScope(t *testing.T) {
	client := &fake{replies: []string{`{"keep":"deploys on Tuesdays","scope":"somewhere","replaces":7}`}}
	got, err := Notice(context.Background(), client, "we deploy on Tuesdays now", "ok", kept, true)
	if err != nil {
		t.Fatal(err)
	}
	if got.Replaces != 0 || got.Scope != "project" {
		t.Fatalf("got %+v, want no replacement and the project scope", got)
	}
	inRange := &fake{replies: []string{`{"keep":"deploys on Tuesdays","scope":"project","replaces":1}`}}
	got, _ = Notice(context.Background(), inRange, "we deploy on Tuesdays now", "ok", kept, true)
	if got.Replaces != 1 {
		t.Fatalf("replaces = %d, want line 1", got.Replaces)
	}
}

func TestTheKeptLinesAreShownNumberedAndAnEmptyListSaysSo(t *testing.T) {
	client := &fake{replies: []string{`{"keep":""}`, `{"keep":""}`}}
	_, _ = Notice(context.Background(), client, "hi", "hello", kept, true)
	input := userMessage(client.calls[0])
	if !strings.Contains(input, "1. deploys on Fridays") || !strings.Contains(input, "2. prefers dark themes") {
		t.Fatalf("the kept lines were not numbered in:\n%s", input)
	}
	_, _ = Notice(context.Background(), client, "hi", "hello", nil, true)
	if !strings.Contains(userMessage(client.calls[1]), "(nothing yet)") {
		t.Fatal("an empty kept list reads as a blank a small model would fill in")
	}
}

func TestBothSidesOfTheExchangeAreClippedOnTheirOwn(t *testing.T) {
	client := &fake{replies: []string{`{"keep":""}`}}
	long := strings.Repeat("a", messageLimit*2)
	_, _ = Notice(context.Background(), client, "short question", long, nil, true)
	input := userMessage(client.calls[0])
	if !strings.Contains(input, "short question") {
		t.Fatal("a long answer pushed the person's own words out")
	}
	if strings.Count(input, "a") > messageLimit+200 {
		t.Fatal("the answer was not clipped")
	}
}

func TestEveryCallLeavesGenerationToTheProvider(t *testing.T) {
	client := &fake{replies: []string{`{"keep":""}`}}
	_, _ = Notice(context.Background(), client, "hi", "hello", nil, true)
	if request := client.calls[0]; request.MaxTokens != nil || request.Temperature != nil {
		t.Fatalf("a reflex call set generation controls: %+v", request)
	}
}

// ── the model the calls run on ──────────────────────────────────────────────

// THE TIER IS THE POINT. A reflex call takes the reflex tier's model, not the
// low tier's, and a person's pin still outranks both.
func TestModelWalksTheReflexRolesLadder(t *testing.T) {
	src := roles.Source(func(key string) (string, bool) {
		value, ok := map[string]string{
			"tiers.reflex": "vendor/tiny",
			"tiers.low":    "vendor/cheap",
			"tiers.high":   "vendor/careful",
		}[key]
		return value, ok
	})
	model, err := Model(src, "vendor/conversation")
	if err != nil {
		t.Fatalf("Model: %v", err)
	}
	if model != "vendor/tiny" {
		t.Fatalf("Model = %q, want the reflex tier's own model", model)
	}
	if model, err = Model(nil, "vendor/conversation"); err != nil || model != "vendor/conversation" {
		t.Fatalf("Model with nothing set = %q, %v", model, err)
	}
}

func TestBindSendsEveryReflexCallToOneModel(t *testing.T) {
	client := &fake{replies: []string{`{"keep":""}`}}
	if _, err := Notice(context.Background(), Bind(client, "vendor/tiny"), "hello", "hi", nil, true); err != nil {
		t.Fatalf("Notice: %v", err)
	}
	if got := client.calls[0].Model; got != "vendor/tiny" {
		t.Fatalf("a bound call named model %q", got)
	}
	if _, ok := Bind(client, "  ").(bound); ok {
		t.Fatal("Bind(\"\") wrapped the client in a pin to nowhere")
	}
}

// ── the prompts ─────────────────────────────────────────────────────────────

// The prompt names the scope words the parser accepts, because it is rendered
// from the parser's own list, and it says the time rule that tells a memory
// from an automation.
func TestTheNoticePromptSpellsTheScopesAndTheTimeRule(t *testing.T) {
	for _, scope := range memoryScopes {
		if !strings.Contains(noticePrompt, scope) {
			t.Fatalf("the notice prompt never names the scope %q", scope)
		}
	}
	if !strings.Contains(noticePrompt, "TIMELESS") || !strings.Contains(noticePrompt, "remind me at six") {
		t.Fatal("the notice prompt lost the rule that anything with a time component is not a memory")
	}
}
