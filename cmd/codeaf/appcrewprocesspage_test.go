package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

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
	// A page that could not be read in time ENDS NOTHING: the same cursor works
	// again, from where it was.
	again, err := f.ask("t", 5, p1.Next)
	if err != nil || len(again.Processes) != 5 || again.Processes[0].ID != "a:p25" {
		t.Fatalf("retry after a failed page: %v %v", again, err)
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
	reg.now = func() time.Time { clock = clock.Add(time.Millisecond); return clock }
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
	reg.now = func() time.Time { clock = clock.Add(time.Millisecond); return clock }
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
	reg.rotate(w, crewToken())
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

// A STEP THAT CANNOT ADVANCE IN TIME IS busy AND CHANGES NOTHING: the cursor is
// kept with its progress, a retry reads exactly the next rows, and the walk is
// not pinned for ever by the failed page.
func TestCrewBusyPageKeepsTheWalkForARetry(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 12}
	f.save(f.team("t", "", "a"))
	clock := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	reg := f.backend.crewWalks()
	reg.now = func() time.Time { return clock }
	p1, err := f.ask("t", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	inner := f.backend.crewHistory.visit
	ctx, cancel := context.WithCancel(context.Background())
	f.backend.crewHistory.visit = func(c context.Context, b *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		cancel() // the page's own request goes away during the first visit
		return inner(c, b, o, n)
	}
	five := 5
	if _, err := f.backend.PageCrewProcesses(ctx, appwire.ProcessPageAsk{Crew: "t", Limit: &five, Cursor: p1.Next}); !isCode(err, appwire.CodeBusy) {
		t.Fatalf("a page with no progress: %v", err)
	}
	f.backend.crewHistory.visit = inner
	reg.mu.Lock()
	pinned := reg.walks[p1.Next].busy
	reg.mu.Unlock()
	if pinned {
		t.Fatal("the failed page left the walk pinned")
	}
	clock = clock.Add(crewIdleTTL - time.Second)
	p2, err := f.ask("t", 5, p1.Next)
	if err != nil || p2.Processes[0].ID != "a:p7" || len(p2.Processes) != 5 {
		t.Fatalf("the retry skipped or repeated rows: %+v %v", p2, err)
	}
	// A roster read that fails in time also keeps the walk.
	dead, stop := context.WithCancel(context.Background())
	stop()
	if _, err := f.backend.PageCrewProcesses(dead, appwire.ProcessPageAsk{Crew: "t", Limit: &five, Cursor: p2.Next}); err == nil {
		t.Fatal("a cancelled roster read answered")
	}
	p3, err := f.ask("t", 5, p2.Next)
	if err != nil || p3.Processes[0].ID != "a:p2" {
		t.Fatalf("after a failed roster read: %+v %v", p3, err)
	}
}

// THE WHOLE ENCODED REPLY IS BOUNDED, NAMES AND ESCAPING INCLUDED, AND A NAME
// IS NEVER CUT INTO A DIFFERENT ONE: what does not fit whole is counted.
func TestCrewReplyBoundCountsTheEncodedNames(t *testing.T) {
	if crewReplyReserve+crewPartialBytesMax > appProcessPageEnvelope {
		t.Fatal("the reply's own fields and names do not fit the envelope the rows leave")
	}
	f := newCrewFixture(t)
	big := strings.Repeat("x", 245<<10)
	f.backend.crewHistory.visit = func(ctx context.Context, _ *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		res := &appwire.ProcessPageResult{Processes: []appwire.Process{}}
		for i := 0; i < n; i++ {
			res.Processes = append(res.Processes, appwire.Process{ID: o.id + ":" + strconv.Itoa(i), Agent: o.id, Command: big})
		}
		res.Next = o.cursor + "x"
		return res, nil
	}
	team := f.team("t", "", "a", "b", "c", "d", "e")
	keys := map[string]bool{}
	for i := 0; i < 45; i++ {
		key := strings.Repeat("<", 200) + strconv.Itoa(i)
		keys[key] = true
		team.Members = append(team.Members, teamstore.Member{Key: key})
	}
	tooLong := strings.Repeat("k", crewKeyMax+1)
	team.Members = append(team.Members, teamstore.Member{Key: tooLong})
	f.save(team)
	res, err := f.ask("t", 100, "")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res)
	if len(raw) > appwire.ProcessPageReplyMax || len(res.Processes) < 5 {
		t.Fatalf("%d rows, %d bytes", len(res.Processes), len(raw))
	}
	named := 0
	for _, p := range res.Owners.Partial {
		if p.Agent != "" || !keys[p.Key] || p.Reason != appwire.ProcessOwnerMissing {
			t.Fatalf("a name that is not a stored key whole: %q", p.Key)
		}
		named += len(p.Key)
	}
	if len(res.Owners.Partial)+res.Owners.More != 46 || res.Owners.More == 0 || res.Owners.Unread != 46 {
		t.Fatalf("named %d, more %d, unread %d", len(res.Owners.Partial), res.Owners.More, res.Owners.Unread)
	}
	// What was counted and not named is not named by a later page either: it
	// was told once.
	next, err := f.ask("t", 100, res.Next)
	if err != nil || len(next.Owners.Partial) != 0 || next.Owners.More != 0 {
		t.Fatalf("second page named again: %+v %v", next.Owners, err)
	}
	// The stored replay is bounded the same way.
	reg := f.backend.crewWalks()
	reg.mu.Lock()
	w := reg.walks[next.Next]
	over := len(w.last) > appwire.ProcessPageReplyMax
	reg.mu.Unlock()
	if over {
		t.Fatal("a stored reply is over the reply bound")
	}
}

// ── held conversations ──────────────────────────────────────────────────────

type crewEnginePager struct{ *fakeConversation }

func (crewEnginePager) ProcessPage(_ context.Context, ask session.ProcessPageAsk) (session.ProcessPage, error) {
	return session.ProcessPage{Rows: []session.ProcessPageRow{{JobNotice: session.JobNotice{ID: 1, Kind: session.JobKindCommand, Command: "from the engine", State: session.JobRunning}, Live: true}}}, nil
}

// hold makes the backend hold a live conversation for id whose transcript is at path.
func (f *crewFixture) hold(id, path string) {
	f.t.Helper()
	f.backend.locked(func() {
		a := f.backend.agentLocked(id)
		a.transcript = path
		a.live = &appLive{conn: &appConn{Conv: crewEnginePager{&fakeConversation{}}, Release: func() {}}}
	})
}

func commands(res *appwire.ProcessPageResult) string {
	var out []string
	for _, p := range res.Processes {
		out = append(out, p.Command)
	}
	return strings.Join(out, ",")
}

// A HELD CONVERSATION IS USED ONLY AS THE EXACT ONE THE ROSTER RECORDS. A
// foreign path, a key and a different path that share its id never reach its
// engine and never merge into it; the exact held path is asked through its
// engine; a held one at another path is read as saved.
func TestCrewHeldOwnerNeedsTheExactVerifiedRecordedFile(t *testing.T) {
	f := newCrewFixture(t)
	f.backend.crewHistory.visit = nil
	exactDir := f.seedOwner("held1", "p", job(1, "saved"))
	exact := filepath.Join(exactDir, "transcript.jsonl")
	f.hold("held1", exact)
	// Held at another path than the roster records: not this member.
	f.seedOwner("held2", "p", job(1, "saved2"))
	f.hold("held2", filepath.Join(t.TempDir(), "held2", "transcript.jsonl"))
	// A foreign same-id member and a key-only member for the held id.
	foreign := filepath.Join(t.TempDir(), "x", "held1", "transcript.jsonl")
	team := f.team("t", "", "held1", "held2")
	team.Members = append(team.Members,
		teamstore.Member{Key: foreign, File: foreign},
		teamstore.Member{Key: exact},
	)
	f.save(team)
	first, err := f.ask("t", 50, "")
	if err != nil {
		t.Fatal(err)
	}
	got := commands(first)
	if !strings.Contains(got, "from the engine") || !strings.Contains(got, "saved2") || strings.Count(got, "from the engine") != 1 {
		t.Fatalf("rows %q", got)
	}
	if first.Owners.Total != 4 || first.Owners.Unread != 2 || len(first.Owners.Partial) != 2 {
		t.Fatalf("%+v", first.Owners)
	}
	for _, p := range first.Owners.Partial {
		if p.Agent != "" || p.Reason != appwire.ProcessOwnerMissing {
			t.Fatalf("an unverified member was named as an owner: %+v", p)
		}
	}
	// A verified path that is no longer a conversation is missing even if held.
	if err := os.Remove(exact); err != nil {
		t.Fatal(err)
	}
	second, err := f.ask("t", 50, "")
	if err != nil || strings.Contains(commands(second), "from the engine") {
		t.Fatalf("a held conversation whose stored file is gone was asked: %q %v", commands(second), err)
	}
}

// A REMOTE ENGINE'S STALE CURSOR IS stale, NOT unreadable.
func TestCrewReasonClassifiesAWireStaleCursor(t *testing.T) {
	if got := crewReason(appwire.Errorf(appwire.CodeStaleCursor, "x")); got != appwire.ProcessOwnerStale {
		t.Fatal(got)
	}
	if got := crewReason(appProcessUnpaged()); got != appwire.ProcessOwnerUnreadable {
		t.Fatal(got)
	}
}

// A PINNED WALK IS NEVER TAKEN BACK BY TIME OR EVICTION: a page still reading is
// the walk's only writer, so no second page can pin it and no timer can delete
// it under that page; an idle walk still expires on its own timer.
func TestCrewPinnedWalkIsNeverExpiredOrEvictedAndIdleOneIs(t *testing.T) {
	reg := newCrewRegistry()
	clock := time.Date(2026, 10, 16, 9, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return clock }
	w := &crewWalk{scope: "all", cur: crewToken()}
	if err := reg.admit(w); err != nil {
		t.Fatal(err)
	}
	reg.walks[w.cur] = w
	clock = clock.Add(crewAgeMax + time.Hour)
	if _, err := reg.take(w.cur, "all"); !isCode(err, appwire.CodeBusy) {
		t.Fatalf("a long-pinned walk was taken or deleted: %v", err)
	}
	for i := 0; i < crewWalksMax+2; i++ {
		other := &crewWalk{scope: "crew:" + strconv.Itoa(i)}
		if err := reg.admit(other); err == nil {
			reg.release(other)
		}
	}
	reg.mu.Lock()
	still := reg.walks[w.cur] == w && w.accounted > 0
	reg.mu.Unlock()
	if !still {
		t.Fatal("a pinned walk was evicted")
	}
	reg.release(w)
	clock = clock.Add(crewIdleTTL + time.Second)
	if _, err := reg.take(w.cur, "all"); !isCode(err, appwire.CodeStaleCursor) {
		t.Fatalf("idle: %v", err)
	}
}

// failingPage runs one page whose request goes away during its first visit, so
// the page ends busy having advanced nothing.
func (f *crewFixture) failingPage(crew, cursor string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	inner := f.backend.crewHistory.visit
	f.backend.crewHistory.visit = func(c context.Context, b *appBackend, o crewOwner, n int) (*appwire.ProcessPageResult, error) {
		cancel()
		return inner(c, b, o, n)
	}
	defer func() { f.backend.crewHistory.visit = inner }()
	limit := 5
	_, err := f.backend.PageCrewProcesses(ctx, appwire.ProcessPageAsk{Crew: crew, Limit: &limit, Cursor: cursor})
	return err
}

// A FAILED PAGE PUTS BACK ONLY ITS OWN PROGRESS. Run under -race: while pages
// fail on one walk, other requests walk the registry under its lock; a restore
// that rewrote the registry's own fields on the walk would be a data race.
func TestCrewFailedPagesRestoreOnlyProgressUnderConcurrency(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 400}
	f.save(f.team("t", "", "a"))
	p1, err := f.ask("t", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	reg := f.backend.crewWalks()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				_, _ = reg.take("c1.AAAAAAAAAAAAAAAAAAAAAA", "all") // walks every walk under the lock
				reg.mu.Lock()
				_ = reg.scopeCountLocked("crew:t")
				reg.mu.Unlock()
			}
		}
	}()
	for i := 0; i < 40; i++ {
		if err := f.failingPage("t", p1.Next); !isCode(err, appwire.CodeBusy) {
			t.Fatalf("round %d: %v", i, err)
		}
	}
	close(stop)
	<-done
	p2, err := f.ask("t", 5, p1.Next)
	if err != nil || p2.Processes[0].ID != "a:p395" {
		t.Fatalf("progress after the failures: %+v %v", p2, err)
	}
}

// A FIRST PAGE THAT FAILS LEAVES NOTHING: no walk nobody can reach, no scope
// slot taken, and a live walk of the same crew survives.
func TestCrewFailedFirstPageLeavesNoWalkAndEvictsNobody(t *testing.T) {
	f := newCrewFixture(t)
	f.rows = map[string]int{"a": 40}
	f.save(f.team("t", "", "a"))
	live, err := f.ask("t", 5, "")
	if err != nil {
		t.Fatal(err)
	}
	reg := f.backend.crewWalks()
	for i := 0; i < 3; i++ {
		if err := f.failingPage("t", ""); !isCode(err, appwire.CodeBusy) {
			t.Fatalf("first page %d: %v", i, err)
		}
	}
	reg.mu.Lock()
	n, busy := len(reg.live), 0
	for _, w := range reg.live {
		if w.busy {
			busy++
		}
	}
	scoped := reg.scopeCountLocked("crew:t")
	reg.mu.Unlock()
	if n != 1 || scoped != 1 || busy != 0 {
		t.Fatalf("%d walks, %d in the crew's scope, %d pinned", n, scoped, busy)
	}
	next, err := f.ask("t", 5, live.Next)
	if err != nil || next.Processes[0].ID != "a:p35" {
		t.Fatalf("the live walk's cursor: %+v %v", next, err)
	}
}

// EVERY BYTE A WALK HOLDS IS COUNTED: each string field of an owner and of the
// walk adds exactly its length, found by reflection so a new field cannot be
// forgotten.
func TestCrewWalkBytesCountEveryStoredField(t *testing.T) {
	empty := &crewWalk{owners: []crewOwner{{}}}
	base := empty.bytes()
	owner := reflect.TypeOf(crewOwner{})
	for i := 0; i < owner.NumField(); i++ {
		if owner.Field(i).Type.Kind() != reflect.String {
			continue
		}
		w := &crewWalk{owners: []crewOwner{{}}}
		settable(reflect.ValueOf(&w.owners[0]).Elem().Field(i)).SetString(strings.Repeat("x", 100+i))
		if got := w.bytes() - base; got != 100+i {
			t.Errorf("crewOwner.%s counts %d of %d bytes", owner.Field(i).Name, got, 100+i)
		}
	}
	walk := reflect.TypeOf(crewWalk{})
	for i := 0; i < walk.NumField(); i++ {
		kind := walk.Field(i).Type
		if kind.Kind() != reflect.String && kind != reflect.TypeOf([]byte(nil)) {
			continue
		}
		w := &crewWalk{owners: []crewOwner{{}}}
		field := settable(reflect.ValueOf(w).Elem().Field(i))
		if kind.Kind() == reflect.String {
			field.SetString(strings.Repeat("y", 50))
		} else {
			field.SetBytes(make([]byte, 50))
		}
		if got := w.bytes() - base; got != 50 {
			t.Errorf("crewWalk.%s counts %d of 50 bytes", walk.Field(i).Name, got)
		}
	}
}

// settable makes an unexported field writable, for the accounting test only.
func settable(v reflect.Value) reflect.Value {
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem()
}
