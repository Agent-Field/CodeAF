package teams

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ptrF(v float64) *float64 { return &v }
func ptrI(v int) *int         { return &v }
func ptrB(v bool) *bool       { return &v }

// tree is harbor > dock > pier, and a separate top-level team yard.
func tree() *File {
	return &File{Version: Version, Teams: []Team{
		{ID: "aaaaaaaaaaaa", Name: "harbor", Members: []Member{{Key: "hm", Word: "harbor boss"}}},
		{ID: "bbbbbbbbbbbb", Name: "dock", Parent: "aaaaaaaaaaaa"},
		{ID: "cccccccccccc", Name: "pier", Parent: "bbbbbbbbbbbb"},
		{ID: "dddddddddddd", Name: "yard"},
	}}
}

var defaults = Defaults{QuestionsUp: true, CapUSDDay: 5, DepthLimit: 3, SubShare: 0.5}

// AN UNSET VALUE IS INHERITED, AND THE RESOLVER SAYS FROM WHERE. pier sets
// nothing: questions come from Settings, the cap from harbor two levels up,
// the share from dock one level up; its own depth override is its own.
func TestEffectiveWalksTheChainAndNamesEachOrigin(t *testing.T) {
	f := tree()
	must(t, f.SetSettings("aaaaaaaaaaaa", func(s *Settings) { s.CapUSDDay = ptrF(10) }))
	must(t, f.SetSettings("bbbbbbbbbbbb", func(s *Settings) { s.SubShare = ptrF(0.25) }))
	must(t, f.SetSettings("cccccccccccc", func(s *Settings) { s.DepthLimit = ptrI(4) }))

	e := f.Effective("cccccccccccc", defaults)
	if !e.QuestionsUp || e.QuestionsUpFrom.Kind != OriginSettings || e.QuestionsUpFrom.Words() != "from Settings" {
		t.Fatalf("questions: %+v", e)
	}
	if e.CapUSDDay != 10 || e.CapFrom.Kind != OriginAncestor || e.CapFrom.Team != "aaaaaaaaaaaa" || e.CapFrom.Words() != "from harbor" {
		t.Fatalf("cap: %v %+v", e.CapUSDDay, e.CapFrom)
	}
	if e.SubShare != 0.25 || e.SubShareFrom.Name != "dock" {
		t.Fatalf("share: %v %+v", e.SubShare, e.SubShareFrom)
	}
	if e.DepthLimit != 4 || e.DepthFrom.Kind != OriginTeam || e.DepthFrom.Inherited() || e.DepthFrom.Words() != "" {
		t.Fatalf("depth: %v %+v", e.DepthLimit, e.DepthFrom)
	}
	// Reset is a nil: pier's depth falls back to Settings.
	must(t, f.SetSettings("cccccccccccc", func(s *Settings) { s.DepthLimit = nil }))
	if e := f.Effective("cccccccccccc", defaults); e.DepthLimit != 3 || e.DepthFrom.Kind != OriginSettings {
		t.Fatalf("a reset override: %+v", e)
	}
	// An explicit 0 cap is an override (no cap), not an absence.
	must(t, f.SetSettings("bbbbbbbbbbbb", func(s *Settings) { s.CapUSDDay = ptrF(0) }))
	if e := f.Effective("cccccccccccc", defaults); e.CapUSDDay != 0 || e.CapFrom.Name != "dock" {
		t.Fatalf("a zero cap override: %+v", e)
	}
	// An unknown team is the defaults throughout.
	if e := f.Effective("nothere", defaults); e.CapUSDDay != 5 || e.CapFrom.Kind != OriginSettings {
		t.Fatalf("an unknown team: %+v", e)
	}
}

// AN OVERRIDE OUTSIDE ITS BAND IS REFUSED, AND ONE IN A HAND-EDITED FILE IS
// DROPPED ON LOAD, reading as inherit.
func TestOverridesAreKeptInTheirBands(t *testing.T) {
	f := tree()
	for _, bad := range []func(*Settings){
		func(s *Settings) { s.CapUSDDay = ptrF(-1) },
		func(s *Settings) { s.DepthLimit = ptrI(0) },
		func(s *Settings) { s.SubShare = ptrF(1.5) },
		func(s *Settings) { s.SubShare = ptrF(0) },
	} {
		if err := f.SetSettings("aaaaaaaaaaaa", bad); err != ErrSetting {
			t.Fatalf("an out-of-band override was taken: %v", err)
		}
	}
	if !f.Teams[0].Settings.Empty() {
		t.Fatal("a refused override changed the team")
	}
	dir := t.TempDir()
	raw := `{"version":2,"teams":[{"id":"aaaaaaaaaaaa","name":"harbor","parent":"","members":[],"manager":"",` +
		`"made":"2026-09-24T00:00:00Z","cap_usd_day":-3,"depth_limit":2,"questions_up":false}]}`
	writeRaw(t, dir, raw)
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Teams[0].Settings
	if s.CapUSDDay != nil || s.DepthLimit == nil || *s.DepthLimit != 2 || s.QuestionsUp == nil || *s.QuestionsUp {
		t.Fatalf("the file's overrides read as %+v", s)
	}
}

// THE OVERRIDES ARE WRITTEN FLAT ON THE TEAM, only when set, and survive a
// round trip; a team with none is written as before.
func TestOverridesAreStoredFlatAndOnlyWhenSet(t *testing.T) {
	plain, _ := json.Marshal(Team{ID: "aaaaaaaaaaaa", Name: "harbor"})
	for _, k := range []string{"questions_up", "cap_usd_day", "depth_limit", "sub_share", "state", "closed_at"} {
		if strings.Contains(string(plain), k) {
			t.Fatalf("a team with no overrides wrote %s: %s", k, plain)
		}
	}
	team := Team{ID: "aaaaaaaaaaaa", Name: "harbor", Settings: Settings{CapUSDDay: ptrF(0), QuestionsUp: ptrB(false)}}
	raw, _ := json.Marshal(team)
	if !strings.Contains(string(raw), `"cap_usd_day":0`) || !strings.Contains(string(raw), `"questions_up":false`) {
		t.Fatalf("the overrides are not flat on the team: %s", raw)
	}
	var back Team
	must(t, json.Unmarshal(raw, &back))
	if back.Settings.CapUSDDay == nil || *back.Settings.CapUSDDay != 0 || *back.Settings.QuestionsUp {
		t.Fatalf("the round trip lost an override: %+v", back.Settings)
	}
	if len(back.extra) != 0 {
		t.Fatalf("an override was kept as an unknown field: %v", back.extra)
	}
}

// DEPTH AND THE SHARE A NEW SUB-TEAM IS MADE WITH.
func TestNestingDepthAndTheSubTeamsShare(t *testing.T) {
	f := tree()
	if f.Depth("aaaaaaaaaaaa") != 1 || f.Depth("cccccccccccc") != 3 || f.Depth("nothere") != 0 {
		t.Fatal("depths are wrong")
	}
	if !f.CanNest("bbbbbbbbbbbb", defaults) || f.CanNest("cccccccccccc", defaults) {
		t.Fatal("a limit of 3 allows a third level and not a fourth")
	}
	if got := f.SubTeamCap("aaaaaaaaaaaa", defaults); got != 2.5 {
		t.Fatalf("half of $5 is %v", got)
	}
	if got := f.SubTeamCap("aaaaaaaaaaaa", Defaults{DepthLimit: 3, SubShare: 0.5}); got != 0 {
		t.Fatalf("a parent with no cap gave its sub-team %v", got)
	}
}

// managed is harbor managed by hm, dock unmanaged under it, yard managed by ym.
func managed() *File {
	f := tree()
	f.Teams[0].Manager = "hm"
	f.Teams[3].Members = []Member{{Key: "ym"}}
	f.Teams[3].Manager = "ym"
	return f
}

// THE HOME IS PICKED IN THE RULING'S ORDER. A conversation only in unmanaged
// dock reports to harbor above it; one in managed yard directly and in dock
// reports to yard (nearest); the manager of harbor reports to nobody, and
// the manager of a sub-team reports one level up.
func TestHomeIsPickedNearestThenStartedThenFirst(t *testing.T) {
	f := managed()
	f.Teams[1].Members = []Member{{Key: "k1"}, {Key: "k2"}}
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "k2"})
	tidy(f.Teams)

	if h, ok := f.Home("k1"); !ok || h.Team != "aaaaaaaaaaaa" || h.Via != "bbbbbbbbbbbb" || h.Distance != 1 {
		t.Fatalf("k1 in unmanaged dock reports to %+v %v", h, ok)
	}
	if h, _ := f.Home("k2"); h.Team != "dddddddddddd" || h.Distance != 0 {
		t.Fatalf("k2 reports to %+v, want yard (its own team managed beats one a level up)", h)
	}
	if links := f.Links("k2"); len(links) != 1 || links[0].Team != "aaaaaaaaaaaa" {
		t.Fatalf("k2's links are %+v", links)
	}
	if _, ok := f.Home("hm"); ok {
		t.Fatal("the top manager reports to somebody")
	}
	// dock gets a manager of its own: it reports one level up, to harbor.
	f.Teams[1].Members = append(f.Teams[1].Members, Member{Key: "dm"})
	f.Teams[1].Manager = "dm"
	tidy(f.Teams)
	if h, _ := f.Home("dm"); h.Team != "aaaaaaaaaaaa" {
		t.Fatalf("a sub-team's manager reports to %+v", h)
	}
	// k1's home is still its dock membership, whose nearest manager is now
	// dock's own: giving a team a manager is the person's act, and it is what
	// the manager is for (see home.go).
	if h, _ := f.Home("k1"); h.Team != "bbbbbbbbbbbb" || h.Via != "bbbbbbbbbbbb" {
		t.Fatalf("k1 in newly managed dock reports to %+v", h)
	}

	// Two managed teams at the same distance: the started one wins over file order.
	g := managed()
	g.Teams[0].Members = append(g.Teams[0].Members, Member{Key: "k3"})
	g.Teams[3].Members = append(g.Teams[3].Members, Member{Key: "k3", Started: true})
	tidy(g.Teams)
	if h, _ := g.Home("k3"); h.Team != "dddddddddddd" {
		t.Fatalf("the started membership lost: %+v", h)
	}
	// Without the start, the first in the file.
	g.Teams[3].Members[1].Started = false
	for i := range g.Teams {
		for j := range g.Teams[i].Members {
			g.Teams[i].Members[j].Home = false
		}
	}
	tidy(g.Teams)
	if h, _ := g.Home("k3"); h.Team != "aaaaaaaaaaaa" {
		t.Fatalf("the first in the file lost: %+v", h)
	}
}

// A HOME NEVER CHANGES BY ITSELF, MOVES WHEN IT IS NO LONGER ONE, AND SETHOME
// MOVES IT ON PURPOSE.
func TestHomeIsStableUntilItStopsBeingOne(t *testing.T) {
	f := managed()
	f.Teams[0].Members = append(f.Teams[0].Members, Member{Key: "k"})
	tidy(f.Teams)
	if h, _ := f.Home("k"); h.Team != "aaaaaaaaaaaa" {
		t.Fatalf("home %+v", h)
	}
	// k joins yard, which is just as near: harbor stays.
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "k", Started: true})
	if tidy(f.Teams) {
		t.Fatal("a nearer or started membership moved a valid home")
	}
	if h, _ := f.Home("k"); h.Team != "aaaaaaaaaaaa" {
		t.Fatalf("home moved to %+v", h)
	}
	// The person moves it.
	must(t, f.SetHome("k", "dddddddddddd"))
	if h, _ := f.Home("k"); h.Team != "dddddddddddd" {
		t.Fatalf("SetHome left %+v", h)
	}
	flags := 0
	for _, tm := range f.Teams {
		if m, ok := tm.Member("k"); ok && m.Home {
			flags++
		}
	}
	if flags != 1 {
		t.Fatalf("k carries %d home flags", flags)
	}
	// SetHome refuses a team with nothing above it.
	f.Teams[1].Members = []Member{{Key: "k"}}
	f.Teams[0].Manager = ""
	if err := f.SetHome("k", "bbbbbbbbbbbb"); err != ErrNoManagerAbove {
		t.Fatalf("SetHome on an unmanaged chain: %v", err)
	}
	// yard's manager cleared: the home is gone and nothing else is above k.
	must(t, f.ClearManager("dddddddddddd"))
	tidy(f.Teams)
	if _, ok := f.Home("k"); ok {
		t.Fatal("k still reports somewhere with no managers left")
	}
	for _, tm := range f.Teams {
		if m, ok := tm.Member("k"); ok && m.Home {
			t.Fatal("a flag was left with nothing above it")
		}
	}
}

// THE HOME FLAG SURVIVES A SAVE AND A LOAD, and a load of a file with none
// gives every conversation with a manager one.
func TestHomesAreStoredAndRepairedOnLoad(t *testing.T) {
	dir := t.TempDir()
	f := managed()
	f.Teams[0].Members = append(f.Teams[0].Members, Member{Key: "k"})
	raw, _ := json.Marshal(disk{Version: Version, Teams: f.Teams})
	writeRaw(t, dir, string(raw))
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := got.Home("k"); !ok || h.Team != "aaaaaaaaaaaa" {
		t.Fatalf("a load gave k %+v %v", h, ok)
	}
	again, _ := Load(dir)
	if m, _ := again.Teams[0].Member("k"); !m.Home {
		t.Fatal("the repair was not written back")
	}
}

// THE LCA OF 1..N PARTIES, WITH UNMANAGED GAPS. Parties in pier and dock
// (neither managed) meet at harbor; a party in yard shares no team with
// harbor's, so the person decides; a party that manages the meeting team is
// passed over for the next one up.
func TestLCAForOneToNPartiesAcrossUnmanagedGaps(t *testing.T) {
	f := managed()
	f.Teams[1].Members = []Member{{Key: "d1"}}
	f.Teams[2].Members = []Member{{Key: "p1"}, {Key: "p2"}}
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "y1"})

	if lca, ok := f.LCA("p1"); !ok || lca.Name != "harbor" {
		t.Fatalf("one party: %v %v", lca.Name, ok)
	}
	if lca, ok := f.LCA("p1", "p2"); !ok || lca.Name != "harbor" {
		t.Fatalf("two in unmanaged pier: %v %v", lca.Name, ok)
	}
	if lca, ok := f.LCA("p1", "d1", "p2"); !ok || lca.Name != "harbor" {
		t.Fatalf("three across pier and dock: %v %v", lca.Name, ok)
	}
	if _, ok := f.LCA("p1", "y1"); ok {
		t.Fatal("parties in two trees found a common manager")
	}
	// Manage pier: the two in pier meet there, pier and dock still at harbor.
	f.Teams[2].Members = append(f.Teams[2].Members, Member{Key: "pm"})
	f.Teams[2].Manager = "pm"
	if lca, _ := f.LCA("p1", "p2"); lca.Name != "pier" {
		t.Fatalf("two in managed pier meet at %v", lca.Name)
	}
	if lca, _ := f.LCA("p1", "d1"); lca.Name != "harbor" {
		t.Fatalf("pier and dock meet at %v", lca.Name)
	}
	// The pier manager as a party: pier is passed over.
	if lca, _ := f.LCA("pm", "p1"); lca.Name != "harbor" {
		t.Fatalf("a manager judged its own case at %v", lca.Name)
	}
	// A conversation in both yard and pier: yard is shared with y1.
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "p1"})
	if lca, _ := f.LCA("p1", "y1"); lca.Name != "yard" {
		t.Fatalf("a shared membership meets at %v", lca.Name)
	}
	if _, ok := f.LCA(); ok {
		t.Fatal("no parties found a decider")
	}
}

// CLOSING CASCADES DOWN, TAKES THE TEAM OUT OF EVERY WALK, AND REOPEN BRINGS
// BACK EXACTLY WHAT IT CLOSED.
func TestCloseCascadesAndReopenUndoesOnlyItsOwn(t *testing.T) {
	f := managed()
	f.Teams[1].Members = []Member{{Key: "k"}}
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "k"})
	must(t, f.SetSettings("aaaaaaaaaaaa", func(s *Settings) { s.CapUSDDay = ptrF(10) }))
	tidy(f.Teams)
	if h, _ := f.Home("k"); h.Team != "dddddddddddd" {
		t.Fatalf("home before %+v", h)
	}
	// pier closed on its own first.
	at := time.Date(2026, 9, 24, 10, 0, 0, 0, time.UTC)
	must(t, f.Close("cccccccccccc", at, ""))
	must(t, f.Close("aaaaaaaaaaaa", at.Add(time.Hour), "p123"))
	for _, id := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"} {
		if tm, _ := f.Team(id); !tm.Closed() {
			t.Fatalf("%s is open after its ancestor closed", id)
		}
	}
	if tm, _ := f.Team("cccccccccccc"); tm.ClosedWith != "cccccccccccc" || !tm.ClosedAt.Equal(at) {
		t.Fatalf("pier's own close was overwritten: %+v", tm)
	}
	if tm, _ := f.Team("aaaaaaaaaaaa"); tm.Report != "p123" {
		t.Fatal("the report pointer was not kept")
	}
	if len(f.Open()) != 1 || len(f.ClosedTeams()) != 3 || f.ClosedTeams()[0].ID != "aaaaaaaaaaaa" {
		t.Fatal("open and closed lists are wrong")
	}
	// Out of every walk.
	if lca, ok := f.LCA("hm"); ok {
		t.Fatalf("a closed team decided: %v", lca.Name)
	}
	if e := f.Effective("bbbbbbbbbbbb", defaults); e.CapFrom.Kind != OriginClosed || e.CapUSDDay != 0 {
		t.Fatalf("a closed team has a cap: %+v", e)
	}
	if f.CanNest("bbbbbbbbbbbb", defaults) {
		t.Fatal("a sub-team can be made under a closed team")
	}
	// k's home was yard all along; with dock closed nothing changes, and a
	// conversation only in harbor's tree now reports nowhere.
	tidy(f.Teams)
	if h, _ := f.Home("k"); h.Team != "dddddddddddd" {
		t.Fatalf("home after %+v", h)
	}
	// A sub-team under a closed parent cannot reopen alone.
	if err := f.Reopen("bbbbbbbbbbbb"); err != ErrParentClosed {
		t.Fatalf("reopening under a closed parent: %v", err)
	}
	must(t, f.Reopen("aaaaaaaaaaaa"))
	if tm, _ := f.Team("bbbbbbbbbbbb"); tm.Closed() {
		t.Fatal("dock, closed by harbor's close, stayed closed")
	}
	if tm, _ := f.Team("cccccccccccc"); !tm.Closed() {
		t.Fatal("pier, closed on its own, was reopened with harbor")
	}
}

// A CLOSED TEAM ROUND-TRIPS, AND A CONVERSATION WHOSE HOME CLOSES IS GIVEN THE
// NEXT ONE ON THE SAME WRITE.
func TestClosingMovesHomesOnTheSameWrite(t *testing.T) {
	dir := t.TempDir()
	f := managed()
	f.Teams[0].Members = append(f.Teams[0].Members, Member{Key: "k"})
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "k"})
	must(t, Save(dir, f.Teams))
	g, _ := Load(dir)
	if h, _ := g.Home("k"); h.Team != "aaaaaaaaaaaa" {
		t.Fatalf("home %+v", h)
	}
	must(t, Update(dir, func(f *File) error { return f.Close("aaaaaaaaaaaa", time.Time{}, "") }))
	g, _ = Load(dir)
	if h, _ := g.Home("k"); h.Team != "dddddddddddd" {
		t.Fatalf("after the close k reports to %+v", h)
	}
	tm, _ := g.Team("aaaaaaaaaaaa")
	if !tm.Closed() || tm.ClosedAt.IsZero() {
		t.Fatalf("the close did not survive the file: %+v", tm)
	}
}

// DELETE IS ONLY FROM CLOSED, AND TAKES THE TEAM'S FILES WITH IT.
func TestDeleteOnlyAClosedTeamAndItsFiles(t *testing.T) {
	dir := t.TempDir()
	f := managed()
	must(t, Save(dir, f.Teams))
	must(t, AppendTraffic(dir, "bbbbbbbbbbbb", Entry{Kind: KindNote, From: FromManager, To: ToEveryone, Text: "x"}))
	if _, err := Delete(dir, "aaaaaaaaaaaa"); err != ErrOpen {
		t.Fatalf("an open team was deleted: %v", err)
	}
	must(t, Update(dir, func(f *File) error { return f.Close("aaaaaaaaaaaa", time.Time{}, "") }))
	gone, err := Delete(dir, "aaaaaaaaaaaa")
	if err != nil || len(gone) != 3 {
		t.Fatalf("delete: %v %v", gone, err)
	}
	g, _ := Load(dir)
	if len(g.Teams) != 1 || g.Teams[0].Name != "yard" {
		t.Fatalf("left %+v", g.Teams)
	}
	if entries, _ := ReadTraffic(dir, "bbbbbbbbbbbb", "", 0); len(entries) != 0 {
		t.Fatal("a deleted team's traffic is still there")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// writeRaw puts raw on disk as dir's teams file, exactly.
func writeRaw(t *testing.T, dir, raw string) {
	t.Helper()
	must(t, os.MkdirAll(dir, 0o700))
	must(t, os.WriteFile(Path(dir), []byte(raw), 0o600))
}

// THE GLOBAL MANAGER IS THE MANAGER OF A REAL ROOT, AND EVERY RULE HOLDS
// WITHOUT A SPECIAL CASE. Making the root moves every top-level team under it;
// parties in two trees then meet at the root instead of the person; a
// top-level manager reports to it; its override is inherited as `from All
// teams`; it is not a level; a team made later at the top lands under it; it
// cannot be closed; and dissolving it puts the tree back.
func TestTheRootHoldsEveryTeamAndIsNotALevel(t *testing.T) {
	f := managed()
	f.Teams[1].Members = []Member{{Key: "d1"}}
	f.Teams[3].Members = append(f.Teams[3].Members, Member{Key: "y1"})
	if _, ok := f.LCA("d1", "y1"); ok {
		t.Fatal("two trees met before there was a root")
	}
	root := f.MakeRoot(time.Time{})
	must(t, f.AddMember(root, Member{Key: "gm"}))
	must(t, f.SetManager(root, "gm"))
	must(t, f.SetSettings(root, func(s *Settings) { s.CapUSDDay = ptrF(20) }))
	tidy(f.Teams)
	if lca, ok := f.LCA("d1", "y1"); !ok || lca.ID != root {
		t.Fatalf("two trees meet at %v %v, want the root", lca.Name, ok)
	}
	if h, _ := f.Home("hm"); h.Team != root {
		t.Fatalf("harbor's manager reports to %+v, want the root", h)
	}
	if e := f.Effective("cccccccccccc", defaults); e.CapUSDDay != 20 || e.CapFrom.Words() != "from All teams" {
		t.Fatalf("the root's cap: %+v", e)
	}
	if f.Depth(root) != 0 || f.Depth("aaaaaaaaaaaa") != 1 || f.Depth("cccccccccccc") != 3 {
		t.Fatal("the root counted as a level")
	}
	f.Teams = append(f.Teams, Team{ID: "eeeeeeeeeeee", Name: "late"})
	tidy(f.Teams)
	if late, _ := f.Team("eeeeeeeeeeee"); late.Parent != root {
		t.Fatal("a team made later at the top did not land under the root")
	}
	if err := f.Close(root, time.Time{}, ""); err != ErrRoot {
		t.Fatalf("the root closed: %v", err)
	}
	if err := f.SetParent(root, "aaaaaaaaaaaa"); err != ErrRoot {
		t.Fatalf("the root moved under a team: %v", err)
	}
	f.DissolveRoot()
	if _, ok := f.Root(); ok {
		t.Fatal("the root survived its dissolving")
	}
	if tm, _ := f.Team("aaaaaaaaaaaa"); tm.Parent != "" {
		t.Fatal("harbor was not put back at the top")
	}
}

// ORGANIZE'S QUIET TEAMS: a team nobody touched in the window and with no
// packet waiting is proposed; one with a recent Traffic line, a recently
// written member, or a waiting packet is not; neither is a closed team.
func TestQuietTeamsAreProposedAndBusyOnesAreNot(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-30 * 24 * time.Hour)
	fresh := filepath.Join(dir, "fresh.jsonl")
	must(t, os.WriteFile(fresh, []byte("x"), 0o600))
	stale := filepath.Join(dir, "stale.jsonl")
	must(t, os.WriteFile(stale, []byte("x"), 0o600))
	must(t, os.Chtimes(stale, old, old))
	must(t, Save(dir, []Team{
		{ID: "aaaaaaaaaaaa", Name: "quiet", Made: old, Members: []Member{{Key: stale}}},
		{ID: "bbbbbbbbbbbb", Name: "talking", Made: old},
		{ID: "cccccccccccc", Name: "writing", Made: old, Members: []Member{{Key: fresh}}},
		{ID: "dddddddddddd", Name: "asked", Made: old, Manager: "m", Members: []Member{{Key: "m", Handle: "boss"}}},
		{ID: "eeeeeeeeeeee", Name: "closed", Made: old, State: TeamClosed, ClosedAt: old},
	}))
	must(t, AppendTraffic(dir, "bbbbbbbbbbbb", Entry{Kind: KindNote, From: FromManager, To: ToEveryone, Text: "hi"}))
	_, err := Raise(dir, Packet{Team: Person, Origin: "dddddddddddd", Kind: PacketQuestion, RaisedBy: "boss", Question: "?"})
	must(t, err)
	f, _ := Load(dir)
	quiet, err := Quiet(dir, f, time.Now(), QuietAfter)
	if err != nil || len(quiet) != 1 || quiet[0] != "aaaaaaaaaaaa" {
		t.Fatalf("quiet teams %v, %v", quiet, err)
	}
}
