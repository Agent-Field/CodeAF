package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

func TestRememberPlaceToolSavesTranscriptAndUndo(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Release", placegraph.Context{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("save", "remember", `{"text":"Run the suite before launch"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("Saved."), nil },
	}}
	agent, journal := placedAgent(t, f, completer)
	f.file(t, agent.id, p)
	turn(t, agent, completer, "Remember: run the suite before launch")
	snap, err := f.store.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	lines := snap.Knowledge(p.ID)
	if len(lines) != 1 || lines[0].Text != "Run the suite before launch" || lines[0].Source.Kind != placegraph.LineSaidInChat || lines[0].Source.ChatID != agent.id || lines[0].Source.At.IsZero() {
		t.Fatalf("saved lines: %+v", lines)
	}
	token := placegraph.RememberUndoToken(lines[0])
	check := func(entries []DisplayEntry) {
		t.Helper()
		for _, e := range entries {
			if e.Role == "aside" && e.AsideKind == NoteKindPlaces && e.Text == "Saved to Release: Run the suite before launch" && len(e.UndoReceipts) == 1 && e.UndoReceipts[0] == token {
				return
			}
		}
		t.Fatalf("missing actionable save note: %+v", entries)
	}
	check(agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatal(err)
	}
	replay, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = journal })
	check(replay.Transcript())
	// The bridge has a different Store from the child engine that saved the line.
	if _, err := f.store.UndoRemember(token); err != nil {
		t.Fatal(err)
	}
	snap, _ = f.store.Snapshot()
	if len(snap.Knowledge(p.ID)) != 0 {
		t.Fatal("Undo left the saved line")
	}
	if _, err := f.store.UndoRemember(token); err == nil {
		t.Fatal("Undo accepted a consumed token")
	}
}

func TestRememberPlaceChoosesFirstParentMostDirectPlace(t *testing.T) {
	f := newPlaceFixture(t)
	root := f.place(t, "Software", placegraph.Context{})
	child := f.place(t, "Release", placegraph.Context{}, root.ID)
	other := f.place(t, "Marketing", placegraph.Context{})
	agent, _ := placedAgent(t, f, &scriptedCompleter{})
	f.file(t, agent.id, child)
	f.file(t, agent.id, root)
	f.file(t, agent.id, other)
	saved, handled, err := agent.rememberPlace("Use short sentences", "")
	if err != nil || !handled || saved != "Saved to Software: Use short sentences · first parent-most place" {
		t.Fatalf("save: %q %v %v", saved, handled, err)
	}
	snap, _ := f.store.Snapshot()
	if len(snap.Knowledge(root.ID)) != 1 || len(snap.Knowledge(other.ID)) != 0 || len(snap.Knowledge(child.ID)) != 0 {
		t.Fatal("saved in the wrong place")
	}
	// One direct child stays the target even though it inherits a parent.
	single, _ := placedAgent(t, f, &scriptedCompleter{})
	f.file(t, single.id, child)
	saved, _, err = single.rememberPlace("Keep strict mode", "place")
	if err != nil || !strings.HasPrefix(saved, "Saved to Release:") || strings.Contains(saved, "parent-most") {
		t.Fatalf("one place: %q %v", saved, err)
	}
}

func TestRememberPlaceRefusalsAndExplicitMemoryScope(t *testing.T) {
	f := newPlaceFixture(t)
	p := f.place(t, "Release", placegraph.Context{})
	agent, _ := placedAgent(t, f, &scriptedCompleter{})
	if _, handled, err := agent.rememberPlace("fact", ""); handled || err != nil {
		t.Fatalf("unplaced fallback: %v %v", handled, err)
	}
	if _, handled, err := agent.rememberPlace("fact", "place"); !handled || err == nil {
		t.Fatal("explicit place silently fell back")
	}
	f.file(t, agent.id, p)
	if _, handled, err := agent.rememberPlace("personal", "user"); handled || err != nil {
		t.Fatal("personal scope saved into a place")
	}
	tools := agent.memoryTools()
	if len(tools) == 0 || tools[0].Name != "remember" {
		t.Fatal("placed chat without a general memory store has no remember tool")
	}
	for _, args := range []string{`{"text":" "}`, `not json`} {
		_, failed, err := tools[0].Execute(context.Background(), json.RawMessage(args))
		if !failed || err != nil {
			t.Fatalf("invalid input %s: %v %v", args, failed, err)
		}
	}
	agent.mu.Lock()
	agent.refreshPlaceGraphLocked()
	agent.mu.Unlock()
	held := false
	for _, tool := range agent.beltTools() {
		held = held || tool.Name == "remember"
	}
	if !held {
		t.Fatal("filing did not add remember to the belt")
	}
	if _, err := f.store.Archive(p.ID); err != nil {
		t.Fatal(err)
	}
	if agent.canRememberPlace() {
		t.Fatal("archived place offered remember")
	}
	agent.mu.Lock()
	agent.refreshPlaceGraphLocked()
	agent.mu.Unlock()
	for _, tool := range agent.beltTools() {
		if tool.Name == "remember" {
			t.Fatal("last place archived but remember remained on the belt")
		}
	}
}
