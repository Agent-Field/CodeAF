package tui3

import (
	"strings"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE TEAMS PAGE'S LAB ────────────────────────────────────────────────────

// teamsPlaceLab is the teams page over three conversations in two teams, orbit
// nested under harbor, with harbor (the conversation in front) selected and no
// manager yet, so the page draws the shared frame.
func teamsPlaceLab(t *testing.T) *app {
	t.Helper()
	a, _, _ := teamsPlaceLabIDs(t)
	return a
}

// teamsPlaceLabIDs is [teamsPlaceLab] with the two teams' ids.
func teamsPlaceLabIDs(t *testing.T) (a *app, harbor, orbit string) {
	t.Helper()
	a, harbor, orbit = menuApp(t)
	a.profileDir = t.TempDir()
	if err := a.teamEdit(func(f *teamstore.File) error { return f.SetParent(orbit, harbor) }); err != nil {
		t.Fatal(err)
	}
	a.width, a.height = 120, 24
	if cmd := a.showPage(pageTeams); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageTeams) {
		t.Fatal("the teams place did not open")
	}
	a.teamsSync()
	a.frame()
	return a, harbor, orbit
}

// teamsHits is, for each row of the last frame, the index of the target the
// keyboard walks on it (the cursor's own where it is on that row), and -1 for
// a row with none.
func teamsHits(a *app) []int {
	a.frame()
	hits := make([]int, a.height)
	for i := range hits {
		hits[i] = -1
	}
	cur := a.teamsCursorIndex()
	for i, tg := range a.tp.targets {
		if tg.y < 0 || tg.y >= len(hits) {
			continue
		}
		if hits[tg.y] < 0 || i == cur {
			hits[tg.y] = i
		}
	}
	return hits
}

// teamsFrameText is the whole frame, plain.
func teamsFrameText(a *app) string {
	f, _, _ := a.frame()
	return plain(f)
}

// teamsTargetOf is the first drawn target of act (and id, when given).
func teamsTargetOf(t *testing.T, a *app, act teamsAct, id string) teamsTarget {
	t.Helper()
	a.frame()
	for _, tg := range a.tp.targets {
		if tg.act == act && (id == "" || tg.id == id) {
			return tg
		}
	}
	t.Fatalf("no target %d %q on the frame:\n%s", act, id, teamsFrameText(a))
	return teamsTarget{}
}

// ── the place ───────────────────────────────────────────────────────────────

// TEAMS IS THE SECOND PLACE, right after home, on the bar and on the digits
// (ruling c-2), and /teams opens it too.
func TestTeamsIsTheSecondPlaceOnTheBarTheDigitsAndTheCommand(t *testing.T) {
	if placeOrder[1] != pageTeams {
		t.Fatalf("the second place is %q", placeOrder[1].word())
	}
	a := placeApp(t)
	bar := plain(a.placeTabBar(120, false, a.pal))
	if !strings.Contains(bar, "home   teams   sessions") {
		t.Fatalf("the bar does not put teams after home: %q", bar)
	}
	drive(t, a, key("alt+2"))
	if !a.at(pageTeams) {
		t.Fatalf("alt+2 landed on %q", a.page.word())
	}
	a.leavePlace()
	typeLine(t, a, "/teams")
	if !a.at(pageTeams) {
		t.Fatalf("/teams landed on %q", a.page.word())
	}
}

// NO TEAMS IS ONE SENTENCE AND TWO BUTTONS, and the organize button opens the
// wall's proposal.
func TestTeamsWithNoTeamsSaysWhatTheyAreAndOffersTwoWays(t *testing.T) {
	a := placeApp(t)
	a.width, a.height = 110, 24
	drive(t, a, key("alt+2"))
	text := teamsFrameText(a)
	for _, want := range []string{"A team is a set of conversations", teamsOrganizeWord, teamsNewTeamWord} {
		if !strings.Contains(text, want) {
			t.Fatalf("the empty page lost %q:\n%s", want, text)
		}
	}
}

// THE RAIL IS THE TREE: orbit under harbor, indented, and the needs-you mark
// only when a packet waits on the person.
func TestTeamsRailIsTheTreeWithMarksOnlyWhenSomethingHappens(t *testing.T) {
	a, harbor, orbit := teamsPlaceLabIDs(t)
	rail := func() []string {
		lines := strings.Split(teamsFrameText(a), "\n")
		for i, l := range lines {
			if at := strings.Index(l, "│"); at >= 0 {
				lines[i] = l[:at]
			}
		}
		return lines
	}
	lines := rail()
	hy, oy := -1, -1
	for y, l := range lines {
		if hy < 0 && strings.Contains(l, "harbor") {
			hy = y
		}
		if oy < 0 && strings.Contains(l, "orbit") {
			oy = y
		}
	}
	if hy < 0 || oy <= hy {
		t.Fatalf("harbor at %d, orbit at %d:\n%s", hy, oy, strings.Join(lines, "\n"))
	}
	if strings.Index(lines[oy], "orbit") <= strings.Index(lines[hy], "harbor") {
		t.Fatalf("orbit is not indented under harbor:\n%s\n%s", lines[hy], lines[oy])
	}
	if strings.Contains(lines[hy], "?") || strings.Contains(lines[oy], "?") {
		t.Fatalf("a quiet team carries a mark:\n%s", strings.Join(lines, "\n"))
	}
	a.tp.packets = []teamstore.Packet{{ID: "p1", Team: teamstore.Person, Origin: orbit,
		Kind: teamstore.PacketQuestion, RaisedBy: "boss", Question: "Friday or Monday?",
		State: teamstore.PacketOpen}}
	a.tp.top = teamsTopCache{}
	a.touch()
	lines = rail()
	if !strings.Contains(lines[oy], "? 1") {
		t.Fatalf("orbit does not say a packet waits on you:\n%s", lines[oy])
	}
	_ = harbor
}

// THE INBOX CARD DECIDES A PACKET with one press on an option's word.
func TestTeamsInboxCardDecidesAPacket(t *testing.T) {
	a, harbor, _ := teamsPlaceLabIDs(t)
	flushTeams(t, a)
	seam := a.teamsSeam()
	p, err := seam.Raise(teamstore.Packet{Team: teamstore.Person, Origin: harbor,
		Kind: teamstore.PacketConflict, RaisedBy: "boss", Question: "Which parser wins?",
		Options: []teamstore.Option{{ID: "a", Label: "Keep the old one", Consequence: "no churn"},
			{ID: "b", Label: "Take the new one", Consequence: "two files move"}},
		Recommendation: &teamstore.Recommendation{Option: "b", Reason: "it is faster"}})
	if err != nil {
		t.Fatal(err)
	}
	drive(t, a, runCmd(a.teamsRead(false))...)
	text := teamsFrameText(a)
	for _, want := range []string{"Which parser wins?", "Keep the old one", "Take the new one", "recommended"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the card lost %q:\n%s", want, text)
		}
	}
	var opt teamsTarget
	for _, tg := range a.tp.targets {
		if tg.act == teamsActOption && tg.arg == p.ID && tg.opt == "b" {
			opt = tg
		}
	}
	if opt.opt == "" {
		t.Fatalf("no button for the recommended option:\n%s", text)
	}
	drive(t, a, runCmd(a.teamsDo(opt))...)
	list, _, _, err := seam.Packets(teamstore.ScopeAll, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range list {
		if q.ID == p.ID && q.Waiting() {
			t.Fatalf("the press did not decide the packet: %+v", q)
		}
	}
}

// flushTeams writes the lab's in-memory edits to its profile.
func flushTeams(t *testing.T, a *app) {
	t.Helper()
	if cmd := a.teamsWrite(); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
}
