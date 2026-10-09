package desktopbridge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

const placesToken = "0123456789abcdef0123456789abcdef"

var placesEpoch = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// placesRig is a Bridge with the places door attached to a store in a temp
// directory and a hand-built world, so every status asserted below is one the
// canonical session types were given, not one a test invented on the wire.
type placesRig struct {
	t     *testing.T
	b     *Bridge
	p     *Places
	srv   *httptest.Server
	path  string
	mu    sync.Mutex
	rows  []session.SessionRow
	clock time.Time
}

func newPlacesRig(t *testing.T) *placesRig {
	t.Helper()
	rig := &placesRig{t: t, path: filepath.Join(t.TempDir(), "places.json"), clock: placesEpoch}
	// Ids and times are deterministic so the contract fixtures are byte-stable.
	var ids int
	var idMu sync.Mutex
	clock := func() time.Time { rig.mu.Lock(); defer rig.mu.Unlock(); return rig.clock }
	store, err := placegraph.Open(placegraph.Options{Path: rig.path, Now: clock, NewID: func(prefix string) string {
		idMu.Lock()
		defer idMu.Unlock()
		ids++
		return fmt.Sprintf("%s%016x", prefix, ids)
	}})
	if err != nil {
		t.Fatal(err)
	}
	rig.b = New(placesToken, nil)
	rig.p = NewPlaces(store)
	rig.p.World = rig.world
	rig.p.Now = clock
	rig.b.UsePlaces(rig.p)
	rig.srv = httptest.NewServer(rig.b)
	t.Cleanup(rig.srv.Close)
	return rig
}

func (r *placesRig) world() session.World {
	r.mu.Lock()
	defer r.mu.Unlock()
	return session.World{Projects: []session.Project{{Name: "app", Sessions: append([]session.SessionRow(nil), r.rows...)}}, Read: r.clock}
}

func (r *placesRig) setRows(rows ...session.SessionRow) {
	r.mu.Lock()
	r.rows = rows
	r.clock = r.clock.Add(time.Minute) // past the world cache
	r.mu.Unlock()
}

func (r *placesRig) advance(d time.Duration) { r.mu.Lock(); r.clock = r.clock.Add(d); r.mu.Unlock() }

// do sends a request and decodes the JSON answer into out (nil to skip).
func (r *placesRig) do(method, path string, body any, out any) int {
	r.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, r.srv.URL+"/api/engine"+path, reader)
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+placesToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			r.t.Fatalf("%s %s: undecodable answer (%d): %v", method, path, resp.StatusCode, err)
		}
	}
	return resp.StatusCode
}

type apiError struct {
	Error   string               `json:"error"`
	Code    string               `json:"code"`
	Applied []placegraph.Receipt `json:"applied"`
	Undone  *int                 `json:"undone"`
}

// mk creates a place and returns its id, failing the test on any refusal.
func (r *placesRig) mk(name string, parents ...string) string {
	r.t.Helper()
	var m Mutation
	if code := r.do("POST", "/places", map[string]any{"name": name, "parents": parents}, &m); code != 200 {
		r.t.Fatalf("create %q: %d", name, code)
	}
	return m.Place.ID
}

func row(id, title string, at time.Time) session.SessionRow {
	return session.SessionRow{ID: id, Title: title, Project: "app", Workspace: "/w", At: at}
}

// waiting is a conversation whose live presence says it is stopped on a person.
func waiting(r session.SessionRow, reason string) session.SessionRow {
	r.Live = true
	r.Presence = session.SessionPresence{SessionID: r.ID, State: session.PresenceWaiting, Reason: reason}
	return r
}

// working is a conversation with one task node out, named by presence and the index.
func working(r session.SessionRow, taskID, label string, started time.Time) session.SessionRow {
	r.Live = true
	r.Presence = session.SessionPresence{SessionID: r.ID, State: session.PresenceWorking, RunningTasks: []session.PresenceTask{{ID: taskID, Title: label, State: "running"}}}
	r.Tasks = session.TaskRollup{Running: 1, Rows: []session.TaskIndexEntry{{ID: taskID, Label: label, Title: label, Status: string(session.TaskRunning), StartedAt: started}}}
	return r
}

func TestPlacesRoutesNeedTheToken(t *testing.T) {
	rig := newPlacesRig(t)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/places"}, {"POST", "/places"}, {"GET", "/places/status"}, {"GET", "/places/root"}, {"POST", "/places/undo"}, {"GET", "/chats/x/places"},
	} {
		for _, auth := range []string{"", "Bearer wrong", "Bearer " + placesToken + "x"} {
			req, _ := http.NewRequest(tc.method, rig.srv.URL+"/api/engine"+tc.path, nil)
			if auth != "" {
				req.Header.Set("Authorization", auth)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 401 {
				t.Errorf("%s %s with %q: got %d, want 401", tc.method, tc.path, auth, resp.StatusCode)
			}
		}
	}
}

func TestPlacesAreAbsentUntilAttached(t *testing.T) {
	b := New(placesToken, nil)
	srv := httptest.NewServer(b)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/engine/places", nil)
	req.Header.Set("Authorization", "Bearer "+placesToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("got %d, want 404 while no store is attached", resp.StatusCode)
	}
}

func TestCreateRenameTintAndInstructionsRoundTrip(t *testing.T) {
	rig := newPlacesRig(t)
	var created Mutation
	if code := rig.do("POST", "/places", map[string]any{"name": "  codeaf  ", "tint": "iris", "instructions": "Be strict."}, &created); code != 200 {
		t.Fatalf("create: %d", code)
	}
	id := created.Place.ID
	if created.Noop || len(created.Receipts) != 1 || len(created.Undo) != 1 || created.Place.Name != "codeaf" || created.Place.EffectiveTint != placegraph.TintIris || created.Place.Instructions != "Be strict." {
		t.Fatalf("unexpected create receipt: %+v", created)
	}
	var updated Mutation
	code := rig.do("POST", "/places/"+id, map[string]any{"name": "codeaf core", "tint": "rose", "instructions": "Be kind.", "policy": map[string]any{"model": "deepseek/deepseek-v4.1-flash"}}, &updated)
	if code != 200 || len(updated.Receipts) != 4 {
		t.Fatalf("update: %d %+v", code, updated)
	}
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.Kind != "place" || home.Title != "codeaf core" || home.Place.Tint != placegraph.TintRose || home.Place.Instructions != "Be kind." || home.Place.Policy.Model != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("home did not read back the writes: %+v", home.Place)
	}
	// Clearing the tint with "" is a real write, and a no-change write is reported as one.
	var cleared, same Mutation
	rig.do("POST", "/places/"+id, map[string]any{"tint": ""}, &cleared)
	rig.do("POST", "/places/"+id, map[string]any{"tint": ""}, &same)
	if cleared.Noop || !same.Noop || len(same.Receipts) != 0 || len(same.Undo) != 0 {
		t.Fatalf("noop honesty: cleared=%+v same=%+v", cleared, same)
	}
	// The graph is on disk where it was injected, not somewhere hidden.
	if _, err := os.Stat(rig.path); err != nil {
		t.Fatalf("store file missing at the injected path: %v", err)
	}
}

func TestAStaleRevisionIs409AndWritesNothing(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("A")
	var g graphResponse
	rig.do("GET", "/places", nil, &g)
	rig.mk("B") // another window moves the graph
	var e apiError
	if code := rig.do("POST", "/places/"+id, map[string]any{"name": "A2", "ifRevision": g.Revision}, &e); code != 409 || e.Code != "stale" {
		t.Fatalf("got %d %+v, want 409 stale", code, e)
	}
	var after graphResponse
	rig.do("GET", "/places", nil, &after)
	for _, pl := range after.Places {
		if pl.ID == id && pl.Name != "A" {
			t.Fatalf("a stale write changed the name to %q", pl.Name)
		}
	}
}

func TestALoopingParentIs409WithASentence(t *testing.T) {
	rig := newPlacesRig(t)
	a := rig.mk("Software")
	b := rig.mk("Release", a)
	var e apiError
	code := rig.do("POST", "/places/"+a+"/parents", map[string]any{"add": b}, &e)
	if code != 409 || e.Code != "cycle" || !strings.Contains(e.Error, "“Software”") || !strings.Contains(e.Error, "“Release”") || strings.Contains(e.Error, "placegraph") {
		t.Fatalf("cycle answer: %d %+v", code, e)
	}
	if code := rig.do("POST", "/places/"+a+"/parents", map[string]any{"add": a}, &e); code != 409 || !strings.Contains(e.Error, "inside itself") {
		t.Fatalf("self parent: %d %+v", code, e)
	}
	if code := rig.do("POST", "/places/"+a+"/parents", map[string]any{"add": b, "remove": b}, &e); code != 400 {
		t.Fatalf("two verbs in one request: %d", code)
	}
}

func TestErrorsAreSentencesNotGoText(t *testing.T) {
	rig := newPlacesRig(t)
	rig.mk("Reports")
	var e apiError
	if code := rig.do("POST", "/places", map[string]any{"name": "reports"}, &e); code != 409 || e.Code != "name_taken" {
		t.Fatalf("duplicate sibling name: %d %+v", code, e)
	}
	for _, tc := range []struct {
		method, path string
		body         any
		status       int
	}{
		{"POST", "/places", map[string]any{"name": ""}, 400},
		{"POST", "/places", map[string]any{"name": "x", "tint": "pink"}, 400},
		{"POST", "/places", map[string]any{"name": "x", "bogus": 1}, 400},
		{"GET", "/places/pl_00000000000000ff", nil, 404},
		{"POST", "/places/pl_00000000000000ff", map[string]any{"name": "x"}, 404},
		{"POST", "/places/undo", map[string]any{"receipts": []string{"rc_missing"}}, 410},
		{"GET", "/places/undo", nil, 405},
		{"PUT", "/places", nil, 405},
	} {
		var e apiError
		if code := rig.do(tc.method, tc.path, tc.body, &e); code != tc.status {
			t.Errorf("%s %s: got %d, want %d (%+v)", tc.method, tc.path, code, tc.status, e)
		}
		if strings.Contains(e.Error, "placegraph:") || strings.Contains(e.Error, "json:") {
			t.Errorf("%s %s leaked Go error text: %q", tc.method, tc.path, e.Error)
		}
	}
}

func TestMembershipNeedsRealChatsAndWritesNothingOtherwise(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("codeaf")
	rig.setRows(row("s1", "Config parser", placesEpoch))
	var e apiError
	if code := rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"s1", "ghost"}}, &e); code != 404 || e.Code != "unknown_chat" {
		t.Fatalf("got %d %+v", code, e)
	}
	var who chatPlacesResponse
	rig.do("GET", "/chats/s1/places", nil, &who)
	if len(who.Places) != 0 || !who.Known {
		t.Fatalf("the refused request must not have filed s1: %+v", who)
	}
	var ghost chatPlacesResponse
	rig.do("GET", "/chats/ghost/places", nil, &ghost)
	if ghost.Known {
		t.Fatal("an id the world has never seen must not be reported known")
	}
	// A conversation this bridge holds open is real even before the world can read it.
	rig.p.live = func() []string { return []string{"fresh"} }
	var m Mutation
	if code := rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"fresh"}, "addedBy": "ai"}, &m); code != 200 || len(m.Memberships) != 1 || m.Memberships[0].AddedBy != placegraph.AddedByAI {
		t.Fatalf("live chat filing: %d %+v", code, m)
	}
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.MissingChats != 1 || len(home.Chats) != 0 || home.Status.Chats != 0 {
		t.Fatalf("an unreadable chat must be counted as missing, never drawn: %+v", home)
	}
}

func TestMembershipMoveRemoveAndUndo(t *testing.T) {
	rig := newPlacesRig(t)
	soft := rig.mk("Software")
	mkt := rig.mk("Marketing")
	rig.setRows(row("s1", "One", placesEpoch), row("s2", "Two", placesEpoch.Add(-time.Hour)))
	var added Mutation
	rig.do("POST", "/places/"+soft+"/members", map[string]any{"chats": []string{"s1", "s2"}}, &added)
	if len(added.Receipts) != 2 || len(added.Undo) != 2 {
		t.Fatalf("two filings must be two receipts: %+v", added)
	}
	var moved Mutation
	if code := rig.do("POST", "/places/"+mkt+"/members", map[string]any{"chats": []string{"s1"}, "moveFrom": soft}, &moved); code != 200 || len(moved.Receipts) != 1 {
		t.Fatalf("move: %d %+v", code, moved)
	}
	var who chatPlacesResponse
	rig.do("GET", "/chats/s1/places", nil, &who)
	if len(who.Places) != 1 || who.Places[0].ID != mkt {
		t.Fatalf("s1 should now be only in Marketing: %+v", who)
	}
	// Moving into Now is unfiling; filing straight into Now is refused with the reason.
	var e apiError
	if code := rig.do("POST", "/places/now/members", map[string]any{"chats": []string{"s2"}}, &e); code != 400 {
		t.Fatalf("filing into Now: %d", code)
	}
	var out Mutation
	rig.do("POST", "/places/now/members", map[string]any{"chats": []string{"s2"}, "moveFrom": soft}, &out)
	var now digestResponse
	rig.do("GET", "/places/now", nil, &now)
	if now.Kind != "now" || len(now.Chats) != 1 || now.Chats[0].ID != "s2" {
		t.Fatalf("Now should hold the unfiled chat: %+v", now)
	}
	// Only the newest receipt can be undone: the older one is refused while a newer
	// change stands on top of it, then succeeds once that one is taken back.
	var undone struct {
		Revision uint64 `json:"revision"`
		Undone   int    `json:"undone"`
	}
	if code := rig.do("POST", "/places/undo", map[string]any{"receipts": moved.Undo}, &e); code != 409 || e.Code != "cannot_undo" || e.Undone == nil || *e.Undone != 0 {
		t.Fatalf("undo of an older receipt under a newer one: %d %+v", code, e)
	}
	if code := rig.do("POST", "/places/undo", map[string]any{"receipts": out.Undo}, &undone); code != 200 || undone.Undone != 1 {
		t.Fatalf("undo: %d %+v", code, undone)
	}
	if code := rig.do("POST", "/places/undo", map[string]any{"receipts": moved.Undo}, &undone); code != 200 {
		t.Fatalf("the older receipt is undoable once the newer is gone: %d", code)
	}
	rig.do("GET", "/chats/s1/places", nil, &who)
	if len(who.Places) != 1 || who.Places[0].ID != soft {
		t.Fatalf("undoing the move puts s1 back in Software: %+v", who)
	}
	rig.do("POST", "/places/"+soft+"/members/remove", map[string]any{"chats": []string{"s1"}}, nil)
	rig.do("POST", "/places/"+mkt+"/members", map[string]any{"chats": []string{"s1"}}, nil)
	var removed Mutation
	rig.do("POST", "/places/"+mkt+"/members/remove", map[string]any{"chats": []string{"s1"}}, &removed)
	if len(removed.Receipts) != 1 {
		t.Fatalf("remove: %+v", removed)
	}
}

func TestMultiStepUndoUnwindsInReverseAndAPartialFailureNamesWhatApplied(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("codeaf")
	var m Mutation
	rig.do("POST", "/places/"+id, map[string]any{"name": "core", "tint": "sage", "instructions": "x"}, &m)
	if len(m.Undo) != 3 {
		t.Fatalf("receipts: %+v", m)
	}
	var undone struct{ Undone int }
	if code := rig.do("POST", "/places/undo", map[string]any{"receipts": m.Undo}, &undone); code != 200 || undone.Undone != 3 {
		t.Fatalf("undo: %d %+v", code, undone)
	}
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.Title != "codeaf" || home.Place.Instructions != "" {
		t.Fatalf("not unwound: %+v", home.Place)
	}
	// The rename lands and the policy is refused: the answer says so.
	var e apiError
	code := rig.do("POST", "/places/"+id, map[string]any{"name": "renamed", "policy": map[string]any{"model": strings.Repeat("m", 500)}}, &e)
	if code != 400 || len(e.Applied) != 1 || e.Applied[0].Action != placegraph.ActionRename {
		t.Fatalf("partial failure: %d %+v", code, e)
	}
}

// The roll-up is read from SessionRow states the canonical world produced. A
// waiting chat counts as needs-you, a chat with a task node out as running, an
// idle one as neither, and a chat the world cannot read is in none of them.
func TestGraphListCarriesRollupAndTotals(t *testing.T) {
	rig := newPlacesRig(t)
	soft := rig.mk("Software")
	cfg := rig.mk("Config parser", soft)
	rel := rig.mk("Release", soft)
	rig.mk("Personal")
	rig.setRows(
		waiting(row("need", "Port fix", placesEpoch), "Allow the v1 branch push?"),
		working(row("run", "Fixtures", placesEpoch.Add(-time.Minute)), "7", "Update fixtures", placesEpoch.Add(-2*time.Minute)),
		row("idle", "Naming", placesEpoch.Add(-time.Hour)),
		row("loose", "Pricing", placesEpoch.Add(-2*time.Hour)),
	)
	for chat, place := range map[string]string{"need": cfg, "run": cfg, "idle": rel} {
		rig.do("POST", "/places/"+place+"/members", map[string]any{"chats": []string{chat}}, nil)
	}
	rig.p.live = func() []string { return []string{"unsaved"} }
	rig.do("POST", "/places/"+rel+"/members", map[string]any{"chats": []string{"unsaved"}}, nil)
	// A chat in two sibling places under one parent is counted once above them.
	rig.do("POST", "/places/"+rel+"/members", map[string]any{"chats": []string{"need"}}, nil)

	var g graphResponse
	if code := rig.do("GET", "/places", nil, &g); code != 200 {
		t.Fatalf("graph: %d", code)
	}
	by := map[string]PlaceView{}
	for _, pl := range g.Places {
		by[pl.ID] = pl
	}
	if s := by[cfg].Status; s.Chats != 2 || s.NeedsYou != 1 || s.Running != 1 {
		t.Errorf("config direct status: %+v", s)
	}
	if s := by[soft].StatusInclusive; s.Chats != 3 || s.NeedsYou != 1 || s.Running != 1 {
		t.Errorf("software must count the diamond chat once: %+v", s)
	}
	if s := by[soft].Status; s.Chats != 0 {
		t.Errorf("software has no chats of its own: %+v", s)
	}
	if by[soft].Counts.Descendants != 2 || by[soft].Counts.ChatsInclusive != 4 {
		t.Errorf("software counts: %+v", by[soft].Counts)
	}
	if g.Totals.Places != 4 || g.Totals.Placed != 4 || g.Totals.Unplaced != 1 || g.Totals.NeedsYou != 1 || g.Totals.Running != 1 || g.Totals.MissingChats != 1 {
		t.Errorf("totals: %+v", g.Totals)
	}
	if g.Now.Chats != 1 || g.Now.Status.Chats != 1 || g.Now.Status.Running != 0 {
		t.Errorf("now: %+v", g.Now)
	}
	// Every number agrees with the store's own counts: one source of truth.
	snap, _ := rig.p.Store.Snapshot()
	for _, pl := range g.Places {
		if want := snap.Counts(pl.ID); want != pl.Counts {
			t.Errorf("%s counts %+v differ from the store's %+v", pl.Name, pl.Counts, want)
		}
	}
	var st struct {
		Places map[string]statusEntry `json:"places"`
		Totals Totals                 `json:"totals"`
	}
	rig.do("GET", "/places/status", nil, &st)
	if st.Places[soft].StatusInclusive != by[soft].StatusInclusive || st.Totals != g.Totals {
		t.Errorf("the status route disagrees with the graph: %+v", st)
	}
}

func TestArchivedConversationsAreNotCountedAsWork(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("codeaf")
	old := waiting(row("old", "Old", placesEpoch), "still asking?")
	old.Archived = true
	rig.setRows(old)
	rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{"old"}}, nil)
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.Status.NeedsYou != 0 || len(home.Attention) != 0 || len(home.Chats) != 1 || !home.Chats[0].Archived {
		t.Fatalf("an archived conversation is listed but is not news: %+v", home)
	}
}

func TestHomeDigestListsAttentionBreadcrumbAndChats(t *testing.T) {
	rig := newPlacesRig(t)
	soft := rig.mk("Software")
	cfg := rig.mk("Config parser", soft)
	other := rig.mk("Docs", soft)
	rig.setRows(
		working(row("run", "Fixtures", placesEpoch.Add(-time.Minute)), "7", "Update fixtures", placesEpoch.Add(-2*time.Minute)),
		waiting(row("need", "Port fix", placesEpoch.Add(-time.Hour)), "Allow the push?"),
		row("quiet", "Notes", placesEpoch.Add(-3*time.Hour)),
	)
	rig.do("POST", "/places/"+cfg+"/members", map[string]any{"chats": []string{"run", "need"}}, nil)
	rig.do("POST", "/places/"+other+"/members", map[string]any{"chats": []string{"quiet"}}, nil)

	var home digestResponse
	rig.do("GET", "/places/"+cfg, nil, &home)
	if len(home.Breadcrumb) != 1 || home.Breadcrumb[0].ID != soft {
		t.Errorf("breadcrumb: %+v", home.Breadcrumb)
	}
	if len(home.Chats) != 2 || home.Chats[0].ID != "run" || home.Chats[1].ID != "need" || !home.Chats[1].NeedsYou || home.Chats[1].Reason != "Allow the push?" || home.Chats[0].Doing != "working" {
		t.Errorf("chats (newest spoken first, statuses from presence): %+v", home.Chats)
	}
	if len(home.Attention) != 2 || home.Attention[0].Kind != "needsYou" || home.Attention[0].Text != "Allow the push?" || home.Attention[1].Kind != "running" || home.Attention[1].TaskID != "7" || home.Attention[1].Text != "Update fixtures" || home.Attention[1].PlaceName != "Config parser" {
		t.Errorf("attention: %+v", home.Attention)
	}
	// The parent's Home carries its children's news, attributed to the child.
	var parent digestResponse
	rig.do("GET", "/places/"+soft, nil, &parent)
	if len(parent.Children) != 2 || len(parent.Chats) != 0 || len(parent.Attention) != 2 || parent.Attention[0].PlaceID != cfg {
		t.Errorf("parent digest: children=%d chats=%d attention=%+v", len(parent.Children), len(parent.Chats), parent.Attention)
	}
	var root digestResponse
	rig.do("GET", "/places/root", nil, &root)
	if root.Kind != "root" || root.Title != "All places" || len(root.Children) != 1 || len(root.Chats) != 3 || root.Place != nil {
		t.Errorf("root digest: %+v", root)
	}
}

func TestRailIsPinnedThenRecentlyOpenedAndBusyPlaces(t *testing.T) {
	rig := newPlacesRig(t)
	a, b, c, d := rig.mk("A"), rig.mk("B"), rig.mk("C"), rig.mk("D")
	var e apiError
	rig.do("POST", "/places/"+a+"/pin", nil, &e)
	rig.do("POST", "/places/"+b+"/pin", nil, &e)
	rig.do("POST", "/places/"+b+"/pin", map[string]any{"index": 0}, &e) // re-pin reorders
	rig.do("POST", "/places/"+c+"/visit", nil, nil)
	rig.advance(time.Hour)
	rig.do("POST", "/places/"+d+"/visit", nil, nil)
	var rail RailView
	rig.do("GET", "/places/rail", nil, &rail)
	if len(rail.Pinned) != 2 || rail.Pinned[0].ID != b || rail.Pinned[1].ID != a {
		t.Fatalf("pinned order: %+v", rail.Pinned)
	}
	if len(rail.Open) != 2 || rail.Open[0].ID != d || rail.Open[1].ID != c || rail.OpenWindowHours != 12 {
		t.Fatalf("open: %+v", rail.Open)
	}
	// Past the idle window a quiet place drops off; one with work in it stays.
	rig.setRows(waiting(row("need", "Ask", placesEpoch), "ok?"))
	rig.do("POST", "/places/"+c+"/members", map[string]any{"chats": []string{"need"}}, nil)
	rig.advance(13 * time.Hour)
	rig.do("GET", "/places/rail", nil, &rail)
	if len(rail.Open) != 1 || rail.Open[0].ID != c {
		t.Fatalf("after 13h only the busy place stays open: %+v", rail.Open)
	}
	// Visiting must not move the revision, so it cannot invalidate an undo.
	var before, after graphResponse
	rig.do("GET", "/places", nil, &before)
	rig.do("POST", "/places/"+a+"/visit", nil, nil)
	rig.do("GET", "/places", nil, &after)
	if before.Revision != after.Revision {
		t.Fatalf("visit moved the revision %d -> %d", before.Revision, after.Revision)
	}
	if code := rig.do("POST", "/places/pl_00000000000000ff/visit", nil, &e); code != 404 {
		t.Fatalf("visit of a missing place: %d", code)
	}
}

func TestArchiveDeleteMergeAndTheirUndo(t *testing.T) {
	rig := newPlacesRig(t)
	soft := rig.mk("Software")
	rel := rig.mk("Release", soft)
	rig.setRows(row("s1", "One", placesEpoch))
	rig.do("POST", "/places/"+rel+"/members", map[string]any{"chats": []string{"s1"}}, nil)

	var prev placegraph.DeleteImpact
	rig.do("GET", "/places/"+rel+"/delete-preview", nil, &prev)
	if prev.ChatsHere != 1 || len(prev.WouldBeUnplaced) != 1 {
		t.Fatalf("preview: %+v", prev)
	}
	var arch Mutation
	rig.do("POST", "/places/"+rel+"/archive", nil, &arch)
	var g graphResponse
	rig.do("GET", "/places", nil, &g)
	if len(g.Places) != 1 || g.Totals.Unplaced != 1 {
		t.Fatalf("an archived place is hidden and its chat is back in Now: %+v", g.Totals)
	}
	rig.do("GET", "/places?archived=1", nil, &g)
	if len(g.Places) != 2 {
		t.Fatalf("archived=1 lists archived places too: %d", len(g.Places))
	}
	var restored Mutation
	rig.do("POST", "/places/"+rel+"/restore", nil, &restored)

	var del Mutation
	if code := rig.do("POST", "/places/"+soft+"/delete", nil, &del); code != 200 || len(del.Undo) != 1 {
		t.Fatalf("delete: %d %+v", code, del)
	}
	raw, _ := json.Marshal(del.Result)
	if !strings.Contains(string(raw), `"childrenMoved":1`) {
		t.Fatalf("delete result: %s", raw)
	}
	var cut Mutation
	rig.do("POST", "/places/"+rel, map[string]any{"name": "Release 2"}, &cut)
	var undone struct{ Undone int }
	var e apiError
	if code := rig.do("POST", "/places/undo", map[string]any{"receipts": del.Undo}, &e); code != 409 {
		t.Fatalf("undo of a delete after a later change must be refused: %d %+v", code, e)
	}
	rig.do("POST", "/places/undo", map[string]any{"receipts": cut.Undo}, &undone)
	if code := rig.do("POST", "/places/undo", map[string]any{"receipts": del.Undo}, &undone); code != 200 {
		t.Fatalf("undo delete: %d", code)
	}
	rig.do("GET", "/places", nil, &g)
	if len(g.Places) != 2 {
		t.Fatalf("delete undone should bring Software back: %d places", len(g.Places))
	}
	other := rig.mk("Docs")
	var merged Mutation
	if code := rig.do("POST", "/places/"+other+"/merge", map[string]any{"into": soft}, &merged); code != 200 || merged.Place == nil || merged.Place.ID != soft {
		t.Fatalf("merge: %d %+v", code, merged)
	}
}

func TestSourcesAreValidatedCanonicalisedAndCheckedWithoutReading(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("codeaf")
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	os.MkdirAll(filepath.Join(repo, ".git"), 0o755)
	file := filepath.Join(dir, "brand-voice.md")
	os.WriteFile(file, []byte("secret contents"), 0o644)
	link := filepath.Join(dir, "link")
	os.Symlink(repo, link)

	var m Mutation
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "repo", "ref": link}, &m); code != 200 {
		t.Fatalf("repo via symlink: %d", code)
	}
	real, _ := filepath.EvalSymlinks(repo)
	if got := m.Place.Sources; len(got) != 1 || got[0].Ref != real || got[0].Label != "repo" || got[0].ID == "" || got[0].Check.State != "ok" || got[0].AddedBy != placegraph.AddedByYou {
		t.Fatalf("source: %+v", m.Place.Sources)
	}
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "file", "ref": file, "label": "Voice"}, &m); code != 200 || m.Place.Sources[1].Label != "Voice" {
		t.Fatalf("file: %d", code)
	}
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "url", "ref": "https://codeaf.dev/docs"}, &m); code != 200 || m.Place.Sources[2].Check.State != "unknown" || m.Place.SourceCount != 3 {
		t.Fatalf("url: %d %+v", code, m.Place.Sources)
	}
	var e apiError
	for _, tc := range []struct {
		body   map[string]any
		status int
		code   string
	}{
		{map[string]any{"kind": "repo", "ref": repo}, 409, "duplicate_source"},
		{map[string]any{"kind": "folder", "ref": "relative/path"}, 400, "invalid_source"},
		{map[string]any{"kind": "folder", "ref": filepath.Join(dir, "nope")}, 404, "invalid_source"},
		{map[string]any{"kind": "folder", "ref": file}, 400, "invalid_source"},
		{map[string]any{"kind": "file", "ref": dir}, 400, "invalid_source"},
		{map[string]any{"kind": "repo", "ref": dir}, 400, "invalid_source"},
		{map[string]any{"kind": "url", "ref": "file:///etc/passwd"}, 400, "invalid_source"},
		{map[string]any{"kind": "url", "ref": "https://user:pw@example.com/"}, 400, "invalid_source"},
		{map[string]any{"kind": "chat", "ref": "ghost"}, 404, "invalid_source"},
		{map[string]any{"kind": "wiki", "ref": "x"}, 400, "invalid_source"},
	} {
		if code := rig.do("POST", "/places/"+id+"/sources", tc.body, &e); code != tc.status || e.Code != tc.code {
			t.Errorf("%v: got %d %+v", tc.body, code, e)
		}
	}
	// A source that disappears is reported missing, not silently kept as fine.
	os.Remove(file)
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.Place.Sources[1].Check.State != "missing" {
		t.Fatalf("a deleted file must read as missing: %+v", home.Place.Sources[1])
	}
	// Removing one keeps the rest, and the receipt undoes it.
	var rm Mutation
	if code := rig.do("POST", "/places/"+id+"/sources/remove", map[string]any{"sourceId": home.Place.Sources[1].ID}, &rm); code != 200 || len(rm.Place.Sources) != 2 {
		t.Fatalf("remove: %d %+v", code, rm.Place)
	}
	if code := rig.do("POST", "/places/"+id+"/sources/remove", map[string]any{"sourceId": "src_nope"}, &e); code != 404 {
		t.Fatalf("remove unknown: %d", code)
	}
	var undone struct{ Undone int }
	rig.do("POST", "/places/undo", map[string]any{"receipts": rm.Undo}, &undone)
	rig.do("GET", "/places/"+id, nil, &home)
	if len(home.Place.Sources) != 3 {
		t.Fatalf("undo should restore the source: %d", len(home.Place.Sources))
	}
}

func TestAChatSourceChecksAgainstTheWorld(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("codeaf")
	rig.setRows(row("s1", "Config parser", placesEpoch))
	var m Mutation
	if code := rig.do("POST", "/places/"+id+"/sources", map[string]any{"kind": "chat", "ref": "s1"}, &m); code != 200 {
		t.Fatalf("chat source: %d", code)
	}
	if s := m.Place.Sources[0]; s.Label != "Config parser" || s.Check.State != "ok" || s.Check.Title != "Config parser" {
		t.Fatalf("%+v", s)
	}
	rig.setRows()
	var home digestResponse
	rig.do("GET", "/places/"+id, nil, &home)
	if home.Place.Sources[0].Check.State != "missing" {
		t.Fatalf("a conversation that left the machine reads as missing: %+v", home.Place.Sources[0])
	}
}

func TestTwoBridgesOnOnePathNeverLoseAnUpdate(t *testing.T) {
	a := newPlacesRig(t)
	store, err := placegraph.Open(placegraph.Options{Path: a.path})
	if err != nil {
		t.Fatal(err)
	}
	bPlaces := NewPlaces(store)
	bPlaces.World = a.world
	other := New(placesToken, nil)
	other.UsePlaces(bPlaces)
	srv := httptest.NewServer(other)
	defer srv.Close()
	a.mk("from-a")
	req, _ := http.NewRequest("POST", srv.URL+"/api/engine/places", strings.NewReader(`{"name":"from-b"}`))
	req.Header.Set("Authorization", "Bearer "+placesToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("second bridge create: %v %v", err, resp)
	}
	resp.Body.Close()
	var g graphResponse
	a.do("GET", "/places", nil, &g)
	if len(g.Places) != 2 {
		t.Fatalf("each bridge must see the other's write: %d places", len(g.Places))
	}
}

func TestConcurrentWritesAreAllKeptAndNeverRace(t *testing.T) {
	rig := newPlacesRig(t)
	parent := rig.mk("Reports")
	rows := make([]session.SessionRow, 12)
	for i := range rows {
		rows[i] = row(fmt.Sprintf("s%02d", i), "chat", placesEpoch.Add(-time.Duration(i)*time.Minute))
	}
	rig.setRows(rows...)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var m Mutation
			code := rig.do("POST", "/places", map[string]any{"name": fmt.Sprintf("place-%02d", i), "parent": parent}, &m)
			if code != 200 {
				t.Errorf("create %d: %d", i, code)
				return
			}
			rig.do("POST", "/places/"+m.Place.ID+"/members", map[string]any{"chats": []string{fmt.Sprintf("s%02d", i)}}, nil)
			rig.do("GET", "/places", nil, nil)
			rig.do("GET", "/places/"+parent, nil, nil)
			rig.do("POST", "/places/"+m.Place.ID+"/visit", nil, nil)
		}()
	}
	wg.Wait()
	var g graphResponse
	rig.do("GET", "/places", nil, &g)
	if g.Totals.Places != 13 || g.Totals.Placed != 12 {
		t.Fatalf("lost updates: %+v", g.Totals)
	}
	var home digestResponse
	rig.do("GET", "/places/"+parent, nil, &home)
	if len(home.Children) != 12 || home.Counts.ChatsInclusive != 12 {
		t.Fatalf("parent: children=%d counts=%+v", len(home.Children), home.Counts)
	}
}

func TestACorruptStoreIsReportedNotHidden(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "places.json")
	os.WriteFile(path, []byte("{not json"), 0o644)
	store, err := placegraph.Open(placegraph.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	b := New(placesToken, nil)
	p := NewPlaces(store)
	p.World = func() session.World { return session.World{} }
	b.UsePlaces(p)
	srv := httptest.NewServer(b)
	defer srv.Close()
	req, _ := http.NewRequest("GET", srv.URL+"/api/engine/places", nil)
	req.Header.Set("Authorization", "Bearer "+placesToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var g graphResponse
	if err := json.NewDecoder(resp.Body).Decode(&g); err != nil {
		t.Fatal(err)
	}
	if g.Recovery == nil || g.Recovery.Kind != "quarantined" || g.Recovery.MovedTo == "" {
		t.Fatalf("recovery must be surfaced: %+v", g.Recovery)
	}
	if len(g.Places) != 0 || g.Places == nil {
		t.Fatalf("an empty graph is an empty list, not null: %#v", g.Places)
	}
}

func TestChatIDFromSessionFile(t *testing.T) {
	if got := ChatIDFromSessionFile("/home/u/.codeaf/v3/projects/b/abc123/transcript.jsonl"); got != "abc123" {
		t.Fatalf("got %q", got)
	}
	if ChatIDFromSessionFile("") != "" {
		t.Fatal("no file, no id")
	}
}
