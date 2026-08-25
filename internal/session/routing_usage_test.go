package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── WHO IS WAITING ──────────────────────────────────────────────────────────
//
// A conversation's turn is a person watching an answer arrive; a task node's
// turn is the identical machinery with nobody in front of it. The adapter
// routes the first on speed and the second on price, and what it reads to tell
// them apart is stamped here.

// intentSpy records the routing intent every scripted step was called under.
type intentSpy struct {
	mu   sync.Mutex
	seen []provider.RoutingIntent
}

func (s *intentSpy) note(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seen = append(s.seen, provider.RoutingIntentFrom(ctx))
}

func (s *intentSpy) first(t *testing.T) provider.RoutingIntent {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.seen) == 0 {
		t.Fatal("no call was made")
	}
	return s.seen[0]
}

func TestAConversationsOwnTurnAsksForSpeed(t *testing.T) {
	spy := &intentSpy{}
	writer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			spy.note(ctx)
			return textResponse("here you go"), nil
		},
	}}
	agent, _ := newTestAgent(t, writer, nil)
	collect(t, mustSubmit(t, agent, "what is in the workspace?"))

	if got := spy.first(t); got != provider.IntentInteractive {
		t.Fatalf("the person's own turn routed as %v, want the interactive ask", got)
	}
}

func TestATaskNodesTurnAsksForPriceInstead(t *testing.T) {
	spy := &intentSpy{}
	writer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			spy.note(ctx)
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, writer, func(config *Config) {
		// The posture a task node's runner is built in, and the one private row
		// that lets such an agent be steered a line at all (session.go's Config).
		config.InTask = true
		config.roomThread = true
	})
	collect(t, mustSubmit(t, agent, "do the work"))

	if got := spy.first(t); got != provider.IntentBackground {
		t.Fatalf("a task node routed as %v, want the errand's price ask", got)
	}
}

// Every errand this package makes goes through one door, and the door says
// nobody is waiting (auxiliary.go's callRole).
func TestAnErrandAsksForPrice(t *testing.T) {
	spy := &intentSpy{}
	writer := &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			spy.note(ctx)
			return textResponse("a name"), nil
		},
	}}
	agent, _ := newTestAgent(t, writer, nil)
	if _, _, err := agent.callRole(context.Background(), "title", "test/model",
		[]ai.Message{textMessage("user", "name this")}); err != nil {
		t.Fatalf("callRole: %v", err)
	}
	if got := spy.first(t); got != provider.IntentBackground {
		t.Fatalf("an errand routed as %v, want the price ask", got)
	}
}

// ── ONE LINE PER RESPONSE ───────────────────────────────────────────────────

// journalCalls reads back every call line the journal holds, in order.
func journalCalls(t *testing.T, path string) []journalCall {
	t.Helper()
	var calls []journalCall
	for _, line := range readLines(t, path) {
		if !strings.Contains(line, `"type":"call"`) {
			continue
		}
		var entry sessionEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("call line: %v", err)
		}
		if entry.Call == nil {
			t.Fatalf("a call line carried no call: %s", line)
		}
		calls = append(calls, *entry.Call)
	}
	return calls
}

func TestEveryResponseWritesItsOwnLineWithItsOwnShape(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	cost := 0.0021
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-1", "ls", `{"path":"."}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := textResponse("nothing in there yet")
			response.Model = "vendor/served-model"
			response.Usage = &ai.Usage{
				PromptTokens:        900,
				CompletionTokens:    40,
				TotalTokens:         940,
				PromptTokensDetails: &ai.PromptTokensDetails{CachedTokens: 850},
				Cost:                &cost,
			}
			return response, nil
		},
	}}
	agent, _ := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "what is in the workspace?"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	calls := journalCalls(t, path)
	if len(calls) != 2 {
		t.Fatalf("journal holds %d call lines, want one per response", len(calls))
	}
	// THE SHAPE A SUMMED SEAL CANNOT SHOW: the second request re-sent a
	// transcript that was almost entirely warm, and that is the whole question a
	// cost autopsy asks.
	second := calls[1]
	if second.Input != 900 || second.CacheRead != 850 || second.Output != 40 {
		t.Fatalf("second call = %+v, want the response's own token counts", second)
	}
	if second.CostUSD != cost {
		t.Fatalf("second call cost = %v, want the provider's own figure %v", second.CostUSD, cost)
	}
	if second.Model != "vendor/served-model" {
		t.Fatalf("second call model = %q, want the model that actually answered", second.Model)
	}

	// And the seal is untouched: the lines are evidence beside the bill, never
	// a second copy of it.
	turns := turnUsageLines(t, path)
	if len(turns) != 1 {
		t.Fatalf("journal holds %d turn seals, want exactly 1", len(turns))
	}
	if turns[0].Calls != 2 {
		t.Fatalf("the seal counted %d calls, want the 2 it always did", turns[0].Calls)
	}
}

// THE EMPTINESS LAW. A response the provider said nothing about writes nothing
// — a row of zeroes would read as a fact.
func TestAResponseWithNoUsageWritesNoLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			response := textResponse("silent")
			response.Usage = nil
			return response, nil
		},
	}}
	agent, _ := newTestAgent(t, writer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "say something"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if calls := journalCalls(t, path); len(calls) != 0 {
		t.Fatalf("a usage-less response wrote %d lines: %+v", len(calls), calls)
	}
}

// A call line is never read back as money. The seal above it already carries
// every dollar on it, and a replay that added both would bill the session twice.
func TestCallLinesAreNotReadBackAsSpend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	write(t, path,
		`{"type":"session","version":1,"id":"abc","cwd":"/w","model":"m","timestamp":"t"}`,
		`{"type":"call","call":{"model":"m","endpoint":"quicksilver","input":900,"cacheRead":850,"output":40,"costUsd":0.0021},"timestamp":"t"}`,
		`{"type":"usage","usage":{"model":"m","input":900,"output":40,"cacheRead":850,"costUsd":0.0021,"calls":1},"timestamp":"t"}`,
	)
	replayed, err := replaySessionFile(path)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replayed.usage.CostUSD != 0.0021 || replayed.usage.Input != 900 || replayed.usage.Calls != 1 {
		t.Fatalf("replayed usage = %+v, want the seal alone", replayed.usage)
	}
}
