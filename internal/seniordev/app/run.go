//go:build !windows

// This file is one run as codeaf starts it: what senior-dev's own command line
// used to do between parsing its flags and printing its terminal event, with
// codeaf's host in place of stdout and codeaf's model API in place of a key.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/processgroup"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/modelsdev"
	"github.com/Agent-Field/codeaf/internal/seniordev/netpolicy"
)

// version is the build senior-dev reports itself as: to the session store it
// writes and to the model catalog it fetches. It is codeaf's own, because
// there is no senior-dev that differs from the codeaf it ships in.
var version = buildinfo.String()

// Stages are the stages a run reports, in the order a run first reaches them:
// the `hello` codeaf draws the whole track from before the run has walked it.
//
// THE LIST IS CLOSED, and a test holds it to the source: every stage the run
// can emit is here, and nothing is here that it cannot emit
// (stages_test.go). compaction-capacity, compaction, router-cancellation and
// model-switch happen inside a model turn — when a rejection pins a window, the
// history is compacted, a call is withdrawn or the coder moves to another
// model — so they sit where the turns are.
var Stages = []string{
	"bootstrap",
	"run-contract",
	"intake",
	"landing",
	"implement",
	"agent-runtime",
	"compaction-capacity",
	"compaction",
	"router-cancellation",
	"model-switch",
	"submit",
	"verification",
	"ship",
	"patch-summary",
	"agent-summary",
}

// Options is what one run is asked to do.
type Options struct {
	// Goal is the brief, exactly as it was given. It is written to
	// .senior-dev/spec.md byte for byte and read back from there, so nothing
	// between the person and the model paraphrases it.
	Goal string
	// High, Low and Frontier are the model pools, comma-separated; an empty Low
	// or Frontier routes on High.
	High     string
	Low      string
	Frontier string
	// Variant is the reasoning effort, sent as `reasoning.effort`.
	Variant string
	// InPlace edits the folder without git: no commits, no refs, and the run's
	// checkpoints kept outside it.
	InPlace bool
	// Crew says the pools came from the crew of the conversation that started
	// the run (`--crew`), not from a person typing them: a model the catalog
	// cannot size is dropped with a note, and a --high left empty routes on
	// [DefaultHighModels] ([crewPools]).
	Crew bool
	// Asked says the --high pool is the models the person asked for (`--asked`).
	// It is kept whole under Crew: every one of them must be sizable, and the
	// run refuses before its first call, naming the one that is not
	// ([askedRefusal]).
	Asked bool
}

// Run runs senior-dev once in the host's workspace and answers how it ended.
// It reports its stages and finished steps through host as it goes; the
// hello before it and the terminal after it are the caller's, which is the
// one place every ending reaches, a panic's included. notes is where its lines
// for a person go: stderr, which codeaf keeps beside the task.
func Run(ctx context.Context, host delegate.Host, options Options, notes io.Writer) delegate.Ending {
	return runWith(ctx, host, options, notes, nil)
}

// runWith is Run with a model backend a test can put in the model API's
// place. An injected backend is the whole of the model side: the run then
// neither needs the host's model API nor loads the model catalog, the shape
// senior-dev's own in-process tests always ran in.
func runWith(ctx context.Context, host delegate.Host, options Options, notes io.Writer, injected backend) delegate.Ending {
	if marker := env.Get(processgroup.RunMarkerEnv); marker != "" {
		// Only the engine is a subreaper. It reaps its own shell descendants
		// before it exits; the host's marker sweep is for a SIGKILL ending.
		if err := processgroup.EnableSubreaper(); err != nil {
			return refused("senior-dev cannot contain its shell processes on this machine: " + err.Error())
		}
		defer processgroup.CleanupDescendants()
	}
	if notes == nil {
		notes = io.Discard
	}
	// A network policy that failed to parse refuses the run: an unrecognized
	// SENIOR_DEV_NET must neither silently allow egress nor silently run a paid
	// multi-hour job in a mode nobody asked for.
	policy := netpolicy.Current()
	if policy.Warning != "" {
		return refused(policy.Warning)
	}
	if policy.Restricted() {
		_, _ = io.WriteString(notes, "[senior-dev] network policy: off — agent-initiated egress disabled "+
			"(the model API is unaffected)\n")
	}
	if strings.TrimSpace(options.Goal) == "" {
		return refused("there is no brief: senior-dev needs the change to make, in words, after the flags")
	}
	api := host.Models()
	if injected == nil && !api.Ready() {
		return refused("senior-dev was started without a model API; codeaf serves one to every run it starts")
	}
	args := cliArgs{
		High: options.High, Low: options.Low, Frontier: options.Frontier,
		Variant: options.Variant, InPlace: options.InPlace,
	}
	if len(splitPool(args.High)) == 0 {
		return refused("--high names no model, and the coder needs one to route on")
	}
	// A CEILING OF ZERO IS NO CEILING, and is passed as none, so senior-dev's
	// own SENIOR_DEV_MAX_COST_USD and SENIOR_DEV_MAX_WALL_H still apply to a run
	// codeaf set no limit on.
	ceilings := host.Ceilings()
	if ceilings.CostUSD > 0 {
		args.MaxCost = &ceilings.CostUSD
	}
	if ceilings.Hours > 0 {
		args.MaxHours = &ceilings.Hours
	}
	workspace := host.Workspace()
	loadedConfig, err := loadSeniorDevConfig(workspace)
	if err != nil {
		return refused("load config: " + err.Error())
	}
	loadedConfig.variant = args.Variant
	events := newRecordWriter(host, notes)
	model := injected
	if model == nil {
		client := newModelAPIBackend(api, args.Variant)
		loadedConfig.applyBackend(client)
		client.events = events
		catalog, err := loadCatalog(ctx, notes)
		if err != nil {
			return refused("model catalog: " + err.Error())
		}
		client.catalog = catalog
		model = client
		known := func(ref string) bool {
			if len(catalog) == 0 {
				return true
			}
			providerID, modelID := normalizeModelRef(splitModelID(ref))
			if _, err := catalog.Resolve(providerID, modelID); err == nil {
				return true
			}
			return len(loadedConfig.model(providerID, modelID)) > 0
		}
		if options.Asked {
			if refusal := askedRefusal(args.High, known); refusal != "" {
				return refused(refusal)
			}
		}
		if options.Crew {
			high := args.High
			args = crewPools(args, known, notes)
			if options.Asked {
				args.High = high
			}
		}
	}

	runner := newPipeline(args, workspace, pipelineDeps{
		Backend: model, Config: loadedConfig, Events: events, Notes: notes,
	})
	defer runner.runtime.Close()
	result, runErr := runner.run(ctx, options.Goal)
	result, runErr = classifyRunError(ctx, runner, result, runErr)
	if runErr != nil {
		_, _ = fmt.Fprintf(notes, "[senior-dev] the run failed: %v\n", runErr)
	}
	// The per-agent rollup lands immediately before the terminal record, so
	// every completed run carries its own account of wall time and model calls.
	if summaryData := events.summary.data(); summaryData != nil {
		events.stage("agent-summary", "completed", summaryData)
	}
	return endingOf(result)
}

// loadCatalog is the models.dev catalog the run prices and sizes models from:
// the cached copy, else a fetch, kept fresh in the background for as long as
// the run lasts. SENIOR_DEV_MODELS_PATH, SENIOR_DEV_MODELS_URL and
// SENIOR_DEV_DISABLE_MODELS_FETCH steer it. It is not a model call: it names
// each model's window and prices, which is what compaction is sized by.
func loadCatalog(ctx context.Context, notes io.Writer) (modelsdev.Catalog, error) {
	catalogClient, err := modelsdev.NewFromEnv(version)
	if err != nil {
		return nil, err
	}
	catalog, err := catalogClient.Get(ctx)
	if err != nil {
		// The host still serves and meters every model call when the third-party
		// catalog is offline. An empty catalog selects the conservative engine
		// limits below; a catalog outage cannot refuse the whole run.
		_, _ = fmt.Fprintf(notes, "[senior-dev] models.dev is unavailable; using conservative model limits: %v\n", err)
		return modelsdev.Catalog{}, nil
	}
	catalogClient.StartRefresh(ctx, func(refreshErr error) {
		_, _ = fmt.Fprintf(notes, "[senior-dev] failed to fetch models.dev: %v\n", refreshErr)
	})
	return catalog, nil
}

// askedRefusal is the sentence for models the person asked for that senior-dev
// cannot size — it needs each model's window to keep a long run's history in
// it — or "" when it can size them all.
func askedRefusal(pool string, known func(string) bool) string {
	var unknown []string
	for _, ref := range splitPool(pool) {
		if !known(ref) {
			unknown = append(unknown, strings.TrimPrefix(ref, orclient.Service+"/"))
		}
	}
	if len(unknown) == 0 {
		return ""
	}
	return "senior-dev cannot work with " + strings.Join(unknown, ", ") +
		": its model catalog does not know how much it can hold, so nothing was started; ask for a model it knows"
}

// refused is the ending of a run that could not start: its brief, its
// settings or its model catalog stood in the way, and nothing ran.
func refused(reason string) delegate.Ending {
	return delegate.Ending{Status: delegate.StatusCrashed, Message: reason}
}

// classifyRunError maps a pipeline error onto the terminal result. Crossing a
// declared budget ceiling is an ordinary ending, not a crash: the run stops
// where it stopped and reports what it had. Only senior-dev's own failures
// crash.
//
// A STOP FROM OUTSIDE IS NOT A CRASH EITHER. codeaf ends a run with SIGTERM —
// the person stopped it, or the run it belongs to ended — and the run's
// context ends with it. The run stops starting new work, ships what it has
// (ship runs on every ending) and says what is true: a ceiling it had crossed
// is budget-exhausted, a candidate it had frozen stands, and anything else is
// work that did not finish, never a program that broke.
func classifyRunError(ctx context.Context, runner *pipeline, result pipelineResult, runErr error) (pipelineResult, error) {
	if runErr == nil {
		return result, nil
	}
	result.CostUSD = runner.totalCost()
	result.WallStart = runner.wallStart
	if errors.Is(runErr, errRunBudget) {
		result.Status = delegate.StatusBudget
		if exhausted, reason := runner.budgetExhausted(); exhausted && reason != "" {
			result.Reason = reason
		} else {
			result.Reason = runErr.Error()
		}
		return result, nil
	}
	if ctx.Err() != nil {
		if exhausted, reason := runner.budgetExhausted(); exhausted {
			result.Status, result.Reason = delegate.StatusBudget, reason
			return result, nil
		}
		result.Status, result.Reason = delegate.StatusFail, "stopped before it finished"
		if account, _ := result.Terminal["reason"].(string); account != "" {
			result.Reason += "; " + account
		}
		return result, nil
	}
	result.Status = delegate.StatusCrashed
	result.Reason = runErr.Error()
	return result, runErr
}

// endingOf is the run's result as the one terminal record codeaf reads.
//
// TWO WITNESSES, KEPT APART. Claim is what senior-dev's model said when it
// submitted; Observed is what senior-dev itself saw when it ran the project's
// build and tests on the tree it froze. Neither is reconciled into the other,
// and everything else senior-dev knows about the ending travels beside them
// in its own spelling.
//
// The sentences are written for a person, because codeaf folds them into the
// commit that lands and the note that says so.
func endingOf(result pipelineResult) delegate.Ending {
	extra := map[string]any{}
	for key, value := range result.Terminal {
		extra[key] = value
	}
	ending := delegate.Ending{
		Status:  result.Status,
		Message: messageOf(result, extra),
		CostUSD: result.CostUSD,
	}
	if rescue, _ := extra["rescue_path"].(string); rescue != "" {
		ending.Message += ". Files that changed in the folder before senior-dev restored its checkpoint were set aside in " + rescue
		if deleted, _ := extra["rescue_deletions"].(bool); deleted {
			manifest, _ := extra["rescue_manifest"].(string)
			ending.Message += "; files deleted during the run are listed in " + manifest + " there"
		}
	}
	if reason, _ := extra["reason"].(string); reason != "" && reason != ending.Message {
		ending.Reason = reason
	}
	delete(extra, "reason")
	ending.Claim, _ = extra["submission_reason"].(string)
	ending.Observed = observedOf(extra)
	if len(extra) > 0 {
		ending.Extra = extra
	}
	return ending
}

// messageOf is the ending in one sentence. A run that submitted is said in
// terms of what its own check of the project found, which is the fact the
// status projects; everything else keeps the reason the run gave.
func messageOf(result pipelineResult, data map[string]any) string {
	inner, _ := data["status"].(string)
	switch {
	case result.Status == delegate.StatusPass && inner == "pass":
		return "submitted a change, and the project's own build and tests passed"
	case result.Status == delegate.StatusPass && inner == "pass-unverified":
		return "submitted a change, and nothing finished checking it"
	case result.Status == delegate.StatusFail && inner == "fail":
		return "submitted a change that the project's own build or tests do not pass"
	}
	return result.Reason
}

// observedOf says what senior-dev itself saw of the project's build and tests
// on the tree the run left, and what it did to that tree, empty when it ran
// nothing.
func observedOf(data map[string]any) string {
	var said []string
	inner, _ := data["status"].(string)
	_, checked := data["verification_commands"]
	commands := wholeNumber(data["verification_commands"])
	failing := wholeNumber(data["verification_failing"])
	failure, _ := data["verification_failure"].(string)
	switch {
	case inner == "pass-unverified":
		said = append(said, "nothing finished running the project's build and tests on the submitted change")
	case !checked:
	case data["verification_timed_out"] == true:
		said = append(said, "the project's build and tests did not finish in the time allowed")
	case failing > 0:
		said = append(said, fmt.Sprintf("%d of the project's %d build and test commands failed", failing, commands))
	case failure != "":
		said = append(said, "the project's check could not run: "+failure)
	case commands > 0:
		said = append(said, fmt.Sprintf("the project's %d build and test commands all passed", commands))
	default:
		said = append(said, "the project has no build or tests it could find to run")
	}
	if data["suite_dead"] == true {
		said = append(said, "its test suite could not even start")
	}
	if source, _ := data["restore_source"].(string); source != "" {
		said = append(said, "the tree was put back to "+RestoredFrom(source))
	}
	return strings.Join(said, "; ")
}

// RestoredFrom names a restore's source the way a person would: the ending's
// observation says it here, and senior-dev's page says it with the same words
// (internal/seniordev's actions.go).
func RestoredFrom(source string) string {
	switch source {
	case "coherent-checkpoint":
		return "the last state whose build and tests could run"
	case "starting-tree":
		return "the tree it started from"
	case "starting-commit":
		return "the commit it started from"
	}
	return source
}

// wholeNumber reads a count out of the terminal data, which holds it as an
// int when the run wrote it and as a float64 once it has been through JSON.
func wholeNumber(value any) int {
	switch number := value.(type) {
	case int:
		return number
	case float64:
		return int(number)
	}
	return 0
}
