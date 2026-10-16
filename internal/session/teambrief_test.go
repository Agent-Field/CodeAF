package session

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// A BRIEF CHANGED IN PLACE REACHES ONLY ITS MEMBER, AT ITS NEXT STEP: it is a
// delivered line that wakes nobody, the manager never reads it back, and the
// surface reads it as a brief line naming who changed it and its log entry.
func TestBriefChangedIsDeliveredToItsMemberAndWakesNobody(t *testing.T) {
	member := teamRole{id: "t", name: "Release", handle: "review", managed: true}
	other := teamRole{id: "t", name: "Release", handle: "backend", managed: true}
	manager := teamRole{id: "t", name: "Release", handle: "release", manager: true, managed: true}
	entry := teams.Entry{ID: "000000000012", Kind: teams.KindBrief, From: teams.FromYou, To: "review", ToKey: "krev", Text: "Check the retry backoff.\nSkip the e2e suite."}
	line := teamLine(member, entry)
	if line != "◆ brief changed by the person #12: Check the retry backoff.\n    Skip the e2e suite." {
		t.Fatalf("line = %q", line)
	}
	if teamLine(other, entry) != "" || teamLine(manager, entry) != "" {
		t.Fatal("a brief reached someone else")
	}
	for _, r := range []teamRole{member, manager} {
		if teamWakes(r, entry) {
			t.Fatalf("%s was woken by a brief", r.handle)
		}
	}
	// What the surface reads back from the delivery.
	got := teamNewsLines(teamNewsGroup(member, []string{line}))
	want := TeamLine{Team: "Release", From: teams.FromYou, Kind: teams.KindBrief, Text: "Check the retry backoff.\nSkip the e2e suite.", Thread: "000000000012"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("read back %+v, want %+v", got, want)
	}
	byManager := entry
	byManager.From = teams.FromManager
	if got := teamNewsLines(teamNewsGroup(member, []string{teamLine(member, byManager)})); len(got) != 1 || got[0].From != teams.FromManager || got[0].Kind != teams.KindBrief {
		t.Fatalf("manager's change read back %+v", got)
	}
}
