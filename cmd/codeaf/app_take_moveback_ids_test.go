package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
)

// N20-1: a chat A made moves to B, gets a turn there, and moves back while A's
// app runs all along. A's next message is numbered from the chat that moved
// back, not from what A's own copy held: the id its live entry carries is the
// id the listing gives that message, and B's message keeps its own. An id the
// app has already seen on B's message would overwrite that message in its view.
func TestAppTakeN20MoveBackNumbersTheNextMessageFromTheMovedChat(t *testing.T) {
	celltest.RequireEngine(t)
	root := e2TempDir(t)
	wakeEnvironment(t, root)
	model := appTwoMessageSource(t, "echo n20-move-back-ids")
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)
	relay := e2WireRelay(t)
	homeA, homeB := outageHomes(t, relay.URL)
	folderA, folderB := filepath.Join(root, "person-a"), filepath.Join(root, "person-b")
	for _, dir := range []string{folderA, folderB} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", folderA)
	a := e2StartApp(t, homeA)
	t.Setenv("HOME", folderB)
	b := e2StartApp(t, homeB)

	created := machineCall[struct{ Agent appwire.Agent }](t, a.w, "agents.create", map[string]any{"workspace": folderA}).Agent
	e2Turn(t, a, created.ID, "first on A")
	row := outageChatRow(t, b, "", func(r appwire.ChatRow) bool { return r.Title != "" && r.CanTake && r.Seen.Head != "" })
	row = c1bSettledRow(t, "N20-1 B sees A's last save", b, a, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != "" })
	there := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	e2Turn(t, b, there.ID, "second on B")
	back := c1bSettledRow(t, "N20-1 A sees B's last save", a, b, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != row.Seen.Head })
	machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: back.Cell, Seen: back.Seen})

	listed := n20Entries(t, a, created.ID)
	fromB := n20Said(listed, "second on B")
	if fromB == "" {
		t.Fatalf("A's listing after the move back lacks B's message: %v", listed)
	}
	a.mu.Lock()
	from := len(a.events)
	a.mu.Unlock()
	e2Turn(t, a, created.ID, "third on A")
	live := ""
	a.mu.Lock()
	for _, f := range a.events[from:] {
		if f.Event != appwire.EventEntryAdded {
			continue
		}
		var added appwire.EntryAdded
		if json.Unmarshal(f.Data, &added) == nil && added.Entry.Agent == created.ID && added.Entry.Kind == appwire.EntryUser {
			live = added.Entry.ID
		}
	}
	a.mu.Unlock()

	var problems []string
	if live == fromB {
		problems = append(problems, "A's message after the move back went out as "+live+", the id of B's message")
	}
	after := n20Entries(t, a, created.ID)
	if said := n20Text(after, live); said != "third on A" {
		problems = append(problems, "the listing gives "+live+" (A's live id) to "+strings.TrimSpace(said)+", not to A's message")
	}
	if said := n20Text(after, fromB); said != "second on B" {
		problems = append(problems, "B's message "+fromB+" now reads "+said)
	}
	// The scripted model names every turn's tool call alike, so only messages
	// and answers are counted here.
	seen := map[string]bool{}
	for _, e := range after {
		if e.Kind != appwire.EntryUser && e.Kind != appwire.EntryAnswer {
			continue
		}
		if seen[e.ID] {
			problems = append(problems, "two listed entries share the id "+e.ID)
		}
		seen[e.ID] = true
	}
	if len(problems) > 0 {
		t.Fatalf("N20-1 move back while A runs:\n  %s", strings.Join(problems, "\n  "))
	}
}

func n20Entries(t *testing.T, a *e2App, id string) []appwire.Entry {
	t.Helper()
	raw, err := a.w.call("agents.entries", appwire.IDParams{ID: id})
	if err != nil {
		t.Fatalf("entries of %s: %s", id, err.Message)
	}
	var page struct {
		Entries []appwire.Entry `json:"entries"`
	}
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatal(err)
	}
	return page.Entries
}

// n20Said is the id of the person's message with these words.
func n20Said(entries []appwire.Entry, text string) string {
	for _, e := range entries {
		if e.Kind == appwire.EntryUser && strings.Contains(e.Text, text) {
			return e.ID
		}
	}
	return ""
}

func n20Text(entries []appwire.Entry, id string) string {
	for _, e := range entries {
		if e.ID == id {
			return e.Text
		}
	}
	return ""
}
