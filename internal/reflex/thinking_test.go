package reflex

import (
	"context"
	"errors"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// observedCompleter records both option-built request fields and effort carried
// in context. A reflex owns neither; it only owns its prompt and non-streaming
// transport hint.
type observedCompleter struct {
	response *ai.Response
	requests []ai.Request
	efforts  []provider.Effort
}

func (c *observedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	request := ai.Request{Messages: messages}
	for _, option := range options {
		if err := option(&request); err != nil {
			return nil, err
		}
	}
	c.requests = append(c.requests, request)
	c.efforts = append(c.efforts, provider.ReasoningEffortFrom(ctx))
	return c.response, nil
}

func TestAReflexAddsNoOutputOrReasoningControl(t *testing.T) {
	client := &observedCompleter{response: reply(`{"keep":""}`)}
	if _, err := Notice(context.Background(), client, "what did we decide?", "nothing yet", kept, true); err != nil {
		t.Fatal(err)
	}
	if got := client.requests[0].MaxTokens; got != nil {
		t.Fatalf("reflex added max_tokens = %d", *got)
	}
	if got := client.requests[0].Temperature; got != nil {
		t.Fatalf("reflex added temperature = %v", *got)
	}
	if got := client.efforts[0]; got != provider.EffortNone {
		t.Fatalf("reflex added reasoning effort %q", got)
	}
}

func TestAReflexPreservesAnExplicitCallerEffort(t *testing.T) {
	client := &observedCompleter{response: reply(`{"keep":""}`)}
	ctx := provider.WithConfiguredReasoningEffort(context.Background(), provider.EffortHigh)
	if _, err := Notice(ctx, client, "what did we decide?", "nothing yet", kept, true); err != nil {
		t.Fatal(err)
	}
	if got := client.efforts[0]; got != provider.EffortHigh {
		t.Fatalf("explicit caller effort = %q, want high", got)
	}
}

func TestAnUnboundEmptyReflexAnswerIsNotRetried(t *testing.T) {
	silent := &fake{replies: []string{"", ""}}
	_, err := Notice(context.Background(), silent, "what did we decide?", "nothing yet", kept, true)
	if !errors.Is(err, ErrReflexFailed) {
		t.Fatalf("Notice error = %v, want %v", err, ErrReflexFailed)
	}
	if len(silent.calls) != 1 {
		t.Fatalf("an empty answer cost %d calls, want exactly one", len(silent.calls))
	}
}

func TestABoundEmptyAnswerFallsBackOnceAndSaysSoOnce(t *testing.T) {
	const primary = "test-vendor/never-answers"
	const low = "test-vendor/low"
	client := &fake{responses: []*ai.Response{
		reply(""),
		reply(`{"keep":"prefers dark themes","scope":"user"}`),
		reply(`{"keep":""}`),
	}}
	var notices []string
	bound := (&Session{}).Bind(client, primary, low, func(text string) {
		notices = append(notices, text)
	})

	result, err := Notice(context.Background(), bound, "I like dark themes", "noted", nil, true)
	if err != nil {
		t.Fatalf("Notice after fallback: %v", err)
	}
	if result.Keep != "prefers dark themes" {
		t.Fatalf("Notice returned %+v, want the low tier's answer", result)
	}
	if len(client.calls) != 2 {
		t.Fatalf("fallback used %d calls, want primary then low", len(client.calls))
	}
	for call, want := range []string{primary, low} {
		if got := client.calls[call].Model; got != want {
			t.Fatalf("call %d model = %q, want %q", call+1, got, want)
		}
		if client.calls[call].MaxTokens != nil {
			t.Fatalf("call %d added max_tokens", call+1)
		}
	}
	if len(notices) != 1 || notices[0] != "memory helper recovered using "+low+"; your model settings are unchanged" {
		t.Fatalf("notices = %q, want the one fallback line", notices)
	}

	if _, err := Notice(context.Background(), bound, "what themes should I use?", "dark ones", nil, true); err != nil {
		t.Fatalf("later Notice on fallback: %v", err)
	}
	if len(client.calls) != 3 || client.calls[2].Model != low {
		t.Fatalf("later calls = %+v, want the low tier directly", client.calls)
	}
	if len(notices) != 1 {
		t.Fatalf("fallback was announced %d times, want once", len(notices))
	}
}
