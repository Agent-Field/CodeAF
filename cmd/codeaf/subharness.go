package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/ctxbudget"
	"github.com/Agent-Field/codeaf/internal/exec"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/plan"
	"github.com/Agent-Field/codeaf/internal/profile"
	"github.com/Agent-Field/codeaf/internal/store"
)

// This is the surface's half of the leaf contract: how the worker is
// constructed for a particular leaf.
//
// The description of the worker — its ruler, its budget shape — is a fact about
// the process and lives in exec. What lives here is the wiring it needs to
// actually run: a provider client, the job's workspace, the store it reports
// through. Those are the surface's, and no two surfaces build them the same
// way, which is why this is a constructor and not an executor.

// leafBuild is everything the worker needs to be constructed for one leaf. It
// is the executor's own argument list, named, because that list is the
// definition of what a leaf's worker is given, and every surface has to hand
// over the same one.
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
	// model names what this leaf runs on, in codeaf's spelling. It is what the
	// router's ledger is keyed on, so it travels with the build rather than
	// being read back off the client.
	model string
	// models is the provider's own catalog, carried for one question: how large
	// this leaf's window is. Only the surface has the catalog, so it is threaded
	// rather than looked up in exec.
	models *catalog.Catalog
	// window is how many tokens the model accepts when the builder already knows
	// it; zero asks [leafBuild.models]. It exists for a model on another service
	// than the catalog's: a conversation on `codex/gpt-5.5` carries the Codex
	// catalog, whose rows are spelled `gpt-5.5`, so asking it about the qualified
	// id answered zero for every leaf the conversation ran (#1383).
	window int
	// fanIn is what actually landed into this leaf, measured once at claim time.
	// Zero is the honest value for a leaf nothing fed, and it is what every
	// budget below reduces to for such a leaf — so a node that gathers nothing
	// keeps byte for byte the budget it had before any of this existed.
	fanIn store.DependencyFanIn
	// swarm arms the leaf's cooperative division verb, and it is a field here
	// rather than a read of settings.Swarm below for one reason: the settings
	// row says the person turned the mode on, and this says the surface
	// building the leaf can actually act on what the leaf asks for.
	//
	// Only the resident settles leaves through the path that grows the graph
	// from a split request. The one-shot headless runner builds its leaves from
	// this same struct — that is the covenant, and it is what stops a leaf
	// behaving differently on one surface than on the other — but its
	// settlement has no cooperative arm, so a leaf armed there would be handed
	// a verb whose answer is silence. A capability that cannot work is absent,
	// not broken.
	swarm bool
}

// The leaf's prompt budget, in the two numbers this package has to state for
// ctxbudget to do the arithmetic.
const (
	// leafPromptFloorTokens is what a leaf's turn costs before a single byte of
	// dependency text is added: the harness's own system message, the working
	// method, the tool schemas, the standing blocks. It is an estimate and it is
	// meant to be a generous one — being wrong upward here costs a little room,
	// and being wrong downward costs a compaction mid-job.
	leafPromptFloorTokens = 8 << 10

	// leafDependencyShare of leafPromptShares is how much of what is left the
	// results feeding this leaf may take. The other share is everything else the
	// leaf is handed and that grows with the job rather than with the fan-in —
	// the notebook, the brief and its contract, the job board, steering — and it
	// must not be squeezed to nothing by one verbose upstream.
	leafDependencyShare = 1
	leafPromptShares    = 2
)

// dependencyPot is what this leaf may be shown of everything that fed into it,
// sized from the window of the model that will actually read it.
//
// It is computed once per worker build and handed to the store as one number,
// which is what keeps it stable: the pot decides how many inputs are carried and
// how hard each is clipped, so a value that drifted between two reads in the
// same pass would move the prompt prefix under a cache that is counting on it
// not moving.
//
// An unknown window falls back to store.MaxDigestBytes — the literal every leaf
// had before this existed — because zero is "nobody could say", never "small".
func (b leafBuild) dependencyPot() int {
	// A nil catalog answers zero, which is the same answer as an unlisted model
	// and wants the same handling: fall back, never guess small.
	return ctxbudget.For(b.contextLength()).WithFloor(leafPromptFloorTokens).
		WithCompletionReserve(gatheringReserve(b.fanIn)).
		Share(leafDependencyShare, leafPromptShares, store.MaxDigestBytes)
}

// gatheringReserve is the room this leaf's reply needs, in tokens, sized from
// what fed it.
//
// The derivation is one line of arithmetic over one measured number. An assembly
// is bounded above by the text it assembles — a node cannot emit more of its
// inputs than it was given — so the room to write it is the ordinary reserve
// plus the landed bytes turned into tokens by the shared estimator:
//
//	reserve = ctxbudget.CompletionReserve() + fanIn.Bytes/ctxbudget.BytesPerToken
//
// A leaf with no dependencies measures zero and gets the ordinary reserve
// unchanged. The clamp lives in ctxbudget, where the window is, and so does the
// consequence: a bigger reserve is a smaller dependency pot, which is the right
// trade in the right direction — the more there is upstream, the more of it a
// gathering node should pull on demand and the less of it should be pushed into
// its prompt whether it needs it or not.
//
// Measured, this is the failure it fixes: an assembler ran its budget out
// partway through the join and had to be bought a paid continuation splice to
// finish typing results it had already read.
func gatheringReserve(fanIn store.DependencyFanIn) int {
	return ctxbudget.CompletionReserve() + fanIn.Bytes/ctxbudget.BytesPerToken
}

// gatheringGrant sizes one leaf's whole-run budget — its turn ceiling and its
// token ceiling — from what actually landed into it, instead of from a flat
// default written for a leaf that gathers nothing.
//
// The derivation, in full, from the two numbers store.DependencyFanIn measures:
//
//	landed = fanIn.Bytes / ctxbudget.BytesPerToken   the upstream text, in tokens
//	turns  = turns  + fanIn.Count
//	tokens = tokens + 2*landed
//
// One extra turn per dependency because opening one handle is one tool call, and
// a node handed fifty results to join and no extra turns to fetch them has been
// told to pull with no hands.
//
// Twice landed because a gathering node does two billable things with what fed
// it: it reads all of it in — through the digests, and through the handles it
// chooses to open — and it writes the assembly back out, and an assembly cannot
// exceed what it assembles. Neither term is a threshold and neither classifies:
// a node with no dependencies measures zero on both and receives exactly the
// defaults it always had, a two-way join gets a little more, and the wide join
// gets what a wide join costs. There is no "is this a synthesis node?" question
// to get wrong, because the measurement already answers it continuously.
func gatheringGrant(turns, tokens int, fanIn store.DependencyFanIn) (int, int) {
	landed := fanIn.Bytes / ctxbudget.BytesPerToken
	return turns + fanIn.Count, tokens + 2*landed
}

// The re-dispatch grant: what a leaf that has been sent round again in place
// is given, in the one number the dispatch path states for ctxbudget.
const (
	// overrunRegrantNum over overrunRegrantDen is how much bigger one
	// re-dispatch's token grant is than the grant of the attempt that ran
	// out: three halves. A re-dispatch exists to finish a truncated tail, and
	// half again is enough to finish one, where doubling would buy the whole
	// run a second time.
	overrunRegrantNum = 3
	overrunRegrantDen = 2

	// overrunGrantCeiling is where the regrant stops adding: four flat leaf
	// grants, which is room to run an ordinary leaf's whole text through twice
	// over and therefore more than finishing a tail can ever cost. The bound
	// is on what the ladder adds and not on what the leaf was granted — a
	// fan-in that measured more than this keeps every token it measured,
	// because the one thing a re-dispatch must never do is hand back room.
	overrunGrantCeiling = 4 * chatLeafTokens
)

// regrantAfterRunningOut is the token grant one claim of a leaf that has been
// re-dispatched in place is given, from the grant this claim measured and the
// attempt it is on. Attempt zero is the grant unchanged; every attempt after
// it grows by [overrunRegrantNum] over [overrunRegrantDen] for each re-dispatch
// before it, and the ladder adds nothing beyond [overrunGrantCeiling].
// Turns are not regrown: a re-dispatch carries its banked turns as inputs
// rather than re-running them, so the turn ceiling was never what ran out.
//
// The growth is the point. A re-dispatch handed the room its predecessor ran
// out of re-runs the same brief to the same truncated ending — the meter stops
// it at the same place — so the runner's release (see resident's overrun
// settle) is worth a claim only if the room moves with it. The wall the leaf
// is given follows, because it is arithmetic over the same number.
func regrantAfterRunningOut(tokens, attempt int) int {
	room := tokens
	for ; attempt > 0 && room < overrunGrantCeiling; attempt-- {
		room = room * overrunRegrantNum / overrunRegrantDen
		if room > overrunGrantCeiling {
			return overrunGrantCeiling
		}
	}
	return room
}

// foldGrant sizes the whole run of a leaf that is going to make one model call.
//
// The gathering grant above is the right arithmetic for a node that has to go
// and open what fed it: a turn per dependency to fetch with, and twice the
// landed text to read it in and write it back out. A fold opens nothing. Its
// material is in its prompt already, so its run is a prompt and an answer, and
// its grant is that and no more:
//
//	pass   = promptFloor + pushed/BytesPerToken + reserve
//	tokens = turns * pass
//
// The reserve is the consumer's own rather than the process-wide constant, and
// it is stated the same way a gathering leaf states it — through ctxbudget,
// which owns both the clamp and the window it is clamped against. An assembly
// is bounded above by the material it assembles, so a node handed 28 KB of
// results needs room to write up to 28 KB back; a constant reserve is what left
// an assembler mid-assembly, buying a paid continuation splice to finish typing
// results it had already read.
//
// Turns multiply because the second call resends the first call's prompt: two
// passes are two prompts and two answers, and a ceiling that budgeted one would
// stop the recovery pass the fold exists to be allowed.
//
// ceiling is what this node would have been granted as an open-ended gathering
// leaf, and the fold may not exceed it. The reserve is a generous per-call
// output cap by design — "a ceiling only costs on the turns that use it" — so
// two of them plus two prompts can add up to more than the open shape's whole
// envelope, and a mode that exists to be the cheaper one must never buy a
// bigger allowance than the mode it replaces. The saving a fold is actually
// for is the turn cap; this is the guarantee that the token ceiling does not
// quietly give it back.
//
// An unknown window keeps the process-wide reserve, which is what every budget
// in the tree does when nobody can say how large the window is.
func foldGrant(window, turns, pushed, ceiling int) (int, int) {
	landed := pushed / ctxbudget.BytesPerToken
	reserve := ctxbudget.CompletionReserve()
	if budget := ctxbudget.For(window).WithFloor(leafPromptFloorTokens).
		WithCompletionReserve(reserve + landed); budget.Known() {
		reserve = budget.CompletionReserveTokens
	}
	tokens := turns * (leafPromptFloorTokens + landed + reserve)
	if ceiling > 0 && tokens > ceiling {
		tokens = ceiling
	}
	return turns, tokens
}

// buildLinear constructs the worker for one leaf. It is a function of leafBuild
// rather than an open-coded call at each dispatch site because every surface has
// to build the leaf's executor from the same argument list, or the two surfaces
// drift apart one field at a time.
func buildLinear(build leafBuild) exec.Executor {
	// The catalog answers the one question the loop needs and cannot look up
	// itself: how much this leaf's model can hold, which is what its
	// observation window is sized from. Threaded rather than read in exec, for
	// the same reason the model name is — the surface owns the catalog, and the
	// loop is handed facts.
	return exec.NewLinear(build.client, build.workspace, build.web,
		build.maxTurns, build.maxTokens, build.deadline).
		WithStore(build.graph).WithMedia(build.media).
		WithAssistedBy(config.AssistedByModelAt(build.settings.ProfileDir, build.model)).
		// The cooperative division verb: on when the person turned the mode
		// on AND the surface holding this leaf can act on what it asks for.
		// See leafBuild.swarm.
		WithSwarm(build.swarm && build.settings.Swarm).
		WithContextLength(build.contextLength())
}

// contextLength is this leaf's window: the one the builder was handed, and the
// catalog's answer about the model when it was handed none.
func (b leafBuild) contextLength() int {
	if b.window > 0 {
		return b.window
	}
	return b.models.ContextLength(b.model)
}

// executorFor builds the worker one leaf was promised. There is one worker, so
// the answer never depends on the name — but the name still arrives, out of
// stores written when it could, and this is the door that resolves it. The
// degradation is the same one Registry.For makes and is made here for the same
// reason: a node that names a worker we do not have should still get its work
// done.
func executorFor(subharness string, build leafBuild) exec.Executor {
	return buildLinear(build)
}

// runningWorker is THE SEAM. It builds the worker one node will actually be run
// by and writes that down against the node in the same breath, because this is
// the one moment in the process where both halves of the fact are in hand: the
// node, and the executor the resolution above just settled on.
//
// It exists because the two facts were never the same column and one of them
// was never written at all. `nodes.subharness` is an ASSIGNMENT and nothing
// writes one, so an autopsy of a run read a table of blanks. Every node of the
// s9 sweep's ink and igel stores said exactly that, and the sweep's diagnosis
// cost a day to a question the store could not answer: WHO DID THE WORK. It
// answers it now, here, and it answers "linear" rather than leaving the blank
// that also means "nobody ran this".
//
// [executorFor] stays the pure resolution it always was, and after this it has
// one caller in the surface. That is a law and not a convenience: a dispatch
// path that resolved a worker without journaling it would put the blank back on
// exactly the nodes nobody thought to look at.
// `TestTheWorkerThatRanIsWrittenAtOneSeam` fails the build on a second caller.
func runningWorker(nodeID, promised string, build leafBuild, reason string) exec.Executor {
	worker := executorFor(promised, build)
	ran := worker.Subharness()
	if reason == "" && strings.TrimSpace(promised) != "" && strings.TrimSpace(promised) != ran {
		// The degradation, said in the record rather than only on a trace file
		// somebody has to know to open. A benchmark cell that silently became a
		// default cell is a measurement of the wrong thing, and this is the row
		// that tells whoever reads the store afterwards which of the two it was.
		reason = "promised " + strings.TrimSpace(promised) + "; ran linear, and this build has one worker"
	}
	recordRunningWorker(build.graph, nodeID, ran, reason)
	return worker
}

// recordRunningWorker journals who is doing the work. It is best-effort in the
// same sense every other note on the dispatch path is: a journal write that
// fails costs an autopsy and must never cost the work.
func recordRunningWorker(graph *store.Store, nodeID, subharness, reason string) {
	if graph == nil || strings.TrimSpace(nodeID) == "" || strings.TrimSpace(subharness) == "" {
		return
	}
	if _, err := graph.RecordNodeRan(nodeID, subharness, reason); err != nil {
		log.Printf("note: could not journal the worker running %s: %v", nodeID, err)
	}
}

// registerLeafExecutors gives a scheduler's registry the worker this build
// constructs for the run in hand. The headless scheduler resolves a node's leaf
// through the registry rather than through executorFor, so this is the same
// constructor reaching the other dispatch path — the two-surface covenant in
// one function.
func registerLeafExecutors(registry *exec.Registry, build leafBuild) {
	// The budget shape belongs to the worker rather than to the leaf, and it is
	// applied here as well as in the resident because a headless registry is
	// built once for a whole run.
	shaped := build
	shaped.deadline = exec.SubharnessFor(exec.LinearSubharness).Deadline(build.maxTokens)
	registry.Register(buildLinear(shaped))
	registerSubharnessRunners(registry, build)
}

// registerSubharnessRunners is the same constructor reaching the SUBHARNESS half
// of the registry: the worker this build constructs, fronted as a typed program
// under the contract in docs/SUBHARNESS-CONTRACT.md.
//
// IT IS THE SAME CONSTRUCTOR, deliberately. The worker fronted here and the
// worker a leaf gets from [executorFor] are built from one line of code, so
// `linear` reached by name is the same worker a node with no name is handed —
// which is what makes the deoptimization path honest, because falling back to
// the long way has to mean falling back to the worker the person would
// otherwise have had.
//
// THE GENERALIST IS NEVER ON A LIST. It is a NAME the deopt path resolves and
// the headless runner may be pointed at, so the lookup has to reach it, and it
// stays off every list a person reads — which is the list's business rather
// than the registry's.
//
// A worker that cannot be fronted is skipped in silence: nothing is registered,
// and the name simply is not a subharness on this surface.
func registerSubharnessRunners(registry *exec.Registry, build leafBuild) {
	info := exec.SubharnessFor(exec.LinearSubharness)
	shaped := build
	shaped.deadline = info.Deadline(build.maxTokens)
	runner, err := exec.FrontExecutor(buildLinear(shaped), exec.LeafManifest(info))
	if err != nil {
		return
	}
	_ = registry.RegisterRunner(runner)
}

// installMeasuredRulers seats the ruler from the worker's own measured history,
// and hands back its profile because that is the one every caller goes on to
// read for prices and spreads.
//
// A machine with no file yet keeps the prior it registered with, which is what
// an empty Anchors already means everywhere else.
//
// It is also where model identity and the lane ledger's fold are seated, and
// that is not a coincidence: this is the one function every surface that records
// anything calls before it reads or writes a profile — the plan command, the
// headless run, chat, and the wake pass. A history keyed on the operator's
// spelling instead of on the model is two histories and two rulers for one
// executor, which is the thing this function exists to prevent one file at a
// time.
func installMeasuredRulers(settings config.Config, model string) *profile.Profile {
	shared := sharedCatalog(settings)
	profile.UseIdentity(shared.Identity)
	// And the ledger's fold, which is a DIFFERENT question with a different
	// answer: a history asks whether two spellings are one model, while a belief
	// about machines has to be filed under the id those machines actually serve.
	// See [catalog.Catalog.Servable].
	lanes.UseServable(shared.Servable)
	// AND THE LANE-SHEET BEAT, for the same one-funnel reason and immediately
	// after the fold it files under: three doors of this binary build no session
	// and so fetched no endpoints page at all, which left every headless install
	// choosing between the one machine its last run was served by (lanebeat.go).
	startLaneBeat(settings, model)
	measured, _ := profile.Load(settings.ProfileDir, model, exec.LinearSubharness)
	plan.UseAnchors(measured.Anchors)
	return measured
}

// sharedCatalog is this process's one model catalog.
//
// It is lazy and memoised for the same reason the surfaces that build their own
// are lazy: discovery is a daily-cached file behind a background fetch, nothing
// it answers is asked before the first frame, and a launch path that waited for
// it would hold the terminal behind a network round trip. Memoising it means the
// identity seam and the surface that shows the model list are looking at the
// same catalog rather than racing two fetches over one cache file.
var sharedCatalog = newSharedCatalog()

// newSharedCatalog builds the memoised accessor [sharedCatalog] is. It is a
// function rather than the value alone so a test can seat its own instance:
// the once is inside, and two runs in one process must not share a catalog
// pointed at whichever of them called first.
func newSharedCatalog() func(config.Config) *catalog.Catalog {
	var once sync.Once
	var resolved *catalog.Catalog
	return func(settings config.Config) *catalog.Catalog {
		once.Do(func() {
			resolved = catalog.LoadLazy(context.Background(), catalog.Options{
				BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
			})
			// A tier row that says auto is answered from this catalog (config.AutoModels):
			// the same non-blocking read, never a fetch, and set once at start-up so every
			// headless door resolves the word against the list it already holds.
			config.AutoModels = resolved.ModelsNow
			// and the pool's index beside it, in the same one-time manner: a tier
			// row that says auto is answered from the index this process was seated
			// with, the seed when no cache is fresher, and the one fetch it makes
			// runs in the background and never blocks this read.
			wirePoolIndex(settings.ProfileDir)
		})
		return resolved
	}
}

// autoSeatRowsBound is how long a headless door waits for the catalog's rows
// when the profile's pick or a tier row needs them. It is sized to cover the
// disk read of a cached catalog and nothing more. It is a variable because the
// test of the bound must not spend three seconds proving the bound is honoured.
var autoSeatRowsBound = 3 * time.Second

// useAutoSeats seats this process's catalog under the seat ladder, and is what
// a headless door calls BEFORE it resolves its seats.
//
// The order is the whole of it. A tier row that says `auto` is answered from
// the rows already in hand ([config.AutoModels]), and every headless door
// climbed the ladder before it asked for a catalog at all — so the word read
// against nothing and landed on the family's table row on every run, on a
// machine whose catalog was sitting in its own cache file. It is the same lazy,
// memoised catalog every one of those doors goes on to use; asking for it a few
// lines earlier waits for nothing.
//
// AND WHEN THE ANSWER NEEDS THE ROWS, THE DOOR WAITS FOR THEM, within
// [autoSeatRowsBound]: a pick taken off the table ([config.CrewPickAt]) and a
// tier row that says auto ([config.AnyTierAutoAt]) are both computed from those
// rows, and the chat surface never met the defect because its picks happen after
// the warm has landed. The bound is a bound on the wait, not on the fetch — when
// it runs out the warm carries on in the background, the resolver falls to the
// family's table row exactly as it did before, and the seat's receipt names the
// rung that answered (`table`), so a run that fell says it fell. A profile with
// neither a pick nor an auto row reads no rows at all, and waits for nothing,
// the way it always has.
func useAutoSeats(settings config.Config) {
	resolved := sharedCatalog(settings)
	if config.CrewPickAt(settings.ProfileDir) == config.CrewPickTable && !config.AnyTierAutoAt(settings.ProfileDir) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), autoSeatRowsBound)
	defer cancel()
	resolved.Warmed(ctx)
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
	// the workspace belongs to a person, the job's own directory otherwise. The
	// path itself comes from exec rather than being spelled again here — this
	// note and the recorder must land in one file, and they stopped doing so the
	// moment the recorder moved and this line did not.
	home := leafWorkerNotes.scratch
	if home == "" {
		home = filepath.Join(leafWorkerNotes.workspace, jobIDOf(leafWorkerNotes.graph, node))
	}
	path := exec.TraceFile(home, node.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
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

// noteUnavailableWorker is the one sentence a build owes a node whose stored
// row names a worker it does not have. Such rows exist: a graph written before
// this build resumes in it, and its nodes still carry the name they were given.
// The work gets done — the registry's promise is degradation, never failure —
// but silent degradation is a run reading as something it was not. The
// registry, the store and exec stay quiet by law; saying it is the surface's
// job, and this is the surface's sentence.
func noteUnavailableWorker(stderr io.Writer, worker string) {
	if stderr == nil {
		return
	}
	fmt.Fprintf(stderr, "note: %q is not a worker; ran linear, and this build has one worker\n",
		strings.TrimSpace(worker))
}
