package desktopbridge

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// placesRecords lists the `places` records the rig's bridge has put on the stream.
func placesRecords(rig *placesRig) []WorldRecord {
	rig.b.worldFeed().mu.Lock()
	defer rig.b.worldFeed().mu.Unlock()
	var out []WorldRecord
	for _, rc := range rig.b.worldFeed().ring {
		if rc.Type == "places" {
			out = append(out, rc)
		}
	}
	return out
}

func TestIfGenerationIsTheSameGuardAsIfRevision(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("A")
	var g graphResponse
	rig.do("GET", "/places", nil, &g)
	if g.Generation == 0 || g.Generation != g.Revision || len(g.Nodes) != len(g.Places) || g.Unplaced == nil {
		t.Fatalf("graph names: %+v", g)
	}
	rig.mk("B")
	var e apiError
	if code := rig.do("POST", "/places/"+id, map[string]any{"name": "A2", "ifGeneration": g.Generation}, &e); code != 409 || e.Code != "stale" {
		t.Fatalf("got %d %+v, want 409 stale", code, e)
	}
	var m Mutation
	if code := rig.do("POST", "/places/"+id, map[string]any{"name": "A2", "ifGeneration": g.Generation + 1}, &m); code != 200 || m.Generation != m.Revision {
		t.Fatalf("fresh generation: %d %+v", code, m)
	}
}

func TestMembershipWritesPublishOnePlacesRecord(t *testing.T) {
	rig := newPlacesRig(t)
	a := rig.mk("A")
	rig.setRows(row("c1", "One", placesEpoch))
	before := len(placesRecords(rig))
	var m Mutation
	if code := rig.do("POST", "/places/"+a+"/members", map[string]any{"chats": []string{"c1"}, "addedBy": "you"}, &m); code != 200 {
		t.Fatalf("members: %d", code)
	}
	recs := placesRecords(rig)
	if len(recs) != before+1 {
		t.Fatalf("want exactly one new places record, got %d", len(recs)-before)
	}
	var got placesRecord
	if err := json.Unmarshal(recs[len(recs)-1].Payload, &got); err != nil || got.Generation != m.Generation || len(got.Nodes) == 0 || len(got.Members) != 1 {
		t.Fatalf("record: %+v err=%v", got, err)
	}
	// A read and a no-op write publish nothing.
	rig.do("GET", "/places", nil, nil)
	rig.do("POST", "/places/"+a+"/members", map[string]any{"chats": []string{"c1"}}, nil)
	if n := len(placesRecords(rig)); n != before+1 {
		t.Fatalf("reads or no-ops published: %d", n-before)
	}
}

func TestRailOpsPinReorderVisitClose(t *testing.T) {
	rig := newPlacesRig(t)
	a, b := rig.mk("A"), rig.mk("B")
	for _, op := range []map[string]any{{"op": "pin", "place": a}, {"op": "pin", "place": b}} {
		if code := rig.do("POST", "/places/rail", op, nil); code != 200 {
			t.Fatalf("%v: %d", op, code)
		}
	}
	var m Mutation
	if code := rig.do("POST", "/places/rail", map[string]any{"op": "reorder", "order": []string{b, a}}, &m); code != 200 || m.Rail == nil || m.Rail.Pinned[0].ID != b {
		t.Fatalf("reorder: %d %+v", code, m.Rail)
	}
	rig.do("POST", "/places/rail", map[string]any{"op": "unpin", "place": a}, nil)
	rig.do("POST", "/places/rail", map[string]any{"op": "visit", "place": a}, nil)
	var rv RailView
	rig.do("GET", "/places/rail", nil, &rv)
	if len(rv.Pinned) != 1 || len(rv.Open) != 1 || rv.Open[0].ID != a {
		t.Fatalf("rail after visit: %+v", rv)
	}
	rig.do("POST", "/places/rail", map[string]any{"op": "close", "place": a}, nil)
	rig.do("GET", "/places/rail", nil, &rv)
	if len(rv.Open) != 0 {
		t.Fatalf("closed place still open: %+v", rv.Open)
	}
	var e apiError
	if code := rig.do("POST", "/places/rail", map[string]any{"op": "explode"}, &e); code != 400 || e.Error == "" {
		t.Fatalf("bad op: %d %+v", code, e)
	}
}

func TestSweepLetsGoOfIdleRowsButNotBusyOnes(t *testing.T) {
	rig := newPlacesRig(t)
	idle, busy := rig.mk("Idle"), rig.mk("Busy")
	rig.setRows(waiting(row("w", "Wait", placesEpoch), "Allow?"))
	rig.do("POST", "/places/"+busy+"/members", map[string]any{"chats": []string{"w"}}, nil)
	rig.do("POST", "/places/rail", map[string]any{"op": "visit", "place": idle}, nil)
	rig.do("POST", "/places/rail", map[string]any{"op": "visit", "place": busy}, nil)
	rig.advance(48 * time.Hour)
	rig.p.sweepOnce()
	var rv RailView
	rig.do("GET", "/places/rail", nil, &rv)
	for _, v := range rv.Open {
		if v.ID == idle {
			t.Fatalf("idle row survived the sweep: %+v", rv.Open)
		}
	}
}

func TestSiblingFilesRegisterRoutesWithoutEditingTheSwitch(t *testing.T) {
	registerPlacesRoute("GET /places/{id}/ping", func(p *Places, w http.ResponseWriter, r *http.Request, id string) {
		write(w, map[string]string{"pong": id})
	})
	defer delete(placesRoutesTable, "GET /places/{id}/ping")
	rig := newPlacesRig(t)
	var out map[string]string
	if code := rig.do("GET", "/places/p1/ping", nil, &out); code != 200 || out["pong"] != "p1" {
		t.Fatalf("%d %v", code, out)
	}
}
