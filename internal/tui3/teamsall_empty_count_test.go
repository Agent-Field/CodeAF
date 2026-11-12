package tui3

import (
	"strings"
	"testing"
)

func TestTeamOverviewOmitsAnEmptyConversationCount(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		a, harbor, _ := teamsPlaceLabIDs(t)
		a.width, a.height = 160, 60
		for i := range a.wall.teams {
			if a.wall.teams[i].ID == harbor {
				a.wall.teams[i].Members = make([]teamMember, count)
			}
		}
		a.tp.sel = teamsAllRow
		text := teamsFrameText(a)
		if strings.Contains(text, "0 conversations") {
			t.Fatal("empty team shows a zero count: " + text)
		}
		want := "1 conversation"
		if count == 2 {
			want = "2 conversations"
		}
		if count > 0 && !strings.Contains(text, want) {
			t.Fatalf("team with %d conversations omits %q: %s", count, want, text)
		}
	}
}
