package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestTeamsMembershipPasteStaysInItsModalAndPreservesDraft(t *testing.T) {
	for _, mode := range []string{"filter", "new", "remove", "manager"} {
		a, id, _ := teamsPlaceLabIDs(t)
		a.input.setText("Original draft")
		a.tmembers = teamMembershipSheet{on: true, team: id, new: mode == "new", removing: mode == "remove", choosingManager: mode == "manager"}
		drive(t, a, tea.PasteMsg{Content: "Pasted\r\nwords"})
		want := "Pasted words"
		if mode == "remove" {
			want = ""
		}
		if a.tmembers.filter.String() != want || a.input.String() != "Original draft" {
			t.Fatalf("%s clipboard escaped modal", mode)
		}
		drive(t, a, key("esc"))
		if a.tmembers.on || a.input.String() != "Original draft" {
			t.Fatal("cancel changed underlying draft")
		}
	}
}

func TestTeamsDialogsBoundLongUnicodeNamesToTheFrame(t *testing.T) {
	for _, width := range []int{40, 80, 160} {
		a, id, _ := teamsPlaceLabIDs(t)
		a.width, a.height = width, 32
		for i := range a.wall.teams {
			if a.wall.teams[i].ID == id {
				a.wall.teams[i].Name = strings.Repeat("長いチーム名 ", 30)
			}
		}
		for _, mode := range []string{"create", "add", "manager", "new"} {
			a.tcreate = teamCreateSheet{}
			a.tmembers = teamMembershipSheet{}
			switch mode {
			case "create":
				a.tcreate.on = true
				a.tcreate.parent = id
			default:
				a.tmembers = teamMembershipSheet{on: true, team: id, choosingManager: mode == "manager", new: mode == "new"}
			}
			a.touch()
			for _, line := range strings.Split(teamsFrameText(a), "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s spilled at %d: %q", mode, width, line)
				}
			}
		}
	}
}

func TestTeamsCrossPaneArrowsUseVisibleRowsAfterIndependentScrolling(t *testing.T) {
	a, _, _ := teamsPlaceLabIDs(t)
	for i := 0; i < 24; i++ {
		a.wall.teams = append(a.wall.teams, team{ID: "extra-" + itoa(i), Name: "Extra " + itoa(i)})
	}
	a.width, a.height = 150, 32
	a.tp.sel = teamsAllRow
	a.tp.cur = teamsRef{act: teamsActSelect, id: teamsAllRow}
	a.touch()
	teamsFrameText(a)
	for i := 0; i < 7; i++ {
		drive(t, a, tea.MouseWheelMsg{X: a.tp.railW + 2, Y: placeHeadRows + 1, Button: tea.MouseWheelDown})
		teamsFrameText(a)
	}
	var from teamsTarget
	for _, target := range a.tp.targets {
		if target.pane && target.y >= placeHeadRows && target.y < a.height-2 {
			from = target
			break
		}
	}
	if from.line < 15 {
		t.Fatal("fixture did not independently scroll main pane")
	}
	a.tp.cur = from.ref()
	if !a.teamsWalk(-1, 0) {
		t.Fatal("cannot cross into sidebar")
	}
	var selected teamsTarget
	best := int(^uint(0) >> 1)
	for _, target := range a.tp.targets {
		if !target.pane {
			if d := abs(target.y - from.y); d < best {
				best = d
			}
			if target.ref() == a.tp.cur {
				selected = target
			}
		}
	}
	if selected.pane || abs(selected.y-from.y) != best {
		t.Fatalf("cross-pane arrow chose hidden logical row: from %+v to %+v", from, selected)
	}
}
