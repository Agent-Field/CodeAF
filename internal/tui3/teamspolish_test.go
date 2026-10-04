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
		if mode == "remove" || mode == "manager" {
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
