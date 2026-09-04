package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func finishedLeafOutcome() *exec.Outcome {
	return &exec.Outcome{
		Text:      "the finished result",
		Artifacts: []string{"answer.txt", "report.json"},
		Turns:     4,
		Usage: exec.Usage{
			PromptTokens:     50,
			CompletionTokens: 7,
			Cost:             1.25,
		},
		Stop:    exec.StopDone,
		Verdict: provider.VerdictVerifiedSuccess,
		Account: &exec.Account{
			Files:  []exec.FileChange{{Path: "answer.txt", Change: exec.ChangeAdded, Added: 3}},
			Checks: []exec.Check{{Command: "go test ./...", Passed: true}},
		},
		Calibration: []string{"comfortably inside the envelope"},
	}
}

func assertFinishedLeaf(t *testing.T, node *plan.Node, outcome *exec.Outcome) {
	t.Helper()
	if node == nil {
		t.Fatal("the restored plan has no first leaf")
	}
	if node.Checked != outcome.Account.Summary() {
		t.Errorf("checked = %q, want %q", node.Checked, outcome.Account.Summary())
	}
	if node.Result != outcome.Text {
		t.Errorf("result = %q, want %q", node.Result, outcome.Text)
	}
	if node.Cost != outcome.Usage.Cost {
		t.Errorf("cost = %v, want %v", node.Cost, outcome.Usage.Cost)
	}
	if node.Turns != outcome.Turns {
		t.Errorf("turns = %d, want %d", node.Turns, outcome.Turns)
	}
	wantTokens := outcome.Usage.PromptTokens + outcome.Usage.CompletionTokens
	if node.Tokens != wantTokens {
		t.Errorf("tokens = %d, want %d", node.Tokens, wantTokens)
	}
	if node.Stop != string(outcome.Stop) {
		t.Errorf("stop = %q, want %q", node.Stop, outcome.Stop)
	}
	if node.Verdict != outcome.Verdict {
		t.Errorf("verdict = %q, want %q", node.Verdict, outcome.Verdict)
	}
	if !reflect.DeepEqual(node.Artifacts, outcome.Artifacts) {
		t.Errorf("artifacts = %#v, want %#v", node.Artifacts, outcome.Artifacts)
	}
	if !reflect.DeepEqual(node.Calibration, outcome.Calibration) {
		t.Errorf("calibration = %#v, want %#v", node.Calibration, outcome.Calibration)
	}
}

func TestAFinishedLeafComesBackFinishedFromTheJournal(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	plans := newJobPlans(graph)
	planJobFixture(t, graph, plans, "task-1")
	prefix, document, node, _, _ := plans.lookup("task-1-n1")
	if prefix != "task-1" || node == nil {
		t.Fatalf("lookup = %q, %v", prefix, node)
	}
	plans.markClaimed(document, node, "", 0)
	claim, claimed, err := graph.Claim("task-1-n1", "worker")
	if err != nil || !claimed {
		t.Fatalf("claim leaf: claimed=%v err=%v", claimed, err)
	}
	outcome := finishedLeafOutcome()
	if err := graph.Complete(claim, outcome.Text); err != nil {
		t.Fatal(err)
	}
	plans.recordOutcome(prefix, document, node, outcome, nil)

	restarted := newJobPlans(graph)
	restored, found := restarted.get("task-1")
	if !found {
		t.Fatal("the restarted registry did not restore the job")
	}
	restoredNode := restored.graph.Node(1)
	assertFinishedLeaf(t, restoredNode, outcome)
	if restoredNode.State != plan.StateDone {
		t.Errorf("state = %q, want the durable row's %q", restoredNode.State, plan.StateDone)
	}
}

func TestAJobWithNoDurableRowsKeepsWhatTheJournalCarried(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	plans := newJobPlans(graph)
	document := &plan.Graph{Goal: "build the thing", NextID: 2, Nodes: []plan.Node{{
		ID: 1, Stage: 1, Kind: plan.KindWork, Title: "the only leaf",
	}}}
	plans.put("task-rowless", document, "task-rowless", "work/model", nil)
	outcome := finishedLeafOutcome()
	plans.recordOutcome("task-rowless", document, document.Node(1), outcome, nil)

	restarted := newJobPlans(graph)
	restored, found := restarted.get("task-rowless")
	if !found {
		t.Fatal("the restarted registry did not restore the rowless job")
	}
	assertFinishedLeaf(t, restored.graph.Node(1), outcome)
}

func TestAJournalTheStoreRefusesDoesNotCostTheEnding(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	plans := newJobPlans(graph)
	document := &plan.Graph{Goal: "build the thing", NextID: 2, Nodes: []plan.Node{{
		ID: 1, Stage: 1, Kind: plan.KindWork, Title: "the only leaf",
	}}}
	plans.put("task-closed", document, "task-closed", "work/model", nil)
	if err := graph.Close(); err != nil {
		t.Fatal(err)
	}

	outcome := finishedLeafOutcome()
	plans.recordOutcome("task-closed", document, document.Node(1), outcome, nil)
	assertFinishedLeaf(t, document.Node(1), outcome)
	if document.Node(1).State != plan.StateDone {
		t.Errorf("state = %q, want %q", document.Node(1).State, plan.StateDone)
	}
}

// A restart must not let the journal's finished word grant the revision pass
// authority over work the durable graph says another process has since claimed.
// The row owns what happened and the journal owns what the leaf measured.
func TestTheDurableRowStillOverrulesTheJournaledEnding(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	plans := newJobPlans(graph)
	planJobFixture(t, graph, plans, "task-1")
	_, document, first, _, _ := plans.lookup("task-1-n1")
	if first == nil {
		t.Fatal("the retained plan has no first leaf")
	}
	firstClaim, claimed, err := graph.Claim("task-1-n1", "worker-1")
	if err != nil || !claimed {
		t.Fatalf("claim first leaf: claimed=%v err=%v", claimed, err)
	}
	outcome := finishedLeafOutcome()
	plans.recordOutcome("task-1", document, first, outcome, nil)
	const rowSummary = "what the row says"
	if err := graph.Complete(firstClaim, rowSummary); err != nil {
		t.Fatal(err)
	}

	_, document, second, _, _ := plans.lookup("task-1-n2")
	if second == nil {
		t.Fatal("the retained plan has no second leaf")
	}
	plans.recordOutcome("task-1", document, second, outcome, nil)
	if _, claimed, err := graph.Claim("task-1-n2", "worker-2"); err != nil || !claimed {
		t.Fatalf("claim second leaf: claimed=%v err=%v", claimed, err)
	}

	restarted := newJobPlans(graph)
	restored, found := restarted.get("task-1")
	if !found {
		t.Fatal("the restarted registry did not restore the job")
	}
	restoredFirst := restored.graph.Node(1)
	if restoredFirst == nil {
		t.Fatal("the restored plan has no first leaf")
	}
	if restoredFirst.Result != rowSummary {
		t.Errorf("first result = %q, want the durable row's %q", restoredFirst.Result, rowSummary)
	}
	gotMeasurements := struct {
		Checked string
		Cost    float64
		Turns   int
		Verdict provider.Verdict
	}{restoredFirst.Checked, restoredFirst.Cost, restoredFirst.Turns, restoredFirst.Verdict}
	wantMeasurements := struct {
		Checked string
		Cost    float64
		Turns   int
		Verdict provider.Verdict
	}{outcome.Account.Summary(), outcome.Usage.Cost, outcome.Turns, outcome.Verdict}
	if !reflect.DeepEqual(gotMeasurements, wantMeasurements) {
		t.Errorf("first measurements = %#v, want the journal's %#v", gotMeasurements, wantMeasurements)
	}
	restoredSecond := restored.graph.Node(2)
	if restoredSecond == nil {
		t.Fatal("the restored plan has no second leaf")
	}
	if restoredSecond.State != plan.StateRunning {
		t.Errorf("second state = %q, want the durable row's %q", restoredSecond.State, plan.StateRunning)
	}
}

// This stands in for issue #502's count over stored events: once a leaf settles,
// its checked clause and cost must have left the process in a plan_graph payload.
func TestASettledEndingReachesTheStoreAsAnEvent(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	plans := newJobPlans(graph)
	planJobFixture(t, graph, plans, "task-1")
	_, document, node, _, _ := plans.lookup("task-1-n1")
	if node == nil {
		t.Fatal("the retained plan has no first leaf")
	}
	outcome := finishedLeafOutcome()
	plans.recordOutcome("task-1", document, node, outcome, nil)

	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var payload json.RawMessage
	for index := len(events) - 1; index >= 0; index-- {
		if events[index].Kind == store.EventPlanGraph && events[index].NodeID == "task-1" {
			payload = events[index].Payload
			break
		}
	}
	if len(payload) == 0 {
		t.Fatal("the store has no plan_graph event for task-1")
	}
	var journaled store.PlanGraph
	if err := json.Unmarshal(payload, &journaled); err != nil {
		t.Fatalf("read the stored plan_graph payload: %v", err)
	}
	carried, err := plan.Load(journaled.Graph)
	if err != nil {
		t.Fatalf("read the graph inside the stored event: %v", err)
	}
	carriedNode := carried.Node(1)
	if carriedNode == nil || carriedNode.Checked != outcome.Account.Summary() || carriedNode.Cost != outcome.Usage.Cost {
		t.Fatalf("stored ending = %#v; want checked %q and cost %v", carriedNode, outcome.Account.Summary(), outcome.Usage.Cost)
	}
}

// The bounded wake pass has no store behind its registry. Losing that journal
// must lose only a later restart's accuracy, never the in-memory leaf ending.
func TestARegistryWithNoStoreStillSettlesTheNode(t *testing.T) {
	plans := newJobPlans(nil)
	document := &plan.Graph{Goal: "build the thing", NextID: 2, Nodes: []plan.Node{{
		ID: 1, Stage: 1, Kind: plan.KindWork, Title: "the only leaf",
	}}}
	plans.put("task-memory", document, "task-memory", "work/model", nil)
	outcome := finishedLeafOutcome()
	plans.recordOutcome("task-memory", document, document.Node(1), outcome, nil)

	assertFinishedLeaf(t, document.Node(1), outcome)
	if document.Node(1).State != plan.StateDone {
		t.Errorf("state = %q, want %q", document.Node(1).State, plan.StateDone)
	}
}
