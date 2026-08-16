package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTerritoryJobsComputesWorkspaceScopeAndContinuitySignals(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "territory-signals.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "signal-a", Brief: "first parser job", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "first parser job"}); err != nil {
		t.Fatal(err)
	}
	first := mustClaim(t, graph, "signal-a", "worker")
	if err := graph.Complete(first, "first result"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("signal-a", "first result", []string{"/workspace/shared/a.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("signal-a", "domain:parser", FactLesson, "first parser lesson"); err != nil {
		t.Fatal(err)
	}

	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "signal-b", Brief: "continued parser job", Stage: 1,
		Needs: []Need{{NodeID: "signal-a", Kind: FeedsInto}},
	}}}, Provenance{Origin: OriginUser, Intent: "continued parser job"}); err != nil {
		t.Fatal(err)
	}
	second := mustClaim(t, graph, "signal-b", "worker")
	if err := graph.Complete(second, "second result"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("signal-b", "second result", []string{"/workspace/shared/b.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RecordFact("signal-b", "domain:parser", FactLesson, "second parser lesson"); err != nil {
		t.Fatal(err)
	}

	jobs, err := graph.TerritoryJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("TerritoryJobs() = %+v, want two jobs", jobs)
	}
	byID := make(map[string]TerritoryJob)
	for _, job := range jobs {
		byID[job.Node.ID] = job
	}
	for _, id := range []string{"signal-a", "signal-b"} {
		job := byID[id]
		if job.Workspace != "/workspace/shared" || job.DominantScope != "domain:parser" {
			t.Fatalf("signals for %s = workspace:%q scope:%q", id, job.Workspace, job.DominantScope)
		}
	}
	if got, want := byID["signal-a"].Continuity, []string{"signal-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("signal-a continuity = %v, want %v", got, want)
	}
	if got, want := byID["signal-b"].Continuity, []string{"signal-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("signal-b continuity = %v, want %v", got, want)
	}
}

func TestTerritoryReparentSurvivesRebuild(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "territory-rebuild.db"))
	memberIDs := foldedTerritoryMembers(t, graph, "rebuild", 4)
	if err := graph.FormTerritory("territory-rebuild", "Parser work",
		"4 jobs learned parser recovery; assets are under /workspace/parser",
		[]string{"node:rebuild-1", "/workspace/parser/report.md"}, memberIDs); err != nil {
		t.Fatalf("FormTerritory: %v", err)
	}

	before, err := graph.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range memberIDs {
		node, ok, err := graph.Node(id)
		if err != nil || !ok || node.Parent != "territory-rebuild" || !node.FoldRoot {
			t.Fatalf("member %q after re-parent = %+v found=%t err=%v", id, node, ok, err)
		}
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	after, err := graph.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("territory changed during rebuild\nbefore: %#v\nafter:  %#v", before, after)
	}
	if !reflect.DeepEqual(eventsAfter, eventsBefore) {
		t.Fatal("Rebuild changed the territory journal")
	}
}

func TestTerritoryActiveNodesCompactsMemberFolds(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "territory-active.db"))
	memberIDs := foldedTerritoryMembers(t, graph, "active", 4)
	if err := graph.FormTerritory("territory-active", "Atlas work", "4 jobs mapped Atlas",
		[]string{"node:active-1"}, memberIDs); err != nil {
		t.Fatalf("FormTerritory: %v", err)
	}

	active, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := nodeIDs(active), []string{RootID, "territory-active"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ActiveNodes() = %v, want %v", got, want)
	}
	for _, id := range memberIDs {
		member, ok, err := graph.Node(id)
		if err != nil || !ok || !member.FoldRoot {
			t.Fatalf("hidden member %q lost fold identity: %+v found=%t err=%v", id, member, ok, err)
		}
	}
}

func TestTerritoryRefusesMemberThatIsNotFoldRoot(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "territory-invalid.db"))
	memberIDs := foldedTerritoryMembers(t, graph, "valid", 3)
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "plain-done", Brief: "plain terminal job", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "plain terminal job"}); err != nil {
		t.Fatal(err)
	}
	claim := mustClaim(t, graph, "plain-done", "worker")
	if err := graph.Complete(claim, "done but not folded"); err != nil {
		t.Fatal(err)
	}
	memberIDs = append(memberIDs, "plain-done")
	err := graph.FormTerritory("territory-invalid", "Invalid work", "must fail", nil, memberIDs)
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("FormTerritory(non-fold-root) error = %v, want ErrInvalid", err)
	}
	if _, found, readErr := graph.Node("territory-invalid"); readErr != nil || found {
		t.Fatalf("invalid territory persisted: found=%t err=%v", found, readErr)
	}
}

func TestRecallMemberHitAlsoSurfacesTerritory(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "territory-recall.db"))
	memberIDs := []string{"memory-1", "memory-2", "memory-3", "memory-4"}
	for index, id := range memberIDs {
		digest := "ordinary archived result"
		if index == 0 {
			digest = "heliotrope recovery uses a bounded retry"
		}
		foldRecallFixture(t, graph, id, "Archive parser work", digest,
			"/workspace/parser/"+id+".md")
	}
	if err := graph.FormTerritory("territory-memory", "Parser work",
		"4 jobs established cartography recovery patterns and durable assets",
		[]string{"node:memory-1", "/workspace/parser"}, memberIDs); err != nil {
		t.Fatal(err)
	}

	hits, err := graph.Recall("heliotrope", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	foundMember, foundTerritory := false, false
	for _, hit := range hits {
		foundMember = foundMember || hit.NodeID == "memory-1"
		foundTerritory = foundTerritory || hit.NodeID == "territory-memory"
	}
	if !foundMember || !foundTerritory {
		t.Fatalf("member recall did not surface its territory: %+v", hits)
	}

	territoryHits, err := graph.Recall("cartography", nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(territoryHits) == 0 || territoryHits[0].NodeID != "territory-memory" {
		t.Fatalf("territory digest was not indexed like an ordinary fold: %+v", territoryHits)
	}

	limited, err := graph.Recall("heliotrope", nil, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(limited) != 1 || limited[0].NodeID != "territory-memory" {
		t.Fatalf("limited member recall did not prioritize its territory line: %+v", limited)
	}
}

func TestSingleJobFoldRetainsLegacyBytes(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "legacy-fold.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "legacy-root", Brief: "deliver legacy result", Stage: 2},
		{ID: "legacy-child", Parent: "legacy-root", Brief: "prepare result", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "deliver legacy result"}); err != nil {
		t.Fatal(err)
	}
	child := mustClaim(t, graph, "legacy-child", "worker")
	if err := graph.Complete(child, "child complete"); err != nil {
		t.Fatal(err)
	}
	root := mustClaim(t, graph, "legacy-root", "worker")
	if err := graph.Complete(root, "root complete"); err != nil {
		t.Fatal(err)
	}
	before, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}

	pointers := []string{"/workspace/legacy/report.md"}
	if err := graph.Fold("legacy-root", "legacy compact digest", pointers); err != nil {
		t.Fatal(err)
	}
	events, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	foldEvent := events[len(events)-1]
	if got, want := string(foldEvent.Payload),
		`{"digest":"legacy compact digest","pointers":["/workspace/legacy/report.md"]}`; got != want {
		t.Fatalf("fold event bytes = %s, want %s", got, want)
	}

	// Project the pre-change Fold SQL onto the pre-fold snapshot. With no nested
	// fold roots, the new folds-of-folds CASE must produce identical node bytes.
	expected := append([]Node(nil), before...)
	for index := range expected {
		if expected[index].ID != "legacy-root" && expected[index].ID != "legacy-child" {
			continue
		}
		expected[index].Folded = true
		expected[index].FoldRoot = expected[index].ID == "legacy-root"
		expected[index].UpdatedSeq = foldEvent.Seq
		if expected[index].ID == "legacy-root" {
			expected[index].FoldDigest = "legacy compact digest"
			expected[index].FoldPointers = append([]string(nil), pointers...)
		}
	}
	actual, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	expectedBytes, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	actualBytes, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actualBytes, expectedBytes) {
		t.Fatalf("single-job fold bytes changed\nactual:   %s\nexpected: %s", actualBytes, expectedBytes)
	}
}

func foldedTerritoryMembers(t *testing.T, graph *Store, prefix string, count int) []string {
	t.Helper()
	ids := make([]string, count)
	for index := 0; index < count; index++ {
		id := prefix + "-" + string(rune('1'+index))
		ids[index] = id
		foldRecallFixture(t, graph, id, "Work on "+id, "Learned from "+id,
			"/workspace/"+prefix+"/"+id+".md")
	}
	return ids
}
