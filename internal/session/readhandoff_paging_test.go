package session

// readhandoff_paging_test.go — the run7 board-deck turn, replayed with a fake
// model. On 2026-11-19 a conversation managing a canvas read its canvas skill a
// window at a time (no offset, then 790, 1190, 1590). The sweep keyed each
// window as its own target, so the fourth window crossed the line and the rest
// of the skill went to a quick task. That task held only the four readers,
// saw the canvas as a folder with a database file in it, and answered that the
// canvas held no frames — it had two. The chat trusted the answer and asked the
// person what the deck was about instead of looking.
//
// The replay below is the same shape: four windows of one file, then the
// command that shows the canvas. It proves the windows stay the chat's own
// reading, nothing is handed off, and the command that shows the two parts is
// the chat's own call — so the parts are what the chat sees.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestPagingOneSkillStaysTheChatsReadingAndItSeesTheCanvas(t *testing.T) {
	var skill strings.Builder
	for line := 1; line <= 8; line++ {
		skill.WriteString("SKILL-LINE-" + string(rune('0'+line)) + "\n")
	}
	page := func(id, args string) step {
		return func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse(id, "read", args), nil
		}
	}
	expectRead := func(needle string, next step) step {
		return func(ctx context.Context, messages []ai.Message) (*ai.Response, error) {
			got := lastToolText(messages)
			if strings.Contains(got, "handed to quick task") {
				t.Errorf("a window of the one skill was handed off: %q", got)
			}
			if !strings.Contains(got, needle) {
				t.Errorf("the window did not read %s into the chat: %q", needle, got)
			}
			return next(ctx, messages)
		}
	}
	model := newQuickLanes([]step{
		page("p1", `{"path":"SKILL.md","limit":2}`),
		expectRead("SKILL-LINE-1", page("p2", `{"path":"SKILL.md","offset":3,"limit":2}`)),
		expectRead("SKILL-LINE-3", page("p3", `{"path":"SKILL.md","offset":5,"limit":2}`)),
		expectRead("SKILL-LINE-5", page("p4", `{"path":"SKILL.md","offset":7}`)),
		expectRead("SKILL-LINE-7", func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("show", "bash", `{"command":"cat parts.txt"}`), nil
		}),
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			got := lastToolText(messages)
			if !strings.Contains(got, "KPIs") || !strings.Contains(got, "Board notes") {
				t.Errorf("the chat's own look at the canvas did not show its two parts: %q", got)
			}
			return textResponse("Building on the KPIs sheet and the Board notes."), nil
		},
	})
	// Wired as readingAgent wires a conversation the hand-off can reach, so the
	// replay would hand the windows off if the sweep still counted them apart.
	registerBeltRunEngine(t, &beltRunDouble{})
	agent, graph, workspace := quickAgent(t, model, func(c *Config) {
		c.Interactive = true
		c.AskConsent = true
	})
	if err := os.WriteFile(filepath.Join(workspace, "SKILL.md"), []byte(skill.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	// parts.txt stands in for what the canvas command prints: the two parts
	// the canvas was seeded with.
	if err := os.WriteFile(filepath.Join(workspace, "parts.txt"), []byte("KPIs (sheet)\nBoard notes (doc)\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	collect(t, mustSubmit(t, agent, "5 slide deck for the board meeting"))
	graph.mu.Lock()
	nodes := len(graph.order)
	graph.mu.Unlock()
	if nodes != 0 {
		t.Fatalf("paging one file started %d task(s); it must stay the chat's reading", nodes)
	}
	if !model.sawIn("", "Board notes (doc)") {
		t.Fatal("the chat never saw the canvas's parts")
	}
}
