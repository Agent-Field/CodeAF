package placeorg

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestRefreshRunsTheStagesInOrderAndPublishesProgressAfterEach(t *testing.T) {
	log := &callLog{}
	r, j := newTestRunner(t, fakeStages(log))
	var stages []string
	plan, err := r.Run(context.Background(), fakeCorpus(), Options{
		Mode: ModeRefresh, Now: fixedClock(),
		Progress: func(p Progress) {
			if !p.Cancellable {
				t.Errorf("progress at %s says it cannot be cancelled", p.Stage)
			}
			if n := len(stages); n == 0 || stages[n-1] != p.Stage {
				stages = append(stages, p.Stage)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"dedup", "kind", "anchors", "vectors", "neighbours", "communities", "nest", "audit", "park", "name", "review", "link"}
	if !reflect.DeepEqual(dedupe(log.calls), want) {
		t.Fatalf("stages ran as %v, want %v", dedupe(log.calls), want)
	}
	wantProgress := append(append([]string(nil), "dedup", "kind", "anchors", "vectors", "neighbours", "communities", "nest", "audit", "park", "name", "review", "link"), "check")
	if !reflect.DeepEqual(stages, wantProgress) {
		t.Fatalf("progress arrived as %v, want %v", stages, wantProgress)
	}
	if !j.has(EntryRunStarted) || !j.has(EntryRunEnded) || !j.has(EntryOp) || !j.has(EntryInvariant) {
		t.Fatalf("the journal is missing a line the record needs: %v", dedupeKinds(j.kinds()))
	}
	if len(plan.Invariants) != len(Invariants) {
		t.Fatalf("the plan carries %d self-check lines, want %d", len(plan.Invariants), len(Invariants))
	}
	for _, res := range plan.Invariants {
		if !res.OK {
			t.Errorf("law %q failed: %s", res.Name, res.Why)
		}
	}
	if !strings.Contains(plan.Summary, "3 places") && !strings.Contains(plan.Summary, "9 places") {
		t.Errorf("the summary %q does not say how many places there are", plan.Summary)
	}
}

func TestTheJournalStatesEveryStagesFallbackAtTheStart(t *testing.T) {
	r, j := newTestRunner(t, fakeStages(&callLog{}))
	if _, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeRefresh, Now: fixedClock()}); err != nil {
		t.Fatal(err)
	}
	var notes []string
	for _, e := range j.entries {
		if e.Kind == EntryFallback {
			notes = append(notes, e.Note)
		}
	}
	for _, want := range []string{"judging a place: keep it", "naming: the first candidate", "reviewing: no change", "a tie: the shallower place"} {
		if !contains(notes, want) {
			t.Errorf("the journal never says %q", want)
		}
	}
}

func TestPastTheCallBudgetTheJudgeFallsBackAndThePlanIsProvisional(t *testing.T) {
	log := &callLog{}
	stages := fakeStages(log)
	stages.Name = fakeName{log: log, asks: true}
	asker := &askCounter{}
	r, j := newTestRunner(t, stages)
	r.Ask = asker.ask
	plan, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeRefresh, Now: fixedClock(), Budget: Budget{Calls: 4, USD: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if asker.calls != 4 {
		t.Fatalf("the model was asked %d times under a budget of 4", asker.calls)
	}
	if plan.Spent.Calls != 4 || plan.Spent.Fallbacks != 5 {
		t.Fatalf("spent %+v; want 4 calls and the 5 remaining places falling back", plan.Spent)
	}
	if !plan.Provisional {
		t.Error("a plan that ran out of budget must say it is provisional")
	}
	if !strings.Contains(plan.Summary, "the budget ran out") {
		t.Errorf("the summary %q does not say why names may be rough", plan.Summary)
	}
	overBudget := 0
	for _, e := range j.entries {
		if e.Kind == EntryFallback && e.Pick != nil && e.Pick.Why == "over budget" {
			overBudget++
		}
	}
	if overBudget != 5 {
		t.Errorf("the journal records %d over-budget fallbacks, want 5", overBudget)
	}
}

func TestACancelledRunReturnsNoPlanAndLeavesTheSnapshotAlone(t *testing.T) {
	log := &callLog{}
	stages := fakeStages(log)
	ctx, cancel := context.WithCancel(context.Background())
	stages.Communities = &fakeCommunities{log: log, onCall: func(n int) {
		if n == 2 {
			cancel()
		}
	}}
	existing := TreeFromNodes([]Node{{ID: "mine", Name: "mine", Depth: 1, Person: true, Members: []string{"chat-0-0"}}})
	r, j := newTestRunner(t, stages)
	plan, err := r.Run(ctx, fakeCorpus(), Options{Mode: ModeRefresh, Now: fixedClock(), Existing: existing})
	if err != context.Canceled || plan != nil {
		t.Fatalf("got plan %v and error %v, want no plan and context.Canceled", plan, err)
	}
	if contains(log.calls, "park") || contains(log.calls, "name") {
		t.Errorf("stages kept running after the cancel: %v", dedupe(log.calls))
	}
	if n, _ := existing.Node("mine"); n.Name != "mine" || len(existing.Roots()) != 1 {
		t.Error("the caller's snapshot changed")
	}
	last := j.entries[len(j.entries)-1]
	if last.Kind != EntryRunEnded || last.Note != "cancelled" {
		t.Errorf("the journal ends with %s %q, want run.ended cancelled", last.Kind, last.Note)
	}
}

func TestTheSameSeedTwiceMakesTheSamePlan(t *testing.T) {
	run := func() *Plan {
		r, _ := newTestRunner(t, fakeStages(&callLog{}))
		plan, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeRefresh, Seed: 7, Now: fixedClock()})
		if err != nil {
			t.Fatal(err)
		}
		return plan
	}
	a, b := run(), run()
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two runs with one seed over one library made different plans")
	}
}

func TestAFailedSelfCheckStopsThePlanFromLeaving(t *testing.T) {
	log := &callLog{}
	stages := fakeStages(log)
	stages.Name = badNames{}
	r, _ := newTestRunner(t, stages)
	plan, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeRefresh, Now: fixedClock()})
	if plan != nil || err == nil || !strings.Contains(err.Error(), "names unique") {
		t.Fatalf("got plan %v, error %v; want the broken name law to stop the plan", plan, err)
	}
}

// badNames gives every place the same name.
type badNames struct{}

func (badNames) Fallbacks() []Fallback { return []Fallback{{When: "naming", Does: "same"}} }
func (badNames) Name(_ context.Context, t *Tree, _ *Corpus, _ Judge) ([]Op, error) {
	var ops []Op
	for id := range t.nodes {
		ops = append(ops, Op{Kind: OpName, Stage: StageName, Place: id, Name: "same", Why: "x"})
	}
	return ops, nil
}

func TestARunWithAStageMissingNamesItBeforeStarting(t *testing.T) {
	stages := fakeStages(&callLog{})
	stages.Nest = nil
	r, j := newTestRunner(t, stages)
	_, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeRefresh})
	if err == nil || !strings.Contains(err.Error(), "nest") {
		t.Fatalf("got %v, want an error naming the nest stage", err)
	}
	if len(j.entries) != 0 {
		t.Error("a run that could not start wrote to the journal")
	}
}

func TestAddOnKeepsEveryPlaceAndFilesOnlyWhatIsUnplaced(t *testing.T) {
	log := &callLog{}
	var nodes []Node
	for g := 0; g < fakeGroups; g++ {
		n := Node{ID: fmt.Sprintf("p%d", g), Name: fmt.Sprintf("group%d", g), Depth: 1, Person: true}
		for i := 0; i < 4; i++ {
			n.Members = append(n.Members, fmt.Sprintf("chat-%d-%d", g, i))
		}
		nodes = append(nodes, n)
	}
	existing := TreeFromNodes(nodes)
	r, _ := newTestRunner(t, fakeStages(log))
	plan, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeAddOn, Now: fixedClock(), Existing: existing})
	if err != nil {
		t.Fatal(err)
	}
	filed := 0
	for _, op := range plan.Ops {
		if op.Kind == OpFile {
			filed++
			if op.Chat[len(op.Chat)-1] < '4' {
				t.Errorf("%s was already placed and was filed again", op.Chat)
			}
		}
	}
	if filed != 6 {
		t.Errorf("filed %d chats, want the 6 that had no place", filed)
	}
	if contains(log.calls, "nest") || contains(log.calls, "audit") || contains(log.calls, "communities") {
		t.Errorf("add-on rebuilt the tree: %v", dedupe(log.calls))
	}
	if plan.Summary != "6 chats filed" {
		t.Errorf("summary %q, want \"6 chats filed\"", plan.Summary)
	}
	if _, home := existing.Home("chat-0-4"); home {
		t.Error("the caller's snapshot gained a chat")
	}
}

func TestRefreshKeepsThePlaceThePersonMadeAndOnlyOffersItsNewName(t *testing.T) {
	log := &callLog{}
	existing := TreeFromNodes([]Node{{ID: "store-1", StoreID: "store-1", Name: "Garden", Depth: 1, Person: true,
		Members: []string{"chat-1-0", "chat-1-1", "chat-1-2", "chat-1-3", "chat-1-4", "chat-1-5"}}})
	r, _ := newTestRunner(t, fakeStages(log))
	plan, err := r.Run(context.Background(), fakeCorpus(), Options{Mode: ModeRefresh, Now: fixedClock(), Existing: existing})
	if err != nil {
		t.Fatal(err)
	}
	var offered []string
	for _, op := range plan.Ops {
		if op.Kind == OpName && op.Proposed {
			offered = append(offered, op.Place)
		}
	}
	// The group's three nested places hold the same chats; the person's place
	// is matched to the first, and only that one is an offer.
	if !reflect.DeepEqual(offered, []string{"c.1"}) {
		t.Errorf("names offered for %v, want only the place that took over the person's", offered)
	}
}

func TestADeclinedRunnerRefusesASecondRunAtOnce(t *testing.T) {
	stages := fakeStages(&callLog{})
	r, _ := newTestRunner(t, stages)
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := r.Run(context.Background(), fakeCorpus(), Options{}); err == nil || !strings.Contains(err.Error(), "already going") {
		t.Fatalf("got %v, want a refusal while a run is going", err)
	}
}

func dedupe(in []string) []string {
	var out []string
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}

func dedupeKinds(in []EntryKind) []EntryKind {
	var out []EntryKind
	for _, s := range in {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
