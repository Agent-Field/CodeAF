package resident

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestRetrospectiveKeepsRunningPastSketchLimit(t *testing.T) {
	graph := openStore(t)
	for index := 1; index <= reflectionJobLimit; index++ {
		settleRetrospectiveJob(t, graph, index)
	}

	var calls [][]JobSketch
	reconciler := New(graph, nil, nil).WithReflector(
		func(_ context.Context, jobs []JobSketch) ([]Learned, error) {
			calls = append(calls, append([]JobSketch(nil), jobs...))
			return nil, nil
		})
	reconciler.reflectOnJobs(context.Background())
	if len(calls) != 1 {
		t.Fatalf("first reflection calls = %d, want 1", len(calls))
	}
	if len(calls[0]) != reflectionJobLimit {
		t.Fatalf("first reflection sketches = %d, want %d", len(calls[0]), reflectionJobLimit)
	}
	firstWatermark, found, err := graph.RetrospectiveWatermark()
	if err != nil || !found || firstWatermark.SettledJobs != reflectionJobLimit {
		t.Fatalf("first watermark = %+v found=%v err=%v", firstWatermark, found, err)
	}

	settleRetrospectiveJob(t, graph, reflectionJobLimit+1)
	reconciler.now = func() time.Time { return firstWatermark.At.Add(reflectionInterval + time.Second) }
	reconciler.reflectOnJobs(context.Background())
	if len(calls) != 2 {
		t.Fatalf("reflection calls after 13th job = %d, want 2", len(calls))
	}
	if len(calls[1]) != reflectionJobLimit {
		t.Fatalf("second reflection sketches = %d, want capped %d", len(calls[1]), reflectionJobLimit)
	}
	if calls[1][0].Ask != "ask job 13" || calls[1][len(calls[1])-1].Ask != "ask job 2" {
		t.Fatalf("second reflection did not receive newest 12: first=%q last=%q", calls[1][0].Ask, calls[1][len(calls[1])-1].Ask)
	}
	watermark, found, err := graph.RetrospectiveWatermark()
	if err != nil || !found || watermark.SettledJobs != reflectionJobLimit+1 {
		t.Fatalf("second watermark = %+v found=%v err=%v", watermark, found, err)
	}
}

func TestRetrospectiveWatermarkSurvivesRebuildAndRestart(t *testing.T) {
	graph := openStore(t)
	for index := 1; index <= reflectionMinJobs; index++ {
		settleRetrospectiveJob(t, graph, index)
	}

	calls := 0
	reflector := func(_ context.Context, _ []JobSketch) ([]Learned, error) {
		calls++
		return nil, nil
	}
	New(graph, nil, nil).WithReflector(reflector).reflectOnJobs(context.Background())
	before, found, err := graph.RetrospectiveWatermark()
	if err != nil || !found {
		t.Fatalf("watermark before rebuild = %+v found=%v err=%v", before, found, err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	after, found, err := graph.RetrospectiveWatermark()
	if err != nil || !found || after != before {
		t.Fatalf("watermark after rebuild = %+v, want %+v (found=%v err=%v)", after, before, found, err)
	}

	restarted := New(graph, nil, nil).WithReflector(reflector)
	restarted.now = func() time.Time { return after.At.Add(time.Minute) }
	restarted.reflectOnJobs(context.Background())
	if calls != 1 {
		t.Fatalf("restart reflected settled history again: calls=%d", calls)
	}
}

func TestRetrospectiveSketchCarriesJobCost(t *testing.T) {
	graph := openStore(t)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "cost-root", Brief: "deliver", Stage: 2},
		{ID: "cost-child", Parent: "cost-root", Brief: "research", Stage: 1},
	}}, store.Provenance{Origin: store.OriginUser, Intent: "measure this job"}); err != nil {
		t.Fatalf("splice cost job: %v", err)
	}
	completeRetrospectiveNode(t, graph, "cost-child", "research complete")
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "cost-child", PromptTokens: 100, CompletionTokens: 20, Cost: 0.01}); err != nil {
		t.Fatalf("record child usage: %v", err)
	}
	completeRetrospectiveNode(t, graph, "cost-root", "delivery complete")
	if err := graph.RecordUsage(store.NodeUsage{NodeID: "cost-root", PromptTokens: 40, CompletionTokens: 5, Cost: 0.0025}); err != nil {
		t.Fatalf("record root usage: %v", err)
	}

	sketches, settled := New(graph, nil, nil).settledJobSketches(time.Now())
	if settled != 1 || len(sketches) != 1 {
		t.Fatalf("settled/sketches = %d/%d, want 1/1", settled, len(sketches))
	}
	job := sketches[0]
	if job.NodeCount != 2 || job.PromptTokens != 140 || job.CompletionTokens != 25 || job.Cost != 0.0125 {
		t.Fatalf("job cost sketch = %+v", job)
	}
	if got, want := job.CostSummary(), "2 nodes · 165 tok · $0.0125"; got != want {
		t.Fatalf("CostSummary() = %q, want %q", got, want)
	}
}

func settleRetrospectiveJob(t *testing.T, graph *store.Store, index int) {
	t.Helper()
	id := fmt.Sprintf("job-%02d", index)
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: fmt.Sprintf("job %d", index), Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: fmt.Sprintf("ask job %d", index)}); err != nil {
		t.Fatalf("splice job %d: %v", index, err)
	}
	completeRetrospectiveNode(t, graph, id, fmt.Sprintf("outcome %d", index))
}

func completeRetrospectiveNode(t *testing.T, graph *store.Store, id, summary string) {
	t.Helper()
	claim, won, err := graph.Claim(id, "retrospective-test")
	if err != nil || !won {
		t.Fatalf("claim %s: won=%v err=%v", id, won, err)
	}
	if err := graph.Complete(claim, summary); err != nil {
		t.Fatalf("complete %s: %v", id, err)
	}
}
