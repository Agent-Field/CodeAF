package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/profile"
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
