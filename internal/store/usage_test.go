package store

import (
	"path/filepath"
	"testing"
)

func TestTopLevelJobUsageMeansDefinedLeafSurprises(t *testing.T) {
	graph := openTestStore(t, filepath.Join(t.TempDir(), "surprise.db"))
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "deliver", Stage: 2},
		{ID: "known-a", Parent: "job", Brief: "first measured leaf", Stage: 1},
		{ID: "known-b", Parent: "job", Brief: "second measured leaf", Stage: 1},
		{ID: "unknown", Parent: "job", Brief: "cold bucket leaf", Stage: 1},
	}}, Provenance{Origin: OriginUser, Intent: "measure prediction error"}); err != nil {
		t.Fatal(err)
	}
	for _, usage := range []NodeUsage{
		{NodeID: "known-a", PromptTokens: 120},
		{NodeID: "known-b", PromptTokens: 45},
		{NodeID: "unknown", PromptTokens: 900},
	} {
		if err := graph.RecordUsage(usage); err != nil {
			t.Fatal(err)
		}
	}
	for _, surprise := range []NodeSurprise{
		{NodeID: "known-a", ActualTokens: 120, ExpectedTokens: 60, Surprise: 1},
		{NodeID: "known-b", ActualTokens: 45, ExpectedTokens: 12, Surprise: 2.75},
	} {
		if err := graph.RecordSurprise(surprise); err != nil {
			t.Fatal(err)
		}
	}

	assert := func(stage string) {
		t.Helper()
		jobs, err := graph.TopLevelJobUsage()
		if err != nil {
			t.Fatal(err)
		}
		got := jobs["job"]
		if got.SurpriseSamples != 2 || got.Surprise == nil || *got.Surprise != 1.875 {
			t.Fatalf("%s aggregate surprise = %+v, want mean 1.875 over two defined leaves", stage, got)
		}
		if got.SurpriseTokens != 165 || got.ExpectedTokens != 72 {
			t.Fatalf("%s prediction totals = actual %d expected %d, want 165/72", stage, got.SurpriseTokens, got.ExpectedTokens)
		}
	}
	assert("incremental")
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	assert("rebuilt")

	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "cold-job", Brief: "no expectation", Stage: 1,
	}}}, Provenance{Origin: OriginUser, Intent: "cold work"}); err != nil {
		t.Fatal(err)
	}
	jobs, err := graph.TopLevelJobUsage()
	if err != nil {
		t.Fatal(err)
	}
	if jobs["cold-job"].Surprise != nil {
		t.Fatalf("all-absent job surprise = %v, want absent", *jobs["cold-job"].Surprise)
	}
}
