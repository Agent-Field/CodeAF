package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// sideAskProbe is the scripted completer with a hand on the side ask: it
// answers a request that opens with the side ask's own system message by shape,
// off the queue, and keeps what that request carried — its messages and how
// many tools it offered — so a test can read what the model was shown.
type sideAskProbe struct {
	*scriptedCompleter
	mu     sync.Mutex
	answer string
	chunks []string
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
		for _, chunk := range p.chunks {
			provider.Emit(ctx, provider.StreamDelta, chunk)
		}
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
	for _, streaming := range []bool{false, true} {
		t.Run(fmt.Sprint(streaming), func(t *testing.T) { sideAskLeavesConversation(t, streaming) })
	}
}

func sideAskLeavesConversation(t *testing.T, streaming bool) {
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
		chunks: []string{proposal[:12], proposal[12:]},
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

	var answer string
	if streaming {
		var chunks []string
		answer, err = agent.SideAskStream(context.Background(), question, func(text string) { chunks = append(chunks, text) })
		if !reflect.DeepEqual(chunks, probe.chunks) {
			t.Fatalf("stream chunks = %q", chunks)
		}
	} else {
		answer, err = agent.SideAsk(context.Background(), question)
	}
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

type streamingSideAskProbe struct {
	*scriptedCompleter
	effort  provider.Effort
	fail    bool
	replace bool
}

func (p *streamingSideAskProbe) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	p.effort = provider.ReasoningEffortFrom(ctx)
	for _, chunk := range []string{`{"type":"mem`, `ber","handle":"sam"}`, "\n"} {
		provider.Emit(ctx, provider.StreamDelta, chunk)
	}
	provider.Emit(ctx, provider.StreamThinking, "private thought")
	if p.replace {
		provider.Emit(ctx, provider.StreamReplaced, "retrying")
		provider.Emit(ctx, provider.StreamDelta, "replacement answer")
	}
	if p.fail {
		return nil, errors.New("stream stopped")
	}
	return textResponse("{\"type\":\"member\",\"handle\":\"sam\"}\n"), nil
}
func TestSideAskStreamReturnsChunksWithoutChangingConversation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			probe := &streamingSideAskProbe{scriptedCompleter: &scriptedCompleter{}, fail: fail}
			agent, _ := newTestAgent(t, probe, nil)
			before := agent.Transcript()
			var chunks []string
			answer, err := agent.SideAskStream(context.Background(), "what people?", func(text string) { chunks = append(chunks, text) })
			if (err != nil) != fail {
				t.Fatalf("error = %v", err)
			}
			want := "{\"type\":\"member\",\"handle\":\"sam\"}"
			if answer != want || strings.Join(chunks, "") != want+"\n" || len(chunks) != 3 {
				t.Fatalf("answer %q, chunks %q", answer, chunks)
			}
			if !reflect.DeepEqual(before, agent.Transcript()) {
				t.Fatal("side ask changed transcript")
			}
		})
	}
}

func TestSideAskStreamKeepsDeliveredTextWhenProviderReplacesAnswer(t *testing.T) {
	probe := &streamingSideAskProbe{scriptedCompleter: &scriptedCompleter{}, replace: true}
	agent, _ := newTestAgent(t, probe, nil)
	var text strings.Builder
	answer, err := agent.SideAskStream(context.Background(), "what people?", func(chunk string) { text.WriteString(chunk) })
	if err == nil || answer != "{\"type\":\"member\",\"handle\":\"sam\"}" || text.String() != answer+"\n" {
		t.Fatalf("answer %q, chunks %q, error %v", answer, text.String(), err)
	}
}

func TestSideAskStreamDisablesReasoningOnlyForItsOwnCall(t *testing.T) {
	probe := &streamingSideAskProbe{scriptedCompleter: &scriptedCompleter{}}
	agent, _ := newTestAgent(t, probe, nil)
	ctx := provider.WithConfiguredReasoningEffort(context.Background(), provider.EffortHigh)
	if _, err := agent.SideAskStream(ctx, "what people?", func(string) {}); err != nil {
		t.Fatal(err)
	}
	if probe.effort != provider.EffortOff {
		t.Fatalf("streaming side ask effort = %q, want off", probe.effort)
	}
	if provider.ReasoningEffortFrom(ctx) != provider.EffortHigh {
		t.Fatal("side ask changed the caller's reasoning setting")
	}
	if _, err := agent.SideAsk(ctx, "what people?"); err != nil {
		t.Fatal(err)
	}
	if probe.effort != provider.EffortLow {
		t.Fatalf("ordinary side ask effort = %q, want existing low default", probe.effort)
	}
}
