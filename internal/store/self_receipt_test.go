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

// Work an older build's charter admitted can still be running when this build
// takes over, and it still settles into a receipt. Its line of inquiry is named
// after the rule it served — from the table that build left behind, when the
// store has one — and two receipts that learned nothing retire the line the way
// any other line retires. There is no charter left to pause, so none is.
func TestTwoNothingSelfReceiptsOnAnOlderBuildsCharterRetireTheLine(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "self-charter-retire.db"))
	if _, err := graph.db.Exec(olderBuildsCharterSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.db.Exec(`INSERT INTO charters (id, invariant, watch, action, rails, status, ratification,
		created_seq, updated_seq, created_at) VALUES ('charter-curiosity', ?, '{}', '{}', '{}', 'active', '{}', 1, 1, ?)`,
		"Probe the parser frontier\nwhenever the machine is quiet", "2026-09-01T09:00:00Z"); err != nil {
		t.Fatal(err)
	}
	for index, id := range []string{"curiosity-1", "curiosity-2"} {
		spliceSelfLeaf(t, graph, id, "Probe the same open question", Provenance{CharterID: "charter-curiosity"})
		settleSelfLeaf(t, graph, id, float64(index+1)/100)
	}

	receipts, err := graph.SelfReceipts(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(receipts) != 2 || receipts[0].Origin != "Probe the parser frontier" ||
		receipts[0].Scope != "charter:charter-curiosity" || receipts[0].TargetKind != "charter" {
		t.Fatalf("receipts = %+v, want them named after the older build's rule", receipts)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var retirement SelfInquiryRetirement
	for _, event := range events {
		switch event.Kind {
		case EventSelfInquiryRetired:
			if err := json.Unmarshal(event.Payload, &retirement); err != nil {
				t.Fatal(err)
			}
		case EventKind("charter_status_changed"):
			t.Fatalf("a removed rule was paused: %s", event.Payload)
		}
	}
	if retirement.Action != "line_retired" || retirement.Reason != selfInquiryRetirementReason ||
		retirement.TargetID != "charter-curiosity" {
		t.Fatalf("retirement=%+v", retirement)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
}

// And on a store that never had the table, the rule's own id is its name.
func TestASelfReceiptNamesACharterItCannotReadByItsID(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "self-charter-name.db"))
	spliceSelfLeaf(t, graph, "curiosity-1", "Probe the same open question", Provenance{CharterID: "charter-9"})
	settleSelfLeaf(t, graph, "curiosity-1", 0.01)
	receipts, err := graph.SelfReceipts(time.Time{})
	if err != nil || len(receipts) != 1 || receipts[0].Origin != "charter-9" {
		t.Fatalf("receipts = %+v err=%v", receipts, err)
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
