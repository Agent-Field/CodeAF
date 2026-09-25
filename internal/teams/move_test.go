package teams

import (
	"strings"
	"testing"
)

// moveFile is harbor (managed, $10 a day) over dock, with api at the top
// level holding its own $3 cap, and a closed team shut beside them.
//
//	harbor ◆ boss   $10/day
//	  dock
//	api   $3/day    (web, srv)
//	shut  closed
func moveFile() *File {
	ten, three := 10.0, 3.0
	f := &File{Teams: []Team{
		{ID: "harbor", Name: "harbor", Members: []Member{{Key: "boss"}}, Manager: "boss",
			Settings: Settings{CapUSDDay: &ten}},
		{ID: "dock", Name: "dock", Parent: "harbor", Members: []Member{{Key: "crane"}}},
		{ID: "api", Name: "api", Members: []Member{{Key: "web"}, {Key: "srv"}}, Settings: Settings{CapUSDDay: &three}},
		{ID: "shut", Name: "shut", State: TeamClosed},
	}}
	tidy(f.Teams)
	return f
}

// WHERE A TEAM MAY GO: never into itself or under itself, never into a closed
// team, never past the depth limit, and a target it is already in is said.
func TestMoveCheckSaysWhyATargetIsBlocked(t *testing.T) {
	f := moveFile()
	d := Defaults{DepthLimit: 2}
	for _, c := range []struct {
		ids    []string
		parent string
		kind   string
	}{
		{[]string{"harbor"}, "harbor", MoveBlockSelf},
		{[]string{"harbor"}, "dock", MoveBlockInside},
		{[]string{"api"}, "shut", MoveBlockClosed},
		{[]string{"api"}, "dock", MoveBlockDepth},
		{[]string{"dock"}, "harbor", MoveBlockHere},
		{[]string{"nobody"}, "", MoveBlockGone},
	} {
		b, ok := f.MoveCheck(c.ids, c.parent, d)
		if ok || b.Kind != c.kind {
			t.Fatalf("%v into %q: got %+v ok %v, want %s", c.ids, c.parent, b, ok, c.kind)
		}
	}
	b, _ := f.MoveCheck([]string{"api"}, "dock", d)
	if b.Name != "dock" || b.Depth != 2 || b.Need != 1 || b.Limit != 2 || b.LimitFrom.Kind != OriginSettings {
		t.Fatalf("the depth block lacks its facts: %+v", b)
	}
	if _, ok := f.MoveCheck([]string{"api"}, "harbor", d); !ok {
		t.Fatal("api may not go into harbor")
	}
	// A team with a level under it needs two levels where it lands.
	if _, ok := f.MoveCheck([]string{"harbor"}, "api", d); ok {
		t.Fatal("harbor and dock under api stand three deep past a limit of two")
	}
	if _, ok := f.MoveCheck([]string{"dock"}, "", d); !ok {
		t.Fatal("dock may not go to the top level")
	}
}

// SEVERAL TEAMS MOVE AS ONE, and a team selected with its parent rides along
// inside it rather than being moved out on its own.
func TestMoveCarriesATeamSelectedWithItsParent(t *testing.T) {
	f := moveFile()
	if got := f.MoveRoots([]string{"dock", "harbor", "api"}); len(got) != 2 || got[0] != "harbor" || got[1] != "api" {
		t.Fatalf("roots of the selection: %v", got)
	}
	if err := f.Move([]string{"harbor", "dock"}, "api"); err != nil {
		t.Fatal(err)
	}
	h, _ := f.Team("harbor")
	k, _ := f.Team("dock")
	if h.Parent != "api" || k.Parent != "harbor" {
		t.Fatalf("harbor under %q, dock under %q", h.Parent, k.Parent)
	}
}

// THE TOP LEVEL IS THE ROOT when there is one: a move to "" writes the root.
func TestMoveToTheTopLevelLandsUnderTheRoot(t *testing.T) {
	f := moveFile()
	root := f.MakeRoot(f.Teams[0].Made)
	if got := f.MoveTarget(""); got != root {
		t.Fatalf("the top level is %q, want the root %q", got, root)
	}
	if b, ok := f.MoveCheck([]string{root}, "harbor", Defaults{DepthLimit: 5}); ok || b.Kind != MoveBlockRoot {
		t.Fatalf("the root may move: %+v", b)
	}
	if err := f.Move([]string{"dock"}, ""); err != nil {
		t.Fatal(err)
	}
	if k, _ := f.Team("dock"); k.Parent != root {
		t.Fatalf("dock at the top is under %q", k.Parent)
	}
}

// WHAT A MOVE CHANGES: api into harbor puts its conversations under harbor's
// manager, its spend in harbor's pool and its conflicts before harbor's
// manager; a move inside one unmanaged, uncapped place changes nothing.
func TestMoveEffectsNameAuthorityPoolAndJudge(t *testing.T) {
	f := moveFile()
	d := Defaults{DepthLimit: 5}
	e, err := f.MoveEffects([]string{"api"}, "harbor", d)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Reports) != 2 || e.Reports[0].Had || !e.Reports[0].Has || e.Reports[0].After.Team != "harbor" {
		t.Fatalf("the reports: %+v", e.Reports)
	}
	if len(e.Pools) != 1 || e.Pools[0].Before != "" || e.Pools[0].After != "harbor" || e.Pools[0].AfterCap != 10 {
		t.Fatalf("the pool: %+v", e.Pools)
	}
	if len(e.Judges) != 1 || e.Judges[0].Before != "" || e.Judges[0].After != "harbor" {
		t.Fatalf("the judge: %+v", e.Judges)
	}
	// The file itself did not move.
	if a, _ := f.Team("api"); a.Parent != "" {
		t.Fatal("MoveEffects moved the team")
	}
	// Two unmanaged teams at the top with no cap: nothing to ask.
	g := &File{Teams: []Team{{ID: "a", Name: "a", Members: []Member{{Key: "x"}}}, {ID: "b", Name: "b"}}}
	e, err = g.MoveEffects([]string{"a"}, "b", d)
	if err != nil || e.Changes() {
		t.Fatalf("a quiet move changes %+v (%v)", e, err)
	}
}

// A COMMITTED MOVE WRITES ONE TRAFFIC LINE PER AFFECTED TEAM PER MEMBER, and a
// refused move writes none. Writing the notices of that one move once is the
// whole log: the same pair of files does not grow a second copy inside
// MoveNotices, and a file that did not move has nothing to append.
func TestMoveTrafficIsAppendedOnceAndNeverOnARefusal(t *testing.T) {
	f := moveFile()
	f.Teams = append(f.Teams, Team{ID: "ops", Name: "ops", Members: []Member{{Key: "lead", Handle: "lead"}}})
	for i := range f.Teams {
		if f.Teams[i].ID == "api" {
			f.Teams[i].Parent = "ops"
			f.Teams[i].Members[0].Handle = "web"
			f.Teams[i].Members[1].Handle = "srv"
		}
	}
	tidy(f.Teams)
	before := f.tidyCopy()
	d := Defaults{DepthLimit: 5}
	if _, ok := f.MoveCheck([]string{"api"}, "shut", d); ok {
		t.Fatal("a closed team accepted the move")
	}
	if n := MoveNotices(before, f, []string{"api"}); len(n) != 0 {
		t.Fatalf("a refused move wrote %d lines", len(n))
	}
	if err := f.Move([]string{"api"}, "api"); err == nil {
		t.Fatal("a team moved inside itself")
	}
	if n := MoveNotices(before, f, []string{"api"}); len(n) != 0 {
		t.Fatalf("a move the store refused wrote %d lines", len(n))
	}
	if err := f.Move([]string{"api"}, "harbor"); err != nil {
		t.Fatal(err)
	}
	notes := MoveNotices(before, f, []string{"api"})
	want := map[string][]string{
		"api":    {"@web moved to harbor", "@srv moved to harbor"},
		"ops":    {"@web moved to harbor", "@srv moved to harbor"},
		"harbor": {"@web joined from ops", "@srv joined from ops"},
	}
	if len(notes) != 6 {
		t.Fatalf("got %d notices, want 6: %+v", len(notes), textsOf(notes))
	}
	got := map[string][]string{}
	for _, n := range notes {
		if n.Entry.Kind != KindEvent || n.Entry.From != FromSystem || n.Entry.To != ToEveryone || n.Entry.State != "" {
			t.Fatalf("a move line is not an ordinary event: %+v", n.Entry)
		}
		got[n.Team] = append(got[n.Team], n.Entry.Text)
	}
	for team, lines := range want {
		if strings.Join(got[team], "\n") != strings.Join(lines, "\n") {
			t.Fatalf("%s traffic: %q, want %q", team, got[team], lines)
		}
	}
	dir := t.TempDir()
	if err := WriteMoveNotices(dir, notes); err != nil {
		t.Fatal(err)
	}
	for team, lines := range want {
		log, err := ReadTraffic(dir, team, "", 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(log) != len(lines) {
			t.Fatalf("%s has %d lines, want %d (written once)", team, len(log), len(lines))
		}
		for i, line := range lines {
			if log[i].Text != line {
				t.Fatalf("%s line %d is %q, want %q", team, i, log[i].Text, line)
			}
		}
	}
	// The move already happened: asking again about the file as it stands
	// adds nothing, so a second commit of the same move cannot double the log.
	if n := MoveNotices(f, f, []string{"api"}); len(n) != 0 {
		t.Fatalf("the move that already landed wrote %d more lines", len(n))
	}
}

// A CONVERSATION MOVED FROM ONE TEAM TO ANOTHER is one line on each side, and
// a transfer that did not happen (the same membership) is none.
func TestMemberMoveTrafficIsAppendedOnceAndNeverOnARefusal(t *testing.T) {
	before := &File{Teams: []Team{
		{ID: "ops", Name: "ops", Members: []Member{{Key: "k", Handle: "web"}}},
		{ID: "harbor", Name: "harbor"},
	}}
	if n := MemberMoveNotices(before, before); len(n) != 0 {
		t.Fatalf("a refused transfer wrote %+v", textsOf(n))
	}
	after := before.tidyCopy()
	if err := after.AddMember("harbor", Member{Key: "k", Handle: "web"}); err != nil {
		t.Fatal(err)
	}
	if err := after.RemoveMember("ops", "k"); err != nil {
		t.Fatal(err)
	}
	notes := MemberMoveNotices(before, after)
	if len(notes) != 2 {
		t.Fatalf("got %+v", textsOf(notes))
	}
	by := map[string]string{}
	for _, n := range notes {
		by[n.Team] = n.Entry.Text
	}
	if by["ops"] != "@web moved to harbor" || by["harbor"] != "@web joined from ops" {
		t.Fatalf("texts %+v", by)
	}
	dir := t.TempDir()
	if err := WriteMoveNotices(dir, notes); err != nil {
		t.Fatal(err)
	}
	log, err := ReadTraffic(dir, "ops", "", 0)
	if err != nil || len(log) != 1 || log[0].Text != "@web moved to harbor" {
		t.Fatalf("ops log %+v (%v)", log, err)
	}
}

func textsOf(notes []MoveNotice) []string {
	out := make([]string, len(notes))
	for i, n := range notes {
		out[i] = n.Team + ": " + n.Entry.Text
	}
	return out
}
