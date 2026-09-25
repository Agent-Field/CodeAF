package session

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/effort"
	"github.com/Agent-Field/codeaf/internal/guard"
	lanes "github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// defaultContextWindow is the window assumed when Config.ContextWindow is
// zero. 128k is the smallest window among the models this surface routes to,
// so assuming it compacts a little early on a larger model and never
// overflows a smaller one — the safe direction for a guess to be wrong in.
const defaultContextWindow = 128_000

// providerTimeout bounds one non-streaming completion. Only the compaction
// summary takes that path (every turn request streams, and the adapter's
// stream client carries no total deadline by design), so this is a backstop
// against a wedged summarizer rather than a limit on how long a turn may run:
// the turn's authority is its context, which Interrupt cancels.
const providerTimeout = 10 * time.Minute

// closeGrace is how long Close waits for a cancelled turn to land its last
// messages. Two seconds is generous for the journal appends a cancelled turn
// still owes and short enough that a quit still feels like one.
const closeGrace = 2 * time.Second

// jobShutdownGrace is how long Close gives every running background job,
// together, to answer a SIGTERM before it is killed. It matches closeGrace for
// the same reason: a quit that takes four seconds already feels like a hang,
// and a dev server that has not shut down in two is not going to.
const jobShutdownGrace = 2 * time.Second

// New builds one session agent against a live provider. It performs no
// network request: the client is constructed, the prompt rendered, and the
// session file — if configured and present — replayed into the transcript.
func New(config Config) (*Agent, error) {
	// The session keeps the service-qualified identity a person chose, with only
	// the thinking level removed. The client keeps the bare wire slug. Folding
	// both into settings.Model would make a later disconnect unable to tell
	// which service this conversation was using.
	launchModel := config.Model
	config.Model, _ = roles.SplitEffort(config.Model)
	// AND THE REQUEST ROAD IS THE CALLER'S WHEN IT SAID ONE. A caller that has
	// already resolved its provider — the bash-belt worker seat, handed this
	// conversation's account-aware completer — hands it in rather than have one
	// minted from settings the seat does not carry; every other caller leaves it
	// nil and New builds it here.
	client := config.completer
	if client == nil {
		built, err := newProviderClient(config, launchModel)
		if err != nil {
			return nil, err
		}
		client = built
	}
	// AND THE OFFER DESK IS POINTED AT THE SIDE THAT HOLDS THE OPEN QUESTIONS,
	// at the same moment and for the same reason: this is where a real transport
	// exists. A pinned lane that goes quiet raises `coreweave is slow · switch to
	// auto? (y)` on the phase channel; `y` comes back through this package, and
	// without this line it would come back to nobody (phasenews.go).
	SetOfferAnswerer(answerOffer(provider.AnswerOffer))
	agent, err := newAgent(config, client)
	if err != nil {
		return nil, err
	}
	agent.manageClient(config)
	return agent, nil
}

// newChildAgent is the production door for another Agent inside this session.
// Tests keep using newAgent with scripted completers; real workers share the
// parent's account-keyed pool and resolve their own model before their first
// request, rather than inheriting whichever adapter the parent last used.
func (a *Agent) newChildAgent(config Config) (*Agent, error) {
	client, account, pool, managed, err := a.childClient(config)
	if err != nil {
		return nil, err
	}
	child, err := newAgent(config, client)
	if err != nil {
		return nil, err
	}
	child.managedClient = managed
	child.clientAccount = account
	child.clientPool = pool
	return child, nil
}

// newAgent is the seam New and the tests share: everything except which
// Completer the turns run against.
func newAgent(config Config, client Completer) (*Agent, error) {
	if strings.TrimSpace(config.Workspace) == "" {
		return nil, errors.New("session: workspace is required")
	}
	if strings.TrimSpace(config.Model) == "" {
		return nil, errors.New("session: model is required")
	}
	if client == nil {
		return nil, errors.New("session: completer is required")
	}
	if config.newerBuild == nil {
		config.newerBuild = buildinfo.StaleNotice
	}
	// WHICH OF THE TWO FIXED PREFIXES THIS SESSION SENDS, SETTLED ONCE AND
	// BEFORE ANYTHING IS BUILT FROM IT (promptprofile.go). It is derived rather
	// than configured — the model's window and the crew's worker seat are the
	// two facts — and it is settled HERE, above the render, because the page,
	// the belt, the shelf and the memory reflex are all built from this one
	// config and a profile resolved twice is a profile that can answer twice.
	config.profile = settlePromptProfile(config)
	system, own := config.System, false
	if strings.TrimSpace(system) == "" {
		system, own = renderSystem(config), true
	}
	agent := &Agent{
		config: config,
		system: system,
		// STAMPED ONLY WHERE WE RENDERED IT. A prompt handed in by a caller is
		// theirs, and re-rendering ours over the top of it later would be this
		// file deciding what another door's worker is told.
		systemAt:  time.Now(),
		systemOwn: own,
		// THIS LAUNCH'S WALL CLOCK, and the only one this package keeps
		// ([Agent.startedAt] says why the summed turn durations are not it).
		startedAt: time.Now(),
		model:     config.Model,
		// A memory-only session still has ONE lineage; it just has no name on
		// disk to derive it from. The file-backed case overwrites this below
		// with the header's id, which survives every resume.
		id: NewSessionID(),
	}
	agent.cacheKey = sessionCacheKey(agent.id)
	// WHAT IS ALREADY KNOWN ABOUT THIS MODEL'S REAL WINDOW, before the first
	// check. The memo survives processes (internal/provider's ServedWindow), so
	// a model that refused an over-long prompt last week is capped from this
	// session's first turn rather than from its first refusal (loop.go).
	agent.noteModelWindow(agent.model)
	// AND WHO THIS SESSION IS WORKING FOR, before anything else is built
	// (principal.go). It is written once here and never again, which is what
	// lets every road that consults it read the field without the lock; the
	// posture that picks it — attended or not, with a ceiling or without — is
	// entirely the door's, and a config that says nothing gets the [Person] this
	// package has always answered to.
	agent.principal = newPrincipalFor(agent)
	// Memory is built before the belt for the same reason the registry is: the
	// belt carries `remember` only when there is a brain to write into, so the
	// store has to exist before the tools are assembled (memory.go). The
	// background lifetime is minted with it, because a pass started by the first
	// turn has to have somewhere to be cancelled from.
	// THE PREDICATE IS [Config.hasStore] AND NOT THE FIELD, because the field is
	// two things: the conversation's own record, which every shape writes and
	// reads, and the writable memory this brain is, which a lean prefix does not
	// have (promptprofile.go). The belt and the page are built from that same
	// predicate a moment later, which is what stops them disagreeing about
	// whether `remember` exists.
	if config.hasStore() {
		agent.memory = newMemoryBrain(config.Memory)
		agent.memoryCtx, agent.memoryStop = context.WithCancel(context.Background())
	}
	// AND THE NAMER'S OWN LIFETIME, minted for every session because every
	// session may name itself and the errand starts on the first message rather
	// than at the end of a turn (title.go).
	agent.titleCtx, agent.titleStop = context.WithCancel(context.Background())
	// The registry is built before the belt because the belt closes over it:
	// bash's background path and the jobs tool are both views onto this one
	// object, and it is the agent's own steering queue they report into.
	// The registry gets both note lanes. A job's exit is work the model started
	// and is waiting to hear finish, so it remains owed at the next step
	// boundary. A watch's deltas are ambient telemetry: they stay live in the
	// job row and log, but wait for a turn boundary before entering the model's
	// context (see [Agent.enqueueJobNote] and [Agent.enqueueWatchNote]).
	// The registry is handed the DROPPINGS home rather than the Place, and the
	// two differ for every agent that is not a session: a worker's job log
	// belongs beside the transcript of the conversation that commissioned it, not
	// in the repository it borrowed to work in (landing.go).
	agent.jobs = newJobRegistry(config.Workspace, config.droppingsPlace(), agent.enqueueJobNote, agent.enqueueWatchNote)
	// And the registry gets the ROSTER lane as well as the waking one. A job is
	// work this conversation started, so it shows on the right the way every
	// other kind of work does — a quiet row while it runs, settled when it ends
	// (jobrow.go). It is set here rather than passed to the constructor because
	// it closes over the agent the constructor is building.
	agent.jobs.announce = agent.announceJobRow
	// And the registry gets the RELEASE lane, for the same reason and by the same
	// route: a command this agent started in the foreground and had taken over
	// into a job is one the work is still waiting for, so the registry says when
	// that ending has been handed over and whoever is parked on it goes and reads
	// it (task_job_park.go). A conversation arms no park and this is never called
	// with anybody waiting.
	agent.jobs.paid = agent.releaseParkedOnJob
	// And the accounts seam before the belt for the belt's own reason: the two
	// connect tools are on it only when there is something behind them, so the
	// hub has to exist before the tools are assembled (connect.go).
	agent.connect = newConnectHub(config)
	agent.tools = agent.belt()
	definitions, err := toolDefinitions(agent.tools)
	if err != nil {
		return nil, err
	}
	agent.definitions = definitions
	// AND THE GROUPS THIS PROFILE HANDS OVER RATHER THAN ASKING FOR. A lean belt
	// is given `ask` at construction because a one-call-per-message model cannot
	// do load-then-ask inside a turn (promptprofile.go's [Config.prearmedGroups]
	// states the whole of it). It goes on through [Agent.armFamily], the one
	// arming door, so the append law and the dedupe are the same ones a
	// connected account and a loaded group ride.
	if err := agent.armPrearmed(); err != nil {
		return nil, err
	}
	agent.messages = []ai.Message{textMessage("system", system)}
	agent.messageReasoning = make([]provider.MessageReasoning, 1)
	agent.refreshSystemLocked()

	if strings.TrimSpace(config.SessionFile) != "" {
		// The folder's name is the id a fresh journal's header takes (place.go):
		// the session was named before the directory that holds it existed, and
		// the header repeats that name rather than minting a second one. A
		// legacy flat session hands "" and the file names itself, exactly as it
		// always did.
		file, replayed, err := openSessionFile(config.SessionFile, config.Workspace, config.Model, config.Place.ID())
		if err != nil {
			return nil, err
		}
		restored := replayed.messages
		agent.file = file
		// AND WHAT AN EARLIER PROCESS OF THIS SESSION MADE. It is the one thing
		// in the journal that cannot be re-derived from the transcript — whether
		// a file was there before the session touched it is a measurement, taken
		// once, at the moment of the call — so a resumed session carries it
		// forward rather than starting the sweep's list empty (principal_audit.go).
		agent.createdFiles = replayed.created
		// A resumed session keeps the name it was given: the title is a fact
		// about the conversation in the file, and re-deriving it from the same
		// opening exchange would pay for an answer we already have.
		agent.title = file.Title()
		// And it keeps its cache lineage for the same reason, which matters
		// more: a session resumed tomorrow re-sends the transcript it built
		// today, and a key that changed with the process would ask the router
		// for a fresh replica and pay to write that whole prefix again.
		if id := file.ID(); id != "" {
			agent.id = id
			agent.cacheKey = sessionCacheKey(id)
		}
		// The system message is rendered fresh rather than replayed: the date
		// and AGENTS.md in the footer are facts about now, not about the
		// session that wrote the file.
		agent.messages = append(agent.messages, restored...)
		agent.messageReasoning = append(agent.messageReasoning, replayed.reasoning...)
		// AND WHETHER THE CONVERSATION ENDS ON A QUESTION NOBODY ANSWERED. It is
		// carried off the journal rather than derived from the transcript because
		// the fact lives on a line the transcript does not keep — which door
		// stopped the turn (resume.go).
		agent.stoppedTurn = replayed.stopped
		// AND WHICH OF THOSE LINES THE PERSON ACTUALLY TYPED, which the messages
		// alone cannot say (admission_compile.go). Work handed out of a reopened
		// session would otherwise carry none of the conversation that preceded
		// the restart, silently. No lock is taken for the reason nothing else in
		// this constructor takes one: the agent is not reachable yet.
		agent.restorePersonTurnsLocked(restored)
		// AND THE TOOL GROUPS AN EARLIER PROCESS OF THIS CONVERSATION LOADED.
		// The belt above was rebuilt from scratch, so the rarely-reached
		// families are back on their shelf — while the transcript just restored
		// still says "Loaded: settings, change_setting". Without this the model
		// would reach for what its own history says it holds and be answered
		// `Unknown tool`, which is the defect beltfacts.go exists to prevent.
		// The record is the journal's own `load_capability` calls; nothing is
		// stored beside them (tools_capabilities.go).
		agent.rearmLoadedCapabilities(restored)
		// AND THE CONVERSATION ABOVE THE LATEST COMPACTION IS SHAPED HERE, ONCE,
		// while the replayed messages are still in hand. It is shaped rather than
		// kept as messages so the pictures a compacted region held are let go of
		// again — a display entry keeps the PATH, not the data URL — and shaped
		// HERE rather than on demand because this is the only moment the region
		// exists at all: nothing after this point re-reads the file, and holding
		// the raw messages until somebody scrolled would hold every compacted-away
		// photo in memory to pay for a shaping that costs one walk.
		//
		// The floor is shaped from the same messages for the same reason it is a
		// count of ENTRIES rather than of messages: the surface it is for indexes
		// entries, and a second rule for how many rows a message makes is a rule
		// that can disagree with [shapeEntries]. The journal counts messages; this
		// is the one place the two are converted, by doing the shaping.
		agent.earlier = shapeEntries(replayed.earlier, file)
		agent.earlierFloor = len(shapeEntries(restored[:replayed.overlap], file))
		// AND THE PICTURES ARE MADE SAFE HERE RATHER THAN IN THE REPLAY. A
		// session resumed onto a model without vision — the journal remembers
		// the model it was written on, the person can start it on another — would
		// otherwise re-send yesterday's base64 to a model that cannot read it,
		// which is [Agent.SetModel]'s hole through the other door. The scrub is
		// the same function both doors call, and it is done HERE because
		// [replaySessionFile] is a pure function of the file: which model this
		// session will ride, and whether it can see, are facts about the agent,
		// and threading a capability closure into a file parser would put the
		// question in the one layer that cannot answer it. No lock is taken for
		// the reason nothing else in this constructor takes one — the agent is
		// not reachable yet.
		agent.scrubBlindImagePartsLocked(agent.model)
		// AND IT KEEPS WHAT IT SPENT. The total is the sum of the file's usage
		// lines (sessionfile.go), so a conversation reopened tomorrow reports the
		// money it actually cost rather than the money this process has spent so
		// far — which for a resumed session was always nothing.
		//
		// THIS ALSO RESTORES THE SPEND RAIL, and that is the intended change: the
		// rail is this conversation's own ceiling (rail.go), so a conversation
		// that reached it yesterday is still over it today. A rail that reset with
		// the process was a ceiling on a window, not on a conversation.
		agent.usage = file.RestoredUsage()
		// AND A JOURNAL WRITTEN BEFORE THE TOTAL WAS KEPT GETS ONE NOW. The sum
		// above came out of the file's own usage lines, so this costs the read
		// of a meta.json and — only when it has no figure at all — one write
		// (placemeta.go's [Agent.stampRestoredSpend]). Without it a conversation
		// that has not spoken since the field arrived would read as free on
		// home forever.
		agent.stampRestoredSpend(agent.usage)
	}
	// AND WHERE THIS CONVERSATION IS ABOUT. The referred places ride on the same
	// meta.json everything else about the session's identity does (places.go), so
	// a conversation reopened tomorrow still knows the folders it turned out to
	// be about and the ground ladder still answers for them without asking
	// anybody again. It is read here rather than at the door because there is
	// only one road in and no launch flag names it; a session with no folder — a
	// headless run, a task node, a test — reads nothing and accrues nothing.
	agent.places = loadPlaces(config.Place.Dir)
	// AND THE MODEL IS TOLD ABOUT THEM BEFORE THE FIRST REQUEST. This is what
	// makes an attachment survive a restart in the only sense that matters: a
	// conversation reopened tomorrow does not merely REMEMBER the folder, its
	// next request names it (placescontext.go). It is composed here rather than
	// at the top of the constructor because the set is only read on this line,
	// and it is [Agent.keepAttached] rather than a field write so that the one
	// composition rule lives in one place. The agent is not reachable yet, so the
	// lock it takes is uncontended.
	agent.keepAttached()
	// AND THE WORK THAT HAS NOT LANDED YET. A conversation closed with changes
	// waiting in its own copy of a folder comes back holding them, and the
	// composer's chip says so again (standingtree.go). A record whose copy is no
	// longer on disk is dropped on the way in, so what is read back is what can
	// actually be landed.
	agent.trees = loadStandingTrees(config.Place.Dir)
	// THE THREAD IS THE SESSION'S OWN ID, and it is minted nowhere: the journal
	// header already carries one that survives every resume, the folder is named
	// by the same string, and a memory-only session has the one this constructor
	// minted for its cache lineage. Deriving a second identity here would give
	// one conversation two threads the day somebody resumed it (chatlog.go).
	// And it takes the DROPPINGS home for the registry's reason: the only thing
	// the journal does with a Place is spill an over-long message's bytes through
	// [writeStub], which is a dropping like any other (landing.go).
	agent.chatlog = newChatJournal(config.Memory, agent.threadID(), config.Workspace, config.droppingsPlace())
	// And the state card is held before any turn has run, so that the note the
	// first request carries already has it: a resumed conversation's card is what
	// it knew yesterday, and a model that had to wait for the first post-turn
	// pass to be told would answer one question in the dark (card.go).
	agent.cardText = agent.stateCardText()
	// The client is wrapped LAST, once the lineage is known: the wrapper is the
	// one place every request this agent makes passes through, so it is where
	// the prompt-cache key is stamped. Wrapping earlier would have to read the
	// key through the agent, which is a pointer cycle to save a line.
	// The account door owns the field itself, while the construction facts stay
	// here beside the lifecycle they describe.
	agent.installSessionClient(client, config)
	// AND THE WORK IS RECOVERED LAST, once this agent can actually run one. A
	// resumed journal may have a task graph beside it — nodes that landed, a node
	// that was still running when the process died, nodes waiting on them — and
	// recovery is load, reconcile with the disk, continue the frontier
	// (task_store.go). A fresh session has no checkpoint and this is a stat.
	agent.recoverTasks()
	// AND THE PROJECT'S RECORD IS RECONCILED BESIDE IT. The checkpoint above is
	// one conversation's graph; the project index is every window's record of
	// what this directory ever ran, and it holds rows that say "running" — a run
	// takes one the moment it starts (orchestrate.go). A process that died owes
	// those rows a closing one, and this is the moment anybody can know it is
	// owed. It runs AFTER recovery so that a node the graph took back is not
	// closed out from under it.
	agent.closeInflightTaskIndexRows()
	// AND WHAT ARRIVED WHILE THE WINDOW WAS SHUT. A standing item that fired
	// into a conversation nobody had open left its news in the session's inbox
	// (internal/standing's Deliver), and this is the moment it is folded into
	// one "while you were away" line for the first turn to read — the same lane
	// and the same reason as the interrupt account above (standing_run.go).
	agent.drainStandingInbox()
	// AND THIS PROCESS SAYS IT HOLDS THIS CONVERSATION. It is how a firing knows
	// to steer its line into a live room instead of writing an inbox line
	// nobody will see until tomorrow (standing_run.go's registry). Close erases
	// it.
	registerLiveSession(agent)
	// AND WHAT WAS ANSWERED WHILE NOBODY WAS HOME IS ANSWERED NOW. An answer left
	// on this conversation's doorstep rides the presence heartbeat
	// (answers.go's [Agent.drainAnswers]), which is the right beat for one
	// window answering another that is RUNNING. It is the wrong one for the
	// conversation that was NOT: a window shut for the night keeps nothing
	// beating, so the answer the person left on home — accept this landing, no
	// to that card — sat in answers.jsonl until the next time anybody happened
	// to open the conversation, and even then until the first tick of the new
	// process. From home's side the row said `answered · waiting for it to pick
	// that up` for as long as the person cared to look, which is home saying it
	// answered and home being wrong. The graph the landing answers to is back
	// above, and the heartbeat that would race this drain starts below, so the
	// doorstep is emptied here: the answer is applied through the same one door
	// as every other, and the conversation opens already settled rather than
	// asking a question somebody answered yesterday.
	agent.drainAnswers()
	// AND THE SESSION STARTS SAYING IT IS HERE. The index above is what work
	// came to; this is the claim that a PROCESS is alive right now, which no
	// file on disk could otherwise make (taskpresence.go). It is last of the
	// three because it describes the state the two lines above just settled.
	agent.startPresence()
	// AND THE LANE SHEET STARTS BEATING FOR THIS SESSION'S MODELS. It is the one
	// thing under here that goes to the network without a person asking, which
	// is why it is the thing most carefully gated: see [Agent.startLaneBeat] for
	// the three sessions that run no beat at all.
	agent.startLaneBeat()
	// AND ONLY NOW MAY IT SPEAK UNPROMPTED. Recovery turns the frontier, and a
	// cascade over the dependents of an interrupted node settles them right here,
	// inside New — before the caller holds the agent, before any surface has
	// subscribed to anything. A turn started at that moment would be answered
	// into a room that does not exist yet: journaled, paid for, and never drawn.
	// Everything recovery has to say is queued instead, and the first turn reads
	// it (see [Agent.wakeLocked]).
	agent.mu.Lock()
	agent.opened = true
	agent.mu.Unlock()
	// THE WALL IS READ AFTER THE SESSION IS OPEN. Recovery may put work back on
	// the frontier, but no ending or notice may run before the caller can hold
	// the agent and a surface can subscribe to its standing lane.
	agent.armWallClock()
	// AND A CONVERSATION A TEAM'S MANAGER STARTED TAKES ITS FIRST TURN ON ITS
	// OWN, once the interface has made it a member (team_wake.go).
	agent.watchTeamStart()
	return agent, nil
}

// ── THE LANE-SHEET BEAT ─────────────────────────────────────────────────────

// startLaneBeat begins this session's lane-sheet beat, or does nothing at all.
//
// The router publishes, per model, one row per machine serving it — first-token
// latency and throughput over the last half hour — and that is a free prior for
// every lane this session might be sent to, so nothing is blind on the first
// call (docs/ARCHITECTURE.md, Decision 10). `internal/lane` decodes it and
// primes the belief from it, but it starts no goroutine of its own by design:
// who is fetching, and when, is a question with an answer in the session's own
// code rather than in a package nobody thought was running.
//
// TWO SESSIONS RUN NO BEAT, and each refusal is a different fact.
//
//   - ROUTING OFF is a person saying they do not want their endpoints chosen
//     for them (internal/provider's velocity.go). With the row off, every lane
//     the belief holds is inert — nothing reads it, nothing is sent from it —
//     and a background fetch would be work nobody asked for on somebody who
//     asked for the opposite.
//   - AND A SESSION WITH NO MODEL SLOT FILLED has nothing to fetch a sheet
//     about, which is the constructor's own guard reaching this far.
//
// THE BASE URL IS NOT A REFUSAL. This seam used to read the hostname and run
// no beat unless it said `openrouter.ai`, which left every session pointed at
// a proxy, a mirror or a router reached by its IP with no sheet at all (issue
// #373). Whether a base publishes an endpoints page is the base's own to say:
// the sheet asks it once, on the first refresh, and a base that says there is
// none is left alone until that answer is stale ([lanes.ErrNoSheetHere]). So
// the beat runs whenever routing is on, and on a base with no page it is one
// quiet request every five minutes rather than a feature that is absent.
//
// It is called once, from the constructor, before the agent is reachable —
// which is what lets the field it writes be read afterwards without a lock,
// exactly as [Agent.presence] is.
func (a *Agent) startLaneBeat() {
	// THE SESSION'S LANE CONTEXT IS MINTED FIRST AND UNCONDITIONALLY, above
	// every refusal below, because it is not the beat's: a probe rides it too
	// (lanenews.go's [Agent.Typing]), and a session that runs no beat may still
	// buy a measurement. It is cancelled once, by Close.
	a.laneCtx, a.laneStop = context.WithCancel(context.Background())
	if a.config.routingInForce() == provider.RoutingOff {
		return
	}
	models := laneBeatModels(a.config)
	if len(models) == 0 {
		return
	}
	ctx := a.laneCtx
	// THE INTERVAL IS THE LANE PACKAGE'S OWN AND IS NOT RESTATED HERE. Zero asks
	// [lanes.Beat] for its default, which is the same five minutes that package
	// already publishes as the age at which a cached sheet is stale — and a
	// second spelling of that number here is a number that would drift, so that
	// one session fetched on a clock the cache disagreed with.
	a.laneBeating = true
	go lanes.Beat(ctx, lanes.Default().Sheet(), models, 0)
}

// laneBeatModels is the models this session actually sends to, deduplicated and
// without the empties.
//
// IT IS THE SLOTS AND NOT THE CATALOG. The conversation's own model is what
// every turn rides, and the task model is what a node rides when its proposal
// names none; both are models this session will really open a connection to, so
// both are worth a prior. The fallback list is deliberately absent: those are
// models a turn moves to only when no endpoint would take the request at all,
// and fetching a sheet for every one of them would be paying, on every session,
// for a refusal that usually never comes.
//
// IT IS THE LEDGER'S NAME FOR EACH SLOT AND NOT THE OPERATOR'S. A slot may hold
// a floating alias — the shipped default is one — and the router publishes an
// endpoints page under the concrete model it points at, never under the alias.
// Fetched under the spelling as typed, the sheet lands in a third ledger key
// beside the two the sighting side was already splitting, and the session that
// paid for it reads no prior at all. See [lanes.LedgerModel]; with no catalog
// installed it is the bare-model normalisation the beat always had.
//
// It takes the Config rather than the agent so that the one question worth
// asking of it — which models does this session mean? — can be asked without
// building a session.
func laneBeatModels(config Config) []string {
	var models []string
	seen := make(map[string]bool, 2)
	for _, slot := range []string{config.Model, config.TaskModel} {
		account := config.clientConfig(slot, 0)
		if account.Direct {
			// A direct service has one road. Its model belongs on no endpoints
			// queue, even while another account in this process has a live beat.
			continue
		}
		slot = lanes.LedgerModel(slot)
		if slot == "" || seen[slot] {
			continue
		}
		seen[slot] = true
		models = append(models, slot)
	}
	return models
}

// stopLaneBeat ends the beat. It is Close's, and it never waits: a fetch in
// flight is a prior the next session will read off the wire again, and a quit
// that waited on somebody else's half-hour aggregate would be a quit that hangs
// on a slow router.
func (a *Agent) stopLaneBeat() {
	if a.laneStop != nil {
		a.laneStop()
	}
}

// threadID is the identity this session posts its transcript under — the
// journal header's id when there is a file, the folder's name when there is a
// folder, and the one minted for the cache lineage otherwise.
//
// The cache key cannot answer it — that is a hash, deliberately, so nothing
// about a session's id reaches a router's logs — so the id itself is held.
// No lock: this runs inside the constructor, before the agent is reachable.
func (a *Agent) threadID() string {
	if id := strings.TrimSpace(a.id); id != "" {
		return id
	}
	return a.config.Place.ID()
}

// sessionCacheKey names one session's prompt-cache lineage. It is derived
// through [provider.RunCacheKey] so the key is the same shape every other
// codeaf lineage uses — a "codeaf-" prefix an operator can recognize in a
// router's logs — and so nothing about the session's own id reaches the wire.
func sessionCacheKey(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return provider.RunCacheKey("session/"+id, "")
}

// Usage returns the session's accumulated usage.
//
// IT IS NOT A SECOND SET OF BOOKS. Every figure in it was put there by
// [Agent.bank] — the one door a call's money goes through — and that same call
// hands the machine's ledger its row (usage_ledger.go). So this and the ledger
// are one total kept by one owner and a tail read of the file, never two
// accumulators that can drift: the status line, the day's reading and the spend
// place cannot disagree by construction (issue #269). It is a running total
// rather than a scan of the file because a surface asks this on the frame clock
// and a ledger with a year in it must not be walked per frame.
//
// The one place it is deliberately LARGER than this session's own ledger rows is
// a fold: a child agent wrote its own calls down under its own id, and folding
// its tally home moves these books and writes nothing there (the fold rule).
// [UsageTree] is how a surface reads the family back out of the ledger without
// counting the same call twice.
func (a *Agent) Usage() Usage {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.usage
}

// ContextTokens is what the conversation now weighs, in tokens: the same figure
// the compaction threshold is checked against, under the same lock.
//
// It is THE honest number, and honest means two different things depending on
// what has happened. When a response has come back, it is the provider's own
// count of the request it just served — the system prompt, the tool schemas,
// every tool result, the arguments of every call, all the bytes a surface
// counting words cannot see. When the transcript has grown since (a 300KB file
// read that has not been sent yet), the content estimate is larger and wins. See
// [Agent.estimateTokensLocked] for why it is the max of the two.
//
// Zero is a session that has neither sent nor recorded anything, which is the
// only case where "nothing" is true.
func (a *Agent) ContextTokens() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.estimateTokensLocked()
}

// Model returns the model the next request will use.
func (a *Agent) Model() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.model
}

// SetModel swaps the model, and A PERSON'S WORD WINS AT THE NEXT REQUEST.
//
// A swap made while the agent is working reaches the work through steer.go's one
// door ([Agent.hearModelLocked], which states the law and the failure it was
// measured against): the request in flight is cut and asked again on the new
// model if nothing of it had reached the person, and otherwise the answer they
// are reading finishes and the next request the work makes carries the new
// model. It is never the next TURN — a task step is one turn and can run for
// twenty minutes.
//
// Between two requests the latched value rides every step and retry, so a swap
// arriving mid-stream cannot send one model the transcript another model was
// half-way through writing (loop.go).
//
// THE TURN ITSELF MAY STILL MOVE, and this is the one thing that moves it. A
// step whose stream is cut over and over spends a budget and then hops to the
// next model in the chain, announced, and the rest of that turn finishes there
// (loop.go's completeWithRetry). It is a rescue and not a preference: what a
// person set here is untouched, so the NEXT turn starts on the model they
// picked, and the only way this field changes is somebody calling this.
//
// AND IT IS WHERE THE PICTURES ARE MADE SAFE. A conversation carrying attached
// images carries them as base64 in the live transcript, re-sent on every step
// of every turn after they arrived; swapping onto a model that cannot see would
// send them to it with no gate in the way, because no image is being attached
// this turn. [Agent.scrubBlindImagePartsLocked] states the whole rule and its
// three deliberate limits — the journal is untouched, the swap is one-way, and a
// model that CAN see is handed everything unchanged.
func (a *Agent) SetModel(model string) { a.setModel(model) }

// setModel is SetModel with the answer to "when does this land", which the doors
// inside this package that have somebody to tell need ([Agent.RetargetTask]) and
// the exported one has nowhere to put. An empty model is no pick at all and
// lands nothing.
func (a *Agent) setModel(model string) ModelLanding {
	model = strings.TrimSpace(model)
	if model == "" {
		return ModelLandsNextRequest
	}
	a.mu.Lock()
	// A PICK THAT CHANGES NOTHING IS NOT A WORD. Re-choosing the model the work is
	// already talking to is a person confirming, not redirecting, and letting it
	// cut would spend their money reaching the same machine again for the same
	// answer.
	//
	// AND THE COMPARISON IS AGAINST THE MODEL THE WORK IS ON, never against the
	// session's own. The two differ exactly when a step has been rescued onto a
	// fallback, and that is the case where getting it wrong costs something both
	// ways: a person picking the SESSION's model while the step rides a fallback
	// is redirecting — real news the old reading called none — and a person
	// picking the FALLBACK the step already rides is confirming, which the old
	// reading called a change and paid a whole cut request for, under a room line
	// that said `switching now` and a step that changed nothing (steer.go's
	// [Agent.ridingNowLocked]).
	changed := a.ridingNowLocked() != model
	a.model = model
	if a.clientPool != nil {
		a.clientPool.setSeat(model)
	}
	landing := ModelLandsNextRequest
	if changed {
		landing = a.hearModelLocked(model)
	}
	if !a.running {
		a.rebindClientLocked(model)
	}
	if a.config.ContextWindowFor != nil {
		if window := a.config.ContextWindowFor(model); window > 0 {
			a.contextWindow.Store(int64(window))
		}
	}
	// AND WHAT IS KNOWN ABOUT THE NEW MODEL'S REAL WINDOW, which is a different
	// fact from the card's figure above and travels with the model rather than
	// with the session (loop.go's [Agent.noteModelWindow]).
	a.noteModelWindow(model)
	a.scrubBlindImagePartsLocked(model)
	a.followModelOnThePageLocked()
	a.mu.Unlock()
	// AND THE BEAT IS TOLD, OUTSIDE THE LOCK. Everything above is about this
	// session's own state; this is about a fetch somebody else will do, and a
	// lock held across a hand-off is a lock held for no reason.
	a.noteLaneModel(model)
	return landing
}

// SetSources replaces the live service set used by this conversation and by
// every child it opens later. If the current model's account moved or was
// removed, the retained provider client is replaced before another request can
// use the old address or bearer. A running turn keeps the client it started on;
// startTurnLocked performs the pending replacement for the next turn.
func (a *Agent) SetSources(sources modelsource.Set) {
	if a == nil || sources.Empty() {
		return
	}
	a.mu.Lock()
	a.config.Sources = sources
	a.clientPool.setSources(sources)
	if !a.running {
		a.rebindClientLocked(a.model)
	}
	a.mu.Unlock()
}

// noteLaneModel tells the sheet beat about a model this session has moved to.
//
// IT IS THE SECOND HALF OF THE REPORTED DEFECT. The beat's model list is
// settled when a session opens, from the two config slots ([laneBeatModels]),
// and [Agent.SetModel] is what the picker calls — so until this line a model a
// person chose deliberately never got a sheet for the rest of the session's
// life, and cold start was the STEADY STATE for exactly the models people care
// most about. Every prior the routing design rests on was absent for them.
//
// NOTHING WAITS FOR IT. [lanes.WantSheet] queues the name and returns; the beat
// fetches it on the other side of the channel and joins it to its round, so the
// second turn on a picked model is warm and the first is no slower for it. A
// session that runs no beat — routing off, a base that is not a router, no model
// slot filled ([Agent.startLaneBeat] states all three) — has nobody to tell, and
// says nothing rather than starting a fetch nobody asked for.
func (a *Agent) noteLaneModel(model string) {
	if a == nil || !a.laneBeating {
		return
	}
	a.mu.Lock()
	configured := a.config.clientConfig(model, 0)
	a.mu.Unlock()
	if configured.Direct {
		return
	}
	lanes.WantSheet(model)
}

// SetAPIKey hands the conversation the key it talks with, after the fact.
//
// It exists for one moment: the surface opened on a profile with no key, asked
// for one on its first screen (internal/tui3's firstrun.go), and the person
// pasted it — into a session that already exists, holding a client built
// without one (internal/provider's NewClient states that a keyless client
// refuses every request until this lands). The config copy is updated too,
// because every worker this agent spawns — a task node, an audit, a standing
// firing — is built from `a.config` and would otherwise inherit the empty key
// the boot had.
//
// A client that cannot take a key — a test double — is left alone rather than
// refused: the config still records the key, which is what the doubles read.
func (a *Agent) SetAPIKey(key string) error {
	key = strings.TrimSpace(key)
	a.mu.Lock()
	defer a.mu.Unlock()
	// Every real session wraps its provider client in [sessionCompleter] before
	// the surface can hand a key over. Asking the wrapper itself whether it can
	// take a key quietly answered no, updated only the config copy below, and
	// left the first model request on the empty bearer it opened with. Reach the
	// same underlying client task children use, then update it before recording
	// the key for workers spawned later.
	if err := a.setDefaultClientKey(key); err != nil {
		return err
	}
	a.config.APIKey = key
	a.config.Sources = a.config.Sources.WithDefaultKey(key)
	a.clientAccount = accountFor(a.config, a.model)
	return nil
}

// ── reasoning strength ──────────────────────────────────────────────────────
//
// How hard the model is asked to think is a CHOICE ABOUT A MODEL, not about a
// session, so it is kept per model id and not as one field. A person who dials
// a reasoning model to high, switches to a cheap one for a quick question and
// switches back finds the high still there: the second model never had a level,
// and setting one on it would have been a decision nobody made.
//
// The levels are the effort ladder's rungs (internal/effort). "off" here means
// SEND NOTHING — no reasoning field on the wire, the model's own default — and
// NOT provider.EffortOff, which sends {"reasoning":{"enabled":false}} to
// suppress the thinking pass outright. The distinction matters at exactly this
// seam: a person turning a knob back to off is saying "stop asking for extra
// thinking", which is the model's default, while EffortOff is a harness economy
// that some endpoints refuse with a 400 (provider's quirks.go). The harness may
// spend a round-trip discovering that; a person changing their mind must not.
//
// OFF HERE IS NOT SILENCE ON THE LADDER. It clears this scope, and the rung
// below — the conversation, the work, the install's default — is what the next
// turn then asks for (effort.go). A person who wants nothing asked for at all
// turns the ladder itself off, which is a choice they make once.

// Reasoning is the level the model now in use will be asked for, "" when none
// is set.
func (a *Agent) Reasoning() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reasoningLocked(a.model).String()
}

// ReasoningFor is the level held for one model id, whichever model is in use.
// It is what a picker asks while drawing a row for a model nobody has switched
// to yet.
func (a *Agent) ReasoningFor(model string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reasoningLocked(model).String()
}

// SetReasoning sets the level for the model now in use, for subsequent turns.
// A turn in flight keeps the level it started with: runTurn latches it once
// (loop.go), so a change made while the agent is working lands at the next
// Submit.
//
// AND THAT IS NO LONGER THE MODEL'S RULE. A model a person names reaches the
// work at the next REQUEST, cutting the one in flight when it has produced
// nothing they could use ([Agent.SetModel], steer.go's THE PERSON'S WORD WINS).
// The two rules differ because the two acts do: a person changing models is
// redirecting work they are watching go the wrong way, and a person turning the
// thinking up is setting a level for the next thing they ask.
//
// WHAT MOVES THE MODEL STILL MOVES THIS. Whenever the step's model changes — a
// rescue's hop, or the person's own word — the level is re-read for the model
// now in hand, because a level is a choice about a model and carrying one across
// would be asking the new model for something nobody set on it.
//
// An unrecognized level is ignored rather than cleared. The two callers are a
// picker that can only produce the four it draws and a flag the door has
// already validated with [ParseReasoning]; between them, a value this does not
// know is a bug upstream, and answering it by silently dropping a level the
// person did set would hide it.
func (a *Agent) SetReasoning(level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setReasoningLocked(a.model, level)
}

// SetReasoningFor sets the level for one model id without switching to it.
func (a *Agent) SetReasoningFor(model, level string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.setReasoningLocked(model, level)
}

func (a *Agent) setReasoningLocked(model, level string) {
	key := reasoningKey(model)
	if key == "" {
		return
	}
	rung, ok := parseReasoning(level)
	if !ok {
		return
	}
	// Absence is stored as absence: a map that held empty entries would answer
	// "this model has a level" for every model anybody ever cycled back to off.
	if rung == effort.None {
		delete(a.reasoning, key)
		return
	}
	if a.reasoning == nil {
		a.reasoning = make(map[string]effort.Rung, 2)
	}
	a.reasoning[key] = rung
}

func (a *Agent) reasoningLocked(model string) effort.Rung {
	key := reasoningKey(model)
	if key == "" {
		return effort.None
	}
	return a.reasoning[key]
}

// reasoningKey folds a model id the way every other lookup on this surface
// does — trimmed and case-insensitive — so a level set from a picker row is
// found again by a /model <slug> typed in another case.
func reasoningKey(model string) string {
	return strings.ToLower(strings.TrimSpace(model))
}

// ParseReasoning normalizes one operator-supplied level and reports whether it
// is one. It answers in the surface's own words so a door can validate
// `--reasoning` without importing the adapter, and "off" and "" both normalize
// to "" — see the block comment above for why off is silence and not
// provider.EffortOff.
//
// It is [effort.Parse] under a name the doors already call, so the five rungs a
// person may type here are the same five the ladder holds and there is no
// second list to keep true.
func ParseReasoning(level string) (string, bool) {
	rung, ok := parseReasoning(level)
	return rung.String(), ok
}

func parseReasoning(level string) (effort.Rung, bool) { return effort.Parse(level) }

// SetContextWindow tells the agent how many tokens the model it is now running
// actually accepts, and the compaction threshold follows it from the next check
// onward.
//
// It exists because [Agent.SetModel] does. Config.ContextWindow is the window of
// the model the session STARTED on; a person who switches to a 1M-token model
// mid-conversation would otherwise keep compacting at the old model's threshold
// — reading 128k of a window eight times that size — and one who switches the
// other way would overflow. A surface that knows the new model's window (the
// catalog's figure) sets it here alongside SetModel; one that does not, does
// not call this, and the configured window stands.
//
// Zero and negative are ignored rather than clearing the window: "I don't know
// this model's size" must not be spelled the same way as "this model has no
// context", and a caller passing an unknown figure through means the first.
func (a *Agent) SetContextWindow(tokens int) {
	if tokens <= 0 {
		return
	}
	a.contextWindow.Store(int64(tokens))
}

// Submit appends text as a user message and runs one turn: provider requests
// interleaved with tool execution until the assistant answers without a tool
// call. The returned channel streams the turn's events and is closed after
// EventTurnDone or EventError. Submitting while a turn runs injects the
// message into that turn (steering) rather than starting a second one.
//
// EVERY call returns a live channel, steering included. The in-flight turn
// owns an event hub, and a steering Submit subscribes a fresh channel to it:
// the caller sees the turn's events from its own subscribe point onward and
// the close at EventTurnDone. There is no replay — a surface that submits per
// message is watching the turn from where it spoke, not re-reading it — and a
// surface holding both channels sees each event on each, which is what a
// per-message caller wants and what a whole-session reader must de-duplicate.
func (a *Agent) Submit(ctx context.Context, text string) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}
	// A @ team or chat in the words is a reference, and the model needs a
	// bounded digest of it (mention.go). The journal keeps the words as typed.
	said := text
	if note := a.mentionNote(text); note != "" {
		text = text + "\n\n" + note
	}
	// A PERSON'S TURN OPENS ON WHAT IS RUNNING, while anything is (plandigest.go
	// states why it is pushed rather than asked for). The digest is read here,
	// on the person's own door, and nowhere else: a wake note, a job's ending
	// and a steer are not sentences that can change what a task should do, and
	// the block is the empty string whenever no run is live, which is every turn
	// of most conversations.
	if digest := a.planDigest(); digest != "" {
		user := planDigested(digest, text)
		user.said = said
		return a.submitUser(ctx, user)
	}
	user := userText(text)
	if text != said {
		user.said = said
	}
	return a.submitUser(ctx, user)
}

// submitUser is Submit's body with the MESSAGE left to the caller: the closed
// check, the steering splice, the spend rail and the turn are the same four
// things whatever the person's message turned out to be, and the second door
// onto them is a draft they marked standing (standing_mark.go).
//
// It is factored rather than copied for [Agent.SubmitImage]'s own reason: the
// steering rules and the rail are laws about a turn starting, and two functions
// applying them separately is two chances for one of them to stop.
func (a *Agent) submitUser(ctx context.Context, user userMessage) (<-chan Event, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, errors.New("session: agent is closed")
	}
	if err := a.resumeWorkLocked(); err != nil {
		a.mu.Unlock()
		return nil, err
	}
	// AND THE SKILLS THE MESSAGE CARRIES ARE CHOSEN NOW, from the words of the
	// message itself rather than the workspace the catalog scores against
	// (skillturn.go). It happens before the steering branch on purpose: a
	// message that arrives mid-turn is journaled like any other, and what the
	// journal keeps is what the person said — the block rides the message the
	// model reads and nothing else.
	a.attachTurnSkillsLocked(&user)
	if a.running {
		// Steering. The message is queued rather than appended here because
		// the transcript's tail is mid-tool-batch: a user message spliced
		// between an assistant's tool_calls and their results is a shape every
		// provider rejects. The loop appends it at the next step boundary,
		// where it is journaled like any other user message.
		a.steering = append(a.steering, user)
		// Subscribing under a.mu — not after releasing it — is what makes the
		// returned channel live rather than a coin flip: the turn's goroutine
		// clears running under this same lock BEFORE it closes the hub, so
		// running == true here means the hub cannot already be closed.
		events := a.hub.subscribe()
		a.mu.Unlock()
		return events, nil
	}
	// The spend rail is checked here, before anything is recorded: a refused
	// turn must do NO work, so the person's text is not journaled either — the
	// message is theirs to send again once the rail moves (rail.go).
	if err := a.railBlockLocked(); err != nil {
		a.mu.Unlock()
		return refusedStream(err), nil
	}
	events := a.startTurnLocked(ctx, user, nil)
	a.mu.Unlock()
	return events, nil
}

// Attach is a reader arriving at a turn IT DID NOT ASK FOR: the whole of what
// this turn has said so far, then its live tail, on one channel.
//
// It is the door for a surface that keeps several sessions alive and shows one
// at a time. Such a surface comes back to a conversation it detached from
// minutes ago and finds a turn half way through an answer; it holds no channel,
// because the caller that started that turn was a window that is no longer
// drawing it. [Agent.Submit] cannot serve it — a steering Submit deliberately
// starts where it spoke, and it would also put a message in the transcript,
// which is not what looking at something is.
//
// WHAT COMES BACK IS ONE STREAM WITH NO GAP AND NO REPEAT. The backlog and the
// subscription are taken under one hold of the hub's lock ([eventHub.attach]),
// so an event that lands during the call is on exactly one side of the join.
// Text deltas arrive folded — a paragraph rather than the two hundred words it
// streamed as — which a surface that appends delta text draws identically.
//
// running is false, and the channel nil, when NO TURN IS IN FLIGHT. That is not
// an error and not an empty stream: there is nothing to watch, the history is
// the journal, and a caller told "nothing is running" draws what it already has
// rather than waiting on a channel that would never carry anything. It is also
// the answer for a closed session.
//
// stop is how the reader leaves, and it is never nil — a caller may call it
// without checking running, and calling it twice is calling it once. After it
// the channel closes, the pump ends and the hub stops holding the subscriber.
// A SURFACE THAT DETACHES MUST CALL IT: nothing else can tell a reader that has
// gone from one that is redrawing, and a stream nobody says goodbye to is a
// parked goroutine and a queue that grows for the rest of the turn.
func (a *Agent) Attach() (events <-chan Event, running bool, stop func()) {
	a.mu.Lock()
	// The hub is read under a.mu for the reason a steering Submit subscribes
	// under it: the turn's goroutine clears running under this same lock BEFORE
	// it closes the hub, so a hub read here with running set cannot already be
	// closed. Closed or nil is handled anyway, because Close does not run
	// through the turn seam.
	if a.closed || !a.running || a.hub == nil {
		a.mu.Unlock()
		return nil, false, func() {}
	}
	hub := a.hub
	// AND WHAT IS STILL BEING ASKED, taken under this same lock rather than read
	// through the map afterwards: the reply's cards are filtered against it
	// ([eventHub.attach]), and a.questionWords belongs to a.mu.
	asking := a.stillAskedLocked()
	a.mu.Unlock()

	stream, live := hub.attach(asking)
	if !live {
		// The turn ended between the two locks. Its channel is already closed,
		// and saying "not running" is the honest answer to a question that was
		// about watching something happen.
		return nil, false, func() {}
	}
	return stream.out, true, func() { hub.drop(stream) }
}

// AttachReplay is [Agent.Attach] and [Agent.Transcript] AS ONE READING, for the
// surface that is about to draw both: the entries to replay, and — when a turn
// is in flight — that turn's live stream, whose backlog replays the turn's work
// from its first event.
//
// IT EXISTS BECAUSE THE TWO CALLS MADE SEPARATELY DREW THE RUNNING TURN TWICE.
// Every completed step of a turn is journaled the moment it completes
// ([Agent.recordLocked]), so mid-turn the transcript already holds the turn's
// first half — and the backlog replays that same half again, in its live form,
// under the copy the replay just drew. Taken under one hold of the lock, the
// split is exact instead of raced: the entries stop at the turn's floor
// ([Agent.turnFloor]) and the stream carries the turn whole, so the
// conversation reads once, with no gap and no repeat.
//
// WHAT STAYS BELOW THE FLOOR IS EVERY WORD A PERSON TYPED. The backlog carries
// the model's work and nothing else — no event kind re-delivers a user message
// — so the turn's opening message, and any steer typed into it since, come from
// the entries or from nowhere. The turn's own asides (a task's landing note
// drained mid-turn) ride the stream's task events instead and are left out with
// the rest of the work.
//
// events is nil when no turn is in flight — the entries are the whole record
// and there is nothing to watch — and stop is never nil, on [Agent.Attach]'s
// terms: calling it without checking events, or twice, is fine.
func (a *Agent) AttachReplay() (entries []DisplayEntry, events <-chan Event, stop func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || !a.running || a.hub == nil {
		return shapeEntries(a.messages, a.file), nil, func() {}
	}
	// The hub cannot already be closed here: the turn's goroutine clears
	// running under a.mu before it closes the hub, and we hold a.mu. The
	// defensive arm keeps the honest answer anyway — a dead stream would hang
	// the caller where the full record answers them.
	stream, live := a.hub.attach(a.stillAskedLocked())
	if !live {
		return shapeEntries(a.messages, a.file), nil, func() {}
	}
	floor := a.turnFloor
	if floor > len(a.messages) {
		floor = len(a.messages)
	}
	kept := append([]ai.Message(nil), a.messages[:floor]...)
	for _, msg := range a.messages[floor:] {
		// A steer is the person's own sentence and no event will ever redraw
		// it; a note the session authored rides the user role too and is the
		// one user-role line left out — its own event lane is its live record.
		if msg.Role == "user" && !a.file.isNote(msg) {
			kept = append(kept, msg)
		}
	}
	hub := a.hub
	return shapeEntries(kept, a.file), stream.out, func() { hub.drop(stream) }
}

// userMessage is a person's message on its way into the transcript: the message
// the model reads, and the durable references the JOURNAL writes in place of
// the parts it must not hold.
//
// The two travel together because they are written together — recordUserLocked
// appends the message and journals the line under one lock — and because the
// live part cannot be recovered from later. A data URL is bytes with no
// provenance: nothing in it says which file it came from or whether that file
// still holds the same picture, which is exactly what the journal has to write
// (see [journalPart]). Carrying the references beside the message is what keeps
// a queued image message — steering, a follow-up — journalable at the moment it
// finally lands, minutes after it was assembled.
//
// Everything the person types is one of these. A text-only message has no
// references and journals exactly as it always did.
type userMessage struct {
	message ai.Message
	refs    []journalPart
	// replyTags names finished tasks whose reports this message carries. It is
	// empty on every person's message and every other authored note.
	replyTags []TaskReplyTag
	// otherResults preserves untagged background outcomes when a batch also
	// carries task results. Their reply obligation must not be lost in folding.
	otherResults bool

	// wake marks a note the model OWES AN ANSWER FOR: a task's completion
	// (task_run.go's reportTaskNode), a background job's exit (jobs.go's reap),
	// or a watch FIRING — the last thing a watch ever says, and the answer to the
	// question it was started for. A watch's intermediate delta is deliberately
	// ambient and is the one piece of background news that is not. It is the difference
	// between the two kinds of news these queues carry — see
	// [Agent.enqueueSteering] — and it is read
	// at exactly two moments: when the note is queued, and when the turn that
	// was running drains what is left of the queue at its end. Both are places
	// where "does anybody have to say something about this" is the question.
	wake bool

	// said is THE PERSON'S OWN WORDS, when what the model reads is not only
	// them. Empty in every ordinary case, and set by two doors: a draft the
	// person MARKED STANDING, whose message carries an instruction in front of
	// the sentence (standing_mark.go), and a message that names a team or a
	// conversation, whose message carries that reference's digest after the
	// sentence (mention.go).
	//
	// THE INSTRUCTION IS THE MODEL'S AND THE JOURNAL IS THE PERSON'S. A
	// transcript that replayed the instruction would show somebody a paragraph
	// they never typed, on the row that is supposed to be the one thing on the
	// screen that is theirs — so the journal and the store keep this, and only
	// the messages this turn reasons from carry the rest.
	said string

	// lead is how many of the message's leading content parts the SESSION put
	// there, for a message whose parts are not all words: a picture message the
	// plan digest opens (plandigest.go's [planDigestedParts]). [userMessage.said]
	// cannot carry that case, because it keeps words alone and the journal of a
	// picture message must keep its pictures. Zero on every other message.
	lead int

	// authored marks a line the SESSION wrote rather than the person: every note
	// that goes through [Agent.enqueueNote] or the watch-only
	// [Agent.enqueueAmbient]. It is WHO SAID IT, where wake is WHAT IS OWED, and
	// the two are separate because an ambient note nobody must answer is still
	// not the person's words.
	//
	// It is read at exactly one place — the journal write in
	// [Agent.recordUserLocked] — and what it buys is a replay that draws the
	// harness's own line in the harness's own lane (sessionfile.go's
	// [sessionEntry.Note]).
	authored bool

	// skills is the ordered shelf names this message's block carried
	// (skillturn.go), set by [Agent.attachTurnSkillsLocked] and read by
	// [Agent.startTurnLocked] to report them as one notice. Empty on every
	// message that carries no block, which is every message before that door
	// and every message a shelf-less shape sends.
	skills []string

	// resumed marks THE PERSON'S OWN WORDS, ALREADY IN THE RECORD: a question
	// this session is asking again because the turn that was answering it ended
	// with nothing said (resume.go). It is set by one door and read by one line
	// of [Agent.startTurnLocked] — the record is skipped, and everything else a
	// turn does with the message it opened on happens exactly as it always does,
	// because the words really are the words the person typed.
	//
	// IT IS NOT [userMessage.empty]. An empty message is a turn about something
	// on the steering queue and has no words at all; this one has the words and
	// they are already written down. Spelling it as empty would take the
	// person's question out of everything a turn reasons about it with — what
	// work handed out of here would carry (task_brief.go), what the turn was
	// woken to answer (wakecause.go) — to avoid one append.
	resumed bool

	// steered marks A LINE SAID INTO A RUNNING NODE FROM OUTSIDE IT
	// (task_room.go's [Agent.SteerTask] and [Agent.relayToTask]). Such lines ride
	// this queue because a node's turns are its runner's to start and this is the
	// only lane into one — and this is how they are told apart again where it
	// matters: a node's runner must not close the agent with a line on the queue
	// that no request has carried (task_run.go's [runTaskChild]).
	//
	// IT IS A FACT ABOUT DELIVERY AND NEVER ABOUT AUTHORSHIP, which is the
	// distinction that was missing when the model's own `tasks … say` came
	// through this mark alone. Who spoke is `authored` — false for the person's
	// words, true for another agent's ([relayNote]) — and the record of a
	// correction is `crossed`. A reader of this field is asking "is somebody
	// waiting on an answer to a line no request has carried", nothing else.
	steered bool

	// ending marks A BACKGROUND JOB'S ENDING — an exit, a person's stop, a
	// render's last word — and it is read for exactly one thing: releasing a task
	// worker parked on a command it started (task_job_park.go).
	//
	// IT IS THE OTHER NEWS A PARKED WORKER WAITS FOR, and it is marked for
	// `steered`'s reason and released in the same locked step, so there is no
	// instant in which the ending is queued and the waiter has not been woken for
	// it ([Agent.enqueueNote]). Every ending carries it, including ones nobody is
	// parked on: a park woken by news it was not waiting for re-reads, finds
	// itself still owed and parks again on a fresh generation, which costs one
	// pass of a loop — while an ending that failed to wake the one worker waiting
	// for it costs that worker its whole allowance.
	ending bool

	// settle marks THE WAKE OF A LANDING THAT HANDS THE MODEL THE DECISION: under
	// `task.settle = auto` (and after somebody pressed "let codeaf decide") the
	// note tells the model to read the work and settle it, and the turn it wakes
	// runs under the checker's own bound rather than the run's wall ([settleWake]).
	// It is read where the wake is started ([Agent.wakeLocked]) and nowhere else.
	settle bool
	// settleCeiling is the number of provider calls this landing's settle turn may
	// spend before it is handed back. It is 1 for a node whose check saw a clean
	// tree and declared no check ([TaskNode.settleCeiling]) and [settleCallCeiling]
	// otherwise.
	settleCeiling int
	// settleModel and settlePrompt narrow an owed-answer landing to its cheap
	// seat and dedicated role page; ordinary settle wakes leave both empty.
	settleModel  string
	settlePrompt string

	// landingQuestion and landingOutcome preserve the two roles inside an owed
	// landing document: what was asked and the evidence the run returned.
	landingQuestion string
	landingOutcome  string

	// steer is THE PERSON'S WORDS TYPED INTO THIS TURN (steer.go's
	// [Agent.Steer]): a correction to the question already being worked on,
	// riding this queue for the reason everything else on it does — a step
	// boundary is the only legal place for a user message mid-turn.
	//
	// IT IS NOT `steered`, AND THE TWO MUST NOT BE READ FOR EACH OTHER. `steered`
	// is a line aimed at a RUNNING NODE, delivered onto that node's own agent by
	// task_room.go, and it promises nothing beyond arrival. This is a splice into
	// THIS conversation's turn, and it carries an identity, three outcomes and a
	// record that says which of them happened. steer.go states the distinction in
	// full; the two marks stay separate so that neither has to lie about what it
	// promises.
	//
	// Nil on every message that is not one, which is every message this queue
	// carried before it existed.
	steer *turnSteer

	// directions are the receipt ids of the lines said to a NODE that this
	// message carries (assignment.go). They are marked READ at the drain that
	// puts them in front of the model and nowhere else: a direction the landing
	// is waiting on must not stop waiting because an agent exited, only because a
	// request actually carried the words.
	directions []uint64

	// crossed is what the record keeps about a line the person sent ACROSS to
	// this agent from the room they were standing in (task_room.go's
	// [Agent.SteerTask]) — the instant they sent it, and the engine's own one-fact
	// account of what the sending did.
	//
	// IT IS THE HALF OF `steered` THE FILE NEEDS. `steered` is read while the line
	// is on the queue, to keep a runner from closing on top of it; this is read
	// once, where the line is written down, so that a page reopened tomorrow
	// draws a correction as a correction instead of as a second brief — which is
	// what it did, because a delivered line was journaled as a plain user message
	// and nothing on it said otherwise.
	//
	// It carries `Consumed: true` on every line, and that is the promise
	// [Agent.SteerTask] actually makes: the words were DELIVERED to an agent that
	// was listening. What the node then does with them is the node's turn to
	// take, and the record does not pretend to know it.
	//
	// Nil on every message that is not one, which is every message but a steer
	// into a node.
	crossed *SteerMark

	// delivered names the DURABLE deliveries this message carries: news whose
	// sender is owed an answer once this conversation's own record holds it, not
	// when its queue took it ([durableDelivery]). Nil on every message that is
	// not one, which is nearly all of them; a batch carries the ids of every note
	// folded into it ([batchSessionNotes]), because the batch is the record those
	// notes end up in.
	delivered []durableDelivery

	// batchKey names repeated ambient updates that collapse to their count and
	// newest fact at a boundary. Today only watches set it: their complete tick
	// history already lives behind `jobs output`, and copying every delta into
	// the model's context is the interruption this field exists to prevent.
	//
	// batchLabel is the person-readable name rendered beside that count. Both
	// are empty for owed task reports and every person's message, whose complete
	// words must survive the batching pass.
	batchKey   string
	batchLabel string
	// batch marks external news that belongs under the boundary's "while you
	// worked" opening. Session control notes deliberately leave it false: a
	// loop-recovery instruction must keep its recognizable first word and is
	// not an account of background work.
	batch bool
}

// userText is the ordinary case: a message that is only words.
func userText(text string) userMessage {
	return userMessage{message: textMessage("user", text)}
}

// wakeNote is a line the SESSION authored that the model owes the person an
// answer for. It is userText with the mark on it, and it is what makes a
// finished task produce a sentence instead of a card nobody replies to.
func wakeNote(text string) userMessage {
	return userMessage{message: textMessage("user", text), wake: true, batch: true}
}

// jobNote is a background job's ending on the owed lane.
//
// A JOB'S ENDING TRAVELS WHOLE. The ending is the moment its output finally
// means something, and a note that withheld it would be an invitation to make
// one more call for what the note was already about. [jobRegistry.settleExit]
// composes the headline, the last [jobExitTailLines] lines and the path to the
// whole log before the note reaches this lane.
//
// A WAIT IS ONLY WORTH TAKING IF WHAT IT WAKES WITH IS WORTH READING. A command
// the work is still standing over stops that work from asking anything at all
// (task_job_park.go), and it stops it so that the command's own ending can be
// the next thing the work reads. Carrying that ending whole is what makes the
// wait worth taking.
func jobNote(text string) userMessage {
	text = strings.TrimSpace(text)
	note := wakeNote(text)
	// AND EVERY ENDING IS MARKED AS ONE. It is what releases a worker parked on
	// the command this note is about, in the same locked step as the append
	// ([userMessage.ending]).
	note.ending = true
	note.otherResults = true
	return note
}

// watchNoteMessage is one ambient watch TICK reduced to the newest fact. The
// batch key is stable across ticks from the same named watch, which is what lets
// three deltas become "3 updates" rather than three mid-turn user-role rows.
//
// A WATCH'S FIRING DOES NOT COME THROUGH HERE. It is owed news and keeps every
// word ([Agent.enqueueWatchNote]).
func watchNoteMessage(name, text string) userMessage {
	return userMessage{
		message:    textMessage("user", watchUpdateSummary(name, text)),
		batchKey:   "watch:" + name,
		batchLabel: name + " watch",
		batch:      true,
	}
}

// watchUpdateSummary keeps what changed and the newest evidence line. Every
// omitted line remains available through the watch's job output.
func watchUpdateSummary(name, text string) string {
	lines := splitLines(strings.TrimSpace(text))
	if len(lines) == 0 {
		return "updated"
	}
	summary := strings.TrimSpace(strings.TrimPrefix(lines[0], "watch "+name))
	summary = strings.TrimSpace(strings.TrimLeft(summary, "·:"))
	if summary == "" {
		summary = "updated"
	}
	for index := len(lines) - 1; index > 0; index-- {
		latest := strings.TrimSpace(lines[index])
		if latest == "" || strings.HasPrefix(latest, "… ") || latest == "(no output)" {
			continue
		}
		return summary + " — " + latest
	}
	return summary
}

// briefNote carries instructions composed by the runtime, including a worker
// opening or landing. Its journal mark prevents a descendant from quoting the
// composed brief as a new statement the person actually typed.
//
// It owes NO answer — `wake` is false — because nothing here starts a turn. A
// node's turns are its runner's to start ([Agent.wakeLocked] declines inside a
// task), and the turn this line is read in is the one the last report begins.
func briefNote(text string) userMessage {
	return userMessage{message: textMessage("user", text), authored: true}
}

// steerNote is a line the PERSON said into a running node. It owes an answer
// like every wake note does, and it is not the session's own words, which is the
// whole of the difference (see [userMessage.steered]).
//
// THE INSTANT IS TAKEN HERE because here is the closest this package stands to
// the keypress: the line is on the queue from this moment and the boundary that
// reads it may be a minute away, and the file's own timestamp is when the line
// was WRITTEN, which is the right answer to a different question
// ([journalSteer] makes the same distinction for the other door).
//
// waiting is the engine's own second answer about the node this is going to
// ([Agent.SteerTask]): it had handed its work out and parked on the reports, so
// this line is what wakes it.
func steerNote(text string, waiting bool) userMessage {
	return userMessage{
		message: textMessage("user", text), wake: true, steered: true,
		crossed: &SteerMark{At: time.Now(), Consumed: true, Landing: SteerDelivered(waiting)},
	}
}

// relayNote is ANOTHER AGENT IN THIS SESSION speaking to a running node: the
// model calling `tasks … say` from the conversation, or a parent talking to a
// piece it handed out ([Agent.relayToTask]).
//
// It carries the two DELIVERY marks a person's line carries, because the
// mechanics are the same: `wake` releases a parked runner, and `steered` keeps a
// runner from closing the agent on top of a line no request has carried.
//
// Authorship is the opposite. It is `authored` — the session's own bit — so the
// journal writes it in the session's lane rather than as the person's
// correction, the folder does not record that the person spoke, and a later
// proposal's quoted ask is untouched. It carries no [SteerMark], which is the
// account of a correction somebody typed.
//
// AND IT SAYS WHOSE WORDS THEY ARE, IN THE WORDS. A worker reads one queue, and
// an unframed sentence there is indistinguishable from the person's own — so a
// model that cannot tell them apart reads "you may change the schema" as a
// grant. The line names the conversation it came from: actionable as
// coordination, useless as authority.
func relayNote(text string, from conversationID) userMessage {
	note := wakeNote(relaySaid(text, from))
	// Set here rather than left to [Agent.enqueueNote], which only marks the
	// notes that are not steered: this one is both, and the two marks answer
	// different questions — steered is about the queue, authored about who spoke.
	note.authored = true
	note.steered = true
	// And it is not batched into "while you worked": a sentence somebody is
	// waiting for an answer to keeps its own shape.
	note.batch = false
	return note
}

// relaySaid is the framing itself, kept beside the constructor so the sentence
// a worker reads and the marks it arrives under are read together.
func relaySaid(text string, from conversationID) string {
	return fmt.Sprintf("%s says: %s\n(That is another agent in this session speaking through the tasks tool, not the person. "+
		"It is worth acting on, and it is not the person's instruction: your brief and acceptance are unchanged, "+
		"and it grants no permission the person has not given.)", relaySpeaker(from), strings.TrimSpace(text))
}

// relaySpeaker names the conversation that spoke, in the vocabulary the worker
// already has for the family it is in.
func relaySpeaker(from conversationID) string {
	if from.task == 0 {
		return "the main conversation"
	}
	return fmt.Sprintf("task %d", from.task)
}

// carryingDirection marks one queued line with the receipt the node recorded it
// as (assignment.go), and touches nothing else on it: who spoke, what wakes and
// how it is journaled were decided by whichever constructor built it. The drain
// that puts the line in front of the model is what marks the direction read.
func carryingDirection(note userMessage, direction uint64) userMessage {
	if direction != 0 {
		note.directions = []uint64{direction}
	}
	return note
}

// SteerDelivered is the ONE FACT about what sending a line to a node did, and it
// is authored HERE — by the engine that did the sending — rather than by
// whichever surface happens to be drawing the page.
//
// THE SURFACE DRAWS IT VERBATIM AND NEVER ITS OWN WORD FOR IT, which is why this
// is exported: the room draws the clause the instant [Agent.SteerTask] answers,
// and the record keeps the same sentence for whoever opens the page tomorrow. A
// surface with its own spelling would be a second author of one fact, and the
// live page and the replayed page would quietly stop agreeing.
//
// waiting is [Agent.SteerTask]'s own second answer: the node had handed its work
// out and parked on the reports, so this line is what wakes it.
func SteerDelivered(waiting bool) string {
	if waiting {
		return steerWokeWord
	}
	return steerDeliveredWord
}

// SteerReceipt is what sending a line to a node DID, as one value that every
// surface — this process's own and a client at the other end of the wire — reads
// the same way.
//
// It replaced a bare bool because there are now THREE outcomes and not two, and
// the third one cannot be an error. A line said while the gate is reading the
// work is TAKEN: it goes onto the node's record and the landing may not publish
// over it (assignment.go). Reported as an error it would have been a success the
// caller had to recognise by matching a sentinel, which the local surface could
// just about do and a hosted one could not — an error crossing the wire arrives
// as text, so the fact would have been lost exactly where the person is furthest
// from the work.
type SteerReceipt struct {
	// Waiting says the node had handed its work out and parked on the reports, so
	// this line is what wakes it. It is the bool this receipt grew out of.
	Waiting bool
	// Held says nobody was inside the node to read the words and the work is not
	// over: they are on the node's record, and the landing revalidates against
	// them before anything is published.
	Held bool
	// Direction is the id of the receipt written on the node's record, and 0 when
	// no record was written. It is what a worker cites to revise the assignment
	// (assignment.go) and what a surface can pair an outcome with later.
	Direction uint64
	// Again says this task already held these words, from the same message of the
	// person's, so nothing was sent a second time and Direction is the receipt it
	// was written down as the first time.
	//
	// ONLY A SEND THAT CARRIES AN IDENTITY CAN ANSWER IT — a forward from the
	// conversation (task_forward.go) or a room's send under a [SteerSource]
	// (task_room.go's [Agent.SteerTaskFrom]). Saying the same sentence into a room
	// twice is saying it twice and is delivered twice: what is recognised is the
	// SEND and never the words.
	Again bool
	// Landing is the engine's own sentence for what happened, drawn verbatim.
	Landing string
	// Heard is WHEN the words reach the work, by the same reading a model pick
	// gets ([ModelLanding], steer.go): [ModelLandsNow] when the request in flight
	// had put nothing in front of anybody and was let go of, so the very next
	// request carries this line; [ModelLandsNextRequest] when an answer was
	// already arriving and is being allowed to finish, or when there was no
	// request out at all.
	//
	// IT IS THE SAME TYPE AS THE PICK'S ON PURPOSE. A person's word is one rule
	// with one clock, and a surface that had to learn a second vocabulary for
	// `continue` would be a surface that could say two different things about one
	// law.
	Heard ModelLanding
}

// The sentences a receipt can carry, and there is no other.
const (
	// steerDeliveredWord is the ordinary case, and it says the one thing the
	// person cannot see for themselves: the words crossed to another agent and
	// did not vanish on the way. It is a word and not a sentence because that is
	// the whole of the news.
	steerDeliveredWord = "delivered"
	// steerWokeWord is the truer fact when the node had said everything it had to
	// say and parked on the pieces it handed out (task_run.go's [TaskGraph.park]):
	// the line does not land in a step the node was about to take, it starts one.
	// Without it the page goes quiet for a moment after the person presses enter,
	// which is exactly the page they would see if the words had gone nowhere.
	steerWokeWord = "it was waiting on its pieces — your line wakes it"
	// steerHeldWord is the third: there was nobody inside the node to read the
	// line, because its work is being checked, and the words were kept rather
	// than sent back. It says what that buys — the check cannot land the task as
	// done over a correction nobody has read — because "held" alone would read
	// like a polite word for lost.
	steerHeldWord = "held on the task's record — it is being checked, and it cannot land as done without this"
	// steerLateWord is the same keeping with the one honest difference: this task
	// was already landing when the words arrived, so they are on its record for
	// the round after this one rather than for the work coming home now. Saying
	// the held sentence here would promise that a merge already going out would
	// wait, which nothing in this harness can make true (assignment.go's
	// publication boundary).
	steerLateWord = "kept on the task's record — it was already landing, so this is for the next round rather than for the work coming home now"
	// steerAgainWord is the fourth answer, and it belongs to one door only: the
	// same message of the person's forwarded to the same task again
	// (task_forward.go). The words are already on that record, so nothing was
	// sent, and the sentence says that rather than reporting a second delivery
	// that did not happen.
	steerAgainWord = "already on the task's record from the same message — nothing was sent a second time"
	// steerRunNoteWord is what a note on a run's own row answers. A run's task
	// has no worker to splice a line into. The words are a note on the task,
	// and the worker reads a note at its next step, which is the same sentence
	// the task room says once the store has the note. Saying the note arrived
	// now would claim a read that has not happened (stoprun.go's
	// [Agent.sayToRunRow]).
	steerRunNoteWord = "the worker reads a note at its next step"
)

// steerRecord is what the JOURNAL keeps about this line when it is a correction
// the person typed into work that was already moving — spliced into this turn
// (steer.go's [Agent.Steer]) or carried across to a node (task_room.go's
// [Agent.SteerTask]) — and nil for every other message, which is nearly all of
// them.
//
// THE TWO DOORS PRODUCE ONE MARK BECAUSE THE PERSON DID ONE THING. What differs
// between them is what was promised and that is inside the mark, not around it:
// a splice says the turn in flight carried the words, a delivery says another
// agent was handed them. Reading one shape here is what lets the record — and
// every page built on it — treat a correction as a correction wherever it was
// typed.
func (u userMessage) steerRecord() *SteerMark {
	if u.steer != nil {
		return &SteerMark{At: u.steer.note.At, Consumed: true, Landing: u.steer.note.Landing}
	}
	return u.crossed
}

// empty reports whether there is nothing here to record. It is the shape a
// WOKEN turn opens with: the note it is about is on the steering queue and the
// turn's first drain lands it, so there is no second message to write (see
// [Agent.wakeLocked]).
func (u userMessage) empty() bool {
	return len(u.message.Content) == 0 && len(u.refs) == 0
}

// text is the message's words — what a queued message says, with its parts left
// out. It is what a reader of the queue wants: the pictures are not a line of
// the conversation, and a data URL rendered into one would be unreadable.
//
// AND IT IS THE PERSON'S WORDS, never the session's in front of them. A message
// the plan digest or a standing mark opens is read by the model whole, but what
// a reader of the message wants — the recall, the owed answer, the ask a
// `forward` carries into a task — is what the person typed, and a digest read
// back as their ask would forward the run's own rows into a worker as the
// person's sentence.
func (u userMessage) text() string {
	if u.said != "" {
		return u.said
	}
	return messageContentText(u.journaled())
}

// journaled is the message as the record keeps it: the person's own sentence
// where the session wrote something in front of it ([userMessage.said]), the
// message without the session's leading parts where those are separate parts
// ([userMessage.lead]), and the message itself every other time.
func (u userMessage) journaled() ai.Message {
	if u.said != "" {
		return textMessage("user", u.said)
	}
	if u.lead > 0 && u.lead <= len(u.message.Content) {
		kept := u.message
		kept.Content = append([]ai.ContentPart(nil), u.message.Content[u.lead:]...)
		return kept
	}
	return u.message
}

// startTurnLocked begins one turn on a transcript the caller has already
// checked, with a.mu held. It is the ONE place a turn starts: Submit reaches it
// with the person's message, and a drained follow-up reaches it with a message
// that was typed while the last turn was still running.
//
// watcher is a stream built before the turn existed — a queued follow-up's —
// and is adopted onto the new hub before the loop can emit anything. Nil means
// the caller takes its own subscription, and it is the returned channel. The
// extra streams are adopted the same way and for the same reason a follow-up's
// is: a turn NOBODY ASKED FOR has to hand its events to the standing wake
// subscriptions ([Agent.Wakes]) before its first byte, or a surface would start
// reading it half way through its own answer.
//
// An EMPTY user message records nothing. That is the woken turn's opening: what
// it is about is already on the steering queue, and the loop's first drain
// writes it (see [Agent.wakeLocked]).
func (a *Agent) startTurnLocked(ctx context.Context, user userMessage, watcher *eventStream, extra ...*eventStream) <-chan Event {
	// The person speaking is what resets a team's loop breaker (team_wakewatch.go).
	a.notePersonTurn(user)
	a.rebindClientLocked(a.model)
	a.running = true
	a.lastTurnTruncated = false
	// AND ANOTHER WINDOW HEARS ABOUT IT NOW rather than at the next heartbeat
	// (taskpresence.go). The nudge never blocks and never takes a lock, which is
	// what lets it sit under a.mu here.
	a.nudgePresence()
	// The routed block is dropped here so a turn never re-lands the memories of
	// the one before it as though they were this turn's. WHAT THIS TURN NEEDS is
	// routed inside the turn goroutine instead ([Agent.refreshMemory], called
	// from the loop): that is a provider call, and this runs with a.mu held.
	//
	// CLEARING IT WITHDRAWS NOTHING. The block rides at the tail as an appended
	// note now ([memoryNoteOpening]), and a note that was said stays where it was
	// said — the field is what the NEXT note would carry, and emptying it means
	// this turn lands no new one until the router has answered.
	//
	// ONLY AN AGENT THAT CAN ROUTE CLEARS THE BLOCK. A task node is handed its
	// memories by the conversation that has the store, which reads them beside
	// the node's work and hands them over whenever they land (memory.go's
	// [nodeMemory], [Agent.takeMemory]); it has nothing to replace them with, and
	// clearing them would take away the one thing it was given — and would race
	// a block that arrives while this turn is opening.
	if a.remembers() {
		a.memoryText = ""
	}
	// AND THE CLOCK IN THE PROMPT IS BROUGHT UP TO DATE BEFORE THE TURN OPENS,
	// when it has gone stale enough to be worth the cold prefix (prompt.go's
	// clockRefresh says why that is free).
	a.refreshClockLocked(time.Now())
	// AND SO ARE THE PERSON'S STANDING ORDERS, on the same trigger and for the
	// same reason the clock has one: a turn must reason with the conditions that
	// hold now, and an order stood up while this conversation was open is not
	// something the next turn may still be blind to (standing_world.go).
	a.refreshStandingLocked()
	a.refreshSystemLocked()
	hub := newEventHub()
	a.hub = hub
	turnCtx, cancel := context.WithCancelCause(ctx)
	a.cancel = cancel
	// AND THE TURN CARRIES ITS OWN SECOND STAGE (abandon.go). The signal rides on
	// the context because the waits that need it are several frames down inside
	// tools that hold no reference to the agent; it is closed by nothing but
	// [Agent.Abandon], so an ordinary turn never selects on anything.
	gone := make(chan struct{})
	a.abandon = gone
	turnCtx = withAbandon(turnCtx, gone)
	// AND THE TURN IS NUMBERED, so that a turn this session has DISOWNED cannot
	// clean up after the turn that replaced it. See [Agent.Abandon]; the cleanup
	// below is the only reader.
	a.turnSeq++
	seq := a.turnSeq
	// A NEW TURN SPENDS NOTHING YET. The running total this resets is what an
	// abandoned turn is journaled with, and carrying the last turn's figure into
	// this one would put another turn's money on that line.
	a.turnSpend = Usage{}
	// done is how Close waits for this turn: closed under a.mu by the cleanup
	// below, after the turn's last message is journaled.
	done := make(chan struct{})
	a.done = done
	// AND WHEN IT OPENED, which is the one fact the takeover beat needs to tell a
	// request somebody is waiting on from one that was already lying on the disk
	// before this turn existed (takeover.go).
	a.turnBegan = time.Now()
	if !user.empty() && !user.resumed {
		a.recordUserLocked(user)
		// AND THE SESSION STARTS NAMING ITSELF NOW, on the person's own words,
		// beside the answer rather than behind it (title.go). The message is in
		// the transcript on the line above, which is the only thing the namer
		// needs; it is started under this lock so that two Submits racing to be
		// the first cannot buy two names.
		a.startTitleLocked()
	}
	// AND THE RECALL STARTS HERE TOO, beside the title and for a stronger version
	// of the title's own reason (memory.go's [Agent.startRecallLocked]). The name
	// is merely something nobody should wait for; the recall is something the
	// person WAS waiting for — 4.3 seconds on the 2026-09-11 census, before their
	// model had been asked anything at all — and starting it at the one place a
	// turn begins is what puts the whole of the turn's own preparation on top of
	// it instead of behind it.
	//
	// IT IS STARTED UNDER THIS LOCK for the title's reason as well: two Submits
	// racing to be the first must not each buy a route.
	a.startRecallLocked(turnCtx, user.text())
	// THEIR NEXT WORDS ARE WHAT CHANGED. A generation Interrupt minted waits
	// here for the sentence that follows Esc, and that sentence is the one
	// decision the leftover handlers and this turn's opening share.
	a.interrupt.note(user.text())
	// THE TURN'S WORK BEGINS HERE, and the floor says so for [Agent.AttachReplay]:
	// everything recorded at or past this index while the turn runs is work the
	// hub's backlog can replay, so a replay-then-attach surface must not be
	// handed it twice. It sits after the user record on purpose — the person's
	// message is below the floor, because no event ever re-carries their words.
	a.turnFloor = len(a.messages)
	// AND THE PERSON'S OWN WORDS ARE KEPT, so that work this turn hands off can
	// carry the sentence that asked for it rather than a paraphrase of it
	// (task_brief.go). A woken turn opens with nothing and changes nothing here.
	a.rememberAskLocked(user)
	// AND WHAT THIS TURN WAS WOKEN TO ANSWER, which is the other half of the same
	// question and belongs to this turn alone (wakecause.go).
	a.forgetOwedLocked()
	a.rememberOwedLocked(user)
	var events <-chan Event
	if watcher != nil {
		hub.adopt(watcher)
		events = watcher.out
	} else {
		events = hub.subscribe()
	}
	for _, stream := range extra {
		hub.adopt(stream)
	}
	// AND THE TURN SAYS WHICH SKILLS IT CARRIED, as one dim notice — the shape
	// the rest of this package reports its own machinery through — so a surface
	// can draw the block beside the message it was chosen for (skillturn.go).
	// The names, not the block: the model reads the block, the person reads
	// the line.
	if len(user.skills) > 0 {
		hub.send(turnSkillsNotice(user.skills))
	}

	go func() {
		// completed is the turn's outcome: true only when the model answered
		// without a tool call and nobody interrupted. It is what decides
		// whether a queued follow-up may start (see [Agent.FollowUp]).
		completed := false
		// NO CAUSE, BECAUSE NOTHING WAS STOPPED. This is the turn's own tidying
		// on the way out and there is nobody left waiting on the context; a door
		// named here would put a stop on a turn that finished (stopcause.go).
		defer cancel(nil)
		defer hub.close()
		defer func() {
			// A TASK NEVER STAYS UNOWNED PAST THE END OF A TURN. Anything the settle
			// policy or the person handed to the model comes back to the person here,
			// and the card draws its chips again (task_run.go's
			// [Agent.handBackUnsettled] states the law). It runs before the lock
			// because it asks this agent for its graph, which takes a.mu itself, and
			// it is safe to run twice — a disowned turn hands back nothing, because
			// the turn that replaced it has already handed back whatever was owed.
			a.handBackUnsettled()
			a.mu.Lock()
			// A DISOWNED TURN CLEANS UP NOTHING. [Agent.Abandon] has already done
			// every act below — drained the queues, cleared running, closed the
			// hub and closed `done` — and the fields this would clear belong to
			// whatever turn the session has started since. Doing them twice would
			// take a live turn's cancel away, close a live turn's hub, and close a
			// `done` channel that is already closed, which is a panic. The number
			// is the whole test (abandon.go).
			if a.turnSeq != seq {
				a.mu.Unlock()
				return
			}
			// Whatever is still queued was typed at this turn and belongs to
			// the transcript. Draining it here — under the same lock that
			// clears running, so no Submit can slip between — is what keeps a
			// leftover from landing AFTER the next Submit's message, answering
			// a question the person asked before the one they just typed.
			//
			// unanswered is the half of that drain nobody has replied to: a note
			// the SESSION authored — a task landing — or A SENTENCE THE PERSON
			// TYPED, either of which arrived after this turn's last request went
			// out, so the model never saw it. It is in the transcript now and
			// nothing is going to speak about it, which is exactly the silence
			// the wake below exists to end.
			//
			// A SPLICED SENTENCE IS NOT PART OF THAT DRAIN. It was aimed at a step
			// of this turn that never came, so writing it into this turn's
			// transcript would be the record claiming the model had read it. It is
			// lifted off the queue first and becomes a message waiting for a turn
			// of its own (steer.go's [Agent.liftSteersLocked]) — which the
			// follow-up drain below then starts, on the follow-up queue's own
			// terms. Everything else on the queue drains exactly as it always has.
			a.liftSteersLocked(hub)
			_, unanswered := a.drainSteeringLocked(hub)
			// AND THE SECOND LOOK AT A YOUNG COMMAND IS LET GO OF WITH THE TURN
			// IT WAS ARMED IN. It re-checks this turn's number before it touches
			// anything, so a leftover is inert either way; stopping it here is
			// what keeps the timer's life the turn's life (steer_grace.go).
			a.stopSteerGraceLocked()
			a.running = false
			// THE REDIRECT TURN HAS SAID ITS PIECE. Later turns plan and name
			// as they always have; an interrupted turn that never received
			// new words leaves the generation standing (interrupt_fan.go).
			a.interrupt.finishTurn()
			// And the presence stops claiming a turn is in flight, for the
			// reason it started claiming one (taskpresence.go).
			a.nudgePresence()
			a.cancel = nil
			a.hub = nil
			a.done = nil
			a.abandon = nil
			close(done)
			// The follow-up drain happens under the SAME lock that cleared
			// running, for the reason the steering drain does: between the two
			// there must be no window in which a Submit could start a turn and
			// have the follow-up land behind it, out of the order the person
			// typed them in.
			if next, ok := a.nextFollowUpLocked(completed); ok {
				// context.Background rather than the finished turn's: the
				// Submit that would have carried a context never happened, and
				// a follow-up inheriting a cancelled one would end before it
				// started. Interrupt and Close still reach it — both go through
				// a.cancel, which this call replaces.
				a.startTurnLocked(context.Background(), next.message, next.stream)
			} else if completed && unanswered && a.wakeLocked() {
				// SOMETHING LANDED IN THE LAST SECONDS OF THIS TURN. A task's
				// note, or the person typing while the answer was still
				// streaming: steering lands at a STEP boundary, and a turn whose
				// last request has already gone out has no next step, so the line
				// is in the transcript, unread by any request. The answer they are
				// owed needs one more turn — started here, with no new message,
				// because the thing to answer is already recorded.
				//
				// WITHOUT THIS THE PERSON'S OWN SENTENCE WAS THE THING THAT WENT
				// UNANSWERED. It reached the journal in their own words, the
				// surface drew it, and the session went idle — which reads from
				// the outside as a message the model never received.
				//
				// It is gated on `completed` for the reason the follow-up drain is
				// (see [Agent.nextFollowUpLocked]): a drain must never resurrect a
				// turn somebody stopped. A person who interrupted gets the note in
				// their transcript and silence, which is what they asked for.
			}
			a.mu.Unlock()
			// AND NO QUESTION OF THE MODEL'S OWN OUTLIVES THE TURN THAT RAISED
			// IT UNLESS IT SAID IT WOULD. It is [Agent.handBackUnsettled]'s law
			// one lane over: a ratify nobody answered and a question somebody
			// asked back on and never returned to have no wait of their own to
			// end, so without this they stood on the desk and against the cap
			// for the rest of the session, about a turn that finished
			// (tools_ask.go's [Agent.retireTurnQuestions]). It is AFTER the
			// unlock because withdrawing takes a.mu, and after the disowned-turn
			// return above because a turn that was abandoned has had every one
			// of these acts done for it already.
			a.retireTurnQuestions()
			// AND A TEAM MEMBER'S MANAGER IS TOLD HOW IT ENDED, after the
			// questions above came down, so the wait the turn ended inside is
			// closed before the ending is said (teamevent.go). The context is
			// the turn's own, still uncancelled unless somebody stopped it.
			a.teamTurnEnded(turnCtx, hub)
		}()
		// A faulted turn must end its streams with a reason rather than take
		// the process down: the person is holding a live channel.
		defer func() {
			if recovered := recover(); recovered != nil {
				completed = false
				hub.send(Event{Kind: EventError, Err: guard.Note("session/turn", recovered)})
			}
		}()
		// The turn's own opening message travels with it, for the one thing that
		// has to know whether a PERSON started this turn (harness.go): a woken
		// turn opens empty and reads its note off the steering queue, and a
		// matcher that went looking in the transcript would score the last thing
		// somebody typed against a turn they did not start.
		completed = a.runTurn(turnCtx, hub, user)
	}()
	return events
}

// FollowUp queues a message to be asked AFTER the current turn ends, and hands
// back the channel that turn will stream on.
//
// It is the second queue beside steering, and the difference between them is
// what each drain does. Steering lands INSIDE the running turn at its next step
// boundary: the model reads it as one more thing the person said mid-work.
// A follow-up waits for the work to finish and then starts a turn of its own —
// the same machinery, the same events, a normal new turn as far as any surface
// is concerned. That is the right shape for "and after that, do this", which as
// steering would arrive as an interruption of the thing it is meant to follow.
//
// ONE AT A TIME is the whole steering mode here: a turn's end dequeues exactly
// one message, and the rest wait for the end of the turn that one starts. A
// drain that started several turns at once, or spliced the whole queue into one
// message, would be a decision the person did not make.
//
// A follow-up queued while nothing is running starts immediately: there is no
// turn end coming to drain it, and a message that sat in a queue until the
// person happened to ask something else would be a silent hold.
func (a *Agent) FollowUp(text string) (<-chan Event, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("session: empty message")
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return nil, errors.New("session: agent is closed")
	}
	if a.workStopped {
		return nil, errWorkStopping
	}
	stream := newEventStream()
	if a.running {
		a.followups = append(a.followups, followUp{message: userText(text), stream: stream})
		return stream.out, nil
	}
	if err := a.railBlockLocked(); err != nil {
		return refuseOn(stream, err), nil
	}
	return a.startTurnLocked(context.Background(), userText(text), stream), nil
}

// followUp is one queued message and the stream its turn will speak on. The
// stream exists from the moment the message is queued so the caller has
// something to hold while it waits.
type followUp struct {
	message userMessage
	stream  *eventStream
}

// nextFollowUpLocked takes the next queued message, if a turn may start for it.
//
// completed is the finished turn's outcome, and this is where the drain law
// lives: A DRAIN MUST NEVER RESURRECT A STOPPED TURN. A turn that was
// interrupted or that died on an error is a turn the person stopped or that
// stopped itself, and starting the next queued message off the back of it would
// take a session that was just halted and set it working again. Interrupt
// clears the queue outright for the same reason; this is the second lock on the
// same door, because a turn can also end badly without anybody interrupting it.
//
// The spend rail is checked here too: a follow-up is a turn, and a turn that
// starts must be one the session can pay for.
func (a *Agent) nextFollowUpLocked(completed bool) (followUp, bool) {
	if len(a.followups) == 0 {
		return followUp{}, false
	}
	if !completed || a.closed {
		a.dropFollowUpsLocked()
		return followUp{}, false
	}
	if err := a.railBlockLocked(); err != nil {
		for _, queued := range a.followups {
			refuseOn(queued.stream, err)
		}
		a.followups = nil
		return followUp{}, false
	}
	next := a.followups[0]
	a.followups = a.followups[1:]
	return next, true
}

// dropFollowUpsLocked forgets every queued follow-up and ends its stream. The
// channel closing with no events is what a caller reads as "this never ran" —
// the alternative, leaving it open, is a surface waiting on a turn that will
// never come.
func (a *Agent) dropFollowUpsLocked() {
	queued := a.followups
	a.followups = nil
	for _, item := range queued {
		item.stream.close()
	}
}

// Interrupt cancels the in-flight turn, if any. The partial reply is kept in
// the transcript; the turn's stream ends with EventTurnDone. A tool call
// blocked on a consent request (consent.go) is released by the same
// cancellation and refuses, so the turn ends rather than waiting on a question
// nobody is going to answer now.
//
// It empties BOTH queues, and they empty differently because their drains do
// different things. The follow-up queue is DROPPED here: draining it would
// start a new turn, and a stop that was followed by the session working again
// is not a stop. The steering queue keeps its own law — the turn's end drains
// it into the transcript (the person typed it, so it is part of the record) —
// and that drain starts nothing, so it cannot resurrect anything. Both queues
// are empty once the interrupted turn has finished.
func (a *Agent) Interrupt() { a.interruptNamed(StopByPerson, "") }

// InterruptFor is the same door for machinery that is not a person: a window
// taking the conversation over, a tab closing, a hosted session being retired.
//
// IT EXISTS SO THAT THE TURN CAN ACCOUNT FOR ITSELF AFTERWARDS. Everything
// below is identical either way — the same queues are dropped, the same work is
// cut — and the only difference is the word the cancelled context
// carries, which is what decides whether the person is owed a sentence about a
// reply that never arrived (stopcause.go).
func (a *Agent) InterruptFor(door StopDoor) { a.interruptNamed(door, "") }

// InterruptNamed is the same stop with the window the door believed had
// gone, when the door has one to name. The name is a label and nothing
// else — never an identity — and empty is a stop that does not know.
func (a *Agent) InterruptNamed(door StopDoor, name string) {
	a.interruptNamed(door, name)
}

func (a *Agent) interruptNamed(door StopDoor, name string) {
	a.interruptDiscussions()
	// ONE GENERATION FOR THIS STOP, minted before the turn context dies so a
	// leftover handler that has not yet entered callRole shares the same
	// "what changed" decision as the redirect that follows (interrupt_fan.go).
	a.interrupt.begin()
	a.mu.Lock()
	cancel := a.cancel
	a.dropFollowUpsLocked()
	// AND THE SECOND LOOK AT A YOUNG COMMAND IS RELEASED BEFORE THIS LOCK IS,
	// not later by the turn's own cleanup. The cancel below is made with the
	// lock let go of, so a watch left armed has a real interval in which the
	// turn is still running and the command's context is still alive — and what
	// it would do there is hand the foreground command to the job registry,
	// where it deliberately SURVIVES an interrupt (jobs.go). A person who
	// pressed stop would be left with the command detached and still running,
	// which is the opposite of what they asked for. The cleanup stops it again
	// and that is idempotent (steer_grace.go).
	a.stopSteerGraceLocked()
	a.mu.Unlock()
	if cancel != nil {
		cancel(stopAs(door, name))
	}
}

// Title is the session's name, empty until it has one (title.go).
func (a *Agent) Title() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.title
}

// ShortTitle is a compatibility alias for older callers. Conversations have one name.
func (a *Agent) ShortTitle() string { return a.Title() }

// Compact runs a compaction pass now (the surface's /compact). It is a no-op
// when the transcript is smaller than the keep-recent floor.
func (a *Agent) Compact(ctx context.Context) error {
	return a.CompactWithFocus(ctx, "")
}

// CompactWithFocus is Compact, and THE FOCUS IS NOW IGNORED.
//
// It was one extra instruction for the summarizer — `/compact keep the API
// decisions and the failing test`, a person saying which part of a lossy summary
// had to survive. There is no summarizer any more (loop.go): a pass stubs tool
// results and folds assistant work, and neither of those is a judgement anybody
// can steer. The kept content is the same whatever is typed after /compact —
// every user message, the recent tail, and the state card — so there is nothing
// for a focus to protect that is not already protected.
//
// The door stays open with its signature unchanged because the surface calls it
// (internal/tui3), and a person who types the old form gets the pass they asked
// for rather than an error about a machine that used to exist.
func (a *Agent) CompactWithFocus(ctx context.Context, _ string) error {
	_, err := a.compact(ctx, nil)
	return err
}

// sessionCompleter is the Completer every request goes through, and it does two
// things to them.
//
// ── THE CACHE LINEAGE, AND WHY IT DEVIATES FROM bare ──
//
// It stamps the session's prompt-cache key on every request. internal/exec/bare
// sends NO key at all, and that is right for what bare is: a leaf is one task,
// run once, whose prefix nothing will ever ask for again — a key there buys a
// replica pin and pays for a cache write nobody reads.
//
// A SESSION IS A LINEAGE, and the arithmetic inverts. Its transcript is re-sent
// whole on every step of every turn, grows all day, and is picked up again
// tomorrow by [Agent] resuming the same file. Without a key each request is free
// to land on whichever replica the router likes, so a warm prefix is a
// coincidence; with one, every step of a days-long conversation asks for the
// same instance and the growing head stays hot. The key is derived from the
// SESSION ID rather than from the model, the task, or the process, because that
// is the identity that survives a /model swap, a restart, and a resume — the
// three things that would otherwise split one conversation into three lineages.
//
// The key is set once, at construction, so nothing at request time can split it.
// An empty key is left off entirely rather than sent blank (provider.WithCacheKey
// ignores it), which keeps a session with no identity unkeyed instead of sharing
// one lineage with every other unkeyed session.
// ── AND THE PATIENCE ──
//
// It marks a TASK CHILD'S calls as ones that wait a provider's pacing out
// instead of giving up on it (internal/provider's patience.go). A conversation
// keeps the bounded patience it always had, and the difference between the two
// is entirely whether anybody is watching: a person in front of a cursor is owed
// an error long before they are owed a ten-minute silence, while a node with a
// worktree and nobody watching loses an hour of real work to a 429 that was
// always going to clear. The wait is still the caller's context's to end, so an
// interrupt or a stop cuts through a parked node's call at once.
type sessionCompleter struct {
	inner    Completer
	cacheKey string
	// patient marks this agent's calls as a task child's: see above.
	patient bool
	// pacing is who to tell while one of those calls is parked, and nil for
	// every conversation and every agent nobody is drawing a card for.
	pacing func(bool)
	// unguarded takes the reply guard off this agent's calls. It is the OFF
	// state rather than the on one so that the zero value is the default.
	unguarded bool
}

func (f sessionCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	ctx = provider.WithCacheKey(ctx, f.cacheKey)
	if f.patient {
		ctx = provider.WithPatientRateLimits(ctx)
	}
	if f.pacing != nil {
		ctx = provider.WithPacingNotice(ctx, f.pacing)
	}
	if f.unguarded {
		ctx = provider.WithoutBabbleGuard(ctx)
	}
	return f.inner.CompleteWithMessages(ctx, messages, options...)
}

// ProbeLanes passes the keystroke's pre-warm through, and does nothing at all
// for an inner completer that cannot buy one ([laneProber]).
//
// IT IS THE DOOR THIS WRAPPER SWALLOWED. [FallbackModels] below was written
// against exactly this hazard and the sentence it carries is the whole of why
// this one is here: the wrapper is the ONE thing every request an agent makes
// goes through, so an optional door it does not forward is a door the
// conversation does not have. Nobody forwarded this one, and because
// [Agent.installSessionClient] wraps on every construction path, the assertion
// in [Agent.probeClientLanes] had never once succeeded in a built agent — on the
// default road or in process. The probe that exists so a turn's first token is
// not also paying for a TLS handshake had therefore never fired at all, and the
// test that covered it built an Agent literal and skipped the wrapper.
// [TestTheCompleterWrapperForwardsEveryDoorTheAdapterOffers] is what stops the
// next one going the same way.
func (f sessionCompleter) ProbeLanes(ctx context.Context, model string) {
	prober, ok := f.inner.(laneProber)
	if !ok {
		return
	}
	prober.ProbeLanes(ctx, model)
}

// FallbackModels passes the adapter's chain through, and answers nil for an
// inner completer that has none ([modelChain]).
//
// The wrapper is the one thing every request this agent makes goes through,
// which makes it the one thing standing between the turn loop and the models the
// adapter would move to. A wrapper that quietly swallowed the chain would leave
// a person who wrote a `models.fallbacks` row watching a turn die on a model
// that could not answer it.
func (f sessionCompleter) FallbackModels(model string) []string {
	chain, ok := f.inner.(modelChain)
	if !ok {
		return nil
	}
	return chain.FallbackModels(model)
}

// Close ends the session: an in-flight turn is cancelled and waited for, the
// running nodes of this session's graph are cut and waited for, background jobs
// are terminated, then the session file is flushed and closed.
//
// The steps are sequential, not concurrent. The turn's wait is first because a
// running turn is what still owes the journal messages; the graph's nodes and
// then the jobs come after, because a turn cancelled mid-tool-call may still be
// the thing that started the work being ended; the file closes last because
// every one of those can still write to it. Each round EXTENDS the quit rather
// than racing it: at most one jobShutdownGrace for the graph and one for the
// jobs, plus one for adaptive runs; each is a grace stragglers share.
//
// The wait is the point. A turn cancelled at Close still has messages to
// journal — the partial reply it kept, the steering it drained — and closing
// the file first would drop exactly the tail a resume needs. The wait is
// bounded because Close is what the person's quit reaches: a turn wedged
// inside a tool must not hold the process open, and past the grace the journal
// simply stops accepting writes rather than writing to a closed descriptor.
func (a *Agent) Close() error {
	// WHAT THE RECORD ALREADY HOLDS IS SETTLED BEFORE THE DOOR SHUTS, so an idle
	// session that read a landing in its last turn does not re-tell it tomorrow.
	// What the record does NOT hold stays owed, which is the whole point: a note
	// still sitting on the queue goes with this process and is said again by the
	// next one ([durableDelivery]).
	a.settleDeliveries()
	// AND SO IS EVERY WRITE THIS SESSION STILL OWES A FILE behind a person's path
	// — the meta.json stamp, the fix shelf's counters, the working copy of a folder
	// referred but not yet cut. A deferred write's whole risk is a process that
	// stops while one is owed, and this is the answer to it ([Agent.SettleWrites],
	// placemeta.go). IT IS HERE AND NOT PAST THE LOCK BELOW: every write it waits
	// on takes `a.mu` to read or replace what it is writing, so a call from inside
	// the lock would wait forever on work waiting for this goroutine.
	a.SettleWrites()
	// AND THE ENDED RUNS' STORES ARE GIVEN BACK. They are held open for the life
	// of the conversation because an ended store never changes ([planState.archives]),
	// and this is where that life ends. A second close finds none.
	a.closePlanArchives()
	a.mu.Lock()
	if a.closed {
		// A SECOND CLOSE WAITS FOR THE FIRST, AND DOES NOT ANSWER OVER THE TOP
		// OF IT. `closed` is set below before anything is cut, so returning here
		// on the strength of it used to tell a second caller that the session
		// had closed while its turn, its nodes and its jobs were all still
		// running — the same false sentence, one layer up, that this quit exists
		// to make true.
		//
		// The wait needs no bound of its own: the caller it is waiting for is
		// itself bounded, by the turn's grace and then one [jobShutdownGrace]
		// each for adaptive runs, the graph and the jobs. A nil channel means a
		// session closed before this field existed in it — impossible now that both
		// are written under this lock, and answered by returning rather than by
		// blocking forever.
		waitOn := a.closeDone
		a.mu.Unlock()
		if waitOn != nil {
			<-waitOn
		}
		return nil
	}
	a.closed = true
	// Nothing armed by a steer outlives the session that armed it
	// (steer_grace.go), and nor does a clock armed on a question (asklane.go).
	a.stopSteerGraceLocked()
	a.stopAskClocksLocked()
	if a.closeDone == nil {
		a.closeDone = make(chan struct{})
	}
	// No wall reader outlives the session. The channel is made only for an
	// unattended session with a wall, and the first Close is the only writer of
	// `closed`, so this close is taken exactly once under the lock the reader's
	// lifetime is guarded by.
	if a.wallStop != nil {
		close(a.wallStop)
	}
	// AS THE LAST ACT, past every round below and past the file's own close: a
	// deferred close runs after `return file.Close()` has evaluated, so a caller
	// released by it is released by a session that has finished leaving. Every
	// road out of this function goes through it, which is the reason it is a
	// defer and not a line at the end.
	defer close(a.closeDone)
	// No memory pass outlives the session. The cancel is what stops one waiting
	// on a provider; the wait below is what lets one that is already writing
	// reach the store (memory.go).
	memoryStop := a.memoryStop
	// No naming errand outlives the session either, on memoryStop's terms: the
	// cancel is what stops one waiting on a provider or sleeping out a backoff,
	// and the wait below is what lets one that has already earned a name write
	// it (title.go).
	titleStop := a.titleStop
	file := a.file
	cancel := a.cancel
	done := a.done
	// The graph THIS session owns, and nil for every session that does not: a
	// task worker reaches its parent's graph through config.tasker, and a nil
	// here is what keeps its close from shutting a door the parent still
	// writes through. Read directly rather than through [Agent.graph], which
	// would build a graph just to close it.
	tasks := a.tasks
	// Nothing queued will ever run now, and a caller holding one of those
	// channels is owed the close rather than a wait that never ends.
	a.dropFollowUpsLocked()
	// And so is a surface waiting for the next woken turn: no more will come,
	// and a lane left open is a pump waiting on a session that has left.
	for _, lane := range a.wakeLanes {
		close(lane)
	}
	a.wakeLanes = nil
	// And a surface watching for the name: no name will be minted now, and a
	// lane left open is a pump waiting on a session that has left (title.go).
	for _, watcher := range a.titleWatchers {
		watcher.close()
	}
	a.titleWatchers = nil
	// And an adaptive run: it holds a context of its own precisely because its
	// turn ended, so this is the only thing that can reach it (orchestrate.go).
	// A harness being designed is a task now, so what ends it is the graph's own
	// stop below, beside every other node (harness_task.go) — for years the line
	// here said the job round would cut it, and that sentence was the false
	// belief issue #381 was made of: the round walks a registry a node only
	// joins from inside its own goroutine.
	a.cancelOrchestrationsLocked()
	a.mu.Unlock()
	// AND THE PROCESS STOPS SAYING IT HOLDS THIS CONVERSATION, before anything
	// below can take time: a firing that lands during the quit writes to the
	// inbox rather than onto a queue that will never be drained again
	// (standing_run.go).
	forgetLiveSession(a)
	// AND THE LANE SHEET STOPS BEATING FOR A SESSION THAT HAS LEFT. It is cut
	// here, beside the line above and before anything that can take time,
	// because it is the one background lane that owes nothing to the quit: a
	// beat holds no write anybody is waiting for.
	a.stopLaneBeat()
	// AND THE BELT RUN, on the adaptive runs' own terms above: it holds a context
	// of its own precisely because the turn that proposed it ended, so this is
	// the only thing that can reach it. A RUN'S LIFE IS THE CONVERSATION'S. Until
	// this line the run's life was neither the turn's nor the conversation's but
	// the PROCESS's, so a conversation that ended while a run was going left
	// workers spending money against a room nobody could read or stop.
	//
	// It is cut out here rather than under the lock above because the run is
	// held under its own ([Agent.beltMu]) and nothing else in this package takes
	// the two together; the line above has already stopped anything new from
	// being started against this conversation.
	a.cutBeltRun()

	// EVERY CANCEL FIRST, THEN THE JOINS. The naming errand may be asleep in a
	// backoff or parked on a provider, and it is the one thing here that owes
	// the quit nothing: cutting it before the memory join — rather than after
	// it, on its own grace — is what keeps a title from putting itself in front
	// of the turn's own cancellation (title.go).
	if titleStop != nil {
		titleStop()
	}
	if cancel != nil {
		cancel(stopFor(StopByClosing))
	}
	if memoryStop != nil {
		a.waitForMemory(memoryStop)
	}
	// AND THE NAME IS JOINED AFTER THE CANCEL RATHER THAN WAITED FOR BEFORE IT.
	// The join is bounded and it is the only reason to wait at all: an errand
	// that has already earned a name owes the journal one line, and cutting the
	// process between the answer and the append would lose it (title.go).
	a.waitForTitle()
	a.closeDiscussions()
	if done != nil {
		timer := time.NewTimer(closeGrace)
		select {
		case <-done:
		case <-timer.C:
		}
		timer.Stop()
	}

	// Adaptive runs own worker journals outside the ordinary task graph. Their
	// cancellations were cut above; join their accepted lifetimes before any
	// store is closed. Setup may have created the graph since the first snapshot.
	a.waitOrchestrations()
	a.mu.Lock()
	tasks = a.tasks
	a.mu.Unlock()

	// THE GRAPH STOPS BEFORE THE JOBS ROUND, and it is a stop of its own because
	// a node is not reachable as a job until its goroutine has put it in the
	// registry: it cancels every node this session is running and waits, bounded
	// by the same grace, for their goroutines to return (task_run.go's
	// [TaskGraph.stopAll]). It shares that grace rather than adding a number,
	// and it is deliberately NOT [Agent.Abandon], which is one turn's bounded
	// stop at a person's escape and never touches a node. Only the session that
	// OWNS the graph does this — `tasks` was read above precisely so that a
	// worker's close cannot reach into its parent's.
	if tasks != nil {
		tasks.stopAll(jobShutdownGrace)
	}

	// A background job's lifetime is the session's: cancelling the turn above
	// says nothing to it (its process was deliberately never bound to a turn's
	// context — jobs.go), so quitting the session is where it ends. Every kill
	// here is a requested one, so no job reports its own death onto a steering
	// queue nobody will drain.
	if a.jobs != nil {
		a.jobs.shutdown(jobShutdownGrace)
	}

	// AND THE SESSION STOPS SAYING IT IS HERE, and takes its presence file with
	// it (taskpresence.go). It happens after the jobs round because work still
	// being killed is work another window may still be looking at, and it is
	// bounded so a quit never waits on a courtesy paid to somebody else's rail.
	a.stopPresence()

	// THE TASK CHECKPOINT STOPS TAKING WRITES HERE, after the turn was waited
	// for and the jobs were cut — every closing transition above has landed —
	// and before anything else can take time. A save that arrives later is a
	// goroutine that outlived the close (the measured one: a woken turn's
	// hand-off admitting its task as the test's directory was being removed),
	// and its graph would overwrite a settled checkpoint with a stale one
	// (task_store.go says the rest). Only the session that OWNS the graph may
	// do this — `tasks` was read above precisely so a worker's close cannot
	// reach the store its parent is still writing.
	if tasks != nil {
		tasks.store.close()
	}

	// The store's copy of the transcript is drained LAST of the writers and
	// before the file is closed, for the reason the turn is waited for: a
	// cancelled turn's tail is queued by the wait above, and closing the log
	// first would drop exactly the lines a compacted resume has nowhere else to
	// read (chatlog.go).
	a.chatlog.close()

	// AND THE DEFERRED WRITES ARE SETTLED A SECOND TIME, because the first one
	// above could only settle what was owed BEFORE the close — and the close is
	// itself a writer: a node stopped, a follow-up dropped, a place referred on
	// the way out each owe one, and every one of those happened in the rounds
	// between here and there. A write still owed when the process stops is the
	// whole risk a deferred write carries ([Agent.SettleWrites], placemeta.go).
	//
	// IT IS SAFE HERE AND ONLY HERE: `a.mu` was released above, and everything
	// this waits on takes that lock.
	a.SettleWrites()

	if file == nil {
		return nil
	}
	return file.Close()
}

// ── transcript ──────────────────────────────────────────────────────────────

// recordLocked appends one COMPLETED message to the transcript and journals
// it. The journal write happens under the same lock as the append so the file
// order is the transcript order by construction; it is one buffered append to
// an already-open file, not a place a turn waits.
func (a *Agent) recordLocked(message ai.Message) {
	a.alignReasoningLocked()
	a.messages = append(a.messages, message)
	a.messageReasoning = append(a.messageReasoning, provider.MessageReasoning{})
	if a.file != nil {
		a.file.appendMessage(message)
	}
	// AND THE STORE GETS IT TOO, when there is one. The journal is what a resume
	// replays; the store is what survives a compaction, which discards the
	// journal's pre-cut lines by definition. The call queues and returns
	// (chatlog.go), so this stays one append under the lock.
	a.chatlog.post(message)
}

// recordUserLocked is recordLocked for a message the PERSON sent: the same
// append, and a journal line that carries the message's durable references
// beside its text.
//
// The split exists because only a person's message can hold a part the journal
// must not write. Everything the model and the tools produce is text and goes
// through recordLocked exactly as before.
func (a *Agent) recordUserLocked(user userMessage) {
	// AND WHOEVER SENT IT IS OWED AN ANSWER ONLY IF THE RECORD REALLY HOLDS IT.
	// `durable` is set by each road below: the journal write answers whether the
	// line reached the file, and a write that failed may not settle a delivery —
	// nothing would ever say that landing again ([durableDelivery]). A session
	// with no journal at all settles on its transcript, which is the whole of the
	// record it has. The senders are told outside this lock
	// ([Agent.settleDeliveries]), which is where a checkpoint may be written.
	durable := false
	defer func() {
		if durable {
			a.holdSettledLocked(user)
		}
	}()
	a.alignReasoningLocked()
	a.messages = append(a.messages, user.message)
	a.messageReasoning = append(a.messageReasoning, provider.MessageReasoning{})
	// AND WHAT IS KEPT IS WHAT THEY SAID. A marked draft's message carries an
	// instruction the person never typed and never sees (standing_mark.go); the
	// turn reasons from it and nothing outlives it, because a replay is a
	// reading of the conversation and that paragraph was never part of one.
	kept := user.journaled()
	// The store's copy is taken before the journal's early return: a session
	// with no file still has a conversation worth keeping, and the person's own
	// words are the last thing that should depend on which layout they opened in.
	a.chatlog.post(kept)
	if a.file == nil {
		durable = true
		return
	}
	if user.authored {
		// THE SESSION'S OWN LINE IS MARKED AS ONE. The transcript keeps it
		// user-role, which is what the model has to read it as; the journal keeps
		// the one bit that says nobody typed it, so a resume can draw it where the
		// live surface drew it (sessionfile.go's [sessionEntry.Note]). A note
		// carries no pictures, which is why this door takes none.
		durable = a.file.appendNote(user.message, noteMarks{
			tags:       user.replyTags,
			deliveries: deliveryIDs(user.delivered),
		})
		return
	}
	if mark := user.steerRecord(); mark != nil {
		// A CORRECTION IS MARKED AS ONE, whichever door carried it. The transcript
		// keeps it an ordinary user message, which is what the model has to read it
		// as; the journal keeps the one bit that says it did not open the turn it
		// sits in, plus the instant and the landing account, so a page reopened
		// tomorrow draws it as the person's own correction rather than as a new
		// question (steer.go, task_room.go, sessionfile.go's [sessionEntry.Steer]).
		durable = a.file.appendSteer(kept, *mark)
		a.stampUserLocked(messageContentText(kept))
		return
	}
	durable = a.file.appendMessage(kept, user.refs...)
	// AND THE FOLDER LEARNS THE PERSON WAS HERE. Resume order is on when the
	// person last spoke and not on file mtime (place.go's [Meta.LastUserAt]),
	// and this line — the one place the person's own words reach the journal —
	// is the only honest witness to that. A session with no folder stamps
	// nothing (placemeta.go).
	a.stampUserLocked(messageContentText(kept))
}

func (a *Agent) record(message ai.Message) {
	a.mu.Lock()
	a.recordLocked(message)
	a.mu.Unlock()
}

// markTurnTruncated preserves the provider's stop reason after the response
// itself has gone. Node reports need this fact, while ordinary transcript
// messages deliberately contain only what the participants said.
func (a *Agent) markTurnTruncated() {
	a.mu.Lock()
	a.lastTurnTruncated = true
	a.mu.Unlock()
}

// turnTruncated is the reporting side of [Agent.markTurnTruncated].
func (a *Agent) turnTruncated() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastTurnTruncated
}

// journalOnly writes one message into this agent's journal WITHOUT putting it in
// front of the model.
//
// It is the door for something a person has to be able to READ BACK and the
// model must not be asked to reason from. Today that is one thing: the replies a
// harness designer streams into its own node's room, so that reopening a design
// tomorrow shows the writing of the page and not only the page (harness_task.go's
// designSeat). Two separate reasons keep those lines out of the transcript, and
// either alone would be enough.
//
//   - THE TRANSCRIPT IS THE THREAD'S BRIEF. The design thread already holds the
//     page that was actually written; a superseded draft beside it is a model
//     being invited to answer "what does step three do" out of the version that
//     was thrown away.
//   - AND A MESSAGE APPENDED MID-TURN IS AN ILLEGAL TRANSCRIPT. These lines land
//     while somebody may be talking to this same agent in the room, and an
//     assistant message that arrives between a tool call and its result is a
//     request the provider refuses. Nothing rebuilds a conversation out of the
//     journal here — a node's journal path is minted fresh for each life of the
//     node (task_run.go's taskJournalPath) and is never resumed — so the file
//     takes the line with no such ordering to break.
func (a *Agent) journalOnly(message ai.Message) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.file != nil {
		a.file.appendMessage(message)
	}
}

// snapshot copies the messages slice for one provider request. The copy is
// shallow and the elements are immutable once recorded, so this costs one
// slice header per step and buys a request that cannot be mutated underneath
// the client by a concurrent compaction. The model is not read here: it is the
// turn's, latched once by runTurn.
func (a *Agent) snapshot() []ai.Message {
	a.mu.Lock()
	defer a.mu.Unlock()
	messages := make([]ai.Message, len(a.messages))
	copy(messages, a.messages)
	return messages
}

// volatileNoteOpening is the first line of the note the two moving blocks ride
// in, and it does two jobs at once.
//
// It tells the MODEL what it is reading. The note arrives in the user role
// because that is the only role a transcript may grow in between an answer and
// the next request, and a block of state in the user role with no opening reads
// as the person having typed it. It is context, not somebody speaking.
//
// And it is what [Agent.landVolatileLocked] MATCHES ON to find the last note it
// landed. That is deliberately read out of the transcript rather than held in a
// field beside it: a compaction fold and a rewind both rebuild the transcript
// without asking this file, and a remembered "what I last said" would go on
// believing a note was still in front of the model long after the fold ate it.
const volatileNoteOpening = "A note from the session, not from the person: where the work stands right now. Facts, not requests — and the last such note is the one that holds."

// memoryNoteOpening is the first line of the note the ROUTED MEMORY BLOCK rides
// in, and it is a second opening rather than a second paragraph of
// [volatileNoteOpening] because the two move on different beats.
//
// The card moves when work lands. The memory block is re-routed against the
// person's own words at the start of every turn (memory.go's
// [Agent.refreshMemory]) and re-stamps every line with an age label that is
// hourly for anything learned today ([renderMemoryBlock]), so it can move on a
// turn where nothing about the work did. Riding both in one note would re-send
// up to memoryBlockRunes of memory every time a goal changed, and re-send the
// card every time the router reached for a different memory.
//
// It says the same last-one-holds sentence for the same reason: a memory that
// was superseded between turns leaves the note that carried it standing in the
// transcript, exactly where it was said, and the model has to be told which of
// them is current. That is the whole price of moving this block out of
// message[0], and it is stated here rather than left to be discovered.
const memoryNoteOpening = "A note from the session, not from the person: what is worth remembering here, from what this person has had codeaf keep. Facts, not requests — and the last such note is the one that holds."

// bashBeltFrameOpening is the first line of the note the bash belt's per-step
// frame rides in (docs/design/bash-task-loop/DESIGN.md, "The per-step frame").
//
// It is a third opening rather than a paragraph of [volatileNoteOpening]
// because the two move on different beats, for the same reason
// [memoryNoteOpening] is its own note: the card moves when work lands, and the
// frame moves at every step boundary. Riding both in one note would re-send the
// card every step, and a card that moved a hundred times a run is a hundred
// notes whatever file renders it.
//
// It says the same last-one-holds sentence the other two openings say, for the
// same reason: a frame that moved leaves the note that carried the older
// numbers standing in the transcript exactly where it was said, and the model
// has to be told which of them is current.
// frameCatchUp bounds how long the per-step frame waits for the node room's
// recorder to count the calls this turn already sent ([Agent.bashBeltFrame]).
// A quarter of a second is a hundred times the funnel's ordinary lag and far
// under a model call, so a worker never stalls on it and a loaded box never
// draws a stale number.
const frameCatchUp = 250 * time.Millisecond

const bashBeltFrameOpening = "A note from the session, not from the person: where this work stands, step by step. Facts, not requests — and the last such note is the one that holds."

// volatileBlockLocked renders the two blocks that MOVE WITH THE WORK: the state
// card, rewritten by the post-turn pass whenever a delta lands (card.go), and
// what the other windows on this project have landed and have running, re-read
// at the start of every turn (taskdelta.go).
//
// Both used to be rendered into message[0] beside the base prompt, and that is
// the bug this exists to have fixed: message[0] is in front of every message
// there is, so a card that moved re-priced the WHOLE conversation as a cold
// prefix on the next request. Invisible on a one-turn benchmark and brutal in
// the long interactive session that is this program's normal life.
//
// Empty is empty: a conversation with no card and no other window produces no
// note at all, which is the emptiness law and not an optimisation.
func (a *Agent) volatileBlockLocked() string {
	return strings.TrimSpace(a.cardText + a.elsewhereText)
}

// landVolatileLocked appends the volatile note to the transcript when what it
// says has moved since the last one landed, and does nothing whatsoever
// otherwise.
//
// IT IS AN APPEND AND NEVER A REWRITE, which is the whole of why this is cheap.
// Every request of a turn re-sends the transcript, and the provider bills the
// leading bytes at the cached rate only for as long as they are the SAME
// leading bytes: the transcript may grow at the back and may not change in the
// middle. So a note that moved is a new note at the tail, the one before it is
// left exactly where it was said, and the only thing anybody pays for twice is
// the note itself.
//
// It is also why the note is a real message rather than something stitched onto
// the request on its way out. A block appended after the transcript on every
// request would sit at a different index each time — request N's copy where
// request N+1 has a tool result — and the endpoints that cache behind explicit
// markers write their entry AT the last user or tool message
// (internal/provider's caching.go). Put the marker on something that moves and
// every request writes a cache entry no later request can ever read.
//
// A NOTE THAT WENT EMPTY IS NOT WITHDRAWN. The card never empties once it has
// something to say — a merge replaces the goal and appends to the lists — and
// the other windows' block does empty, as landings age out of the short memory
// behind it. Neither is worth a message: what an old note says was true when it
// was said, and a retraction would cost a message to tell the model nothing.
func (a *Agent) landVolatileLocked() {
	a.alignReasoningLocked()
	// A transcript with no system message is one nothing has opened yet, and a
	// note that landed there would BE message[0].
	if len(a.messages) == 0 {
		return
	}
	// THE MEMORY BLOCK IS THE FIRST OF THE TWO and rode in message[0] until this
	// wave. It is routed against the person's words at the start of every turn
	// (memory.go), so leaving it in front of the whole conversation re-priced the
	// entire transcript on any turn the router reached differently — the same bug
	// the card had, on a faster beat. See [memoryNoteOpening].
	// THE TEAM ROLE LANDS FIRST, because it is the one note that says what this
	// conversation is rather than what is around it, and it was composed from a
	// read made before this request ([Agent.teamBoundary]) rather than beside
	// the work, so it is never a step late (team.go's [teamRoleNoteOpening]).
	a.landTeamRoleLocked()
	a.landNoteLocked(memoryNoteOpening, strings.TrimSpace(a.memoryText))
	a.landNoteLocked(volatileNoteOpening, a.volatileBlockLocked())
	// AND A MANAGER'S TEAM, in a note of its own for the reason the memory block
	// has one: a team moves whenever a member does, and riding the card's note
	// would re-send the card each time (team.go's [teamNoteOpening]). A
	// conversation that manages nothing has an empty block and lands nothing.
	a.landNoteLocked(teamNoteOpening, a.teamBlockLocked())
}

// landNoteLocked appends one of the session's own notes when what it says has
// moved since the last note with the same opening, and does nothing whatsoever
// otherwise. The opening is the identity: two notes with two openings ride at
// the tail independently, and neither one's movement costs the other a byte.
func (a *Agent) landNoteLocked(opening, block string) {
	if block == "" {
		return
	}
	note := opening + "\n\n" + block
	if a.lastNoteLocked(opening) == note {
		return
	}
	// Not [Agent.recordLocked]: the note is not the conversation. Journaling it
	// would draw a block of state on the screen of anybody replaying the session,
	// on the row that is supposed to be theirs, and posting it to the store would
	// make it answer searches of what was said. It is context assembled for one
	// request, and a resume rebuilds it from the card and the index on the first
	// turn that needs it.
	a.messages = append(a.messages, textMessage("user", note))
	a.messageReasoning = append(a.messageReasoning, provider.MessageReasoning{})
}

// lastVolatileNoteLocked is the newest volatile note in the transcript, or ""
// when none has landed since the last fold. See [volatileNoteOpening] for why
// the transcript is the only place this is read from.
func (a *Agent) lastVolatileNoteLocked() string {
	return a.lastNoteLocked(volatileNoteOpening)
}

// lastNoteLocked is the newest note in the transcript carrying this opening.
func (a *Agent) lastNoteLocked(opening string) string {
	for index := len(a.messages) - 1; index >= 0; index-- {
		text := messageContentText(a.messages[index])
		if strings.HasPrefix(text, opening) {
			return text
		}
	}
	return ""
}

// isVolatileNote reports whether a user-role message is the session's own note
// rather than something anybody said.
//
// EVERY READER OF THE USER ROLE HAS TO ASK. The note is user-role because that
// is the only role the model can be told something in, and it is the second line
// in this package that has to be told apart from the person's own by a marker —
// the compaction note ([isCompactionNote]) is the first. Without this one the
// note would be drawn on the screen as a paragraph the person typed, offered as
// a rewind point in their own words, and quoted by `/why` as the instruction the
// turn is working on. It is recognized by the opening it is built with and never
// by guessing at wording.
//
// BOTH OPENINGS ANSWER YES. There are three of these notes now — the card and
// the other windows in one, the routed memory block in the second
// ([memoryNoteOpening]), and the bash belt's per-step frame in the third
// ([bashBeltFrameOpening]) — and every caller of this asks the same question
// about all of them: is this user-role message something a person typed. None
// of them is.
func isVolatileNote(text string) bool {
	return strings.HasPrefix(text, volatileNoteOpening) ||
		strings.HasPrefix(text, memoryNoteOpening) ||
		strings.HasPrefix(text, bashBeltFrameOpening) ||
		strings.HasPrefix(text, teamNoteOpening) ||
		strings.HasPrefix(text, teamRoleNoteOpening)
}

// mayBashBelt is [Config.mayBashBelt] asked of a live agent, so that the
// runner-side roads and the drain read the same predicate the belt was built
// from, under the same lock — the shape [Agent.signsGitWork] established for
// exactly this reason. The one-reading law every belt verb follows means the
// flag is asked here and nowhere else on a built agent.
func (a *Agent) mayBashBelt() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.config.mayBashBelt()
}

// bashBeltFrame renders the per-step frame a bash-belt worker reads at each
// step boundary — the step count against the node's step cap and the steps it
// has left, the run's spend against the conversation's spend rail (or, with no
// rail set, the node's spend so far), the fan-out slots still free, one line of
// family news and one line of child news — or "" when there is nothing to say.
// docs/design/bash-task-loop/DESIGN.md, "The per-step frame", is the shape's
// authority and this is its whole mechanism: a rendering at the drain, and
// nothing else.
//
// EVERY NUMBER IN IT IS ALREADY COUNTED. The steps are the node room's own
// recorder count (task_live.go's [taskLive.steps]), which is the same unit the
// runner's thresholds are counted in (task_child_run.go), and the steps it has
// left are that count subtracted from the cap the node named ([TaskNode.limits]
// reads the same record the runner's checkpoint seam does); the spend with a
// limit set is the plan store's per-project rollup for the run
// ([TaskGraph.planRunSpend]), the figure every worker's calls feed, and without
// one it is [TaskNode.spend], the figure the runner already publishes at every
// step's end ([childRun.tellSpend]); the free slots are the fan cap
// [TaskGraph.claimChild] enforces, counted the way that cap counts it; the
// family news is the owed-and-outstanding pair the runner reads at
// [childRun.step]; and the child news is this node's own children and where
// each stands, the graph's own nodes. The frame composes from those and invents
// no counter, no diff and no state of its own: a worker that is not on the bash
// belt composes nothing, and a worker that is still gets nothing when the facts
// behind it are zero, because [Agent.landNoteLocked] lands an empty block as
// nothing at all.
//
// THE LOCKS ARE TAKEN IN THE ORDER THE PACKAGE ALREADY USES, and this method
// is built to be called OUTSIDE the agent's own lock. The spend is read before
// the graph's lock for the reason [TaskNode.notice] states — it asks the room
// for this agent and this agent for its own usage, and taking those under the
// graph's lock would be a second lock order in a package that has one — and
// the family pair is read through [Agent.taskNewsStanding] because owed and
// outstanding are one fact, and reading them as two is exactly what its law
// refuses. The fan cap and the children are read under the graph's lock, the
// same lock the cap is taken under and the same lock the children's own state
// is kept under, and for the same reason the caller composes this before taking
// a.mu: nothing under the graph's lock may run inside it.
func (a *Agent) bashBeltFrame(hub *eventHub) string {
	a.mu.Lock()
	belt, graph, id := a.config.mayBashBelt(), a.config.tasker, a.config.taskID
	a.mu.Unlock()
	if !belt || graph == nil || id == 0 {
		return ""
	}
	node := graph.node(id)
	if node == nil {
		return ""
	}
	// THE RUN'S LIMIT AND ITS ROLLUP, read before the graph's lock — the plan's
	// gate may be taken before the graph's mu, never after it, and this is the
	// one road in the frame that takes the gate. The limit is the conversation's
	// own spend rail: a worker's config carries none ([newTaskAgentOn] builds
	// its literal field by field), and the conversation that owns the run is
	// where a person set one, read under the home agent's lock the way
	// [childRun.tellSpend] reaches home. The rollup is the plan store's
	// per-project total for the run, opened and read fresh the way every pulse
	// reads the store.
	rail := 0.0
	if home := graph.home; home != nil {
		home.mu.Lock()
		rail = home.config.SpendRailUSD
		home.mu.Unlock()
	}
	runSpend := graph.planRunSpend()
	spend := node.spend()
	node.graph.mu.Lock()
	recorder := node.room.recorder()
	maxSteps := thresholdOr(node.spec.maxSteps, taskMaxSteps)
	// THE FAN CAP IS THE ONE [TaskGraph.claimChild] HOLDS TO, counted exactly the
	// way that cap counts it: the slots this node is holding for proposals in
	// flight, plus the admitted children that have taken their own. The count is
	// taken here rather than through the cap because the cap answers the model a
	// refusal and this only renders the room.
	held := node.graph.claims[id]
	kidsNew, kidsSettled := 0, 0
	for _, kid := range node.graph.order {
		child := node.graph.nodes[kid]
		if child == nil || child.parent != id {
			continue
		}
		held++
		if child.state == TaskRunning || child.state == TaskQueued {
			kidsNew++
		} else {
			kidsSettled++
		}
	}
	node.graph.mu.Unlock()
	// The tail is asked for as zero because the frame wants the count and not
	// the narrative: the steps are the same number the room already shows
	// everybody but the model, and a copy of two hundred quoted lines would buy
	// a rendering that reads none of them.
	steps := recorder.state(0).Steps
	// THE COUNT IS READ ONLY ONCE IT HAS COUNTED WHAT THIS TURN SENT. The
	// recorder is fed on the runner's goroutine ([taskRoom.publish]) while this
	// worker is already composing its next request, so the count read the
	// instant after a tool end can be one behind the call that just finished —
	// and a frame one step behind is the same note twice, which lands as
	// nothing. The hub knows how many ends this turn sent and the recorder's
	// count at the turn's opening drain, so the frame waits for their sum
	// ([taskLive.awaitSteps]); the wait is nothing when the funnel is level, and
	// bounded when it is not. A batch run with no hub reads the count as it is.
	if hub != nil {
		hub.mu.Lock()
		finished, atOpen := hub.finishedCalls, hub.stepsAtOpen
		if finished == 0 {
			hub.stepsAtOpen, atOpen = steps, steps
		}
		hub.mu.Unlock()
		if finished > 0 {
			steps = recorder.awaitSteps(atOpen+finished, frameCatchUp)
		}
	}
	owed, working := a.taskNewsStanding()

	var lines []string
	// ZERO RENDERS AS NOTHING, and the line is the room's own shape: the step
	// count against the cap the node named ([TaskNode.limits] reads the same
	// record the runner's checkpoint seam does), the steps left in that cap, and
	// the money in the same two decimals [taskRowText] draws, and never a $0.00
	// made up for a model that has published no price ([TaskNode.spend] says why
	// that is a lie rather than a figure). A node that has finished no call has
	// no step to be on and draws nothing.
	if steps > 0 {
		stepLine := fmt.Sprintf("step %d/%d", steps, maxSteps)
		// THE MONEY HAS TWO RENDERINGS. With a limit set, the figure is the
		// run's — the store's rollup for the whole plan against the limit the
		// conversation was given, in place of the session-only figure this
		// clause used to draw, which was one worker's share of a run the other
		// workers are also billing. A run whose rollup is still nothing — no
		// plan, no charged row, or the best-effort write not landed yet — falls
		// back to the session-only figure, so a limit never renders without a
		// number behind it. With no limit set the clause is the one it has
		// always been: this node's own spend so far. Either road keeps the zero
		// law: no $0.00 invented for a model that has published no price
		// ([TaskNode.spend] says why that is a lie).
		switch {
		case rail > 0 && runSpend > 0:
			stepLine += fmt.Sprintf(" · spent $%.2f of $%.2f", runSpend, rail)
		case rail > 0 && spend > 0:
			stepLine += fmt.Sprintf(" · spent $%.2f of $%.2f", spend, rail)
		case rail <= 0 && spend > 0:
			stepLine += " · $" + strconv.FormatFloat(spend, 'f', 2, 64) + " so far"
		}
		// AND THE STEPS LEFT ARE THE CAP MINUS THE COUNT, drawn only when there
		// are any: a node at its last step has no step left to be told about.
		if left := maxSteps - steps; left > 0 {
			stepLine += fmt.Sprintf(" · %d steps left", left)
		}
		lines = append(lines, stepLine)
	}
	// AND THE FREE SLOTS ARE THE CAP MINUS WHAT IT HOLDS, drawn only for a node
	// that has fanned out at all — the slots a node that never divided still
	// holds are not news it has to be told — and drawn as nothing when none are
	// left, the point at which [TaskGraph.claimChild] is the door that answers.
	if free := taskFanLimit - held; held > 0 && free > 0 {
		lines = append(lines, fmt.Sprintf("free slots: %d", free))
	}
	// AND THE FAMILY'S NEWS IS THE PAIR THE RUNNER'S OWN DRAIN READS
	// (task_child_run.go). A report is in hand until the request about to go
	// out carries it, and parts still working are the reason this step may be
	// one about waiting. A node that never divided draws no family line at all.
	if owed > 0 || working {
		reports := fmt.Sprintf("%d reports", owed)
		if owed == 1 {
			reports = "1 report"
		}
		family := "family: "
		if owed > 0 {
			family += reports + " in hand"
			if working {
				family += "; "
			}
		}
		if working {
			family += "parts still working"
		}
		lines = append(lines, family)
	}
	// AND THE CHILD NEWS IS THIS NODE'S OWN CHILDREN AND WHERE EACH STANDS, read
	// off the graph's nodes and nothing else: a child still running or queued is
	// new, an ending of any kind has settled it. Each half is drawn only when it
	// is not zero, and a node with no children draws no line at all.
	if kidsNew > 0 || kidsSettled > 0 {
		var parts []string
		if kidsNew > 0 {
			parts = append(parts, fmt.Sprintf("%d new", kidsNew))
		}
		if kidsSettled > 0 {
			parts = append(parts, fmt.Sprintf("%d settled", kidsSettled))
		}
		lines = append(lines, "parts: "+strings.Join(parts, ", "))
	}
	return strings.Join(lines, "\n")
}

// drainSteering moves queued messages into the transcript at a step boundary
// and reports how many source notes landed.
//
// The hub is here for one reason: THIS DRAIN IS WHERE A STEER BECOMES TRUE
// (steer.go). A sentence the person spliced into this turn is consumed at the
// instant it is recorded here — not when they typed it — and the surface holding
// its stream is told so on the same beat. A nil hub is a batch run with no turn
// around it and says nothing, exactly as every other send on this path does.
//
// AMBIENT NEWS JOINS ONLY AT A TURN BOUNDARY OR BESIDE OWED NEWS. The opening
// request is a turn boundary; later calls in the same tool loop are only step
// boundaries and leave a watch's updates held. An owed task or job note is the
// exception because the next request must carry it, and splitting the same
// interval into an owed note now and an ambient note later would be two accounts
// where one batch is the honest shape.
func (a *Agent) drainSteering(hub *eventHub) int {
	// the first moment there is no lock to write a checkpoint under.
	defer a.settleDeliveries()
	// THE FRAME IS COMPOSED BEFORE THIS AGENT'S OWN LOCK IS TAKEN, because it
	// reads the graph and the family pair and neither of those may be taken
	// under a.mu ([Agent.bashBeltFrame] states the order these locks have). The
	// composition is cheap on every belt but the bash belt's: the predicate is
	// the first thing it reads, and an agent that is not on the experiment
	// composes nothing and lands nothing.
	frame := a.bashBeltFrame(hub)
	// AND WHAT THIS CONVERSATION'S TEAMS SAID TO IT, read on the same side of
	// the lock for the same reason: it is a stat of the teams file and of each
	// Traffic log, and a read only when one of them moved (team.go's
	// [Agent.teamBoundary]). What comes back is one marked note on the steering
	// queue, so the drain below puts it in front of this very request: at the
	// turn's opening and at every step after it, which is what makes a line the
	// manager sends mid-turn land mid-turn. A conversation in no team pays one
	// stat, and one with no profile or a task node pays nothing.
	if news := a.teamBoundary(); news != "" {
		a.enqueueNote(userText(news))
	}
	a.mu.Lock()
	opening := !a.running || len(a.messages) == a.turnFloor
	// AND THE VOLATILE NOTE LANDS HERE, ahead of the steering, for the reason the
	// drain itself is here: this seam runs immediately before the next request
	// (loop.go) with the transcript tail on a tool result or an assistant answer,
	// which is the one shape a user message may legally follow. Ahead rather than
	// behind because the note is the ground the person's line is said against.
	a.landVolatileLocked()
	// AND THE BASH BELT'S FRAME LANDS BESIDE THE CARD, at the same drain point
	// and ahead of the steering, because this seam is the one that runs
	// immediately before the next request (loop.go) and the frame is the one
	// note whose whole job is to say where the work stands at that instant. It
	// rides its own note rather than the card's because the two move on
	// different beats ([bashBeltFrameOpening]), and [landNoteLocked] lands an
	// empty frame as nothing at all, which is the flag-off road and the zero
	// road alike.
	a.landNoteLocked(bashBeltFrameOpening, frame)
	// WHICH DIRECTIONS THIS REQUEST WILL CARRY, read before the drain empties the
	// queue and acted on after the lock is released. This is the only place a
	// direction becomes READ (assignment.go): the turn's END drain reaches the
	// same queue and carries nothing into a request, so it must not mark one.
	carried := queuedDirections(a.steering)
	// THIS DRAIN IS THE ONE THAT ANSWERS. It runs immediately before the next
	// request (loop.go), so anything on the queue is in front of the model from
	// here — which is precisely what a task node's runner is waiting to be true
	// of its sub-tasks' reports (see [Agent.postTaskNews]). The turn's END drain
	// deliberately does not clear it: those notes reached the transcript and no
	// request.
	a.taskNotes = 0
	includeAmbient := opening || boundaryNoteHeld(a.steering)
	landed, _ := a.drainQueuedLocked(hub, includeAmbient)
	a.mu.Unlock()
	// Outside this agent's lock, because it takes the graph's (assignment.go) and
	// there is no order in which those two are ever taken the other way round.
	a.markDirectionsCarried(carried)
	return landed
}

// queuedDirections is every node receipt id on one queue.
func queuedDirections(queued []userMessage) []uint64 {
	var ids []uint64
	for _, message := range queued {
		ids = append(ids, message.directions...)
	}
	return ids
}

// drainSteeringLocked is the TURN-boundary drain for callers already holding
// a.mu. The turn's end must take both queues and clear running without a gap;
// tests and batch runners call the same complete drain directly.
//
// It reports how many messages landed AND whether any of them was a wake note
// (see [Agent.enqueueSteering]). The second answer only means anything to the
// turn's end: a note drained at a step boundary is one the next request carries,
// so the model answers it as part of the turn it is already in, while a note
// drained after the last request is one nobody has said a word about.
func (a *Agent) drainSteeringLocked(hub *eventHub) (int, bool) {
	return a.drainQueuedLocked(hub, true)
}

// drainQueuedLocked drains steering and, when includeAmbient says this is a
// legal boundary for it, the ambient queue. External background-news lines
// become one authored user-role message; control guidance and a person's steer
// remain their own messages.
func (a *Agent) drainQueuedLocked(hub *eventHub, includeAmbient bool) (int, bool) {
	for _, message := range a.discussionHistory {
		a.recordLocked(message)
	}
	a.discussionHistory = nil
	for _, id := range a.discussionPending {
		a.discussionRecorded[id] = true
	}
	a.discussionPending = nil
	a.forgetRecordedDiscussionsLocked()
	queued := a.steering
	a.steering = nil
	if includeAmbient {
		queued = append(queued, a.ambient...)
		a.ambient = nil
	}
	sourceCount := len(queued)
	queued = coalesceSessionNotes(queued)
	owed := false
	for _, message := range queued {
		a.recordUserLocked(message)
		// AND A SPLICED SENTENCE IS NOW A SENTENCE THE MODEL HAS (steer.go). It
		// is said here rather than at the queueing because this is the drain that
		// answers — see the caller — so the event and the record become true in
		// the same breath. The turn's END drain reaches no steer at all: they are
		// lifted off the queue before it ([Agent.liftSteersLocked]).
		a.consumedSteerLocked(hub, message)
		a.replyTags = append(a.replyTags, message.replyTags...)
		// A STEERING MESSAGE IS STILL THE PERSON ASKING. It arrives mid-turn and
		// is often the correction the work about to be handed off must carry, so
		// the newest thing they typed is what a proposal made after this drain
		// quotes (task_brief.go).
		a.rememberAskLocked(message)
		// AND WHAT A RESULT DRAINED HERE IS THE RESULT OF, so a landing that
		// arrives mid-turn is owed by the turn it arrives in (wakecause.go).
		a.rememberOwedLocked(message)
		// AND IT IS STILL THE PERSON WAITING. Two kinds of line here are owed a
		// sentence: a wake note, which is work the harness did that nobody
		// watched, and a message with no `authored` mark — which is the person
		// themselves, typing, and there is no third thing that can be. An AMBIENT
		// note is the one line that is owed nothing ([Agent.enqueueAmbientNote]):
		// context nobody asked for, and starting a turn to speak about it would
		// be the session talking to itself out loud.
		//
		// This answer only means anything at the turn's END — see the caller —
		// and it is what keeps a sentence typed in the last seconds of a turn
		// from landing in the transcript with nobody ever asked about it.
		owed = owed || message.wake || !message.authored
	}
	return sourceCount, owed
}

// boundaryNoteHeld reports whether the next step is already owed external news.
// A person's steer does not open the ambient gate: it must arrive in their own
// words, without unrelated watch output attached to their correction.
func boundaryNoteHeld(queued []userMessage) bool {
	for _, message := range queued {
		if message.authored && message.batch {
			return true
		}
	}
	return false
}

// coalesceSessionNotes replaces every batch-marked background-news line in one
// drain with one authored batch at the first such line's position. This
// preserves the ordering and identity of person-authored steers and internal
// control notes while ensuring a provider sees one "while you worked" message
// rather than a run of synthetic user rows.
func coalesceSessionNotes(queued []userMessage) []userMessage {
	var notes []userMessage
	first := -1
	for index, message := range queued {
		if !message.authored || !message.batch {
			continue
		}
		if first < 0 {
			first = index
		}
		notes = append(notes, message)
	}
	if len(notes) == 0 {
		return queued
	}

	batched := batchSessionNotes(notes)
	out := make([]userMessage, 0, len(queued)-len(notes)+1)
	for index, message := range queued {
		if index == first {
			out = append(out, batched)
		}
		if !message.authored || !message.batch {
			out = append(out, message)
		}
	}
	return out
}

// batchSessionNotes renders one boundary note. Repeated watches collapse by
// stable key to a count and their newest summary; every other note keeps its
// complete text because task and hand reports have no other source the model
// can safely infer from.
func batchSessionNotes(notes []userMessage) userMessage {
	type repeated struct {
		label  string
		count  int
		latest string
	}
	repeats := make(map[string]*repeated)
	order := make([]string, 0, len(notes))
	parts := make([]string, 0, len(notes))
	var (
		wake         bool
		otherResults bool
		tags         []TaskReplyTag
		delivered    []durableDelivery
	)
	for _, note := range notes {
		wake = wake || note.wake
		otherResults = otherResults || note.otherResults
		tags = append(tags, note.replyTags...)
		// The batch is the record these notes end up in, so it carries what
		// settles each of them ([durableDelivery]); dropped here, every landing in
		// the batch would be re-told on the next resume.
		delivered = append(delivered, note.delivered...)
		if note.batchKey == "" {
			parts = append(parts, strings.TrimSpace(note.text()))
			order = append(order, "")
			continue
		}
		group := repeats[note.batchKey]
		if group == nil {
			group = &repeated{label: note.batchLabel}
			repeats[note.batchKey] = group
			order = append(order, note.batchKey)
		}
		group.count++
		group.latest = strings.TrimSpace(note.text())
	}

	rendered := make([]string, 0, len(order))
	part := 0
	for _, key := range order {
		if key == "" {
			rendered = append(rendered, parts[part])
			part++
			continue
		}
		group := repeats[key]
		if group == nil {
			continue
		}
		rendered = append(rendered, fmt.Sprintf("%s: %s — latest: %s",
			group.label, countedUpdates(group.count), group.latest))
		delete(repeats, key)
	}
	text := "while you worked: " + strings.Join(rendered, "; ")
	if strings.Contains(text, "\n") {
		text = "while you worked:\n\n" + strings.Join(rendered, "\n\n")
	}
	return userMessage{
		message:      textMessage("user", text),
		replyTags:    tags,
		otherResults: otherResults,
		delivered:    delivered,
		wake:         wake,
		authored:     true,
	}
}

func countedUpdates(count int) string {
	if count == 1 {
		return "1 update"
	}
	return fmt.Sprintf("%d updates", count)
}

// takeReplyTags hands the surface each finished-task identity exactly once.
func (a *Agent) takeReplyTags() []TaskReplyTag {
	a.mu.Lock()
	defer a.mu.Unlock()
	tags := append([]TaskReplyTag(nil), a.replyTags...)
	a.replyTags = nil
	return tags
}

// enqueueSteering puts one OWED line the session authored — principally a task
// node landing — onto the queue the person's steering rides, and wakes the
// session if nobody is working.
//
// The lane is shared on purpose. Both are news that arrives while the model is
// busy, both must land at a step boundary rather than inside a tool batch, and
// both belong in the transcript as plain user-role text. The authored mark lets
// the drain batch session news without changing a person's steering into
// somebody else's words.
//
// ── WHY IT WAKES, AND WHY THE OTHER LANE DOES NOT ──
//
// A queue alone was the whole defect. A person hands off a task, the work runs
// for eleven minutes, it lands — and the note sat here until the person happened
// to type something else, so what they got for their research was a card and
// silence. The answer they asked for is a SENTENCE from the model ("the
// comparison is at ~/oauth.md; the short version is…"), and a model that is never
// asked never writes one. So a note that lands on an idle session starts a turn.
//
// THAT IS TRUE OF A BACKGROUND JOB'S ENDING, through [Agent.enqueueJobNote].
// The model started the job and was explicitly told not to poll because the
// ending would come back, so the exit remains owed. IT IS TRUE OF A WATCH'S
// FIRING FOR THE SAME REASON: a watch that matched, went quiet or failed its way
// out has answered what it was started for and will never speak again, so that
// last note is owed and [Agent.enqueueWatchNote] queues it here. Only a watch's
// repeated DELTAS are different — telemetry with a complete log behind them —
// and that lane holds those for a turn boundary instead.
//
// It is not a turn per event. Everything below coalesces: the notes queue, the
// FIRST owed one starts a turn, and one authored batch lands at the next legal
// boundary. The rail, InTask, Close and the not-yet-open session all still
// decline ([Agent.wakeLocked]).
//
// [Agent.enqueueAmbientNote] is the same step queue with the wake removed. It
// carries guidance for the turn that is already running — recovery, loop
// control, an OAuth connection — while [Agent.enqueueWatchNote] is the separate
// turn-boundary queue for periodic telemetry. Both are real context nobody is
// standing there waiting for, but only the former is an instruction to the
// current turn.
//
// A closed agent drops the note rather than queueing it: after Close nothing
// drains, and the journal it would be written to is already shut.
func (a *Agent) enqueueSteering(text string) {
	a.enqueueNote(wakeNote(text))
}

// enqueueSettleSteering is [Agent.enqueueSteering] for A LANDING THAT HANDS THE
// MODEL A DECISION: the same lane and the same addressing, with the wake marked a
// settle one so the turn it starts runs under the checker's own bound rather than
// the run's wall ([settleWake]). The person pressed "let codeaf decide", which is
// exactly what `task.settle = auto` does at a landing, so the bound is the same.
func (a *Agent) enqueueSettleSteering(text string, ceiling int) {
	note := wakeNote(text)
	note.settle, note.settleCeiling = true, ceiling
	a.enqueueNote(note)
}

// enqueueJobNote is the registry's owed lane, and what it carries is the ending
// as [jobRegistry.settleExit] composed it. The whole log remains available
// through `jobs output` for anything past the tail.
func (a *Agent) enqueueJobNote(text string) {
	a.enqueueNote(jobNote(text))
}

// enqueueWatchNote is the registry's watch lane, and it is TWO lanes chosen by
// what the news actually is (jobs.go's notifyWatch carries the difference).
//
// A TICK IS AMBIENT. Repeated deltas from one watch remain separate until a
// boundary can truthfully count and collapse them, and never wake a session by
// themselves: a delta is telemetry with a complete log behind it, and a turn per
// delta turns a quiet observer into an autonomous conversation.
//
// THE FIRING IS OWED. When a watch fires it has answered the question it was
// started for and it is over — nothing further will ever come from it. Held for
// a turn boundary that is a conversation which learned the thing it was waiting
// for and said nothing until the person happened to type: measured 2026-08-31,
// a session watching `gh pr checks` for the checks to settle. So it goes on the
// step queue with the wake mark, exactly as a background job's exit does
// ([Agent.enqueueJobNote]), and the turn it starts is read for what remains by
// the checkpoint's `user.wake` law like any other woken turn.
//
// AND IT KEEPS ITS COMPLETE TEXT. A firing note is one sentence naming the terms
// that were met plus the single line of evidence behind them, which is the whole
// of the answer; the batch key that collapses ticks to a count would spend that
// evidence to save a line, and [userMessage.batchKey] already states that owed
// news does not carry one.
func (a *Agent) enqueueWatchNote(name, text string, fired bool) {
	if fired {
		note := wakeNote(text)
		note.otherResults = true
		a.enqueueNote(note)
		return
	}
	// A tick is delivered as PROGRESS, which is the kind whose whole content is
	// that it may not start a turn ([mailbox]): the queue it lands on is chosen
	// by the kind rather than by each caller remembering which door not to use.
	a.accept(delivery{origin: fromRuntime, kind: msgProgress, note: watchNoteMessage(name, text)})
}

// enqueueAmbientNote queues step-scoped session guidance nobody is waiting on
// and never starts a turn. Unlike periodic watch telemetry, recovery and loop
// guidance is an instruction for the next model step and stays on steering.
func (a *Agent) enqueueAmbientNote(text string) {
	a.enqueueNote(userText(text))
}

// enqueueAmbient is the periodic-watch queue's one door. It mirrors
// enqueueNote's authorship and closed-session laws without putting telemetry
// where a mid-turn step drain can take it by accident.
func (a *Agent) enqueueAmbient(note userMessage) bool {
	text := strings.TrimSpace(note.text())
	if text == "" {
		return false
	}
	note.message = textMessage("user", text)
	note.authored = true
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.ambient = append(a.ambient, note)
	return true
}

// steeringHeld reports whether a line SAID INTO THIS NODE FROM OUTSIDE is on
// the queue with no request having carried it yet — the person's own words
// ([steerNote]) or another agent's coordination ([relayNote]), which differ in
// authorship and not in what the runner owes them. It is what stops a node's
// runner closing the agent on top of a sentence somebody is waiting for an
// answer to (task_run.go's [runTaskChild]); every drain empties the queue, so it
// answers false again the moment the line is in front of the model.
func (a *Agent) steeringHeld() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, message := range a.steering {
		if message.steered {
			return true
		}
	}
	return false
}

// enqueueNote is the queue itself, and it reports whether the note was taken:
// false is a closed agent, whose queue nothing will ever drain again.
func (a *Agent) enqueueNote(note userMessage) bool {
	text := strings.TrimSpace(note.text())
	if text == "" {
		return false
	}
	note.message = textMessage("user", text)
	// Every OWED session note is the session's words. It is set here, at the one
	// door they come through, rather than at each constructor — and a line the
	// person steered in is neither, which is what [steerNote] says.
	if !note.steered {
		note.authored = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false
	}
	a.steering = append(a.steering, note)
	if note.steered || note.ending {
		// A NODE'S RUNNER IS WAITING TO BE TOLD, and this is the same release a
		// report makes without being a report ([Agent.postTaskNews]). It is done
		// under the lock the append is done under so there is no instant in
		// which the line is queued and the waiter has not been woken for it.
		//
		// A JOB'S ENDING RIDES WITH ITS OWN APPEND FOR THAT EXACT REASON. A worker
		// parked on a command it started (task_job_park.go) is waiting for this
		// note and nothing else, and every shape of release that fired somewhere
		// NEAR the append instead of with it had an interleaving that let the
		// worker wake to an empty queue: the reaper releasing from inside a
		// person's kill before that person's line was written, and a job exiting
		// on its own in the instant a person pressed stop. Both close here,
		// where the queue and the wake are one step ([userMessage.ending]).
		a.releaseTaskWaitLocked()
	}
	if note.wake {
		// A turn already running is the coalescing case and needs nothing done:
		// wakeLocked declines, and the note lands in that turn at its next step
		// boundary exactly as a person's steering does.
		a.wakeLocked()
	}
	return true
}

// holdSettledLocked keeps the acknowledgements this message earned until they
// can be sent outside a.mu. The caller holds the lock.
func (a *Agent) holdSettledLocked(user userMessage) {
	a.settling = append(a.settling, user.delivered...)
}

// settleDeliveries tells the senders of everything this conversation has
// RECORDED that their news arrived. It runs with no lock held, because settling
// a landing writes a checkpoint.
//
// The two callers are the two moments the record is known to be on disk: the
// drain that puts a message in front of the model, and the close. A crash
// between the record and this call re-tells the landing on resume — a duplicate
// rather than a loss, which is the direction this has to fail in.
func (a *Agent) settleDeliveries() {
	a.mu.Lock()
	settling := a.settling
	a.settling = nil
	a.mu.Unlock()
	for _, delivery := range settling {
		if delivery.settled != nil {
			delivery.settled()
		}
	}
}

// postTaskNews records that one of this agent's OWN sub-tasks has handed over
// its report, and releases whoever is waiting to hear it.
//
// A NODE'S TURNS ARE ITS RUNNER'S TO START, which is why this is not a wake:
// [Agent.wakeLocked] declines inside a task and must, because a node starting
// turns of its own would be a second conversation inside a worktree with nobody
// reading it. So a child's report is COUNTED here, and the runner that is
// already holding the parent open re-enters the model with it (task_run.go's
// [runTaskChild]). The count is cleared by the drain that puts those notes into
// a request ([Agent.drainSteering]), so "outstanding" means what it says.
func (a *Agent) postTaskNews() {
	a.mu.Lock()
	a.taskNotes++
	a.releaseTaskWaitLocked()
	a.mu.Unlock()
}

// releaseTaskWaitLocked ends the wait a node's runner is holding, for callers
// already holding a.mu. It is separate from the count above because a report is
// not the only thing a parked parent has to wake for: the person's own steered
// line is the other, and it is news without being a report
// ([taskRoom.steerIn]).
func (a *Agent) releaseTaskWaitLocked() {
	if a.taskNews != nil {
		close(a.taskNews)
		a.taskNews = nil
	}
}

// taskNewsWait is the generation a runner takes BEFORE it asks whether anything
// is outstanding. Holding the channel first is what closes the gap between the
// question and the wait: a report that lands in it closes this channel, so the
// wait returns immediately instead of missing the news it was waiting for.
func (a *Agent) taskNewsWait() <-chan struct{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.taskNews == nil {
		a.taskNews = make(chan struct{})
	}
	return a.taskNews
}

// taskNewsOwed reports how many sub-task reports this agent has been handed
// that no request has carried yet.
func (a *Agent) taskNewsOwed() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.taskNotes
}

// taskNewsStanding answers BOTH of the questions a parked parent asks of one
// instant — is anything this agent handed out still outstanding, and is anything
// owed to the model that no request has carried — and answers them AS ONE FACT.
//
// ── THE PAIR IS ONE READING, BECAUSE THE DELIVERY IS ONE WRITING ──
//
// [Agent.deliverTaskNote] states the law from the writing side: THE QUEUE, THEN
// THE FACT, THEN THE WAKE, AND NEVER IN ANY OTHER ORDER — so that a waiter which
// sees a child no longer outstanding also sees its note owed. Two separate reads
// cannot hold that promise in EITHER order, because the delivery's writes live
// under two different locks and one landing between the reads is seen half-done:
//
//	owed, then outstanding    the mark arrives between them, so the runner reads
//	                          "a note owed, nothing outstanding", takes its
//	                          integration turn carrying every report — correctly —
//	                          and is then told about news that turn already
//	                          carried. One model turn spent on an empty request,
//	                          which is real money and a blank exchange in the room.
//	outstanding, then owed    the same tear the other way up: the wake has not
//	                          landed yet, so the runner reads "nothing
//	                          outstanding, nothing owed" and leaves its loop with
//	                          a report sitting unread on the queue.
//
// So the two writes are made under this lock ([Agent.handOverTaskNews]) and the
// two reads are taken under it here, and no reader can see half a delivery. The
// reads are still taken in the law's own order, so that the pair reads the way
// the delivery writes even to somebody who arrives at this line knowing nothing
// about the lock.
func (a *Agent) taskNewsStanding() (owed int, working bool) {
	a.handover.Lock()
	defer a.handover.Unlock()
	working = a.childrenOutstanding()
	owed = a.taskNewsOwed()
	return owed, working
}

// handOverTaskNews makes the last two writes of a delivery — the fact that a
// piece of work is no longer outstanding, and the news that its report is owed
// to the model — one step as far as [Agent.taskNewsStanding] is concerned.
//
// THE FACT DIFFERS BY ROAD AND THE WAKE DOES NOT. A divided part's report marks
// its node reported ([TaskNode.noteHandedOver]), and a road written next year
// will mark something else. The parent parked on them cannot tell the roads
// apart and must not have to, so every one of them hands its news over here.
//
// NOTHING SLOW GOES INSIDE, AND WHAT IS INSIDE IS THERE TO KEEP TWO WRITES IN
// ORDER. This seam holds two writes and the small locks they take; the
// checkpoint a mark owes the disk is written by the caller after this returns,
// because a lock a parked runner takes on every pass is not a lock to write a
// file under.
//
// The rule this lock is held by is the same for every holder, here and
// elsewhere: a hold is allowed only when it is BOUNDED — local work whose end
// this process decides — and only when holding it is what keeps two facts one
// fact for whoever reads them next. Admitting a division's parts is the other
// holder that meets the bar: the claim, the freeze and the admits are local, and
// they are held together with the withdrawal check so that the reader above
// either sees the parts outstanding or sees them never admitted, with no instant
// between. A hold that waits on a person, on a model or on the network is never
// allowed here, whatever it would make atomic: the parked runner takes this lock
// on every pass, and a wait of that kind is not this process's to end.
// TestWhatIsDoneUnderTheHandoverLockIsBounded holds every holder to that: each
// call made under the hold is written down in the law with the reason it ends.
func (a *Agent) handOverTaskNews(settled func()) {
	a.handover.Lock()
	defer a.handover.Unlock()
	if settled != nil {
		settled()
	}
	a.postTaskNews()
}

// resumeTurn starts one turn on what is ALREADY on the steering queue and hands
// back its stream, for the one caller whose turns are driven from outside: a
// task node's runner, re-entering the model with its sub-tasks' reports.
//
// It is [Agent.wakeLocked] with the wake taken out. Same empty opening message
// — what the turn is about is on the queue and the loop's first drain records
// it — and the same refusals, minus the two that only make sense for a
// conversation: there is no rail on a node (its spend is the session's, and the
// session's rail was checked when the work was proposed) and no wake lane to
// hand a stream to, because the caller is holding it.
//
// A nil answer means there is no turn to have: the agent is closed, or one is
// already running, which for a runner means the child is still working and the
// notes will land in it at a step boundary.
func (a *Agent) resumeTurn(ctx context.Context) <-chan Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.running || !a.opened {
		return nil
	}
	return a.startTurnLocked(ctx, userMessage{}, nil)
}

// ── THE BOUND A SETTLE TURN RUNS UNDER ──────────────────────────────────────
//
// The turn a landing note wakes is the conversation's own turn, and under
// `task.settle = auto` it is the turn that has to read the work and settle it.
// Left ordinary it is an ordinary full-belt turn with no bound of its own, and it
// was measured running 28 minutes of 49 tool-call rounds on the high-tier model,
// stopped only by the run's wall, over a tree that was already clean.
//
// So the wake is marked a SETTLE wake where the note is minted (task_run.go's
// [Agent.postTaskMessage]) and the turn it starts is given the checker's own
// contract: a call window it is told ([openCallWindow], which is what makes the
// model read the clock the way the checker does), a ceiling on the calls it may
// spend, and — when the run carries a ceiling of its own — a share of its money.
// A bound that trips hands the node back with the reason and the count on its
// report rather than stopping the turn silently (task_run.go's
// [Agent.markSettleBound]).
const (
	// settleCallCeiling is how many provider calls a settle turn may spend before
	// it is handed back. It is single digits in the spirit of the checker's own
	// shares: reading a report, the transcript and the diff, and spending a verb
	// on it, is a handful of calls — and the measured runaway was forty-nine rounds
	// of a turn that had no ceiling at all.
	settleCallCeiling = 6
	// settleBoundShare divides two things the settle turn may not exceed its own
	// share of: the run's remaining wall, and — when a Steward is armed — the
	// run's own money. It is [turnWallShare]'s shape for [turnWallShare]'s reason:
	// a share means the same thing at every ceiling somebody sets, and what is
	// left is what the rest of the run gets.
	settleBoundShare = 3
	// settleWindowCount is what a settle turn's hand-back says when its WINDOW ran
	// out rather than its calls — the deadline the checker's contract opened and
	// the model was told, which is the third way this bound can trip.
	settleWindowCount = "its window"
)

// settleWake is the ceiling one settle turn runs under, carried to it on the
// context the wake starts the turn with ([withSettleWake]).
//
// ONLY THE CEILING RIDES HERE. The window is a deadline the turn opens for itself
// the moment it begins ([Agent.runTurn]), because its length is read off the
// [Steward] — and that reading runs the run's own spend closure, which takes the
// agent's lock, so it may not be made where the wake is decided under that lock.
type settleWake struct {
	ceiling int
	model   string
	prompt  string
}

type settleWakeKey struct{}

// withSettleWake marks ctx as the opening of a settle turn and states its bound.
func withSettleWake(ctx context.Context, wake settleWake) context.Context {
	return context.WithValue(ctx, settleWakeKey{}, wake)
}

// settleWakeFrom reports the bound a turn runs under, and false for every turn
// nobody marked as a settle wake.
func settleWakeFrom(ctx context.Context) (settleWake, bool) {
	wake, ok := ctx.Value(settleWakeKey{}).(settleWake)
	return wake, ok
}

// settleWakeLocked is the bound the turn about to start runs under, when the
// queue it is starting on holds a landing that hands the model the decision. The
// lock is the caller's, which is what lets it read the queue at all.
//
// MORE THAN ONE LANDING CAN BE ON THE QUEUE, and the ceiling is the WIDEST of
// them: a node whose check saw a clean tree wants one call and a node that did
// not may want several, and a turn that has to settle both is not cut to the
// narrowest one's share.
func (a *Agent) settleWakeLocked() (settleWake, bool) {
	wake := settleWake{}
	for _, note := range a.steering {
		if !note.settle {
			continue
		}
		if note.settleCeiling > wake.ceiling {
			wake.ceiling = note.settleCeiling
		}
		if note.settlePrompt != "" {
			wake.model, wake.prompt = note.settleModel, note.settlePrompt
		}
	}
	return wake, wake.ceiling != 0
}

// settleWindow is how long a settle turn is given before it is handed back: a
// share of what is left of the run's wall when a [Steward] is armed, and the
// checker's own deadline — the length of one whole verdict — when it is not.
//
// IT IS A SHARE AND NOT A DURATION where there is a wall, for [turnWallShare]'s
// reason; and where there is no wall it is [auditDeadline], because a settle turn
// is the same job a check is — read the work and answer — and the checker's own
// bound is the one figure that job already has.
func (a *Agent) settleWindow() time.Duration {
	if steward := a.steward(); steward != nil {
		budget := steward.Budget()
		if left := budget.Wall - budget.SpentWall; left > 0 {
			if share := left / settleBoundShare; share > 0 {
				return share
			}
		}
	}
	return auditDeadline
}

// wakeLocked starts a turn for what is waiting on the steering queue, with a.mu
// held, and reports whether one began.
//
// It records NOTHING. The turn opens on an empty message and the loop's first
// act is to drain the queue (loop.go), which records and journals every note in
// the order it arrived and does it before the first provider request. That is
// what makes the coalescing free: two tasks landing in the same idle window
// append two notes, the first sets running under this lock, and the second finds
// a turn already going — ONE wake, one request, both outcomes in front of the
// model. A timer would have bought the same behaviour and a window in which a
// settle could be lost.
//
// ── WHAT DECLINES A WAKE ──
//
//   - a turn is already running: the note is steering, not a second turn.
//   - the session is closed: nothing drains after Close.
//   - InTask: this agent is one task node's runner (session.go's Config), whose
//     turns belong to the runner that drives it. A node starting a turn of its
//     own would be a second conversation inside a worktree. THE ONE EXCEPTION
//     is a node that is a ROOM and not a worker — the thread a sub-harness is
//     designed in (harness_task.go) — which is a conversation by construction
//     and whose steering has no turn to land in unless it starts one.
//     own would be a second conversation inside a worktree. A node WITH
//     SUB-TASKS still has to be re-entered when one of them reports, and it is —
//     through [Agent.resumeTurn], by the runner, which is the same turn started
//     by somebody who is reading it.
//   - the session is not open yet: recovery settles nodes inside New, and a turn
//     started there speaks to nobody (see [newAgent]).
//   - the spend rail: a turn that starts must be one the session can pay for,
//     and this is the one turn nobody asked for. The note stays queued and is
//     read by whatever the person says next.
//
// WHAT IT DOES NOT CHECK is that there is anything to answer, and it cannot:
// its two callers know that in two different ways. The enqueue has just put a
// note on the queue; the turn's end has just drained one INTO the transcript, so
// the queue is empty and the thing to answer is the last message. A third caller
// would have to establish the same fact before calling.
func (a *Agent) wakeLocked() bool {
	// A WALL THAT HAS PASSED BUYS NO TURN NOBODY ASKED FOR. This is arithmetic
	// on the Steward's own clock rather than a stopped flag: a stopped flag is a
	// different decision in [Steward.Decide], while the wall's budget ending is
	// the one stop allowed to win over work still moving. [wallIsUp] reads the
	// Steward's own wall and clock without asking its spend closure while this
	// function holds a.mu.
	wallGone := wallIsUp(a.steward())
	if a.running || a.closed || a.workStopped || wallGone || (a.config.InTask && !a.config.roomThread) || !a.opened {
		return false
	}
	if err := a.railBlockLocked(); err != nil {
		return false
	}

	// Every standing subscription gets this turn's stream BEFORE it starts, so a
	// surface draws the answer from its first delta. A lane whose reader is that
	// far behind is skipped rather than waited on: this call holds the lock
	// Interrupt needs, and a wake is not worth a session that cannot be stopped.
	watchers := make([]*eventStream, 0, len(a.wakeLanes))
	for _, lane := range a.wakeLanes {
		stream := newEventStream()
		select {
		case lane <- stream.out:
			watchers = append(watchers, stream)
		default:
			stream.close()
		}
	}
	// The turn's own subscription is drained and thrown away. A woken turn has no
	// caller holding a channel — that is what makes it a wake — and an
	// unread stream would park its pump goroutine on the first event forever.
	sink := newEventStream()
	go func() {
		for range sink.out { //nolint:revive // draining is the point
		}
	}()
	// THE OPENING MESSAGE IS EMPTY BUT MARKED A WAKE, and the two facts are not in
	// tension: empty is still empty — [userMessage.empty] reads the content, which
	// there is none of, so nothing is recorded and the loop's first drain still
	// writes the note off the queue exactly as before. The `wake` bit rides beside
	// that emptiness so the metered loop can tell WHOSE turn this is: nobody typed
	// it and nobody is holding a channel to answer it, which is what
	// [Agent.checkpointReopen] needs to know before it decides whether a turn that
	// stopped short is worth reading. Every gate that reads `user.wake` on an
	// opening message already treats empty as the same class (route_judge.go,
	// harness.go, task_brief.go), so the bit changes nothing but the one reading
	// that was missing.
	//
	// AND A TURN STARTED ON A LANDING THAT HANDS THE MODEL THE DECISION IS A
	// SETTLE TURN, which runs under the checker's own bound ([settleWake]). The
	// ceiling rides the context the turn is started with, and the turn opens its
	// window for itself once it is running ([Agent.runTurn], because the window's
	// length is read off the Steward's own clock).
	ctx := a.wokenTurnContext()
	if wake, settle := a.settleWakeLocked(); settle {
		ctx = withSettleWake(ctx, wake)
	}
	a.startTurnLocked(ctx, userMessage{wake: true}, sink, watchers...)
	return true
}

// wokenTurnContext is what a turn nobody submitted starts under: the plain
// background, unless a fixture supplied a base ([Agent.wokenTurnBase]).
func (a *Agent) wokenTurnContext() context.Context {
	if a.wokenTurnBase != nil {
		if ctx := a.wokenTurnBase(); ctx != nil {
			return ctx
		}
	}
	return context.Background()
}

// Wakes is the standing subscription to turns THE SESSION STARTED ON ITS OWN:
// one channel per woken turn, handed over before that turn's first event, and
// closed when it ends — the same shape [Agent.Submit] returns, because it is the
// same thing.
//
// It exists because a wake has no caller. Every other turn is somebody asking
// for something and reading the answer off the channel they were given; a turn
// started by a task landing is the model speaking to a room nobody is holding a
// microphone into, and without this the answer would reach the journal and never
// the screen. A surface adopts each stream exactly as it adopts a follow-up's
// (internal/tui3's followup.go): draw the turn, pump to close.
//
// The lane is buffered and NEVER BLOCKS the session: a subscriber that has
// stopped reading misses wakes rather than freezing the agent that is trying to
// tell it something. It closes with the session.
func (a *Agent) Wakes() <-chan (<-chan Event) {
	lane, _ := a.WatchWakes()
	return lane
}

// WatchWakes is [Agent.Wakes] with a way to stop. It is the same subscription —
// one channel per woken turn, buffered, never blocking the session — and stop
// takes it back off the session's list and closes it, so a surface that has
// detached from this conversation is not a lane the next wake has to try to
// send into.
//
// It is a SECOND DOOR rather than a changed one because [Agent.Wakes]' shape is
// the one internal/tui3 declares in its own interface, and a surface moves onto
// this when it has something to do with the stop.
//
// stop is never nil and calling it twice is calling it once. The closed channel
// it leaves behind is the same thing the session's own close hands every wake
// lane, so a reader needs no second rule for "this lane has ended".
func (a *Agent) WatchWakes() (<-chan (<-chan Event), func()) {
	lane := make(chan (<-chan Event), wakeLaneDepth)
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		close(lane)
		return lane, func() {}
	}
	a.wakeLanes = append(a.wakeLanes, lane)
	var once sync.Once
	return lane, func() { once.Do(func() { a.dropWakeLane(lane) }) }
}

// dropWakeLane takes one wake lane off the session and ends it.
//
// The close happens UNDER a.mu, and it has to: [Agent.wakeLocked] hands the
// turn's stream to every lane on this list while holding the same lock, so a
// close taken outside it could land on a lane that call is mid-send into. A
// lane already gone — the session closed under it — is left alone, because the
// close it is owed has already happened.
func (a *Agent) dropWakeLane(lane chan (<-chan Event)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for at, held := range a.wakeLanes {
		if held == lane {
			a.wakeLanes = append(a.wakeLanes[:at], a.wakeLanes[at+1:]...)
			close(lane)
			return
		}
	}
}

// wakeLaneDepth is how many woken turns a subscriber may be behind on before it
// starts missing them. Wakes are rare — one per idle window in which work
// landed — so a handful is a surface that has stopped reading, not a busy one.
const wakeLaneDepth = 8

func textMessage(role, text string) ai.Message {
	return ai.Message{Role: role, Content: []ai.ContentPart{{Type: "text", Text: text}}}
}

// ── event hub ───────────────────────────────────────────────────────────────

// eventHub is one turn's fan-out: every Submit that lands on the turn — the
// one that started it and any steering Submit after — gets its own eventStream,
// and the turn's events go to all of them.
//
// Subscribers are independent queues rather than one queue with many readers
// because the events are a narrative, not work: a slow surface must fall
// behind on its own channel, not steal deltas from the surface beside it.
//
// THERE ARE TWO WAYS IN AND THEY ANSWER TWO DIFFERENT PEOPLE. [eventHub.
// subscribe] starts empty, which is what a Submit wants: the events before it
// spoke belong to a stream somebody was already reading, and replaying them
// would make the second caller re-draw the first caller's turn.
// [eventHub.attach] replays this turn from its first event, which is what a
// surface arriving at a conversation MID-TURN wants: it holds no stream, it
// never saw the events, and starting empty would leave it looking at a session
// that is visibly working and saying nothing.
type eventHub struct {
	mu          sync.Mutex
	subscribers []*eventStream
	closed      bool

	// finishedCalls is how many tool ends and tool failures this turn has sent
	// — the same events the node room's recorder counts as steps
	// (task_live.go) — and stepsAtOpen is the recorder's count at the turn's
	// opening drain, before any of them. Together they are what the per-step
	// frame ([Agent.bashBeltFrame]) knows the recorder must reach before the
	// number it draws is the number the work is on.
	finishedCalls int
	stepsAtOpen   int

	// backlog is every event this turn has sent, in order, kept for whoever
	// attaches next and dropped whole when the hub closes.
	//
	// WHAT BOUNDS IT IS THE TURN. The data here is the reply the model is
	// writing plus one entry per tool boundary, so a backlog is one turn's text
	// and it is let go of at [eventHub.close] — a session is never holding more
	// than the turn in flight.
	//
	// AND CONSECUTIVE TEXT FOLDS INTO ONE ENTRY ([foldsInto]). A long answer
	// arrives as thousands of one-word deltas, and keeping thousands of Events
	// to hold the same string is paying a slice header and a struct per word for
	// nothing: a surface appends delta text, so one delta carrying a paragraph
	// and a hundred carrying its words draw identically. It is the only
	// reshaping done here, it is deliberately not a summary, and it stops at
	// every event that is not text — so a tool call in the middle of an answer
	// still lands between the two halves it landed between live.
	backlog []Event
	// folding is the text of the run currently folding into the LAST backlog
	// entry, accumulated instead of concatenated.
	//
	// `backlog[last].Text += delta` is the obvious way to fold and it is
	// quadratic: every delta copies the whole answer so far, so a reply of a
	// megabyte moves half a terabyte of bytes through this lock while the
	// provider is still writing. The builder holds the same characters in the
	// same order and hands them over in one allocation at [eventHub.foldedLocked],
	// which is called at every point the folded entry can actually be READ — an
	// attach, and the append that ends the run. Between those points
	// backlog[last].Text is STALE BY DESIGN and nothing but [foldsInto] may look
	// at that entry, which is safe because [foldsInto] reads Kind and never Text.
	folding strings.Builder
}

func newEventHub() *eventHub { return &eventHub{} }

// subscribe returns a fresh channel carrying the turn's events from now on. A
// subscription to a finished hub is an already-closed channel: the turn whose
// events it would carry is over, and a channel that never closes would hang
// the caller instead of telling it so.
func (h *eventHub) subscribe() <-chan Event {
	stream := newEventStream()
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		stream.close()
		return stream.out
	}
	h.subscribers = append(h.subscribers, stream)
	h.mu.Unlock()
	return stream.out
}

// attach is subscribe FOR SOMEBODY WHO WAS NOT HERE: the turn's events from its
// first one, then the live tail, on one channel with no gap and no repeat.
//
// THE BACKLOG AND THE SUBSCRIPTION ARE ONE ACT, under one hold of the lock, and
// that is the whole of why this is a method on the hub rather than two calls
// from outside it. An event that landed between copying the backlog and joining
// the subscribers would be an event nobody ever sees; one that landed the other
// way round would be drawn twice. Seeding under the lock is safe for
// [eventHub.send]'s own reason — an eventStream send is an append and a signal,
// never a wait.
//
// A CARD FOR A DECISION SOMEBODY HAS ALREADY MADE IS NOT REPLAYED, and asking
// is what says which those are — the questions still waiting on somebody, as
// [Agent.stillAskedLocked] reads them off the book at the moment of the attach.
// The backlog is a faithful log of the turn and stays one; what is filtered is the
// REPLAY, because a card is the one event in a turn that is not a report of
// something that happened but a question about something that has not. Without
// this, a person who approved a task and then looked at another tab was asked
// the same question again every time they came back, for as long as the turn ran
// — and the question, which takes the keyboard where it is drawn, made the
// conversation behind it unscrollable with it.
//
// A nil map filters nothing, which is the honest answer for a caller that cannot
// say what is open: every card replays, exactly as it did before this existed.
//
// false is a hub that has already closed, and the stream is closed with it: the
// turn whose events it would carry is over, and a caller is owed that answer
// rather than a channel that never ends. It is the same shape [taskRoom.join]
// answers a landed node with.
func (h *eventHub) attach(asking map[string]bool) (*eventStream, bool) {
	stream := newEventStream()
	if h == nil {
		stream.close()
		return stream, false
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		stream.close()
		return stream, false
	}
	h.foldedLocked()
	for _, event := range h.backlog {
		if key, asks := questionAsked(event); asks && !asking[key] {
			continue
		}
		stream.send(event)
	}
	h.subscribers = append(h.subscribers, stream)
	h.mu.Unlock()
	return stream, true
}

// drop is a subscriber LEAVING: it stops being fanned out to and its pump ends.
//
// It exists because a surface that detaches from a conversation must be able to
// stop draining. Without it the two halves both leak — the hub holds a stream
// nobody will ever read, and that stream's pump parks forever on its next send
// (see [eventStream.pump]) — and a process holding several conversations pays
// both costs per detach rather than once per session.
//
// A stream this hub does not hold is left alone rather than refused: a caller
// that dropped twice, or that dropped after the turn ended, is asking for a
// state this already is.
func (h *eventHub) drop(stream *eventStream) {
	if h == nil || stream == nil {
		stream.leave()
		return
	}
	h.mu.Lock()
	for at, held := range h.subscribers {
		if held == stream {
			h.subscribers = append(h.subscribers[:at], h.subscribers[at+1:]...)
			break
		}
	}
	h.mu.Unlock()
	stream.leave()
}

// release takes one subscriber off the hub AND LEAVES ITS CHANNEL OPEN. It is
// [eventHub.drop] for a stream that is not finished with — the reader has not
// gone anywhere, this turn is simply no longer the turn it is watching.
//
// It has one caller, and the whole of the reason is there: a steer that fell
// through is re-homed onto the follow-up queue, and the person holding its
// stream is owed the turn their words then start on the channel they are already
// reading (steer.go's [Agent.liftSteersLocked]). [eventHub.drop] cannot serve
// that — it ends the reader — and leaving the stream subscribed cannot either,
// because [eventHub.close] would close it a moment later.
//
// A stream this hub does not hold is left alone, on drop's terms: a caller
// asking for a state this already is has asked for nothing.
func (h *eventHub) release(stream *eventStream) {
	if h == nil || stream == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for at, held := range h.subscribers {
		if held == stream {
			h.subscribers = append(h.subscribers[:at], h.subscribers[at+1:]...)
			return
		}
	}
}

// adopt hands an ALREADY-BUILT stream to this hub. It is subscribe for a
// caller that had to hold its channel before the turn it belongs to existed: a
// queued follow-up is handed a stream the moment it is queued, and that stream
// becomes a subscriber of whichever turn eventually runs it. A hub that has
// already closed closes the stream instead, so the caller's channel ends rather
// than waiting for a turn that is over.
func (h *eventHub) adopt(stream *eventStream) {
	if stream == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		stream.close()
		return
	}
	h.subscribers = append(h.subscribers, stream)
	h.mu.Unlock()
}

// send fans one event out to every current subscriber. The lock is held across
// the fan-out — an eventStream send is an append and a signal, never a wait —
// so every subscriber sees the same events in the same order, and a subscriber
// arriving mid-fan-out lands cleanly before or after this event rather than
// inside it.
//
// IT REPORTS WHETHER THE EVENT LANDED, because a caller holding something a
// person is owed must be able to keep it rather than trust an ordering. Only
// this lock can answer that: a hub reads open until the moment it closes under
// it, so anything a caller checked beforehand is already a guess. Almost every
// caller is drawing a line that belongs to the turn it is in and correctly
// ignores the answer; [Agent.sayMemory] is the one that holds a line over.
func (h *eventHub) send(event Event) (landed bool) {
	// NOBODY WATCHING IS AN ORDINARY CASE. A completion driven without a turn
	// around it — a test of the retry loop, a future headless caller — has no
	// hub, and a line nobody can read is a line worth not drawing rather than a
	// panic in the middle of a request.
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	if event.Kind == EventToolEnd || event.Kind == EventToolFailed {
		h.finishedCalls++
	}
	// The backlog is written BEFORE the fan-out and under the same lock, so what
	// the next attacher is handed is exactly what the subscribers already have —
	// one funnel, one order, two readers. It is the rule [taskRoom.publish]
	// already keeps for the same reason one file over.
	if last := len(h.backlog) - 1; last >= 0 && foldsInto(h.backlog[last], event) {
		if h.folding.Len() == 0 {
			h.folding.WriteString(h.backlog[last].Text)
		}
		h.folding.WriteString(event.Text)
	} else {
		h.foldedLocked()
		h.backlog = append(h.backlog, event)
	}
	for _, stream := range h.subscribers {
		stream.send(event)
	}
	return true
}

// foldedLocked settles the accumulated run back into the backlog entry it
// belongs to, and is a no-op whenever no run is open. It runs at every point the
// entry can be read — before a new entry is appended over the top of the run,
// and before [eventHub.attach] copies the backlog out — so what a reader is
// handed is always the whole folded text, in one string, exactly as the
// concatenation would have spelled it.
func (h *eventHub) foldedLocked() {
	if h.folding.Len() == 0 {
		return
	}
	if last := len(h.backlog) - 1; last >= 0 {
		h.backlog[last].Text = h.folding.String()
	}
	h.folding.Reset()
}

// foldsInto reports whether a new event may be folded into the one before it in
// a backlog — which is true for exactly the two kinds that are a STREAM OF TEXT
// and carry nothing else.
//
// EventTextDelta and EventReasoning are each emitted as `Event{Kind: …, Text: …}`
// and nothing more, at every one of the six places that emit them (loop.go,
// image.go, harness.go, task_room.go). THAT IS THE LAW THIS DEPENDS ON: a kind
// that ever grows a second field must come off this list in the same change, or
// the fold will quietly drop it for whoever attaches next.
func foldsInto(prev, next Event) bool {
	if prev.Kind != next.Kind {
		return false
	}
	return prev.Kind == EventTextDelta || prev.Kind == EventReasoning
}

// close ends every subscriber's channel. It runs after the turn's last event,
// so each channel closes once its queue has drained.
//
// THE BACKLOG GOES WITH IT, and that is what keeps this turn's text from being
// this session's memory footprint: nothing can attach to a finished turn — the
// history is the journal, and [eventHub.attach] answers a closed hub with a
// closed channel — so a kept backlog would be a copy of the reply nobody could
// ever ask for.
func (h *eventHub) close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subscribers := h.subscribers
	h.subscribers = nil
	h.backlog = nil
	// The half-folded run goes with the backlog it belonged to, for the same
	// reason: nothing can attach to a finished turn, so the characters it holds
	// are a copy of the reply nobody could ever ask for.
	h.folding.Reset()
	h.mu.Unlock()
	for _, stream := range subscribers {
		stream.close()
	}
}

// ── event stream ────────────────────────────────────────────────────────────

// eventStream is an unbounded queue in front of one subscriber's channel.
//
// The two obvious alternatives are both wrong here. A blocking send stalls the
// loop — mid-tool-batch, with a provider connection open — behind a surface
// that is redrawing; a fixed buffer with a dropping send loses text deltas,
// which are the one event whose loss is visible as corruption rather than as
// latency. So the producer never waits and never drops, and the cost is one
// goroutine per turn.
// A READER MAY ALSO LEAVE, which is the other half of the same bargain. The
// producer never waits, so an abandoned stream costs a queue that grows and a
// pump parked on its next send; [eventStream.leave] is how a surface that has
// detached from a conversation says it is gone and gets both back.
type eventStream struct {
	out chan Event

	mu     sync.Mutex
	cond   *sync.Cond
	queue  []Event
	closed bool

	// left says the READER has gone, where closed says the PRODUCER has. They
	// are two different ends of the same channel and neither implies the other:
	// a turn that ends while nobody is reading closes a stream that was already
	// left, and a surface that detaches mid-turn leaves one that is still being
	// sent to.
	left bool
	// gone is closed with left, so a pump already blocked on its send has
	// something to select on — the flag alone would only be read once the send
	// it is parked on completed, which is the case that never comes.
	gone chan struct{}
}

// refusedStream is one event and a closed channel: a turn that was refused
// before it started still answers on a stream, because every caller of Submit
// reads its answer the same way — from the channel. An error return would make
// a refusal the one outcome a surface has to handle twice.
func refusedStream(err error) <-chan Event {
	return refuseOn(newEventStream(), err)
}

// refuseOn is refusedStream onto a stream somebody already holds — a queued
// follow-up's.
func refuseOn(stream *eventStream, err error) <-chan Event {
	stream.send(Event{Kind: EventError, Err: err})
	stream.close()
	return stream.out
}

func newEventStream() *eventStream {
	stream := &eventStream{out: make(chan Event), gone: make(chan struct{})}
	stream.cond = sync.NewCond(&stream.mu)
	go stream.pump()
	return stream
}

func (s *eventStream) send(event Event) {
	if event.Kind == EventError && event.Err != nil {
		event.Err = scrubEventError(event.Err)
	}
	s.mu.Lock()
	// A STREAM THE READER LEFT IS NOT QUEUED INTO. Dropping here is the point of
	// leaving: everything this fan-out is careful never to drop is careful on
	// behalf of somebody who is going to read it, and nobody is.
	if !s.closed && !s.left {
		s.queue = append(s.queue, event)
		s.cond.Signal()
	}
	s.mu.Unlock()
}

type scrubbedEventError struct {
	cause error
	text  string
}

func (e *scrubbedEventError) Error() string { return e.text }
func (e *scrubbedEventError) Unwrap() error { return e.cause }

// scrubEventError is the last boundary before an EventError becomes visible.
// It preserves the typed cause for readers using errors.Is or errors.As while
// ensuring the sentence a surface receives contains no assembled credential.
func scrubEventError(err error) error {
	if err == nil {
		return nil
	}
	said := err.Error()
	clean := string(trace.Scrub([]byte(said)))
	if clean == said {
		return err
	}
	return &scrubbedEventError{cause: err, text: clean}
}

func (s *eventStream) close() {
	s.mu.Lock()
	s.closed = true
	s.cond.Signal()
	s.mu.Unlock()
}

// leave is the READER saying it is gone: the queue is dropped, the pump ends
// and the channel closes, whether or not the turn behind it is over.
//
// It is idempotent and safe on a nil stream, because both are the ordinary case
// for a caller that stops twice or stops something that never started. A stream
// this is called on is finished for good — there is no coming back, and a
// surface that wants the turn again attaches for a fresh one.
func (s *eventStream) leave() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if !s.left {
		s.left = true
		s.queue = nil
		close(s.gone)
		s.cond.Signal()
	}
	s.mu.Unlock()
}

// pump drains the queue into the channel and closes it when the turn is over
// and nothing is left.
//
// A consumer that SAYS it is gone ([eventStream.leave]) ends this goroutine at
// once, from either place it can be waiting: the cond var it sleeps on, and the
// send it is parked on. A consumer that merely walks away without saying so
// still parks it forever, exactly as it always did — nothing here can tell that
// case from a surface that is slow — which is why every lane that can be left
// now has a way to say it.
func (s *eventStream) pump() {
	defer close(s.out)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed && !s.left {
			s.cond.Wait()
		}
		if s.left || len(s.queue) == 0 {
			s.mu.Unlock()
			return
		}
		event := s.queue[0]
		s.queue = s.queue[1:]
		s.mu.Unlock()
		select {
		case s.out <- event:
		case <-s.gone:
			return
		}
	}
}

// DisplayEntry is one journaled message shaped for surface replay: who spoke
// and what they said, with tool calls flattened to their gloss and carrying the
// payload the journal kept for them. Reasoning metadata is deliberately absent
// from this display shape even though the journal keeps it for model continuity;
// nothing about the wire itself is shown to the person.
type DisplayEntry struct {
	// Role is "user" | "assistant" | "tool" | "note" | "aside".
	//
	// "note" is a system-injected marker a surface draws as a rule of its own — a
	// compaction summary is the one that exists.
	//
	// "aside" is a line the SESSION WROTE and the person did not: a task's
	// completion note, a job's exit, a resume's account of what an interrupt left
	// behind (agent.go's [Agent.enqueueNote]). It rides the user role in the
	// transcript because that is the only role the model can be told something
	// in, and it is separated here because a surface that drew it as a user
	// message would be putting words in somebody's mouth — words that, live, that
	// same surface deliberately never draws. It is answered from the journal's own
	// mark, so a line from a file written before the mark existed still arrives as
	// "user", which is exactly what it always was.
	Role string
	Text string
	Tool string // set when the entry is one call in a batch
	Hint string // the call's gloss, as the tool cluster rendered it

	// CallID is the provider's own identity for a tool entry's call, exactly as
	// the record holds it, and "" for every entry that is not a call.
	//
	// IT IS THE ONLY THING A LIVE END CAN PAIR ON. A page opened on work already
	// running draws its rows out of the record and then keeps listening; the end
	// that arrives a second later has to land on the row that is already there,
	// or the same call is drawn twice — once running forever, once finished. The
	// id is what the two halves have in common, so it is carried rather than
	// dropped at the shaping.
	CallID string

	// Answered is whether the record already holds the RESULT of this call.
	//
	// IT IS DERIVED FROM AN ABSENCE, and the absence is load-bearing: the
	// assistant message is journaled BEFORE its batch runs (loop.go), so a
	// session read while a call is in flight names the asking and nothing else. A
	// surface that drew every recorded call as finished would tell a person the
	// work is further along than it is.
	//
	// It is a field of its own rather than `Output != ""` because a call that
	// returned nothing and a call that has not returned are different facts and a
	// page speaks about them differently.
	Answered bool

	// Args and Output are a TOOL entry's payload, in exactly the two shapes a
	// live surface already holds them in ([Event.Args] and [Event.Output]): the
	// arguments the model sent, compacted onto one line and capped, and the text
	// of the result that answered them, capped rune-safe for display.
	//
	// They are populated for a tool entry whenever the journal carries them,
	// which is every session file this build writes — the arguments ride the
	// assistant message's tool_calls and the result is the tool message keyed by
	// the same id. Both are "" otherwise: for every entry that is not a call, for
	// a call whose result never reached the file (a session killed mid-batch), and
	// for a file written before either was journaled.
	//
	// EMPTY MEANS NO PAYLOAD, and a surface must read it that way rather than as
	// an empty result: a replayed row with nothing behind it has nothing to
	// expand, and offering an expansion that opens on a blank is the defect this
	// field exists to end.
	//
	// CONTRACT, inherited from [Event.Output]: Output is FOR DISPLAY ONLY. It is a
	// capped copy, never the result the model read.
	Args   string
	Output string

	// Caption and CaptionCategory are WHAT THE NARRATOR SAID ABOUT THE BATCH
	// THIS CALL OPENED, and the family of work it named (caption.go,
	// actioncategory.go). They are set on the batch's FIRST call and on nothing
	// else, which is the same anchor the live [Event] carries, so a page built
	// out of the record keys the step exactly where a page built out of the
	// stream does.
	//
	// THEY ARE THE REASON A REOPENED CONVERSATION READS AS ITSELF. Without them
	// a surface recomposes a title from the tool names — "running 1 command"
	// where the person had been reading "starting the local server" — and draws
	// the family those names imply, so a step the narrator called a `test`
	// becomes a `run` the moment the file is read back.
	//
	// Both are empty for every entry that is not a batch anchor, for every batch
	// the narrator never spoke about, and for every file written before the
	// `caption` line existed. A surface reads that emptiness as "recompose", not
	// as "draw nothing".
	Caption         string
	CaptionCategory ActionCategory

	// Took is HOW LONG THIS CALL'S OWN WORK RAN, from begin to end of its
	// Execute — the same figure EventToolFinished carries live.
	//
	// IT IS WHY A REOPENED PAGE STILL SAYS WHAT A CALL TOOK. The live stream
	// writes the figure onto the row as the call finishes; a page built out of
	// the record after the batch has no stream to watch, and without this field
	// the row came back with Args and Output but no duration. Zero when the
	// journal never recorded one (every file written before the `took` line, a
	// call that never finished), which a surface reads as "say nothing" by the
	// emptiness law — the same reading toolview.go's [elapsedWord] already makes
	// of a live row that never got EventToolFinished.
	Took time.Duration

	// ImageRefs are the paths of the pictures a person's message carried, in the
	// order they sit in it — what the journal wrote where the bytes would have
	// been (see [journalPart]). It is what lets a replayed message mark its
	// attachments the way the live surface does, "[photo.png]", instead of
	// showing the words alone as though nothing had been attached.
	//
	// Nil for every message that carried none, and for a session with no file:
	// the paths are the JOURNAL's record, and a conversation that lives only in
	// memory never wrote one.
	ImageRefs []string
	// ReplyTags label the assistant entry that answers finished task notes. They
	// are nil on every ordinary reply.
	ReplyTags []TaskReplyTag

	// Steer marks a user entry that was typed INTO the turn it sits inside
	// rather than starting one of its own (steer.go), and carries the instant it
	// was sent.
	//
	// IT IS WHAT MAKES A TURN A TRUNK WITH ELBOWS. The entry that opened the turn
	// is the question; every entry after it that carries this, up to the next
	// question, is a correction the person made while the work was running — so a
	// surface can draw the turn as one thing with the steers hanging off it
	// instead of as a run of unrelated messages from somebody who kept
	// interrupting themselves.
	//
	// It is answered from the JOURNAL's own mark, exactly as "aside" is: a
	// message replayed out of a file written before steering existed carries nil
	// and draws as the plain user line it always was. A steer that FELL THROUGH
	// never appears here at all — it was never part of the turn, and what replays
	// is the ordinary question it became (the record of the fall-through is its
	// own line in the session file).
	//
	// Nil on every other entry, and on every session with no file to have kept a
	// mark.
	Steer *SteerMark

	// Team is what a TEAM DELIVERY handed this conversation, line by line, on
	// an "aside" that is one (teamshape.go): the manager's brief that started
	// it (Kind [teams.KindStart]), a manager's note or directive, a teammate's
	// post. It is what lets a surface draw the brief as a quoted card headed by
	// who sent it rather than as the aside's first line. Nil on every other
	// entry; the aside's Text still holds the whole delivery as the model read it.
	Team []TeamLine
}

// SteerMark is what the record keeps about one steer that LANDED: when the
// person sent it, and — always true here — that the turn it was typed into
// carried it to the model.
//
// Consumed is a field rather than an assumption because the record has to be
// able to say both things, and because a reader of a session file finds the
// other answer written next to these same words ([journalSteer]).
type SteerMark struct {
	At       time.Time
	Consumed bool
	Landing  string
}

// Transcript returns the conversation so far as display entries, oldest
// first. It exists for replay-on-resume: the surface renders the tail instead
// of opening on an empty screen. The system prompt is never included; a
// compaction summary note is.
func (a *Agent) Transcript() []DisplayEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return shapeEntries(a.messages, a.file)
}

// EarlierHistory is the conversation a compaction pass edited away, and where
// the pass's own rewritten copy of it ends in the live transcript.
//
// THE CONVERSATION, TOLD ONCE AND WHOLE, IS `Entries` FOLLOWED BY
// `Transcript()[Floor:]`. That is the contract, and it is a splice rather than a
// prefix because the pass does not delete the history it shortens — it rewrites
// it in place and journals the whole rewritten window again, so the same
// conversation is in the file twice: once as it happened, above the marker, and
// once with its tool results stubbed and its long runs of work folded, below.
// Drawing both would show the session to itself twice.
type EarlierHistory struct {
	// Entries is the region, oldest first, in the shape [Agent.Transcript] uses.
	// Empty when there is nothing to offer.
	Entries []DisplayEntry
	// Floor is how many entries at the start of [Agent.Transcript] the region
	// replaces. Zero whenever Entries is empty.
	Floor int
}

// EarlierHistory is what a surface needs to scroll back through a compaction:
// the conversation above the latest pass, and the floor beneath which the live
// transcript is that same conversation rewritten.
//
// IT IS HISTORY AND NOT CONTEXT. Nothing here sends it, the model does not carry
// it, and it does not change when the person rewinds — a rewind edits the live
// transcript, and the region above the marker was already out of the model's
// hands before the cut was offered.
//
// IT IS EMPTY IN THREE CASES, and a surface must behave exactly as it always did
// in all three: a session that was never compacted, a session compacted by a
// build that did not write the window's length ([compactionOverlap] explains
// why that cannot be guessed at), and a session with no journal at all. The
// third is the honest one to remember — a memory-only conversation has no file
// to have kept the words the pass took out.
//
// WHAT IT COVERS when it is not empty: the transcript exactly as it stood one
// instant before the latest pass edited it. For a session compacted once that is
// the whole conversation from its first word, in the original lines, before
// anything was stubbed or folded. For a session compacted more than once it
// still opens on the conversation's first words — a pass never folds what a
// person said — and carries the older passes' own edits in the middle of it,
// because those edited lines are what the file holds there. See
// [replayedSession.earlier] for why the older markers are applied rather than
// walked through.
func (a *Agent) EarlierHistory() EarlierHistory {
	a.mu.Lock()
	defer a.mu.Unlock()
	return EarlierHistory{Entries: a.earlier, Floor: a.earlierFloor}
}

// displayEntries is the shaping without a journal behind it: [Agent.Rewind]
// hands it the turn it just dropped, so a surface un-draws exactly the rows it
// drew. The dropped turn's rows are being REMOVED — nothing is about to expand
// one — so the picture paths a file would have answered are not asked for.
func displayEntries(messages []ai.Message) []DisplayEntry {
	return shapeEntries(messages, nil)
}

// shapeEntries is the shaping itself, over any run of messages.
//
// The journal is consulted for ONE thing — the paths of a message's pictures,
// which cannot be recovered from the message itself (a data URL is bytes with no
// provenance) — and nil is a session that has no file to ask.
//
// Everything else comes from the messages, and the tool payload is the reason
// the results are indexed first: a call's arguments ride the assistant message
// that made it, and its result is a SEPARATE message further down, keyed by the
// call's id. One pass to index, one pass to shape, so a batch of ten calls costs
// one walk rather than ten.
func shapeEntries(messages []ai.Message, journal *sessionFile) []DisplayEntry {
	results := toolResults(messages)
	// Sized by [entryRows], which is the rule this loop appends by, so a
	// transcript full of tool batches is not grown a power of two at a time.
	entries := make([]DisplayEntry, 0, countEntries(messages))
	var replyTags []TaskReplyTag
	for _, msg := range messages {
		// THE MODEL'S PRIVATE CONTEXT IS NOT CONVERSATION. Use the same rule
		// as compaction's count so hidden guidance cannot move the history seam.
		if entryRows(msg) == 0 {
			continue
		}
		role := msg.Role
		if role == "user" && journal.isNote(msg) {
			// A LINE THE SESSION WROTE IS NOT THE PERSON'S. It is user-role in the
			// transcript because that is the only role the model can be told
			// something in, and the journal is the only place that remembers the
			// difference (sessionfile.go's [sessionEntry.Note]). Drawn as a user
			// message it would be this build putting words in somebody's mouth —
			// the exact thing the live surface refuses to do with the same note.
			// It is NOT "note": that role is the compaction marker a surface draws
			// as a rule of its own, and these two are not one shape.
			role = "aside"
			replyTags = append(replyTags, journal.taskReplyTags(msg)...)
		}
		var team []TeamLine
		if role == "aside" {
			team = teamNewsLines(messageContentText(msg))
		}
		var tags []TaskReplyTag
		if role == "assistant" && len(replyTags) > 0 {
			tags = append([]TaskReplyTag(nil), replyTags...)
			replyTags = nil
		}
		entries = append(entries, DisplayEntry{
			Role:      role,
			Text:      messageContentText(msg),
			ImageRefs: journal.imageRefs(msg),
			ReplyTags: tags,
			// The journal is the only thing that remembers a user line was typed
			// INTO the turn above it rather than opening one of its own: the
			// message itself is an ordinary user message, because that is what the
			// model has to read it as (steer.go).
			Steer: journal.steerMark(msg),
			Team:  team,
		})
		for callIndex := range msg.ToolCalls {
			call := &msg.ToolCalls[callIndex]
			result, answered := results[call]
			// The step's own title, off the journal's `caption` line, keyed by
			// the anchor the narration was recorded against. Every call that is
			// not a batch anchor answers empty and carries nothing.
			told, family := journal.caption(call.ID)
			entries = append(entries, DisplayEntry{
				Role:   "tool",
				Tool:   call.Function.Name,
				CallID: call.ID,
				Hint:   gloss(*call),

				Caption:         told,
				CaptionCategory: family,
				// The same two renderings a live row is drawn from (loop.go),
				// applied to the same fields the journal kept: a replayed row and
				// the row it replaces are the same row, or replay is a second
				// rendering of one conversation.
				Args:     argsText(*call),
				Output:   capOutput(result),
				Answered: answered,
				// And the call's own duration, off the journal's `took` line —
				// the same figure EventToolFinished carried while the window was
				// open. Zero when the file never recorded one.
				Took: journal.took(call.ID),
			})
		}
	}
	return entries
}

// legacyCheckpointCarryOnLead is the reserved continuation prefix written by
// older journals. Match the complete prefix so ordinary words typed by a person
// beginning with "[carry on]" remain visible.
const legacyCheckpointCarryOnLead = "[carry on] You stopped, but what was asked is not finished. " +
	"Somebody reading the work against the request says this is what is left. " +
	"Carry on with it, and do not summarise what you have already done:\n"

// explainingCheckpointCarryOnLead is the continuation prefix journals wrote
// before [NoChangeReply] existed, when a model that found the note mistaken was
// told to explain itself (#1065). Its complete prefix is matched for the same
// reason as [legacyCheckpointCarryOnLead]'s.
const explainingCheckpointCarryOnLead = "[carry on] A reader of a bounded account of the work raised the observation below. " +
	"Check it against the actual current work and the person's request before changing anything. " +
	"Fix any confirmed gap. If the observation is mistaken or already satisfied, preserve the correct work, " +
	"explain the evidence briefly, and finish; do not invent a change to satisfy the observation.\n"

// entryRows is how many rows the shaping above makes of ONE message: none at
// all for system messages and private continuation context, and otherwise the
// message's own row plus one for every tool call riding it. It is the rule the loop appends by, written
// down so it can be read without being run.
//
// IT MUST MOVE WHENEVER THAT LOOP DOES. A second rule for how many rows a
// message makes is a rule that can disagree with the shaping, and the thing that
// would disagree is a floor — the count [EarlierHistory] hands a surface to
// splice its scrollback at. TestTheEntryCountAgreesWithTheShaping is what keeps
// the two honest: a row added or dropped above and not here fails that test
// rather than moving somebody's history under them.
func entryRows(msg ai.Message) int {
	if msg.Role == "system" {
		return 0
	}
	if msg.Role == "user" {
		text := messageContentText(msg)
		// The continuation's full reserved lead identifies older journals too:
		// they wrote it as an unmarked user message even though nobody typed it.
		// Keep the model's record intact; only its display projection omits it.
		if isVolatileNote(text) || strings.HasPrefix(text, checkpointCarryOnLead) ||
			strings.HasPrefix(text, explainingCheckpointCarryOnLead) || strings.HasPrefix(text, legacyCheckpointCarryOnLead) {
			return 0
		}
	}
	// A CARRY-ON ANSWERED [NoChangeReply] WITHDREW ITSELF AS THE ANSWER. The model
	// needs the token in its record; a person reading the history needs the
	// settled answer before it, which is what a missing row leaves standing.
	if msg.Role == "assistant" && len(msg.ToolCalls) == 0 && IsNoChangeReply(messageContentText(msg)) {
		return 0
	}
	return 1 + len(msg.ToolCalls)
}

// countEntries is len(shapeEntries(messages, …)) without the shaping.
//
// A compaction pass needs exactly that number while it holds the session lock,
// and building the rows to keep nothing but their count walks the whole
// transcript's content a second time — every argument compacted and capped,
// every tool result measured, every picture looked up — to measure a list
// (loop.go's [Agent.compact]). The journal is not a parameter because it cannot
// change the answer: it renames one role and finds the paths of pictures, and
// neither adds nor removes a row.
func countEntries(messages []ai.Message) int {
	rows := 0
	for _, msg := range messages {
		rows += entryRows(msg)
	}
	return rows
}

// toolResults indexes the text answered by each call occurrence. The pairing
// respects assistant batches, so a reused provider ID cannot rewrite an earlier
// result or make a later, unanswered call look complete.
func toolResults(messages []ai.Message) map[*ai.ToolCall]string {
	paired := toolResultCalls(messages)
	results := make(map[*ai.ToolCall]string, len(paired))
	for index, call := range paired {
		results[call] = messageContentText(messages[index])
	}
	return results
}
