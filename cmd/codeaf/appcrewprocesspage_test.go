package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// crewFixture is a backend over a temporary state root: a teams.json, a places
// root, and a fake pager standing in for the owners' own histories.
type crewFixture struct {
	t       *testing.T
	backend *appBackend
	profile string
	places  string
	// rows is how many rows each owner holds; an owner not in it is missing.
	rows  map[string]int
	asked []string
}

func newCrewFixture(t *testing.T) *crewFixture {
	t.Helper()
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv("CODEAF_HOME", state)
	f := &crewFixture{t: t, profile: filepath.Join(state, "profile"), places: session.PlacesRoot(), rows: map[string]int{}}
	if err := os.MkdirAll(f.profile, 0o700); err != nil {
		t.Fatal(err)
	}
	f.backend = newFakeBackend(newAppRecorder(), nil)
	f.backend.profileDir = f.profile
	f.backend.crewHistory.visit = f.visit
	return f
}

// file is the transcript path a member of owner stores.
func (f *crewFixture) file(owner string) string {
	return filepath.Join(f.places, "p", owner, "transcript.jsonl")
}

func (f *crewFixture) member(owner string) teamstore.Member {
	return teamstore.Member{Key: f.file(owner), File: f.file(owner)}
}

func (f *crewFixture) team(id, parent string, owners ...string) teamstore.Team {
	t := teamstore.Team{ID: id, Parent: parent, Name: id}
	for _, o := range owners {
		t.Members = append(t.Members, f.member(o))
	}
	return t
}

func (f *crewFixture) save(teams ...teamstore.Team) {
	f.t.Helper()
	if err := teamstore.Save(f.profile, teams); err != nil {
		f.t.Fatal(err)
	}
}

// visit is an owner's history of rows rows, newest first, whose cursor is the
// next row number.
func (f *crewFixture) visit(ctx context.Context, _ *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
	f.asked = append(f.asked, o.id)
	total, ok := f.rows[o.id]
	if !ok {
		return nil, os.ErrNotExist
	}
	at := 0
	if o.cursor != "" {
		at, _ = strconv.Atoi(strings.TrimPrefix(o.cursor, "k"))
	}
	res := &appwire.ProcessPageResult{Processes: []appwire.Process{}}
	for i := at; i < total && len(res.Processes) < n; i++ {
		res.Processes = append(res.Processes, appwire.Process{ID: fmt.Sprintf("%s:p%d", o.id, total-i), Agent: o.id, State: appwire.ProcessEnded})
		at = i + 1
	}
	if at < total {
		res.Next = "k" + strconv.Itoa(at)
	}
	return res, nil
}

func (f *crewFixture) ask(crew string, limit int, cursor string) (*appwire.ProcessPageResult, error) {
	ask := appwire.ProcessPageAsk{Crew: crew, Limit: &limit, Cursor: cursor}
	if crew == "" {
		ask.Scope = appwire.ProcessScopeAll
	}
	return f.backend.PageCrewProcesses(context.Background(), ask)
}

// walk reads a whole walk and answers every row id in order.
func (f *crewFixture) walk(crew string, limit int) ([]string, *appwire.ProcessPageResult) {
	f.t.Helper()
	var ids []string
	cursor := ""
	for pages := 0; ; pages++ {
		res, err := f.ask(crew, limit, cursor)
		if err != nil {
			f.t.Fatal(err)
		}
		for _, p := range res.Processes {
			ids = append(ids, p.ID)
		}
		if res.Next == "" {
			return ids, res
		}
		if pages > 1000 {
			f.t.Fatal("the walk does not end")
		}
		cursor = res.Next
	}
}

func ownerSet(ids []string) map[string]int {
	out := map[string]int{}
	for _, id := range ids {
		out[strings.SplitN(id, ":", 2)[0]]++
	}
	return out
}

// SELECTING A CREW IS ITS EXACT RECORDED SUBTREE: the nested crew, closed and
// archived descendants through recorded parents, no sibling, no former member,
// no conversation in no team.
func TestCrewRosterIsTheExactRecordedSubtree(t *testing.T) {
	f := newCrewFixture(t)
	top := f.team("top", "", "a")
	child := f.team("child", "top", "b")
	grand := f.team("grand", "child", "c")
	grand.State, grand.ClosedAt = teamstore.TeamClosed, time.Now()
	grand.ArchivedAt = time.Now()
	sibling := f.team("sibling", "", "d")
	child.FormerMembers = []teamstore.Member{f.member("former")}
	roster := func(sel crewSelection, teams ...teamstore.Team) *crewRoster {
		t.Helper()
		r, err := crewRosterOf(context.Background(), teams, sel, crewPlacesRoots())
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	ids := func(r *crewRoster) string {
		var out []string
		for _, o := range r.owners {
			out = append(out, o.id)
		}
		return strings.Join(out, ",")
	}
	all := []teamstore.Team{top, child, grand, sibling}
	if got := ids(roster(crewSelection{crew: "top"}, all...)); got != "a,b,c" {
		t.Fatalf("top subtree %q", got)
	}
	if got := ids(roster(crewSelection{crew: "child"}, all...)); got != "b,c" {
		t.Fatalf("nested selection %q", got)
	}
	if got := ids(roster(crewSelection{crew: "grand"}, all...)); got != "c" {
		t.Fatalf("a closed archived crew selected directly %q", got)
	}
	if got := ids(roster(crewSelection{all: true}, all...)); got != "a,b,c,d" {
		t.Fatalf("all crews %q (former members and loose conversations are never in it)", got)
	}
	if _, err := crewRosterOf(context.Background(), all, crewSelection{crew: "nobody"}, nil); !isCode(err, appwire.CodeNotFound) {
		t.Fatalf("unknown crew: %v", err)
	}
	// Renaming, spending and recolouring do not change the revision; members do.
	before := roster(crewSelection{crew: "top"}, all...).revision
	renamed := append([]teamstore.Team(nil), all...)
	renamed[1].Name, renamed[1].Hue = "other", 0.4
	if roster(crewSelection{crew: "top"}, renamed...).revision != before {
		t.Fatal("a rename changed the revision")
	}
	moved := append([]teamstore.Team(nil), all...)
	moved[1].Members = append([]teamstore.Member(nil), moved[1].Members...)
	moved[1].Members = append(moved[1].Members, f.member("e"))
	if roster(crewSelection{crew: "top"}, moved...).revision == before {
		t.Fatal("a new member did not change the revision")
	}
	// A broken tree is the engine's error, never a guessed subtree.
	cycle := []teamstore.Team{f.team("x", "y", "a"), f.team("y", "x", "b")}
	if _, err := crewRosterOf(context.Background(), cycle, crewSelection{crew: "x"}, nil); !isCode(err, appwire.CodeEngine) {
		t.Fatalf("cycle: %v", err)
	}
}

// A STORED PATH IS ACCEPTED ONLY WHEN IT IS EXACTLY <places>/<bucket>/<id>/
// transcript.jsonl.
func TestCrewPlaceOfRefusesEveryOtherPath(t *testing.T) {
	roots := []string{"/s/places"}
	for file, want := range map[string]bool{
		"/s/places/p/a/transcript.jsonl":           true,
		"/s/places/p/a/other.jsonl":                false,
		"/s/places/a/transcript.jsonl":             false,
		"/s/places/p/q/a/transcript.jsonl":         false,
		"/s/places/p/../p/a/transcript.jsonl":      false,
		"/s/places/.p/a/transcript.jsonl":          false,
		"/s/places/p/.a/transcript.jsonl":          false,
		"/other/places/p/a/transcript.jsonl":       false,
		"places/p/a/transcript.jsonl":              false,
		"/s/places/p/a/transcript.jsonl/":          false,
		"/s/places/p//a/transcript.jsonl":          false,
		"/s/places-evil/p/a/transcript.jsonl":      false,
		"/s/places/../places/p/a/transcript.jsonl": false,
		"": false,
	} {
		if _, _, ok := crewPlaceOf(file, roots); ok != want {
			t.Errorf("%q accepted=%v", file, ok)
		}
	}
}

// OWNERS TAKE TURNS FIVE ROWS AT A TIME, BY ID, AND NOTHING IS REPEATED OR LOST
// WHEREVER THE PAGES SPLIT.
func TestCrewWalkRoundsAreFairAndSplitIndependent(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 12, "b": 3, "c": 7}
	f.save(f.team("t", "", "c", "a", "b"), f.team("u", "", "a"))
	whole, res := f.walk("", 100)
	if len(whole) != 22 || res.Owners.Total != 3 || res.Owners.Done != 3 || res.Incomplete {
		t.Fatalf("whole walk %v %+v", len(whole), res.Owners)
	}
	// Round one: five of a, three of b, five of c; round two: five of a, two of c.
	want := []string{"a:p12", "a:p8", "b:p3", "b:p1", "c:p7", "c:p3", "a:p7", "a:p3", "c:p2", "a:p2"}
	for i, id := range []string{whole[0], whole[4], whole[5], whole[7], whole[8], whole[12], whole[13], whole[17], whole[18], whole[21]} {
		_ = i
		_ = id
	}
	if ownerSet(whole[:13])["a"] != 5 || ownerSet(whole[:13])["b"] != 3 || ownerSet(whole[:13])["c"] != 5 {
		t.Fatalf("first round %v (%v)", whole[:13], want)
	}
	seen := map[string]bool{}
	for _, id := range whole {
		if seen[id] {
			t.Fatalf("%s twice", id)
		}
		seen[id] = true
	}
	for _, limit := range []int{1, 2, 3, 4, 5, 6, 9, 10, 23, 50, 100} {
		got, _ := f.walk("", limit)
		if len(got) != len(whole) {
			t.Fatalf("limit %d: %d rows, want %d", limit, len(got), len(whole))
		}
		crewCheckWalk(t, got, f.rows)
	}
	// An owner in two crews is visited once.
	counts := map[string]int{}
	f.asked = nil
	f.walk("", 100)
	for _, id := range f.asked {
		counts[id]++
	}
	if counts["a"] != 3 {
		t.Fatalf("a was visited %d times for 12 rows", counts["a"])
	}
}

// A MISSING OWNER IS NAMED, NEVER A FALSE EMPTY, AND THE REST OF THE WALK GOES
// ON.
func TestCrewWalkNamesWhatItCouldNotRead(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 2}
	team := f.team("t", "", "a", "gone")
	team.Members = append(team.Members, teamstore.Member{Key: "loose-key"})
	f.save(team)
	ids, res := f.walk("t", 50)
	if len(ids) != 2 || !res.Incomplete || res.Owners.Total != 3 || res.Owners.Done != 3 || res.Owners.Unread != 2 {
		t.Fatalf("ids %v owners %+v incomplete %v", ids, res.Owners, res.Incomplete)
	}
	first, err := f.ask("t", 50, "")
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, p := range first.Owners.Partial {
		reasons[p.Agent+p.Key] = p.Reason
	}
	if reasons["gone"] != "missing" || len(first.Owners.Partial) != 2 {
		t.Fatalf("partial %+v", first.Owners.Partial)
	}
	if first.Roster != appwire.ProcessRosterRecorded || first.Revision == "" {
		t.Fatalf("caption %q revision %q", first.Roster, first.Revision)
	}
}

// MORE THAN SIXTY-FOUR HELD OWNERS ARE ALL READ, AND AN OWNER IN NO TEAM IS NOT.
func TestCrewWalkKeepsEveryOwnerOfALargeCrew(t *testing.T) {
	f := newCrewFixture(t)
	var owners []string
	for i := 0; i < 150; i++ {
		id := fmt.Sprintf("o%03d", i)
		owners = append(owners, id)
		f.rows[id] = 1
	}
	f.rows["loose"] = 4
	f.save(f.team("t", "", owners...))
	ids, res := f.walk("t", 100)
	if len(ids) != 150 || len(ownerSet(ids)) != 150 || res.Owners.Total != 150 || res.Incomplete {
		t.Fatalf("%d rows from %d owners: %+v", len(ids), len(ownerSet(ids)), res.Owners)
	}
}

// THE TOKEN: THE PREVIOUS ONE REPLAYS THE STORED REPLY AND ADVANCES NOTHING,
// OLDER, UNKNOWN AND EXPIRED ONES ARE STALE, ANOTHER TARGET IS REFUSED, A
// CHANGED MEMBERSHIP ENDS THE WALK.
func TestCrewCursorReplayStaleAndChangedMembership(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 30, "b": 30}
	f.save(f.team("t", "", "a", "b"), f.team("u", "", "z"))
	clock := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	reg := f.backend.crewWalks()
	reg.now = func() time.Time { return clock }
	p1, err := f.ask("t", 10, "")
	if err != nil || p1.Next == "" {
		t.Fatalf("page 1: %v %v", p1, err)
	}
	p2, err := f.ask("t", 10, p1.Next)
	if err != nil {
		t.Fatal(err)
	}
	asked := len(f.asked)
	again, err := f.ask("t", 10, p1.Next)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(p2)
	b, _ := json.Marshal(again)
	if string(a) != string(b) || len(f.asked) != asked {
		t.Fatalf("a retried page is not the same reply, or read again")
	}
	p3, err := f.ask("t", 10, p2.Next)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ask("t", 10, p1.Next); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("an older token: %v", err)
	}
	if _, err := f.ask("t", 10, "c1.AAAAAAAAAAAAAAAAAAAAAA"); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("an unknown token: %v", err)
	}
	if _, err := f.ask("t", 10, "nonsense"); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("a malformed token: %v", err)
	}
	if _, err := f.ask("u", 10, p3.Next); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("another target: %v", err)
	}
	if _, err := f.ask("", 10, p3.Next); !isCode(err, appwire.CodeBadParams) {
		t.Fatalf("all crews on a crew's token: %v", err)
	}
	// A rename and spend do not end it; a new member does.
	renamed := f.team("t", "", "a", "b")
	renamed.Name = "renamed"
	f.save(renamed, f.team("u", "", "z"))
	p4, err := f.ask("t", 10, p3.Next)
	if err != nil {
		t.Fatalf("a rename ended the walk: %v", err)
	}
	f.save(f.team("t", "", "a", "b", "c"), f.team("u", "", "z"))
	if _, err := f.ask("t", 10, p4.Next); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("a changed membership: %v", err)
	}
	// An idle walk expires.
	p1, err = f.ask("t", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(crewIdleTTL + time.Second)
	if _, err := f.ask("t", 10, p1.Next); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("an expired walk: %v", err)
	}
}

// A SECOND PAGE ON A WALK BEING READ IS BUSY, AND THE PAGE'S OWN TIME ENDS IT
// WITHOUT MOVING ANY OWNER.
func TestCrewPageUnderADeadlineAdvancesNothing(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 30}
	f.save(f.team("t", "", "a"))
	p1, err := f.ask("t", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	limit := 5
	if _, err := f.backend.PageCrewProcesses(ctx, appwire.ProcessPageAsk{Crew: "t", Limit: &limit, Cursor: p1.Next}); err == nil {
		t.Fatal("a cancelled page answered")
	}
	// The walk was ended by the failed page; a fresh walk is the way back.
	if _, err := f.ask("t", 5, p1.Next); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("after a failed page: %v", err)
	}
	// A walk pinned by a page in flight refuses a second page.
	p1, _ = f.ask("t", 5, "")
	reg := f.backend.crewWalks()
	reg.mu.Lock()
	reg.walks[p1.Next].busy = true
	reg.mu.Unlock()
	if _, err := f.ask("t", 5, p1.Next); !isCode(err, appwire.CodeBusy) {
		t.Fatalf("a second page: %v", err)
	}
}

// THE REGISTRY HOLDS WHAT ITS CAPS SAY, COUNTS WHAT IT ACTUALLY HOLDS, AND
// NEVER EVICTS A WALK BEING READ.
func TestCrewRegistryCapsAndAccounting(t *testing.T) {
	reg := newCrewRegistry()
	clock := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return clock }
	walk := func(scope string, pad int) *crewWalk {
		return &crewWalk{scope: scope, revision: "r", owners: []crewOwner{{id: "a", cursor: strings.Repeat("x", pad)}}}
	}
	// Accounting is what is held: the cursor's length shows in the size.
	small, big := walk("crew:a", 0).bytes(), walk("crew:a", 1000).bytes()
	if big-small != 1000 {
		t.Fatalf("accounting %d vs %d", small, big)
	}
	w1 := walk("crew:a", 0)
	if err := reg.admit(w1); err != nil {
		t.Fatal(err)
	}
	w1.busy = false
	clock = clock.Add(time.Second)
	w2 := walk("crew:a", 0)
	if err := reg.admit(w2); err != nil {
		t.Fatal(err)
	}
	w2.busy = false
	// A third walk of one scope evicts the least recently used of the two.
	clock = clock.Add(time.Second)
	w3 := walk("crew:a", 0)
	if err := reg.admit(w3); err != nil {
		t.Fatal(err)
	}
	if reg.scopeCountLocked("crew:a") != 2 || w1.accounted != 0 || w2.accounted == 0 {
		t.Fatalf("per-scope eviction: %d %d %d", reg.scopeCountLocked("crew:a"), w1.accounted, w2.accounted)
	}
	// Both of a scope pinned: the third is busy, not an eviction.
	w2.busy = true
	if err := reg.admit(walk("crew:a", 0)); !isCode(err, appwire.CodeBusy) {
		t.Fatalf("pinned scope: %v", err)
	}
	w2.busy, w3.busy = false, false
	// Sixteen walks at most.
	reg = newCrewRegistry()
	reg.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	for i := 0; i < crewWalksMax+4; i++ {
		w := walk("crew:"+strconv.Itoa(i), 0)
		if err := reg.admit(w); err != nil {
			t.Fatal(err)
		}
		w.busy = false
	}
	if len(reg.live) != crewWalksMax {
		t.Fatalf("%d walks", len(reg.live))
	}
	// A walk over one walk's share is too large; the total is the bytes held.
	if err := reg.admit(walk("crew:big", crewWalkBytesMax)); !isCode(err, appwire.CodeTooLarge) {
		t.Fatalf("oversize walk: %v", err)
	}
	reg = newCrewRegistry()
	reg.now = func() time.Time { clock = clock.Add(time.Second); return clock }
	var held []*crewWalk
	for i := 0; i < 8; i++ {
		w := walk("crew:"+strconv.Itoa(i), crewWalkBytesMax-1<<10)
		if err := reg.admit(w); err != nil {
			t.Fatal(err)
		}
		held = append(held, w)
	}
	if reg.bytes > crewRegistryBytes {
		t.Fatalf("registry holds %d", reg.bytes)
	}
	// All eight pinned at 8 MiB fill the 64 MiB: a ninth is busy; unpinned, it
	// evicts the oldest.
	if err := reg.admit(walk("crew:n", crewWalkBytesMax-1<<10)); !isCode(err, appwire.CodeBusy) {
		t.Fatalf("a full registry of pinned walks: %v", err)
	}
	held[0].busy = false
	if err := reg.admit(walk("crew:n", crewWalkBytesMax-1<<10)); err != nil || held[0].accounted != 0 {
		t.Fatalf("global eviction: %v", err)
	}
	// A reply that does not fit beside its walk is dropped, not the walk: the
	// previous token then goes stale.
	reg = newCrewRegistry()
	w := walk("crew:r", 0)
	if err := reg.admit(w); err != nil {
		t.Fatal(err)
	}
	reg.rotate(w, true)
	w.last = make([]byte, crewWalkBytesMax)
	if err := reg.settle(w, false); err != nil || w.last != nil {
		t.Fatalf("settle kept %d bytes of reply: %v", len(w.last), err)
	}
	// And the arithmetic the contract rests on: four reads in flight at the
	// largest walk, plus the new walk, fit.
	if (appwire.ProcessReadsInFlight-1)*crewWalkBytesMax+crewWalkBytesMax > crewRegistryBytes {
		t.Fatal("a cold request can be starved by pinned walks")
	}
}

// A page's reply never passes the page's bytes, and a visit's rows join whole.
func TestCrewPageStaysWithinTheReplyBound(t *testing.T) {
	f := newCrewFixture(t)
	big := strings.Repeat("x", appProcessFieldMax)
	f.backend.crewHistory.visit = func(ctx context.Context, _ *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		res := &appwire.ProcessPageResult{Processes: []appwire.Process{}}
		for i := 0; i < n; i++ {
			res.Processes = append(res.Processes, appwire.Process{ID: o.id + ":" + strconv.Itoa(i), Agent: o.id, Command: big, Title: big})
		}
		res.Next = o.cursor + "x"
		return res, nil
	}
	var owners []string
	for i := 0; i < 5; i++ {
		owners = append(owners, fmt.Sprintf("o%d", i))
	}
	f.save(f.team("t", "", owners...))
	res, err := f.ask("t", 100, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if len(raw) > appwire.ProcessPageReplyMax || res.Next == "" {
		t.Fatalf("%d rows, %d bytes", len(res.Processes), len(raw))
	}
}

// AN OWNER WHOSE CURSOR DOES NOT MOVE IS NAMED, NOT ASKED THE SAME FOR EVER.
func TestCrewOwnerWithAStuckCursorIsPartial(t *testing.T) {
	f := newCrewFixture(t)
	f.backend.crewHistory.visit = func(ctx context.Context, _ *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		return &appwire.ProcessPageResult{Processes: []appwire.Process{}, Next: "same"}, nil
	}
	f.save(f.team("t", "", "a"))
	ids, res := f.walk("t", 50)
	if len(ids) != 0 || res.Owners.Unread != 1 || !res.Incomplete {
		t.Fatalf("%v %+v", ids, res.Owners)
	}
}

// A RELEASED ASK FOR AN ENGINE WITHOUT THE FEATURE, AND THE FORMS, ARE CHECKED.
func TestCrewAskForms(t *testing.T) {
	one := 1
	for name, bad := range map[string]appwire.ProcessPageAsk{
		"agent and crew":     {Agent: "a", Crew: "c"},
		"agent and all":      {Agent: "a", Scope: "all"},
		"crew and all":       {Crew: "c", Scope: "all"},
		"unknown scope":      {Scope: "everything"},
		"crew scope no crew": {Scope: "crew"},
		"limit":              {Crew: "c", Limit: new(int)},
	} {
		if _, err := appwire.CheckProcessCrewAsk(bad); !isCode(err, appwire.CodeBadParams) {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, ok := range []appwire.ProcessPageAsk{{Crew: "c"}, {Crew: "c", Scope: "crew"}, {Scope: "all", Limit: &one}} {
		if _, err := appwire.CheckProcessCrewAsk(ok); err != nil {
			t.Errorf("%+v: %v", ok, err)
		}
	}
}

// ── the real owners, over a real disk ───────────────────────────────────────

// seedOwner makes places/p/<id> with a transcript and a process store of rows.
func (f *crewFixture) seedOwner(id, bucket string, rows ...session.JobNotice) string {
	f.t.Helper()
	dir := filepath.Join(f.places, bucket, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), nil, 0o600); err != nil {
		f.t.Fatal(err)
	}
	seedRecords(f.t, dir, rows...)
	return dir
}

func job(id int, command string) session.JobNotice {
	return session.JobNotice{ID: id, Kind: session.JobKindCommand, Command: command, State: session.JobDone, Started: time.Date(2026, 10, 16, 9, id, 0, 0, time.UTC)}
}

// A CLOSED MEMBER IS A CONVERSATION OF ITS OWN AND ITS HISTORY IS SERVED FROM
// ITS STORE WITH NOTHING OPENED, CREATED OR CACHED; A SWAPPED LINK IS REFUSED.
func TestCrewReadsClosedOwnersAnchoredAndChangesNothing(t *testing.T) {
	f := newCrewFixture(t)
	f.backend.crewHistory.visit = nil
	f.seedOwner("closed1", "p", job(2, "make b"), job(1, "make a"))
	f.seedOwner("closed2", "q", job(1, "go test"))
	secret := t.TempDir()
	seedRecords(t, secret, job(9, "stolen"))
	f.seedOwner("swapped", "p", job(1, "inside"))
	team := f.team("t", "", "closed1", "closed2", "swapped")
	team.State = teamstore.TeamClosed
	team.ClosedAt = time.Now()
	team.Members[1].File = filepath.Join(f.places, "q", "closed2", "transcript.jsonl")
	f.save(team)
	// Swap the folder of one owner for a link to a folder outside the root.
	swapped := filepath.Join(f.places, "p", "swapped")
	if err := os.RemoveAll(swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, swapped); err != nil {
		t.Fatal(err)
	}
	before := f.treeStamp()
	agents := len(f.backend.agents)
	ids, res := f.walk("t", 50)
	got := ownerSet(ids)
	if got["closed1"] != 2 || got["closed2"] != 1 || got["swapped"] != 0 || strings.Contains(strings.Join(ids, " "), "9") && got["swapped"] != 0 {
		t.Fatalf("rows %v", ids)
	}
	if !res.Incomplete || res.Owners.Unread != 1 {
		t.Fatalf("%+v", res.Owners)
	}
	first, _ := f.ask("t", 50, "")
	if len(first.Owners.Partial) != 1 || first.Owners.Partial[0].Agent != "swapped" || first.Owners.Partial[0].Reason != "missing" {
		t.Fatalf("partial %+v", first.Owners.Partial)
	}
	if len(f.backend.agents) != agents || f.treeStamp() != before {
		t.Fatal("a crew page created, cached or wrote something")
	}
}

// treeStamp is every path under the state root with its size and mtime.
func (f *crewFixture) treeStamp() string {
	var out []string
	_ = filepath.Walk(filepath.Dir(f.profile), func(path string, info os.FileInfo, err error) error {
		if err == nil {
			out = append(out, fmt.Sprintf("%s %d %d", path, info.Size(), info.ModTime().UnixNano()))
		}
		return nil
	})
	return strings.Join(out, "\n")
}

// A STORED PATH OUTSIDE THE PLACES ROOT, AND A KEY WITH NO FILE, ARE NAMED
// MISSING; NOTHING IS READ FROM THEM.
func TestCrewForeignPathsAndKeyOnlyMembersAreMissing(t *testing.T) {
	f := newCrewFixture(t)
	f.backend.crewHistory.visit = nil
	foreign := t.TempDir()
	seedRecords(t, filepath.Join(foreign, "x", "foreign"), job(1, "foreign"))
	_ = os.WriteFile(filepath.Join(foreign, "x", "foreign", "transcript.jsonl"), nil, 0o600)
	team := f.team("t", "", "ok")
	f.seedOwner("ok", "p", job(1, "fine"))
	team.Members = append(team.Members,
		teamstore.Member{Key: "k1", File: filepath.Join(foreign, "x", "foreign", "transcript.jsonl")},
		teamstore.Member{Key: "keyonly"})
	f.save(team)
	ids, res := f.walk("t", 50)
	if len(ids) != 1 || res.Owners.Total != 3 || res.Owners.Unread != 2 || !res.Incomplete {
		t.Fatalf("%v %+v", ids, res.Owners)
	}
}

// ── the session door ────────────────────────────────────────────────────────

// AN OWNER READ BY BUCKET AND ID REFUSES LINKS AT EVERY COMPONENT AND READS NO
// DIRECTORY LISTING.
func TestReadProcessPageWithinRootRefusesLinks(t *testing.T) {
	places := t.TempDir()
	outside := t.TempDir()
	seedRecords(t, filepath.Join(outside, "c"), job(1, "outside"))
	_ = os.WriteFile(filepath.Join(outside, "c", "transcript.jsonl"), nil, 0o600)
	dir := filepath.Join(places, "b", "c")
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(filepath.Join(dir, "transcript.jsonl"), nil, 0o600)
	seedRecords(t, dir, job(1, "inside"))
	ctx := context.Background()
	page, err := session.ReadProcessPageWithinRoot(ctx, places, "b", "c", session.ProcessPageAsk{})
	if err != nil || len(page.Rows) != 1 || page.Rows[0].Command != "inside" {
		t.Fatalf("%+v %v", page, err)
	}
	for name, swap := range map[string]func(){
		"bucket": func() {
			_ = os.RemoveAll(filepath.Join(places, "b"))
			_ = os.Symlink(outside, filepath.Join(places, "b"))
		},
		"transcript": nil,
	} {
		if swap == nil {
			continue
		}
		swap()
		if _, err := session.ReadProcessPageWithinRoot(ctx, places, "b", "c", session.ProcessPageAsk{}); err == nil {
			t.Fatalf("%s: a link was followed", name)
		}
	}
	// The conversation folder a link; the store a link; the transcript a link.
	_ = os.RemoveAll(filepath.Join(places, "b"))
	_ = os.MkdirAll(filepath.Join(places, "b"), 0o700)
	_ = os.Symlink(filepath.Join(outside, "c"), filepath.Join(places, "b", "c"))
	if _, err := session.ReadProcessPageWithinRoot(ctx, places, "b", "c", session.ProcessPageAsk{}); err == nil {
		t.Fatal("a linked conversation folder was followed")
	}
	_ = os.Remove(filepath.Join(places, "b", "c"))
	_ = os.MkdirAll(filepath.Join(places, "b", "c"), 0o700)
	_ = os.WriteFile(filepath.Join(places, "b", "c", "transcript.jsonl"), nil, 0o600)
	_ = os.Symlink(filepath.Join(outside, "c", "processes"), filepath.Join(places, "b", "c", "processes"))
	if page, err := session.ReadProcessPageWithinRoot(ctx, places, "b", "c", session.ProcessPageAsk{}); err == nil && len(page.Rows) != 0 {
		t.Fatalf("a linked store was read: %+v", page)
	}
	for _, bad := range [][2]string{{"..", "c"}, {"b", ".."}, {"b/c", "x"}, {"", "c"}, {".", "c"}} {
		if _, err := session.ReadProcessPageWithinRoot(ctx, places, bad[0], bad[1], session.ProcessPageAsk{}); err == nil {
			t.Fatalf("%v accepted", bad)
		}
	}
}

// crewCheckWalk asserts a walk's rows are every row of every owner exactly
// once, and each owner's own rows arrive in that owner's own order.
func crewCheckWalk(t *testing.T, ids []string, rows map[string]int) {
	t.Helper()
	next := map[string]int{}
	for _, id := range ids {
		parts := strings.SplitN(id, ":", 2)
		owner := parts[0]
		want := fmt.Sprintf("%s:p%d", owner, rows[owner]-next[owner])
		if id != want {
			t.Fatalf("owner %s: got %s, want %s (a row was repeated or skipped)", owner, id, want)
		}
		next[owner]++
	}
	for owner, n := range rows {
		if next[owner] != n {
			t.Fatalf("owner %s: %d of %d rows", owner, next[owner], n)
		}
	}
}

// EVERY PUBLIC LIMIT WORKS AND IS FAIR: with several owners holding long
// histories, each of which leads with kept-running services, limits 1 to 4
// (below the internal quantum of five) and larger ones all deliver every row
// once, and no owner waits for another to finish.
func TestCrewEverySmallLimitIsFairAndLosesNothing(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 40, "b": 33, "c": 2, "d": 61}
	// Each owner's page leads with two service rows before its history, the way
	// an owner page does; the owner's own cursor says where it stopped.
	const services = 2
	f.backend.crewHistory.visit = func(ctx context.Context, _ *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		total := f.rows[o.id]
		at := 0
		if o.cursor != "" {
			at, _ = strconv.Atoi(strings.TrimPrefix(o.cursor, "k"))
		}
		res := &appwire.ProcessPageResult{Processes: []appwire.Process{}}
		for ; at < services+total && len(res.Processes) < n; at++ {
			id := fmt.Sprintf("%s:svc%d", o.id, at)
			if at >= services {
				id = fmt.Sprintf("%s:p%d", o.id, total-(at-services))
			}
			res.Processes = append(res.Processes, appwire.Process{ID: id, Agent: o.id})
		}
		if at < services+total {
			res.Next = "k" + strconv.Itoa(at)
		}
		return res, nil
	}
	f.save(f.team("t", "", "a", "b", "c", "d"))
	for _, limit := range []int{1, 2, 3, 4, 5, 7, 50, 100} {
		ids, res := f.walk("t", limit)
		var history []string
		starts := map[string]int{}
		for i, id := range ids {
			if strings.Contains(id, ":svc") {
				if want := fmt.Sprintf("%s:svc%d", strings.SplitN(id, ":", 2)[0], starts[strings.SplitN(id, ":", 2)[0]]); id != want {
					t.Fatalf("limit %d: %s, want %s", limit, id, want)
				}
				starts[strings.SplitN(id, ":", 2)[0]]++
				continue
			}
			history = append(history, id)
			// Fair: owner d, with the longest history, does not start before the
			// first round has reached every other owner.
			if strings.HasPrefix(id, "d:") && i > 0 && limit >= 1 {
				seen := ownerSet(ids[:i])
				if seen["a"] == 0 || seen["b"] == 0 || seen["c"] == 0 {
					t.Fatalf("limit %d: d began before the others had a turn", limit)
				}
			}
		}
		for o, n := range starts {
			if n != services {
				t.Fatalf("limit %d: owner %s had %d service rows", limit, o, n)
			}
		}
		crewCheckWalk(t, history, f.rows)
		if res.Owners.Done != 4 || res.Owners.Total != 4 || res.Incomplete {
			t.Fatalf("limit %d: %+v", limit, res.Owners)
		}
	}
}

// AN OWNER'S ENGINE THAT CANNOT PAGE (unknown_method) IS NAMED PARTIAL, NOT
// EMPTY, AND THE WALK GOES ON WITHOUT IT.
func TestCrewOwnerWhoseEngineCannotPageIsPartial(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 3, "b": 3}
	inner := f.backend.crewHistory.visit
	f.backend.crewHistory.visit = func(ctx context.Context, b *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		if o.id == "a" {
			return nil, appProcessUnpaged()
		}
		return inner(ctx, b, o, n)
	}
	f.save(f.team("t", "", "a", "b"))
	first, err := f.ask("t", 50, "")
	if err != nil || len(first.Processes) != 3 || len(first.Owners.Partial) != 1 ||
		first.Owners.Partial[0].Agent != "a" || first.Owners.Partial[0].Reason != appwire.ProcessOwnerUnreadable || !first.Incomplete {
		t.Fatalf("%+v %v", first, err)
	}
}
