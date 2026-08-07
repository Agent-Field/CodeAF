package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestSelfReceiptEmitsFromSettledUsageAndCapturesLearning(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "self-receipts.db"))
	intent := "Practice the parser frontier"
	spliceSelfLeaf(t, graph, "practice-1", intent, Provenance{})
	fact, err := graph.RecordFact("practice-1", "repo:/work/parser", FactLesson,
		"validate the recovery token before advancing")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := graph.RecordSkillCandidate("practice-1", "tool:parser",
		"replay malformed tokens through the recovery harness", "/tmp/parser-skill")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.RecordSurprise(NodeSurprise{
		NodeID: "practice-1", ActualTokens: 180, ExpectedTokens: 100, Surprise: 0.8,
	}); err != nil {
		t.Fatal(err)
	}
	settleSelfLeaf(t, graph, "practice-1", 0.31)

	spliceSelfLeaf(t, graph, "practice-2", intent, Provenance{})
	if err := graph.RecordSurprise(NodeSurprise{
		NodeID: "practice-2", ActualTokens: 130, ExpectedTokens: 100, Surprise: 0.3,
	}); err != nil {
		t.Fatal(err)
	}
	settleSelfLeaf(t, graph, "practice-2", 0.12)

	assert := func(stage string) []SelfReceipt {
		t.Helper()
		receipts, err := graph.SelfReceipts(time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if len(receipts) != 2 {
			t.Fatalf("%s receipts = %+v, want 2", stage, receipts)
		}
		first, second := receipts[0], receipts[1]
		if first.NodeID != "practice-1" || first.Origin != intent || first.Cost != 0.31 ||
			!reflect.DeepEqual(first.FactIDs, []int64{fact.Seq}) ||
			!reflect.DeepEqual(first.SkillIDs, []int64{skill.Seq}) || first.Nothing ||
			first.Surprise == nil || *first.Surprise != 0.8 || first.SurpriseDelta != nil {
			t.Fatalf("%s first receipt = %+v", stage, first)
		}
		if second.Cost != 0.12 || second.SurpriseDelta == nil || *second.SurpriseDelta != 0.5 || second.Nothing {
			t.Fatalf("%s second receipt = %+v, want positive 0.5 surprise delta", stage, second)
		}
		spend, err := graph.SelfSpendToday()
		if err != nil || spend != 0.43 {
			t.Fatalf("%s self spend = %.4f err=%v, want 0.43", stage, spend, err)
		}
		return receipts
	}
	before := assert("incremental")
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after := assert("rebuilt")
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("receipts changed on rebuild\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestTwoNothingSelfReceiptsPauseOriginatingCharterWithReason(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "self-charter-retire.db"))
	charter := mustTestCharter(t, "charter-curiosity", CharterActive, CharterRails{
		PerFiringBudgetUSD: 0.10, MaxFiringsPerDay: 3,
	})
	if err := graph.CreateCharter(charter); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"curiosity-1", "curiosity-2"} {
		spliceSelfLeaf(t, graph, id, "Probe the same open question", Provenance{CharterID: charter.ID})
		settleSelfLeaf(t, graph, id, float64(index+1)/100)
		current, found, err := graph.Charter(charter.ID)
		if err != nil || !found {
			t.Fatalf("charter after receipt %d found=%t err=%v", index+1, found, err)
		}
		want := CharterActive
		if index == 1 {
			want = CharterPaused
		}
		if current.Status != want {
			t.Fatalf("charter after receipt %d = %s, want %s", index+1, current.Status, want)
		}
	}

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var retirement SelfInquiryRetirement
	var pauseReason string
	for _, event := range events {
		switch event.Kind {
		case EventSelfInquiryRetired:
			if err := json.Unmarshal(event.Payload, &retirement); err != nil {
				t.Fatal(err)
			}
		case EventCharterStatusChanged:
			var payload charterStatusPayload
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.Status == CharterPaused {
				pauseReason = payload.Reason
			}
		}
	}
	if retirement.Action != "charter_paused" || retirement.Reason != selfInquiryRetirementReason ||
		pauseReason != selfInquiryRetirementReason {
		t.Fatalf("retirement=%+v pause reason=%q", retirement, pauseReason)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	rebuilt, found, err := graph.Charter(charter.ID)
	if err != nil || !found || rebuilt.Status != CharterPaused {
		t.Fatalf("rebuilt charter = %+v found=%t err=%v", rebuilt, found, err)
	}
}

func TestTwoNothingSelfReceiptsRetireOriginatingFact(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "self-fact-retire.db"))
	left, err := graph.RecordFact("", "tool:search", FactLesson, "use lexical search first")
	if err != nil {
		t.Fatal(err)
	}
	right, err := graph.RecordFact("", "tool:search", FactLesson, "use semantic search first")
	if err != nil {
		t.Fatal(err)
	}
	pair, err := graph.RecordUnsettledFact("", "tool:search", UnsettledPair{Approaches: []UnsettledApproach{
		{Approach: "lexical", Scope: "tool:search", Evidence: []int64{left.Seq}},
		{Approach: "semantic", Scope: "tool:search", Evidence: []int64{right.Seq}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"question-1", "question-2"} {
		spliceSelfLeaf(t, graph, id, "Settle search strategy", Provenance{TrialOf: pair.Seq})
		settleSelfLeaf(t, graph, id, 0.01)
	}
	retired, found, err := graph.FactBySeq(pair.Seq)
	if err != nil || !found || retired.Status != FactSuperseded ||
		!strings.Contains(retired.StatusNote, "2 consecutive") {
		t.Fatalf("retired fact = %+v found=%t err=%v", retired, found, err)
	}
}

func spliceSelfLeaf(t *testing.T, graph *Store, id, intent string, extra Provenance) {
	t.Helper()
	extra.Origin = OriginSelf
	extra.Intent = intent
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: id, Brief: intent, Stage: 1,
	}}}, extra); err != nil {
		t.Fatal(err)
	}
}

func settleSelfLeaf(t *testing.T, graph *Store, id string, cost float64) {
	t.Helper()
	if err := graph.RecordUsage(NodeUsage{NodeID: id, Cost: cost}); err != nil {
		t.Fatal(err)
	}
	claim, ok, err := graph.Claim(id, "self-receipt-test")
	if err != nil || !ok {
		t.Fatalf("claim %s ok=%t err=%v", id, ok, err)
	}
	if err := graph.Start(claim); err != nil {
		t.Fatal(err)
	}
	if err := graph.Complete(claim, "settled"); err != nil {
		t.Fatal(err)
	}
}
