package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if _, ok := recordSingleLeaf(settings, settings.Model, specialist, outcome, ""); !ok {
		t.Fatal("the specialist's leaf was not recorded")
	}
	ordinary := store.Node{ID: "b", Title: "read the file", Brief: "read it"}
	if _, ok := recordSingleLeaf(settings, settings.Model, ordinary, outcome, ""); !ok {
		t.Fatal("the ordinary leaf was not recorded")
	}
	// A worker this build does not have is served by the generalist, so its
	// record describes what happened rather than what was asked for.
	stranger := store.Node{ID: "c", Title: "review it", Brief: "review", Subharness: "reviewer"}
	if _, ok := recordSingleLeaf(settings, settings.Model, stranger, outcome, ""); !ok {
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
	// Only an empty flag is "nobody forced anything". Naming the generalist is
	// a forcing like any other and comes back as the generalist's own name —
	// without that, the arm of a measurement that holds the default worker
	// fixed cannot be expressed at all.
	for _, baseline := range []string{"", "  "} {
		if got := resolveSubharnessFlag(baseline, &stderr); got != "" {
			t.Fatalf("resolveSubharnessFlag(%q) = %q, want no forcing", baseline, got)
		}
	}
	for _, named := range []string{"linear", " linear "} {
		if got := resolveSubharnessFlag(named, &stderr); got != exec.LinearSubharness {
			t.Fatalf("resolveSubharnessFlag(%q) = %q, want the generalist forced", named, got)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("naming the generalist said something: %q", stderr.String())
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

// The other arm of a worker measurement: `aforge do --subharness linear` on an
// ask the compiler reads as coding must actually be measured on the generalist.
//
// It could not be, and the reason was one representation: the generalist and
// "nobody said" were both the empty string, so the flag that named it was
// indistinguishable from a flag nobody passed, and the run went to the
// specialist the compiler had chosen — the exact variable the benchmark was
// holding fixed. The name is the fix, and this is the assertion that the name
// reaches all the way to what ran.
func TestSubharnessFlagForcesTheGeneralistOnACodingAsk(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(sweInfo())

	script := newScriptedBrain(t)
	// The compiler reads this ask the way it reads any coding ask: one job for
	// the coding pipeline. Nothing about the ask is in dispute; the flag is.
	script.compileSubharness = exec.SWESubharness
	script.gatePasses = true
	defer script.close()

	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task:       "fix the failing parser tests and add a regression test",
		timeout:    60 * time.Second,
		asJSON:     true,
		subharness: "linear",
		stdout:     &stdout, stderr: &stderr,
		newClient: script.client,
	}); err != nil {
		t.Fatalf("the errand did not settle cleanly: %v\nstdout:\n%s\nstderr:\n%s",
			err, stdout.String(), stderr.String())
	}

	var outcome struct {
		Subharness string `json:"subharness"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("stdout is not the machine-readable outcome: %v\n%s", err, stdout.String())
	}
	if outcome.Subharness != exec.LinearSubharness {
		t.Fatalf("the errand ran on %q despite --subharness linear", outcome.Subharness)
	}
	// The generalist did not merely get the credit: it did the work. The
	// scripted worker only ever answers the generalist's leaf loop, so a draft
	// is proof that no coding pipeline was constructed for this leaf.
	if script.count("draft") == 0 {
		t.Fatalf("no generalist leaf ever ran\nstderr:\n%s", stderr.String())
	}
}
