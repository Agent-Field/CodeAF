package desktopbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/session"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/placegraph"
	"github.com/Agent-Field/codeaf/internal/placeorg"
	"github.com/Agent-Field/codeaf/internal/placeorg/correct"
	"github.com/Agent-Field/codeaf/internal/placeorg/steer"
	"github.com/Agent-Field/codeaf/internal/placeorg/stream"
)

func TestAnUnwiredOrganizerAnswersUnknown(t *testing.T) {
	rig := newAdviceRig(t, true)
	for _, call := range []struct{ method, path, body string }{
		{"GET", "/api/engine/places/organize", ""},
		{"POST", "/api/engine/places/organize", `{"mode":"add-on"}`},
		{"POST", "/api/engine/places/organize/cancel", ""},
		{"GET", "/api/engine/places/tree", ""},
		{"POST", "/api/engine/places/guess", `{}`},
		{"POST", "/api/engine/places/steer", `{"scope":"now","sentence":"by month"}`},
	} {
		w := request(rig.b, call.method, call.path, call.body)
		if w.Code != 404 || !strings.Contains(w.Body.String(), "unknown engine action") {
			t.Fatalf("%s %s: %d %s", call.method, call.path, w.Code, w.Body.String())
		}
	}
}

func TestOrganizeReportsRunningThenCancels(t *testing.T) {
	rig := newAdviceRig(t, true)
	started := make(chan struct{})
	var once sync.Once
	rig.advice.Organizer = &Organizer{State: placeorg.NewState(), Run: func(ctx context.Context, _ *placeorg.Corpus, opts placeorg.Options) (*placeorg.Plan, error) {
		once.Do(func() { close(started) })
		if opts.Progress != nil {
			opts.Progress(placeorg.Progress{Stage: "vectors", Done: 1, Total: 4, Cancellable: true})
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	w := request(rig.b, "POST", "/api/engine/places/organize", `{"mode":"add-on"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"started":true`) {
		t.Fatalf("start: %d %s", w.Code, w.Body.String())
	}
	var start struct {
		Run string `json:"run"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &start)
	<-started
	w = request(rig.b, "GET", "/api/engine/places/organize", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"running":true`) || strings.Contains(w.Body.String(), `"plan"`) {
		t.Fatalf("while running: %s", w.Body.String())
	}
	w = request(rig.b, "POST", "/api/engine/places/organize", `{}`)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"code":"running"`) || !strings.Contains(w.Body.String(), start.Run) {
		t.Fatalf("second start: %d %s", w.Code, w.Body.String())
	}
	if !organizingPublished(rig.b) {
		t.Fatal("the world feed did not say places are being organized")
	}
	w = request(rig.b, "POST", "/api/engine/places/organize/cancel", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"cancelled":true`) {
		t.Fatalf("cancel: %d %s", w.Code, w.Body.String())
	}
	eventually(t, "the run to stop", func() bool {
		w := request(rig.b, "GET", "/api/engine/places/organize", "")
		return strings.Contains(w.Body.String(), `"running":false`) && strings.Contains(w.Body.String(), `"outcome":"cancelled"`)
	})
}

func TestAcceptSkipsARowAndReturnsOneReceipt(t *testing.T) {
	rig := newAdviceRig(t, true)
	desk := rig.place("Desk")
	rig.advice.Organizer = &Organizer{State: placeorg.NewState(), Run: func(_ context.Context, _ *placeorg.Corpus, opts placeorg.Options) (*placeorg.Plan, error) {
		if opts.Progress != nil {
			opts.Progress(placeorg.Progress{Stage: "name", Done: 1, Total: 1, Cancellable: true})
		}
		return &placeorg.Plan{Summary: "1 rename offered", Ops: []placeorg.Op{
			{Kind: placeorg.OpRename, Place: desk, Name: "Workbench", Why: "Rename to Workbench", Proposed: true},
			{Kind: placeorg.OpFile, Place: desk, Chat: "c1", Why: "File a chat", Proposed: true},
		}}, nil
	}}
	w := request(rig.b, "POST", "/api/engine/places/organize", `{"mode":"refresh"}`)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var planID string
	eventually(t, "a plan", func() bool {
		w := request(rig.b, "GET", "/api/engine/places/organize", "")
		var snap struct {
			Running bool `json:"running"`
			Plan    struct {
				ID  string `json:"id"`
				Ops []struct {
					Index int    `json:"index"`
					Kind  string `json:"kind"`
				} `json:"ops"`
			} `json:"plan"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &snap)
		planID = snap.Plan.ID
		return !snap.Running && len(snap.Plan.Ops) == 2 && snap.Plan.Ops[0].Kind == "rename" && snap.Plan.Ops[1].Kind == "file"
	})
	if len(planID) < 5 || planID[:5] != "prop_" {
		t.Fatalf("plan id %q", planID)
	}
	w = request(rig.b, "POST", "/api/engine/places/proposals/"+planID+"/accept", `{"skip":[0]}`)
	if w.Code != 200 {
		t.Fatalf("accept: %d %s", w.Code, w.Body.String())
	}
	var reply struct {
		Receipt  string `json:"receipt"`
		Receipts []struct {
			ID string `json:"id"`
		} `json:"receipts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reply); err != nil || reply.Receipt == "" || len(reply.Receipts) != 1 || reply.Receipts[0].ID != reply.Receipt {
		t.Fatalf("one receipt: %+v %s", reply, w.Body.String())
	}
	snap, _ := rig.store.Snapshot()
	pl, _ := snap.Place(desk)
	if pl.Name != "Desk" {
		t.Fatalf("the skipped rename applied: %q", pl.Name)
	}
	if got := snap.PlacesOf("c1"); len(got) != 1 || got[0].PlaceID != desk {
		t.Fatalf("the kept filing: %+v", got)
	}
	w = request(rig.b, "POST", "/api/engine/places/proposals/"+planID+"/accept", `{}`)
	if w.Code != 409 {
		t.Fatalf("second accept: %d %s", w.Code, w.Body.String())
	}
}

func TestDecliningAnOrganizerPlanClearsIt(t *testing.T) {
	rig := newAdviceRig(t, true)
	desk := rig.place("Desk")
	rig.advice.Organizer = &Organizer{State: placeorg.NewState(), Run: func(context.Context, *placeorg.Corpus, placeorg.Options) (*placeorg.Plan, error) {
		return &placeorg.Plan{Ops: []placeorg.Op{{Kind: placeorg.OpRename, Place: desk, Name: "Workbench", Why: "Rename to Workbench", Proposed: true}}}, nil
	}}
	if w := request(rig.b, "POST", "/api/engine/places/organize", `{}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var planID string
	eventually(t, "a plan", func() bool {
		w := request(rig.b, "GET", "/api/engine/places/organize", "")
		var snap struct {
			Plan struct {
				ID string `json:"id"`
			} `json:"plan"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &snap)
		planID = snap.Plan.ID
		return planID != ""
	})
	if w := request(rig.b, "POST", "/api/engine/places/proposals/"+planID+"/decline", ""); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	w := request(rig.b, "GET", "/api/engine/places/organize", "")
	var after struct {
		Plan *struct {
			ID string `json:"id"`
		} `json:"plan"`
		Last *struct {
			Outcome string `json:"outcome"`
		} `json:"last"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil || after.Plan != nil || after.Last == nil || after.Last.Outcome != "declined" {
		t.Fatalf("after decline: %s", w.Body.String())
	}
}

func TestAGuessNamesTheFolderAndAnEmptyDraftNamesNothing(t *testing.T) {
	rig := newAdviceRig(t, true)
	id := rig.folderPlace("Release", "/work/release")
	rig.advice.Organizer = &Organizer{State: placeorg.NewState(), stream: stream.New()}
	w := request(rig.b, "POST", "/api/engine/places/guess", `{"draft":"the release failed","workspace":"/work/release","chatId":"c1"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), id) || !strings.Contains(w.Body.String(), `"confidence":100`) {
		t.Fatalf("guess: %d %s", w.Code, w.Body.String())
	}
	w = request(rig.b, "POST", "/api/engine/places/guess", `{}`)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatalf("empty: %s", w.Body.String())
	}
	rig.mu.Lock()
	rig.policy.LiveGuess = false
	rig.mu.Unlock()
	w = request(rig.b, "POST", "/api/engine/places/guess", `{"draft":"the release failed","workspace":"/work/release"}`)
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "{}" {
		t.Fatalf("off: %s", w.Body.String())
	}
}

func TestASteerAppliesAMonthViewAndRefusesWhatItCannotRead(t *testing.T) {
	rig := newAdviceRig(t, true)
	desk := rig.place("Desk")
	org := &Organizer{State: placeorg.NewState(), steer: steer.New(), Ask: func() placegraph.Asker {
		return func(context.Context, placegraph.ModelRequest) (string, error) { return "month", nil }
	}}
	rig.advice.Organizer = org
	w := request(rig.b, "POST", "/api/engine/places/steer", `{"scope":"`+desk+`","sentence":"by month"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"understood":true`) || !strings.Contains(w.Body.String(), `"axis":"month"`) {
		t.Fatalf("steer: %d %s", w.Code, w.Body.String())
	}
	w = request(rig.b, "GET", "/api/engine/places/"+desk+"/lenses", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "by month") {
		t.Fatalf("lenses: %s", w.Body.String())
	}
	org.Ask = func() placegraph.Asker {
		return func(context.Context, placegraph.ModelRequest) (string, error) { return "none", nil }
	}
	w = request(rig.b, "POST", "/api/engine/places/steer", `{"scope":"`+desk+`","sentence":"make it nicer"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"understood":false`) {
		t.Fatalf("unclear: %d %s", w.Code, w.Body.String())
	}
}

func TestThePlaceTreeCountsHomes(t *testing.T) {
	rig := newAdviceRig(t, true)
	desk := rig.place("Desk")
	if _, _, err := rig.store.AddChat("c1", desk, placegraph.AddedByYou); err != nil {
		t.Fatal(err)
	}
	rig.advice.Organizer = &Organizer{State: placeorg.NewState()}
	w := request(rig.b, "GET", "/api/engine/places/tree", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"name":"Desk"`) || !strings.Contains(w.Body.String(), `"chats":1`) {
		t.Fatalf("tree: %s", w.Body.String())
	}
}

func TestABadOrganizeModeIsRefused(t *testing.T) {
	rig := newAdviceRig(t, true)
	rig.advice.Organizer = &Organizer{State: placeorg.NewState()}
	w := request(rig.b, "POST", "/api/engine/places/organize", `{"mode":"sideways"}`)
	if w.Code != 400 || !strings.Contains(w.Body.String(), "add-on or refresh") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

func TestWireOrganizerBuildsTheStages(t *testing.T) {
	rig := newAdviceRig(t, true)
	if err := WireOrganizer(rig.advice, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	o := rig.advice.Organizer
	if o == nil || o.Stages.Dedup == nil || o.Stages.Stream == nil || o.Stages.Steer == nil || o.Stages.Correct == nil || o.Stages.Park == nil || o.stream == nil || o.steer == nil {
		t.Fatal("the organiser was wired without the stages a run needs")
	}
}

func TestAHandMoveIsRememberedOnTheDestination(t *testing.T) {
	rig := newAdviceRig(t, true)
	desk := rig.place("Desk")
	org := &Organizer{
		State:   placeorg.NewState(),
		correct: correct.New(placeorg.SpacePrimary, &memVectors{}, nil),
	}
	rig.advice.Organizer = org
	rig.advice.noteHandMove("c1", "", desk)
	if len(org.State.Exemplars) != 1 || org.State.Exemplars[0].Chat != "c1" || org.State.Exemplars[0].To != desk {
		t.Fatalf("%+v", org.State.Exemplars)
	}
	rig.advice.noteHandMove("c1", desk, "")
	if len(org.State.Exemplars) != 1 {
		t.Fatal("a move with no destination was remembered")
	}
}

func organizingPublished(b *Bridge) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		b.world.mu.Lock()
		found := false
		for _, rec := range b.world.ring {
			if rec.Type == "placesOrganizing" {
				found = true
				break
			}
		}
		b.world.mu.Unlock()
		if found {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// offerRig gives the rig n conversations that are in no place.
func offerRig(t *testing.T, n int, state *placeorg.State) *adviceRig {
	t.Helper()
	rig := newAdviceRig(t, true)
	rig.mu.Lock()
	for i := 0; i < n; i++ {
		rig.rows = append(rig.rows, session.SessionRow{ID: fmt.Sprintf("chat-%d", i), Title: "A chat"})
	}
	rig.mu.Unlock()
	rig.advice.Organizer = &Organizer{State: state, statePath: filepath.Join(t.TempDir(), "places-org.json")}
	return rig
}

func offerOf(t *testing.T, rig *adviceRig) *offerWire {
	t.Helper()
	w := request(rig.b, "GET", "/api/engine/places/organize", "")
	var got organizeWire
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	return got.Offer
}

func TestTheFirstRunOfferNeedsEnoughUnplacedChats(t *testing.T) {
	if got := offerOf(t, offerRig(t, OrganizeOfferMin-1, placeorg.NewState())); got != nil {
		t.Fatalf("offered below the minimum: %+v", got)
	}
	if got := offerOf(t, offerRig(t, OrganizeOfferMin, placeorg.NewState())); got == nil || got.Unplaced != OrganizeOfferMin {
		t.Fatalf("not offered at the minimum: %+v", got)
	}
	organized := placeorg.NewState()
	organized.Organized = true
	if got := offerOf(t, offerRig(t, 30, organized)); got != nil {
		t.Fatalf("offered after a plan was accepted: %+v", got)
	}
}

func TestNotNowSilencesTheOfferUntilTheCountDoubles(t *testing.T) {
	state := placeorg.NewState()
	rig := offerRig(t, 14, state)
	if w := request(rig.b, "POST", "/api/engine/places/organize/offer/dismiss", `{}`); w.Code != 200 {
		t.Fatalf("dismiss: %d %s", w.Code, w.Body.String())
	}
	if state.OfferDismissedAt != 14 || offerOf(t, rig) != nil {
		t.Fatalf("dismissed at %d, offer %+v", state.OfferDismissedAt, offerOf(t, rig))
	}
	// The dismissal is on disk, so a restart keeps it.
	reread, _, err := placeorg.LoadState(rig.advice.Organizer.statePath, time.Now)
	if err != nil || reread.OfferDismissedAt != 14 {
		t.Fatalf("reread: %v %+v", err, reread)
	}
	rig.mu.Lock()
	for i := 14; i < 28; i++ {
		rig.rows = append(rig.rows, session.SessionRow{ID: fmt.Sprintf("chat-%d", i), Title: "A chat"})
	}
	rig.mu.Unlock()
	// The world read is cached for a moment; the test lets it go.
	rig.b.places.cacheMu.Lock()
	rig.b.places.cacheDone = false
	rig.b.places.cacheMu.Unlock()
	if got := offerOf(t, rig); got == nil || got.Unplaced != 28 {
		t.Fatalf("not offered again at double: %+v", got)
	}
}
