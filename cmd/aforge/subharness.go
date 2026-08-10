package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// This is the surface's half of the subharness contract: which workers this
// build has, and how one is constructed for a particular leaf.
//
// The description of a worker — its purpose, its ruler, its budget shape — is a
// fact about the process and lives in exec. What lives here is the wiring a
// worker needs to actually run: a provider client, the job's workspace, the
// store it reports through. Those are the surface's, and no two surfaces build
// them the same way, which is exactly why the table is a table of constructors
// and not a table of executors.
//
// Wave one shipped one entry, and that was the point: the seam had to be
// load-bearing before the second worker existed, or the second worker would
// have arrived as a rewrite of every dispatch path instead of a registration.
// swe is the proof — it is two lines here and one file beside this one.

// installSubharnesses declares this build's workers to the whole process. It is
// called once, before any command runs, so every surface — chat, do, run, wake
// — sees the same menu and the same rulers. Adding a worker is a line here and
// a line in leafExecutors, and nothing else.
func installSubharnesses() {
	exec.RegisterSubharness(sweInfo())
}

// leafBuild is everything a worker needs to be constructed for one leaf. It is
// the linear executor's own argument list, named, because that list is the
// definition of what a leaf's worker is given and a second worker is given no
// less.
type leafBuild struct {
	settings  config.Config
	client    exec.Completer
	workspace *exec.Workspace
	web       *exec.Web
	graph     *store.Store
	media     *exec.MediaTools
	maxTurns  int
	maxTokens int
	deadline  time.Duration
	// model names what this leaf runs on, in aforge's spelling. The generalist
	// never needed it — its client already is that model — but a worker that
	// drives a separate process has to be able to say the name out loud, and a
	// specialist quietly substituting its own vendor defaults would make the
	// router's ledger a record of models nobody chose.
	model string
	// models is the provider's own catalog, carried for one question: what
	// concrete model a floating alias stands for. Only a worker that hands the
	// name to another process needs the answer, and only the surface has the
	// catalog, so it is threaded rather than looked up in exec.
	models *catalog.Catalog
}

// leafExecutors is name-to-constructor: what a surface calls when a node says
// it wants a particular worker. Linear's entry builds exactly what every leaf
// has always been built with, so routing through the table changes nothing for
// the leaf that takes the default.
var leafExecutors = map[string]func(leafBuild) exec.Executor{
	exec.LinearSubharness: func(build leafBuild) exec.Executor {
		// The catalog is already here for the specialist's sake, and it answers
		// one more question the generalist needs: how much this leaf's model can
		// hold, which is what its observation window is sized from. Threaded
		// rather than looked up in exec, for the same reason the model name is —
		// the surface owns the catalog, and the loop is handed facts.
		return exec.NewLinear(build.client, build.workspace, build.web,
			build.maxTurns, build.maxTokens, build.deadline).
			WithStore(build.graph).WithMedia(build.media).
			WithAttribution(config.AttributionAt(build.settings.ProfileDir)).
			WithContextLength(build.models.ContextLength(build.model))
	},
	// The coding pipeline takes none of the leaf loop's wiring, because it
	// shares none of it: no provider client (it opens its own connections from
	// the key), no toolbox, no store. What it needs is the workspace, the
	// model this leaf was promised, the credentials to reach it, and a clock.
	exec.SWESubharness: func(build leafBuild) exec.Executor {
		return exec.NewSWE(build.workspace, engineModelID(build.models, build.model),
			build.settings.APIKey, build.settings.BaseURL, build.deadline).
			WithMaxCost(sweMaxCost(os.Getenv))
	},
}

// engineModelID is the leaf's model in the spelling a second process can look
// up, and it exists because of one live failure that cost nothing and told us
// everything: `models.dev: model "deepseek/deepseek-v4-flash-latest" not found
// for provider "openrouter"` — the engine dead at startup, $0 spent, on the
// default model of every aforge install.
//
// A floating alias is a real OpenRouter id, which is why every linear leaf in
// the product runs on one without noticing. It is not a models.dev id, and the
// engine prices its calls from models.dev. The alias has to be resolved on this
// side of the process boundary, where the catalog that knows about aliases
// lives; exec stays generic and is handed a name.
//
// Resolution that fails passes the name through untouched. A model nobody chose
// must never enter the pools, so the failure mode is the engine's own error
// message about the id it was actually given — not a quiet substitution of some
// near neighbour that happens to be in a catalog.
func engineModelID(models *catalog.Catalog, model string) string {
	model = strings.TrimSpace(model)
	if models == nil || model == "" {
		return model
	}
	return models.Concrete(model)
}

// executorFor builds the worker one leaf was promised, degrading to the
// generalist for a name this build cannot construct. The degradation is the
// same one Registry.For makes and is made here for the same reason: a node that
// names a worker we do not have should still get its work done.
func executorFor(subharness string, build leafBuild) exec.Executor {
	if construct, ok := leafExecutors[strings.TrimSpace(subharness)]; ok && exec.KnownSubharness(subharness) {
		return construct(build)
	}
	return leafExecutors[exec.LinearSubharness](build)
}

// registerLeafExecutors fills a scheduler's registry with every worker this
// build can construct for the run in hand. The headless scheduler resolves a
// node's choice through the registry rather than through executorFor, so this
// is the same table reaching the other dispatch path — the two-surface covenant
// in one function.
func registerLeafExecutors(registry *exec.Registry, build leafBuild) {
	for _, info := range exec.Subharnesses() {
		construct, ok := leafExecutors[info.Name]
		if !ok {
			// Described but not constructible on this surface. Nothing is
			// registered, so Registry.For hands its leaves to the generalist.
			continue
		}
		// Each worker gets its own budget shape, here as well as in the
		// resident, because a headless registry is built once for a whole run:
		// the generalist's fifteen-minute hang backstop applied to a coding
		// pipeline is not a backstop, it is a guillotine at the first merge.
		shaped := build
		shaped.deadline = info.Deadline(build.maxTokens)
		registry.Register(construct(shaped))
	}
}

// installMeasuredRulers seats every worker's ruler from that worker's own
// measured history, and hands back the generalist's profile because that is the
// one every caller goes on to read for prices and spreads.
//
// A worker with no file yet keeps the prior it registered with, which is what
// an empty Anchors already means everywhere else.
func installMeasuredRulers(profileDir, model string) *profile.Profile {
	measured, _ := profile.Load(profileDir, model, exec.LinearSubharness)
	plan.UseAnchors(measured.Anchors)
	for _, info := range exec.Subharnesses() {
		specialist, err := profile.Load(profileDir, model, info.Name)
		if err != nil {
			continue
		}
		plan.UseAnchorsFor(info.Name, specialist.Anchors)
	}
	return measured
}

// profileSubharness is the file a measurement belongs in. Every measurement
// belongs in exactly one, and an unregistered name belongs in the generalist's:
// the leaf did run on the generalist, because that is what Registry.For handed
// it, and a record has to describe what happened rather than what was asked
// for. Reflex and direct buckets stay linear-only for the same reason — those
// rungs have no specialist to be measured against.
func profileSubharness(name string) string {
	if exec.KnownSubharness(name) {
		return strings.TrimSpace(name)
	}
	return exec.LinearSubharness
}

// withBoundaryEvidence is the one comparison no single worker can make about
// itself: what this run cost against what the generalist's ordinary leaf costs.
//
// A specialist knows whether a job sat inside its own envelope — it says so in
// Outcome.Calibration — but "inside my envelope" and "cheaper than the ordinary
// worker's median leaf" are different claims, and only the second one says the
// boundary between the two rulers is in the wrong place. A specialist run that
// came in under the generalist's median is a specialist that was reached for
// when the generalist would have done, and that is the boundary-too-low half of
// the evidence the recalibration call needs to move the seam in both directions.
//
// It is written generically and it has to be: the comparison is "any worker that
// is not the baseline, against the baseline", which is a fact about the registry
// and not about any worker's name. Nothing here may ask which specialist this is
// — the grep law is not decoration, it is what keeps the next specialist a
// registration instead of a rewrite.
func withBoundaryEvidence(settings config.Config, model, worker string, record profile.Record) profile.Record {
	if worker == exec.LinearSubharness || record.Cost <= 0 {
		return record
	}
	generalist, err := profile.Load(settings.ProfileDir, model, exec.LinearSubharness)
	if err != nil {
		return record
	}
	median := medianProfileCost(generalist)
	if median <= 0 || record.Cost >= median {
		return record
	}
	record.Calibration = append(record.Calibration, fmt.Sprintf(
		"it cost $%.4f, under the default worker's median leaf at $%.4f — "+
			"work this cheap may not have needed a specialist, and the boundary may sit too high",
		record.Cost, median))
	return record
}

// promisedWorker is the node's own answer to "who runs this", read in the order
// admission settled it: the row's worker where there is one, the subtree's
// otherwise. It is a pure read — every surface that only wants to *know* asks
// this one, and only the dispatch path asks the one that also speaks.
func promisedWorker(node store.Node) string {
	if settled := strings.TrimSpace(node.Subharness); settled != "" {
		return settled
	}
	return strings.TrimSpace(node.Provenance.Subharness)
}

// leafWorkerNotes is where the conversational surface says the same thing the
// headless one says on stderr. A chat window has no stderr a person will ever
// read, and the note does not belong in the thread — it is not conversation, it
// is machinery admitting a limit — so it goes where every other machinery fact
// about one leaf goes: that node's flight recorder, once, before the worker
// writes its first turn into the same file.
//
// It is seated rather than threaded because the dispatch path that discovers the
// degradation is handed a node and nothing else; the surface's own coordinates
// are a fact about the process, exactly like the worker table above it.
var leafWorkerNotes struct {
	mutex     sync.Mutex
	workspace string
	scratch   string
	graph     *store.Store
	said      map[string]bool
}

// seatLeafWorkerNotes tells this process where its jobs work. A surface that
// never calls it — a test, an embedder — degrades exactly as before and says
// nothing anywhere, which is the same silence the registry keeps.
func seatLeafWorkerNotes(workspace, scratch string, graph *store.Store) {
	leafWorkerNotes.mutex.Lock()
	defer leafWorkerNotes.mutex.Unlock()
	leafWorkerNotes.workspace, leafWorkerNotes.scratch, leafWorkerNotes.graph = workspace, scratch, graph
	leafWorkerNotes.said = nil
}

// noteDegradedLeafWorker writes the one line, once per node. Everything about it
// is best-effort: a missing directory, an unwritable file and an unseated
// surface all mean the same thing here, which is that the work goes on.
func noteDegradedLeafWorker(node store.Node, worker string) {
	if !degradedWorker(worker) {
		return
	}
	leafWorkerNotes.mutex.Lock()
	defer leafWorkerNotes.mutex.Unlock()
	if leafWorkerNotes.workspace == "" || leafWorkerNotes.graph == nil || node.ID == "" {
		return
	}
	if leafWorkerNotes.said == nil {
		leafWorkerNotes.said = make(map[string]bool, 1)
	}
	if leafWorkerNotes.said[node.ID] {
		return
	}
	leafWorkerNotes.said[node.ID] = true
	// The same place the tracer will open a moment later: the scratch home when
	// the workspace belongs to a person, the job's own directory otherwise.
	home := leafWorkerNotes.scratch
	if home == "" {
		home = filepath.Join(leafWorkerNotes.workspace, jobIDOf(leafWorkerNotes.graph, node))
	}
	directory := filepath.Join(home, ".obs")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(directory, fmt.Sprintf("%d.trace.log", node.CreatedSeq)),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	noteUnavailableWorker(file, worker)
}

// degradedWorker answers whether a node's promised worker is one this build
// cannot construct. The generalist and the unnamed are never degradations —
// they are the default — and the registry's own answer is the authority, so this
// asks the same question Registry.For asks a moment later.
func degradedWorker(worker string) bool {
	worker = strings.TrimSpace(worker)
	return worker != "" && worker != exec.LinearSubharness && !exec.KnownSubharness(worker)
}

// noteUnavailableWorker is the one sentence a build owes a node whose promised
// worker it does not have. The work still gets done on the generalist — the
// registry's promise is degradation, never failure — but silent degradation is
// how a measurement of the specialist becomes a measurement of the default
// wearing its name. The registry, the store and exec stay quiet by law; saying
// it is the surface's job, and this is the surface's sentence.
func noteUnavailableWorker(stderr io.Writer, worker string) {
	if stderr == nil {
		return
	}
	fmt.Fprintf(stderr, "note: worker %q not in this build; ran linear\n", strings.TrimSpace(worker))
}

// resolveSubharnessFlag reads what a person typed on the command line. An
// unknown name is a note on stderr and the default worker, never a refusal: the
// flag exists for measurement runs, and a benchmark that dies at argument
// parsing because a build shipped without one worker has wasted more than the
// measurement was worth.
func resolveSubharnessFlag(name string, stderr io.Writer) string {
	name = strings.TrimSpace(name)
	if name == "" || name == exec.LinearSubharness {
		return ""
	}
	if exec.KnownSubharness(name) {
		return name
	}
	available := "none are registered in this build"
	if registered := exec.Subharnesses(); len(registered) > 0 {
		names := make([]string, 0, len(registered))
		for _, info := range registered {
			names = append(names, info.Name)
		}
		available = "this build has: " + strings.Join(names, ", ")
	}
	fmt.Fprintf(stderr, "no subharness named %q — %s; running on the default worker\n", name, available)
	return ""
}
