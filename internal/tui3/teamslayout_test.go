package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/session"
)

func TestTeamsAllColumnsStackWithEqualGapsAndCorrectDoors(t *testing.T) {
	a, parent, child := teamsPlaceLabIDs(t)
	a.wall.teams = append(a.wall.teams, team{ID: "second", Name: "Second"}, team{ID: "third", Name: "Third"}, team{ID: "fourth", Name: "Fourth"})
	roots := a.teamsOverviewRoots()
	d := &teamsDraw{a: a}
	rows := a.teamsOverviewGrid(d, roots, 130, 0, 0, false, map[string]bool{})
	bounds := map[string][2]int{}
	for _, target := range d.targets {
		if target.arg != "overview" || target.x1-target.x0 != 64 {
			continue
		}
		b, exists := bounds[target.id]
		if !exists {
			b = [2]int{target.y, target.y}
		}
		b[0], b[1] = min(b[0], target.y), max(b[1], target.y)
		bounds[target.id] = b
	}
	for i := 2; i < len(roots); i++ {
		above, below := bounds[roots[i-2].ID], bounds[roots[i].ID]
		if below[0]-above[1] != 2 {
			t.Fatalf("unequal card gap: %s %+v above %s %+v", roots[i].ID, below, roots[i-2].ID, above)
		}
	}
	if bounds["third"][0] == bounds["fourth"][0] {
		t.Fatal("short column was padded to taller neighbor")
	}
	if len(rows) != max(bounds["third"][1], bounds["fourth"][1])+1 {
		t.Fatal("grid has trailing rows or cuts off last border")
	}
	// The child must retain its own door inside the taller first card.
	a.tp.targets = d.targets
	found := false
	for _, target := range d.targets {
		if target.id != child || target.arg != "overview" {
			continue
		}
		// TargetAt uses full-frame coordinates and viewport bounds.
		a.height = 100
		for i := range a.tp.targets {
			a.tp.targets[i].y += placeHeadRows
		}
		hit, ok := a.teamsTargetAt(target.x0, target.y+placeHeadRows)
		if !ok || hit.id != child || hit.id == parent {
			t.Fatal("nested door lost to parent")
		}
		found = true
		break
	}
	if !found {
		t.Fatal("missing nested card")
	}
}

func TestTeamsWheelUsesPointerAndReachesLastBorder(t *testing.T) {
	for _, width := range []int{40, 150} {
		a, _, _ := teamsPlaceLabIDs(t)
		for i := 0; i < 24; i++ {
			a.wall.teams = append(a.wall.teams, team{ID: "extra-" + itoa(i), Name: "Extra " + itoa(i)})
		}
		a.width, a.height = width, 24
		a.tp.sel = teamsAllRow
		a.tp.cur = teamsRef{act: teamsActSelect, id: teamsAllRow}
		a.touch()
		teamsFrameText(a)
		cur := a.tp.cur
		paneX, paneY := a.tp.railW+2, placeHeadRows+a.tp.paneTop+1
		wheel := func(x, y int, button tea.MouseButton) {
			drive(t, a, tea.MouseWheelMsg{X: x, Y: y, Button: button})
			teamsFrameText(a)
		}
		wheel(paneX, paneY, tea.MouseWheelDown)
		if a.tp.paneOffset != 3 || a.tp.railOffset != 0 || a.tp.cur != cur {
			t.Fatalf("sidebar focus trapped main wheel at %d", width)
		}
		for i := 0; i < 100; i++ {
			wheel(paneX, paneY, tea.MouseWheelDown)
		}
		if a.tp.paneOffset != a.tp.paneRows-a.tp.paneRoom {
			t.Fatal("pane stopped before bottom")
		}
		// The final card's border is a live door on the last visible row.
		bottom := placeHeadRows + a.tp.paneTop + a.tp.paneRoom - 1
		sawBorder := false
		for _, target := range a.tp.targets {
			if target.pane && target.arg == "overview" && target.y == bottom && target.x1-target.x0 > 10 {
				sawBorder = true
			}
		}
		if !sawBorder {
			t.Fatalf("last border clipped at %d", width)
		}
		offset := a.tp.paneOffset
		railY := placeHeadRows + 1
		wheel(2, railY, tea.MouseWheelDown)
		if a.tp.railOffset != 3 || a.tp.paneOffset != offset {
			t.Fatal("sidebar wheel moved main pane")
		}
		for i := 0; i < 100; i++ {
			wheel(paneX, paneY, tea.MouseWheelUp)
		}
		if a.tp.paneOffset != 0 || a.tp.cur != cur {
			t.Fatal("return to top changed focus")
		}
	}
}

func TestTeamsRecentMessagesSortSiblingsAndInheritChildren(t *testing.T) {
	a, parent, child := teamsPlaceLabIDs(t)
	a.wall.teams = []team{
		{ID: parent, Name: "Parent", Members: []teamMember{{Key: "parent-msg"}}},
		{ID: child, Name: "Child", Parent: parent, Members: []teamMember{{Key: "child-msg"}}},
		{ID: "second", Name: "Second", Members: []teamMember{{Key: "second-msg"}}},
		{ID: "unknown", Name: "Unknown"},
	}
	base := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	a.tp.previews = map[string]teamsPreview{"parent-msg": {messageAt: base}, "child-msg": {messageAt: base.Add(time.Hour)}, "second-msg": {messageAt: base.Add(2 * time.Hour)}}
	roots := a.teamsOverviewRoots()
	if roots[0].ID != "second" || roots[1].ID != parent {
		t.Fatal("newest root not first")
	}
	a.tp.previews["child-msg"] = teamsPreview{messageAt: base.Add(3 * time.Hour)}
	roots = a.teamsOverviewRoots()
	tree := a.teamsOpenTree()
	if roots[0].ID != parent || tree[0].id != parent || tree[1].id != child || tree[2].id != "second" {
		t.Fatal("child reply did not promote same hierarchy in both surfaces")
	}
	if a.teamsRailRows()[0].kind != railRowAll {
		t.Fatal("All teams not pinned")
	}
	// Live assistant completion is visible without waiting for the disk reader.
	a.wall.teams[2].Members = []teamMember{{Key: a.frontTabKey()}}
	a.entries = []entry{{kind: entryAssistant, ended: base.Add(4 * time.Hour)}}
	if a.teamsOverviewRoots()[0].ID != "second" {
		t.Fatal("live reply did not reorder cards")
	}
	a.tp.previews["child-msg"] = teamsPreview{messageAt: base.Add(5 * time.Hour)}
	a.steerAccepted(&session.SteerNote{ID: 1, Words: "Use the other layout", At: base.Add(6 * time.Hour)})
	if a.teamsOverviewRoots()[0].ID != "second" {
		t.Fatal("accepted correction did not promote its team")
	}
}

func TestTeamsPreviewRecencyIgnoresSettingsAndRewoundMessages(t *testing.T) {
	file := filepath.Join(t.TempDir(), "journal.jsonl")
	journal := `{"type":"message","role":"user","timestamp":"2026-10-03T10:00:00Z","content":"Prompt"}
{"type":"message","role":"assistant","timestamp":"2026-10-03T11:00:00Z","content":"Reply"}
{"type":"model","timestamp":"2026-10-03T12:00:00Z","model":"new-model"}
{"type":"title","timestamp":"2026-10-03T13:00:00Z","title":"New title"}
`
	read := func(s string) teamsPreview {
		t.Helper()
		if err := os.WriteFile(file, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
		return teamsReadPreview(file, teamsPreview{})
	}
	expected := time.Date(2026, 10, 3, 11, 0, 0, 0, time.UTC)
	if got := read(journal); !got.messageAt.Equal(expected) {
		t.Fatalf("settings promoted conversation: %+v", got)
	}
	journal += `{"type":"message","role":"user","timestamp":"2026-10-03T14:00:00Z","content":"Removed prompt"}
{"type":"message","role":"assistant","timestamp":"2026-10-03T15:00:00Z","content":"Removed reply"}
{"type":"rewind","dropped":2}
`
	got := read(journal)
	if !got.messageAt.Equal(expected) || strings.Contains(got.text, "Removed") {
		t.Fatalf("rewound message promoted team: %+v", got)
	}
	a, _, _ := teamsPlaceLabIDs(t)
	a.wall.teams = []team{{ID: "rewound", Members: []teamMember{{Key: "rewound-chat", File: file}}}, {ID: "later", Members: []teamMember{{Key: "later-chat"}}}}
	a.tp.previews = map[string]teamsPreview{"rewound-chat": got, "later-chat": {messageAt: expected.Add(time.Hour)}}
	a.tp.world = map[string]session.SessionRow{file: {At: expected.Add(3 * time.Hour)}}
	if a.teamsOverviewRoots()[0].ID != "later" {
		t.Fatal("stale user metadata overrode rewound journal time")
	}
	a.tp.previews["rewound-chat"] = teamsPreview{}
	if a.teamsOverviewRoots()[0].ID != "rewound" {
		t.Fatal("old journal without timestamps lost metadata fallback")
	}

}
