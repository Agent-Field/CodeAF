package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// isCaptionCall reports whether this request is the narrator's, by the one thing
// only it sends: its own system line. The instruction itself rides at the END of
// the user message, where a small model reads it (caption.go).
//
// It is the door a fixture uses to ANSWER the narrator. The narrator is an
// errand, so it never rides the scripted queue (agent_test.go); a test that
// wants to see its request or hand it a line installs it as an aside.
func isCaptionCall(messages []ai.Message) bool {
	if len(messages) == 0 || messages[0].Role != "system" {
		return false
	}
	return messageContentText(messages[0]) == captionSystem
}

// answerTheNarrator hands every narrator call one line and counts the asks.
func answerTheNarrator(client *scriptedCompleter, line string) func() int {
	asked := 0
	client.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isCaptionCall(messages) {
			return nil, false
		}
		asked++
		return textResponse(line), true
	}
	return func() int {
		client.mu.Lock()
		defer client.mu.Unlock()
		return asked
	}
}

func TestTheNarratorDoesNotFireOnABatchThatFinishesFast(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	hub := newEventHub()
	defer hub.close()

	results := agent.runToolsWarm(context.Background(), agent.newEpisode(),
		[]ai.ToolCall{fixBash("true")}, hub, nil)
	if len(results) != 1 || results[0].isError {
		t.Fatalf("the instant batch did not finish cleanly: %+v", results)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for _, event := range hub.backlog {
		if event.Kind == EventCaption {
			t.Fatalf("a fast batch grew a narrator caption: %q", event.Text)
		}
	}
}

func TestTheNarratorStopsAfterThreeCallsInOneTurn(t *testing.T) {
	client := &scriptedCompleter{}
	asked := answerTheNarrator(client, "checking the silence")
	agent, _ := newTestAgent(t, client, nil)
	hub := newEventHub()
	defer hub.close()
	ep := agent.newEpisode()
	ctx := withEpisode(context.Background(), ep)
	calls := []ai.ToolCall{fixBash("sleep 30")}

	for range captionCalls + 1 {
		agent.maybeCaption(ctx, hub, calls, []string{`{"command":"sleep 30"}`})
	}

	if got := asked(); got != captionCalls {
		t.Fatalf("narrator calls = %d, want the per-turn cap %d", got, captionCalls)
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if got := countKind(hub.backlog, EventCaption); got != captionCalls {
		t.Fatalf("caption events = %d, want %d", got, captionCalls)
	}
}

func TestANarratorAnswerThatIsTheInstructionIsRefused(t *testing.T) {
	for _, echoed := range []string{
		captionPrompt,
		"What is this work trying to find out?",
		"Sure: " + captionPrompt,
	} {
		if got := cleanCaption(echoed); got != "" {
			t.Errorf("cleanCaption(%q) = %q, want the instruction refused", echoed, got)
		}
	}
	if got := cleanCaption("Caption: checking where the fold is minted"); got != "checking where the fold is minted" {
		t.Fatalf("an ordinary caption cleaned to %q", got)
	}
	got := cleanCaption("Good leads. Fetching the key pages to confirm which are open.")
	if got != "Fetching the key pages to confirm which are open" {
		t.Fatalf("cleanCaption did not keep one short sentence: %q", got)
	}
	long := "fetching the key pages to confirm which are actually still open tonight in toronto"
	if got := cleanCaption(long); got != "fetching the key pages to confirm" {
		t.Fatalf("cleanCaption left a mid-clause cut: %q", got)
	}
	if strings.Contains(cleanCaption(long), "…") {
		t.Fatal("cleanCaption appended an ellipsis")
	}
}

func TestCleanCaptionKeepsTokenPunctuationAndSentenceBoundaries(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "file extension",
			line: "Reading livesteps.go now.",
			want: "Reading livesteps.go now",
		},
		{
			name: "dotted token in a sentence",
			line: "I will update config.json and then run the suite.",
			want: "I will update config.json and then run the suite",
		},
		{
			name: "version number",
			line: "Bumping the pin to v1.2.3 across the three services.",
			want: "Bumping the pin to v1.2.3 across the three services",
		},
		{
			name: "dot inside a path without a sentence end",
			line: "searching ~/.aforge/v3/projects for the transcript",
			want: "searching ~/.aforge/v3/projects for the transcript",
		},
		{
			name: "real sentence boundary",
			line: "Good leads. Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
		{
			name: "newline boundary",
			line: "Reading livesteps.go now\nThen checking the renderer.",
			want: "Reading livesteps.go now",
		},
		{
			name: "word limit and dangling words",
			line: "fetching the key pages to confirm which are actually still open tonight in toronto",
			want: "fetching the key pages to confirm",
		},
		{
			name: "question mark inside a token",
			line: "fetching https://api.example.com/v1?limit=10 for the list",
			want: "fetching https://api.example.com/v1?limit=10 for the list",
		},
		{
			name: "exclamation mark inside a token",
			line: "checking cache!primary before reading the fallback",
			want: "checking cache!primary before reading the fallback",
		},
		{
			name: "question mark at a sentence end",
			line: "Ready? Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
		{
			name: "terminator run at a sentence end",
			line: "Done!! Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
		{
			name: "closing punctuation before a sentence end",
			line: "Good (“confirmed!”). Fetching the key pages to confirm which are open.",
			want: "Fetching the key pages to confirm which are open",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanCaption(tt.line)
			if got != tt.want {
				t.Fatalf("cleanCaption(%q) = %q, want %q", tt.line, got, tt.want)
			}
			if strings.Contains(got, "…") {
				t.Fatalf("cleanCaption(%q) appended an ellipsis: %q", tt.line, got)
			}
		})
	}
}

func TestTheNarratorInstructionComesLast(t *testing.T) {
	client := &scriptedCompleter{}
	seen := 0
	client.aside = func(messages []ai.Message) (*ai.Response, bool) {
		if !isCaptionCall(messages) {
			return nil, false
		}
		seen++
		user := messageContentText(messages[1])
		if !strings.HasSuffix(user, captionPrompt) {
			t.Errorf("instruction is not last:\n%s", user)
		}
		return textResponse("checking the fold"), true
	}
	agent, _ := newTestAgent(t, client, nil)
	agent.maybeCaption(withEpisode(context.Background(), agent.newEpisode()),
		newEventHub(), []ai.ToolCall{fixBash("true")}, []string{`{"command":"true"}`})
	if seen != 1 {
		t.Fatalf("the narrator asked %d times, want one", seen)
	}
}
