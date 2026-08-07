package resident

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func TestTerritoryClusteringComputesWorkspaceScopeAndContinuitySignals(t *testing.T) {
	jobs := []store.TerritoryJob{
		{Node: store.Node{ID: "workspace-a", CreatedSeq: 1}, Workspace: "/workspace/atlas"},
		{Node: store.Node{ID: "workspace-b", CreatedSeq: 2}, Workspace: "/workspace/atlas", DominantScope: "domain:parser"},
		{Node: store.Node{ID: "scope-c", CreatedSeq: 3}, DominantScope: "domain:parser"},
		{Node: store.Node{ID: "continuity-d", CreatedSeq: 4}, Continuity: []string{"scope-c"}},
		{Node: store.Node{ID: "generic-e", CreatedSeq: 5}, DominantScope: "user"},
		{Node: store.Node{ID: "generic-f", CreatedSeq: 6}, DominantScope: "user"},
	}

	clusters := clusterTerritoryJobs(jobs)
	sizes := make([]int, len(clusters))
	for index := range clusters {
		sizes[index] = len(clusters[index])
	}
	if want := []int{4, 1, 1}; !reflect.DeepEqual(sizes, want) {
		t.Fatalf("cluster sizes = %v, want %v", sizes, want)
	}
	if got, want := territoryTitle(clusters[0]), "Parser work"; got != want {
		t.Fatalf("territory title = %q, want %q", got, want)
	}
}

func TestTerritoryFormationRequiresFourOldJobsAndTakesOneActionPerReflection(t *testing.T) {
	graph := openStore(t)
	for index := 1; index <= 3; index++ {
		foldResidentTerritoryJob(t, graph, fmt.Sprintf("atlas-%d", index), "domain:atlas", "atlas")
	}

	digestCalls := make([][]TerritoryDigestJob, 0)
	reconciler := New(graph, nil, nil).
		WithReflector(func(_ context.Context, _ []JobSketch) ([]Learned, error) {
			return nil, nil
		}).
		WithTerritoryDigester(func(_ context.Context, _ string, jobs []TerritoryDigestJob, _ string) (string, error) {
			digestCalls = append(digestCalls, append([]TerritoryDigestJob(nil), jobs...))
			return fmt.Sprintf("%d jobs packed", len(jobs)), nil
		})
	reconciler.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	reconciler.reflectOnJobs(context.Background())
	if len(digestCalls) != 0 {
		t.Fatalf("three-job cluster formed a territory: calls=%d", len(digestCalls))
	}

	foldResidentTerritoryJob(t, graph, "atlas-4", "domain:atlas", "atlas")
	for index := 1; index <= 4; index++ {
		foldResidentTerritoryJob(t, graph, fmt.Sprintf("quartz-%d", index), "domain:quartz", "quartz")
	}
	reconciler.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	reconciler.reflectOnJobs(context.Background())
	if len(digestCalls) != 1 || len(digestCalls[0]) != territoryMinJobs {
		t.Fatalf("one reflection digest calls = %d jobs=%v, want one four-job action",
			len(digestCalls), digestJobCounts(digestCalls))
	}

	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	territoryCount, nestedMembers, rootJobs := 0, 0, 0
	territoryID := ""
	for _, node := range nodes {
		if node.Group == store.TerritoryGroup {
			territoryCount++
			territoryID = node.ID
		}
	}
	for _, node := range nodes {
		if node.FoldRoot && node.Group != store.TerritoryGroup {
			if node.Parent == territoryID {
				nestedMembers++
			}
			if node.Parent == store.RootID {
				rootJobs++
			}
		}
	}
	if territoryCount != 1 || nestedMembers != 4 || rootJobs != 4 {
		t.Fatalf("one-action formation = territories:%d nested:%d root-jobs:%d",
			territoryCount, nestedMembers, rootJobs)
	}
}

func TestTerritoryGrowthAddsOneMatchingSettledJobAndRefreshesDigest(t *testing.T) {
	graph := openStore(t)
	for index := 1; index <= 4; index++ {
		foldResidentTerritoryJob(t, graph, fmt.Sprintf("growth-%d", index), "domain:growth", "growth")
	}

	digestCalls := make([][]TerritoryDigestJob, 0)
	reconciler := New(graph, nil, nil).
		WithReflector(func(_ context.Context, _ []JobSketch) ([]Learned, error) {
			return nil, nil
		}).
		WithTerritoryDigester(func(_ context.Context, _ string, jobs []TerritoryDigestJob, _ string) (string, error) {
			digestCalls = append(digestCalls, append([]TerritoryDigestJob(nil), jobs...))
			return fmt.Sprintf("digest for %d jobs", len(jobs)), nil
		})
	reconciler.now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	reconciler.reflectOnJobs(context.Background())
	territory := onlyTerritory(t, graph)

	foldResidentTerritoryJob(t, graph, "growth-5", "domain:growth", "growth")
	reconciler.now = func() time.Time { return time.Now().Add(3 * time.Hour) }
	reconciler.reflectOnJobs(context.Background())
	if got, want := digestJobCounts(digestCalls), []int{4, 5}; !reflect.DeepEqual(got, want) {
		t.Fatalf("territory digest job counts = %v, want %v", got, want)
	}
	member, ok, err := graph.Node("growth-5")
	if err != nil || !ok || member.Parent != territory.ID || !member.FoldRoot {
		t.Fatalf("grown member = %+v found=%t err=%v", member, ok, err)
	}
	refreshed, ok, err := graph.Node(territory.ID)
	if err != nil || !ok || refreshed.FoldDigest != "digest for 5 jobs" {
		t.Fatalf("refreshed territory = %+v found=%t err=%v", refreshed, ok, err)
	}
	active, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 || active[0].ID != store.RootID || active[1].ID != territory.ID {
		t.Fatalf("active nodes after growth = %+v", active)
	}
}

func foldResidentTerritoryJob(t *testing.T, graph *store.Store, id, scope, workspace string) {
	t.Helper()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: id, Brief: "complete " + id, Title: id, Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, Intent: "complete " + id}); err != nil {
		t.Fatalf("splice %s: %v", id, err)
	}
	completeRetrospectiveNode(t, graph, id, "learned from "+id)
	if err := graph.Fold(id, "learned from "+id,
		[]string{filepath.Join("/workspace", workspace, id+".md")}); err != nil {
		t.Fatalf("fold %s: %v", id, err)
	}
	if _, err := graph.RecordFact(id, scope, store.FactLesson, "lesson from "+id); err != nil {
		t.Fatalf("record fact for %s: %v", id, err)
	}
}

func onlyTerritory(t *testing.T, graph *store.Store) store.Node {
	t.Helper()
	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	var territories []store.Node
	for _, node := range nodes {
		if node.Group == store.TerritoryGroup {
			territories = append(territories, node)
		}
	}
	if len(territories) != 1 {
		t.Fatalf("territories = %+v, want one", territories)
	}
	return territories[0]
}

func digestJobCounts(calls [][]TerritoryDigestJob) []int {
	counts := make([]int, len(calls))
	for index := range calls {
		counts[index] = len(calls[index])
	}
	return counts
}
