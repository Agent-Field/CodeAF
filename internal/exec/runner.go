package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// THE RUNNER: a subharness is a FUNCTION, not a chat.
//
// That sentence is PRD §4 and it is the keystone every downstream feature leans
// on. A run takes typed input, produces typed output, and ends either having
// produced its declared shape or INCOMPLETE with a reason. There is no third
// ending, and in particular there is no "done with a strange message" — which
// was the only ending the old one-string program form could offer, and the
// reason nothing downstream of a run could ever be built on it.
//
// Two implementations are real and the contract permits no more than it has to:
//
//   - A GO RUNNER implements this natively and is registered at compile time.
//     The owner's custom Go subharnesses land here first-class with zero porting,
//     and `linear` and `swe` are re-fronted through it ([ExecutorRunner]).
//   - THE JS RUNNER IS ONE GENERIC GO RUNNER parameterized by a bundle. There is
//     not one runner per bundle: there is one goja host, and a bundle is its
//     argument. The runtime lane builds it; this file defines the door it comes
//     through ([BundleSource]).
//
// Future runners — a subprocess, WASM — are permitted by this shape and are not
// being built. Nothing here may be complicated by their possibility.
type Runner interface {
	// Manifest is everything true about this subharness before it runs. It
	// answers the name too, which is why there is no second Name method: a
	// runner whose name and whose manifest could disagree is a registry entry
	// filed under something it does not describe.
	Manifest() Manifest

	// Run does the work. The input is JSON matching the manifest's input schema;
	// the Env is the whole capability surface (env.go) and the only way to
	// spend. AN ERROR IS A RUN THAT COULD NOT BE MADE TO HAPPEN — a broken
	// bundle, a dead context — and is what the deoptimization path catches. A
	// run that HAPPENED and did not finish returns a [RunResult] with its
	// Incomplete reason set and no error at all.
	Run(ctx context.Context, input json.RawMessage, env Env) (RunResult, error)
}

// RunResult is what a run produced.
type RunResult struct {
	// Output is the typed answer, matching the manifest's output schema. A run
	// that ends with nothing here has not finished, and [RunResult.Finished] says
	// so without anybody having to remember the rule.
	Output json.RawMessage `json:"output,omitempty"`

	// Report is the short prose a person reads: what happened, in the
	// vocabulary law's words. It is not a summary of the output — the output is
	// rendered from its own schema — it is the account of the run.
	Report string `json:"report,omitempty"`

	// Artifacts are the files the run made, if it made any.
	Artifacts []Artifact `json:"artifacts,omitempty"`

	// Incomplete is WHY it did not finish, in a person's words, and empty when
	// it did. Out of budget, out of time, a question nobody was there to answer,
	// a step that could not produce its shape: all of them end here, all of them
	// say what ran out. "incomplete" is one of the five sanctioned words for the
	// state of work, and nothing may reach a person calling this a failure.
	Incomplete string `json:"incomplete,omitempty"`

	// FellBack is set when the work was done the long way — the program's guards
	// did not pass, or it broke partway, and the general worker finished the job
	// with the original input. IT IS NOT A FAILURE AND MUST NOT BE DRAWN AS ONE:
	// the person is told the step needed a closer look and was handled the long
	// way. The value is the person-facing half of that sentence, drawn from the
	// guard's own Because line where it had one.
	FellBack string `json:"fell_back,omitempty"`

	// Spend is the whole run's ledger, which is the sum of its journal's
	// entries and can be nothing else.
	Spend Spend `json:"spend,omitzero"`
}

// Finished says the run produced what it promised. It is the machine-checkable
// finish line, asked in one place so that no surface invents a second reading of
// what "done" means.
func (r RunResult) Finished() bool {
	return r.Incomplete == "" && len(r.Output) > 0 && string(r.Output) != "null"
}

// Artifact is one file a run produced.
type Artifact struct {
	// Name is what it is called where a person reads it.
	Name string `json:"name"`
	// Path is where it actually is.
	Path string `json:"path"`
	// Note is one line about what it is for, drawn beside it. Nothing renders as
	// nothing.
	Note string `json:"note,omitempty"`
}

// ErrNoSubharness is what a lookup answers for a name nothing in this build or
// any of its stores has heard of. It is a real error and not a fallback: a
// LEAF that names an unknown worker is served by the generalist (Registry.For's
// promise, unchanged below), but a PERSON who typed a name into `/subharness`
// has made a typo and would be badly served by silently getting something else.
var ErrNoSubharness = errors.New("no subharness by that name")

// BundleSource is a place bundles are loaded from — the store lane's hook, and
// the only one it needs. Registering one at a [Layer] is the whole of putting a
// store in front of the registry.
//
// IT IS ASKED, NEVER SCANNED. A source answers one name at a time because the
// stores are directories on disk that change under a running process: a
// registry that had cached a listing would run a version the person had
// replaced, and one that re-listed on every lookup would stat a directory per
// dispatch. What the source caches, and how it decides a page has moved, is the
// store lane's business.
type BundleSource interface {
	// Runner loads the head version of one bundle. Not found is (nil, false),
	// which is not an error — it is the next layer's turn.
	Runner(name string) (Runner, bool)
	// Manifests is everything this source can offer, for the lists and the
	// menus. A source that cannot be read answers nothing: a registry is not
	// worth failing a launch over, and a list is not where a person learns their
	// disk is gone.
	Manifests() []Manifest
}

// bundleLayer is one registered source and where it sits in the lookup.
type bundleLayer struct {
	layer  Layer
	source BundleSource
}

// RegisterRunner adds a Go-native subharness to this registry.
//
// IT IS PER-REGISTRY AND NOT PROCESS-GLOBAL, and the split is the same one this
// package has always drawn: a DESCRIPTION is a fact about the process and lives
// in the global table (subharness.go), while a WORKER is built by a surface out
// of its own clients and workspaces and lives on the registry that surface
// built. A JS runner needs no such wiring and could have been global; making it
// follow the same rule is what keeps one lookup instead of two.
//
// A runner whose manifest does not validate is refused, with the manifest's own
// prose. Registration is the one door, so a subharness is never half-installed.
func (r *Registry) RegisterRunner(runner Runner) error {
	if runner == nil {
		return errors.New("a registration needs something to run")
	}
	manifest := runner.Manifest()
	if err := manifest.Validate(); err != nil {
		return err
	}
	// Layer zero, stamped rather than believed: a Go runner is in this binary by
	// construction, and nothing it says about itself can change that.
	manifest.Provenance = LayerBuiltIn.Provenance()
	if r.runners == nil {
		r.runners = make(map[string]Runner, 4)
	}
	r.runners[manifest.Name] = stamped{Runner: runner, manifest: manifest}
	return nil
}

// UseBundles puts a store in front of this registry at one layer. The store lane
// registers one source for the project store and one for the home store; the
// phase that builds the packed trailer registers one at [LayerPacked] and edits
// no lookup.
//
// Registering the same layer twice replaces it, because a layer is a PLACE and a
// place has one store.
func (r *Registry) UseBundles(layer Layer, source BundleSource) {
	if source == nil {
		return
	}
	for index, existing := range r.bundles {
		if existing.layer == layer {
			r.bundles[index].source = source
			return
		}
	}
	r.bundles = append(r.bundles, bundleLayer{layer: layer, source: source})
	sort.SliceStable(r.bundles, func(i, j int) bool { return r.bundles[i].layer < r.bundles[j].layer })
}

// Subharness resolves a name to what will actually run it, in the lookup order
// PRD §7 states: compiled-in first, then the packed trailer, then the project
// store, then the home store; first hit wins.
//
// COMPILED-IN IS LAYER ZERO AND WINS, which is this build's reading of "Go-native
// subharnesses are layer 0 implicitly". A bundle on disk may not shadow a name
// the binary ships, because those names are what the manual, the system prompt
// and every menu describe — a store that could quietly replace one would turn
// all three into documents about a program that did not run.
//
// A name nothing has is [ErrNoSubharness]. That is the difference between this
// door and [Registry.For] beside it: For serves a leaf that asked for a worker
// this build does not have, because the work still has to get done; this serves
// a person or a program that named a subharness, where getting something else
// would be worse than being told.
func (r *Registry) Subharness(name string) (Runner, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNoSubharness
	}
	if runner, ok := r.runners[name]; ok {
		return runner, nil
	}
	for _, layer := range r.bundles {
		if runner, ok := layer.source.Runner(name); ok && runner != nil {
			// The provenance is where it was FOUND, stamped over whatever the
			// manifest on disk claimed. See [Manifest.Provenance].
			manifest := runner.Manifest()
			manifest.Provenance = layer.layer.Provenance()
			return stamped{Runner: runner, manifest: manifest}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrNoSubharness, name)
}

// Manifests is every subharness this registry can reach, compiled-in and stored,
// in one list with the lookup's own precedence already applied: a name found at
// two layers appears once, described by the layer that would actually run it.
//
// This is the list every surface draws — the `/subharness` picker, the catalog
// index when it arrives, the per-turn reminder. There is no second enumeration
// anywhere, which is what makes "the person cannot tell which is which" true by
// construction rather than by discipline.
func (r *Registry) Manifests() []Manifest {
	found := make(map[string]Manifest, len(r.runners))
	order := make([]string, 0, len(r.runners))
	keep := func(manifest Manifest, layer Layer) {
		if manifest.Name == "" {
			return
		}
		if _, already := found[manifest.Name]; already {
			return
		}
		manifest.Provenance = layer.Provenance()
		found[manifest.Name] = manifest
		order = append(order, manifest.Name)
	}
	for _, runner := range r.runners {
		keep(runner.Manifest(), LayerBuiltIn)
	}
	for _, layer := range r.bundles {
		for _, manifest := range layer.source.Manifests() {
			keep(manifest, layer.layer)
		}
	}
	sort.Strings(order)
	list := make([]Manifest, 0, len(order))
	for _, name := range order {
		list = append(list, found[name])
	}
	return list
}

// Generalist is the worker a run falls back to. It is the same executor
// [Registry.For] hands an unknown name, exposed by name because the
// deoptimization path needs it deliberately rather than by accident: a program
// whose guard did not pass has ASKED for the long way, and the long way is this.
func (r *Registry) Generalist() Executor { return r.fallback }

// stamped is a runner wearing a manifest the registry corrected — the
// provenance, and nothing else. It exists so that the stamping happens once, at
// the door, instead of at every surface that draws a mark.
type stamped struct {
	Runner
	manifest Manifest
}

func (s stamped) Manifest() Manifest { return s.manifest }

// ── Re-fronting the workers this build already has ──────────────────────────
//
// `linear` and `swe` are the first Go-native subharnesses under this contract,
// and they are re-fronted rather than rewritten: an [Executor] is already a
// worker that takes a job and produces an outcome, so what stands between it and
// a [Runner] is a typed front door and a typed answer. [ExecutorRunner] is that
// door and nothing else. Every existing caller of Registry.For, of
// executorFor, and of the leaf table in cmd/aforge reaches the same executor by
// the same path it always did — this is a second way IN, not a change to what
// runs.

// TaskInput is the typed front door of every subharness that is really a leaf
// worker. It is [Task]'s four prose fields and no more, because those are the
// four things a leaf is actually told: what the whole job is for, what THIS
// piece is, how this kind of work is done well, and what to call it.
type TaskInput struct {
	// Brief is the self-contained instruction and the one required field. A leaf
	// with no brief has been handed nothing to do.
	Brief string `json:"brief"`
	// Goal is the whole plan's aim, for orientation. Empty where there is no
	// wider plan, which is every direct invocation.
	Goal string `json:"goal,omitempty"`
	// Contract is the working method — how this kind of job is done well.
	Contract string `json:"contract,omitempty"`
	// Title is what the work is called on a roster row.
	Title string `json:"title,omitempty"`
}

// TaskOutput is what such a worker promises: its final message, and the files it
// left behind.
type TaskOutput struct {
	Result    string   `json:"result"`
	Artifacts []string `json:"artifacts,omitempty"`
}

// taskInputSchema and taskOutputSchema are [TaskInput] and [TaskOutput] written
// as JSON Schema. They are hand-written rather than reflected because reflection
// over a struct is the runtime lane's tool and this file may not depend on it;
// when that tool lands, these two become its first callers and the structs stay
// the source of truth.
const (
	taskInputSchema = `{
  "type": "object",
  "x-order": ["brief", "goal", "contract", "title"],
  "required": ["brief"],
  "properties": {
    "brief": {"type": "string", "title": "the work", "description": "what to do, whole and self-contained"},
    "goal": {"type": "string", "title": "the wider aim", "description": "what this is a piece of, for orientation"},
    "contract": {"type": "string", "title": "how it is done well", "description": "the working method for this kind of job"},
    "title": {"type": "string", "title": "what to call it", "description": "three words for the roster row"}
  }
}`
	taskOutputSchema = `{
  "type": "object",
  "required": ["result"],
  "properties": {
    "result": {"type": "string", "title": "what it produced"},
    "artifacts": {"type": "array", "title": "files it left", "items": {"type": "string"}}
  }
}`
)

// LeafManifest grows one leaf worker's registration into a manifest.
//
// EVERY REGISTRATION MADE THROUGH [RegisterSubharness] IS A LEAF WORKER, without
// exception in this tree: linear, swe, bare, and the owner's next Go one are all
// workers that are handed a job and produce an outcome. So they all have the
// same typed front door and the same typed promise — [TaskInput] and
// [TaskOutput] — and stating that here, once, is what re-fronts every one of
// them under the subharness contract without a single caller changing a line.
//
// It leaves cues, whitelist and guards empty, and that is honest rather than
// unfinished: a leaf worker matches by being CHOSEN by the compiler from the
// menu its purpose is on, its tools are the belt its surface built it with, and
// there is no cheap precondition to check before handing somebody a general
// agent. A worker that wants any of the three fills them in and registers
// through [RegisterManifest] instead.
func LeafManifest(info SubharnessInfo) Manifest {
	return Manifest{
		SubharnessInfo: info,
		Input:          Schema(taskInputSchema),
		Output:         Schema(taskOutputSchema),
		Provenance:     FromBinary,
	}
}

// ExecutorRunner fronts one [Executor] as a [Runner].
//
// IT IS GENERIC AND MUST STAY SO. Nothing in it may ask which worker it is
// wrapping — that is the same law that keeps the leaf table a table, and it is
// what makes the owner's next Go subharness a registration instead of an edit to
// this file. The executor's own manifest carries the purpose, the ruler and the
// budget shape; this type carries the translation and nothing else.
//
// It takes no [Env]. That is not an oversight and it is the honest shape: an
// executor built by a surface already has its own client, its own workspace and
// its own belt, and handing it a second capability surface it cannot reach would
// be a promise the wrapper could not keep. A Go subharness that WANTS the host
// doors implements [Runner] directly, which is the whole reason the interface is
// the contract and this adapter is only a bridge.
type ExecutorRunner struct {
	executor Executor
	manifest Manifest
}

// FrontExecutor wraps an executor with the manifest that describes it. The
// manifest's name has to be the executor's own subharness, because a registry
// entry filed under one name and dispatching to another is a measurement of the
// wrong worker wearing the right one's history.
func FrontExecutor(executor Executor, manifest Manifest) (*ExecutorRunner, error) {
	if executor == nil {
		return nil, errors.New("there is no worker here to front")
	}
	if manifest.Name == "" {
		manifest.Name = executor.Subharness()
	}
	if manifest.Name != executor.Subharness() {
		return nil, fmt.Errorf("this manifest is called %q and the worker behind it is %q",
			manifest.Name, executor.Subharness())
	}
	if manifest.Input.Empty() {
		manifest.Input = Schema(taskInputSchema)
	}
	if manifest.Output.Empty() {
		manifest.Output = Schema(taskOutputSchema)
	}
	if err := manifest.Validate(); err != nil {
		return nil, err
	}
	return &ExecutorRunner{executor: executor, manifest: manifest}, nil
}

func (e *ExecutorRunner) Manifest() Manifest { return e.manifest }

// Executor is the worker underneath, for the dispatch paths that still want it
// whole — the scheduler's, and the deoptimization path's.
func (e *ExecutorRunner) Executor() Executor { return e.executor }

// Run turns typed input into a [Task], runs it, and turns the [Outcome] back
// into typed output.
//
// THE ENDING IS READ FROM THE OUTCOME AND NOT GUESSED. A leaf that ran out of
// budget lands truthfully on StopDone — that is what [Outcome.Exhausted] exists
// to tell apart — so a wrapper that read Stop alone would report a truncated
// partial as a finished deliverable, which is exactly the failure the Exhausted
// field was added for. [Outcome.Overran] is the one question worth asking and it
// asks both fields.
//
// THE ENV IS TAKEN AND NOT USED, AND THAT IS A FACT ABOUT WHAT AN EXECUTOR IS
// rather than an oversight — it is named `_` so nobody reads it as a seam that
// merely has not been wired yet. An [Executor] is a whole worker with its own
// client and its own belt (linear.go), not a program stepping through host
// calls: it never asks for a tool, a model call or a person, so there is nothing
// here for the Env's doors to serve. The parameter stays because [Runner] is one
// interface over both shapes, and [runSpend]'s own note says the same thing from
// the ledger's side — a fronted leaf reports its spend and journals nothing.
//
// WHAT THAT COSTS IS PAID IN [Deopt] AND NOWHERE ELSE. Because this belt cannot
// be filtered, a program's approved ceiling cannot be applied to a worker
// reached through here — so the decision about whether the fallback may run at
// all is taken before this function, on the program's own manifest
// ([DeoptHeld]). Nothing about that decision belongs in here: this function
// serves the ordinary path too, where the manifest IS this runner's own.
func (e *ExecutorRunner) Run(ctx context.Context, input json.RawMessage, _ Env) (RunResult, error) {
	var typed TaskInput
	if len(input) > 0 && string(input) != "null" {
		if err := json.Unmarshal(input, &typed); err != nil {
			return RunResult{}, fmt.Errorf("this input is not what %q takes: %w", e.manifest.Name, err)
		}
	}
	if strings.TrimSpace(typed.Brief) == "" {
		return RunResult{}, fmt.Errorf("%q needs a brief — there is nothing here to do", e.manifest.Name)
	}
	outcome, err := e.executor.Run(ctx, Task{
		Title:    typed.Title,
		Goal:     typed.Goal,
		Brief:    typed.Brief,
		Contract: typed.Contract,
	})
	if err != nil {
		return RunResult{}, err
	}
	if outcome == nil {
		return RunResult{Incomplete: "it stopped without saying anything"}, nil
	}
	output, marshalErr := json.Marshal(TaskOutput{Result: outcome.Text, Artifacts: outcome.Artifacts})
	if marshalErr != nil {
		return RunResult{}, marshalErr
	}
	result := RunResult{Output: output, Report: outcome.Text, Spend: spendOf(outcome.Usage)}
	for _, path := range outcome.Artifacts {
		result.Artifacts = append(result.Artifacts, Artifact{Name: path, Path: path})
	}
	if outcome.Overran() {
		result.Incomplete = "it ran out of room before it was finished"
	} else if strings.TrimSpace(outcome.Text) == "" {
		result.Incomplete = "it finished without producing anything"
	}
	return result, nil
}

// spendOf reads a leaf's [Usage] into the ledger's figures. The two differ in
// one place and it is worth being explicit about: [Usage] folds cache reads into
// one CachedTokens field, so the write side arrives here as zero. That is the
// executor's own accounting being less detailed than the ledger's, not a loss —
// nothing downstream of a leaf has ever been told the two apart.
func spendOf(usage Usage) Spend {
	return Spend{
		Calls:     usage.Calls,
		Input:     usage.PromptTokens,
		Output:    usage.CompletionTokens,
		CacheRead: usage.CachedTokens,
		CostUSD:   usage.Cost,
	}
}
