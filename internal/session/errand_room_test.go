package session

import (
	"context"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// AN ERRAND MAY ASK FOR THE ROOM IT NEEDS: a reasoning model spent 5120 tokens
// thinking on a repository's notes and had nothing left to answer with, while
// the caller had asked for 8192 and been held to 4096.
func TestAnErrandGetsTheOutputBoundItAskedFor(t *testing.T) {
	ask := errandAsk("repo_notes")
	ask.MaxTokens = 8192
	got, err := ask.Normalize()
	if err != nil || got.MaxTokens != 8192 {
		t.Fatalf("max tokens = %d, err %v, want 8192", got.MaxTokens, err)
	}
	ask.MaxTokens = 8193
	if got, _ := ask.Normalize(); got.MaxTokens != 8192 {
		t.Fatalf("the bound did not hold: %d", got.MaxTokens)
	}
}

// A CALL THAT SPENT EVERYTHING AND SAID NOTHING SAYS SO. It is billed, it keeps
// its usage, it has a reason a caller can act on, and it is not cached, so the
// next press asks again. A plain ask used to come back with no text and no
// reason; a JSON ask called it invalid JSON.
func TestAnErrandThatGaveNoAnswerSaysSoAndKeepsItsCost(t *testing.T) {
	for name, json := range map[string]bool{"plain": false, "json": true} {
		t.Run(name, func(t *testing.T) {
			c := &replyCompleter{answer: func(context.Context, []ai.Message, ai.Request) (*ai.Response, error) {
				response := textResponse("  ")
				response.Choices[0].FinishReason = "length"
				return response, nil
			}}
			a, _ := newTestAgent(t, pricedAt(c, 0.02), func(cfg *Config) { cfg.OneModel = true })
			ask := errandAsk("repo_notes")
			ask.JSON, ask.Cache = json, "k1"
			for press := 1; press <= 2; press++ {
				got, err := a.RunErrand(t.Context(), ask)
				if err != nil || got.Text != "" || got.Reason != ErrandNoAnswer || got.Usage.Out != 5 || got.Cached {
					t.Fatalf("press %d: answer=%+v err=%v", press, got, err)
				}
			}
			if c.calls.Load() != 2 {
				t.Fatalf("no answer was kept in the cache: %d calls", c.calls.Load())
			}
		})
	}
}
