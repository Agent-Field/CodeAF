package tui3

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE MODEL CURSOR IS A DRAWN ROW, NOT AN INDEX. The person's report
// (2026-09-30): the cursor disappeared once the list bottomed out and they
// kept navigating past the last item, and the highlight was not visible at
// all when the menu first opened. Both had one root: the scroll window was
// counted in list rows while the drawing spends extra lines on a group's
// heading, the machines' heading and a why line — so the cursor could sit
// inside the logical window and still fall past the rendered bottom edge.
// These tests assert the RENDERED frame, because an index inside a window
// that never drew the row is exactly the bug.

// groupedCatalog is a list of two services, five models each, with the group
// headings the real list draws from connected providers. The headings are
// what pushed the cursor's row out of the rendered frame, so the fixture
// carries them even though [pickerApp]'s single-source ladder would not.
var groupedCatalog = func() []Model {
	out := make([]Model, 0, 10)
	for _, group := range []struct {
		name string
		head string
	}{
		{"alpha", "alpha · 5"},
		{"beta", "beta · 5"},
	} {
		for i := 1; i <= 5; i++ {
			out = append(out, Model{
				ID:            fmt.Sprintf("%s/model-%d", group.name, i),
				ContextLength: 200_000,
				Group:         group.name,
				GroupHead:     group.head,
				GroupOrder:    len(out),
			})
		}
	}
	return out
}()

// drawnCursorLines returns the menu lines wearing the cursor's ground. A
// wide row spends one line, while a phone row may carry its band over its tail.
func drawnCursorLines(a *app) []string {
	var out []string
	for _, line := range modelMenuLines(a) {
		if strings.Contains(line, "\x1b[48;") {
			out = append(out, line)
		}
	}
	return out
}

// A large catalog puts the held model beyond the initial window at every
// terminal width. Service headings and phone tails must share its line budget.
func TestTheMenuOpensOnTheModelInUseAndDrawsItsCursor(t *testing.T) {
	models := make([]Model, 400)
	for i := range models {
		group := "alpha"
		if i >= 200 {
			group = "beta"
		}
		models[i] = Model{ID: fmt.Sprintf("%s/model-%03d", group, i),
			ContextLength: 200_000, Group: group, GroupHead: group + " models", GroupOrder: i / 200}
	}
	current := models[397].ID
	for _, width := range []int{140, 100, 62, 50} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			a := pickerApp(t, &fakeAgent{model: current}, models)
			a.width, a.height = width, 30
			a.pal = newPalette(tokens.ANSI256, false)
			typeLine(t, a, "/model")
			shown := frame(a)
			visibleCursor := false
			for _, line := range strings.Split(shown, "\n") {
				if strings.Contains(plain(line), current) && strings.Contains(line, "\x1b[48;") {
					visibleCursor = true
				}
			}
			if !visibleCursor {
				t.Fatalf("opening frame at %d did not highlight the model in use (%s): %s", width, current, plain(shown))
			}
			if chosen, _ := a.pick.choice(); chosen.ID != current {
				t.Fatalf("menu opened on %s, want %s", chosen.ID, current)
			}
			// The phone band wraps with its row. Every painted line must belong
			// to that one selectable row, even when its tail has its own line.
			rows, owners := a.pick.rowsOwned(width, a.overlayHeight(), a.pal, -1, nil)
			for line, row := range rows {
				if strings.Contains(row, "\x1b[48;") && owners[line] != a.pick.cursor {
					t.Fatalf("opening frame painted another row: owner=%d cursor=%d", owners[line], a.pick.cursor)
				}
			}
			if !strings.Contains(plain(shown), "beta models") {
				t.Fatalf("opening frame lost its service heading: %s", plain(shown))
			}
		})
	}
}

func TestEnterImmediatelyAfterModelKeepsTheModelInUse(t *testing.T) {
	agent := &fakeAgent{model: "openai/gpt-4.1-mini", window: 1_000_000}
	a := pickerApp(t, agent, pickerCatalog)
	a.ctxWindow = agent.window
	typeLine(t, a, "/model")
	drive(t, a, key("enter"))
	if a.ctxWindow != 1_000_000 || agent.window != a.ctxWindow {
		t.Fatalf("Enter changed the context window to %d (agent %d)", a.ctxWindow, agent.window)
	}
	if a.model != "openai/gpt-4.1-mini" || agent.model != a.model {
		t.Fatalf("Enter immediately after /model switched to %s (agent %s)", a.model, agent.model)
	}
}

func TestARefreshReturnsEachModelDoorToTheModelItHolds(t *testing.T) {
	for _, task := range []bool{false, true} {
		t.Run(fmt.Sprintf("task-%t", task), func(t *testing.T) {
			a := pickerApp(t, &fakeAgent{model: "openai/gpt-4.1-mini"}, pickerCatalog)
			held := a.model
			if task {
				held = "moonshotai/kimi-k3"
				a.tasks = map[uint64]*taskNode{42: {model: held}}
				a.openTaskPicker(42)
			} else {
				typeLine(t, a, "/model")
			}
			typeInto(t, a, "i")
			drive(t, a, key("down"))
			a.modelsFetched(modelsFetchedMsg{all: true})
			if chosen, _ := a.pick.choice(); chosen.ID != held {
				t.Fatalf("refresh moved confirmation from %s to %s", held, chosen.ID)
			}
			if a.pick.filter.String() != "i" {
				t.Fatal("refresh erased the typed filter")
			}
		})
	}
}

func TestAMissingHeldModelFallsBackPastUnavailableRows(t *testing.T) {
	models := []Model{
		{Unavailable: true, Notice: "no models", Group: "alpha"},
		{ID: "beta/first", Group: "beta", GroupOrder: 1},
		{ID: "beta/last", Group: "beta", GroupOrder: 1},
	}
	var p picker
	p.start(models, "absent")
	// A refresh must choose the first selectable row even after a person walked
	// farther down the previous list, with the same query still in the box.
	p.filter.setText("beta")
	p.rank()
	p.move(1)
	p.restock(models)
	if chosen, _ := p.choice(); chosen.ID != "beta/first" {
		t.Fatalf("missing held model left the cursor on %s, want beta/first", chosen.ID)
	}
	p.filter.setText("")
	p.rank()
	if chosen, ok := p.choice(); !ok || chosen.ID != "beta/first" {
		t.Fatalf("emptying the filter selected an unavailable row: %+v", chosen)
	}
}

// The machines' heading and the reason for an unmeasured fold both spend
// screen lines. The cursor must remain drawn in both states at every width.
func TestTheCursorStaysVisibleWithMachineHeadingsAndWhyLines(t *testing.T) {
	for _, measured := range []bool{true, false} {
		for _, width := range []int{140, 100, 62, 50} {
			t.Run(fmt.Sprintf("width-%d-measured-%t", width, measured), func(t *testing.T) {
				rows := threeLanes()
				if !measured {
					rows = nil
				}
				laneLab(t, rows)
				a := laneApp(t)
				a.width, a.height = width, 30
				a.pal = newPalette(tokens.ANSI256, false)
				typeLine(t, a, "/model")
				drive(t, a, key("right"), key("down"), key("right"))
				p := &a.pick
				if !p.machines {
					t.Fatal("fixture did not open the machines")
				}
				headingSeen, reasonSeen := false, false
				for step := 0; step < len(p.list)+2; step++ {
					drawn, owners := p.rowsOwned(width, a.overlayHeight(), a.pal, -1, nil)
					shown := plain(strings.Join(drawn, "\n"))
					headingSeen = headingSeen || strings.Contains(shown, "first") && strings.Contains(shown, "t/s")
					reasonSeen = reasonSeen || strings.Contains(shown, "no host has been measured")
					if !slices.Contains(owners, p.cursor) {
						t.Fatalf("cursor absent at width %d: %s", width, shown)
					}
					drive(t, a, key("down"))
				}
				if measured && width >= 100 && !headingSeen {
					t.Fatal("fixture never drew the machines' heading")
				}
				if !measured && !reasonSeen {
					t.Fatal("fixture never drew why the machines are absent")
				}
			})
		}
	}
}

func TestAnEmptyModelSlotSkipsTheLeadingUnavailableNotice(t *testing.T) {
	a := placeApp(t)
	a.models = func() []Model {
		return []Model{
			{Unavailable: true, Notice: "no models", Group: "alpha"},
			{ID: "beta/first", Group: "beta", GroupOrder: 1},
		}
	}
	a.showPage(pageHome)
	a.target.model, a.model = "", ""
	a.openTargetPicker()
	if chosen, ok := a.target.pick.choice(); !ok || chosen.ID != "beta/first" {
		t.Fatalf("empty draft model opened on an unavailable notice, want beta/first: %+v", chosen)
	}
}

func TestTheCursorStaysVisibleAtTheBottomOfTheList(t *testing.T) {
	// A TRANSCRIPT THAT CAN SCROLL, so "the conversation did not move" is a real
	// assertion and not the accident of a short one.
	a := benchApp(20)
	// The assertion below inspects the cursor's ground. Pin a color palette
	// rather than inheriting a CI runner's terminal-free NoColor profile.
	a.pal = newPalette(tokens.ANSI256, false)
	a.models = func() []Model { return groupedCatalog }
	a.model = "beta/model-3"
	a.frame()
	// Park the transcript away from the live edge first, with the picker shut,
	// the way the wheel's own test does — an offset that never left 0 would
	// make the no-scroll assertion below read nothing.
	drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelUp})
	if a.offset == 0 {
		t.Fatal("the fixture transcript is too short to scroll")
	}
	typeLine(t, a, "/model")
	last := len(a.pick.list) - 1
	offset := a.offset

	// FAR MORE STEPS THAN THE LIST HAS ROWS, across every navigation there is:
	// repeated arrows, page keys and wheel notches all bottom out on the same
	// visible row.
	for i := 0; i < 30; i++ {
		drive(t, a, key("down"))
	}
	drive(t, a, key("pgdown"))
	for i := 0; i < 10; i++ {
		drive(t, a, tea.MouseWheelMsg{X: 4, Y: a.bodyTop() + 1, Button: tea.MouseWheelDown})
	}
	if a.pick.cursor != last {
		t.Fatalf("the cursor rests on %d of %d rows, want the last", a.pick.cursor, last)
	}
	// AND THE CONVERSATION BEHIND THE MENU DID NOT MOVE — not for the arrows,
	// not for the page key, not for the wheel.
	if a.offset != offset {
		t.Fatalf("navigation at the bottom moved the transcript: %d → %d", offset, a.offset)
	}

	// AND THE CURSOR'S ROW IS ON THE SCREEN. The window bottoms out with the
	// group headings inside it; the cursor is the last drawn row.
	lines := drawnCursorLines(a)
	if len(lines) != 1 {
		t.Fatalf("at the bottom the frame drew %d grounded rows, want exactly the cursor's:\n%s",
			len(lines), plain(strings.Join(modelMenuLines(a), "\n")))
	}
	lastID := a.pick.all[a.pick.hits[a.pick.list[last].hit]].ID
	if !strings.Contains(lines[0], lastID) {
		t.Fatalf("the cursor's ground is not on the last row (%s):\n%q", lastID, plain(lines[0]))
	}
	// AND THE WINDOW SHOWS THE END OF THE LIST, not a page that stops short:
	// the door rides the list's end, so it is on the screen too.
	shown := plain(strings.Join(modelMenuLines(a), "\n"))
	if !strings.Contains(shown, addProviderRowWord) {
		t.Fatalf("the list bottomed out above its own end:\n%s", shown)
	}
}
