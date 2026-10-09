package desktopbridge

import (
	"net/http"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/session"
)

const (
	modelA = "fixture/model-a"
	modelB = "fixture/model-b"
	modelC = "fixture/model-c"
)

// setModel gives a place its own default model through the real write route.
func (r *placesRig) setModel(id, model string) {
	r.t.Helper()
	if code := r.do("POST", "/places/"+id, map[string]any{"policy": map[string]any{"model": model}}, nil); code != 200 {
		r.t.Fatalf("set model on %s: %d", id, code)
	}
}

func (r *placesRig) effective(id string) effectiveModelAnswer {
	r.t.Helper()
	var out effectiveModelAnswer
	if code := r.do("GET", "/places/"+id+"/effective-model", nil, &out); code != 200 {
		r.t.Fatalf("GET effective-model %s: %d", id, code)
	}
	return out
}

// withDoor attaches the engine's own door so the engine "reads" places.
func (r *placesRig) withDoor() {
	r.t.Helper()
	dir := r.t.TempDir()
	door := &session.PlaceGraphDoor{Path: r.path, ChoicesPath: dir + "/choices.json"}
	if err := r.p.UseDoor(door); err != nil {
		r.t.Fatal(err)
	}
}

func TestEffectiveModelNoPlaceOpinionIsNone(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	id := rig.mk("Plain")
	if got := rig.effective(id); got.State != EffectiveModelNone || got.Model != "" || got.DecidedBy != nil {
		t.Fatalf("no policy anywhere must be none: %+v", got)
	}
}

func TestEffectiveModelOwnPolicyApplies(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	id := rig.mk("Own")
	rig.setModel(id, modelA)
	got := rig.effective(id)
	if got.State != EffectiveModelApplies || got.Model != modelA || got.DecidedBy == nil || got.DecidedBy.ID != id || got.DecidedBy.Name != "Own" {
		t.Fatalf("own model must apply and credit the place: %+v", got)
	}
}

func TestEffectiveModelInheritedFromParent(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	parent := rig.mk("Parent")
	child := rig.mk("Child", parent)
	rig.setModel(parent, modelA)
	got := rig.effective(child)
	if got.State != EffectiveModelApplies || got.Model != modelA || got.DecidedBy.ID != parent {
		t.Fatalf("child must inherit the parent's model: %+v", got)
	}
}

func TestEffectiveModelDisagreeingAncestorsFollowTheResolversRule(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	grand := rig.mk("Grand")
	parent := rig.mk("Parent", grand)
	child := rig.mk("Child", parent)
	rig.setModel(grand, modelA)
	rig.setModel(parent, modelB)
	// Parent and grandparent disagree. The resolver's rule (commonDecider) is the
	// nearest common ancestor-or-self of the opinionated places, which is the
	// grandparent: this route must say exactly that, not its own idea of "nearest".
	got := rig.effective(child)
	if got.State != EffectiveModelApplies || got.Model != modelA || got.DecidedBy.ID != grand || got.Outcome != "decided" {
		t.Fatalf("the resolver's decider must be quoted: %+v", got)
	}
}

func TestEffectiveModelChildOverridesParent(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	parent := rig.mk("Parent")
	child := rig.mk("Child", parent)
	rig.setModel(parent, modelA)
	rig.setModel(child, modelB)
	got := rig.effective(child)
	if got.State != EffectiveModelApplies || got.Model != modelA || got.DecidedBy.ID != parent {
		// A parent that disagrees with its child decides over it (6f); the resolver, not this route, owns that.
		t.Fatalf("the resolver's rule must pass through unchanged: %+v", got)
	}
}

func TestEffectiveModelTwoParentsWithACommonAncestorUseIt(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	root := rig.mk("Root")
	left := rig.mk("Left", root)
	right := rig.mk("Right", root)
	both := rig.mk("Both", left, right)
	rig.setModel(left, modelA)
	rig.setModel(right, modelB)
	rig.setModel(root, modelC)
	got := rig.effective(both)
	if got.State != EffectiveModelApplies || got.Model != modelC || got.DecidedBy.ID != root || got.Outcome != "decided" {
		t.Fatalf("the common ancestor must decide: %+v", got)
	}
}

func TestEffectiveModelUnresolvedConflictNeedsAPickAndAppliesNothing(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	left := rig.mk("Left")
	right := rig.mk("Right")
	both := rig.mk("Both", left, right)
	rig.setModel(left, modelA)
	rig.setModel(right, modelB)
	got := rig.effective(both)
	if got.State != EffectiveModelNeedsPick || got.Model != "" || got.DecidedBy != nil || len(got.Wanted) != 2 {
		t.Fatalf("disagreeing parents with no decider must need a pick: %+v", got)
	}
	if got.Wanted[0].Model == got.Wanted[1].Model || got.Wanted[0].Name == "" {
		t.Fatalf("the candidates must be named and different: %+v", got.Wanted)
	}
}

func TestEffectiveModelWithoutTheEnginesDoorIsUnavailable(t *testing.T) {
	rig := newPlacesRig(t)
	id := rig.mk("Own")
	rig.setModel(id, modelA)
	got := rig.effective(id)
	if got.State != EffectiveModelUnavailable || got.Model != "" || got.Reason == "" {
		t.Fatalf("an engine that does not read places must not be quoted as applying: %+v", got)
	}
}

func TestEffectiveModelRefusals(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	id := rig.mk("Own")
	var e apiError
	for _, reserved := range []string{"root", "now", "status", "stale", "undo"} {
		if code := rig.do("GET", "/places/"+reserved+"/effective-model", nil, &e); code != 400 || e.Code != "reserved" {
			t.Errorf("%s: %d %+v, want 400 reserved", reserved, code, e)
		}
	}
	if code := rig.do("GET", "/places/pl_nope/effective-model", nil, &e); code != 404 {
		t.Errorf("unknown place: %d, want 404", code)
	}
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		if code := rig.do(method, "/places/"+id+"/effective-model", nil, nil); code != 405 {
			t.Errorf("%s: %d, want 405", method, code)
		}
	}
	req, _ := http.NewRequest("GET", rig.srv.URL+"/api/engine/places/"+id+"/effective-model", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("without a token: %d, want 401", resp.StatusCode)
	}
}

func TestEffectiveModelReadsChangeNothingAndOpenNoSession(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	left := rig.mk("Left")
	right := rig.mk("Right")
	both := rig.mk("Both", left, right)
	rig.setModel(left, modelA)
	rig.setModel(right, modelB)
	before, err := os.ReadFile(rig.path)
	if err != nil {
		t.Fatal(err)
	}
	var rev uint64
	for range 3 {
		rev = rig.effective(both).Revision
		rig.effective(left)
	}
	after, _ := os.ReadFile(rig.path)
	if string(before) != string(after) {
		t.Fatal("reading the effective model rewrote the places file")
	}
	var g struct{ Revision uint64 }
	rig.do("GET", "/places", nil, &g)
	if g.Revision != rev {
		t.Fatalf("revision moved: %d vs %d", g.Revision, rev)
	}
	// The imagined filing must not leak into the real graph: no chat is in any place.
	if snap, err := rig.p.Store.Snapshot(); err != nil || len(snap.Memberships) != 0 {
		t.Fatalf("the imagined chat leaked into the graph: %v %+v", err, snap.Memberships)
	}
	// This rig has no sessions and no provider at all: reaching here with the
	// bridge in a state that cannot open a session or call a model is the proof.
	if n := len(rig.b.sessions); n != 0 {
		t.Fatalf("a session was opened: %d", n)
	}
}

// TestEffectiveModelIsWhatTheEngineResolvesOnceTheChatIsFiled pins the claim the
// whole route rests on: the preview and the real resolver cannot disagree.
func TestEffectiveModelIsWhatTheEngineResolvesOnceTheChatIsFiled(t *testing.T) {
	rig := newPlacesRig(t)
	rig.withDoor()
	root := rig.mk("Root")
	left := rig.mk("Left", root)
	right := rig.mk("Right", root)
	solo := rig.mk("Solo")
	rig.setModel(root, modelC)
	rig.setModel(left, modelA)
	rig.setModel(right, modelB)
	rig.setRows(row("chata", "a", placesEpoch), row("chatb", "b", placesEpoch), row("chatc", "c", placesEpoch), row("chatd", "d", placesEpoch))
	for i, id := range []string{root, left, right, solo} {
		preview := rig.effective(id)
		chat := "chat" + string(rune('a'+i))
		if code := rig.do("POST", "/places/"+id+"/members", map[string]any{"chats": []string{chat}}, nil); code != 200 {
			t.Fatalf("file %s: %d", chat, code)
		}
		snap, err := rig.p.Store.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		var real string
		for _, d := range snap.Resolve(chat, placegraph.ResolveOptions{}).Policy {
			if d.Field == "model" {
				real = d.Value
			}
		}
		if preview.Model != real {
			t.Errorf("%s: preview %q, engine resolves %q", id, preview.Model, real)
		}
	}
}
