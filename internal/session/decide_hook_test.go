package session

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/decide"
	"github.com/Agent-Field/codeaf/internal/placegraph"
)

// THE GATE IS ASKED BEFORE A QUESTION IS SHOWN. Each branch is a fake place
// and a fake score, on a real session agent, because the answer has to go
// through ResolveQuestion and the decision ledger and not a copy of either.

func TestDecideGateAsksThePersonWhenItMustNotDecide(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	t.Run("irreversible", func(t *testing.T) {
		agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 100)
		q := decideQuestion(StakesIrreversible, AskPermission)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideForPerson || !got.Reach || got.Escalated || gate.resolves != 0 {
			t.Fatalf("irreversible was decided: %+v resolves=%d", got, gate.resolves)
		}
		if q.Proposal != nil || q.Pick != nil {
			t.Fatalf("irreversible grew a proposal: pick=%+v proposal=%+v", q.Pick, q.Proposal)
		}
		assertNoDecision(t, gate)
	})
	t.Run("always ask me", func(t *testing.T) {
		agent, gate := newDecideFixture(t, fixed, "marketing", true, decide.ModeDeciding, 100)
		q := decideQuestion(StakesReversible, AskPermission)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideForPerson || !got.Reach || gate.resolves != 0 {
			t.Fatalf("always ask me was decided: %+v resolves=%d", got, gate.resolves)
		}
		assertNoDecision(t, gate)
	})
	t.Run("policy forbids", func(t *testing.T) {
		agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 100)
		gate.PolicyForbids = func(Question) bool { return true }
		q := decideQuestion(StakesReversible, AskPermission)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideForPerson || !got.Reach || gate.resolves != 0 {
			t.Fatalf("a forbidden policy was decided: %+v resolves=%d", got, gate.resolves)
		}
		assertNoDecision(t, gate)
	})
	t.Run("a judgement is a person's", func(t *testing.T) {
		agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 100)
		q := decideQuestion(StakesReversible, AskJudgement)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideForPerson || gate.resolves != 0 {
			t.Fatalf("a judgement was decided: %+v resolves=%d", got, gate.resolves)
		}
	})
	t.Run("kind set to always ask", func(t *testing.T) {
		agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeAsk, 100)
		q := decideQuestion(StakesReversible, AskPermission)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideForPerson || q.Proposal != nil || gate.resolves != 0 {
			t.Fatalf("always-ask kind proposed or decided: %+v proposal=%+v resolves=%d", got, q.Proposal, gate.resolves)
		}
	})
}

func TestDecideGateLearningAttachesTheProposal(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeLearning, 92)
	if err := gate.stores["marketing"].RecordOutcome(permKindKey(), decide.Outcome{Agreed: true, At: fixed}); err != nil {
		t.Fatal(err)
	}
	// Thirteen more, so the count is 14 of 20. The mode stays learning:
	// graduating is a different write, and 14 is not 18.
	for i := 0; i < 13; i++ {
		if err := gate.stores["marketing"].RecordOutcome(permKindKey(), decide.Outcome{Agreed: true, At: fixed}); err != nil {
			t.Fatal(err)
		}
	}
	q := decideQuestion(StakesReversible, AskPermission)
	got := gate.apply(agent, &q)
	if got.Outcome != DecidePropose || !got.Reach || got.PlaceID != "marketing" || got.Escalated {
		t.Fatalf("learning did not propose: %+v", got)
	}
	if q.Pick == nil || q.Pick.Key != "1" || q.Pick.Percent != 92 || q.Pick.Reason != "matches what Marketing knows" {
		t.Fatalf("pick = %+v", q.Pick)
	}
	if q.Proposal == nil || q.Proposal.Place != "Marketing" || q.Proposal.Agreed != 14 || q.Proposal.Of != 20 {
		t.Fatalf("proposal = %+v", q.Proposal)
	}
	if decide.RingSize != 20 {
		t.Fatalf("graduation window = %d, want 20", decide.RingSize)
	}
	again := decideQuestion(StakesReversible, AskPermission)
	replay := gate.apply(agent, &again)
	if replay.Outcome != DecidePropose || again.Proposal == nil || again.Proposal.Agreed != 14 || again.Pick.Percent != 92 {
		t.Fatalf("replay = %+v proposal=%+v pick=%+v", replay, again.Proposal, again.Pick)
	}
	if gate.resolves != 0 {
		t.Fatalf("learning answered the question: %d", gate.resolves)
	}
	assertNoDecision(t, gate)
}

func TestDecideGateLearningDoesNotEscalateToADecidingParent(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideChain(t, fixed, decide.ModeLearning, 92, decide.ModeDeciding, 97)
	q := decideQuestion(StakesReversible, AskPermission)
	got := gate.apply(agent, &q)
	if got.Outcome != DecidePropose || got.Escalated || got.PlaceID != "marketing" {
		t.Fatalf("learning escalated: %+v", got)
	}
	if q.Proposal == nil || q.Proposal.Place != "Marketing" {
		t.Fatalf("proposal = %+v", q.Proposal)
	}
	assertNoDecision(t, gate)
}

func TestDecideGateDecidesOnceWhenThePlaceIsSure(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	because := "You allowed go test here 6 times. Reversible."
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, placegraph.DecideThresholdDefault)
	gate.Score = func(placeID string, q Question) (decide.Result, string, error) {
		return decide.Result{Percent: placegraph.DecideThresholdDefault, Because: because}, "1", nil
	}
	q := decideQuestion(StakesReversible, AskPermission)
	got := gate.apply(agent, &q)
	if got.Outcome != DecideTake || got.Reach || got.Escalated || got.PlaceID != "marketing" {
		t.Fatalf("did not decide: %+v", got)
	}
	wantText := "Allowed automatically by Marketing · " + because + " · Why?"
	if got.Receipt.Text != wantText || got.Receipt.Percent != 90 {
		t.Fatalf("receipt = %+v", got.Receipt)
	}
	if gate.resolves != 1 || len(gate.Receipts()) != 1 {
		t.Fatalf("resolves=%d receipts=%d", gate.resolves, len(gate.Receipts()))
	}
	records := agent.Decisions()
	if len(records) != 1 || records[0].Scope != ScopeOnce || records[0].By != DecidedByDial {
		t.Fatalf("session record = %+v", records)
	}
	if len(records[0].Picked) != 1 || records[0].Picked[0] != "1" {
		t.Fatalf("picked = %v", records[0].Picked)
	}
	listed, err := gate.stores["marketing"].List()
	if err != nil || len(listed) != 1 {
		t.Fatalf("ledger = %+v err=%v", listed, err)
	}
	if listed[0].By != "marketing" || listed[0].Percent != 90 || listed[0].Because != because || !listed[0].Reversible {
		t.Fatalf("decision = %+v", listed[0])
	}
	if listed[0].QuestionRef.ID != q.Token() || listed[0].Stakes != string(StakesReversible) {
		t.Fatalf("decision ref = %+v", listed[0])
	}
	again := decideQuestion(StakesReversible, AskPermission)
	replay := gate.apply(agent, &again)
	if replay.Outcome != DecideTake || replay.Reach || gate.resolves != 1 || len(gate.Receipts()) != 1 {
		t.Fatalf("replay decided again: %+v resolves=%d receipts=%d", replay, gate.resolves, len(gate.Receipts()))
	}
	listed, _ = gate.stores["marketing"].List()
	if len(listed) != 1 || len(agent.Decisions()) != 1 {
		t.Fatalf("replay wrote a second record: ledger=%d session=%d", len(listed), len(agent.Decisions()))
	}
}

func TestDecideGateDecidesACostlyQuestionThePlaceIsSureAbout(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 97)
	q := decideQuestion(StakesCostly, AskPermission)
	got := gate.apply(agent, &q)
	if got.Outcome != DecideTake || got.Reach {
		t.Fatalf("costly was not decided: %+v", got)
	}
	listed, err := gate.stores["marketing"].List()
	if err != nil || len(listed) != 1 || listed[0].Stakes != string(StakesCostly) || !listed[0].Reversible {
		t.Fatalf("ledger = %+v err=%v", listed, err)
	}
}

func TestDecideGateEscalatesWhenThePlaceIsNotSure(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	t.Run("a parent that is sure decides", func(t *testing.T) {
		because := "You allowed go test here 6 times. Reversible."
		agent, gate := newDecideChain(t, fixed, decide.ModeDeciding, 40, decide.ModeDeciding, 97)
		gate.Score = func(placeID string, q Question) (decide.Result, string, error) {
			if placeID == "software" {
				return decide.Result{Percent: 97, Because: because}, "1", nil
			}
			return decide.Result{Percent: 40, Because: "mixed"}, "1", nil
		}
		q := decideQuestion(StakesReversible, AskPermission)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideTake || !got.Escalated || got.Reach || got.PlaceID != "software" {
			t.Fatalf("parent did not decide: %+v", got)
		}
		if got.Receipt.Text != "Allowed automatically by Software · "+because+" · Why?" {
			t.Fatalf("receipt = %q", got.Receipt.Text)
		}
		if gate.resolves != 1 {
			t.Fatalf("resolves = %d", gate.resolves)
		}
		child, _ := gate.stores["marketing"].List()
		parent, err := gate.stores["software"].List()
		if err != nil || len(child) != 0 || len(parent) != 1 || parent[0].PlaceID != "software" {
			t.Fatalf("child=%+v parent=%+v err=%v", child, parent, err)
		}
		replay := gate.apply(agent, &q)
		if replay.PlaceID != "software" || gate.resolves != 1 {
			t.Fatalf("replay = %+v resolves=%d", replay, gate.resolves)
		}
	})
	t.Run("nowhere to go asks the person", func(t *testing.T) {
		agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 40)
		q := decideQuestion(StakesReversible, AskPermission)
		got := gate.apply(agent, &q)
		if got.Outcome != DecideForPerson || !got.Reach || !got.Escalated || gate.resolves != 0 {
			t.Fatalf("below threshold = %+v resolves=%d", got, gate.resolves)
		}
		if q.Proposal != nil {
			t.Fatalf("a low score became a proposal: %+v", q.Proposal)
		}
		assertNoDecision(t, gate)
	})
}

func TestDecideGateNeverDecidesAWithdrawnQuestion(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 100)
	q := decideQuestion(StakesReversible, AskPermission)
	q.Withdrawn = &Withdrawal{Reason: "the turn moved on", By: AskerEngine, At: fixed}
	got := gate.apply(agent, &q)
	if got.Outcome != DecideForPerson || gate.resolves != 0 || len(gate.Receipts()) != 0 {
		t.Fatalf("withdrew into a decision: %+v resolves=%d", got, gate.resolves)
	}
	again := gate.apply(agent, &q)
	if again.Outcome != DecideForPerson || gate.resolves != 0 {
		t.Fatalf("replay of a withdrawn question decided: %+v resolves=%d", again, gate.resolves)
	}
	assertNoDecision(t, gate)
}

func TestDecideGateHoldsADecisionOffThePresenceDesk(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeDeciding, 97)
	agent.SetDecideGate(gate.DecideGate)
	q := decideQuestion(StakesReversible, AskPermission)
	letGo := agent.presenceAskingWhole(q, nil)
	letGo()
	if deskRows(agent) != 0 {
		t.Fatalf("a decided question reached the desk: %d", deskRows(agent))
	}
	agent.mu.Lock()
	banked := len(agent.questionWords)
	agent.mu.Unlock()
	if banked != 0 {
		t.Fatalf("a decided question was raised: %d", banked)
	}
	if gate.resolves != 1 || len(agent.Decisions()) != 1 {
		t.Fatalf("resolves=%d records=%d", gate.resolves, len(agent.Decisions()))
	}
	letGo = agent.presenceAskingWhole(q, nil)
	letGo()
	if deskRows(agent) != 0 || gate.resolves != 1 {
		t.Fatalf("replay reached the desk or decided again: desk=%d resolves=%d", deskRows(agent), gate.resolves)
	}
}

func TestDecideGateShowsAProposalOnThePresenceDesk(t *testing.T) {
	fixed := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	agent, gate := newDecideFixture(t, fixed, "marketing", false, decide.ModeLearning, 92)
	agent.SetDecideGate(gate.DecideGate)
	letGo := agent.presenceAskingWhole(decideQuestion(StakesReversible, AskPermission), nil)
	defer letGo()
	if deskRows(agent) != 1 {
		t.Fatalf("the proposal never reached the desk: %d", deskRows(agent))
	}
	agent.presence.mu.Lock()
	full := agent.presence.asks[0].question.Full
	agent.presence.mu.Unlock()
	if full == nil || full.Proposal == nil || full.Proposal.Of != 20 || full.Pick == nil || full.Pick.Percent != 92 {
		t.Fatalf("desk question = %+v", full)
	}
}

func TestDecideGateWithNoHookReachesPresence(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place.Dir = t.TempDir()
	})
	letGo := agent.presenceAskingWhole(decideQuestion(StakesReversible, AskPermission), nil)
	defer letGo()
	if deskRows(agent) != 1 {
		t.Fatalf("desk rows = %d, want the question shown", deskRows(agent))
	}
}

type decideFixture struct {
	*DecideGate
	stores map[string]*decide.Store
}

func newDecideFixture(t *testing.T, at time.Time, placeID string, alwaysAsk bool, mode decide.Mode, percent int) (*Agent, *decideFixture) {
	t.Helper()
	return newDecideChain(t, at, mode, percent, "", 0, fixturePlace{id: placeID, alwaysAsk: alwaysAsk})
}

type fixturePlace struct {
	id        string
	alwaysAsk bool
}

func newDecideChain(t *testing.T, at time.Time, childMode decide.Mode, childPercent int, parentMode decide.Mode, parentPercent int, extra ...fixturePlace) (*Agent, *decideFixture) {
	t.Helper()
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.Place.Dir = dir
	})
	// A chain starts at Marketing and can climb to Software. A single place
	// has nowhere to climb, which is the "nowhere to go" branch.
	places := map[string]placegraph.Place{
		"marketing": {ID: "marketing", Name: "Marketing", Parents: []string{"software"}},
		"software":  {ID: "software", Name: "Software"},
	}
	member := "marketing"
	if parentMode == "" && len(extra) == 1 {
		member = extra[0].id
		places = map[string]placegraph.Place{
			member: {ID: member, Name: placeTitle(member)},
		}
	}
	settings := map[string]placegraph.Decide{}
	if len(extra) == 1 && extra[0].alwaysAsk {
		settings[extra[0].id] = placegraph.Decide{AlwaysAsk: true, Threshold: placegraph.DecideThresholdDefault}
	}
	graph := gateGraph{
		places:   places,
		members:  []placegraph.Membership{{ChatID: "launch", PlaceID: member}},
		settings: settings,
	}
	fx := &decideFixture{stores: map[string]*decide.Store{}}
	fx.DecideGate = &DecideGate{
		Graph:  graph,
		ChatID: "launch",
		OpenStore: func(id string) (*decide.Store, error) {
			if s, ok := fx.stores[id]; ok {
				return s, nil
			}
			s, err := decide.Open(dir, id, func() time.Time { return at })
			if err != nil {
				return nil, err
			}
			fx.stores[id] = s
			return s, nil
		},
		Score: func(placeID string, q Question) (decide.Result, string, error) {
			percent := childPercent
			if placeID == "software" {
				percent = parentPercent
			}
			return decide.Result{Percent: percent, Because: "matches what Marketing knows"}, "1", nil
		},
		Now: func() time.Time { return at },
	}
	childStore, err := fx.OpenStore("marketing")
	if err != nil {
		t.Fatal(err)
	}
	if childMode != "" && childMode != decide.ModeLearning {
		if err := childStore.SetMode(permKindKey(), childMode); err != nil {
			t.Fatal(err)
		}
	}
	if parentMode != "" {
		parentStore, err := fx.OpenStore("software")
		if err != nil {
			t.Fatal(err)
		}
		if parentMode != decide.ModeLearning {
			if err := parentStore.SetMode(permKindKey(), parentMode); err != nil {
				t.Fatal(err)
			}
		}
	}
	return agent, fx
}

func placeTitle(id string) string {
	if id == "" {
		return ""
	}
	return string(id[0]-32) + id[1:]
}

func permKindKey() string { return decide.KindKey(string(AskPermission), "") }

func decideQuestion(stakes Stakes, ask AskKind) Question {
	q := wellFormed()
	q.ID = 7
	q.Ask = ask
	q.Stakes = stakes
	q.Head = "May it run go test?"
	q.Reason = "the suite has not run yet"
	return q
}

func assertNoDecision(t *testing.T, gate *decideFixture) {
	t.Helper()
	if len(gate.Receipts()) != 0 {
		t.Fatalf("receipts = %+v", gate.Receipts())
	}
	for id, store := range gate.stores {
		listed, err := store.List()
		if err != nil || len(listed) != 0 {
			t.Fatalf("%s ledger = %+v err=%v", id, listed, err)
		}
	}
}

// gateGraph is the slice of the place graph the gate reads. It is not a store.
type gateGraph struct {
	places   map[string]placegraph.Place
	members  []placegraph.Membership
	settings map[string]placegraph.Decide
}

func (g gateGraph) Place(id string) (placegraph.Place, bool) {
	p, ok := g.places[id]
	return p, ok
}

func (g gateGraph) PlacesOf(chatID string) []placegraph.Membership {
	var out []placegraph.Membership
	for _, m := range g.members {
		if m.ChatID == chatID {
			out = append(out, m)
		}
	}
	return out
}

func (g gateGraph) EffectiveDecide(id string) (placegraph.Decide, string) {
	if d, ok := g.settings[id]; ok {
		return d, id
	}
	return placegraph.DefaultDecide(), ""
}
