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
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
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
	return a.memory.reflex.Bind(billedCompleter{agent: a, inner: a.client}, named, fallback, a.sayMemory)
}

// billedCompleter is what makes a reflex call cost something a person can see.
//
// THE PERSON PAYS FOR IT, SO IT CANNOT BE FREE — the argument
// [Agent.addAuxiliaryUsage] states for the title and the compaction summary,
// and it bites harder here: this is the only auxiliary call made twice EVERY
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
		if reflex.EmptyAtCeiling(response, request.MaxTokens) {
			b.agent.addEmptyReflexUsage(response, request.Model)
		} else {
			b.agent.addAuxiliaryUsageAs(response, request.Model, 1, string(roles.RoleReflex))
		}
	}
	return response, err
}

// ── the pre-turn block ──────────────────────────────────────────────────────

// memoryBlock is THE ONE SEAM every caller uses: the conversation before a
// turn, and a task node when its context is assembled (task_run.go). cue is
// what the block is chosen against — the message just typed, or the brief the
// node is about to work from.
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

// refreshMemory is the conversation's own call: route this turn's message, hand
// any memory COMMAND in it to the store, and put the resulting block in front of
// the model before the first request goes out.
//
// It runs inside the turn goroutine and NOT under a.mu, because it makes a
// provider call. The lock is taken once at the end, for the two assignments.
func (a *Agent) refreshMemory(ctx context.Context, hub *eventHub, cue string) {
	if !a.remembers() {
		return
	}
	// The legacy file, once, before anything is routed — so a person whose
	// standing preferences lived in memory.md is answered out of them on the
	// very first turn after the upgrade rather than the second.
	a.importMemoryFile(hub)
	// AND WHATEVER THE LAST POST-TURN PASS HAD NOWHERE TO SAY. It writes after
	// the turn is sealed and its hub closed, so a supersession settled there has
	// no stream; this is the first one it gets.
	for _, line := range a.memory.takeNotices() {
		memoryNotice(hub, line)
	}

	block := a.routedMemory(ctx, cue, hub, true)

	a.mu.Lock()
	a.memoryText = block
	a.refreshSystemLocked()
	a.mu.Unlock()
}

// routedMemory is the whole pre-turn pass. record says whether the ids it
// injected are this session's to count.
func (a *Agent) routedMemory(ctx context.Context, cue string, hub *eventHub, record bool) string {
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
		a.runMemoryCommand(hub, *routed.Cmd)
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
func (a *Agent) runMemoryCommand(hub *eventHub, cmd reflex.Cmd) {
	if strings.TrimSpace(cmd.Arg) == "" {
		return
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
		memoryNotice(hub, "remembered · "+memory.Title)
	case "forget":
		title, err := a.forgetMatching(cmd.Arg)
		if err != nil {
			return
		}
		if title == "" {
			memoryNotice(hub, "nothing matched · "+cmd.Arg)
			return
		}
		memoryNotice(hub, "forgot · "+title)
	}
}

// memoryNotice is the one line any of this ever says out loud.
func memoryNotice(hub *eventHub, text string) {
	if hub == nil {
		return
	}
	hub.send(Event{Kind: EventNotice, Text: text})
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
// THE HELD LINE IS NOT A COMPROMISE, it is where this pass lives. `remember`
// and `forget` are settled by the router BEFORE the turn, so they have a hub.
// The post-turn pass runs once the turn is sealed and its hub closed, on the
// session's own lifetime — that is the whole reason nothing is waiting on it —
// so a line written there has no stream in the room, and holding it until the
// next refresh is what keeps it from being lost instead.
func (a *Agent) sayMemory(text string) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub != nil {
		memoryNotice(hub, text)
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
func (a *Agent) importMemoryFile(hub *eventHub) {
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
		memoryNotice(hub, fmt.Sprintf("imported %d memories from memory.md", imported))
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
			if err := json.Unmarshal(args, &parsed); err != nil {
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

// refreshSystemLocked rebuilds message[0] from the base prompt, the person's
// standing orders and the memory block this turn was routed. It is called at
// construction — where the block is empty, or is the one a task node opened with
// — and again once the router has answered, at the start of every turn.
//
// message[0] is REPLACED rather than appended to: a.system stays the base, so
// every refresh renders base + current blocks instead of stacking one turn's
// memories on top of the last one's.
//
// WHAT LIVES HERE IS WHAT HOLDS FOR THE LIFE OF THE CONVERSATION, and that is
// the whole rule. message[0] sits in front of every message there is, so one
// changed byte in it re-prices the entire transcript at the uncached rate — five
// times the cached one — on the very next request. The base prompt never moves.
// An order was agreed on a card and holds until the person says otherwise, and a
// conversation may run all day without one moving (standing_world.go). The
// memory block is the one thing in here that is not free, and it was measured
// rather than assumed: it is routed per turn, so it moves when the SUBJECT
// moves, and [renderMemoryBlock] stamps each line with an age label whose
// granularity is hourly for a memory learned today and daily after that
// (store.AgeLabel) — so a set that did not change re-renders byte for byte for
// a session's whole length unless it is carrying something learned this
// morning. It stays because it is REPLACED and never stacked: a turn's memories
// are that turn's, superseded lines are the one thing the memory store works to
// keep out of a prompt, and a tail note that appended each turn's set would put
// them all back. What it costs when it does move is the same cold prefix the
// clock costs when it is brought forward (prompt.go's clockRefresh), and for the
// same reason: a model reasoning from a stale standing fact is worse than a
// re-priced conversation.
//
// THE TWO BLOCKS THAT MOVE WITH THE WORK ARE NOT HERE. The state card is
// rewritten by the post-turn pass every time a delta lands, and the other
// windows' work is re-read at the start of every turn; both used to ride at the
// end of this string, and between them they re-priced the whole conversation on
// most turns of a working session. They ride at the TAIL of the transcript now,
// as one appended note ([Agent.landVolatileLocked]), where a change costs the
// note and nothing behind it.
func (a *Agent) refreshSystemLocked() {
	if len(a.messages) == 0 {
		return
	}
	a.messages[0] = textMessage("system", a.system+a.standingText+a.memoryText)
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
