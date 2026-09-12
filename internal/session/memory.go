package session

// Memory is what one conversation knows and the next one would otherwise have
// to be told again.
//
// It used to be a file of lines — ~/.aforge/v3/memory.md, appended to by a
// `note` tool, filtered by a `forget` tool, and rendered whole into the system
// prompt of every turn. That shape had one virtue (a person could read it) and
// one fatal property: it only ever grew, and everything in it was paid for on
// every request whether or not the turn had anything to do with it.
//
// What replaces it is the store's event-sourced brain (internal/store's
// memory.go) with a per-turn reflex in front of it (internal/reflex). The
// arrangement is four moments, and every one of them is optional:
//
//   - BEFORE THE TURN, the store ranks what is remembered against the message
//     just typed and hands the router a SHORTLIST of eight titles — never
//     bodies ([store.Store.MemoryCandidates]). The router answers which two or
//     three of them bear on it, and only those are rendered into the <memory>
//     block, each stamped with how long ago it was learned. A thousand
//     remembered things cost the same small call as eight do, where the file
//     cost all thousand.
//
//   - AFTER THE TURN, off the person's path entirely, the extractor reads the
//     exchange and answers whether it held anything worth carrying into another
//     session. Almost always it did not, which is the answer the gate is shaped
//     around. It answers a second question in the same call: which of the lines
//     this turn was shown actually bore on the answer, which is the only thing
//     the store's ranking counts.
//
//   - WHEN IT DID, the decider settles the new thing against what the store
//     already holds near it: add it, refine one of them, replace one of them, or
//     skip. That is what keeps the same preference stated in three sessions from
//     becoming three rows.
//
//   - AND THE MODEL HAS ONE HAND OF ITS OWN, `remember`, which walks the same
//     decide-and-apply path synchronously so the answer it gets back is the
//     title that actually landed.
//
// THE WHOLE FEATURE IS ABSENT RATHER THAN BROKEN WHEN THERE IS NO STORE. A nil
// [Config.Memory] is memory off: no block, no reflex call, and no `remember` on
// the belt — the model does not have the verb. That is what the memory.enabled
// row turns off, at the door, by declining to open a brain at all.
//
// A REFLEX FAILURE IS INVISIBLE. Every call here fails open: the block is empty,
// the extraction never happened, and the turn is exactly the turn it would have
// been if this file did not exist. internal/reflex states that law; this is the
// half of it that has to keep it.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/lane"
	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/reflex"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// memoryBlockRunes bounds what rides in the system prompt. Roughly 1200
	// tokens at four characters a token — a dozen remembered lines, which is far
	// more than any turn has ever needed, and a hard stop against a router that
	// asks for everything it was shown.
	memoryBlockRunes = 4800

	// memoryCueWords is the floor below which a message is not routed at all.
	// "yes", "go on", "and the other one" are continuations: they say nothing
	// the index could be matched against, and a call made on every one of them
	// is a call made on half the messages in a working conversation.
	memoryCueWords = 3

	// memoryListLimit is how many lines /memories prints when nobody narrowed
	// it. Fifty is a screen and a half — long enough to be the whole answer for
	// anybody, short enough not to bury a conversation.
	memoryListLimit = 50

	// memoryNeighbors is how many near things a candidate is judged against. It
	// is internal/reflex's own figure and the store's own idea of "close".
	memoryNeighbors = 3
)

// memoryBrain is the store this session remembers into, and the one piece of
// per-turn bookkeeping that goes with it.
//
// It sits OUTSIDE a.mu and holds a lock of its own, for the reason the job
// registry does: its writer is a goroutine that outlives the turn that started
// it, and it must never contend for the lock [Agent.Interrupt] has to be able to
// take.
type memoryBrain struct {
	store *store.Store
	// reflex is the failover memory for this conversation. It lives with the
	// store because every reflex call is a memory call, and because the
	// post-turn writer and next turn's router can overlap.
	reflex *reflex.Session

	mu sync.Mutex
	// injected is what the router asked for on the LAST routed turn — id and
	// title both, because the post-turn pass does not merely count these, it
	// shows them to the extractor and asks which of them helped
	// ([store.Store.RecordMemoryOutcome]). It is replaced per turn rather than
	// accumulated: what a turn retrieved belongs to that turn.
	injected []reflex.Stub
	// said holds the dim lines this session owes the person and has had no
	// stream to say them on. The post-turn pass writes memories after the turn
	// is sealed and its hub is closed, so a supersession settled there has
	// nowhere to land; the next turn's refresh flushes them.
	said []string
	// imported records that the legacy memory.md has already been looked at.
	// The rename on disk is the durable answer; this is what keeps a session
	// from stat-ing the same absent file every turn.
	imported bool
}

func newMemoryBrain(s *store.Store) *memoryBrain {
	return &memoryBrain{store: s, reflex: &reflex.Session{}}
}

// remembers reports whether this session has a brain at all. Every entry point
// in this file asks it first, and the answer is a fact about the wiring rather
// than about a setting: the door opens no store when memory is off.
func (a *Agent) remembers() bool { return a.memory != nil && a.memory.store != nil }

// memorySourceSession is the journal header id attached to a memory write. A
// test or embedded session without a journal still has the stable session name
// used everywhere else for lineage.
func (a *Agent) memorySourceSession() string {
	if id := a.journalID(); id != "" {
		return id
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID()
}

// reflexClient is this session's own client pinned to the reflex model, or nil
// when the ladder cannot name one.
//
// It is built per call rather than held, for [Agent.SetModel]'s sake: the last
// rung of the reflex ladder is the model the conversation is talking to, and a
// client bound at construction would answer for a model the person has since
// left. The binding itself is one small struct.
func (a *Agent) reflexClient() reflex.Completer {
	a.mu.Lock()
	source, model := a.config.RolesSource, a.model
	a.mu.Unlock()
	named, err := reflex.Model(roles.Source(source), model)
	if err != nil {
		return nil
	}
	fallback := reflex.FallbackModel(roles.Source(source), model)
	routed := a.routedCompleter()
	if routed == nil {
		return nil
	}
	return a.memory.reflex.Bind(billedCompleter{agent: a, inner: routed}, named, fallback, a.sayMemory)
}

// billedCompleter is what makes a reflex call cost something a person can see.
//
// THE PERSON PAYS FOR IT, SO IT CANNOT BE FREE — the argument
// [Agent.addAuxiliaryUsage] states for the title call, and it bites harder
// here: this is the only auxiliary call made twice EVERY
// turn, so a feature whose whole claim is that it is nearly free is exactly the
// one that has to prove it on the bill. It is folded into the SESSION total and
// never into a turn's, because no turn asked for it.
//
// It is a wrapper here rather than a change to internal/reflex because that
// package is the client only: it takes a [reflex.Completer] and has no idea
// what a session's accounting is.
type billedCompleter struct {
	agent *Agent
	inner Completer
}

func (b billedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	// EVERY REFLEX CALL IS A MEMORY CALL, said here because this wrapper is the
	// one door all of them go through — internal/reflex takes a completer and
	// knows nothing about roles, so a stamp inside it would be a stamp in the
	// wrong package. The role is what keeps the reflex off the status line: it
	// runs twice a turn, beside an answer somebody is reading, and a surface
	// that drew whichever call answered last was drawing this one
	// (internal/lane's roles.go, and phasenews.go for what it decides).
	// Preserve the operation's role: foreground recall and background keeping
	// have different latency value even though both are billed as memory.
	if !provider.RoleFrom(ctx).Known() {
		ctx = provider.WithRole(ctx, lane.RoleMemory)
	}
	response, err := b.inner.CompleteWithMessages(ctx, messages, options...)
	if err == nil {
		// The active model is read from the same options the provider reads.
		// Reflex may have moved this session to the low tier, and a fixed name
		// here would charge that answer to the model that failed.
		var request ai.Request
		for _, option := range options {
			if optionErr := option(&request); optionErr != nil {
				continue
			}
		}
		// The predicate is reflex's own, for the reason it is exported: a call
		// the reflex could not use and a call the bill calls empty must be the
		// same call. It used to mean "empty at the ceiling we sent"; nothing
		// sends a ceiling now, so it means empty.
		if reflex.AnsweredNothing(response) {
			b.agent.addEmptyReflexUsage(response, request.Model)
		} else {
			b.agent.addAuxiliaryUsageAs(response, request.Model, 1, string(roles.RoleReflex))
		}
	}
	return response, err
}

// ── the pre-turn block ──────────────────────────────────────────────────────

// memoryBlock is THE ONE SEAM every caller uses: the conversation before a
// turn, and a task node's reading beside its work ([nodeMemory]). cue is what
// the block is chosen against — the message just typed, or the brief the node
// is working from.
//
// It returns "" for everything that could go wrong, and that is the contract:
// no store, an empty index, a cue with nothing in it to route against, a
// provider that never answered. The caller renders what it gets and never
// branches on why.
//
// A caller reaching THIS function records nothing: use telemetry belongs to the
// conversation that was actually answered, and a node borrowing its parent's
// counters would credit the parent's memories with retrievals nobody made.
func (a *Agent) memoryBlock(ctx context.Context, cue string) string {
	return a.routedMemory(ctx, cue, nil, false)
}

// ── what a task node is handed ──────────────────────────────────────────────

// nodeMemory is the block one task node's brief was routed to, read ONCE per
// node run and BESIDE the work rather than in front of it (task_beside.go).
//
// IT USED TO BE A CONSTRUCTOR ARGUMENT, and that is the shape this replaces. The
// router was asked inside [Agent.newTaskAgentOn], so every worker a node built
// waited on one reflex call before it existed — six seconds on the measured node,
// serially, between the working copy and everything else — and a node that built
// a second worker, a repair round and a merge resolver asked the same question of
// the same brief four times. The cue is the node's assembled brief, which is
// settled at admission and does not move while the node runs, so one answer is
// the answer for every worker the node builds.
//
// MEMORY IS AN AID, NOT A CONTRACT, and that is what lets it arrive late. A worker
// whose first request goes out before the router has answered opens without the
// block, and the block rides the next request that worker makes: a task worker
// never clears what it was handed ([Agent.remembers] is false on a node) and the
// drain before every request lands it at the tail (agent.go's
// [Agent.landVolatileLocked]). That drain IS the join point — whatever has
// arrived by the time a request is assembled goes with it, and nothing waits.
//
// It fails open exactly as [Agent.memoryBlock] does: no store, no reflex, no
// answer, and every worker opens with the prompt it always did.
type nodeMemory struct {
	// id is the node this reading was routed for. A context carrying it may reach
	// a constructor building a DIFFERENT node's worker, and a part must never be
	// handed its parent's memories under its own brief.
	id uint64
	// start routes the brief the first time any worker asks for it, and the
	// runner may ask for it earlier to overlap the working copy being made.
	start  sync.Once
	route  func(context.Context) string
	ctx    context.Context
	reader *besideWork

	mu      sync.Mutex
	settled bool
	block   string
	// waiting is every worker built before the answer came back. They are handed
	// it when it does, and a worker closed by then takes nothing
	// ([Agent.takeMemory]).
	waiting []*Agent
}

type nodeMemoryKey struct{}

// withNodeMemory puts one node's memory reading on the node's context, for every
// worker the node goes on to build. It starts nothing: a node whose body never
// builds a worker (a saved program's run) makes no reflex call, which is what it
// made before this existed. The second answer is the one join point, and the
// node's runner defers it.
func (a *Agent) withNodeMemory(ctx context.Context, node *TaskNode) (context.Context, func()) {
	if a == nil || node == nil || !a.remembers() {
		return ctx, func() {}
	}
	cue := node.assembledBrief()
	memory := &nodeMemory{
		id:    node.id,
		ctx:   ctx,
		route: func(ctx context.Context) string { return a.memoryBlock(ctx, cue) },
	}
	return context.WithValue(ctx, nodeMemoryKey{}, memory), memory.end
}

// nodeMemoryOn is the reading a node's context carries for that node, or nil.
func nodeMemoryOn(ctx context.Context, node *TaskNode) *nodeMemory {
	memory, _ := ctx.Value(nodeMemoryKey{}).(*nodeMemory)
	if memory == nil || node == nil || memory.id != node.id {
		return nil
	}
	return memory
}

// begin starts the routing if nobody has yet. It is idempotent, so the runner
// can ask for it early and the constructor can ask for it again.
func (m *nodeMemory) begin() {
	if m == nil {
		return
	}
	m.start.Do(func() {
		m.reader = beside(m.ctx, func(ctx context.Context) {
			m.settle(m.route(ctx))
		})
	})
}

// settle is the router's answer arriving: kept for every worker still to come,
// and handed to every worker already waiting. The workers are handed it outside
// this reading's lock, because [Agent.takeMemory] takes the worker's own.
func (m *nodeMemory) settle(block string) {
	for _, worker := range m.keep(block) {
		worker.takeMemory(block)
	}
}

// keep records the answer and gives up the list of workers waiting on it.
func (m *nodeMemory) keep(block string) []*Agent {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.settled, m.block = true, block
	waiting := m.waiting
	m.waiting = nil
	return waiting
}

// handTo gives one worker the node's block: at once when it has arrived, and the
// moment it arrives otherwise.
func (m *nodeMemory) handTo(worker *Agent) {
	if m == nil || worker == nil {
		return
	}
	m.begin()
	if block, arrived := m.arrivedFor(worker); arrived {
		worker.takeMemory(block)
	}
}

// arrivedFor answers the block when it has come back, and otherwise puts the
// worker on the list [nodeMemory.settle] hands it to.
func (m *nodeMemory) arrivedFor(worker *Agent) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.settled {
		m.waiting = append(m.waiting, worker)
		return "", false
	}
	return m.block, true
}

// end is the join point [Agent.withNodeMemory] hands the runner.
func (m *nodeMemory) end() {
	if m == nil {
		return
	}
	// A READING NOBODY STARTED IS SHUT HERE, so a worker built after the node's
	// run is over cannot start one that would outlive it. The Once is also what
	// makes the reader safe to read: it was written inside the same Once.
	m.start.Do(func() {})
	m.reader.end()
}

// takeMemory is a task worker receiving the block its node's brief was routed
// to. It lands on the next request this worker assembles, and it replaces
// nothing a person typed: a worker has no turn of its own to route against.
func (a *Agent) takeMemory(block string) {
	if block == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return
	}
	a.memoryText = block
}

// refreshMemory is the conversation's own call: route this turn's message, hand
// any memory COMMAND in it to the store, and put the resulting block in front of
// the model.
//
// IT NO LONGER RUNS IN FRONT OF THE FIRST REQUEST, and that is the whole of what
// changed here. It used to be one line of [Agent.runTurn], awaited, so the
// person's own model was not asked until a reflex machine had answered: the call
// census of 2026-09-11 measured sixteen of them at a mean of 4.3 seconds and a
// maximum of 10.7, and every message paid it before its first token. It is
// started at the front of the turn and READ AT THE WIRE instead ([recallAside]),
// which is loop.go's stated law about auxiliary readings.
//
// IT REPORTS WHETHER THE BLOCK MOVED, because that is the one fact the aside has
// to act on: a block that is byte-for-byte what the request already carried
// changes nothing and is worth no re-ask, and on a conversation whose subject is
// holding still that is every turn after the first.
//
// It runs on its own goroutine and NOT under a.mu, because it makes a provider
// call. The lock is taken once at the end, for the two assignments.
//
// EVERY LINE IT SAYS GOES THROUGH [Agent.sayMemory], and never onto a stream it
// was handed when it started. A reading beside the turn may run after the turn
// it was started for has ended — it is never joined, and a busy machine can
// leave it unscheduled until then — and a line said onto that turn's closed
// stream is a line nobody ever reads. That is how a held supersession was lost:
// it was taken off the queue here and said to a hub that had already closed.
func (a *Agent) refreshMemory(ctx context.Context, cue string) bool {
	if !a.remembers() {
		return false
	}
	// The legacy file, once, before anything is routed — so a person whose
	// standing preferences lived in memory.md is answered out of them on the
	// very first turn after the upgrade rather than the second.
	a.importMemoryFile()
	// AND WHATEVER THE LAST POST-TURN PASS HAD NOWHERE TO SAY. It writes after
	// the turn is sealed and its hub closed, so a supersession settled there has
	// no stream; this is the first one it gets, or, when this reading has
	// outlived its own turn, the next.
	for _, line := range a.memory.takeNotices() {
		a.sayMemory(line)
	}

	// AND THERE IS NO PHASE WORD ON IT ANY MORE. `preparing saved context` was an
	// honest sentence about a wait the person really was serving — and it is not a
	// wait any more, so a status line that still said it would be this surface
	// naming a call nobody is behind. THE PHASE CLOCK BELONGS TO THE THING A
	// PERSON IS WAITING FOR (internal/lane's roles.go: only a visible role owns
	// it), and what they are waiting for from the instant they press enter is the
	// model's own first word.
	block := a.routedMemory(ctx, cue, a.sayMemory, true)

	a.mu.Lock()
	moved := a.memoryText != block
	a.memoryText = block
	// AND IT LANDS AT THE TAIL, not in message[0]. The drain immediately before
	// the first request would land it anyway (loop.go), and it is landed here as
	// well so that this pass is complete on its own: what it routed is in front
	// of the model the moment it has routed it, on any road that reaches here.
	// The lander does nothing at all when the block has not moved, which on a
	// conversation whose subject is holding still is every turn after the first.
	a.landVolatileLocked()
	a.mu.Unlock()
	return moved
}

// ── THE RECALL RIDES BESIDE THE TURN ────────────────────────────────────────
//
// recallAside is the pre-turn recall as a HANDLE the turn carries, in the shape
// [routeRace] already established for the work-or-words question (route_judge.go):
// started at the front of the turn, never waited on, asked at the moments where
// its answer can still be spent.
//
// WHAT IT IS FOR IS QUALITY AND NOT SPEED, which is the half that is easy to lose
// when a wait is taken off a path. The routed block is what remembered lines this
// request carries, so a recall that is merely DROPPED is a turn answered without
// the person's own preferences in front of it, and a dropped route also leaves the
// store's outcome ledger silent about lines it never got to offer — which is how
// the ranking learns. So nothing here drops anything. Every answer is APPLIED:
//
//   - Back before the request is assembled — the ordinary case once the whole
//     front of the turn overlaps it — and the first request carries it, exactly as
//     it always did.
//   - Back after the request went out but before the model's first token: the
//     generation is cut and re-asked ONCE, with the block in
//     ([Agent.cutGeneration], loop.go's errRecallCut). The person has read nothing
//     yet, so nothing is taken off their screen, and the provider's own prefix
//     cache makes the second send the cheap one.
//   - Back after the first token: it is LATE, and late means the NEXT step of this
//     same turn carries it — the block is already in the transcript by then — with
//     the lateness written down rather than passed over in silence.
//
// IT IS A [sidecar] LIKE EVERY OTHER READING BESIDE THE WORK (sidecar.go), with
// three facts of its own bolted on: whether the person has started reading, the
// one re-ask this turn may buy, and whether the block ended up late. Those are
// the recall's business and nothing else's, which is exactly why they are here
// and the start/take/end law is not.
type recallAside struct {
	agent   *Agent
	reading *sidecar[bool]
	// reached says whether the person has anything of this request in front of
	// them, so it can no longer be re-asked without taking something off their
	// screen. It is NOT this aside's own reading: it is the turn's, shared with
	// every other door that asks the same question, and it is handed over by the
	// loop ([reachedThePerson], steer.go). There were two readings of this once
	// and they disagreed about a visible thought.
	//
	// IT IS AN ATOMIC POINTER FOR THE REASON THE FIELD IT REPLACED WAS AN ATOMIC
	// BOOL: the two ends are two goroutines. The turn's own hands it over
	// ([recallAside.watch]) while this aside's reading may already be answering
	// from the sidecar's, and a plain pointer written on one and read on the
	// other is a race whether or not it is ever observed. A nil load is simply a
	// reading not handed over yet, which [reachedThePerson.did] already answers
	// as "nothing has reached them" — the safe answer, and the one that was true
	// at that instant.
	reached atomic.Pointer[reachedThePerson]
	// resent is the ONE re-ask this aside may buy. One, because a second would be
	// a door that could cut generations forever, and because there is only ever
	// one block to land.
	resent atomic.Bool
	// late says the block landed behind the first token and is riding the next
	// step instead of this one. The turn's own decomposition row reads it.
	late atomic.Bool
	// cutAt is when the re-ask cut the request in flight, in Unix nanoseconds,
	// and zero where no re-ask happened.
	//
	// ── WHY THE MOMENT AND NOT THE FACT ─────────────────────────────────────
	//
	// [recallAside.reasked] already says a re-ask happened, and the decomposition
	// row already names it. What neither of them can say is what it COST, and
	// without that the move cannot be judged at all.
	//
	// The turn's own law asserts `message → the main request on the wire ≤ 50 ms`,
	// and a re-asked turn passes it: the first request really does leave in a
	// millisecond. It is then THROWN AWAY. On the one such turn the ledger holds
	// (2026-09-12T03:06Z), SendMS was 1 and the person waited 7,389 ms for their
	// first word, because the request that carried it did not leave until the
	// recall landed. The instrument reported the law kept on a turn where a
	// reading cost the person seven seconds — satisfied in the letter by sending
	// a request nobody intended to read.
	//
	// So the row carries how long the discarded request was on the wire, which is
	// exactly the difference between the two moves available here: cutting and
	// re-asking, and simply assembling the block before the first request. Both
	// reach the first word at the same instant — the recall's own duration plus
	// one time-to-first-token — and the re-ask pays for two requests to get
	// there. Which is right depends on whether the second request is cheaper for
	// having warmed the prefix cache, and that is a question about a population
	// of turns rather than about one.
	//
	// THE LEDGER CANNOT ANSWER IT YET, WHICH IS WHY THIS IS AN INSTRUMENT AND NOT
	// A RULE. `journalPace` landed days ago: there are three pace rows on disk
	// and one of them has a recall beside it. Choosing between the two moves on
	// n=1 would be a threshold nobody could derive, which is the one thing the
	// architecture bar refuses outright.
	cutAt atomic.Int64
}

// startRecallLocked launches the recall beside the title, from the one place a
// turn starts (agent.go's [Agent.startTurnLocked]).
//
// IT IS STARTED THERE AND NOT IN THE LOOP, and the difference is measured in the
// only currency that matters here: how much of the turn's own preparation the
// reflex call gets to hide behind. Everything between a person's keystroke and
// the wire — the system prompt refresh, the acceptance, the baseline, the harness
// route, the work-or-words race, what the other windows have landed, the
// transcript snapshot — now runs WHILE it is in flight rather than after it.
//
// It takes no lock of its own: it is called with a.mu held, and the goroutine
// below takes the lock when it needs it.
//
// THE TURN'S HUB IT IS HANDED IS NOT WHERE THE RECALL SPEAKS. The reading may
// outlive this turn, and even inside it a line sent before the turn's stream is
// subscribed is a line its reader never gets, so every line goes through
// [Agent.sayMemory], which reads whichever stream is live under the same lock.
func (a *Agent) startRecallLocked(ctx context.Context, cue string) {
	a.recall = nil
	// [Agent.remembers] takes no lock — it reads two pointers fixed at
	// construction — so it is legal under a.mu and is the same gate the routing
	// pass itself keeps.
	if !a.remembers() || strings.TrimSpace(cue) == "" {
		return
	}
	aside := &recallAside{agent: a}
	aside.reading = readBeside(ctx,
		func(readCtx context.Context) bool { return a.refreshMemory(readCtx, cue) },
		// AND THE ACT IS THE INTERRUPTION, which is the only power a reading beside
		// the work has over it (sidecar.go). A block that moved and landed before
		// the person read a word cuts the request so it can be sent again carrying
		// it; everything else is recorded and spent later.
		func(moved bool) {
			if moved {
				aside.applyOrDefer()
			}
		})
	a.recall = aside
}

// takeRecall is the turn's handle on the recall its own Submit started. It is
// taken rather than read so that a turn cannot be handed the last turn's aside.
func (a *Agent) takeRecall() *recallAside {
	a.mu.Lock()
	defer a.mu.Unlock()
	aside := a.recall
	a.recall = nil
	return aside
}

// watch hands this aside the turn's own reading of what the person has in front
// of them. The loop calls it once, with the reading its stream observer fills.
func (r *recallAside) watch(reached *reachedThePerson) {
	if r != nil {
		r.reached.Store(reached)
	}
}

// applyOrDefer is what the recall does with a block that moved: spend it on THIS
// request if the person has read nothing yet, and on the next step if they have.
func (r *recallAside) applyOrDefer() {
	if r == nil {
		return
	}
	if r.reached.Load().did() {
		r.late.Store(true)
		return
	}
	if r.resent.Load() {
		return
	}
	// A GENERATION THAT HAS NOT STARTED NEEDS NO CUTTING. The block is already in
	// the transcript, so the request being assembled will carry it and this aside
	// keeps its one re-ask for a turn that actually needs one.
	if r.agent.cutGeneration(errRecallCut) {
		r.resent.Store(true)
		r.cutAt.Store(time.Now().UnixNano())
	}
}

// wasLate reports that the block arrived behind the first token, for the turn's
// decomposition row. It is the honest half of the emptiness law here: a recall
// that could not be spent on the request it was routed for is news, and a build
// that simply said nothing about it is how a dropped route looked like a turn
// with no memories to offer.
func (r *recallAside) wasLate() bool { return r != nil && r.late.Load() }

// reasked reports that this turn spent its one cut-and-re-ask on the block.
func (r *recallAside) reasked() bool { return r != nil && r.resent.Load() }

// cutMoment is when the re-ask cut the request in flight, and the zero time
// where there was none. See [recallAside.cutAt] for what it is for.
func (r *recallAside) cutMoment() time.Time {
	if r == nil {
		return time.Time{}
	}
	nanos := r.cutAt.Load()
	if nanos == 0 {
		return time.Time{}
	}
	return time.Unix(0, nanos)
}

// end discards the recall, whether or not it has answered — [sidecar.end].
func (r *recallAside) end() {
	if r == nil {
		return
	}
	r.reading.end()
}

// everAsked reports whether this turn routed anything at all, for the
// decomposition row loop.go writes.
func (r *recallAside) everAsked() bool { return r != nil && r.reading.everAsked() }

// routedMemory is the whole pre-turn pass. say is where the one line about an
// instruction to the store lands, and nil says nothing, which is a node's
// reading borrowing its parent's store; record says whether the ids it injected
// are this session's to count.
func (a *Agent) routedMemory(ctx context.Context, cue string, say func(string), record bool) string {
	if !a.remembers() {
		return ""
	}
	if record {
		a.memory.setInjected(nil)
	}
	if memoryTrivialCue(cue) {
		return ""
	}
	// THE STORE RANKS, THE MODEL REJECTS. What the router is shown is the few
	// lines most likely to bear on this cue, ranked in SQL against the words of
	// the message, how often each line has actually helped, and how recently it
	// changed ([store.Store.MemoryCandidates]). It used to be every title in the
	// store, which cost about 3,200 tokens at two hundred memories and grew with
	// everything the person had ever asked to be kept.
	//
	// The model is still asked, and it is asked the same question in the same
	// words, because the failure mode here is the SEMANTIC NEAR-MISS rather than
	// the random hit: one top-retrieved non-answer line costs 18–20% relative
	// (Cuconasu et al., SIGIR 2024) while random ones are harmless. A store of
	// near-synonymous preferences is nothing but hard distractors, and rejecting
	// them is the one job arithmetic cannot do. Showing it the haystack is what
	// stops.
	candidates, err := a.memory.store.MemoryCandidates(cue, store.MemoryCandidatesDefault)
	if err != nil || len(candidates) == 0 {
		// AN EMPTY SHORTLIST IS NOT A CALL, and with two arithmetic rankings
		// under it an empty one means an empty store. There is nothing to route
		// against, and a reflex that billed for that would bill for every turn
		// of every fresh install.
		return ""
	}
	client := a.reflexClient()
	if client == nil {
		return ""
	}
	stubs := make([]reflex.Stub, 0, len(candidates))
	for _, row := range candidates {
		stubs = append(stubs, reflex.Stub{ID: row.ID, Title: row.Title, Type: row.Type, Scope: row.Scope})
	}
	routed, err := reflex.Route(ctx, client, cue, stubs)
	if err != nil {
		return ""
	}
	if routed.Cmd != nil {
		a.runMemoryCommand(say, *routed.Cmd)
	}
	if len(routed.Inject) == 0 {
		return ""
	}
	memories, err := a.memory.store.GetMemories(routed.Inject)
	if err != nil || len(memories) == 0 {
		return ""
	}
	block, kept := renderMemoryBlock(memories, time.Now())
	if record {
		a.memory.setInjected(kept)
	}
	return block
}

// runMemoryCommand is what the router does with an instruction ABOUT memory
// rather than a turn that needs some: "remember that I prefer tabs", "forget
// what I said about the deploy".
//
// It is answered in one dim line and nothing else. The person gave an
// instruction and it either happened or it did not; a card, an event kind or a
// paragraph would all be this surface making a ceremony out of a note.
func (a *Agent) runMemoryCommand(say func(string), cmd reflex.Cmd) {
	if strings.TrimSpace(cmd.Arg) == "" {
		return
	}
	if say == nil {
		say = func(string) {}
	}
	switch cmd.Name {
	case "remember":
		memory, err := a.memory.store.AddMemory(store.Memory{
			Type:          memoryTypeOf(cmd.Arg),
			Scope:         store.MemoryScopeUser,
			Title:         memoryTitleFrom(cmd.Arg),
			Text:          cmd.Arg,
			SourceSession: a.memorySourceSession(),
		})
		if err != nil {
			return
		}
		say("remembered · " + memory.Title)
	case "forget":
		title, err := a.forgetMatching(cmd.Arg)
		if err != nil {
			return
		}
		if title == "" {
			say("nothing matched · " + cmd.Arg)
			return
		}
		say("forgot · " + title)
	}
}

// renderMemoryBlock writes the block and reports which lines actually made it
// in.
//
// The order is the ROUTER'S — the store returns what it was asked for in the
// order it was asked (GetMemories states that contract), and the router is the
// only thing in the system that saw the actual question. So when the budget runs
// out it is the TAIL that goes: the last line the router named is the one it
// thought about least, and the first one stays where a model actually reads it
// (Lost in the Middle, TACL 2024 — gold at position 1 scores 73.4 against 50.5
// mid-context, which is itself below answering with nothing at all).
//
// EVERY LINE CARRIES ITS AGE, at about five tokens each. A model cannot judge
// whether a remembered thing has gone stale if it cannot see when it was
// learned, and LongMemEval (ICLR 2025) measures time-awareness as the largest
// single category lever it ablated. It is SHOWN and never asked for: the same
// paper found a small model asked to PRODUCE a date range hallucinates one, and
// everything upstream of this block is a two-hundred-token model on a cheap
// tier. An unknown age renders as nothing, which is this tree's law about
// zeroes and is also the honest answer for a row whose journal entry predates
// the column.
func renderMemoryBlock(memories []store.Memory, now time.Time) (string, []reflex.Stub) {
	var (
		body strings.Builder
		kept []reflex.Stub
		used int
	)
	for _, memory := range memories {
		line := "- " + memory.Title + ": " + memory.Text
		if memory.Title == "" {
			line = "- " + memory.Text
		}
		if age := store.AgeLabel(memory.UpdatedAt, now); age != "" {
			line += " (learned " + age + ")"
		}
		length := utf8.RuneCountInString(line) + 1
		if used+length > memoryBlockRunes {
			break
		}
		used += length
		body.WriteString(line)
		body.WriteString("\n")
		kept = append(kept, reflex.Stub{
			ID: memory.ID, Title: memory.Title, Type: memory.Type, Scope: memory.Scope,
		})
	}
	if len(kept) == 0 {
		return "", nil
	}
	return "\n<memory>\n" + body.String() + "</memory>\n", kept
}

// memoryTrivialCue reports that there is nothing here worth a call: an empty
// message, or a continuation of fewer than three words with no memory verb in
// it. "yes", "go on", "that one" say nothing the index could be matched
// against; "forget that" is three characters shorter and is the whole point of
// the exception.
func memoryTrivialCue(cue string) bool {
	fields := strings.Fields(cue)
	if len(fields) == 0 {
		return true
	}
	if len(fields) >= memoryCueWords {
		return false
	}
	lowered := strings.ToLower(cue)
	for _, verb := range []string{"remember", "forget"} {
		if strings.Contains(lowered, verb) {
			return false
		}
	}
	return true
}

// setInjected replaces what this turn retrieved.
func (m *memoryBrain) setInjected(stubs []reflex.Stub) {
	m.mu.Lock()
	m.injected = stubs
	m.mu.Unlock()
}

// takeInjected reads what was injected and clears it, so no turn can account
// for another turn's retrievals.
func (m *memoryBrain) takeInjected() []reflex.Stub {
	m.mu.Lock()
	stubs := m.injected
	m.injected = nil
	m.mu.Unlock()
	return stubs
}

// queueNotice holds one dim line until there is somewhere to say it.
func (m *memoryBrain) queueNotice(text string) {
	m.mu.Lock()
	m.said = append(m.said, text)
	m.mu.Unlock()
}

// takeNotices drains the held lines.
func (m *memoryBrain) takeNotices() []string {
	m.mu.Lock()
	held := m.said
	m.said = nil
	m.mu.Unlock()
	return held
}

// ── the post-turn pass ──────────────────────────────────────────────────────

// learnFromTurn is what happens after the model has finished answering: the
// exchange is read by the extractor, and anything worth keeping is settled
// against what is already there.
//
// IT IS NEVER ON THE PERSON'S PATH. The turn is sealed, the room is quiet, and
// this runs in a goroutine on the session's own context — cancelled by Close,
// waited for by Close, and silent whatever happens to it. There is no event kind
// for "a small thing did not work" (title.go's reasoning, unchanged).
func (a *Agent) learnFromTurn(userMsg, assistantMsg string) {
	if !a.remembers() {
		return
	}
	injected := a.memory.takeInjected()
	if !a.startMemoryJob() {
		return
	}
	go func() {
		defer a.memoryJobs.Done()
		ctx := a.memoryCtx
		client := a.reflexClient()
		if client == nil {
			return
		}
		found, err := reflex.Extract(ctx, client, userMsg, assistantMsg, injected)
		if err != nil {
			// AND NOTHING IS COUNTED. The accounting below is the extractor's
			// answer; a provider outage is not evidence that a memory failed to
			// help, and recording it as one would let somebody else's bad
			// afternoon push a good line down the store's ranking.
			return
		}
		a.recordMemoryOutcome(injected, found.Used)
		// THE STATE DELTA IS SETTLED FIRST, AND SEPARATELY FROM THE MEMORY GATE.
		// Mem answers whether this exchange held anything worth carrying into
		// ANOTHER session; the delta answers what it did to THIS one, and those
		// are different questions — most exchanges that move the work forward
		// are worth remembering nowhere. The card is what compaction now leans
		// on, so a delta dropped because mem came back 0 would leave the pass
		// with nothing to hand the model back (card.go).
		if found.State != nil {
			a.mergeStateCard(*found.State)
		}
		if found.Mem == 0 {
			return
		}
		_, _ = a.applyCandidate(ctx, client, found)
	}()
}

// recordMemoryOutcome settles what this turn's injected memories did: the ones
// the extractor named bore on the answer, and the rest were put in front of a
// model and bore on nothing.
//
// THE COUNTER MEASURES HELP, NOT INJECTION. It used to credit every id the
// router named, at the top of this pass, on the argument that being handed to a
// model is a fact already true — which it is, and which is not the fact the
// ranking needs. RoMeRL (arXiv 2608.02508) names that the "memory-reward trap":
// co-retrieved memories all take the credit, so a line that has never once
// changed an answer rises on the strength of sounding relevant.
//
// The unused half is the important half, and it is fixstore.go's bargain
// exactly: a fix that was offered and then failed is counted against itself,
// because a store that only ever counted successes would rank a coin toss at
// the top of its own signature forever.
func (a *Agent) recordMemoryOutcome(injected []reflex.Stub, used []string) {
	if len(injected) == 0 {
		return
	}
	helped := make(map[string]bool, len(used))
	for _, id := range used {
		helped[id] = true
	}
	var confirmed, unused []string
	for _, stub := range injected {
		if helped[stub.ID] {
			confirmed = append(confirmed, stub.ID)
			continue
		}
		unused = append(unused, stub.ID)
	}
	_ = a.memory.store.RecordMemoryOutcome(confirmed, unused)
	// AND THE JOURNAL TAKES A PHOTOGRAPH ONCE A WEEK, so a Rebuild lands on a
	// ranking floor rather than on zero. The interval is fixstore.go's own, and
	// it is borrowed rather than respelled for the reason that file states about
	// a person's working memory — there must not be two spellings of "a week"
	// in one feature.
	_, _ = a.memory.store.SnapshotMemoryRanking(fixDecayInterval)
}

// startMemoryJob registers one background memory pass, and refuses once the
// session is closing. It is the [jobRegistry]'s bargain in miniature: the work
// is tracked, so Close can wait for it, and cancelled, so Close does not wait
// long.
func (a *Agent) startMemoryJob() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.memoryCtx == nil {
		return false
	}
	a.memoryJobs.Add(1)
	return true
}

// applyCandidate settles one candidate against its neighbours and writes the
// answer. It is the ONE write path: the post-turn pass and the `remember` tool
// both come through here, so a memory written by the model and a memory the
// session noticed cannot be deduplicated by two different rules.
func (a *Agent) applyCandidate(ctx context.Context, client reflex.Completer, candidate reflex.ExtractResult) (store.Memory, error) {
	if strings.TrimSpace(candidate.Text) == "" {
		return store.Memory{}, errors.New("session: a memory with no text says nothing")
	}
	fresh := store.Memory{
		Type:          candidate.Type,
		Scope:         candidate.Scope,
		Title:         strings.TrimSpace(candidate.Title),
		Text:          candidate.Text,
		Tags:          candidate.Tags,
		SourceSession: a.memorySourceSession(),
	}
	if fresh.Title == "" {
		fresh.Title = memoryTitleFrom(candidate.Text)
	}
	neighbors, err := a.memory.store.SearchMemories(candidate.Text, memoryNeighbors)
	if err != nil || len(neighbors) == 0 {
		// NOTHING NEAR IT IS NOT A QUESTION. A store with no opinion about this
		// subject has nothing for a decider to weigh, and asking anyway would be
		// a call whose answer is known.
		return a.memory.store.AddMemory(fresh)
	}
	near := make([]reflex.Neighbor, 0, len(neighbors))
	for _, neighbor := range neighbors {
		near = append(near, reflex.Neighbor{ID: neighbor.ID, Title: neighbor.Title, Text: neighbor.Text})
	}
	decided, err := reflex.Decide(ctx, client, candidate, near)
	if err != nil {
		return store.Memory{}, err
	}
	if title := strings.TrimSpace(decided.Title); title != "" {
		fresh.Title = title
	}
	if text := strings.TrimSpace(decided.Text); text != "" {
		fresh.Text = text
	}
	if len(decided.Tags) > 0 {
		fresh.Tags = decided.Tags
	}
	switch decided.Op {
	case "add":
		return a.memory.store.AddMemory(fresh)
	case "update":
		// A MISSING TARGET IS A SKIP. internal/reflex validates the enum and
		// leaves the id to the only thing that knows whether it names anything;
		// this is that thing, and the honest answer to "refine the memory that
		// is not there" is to change nothing.
		if decided.TargetID == "" {
			return store.Memory{}, nil
		}
		if err := a.memory.store.UpdateMemoryFromSession(decided.TargetID, fresh.Title, fresh.Text, fresh.Tags, fresh.SourceSession); err != nil {
			return store.Memory{}, err
		}
		return store.Memory{ID: decided.TargetID, Title: fresh.Title, Text: fresh.Text}, nil
	case "supersede":
		if decided.TargetID == "" {
			return store.Memory{}, nil
		}
		// The old line is read BEFORE it is retired, because the note names it
		// and a read afterwards would be a second query for a row this one
		// already had in hand.
		retired, _, _ := a.memory.store.MemoryRecord(decided.TargetID)
		replacement, err := a.memory.store.SupersedeMemory(decided.TargetID, fresh)
		if err != nil {
			return store.Memory{}, err
		}
		a.saySuperseded(retired.Title, replacement.Title)
		return replacement, nil
	}
	return store.Memory{}, nil
}

// saySuperseded is the one dim line a retirement gets, and the reason it exists
// is that it used to get none.
//
// A supersession is a model deciding, out of ordinary conversation and with
// nobody asked, that something the person told this store is no longer true.
// That is MINJA's exact mechanism (arXiv 2503.03704: 98.2% injection success
// through nothing but ordinary queries), and BEAM (ICLR 2026) measures
// contradiction resolution at 0.000–0.053 for every model and method it tested
// — retiring a fact correctly is the hardest thing in this literature and
// nothing can do it. `remember` and `forget` each say one line when they act.
// This does the same, in the same words and on the same lane:
//
//	superseded · deploys on Fridays → deploys on Tuesdays
//
// It is dim, it is one line, and it is not a question. The record is not
// destroyed either — the old row stays readable at its id — so the line is
// where a person notices, and /memory is where they look.
func (a *Agent) saySuperseded(oldTitle, newTitle string) {
	oldTitle, newTitle = strings.TrimSpace(oldTitle), strings.TrimSpace(newTitle)
	if oldTitle == "" || newTitle == "" {
		// A retirement whose two halves cannot both be named is a line that
		// would say less than nothing.
		return
	}
	a.sayMemory("superseded · " + oldTitle + " → " + newTitle)
}

// sayMemory puts one dim line in front of the person, on the turn's own stream
// when there is one and on the next turn's when there is not.
//
// THE HELD LINE IS NOT A COMPROMISE, it is where this pass lives. The post-turn
// pass runs once the turn is sealed and its hub closed, on the session's own
// lifetime — that is the whole reason nothing is waiting on it — so a line
// written there has no stream in the room, and holding it until the next
// refresh is what keeps it from being lost instead. `remember` and `forget` are
// settled by the recall beside the turn, which usually lands inside it and
// sometimes after it, and this is the one door for both.
//
// THE HUB ITSELF SAYS WHETHER THE LINE LANDED, and that is what makes "when
// there is one" true — not an ordering between this door and the roads that end
// turns. [eventHub.send] reports it from under the hub's own lock, which is the
// only place the answer is not already stale, and a line that did not land is
// held for the next turn exactly as a line that found no hub at all is. A nil
// hub answers false by the same door, so there is one road here and not two.
//
// The line is still said under a.mu, which is how this reads the pointer at all
// — and because every road that ends a turn clears a.hub under this lock before
// closing the hub, the usual case is that a live hub takes it. That is now an
// explanation of why holding a line is rare, and no longer the reason the door
// is correct. A hub send is an append and a signal, so the lock is held for
// nothing longer than the steer drains already hold it.
func (a *Agent) sayMemory(text string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.hub.send(Event{Kind: EventNotice, Text: text}) {
		return
	}
	a.memory.queueNotice(text)
}

// ── what a person and the model can ask for by hand ─────────────────────────

// MemoryLine is one remembered thing as a surface prints it.
type MemoryLine struct{ ID, Title, Text string }

// Remembers reports whether this session has a brain at all.
//
// It is exported for the surface's sake and it is the difference between two
// very different lines on screen: "nothing is remembered yet", which is a fact
// about an empty store, and "memory is off for this session", which is a fact
// about the wiring and names the row that changes it. A surface that could only
// see the error would say the first when it meant the second.
func (a *Agent) Remembers() bool { return a.remembers() }

// Remember writes one thing down now and answers with the title it landed
// under. It is /remember and it is the `remember` tool, which are the same
// errand asked by two different mouths.
//
// It goes through the same decide-and-apply path the post-turn pass does, so
// telling aforge twice in two sessions that you prefer tabs refines one memory
// instead of making two.
func (a *Agent) Remember(text string) (string, error) {
	return a.RememberScoped(text, store.MemoryScopeUser)
}

// RememberScoped is Remember with the blast radius named: something true about
// you everywhere, only inside this project, or only on this machine.
func (a *Agent) RememberScoped(text, scope string) (string, error) {
	if !a.remembers() {
		return "", errors.New("this build is not remembering anything")
	}
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "", errors.New("there is nothing to remember")
	}
	if strings.TrimSpace(scope) == "" {
		scope = store.MemoryScopeUser
	}
	candidate := reflex.ExtractResult{
		Mem:   1,
		Type:  memoryTypeOf(text),
		Scope: scope,
		Title: memoryTitleFrom(text),
		Text:  text,
	}
	client := a.reflexClient()
	if client == nil {
		// NO REFLEX IS NOT NO MEMORY. A person who typed /remember said what
		// they wanted kept; refusing them because a router model is unreachable
		// would be losing their words to somebody else's outage.
		memory, err := a.memory.store.AddMemory(store.Memory{
			Type: candidate.Type, Scope: candidate.Scope,
			Title: candidate.Title, Text: candidate.Text,
			SourceSession: a.memorySourceSession(),
		})
		if err != nil {
			return "", err
		}
		return memory.Title, nil
	}
	ctx := a.memoryContext()
	memory, err := a.applyCandidate(ctx, client, candidate)
	if err != nil {
		return "", err
	}
	if memory.Title == "" {
		// The decider skipped it: the store already holds this, which is the
		// answer rather than a failure.
		return candidate.Title, nil
	}
	return memory.Title, nil
}

// Forget drops the best match for a query and answers with the title it
// dropped, or "" when nothing matched.
func (a *Agent) Forget(query string) (string, error) {
	if !a.remembers() {
		return "", errors.New("this build is not remembering anything")
	}
	return a.forgetMatching(query)
}

func (a *Agent) forgetMatching(query string) (string, error) {
	found, err := a.memory.store.SearchMemories(query, 1)
	if err != nil {
		return "", err
	}
	if len(found) == 0 {
		return "", nil
	}
	if err := a.memory.store.ForgetMemoryFromSession(found[0].ID, a.memorySourceSession()); err != nil {
		return "", err
	}
	return found[0].Title, nil
}

// Memories lists what is kept, newest-touched first — or the best matches for a
// query, when one is given.
func (a *Agent) Memories(query string) ([]MemoryLine, error) {
	if !a.remembers() {
		return nil, errors.New("this build is not remembering anything")
	}
	var (
		found []store.Memory
		err   error
	)
	if strings.TrimSpace(query) == "" {
		found, err = a.memory.store.ListMemories("", memoryListLimit)
	} else {
		found, err = a.memory.store.SearchMemories(query, memoryListLimit)
	}
	if err != nil {
		return nil, err
	}
	lines := make([]MemoryLine, 0, len(found))
	for _, memory := range found {
		lines = append(lines, MemoryLine{ID: memory.ID, Title: memory.Title, Text: memory.Text})
	}
	return lines, nil
}

// memoryContext is the session's own background context, or the process's when
// there is none. Nothing here belongs to a turn: a write started by /remember
// must not die because the person interrupted the answer they were reading.
func (a *Agent) memoryContext() context.Context {
	a.mu.Lock()
	ctx := a.memoryCtx
	a.mu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// ── the legacy file ─────────────────────────────────────────────────────────

// importMemoryFile carries a person's old memory.md into the store, once, and
// then renames it out of the way.
//
// IT NEVER DELETES ANYTHING. The file is renamed to memory.md.imported, which is
// both the durable record that this already happened and the person's copy of
// what they wrote. A second run finds no memory.md and does nothing; a person
// who wants it back has the file.
func (a *Agent) importMemoryFile() {
	if !a.remembers() {
		return
	}
	a.memory.mu.Lock()
	if a.memory.imported {
		a.memory.mu.Unlock()
		return
	}
	a.memory.imported = true
	a.memory.mu.Unlock()

	path := strings.TrimSpace(a.config.MemoryImport)
	if path == "" {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		return
	}
	var lines []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), store.MemoryTextRunes*4)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		if utf8.RuneCountInString(line) > store.MemoryTextRunes {
			line = string([]rune(line)[:store.MemoryTextRunes])
		}
		lines = append(lines, line)
	}
	_ = file.Close()

	imported := 0
	for _, line := range lines {
		if _, err := a.memory.store.AddMemory(store.Memory{
			Type:          store.MemoryFact,
			Scope:         store.MemoryScopeUser,
			Title:         memoryTitleFrom(line),
			Text:          line,
			SourceSession: a.memorySourceSession(),
		}); err == nil {
			imported++
		}
	}
	// The rename happens whether or not a line landed: a file that could not be
	// imported this time will not import better next time, and a session that
	// re-read it every turn would be a session that never stopped trying.
	if err := os.Rename(path, path+".imported"); err != nil {
		return
	}
	if imported > 0 {
		a.sayMemory(fmt.Sprintf("imported %d memories from memory.md", imported))
	}
}

// ── the small judgements ────────────────────────────────────────────────────

// memoryTypeOf is the whole heuristic, and it is deliberately two answers wide.
// A line carrying "prefer", "always" or "never" is somebody stating how they
// want things done; everything else is a fact. The three richer types
// (decision, correction, project_state) are the extractor's to choose, because
// telling them apart takes reading an exchange — which is exactly what a
// heuristic here cannot do.
func memoryTypeOf(text string) string {
	lowered := strings.ToLower(text)
	for _, word := range []string{"prefer", "always", "never"} {
		if strings.Contains(lowered, word) {
			return store.MemoryPreference
		}
	}
	return store.MemoryFact
}

// memoryTitleFrom names a memory by its first six words. A title is an index
// line — the router reads hundreds at once and picks by them — so the opening of
// the sentence is both the cheapest and the most recognisable thing to use.
func memoryTitleFrom(text string) string {
	fields := strings.Fields(text)
	if len(fields) > 6 {
		fields = fields[:6]
	}
	title := strings.Join(fields, " ")
	if utf8.RuneCountInString(title) > store.MemoryTitleRunes {
		title = string([]rune(title)[:store.MemoryTitleRunes])
	}
	return title
}

// ── the one tool ────────────────────────────────────────────────────────────

const rememberDescription = "Remember one durable thing across sessions: a preference the person stated, a correction they made, a decision that will still bind tomorrow. Write it as a standing truth in one short line ('prefers tabs over spaces in Go'), not as a log of what just happened. It is settled against what is already remembered — a near-duplicate refines the existing line rather than adding a second — and the title it landed under comes back to you. Do not remember what the transcript already holds, what the repo or AGENTS.md already records, or anything that will be false tomorrow."

const rememberSchemaJSON = `{"type":"object","properties":{"text":{"type":"string","description":"The single line to remember, in plain words"},"scope":{"type":"string","enum":["user","project","env"],"description":"How far the truth reaches: the person everywhere (default), this project only, or this machine only"}},"required":["text"],"additionalProperties":false}`

// The gloss a person reads beside a memory call is the thing itself —
// "remember prefers tabs over spaces" — for the reason every other tool's gloss
// is its path or its command: the tool name alone says a memory call happened
// and not what it did.
func init() { glossField["remember"] = "text" }

// memoryTools is the one hand, or nothing at all when this session has no brain.
//
// Nothing at all is the point, and it is the law CLAUDE.md states: a belt
// carrying `remember` against no store is a model told it can remember, whose
// every call is refused. A session without memory simply does not have the verb.
func (a *Agent) memoryTools() []bare.Tool {
	if !a.remembers() {
		return nil
	}
	return []bare.Tool{{
		Name:        "remember",
		Description: rememberDescription,
		Schema:      json.RawMessage(rememberSchemaJSON),
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Text  string `json:"text"`
				Scope string `json:"scope"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}
			title, err := a.RememberScoped(parsed.Text, parsed.Scope)
			if err != nil {
				return "Could not remember that: " + err.Error(), true, nil
			}
			return "remembered: " + title, false, nil
		},
	}}
}

// refreshSystemLocked rebuilds message[0] from the base prompt, the folders
// somebody attached, the person's standing orders and this session's record of
// what it has decided.
//
// message[0] is REPLACED rather than appended to: a.system stays the base, so
// every refresh renders base + current blocks instead of stacking one act's
// block on top of the last one's.
//
// THE ATTACHED FOLDERS ARE THE THIRD BLOCK and they are here for the standing
// orders' reason exactly: a folder somebody attached holds for the life of the
// conversation until they remove it, so it moves once per deliberate act and
// otherwise renders byte for byte (placescontext.go).
//
// THE DECISION RECORD IS A SNAPSHOT AND NOT A LIVE READING, and that is the one
// thing in this block that does not move when the thing behind it does. A
// question answered — every `ask`, and every `allow once` on a tool — used to
// rewrite this message the moment the answer was applied, which bought a cold
// prefix over the whole conversation for a line the model already had: the
// answer's own tool result is in front of it either way, and what the record is
// FOR is the gate that refuses the same question twice ([Question.Check] reads
// the file, not this). So the section is re-read only when message[0] is being
// rebuilt for some OTHER reason — a folder attached, a standing order agreed,
// the clock brought forward — all of which re-price the prefix anyway, and every
// decision made since rides in the transcript where it happened.
//
// WHAT LIVES HERE IS WHAT MOVES ONLY ON A DELIBERATE ACT, and that is the whole
// rule. message[0] sits in front of every message there is, so one changed byte
// in it re-prices the entire transcript at the uncached rate — five times the
// cached one — on the very next request. The base prompt never moves. A folder
// somebody attached holds until they remove it. An order was agreed on a card
// and holds until the person says otherwise, and a conversation may run all day
// without one moving (standing_world.go). A decision is recorded when a question
// is answered and never again. Each of those is a thing a person did, at most a
// handful of times in a session, and what it costs is the same cold prefix the
// clock costs when it is brought forward (prompt.go's clockRefresh), for the
// same reason: a model reasoning from a stale standing fact is worse than a
// re-priced conversation.
//
// THE BLOCKS THAT MOVE ON A TURN ARE NOT HERE, and the memory block was the last
// of them to leave. The state card is rewritten by the post-turn pass every time
// a delta lands and the other windows' work is re-read at the start of every
// turn; the routed memory block is re-chosen against the person's own words at
// the start of every turn, and [renderMemoryBlock] re-stamps every line it keeps
// with an age label whose granularity is hourly for anything learned today
// (store.AgeLabel), so it can move on a turn where the router chose identically.
// All three ride at the TAIL of the transcript now, as appended notes
// ([Agent.landVolatileLocked]), where a change costs the note and nothing behind
// it. What that buys is stated as a law and pinned as one: message[0] is
// byte-identical from one turn to the next unless somebody did something
// (prefixcache_test.go).
//
// WHAT MOVING THE MEMORY BLOCK COSTS is a superseded line left standing in the
// transcript where it was said, which is why the note it rides in says the last
// one holds ([memoryNoteOpening]). That is the trade, and it is the right way
// round: a stale line the model is told is stale is cheaper than re-pricing
// every token of a working session's conversation on the turn the subject moved.
func (a *Agent) refreshSystemLocked() {
	if len(a.messages) == 0 {
		return
	}
	// THE SNAPSHOT IS TAKEN WHEN THE REST OF THE MESSAGE MOVES, and only then.
	// Everything ahead of the record is what a deliberate act rewrites; when one
	// of those has moved, this message is being re-priced whatever the record
	// says, so it is the moment to take the record as it now stands. When
	// nothing ahead of it moved — a turn opening, a memory set routed — the
	// record it already carries goes back out byte for byte.
	if !a.recordRead {
		// THE SESSION'S OWN RECORD IS READ ONCE, on the first rebuild — which is
		// the conversation opening, before anything is asked. Every later reading
		// is written by the one thing that changes the file, off this lock
		// ([Agent.takeRecord]); this session's engine is the only writer of it,
		// because every window's answer crosses the wire to this process
		// (question.go's [Agent.ResolveQuestion] is the one door).
		a.recordRead = true
		a.recordText = DecisionsSection(a.Decisions())
	}
	head := a.system + a.placesText + a.standingText
	if head != a.systemHead {
		a.systemHead = head
		a.recordShown = a.recordText
	}
	record := a.recordShown
	if record != "" {
		record = "\n\n" + record + "\n"
	}
	a.messages[0] = textMessage("system", head+record)
}

// takeRecord re-renders the decision section from the file, off every lock this
// package holds while a person is waiting.
//
// IT IS CALLED BY THE ONE THING THAT CHANGES THE FILE (question.go's
// [Agent.recordDecision]) and by the session opening, which are the two moments
// the record can have moved. The read and the render happen with a.mu released
// because the file grows with the conversation and the lock is the one an
// interrupt has to be able to take; what is held under the lock is the finished
// string.
func (a *Agent) takeRecord() {
	text := DecisionsSection(a.Decisions())
	a.mu.Lock()
	a.recordText = text
	a.mu.Unlock()
}

// refreshCardLocked holds the state card's new text for the note that carries
// it. It is the card's own door, called by the post-turn pass once a delta has
// actually changed something — and it does NOT touch message[0] any more, for
// the reason [Agent.refreshSystemLocked] states: the card moves with the work,
// and what moves with the work rides at the tail.
func (a *Agent) refreshCardLocked(text string) {
	a.cardText = text
}

// mergeStateCard folds one exchange's delta into the card and, when something
// actually moved, puts the new block in front of the model.
//
// It runs on the post-turn goroutine, which is why the two locks are taken in
// this order and never together: the card's own lock covers the merge and the
// file write, and a.mu is taken afterwards for the two assignments alone. The
// card is readable from a compaction pass, from a resume and from here, and a
// pass holding a.mu across a file write is the deadlock this package refuses.
func (a *Agent) mergeStateCard(delta reflex.StateDelta) {
	if !a.card().merge(delta) {
		return
	}
	text := a.card().text()
	a.mu.Lock()
	a.refreshCardLocked(text)
	a.mu.Unlock()
}

// waitForMemory gives every background memory pass a bounded moment to land,
// and then cuts whatever is left.
//
// IT WAITS BEFORE IT CANCELS, which is the opposite order to the turn's own
// shutdown and is deliberate. A turn that is cancelled has a person watching who
// asked to leave; a memory pass has nobody waiting on it and is two seconds from
// keeping something the person said. Losing that to save two seconds on a quit
// is the wrong trade — and the grace is the same closeGrace the journal gets, so
// a wedged pass still cannot hold the process.
func (a *Agent) waitForMemory(stop context.CancelFunc) {
	settled := make(chan struct{})
	go func() {
		a.memoryJobs.Wait()
		close(settled)
	}()
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	select {
	case <-settled:
	case <-timer.C:
	}
	stop()
}
