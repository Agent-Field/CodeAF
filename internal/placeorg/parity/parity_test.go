package parity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAdjustedRandOfTheSameLabelsIsOne(t *testing.T) {
	t.Parallel()
	labels := []string{"a", "a", "b", "b"}
	if got := AdjustedRand(labels, labels); got != 1 {
		t.Fatalf("ARI %v", got)
	}
}

func TestTheSyntheticFixtureLoads(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile(filepath.Join("testdata", "labels.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Assignments map[string]ChatLevels `json:"assignments"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Assignments) != 2 {
		t.Fatalf("assignments %d", len(doc.Assignments))
	}
	var got, want []string
	for _, id := range []string{"c1", "c2"} {
		got = append(got, "leaf")
		want = append(want, "leaf")
		_ = doc.Assignments[id]
	}
	if AdjustedRand(got, want) != 1 {
		t.Fatal("identical labels did not score 1")
	}
}

func TestSavedEmbeddingsMatchTheReference(t *testing.T) {
	dir := os.Getenv("PLACEORG_REF")
	if dir == "" {
		t.Skip("PLACEORG_REF is unset; the owner's embeddings are not in this repository")
	}
	ref, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.IDs) == 0 || len(ref.Vectors) != len(ref.IDs) {
		t.Fatalf("rows %d vectors %d", len(ref.IDs), len(ref.Vectors))
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("no caller")
	}
	graph := filepath.Join(filepath.Dir(file), GraphBesideParity())
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	result, err := Run(ctx, ref, graph)
	report := filepath.Join(filepath.Dir(file), "REPORT.md")
	if result.Chats > 0 {
		if werr := WriteReport(report, result); werr != nil {
			t.Fatal(werr)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed != len(result.Invariants) || result.Passed != 11 {
		t.Fatalf("invariants %d/%d", result.Passed, len(result.Invariants))
	}
	for _, name := range LevelNames {
		t.Logf("ARI %s = %.3f", name, result.ARI[name])
		if result.ARI[name] < Floor {
			t.Errorf("%s ARI %.3f, want at least %.2f", name, result.ARI[name], Floor)
		}
	}
	t.Logf("wall %s ops %v ref %v", result.Wall.Round(time.Millisecond), result.OpCounts, result.RefOps)
}
