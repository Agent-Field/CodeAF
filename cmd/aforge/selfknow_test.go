package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

func TestMeasureSelfKnowledgeReportsDirectWorkModelBucket(t *testing.T) {
	settings := config.Config{ProfileDir: t.TempDir(), Model: "talking-model"}
	workingModel := "working-model"
	measured, err := profile.Load(settings.ProfileDir, workingModel, "linear")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 5; index++ {
		measured.Add(profile.Record{
			Title:  "direct task",
			Size:   profile.BucketDirect,
			Turns:  index + 1,
			Tokens: (index + 1) * 1_000,
		})
	}
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}

	got := measureSelfKnowledge(settings, workingModel)
	if !strings.Contains(got, "direct: median 3000 tokens, 3 turns; n=5") {
		t.Fatalf("measureSelfKnowledge() = %q, want the direct work-model bucket", got)
	}
	if strings.Contains(got, "atomic:") {
		t.Fatalf("measureSelfKnowledge() = %q, direct work was mislabeled atomic", got)
	}
}

func TestMeasureSelfKnowledgeReportsReflexBoundary(t *testing.T) {
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker-model"}
	measured, err := profile.Load(settings.ProfileDir, settings.Model, "linear")
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 5; index++ {
		measured.Add(profile.Record{
			Title: "reflex", Size: profile.BucketReflex,
			Turns: index, Tokens: index * 100, Cost: float64(index) / 100,
			Promoted: index > 3, Verdict: provider.VerdictUnverifiedSuccess,
		})
	}
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}

	got := measureSelfKnowledge(settings, settings.Model)
	for _, want := range []string{
		"reflex: median 300 tokens, 3 turns; n=5",
		"success=60.0%",
		"promoted=40.0%",
		"avg cost=$0.0300",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("measureSelfKnowledge() = %q, want %q", got, want)
		}
	}
}
