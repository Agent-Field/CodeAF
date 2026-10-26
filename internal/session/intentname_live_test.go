package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestLiveIntentNamesSixConversations(t *testing.T) {
	if os.Getenv("CODEAF_LIVE_INTENT_NAMES") != "1" {
		t.Skip("set CODEAF_LIVE_INTENT_NAMES=1 to use the real naming model")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		t.Fatal("OPENROUTER_API_KEY is required")
	}
	const model = "deepseek/deepseek-v4.1-flash"
	var names []string
	for _, words := range []string{
		"Help me plan Mira's retirement party for twenty people at the lake",
		"Compare train and ferry options for a weekend in Victoria",
		"Review the wording of our community garden volunteer invitation",
		"Explain why my sourdough starter smells like apples",
		"hi",
		"look at this",
	} {
		dir := t.TempDir()
		agent, err := New(Config{
			Workspace: dir, SessionFile: filepath.Join(dir, "conversation.jsonl"),
			APIKey: key, BaseURL: "https://openrouter.ai/api/v1", Model: model,
			System:      "Answer the person briefly in plain words. For a vague greeting, offer help planning a neighborhood picnic. Do not use tools.",
			IntentNames: true, IntentSiblingNames: append([]string(nil), names...),
			RolesSource: func(string) (string, bool) { return model, true },
		})
		if err != nil {
			t.Fatal(err)
		}
		began := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		events, err := agent.SubmitMessage(ctx, Submission{Text: words})
		if err != nil {
			cancel()
			_ = agent.Close()
			t.Fatal(err)
		}
		var heard []string
		var reply strings.Builder
		for event := range events {
			if event.Kind == EventTitleChanged {
				heard = append(heard, event.Text)
			}
			if event.Kind == EventTextDelta {
				reply.WriteString(event.Text)
			}
		}
		agent.waitForTitle()
		elapsed := time.Since(began).Round(time.Millisecond)
		title := agent.Title()
		cancel()
		if err := agent.Close(); err != nil {
			t.Fatal(err)
		}
		if title == "" || utf8.RuneCountInString(title) > 24 || title == OpeningName(words, nil) {
			t.Fatalf("model name = %q", title)
		}
		for _, previous := range names {
			if title == previous {
				t.Fatalf("duplicate name %q", title)
			}
		}
		names = append(names, title)
		t.Logf("opener=%q name=%q elapsed=%s title_events=%q reply_chars=%d", words, title, elapsed, heard, utf8.RuneCountInString(reply.String()))
	}
}
