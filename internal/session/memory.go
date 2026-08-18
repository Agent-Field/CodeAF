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
//   - BEFORE THE TURN, the router reads the message just typed against an index
//     of TITLES — never bodies — and answers which two or three remembered
//     lines bear on it. Only those are rendered into the <memory> block. A
//     hundred remembered things cost one small call and three lines of prompt,
//     where the file cost all hundred.
//
//   - AFTER THE TURN, off the person's path entirely, the extractor reads the
//     exchange and answers whether it held anything worth carrying into another
//     session. Almost always it did not, which is the answer the gate is shaped
//     around.
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
	// memoryIndexLimit is how much of the index the router is shown. It is the
	// store's own read limit and internal/reflex's own prompt limit, spelled
	// here because this is the caller that has to pick a number: two hundred
	// titles is already a large prompt for a near-free model.
	memoryIndexLimit = 200

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

	mu sync.Mutex
	// injected is what the router asked for on the LAST routed turn, kept only
	// so the post-turn pass can count those retrievals ([store.Store.BumpMemoryUse]).
	// It is replaced per turn rather than accumulated: use telemetry about a
	// turn belongs to that turn.
	injected []string
	// imported records that the legacy memory.md has already been looked at.
	// The rename on disk is the durable answer; this is what keeps a session
	// from stat-ing the same absent file every turn.
	imported bool
}

func newMemoryBrain(s *store.Store) *memoryBrain { return &memoryBrain{store: s} }

// remembers reports whether this session has a brain at all. Every entry point
// in this file asks it first, and the answer is a fact about the wiring rather
// than about a setting: the door opens no store when memory is off.
func (a *Agent) remembers() bool { return a.memory != nil && a.memory.store != nil }

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
	return reflex.Bind(billedCompleter{agent: a, inner: a.client}, named)
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
		b.agent.addAuxiliaryUsage(response)
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
	index, err := a.memory.store.MemoryIndex(memoryIndexLimit)
	if err != nil || len(index) == 0 {
		// AN EMPTY INDEX IS NOT A CALL. There is nothing to route against, and a
		// reflex that billed for that would bill for every turn of every fresh
		// install.
		return ""
	}
	client := a.reflexClient()
	if client == nil {
		return ""
	}
	stubs := make([]reflex.Stub, 0, len(index))
	for _, row := range index {
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
	block, kept := renderMemoryBlock(memories)
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
			Type:  memoryTypeOf(cmd.Arg),
			Scope: store.MemoryScopeUser,
			Title: memoryTitleFrom(cmd.Arg),
			Text:  cmd.Arg,
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

// renderMemoryBlock writes the block and reports which ids actually made it in.
//
// The order is the ROUTER'S — the store returns what it was asked for in the
// order it was asked (GetMemories states that contract), and the router is the
// only thing in the system that saw the actual question. So when the budget runs
// out it is the TAIL that goes: the last line the router named is the one it
// thought about least.
func renderMemoryBlock(memories []store.Memory) (string, []string) {
	var (
		body strings.Builder
		kept []string
		used int
	)
	for _, memory := range memories {
		line := "- " + memory.Title + ": " + memory.Text
		if memory.Title == "" {
			line = "- " + memory.Text
		}
		length := utf8.RuneCountInString(line) + 1
		if used+length > memoryBlockRunes {
			break
		}
		used += length
		body.WriteString(line)
		body.WriteString("\n")
		kept = append(kept, memory.ID)
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
func (m *memoryBrain) setInjected(ids []string) {
	m.mu.Lock()
	m.injected = ids
	m.mu.Unlock()
}

// takeInjected reads the ids and clears them, so no turn can count another
// turn's retrievals.
func (m *memoryBrain) takeInjected() []string {
	m.mu.Lock()
	ids := m.injected
	m.injected = nil
	m.mu.Unlock()
	return ids
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
	ids := a.memory.takeInjected()
	if !a.startMemoryJob() {
		return
	}
	go func() {
		defer a.memoryJobs.Done()
		ctx := a.memoryCtx
		// The retrieval count first, because it is a fact that is already true:
		// those memories were handed to a model whatever the extractor says next.
		if len(ids) > 0 {
			_ = a.memory.store.BumpMemoryUse(ids)
		}
		client := a.reflexClient()
		if client == nil {
			return
		}
		found, err := reflex.Extract(ctx, client, userMsg, assistantMsg)
		if err != nil {
			return
		}
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
		Type:  candidate.Type,
		Scope: candidate.Scope,
		Title: strings.TrimSpace(candidate.Title),
		Text:  candidate.Text,
		Tags:  candidate.Tags,
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
		if err := a.memory.store.UpdateMemory(decided.TargetID, fresh.Title, fresh.Text, fresh.Tags); err != nil {
			return store.Memory{}, err
		}
		return store.Memory{ID: decided.TargetID, Title: fresh.Title, Text: fresh.Text}, nil
	case "supersede":
		if decided.TargetID == "" {
			return store.Memory{}, nil
		}
		return a.memory.store.SupersedeMemory(decided.TargetID, fresh)
	}
	return store.Memory{}, nil
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
	if err := a.memory.store.ForgetMemory(found[0].ID); err != nil {
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
			Type:  store.MemoryFact,
			Scope: store.MemoryScopeUser,
			Title: memoryTitleFrom(line),
			Text:  line,
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

// refreshSystemLocked rebuilds message[0] from the base prompt, the memory block
// this turn was routed, and the state card. It is called at construction — where
// the block is empty, or is the one a task node opened with — and again once the
// router has answered, at the start of every turn.
//
// message[0] is REPLACED rather than appended to: a.system stays the base, so
// every refresh renders base + current blocks instead of stacking one turn's
// memories on top of the last one's.
//
// THE CARD RIDES AFTER THE MEMORY BLOCK, and the order is the argument for it.
// Memory is what is true across conversations; the card is what is true in this
// one. A model reading downward meets the standing facts first and the live
// situation last, which is the order it needs them in — and it is also the order
// that keeps the prompt prefix stable for the cache, because the card is the
// half that moves.
func (a *Agent) refreshSystemLocked() {
	if len(a.messages) == 0 {
		return
	}
	a.messages[0] = textMessage("system", a.system+a.memoryText+a.cardText)
}

// refreshCardLocked re-renders the state card into message[0]. It is the card's
// own door onto [Agent.refreshSystemLocked], called by the post-turn pass once a
// delta has actually changed something.
func (a *Agent) refreshCardLocked(text string) {
	a.cardText = text
	a.refreshSystemLocked()
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
