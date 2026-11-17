package factory_test

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestFileConditionsUseKnownFilesBeforeArea(t *testing.T) {
	for _, tc := range []struct {
		name string
		item factory.Item
		when string
		want bool
	}{
		{"unknown auth", factory.Item{}, "touches auth", true},
		{"unknown ui", factory.Item{}, "has ui", true},
		{"area fallback", factory.Item{Triage: factory.Triage{Area: "AUTH"}}, "touches auth", true},
		{"known area misses", factory.Item{Triage: factory.Triage{Area: "docs"}}, "touches auth", false},
		{"forge files", factory.Item{Files: []factory.FileChange{{Path: "src/pages/a.tsx"}}}, "has ui", true},
		{"files override area", factory.Item{Triage: factory.Triage{Area: "auth"}, Change: &factory.Change{Files: []factory.ChangedFile{{Path: "docs/a.md"}}}}, "touches auth", false},
		{"measured change overrides forge", factory.Item{Files: []factory.FileChange{{Path: "docs/a.md"}}, Change: &factory.Change{Files: []factory.ChangedFile{{Path: "auth.go"}}}}, "touches auth", true},
		{"planned paths are not changes", factory.Item{PlannedFiles: []string{"docs/a.md"}}, "touches auth", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := factory.Fits(factory.Stage{When: tc.when}, tc.item); got != tc.want {
				t.Fatalf("Fits = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPRPipelineFileConditionsStayPendingInDeskPreview(t *testing.T) {
	for _, when := range []string{"touches auth", "has ui"} {
		f, err := pipeline.Parse("pipeline:\n  pr: [inspect]\nsteps:\n  inspect:\n    prompt: inspect the change\n    when: " + when + "\n")
		if err != nil || len(f.Problems) != 0 {
			t.Fatalf("parse: %v, %+v", err, f.Problems)
		}
		stages := f.Recipe().ByKind[factory.KindPR]
		if len(stages) != 1 || stages[0].When != when {
			t.Fatalf("PR stages: %+v", stages)
		}
		it := factory.Item{Kind: factory.KindPR, Stages: stages}
		if !factory.Fits(stages[0], it) {
			t.Fatal("unknown files treated as a nonmatch")
		}
		preview := api.FromItem(it)
		if len(preview.Steps) != 1 || preview.Steps[0].State != api.StepPending || preview.Steps[0].When != when {
			t.Fatalf("unknown condition shown as skipped: %+v", preview.Steps)
		}
	}
}
