package cellindex

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
)

func chatCell(t *testing.T) cell.Cell {
	t.Helper()
	c, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.Sandboxed})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// writeTranscript gives the chat's journal one usage line per cost, so the
// digest says what the chat spent when no receipt chain does.
func writeTranscript(t *testing.T, c cell.Cell, costs ...float64) {
	t.Helper()
	body := `{"type":"session","id":"` + c.ID + `","timestamp":"2026-01-01T00:00:00Z"}` + "\n"
	for _, usd := range costs {
		body += `{"type":"usage","timestamp":"2026-01-01T00:00:01Z","usage":{"model":"m","role":"turn","calls":1,"input":10,"output":5,"costUsd":` + ftoa(usd) + `}}` + "\n"
	}
	path := session.Place{Dir: c.Root}.Transcript()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ftoa(f float64) string { return string(strconv.AppendFloat(nil, f, 'f', -1, 64)) }

// A copy of a chat this machine already held has the ledger rows of the older
// copy. A refresh after a take adds the rows of the turns made since, once, and
// the spend on the session row follows.
func TestRefreshBringsInTheRowsOfTurnsMadeSinceTheOlderCopy(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	c := chatCell(t)
	older := []float64{0.01, 0.02}
	writeTranscript(t, c, older...)
	if _, err := Rebuild(c, ""); err != nil {
		t.Fatal(err)
	}
	before := totalOf(t)
	if before.micro != 30000 {
		t.Fatalf("older copy holds %+v", before)
	}

	writeTranscript(t, c, 0.01, 0.02, 0.03, 0.04) // the take brings two more turns
	if built, err := Rebuild(c, ""); err != nil || len(built) != 0 {
		t.Fatalf("an ordinary open must leave present indexes alone, built %v err %v", built, err)
	}
	built, err := Refresh(c, "")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(built, "usage.jsonl") || !contains(built, "meta.json") {
		t.Fatalf("refresh built %v", built)
	}
	if got := totalOf(t); got.micro != 100000 || got.in != 40 {
		t.Fatalf("after refresh the ledger holds %+v, want all four turns once", got)
	}
	if m, _ := session.LoadMeta(c.Root); m.SpentUSD < 0.0999 || m.SpentUSD > 0.1001 {
		t.Fatalf("session row spend %v", m.SpentUSD)
	}
	if built, _ := Refresh(c, ""); len(built) != 0 {
		t.Fatalf("a second refresh built %v", built)
	}
}

// A memory both copies had and the newer copy edited is edited on this machine
// too, and its journal does not double.
func TestRefreshReplaysMemoryEventsTheGraphNeverSaw(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	c := chatCell(t)
	session.CellMemories.Bind(c.ID, c.Root, nil)
	writeTranscript(t, c, 0.01)

	a, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.SetMemoryLedger(session.CellMemories)
	m, err := a.AddMemory(store.Memory{Type: store.MemoryFact, Scope: store.MemoryScopeProject, Title: "t", Text: "old", SourceSession: c.ID})
	if err != nil {
		t.Fatal(err)
	}
	// This machine's graph holds the first event only.
	held, _ := session.ReadMemoryEvents(c.Root)
	b, err := store.Open(home.Join("graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ImportMemoryEvents(held); err != nil {
		t.Fatal(err)
	}
	b.Close()
	if err := a.UpdateMemoryFromSession(m.ID, "t", "new", nil, c.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := Refresh(c, ""); err != nil {
		t.Fatal(err)
	}
	b, _ = store.Open(home.Join("graph.db"))
	defer b.Close()
	got, found, err := b.MemoryRecord(m.ID)
	if err != nil || !found || got.Text != "new" {
		t.Fatalf("memory on this machine: %+v found %v err %v", got, found, err)
	}
	if built, _ := Refresh(c, ""); contains(built, "graph.db memories") {
		t.Fatalf("a second refresh replayed the ledger: %v", built)
	}
}
