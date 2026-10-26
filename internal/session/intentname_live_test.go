package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

type liveIntentCall struct {
	Model    string       `json:"model"`
	Messages []ai.Message `json:"messages"`
	Answer   string       `json:"answer"`
	Error    string       `json:"error,omitempty"`
}

type liveIntentRecorder struct {
	Completer
	mu     sync.Mutex
	calls  []liveIntentCall
	secret string
}

func (r *liveIntentRecorder) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var request ai.Request
	for _, option := range options {
		_ = option(&request)
	}
	response, err := r.Completer.CompleteWithMessages(ctx, messages, options...)
	call := liveIntentCall{Model: request.Model, Messages: messages}
	if response != nil {
		call.Answer = response.Text()
	}
	if err != nil {
		call.Error = strings.ReplaceAll(err.Error(), r.secret, "<REDACTED>")
	}
	r.mu.Lock()
	r.calls = append(r.calls, call)
	r.mu.Unlock()
	return response, err
}

func TestLiveIntentNamesNineConversations(t *testing.T) {
	if os.Getenv("CODEAF_LIVE_INTENT_NAMES") != "1" {
		t.Skip("set CODEAF_LIVE_INTENT_NAMES=1 to use the real naming model")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	const model = "deepseek/deepseek-v4.1-flash"
	var names []string
	for _, exchange := range [][]string{
		{"Help me plan Mira's retirement party for twenty people at the lake"},
		{"Compare train and ferry options for a weekend in Victoria"},
		{"Review the wording of our community garden volunteer invitation"},
		{"Explain why my sourdough starter smells like apples"},
		{"hi"},
		{"look at this"},
		{"can you help"},
		{"continue"},
		{"hi", "my sourdough smells like apples"},
	} {
		words := exchange[0]
		dir := t.TempDir()
		client, err := provider.NewClient(provider.Config{APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: model})
		if err != nil {
			t.Fatal(err)
		}
		recorder := &liveIntentRecorder{Completer: client, secret: key}
		agent, err := New(Config{
			Workspace: dir, SessionFile: filepath.Join(dir, "conversation.jsonl"),
			APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: model,
			System:      "Answer the person briefly in plain words. Do not invent a topic for a vague message. Ask what they need. Do not use tools.",
			completer:   recorder,
			IntentNames: true, IntentSiblingNames: append([]string(nil), names...),
			RolesSource: func(string) (string, bool) { return model, true },
		})
		if err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		var heard []string
		var reply strings.Builder
		for _, message := range exchange {
			events, err := agent.SubmitMessage(ctx, Submission{Text: message})
			if err != nil {
				cancel()
				_ = agent.Close()
				t.Fatal(err)
			}
			for event := range events {
				if event.Kind == EventTitleChanged {
					heard = append(heard, event.Text)
				}
				if event.Kind == EventTextDelta {
					reply.WriteString(event.Text)
				}
			}
			agent.waitForTitle()
		}
		elapsed := time.Since(began).Round(time.Millisecond)
		title := agent.Title()
		cancel()
		if err := agent.Close(); err != nil {
			t.Fatal(err)
		}
		recorder.mu.Lock()
		calls := append([]liveIntentCall(nil), recorder.calls...)
		recorder.mu.Unlock()
		evidence, err := json.MarshalIndent(struct {
			Opener  string           `json:"opener"`
			Name    string           `json:"name"`
			Latency string           `json:"latency"`
			Calls   []liveIntentCall `json:"calls"`
		}{strings.Join(exchange, " → "), title, elapsed.String(), calls}, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if out := os.Getenv("CODEAF_LIVE_INTENT_EVIDENCE"); out != "" {
			if err := os.MkdirAll(out, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("%02d.json", len(names)+1)), evidence, 0600); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("evidence=%s", evidence)
		for _, call := range calls {
			if call.Model != model {
				t.Fatalf("unexpected model %q", call.Model)
			}
			if call.Error != "" {
				t.Logf("failed request messages=%v", call.Messages)
				t.Fatal(call.Error)
			}
		}
		if title == "" || utf8.RuneCountInString(title) > 40 || (title != OpeningName(words, nil) && (utf8.RuneCountInString(title) > 24 || strings.HasSuffix(title, "…"))) {
			t.Fatalf("model name = %q", title)
		}
		if len(exchange) == 1 {
			switch words {
			case "hi", "look at this", "can you help", "continue":
				if title != words {
					t.Fatalf("vague opener changed from %q to %q", words, title)
				}
			}
		}
		if len(exchange) > 1 && title == OpeningName(words, nil) {
			t.Fatalf("concrete follow-up kept placeholder %q", title)
		}
		for _, previous := range names {
			if title == previous {
				t.Fatalf("duplicate name %q", title)
			}
		}
		names = append(names, title)
		t.Logf("opener=%q name=%q elapsed=%s title_events=%q reply_chars=%d", strings.Join(exchange, " → "), title, elapsed, heard, utf8.RuneCountInString(reply.String()))
	}
}
