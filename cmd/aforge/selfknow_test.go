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

func TestSelfKnowledgeShrinkageWeightsByBucketEvidence(t *testing.T) {
	if got := selfKnowledgeShrunkMedian(900, 100, 8); got != 500 {
		t.Fatalf("eight-sample estimate = %d, want equal local/global weight at 500", got)
	}
	if got := selfKnowledgeShrunkMedian(900, 100, 24); got != 700 {
		t.Fatalf("24-sample estimate = %d, want 3:1 local/global weight at 700", got)
	}
}

func TestMeasureSelfKnowledgeAddsCompilerErrorBars(t *testing.T) {
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker-model"}
	measured, err := profile.Load(settings.ProfileDir, settings.Model, "linear")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < profile.MinSamples; index++ {
		measured.Add(profile.Record{
			Title: "baseline", Size: "atomic", Turns: 1, Tokens: 100,
		})
	}
	// The first measured miss is 100% on both dimensions; the following exact
	// run is a defined zero, so the bucket's mean surprise is 50%.
	measured.Add(
		profile.Record{Title: "miss", Size: "atomic", Turns: 2, Tokens: 200},
		profile.Record{Title: "exact", Size: "atomic", Turns: 1, Tokens: 100},
	)
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}

	got := measureSelfKnowledge(settings, settings.Model)
	for _, want := range []string{
		"atomic: median 100 tokens, 1 turns",
		"n=10, shrunk toward global",
		"typical miss: ±50%",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("compiler measured-cost line = %q, want %q", got, want)
		}
	}
}

// The invoice reaches the planner only from real measurements, and it reaches it
// through the same profile file everything else reads.
func TestTheMeasuredInvoiceIsEmptyUntilThereIsSomethingToInvoice(t *testing.T) {
	fresh := config.Config{ProfileDir: t.TempDir(), Model: "vendor/model"}
	if got := measuredInvoice(fresh, fresh.Model); got != "" {
		t.Fatalf("an unmeasured machine rendered a price list:\n%s", got)
	}

	settings := config.Config{ProfileDir: t.TempDir(), Model: "vendor/model"}
	measured, err := profile.Load(settings.ProfileDir, settings.Model, "linear")
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < profile.MinSamples; index++ {
		measured.Add(profile.Record{Title: "a leaf", Size: "atomic", Turns: 6,
			Tokens: 40_000, Cost: 0.05, Verdict: provider.VerdictVerifiedSuccess})
	}
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	rendered := measuredInvoice(settings, settings.Model)
	if !strings.Contains(rendered, "MEASURED HERE") || !strings.Contains(rendered, "40,000 tokens") {
		t.Fatalf("a measured machine did not render its own numbers:\n%s", rendered)
	}
}
