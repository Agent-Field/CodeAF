package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// A measurement belongs in the file of whatever actually ran the leaf. The
// generalist's profile is what the planner's ruler is rewritten from, so a
// specialist's cost landing there would not merely be misfiled — it would teach
// the planner that ordinary leaves are enormous and stop it splitting anything.
func TestMeasurementsLandInTheirOwnWorkersFile(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "coding"})

	dir := t.TempDir()
	settings := config.Config{ProfileDir: dir, Model: "worker/model"}
	outcome := &exec.Outcome{Turns: 4, Verdict: provider.VerdictVerifiedSuccess}
	outcome.Usage.PromptTokens = 1000

	specialist := store.Node{ID: "a", Title: "fix the parser", Brief: "fix it", Subharness: "swe"}
	if _, ok := recordSingleLeaf(settings, settings.Model, specialist, outcome); !ok {
		t.Fatal("the specialist's leaf was not recorded")
	}
	ordinary := store.Node{ID: "b", Title: "read the file", Brief: "read it"}
	if _, ok := recordSingleLeaf(settings, settings.Model, ordinary, outcome); !ok {
		t.Fatal("the ordinary leaf was not recorded")
	}
	// A worker this build does not have is served by the generalist, so its
	// record describes what happened rather than what was asked for.
	stranger := store.Node{ID: "c", Title: "review it", Brief: "review", Subharness: "reviewer"}
	if _, ok := recordSingleLeaf(settings, settings.Model, stranger, outcome); !ok {
		t.Fatal("the degraded leaf was not recorded")
	}

	for _, testCase := range []struct {
		worker string
		file   string
		count  int
	}{
		{"swe", "profile-worker-model-swe.json", 1},
		{"linear", "profile-worker-model-linear.json", 2},
	} {
		if _, err := os.Stat(filepath.Join(dir, testCase.file)); err != nil {
			t.Fatalf("%s was never written: %v", testCase.file, err)
		}
		loaded, err := profile.Load(dir, settings.Model, testCase.worker)
		if err != nil {
			t.Fatal(err)
		}
		if len(loaded.Records) != testCase.count {
			t.Fatalf("%s holds %d records, want %d", testCase.file, len(loaded.Records), testCase.count)
		}
	}
}

// The flag exists for measurement runs. A build that shipped without the worker
// somebody named must still do the work — a benchmark that dies at argument
// parsing has wasted more than the measurement was worth.
func TestSubharnessFlagDegradesWithANote(t *testing.T) {
	var stderr bytes.Buffer
	if got := resolveSubharnessFlag("swe", &stderr); got != "" {
		t.Fatalf("an unregistered name resolved to %q", got)
	}
	if !strings.Contains(stderr.String(), `no subharness named "swe"`) ||
		!strings.Contains(stderr.String(), "none are registered in this build") {
		t.Fatalf("the note does not say what happened: %q", stderr.String())
	}

	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "coding"})
	stderr.Reset()
	if got := resolveSubharnessFlag("swe", &stderr); got != "swe" {
		t.Fatalf("a registered name resolved to %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("a good name still said something: %q", stderr.String())
	}
	// The baseline is never a "forced" choice: it is what happens anyway.
	for _, baseline := range []string{"", "  ", "linear"} {
		if got := resolveSubharnessFlag(baseline, &stderr); got != "" {
			t.Fatalf("resolveSubharnessFlag(%q) = %q", baseline, got)
		}
	}
	stderr.Reset()
	if got := resolveSubharnessFlag("reviewer", &stderr); got != "" {
		t.Fatalf("an unknown name resolved to %q", got)
	}
	if !strings.Contains(stderr.String(), "this build has: swe") {
		t.Fatalf("the note does not name what is available: %q", stderr.String())
	}
}

// Every leaf still resolves to a worker, and with one registered that worker is
// the one every leaf has always had.
func TestExecutorForAlwaysResolvesToSomething(t *testing.T) {
	build := leafBuild{settings: config.Config{ProfileDir: t.TempDir()}, maxTurns: 1, maxTokens: 1000}
	for _, name := range []string{"", "linear", "swe", "nobody"} {
		worker := executorFor(name, build)
		if worker == nil {
			t.Fatalf("executorFor(%q) built nothing", name)
		}
		if got := worker.Subharness(); got != exec.LinearSubharness {
			t.Fatalf("executorFor(%q) = %q, want the generalist", name, got)
		}
	}
}

// The measured line under a worker's purpose says nothing until there is enough
// of it to be honest — a median of three leaves is not a measurement.
func TestSubharnessKnowledgeWaitsForItsEvidenceGate(t *testing.T) {
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker/model"}
	measured, err := profile.Load(settings.ProfileDir, settings.Model, "swe")
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < profile.MinSamples; index++ {
		measured.Add(profile.Record{Title: "coding", Turns: index, Tokens: index * 1000,
			Cost: 0.01, Verdict: provider.VerdictVerifiedSuccess})
	}
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	if line := subharnessKnowledge(settings, settings.Model, "swe"); line != "" {
		t.Fatalf("it spoke below the gate: %q", line)
	}
	measured.Add(profile.Record{Title: "coding", Turns: 9, Tokens: 9000,
		Cost: 0.01, Verdict: provider.VerdictSemanticFailure})
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	line := subharnessKnowledge(settings, settings.Model, "swe")
	for _, want := range []string{"median", "over 8 runs", "88% succeeded"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the measured line is missing %q: %q", want, line)
		}
	}
}
