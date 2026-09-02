package session

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func waitTitleJob(t *testing.T, agent *Agent) {
	t.Helper()
	agent.mu.Lock()
	done := agent.titleDone
	agent.mu.Unlock()
	if done == nil {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("title job did not settle")
	}
}

func waitTitleChanged(t *testing.T, lane <-chan Event) Event {
	t.Helper()
	select {
	case event, open := <-lane:
		if !open {
			t.Fatal("title lane closed before the title arrived")
		}
		if event.Kind != EventTitleChanged {
			t.Fatalf("standing lane carried %v, want EventTitleChanged", event.Kind)
		}
		return event
	case <-time.After(5 * time.Second):
		t.Fatal("no EventTitleChanged on the standing lane")
		return Event{}
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
	lane, stop := agent.WatchTaskUpdates()
	defer stop()

	events := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	if _, onTurn := firstOfKind(events, EventTitleChanged); onTurn {
		t.Fatal("the asynchronous title kept riding the completed turn")
	}
	changed := waitTitleChanged(t, lane)
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

// A surface may open its lifetime lane after the first turn has closed and the
// naming errand has already landed. The title is session state, so subscribing
// late replays it instead of depending on having watched the original moment.
func TestATitleIsReplayedToALateLifetimeSubscriber(t *testing.T) {
	agent, _ := titleAgent(t, &scriptedCompleter{}, nil)
	agent.setTitle("parser session")
	lane, stop := agent.WatchTaskUpdates()
	defer stop()
	if changed := waitTitleChanged(t, lane); changed.Text != "parser session" {
		t.Fatalf("late title = %q", changed.Text)
	}
}

// Registration racing a title landing has one owner. This forces the precise
// interleaving that used to duplicate the event: title state is visible, the
// surface registers and replays it, and only then does live publication run.
// A task update is the marker after both possible title sends, so the count
// needs no sleep or negative timeout.
func TestATitleRacingALifetimeSubscriptionArrivesOnce(t *testing.T) {
	agent, _ := titleAgent(t, &scriptedCompleter{}, nil)
	landing := agent.beginTitleLanding("parser session")
	lane, stop := agent.WatchTaskUpdates()
	defer stop()

	agent.finishTitleLanding(landing)
	agent.emitTaskUpdate(TaskNotice{ID: 99, State: TaskRunning})

	titles := 0
	for {
		select {
		case event := <-lane:
			switch event.Kind {
			case EventTitleChanged:
				titles++
			case EventTaskUpdate:
				if titles != 1 {
					t.Fatalf("title arrived %d times before the live marker, want once", titles)
				}
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the lifetime lane never reached its live marker")
		}
	}
}

// A NAME IS WORDS, even when the model answers with a filename. The instruction
// asks for eight lowercase words and a model that has read a million
// identifiers sometimes welds them together; the welding is undone once, here,
// rather than at each of the places the name is drawn.
func TestASluggedTitleIsMintedAsWords(t *testing.T) {
	for _, row := range []struct{ said, want string }{
		{"porting_the_parser", "porting the parser"},
		{"fix-the-nil-map", "fix the nil map"},
		// A name that is already words keeps every character it has, hyphens
		// inside those words included: they are somebody's spelling, not a
		// separator this function gets to reinterpret.
		{"port-b failures", "port-b failures"},
	} {
		completer := &scriptedCompleter{steps: titleTurn("the parser is fine", row.said)}
		agent, _ := titleAgent(t, completer, nil)
		lane, stop := agent.WatchTaskUpdates()
		collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
		changed := waitTitleChanged(t, lane)
		stop()
		if changed.Text != row.want {
			t.Fatalf("a title answered as %q was minted %q, want %q", row.said, changed.Text, row.want)
		}
		if err := agent.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
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
	lane, stop := agent.WatchTaskUpdates()
	defer stop()

	first := collect(t, mustSubmit(t, agent, "why is the tokenizer slow?"))
	_ = waitTitleChanged(t, lane)
	second := collect(t, mustSubmit(t, agent, "and the parser?"))

	if got := countKind(first, EventTitleChanged); got != 0 {
		t.Fatalf("first turn fired %d title events, want the standing lane to carry it", got)
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
			waitTitleJob(t, agent)

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
	waitTitleJob(t, agent)

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

// EventTurnDone and stream closure are the end of the turn even while the
// session's title provider is stalled. A second submission therefore starts a
// new turn, and the eventual title update stays on the lifetime lane rather
// than leaking into either turn stream.
func TestASlowTitleDoesNotBlockOrSteerTheNextTurn(t *testing.T) {
	titleStarted := make(chan struct{})
	releaseTitle := make(chan struct{})
	secondStarted := make(chan struct{})
	var once sync.Once
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("first answer"), nil },
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			once.Do(func() { close(titleStarted) })
			select {
			case <-releaseTitle:
				return textResponse("parser session"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			close(secondStarted)
			return textResponse("second answer"), nil
		},
	}}
	agent, _ := titleAgent(t, completer, nil)
	lane, stop := agent.WatchTaskUpdates()
	defer stop()

	first := collect(t, mustSubmit(t, agent, "inspect the parser"))
	if last := first[len(first)-1]; last.Kind != EventTurnDone {
		t.Fatalf("first stream ended with %v", last.Kind)
	}
	select {
	case <-titleStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("title provider did not start")
	}
	second := mustSubmit(t, agent, "now inspect the lexer")
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("the stalled title kept the next turn from starting")
	}
	secondEvents := collect(t, second)
	if last := secondEvents[len(secondEvents)-1]; last.Kind != EventTurnDone {
		t.Fatalf("second stream ended with %v", last.Kind)
	}
	close(releaseTitle)
	if changed := waitTitleChanged(t, lane); changed.Text != "parser session" {
		t.Fatalf("title event = %q", changed.Text)
	}
	if countKind(secondEvents, EventTitleChanged) != 0 {
		t.Fatal("the first turn's title leaked onto the second turn")
	}
}

// Close owns the detached provider call: it cancels it, waits without holding
// the agent lock, and closes the journal only after the call has settled.
func TestCloseCancelsAndSettlesAnAsyncTitle(t *testing.T) {
	titleStarted := make(chan struct{})
	titleCancelled := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("answer"), nil },
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(titleStarted)
			<-ctx.Done()
			close(titleCancelled)
			return nil, ctx.Err()
		},
	}}
	agent, path := titleAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "inspect the parser"))
	select {
	case <-titleStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("title provider did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- agent.Close() }()
	select {
	case <-titleCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel the title provider")
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked with the title cleanup")
	}
	for _, line := range readLines(t, path) {
		if strings.Contains(line, `"type":"title"`) {
			t.Fatalf("cancelled title wrote after Close: %s", line)
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
