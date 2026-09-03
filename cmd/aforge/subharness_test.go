package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// There is one profile file because there is one worker, and every measurement
// belongs in it. A record filed anywhere else would be a record no ruler is
// rewritten from — the planner reads this one file and nothing else.
func TestEveryMeasurementLandsInTheOneWorkersFile(t *testing.T) {
	dir := t.TempDir()
	settings := config.Config{ProfileDir: dir, Model: "worker/model"}
	outcome := &exec.Outcome{Turns: 4, Verdict: provider.VerdictVerifiedSuccess}
	outcome.Usage.PromptTokens = 1000

	ordinary := store.Node{ID: "a", Title: "read the file", Brief: "read it"}
	if _, ok := recordSingleLeaf(settings, settings.Model, ordinary, outcome); !ok {
		t.Fatal("the ordinary leaf was not recorded")
	}
	// A row an older graph wrote, naming a worker this build does not have. It
	// ran on linear, so its record describes linear — a record has to say what
	// happened rather than what was asked for.
	stored := store.Node{ID: "b", Title: "review it", Brief: "review", Subharness: "retired-worker"}
	if _, ok := recordSingleLeaf(settings, settings.Model, stored, outcome); !ok {
		t.Fatal("the leaf from an older graph was not recorded")
	}

	loaded, err := profile.Load(dir, settings.Model, exec.LinearSubharness)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Records) != 2 {
		t.Fatalf("the profile holds %d records, want both of them", len(loaded.Records))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "profile-") {
			continue
		}
		if name != "profile-worker-model-linear.json" {
			t.Fatalf("a second profile file was written: %s", name)
		}
	}
}

// Every leaf still resolves to a worker, whatever name it arrives carrying.
func TestExecutorForAlwaysResolvesToSomething(t *testing.T) {
	build := leafBuild{settings: config.Config{ProfileDir: t.TempDir()}, maxTurns: 1, maxTokens: 1000}
	for _, name := range []string{"", "linear", "retired-worker", "nobody"} {
		worker := executorFor(name, build)
		if worker == nil {
			t.Fatalf("executorFor(%q) built nothing", name)
		}
		if got := worker.Subharness(); got != exec.LinearSubharness {
			t.Fatalf("executorFor(%q) = %q, want the one worker", name, got)
		}
	}
}

// `--subharness` is not a flag, and the way to prove that is the flag package's
// own sentence rather than a grep: a person who types it is told the flag does
// not exist, and the run stops there.
//
// The usage text is the other half. It is what `aforge --help` prints, and a
// program that had stopped taking the flag while still advertising it would be
// telling everybody to type something that fails.
func TestTheSubharnessFlagIsNotAFlag(t *testing.T) {
	// THE REFUSAL IS ON STDERR AND THE EXIT IS 1. A flag that could not be read
	// is the first rung of the one ladder — nothing was attempted — so the door
	// hands back an exit status rather than a sentence, and the sentence itself
	// has already gone to stderr where a caller reads failures (envelope.go,
	// usage.go).
	_, said := captureUsage(t)
	err := runDo([]string{"--subharness", "linear", "fix the failing test"})
	if code := exitCodeOf(err); code != 1 {
		t.Fatalf("do accepted --subharness (exit %d)", code)
	}
	if !strings.Contains(said.String(), "flag provided but not defined") {
		t.Fatalf("do answered %q, want the flag package's own not-defined error", said.String())
	}
	if !strings.Contains(said.String(), "subharness") {
		t.Fatalf("the refusal does not name what was typed: %q", said.String())
	}
	_, said = captureUsage(t)
	if code := exitCodeOf(runExecute([]string{"--subharness", "linear", "a-program"})); code != 1 ||
		!strings.Contains(said.String(), "flag provided but not defined") {
		t.Fatalf("run answered %q, want the flag package's own not-defined error", said.String())
	}

	for _, banned := range []string{"--subharness", "Workers:"} {
		if strings.Contains(usageText, banned) {
			t.Fatalf("the usage text still offers %q", banned)
		}
	}
	// `swe` as a word, because "answer" carries the three letters and the usage
	// text is full of answers.
	if word := regexp.MustCompile(`\bswe\b`); word.MatchString(usageText) {
		t.Fatalf("the usage text still names a worker: %q", word.FindString(usageText))
	}

	// The saved-program door is what `aforge run` MEANS now, and the old
	// `run subharness` spelling still reaches it. Either one with no program
	// after it answers with the saved-program usage, which is proof the word
	// routed to runSubharnessCommand and not to the plan runner.
	for _, spelling := range [][]string{{"subharness"}, {}} {
		_, said := captureUsage(t)
		err := runExecute(spelling)
		if err == nil || !strings.Contains(err.Error(), "aforge run <program>") {
			t.Fatalf("`aforge run %v` answered %v, want the saved-program usage", spelling, err)
		}
		_ = said
	}
}

// #207's replication, deterministic: a leaf that cannot do the work ends with
// its failure named, and nothing hands it to anybody else.
//
// The defect it pins is one line of narration a benchmark read as recovery: an
// arrow from one worker to another, printed after a failed attempt, on a run
// that had been pinned to one worker. There is one worker now, so a failed
// attempt is retried on it or the node ends — and what proves it is not a grep
// over this package but the run's own journal, which must carry no worker
// change at all.
func TestAFailedAttemptEndsWithItsFailureNamedAndIsHandedToNobody(t *testing.T) {
	script := newScriptedBrain(t)
	script.leafFails = true
	defer script.close()

	database := filepath.Join(t.TempDir(), "graph.db")
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task:     "fix the failing parser tests",
		database: database, keep: true,
		timeout: 60 * time.Second, asJSON: true,
		stdout: &stdout, stderr: &stderr, newClient: script.client,
	})
	var status exitStatus
	if !asExitStatus(err, &status) || (status != exitCannotRun && status != exitIncomplete) {
		t.Fatalf("a run whose every leaf failed left with %v, want exit 1 or 2\nstderr:\n%s",
			err, stderr.String())
	}
	if script.count("leaf-failed") == 0 {
		t.Fatalf("no leaf was ever attempted\nstderr:\n%s", stderr.String())
	}

	var outcome struct {
		Deliverable string `json:"deliverable"`
		Subharness  string `json:"subharness"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &outcome); err != nil {
		t.Fatalf("stdout is not the machine-readable outcome: %v\n%s", err, stdout.String())
	}
	if strings.TrimSpace(outcome.Deliverable) == "" {
		t.Fatalf("the run ended without naming what went wrong:\n%s", stdout.String())
	}
	if outcome.Subharness != exec.LinearSubharness {
		t.Fatalf("the run reports it was taken by %q", outcome.Subharness)
	}

	// The journal. A worker change is an event, and no run of this build may
	// write one.
	graph, openErr := store.Open(database)
	if openErr != nil {
		t.Fatal(openErr)
	}
	defer graph.Close()
	events, eventsErr := graph.Events(0, 5000)
	if eventsErr != nil {
		t.Fatal(eventsErr)
	}
	for _, event := range events {
		if event.Kind == store.EventNodeWorkerChanged {
			t.Fatalf("the run journaled a change of worker: %s", string(event.Payload))
		}
	}

	// And the stream a person reads. The arrow and the hand-over are the two
	// sentences the incident was read from.
	said := stdout.String() + stderr.String()
	for _, banned := range []string{"escalated linear", "handed to", "↻"} {
		if strings.Contains(said, banned) {
			t.Fatalf("the run said %q:\n%s", banned, said)
		}
	}
}
