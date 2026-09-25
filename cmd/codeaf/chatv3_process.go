package main

// chatv3_process.go is what a v3 process owns once and what one conversation
// borrows from it.
//
// The split exists because [openV3Launch] used to be BOTH. Every call loaded the
// model catalog, opened the sub-harness registry, opened the machine's chat
// database and resolved the deliverables index — which was correct while a
// process opened exactly one conversation and is wrong the moment it can open a
// second. Calling it twice would leak stores, repeat the catalog warm, and re-run
// a boot pass that is documented as never repeated.
//
// So there are two objects and the line between them is one question: does the
// answer depend on WHICH DIRECTORY the conversation is about?
//
//   - [v3Process] — no. The profile, the catalog, the harness registry, the
//     memory store, the deliverables index, the accounts manager, the input
//     history file, and where the person was standing when the process started.
//     Built once, in [openV3Process], and handed to every launch.
//   - [v3Launch] — yes. The session folder, the gate, the roles, the spend rail,
//     the context window, and whether this directory keeps a draft or a history
//     at all. Built per workspace, in [openV3Launch].
//
// And on top of both, [v3Seam] — the two closures the surface opens
// conversations through (internal/tui3's Open and Start), which is where the
// per-agent closures are minted so that a conversation and the seams that
// answer for it can never come apart.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/connect"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/history"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/subharness"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// v3Process is everything a launch may borrow but must not build twice.
type v3Process struct {
	// Settings is the one profile every governance row is resolved out of, and
	// ProfileDir the directory the settings panel writes back into. They must be
	// the same one: with CODEAF_PROFILE_DIR set, a panel writing ~/.codeaf while
	// the session read the named profile is a gate turned off in the sheet that
	// stays on with nothing on screen saying why.
	Settings          config.Config
	ProfileDir        string
	UnreadProfileKeys []string
	// Models is ONE lazy warm and one cache on disk. N catalogs would be N
	// network round trips for one answer.
	Models *catalog.Catalog
	// processCtx is the lifetime shared by background work owned by this process.
	// processStop closes that lifetime before closeAll joins each owned worker.
	processCtx  context.Context
	processStop context.CancelFunc
	// Shelf holds Models until somebody asks /model for today's list, and the
	// refreshed catalog after (chatv3_modelshelf.go). The picker and the two
	// session readers that answer about a model somebody may have just picked
	// out of that list — can it see, may a task be handed to it — read here.
	Shelf *v3ModelShelf
	// serviceNotices are the surfaces to tell when a provider's listing lands.
	// Appended by each launch that opens the surface, never read on the draw
	// path.
	serviceNotices []func(string, string)
	// Harnesses is the registry under the state root. The law is already written
	// at [openV3Launch]: two stores at one directory is how /harness and the
	// offer card come to name different harnesses.
	Harnesses *subharness.Store
	// Memory is the machine's chat database, and it MUST be one. [store.Open]
	// builds a SQLite handle with an eight-connection pool, and every write goes
	// through a begin that the driver strips the caller's context off — a
	// transaction that loses the race waits the full busy timeout and cannot be
	// cancelled (internal/store's writelock.go, busyWait 10s). Two *Store values
	// on one file in one process are two pools with no in-process lock between
	// them, so the only thing arbitrating their writes would be that timeout.
	//
	// Memory is legitimately PROFILE-scoped, which is why one handle is enough:
	// the memory row is not in [config.ProjectKeys], so no workspace has its own
	// answer to give. Each conversation still gets its own memory pass and its
	// own context, which is per-agent already.
	Memory *store.Store
	// Skills is the skill shelf every conversation this process opens reads:
	// the Memory store itself when memory is on, and otherwise a store of its
	// own that holds nothing but the skills the folders on disk hold
	// ([v3SkillShelf]). It is profile-scoped for Memory's reason, and one
	// handle for its reason too.
	Skills *store.Store
	// skillsDir is the folder the memory-off shelf lives in, removed with it
	// at close; empty when the shelf is the Memory store.
	skillsDir string
	// Artifacts is the deliverables index — one file per machine, and /export
	// and /files must resolve the same one the session's own products record
	// themselves in.
	Artifacts string
	// Conns is the accounts manager. One per process because an account
	// connected on the panel is connected for every conversation's belt in the
	// same breath, and because a token one manager refreshes is a token the
	// other would not know had moved.
	Conns *connect.Manager
	// LaunchDir is where the person was standing when the process started,
	// captured ONCE. The process never changes directory (`codeaf engine` is the
	// one door that does, and it does it before it builds any of this), so one
	// capture is the honest one — and a conversation opened later into another
	// project is still a conversation this door opened from here.
	LaunchDir string

	// mu guards everything below: the lazily opened history file and the list of
	// agents this process has built. Both are touched from the surface's
	// goroutine and from the door's defer, which are not the same one.
	mu              sync.Mutex
	recall          *history.Store
	agents          []*session.Agent
	standingStarted bool
	standingStop    chan struct{}
	standingDone    chan struct{}
	sweepCancel     context.CancelFunc
	sweepDone       chan struct{}
	// catalogs are the lazy catalogs this process opened beside Models — a
	// direct service's own listing, asked for when a conversation is opened on
	// one of its models ([v3Process.ownCatalog]). closeAll cancels and joins each.
	catalogs []*catalog.Catalog
	closed   bool
	// creditWatcher owns the balance reads shared by all conversations.
	creditWatcher *v3CreditWatcher
}

// lifetime is the context background work owned by this process runs under,
// and it ends when closeAll begins. A process built without one (a test's bare
// literal) hands out the plain background, which is what it had before.
func (p *v3Process) lifetime() context.Context {
	if p == nil || p.processCtx == nil {
		return context.Background()
	}
	return p.processCtx
}

// ownCatalog hands a lazy catalog this process opened to closeAll, which
// cancels and joins its warm the way it does Models' (#1274). A catalog handed
// over after the close has begun is closed at once, so none is left unowned.
func (p *v3Process) ownCatalog(models *catalog.Catalog) {
	if p == nil || models == nil {
		return
	}
	if !p.keepCatalog(models) {
		models.Close()
	}
}

// keepCatalog files one catalog for closeAll, and answers false when the close
// has already begun and nothing will come back for it.
func (p *v3Process) keepCatalog(models *catalog.Catalog) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.catalogs = append(p.catalogs, models)
	return true
}

// takeCatalogs hands over every catalog [v3Process.ownCatalog] was given and
// empties the list, so the joins run after the lock is let go.
func (p *v3Process) takeCatalogs() []*catalog.Catalog {
	p.mu.Lock()
	defer p.mu.Unlock()
	owned := p.catalogs
	p.catalogs = nil
	return owned
}

// openV3Process builds the once-only half of a v3 launch.
//
// `door` is the word the missing-key sentence names, and it moved here from
// [v3Options] because this is now the first thing that reads the profile and
// therefore the first thing that can fail for the want of a key. Empty is
// "chat", which is what both terminal doors say — `codeaf resume` has always
// said it and says it still, because what it could not open is a chat.
func openV3Process(door string) (*v3Process, error) { return openV3ProcessWith(door, false) }

// openV3ProcessWith is [openV3Process] with the one thing a door knows that
// the process does not: whether somebody is sitting at this terminal who can
// be ASKED for a key.
//
// `askKey` true is an interactive local chat — a person, a TTY, no --once and
// no --host — and it opens the process with no key at all when none is found,
// so the surface can connect the default OpenRouter provider in a browser
// (internal/tui3's firstrun.go). Every other door keeps the refusal: nobody is
// there to finish the browser trip, and a process that opened keyless would fail
// on its first request instead of at the door where the sentence can be read.
func openV3ProcessWith(door string, askKey bool) (*v3Process, error) {
	settings, err := config.Load()
	if err != nil && askKey && errors.Is(err, config.ErrNoAPIKey) {
		// A keyless process is useful only when the surface can answer it. The
		// browser flow mints an OpenRouter key, so a custom OpenAI-compatible
		// endpoint keeps the explicit-key refusal instead of opening a session
		// whose very first model call is guaranteed to fail.
		if keyless, loadErr := config.LoadKeyless(); loadErr == nil && v3UsesDefaultOpenRouter(keyless) {
			settings, err = keyless, nil
		}
	}
	if err != nil {
		if strings.TrimSpace(door) == "" {
			door = "chat"
		}
		fmt.Fprintln(os.Stderr, "codeaf "+door+" needs a model to talk with.")
		if keyless, loadErr := config.LoadKeyless(); loadErr == nil && v3UsesDefaultOpenRouter(keyless) {
			fmt.Fprintln(os.Stderr, "run `codeaf` in a terminal to connect OpenRouter, or export "+config.APIKeyEnv+" (or OPENAI_API_KEY) and run it again.")
		} else {
			fmt.Fprintln(os.Stderr, "export "+config.APIKeyEnv+" (or OPENAI_API_KEY) and run it again.")
		}
		return nil, err
	}
	// AND THE BACKGROUND CHECKS ARE PUT BACK IF THEY DRIFTED, once per process,
	// in the background, saying nothing on screen (chatv3_standing.go). A timer
	// naming a program that has moved is a timer that runs nothing, and this is
	// the one moment this build knows where the program actually is.
	startBackgroundRepair(settings.ProfileDir)
	// WHERE THE PERSON IS STANDING, which is not the same fact as which project
	// this is: `codeaf` typed in repo/cmd/ is a conversation about the
	// repository, and the subdirectory is recorded rather than resolved away
	// (Decision 26).
	launchDir, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolve working directory: %w", err)
	}
	// ONE BOOT PASS, before anything reads the new layout, so a machine that
	// last ran the flat layout opens on its own conversations rather than on an
	// empty list (chatv3_migrate.go). It is never fatal and never repeated —
	// which is a promise this file is now the keeper of.
	migrateV3Layout()
	// Model discovery starts here and is waited for NOWHERE. On a cold cache
	// resolving it is a network round-trip, and everything it feeds has a good
	// answer without it.
	discovery := catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: settings.ProfileDir,
		HTTPClient: config.CatalogHTTPClient(settings.Sources.Default()),
	}
	processCtx, processStop := context.WithCancel(context.Background())
	models := catalog.LoadLazy(processCtx, discovery)
	// The crew router picks its seats from this catalog (config.CrewCatalog):
	// the same non-blocking read, never a fetch, and set once at start-up.
	seatCrewRows(models.ModelsNow)
	wirePoolIndex(settings.ProfileDir)
	shelf := newV3ModelShelf(models, discovery)
	shelf.setSources(settings.Sources)
	process := &v3Process{
		Settings:          settings,
		ProfileDir:        settings.ProfileDir,
		UnreadProfileKeys: append([]string(nil), settings.UnreadProfileKeys...),
		Models:            models,
		processCtx:        processCtx,
		processStop:       processStop,
		Shelf:             shelf,
		Harnesses:         subharness.Default(),
		Memory:            v3Memory(settings.ProfileDir),
		Artifacts:         artifactsIndexPath(),
		Conns:             v3Connect(settings.ProfileDir),
		LaunchDir:         launchDir,
	}
	process.Skills, process.skillsDir = v3SkillShelf(process.Memory)
	process.creditWatcher = newV3CreditWatcher(process)
	process.startPlaceSweep()
	return process, nil
}

// v3UsesDefaultOpenRouter identifies the one endpoint the browser flow can
// authenticate. A harmless trailing slash and URL case do not turn the built-in
// address into a custom provider.
func v3UsesDefaultOpenRouter(settings config.Config) bool {
	normalize := func(raw string) string { return strings.TrimRight(strings.TrimSpace(raw), "/") }
	return strings.EqualFold(normalize(settings.BaseURL), normalize(config.DefaultBaseURL))
}

// history is the process's ONE input-history file, opened on the first
// conversation whose workspace says it keeps one.
//
// It is lazy rather than opened at boot because the row is answered per
// workspace ([config.KeyHistoryEnabled] is in the project layer) and "history
// off" has always meant no file and no goroutine at all. One store rather than
// one per conversation because the file is one file: every entry carries the
// directory it was typed in, and the surface asks for its own directory's
// (internal/tui3's recall.go).
//
// A nil is returned as a plain nil interface and never as a typed one, because
// the surface tests its recall door with a bare nil check.
func (p *v3Process) history() tui3.History {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	if p.recall == nil {
		dir, err := v3Dir()
		if err != nil {
			// A history file that cannot be opened costs the up arrow and
			// nothing else, which is why this is dropped rather than returned.
			return nil
		}
		p.recall = history.New(filepath.Join(dir, "history.jsonl"))
	}
	return p.recall
}

// track records an agent this process built, so [v3Process.closeAll] can close
// it however the surface returns.
func (p *v3Process) track(agent *session.Agent) {
	if agent == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents = append(p.agents, agent)
}

// forget drops a conversation this process tracked, once that conversation has
// been closed by whoever owns it.
//
// IT IS THE PAIR OF [v3Process.track] AND THE REASON TRACKING IS SAFE ON A
// LONG-LIVED PROCESS. A surface tracks the handful of conversations one
// terminal opens and goes away with them; an engine process outlives every
// conversation it serves, so without this the list would grow for as long as
// the daemon runs, and every broadcast — [v3Process.setModelSources],
// [v3Process.setAPIKey] — would walk agents that ended hours ago.
//
// The match is POINTER IDENTITY, because two conversations on one workspace are
// two agents with the same everything else. Removing is order-preserving: the
// list is small and the boot conversation being first in it is what makes
// [v3Process.closeAll] read in the order the person opened them.
func (p *v3Process) forget(agent *session.Agent) {
	if agent == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := p.agents[:0]
	for _, held := range p.agents {
		if held != agent {
			kept = append(kept, held)
		}
	}
	// The tail is cleared rather than left pointing at what was dropped: the
	// slice keeps its array, and a retained agent there would outlive the
	// conversation for as long as this process holds the list.
	for i := len(kept); i < len(p.agents); i++ {
		p.agents[i] = nil
	}
	p.agents = kept
}

// setAPIKey is the key arriving after the door: the first-run screen or the
// settings row handed one over, and every conversation this process holds —
// the one on screen and any kept behind it — starts talking with it on its
// next request. The process's own settings take it too, so a conversation
// opened later (/new, the picker, home) is built with the key rather than with
// the empty one the boot found ([v3Seam.launch] reads it back from here).
func (p *v3Process) setAPIKey(key string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Settings.APIKey = strings.TrimSpace(key)
	p.Settings.Sources = p.Settings.Sources.WithDefaultKey(p.Settings.APIKey)
	for _, agent := range p.agents {
		if err := agent.SetAPIKey(key); err != nil {
			return err
		}
	}
	return nil
}

// setModelSources makes a profile connection live for every retained
// conversation and for launches opened later in this process.
func (p *v3Process) setModelSources(sources modelsource.Set) {
	if sources.Empty() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Settings.Sources = sources
	if p.Shelf != nil {
		p.Shelf.setSources(sources)
	}
	for _, agent := range p.agents {
		agent.SetSources(sources)
	}
}

// refreshAllModels is [tui3.Options.RefreshAllModels]: ctrl+r in /model walks
// the router's catalog AND every connected provider's listing (issue #1508).
// One provider's refusal never stops the walk: each fetch is its own call and
// its own error, and the group that could not list names its own reason
// ([v3ModelShelf.fetchErrors]). Runs as a command off the event loop.
func (p *v3Process) refreshAllModels(ctx context.Context) {
	if p == nil || p.Shelf == nil {
		return
	}
	if _, _, err := p.Shelf.refresh(ctx); err != nil {
		// The default provider's own refusal is recorded on the shelf like any
		// other: nothing here writes to a terminal that is not ours to write.
		p.Shelf.fetchError(modelsource.DefaultID, err)
	} else {
		p.Shelf.fetchError(modelsource.DefaultID, nil)
	}
	for _, fetch := range p.Shelf.warmAll(ctx, false) {
		if fetch.err == nil {
			p.noteServiceModels(fetch.service)
		}
	}
}

// warmEmptyProviders is [tui3.Options.WarmEmptyProviders]: the launch half of
// issue #1508. Every connected provider that lists models and whose cache file
// is missing or empty is fetched once, off the loop, through the connect path's
// own door. It is called after setSources has filled what the caches could,
// so a warm provider costs nothing and a cold one fills its group without a
// reopen.
func (p *v3Process) warmEmptyProviders(ctx context.Context) {
	if p == nil || p.Shelf == nil {
		return
	}
	for _, fetch := range p.Shelf.warmAll(ctx, true) {
		if fetch.err == nil {
			p.noteServiceModels(fetch.service)
		}
	}
}

// onServiceModelsLanded is the closure a launch hands its surface as
// [tui3.Options.OnServiceModels]: the process fans the news out and nothing
// here needs the surface's own app. The fan-out registers nothing per launch —
// [v3Process.noteServiceModels] walks the doors the process retained — so a
// second window in this process hears its own provider news the same way.
func (p *v3Process) onServiceModelsLanded(source, address string) {
	p.noteServiceModelsTo(source, address)
}

// registerServiceNotice adds one surface's door to the fan-out. The launch
// calls it with the closure its surface answered [tui3.Options.OnServiceModels]
// with, so a fetch that lands in THIS process — a launch warm or a ctrl+r walk
// in another window's conversation — reaches every open picker without a
// reopen. A closed surface takes itself off the list.
func (p *v3Process) registerServiceNotice(tell func(source, address string)) func() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.serviceNotices = append(p.serviceNotices, tell)
	at := len(p.serviceNotices) - 1
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.serviceNotices[at] = nil
	}
}

// noteServiceModelsTo is one surface's slice of the news: the drop of its memo
// and the restock of its open picker happen on ITS loop, through the callback
// the surface itself supplied — which is the only side allowed to touch the
// app's memos.
func (p *v3Process) serviceNoticesSnapshot() []func(string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]func(string, string){nil}, p.serviceNotices...)
}

func (p *v3Process) noteServiceModelsTo(source, address string) {
	notify := p.serviceNoticesSnapshot()
	for _, tell := range notify {
		if tell != nil {
			tell(source, address)
		}
	}
}

// noteServiceModels tells every live surface one provider's listing changed,
// so its memo is dropped and an open picker restocks. The process holds the
// launch doors; each registers itself here when it opens the surface.
func (p *v3Process) noteServiceModels(service modelsource.Connected) {
	p.noteServiceModelsTo(service.Source.ID, service.Address)
}

// refreshModelSources re-reads this process's own profile and makes that
// answer live in every conversation it retains.
//
// The surface on the linked-local road writes a newly connected service into
// the same profile, but it cannot push an address or key into this process
// without inventing a second authority and a new wire message. Re-resolving at
// the engine keeps key-environment precedence and profile writes behind
// [config.ResolveSources], the one door that already owns them.
func (p *v3Process) refreshModelSources() {
	if p == nil {
		return
	}
	profileDir, key, base := p.sourceSeeds()
	p.setModelSources(config.ResolveSources(profileDir, key, base))
}

// sourceSeeds is the snapshot [config.ResolveSources] is run from. It is its
// own method so the lock is let go before the resolve, which re-reads the
// profile from disk and re-takes the mutex through [v3Process.setModelSources].
func (p *v3Process) sourceSeeds() (profileDir, key, base string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ProfileDir, p.Settings.APIKey, p.Settings.BaseURL
}

func (p *v3Process) currentAccount() (string, modelsource.Set) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Settings.APIKey, p.Settings.Sources
}

// takeForClose marks the process closed and hands over what is left to close,
// or answers false when a previous call already took it. It is its own method
// so the mutex is held from a defer while the closes, which are slow and
// re-enter the process, happen outside it.
func (p *v3Process) takeForClose() (agents []*session.Agent, recall *history.Store, first bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil, nil, false
	}
	p.closed = true
	agents, recall = p.agents, p.recall
	p.agents, p.recall = nil, nil
	return agents, recall, true
}

// warmModels seats the model warm on the profile's start-up errand tracker, so
// [v3Process.closeAll] cancels and waits for it exactly as it does the pool
// errands (poolindex.go's [poolErrandGoCtx]).
//
// A WARMER IS A WRITER, AND [guard.Go] JOINS NOTHING. The warm resolves the
// model catalog — a network round-trip on a cold cache — and then writes the
// picker's cache through [tui3.WriteModelCache], which resolves CODEAF_HOME AT
// THE MOMENT IT WRITES. Started fire-and-forget, a warm that outlives the
// conversation that asked for it lands in whichever state root is current when
// the rows arrive: in a test, the next test's own TempDir, whose clean-up then
// fails with `directory not empty`; on a door that reopens on another profile, a
// directory the process no longer owns. Seating it on the tracker is what makes
// closeAll's own promise — that nothing this process started is still writing
// under its profile once it closes — true for this writer too. The context the
// tracker hands the errand is what lets the warm's own wait end at the close.
func (p *v3Process) warmModels(scope string, agent *session.Agent, started string) {
	// THE WARM IS ALWAYS THE PROCESS'S OWN CATALOG, the default service's, and
	// that is why no caller hands it one. It writes the default service's model
	// cache from what it warmed ([warmV3Models]), so a caller that passed the
	// catalog its conversation STARTED on — which is a direct service's the
	// moment the conversation opens on `codex/gpt-5.5`, as every launch does once
	// a Codex sign-in has saved that as the chat model — wrote Codex's bare ids
	// into the OpenRouter list. Both the engine road and the in-process road
	// handed it the launch's catalog until #1383.
	models := p.Models
	poolErrandGoCtx(p.ProfileDir, scope, func(ctx context.Context) {
		warmV3Models(ctx, models, agent, started)
	})
}

// closeAll closes every conversation this process opened and then the stores
// they shared. It is IDEMPOTENT and it is the door's defer.
//
// WHAT IT IS FOR. The outer defer used to own exactly one agent — the boot one —
// so a [tea.Program] that returned by any road other than the surface's own quit
// leaked every conversation opened after boot: their session-file flocks, their
// presence heartbeats, their jobs. Every window on the machine would go on
// reading those transcripts as held by another window until this process exited.
//
// THE ORDER IS THE POINT. Agents first, in parallel — each one's Close is
// bounded on every axis and the phases inside it are sequential, so N of them
// run in the time of the slowest rather than the sum. THEN the shared stores,
// and the memory database LAST: an agent's memory pass may still be writing when
// Close begins, its own Close waits for that pass, and a database shut before
// that wait would be a write into a closed handle. That store is also the one
// with the ten-second busy wait behind it (internal/store's writelock.go:40-50),
// so closing it under a live writer is not a fast error but a long one.
func (p *v3Process) closeAll() {
	agents, recall, first := p.takeForClose()
	if !first {
		return
	}

	// Cancel the process lifetime first, then join the catalog warm. Catalog.Close
	// also cancels its derived context, and its join is what makes the promise
	// that no cache write can happen after closeAll returns airtight.
	if p.processStop != nil {
		p.processStop()
	}
	if p.creditWatcher != nil {
		p.creditWatcher.close()
	}
	if p.Models != nil {
		p.Models.Close()
	}
	// AND EVERY OTHER CATALOG THIS PROCESS OPENED, for the same promise: a direct
	// service's own listing warms under the same lifetime and is joined here.
	for _, models := range p.takeCatalogs() {
		models.Close()
	}

	// Cancellation is checked between entries and before destructive operations.
	// Joining therefore waits only for the current bounded filesystem operation,
	// and guarantees the sweep cannot rename or remove after close returns.
	p.stopPlaceSweep()

	// The start-up errands this process seated on its profile — the pool index
	// refresh and the outbox push — were started fire-and-forget.
	// [stopPoolErrands] cancels them and waits, so nothing this process started
	// is still writing under its profile once it closes (poolindex.go states
	// the seam). It runs first, before the conversations and the stores, because
	// it is the process's own errand and not a conversation's.
	stopPoolErrands(p.ProfileDir)

	// Stop the standing clock before closing anything it may borrow. Waiting
	// for its loop also waits for a pass already in flight, so no standing
	// writer can outlive this process close.
	p.stopStandingTicks()

	var waiting sync.WaitGroup
	for _, agent := range agents {
		waiting.Add(1)
		// THE CLOSE IS SPAWNED UNDER THE GUARD, like every other fire-and-forget
		// goroutine in this tree (internal/guard's sweep_test.go states the law).
		// It matters most exactly here: this runs while the process is on its way
		// out, and one agent whose Close faults would take down the surface
		// mid-teardown — with the Wait below never returning, since the panic
		// would carry the deferred Done away with the goroutine. Under
		// [guard.Go] the fault is noted, Done still runs, and the remaining
		// conversations still get closed.
		guard.Go("chatv3/close-agent", func() {
			defer waiting.Done()
			// The error is dropped for the reason the door's defer always
			// dropped it: nothing is left to say it to, and a session file that
			// would not flush is not a reason to hold the terminal.
			_ = agent.Close()
		})
	}
	waiting.Wait()

	// The ledger's background writer is drained AFTER the agents have closed,
	// because closing an agent can seal a last turn and a seal records a line.
	// It is the same bargain the recall store's Close makes one line below: a
	// queue written on the way out, so the last thing a person did is on disk
	// before the terminal comes back.
	session.CloseUsage()

	if recall != nil {
		_ = recall.Close()
	}
	if p.Memory != nil {
		_ = p.Memory.Close()
	}
	// The memory-off shelf goes with the process that built it: it was only
	// ever a reading of the skill folders, and the next launch reads them
	// again.
	if p.skillsDir != "" {
		if p.Skills != nil {
			_ = p.Skills.Close()
		}
		_ = os.RemoveAll(p.skillsDir)
	}
}

// ── the agent-building seam ─────────────────────────────────────────────────

// v3Seam is what internal/tui3's Open and Start are built on: the process's
// shared resources, the launch this door already made, and the flags the command
// line said once.
//
// It exists so that the two closures are one piece of code with one branch in it
// — a workspace the caller named, or this door's own — instead of four closures
// that would drift about which of eleven per-conversation answers they
// remembered to fetch.
type v3Seam struct {
	proc *v3Process
	// boot is the launch this door opened on, and it is what a conversation
	// with no workspace of its own borrows: /new and the resume picker are
	// asking about the project this window is already in.
	boot *v3Launch
	// seed is the command line's own say — the model, --no-compact, --yolo —
	// carried so that a conversation opened an hour later is the same launch
	// this one is rather than a second one quietly drifting away from it.
	seed v3Options
}

// v3GoneWord is what a door says about a workspace that is not there.
//
// IT IS THE SURFACE'S OWN SENTENCE rather than a second spelling of it. Home
// stats the workspace on the keystroke and refuses there; this catches a
// directory that disappeared between that stat and this open, and a person must
// not be able to tell which of the two answered.
const v3GoneWord = tui3.WorkspaceGoneWord

// v3OpenTarget canonicalises a workspace a caller named, and refuses one that is
// not a directory.
//
// EMPTY MEANS THIS DOOR'S OWN and is not checked, which is what /new and the
// picker pass: there is no second directory to resolve, and re-resolving the one
// the launch already settled would be this file second-guessing it.
//
// A NAMED ONE IS STATTED HERE even though home stats it on the keystroke
// (internal/tui3's home.go), because this seam is also reachable from /resume and
// from a flag, and because a folder can be removed between the keystroke and the
// open. Canonical means [filepath.EvalSymlinks] then [filepath.Clean] — the same
// identity every other door in this wave will compare against, so two spellings
// of one directory are one workspace and not two.
func v3OpenTarget(workspace string) (string, error) {
	named := strings.TrimSpace(workspace)
	if named == "" {
		return "", nil
	}
	resolved, err := filepath.EvalSymlinks(named)
	if err != nil {
		return "", fmt.Errorf("%s · %s", v3GoneWord, named)
	}
	resolved = filepath.Clean(resolved)
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("%s · %s", v3GoneWord, named)
	}
	return resolved, nil
}

// launch resolves the launch a conversation runs on: this door's own when the
// caller named no workspace, and a fresh one — through the SAME governance every
// other launch goes through — when it named another project.
func (s *v3Seam) launch(workspace string) (*v3Launch, error) {
	target, err := v3OpenTarget(workspace)
	if err != nil {
		return nil, err
	}
	launch := s.boot
	if target != "" {
		opts := s.seed
		opts.Workspace = target
		if launch, err = openV3Launch(s.proc, opts); err != nil {
			return nil, err
		}
	}
	// THE KEY IS THE ONE FIELD READ BACK FROM THE PROCESS rather than from the
	// launch. A boot that opened keyless and was handed a key on the first
	// screen ([v3Process.setAPIKey]) still carries the empty key it was assembled
	// with, and a /new built from it would open a conversation that refuses
	// every request. A copy is patched rather than the boot itself, because the
	// boot is shared and this is a reading, not a change to it.
	key, sources := s.proc.currentAccount()
	current := *launch
	current.Config.APIKey = key
	current.Config.Sources = sources
	launch = &current
	return launch, nil
}

// start mints a fresh conversation: a new session folder in the workspace's own
// bucket, and an agent opened on it.
func (s *v3Seam) start(workspace string) (tui3.Conversation, error) {
	launch, err := s.launch(workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	launch = v3FreshDefault(launch, s.seed.Model)
	place, err := v3NextSession(launch.Place, launch.Workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	// The launch's config, pointed at the new folder and carrying the gate AS IT
	// STANDS NOW rather than as it stood at boot (chatv3_approval.go says why the
	// second half is not optional).
	cfg := v3CurrentGate(launch.Config, launch.Workspace, s.proc.ProfileDir, s.seed.Yolo)
	cfg, err = v3PointAt(cfg, place)
	if err != nil {
		return tui3.Conversation{}, err
	}
	return s.open(launch, cfg, false)
}

// resume opens a transcript somebody picked, on the launch its workspace
// answers for.
func (s *v3Seam) resume(workspace, transcript string) (tui3.Conversation, error) {
	launch, err := s.launch(workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	// THE GATE IS THE ONE THING THAT IS NOT A PROPERTY OF THE LAUNCH. It was one
	// only while nothing could change it mid-session; now that a banked rule can,
	// an old conversation reopened afterwards has to open behind the rule and not
	// behind the boot.
	cfg := v3CurrentGate(launch.Config, launch.Workspace, s.proc.ProfileDir, s.seed.Yolo)
	cfg, err = v3Reopen(cfg, transcript, launch.Workspace)
	if err != nil {
		return tui3.Conversation{}, err
	}
	return s.open(launch, cfg, true)
}

// open is the half both of the two above end in: the agent, and the bundle of
// per-agent seams minted around it.
//
// ROLLBACK. The agent takes the session-file flock and starts a presence
// heartbeat as it opens (internal/session's sessionfile.go and taskpresence.go),
// and the steps after it can still fail — the project layer's two rows are read
// off the disk. If any of them does, the partial agent is CLOSED before the
// error travels, which releases the lock and removes the presence file. Without
// it a failed open leaves a transcript that every window on the machine reads as
// held by another window until this process exits.
func (s *v3Seam) open(launch *v3Launch, cfg session.Config, resumed bool) (tui3.Conversation, error) {
	// There is a surface, and it answers (internal/tui3's consent.go). Every
	// conversation this seam opens is one somebody is looking at.
	cfg.AskConsent = true
	// And it holds every standing lane for every one of them: the surface opens
	// those subscriptions again on each conversation it takes (internal/tui3's
	// switcher.go), which is what lets chat offer a saved program with an intake
	// card — and design a harness, and run adaptively — here as well as in the
	// first conversation of the process. The lanes are asked for the same way
	// the boot conversation asks (chatv3_lanes.go), so a conversation opened by
	// /new is not a lesser one than the conversation it replaced.
	cfg, open := v3Shape(cfg, v3LanesHere())
	// The PROJECT and not the tools root: the sentence a held journal answers
	// with names the directory a host is keyed by ([v3Launch.Project]).
	agent, cfg, notice, err := openV3Agent(cfg, launch.Project, open)
	if err != nil {
		return tui3.Conversation{}, err
	}
	s.proc.track(agent)
	if notice != "" {
		// The session file moved under us, so this is a new conversation rather
		// than the one that was asked for, and the sentence says so.
		resumed = false
	}
	conv, err := s.bundle(agent, launch, cfg, resumed, notice)
	if err != nil {
		_ = agent.Close()
		return tui3.Conversation{}, err
	}
	return conv, nil
}

// v3Live is what a conversation's seams are minted around: the surface's own
// view of a session, and the one door onto the gate that session is behind.
// *session.Agent is the only thing in this tree that satisfies it.
//
// IT IS AN INTERFACE SO THE BINDING CAN BE READ. Which conversation a banked rule
// reaches is the whole of what went wrong here, and internal/session keeps the
// standing gate private — correctly, since every read of it has to go through one
// lock. Naming the two halves the bundle actually uses lets a test hold the
// receiving end and say which conversation was written to, without that package
// growing a reader for the sake of a test.
type v3Live interface {
	tui3.Agent
	v3Gate
}

// bundle is the per-conversation half of the seam: the agent, where it works,
// what its project lets it keep, and the three approval closures BOUND TO THAT
// AGENT.
//
// The binding is the whole reason this function exists rather than a struct
// literal at the door. The trio used to be built once at boot around the boot
// agent, so an "always" answered after a /new or a resume was written to the
// profile and then pushed into a session that had already been closed: saved,
// said to be saved, and not in force until the next launch, with nothing on
// screen saying so. Minted here, they cannot outlive the agent they are about.
func (s *v3Seam) bundle(agent v3Live, launch *v3Launch, cfg session.Config,
	resumed bool, notice string) (tui3.Conversation, error) {
	workspace := cfg.Workspace
	profileDir := s.proc.ProfileDir
	// The two things this surface keeps on the person's behalf rather than the
	// session's, both read THROUGH THE PROJECT LAYER: a repository that says
	// "record nothing from this directory" is answering about ITS directory,
	// which is the whole reason the layer is per-workspace and the whole reason
	// these two are per conversation rather than per launch.
	keepHistory, err := config.ProjectBoolAt(workspace, profileDir, config.KeyHistoryEnabled)
	if err != nil {
		return tui3.Conversation{}, err
	}
	keepDraft, err := config.ProjectBoolAt(workspace, profileDir, config.KeyDraftPersist)
	if err != nil {
		return tui3.Conversation{}, err
	}
	draft := ""
	if keepDraft {
		if dir, err := v3Dir(); err == nil {
			draft = tui3.DraftFile(dir, workspace)
		}
	}
	var recall tui3.History
	if keepHistory {
		recall = s.proc.history()
	}
	bucket := launch.Bucket
	var anchor func(string) (string, error)
	if anchored, ok := agent.(interface {
		AnchorWorkspace(path string) (string, error)
	}); cfg.Place.Owned && ok {
		anchor = func(path string) (string, error) { return s.anchor(anchored, path) }
	}
	return tui3.Conversation{
		Agent:           agent,
		SessionFile:     cfg.SessionFile,
		Workspace:       workspace,
		Owned:           cfg.Place.Owned,
		AnchorWorkspace: anchor,
		Resumed:         resumed,
		Notice:          notice,
		ContextWindow:   cfg.ContextWindow,
		DraftFile:       draft,
		History:         recall,
		// This project's conversations, walked on the keystroke that asks for
		// them and never at boot — and this PROJECT's, which is what makes the
		// closure per conversation rather than per process.
		RecentSessions:   func() []tui3.Session { return v3RecentSessions(bucket) },
		SaveApproval:     bankToolApproval(agent, workspace, profileDir, s.seed.Yolo),
		SaveBashApproval: bankBashApproval(agent, workspace, profileDir, s.seed.Yolo),
		ApplyApprovals:   applyV3Approvals(agent, workspace, profileDir, s.seed.Yolo),
	}, nil
}

func (s *v3Seam) anchor(agent interface {
	AnchorWorkspace(path string) (string, error)
}, path string) (string, error) {
	resolved, err := agent.AnchorWorkspace(path)
	if err != nil {
		return "", err
	}
	s.boot.Workspace = resolved
	s.boot.Config.Workspace = resolved
	s.boot.Config.Place.Workspace = resolved
	s.boot.Config.Place.Owned = false
	s.boot.Place = s.boot.Config.Place
	if bucket, bucketErr := v3ProjectDir(resolved); bucketErr == nil {
		s.boot.Bucket = bucket
	}
	return resolved, nil
}
