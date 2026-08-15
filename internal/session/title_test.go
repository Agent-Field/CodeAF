package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// titleTurn is a session's first turn: one answer, then the namer's reply.
func titleTurn(answer, title string) []step {
	return []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(answer), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse(title), nil
		},
	}
}

func titleAgent(t *testing.T, completer Completer, mutate func(*Config)) (*Agent, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SessionFile = path
		if mutate != nil {
			mutate(config)
		}
	})
	return agent, path
}

// The session names itself once, writes the name to its journal, says so on
// the turn's stream, and answers Title() with it.
func TestTitleLandsInTheFileAndOnTheStream(t *testing.T) {
	completer := &scriptedCompleter{steps: titleTurn("the parser is fine", "  \"Tokenizer Bug Hunt.\"  ")}
	agent, path := titleAgent(t, completer, nil)

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	changed, ok := firstOfKind(events, EventTitleChanged)
	if !ok {
		t.Fatalf("no EventTitleChanged; got %v", kinds(events))
	}
	// The quotes, the trailing stop and the surrounding space are the three
	// things a model adds against the instruction.
	if changed.Text != "Tokenizer Bug Hunt" {
		t.Fatalf("title = %q, want it cleaned up", changed.Text)
	}
	if got := agent.Title(); got != "Tokenizer Bug Hunt" {
		t.Fatalf("Title() = %q", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	titles := 0
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			titles++
			if !strings.Contains(line, `"title":"Tokenizer Bug Hunt"`) {
				t.Fatalf("title line = %s", line)
			}
		}
	}
	if titles != 1 {
		t.Fatalf("%d title lines, want exactly 1", titles)
	}
}

// One name per session: the second turn does not pay for a second one, and a
// resumed session keeps the name it already has.
func TestTheSessionIsNamedOnlyOnce(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("tokenizer speed"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("second"), nil },
	}}
	agent, path := titleAgent(t, completer, nil)

	first := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	second := collect(t, mustSubmit(t, agent, "and the parser?"))

	if got := countKind(first, EventTitleChanged); got != 1 {
		t.Fatalf("first turn fired %d title events, want 1", got)
	}
	if got := countKind(second, EventTitleChanged); got != 0 {
		t.Fatalf("the second turn named the session again (%d events)", got)
	}
	if completer.requests() != 3 {
		t.Fatalf("requests = %d, want 3 (two turns and one namer)", completer.requests())
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A resume reads the name off the file rather than paying for it again.
	resumed, err := newAgent(Config{
		Workspace: agent.config.Workspace, Model: "test/model", System: "SYSTEM", SessionFile: path,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	t.Cleanup(func() { _ = resumed.Close() })
	if got := resumed.Title(); got != "tokenizer speed" {
		t.Fatalf("resumed Title() = %q", got)
	}
	collect(t, mustSubmit(t, resumed, "carry on"))
	if got := resumed.Title(); got != "tokenizer speed" {
		t.Fatalf("the resumed session renamed itself to %q", got)
	}
}

// The namer rides internal/roles: a pin beats the tier, the tier beats the
// session model, and the turn itself is untouched by either.
func TestTitleModelFollowsTheRolesLadder(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		settings map[string]string
		want     string
	}{
		{"nothing set", nil, "test/model"},
		{"the tier", map[string]string{roles.TierKey(roles.TierLow): "cheap/model"}, "cheap/model"},
		{"the pin beats the tier", map[string]string{
			roles.TierKey(roles.TierLow):       "cheap/model",
			roles.PinKey(roles.RoleTitle):      "pinned/model",
			roles.PinKey(roles.RoleCompaction): "other/model",
		}, "pinned/model"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			completer := &scriptedCompleter{steps: titleTurn("answered", "a name")}
			settings := testCase.settings
			agent, _ := titleAgent(t, completer, func(config *Config) {
				config.RolesSource = func(key string) (string, bool) {
					value, ok := settings[key]
					return value, ok
				}
			})
			collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

			if got := completer.model(0); got != "test/model" {
				t.Fatalf("the turn rode %q, want the session model", got)
			}
			if got := completer.model(1); got != testCase.want {
				t.Fatalf("the namer rode %q, want %q", got, testCase.want)
			}
		})
	}
}

// A namer that fails leaves the session unnamed and the turn untouched. It is
// bookkeeping: nothing the person asked for went wrong.
func TestAFailedTitleNeverBreaksTheTurn(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("the answer"), nil },
		func(context.Context, []ai.Message) (*ai.Response, error) { return nil, errors.New("namer is down") },
	}}
	agent, path := titleAgent(t, completer, nil)

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	if last := events[len(events)-1]; last.Kind != EventTurnDone && last.Kind != EventTitleChanged {
		t.Fatalf("turn ended with %v, want it to end normally", last.Kind)
	}
	if _, failed := firstOfKind(events, EventError); failed {
		t.Fatal("a failed namer was reported as a failed turn")
	}
	if got := agent.Title(); got != "" {
		t.Fatalf("Title() = %q, want the session to stay unnamed", got)
	}
	if got := messageText(lastMessage(agent)); got != "the answer" {
		t.Fatalf("transcript tail = %q, want the turn's own answer", got)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			t.Fatalf("a failed namer wrote a title line: %s", line)
		}
	}
}

// A session with no journal has nowhere to keep a name and nothing to be
// listed in, so it does not pay for one.
func TestAnInMemorySessionDoesNotNameItself(t *testing.T) {
	completer := &scriptedCompleter{steps: titleTurn("the answer", "a name")}
	agent, _ := newTestAgent(t, completer, nil)

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))

	if completer.requests() != 1 {
		t.Fatalf("requests = %d, want 1 — an in-memory session paid for a name", completer.requests())
	}
	if countKind(events, EventTitleChanged) != 0 {
		t.Fatal("an in-memory session announced a name")
	}
}
