package session

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// sideAskProbe is the scripted completer with a hand on the side ask: it
// answers a request that opens with the side ask's own system message by shape,
// off the queue, and keeps what that request carried — its messages and how
// many tools it offered — so a test can read what the model was shown.
type sideAskProbe struct {
	*scriptedCompleter
	mu     sync.Mutex
	answer string
	asked  [][]ai.Message
	tools  []int
}

func (p *sideAskProbe) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	if len(messages) > 0 && messageText(messages[0]) == sideAskSystem {
		var request ai.Request
		for _, option := range options {
			_ = option(&request)
		}
		p.mu.Lock()
		p.asked = append(p.asked, append([]ai.Message(nil), messages...))
		p.tools = append(p.tools, len(request.Tools))
		answer := p.answer
		p.mu.Unlock()
		return textResponse(answer), nil
	}
	return p.scriptedCompleter.CompleteWithMessages(ctx, messages, options...)
}

// A SIDE ASK IS NEVER A TURN. The model is shown the conversation as text and
// the question after it, with no belt to call; its answer comes back to the
// caller; and the conversation is exactly what it was — the same transcript,
// the same journal, no line of the question or the answer in either — so the
// next turn the person starts is sent without them.
func TestASideAskLeavesTheConversationAsItWas(t *testing.T) {
	const proposal = `{"name": "flaky", "goal": "fix the flaky tests", "helpers": [{"handle": "ci", "role": "ci watcher", "brief": "watch the runs"}]}`
	const question = "Propose a crew for the work in this conversation. Reply with one JSON object."
	probe := &sideAskProbe{
		scriptedCompleter: &scriptedCompleter{steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("Three tests fail on CI; the clock test is the flaky one."), nil
			},
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("Carrying on with the clock test."), nil
			},
		}},
		answer: proposal,
	}
	journal := filepath.Join(t.TempDir(), "transcript.jsonl")
	agent, _ := newTestAgent(t, probe, func(c *Config) { c.SessionFile = journal })

	events, err := agent.Submit(context.Background(), "find out why CI is red")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	before := agent.Transcript()
	agent.mu.Lock()
	held := len(agent.messages)
	agent.mu.Unlock()
	written, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}

	answer, err := agent.SideAsk(context.Background(), question)
	if err != nil || answer != proposal {
		t.Fatalf("the side ask answered %q, %v", answer, err)
	}

	probe.mu.Lock()
	asked, tools := probe.asked, probe.tools
	probe.mu.Unlock()
	if len(asked) != 1 || len(asked[0]) != 2 || tools[0] != 0 {
		t.Fatalf("the side ask sent %d requests, the first with %d messages and %v tools", len(asked), len(asked[0]), tools)
	}
	shown := messageText(asked[0][1])
	for _, want := range []string{"PERSON: find out why CI is red", "ASSISTANT: Three tests fail on CI", "The app's question:\n\n" + question} {
		if !strings.Contains(shown, want) {
			t.Fatalf("the model was not shown %q in:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "SYSTEM") {
		t.Fatalf("the conversation's own system prompt was read as conversation:\n%s", shown)
	}

	if after := agent.Transcript(); !reflect.DeepEqual(before, after) {
		t.Fatalf("the side ask moved the transcript:\nbefore %+v\nafter  %+v", before, after)
	}
	agent.mu.Lock()
	still := len(agent.messages)
	agent.mu.Unlock()
	if still != held {
		t.Fatalf("the side ask left %d messages where the turn left %d", still, held)
	}
	now, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(now), "Propose a crew") || strings.Contains(string(now), `\"helpers\"`) {
		t.Fatalf("the side ask's words reached the journal:\n%s", now[len(written):])
	}
	for _, entry := range ReadTranscript(journal).Entries {
		if strings.Contains(entry.Text, "Propose a crew") || strings.Contains(entry.Text, `"helpers"`) {
			t.Fatalf("the journal reads back a side ask's words as an entry: %+v", entry)
		}
	}

	events, err = agent.Submit(context.Background(), "go on")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	for i := 0; i < probe.requests(); i++ {
		for _, message := range probe.request(i) {
			if text := messageText(message); strings.Contains(text, "Propose a crew") || strings.Contains(text, `"helpers"`) {
				t.Fatalf("a turn after the side ask was sent its words: %q", text)
			}
		}
	}
}

// A SIDE ASK READS THE NEWEST OF A LONG CONVERSATION and says that the start is
// not shown; an empty conversation is said to be empty; a compacted one opens
// on its summary; and there is no side ask without a question.
func TestASideAskIsBoundedAndSaysWhatItLeftOut(t *testing.T) {
	var long []ai.Message
	long = append(long, textMessage("system", "SYSTEM"))
	long = append(long, textMessage("user", "the very first thing said"))
	for i := 0; i < 400; i++ {
		long = append(long, textMessage("assistant", strings.Repeat("x", 400)))
	}
	long = append(long, textMessage("user", "the newest thing said"))
	got := sideAskPrompt(long, "what now?")
	if len(got) > sideAskBytes+1024 {
		t.Fatalf("a long conversation sent %d bytes", len(got))
	}
	if !strings.Contains(got, "the newest thing said") || strings.Contains(got, "the very first thing said") || !strings.Contains(got, sideAskElided) {
		t.Fatalf("the prompt did not keep the newest end and say it cut the start:\n%s", got[:200])
	}
	if !strings.HasSuffix(got, "The app's question:\n\nwhat now?") {
		t.Fatalf("the question is not last: %q", got[len(got)-80:])
	}

	if got := sideAskPrompt([]ai.Message{textMessage("system", "SYSTEM")}, "what now?"); !strings.Contains(got, sideAskNothing) {
		t.Fatalf("an empty conversation reads %q", got)
	}

	compacted := []ai.Message{
		textMessage("system", "SYSTEM"),
		textMessage("user", summaryNote("the person wants CI green", "see the journal")),
		textMessage("user", "and now?"),
	}
	if got := sideAskPrompt(compacted, "what now?"); !strings.Contains(got, "EARLIER, SUMMARIZED: the person wants CI green") {
		t.Fatalf("a compacted conversation lost its summary:\n%s", got)
	}

	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := agent.SideAsk(context.Background(), "  "); err == nil {
		t.Fatal("a side ask with no question was asked")
	}
}
