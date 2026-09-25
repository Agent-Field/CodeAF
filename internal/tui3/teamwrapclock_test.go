package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// setWrap puts a wrap-up that began at started with bound on team id, the way
// the manager's session writes it to teams.json.
func setWrap(t *testing.T, a *app, id string, started time.Time, bound time.Duration) {
	t.Helper()
	if err := a.teamEdit(func(f *teamstore.File) error {
		i := teamIndex(f.Teams, id)
		if i < 0 {
			return nil
		}
		f.Teams[i].Wrap = &teamstore.Wrap{Started: started, Bound: bound}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Contract 5.1, 5.4 and 5.5: the teams page's header says how long a team that
// is wrapping up has left, moves on the clock alone, and says nothing for a
// team with no wrap-up.
func TestTheTeamsHeaderSaysHowLongAWrapUpHasLeft(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	base := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	now := base.Add(3 * time.Minute)
	a.clock = func() time.Time { return now }
	setWrap(t, a, harbor, base, 15*time.Minute)
	drive(t, a, runCmd(a.teamsSelect(harbor))...)
	if text := teamsFrameText(a); !strings.Contains(text, "wrapping up · 12m left") {
		t.Fatalf("the header does not say the wrap-up's time left:\n%s", text)
	}
	now = base.Add(14*time.Minute + 30*time.Second)
	if text := teamsFrameText(a); !strings.Contains(text, "wrapping up · under a minute left") {
		t.Fatalf("the header did not move with the clock:\n%s", text)
	}
	now = base.Add(16 * time.Minute)
	if text := teamsFrameText(a); !strings.Contains(text, "wrapping up · out of time") {
		t.Fatalf("the header does not say the wrap-up is out of time:\n%s", text)
	}
	drive(t, a, runCmd(a.teamsSelect(orbit))...)
	if text := teamsFrameText(a); strings.Contains(text, "wrapping up") {
		t.Fatalf("a team with no wrap-up says it is wrapping up:\n%s", text)
	}
}

// Contract 5.3: a wrap-up from a file written before the bound was stored says
// it is wrapping up and names no time.
func TestAWrapUpWithNoBoundNamesNoTime(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	setWrap(t, a, harbor, a.now().Add(-time.Minute), 0)
	drive(t, a, runCmd(a.teamsSelect(harbor))...)
	text := teamsFrameText(a)
	if !strings.Contains(text, "wrapping up") || strings.Contains(text, "left") || strings.Contains(text, "out of time") {
		t.Fatalf("a boundless wrap-up named a time:\n%s", text)
	}
}

// Contract 5.2: the manager's side column says the same words as the header.
func TestTheManagersColumnSaysHowLongAWrapUpHasLeft(t *testing.T) {
	a := managerColumnApp(t)
	team, kind, _ := a.sideTeam()
	if kind != sideKindManager {
		t.Fatal("the manager is not in front")
	}
	base := time.Date(2026, 9, 25, 15, 0, 0, 0, time.UTC)
	now := base.Add(3 * time.Minute)
	a.clock = func() time.Time { return now }
	draw := func() string {
		lines, _ := a.sideTrafficView(30)
		var rows []string
		for _, l := range lines {
			rows = append(rows, l.text)
		}
		return ansi.Strip(strings.Join(rows, "\n"))
	}
	if got := draw(); strings.Contains(got, "wrapping up") {
		t.Fatalf("a team with no wrap-up says it is wrapping up:\n%s", got)
	}
	setWrap(t, a, team.ID, base, 15*time.Minute)
	if got := draw(); !strings.Contains(got, "wrapping up · 12m left") {
		t.Fatalf("the manager's column does not say the wrap-up's time left:\n%s", got)
	}
	now = base.Add(5 * time.Minute)
	if got := draw(); !strings.Contains(got, "wrapping up · 10m left") {
		t.Fatalf("the manager's column did not move with the clock:\n%s", got)
	}
}
