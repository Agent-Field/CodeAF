package serve_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// The preview grants from the label alone: a read's size, a diff's size and a
// chat guess change neither who would start nor who would ask.
func TestDryRunIgnoresEveryNonLabelSize(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte(dryRunYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	floor := func(tr factory.Triage) []factory.Item {
		return []factory.Item{
			{ID: 1, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready", "size:S"}, Cap: 1, Triage: tr},
			{ID: 2, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready", "size:L"}, Cap: 1, Triage: tr},
			{ID: 3, Repo: "acme/web", State: factory.StateNew, Kind: factory.KindIssue, Labels: []string{"agent-ready"}, Cap: 1, Triage: tr},
		}
	}
	run := func(tr factory.Triage) api.DryRun {
		seam := factory.FixtureSeam(now)
		seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
			return factory.DefaultRecipe(), nil, dir, nil
		}
		seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{Items: floor(tr)}, nil }
		return getDryRun(t, seam, "/v1/repos/acme/web/dryrun").DryRun
	}
	base := run(factory.Triage{})
	if !reflect.DeepEqual(base.WouldStart, []int{1}) || len(base.WouldAsk) != 2 {
		t.Fatalf("baseline dry run = %+v", base)
	}
	for _, tr := range []factory.Triage{
		{Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: now},
		{Size: "L", SizeFrom: factory.SizeFromRead, TriagedAt: now},
		{Size: "S", SizeFrom: factory.SizeFromDiff},
		{Size: "S", SizeFrom: factory.SizeFromChat},
		{Size: "S", TriagedAt: now},
	} {
		if got := run(tr); !reflect.DeepEqual(got, base) {
			t.Errorf("triage %+v moved the preview: %+v, want %+v", tr, got, base)
		}
	}
}
