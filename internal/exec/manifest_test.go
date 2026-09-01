package exec

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// The subharness contract's shapes, pinned. Several lanes build against them at
// once (docs/SUBHARNESS-CONTRACT.md), so what is asserted here is what may not
// move under them without somebody noticing.

func TestAManifestSaysWhatIsMissingInWordsItsAuthorCanActOn(t *testing.T) {
	cases := []struct {
		name     string
		manifest Manifest
		wants    string
	}{
		{"no name", Manifest{}, "needs a name"},
		{"a space in the name", Manifest{SubharnessInfo: SubharnessInfo{Name: "weekly marketing", Purpose: "p"}}, "one word"},
		{"a capital in the name", Manifest{SubharnessInfo: SubharnessInfo{Name: "Weekly", Purpose: "p"}}, "lowercase"},
		{"no purpose", Manifest{SubharnessInfo: SubharnessInfo{Name: "weekly"}}, "needs a purpose"},
		{
			"an input schema that is not an object",
			Manifest{SubharnessInfo: SubharnessInfo{Name: "weekly", Purpose: "p"}, Input: Schema(`{"type":"string"}`)},
			"has to be an object",
		},
		{
			"a check with nothing to look at",
			Manifest{SubharnessInfo: SubharnessInfo{Name: "weekly", Purpose: "p"}, Guards: []Guard{{Kind: GuardFile}}},
			"a path to look for",
		},
	}
	for _, one := range cases {
		t.Run(one.name, func(t *testing.T) {
			err := one.manifest.Validate()
			if err == nil {
				t.Fatalf("validated %+v, which is not a manifest", one.manifest)
			}
			if !strings.Contains(err.Error(), one.wants) {
				t.Fatalf("said %q, which does not tell the author %q", err, one.wants)
			}
		})
	}
}

// The worker is the one exemption, and it is exempt by name: its purpose is
// deliberately empty because it is never something anyone picks — it is what
// the work gets.
func TestTheBaselineIsTheOneManifestAllowedNoPurpose(t *testing.T) {
	if err := linearManifest.Validate(); err != nil {
		t.Fatalf("the baseline does not validate: %v", err)
	}
	if linearManifest.Purpose != "" {
		t.Fatalf("the baseline grew a purpose (%q); it is judged against, not chosen", linearManifest.Purpose)
	}
}

func TestAnIntakeCardReadsItsFieldsInAStableOrder(t *testing.T) {
	schema := Schema(`{
	  "type": "object",
	  "x-order": ["brief", "when"],
	  "required": ["brief"],
	  "properties": {
	    "brief": {"type": "string", "title": "the work"},
	    "when": {"type": "string", "default": "monday"},
	    "audience": {"type": "string"}
	  }
	}`)
	fields := schema.Fields()
	if len(fields) != 3 {
		t.Fatalf("read %d fields out of a schema with three", len(fields))
	}
	// The stated order first, then everything else alphabetically — so a card
	// drawn twice is the same card.
	for index, want := range []string{"brief", "when", "audience"} {
		if fields[index].Name != want {
			t.Fatalf("field %d is %q, wanted %q", index, fields[index].Name, want)
		}
	}
	if !fields[0].Required {
		t.Fatal("brief is required and the card was not told")
	}
	if fields[1].Required {
		t.Fatal("when is optional and the card was told otherwise")
	}
	if string(fields[1].Default) != `"monday"` {
		t.Fatalf("the default came back as %q", fields[1].Default)
	}
}

// A schema round-trips through a manifest as the bytes it was written as. That
// is what makes the contract language-agnostic: a bundle's manifest.json is not
// reshaped by passing through Go.
func TestASchemaSurvivesAManifestUnchanged(t *testing.T) {
	original := Manifest{
		SubharnessInfo: SubharnessInfo{Name: "weekly", Purpose: "the weekly marketing pass"},
		Cues:           []string{"marketing", "weekly"},
		Input:          Schema(`{"type":"object","properties":{"brief":{"type":"string"}}}`),
		Whitelist:      []string{"read", "write"},
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var back Manifest
	if err := json.Unmarshal(encoded, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Input.Fields()) != 1 || back.Input.Fields()[0].Name != "brief" {
		t.Fatalf("the input schema did not survive: %s", back.Input)
	}
	if back.Purpose != original.Purpose || len(back.Cues) != 2 || len(back.Whitelist) != 2 {
		t.Fatalf("the manifest did not survive: %+v", back)
	}
}

// namedRunner is a subharness with nothing behind it, for the lookup tests.
type namedRunner struct{ manifest Manifest }

func (n namedRunner) Manifest() Manifest { return n.manifest }

func (n namedRunner) Run(context.Context, json.RawMessage, Env) (RunResult, error) {
	return RunResult{}, nil
}

// mapSource is a store with a fixed set of bundles in it.
type mapSource struct{ runners map[string]Runner }

func (m mapSource) Runner(name string) (Runner, bool) {
	runner, ok := m.runners[name]
	return runner, ok
}

func (m mapSource) Manifests() []Manifest {
	list := make([]Manifest, 0, len(m.runners))
	for _, runner := range m.runners {
		list = append(list, runner.Manifest())
	}
	return list
}

func manifestNamed(name string) Manifest {
	return Manifest{SubharnessInfo: SubharnessInfo{Name: name, Purpose: "for the test"}}
}

// A BUNDLE ON DISK MAY NOT SHADOW A NAME THE BINARY SHIPS, and the project store
// is looked at before the home store. Both are the lookup order of PRD §7, and
// both are the reason the manual can describe a name and be right about it.
func TestTheLookupOrderIsBuiltInThenProjectThenHome(t *testing.T) {
	registry := NewRegistry(&namedExecutor{name: LinearSubharness})
	if err := registry.RegisterRunner(namedRunner{manifest: manifestNamed("shared")}); err != nil {
		t.Fatal(err)
	}
	registry.UseBundles(LayerHome, mapSource{runners: map[string]Runner{
		"shared": namedRunner{manifest: manifestNamed("shared")},
		"both":   namedRunner{manifest: manifestNamed("both")},
		"mine":   namedRunner{manifest: manifestNamed("mine")},
	}})
	registry.UseBundles(LayerProject, mapSource{runners: map[string]Runner{
		"both": namedRunner{manifest: manifestNamed("both")},
	}})

	for _, one := range []struct{ name, wants string }{
		{"shared", string(FromBinary)},
		{"both", string(FromProject)},
		{"mine", string(FromYou)},
	} {
		runner, err := registry.Subharness(one.name)
		if err != nil {
			t.Fatalf("%q: %v", one.name, err)
		}
		if got := string(runner.Manifest().Provenance); got != one.wants {
			t.Fatalf("%q came from %q, wanted %q", one.name, got, one.wants)
		}
	}

	// The one list every surface draws says the same thing about precedence.
	found := map[string]Provenance{}
	for _, manifest := range registry.Manifests() {
		found[manifest.Name] = manifest.Provenance
	}
	if len(found) != 3 {
		t.Fatalf("the list has %d entries for three names: %v", len(found), found)
	}
	if found["both"] != FromProject {
		t.Fatalf("the list disagrees with the lookup about %q: %q", "both", found["both"])
	}
}

// A person who typed a name that does not exist is told so. That is the whole
// difference between this door and Registry.For beside it, which serves a leaf
// the generalist rather than refusing it.
func TestANameNothingHasIsToldRatherThanSubstituted(t *testing.T) {
	registry := NewRegistry(&namedExecutor{name: LinearSubharness})
	if _, err := registry.Subharness("nothing-by-this-name"); !errors.Is(err, ErrNoSubharness) {
		t.Fatalf("answered %v, wanted no-such-subharness", err)
	}
	if worker := registry.For("nothing-by-this-name"); worker.Subharness() != LinearSubharness {
		t.Fatalf("a leaf that named an unknown worker got %q instead of the generalist", worker.Subharness())
	}
	if registry.Generalist() == nil {
		t.Fatal("the deoptimization path has no long way to fall back to")
	}
}

// swallowingExecutor answers a fixed outcome, so the re-fronting can be checked
// without a provider.
type swallowingExecutor struct {
	name    string
	outcome *Outcome
	brief   string
}

func (s *swallowingExecutor) Subharness() string { return s.name }

func (s *swallowingExecutor) Run(_ context.Context, task Task) (*Outcome, error) {
	s.brief = task.Brief
	return s.outcome, nil
}

// A leaf worker is re-fronted rather than rewritten: the same executor, a
// typed front door in front of it.
func TestAWorkerFrontedAsASubharnessTakesTypedInputAndPromisesTypedOutput(t *testing.T) {
	worker := &swallowingExecutor{name: "digger", outcome: &Outcome{
		Text:      "the flake was a shared temp directory",
		Artifacts: []string{"/tmp/patch.diff"},
		Stop:      StopDone,
		Usage:     Usage{Calls: 3, PromptTokens: 10, CompletionTokens: 4, Cost: 0.5},
	}}
	runner, err := FrontExecutor(worker, LeafManifest(SubharnessInfo{
		Name: "digger", Purpose: "chasing one flake to its cause", DeadlineFloor: time.Hour,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if runner.Manifest().Input.Empty() || runner.Manifest().Output.Empty() {
		t.Fatal("a re-fronted worker has no typed door")
	}
	if runner.Manifest().Deadline(0) != time.Hour {
		t.Fatal("the cost shape did not survive the fronting")
	}

	result, err := runner.Run(t.Context(), json.RawMessage(`{"brief":"find the flake"}`), UnwiredEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if worker.brief != "find the flake" {
		t.Fatalf("the worker was handed %q", worker.brief)
	}
	if !result.Finished() {
		t.Fatalf("a worker that produced its answer did not finish: %+v", result)
	}
	var typed TaskOutput
	if err := json.Unmarshal(result.Output, &typed); err != nil {
		t.Fatal(err)
	}
	if typed.Result != worker.outcome.Text || len(typed.Artifacts) != 1 {
		t.Fatalf("the output did not carry the run: %+v", typed)
	}
	if result.Spend.Calls != 3 || result.Spend.CostUSD != 0.5 {
		t.Fatalf("the ledger did not carry the run: %+v", result.Spend)
	}

	// A leaf whose budget ran out lands truthfully on StopDone. Reading Stop
	// alone would post a truncated partial as a finished deliverable.
	worker.outcome = &Outcome{Text: "half of it", Stop: StopDone, Exhausted: StopBudget}
	result, err = runner.Run(t.Context(), json.RawMessage(`{"brief":"find the flake"}`), UnwiredEnv{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Finished() || result.Incomplete == "" {
		t.Fatalf("a run that overran was reported as finished: %+v", result)
	}
}

// A run needs something to do, and a manifest cannot be filed under a name that
// is not the worker's.
func TestTheTypedDoorRefusesWhatItCannotRun(t *testing.T) {
	worker := &swallowingExecutor{name: "digger", outcome: &Outcome{Text: "ok"}}
	if _, err := FrontExecutor(worker, manifestNamed("marketing")); err == nil {
		t.Fatal("a worker was filed under somebody else's name")
	}
	runner, err := FrontExecutor(worker, LeafManifest(SubharnessInfo{Name: "digger", Purpose: "chasing one flake to its cause"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(t.Context(), json.RawMessage(`{}`), UnwiredEnv{}); err == nil {
		t.Fatal("a run with no brief was accepted")
	}
}

// Every door of the unwired host is honest about having nothing behind it, so a
// lane can compile against Env before a host exists.
func TestTheUnwiredHostAnswersEveryDoorTheSameHonestWay(t *testing.T) {
	var host Env = UnwiredEnv{}
	ctx := t.Context()
	if _, err := host.AI(ctx, "prompts/brief.md", nil, AIOptions{}); !errors.Is(err, ErrNotWired) {
		t.Fatalf("ai() answered %v", err)
	}
	if _, err := host.Tool(ctx, "read", nil); !errors.Is(err, ErrNotWired) {
		t.Fatalf("tool() answered %v", err)
	}
	if _, err := host.Ask(ctx, "which one?", AskOptions{}); !errors.Is(err, ErrNotWired) {
		t.Fatalf("ask() answered %v", err)
	}
	if err := host.Remember(ctx, "a note"); !errors.Is(err, ErrNotWired) {
		t.Fatalf("remember() answered %v", err)
	}
	if _, err := host.Recall(ctx, ""); !errors.Is(err, ErrNotWired) {
		t.Fatalf("recall() answered %v", err)
	}
	if err := host.Log(ctx, "running"); !errors.Is(err, ErrNotWired) {
		t.Fatalf("log() answered %v", err)
	}
}

// The ledger forgets a model the moment two calls disagree, and a journal entry
// summed across a run is the run's whole cost.
func TestTheLedgerForgetsAModelTwoCallsDisagreedOn(t *testing.T) {
	var run Spend
	run.Add(Spend{Model: "sonnet", Calls: 1, Input: 100, CostUSD: 0.01})
	if run.Model != "sonnet" {
		t.Fatalf("one call on sonnet reported %q", run.Model)
	}
	run.Add(Spend{Calls: 1, Input: 50})
	if run.Model != "sonnet" {
		t.Fatal("a call that named no model changed the run's model")
	}
	run.Add(Spend{Model: "opus", Calls: 1, CostUSD: 0.02})
	if run.Model != "" || !run.Mixed() {
		t.Fatalf("a run on two models still claims %q", run.Model)
	}
	run.Add(Spend{Model: "sonnet", Calls: 1})
	if run.Model != "" {
		t.Fatal("a third agreeing call restored a name the run had outgrown")
	}
	if run.Calls != 4 || run.Input != 150 || run.CostUSD != 0.03 {
		t.Fatalf("the figures did not sum: %+v", run)
	}
	if !run.Reported() {
		t.Fatal("a run the provider priced reads as unaccounted for")
	}
	if (Spend{Calls: 9}).Reported() {
		t.Fatal("nine calls nobody priced read as a run that cost something")
	}
}

// A journal that is not there is a run nobody is watching, which is a real case
// and not an error.
func TestARunNobodyIsWatchingStillRuns(t *testing.T) {
	if err := Record(nil, JournalEntry{Seq: 1, Call: CallLog, Note: "running"}); err != nil {
		t.Fatalf("writing into no journal answered %v", err)
	}
}
