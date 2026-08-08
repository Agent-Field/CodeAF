package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func dockModel(t *testing.T) *Model {
	t.Helper()
	now := time.Now()
	delivery := store.Message{Seq: 8, Role: store.RoleSystem, Body: "Landed in /tmp/launch.md\nDetail."}
	model := New(&fakeBackend{}, "dock")
	model.messages = []store.Message{
		{Seq: 1, SessionID: "dock", Role: store.RoleUser, Time: now,
			Body: "Build me a launch note that covers the release, the migration, and the rollback plan."},
		{Seq: 2, SessionID: "dock", Role: store.RoleAgent, Time: now,
			Body: "On it — the launch note is queued and I will report back when it lands."},
		{Seq: 3, SessionID: "dock", Role: store.RoleSystem, CommandSeq: 7, Time: now,
			Body: "Read the compiled request.\nAssumed: no schema changes."},
	}
	model.snapshot = store.Snapshot{Nodes: []store.Node{
		{ID: "job-1", Brief: "Draft the launch note", Title: "Draft the launch note", Status: store.Running},
		{ID: "job-2", Brief: "Audit the migration path for the release", Status: store.Pending},
	}}
	model.cardSnapshot = model.snapshot
	model.cards = []jobCard{
		{ID: "active", RootID: "active", State: cardWorking, Total: 4, Done: 1,
			Title:  "Docked work on the migration audit",
			Latest: "reading the migration table in the repository"},
		{ID: "landed", RootID: "landed", State: cardSettled, BirthSeq: 5,
			Title: "Settled launch note", Outcome: "Landed in /tmp/launch.md", Deliverable: &delivery},
	}
	return model
}

// docs/JOURNEY.md names the quick-access answer: aforge has to be excellent as
// a docked, narrow, always-open pane. That is a width sweep, not an opinion —
// every surface a person can be looking at, at every width one of these panes
// gets, with the frame never wider than the terminal.
func TestEverySurfaceFitsTheDockedPane(t *testing.T) {
	for _, width := range dockWidths {
		model := dockModel(t)
		model.setSize(width, 26)
		assertFitsWidth(t, model.View(), width, "thread")

		model.graphOpen = true
		model.setSize(width, 26)
		assertFitsWidth(t, model.View(), width, "board")
		model.graphOpen = false

		model.cardExpanded["landed"] = true
		model.cardExpanded["active"] = true
		model.setSize(width, 26)
		assertFitsWidth(t, model.View(), width, "expanded cards")

		model.openHelp()
		assertFitsWidth(t, model.View(), width, "help")
		model.closeHelp()

		empty := New(&fakeBackend{}, "empty")
		empty.setSize(width, 26)
		assertFitsWidth(t, empty.View(), width, "empty thread")
	}
}

// The header degrades in a fixed order and never loses the places or the doors
// that have no other route. The residency work established the idiom; this is
// the rest of the bar held to it.
func TestTheHeaderStaysHonestDownToSixtyColumns(t *testing.T) {
	model := dockModel(t)
	for _, width := range dockWidths {
		model.setSize(width, 26)
		header := ansi.Strip(model.renderTopBar())
		if lipgloss.Width(header) > width {
			t.Fatalf("width %d header is %d cells: %q", width, lipgloss.Width(header), header)
		}
		for _, door := range []string{"aforge", "models", "tasks", "?"} {
			if !strings.Contains(header, door) {
				t.Fatalf("width %d header dropped %q: %q", width, door, header)
			}
		}
		// The places are the navigation; they are the last thing to yield and
		// they never yield at any width a dock actually has.
		if !strings.Contains(header, "thread · board · self") {
			t.Fatalf("width %d header lost the places: %q", width, header)
		}
	}
}

// The board is a read, and in a narrow frame it takes the pane whole rather
// than squeezing beside the thread into a column of ellipses. Either way the
// mouth stays on screen: the composer is never what yields.
func TestTheBoardYieldsRatherThanTruncatingInTheDock(t *testing.T) {
	model := dockModel(t)
	model.graphOpen = true
	model.setSize(60, 26)
	if model.horizontal {
		t.Fatal("a 60-column frame split itself between the thread and the rail")
	}
	if model.graphWidth != model.width {
		t.Fatalf("the narrow board took %d of %d columns", model.graphWidth, model.width)
	}
	frame := ansi.Strip(model.View())
	if !strings.Contains(frame, "tasks") {
		t.Fatalf("the narrow board never rendered:\n%s", frame)
	}
	if !strings.Contains(frame, "Ask the graph") {
		t.Fatalf("the composer left the frame while the board was open:\n%s", frame)
	}
}

// The modals used to be drawn from y=1 to the bottom of the terminal, over the
// composer. In a 60-column dock that left the input's own corners showing on
// either side of the panel, and took away the one control the guide is teaching.
func TestModalsStopAboveTheComposer(t *testing.T) {
	for _, width := range dockWidths {
		model := New(&fakeBackend{}, "modal-room")
		model.setSize(width, 26)
		model.openHelp()
		frame := model.View()
		_ = frame
		if bottom := model.helpBounds.bottom(); bottom > model.inputBounds.y {
			t.Fatalf("width %d: help runs to row %d and the composer starts at %d",
				width, bottom, model.inputBounds.y)
		}
		plain := ansi.Strip(model.View())
		if !strings.Contains(plain, "Ask the graph") {
			t.Fatalf("width %d: the composer is gone under the guide:\n%s", width, plain)
		}
		model.closeHelp()
	}
}
