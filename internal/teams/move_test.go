package teams

import "testing"

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
