package factory_test

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// Contract P3: A loaded file's first decision records the issue fallback
// through Item.Note and leaves pull request and CI jobs without that line.
func TestPipelineDecisionNotesTheIssueFallbackOnlyOnce(t *testing.T) {
	const want = "This repository has no .codeaf/factory.yaml on its main branch, so this job runs the built-in steps."
	file := pipeline.Defaults()
	file.Source, file.From = pipeline.SourceDefaults, factory.FromDefault
	at := time.Date(2026, 11, 27, 14, 0, 0, 0, time.UTC)
	for _, kind := range []factory.Kind{factory.KindIssue, factory.KindPR, factory.KindCI} {
		t.Run(string(kind), func(t *testing.T) {
			it := factory.Item{Kind: kind}
			file.DecideJobPipeline(&it, at, "")
			file.DecideJobPipeline(&it, at.Add(time.Minute), "")
			it.ForgetPipeline()
			file.DecideJobPipeline(&it, at.Add(2*time.Minute), "")
			if !it.PipelineIsDecided() {
				t.Fatal("pipeline file was never decided")
			}
			if kind == factory.KindIssue {
				if len(it.Activity) != 1 || it.Activity[0].What != want || !it.Activity[0].At.Equal(at) {
					t.Fatalf("activity = %+v, want the single fallback note at %v", it.Activity, at)
				}
			} else if len(it.Activity) != 0 {
				t.Fatalf("activity = %+v, want none", it.Activity)
			}
		})
	}
}
