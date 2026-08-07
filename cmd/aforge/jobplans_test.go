package main

import (
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func openPlanStore(t *testing.T) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	return graph
}

func surpriseNodes(t *testing.T, graph *store.Store) []string {
	t.Helper()
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var nodes []string
	for _, event := range events {
		if event.Kind == store.EventSurpriseRecorded {
			nodes = append(nodes, event.NodeID)
		}
	}
	return nodes
}

func measuredRecords(planIDs ...int) []landedProfileRecord {
	expected := 1000
	surprise := 2.0
	records := make([]landedProfileRecord, 0, len(planIDs))
	for _, id := range planIDs {
		records = append(records, landedProfileRecord{planID: id, record: profile.Record{
			Title: "measured", Tokens: 2000, ExpectedTokens: &expected, Surprise: &surprise,
		}})
	}
	return records
}

// The id namespace a job's nodes were minted under is the registry's own key,
// and every attempt to re-derive it from the landed node's id got it wrong.
//
// A planned root IS the prefix — "task-1", with no "-n" anywhere in it — so the
// guard that looked for one never matched and plan recalibration simply never
// ran for a chat job. And the branch that did fire was the one case where the
// slice is wrong: an overrun repair root "task-1-n2-x1" sliced back to "task-1"
// and wrote this repair's surprises onto the unrelated sibling leaves of the
// job it was repairing, which exist, so the store accepted every one.
func TestTakeIfRootNamesTheNamespaceItsOwnNodesWereMintedUnder(t *testing.T) {
	graph := openPlanStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1", Brief: "the job", Stage: 2},
		{ID: "task-1-n1", Parent: "task-1", Brief: "first", Stage: 1},
		{ID: "task-1-n2", Parent: "task-1", Brief: "second", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "the job"}); err != nil {
		t.Fatal(err)
	}
	// The repair subtree an overrun splices under the same job.
	if err := graph.Splice("task-1", store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-1-n2-x1", Brief: "finish the remainder", Stage: 3},
		{ID: "task-1-n2-x1-n1", Parent: "task-1-n2-x1", Brief: "the rest", Stage: 3},
	}}, store.Provenance{Origin: store.OriginSelf, Intent: "finish the remainder"}); err != nil {
		t.Fatal(err)
	}

	plans := &jobPlans{graphs: map[string]plannedJob{}}
	planned := &plan.Graph{Goal: "the job", Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "first"},
		{ID: 2, Stage: 1, Kind: plan.KindWork, Title: "second"},
	}}
	repair := &plan.Graph{Goal: "finish the remainder", Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "the rest"},
	}}
	plans.put("task-1", planned, "task-1", "work/model", nil)
	plans.put("task-1-n2-x1", repair, "task-1-n2-x1", "work/model", nil)

	// A planned root: recalibration has to run at all, and against its own key.
	landed, prefix := plans.takeIfRoot("task-1")
	if landed == nil {
		t.Fatal("a planned job's own root did not land its graph")
	}
	if prefix != "task-1" {
		t.Fatalf("prefix = %q, want the registry key the nodes were minted under", prefix)
	}
	recordPlanSurprises(graph, prefix, measuredRecords(1, 2))

	// A repair root: its surprises are its own, and must not reach the job's
	// original leaves.
	repaired, repairPrefix := plans.takeIfRoot("task-1-n2-x1")
	if repaired == nil {
		t.Fatal("a repair root did not land its own graph")
	}
	if repairPrefix != "task-1-n2-x1" {
		t.Fatalf("repair prefix = %q, want its own namespace", repairPrefix)
	}
	recordPlanSurprises(graph, repairPrefix, measuredRecords(1))

	got := surpriseNodes(t, graph)
	want := map[string]bool{"task-1-n1": true, "task-1-n2": true, "task-1-n2-x1-n1": true}
	if len(got) != len(want) {
		t.Fatalf("surprises landed on %v, want exactly %v", got, want)
	}
	for _, id := range got {
		if !want[id] {
			t.Fatalf("a surprise was written onto %q, which this job never ran", id)
		}
	}
}

// A restart used to forget every in-flight plan, and the comment defending it
// said that cost only telemetry. It stopped being true when result-driven
// revision began reading the same map: a job planned last night is still
// running this morning and its remaining steps are still editable.
func TestAPlanSurvivesTheProcessThatMadeIt(t *testing.T) {
	graph := openPlanStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-9", Brief: "the job", Stage: 3},
		{ID: "task-9-n1", Parent: "task-9", Brief: "gather", Stage: 1},
		{ID: "task-9-n2", Parent: "task-9", Brief: "write", Stage: 2},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "the job"}); err != nil {
		t.Fatal(err)
	}
	before := newJobPlans(graph)
	planned := &plan.Graph{Goal: "the job", NextID: 4, Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Gather", Brief: "gather",
			Contract: "check both sources", State: plan.StatePending},
		{ID: 2, Stage: 2, Kind: plan.KindWork, Title: "Write", Brief: "write",
			Needs: []int{1}, State: plan.StatePending},
		{ID: 3, Stage: 3, Kind: plan.KindSynthesis, Title: "Synthesis",
			Needs: []int{2}, State: plan.StatePending},
	}}
	before.put("task-9", planned, "task-9", "work/model", nil)

	// The first leaf runs and lands, in the durable graph, as it would have.
	claim, won, err := graph.Claim("task-9-n1", "worker")
	if err != nil || !won {
		t.Fatalf("claim: won=%t err=%v", won, err)
	}
	if err := graph.Complete(claim, "the v2 endpoints are all 410 Gone"); err != nil {
		t.Fatal(err)
	}

	// The restart: a brand new registry over the same store, holding nothing.
	after := newJobPlans(graph)
	prefix, restored, node, model, client := after.lookup("task-9-n2")
	if restored == nil {
		t.Fatal("the plan did not survive the restart; the job's remainder is no longer editable")
	}
	if prefix != "task-9" {
		t.Fatalf("prefix = %q, want the namespace the nodes were minted under", prefix)
	}
	if node == nil || node.ID != 2 || node.Title != "Write" {
		t.Fatalf("plan node = %+v, want the second leaf", node)
	}
	if model != "work/model" {
		t.Fatalf("model = %q, want the model the job was sized for", model)
	}
	if client != nil {
		t.Fatal("a rehydrated job carried a client from a process that no longer exists")
	}
	if contract := planNodeContract(restored.Node(1)); contract != "check both sources" {
		t.Fatalf("working method lost across the restart: %q", contract)
	}
	// Structure comes from the journal; what has happened comes from the store.
	// A rehydrated plan that thought everything was still pending would hand the
	// sentinel a licence to rewrite work that has already run.
	if first := restored.Node(1); first == nil || first.State != plan.StateDone {
		t.Fatalf("landed node state = %+v, want done", first)
	} else if first.Result != "the v2 endpoints are all 410 Gone" {
		t.Fatalf("landed node result = %q, want what the leaf actually said", first.Result)
	}
	if second := restored.Node(2); second == nil || second.State != plan.StatePending {
		t.Fatalf("unstarted node state = %+v, want pending", second)
	}
	// And the redirect path finds it, so the user's words reach the plan rather
	// than a receipt claiming it needed no changes.
	if _, ok := after.get("task-9"); !ok {
		t.Fatal("a redirect after the restart still finds no plan, and answers with a receipt saying it read one")
	}
}

// isJobNode is the separator test the whole id scheme rests on: "task-14" is
// not a prefix of "task-142-n1" in any sense the graph means, however much it
// looks like one to strings.HasPrefix. Without it a landed leaf of one job
// counted another job's pending nodes and spent a full sentinel pass — under
// the registry lock — on a remainder it had no business reading.
func TestIsJobNodeRefusesTheNeighbouringJob(t *testing.T) {
	for _, row := range []struct {
		id, prefix string
		want       bool
	}{
		{"task-14", "task-14", true},
		{"task-14-n1", "task-14", true},
		{"task-142", "task-14", false},
		{"task-142-n1", "task-14", false},
		{"task-14-n2-x1-n1", "task-14", true},
	} {
		if got := isJobNode(row.id, row.prefix); got != row.want {
			t.Errorf("isJobNode(%q, %q) = %t, want %t", row.id, row.prefix, got, row.want)
		}
	}
}
