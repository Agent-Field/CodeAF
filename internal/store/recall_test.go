package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGraphFTSMaintainedAcrossSpliceCompleteAndFold(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "recall.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "memory", Brief: "Remember the parser", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "Repair the phosphorescent parser"}); err != nil {
		t.Fatal(err)
	}
	assertGraphFTSMatch(t, graph, "phosphorescent", "memory")

	claim := mustClaim(t, graph, "memory", "worker")
	if err := graph.Complete(claim, "The tokenizer needs sentinel handling"); err != nil {
		t.Fatal(err)
	}
	assertGraphFTSMatch(t, graph, "tokenizer", "memory")

	if err := graph.Fold("memory", "Use the amber recovery procedure", []string{"/tmp/parser-notes"}); err != nil {
		t.Fatal(err)
	}
	assertGraphFTSMatch(t, graph, "amber", "memory")
}

func TestRebuildReproducesGraphFTS(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "rebuild-recall.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "memory", Brief: "Remember the cache", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "Diagnose the heliotrope cache"}); err != nil {
		t.Fatal(err)
	}
	claim := mustClaim(t, graph, "memory", "worker")
	if err := graph.Complete(claim, "The cache key includes the tenant"); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("memory", "Keep tenant in every cache key", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := graph.db.Exec(`DELETE FROM graph_fts`); err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assertGraphFTSMatch(t, graph, "heliotrope", "memory")
	assertGraphFTSMatch(t, graph, "tenant", "memory")
}

func TestRecallRanksExactIntentAheadOfNewerSimilarMemory(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "ranking.db"))
	foldRecallFixture(t, graph, "exact", "Repair the lunar parser", "Use the exact recovery", "/workspace/parser/exact.md")
	foldRecallFixture(t, graph, "similar", "Repair parser behavior", "Lunar parsing used a broad recovery", "/workspace/parser/similar.md")

	hits, err := graph.Recall("Repair the lunar parser", []string{"/workspace/parser"}, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("Recall() returned %d hits, want 2: %+v", len(hits), hits)
	}
	if hits[0].NodeID != "exact" {
		t.Fatalf("top recall = %q, want exact intent match: %+v", hits[0].NodeID, hits)
	}
	if hits[0].Digest != "Use the exact recovery" ||
		len(hits[0].Pointers) != 1 || hits[0].Pointers[0] != "/workspace/parser/exact.md" ||
		hits[0].Age == "" || hits[0].Score <= hits[1].Score {
		t.Fatalf("exact recall metadata = %+v, runner-up = %+v", hits[0], hits[1])
	}
}

func TestRecallUsesRecencyAsTiebreak(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "recency.db"))
	foldRecallFixture(t, graph, "older", "Tune the quartz cache", "Keep the cache warm", "/workspace/cache/old.md")
	foldRecallFixture(t, graph, "newer", "Tune the quartz cache", "Keep the cache warm", "/workspace/cache/new.md")

	hits, err := graph.Recall("Tune the quartz cache", nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].NodeID != "newer" || hits[0].Score <= hits[1].Score {
		t.Fatalf("recency ranking = %+v, want newer first", hits)
	}
}

func TestRecallScopeOnlyAndHostileFTSDegrade(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "scope.db"))
	foldRecallFixture(t, graph, "scoped", "Unrelated wording", "The workspace has a hidden rule", "/workspace/atlas/notes.md")

	hits, err := graph.Recall("", []string{"workspace:/workspace/atlas"}, 3)
	if err != nil || len(hits) != 1 || hits[0].NodeID != "scoped" {
		t.Fatalf("scope recall = (%+v, %v), want scoped hit", hits, err)
	}
	hits, err = graph.Recall(strings.Repeat(`\" ) OR (`, 20), nil, 3)
	if err != nil || len(hits) != 0 {
		t.Fatalf("hostile recall = (%+v, %v), want a miss", hits, err)
	}
}

func foldRecallFixture(t *testing.T, graph *Store, id, intent, digest, pointer string) {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: id, Brief: intent, Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: intent}); err != nil {
		t.Fatal(err)
	}
	claim := mustClaim(t, graph, id, "worker")
	if err := graph.Complete(claim, digest); err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold(id, digest, []string{pointer}); err != nil {
		t.Fatal(err)
	}
}

func assertGraphFTSMatch(t *testing.T, graph *Store, term, wantID string) {
	t.Helper()
	var id string
	if err := graph.db.QueryRow(`
		SELECT node_id FROM graph_fts WHERE graph_fts MATCH ?`, term).Scan(&id); err != nil {
		t.Fatalf("graph FTS match %q: %v", term, err)
	}
	if id != wantID {
		t.Fatalf("graph FTS match %q = %q, want %q", term, id, wantID)
	}
}
