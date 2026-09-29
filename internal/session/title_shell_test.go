package session

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestShellTurnsAreNotTheOpeningExchange(t *testing.T) {
	shell := []ai.Message{
		textMessage("user", "!printf 'COMBO-%s' hi"),
		{Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "user_bash_test"}}},
		textMessage("tool", "COMBO-hi"),
	}
	for _, tc := range []struct {
		name             string
		messages         []ai.Message
		question, answer string
	}{
		{name: "shell only", messages: shell},
		{name: "several shell turns", messages: append(append([]ai.Message{}, shell...), shell...)},
		{name: "waiting for ordinary answer", messages: append(append([]ai.Message{}, shell...), textMessage("user", "What did that print?"))},
		{name: "tool narration is not the answer", messages: append(append([]ai.Message{}, shell...), textMessage("user", "What did that print?"), ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: "Let me check"}}, ToolCalls: []ai.ToolCall{{ID: "model_read_test"}}})},
		{name: "ordinary follow-up", messages: append(append([]ai.Message{}, shell...), textMessage("user", "Explain the output"), textMessage("assistant", "It prints a greeting")), question: "Explain the output", answer: "It prints a greeting"},
		{name: "unanswered shell follow-up does not prevent later naming", messages: append(append([]ai.Message{}, shell...), textMessage("user", "First question"), textMessage("user", "What did that print?"), textMessage("assistant", "It printed a greeting")), question: "What did that print?", answer: "It printed a greeting"},
		{name: "ordinary exclamation", messages: []ai.Message{textMessage("user", "! means what in bash?"), textMessage("assistant", "It can negate status")}, question: "! means what in bash?", answer: "It can negate status"},
		{name: "model shell call", messages: []ai.Message{textMessage("user", "Check the project"), {Role: "assistant", ToolCalls: []ai.ToolCall{{ID: "model_bash_test"}}}, textMessage("tool", "clean"), textMessage("assistant", "The project is clean")}, question: "Check the project", answer: "The project is clean"},
		{name: "interrupted question", messages: []ai.Message{textMessage("user", "First question"), textMessage("user", "Second question"), textMessage("assistant", "Second answer")}, question: "First question"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{messages: tc.messages}
			question, answer := a.firstExchangeLocked()
			if question != tc.question || answer != tc.answer {
				t.Fatalf("exchange = %q / %q, want %q / %q", question, answer, tc.question, tc.answer)
			}
		})
	}
}

func TestShellOpeningKeepsLiteralPlaceholderUntilTheOrdinaryAnswer(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		name := "live"
		if reopen {
			name = "reopened"
		}
		t.Run(name, func(t *testing.T) {
			const command = `!printf 'MiXeD  two_spaces;\n'`
			const question = "What did that print?"
			const answer = "It printed MiXeD  two_spaces; followed by a newline."
			const title = "explaining the greeting format"
			asked := make(chan string, 1)
			var a *Agent
			c := &scriptedCompleter{steps: []step{
				func(context.Context, []ai.Message) (*ai.Response, error) {
					a.mu.Lock()
					started := a.titleTried
					a.mu.Unlock()
					if started {
						t.Error("naming started before the ordinary answer existed")
					}
					return textResponse(answer), nil
				},
			}}
			c.aside = func(messages []ai.Message) (*ai.Response, bool) {
				if !isTitleCall(messages) {
					return nil, false
				}
				asked <- messageContentText(messages[len(messages)-1])
				return textResponse(title), true
			}
			dir := filepath.Join(t.TempDir(), "0123456789abcdef")
			a, _ = newTestAgent(t, c, func(cfg *Config) {
				cfg.Place = Place{Dir: dir, Workspace: cfg.Workspace}
				cfg.SessionFile = filepath.Join(dir, "transcript.jsonl")
			})
			ch, err := a.SubmitBash(t.Context(), command)
			if err != nil {
				t.Fatal(err)
			}
			collect(t, ch)
			a.SettleWrites()
			meta, err := LoadMeta(dir)
			if err != nil || meta.Title != command || a.Title() != "" || c.asideRequests() != 0 {
				t.Fatalf("shell named itself or changed its literal preview: %+v, %v, title %q", meta, err, a.Title())
			}
			if reopen {
				if err := a.Close(); err != nil {
					t.Fatal(err)
				}
				a, err = newAgent(a.config, c)
				if err != nil {
					t.Fatal(err)
				}
				defer a.Close()
			}
			ch, err = a.Submit(t.Context(), question)
			if err != nil {
				t.Fatal(err)
			}
			collect(t, ch)
			if got := awaitTitle(t, a); got != title {
				t.Fatalf("title = %q", got)
			}
			a.titleJobs.Wait()
			request := <-asked
			if !strings.Contains(request, question) || !strings.Contains(request, answer) || strings.Contains(request, command) {
				t.Fatalf("namer did not use the ordinary question and answer: %q", request)
			}
			a.SettleWrites()
			meta, err = LoadMeta(dir)
			if err != nil || meta.Title != title {
				t.Fatalf("Home metadata = %+v, %v", meta, err)
			}
		})
	}
}
