package desktopbridge

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
)

const staleDay = 24 * time.Hour

type staleReply struct {
	AfterDays  int                     `json:"afterDays"`
	SnoozeDays int                     `json:"snoozeDays"`
	Places     []placegraph.StalePlace `json:"places"`
}

func (r *placesRig) withStale() *placegraph.StaleBook {
	r.t.Helper()
	book, err := placegraph.OpenStale(filepath.Join(filepath.Dir(r.path), "places-stale.json"))
	if err != nil {
		r.t.Fatal(err)
	}
	r.p.Stale = book
	return book
}

func (r *placesRig) stale() staleReply {
	r.t.Helper()
	var out staleReply
	if code := r.do("GET", "/places/stale", nil, &out); code != 200 {
		r.t.Fatalf("GET /places/stale: %d", code)
	}
	return out
}

func TestStaleRoutesAreAbsentWithoutASnoozeFile(t *testing.T) {
	rig := newPlacesRig(t)
	if code := rig.do("GET", "/places/stale", nil, nil); code != 404 {
		t.Fatalf("GET: %d, want 404", code)
	}
	id := rig.mk("Old")
	if code := rig.do("POST", "/places/"+id+"/stale-snooze", nil, nil); code != 404 {
		t.Fatalf("POST: %d, want 404", code)
	}
}

func TestStaleRoutesNeedTheToken(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	for _, method := range []string{"GET", "POST"} {
		path := "/places/stale"
		if method == "POST" {
			path = "/places/pl_x/stale-snooze"
		}
		req, _ := http.NewRequest(method, rig.srv.URL+"/api/engine"+path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s without a token: %d", method, resp.StatusCode)
		}
	}
}

func TestSixtyDaysUntouchedOffersAPlaceAndTheBoundaryIsExact(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	id := rig.mk("Old launch")
	rig.advance(placegraph.StaleAfterDays*staleDay - time.Nanosecond)
	if got := rig.stale(); len(got.Places) != 0 {
		t.Fatalf("offered a nanosecond early: %+v", got.Places)
	}
	rig.advance(time.Nanosecond)
	got := rig.stale()
	if len(got.Places) != 1 || got.Places[0].ID != id || got.Places[0].DaysUntouched != 60 || got.AfterDays != 60 || got.SnoozeDays != 30 {
		t.Fatalf("got %+v", got)
	}
}

func TestOpeningAPlaceTouchesIt(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	id := rig.mk("Old launch")
	rig.advance(90 * staleDay)
	if len(rig.stale().Places) != 1 {
		t.Fatal("expected the place to be due")
	}
	if code := rig.do("POST", "/places/"+id+"/visit", nil, nil); code != 200 {
		t.Fatalf("visit: %d", code)
	}
	if got := rig.stale(); len(got.Places) != 0 {
		t.Fatalf("a place just opened is still offered: %+v", got.Places)
	}
}

func TestPinnedRunningAndWaitingPlacesAreNeverOffered(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	pinned := rig.mk("Pinned")
	running := rig.mk("Running")
	waits := rig.mk("Waits")
	plain := rig.mk("Plain")
	if code := rig.do("POST", "/places/"+pinned+"/pin", nil, nil); code != 200 {
		t.Fatalf("pin: %d", code)
	}
	rig.setRows(
		working(row("c1", "build", placesEpoch), "t1", "build", placesEpoch),
		waiting(row("c2", "approve", placesEpoch), "approve a write"),
	)
	rig.do("POST", "/places/"+running+"/members", map[string]any{"chats": []string{"c1"}}, nil)
	rig.do("POST", "/places/"+waits+"/members", map[string]any{"chats": []string{"c2"}}, nil)
	// 90 days on, the chats are still live in the world the rig hands out: busy beats age.
	rig.advance(90 * staleDay)
	got := rig.stale()
	if len(got.Places) != 1 || got.Places[0].ID != plain {
		t.Fatalf("got %+v, want only %q", got.Places, plain)
	}
}

func TestAChatSpokenInRecentlyKeepsItsPlaceFromBeingOffered(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	talked := rig.mk("Talked")
	quiet := rig.mk("Quiet")
	rig.advance(90 * staleDay)
	rig.setRows(row("c1", "recent", rig.clock.Add(-2*staleDay)))
	rig.do("POST", "/places/"+talked+"/members", map[string]any{"chats": []string{"c1"}}, nil)
	got := rig.stale()
	if len(got.Places) != 1 || got.Places[0].ID != quiet {
		t.Fatalf("got %+v", got.Places)
	}
}

func TestNotNowSurvivesAnotherWindowAndEndsOnTheThirtiethDay(t *testing.T) {
	rig := newPlacesRig(t)
	book := rig.withStale()
	id := rig.mk("Old launch")
	rig.advance(70 * staleDay)
	var done struct {
		OK    bool      `json:"ok"`
		Until time.Time `json:"until"`
	}
	snoozedAt := rig.clock
	if code := rig.do("POST", "/places/"+id+"/stale-snooze", nil, &done); code != 200 || !done.OK {
		t.Fatalf("snooze: %d %+v", code, done)
	}
	if !done.Until.Equal(snoozedAt.Add(placegraph.StaleSnoozeDays * staleDay)) {
		t.Fatalf("until %v", done.Until)
	}
	if len(rig.stale().Places) != 0 {
		t.Fatal("still offered right after Not now")
	}
	// A second window: a bridge over the same files, with nothing in memory.
	second := newPlacesRigAt(t, rig.path)
	second.clock = rig.clock
	second.withStale()
	if len(second.stale().Places) != 0 {
		t.Fatal("another window offered it again")
	}
	_ = book
	rig.advance(placegraph.StaleSnoozeDays*staleDay - time.Nanosecond)
	if len(rig.stale().Places) != 0 {
		t.Fatal("offered a nanosecond early")
	}
	rig.advance(time.Nanosecond)
	if got := rig.stale(); len(got.Places) != 1 || got.Places[0].ID != id {
		t.Fatalf("not offered again on day thirty: %+v", got.Places)
	}
}

func TestNotNowDoesNotTouchTheGraph(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	id := rig.mk("Old launch")
	before, _ := rig.p.Store.Revision()
	rig.advance(70 * staleDay)
	rig.do("POST", "/places/"+id+"/stale-snooze", nil, nil)
	after, _ := rig.p.Store.Revision()
	if before != after {
		t.Fatalf("a snooze moved the graph revision %d -> %d, which would break an undo", before, after)
	}
}

func TestNotNowForAPlaceThatIsGoneIsRefused(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	var e apiError
	if code := rig.do("POST", "/places/pl_0000000000000099/stale-snooze", nil, &e); code != 404 || e.Code != "not_found" {
		t.Fatalf("got %d %+v", code, e)
	}
}

func TestArchivingTheOfferedPlaceRemovesTheOffer(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	id := rig.mk("Old launch")
	rig.advance(70 * staleDay)
	if code := rig.do("POST", "/places/"+id+"/archive", nil, nil); code != 200 {
		t.Fatalf("archive: %d", code)
	}
	if got := rig.stale(); len(got.Places) != 0 {
		t.Fatalf("an archived place is still offered: %+v", got.Places)
	}
}

// newPlacesRigAt is a second window: a fresh bridge and store over the graph
// file another rig already wrote, sharing nothing in memory with it.
func newPlacesRigAt(t *testing.T, path string) *placesRig {
	t.Helper()
	rig := &placesRig{t: t, path: path, clock: placesEpoch}
	clock := func() time.Time { rig.mu.Lock(); defer rig.mu.Unlock(); return rig.clock }
	store, err := placegraph.Open(placegraph.Options{Path: path, Now: clock})
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

// TestStaleWireFixtures writes (UPDATE_PLACES_FIXTURES=1) or checks the real
// answers of the two stale routes, so the TypeScript client is tested against
// what the engine sends and not a hand-copied shape.
func TestStaleWireFixtures(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withStale()
	old := rig.mk("Launch week")
	rig.advance(75 * staleDay)
	got := map[string][]byte{}
	capture := func(name, method, path string) {
		req, _ := http.NewRequest(method, rig.srv.URL+"/api/engine"+path, nil)
		req.Header.Set("Authorization", "Bearer "+placesToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(resp.Body)
		got[name] = buf.Bytes()
	}
	capture("stale", "GET", "/places/stale")
	capture("stale-snoozed", "POST", "/places/"+old+"/stale-snooze")
	capture("stale-none", "GET", "/places/stale")
	update := os.Getenv("UPDATE_PLACES_FIXTURES") == "1"
	for name, data := range got {
		file := filepath.Join(placesFixtureDir, name+".json")
		if update {
			if err := os.WriteFile(file, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("fixture %s is missing; regenerate with UPDATE_PLACES_FIXTURES=1: %v", name, err)
		}
		if !bytes.Equal(want, data) {
			t.Errorf("the wire answer %q changed; regenerate the fixtures (UPDATE_PLACES_FIXTURES=1)\n--- committed\n%s\n--- now\n%s", name, want, data)
		}
	}
}
