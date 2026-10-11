package placegraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestPlan500OperationsAreOneRevisionAndReceipt(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Original")
	before := snap(t, s).State
	beforeBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ops := make([]PlanOp, 500)
	for i := range ops {
		ops[i] = PlanOp{Kind: PlanFile, PlaceID: p.ID, ChatID: fmt.Sprintf("chat-%d", i)}
	}
	count := len(s.Receipts())
	rc, err := s.ApplyPlan(ops)
	if err != nil {
		t.Fatal(err)
	}
	after := snap(t, s)
	if after.Revision != before.Revision+1 || len(s.Receipts()) != count+1 || rc.Action != ActionPlan || len(after.Memberships) != 500 {
		t.Fatalf("receipt %+v, graph %+v", rc, after)
	}
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	// Plan Undo restores the original revision as well as the structure.
	restored := snap(t, s).State
	if !reflect.DeepEqual(before, restored) {
		t.Fatal("undo did not restore the prior document")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, beforeBytes) {
		t.Fatal("plan undo did not restore exact prior file bytes")
	}
	var disk State
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, disk) {
		t.Fatal("persisted undo differs")
	}
}

func TestInvalidPlanChangesNoBytesOrReceipts(t *testing.T) {
	for _, bad := range []PlanOp{
		{Kind: PlanFile, PlaceID: "missing", ChatID: "valid"},
		{Kind: PlanCreate, PlaceID: NowID, Name: "Forbidden"},
		{Kind: PlanKind("unknown")},
		{Kind: PlanFile, PlaceID: "target", ChatID: ""},
		{Kind: PlanReparent, PlaceID: "target", Parents: []string{"target"}},
		{Kind: PlanCreate, PlaceID: "duplicate", Name: "Original"},
	} {
		t.Run(string(bad.Kind)+bad.PlaceID, func(t *testing.T) {
			s, path := newStore(t)
			_, err := s.ApplyPlan([]PlanOp{{Kind: PlanCreate, PlaceID: "target", Name: "Original"}})
			if err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(path)
			receipts := s.Receipts()
			_, err = s.ApplyPlan([]PlanOp{{Kind: PlanRename, PlaceID: "target", Name: "Temporary"}, bad, {Kind: PlanRename, PlaceID: "target", Name: "Original"}})
			if err == nil {
				t.Fatal("invalid plan succeeded")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) || !reflect.DeepEqual(receipts, s.Receipts()) {
				t.Fatal("refused plan changed store")
			}
		})
	}
}

func TestPlanOperationsPreserveLinksAndUndo(t *testing.T) {
	s, _ := newStore(t)
	original := mk(t, s, "Existing")
	before := snap(t, s).State
	ops := []PlanOp{
		{Kind: PlanCreate, PlaceID: "a", Name: "Alpha"},
		{Kind: PlanCreate, PlaceID: "b", Name: "Beta", Parents: []string{"a"}},
		{Kind: PlanFile, PlaceID: "b", ChatID: "chat"},
		{Kind: PlanLink, PlaceID: original.ID, ChatID: "chat"},
		{Kind: PlanRename, PlaceID: "a", Name: "Renamed"},
		{Kind: PlanReparent, PlaceID: "b", Parents: []string{original.ID}},
		{Kind: PlanMove, PlaceID: "a", FromPlaceID: "b", ChatID: "chat"},
		{Kind: PlanMerge, PlaceID: original.ID, FromPlaceID: "b"},
		{Kind: PlanDelete, PlaceID: "a"},
	}
	rc, err := s.ApplyPlan(ops)
	if err != nil {
		t.Fatal(err)
	}
	rows := snap(t, s).PlacesOf("chat")
	if len(rows) != 1 || !rows[0].Link || rows[0].PlaceID != original.ID {
		t.Fatalf("rows %+v", rows)
	}
	if len(snap(t, s).ChatsIn(original.ID, false)) != 1 {
		t.Fatal("shortcut missing from ChatsIn")
	}
	if _, err := s.Undo(rc.ID); err != nil {
		t.Fatal(err)
	}
	restored := snap(t, s).State
	restored.Revision = before.Revision
	if !reflect.DeepEqual(before, restored) {
		t.Fatal("mixed plan undo differs")
	}
}

func TestOldMembershipWithoutLinkLoadsWithoutRewriting(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Existing")
	if _, _, err := s.AddChat("chat", p.ID, AddedByYou); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	if bytes.Contains(before, []byte(`"link"`)) {
		t.Fatal("false link was persisted")
	}
	loaded, err := Open(Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	rows := snap(t, loaded).PlacesOf("chat")
	if len(rows) != 1 || rows[0].Link {
		t.Fatalf("old row %+v", rows)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("load rewrote old file")
	}
}

func planRecommender(t *testing.T) *Recommender {
	t.Helper()
	s, _ := newStore(t)
	ledger, err := OpenLedger(filepath.Join(t.TempDir(), "places-ai.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Recommender{Store: s, Ledger: ledger, Policy: DefaultRecommendPolicy, Now: func() time.Time { return time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) }}
}

func TestPlanAcceptTracksEveryCreatedPlaceAndSupportsSkip(t *testing.T) {
	r := planRecommender(t)
	p := Proposal{ID: "plan", Kind: ProposalPlan, Status: StatusPending, Ops: []PlanOp{
		{Kind: PlanCreate, PlaceID: "a", Name: "Alpha"},
		{Kind: PlanCreate, PlaceID: "b", Name: "Beta", Parents: []string{"a"}},
		{Kind: PlanFile, PlaceID: "b", ChatID: "chat"},
		{Kind: PlanRename, PlaceID: "a", Name: "Skipped"},
	}}
	if err := r.Ledger.update(r.now(), func(st *ledgerState) error { st.Proposals = append(st.Proposals, p); return nil }); err != nil {
		t.Fatal(err)
	}
	result, err := r.Accept("plan", AcceptEdit{Skip: []int{3}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Receipts) != 1 || result.Proposal.Status != StatusAccepted {
		t.Fatalf("%+v", result)
	}
	if p, _ := snap(t, r.Store).Place("a"); p.Name != "Alpha" {
		t.Fatal(p.Name)
	}
	if err := r.Ledger.view(r.now(), func(st *ledgerState) error {
		if !reflect.DeepEqual(st.AIPlaces, []string{"a", "b"}) {
			t.Fatalf("AI places %v", st.AIPlaces)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPlanAcceptRefusesChangedRevisionAndFilledCaps(t *testing.T) {
	for _, stale := range []bool{true, false} {
		t.Run(fmt.Sprint(stale), func(t *testing.T) {
			r := planRecommender(t)
			if stale {
				mk(t, r.Store, "Manual")
			} else {
				r.Policy = func() RecommendPolicy { p := DefaultRecommendPolicy(); p.MaxAIPlaces = 0; return p }
			}
			p := Proposal{ID: "plan", Kind: ProposalPlan, Status: StatusPending, Ops: []PlanOp{{Kind: PlanCreate, PlaceID: "a", Name: "Alpha"}}}
			if err := r.Ledger.update(r.now(), func(st *ledgerState) error { st.Proposals = append(st.Proposals, p); return nil }); err != nil {
				t.Fatal(err)
			}
			before := snap(t, r.Store).State
			_, err := r.Accept("plan", AcceptEdit{})
			want := ErrPolicyLimit
			if stale {
				want = ErrProposalGone
			}
			if !errors.Is(err, want) {
				t.Fatalf("want %v got %v", want, err)
			}
			if !reflect.DeepEqual(before, snap(t, r.Store).State) {
				t.Fatal("refused accept changed graph")
			}
		})
	}
}

func TestOrganizingPolicyDefaultsAndValidation(t *testing.T) {
	p := DefaultRecommendPolicy()
	if p.OrganizeMode != "add-on" || p.OrganizeCallsPerRun != 150 || p.OrganizeUSDPerRun != 5 || !p.LiveGuess {
		t.Fatalf("%+v", p)
	}
	for _, bad := range []struct{ key, value string }{{"organizeMode", `"unknown"`}, {"organizeCallsPerRun", "-1"}, {"organizeUSDPerRun", "0.05"}, {"liveGuess", "1"}} {
		if err := p.Set(bad.key, json.RawMessage(bad.value)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", bad.key, err)
		}
	}
	if err := p.Set("organizeMode", json.RawMessage(`"refresh"`)); err != nil {
		t.Fatal(err)
	}
	p.OrganizeMode = "invalid"
	if p.Normalized().OrganizeMode != "add-on" {
		t.Fatal("mode not normalized")
	}
}

func TestPlanProposalIsListedAndItsRunIsIdempotent(t *testing.T) {
	r := planRecommender(t)
	ops := []PlanOp{{Kind: PlanCreate, PlaceID: "a", Name: "Alpha"}}
	p, err := r.ProposePlan("run-one", ops, "One new place")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := r.ProposePlan("run-one", nil, "Replacement")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, repeated) {
		t.Fatal("repeat changed offer")
	}
	pending, err := r.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].ID != p.ID {
		t.Fatalf("pending %+v", pending)
	}
	res, err := r.Accept(p.ID, AcceptEdit{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Receipts) != 1 {
		t.Fatal(res)
	}
}

func TestPlanUndoRestoresOriginalFormattingAndMissingFile(t *testing.T) {
	t.Run("formatting", func(t *testing.T) {
		s, path := newStore(t)
		p := mk(t, s, "Original")
		data, _ := os.ReadFile(path)
		var compact bytes.Buffer
		if err := json.Compact(&compact, data); err != nil {
			t.Fatal(err)
		}
		before := append(compact.Bytes(), '\n')
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Fatal(err)
		}
		rc, err := s.ApplyPlan([]PlanOp{{Kind: PlanRename, PlaceID: p.ID, Name: "After"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Undo(rc.ID); err != nil {
			t.Fatal(err)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(before, after) {
			t.Fatal("formatting changed")
		}
	})
	t.Run("missing", func(t *testing.T) {
		s, path := newStore(t)
		rc, err := s.ApplyPlan([]PlanOp{{Kind: PlanCreate, PlaceID: "a", Name: "Alpha"}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Undo(rc.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("file still exists: %v", err)
		}
	})
}

func TestPlanWriteFailureLeavesBytesAndReceiptsIntact(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Original")
	before, _ := os.ReadFile(path)
	receipts := s.Receipts()
	s.beforeRename = func() error { return errors.New("injected replace failure") }
	if _, err := s.ApplyPlan([]PlanOp{{Kind: PlanRename, PlaceID: p.ID, Name: "After"}}); err == nil {
		t.Fatal("write succeeded")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || !reflect.DeepEqual(receipts, s.Receipts()) {
		t.Fatal("failed write changed store")
	}
}

func TestPlanUndoRefusesALaterVisitAndChainedPlansUndoExactly(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Original")
	before, _ := os.ReadFile(path)
	first, err := s.ApplyPlan([]PlanOp{{Kind: PlanRename, PlaceID: p.ID, Name: "First"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.ApplyPlan([]PlanOp{{Kind: PlanRename, PlaceID: p.ID, Name: "Second"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(first.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("chained undo differs")
	}
	rc, err := s.ApplyPlan([]PlanOp{{Kind: PlanRename, PlaceID: p.ID, Name: "Visited"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.TouchOpened(p.ID, time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Undo(rc.ID); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("later visit: %v", err)
	}
}

func TestEmptyAndUnchangedPlansMakeNoReceipt(t *testing.T) {
	s, path := newStore(t)
	p := mk(t, s, "Original")
	before, _ := os.ReadFile(path)
	count := len(s.Receipts())
	for _, ops := range [][]PlanOp{nil, {{Kind: PlanRename, PlaceID: p.ID, Name: p.Name}}} {
		rc, err := s.ApplyPlan(ops)
		if err != nil || !rc.Noop() {
			t.Fatalf("%+v %v", rc, err)
		}
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || len(s.Receipts()) != count {
		t.Fatal("noop changed store")
	}
}

func TestMergingAHomeWithAShortcutKeepsAHome(t *testing.T) {
	s, _ := newStore(t)
	_, err := s.ApplyPlan([]PlanOp{
		{Kind: PlanCreate, PlaceID: "a", Name: "Alpha"},
		{Kind: PlanCreate, PlaceID: "b", Name: "Beta"},
		{Kind: PlanFile, PlaceID: "a", ChatID: "chat"},
		{Kind: PlanLink, PlaceID: "b", ChatID: "chat"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.MergePlaces("a", "b"); err != nil {
		t.Fatal(err)
	}
	rows := snap(t, s).PlacesOf("chat")
	if len(rows) != 1 || rows[0].Link {
		t.Fatalf("home lost: %+v", rows)
	}
}

func TestTheOrganiserCanReuseExportedPlaceHelpers(t *testing.T) {
	if got := CleanFolder(" /tmp/example/../workspace "); got != "/tmp/workspace" {
		t.Fatal(got)
	}
	var answer struct {
		Place string `json:"place"`
	}
	if err := DecodeObject("```json\n{\"place\":\"p2\"}\n```", &answer); err != nil {
		t.Fatal(err)
	}
	if i, ok := PickIndex(answer.Place, "p", []string{"Alpha", "Beta"}); !ok || i != 1 {
		t.Fatalf("%d %v", i, ok)
	}
	if _, ok := PickIndex("unshown", "p", []string{"Alpha"}); ok {
		t.Fatal("accepted unknown option")
	}
	if name, err := CleanSuggestedName("'Example'"); err != nil || name != "Example" {
		t.Fatalf("%s %v", name, err)
	}
	if _, err := CleanSuggestedName("https://example.com"); err == nil {
		t.Fatal("accepted URL")
	}
	s, _ := newStore(t)
	a := mk(t, s, "Example")
	b := mk(t, s, "Examples")
	offers := SameNameMerges(snap(t, s), []string{a.ID})
	if len(offers) != 1 || offers[0].PlaceID != b.ID || offers[0].FromPlaceID != a.ID {
		t.Fatalf("%+v", offers)
	}
}
