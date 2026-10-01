package tui3

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/session"
)

// seniorDevReceiptAgent returns the engine's ceiling unchanged so this test
// can ask whether the typed door actually puts that receipt on the screen.
type seniorDevReceiptAgent struct{ *fakeAgent }

func (*seniorDevReceiptAgent) Delegates() session.DelegateReport {
	return session.DelegateReport{Rows: []session.DelegateRow{{Name: "senior-dev"}}}
}

func (*seniorDevReceiptAgent) StartDelegate(context.Context, string, string) (uint64, string, string, error) {
	return 7, "Repair the parser", (delegate.Ceilings{CostUSD: 1, Hours: 0.5}).Summary(), nil
}

func TestTypedSeniorDevReceiptNamesProgramAndCeilingOutsideWorked(t *testing.T) {
	a := newTestApp(&seniorDevReceiptAgent{fakeAgent: &fakeAgent{model: "m"}})
	a.width, a.height = 160, 45
	a.workMode = config.WorkFold
	a.turn = 1
	a.entries = []entry{{kind: entryUser, text: "Earlier question", turn: 1},
		{kind: entryAssistant, text: "Earlier answer.", turn: 1, settled: true}}
	msg := settleDoor(t, a, a.runDelegateCommand("senior-dev", "repair the parser"))
	drive(t, a, msg)
	want := "senior-dev task 7 started · Repair the parser · up to $1.00 and 30m"
	if got := taskText(a); !strings.Contains(got, want) {
		t.Fatalf("the running program has no visible start receipt %q:\n%s", want, got)
	}
	a.entries = append(a.entries,
		entry{kind: entryTool, tool: "read", status: toolOK, turn: 1},
		entry{kind: entryAssistant, text: "A later reply.", settled: true, turn: 1})
	a.touch()
	if got := taskText(a); !strings.Contains(got, want) || !strings.Contains(got, "worked") {
		t.Fatalf("settling a reply swallowed the start receipt:\n%s", got)
	}
	// A delayed start receipt must stay in the conversation that asked for it.
	started := msg.(taskStartedMsg)
	started.conv = "another conversation"
	before := len(a.entries)
	drive(t, a, started)
	if len(a.entries) != before {
		t.Fatal("another conversation's start receipt entered this conversation")
	}
}

// The program is already on the real engine's notices. Exercise both surface
// lanes without plan rows so the card cannot borrow a badge from a later read.
func TestSeniorDevEndedCardNamesProgramOnBothChatRoads(t *testing.T) {
	for _, road := range []string{"engine", "no-host"} {
		t.Run(road, func(t *testing.T) {
			a := newTestApp(&fakeAgent{model: "m"})
			a.width, a.height = 160, 45
			a.workMode = config.WorkFold
			a.turn = 1
			a.entries = []entry{{kind: entryUser, text: "Hand it over", turn: 1},
				{kind: entryAssistant, text: "It is running.", settled: true, turn: 1}}
			send := func(ev session.Event) {
				var msg tea.Msg = streamEventMsg{gen: a.gen, ev: ev}
				if road == "engine" {
					msg = taskEventMsg{gen: a.taskGen, ev: ev}
				}
				drive(t, a, msg)
			}
			send(update(7, "Repair the parser", session.TaskRunning, session.TaskNotice{Program: "senior-dev"}))
			send(update(7, "Repair the parser", session.TaskDone, session.TaskNotice{Report: "submitted a change"}))
			card := a.doneCardFor(7)
			if card == nil || card.program != "senior-dev" {
				t.Fatalf("the landing lost the program identity: %+v", card)
			}
			if got := taskText(a); !strings.Contains(got, "Repair the parser [senior-dev]") {
				t.Fatalf("the compact landing has no badge:\n%s", got)
			}
			if rows := a.doneRows(card, 160, false); len(rows) != 1 {
				t.Fatalf("the compact landing stopped being one row: %v", rows)
			}
			card.open = true
			if got := plain(strings.Join(a.doneRows(card, 160, false), "\n")); !strings.Contains(got, "senior-dev's ending went to the chat") || !strings.Contains(got, "submitted a change") {
				t.Fatalf("the opened program card lost its ending:\n%s", got)
			}
			card.open = false
			a.turn = 2
			a.entries = append(a.entries,
				entry{kind: entryTool, tool: "read", status: toolOK, turn: 2},
				entry{kind: entryAssistant, text: "Its work is on its branch.", settled: true, turn: 2})
			a.touch()
			if got := taskText(a); !strings.Contains(got, "Repair the parser [senior-dev]") || !strings.Contains(got, "worked") {
				t.Fatalf("the wake reply hid the program's badge:\n%s", got)
			}
		})
	}
}

func TestSeniorDevEndedCardKeepsBadgeWhenTitleIsCut(t *testing.T) {
	a := newTestApp(&fakeAgent{model: "m"})
	card := &taskDone{title: strings.Repeat("Repair the parser ", 8), program: "senior-dev"}
	for _, tc := range []struct {
		width int
		badge string
	}{{160, "[senior-dev]"}, {40, "[senior-dev]"}, {24, "[sd]"}} {
		head := a.doneHead(card, tc.width, false)
		if !strings.Contains(plain(head), tc.badge) || ansi.StringWidth(head) > tc.width {
			t.Fatalf("width %d: the card lost its badge or overflowed: %q", tc.width, head)
		}
		if !strings.Contains(head, a.pal.programInk(tc.badge)) {
			t.Fatalf("width %d: the card's badge bypassed its painter: %q", tc.width, head)
		}
	}
	card.program = ""
	if got := plain(a.doneHead(card, 160, false)); strings.Contains(got, "[sd]") || strings.Contains(got, "[senior-dev]") {
		t.Fatalf("an ordinary task borrowed a program badge: %s", got)
	}
}

// Adjacent landings use the batch renderer, so opening the single-card
// renderer cannot prove that the conversation keeps a program's identity.
func seniorDevBatchApp(t *testing.T, road, title string) *app {
	t.Helper()
	a := newTestApp(&fakeAgent{model: "m"})
	a.width, a.height = 160, 45
	a.workMode = config.WorkFold
	a.turn = 1
	a.entries = []entry{{kind: entryUser, text: "Hand it over", turn: 1},
		{kind: entryAssistant, text: "It is running.", settled: true, turn: 1}}
	for _, ev := range []session.Event{
		update(7, title, session.TaskRunning, session.TaskNotice{Program: "senior-dev"}),
		update(8, "Read the guide", session.TaskRunning, session.TaskNotice{}),
		update(7, title, session.TaskDone, session.TaskNotice{Report: "submitted the parser change", Changed: []string{"parser.go"}}),
		update(8, "Read the guide", session.TaskDone, session.TaskNotice{Report: "the guide was read"}),
	} {
		var msg tea.Msg = streamEventMsg{gen: a.gen, ev: ev}
		if road == "engine" {
			msg = taskEventMsg{gen: a.taskGen, ev: ev}
		}
		drive(t, a, msg)
	}
	first := a.doneEntryFor(7)
	if first < 0 || a.doneEntryFor(8) != first+1 {
		t.Fatalf("the landings are not adjacent: %+v", a.entries)
	}
	if got := taskText(a); !strings.Contains(got, "2 tasks done") || strings.Contains(got, title) {
		t.Fatalf("the settled batch is not compact:\n%s", got)
	}
	// Use the key the real conversation advertises, rather than setting open
	// or drawing either card through doneRows directly.
	a.sel = first
	drive(t, a, key("ctrl+o"))
	if !a.doneCardFor(7).open || a.doneCardFor(8).open {
		t.Fatal("expanding the batch changed the ordinary card's own open state")
	}
	return a
}

func TestSeniorDevBatchKeepsProgramBadgeOnBothChatRoads(t *testing.T) {
	for _, road := range []string{"engine", "no-host"} {
		t.Run(road, func(t *testing.T) {
			a := seniorDevBatchApp(t, road, "Repair the parser")
			if got := taskText(a); !strings.Contains(got, "Repair the parser [senior-dev]") || !strings.Contains(got, "Read the guide") {
				t.Fatalf("the expanded batch lost the program identity or its ordinary sibling:\n%s", got)
			}
			a.doneCardFor(7).title = strings.Repeat("Repair the parser ", 8)
			for _, tc := range []struct {
				width int
				badge string
			}{{160, "[senior-dev]"}, {40, "[sd]"}, {24, "[sd]"}} {
				a.width = tc.width
				a.touch()
				if got := taskText(a); !strings.Contains(got, tc.badge) {
					t.Fatalf("width %d: the expanded batch cut off its badge:\n%s", tc.width, got)
				}
				// Check the row's painter and width as well as the whole surface.
				row := a.rollupRow(a.doneCardFor(7), tc.width, false)
				if ansi.StringWidth(row) > tc.width || !strings.Contains(row, a.pal.programInk(tc.badge)) {
					t.Fatalf("width %d: the batch badge bypassed its painter or overflowed: %q", tc.width, row)
				}
				ordinary := plain(a.rollupRow(a.doneCardFor(8), tc.width, false))
				if strings.Contains(ordinary, "[sd]") || strings.Contains(ordinary, "[senior-dev]") {
					t.Fatalf("the ordinary sibling borrowed the program badge: %s", ordinary)
				}
			}
		})
	}
}

func TestSeniorDevBatchOpenedCardRetainsEndingOnBothChatRoads(t *testing.T) {
	for _, road := range []string{"engine", "no-host"} {
		t.Run(road, func(t *testing.T) {
			a := seniorDevBatchApp(t, road, "Repair the parser")
			assertEnding := func() {
				t.Helper()
				got := taskText(a)
				for _, want := range []string{"senior-dev's ending went to the chat", "submitted the parser change"} {
					if !strings.Contains(got, want) {
						t.Fatalf("the opened card in the batch lost %q:\n%s", want, got)
					}
				}
				if strings.Contains(got, "the guide was read") {
					t.Fatalf("opening senior-dev's card opened its sibling's output:\n%s", got)
				}
			}
			assertEnding()
			drive(t, a, key("ctrl+o"))
			if got := taskText(a); strings.Contains(got, "submitted the parser change") || !strings.Contains(got, "2 tasks done") {
				t.Fatalf("closing the batch did not restore its compact row:\n%s", got)
			}
			drive(t, a, key("ctrl+o"))
			assertEnding()
			a.turn = 2
			a.entries = append(a.entries,
				entry{kind: entryTool, tool: "read", status: toolOK, turn: 2},
				entry{kind: entryAssistant, text: "Its work is on its branch.", settled: true, turn: 2})
			a.touch()
			assertEnding()
			if got := taskText(a); !strings.Contains(got, "worked") {
				t.Fatalf("the later reply did not fold its own work:\n%s", got)
			}
		})
	}
}
