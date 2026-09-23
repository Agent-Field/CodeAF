package main

import (
	"context"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/subharness"
	"github.com/Agent-Field/codeaf/internal/substore"
)

// THE SUBHARNESS SIDE OF ONE CONVERSATION: the programs it can reach, where they
// keep what they learn, and what it says about the last time each of them ran.
//
// It is assembled here for the reason every other registry on this path is
// (chatv3_harness.go): where the bundles live is the SURFACE'S decision, and a
// package that opened ~/.codeaf/subharnesses itself would open it from a test and
// from a task node's own agent too. internal/session is handed the four seams and
// nothing about a directory.
//
// THE WHOLE OF IT IS SILENT ON FAILURE, which is [v3HarnessEntries]'s posture
// carried over word for word: a store that cannot be read, a client that cannot
// be built and a workspace that cannot be resolved all mean SUBHARNESSES OFF —
// the doors in internal/session answer nothing, calmly, and the surface draws
// nothing. A registry is not worth failing a launch over.

// v3Subharness is what one conversation is handed. THE ZERO VALUE IS
// SUBHARNESSES OFF: every field is the nil [session.Config] already treats as
// "this build has none", so a caller assigns all four without asking whether the
// wiring worked.
type v3Subharness struct {
	Registry *exec.Registry
	Memory   session.SubharnessMemory
	LastRun  func(name string) string
	Record   func(name string, note session.SubharnessRunNote)
	// Belt is the seam a tool guard is checked through. See [beltWatch] for why
	// the indirection has to exist: the belt belongs to the [session.Agent] this
	// config is about to build, so there is nothing to ask at the moment the
	// registry is made.
	//
	// THE DOOR AT THE OTHER END IS session.Agent.ToolOnBelt, and the fill is one
	// line beside the agent's construction (chatv3.go, just after openV3Agent):
	//
	//	launch.Subharnesses.Belt.watch(agent.ToolOnBelt)
	//
	// Until that line runs — and on any surface that never runs it — every TOOL
	// guard answers false and a program gated on one takes the long way. That is
	// the safe direction and the one jsrun asks for, and it is never silent: the
	// person is told the step needed a closer look.
	Belt *beltWatch
}

// v3Subharnesses builds that. The workspace is the directory this conversation
// works in, which is both what a file guard is asked about and what decides
// whether there is a project store to consult at all.
//
// pages is the page store this conversation's designs are written to, handed in
// so the last-run reading can consult it. See [subharnessLastRun] for why one
// reader has to ask two stores.
//
// window is the model's context length as the conversation's own start window
// read it ([v3StartWindow]), zero when nobody could say; it is handed in rather
// than asked of models here because models is the catalog of the model's
// service, spelled in that service's bare ids, while model is the qualified id.
func v3Subharnesses(settings config.Config, models *catalog.Catalog, model string, window int, workspace string, pages *subharness.Store) v3Subharness {
	off := v3Subharness{}
	space, err := exec.NewWorkspace(strings.TrimSpace(workspace))
	if err != nil {
		return off
	}
	// The client is built the way every other client on this path is
	// (chatv3_harness.go, and internal/session's own agent.go): a plain adapter
	// rather than [config.Config.Client]'s router. The router keeps a ledger that
	// has to be flushed on the way out, and this surface has nowhere to hang that
	// close — a conversation's registry lives as long as the process does. Its
	// account settings still come through [config.Config.ClientConfig], so the
	// key, base URL, model slug and thinking level have the same one source as
	// every other client.
	//
	// The timeout is the harness node's, interpolated rather than restated: both
	// are a backstop against a wedged endpoint on a non-streamed call, and a
	// second number here would be the one that drifts.
	configured := settings.ClientConfig(model)
	configured.Timeout = harnessTimeout
	client, err := provider.NewClient(configured)
	if err != nil {
		return off
	}

	web := exec.NewWeb()
	// The turn, token and deadline ceilings are left at zero, which is how this
	// file states them without restating them: internal/exec owns every one of
	// those numbers and applies its own when it is handed nothing.
	//
	// THE WINDOW IS ASKED THROUGH THE ONE READING THAT NEVER WAITS ([v3StartWindow],
	// by the caller), and that is a launch-path law rather than a preference here.
	// [catalog.Catalog.ContextLength] RESOLVES the lazy catalog, and resolving it
	// on a stale cache is a GET /models with a fifteen-second ceiling
	// (internal/catalog's LoadLazy says so in as many words) — so asked on this
	// line it holds the FIRST FRAME of a conversation behind a fetch, for a
	// number no frame reads. What it configures is a subharness runner, and
	// nothing touches one until somebody runs a program minutes later.
	//
	// Zero is the honest answer while the catalog is still warming, and it is the
	// SAME zero session.Config.ContextWindow is filled with by the same call on
	// the same model a few lines later (chatv3.go): internal/exec applies its own
	// conservative default for it — "an unknown or unavailable model is zero,
	// which is not an error", [exec.Linear.WithContextLength] — exactly as
	// internal/session does for the window it was handed. One question, one
	// non-blocking answer, two readers.
	linear := exec.NewLinear(client, space, web, 0, 0, 0).
		WithAssistedBy(config.AssistedByModelAt(settings.ProfileDir, model)).
		WithContextLength(window)
	registry := exec.NewRegistry(linear)
	// THE GENERALIST IS WHAT THE DEOPTIMIZATION PATH FALLS BACK TO, so it is
	// registered before anything else can need it: a guard that does not pass
	// hands the ORIGINAL input to [exec.Registry.Generalist], and a registry
	// without one would turn "this needed a closer look" into "this could not be
	// done at all".
	//
	// This is a SMALLER leafBuild than the headless runner's and deliberately so.
	// A conversation has no job graph, no per-leaf media bundle and no fan-in
	// measurement to size a budget from — those are facts about a node in a plan,
	// and there is no plan here. What is left is what a worker actually needs to
	// run: a client, a workspace, the web, and the model the person is talking to.
	// `swarm` stays off for the reason [leafBuild.swarm] gives: only a surface
	// that settles leaves through the path that grows a graph from a division
	// request can act on the verb, and a leaf armed anywhere else would be handed
	// a verb whose answer is silence.
	registerSubharnessRunners(registry, leafBuild{
		settings: settings, client: client, workspace: space, web: web,
		model: model, models: models, window: window,
	})

	// The look every bundle's guards are checked through. One per conversation,
	// shared by every runner the stores build, because both questions it answers
	// are about this conversation rather than about any one program.
	belt := &beltWatch{}
	look := bundleLook{workspace: space.Root(), belt: belt.on}
	build := subharnessBuild(look)

	// The person's own bundles. This is the layer that always exists.
	// It is named rather than inlined because the same store answers three of the
	// four seams below: what can be reached, what each program remembers, and what
	// is known about the last time one ran.
	homeStore := substore.Home()
	registry.UseBundles(exec.LayerHome, homeStore.Source(build))
	// And the repository's, when this workspace really is one. [substore.ProjectDir]
	// is a NAME and nothing else — the directory is whatever a `git pull` left
	// there — so the question of whether there is a project at all is asked here,
	// with the same answer every other project-shaped question on this path takes
	// ([v3GitRoot]: no git, no repository and an unreadable one are one answer).
	// A repository with no such directory registers a source that lists nothing,
	// which is the correct and quiet outcome.
	//
	// FOR A BORROWED CONVERSATION THIS COSTS NO SUBPROCESS AT ALL. The workspace
	// under it IS the root the launch already resolved a moment ago
	// ([v3Workspace]), and [v3GitRoot] hands that answer straight back out of
	// [v3GitRoots] rather than spending a second `git rev-parse` learning it
	// twice. An owned session's work directory is a different repository and is
	// genuinely asked, which is the same call it always was.
	if root, ok := v3GitRoot(space.Root()); ok {
		registry.UseBundles(exec.LayerProject, substore.At(substore.ProjectReadDir(root)).Source(build))
	}

	return v3Subharness{
		Registry: registry,
		// MEMORY IS THE HOME STORE'S AND ONLY THE HOME STORE'S. What a subharness
		// has learnt is what it learnt ON THIS MACHINE, and the home store is the
		// one that is always there to hold it — a project store may be absent,
		// may be read-only, and is under version control, so a note written into
		// it would be committed into everybody else's checkout as though they had
		// learnt it too.
		Memory:  storeMemory{store: homeStore},
		LastRun: subharnessLastRun(homeStore, pages, time.Now),
		Record:  subharnessRecordRun(homeStore),
		Belt:    belt,
	}
}

// UsePages puts the page store in front of this conversation's registry — the
// third place a program can live, beside the two bundle stores above.
//
// IT IS THE SAME LIST AND NOT A SECOND ONE. A program a person had designed sat
// in that store reachable only by saying something that matched it; from here it
// is a row on `/subharness` like any other, marked `yours` like anything else of
// theirs, and run through the door every row is run through.
//
// IT IS WIRED LATE, and that is the whole reason it is a method rather than a
// line inside [v3Subharnesses]: what runs a page is the seam the surface builds
// after governance and the media pair have landed (chatv3.go), and a registry
// assembled before that would have to build a second runner for the same store.
// One store, one runner, one path in.
//
// A build with either half missing wires nothing, which is the same silence
// every other seam on this path keeps: no registry is subharnesses off, and no
// runner is a store with nothing to run its pages with.
func (s v3Subharness) UsePages(store *subharness.Store, run subharness.RunPage) {
	if s.Registry == nil {
		return
	}
	s.Registry.UseBundles(exec.LayerPages, store.Source(run))
}

// storeMemory is [substore.Store] seen through the door internal/session spells:
// the same two calls with the subharness's name in front of them.
//
// The name is the WHOLE of the scoping, which is [substore.Store.Memory]'s own
// law — a subharness's notes are per name and not per version, so learning
// survives a re-mint instead of being thrown away by it.
type storeMemory struct{ store *substore.Store }

func (m storeMemory) Remember(ctx context.Context, subharness, note string) error {
	return m.store.Memory(subharness).Remember(ctx, note)
}

func (m storeMemory) Recall(ctx context.Context, subharness, query string) ([]exec.Note, error) {
	return m.store.Memory(subharness).Recall(ctx, query)
}

// subharnessLastRun is the DIM NOTE UNDER A ROW: when this program last ran, how
// it went, and what it cost.
//
// ── TWO STORES, ONE ANSWER, AND WHY IT IS READ RATHER THAN WRITTEN ──
//
// A subharness has two doors, and until this reading merged them the two doors
// disagreed about the same program:
//
//   - Every run started through `/subharness` — the only road a bundle or a
//     compiled-in worker has — leaves a one-line note in the HOME STORE, written
//     by internal/session the moment the run lands (its subharness_run.go).
//   - Every run of a PAGE — whichever list started it — leaves a whole trace in
//     the PAGE STORE, written by the one run door this surface builds
//     ([v3RunHarness]), because that door is what `/harness` and `/subharness`
//     both end up calling.
//
// So a page run from `/harness` was invisible to `/subharness`: the note was
// never written, and the row went on saying nothing about a program that had run
// four times that afternoon.
//
// THE FIX IS ON THE READ SIDE AND DELIBERATELY SO. The obvious alternative — the
// `/harness` road also writing a home-store note — would give ONE fact TWO
// writers, and the second of them would be writing into a store it has no other
// business in. Neither store can see the other's runs (a bundle has no trace to
// save; a page's own trace is what its history IS), so each stays the authority
// for what it can actually observe and this reader asks both and answers with
// WHICHEVER IS NEWER. A run that wrote to both — a page started from
// `/subharness` — describes the same run twice and either answer is true; the
// note is the later stamp of the two and so it wins, which keeps the cost the
// trace never carried.
//
// The words are neither store's ([subharness.LastRunLine]): `/harness` draws
// this same sentence about this same program from its own trace, and one
// vocabulary is what makes the two lists one surface.
func subharnessLastRun(home *substore.Store, pages *subharness.Store, now func() time.Time) func(string) string {
	if home == nil && pages == nil {
		return nil
	}
	return func(name string) string {
		var newest subharness.LastRun
		if home != nil {
			if note, ok := home.LastRun(name); ok {
				newest = subharness.LastRun{At: note.At, Finished: note.Finished, CostUSD: note.CostUSD}
			}
		}
		if pages != nil {
			if trace, ok := pages.LastTrace(name); ok {
				if run := subharness.TraceRun(trace); run.At.After(newest.At) {
					newest = run
				}
			}
		}
		return subharness.LastRunLine(newest, now())
	}
}

// subharnessRecordRun is the write half of the note above, in the vocabulary
// internal/session hands it over in.
//
// IT IS AN ADAPTER AND NOTHING MORE. The writing itself is
// [subharnessRunRecorder]'s, so a run started in a conversation and a run started
// from the command line leave the same note in the same place — the headless
// command cannot reach [session.SubharnessRunNote] at all (it may not import the
// task system), and two writers would be two answers to "when did this last run".
func subharnessRecordRun(store *substore.Store) func(string, session.SubharnessRunNote) {
	write := subharnessRunRecorder(store)
	if write == nil {
		return nil
	}
	return func(name string, note session.SubharnessRunNote) {
		write(name, substore.RunNote{
			At: note.At, Finished: note.Finished, Why: note.Why, CostUSD: note.CostUSD,
		})
	}
}
