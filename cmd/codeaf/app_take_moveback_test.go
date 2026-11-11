package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
)

// N20: a chat started in A's home folder moves to B, gets a turn there while B
// keeps running, and moves back. The chat A opens holds B's turn, under the id
// and title A has always known it by. The two computers have different home
// folders, as two Macs do, so B keeps its copy in a copy folder of its own.
func TestAppTakeN20MoveBackKeepsTheOtherComputersTurn(t *testing.T) {
	celltest.RequireEngine(t)
	root := e2TempDir(t)
	wakeEnvironment(t, root)
	model := appTwoMessageSource(t, "echo n20-move-back")
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
	row = c1bSettledRow(t, "N20 B sees A's last save", b, a, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != "" })
	there := machineCall[appwire.TakeResult](t, b.w, "agents.take", appwire.TakeParams{Cell: row.Cell, Seen: row.Seen}).Agent
	t.Logf("B took %s: workspace %s", there.ID, there.Workspace)
	e2Turn(t, b, there.ID, "second on B")

	back := c1bSettledRow(t, "N20 A sees B's last save", a, b, row.Cell, func(r appwire.ChatRow) bool { return r.Seen.Head != row.Seen.Head })
	taken := machineCall[appwire.TakeResult](t, a.w, "agents.take", appwire.TakeParams{Cell: back.Cell, Seen: back.Seen}).Agent
	t.Logf("A took back %s (created %s): title %q workspace %s", taken.ID, created.ID, taken.Title, taken.Workspace)

	var problems []string
	if taken.ID != created.ID {
		problems = append(problems, "take back answered agent "+taken.ID+", not the chat A lists as "+created.ID)
	}
	if taken.Title == "" {
		problems = append(problems, "take back answered an agent with no title")
	}
	for _, id := range []string{taken.ID, created.ID} {
		raw, err := a.w.call("agents.entries", appwire.IDParams{ID: id})
		if err != nil {
			problems = append(problems, "entries of "+id+": "+err.Message)
			continue
		}
		if !strings.Contains(string(raw), "second on B") {
			problems = append(problems, "agent "+id+" on A lacks B's turn")
		}
	}
	if len(problems) > 0 {
		t.Fatalf("N20 move back:\n  %s", strings.Join(problems, "\n  "))
	}
}
