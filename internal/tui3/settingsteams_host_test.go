package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// AN EDIT OVER A HOST SEAM LANDS IN THE FAR PROFILE and nowhere on this
// machine. The row says where the value came from in the same words the team
// card uses for a value inherited from these defaults.
func TestTeamsSettingsOverAHostSeamLandInTheFarProfile(t *testing.T) {
	a := placeApp(t)
	laptop := a.profileDir
	far := t.TempDir()
	a.host = "spark"
	a.teamsDisk.door = localTeams(far, &a.teamsDisk.watch)
	before := teamstore.DefaultsAt(laptop)
	drive(t, a, key(placeChord(pageSettings)))
	openTeamsTab(t, a)
	if a.sheet.farTeams == nil || !a.sheet.teamDefaultsWrite {
		t.Fatal("the Teams tab did not take the far defaults as editable")
	}
	if note := a.sheet.footNote(); !strings.Contains(note, "saved on spark") {
		t.Fatalf("the Teams tab says %q", note)
	}
	if screen := placeFrameText(a); !strings.Contains(screen, "from Settings") {
		t.Fatalf("the row does not say where the value came from:\n%s", screen)
	}
	if item, ok := a.sheet.current(); !ok || item.row.Key != config.KeyTeamsQuestionsUp {
		t.Fatalf("the cursor is not on questions: %+v", item.row.Key)
	}
	drive(t, a, key("enter"))
	got := teamstore.DefaultsAt(far)
	if got.QuestionsUp {
		t.Fatal("the far profile still has questions going up")
	}
	if after := teamstore.DefaultsAt(laptop); after != before {
		t.Fatalf("the laptop profile changed: %+v", after)
	}
	if words := (&teamstore.File{}).Effective("missing", got).QuestionsUpFrom.Words(); words != "from Settings" {
		t.Fatalf("provenance %q", words)
	}

	drive(t, a, key("down"), key("down"), key("enter"), key("4"), key("enter"))
	if cap := teamstore.DefaultsAt(far).CapUSDDay; cap != 4 {
		t.Fatalf("the far cap is %v", cap)
	}
	if teamstore.DefaultsAt(laptop) != before {
		t.Fatal("the cap write touched the laptop profile")
	}
}

// AN OLDER ENGINE KEEPS THE TAB READ-ONLY and says so in one line. The far
// profile is not written, and neither is this machine's.
func TestTeamsSettingsOverAnOlderEngineStayReadOnly(t *testing.T) {
	a := placeApp(t)
	laptop := a.profileDir
	far := t.TempDir()
	door := localTeams(far, &a.teamsDisk.watch)
	door.ApplyDefault = nil
	a.host = "spark"
	a.teamsDisk.door = door
	beforeLap := teamstore.DefaultsAt(laptop)
	beforeFar := teamstore.DefaultsAt(far)
	drive(t, a, key(placeChord(pageSettings)))
	openTeamsTab(t, a)
	if a.sheet.teamDefaultsWrite {
		t.Fatal("an older engine's Teams tab is editable")
	}
	if a.sheet.farTeams == nil {
		t.Fatal("the tab did not show the far defaults it can still read")
	}
	if note := a.sheet.footNote(); !strings.Contains(note, "not available over this connection") {
		t.Fatalf("the Teams tab says %q", note)
	}
	before := a.sheet.items[a.sheet.cursor].row.Value()
	drive(t, a, key("enter"))
	if a.sheet.edit != nil || !strings.Contains(a.sheet.msg, "not available over this connection") {
		t.Fatalf("a Teams row took an edit (msg %q)", a.sheet.msg)
	}
	if after := a.sheet.items[a.sheet.cursor].row.Value(); after != before {
		t.Fatalf("the row changed from %q to %q", before, after)
	}
	if teamstore.DefaultsAt(far) != beforeFar || teamstore.DefaultsAt(laptop) != beforeLap {
		t.Fatal("a refused edit was written")
	}
}

func openTeamsTab(t *testing.T, a *app) {
	t.Helper()
	for i, title := range settingTabs {
		if title == tabTeams {
			a.sheet.tab = i
		}
	}
	a.sheet.cursor = 0
	a.sheet.build()
}
