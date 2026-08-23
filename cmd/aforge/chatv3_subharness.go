package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/substore"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
)

// THE SUBHARNESS SIDE OF ONE CONVERSATION: the programs it can reach, where they
// keep what they learn, and what it says about the last time each of them ran.
//
// It is assembled here for the reason every other registry on this path is
// (chatv3_harness.go): where the bundles live is the SURFACE'S decision, and a
// package that opened ~/.aforge/subharnesses itself would open it from a test and
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
func v3Subharnesses(settings config.Config, models *catalog.Catalog, model, workspace string) v3Subharness {
	off := v3Subharness{}
	space, err := exec.NewWorkspace(strings.TrimSpace(workspace))
	if err != nil {
		return off
	}
	// The client is built the way every other client on this path is
	// (chatv3_harness.go, and internal/session's own agent.go): a plain adapter
	// rather than [config.Config.Client]'s router. The router keeps a ledger that
	// has to be flushed on the way out, and this surface has nowhere to hang that
	// close — a conversation's registry lives as long as the process does.
	//
	// The timeout is the harness node's, interpolated rather than restated: both
	// are a backstop against a wedged endpoint on a non-streamed call, and a
	// second number here would be the one that drifts.
	client, err := provider.NewClient(provider.Config{
		APIKey:         settings.APIKey,
		BaseURL:        settings.BaseURL,
		Model:          model,
		Timeout:        harnessTimeout,
		SiteURL:        settings.SiteURL,
		SiteName:       settings.SiteName,
		SiteCategories: settings.SiteCategories,
	})
	if err != nil {
		return off
	}

	web := exec.NewWeb()
	// The turn, token and deadline ceilings are left at zero, which is how this
	// file states them without restating them: internal/exec owns every one of
	// those numbers and applies its own when it is handed nothing.
	linear := exec.NewLinear(client, space, web, 0, 0, 0).
		WithAttribution(settings.Attribution).
		WithContextLength(models.ContextLength(model))
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
		model: model, models: models,
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
	if root, ok := v3GitRoot(space.Root()); ok {
		registry.UseBundles(exec.LayerProject, substore.At(substore.ProjectDir(root)).Source(build))
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
		LastRun: subharnessLastRun(homeStore, time.Now),
		Record:  subharnessRecordRun(homeStore),
		Belt:    belt,
	}
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

// subharnessLastRun is the DIM NOTE UNDER A ROW, and rendering it is this
// surface's job rather than the store's: [substore.RunNote] keeps the facts and
// says outright that it renders nothing, because the emptiness law, the word for
// an unfinished run and how a cost is drawn are all decided here and in one
// place.
func subharnessLastRun(store *substore.Store, now func() time.Time) func(string) string {
	if store == nil {
		return nil
	}
	return func(name string) string {
		note, ok := store.LastRun(name)
		if !ok {
			return ""
		}
		return subharnessRunLine(note, now())
	}
}

// subharnessRunLine is that note as one short line: when, how it went, and what
// it cost.
//
// NO HISTORY IS THE EMPTY STRING. Never "never run", never "0 runs" — the
// emptiness law, and a row with nothing to say says nothing. A zero cost is not
// drawn for the same reason: a provider that published no figure left "nobody
// said" behind and not "free".
//
// THE WORD FOR A RUN THAT DID NOT FINISH IS "incomplete". It is one of the five
// sanctioned words for the state of work and nothing here may call it a failure.
// The run's own sentence about what ran out is NOT on this line: it was written
// to be read in full, this is one dim line under a name, and a row that wrapped
// would cost the list the scannability the note exists for.
func subharnessRunLine(note substore.RunNote, now time.Time) string {
	when := reltime.Short(note.At, now)
	if when == "" {
		return ""
	}
	parts := []string{when, "incomplete"}
	if note.Finished {
		parts[1] = "finished"
	}
	if note.CostUSD > 0 {
		parts = append(parts, subharnessCost(note.CostUSD))
	}
	return strings.Join(parts, " · ")
}

// subharnessCost is what a run cost, in the cells a dim row can spare. Cents
// while a run is cheap, so a program that spent a fraction of one is not drawn as
// though it had spent nothing — which is the same ladder the status line's own
// reading takes, minus its zero rung: a zero never reaches here, because a cost
// nobody reported is not drawn at all.
func subharnessCost(usd float64) string {
	if usd < 0.01 {
		return fmt.Sprintf("$%.4f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
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
