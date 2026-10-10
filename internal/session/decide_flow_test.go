package session

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// THE DECISION GATE ACROSS A PLACE GRAPH, START TO FINISH. The hook tests pin
// one branch each. These follow one place through its whole life: it proposes
// while it learns, graduates at 18 of 20, decides, is overturned, and has to
// earn the right again. Every ledger is a real file in a temp directory and
// the clock is fixed, so nothing here sleeps.

// graduate answers the permission kind the way the person would, the given
// number of agreements and then disagreements, through the same door the
// session uses when a person answers a proposal.
func graduate(t *testing.T, s *decide.Store, agreed, overruled int, at time.Time) decide.KindState {
	t.Helper()
	var st decide.KindState
	var err error
	for i := 0; i < agreed+overruled; i++ {
		chosen := "1"
		if i >= agreed {
			chosen = "3"
		}
		st, err = s.RecordProposal(decide.ProposalAnswer{AskKind: string(AskPermission), ProposedKey: "1", ChosenKey: chosen, At: at})
		if err != nil {
			t.Fatal(err)
		}
	}
	return st
}

func TestAPlaceLearnsThenDecidesThenMustEarnItAgain(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeLearning, 97)
	store := gate.stores["marketing"]

	// A new place proposes and answers nothing.
	q := decideQuestion(StakesReversible, AskPermission)
	if got := gate.apply(agent, &q); got.Outcome != DecidePropose || gate.resolves != 0 {
		t.Fatalf("a new place did not propose: %+v", got)
	}

	// 17 of 19 is not yet 18 of 20: the ring has to be full.
	if st := graduate(t, store, 17, 2, fixed); st.Mode != decide.ModeLearning {
		t.Fatalf("graduated early: %+v", st)
	}
	// The 20th answer is the one that crosses the line.
	st, err := store.RecordProposal(decide.ProposalAnswer{AskKind: string(AskPermission), ProposedKey: "1", ChosenKey: "1", At: fixed})
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != decide.ModeDeciding || decide.Agreements(st) != 18 || len(st.Recent) != decide.RingSize {
		t.Fatalf("18 of 20 did not graduate: mode=%s agreed=%d ring=%d", st.Mode, decide.Agreements(st), len(st.Recent))
	}

	// Now it decides, once, and leaves a ledger row and a receipt.
	next := decideQuestion(StakesReversible, AskPermission)
	next.ID = 8
	got := gate.apply(agent, &next)
	if got.Outcome != DecideTake || got.Reach || gate.resolves != 1 {
		t.Fatalf("a graduated place did not decide: %+v resolves=%d", got, gate.resolves)
	}
	rows, _ := store.List()
	if len(rows) != 1 || rows[0].By != "marketing" || rows[0].OverturnedAt != nil {
		t.Fatalf("ledger = %+v", rows)
	}

	t.Run("restart keeps the mode and the ledger", func(t *testing.T) {
		// Dropping the cached handle makes the fixture open the file afresh.
		delete(gate.stores, "marketing")
		reopened, err := gate.OpenStore("marketing")
		if err != nil {
			t.Fatal(err)
		}
		mode, _ := reopened.Mode(permKindKey())
		again, _ := reopened.List()
		if mode.Mode != decide.ModeDeciding || len(again) != 1 || again[0].ID != rows[0].ID {
			t.Fatalf("after restart: mode=%+v ledger=%+v", mode, again)
		}
	})

	t.Run("an overturn drops only that kind", func(t *testing.T) {
		const other = "permission:git"
		if err := store.SetMode(other, decide.ModeDeciding); err != nil {
			t.Fatal(err)
		}
		if err := store.OverturnDecided(rows[0].ID, fixed); err != nil {
			t.Fatal(err)
		}
		dropped, _ := store.Mode(permKindKey())
		kept, _ := store.Mode(other)
		if dropped.Mode != decide.ModeLearning || len(dropped.Recent) != 0 || kept.Mode != decide.ModeDeciding {
			t.Fatalf("overturn spread: dropped=%+v kept=%+v", dropped, kept)
		}
		// Back in learning, the same sure score proposes instead of deciding.
		after := decideQuestion(StakesReversible, AskPermission)
		after.ID = 9
		if res := gate.apply(agent, &after); res.Outcome != DecidePropose || gate.resolves != 1 {
			t.Fatalf("an overturned kind still decided: %+v resolves=%d", res, gate.resolves)
		}
	})
}

func TestAGraduatedPlaceStillSendsIrreversibleWorkToNextUp(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeLearning, 100)
	graduate(t, gate.stores["marketing"], 20, 0, fixed)
	if st, _ := gate.stores["marketing"].Mode(permKindKey()); st.Mode != decide.ModeDeciding {
		t.Fatalf("setup: %+v", st)
	}
	q := decideQuestion(StakesIrreversible, AskPermission)
	got := gate.apply(agent, &q)
	if got.Outcome != DecideForPerson || !got.Reach || gate.resolves != 0 || q.Proposal != nil {
		t.Fatalf("irreversible was not left for the person: %+v", got)
	}
	assertNoDecision(t, gate)
}

func TestAGraduatedChildThatIsNotSureEscalatesToItsParent(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideChain(t, fixed, decide.ModeLearning, 40, decide.ModeDeciding, 97)
	graduate(t, gate.stores["marketing"], 20, 0, fixed)
	q := decideQuestion(StakesReversible, AskPermission)
	got := gate.apply(agent, &q)
	if got.Outcome != DecideTake || !got.Escalated || got.PlaceID != "software" {
		t.Fatalf("parent did not take it: %+v", got)
	}
	child, _ := gate.stores["marketing"].List()
	parent, _ := gate.stores["software"].List()
	if len(child) != 0 || len(parent) != 1 {
		t.Fatalf("the decision belongs in the parent's ledger: child=%d parent=%d", len(child), len(parent))
	}
}

func TestAKnowsLineThatIsSupersededStopsBackingAProposal(t *testing.T) {
	f := newPlaceFixture(t)
	place := f.place(t, "Release", placegraph.Context{})
	old, _, err := f.store.AddLine(placegraph.Line{PlaceID: place.ID, Text: "Running go test is always fine", Source: placegraph.LineSource{Kind: placegraph.LineLearned}})
	if err != nil {
		t.Fatal(err)
	}
	proposed := func(chat *Agent) *Pick {
		hold := chat.presenceAskingWhole(goTestPermission(7), nil)
		defer hold()
		chat.presence.mu.Lock()
		defer chat.presence.mu.Unlock()
		if len(chat.presence.asks) == 0 || chat.presence.asks[0].question.Full == nil {
			return nil
		}
		return chat.presence.asks[0].question.Full.Pick
	}
	first := wiredChat(t, f)
	f.file(t, first.id, place)
	if pick := proposed(first); pick == nil || pick.Percent != decide.KnowsMatchPercent {
		t.Fatalf("the live line did not back a proposal: %+v", pick)
	}

	// The newer line wins, and the old one is struck. It names no subject, so
	// there is nothing left to back the answer.
	if _, ask, _, err := f.store.WriteKnowledge(place.ID, "", "Ask before running anything here", old.ID); err != nil || ask != nil {
		t.Fatalf("supersede: ask=%+v err=%v", ask, err)
	}
	second := wiredChat(t, f)
	if pick := proposed(second); pick != nil && strings.Contains(pick.Reason, "knows") {
		t.Fatalf("a struck line still backs a proposal: %+v", pick)
	}
}

// A plan the person cancels never reached a door: no step ran, so there is
// nothing to undo, and the plan cannot be started afterwards.
func TestCancellingAPlanTouchesNothing(t *testing.T) {
	fx := &fakePlanEffects{}
	book := NewPlanBook(fx, nil)
	plan := Plan{ID: "p1", ReachesBeyond: true, Steps: oneOfEach()}
	if receipt, err := book.Propose(context.Background(), plan); err != nil || receipt != nil {
		t.Fatalf("a plan that reaches beyond the chat should wait: %v %v", receipt, err)
	}
	if err := book.Cancel("p1"); err != nil {
		t.Fatal(err)
	}
	if len(fx.calls) != 0 {
		t.Fatalf("cancel ran effects: %v", fx.calls)
	}
	if _, err := book.Go(context.Background(), "p1"); err == nil {
		t.Fatal("a cancelled plan ran")
	}
	if len(fx.calls) != 0 {
		t.Fatalf("go after cancel ran effects: %v", fx.calls)
	}
}
