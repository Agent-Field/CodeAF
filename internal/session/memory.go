package session

// Memory is what one conversation knows and the next one would otherwise have
// to be told again: a preference, a standing rule, a correction, a decision, a
// lasting fact about the person, their machine or their project.
//
//   - STORAGE IS NOT THE PROMPT BUDGET. Writes remain available after the
//     working set fills. Local activation selects relevant lines, and explicit
//     person pins always ride. The footer names how many other lines remain
//     searchable, so admission never hides the existence of saved knowledge.
//
//   - RETRIEVAL IS LOCAL. Project, current brief and message cues assemble the
//     block without a model request. Surfacing is recorded once per session;
//     the existing post-turn call identifies actual use and durable captures.
//
//   - THERE IS ONE WRITE DOOR ([Agent.keep]) and three ways to it: the person
//     (/remember, /always, the Memories tab), the model's own `remember`, and
//     the pass after a turn noticing something durable the person said. None of
//     them asks a model whether to save: the duplicate check is the store's, and
//     a line the noticing pass says replaces another replaces it.
//
//   - NOTHING CHANGES SILENTLY. Every line kept, replaced or forgotten by the
//     model or by the noticing pass says one line in the conversation, naming
//     where to take it back: the Memories tab lists every line with who kept
//     it, forgets any of them, and puts back any line that was let go. There
//     is no forgetting by slash command — the tab is the one place a person
//     does it by hand, and the model's `forget` is the one place it is asked
//     for in words.
//
//   - THE SWITCH GOVERNS NOTICING, NOT MEMORY. The person's `memory.enabled`
//     row turns the noticing pass off; saved lines remain available and
//     /remember goes on working. It is read live, at the end of every
//     turn, so a long-lived engine follows a change without a restart.
//
// A READ-ONLY POSTURE (an automation's run, [Config.bindingOnlyMemory]) reads
// the block and does nothing else: no noticing, no verbs, no lines said.
//
// A REFLEX FAILURE IS INVISIBLE TO THE TURN. The noticing pass runs after the
// turn is sealed; a provider that does not answer leaves the turn exactly what
// it was. A transient failure gets one delayed retry while the session remains
// open; only an exhausted failure is journaled and said once per window. The
// exchange is not queued across application restarts.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/lane"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/redact"
	"github.com/Agent-Field/codeaf/internal/reflex"
	"github.com/Agent-Field/codeaf/internal/roles"
	"github.com/Agent-Field/codeaf/internal/store"
)

const (
	// memoryFailureWindow is how long one "couldn't update memory" line buys.
	memoryFailureWindow = 5 * time.Minute
)

// memoryFull is a refusal for want of room as a person reads it: its words
// are [MemoryFullWords] said from this conversation, and it still answers
// errors.Is for [store.ErrMemoryFull].
type memoryFull struct {
	err   error
	words string
}

func (f memoryFull) Error() string { return f.words }
func (f memoryFull) Unwrap() error { return f.err }

// memoryRoomWords is where every full-memory receipt sends the person.
const memoryRoomWords = " · forget a line on the Memories tab to make room"

// MemoryFullWords is the receipt for a change the store refused for want of
// room ([store.ErrMemoryFull]), said from a conversation whose project lines
// are kept under here ("" when it has none), named hereName.
func MemoryFullWords(err error, here, hereName string) string {
	text := err.Error()
	if at := strings.Index(text, store.ErrMemoryFull.Error()); at >= 0 && strings.Contains(text, memoryRoomWords) {
		// ALREADY SAID: a conversation's own receipt, come back over the wire.
		return text[at:]
	}
	return "memory is full " + memoryFullWhere(err, here, hereName) + memoryRoomWords
}

// memoryFullWhere is which conversations a refused change would overflow:
// "here" when it is this conversation's block, "for <name>" when the store
// named another project, and "in another project" when it could not.
//
// OVER --host THE REFUSAL ARRIVES AS TEXT, which carries the project's name and
// not its owner ([store.MemoryFullError.Error]), so the name is compared
// instead. A refusal naming no project is read as this conversation's: it is
// the person's and this machine's lines alone, unless the project it would
// overflow was kept before lines carried a project's name.
func memoryFullWhere(err error, here, hereName string) string {
	var full *store.MemoryFullError
	if errors.As(err, &full) {
		switch {
		case full.Owner == "" || full.Owner == here:
			return "here"
		case full.Project != "":
			return "for " + full.Project
		}
		return "in another project"
	}
	_, name, named := strings.Cut(err.Error(), store.ErrMemoryFull.Error()+" for ")
	if !named || name == "" || name == hereName {
		return "here"
	}
	return "for " + name
}

// memoryFullError is the store's refusal for want of room as this
// conversation says it ([MemoryFullWords]).
func (a *Agent) memoryFullError(err error) memoryFull {
	return memoryFull{err: err, words: MemoryFullWords(err, a.memoryHere(), a.memoryHereName())}
}

// memoryHere is the owner this conversation keeps project lines under, or ""
// when it has no project of its own; memoryHereName is that project's name.
func (a *Agent) memoryHere() string {
	owner, widened := a.ownerFor(store.MemoryScopeProject)
	if widened {
		return ""
	}
	return owner
}

func (a *Agent) memoryHereName() string {
	_, name, _ := a.projectKey()
	return name
}

// memoryTakeBack is the clause every receipt for a change made on the person's
// behalf ends with: where they go to see it and take it back. It names the tab
// and not its key, because this is the engine speaking and the key is the
// surface's to spell.
const memoryTakeBack = "see or forget it on the Memories tab"

// memoryBrain is the store this conversation remembers into, and the little
// bookkeeping that goes with it. It holds a lock of its own, outside a.mu,
// because its writer is a goroutine that outlives the turn that started it.
type memoryBrain struct {
	store *store.Store
	// reflex is the failover memory for this conversation's noticing calls.
	reflex *reflex.Session

	mu sync.Mutex
	// said holds the lines this conversation owes the person and had no
	// stream to say them on: the noticing pass writes after the turn's hub has
	// closed, so its line is held for the next turn.
	said []string
	// imported records that the legacy memory.md has been looked at.
	imported bool
	// overflowSaid records that this conversation has said its block holds
	// more than fits ([Agent.refreshMemoryLocked]); it is said once.
	overflowSaid bool
	// upgradeChecked records that this conversation has asked whether the
	// machine still owes the person the memory-off notice
	// ([Agent.noteMemoryOffUpgradeLocked]).
	upgradeChecked bool
	// typed is whether the person typed the turn now running. The model's
	// `remember` and `forget` act only on such a turn ([Agent.memoryTools]).
	typed atomic.Bool
	// failureSaid is when the last failure line went out.
	failureSaid time.Time
	// surfaced prevents a cache refresh from rehearsing the same line repeatedly.
	surfaced         map[string]bool
	selected         []store.Memory
	selection        *store.MemoryBlock
	selectionContext store.MemoryContext
	contextWords     string
	mutationSeq      int64
	dwellContext     store.MemoryContext
}

func newMemoryBrain(s *store.Store) *memoryBrain {
	return &memoryBrain{store: s, reflex: &reflex.Session{}}
}

// remembers reports whether this conversation has a memory store at all.
func (a *Agent) remembers() bool {
	return a.memory != nil && a.memory.store != nil
}

// bindingBrain is the store a read of the block may use: the read-only lent
// store for a task worker, and otherwise the conversation's own.
func (c Config) bindingBrain() *store.Store {
	if c.bindingStore != nil {
		return c.bindingStore
	}
	return c.Memory
}

// memoryWritable is remembers() AND not a read-only posture.
func (a *Agent) memoryWritable() bool {
	return a.remembers() && !a.config.bindingOnlyMemory
}

// noticing reports whether the pass after a turn may keep something on its
// own: a writable conversation whose person has not turned it off. The row is
// read HERE, every time, rather than once at the door, so an engine that has
// been running for a day follows the person's change on the very next turn.
func (a *Agent) noticing() bool {
	return a.memoryWritable() && config.MemoryEnabledAt(a.config.ProfileDir)
}

// memorySourceSession is the journal header id attached to a memory write.
func (a *Agent) memorySourceSession() string {
	if id := a.journalID(); id != "" {
		return id
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sessionID()
}

// ── who the memory belongs to ───────────────────────────────────────────────

// projectKey is this conversation's project key and name, read under the lock
// because anchoring a workspace moves them.
func (a *Agent) projectKey() (key, name string, owned bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return strings.TrimSpace(a.config.MemoryProjectKey), strings.TrimSpace(a.config.MemoryProjectName), a.config.Place.Owned
}

// memoryOwners is what this conversation may SEE: the person, this machine,
// and this project.
func (a *Agent) memoryOwners() []string {
	key, _, _ := a.projectKey()
	return MemoryOwners(key)
}

// MemoryOwners is the owner list a conversation at one project may SEE: the
// person, this machine, and the project when its key is known. It is the one
// builder of that list, exported for the doors outside this package that read
// the block for a place with no conversation open on it (`codeaf do`).
func MemoryOwners(projectKey string) []string {
	owners := []string{store.OwnerUser, store.OwnerMachine}
	if key := strings.TrimSpace(projectKey); key != "" {
		owners = append(owners, store.OwnerProject(key))
	}
	return owners
}

// ownerFor answers the owner a line kept under a scope word lands in, and
// whether a project line had to be widened to the person.
//
// A CONVERSATION WITH NO PROJECT OF ITS OWN KEEPS ITS LINES FOR THE PERSON.
// Home, a folder nobody could prove the identity of, or a scratch folder a
// conversation owns would otherwise make a "project" line nobody else ever
// reads — so the line holds everywhere, and the receipt says so rather than
// letting the reach be a surprise.
func (a *Agent) ownerFor(scope string) (owner string, widened bool) {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case store.MemoryScopeEnv, "machine":
		return store.OwnerMachine, false
	case store.MemoryScopeUser, "you", "everywhere":
		return store.OwnerUser, false
	}
	if key, _, owned := a.projectKey(); key != "" && !owned {
		return store.OwnerProject(key), false
	}
	return store.OwnerUser, true
}

// memoryReach is where a line holds, in the words every receipt uses.
func memoryReach(owner string, widened bool) string {
	switch {
	case widened:
		return "for you everywhere, since this conversation has no project of its own"
	case owner == store.OwnerMachine:
		return "for this machine"
	case owner == store.OwnerUser:
		return "for you everywhere"
	}
	return "for this project"
}

// ── the block ───────────────────────────────────────────────────────────────

// memoryHeading and memoryPreamble open every block, in a conversation and in a
// brief alike: ONE spelling, because a heading written in two places will one
// day be written two ways.
const (
	memoryHeading  = "Memory"
	memoryPreamble = "Respect saved conditions: must/only/never bind; preferences are optional. Current user instructions win; current authoritative evidence corrects stale facts. Narrow exceptions don't revoke broader rules. Other lines: search_memories. Changes are shown."
	// memoryReport is the closing line a task's or a run's section carries and
	// a conversation's does not: a node works with nobody to ask, so a line it
	// cannot honour has exactly one place to be said.
	memoryReport = "If you cannot honour one of these, say so in your report."
)

// renderMemory is the section itself: the heading, the preamble, the lines
// grouped by where they hold, how many did not fit, and the caller's closing.
//
// THE EMPTINESS LAW: no lines is NO SECTION — not an empty heading.
//
// The compatibility renderer uses the store budget. Live prompt composition
// uses renderMemorySelection after activation admission instead.
func renderMemory(memories []store.Memory, closing string) string {
	fit, omitted := store.FitMemories(memories)
	return renderMemorySelection(fit, omitted, closing)
}

// renderMemorySelection renders the admitted set without imposing a second budget on pins.
func renderMemorySelection(fit []store.Memory, omitted int, closing string) string {
	var lines strings.Builder
	shown, group := 0, ""
	for _, memory := range fit {
		if heading := memoryGroupHeading(memory); heading != group {
			group = heading
			lines.WriteString(heading + ":\n")
		}
		shown++
		lines.WriteString("- " + store.MemoryQuote(memory.Text) + "\n")
	}
	if shown == 0 && omitted == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(memoryHeading + ":\n\n" + memoryPreamble + "\n\n")
	out.WriteString(lines.String())
	if omitted > 0 {
		out.WriteString("· " + strconv.Itoa(omitted) + " more in memory\n")
	}
	if closing != "" {
		out.WriteString("\n" + closing + "\n")
	}
	return out.String()
}

// memoryGroupHeading is the sub-heading a line's owner renders under.
func memoryGroupHeading(memory store.Memory) string {
	switch store.OwnerKindOf(memory.Owner) {
	case store.OwnerUser:
		return "For the person, everywhere"
	case store.OwnerMachine:
		return "For this machine"
	}
	if name := strings.TrimSpace(memory.ProjectName); name != "" {
		return "For this project (" + name + ")"
	}
	return "For this project"
}

// MemoryWorld renders the memory section a starting worker's brief closes on,
// or "" when nothing is kept or there is no store. It is the exported door for
// the callers outside this package seated where a node is — a run handing its
// workers a brief, `codeaf do` handing its own.
//
// A READ THAT FAILS IS NO SECTION: a worker is not where a person learns their
// disk is gone.
func MemoryWorld(st *store.Store, owners []string) string {
	if st == nil || len(owners) == 0 {
		return ""
	}
	ctx := store.MemoryContext{Owners: owners}
	for _, owner := range owners {
		if store.OwnerKindOf(owner) != store.OwnerUser && store.OwnerKindOf(owner) != store.OwnerMachine {
			ctx.Project = owner
		}
	}
	return memoryWorldForContext(st, ctx)
}

func memoryWorldForContext(st *store.Store, ctx store.MemoryContext) string {
	if st == nil || len(ctx.Owners) == 0 {
		return ""
	}
	block, err := st.AssembleMemoryBlock(ctx)
	if err != nil {
		return ""
	}
	return renderMemorySelection(block.Memories, block.Omitted, memoryReport)
}

// memoryWorld is the conversation answering for a piece of its own work, from
// the brain a read may use, for the owners this conversation sees — so a
// conversation and the work it hands out cannot disagree about what is kept.
func (a *Agent) memoryWorld() string {
	return a.memoryWorldFor("")
}

// memoryWorldFor admits memories using the worker's actual assignment. Read
// conversation metadata under its lock, then do local retrieval off that lock.
func (a *Agent) memoryWorldFor(brief string) string {
	st, ctx := a.snapshotMemoryContext(brief)
	return memoryWorldForContext(st, ctx)
}

func (a *Agent) snapshotMemoryContext(brief string) (*store.Store, store.MemoryContext) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ctx := store.MemoryContext{Owners: MemoryOwners(a.config.MemoryProjectKey), Project: a.config.MemoryProjectKey, Session: a.sessionID(), Brief: brief + " " + a.cardText}
	return a.config.bindingBrain(), ctx
}

// memoryWorld is retained for callers asking for the conversation's context.
func (g *TaskGraph) memoryWorld() string {
	if g.home == nil {
		return ""
	}
	return g.home.memoryWorld()
}

// memoryWorldsForFrontier snapshots queued assignments under the graph lock,
// then resolves their distinct memory contexts outside both graph and agent
// locks. A rule for one task must not disappear behind another task's topic.
func (g *TaskGraph) memoryWorldsForFrontier() map[uint64]string {
	if g.home == nil {
		return nil
	}
	briefs := g.snapshotQueuedMemoryBriefs()
	worlds := make(map[uint64]string, len(briefs))
	for id, brief := range briefs {
		worlds[id] = g.home.memoryWorldFor(brief)
	}
	return worlds
}

// queuedWithMemoryContext leaves a node admitted after the snapshot for its
// own admission's frontier pass; it must not start with an unrelated fallback.
func queuedWithMemoryContext(node *TaskNode, worlds map[uint64]string) bool {
	if node == nil || node.state != TaskQueued {
		return false
	}
	_, ready := worlds[node.id]
	return worlds == nil || ready
}

// snapshotQueuedMemoryBriefs never takes the agent lock or touches storage.
func (g *TaskGraph) snapshotQueuedMemoryBriefs() map[uint64]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	briefs := map[uint64]string{}
	for _, id := range g.order {
		node := g.nodes[id]
		if node != nil && node.state == TaskQueued {
			briefs[id] = g.inheritedLocked(node)
		}
	}
	return briefs
}

// refreshMemoryLocked puts what this conversation keeps in front of the model.
// It is called with a.mu held at the start of every turn, beside the clock's
// refresh: a line kept a moment ago, in this window or another, holds over the
// very next turn.
//
// IT IS NOT GATED ON THE PROMPT PROFILE: a person's lines hold over a small
// model exactly as over a large one, and reading them is one indexed read.
//
// A READ THAT FAILS KEEPS THE LAST BLOCK. The lines did not stop holding
// because one read did not answer, and a block that vanished for a turn would
// re-price the prefix twice to say nothing true.
func (a *Agent) refreshMemoryLocked() {
	st := a.config.Memory
	if st == nil {
		a.memoryText = ""
		return
	}
	ctx := store.MemoryContext{Owners: MemoryOwners(a.config.MemoryProjectKey), Project: a.config.MemoryProjectKey, Session: a.sessionID(), Brief: a.cardText, Tasks: memoryInflightTasks(a.cardText)}
	if a.memory != nil {
		ctx.Keywords = strings.Fields(a.memory.contextWords)
	}
	revision, err := st.MemoryMutationSeq(ctx.Owners)
	if err != nil {
		return
	}
	if a.reuseMemoryDwell(&ctx, revision) {
		return
	}
	block, err := st.AssembleMemoryBlock(ctx)
	if err != nil {
		return
	}
	store.LogMemoryAssembly(block, "conversation")
	section := renderMemorySelection(block.Memories, block.Omitted, "")
	a.memoryText = ""
	if section != "" {
		a.memoryText = "\n<memory>\n" + section + "</memory>\n"
	}
	a.noteMemoryOffUpgradeLocked()
	if a.memory != nil && !a.config.bindingOnlyMemory {
		a.memory.mu.Lock()
		a.memory.selected = append([]store.Memory(nil), block.Memories...)
		a.memory.selection = &block
		a.memory.selectionContext = ctx
		a.memory.mutationSeq = revision
		if len(ctx.DwellIDs) == 0 {
			a.memory.dwellContext = ctx
		}
		if a.memory.surfaced == nil {
			a.memory.surfaced = map[string]bool{}
		}
		for _, memory := range block.Memories {
			if !a.memory.surfaced[memory.ID] {
				if err := st.RecordMemoryEvent(memory.ID, "surfaced", ctx.Session); err == nil {
					a.memory.surfaced[memory.ID] = true
				}
			}
		}
		a.memory.mu.Unlock()
	}
}

// memoryUpgradeMarker is the file that says this machine has had its first
// start after memory stopped turning off. It is per machine, beside the store,
// because the notice it guards is about what the store's lines now do.
func memoryUpgradeMarker() string { return home.Join("v3", "memory-upgrade-noticed") }

// noteMemoryOffUpgradeLocked is the owner's ruling for a person who had memory
// OFF before this build: off used to keep their saved lines out of every
// prompt, and now it governs only the noticing, so the lines ride again. That
// is said ONCE PER MACHINE — on the first start after the upgrade, in the
// first conversation that turn starts in — and nothing about the lines changes.
//
// THE MARKER IS CREATED FIRST, EXCLUSIVELY, WHATEVER THE SETTING SAYS. The
// first start after the upgrade is the one moment "had memory off before" is
// true of the row: a person who turns noticing off next month never had memory
// off in the old sense and is owed nothing. Creating it exclusively means two
// windows starting at once say it once between them. An ask-here errand does
// not take the one chance: it is usually a single exchange in a pane that goes.
func (a *Agent) noteMemoryOffUpgradeLocked() {
	if a.memory == nil || a.config.bindingOnlyMemory || a.config.Errand || a.memory.upgradeChecked {
		return
	}
	a.memory.upgradeChecked = true
	marker := memoryUpgradeMarker()
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = file.Close()
	if config.MemoryEnabledAt(a.config.ProfileDir) {
		return
	}
	count, err := a.memory.store.ActiveMemoryCount()
	if err != nil || count == 0 {
		return
	}
	a.memory.mu.Lock()
	a.memory.said = append(a.memory.said, memoryOffUpgradeWords(count))
	a.memory.mu.Unlock()
}

// memoryOffUpgradeWords is that one line.
func memoryOffUpgradeWords(count int) string {
	lines := strconv.Itoa(count) + " kept memory lines now ride every conversation they hold in"
	if count == 1 {
		lines = "1 kept memory line now rides every conversation it holds in"
	}
	return lines + " — memory off now only stops noticing · see or forget them on the Memories tab"
}

// memoryOverflowWords is that one line.
func memoryOverflowWords(left int) string {
	lines := strconv.Itoa(left) + " remembered lines don't"
	if left == 1 {
		lines = "1 remembered line doesn't"
	}
	return lines + " ride this conversation · find them on the Memories tab or with search_memories"
}

// memoryBlock is the <memory> block message[0] carries, or "" when nothing is
// kept.
func memoryBlock(memories []store.Memory) string {
	section := renderMemory(memories, "")
	if section == "" {
		return ""
	}
	return "\n<memory>\n" + section + "</memory>\n"
}

// ── the one write door ──────────────────────────────────────────────────────

// keep writes one line through the store's write door. It is EVERY mouth: the person's /remember, the model's `remember`,
// the noticing pass and the legacy import. It asks no model anything.
//
// replaced, when set, is a line this one supersedes — the noticing pass saying
// "this corrects line 3". It must be a line this conversation can see; the
// store refuses anything else.
//
// The answer is the line as kept, where it holds in receipt words, and whether
// it was new.
func (a *Agent) keep(text, scope, origin string, replaced *store.Memory) (store.Memory, string, bool, error) {
	return a.keepDurable(text, scope, origin, replaced, false, "")
}

// keepLine retains the legacy import signature. Storage is unbounded; only
// admission to a model request applies the prompt budget.
func (a *Agent) keepLine(text, scope, origin string, replaced *store.Memory, unbounded bool) (store.Memory, string, bool, error) {
	return a.keepDurable(text, scope, origin, replaced, unbounded, "")
}

func (a *Agent) keepDurable(text, scope, origin string, replaced *store.Memory, unbounded bool, durability string) (store.Memory, string, bool, error) {
	if !a.memoryWritable() {
		return store.Memory{}, "", false, errors.New("this conversation keeps no memory")
	}
	if durability == "ephemeral" {
		return store.Memory{}, "", false, nil
	}
	// EVERY DOOR REDACTS. A person pasting a token beside what they want kept
	// must not have it stored and rendered in front of every later turn.
	text = redact.Secrets(strings.Join(strings.Fields(text), " "))
	if text == "" {
		return store.Memory{}, "", false, errors.New("there is nothing to keep")
	}
	owner, widened := a.ownerFor(scope)
	if replaced != nil && replaced.Owner != owner {
		// A REPLACEMENT HOLDS WHERE THE LINE IT REPLACES HELD. A noticing pass
		// that said "this corrects line 3" and named a different scope word has
		// still named line 3.
		owner, widened = replaced.Owner, false
	}
	_, projectName, _ := a.projectKey()
	reach := memoryReach(owner, widened)
	if replaced != nil {
		fresh, added, err := a.memory.store.SupersedeMemoryForOwners(a.memoryOwners(), replaced.ID, store.Memory{
			Owner: owner, Type: memoryTypeForDurability(text, durability), Text: text,
			Origin: origin, ProjectName: projectName, SourceSession: a.memorySourceSession(),
		})
		if err != nil {
			return store.Memory{}, "", false, err
		}
		return fresh, reach, added, nil
	}
	result, err := a.memory.store.Write(store.WriteRequest{
		Owner: owner, Type: memoryTypeForDurability(text, durability), Text: text,
		Origin: origin, ProjectName: projectName, SourceSession: a.memorySourceSession(),
		Unbounded: unbounded, Durability: durability,
	})
	if err != nil {
		return store.Memory{}, "", false, err
	}
	if result.Outcome == store.WriteOutcomeSkipped {
		return result.Memory, reach, false, nil
	}
	return result.Memory, reach, true, nil
}

// ── forgetting ──────────────────────────────────────────────────────────────

// forgetLine drops the one line a query names among the lines this
// conversation can see, and answers the receipt. It is the model's `forget`;
// a person forgets a line on the Memories tab.
//
// IT NEVER GUESSES. Every word of the query must appear in the line (an exact
// line wins outright); one match is forgotten, none says so, and more than one
// forgets NOTHING and lists them — "forget the deploy line" in a store with
// three deploy lines is a question, not an instruction.
func (a *Agent) forgetLine(query string) (string, error) {
	visible, err := a.memory.store.MemoriesFor(a.memoryOwners())
	if err != nil {
		return "", err
	}
	matches := matchMemories(visible, query)
	switch len(matches) {
	case 0:
		return "nothing remembered matches " + strconv.Quote(strings.TrimSpace(query)), nil
	case 1:
	default:
		var out strings.Builder
		out.WriteString(strconv.Itoa(len(matches)) + " remembered lines match " + strconv.Quote(strings.TrimSpace(query)) +
			" · nothing was forgotten · say more of the line, or forget one on the Memories tab:")
		for _, memory := range matches {
			out.WriteString("\n  " + memory.Text)
		}
		return out.String(), nil
	}
	target := matches[0]
	if err := a.memory.store.ForgetMemoryForOwners(a.memoryOwners(), target.ID, a.memorySourceSession()); err != nil {
		return "", err
	}
	return "forgot · " + target.Text, nil
}

// matchMemories is the lines a query names: an exact line if there is one, and
// otherwise every line that contains every word of the query.
func matchMemories(memories []store.Memory, query string) []store.Memory {
	want := store.NormalizeMemoryWords(query)
	if want == "" {
		return nil
	}
	for _, memory := range memories {
		if store.NormalizeMemoryWords(memory.Text) == want {
			return []store.Memory{memory}
		}
	}
	words := strings.Fields(want)
	var found []store.Memory
	for _, memory := range memories {
		line := store.NormalizeMemoryWords(memory.Text)
		all := true
		for _, word := range words {
			if !strings.Contains(line, word) {
				all = false
				break
			}
		}
		if all {
			found = append(found, memory)
		}
	}
	return found
}

// ── what a person can ask for by hand ───────────────────────────────────────

// MemoryLine is one remembered thing as a surface prints it.
type MemoryLine struct {
	ID    string
	Title string
	Text  string
	// Where is where it holds, in receipt words ("for this project").
	Where string
	// Origin is who kept it ([store.OriginPerson] and its siblings), or "".
	Origin string
}

// Remembers reports whether this conversation has a memory store at all. It is
// true whatever the noticing row says: lines the person kept hold either way.
func (a *Agent) Remembers() bool { return a.remembers() }

// Remember keeps one line the PERSON typed — /remember <text> — for this
// project, or for the person everywhere when the conversation has no project
// of its own, and answers the receipt the surface prints.
func (a *Agent) Remember(text string) (string, error) {
	return a.rememberAs(text, store.MemoryScopeProject)
}

// RememberAlways is /always: the same errand as /remember, kept because people
// were taught the word. everywhere is the surface saying it was typed on home,
// which keeps it for the person in every project.
func (a *Agent) RememberAlways(text string, everywhere bool) (string, error) {
	scope := store.MemoryScopeProject
	if everywhere {
		scope = store.MemoryScopeUser
	}
	return a.rememberAs(text, scope)
}

func (a *Agent) rememberAs(text, scope string) (string, error) {
	memory, reach, added, err := a.keep(text, scope, store.OriginPerson, nil)
	if errors.Is(err, store.ErrMemoryFull) {
		return "", a.memoryFullError(err)
	}
	if err != nil {
		return "", err
	}
	if !added {
		return "already remembered · " + memory.Text + " · " + reach, nil
	}
	return "remembered · " + memory.Text + " · " + reach, nil
}

// Memories lists what this conversation keeps, in the order its block carries
// it — or the lines a query names.
func (a *Agent) Memories(query string) ([]MemoryLine, error) {
	if !a.remembers() {
		return nil, errors.New("this conversation keeps no memory")
	}
	found, err := a.memory.store.MemoriesFor(a.memoryOwners())
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) != "" {
		found = matchMemories(found, query)
	}
	return memoryLines(found), nil
}

// AlwaysMemories is the bare /always listing: what holds here, or with
// everywhere what holds for the person and this machine wherever they work.
func (a *Agent) AlwaysMemories(everywhere bool) ([]MemoryLine, error) {
	if !a.remembers() {
		return nil, errors.New("this conversation keeps no memory")
	}
	owners := a.memoryOwners()
	if everywhere {
		owners = MemoryOwners("")
	}
	found, err := a.memory.store.MemoriesFor(owners)
	if err != nil {
		return nil, err
	}
	return memoryLines(found), nil
}

// MoveMemory moves one line — any line the memory place is showing — to where
// the person says it holds: `user` (for them everywhere), `env` (this machine)
// or `project` (this conversation's project, or the person everywhere when it
// has none). It answers the receipt.
func (a *Agent) MoveMemory(id, scope string) (string, error) {
	if !a.memoryWritable() {
		return "", errors.New("this conversation keeps no memory")
	}
	owner, widened := a.ownerFor(scope)
	_, name, _ := a.projectKey()
	merged, err := a.memory.store.MoveMemory(id, owner, name)
	if errors.Is(err, store.ErrMemoryFull) {
		return "", a.memoryFullError(err)
	}
	if err != nil {
		return "", err
	}
	if merged {
		return "already kept " + memoryReach(owner, widened) + " · this copy is let go", nil
	}
	return "now " + memoryReach(owner, widened), nil
}

// MemoryProject is the owner this conversation keeps project lines under, or
// "" when it has no project of its own — what lets the Memories tab say which
// group is "this project". In process it cannot fail; the error is the wire's
// (internal/remote), where "could not ask" must not read as "no project".
func (a *Agent) MemoryProject() (string, error) {
	return a.memoryHere(), nil
}

func memoryLines(found []store.Memory) []MemoryLine {
	lines := make([]MemoryLine, 0, len(found))
	for _, memory := range found {
		lines = append(lines, MemoryLine{
			ID: memory.ID, Title: memory.Title, Text: memory.Text,
			Where: memoryReach(memory.Owner, false), Origin: memory.Origin,
		})
	}
	return lines
}

// ── noticing ────────────────────────────────────────────────────────────────

// learnFromTurn is the pass after a completed turn, off the person's path
// entirely: one reflex call that updates the state card and, when this
// conversation may notice ([Agent.noticing]) and the PERSON started the turn,
// may keep one line the person said.
//
// ONLY THE PERSON'S OWN TURNS ARE READ FOR MEMORY. A task's report, a watch's
// news or a team's directive arrives as a user-role note the session wrote
// ([userMessage.authored]); a line kept from one of those would be a line about
// the person that the person never said.
func (a *Agent) learnFromTurn(user userMessage, answer string) {
	if !a.memoryWritable() {
		return
	}
	// AN ASK-HERE ERRAND IS NOT NOTICED: it is usually one exchange, so the
	// line saying what was kept would have no turn after it to be said on, and
	// what is typed there is a reminder or a watch rather than a preference.
	askMemory := personTyped(user) && a.noticing() && !a.config.Errand
	words := user.said
	if strings.TrimSpace(words) == "" {
		words = user.text()
	}
	if !a.startMemoryJob() {
		return
	}
	a.memory.mu.Lock()
	kept := append([]store.Memory(nil), a.memory.selected...)
	usedContext := a.memory.selectionContext
	a.memory.mu.Unlock()
	go func() {
		defer a.memoryJobs.Done()
		a.importMemoryFile()
		a.noticeTurn(words, answer, askMemory, kept, usedContext, personTyped(user))
	}()
}

// noticeTurn is the reading itself: the lines this conversation can see,
// numbered so the reading can say one is corrected, the one call, and the
// state card it always answers.
func (a *Agent) noticeTurn(words, answer string, askMemory bool, kept []store.Memory, usedContext store.MemoryContext, personAuthored bool) {
	client := a.reflexClient()
	if client == nil {
		return
	}
	texts := make([]string, len(kept))
	for index, memory := range kept {
		texts[index] = memory.Text
	}
	found, err := a.noticeWithRecovery(client, words, answer, texts, askMemory)
	if err != nil {
		a.reportNoticeFailure(err)
		return
	}
	if found.State != nil {
		a.mergeStateCard(*found.State)
	}
	for _, index := range found.Used {
		if index > 0 && index <= len(kept) && !memoryFeedbackContradicts(found, index) {
			if err := a.memory.store.RecordMemoryContextEvent(kept[index-1].ID, "used", usedContext); err != nil {
				a.journalMemoryFailure("used", err)
			}
		}
	}
	if personAuthored {
		a.recordMemoryContradictions(found, kept, usedContext)
	}
	if found.Keep == "" || found.Durability == "ephemeral" {
		return
	}
	var replaced *store.Memory
	if found.Replaces > 0 && found.Replaces <= len(kept) {
		line := kept[found.Replaces-1]
		replaced = &line
	}
	a.keepNoticedDurable(found.Keep, found.Scope, replaced, found.Durability)
}

// reportNoticeFailure excludes deliberate chat shutdown from provider failures.
func (a *Agent) reportNoticeFailure(err error) {
	if a.memoryCtx.Err() == nil {
		a.journalMemoryFailure("notice", err)
	}
}

// noticeWithRecovery gives a transient outage one later chance without putting
// another wait on the person's turn. The original exchange and numbered working
// set stay together, so a correction never applies to a newer turn's memories.
// The job remains owned by memoryCtx and memoryJobs, including its backoff.
func (a *Agent) noticeWithRecovery(client reflex.Completer, words, answer string, texts []string, askMemory bool) (reflex.NoticeResult, error) {
	found, err := reflex.Notice(a.memoryCtx, client, words, answer, texts, askMemory)
	kind, _ := provider.RouteFailureOf(err)
	if err == nil || kind != provider.RouteTransient || a.memoryCtx.Err() != nil {
		return found, err
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-a.memoryCtx.Done():
		return reflex.NoticeResult{}, a.memoryCtx.Err()
	case <-timer.C:
	}
	if err := a.memoryCtx.Err(); err != nil {
		return reflex.NoticeResult{}, err
	}
	return reflex.Notice(a.memoryCtx, client, words, answer, texts, askMemory)
}

// keepNoticed writes what the reading found through the one door and SAYS SO:
// a line kept on the person's behalf is never kept in silence.
func (a *Agent) keepNoticed(text, scope string, replaced *store.Memory) {
	a.keepNoticedDurable(text, scope, replaced, "")
}

func (a *Agent) keepNoticedDurable(text, scope string, replaced *store.Memory, durability string) {
	memory, reach, added, err := a.keepDurable(text, scope, store.OriginNoticed, replaced, false, durability)
	switch {
	case errors.Is(err, store.ErrMemoryFull):
		a.sayMemory("noticed something worth keeping, but memory is full " +
			memoryFullWhere(err, a.memoryHere(), a.memoryHereName()) + " · " + text + memoryRoomWords)
	case err != nil:
		a.journalMemoryFailure("notice", err)
	case !added && replaced != nil:
		// THE CORRECTION WAS ALREADY KEPT, so the line it corrects is let go
		// and nothing new is written — which is still a change to what rides,
		// and is said ([store.Store.SupersedeMemoryForOwners]).
		a.sayMemory("noticed · " + replaced.Text + " is let go · already kept: " + memory.Text + " · " + memoryTakeBack)
	case !added:
		// Already kept: nothing changed, so nothing is said.
	case replaced != nil:
		a.sayMemory("noticed and remembered · " + memory.Text + " · replaces " + replaced.Text + " · " + reach + " · " + memoryTakeBack)
	default:
		a.sayMemory("noticed and remembered · " + memory.Text + " · " + reach + " · " + memoryTakeBack)
	}
}

// reflexClient is this conversation's own client pinned to the reflex model,
// or nil when the ladder cannot name one. It is built per call, for
// [Agent.SetModel]'s sake: the last rung of the reflex ladder is the model the
// conversation is talking to.
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
	return a.memory.reflex.Bind(billedCompleter{agent: a, inner: routed}, named, fallback, a.sayMemory, model)
}

// billedCompleter is what makes a reflex call cost something a person can see:
// it is folded into the SESSION total and never into a turn's, because no turn
// asked for it.
type billedCompleter struct {
	agent *Agent
	inner Completer
}

func (b billedCompleter) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	// EVERY REFLEX CALL IS A MEMORY CALL, and the role is what keeps it off
	// the status line: it runs beside an answer somebody is reading.
	if !provider.RoleFrom(ctx).Known() {
		ctx = provider.WithRole(ctx, lane.RoleMemory)
	}
	response, err := b.inner.CompleteWithMessages(ctx, messages, options...)
	if err == nil {
		// The active model is read from the same options the provider reads.
		var request ai.Request
		for _, option := range options {
			if optionErr := option(&request); optionErr != nil {
				continue
			}
		}
		if reflex.AnsweredNothing(response) {
			b.agent.addEmptyReflexUsage(response, request.Model)
		} else {
			b.agent.addAuxiliaryUsageAs(response, request.Model, 1, string(roles.RoleReflex))
		}
	}
	return response, err
}

// ── the model's two verbs ───────────────────────────────────────────────────

// rememberDescription and forgetDescription are what the model is told. The
// time sentence is the mirror of the automation tool's: a timeless rule is a
// memory, and anything with a time component is an automation.
const (
	rememberDescription = "Keep durable statements the person stated/accepted, no inferred details; a time component needs automation."
	rememberSchemaJSON  = `{"type":"object","properties":{"text":{"type":"string"},"scope":{"type":"string","enum":["user","project","env"],"description":"project default; user for all work; env for this machine"},"durability":{"type":"string","enum":["standing_rule","decision","durable_fact","project_state"],"description":"For new captures"}},"required":["text"],"additionalProperties":false}`
	forgetDescription   = "Forget revoked lines; exceptions keep broader rules."
	forgetSchemaJSON    = `{"type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`
)

func init() {
	glossField["remember"] = "text"
	glossField["forget"] = "text"
}

// memoryNotTypedWords is what the model's `remember` and `forget` answer on a
// turn the person did not type.
//
// A TURN THE PERSON DID NOT TYPE MAY NOT CHANGE WHAT THEY ARE REMEMBERED BY. A
// task's report, a team's directive or a watch's news opens a turn of its own
// ([userMessage.authored]), and a tool result inside one can carry words
// somebody else wrote; a line kept from it would ride every future
// conversation, with its receipt said where nobody may be watching. It is the
// noticing pass's own rule ([Agent.learnFromTurn]), held for the model's verbs.
const memoryNotTypedWords = "Not on this turn: the person did not type it, so nothing is kept or forgotten. Say what you would keep, and the person can /remember it."

// personTyped reports whether a turn's opening message is the person's own
// words: not a note the session wrote ([userMessage.authored]), not a woken
// turn's opening — a task's report, a job's end, a watch firing or a team's
// message, whose note arrives on the steering queue ([Agent.wakeLocked]) — and
// not empty.
func personTyped(user userMessage) bool {
	return !user.authored && !user.wake && !user.empty()
}

// armMemoryVerbs is said at the start of every turn: the model's memory verbs
// act only on a turn the person typed ([memoryNotTypedWords]). A turn the
// session opened — a task's landing, a job's ending, an answer to a question —
// is not armed, with one exception: the empty turn started to answer a line the
// person typed in the last seconds of the turn before
// ([userMessage.answersPerson]), which is answering them.
func (a *Agent) armMemoryVerbs(user userMessage) {
	if a.memory != nil {
		a.memory.typed.Store(personTyped(user) || user.answersPerson)
	}
}

// heardThePerson is a line the person typed drained into the running turn
// ([Agent.drainSteering]): it arms the verbs for the rest of that turn, so a
// steer typed into a woken turn still has them. The next turn arms afresh.
func (a *Agent) heardThePerson() {
	if a.memory != nil {
		a.memory.typed.Store(true)
	}
}

// memoryTools is `remember` and `forget`, on a writable conversation's belt
// only: a read-only posture and a task worker do not have the verbs at all.
//
// A LEAN PREFIX DOES NOT CARRY THEM EITHER, and only for its byte budget
// (prefixbudget_test.go): the lines kept still ride its message[0], the pass
// after a turn still notices, and its page tells the model the person can type
// /remember — what a small window gives up is the model's own two hands.
func (a *Agent) memoryTools() []bare.Tool {
	if !a.memoryWritable() || a.config.promptProfile().lean() {
		return nil
	}
	return []bare.Tool{a.memorySearchTool(), {
		Name:        "remember",
		Description: rememberDescription,
		Schema:      json.RawMessage(rememberSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Text       string `json:"text"`
				Scope      string `json:"scope"`
				Durability string `json:"durability"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			if !a.memory.typed.Load() {
				return memoryNotTypedWords, true, nil
			}
			switch parsed.Durability {
			case "", "standing_rule", "decision", "durable_fact", "project_state":
			default:
				return "Invalid durability", true, nil
			}
			memory, reach, added, err := a.keepDurable(parsed.Text, parsed.Scope, store.OriginModel, nil, false, parsed.Durability)
			if errors.Is(err, store.ErrMemoryFull) {
				return a.memoryFullError(err).Error(), true, nil
			}
			if err != nil {
				return "Could not remember that: " + err.Error(), true, nil
			}
			if !added {
				return "already remembered " + reach + ": " + memory.Text, false, nil
			}
			a.sayMemory("remembered · " + memory.Text + " · " + reach + " · " + memoryTakeBack)
			return "remembered " + reach + ": " + memory.Text, false, nil
		},
	}, {
		Name:        "forget",
		Description: forgetDescription,
		Schema:      json.RawMessage(forgetSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Text string `json:"text"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return rejectToolArguments(ctx, "Invalid arguments: "+err.Error()), true, nil
			}
			if strings.TrimSpace(parsed.Text) == "" {
				return "Name the line to forget by its words.", true, nil
			}
			if !a.memory.typed.Load() {
				return memoryNotTypedWords, true, nil
			}
			receipt, err := a.forgetLine(parsed.Text)
			if err != nil {
				return "Could not forget that: " + err.Error(), true, nil
			}
			if strings.HasPrefix(receipt, "forgot · ") {
				a.sayMemory(receipt + " · put it back on the Memories tab")
			}
			return receipt, false, nil
		},
	}}
}

// ── lines said ──────────────────────────────────────────────────────────────

// sayMemory puts one line in front of the person: on the turn's own stream
// when there is one, and otherwise on the standing lane every surface holds for
// the life of the session ([Agent.WatchTaskUpdates]) — the noticing pass runs
// after the turn's stream has closed, and the last turn before a person quits
// has no next turn to carry its line. Only with no surface watching at all is
// the line held for the next turn ([Agent.flushMemoryNotices]).
func (a *Agent) sayMemory(text string) {
	if a.config.bindingOnlyMemory || a.memory == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	event := Event{Kind: EventNotice, Text: text, Unrecorded: true, NoticeKind: NoticeKindMemory}
	if a.hub.send(event) {
		return
	}
	if len(a.taskWatchers) > 0 {
		for _, watcher := range a.taskWatchers {
			watcher.send(event)
		}
		return
	}
	a.memory.mu.Lock()
	a.memory.said = append(a.memory.said, text)
	a.memory.mu.Unlock()
}

// flushMemoryNotices says the lines the last post-turn pass had nowhere to
// say. It is called once a turn's hub is live.
func (a *Agent) flushMemoryNotices() {
	if a.memory == nil {
		return
	}
	a.memory.mu.Lock()
	held := a.memory.said
	a.memory.said = nil
	a.memory.mu.Unlock()
	for _, line := range held {
		a.sayMemory(line)
	}
}

// journalMemoryFailure is what a failed memory write becomes: an event in the
// journal with the reason, and — once per window — one line saying so.
//
// THE FALLBACK FILE IS THE DB-IS-THE-PROBLEM ANSWER: when the journal itself
// refuses the record, the reason goes to `v3/memory-failures.log` instead.
func (a *Agent) journalMemoryFailure(via string, err error) {
	if err == nil || a.memory == nil {
		return
	}
	reason := err.Error()
	if storeErr := a.memory.store.JournalMemoryFailure(via, err); storeErr != nil {
		appendMemoryFailureLog(home.Join("v3", "memory-failures.log"), via, reason, storeErr.Error())
	}
	a.memory.mu.Lock()
	window := time.Since(a.memory.failureSaid) >= memoryFailureWindow
	if window {
		a.memory.failureSaid = time.Now()
	}
	a.memory.mu.Unlock()
	if window {
		if via == "notice" {
			a.sayMemory("couldn't save this exchange to memory · use /remember to save an important detail")
		} else {
			a.sayMemory("couldn't update memory just now · " + clipWords(reason, 120))
		}
	}
}

// clipWords bounds a reason for a line on a screen.
func clipWords(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit-1]) + "…"
}

// appendMemoryFailureLog appends one line, best effort.
func appendMemoryFailureLog(path, via, reason, storeErr string) {
	if path == "" {
		log.Printf("session: a memory write failed and could not be journaled (via %s): %s", via, reason)
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	stamp := time.Now().UTC().Format(time.RFC3339)
	_, _ = file.WriteString(stamp + " via=" + via + " error=" + reason + " journal_error=" + storeErr + "\n")
}

// startMemoryJob registers one background memory pass, and refuses once the
// conversation is closing, so Close can wait for it.
func (a *Agent) startMemoryJob() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.workStopped || a.memoryCtx == nil {
		return false
	}
	a.memoryJobs.Add(1)
	return true
}

// ── the legacy file ─────────────────────────────────────────────────────────

// readMemoryFile reads the legacy file's lines: a leading "- " dropped,
// headings, comments and blank lines skipped, each line cut to what the store
// holds. found is false when there is no file to read, which is every machine
// that has already been through the import.
func readMemoryFile(path string) (lines []string, found bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, nil
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64<<10), store.MemoryTextRunes*4)
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(scanner.Text()), "- "))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "<!--") {
			continue
		}
		if utf8.RuneCountInString(line) > store.MemoryTextRunes {
			line = string([]rune(line)[:store.MemoryTextRunes])
		}
		lines = append(lines, line)
	}
	return lines, true, scanner.Err()
}

// importMemoryFile carries the legacy ~/.codeaf/v3/memory.md into the store,
// once, for the person everywhere, and renames it out of the way only after
// every line landed. One line says how many it carried.
func (a *Agent) importMemoryFile() {
	if !a.memoryWritable() {
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
	lines, found, err := readMemoryFile(path)
	if !found {
		return
	}
	if err != nil {
		a.retryImport(fmt.Errorf("reading %s: %w", path, err))
		return
	}
	imported := 0
	for _, line := range lines {
		_, _, added, err := a.keepLine(line, store.MemoryScopeUser, store.OriginPerson, nil, true)
		if err != nil {
			a.retryImport(fmt.Errorf("importing a memory.md line: %w", err))
			return
		}
		if added {
			imported++
		}
	}
	if err := os.Rename(path, path+".imported"); err != nil {
		a.retryImport(fmt.Errorf("moving %s out of the way: %w", path, err))
		return
	}
	if imported > 0 {
		a.sayMemory(fmt.Sprintf("imported %d lines from memory.md", imported))
	}
}

// retryImport journals an import failure and gives the import its next turn
// back: the once-marker was set optimistically.
func (a *Agent) retryImport(err error) {
	a.memory.mu.Lock()
	a.memory.imported = false
	a.memory.mu.Unlock()
	a.journalMemoryFailure("import", err)
}

// memoryTypeOf is a cheap guess at what kind of line this is. Nothing reads it
// to decide anything; it is a fact about the line worth recording.
func memoryTypeOf(text string) string {
	lower := strings.ToLower(text)
	for _, cue := range []string{"prefer", "always", "never", "like", "want"} {
		if strings.Contains(lower, cue) {
			return store.MemoryPreference
		}
	}
	return store.MemoryFact
}

// memoryTypeForDurability preserves the capture class for inspection and
// consolidation rather than storing every progress update as a lasting fact.
func memoryTypeForDurability(text, durability string) string {
	switch durability {
	case "durable_fact":
		return store.MemoryFact
	case "project_state":
		return store.MemoryProjectState
	case "decision":
		return store.MemoryDecision
	case "standing_rule":
		return store.MemoryPreference
	default:
		return memoryTypeOf(text)
	}
}

// ── message[0], the state card, and close ───────────────────────────────────

// refreshSystemLocked rebuilds message[0] from the base prompt, the folders
// somebody attached, what this conversation keeps, and its record of what it
// has decided.
//
// Context admission and deliberate changes determine the memory prefix. message[0] sits in front of every message there is, so one changed byte
// in it re-prices the entire transcript at the uncached rate on the very next
// request. The base prompt never moves; a folder holds until it is removed; a
// memory line moves when somebody keeps or forgets one; a decision is recorded
// when a question is answered. What that buys is pinned as a law: message[0] is
// byte-identical from one turn to the next unless somebody did something
// (prefixcache_test.go).
func (a *Agent) refreshSystemLocked() {
	if len(a.messages) == 0 {
		return
	}
	if !a.recordRead {
		a.recordRead = true
		a.recordText = DecisionsSection(a.Decisions())
	}
	head := a.system + a.placesText + a.memoryText
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

// takeRecord re-renders the decision section from the file, off every lock.
func (a *Agent) takeRecord() {
	text := DecisionsSection(a.Decisions())
	a.mu.Lock()
	a.recordText = text
	a.mu.Unlock()
}

// refreshCardLocked holds the state card's new text for the note that carries
// it at the tail of the transcript.
func (a *Agent) refreshCardLocked(text string) {
	a.cardText = text
}

// mergeStateCard folds one exchange's delta into the card and, when something
// actually moved, puts the new block in front of the model. The card's own lock
// covers the merge and the file write; a.mu is taken afterwards for the
// assignment alone.
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
// then cuts whatever is left, and then JOINS what it cut: a pass the grace ran
// out on may be inside a store write no context reaches, and the door that
// owns the store closes it the moment Close returns.
func (a *Agent) waitForMemory(stop context.CancelFunc) {
	settled := make(chan struct{})
	go func() {
		a.memoryJobs.Wait()
		close(settled)
	}()
	if memoryJoined(settled) {
		stop()
		return
	}
	stop()
	if !memoryJoined(settled) {
		log.Printf("session: a memory pass was still running %s after it was cancelled; "+
			"closing without it", closeGrace)
	}
}

// memoryJoined waits up to [closeGrace] for settled and answers whether it
// closed.
func memoryJoined(settled <-chan struct{}) bool {
	timer := time.NewTimer(closeGrace)
	defer timer.Stop()
	select {
	case <-settled:
		return true
	case <-timer.C:
		return false
	}
}
