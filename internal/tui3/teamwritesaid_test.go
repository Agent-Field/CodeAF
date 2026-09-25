package tui3

import (
	"errors"
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// refusingSeam is this machine's teams seam with its write swapped for one
// that hands every change the file as fake holds it and answers err, or, when
// err is nil, whatever the changes made of it.
func refusingSeam(a *app, err error) TeamsSeam {
	seam := localTeams(a.profileDir, &a.teamsDisk.watch)
	seam.Update = func(change func(*teamstore.File) error) ([]teamstore.Team, string, error) {
		if err != nil {
			return nil, "", err
		}
		f := &teamstore.File{}
		if cerr := change(f); cerr != nil {
			return nil, "", cerr
		}
		return f.Teams, "stamp", nil
	}
	return seam
}

// wallMakeFromFront opens the wall, picks the first tile and makes a team
// called name from it, as the card's Create does, without running the write.
func wallMakeFromFront(t *testing.T, a *app, name string) {
	t.Helper()
	_ = a.openWall()
	tiles := a.wallShown(a.now())
	if len(tiles) == 0 {
		t.Fatal("no tiles")
	}
	a.wall.marked = map[string]bool{tiles[0].tab.key: true}
	a.wall.name = name
	_ = a.wallMakeTeam(a.wallShown(a.now()))
}

// Contract 2.1 and 2.2: a team the store refused to save is never said to be
// made. Before the write comes back the row says nothing; after a refusal it
// says the team was not saved, in place of `Made`.
func TestAWallTeamTheStoreRefusedIsNeverSaidToBeMade(t *testing.T) {
	a, _, _ := tabApp(t)
	a.teamsDisk.door = refusingSeam(a, teamstore.ErrBusy)
	wallMakeFromFront(t, a, "beta")
	if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); strings.Contains(frame, "Made beta") {
		t.Fatalf("the wall said the team was made before the store took it:\n%s", frame)
	}
	drive(t, a, runCmd(a.teamsWrite())...)
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if strings.Contains(frame, "Made beta") {
		t.Fatalf("the wall said a refused team was made:\n%s", frame)
	}
	if !strings.Contains(frame, "beta was not saved") {
		t.Fatalf("the wall did not say the team was not saved:\n%s", frame)
	}
	// Contract 2.4: the team stays in this window.
	if teamNamed(a.wall.teams, "beta") < 0 {
		t.Fatal("the refused team left the window")
	}
}

// Contract 2.1: a team the store took says `Made` once the write is back.
func TestAWallTeamSaysMadeOnlyOnceTheStoreTookIt(t *testing.T) {
	a, _, _ := tabApp(t)
	a.teamsDisk.door = refusingSeam(a, nil)
	wallMakeFromFront(t, a, "gamma")
	if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); strings.Contains(frame, "Made gamma") {
		t.Fatalf("the wall said the team was made before the store took it:\n%s", frame)
	}
	drive(t, a, runCmd(a.teamsWrite())...)
	if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); !strings.Contains(frame, "Made gamma · 1") {
		t.Fatalf("the wall did not say the team was made:\n%s", frame)
	}
}

// Contract 2.3: an edit the store took is not blamed for a sibling edit the
// store refused in the same write.
func TestATakenTeamIsNotBlamedForASiblingEditsRefusal(t *testing.T) {
	a, _, _ := tabApp(t)
	a.teamsDisk.door = refusingSeam(a, nil)
	wallMakeFromFront(t, a, "delta")
	// This edit holds in the window (which is at the current version) and is
	// refused by the store's file (a fake at version 0).
	if err := a.teamEdit(func(f *teamstore.File) error {
		if f.Version == 0 {
			return errors.New("the file moved")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamsWrite())...)
	frame := wallPlainFrame(a.wallFrame(a.width, a.height))
	if !strings.Contains(frame, "Made delta · 1") || strings.Contains(frame, "delta was not saved") {
		t.Fatalf("the made team was blamed for another edit's refusal:\n%s", frame)
	}
}

// Contract 2.1 and 2.2: Organize's `Organized` is said only once the store took
// it, and a refused Apply says it was not saved instead.
func TestARefusedOrganizeIsNeverSaidToBeOrganized(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		a, _, _ := tabApp(t)
		var err error
		if refuse {
			err = teamstore.ErrBusy
		}
		a.teamsDisk.door = refusingSeam(a, err)
		_ = a.openWall()
		tiles := a.wallShown(a.now())
		o := &a.wall.org
		o.on, o.props = true, []orgProp{{name: "sorted", keys: []string{tiles[0].tab.key}, names: []string{tiles[0].name}, take: true}}
		a.wallOrganizeApply()
		if frame := wallPlainFrame(a.wallFrame(a.width, a.height)); strings.Contains(frame, "Organized ·") {
			t.Fatalf("refuse=%v: Organized was said before the store took it:\n%s", refuse, frame)
		}
		drive(t, a, runCmd(a.teamsWrite())...)
		frame := wallPlainFrame(a.wallFrame(a.width, a.height))
		switch {
		case refuse && (strings.Contains(frame, "Organized ·") || !strings.Contains(frame, "not saved")):
			t.Fatalf("a refused Organize said it was done, or not that it was refused:\n%s", frame)
		case !refuse && !strings.Contains(frame, "Organized · 1 new team"):
			t.Fatalf("a taken Organize did not say so:\n%s", frame)
		}
	}
}

// Contract 2.1 and 2.2: the teams page says a team is closed only once the
// store took the close, and says the close was not saved when it refused.
func TestTheTeamsPageSaysAClosedTeamOnlyOnceTheStoreTookIt(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		a, harbor, _ := teamsPlaceLabIDs(t)
		if refuse {
			a.teamsDisk.door = refusingSeam(a, teamstore.ErrBusy)
		}
		_ = a.teamsCloseNow(harbor, "")
		if text := teamsFrameText(a); strings.Contains(text, "harbor is closed") {
			t.Fatalf("refuse=%v: the page said harbor is closed before the store took it:\n%s", refuse, text)
		}
		drive(t, a, runCmd(a.teamsWrite())...)
		text := teamsFrameText(a)
		switch {
		case refuse && (strings.Contains(text, "harbor is closed") || !strings.Contains(text, "the close of harbor was not saved")):
			t.Fatalf("a refused close said it closed, or not that it was refused:\n%s", text)
		case !refuse && !strings.Contains(text, "harbor is closed"):
			t.Fatalf("a taken close was not said:\n%s", text)
		}
	}
}
