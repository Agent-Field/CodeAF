package desktopbridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
)

// decisionRig is a places rig whose decision door is a real ledger in a temp
// directory, with an undo the test can fail.
type decisionRig struct {
	*placesRig
	dir     string
	undone  []string
	undoErr error
	deps    []decide.Dependent
}

func newDecisionRig(t *testing.T) *decisionRig {
	t.Helper()
	rig := &decisionRig{placesRig: newPlacesRig(t), dir: t.TempDir()}
	rig.p.Decisions = &DecisionDoor{
		Open: func(id string) (*decide.Store, error) {
			return decide.Open(rig.dir, id, func() time.Time { return rig.clock })
		},
		Undo: func(_ context.Context, u decide.Undo) error {
			if rig.undoErr != nil {
				return rig.undoErr
			}
			rig.undone = append(rig.undone, u.Token)
			return nil
		},
		Dependents: func(context.Context) ([]decide.Dependent, error) { return rig.deps, nil },
	}
	return rig
}

func (r *decisionRig) store(place string) *decide.Store {
	s, err := decide.Open(r.dir, place, func() time.Time { return r.clock })
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func (r *decisionRig) add(place, id string, at time.Time, mutate ...func(*decide.Decision)) {
	d := decide.Decision{ID: id, PlaceID: place, AskKind: "permission", Subject: "shell-read", Action: "ran ls",
		By: place, Because: "you allowed it twice", Percent: 97, Reversible: true, At: at, Undo: decide.Undo{Token: id}}
	for _, m := range mutate {
		m(&d)
	}
	if err := r.store(place).Append(d); err != nil {
		r.t.Fatal(err)
	}
}

func TestDecisionsListIsNewestFirstAndPagesByCursor(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	for i := 0; i < 5; i++ {
		rig.add(place, fmt.Sprintf("%s:q%d", place, i), placesEpoch.Add(-time.Duration(5-i)*time.Hour))
	}
	var first decisionPageBody
	if code := rig.do("GET", "/places/"+place+"/decisions?limit=2", nil, &first); code != 200 || len(first.Decisions) != 2 || first.Next == "" {
		t.Fatalf("first page: %d %+v", code, first)
	}
	if first.Decisions[0].ID != place+":q4" || first.Decisions[1].ID != place+":q3" || first.Decisions[0].ByName != "Release" {
		t.Fatalf("order: %+v", first.Decisions)
	}
	var second, third decisionPageBody
	rig.do("GET", "/places/"+place+"/decisions?limit=2&cursor="+first.Next, nil, &second)
	rig.do("GET", "/places/"+place+"/decisions?limit=2&cursor="+second.Next, nil, &third)
	if second.Decisions[0].ID != place+":q2" || second.Next == "" || len(third.Decisions) != 1 || third.Decisions[0].ID != place+":q0" || third.Next != "" {
		t.Fatalf("paging: %+v %+v", second, third)
	}
}

func TestDecisionsListEdges(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	var empty decisionPageBody
	if code := rig.do("GET", "/places/"+place+"/decisions", nil, &empty); code != 200 || empty.Decisions == nil || len(empty.Decisions) != 0 {
		t.Fatalf("empty ledger must be []: %d %+v", code, empty)
	}
	for _, query := range []string{"?limit=0", "?limit=201", "?limit=x", "?cursor=!!"} {
		if code := rig.do("GET", "/places/"+place+"/decisions"+query, nil, nil); code != 400 {
			t.Errorf("%s: %d", query, code)
		}
	}
	if code := rig.do("GET", "/places/nope/decisions", nil, nil); code != 404 {
		t.Errorf("unknown place: %d", code)
	}
}

func TestDecisionRoutesWithoutADoorAre501(t *testing.T) {
	rig := newPlacesRig(t)
	place := rig.mk("Release")
	for _, probe := range []struct{ method, path string }{
		{"GET", "/places/" + place + "/decisions"}, {"GET", "/places/" + place + "/decide-status"}, {"GET", "/decisions/" + place + ":q"},
	} {
		if code := rig.do(probe.method, probe.path, nil, nil); code != 501 {
			t.Errorf("%s: %d", probe.path, code)
		}
	}
}

func TestDecideStatusModes(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	status := func() DecideStatusBody {
		var out DecideStatusBody
		if code := rig.do("GET", "/places/"+place+"/decide-status", nil, &out); code != 200 {
			t.Fatalf("status: %d", code)
		}
		return out
	}
	if got := status(); got.Mode != "none" || got.TotalWeek != nil || got.Learning != nil {
		t.Fatalf("never asked: %+v", got)
	}
	for i := 0; i < 3; i++ {
		if _, err := rig.store(place).RecordProposal(decide.ProposalAnswer{AskKind: "permission", SubjectClass: "shell-read", ProposedKey: "y", ChosenKey: "y"}); err != nil {
			t.Fatal(err)
		}
	}
	got := status()
	if got.Mode != "learning" || got.Learning == nil || got.Learning.Agreed != 3 || got.Learning.Of != decide.RingSize {
		t.Fatalf("learning: %+v", got)
	}
	if err := rig.store(place).SetMode("permission:shell-read", decide.ModeDeciding); err != nil {
		t.Fatal(err)
	}
	rig.add(place, place+":a", placesEpoch.Add(-time.Hour))
	rig.add(place, place+":b", placesEpoch.Add(-2*time.Hour), func(d *decide.Decision) { n := placesEpoch; d.OverturnedAt = &n })
	rig.add(place, place+":old", placesEpoch.Add(-9*24*time.Hour))
	got = status()
	if got.Mode != "deciding" || got.Learning != nil || got.TotalWeek == nil || *got.TotalWeek != 2 || *got.AgreedWeek != 1 {
		t.Fatalf("deciding: %+v", got)
	}
	if code := rig.do("PUT", "/places/"+place+"/decide", map[string]any{"alwaysAsk": true}, nil); code != 200 {
		t.Fatalf("always ask: %d", code)
	}
	if got := status(); got.Mode != "always-ask" {
		t.Fatalf("always-ask: %+v", got)
	}
}

func TestDecideSettingsValidatesAndInherits(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	var set decideMutation
	if code := rig.do("PUT", "/places/"+place+"/decide", map[string]any{"alwaysAsk": false, "threshold": 75}, &set); code != 200 || set.Decide.Threshold != 75 || set.From != place || len(set.Undo) != 1 {
		t.Fatalf("set: %d %+v", code, set)
	}
	// Omitting the threshold keeps the effective figure rather than zeroing it.
	var keep decideMutation
	if code := rig.do("PUT", "/places/"+place+"/decide", map[string]any{"alwaysAsk": true}, &keep); code != 200 || keep.Decide.Threshold != 75 || !keep.Decide.AlwaysAsk {
		t.Fatalf("keep: %d %+v", code, keep)
	}
	for _, bad := range []any{map[string]any{"threshold": 0}, map[string]any{"threshold": 101}, map[string]any{"bogus": 1}} {
		if code := rig.do("PUT", "/places/"+place+"/decide", bad, nil); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	var cleared decideMutation
	if code := rig.do("PUT", "/places/"+place+"/decide", map[string]any{"inherit": true}, &cleared); code != 200 || cleared.Decide.Threshold != 90 || cleared.From != "" {
		t.Fatalf("inherit: %d %+v", code, cleared)
	}
	if code := rig.do("PUT", "/places/nope/decide", map[string]any{"threshold": 80}, nil); code != 404 {
		t.Errorf("unknown place: %d", code)
	}
}

func TestOverturnReversesOnceAndMapsRefusalsTo409(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	id := place + ":q1"
	rig.add(place, id, placesEpoch.Add(-time.Hour))
	rig.add(place, place+":fixed", placesEpoch.Add(-time.Hour), func(d *decide.Decision) { d.Reversible = false })
	rig.deps = []decide.Dependent{{ID: "t1", Kind: decide.DependentTask, At: placesEpoch, Uses: []string{id}}}

	var got decide.OverturnResult
	if code := rig.do("POST", "/decisions/"+id+"/overturn", map[string]any{"next": "ask"}, &got); code != 200 ||
		got.Mode != decide.ModeAsk || len(got.Dependents) != 1 || got.Summary != "1 task used this" || len(rig.undone) != 1 {
		t.Fatalf("overturn: %d %+v undone=%v", code, got, rig.undone)
	}
	var row DecisionRow
	if code := rig.do("GET", "/decisions/"+id, nil, &row); code != 200 || row.OverturnedAt == nil {
		t.Fatalf("row: %d %+v", code, row)
	}
	if code := rig.do("POST", "/decisions/"+id+"/overturn", map[string]any{"next": "ask"}, nil); code != 200 || len(rig.undone) != 1 {
		t.Fatalf("second overturn undid again: %v", rig.undone)
	}

	var refused apiError
	if code := rig.do("POST", "/decisions/"+place+":fixed/overturn", map[string]any{"next": "keep"}, &refused); code != 409 || refused.Error != decide.ErrNotUndoable.Error() {
		t.Fatalf("irreversible: %d %+v", code, refused)
	}
	rig.add(place, place+":late", placesEpoch.Add(-time.Hour))
	rig.undoErr = fmt.Errorf("gone: %w", decide.ErrNotUndoable)
	if code := rig.do("POST", "/decisions/"+place+":late/overturn", map[string]any{"next": "keep"}, &refused); code != 409 || refused.Error == "" {
		t.Fatalf("engine refusal: %d %+v", code, refused)
	}
	rig.undoErr = errors.New("disk")
	if code := rig.do("POST", "/decisions/"+place+":late/overturn", map[string]any{"next": "keep"}, nil); code != 500 {
		t.Fatalf("undo failure: %d", code)
	}
}

func TestOverturnValidatesBody(t *testing.T) {
	rig := newDecisionRig(t)
	place := rig.mk("Release")
	rig.add(place, place+":q", placesEpoch.Add(-time.Hour))
	for name, body := range map[string]any{"missing next": map[string]any{}, "bad next": map[string]any{"next": "later"}, "unknown field": map[string]any{"next": "ask", "cascade": true}} {
		if code := rig.do("POST", "/decisions/"+place+":q/overturn", body, nil); code != 400 {
			t.Errorf("%s: %d", name, code)
		}
	}
	if code := rig.do("POST", "/decisions/"+place+":ghost/overturn", map[string]any{"next": "ask"}, nil); code != 404 {
		t.Errorf("unknown decision: %d", code)
	}
	if len(rig.undone) != 0 {
		t.Fatalf("a refused request undid work: %v", rig.undone)
	}
	rig.p.Decisions.Undo = nil
	if code := rig.do("POST", "/decisions/"+place+":q/overturn", map[string]any{"next": "ask"}, nil); code != 501 {
		t.Errorf("no undo door: %d", code)
	}
}

func TestDecisionRoutesKeepTheAuthGuard(t *testing.T) {
	rig := newDecisionRig(t)
	for _, probe := range []struct{ method, path string }{
		{"GET", "/places/p/decisions"}, {"GET", "/places/p/decide-status"}, {"GET", "/decisions/p:q"},
		{"POST", "/decisions/p:q/overturn"}, {"PUT", "/places/p/decide"},
	} {
		req, _ := http.NewRequest(probe.method, rig.srv.URL+"/api/engine"+probe.path, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 401 {
			t.Errorf("%s %s without a token: %d", probe.method, probe.path, resp.StatusCode)
		}
		req.Header.Set("Authorization", "Bearer "+placesToken)
		req.Header.Set("Origin", "https://evil.example")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 403 {
			t.Errorf("%s %s foreign origin: %d", probe.method, probe.path, resp.StatusCode)
		}
	}
}
