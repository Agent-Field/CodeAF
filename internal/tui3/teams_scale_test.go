package tui3

import (
	"fmt"
	"reflect"
	"testing"
)

func teamsScaleApp(tb testing.TB, count, members int) *app {
	tb.Helper()
	a := teamsPerfApp(tb, 0)
	a.tp.sel = teamsAllRow
	a.wall.teams = nil
	for i := 0; i < count; i++ {
		tm := team{ID: fmt.Sprintf("scale-%03d", i), Name: fmt.Sprintf("Scale team %03d", i)}
		for j := 0; j < members; j++ {
			key := fmt.Sprintf("scale-%03d-member-%03d", i, j)
			if j == 0 {
				tm.Manager = key
			}
			tm.Members = append(tm.Members, teamMember{Key: key, File: key, Handle: key, Word: key})
			a.tp.previews[key] = teamsPreview{count: 2, messages: [2]teamsPreviewMessage{{role: "user", text: "Review this fictional change"}, {role: "assistant", text: "The fictional change is ready for review."}}}
		}
		a.wall.teams = append(a.wall.teams, tm)
	}
	return a
}

func TestTeamsPaintHomesMatchFreshCrewAndExpireAfterFrame(t *testing.T) {
	a := teamsScaleApp(t, 3, 8)
	m := a.wall.teams[0].Members[1]
	m.Home = true
	a.wall.teams[0].Members[1] = m
	a.wall.teams[1].Members = append(a.wall.teams[1].Members, m)
	a.wall.teams[1].Members[len(a.wall.teams[1].Members)-1].Home = false
	want := a.teamsCrew(a.wall.teams[1])
	a.tp.homes = a.teamTree().Homes()
	if got := a.teamsCrew(a.wall.teams[1]); !reflect.DeepEqual(got, want) {
		t.Fatal("batched home changed shared member's reporting label")
	}
	a.tp.homes = nil
	a.teamsBody(a.width, 40)
	if a.tp.homes != nil {
		t.Fatal("paint retained membership facts across edits")
	}
	a.wall.teams[0].Members[1].Home = false
	a.wall.teams[1].Members[len(a.wall.teams[1].Members)-1].Home = true
	a.tp.homes = a.teamTree().Homes()
	got := a.teamsCrew(a.wall.teams[1])
	a.tp.homes = nil
	if !reflect.DeepEqual(got, a.teamsCrew(a.wall.teams[1])) {
		t.Fatal("membership edit retained an earlier reporting label")
	}
}

func BenchmarkTeamsAllScale(b *testing.B) {
	for _, count := range []int{10, 100, 200} {
		b.Run(fmt.Sprintf("teams=%d/members=100", count), func(b *testing.B) {
			a := teamsScaleApp(b, count, 100)
			room := a.height - placeHeadRows - placeFootRowsFor(pageTeams, a.height)
			a.teamsBody(a.width, room)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				a.tp.hot = teamsRef{act: teamsActSelect, id: fmt.Sprintf("scale-%03d", i%2), arg: "overview"}
				a.teamsBody(a.width, room)
			}
		})
	}
}
