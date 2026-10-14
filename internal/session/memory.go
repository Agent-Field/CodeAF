package session

// Memory is what one conversation knows and the next one would otherwise have
// to be told again: a preference, a standing rule, a correction, a decision, a
// lasting fact about the person, their machine or their project.
//
// IT IS THE SIMPLEST THING THAT WORKS, AND THAT IS THE DESIGN.
//
//   - A MEMORY IS ONE LINE WITH ONE STRENGTH. Every line a conversation can see —
//     the person's, this machine's, and this project's — rides one <memory>
//     block in message[0], in front of every turn, every task brief and every
//     automation run. There is no router choosing which lines a message needs,
//     no ranking, and no flag that makes one line weigh more than another: a
//     line the person typed and a line that was noticed for them are the same
//     thing once kept, and the person can see and take back either.
//
//   - WHAT IS SAVED IS WHAT THE MODEL SEES. The block is bounded
//     ([store.MemoryBlockRunes]), and the store refuses — inside the write that
//     would do it — any change that would carry ANY conversation's block past
//     the bound, with a sentence saying so. Only a store an earlier build filled
//     with no bound can hold more than a block fits; then [store.FitMemories]
//     chooses what rides and the conversation says, once, how many lines did
//     not. The block has no clock in it, so it renders byte for byte — and the
//     provider's cached prefix stays warm — until somebody changes a line.
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
//     row turns the noticing pass off; the lines already kept go on riding every
//     turn, and /remember goes on working. It is read live, at the end of every
//     turn, so a long-lived engine follows a change without a restart.
//
// A READ-ONLY POSTURE (an automation's run, [Config.bindingOnlyMemory]) reads
// the block and does nothing else: no noticing, no verbs, no lines said.
//
// A REFLEX FAILURE IS INVISIBLE TO THE TURN. The noticing pass runs after the
// turn is sealed; a provider that does not answer leaves the turn exactly what
// it was, and the failure is journaled and said once per window.

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
	// heard is a line the person typed that reached the transcript after the
	// running turn's loop had ended — the line the follow-up turn started for it
	// answers ([Agent.armMemoryVerbs]).
	heard atomic.Bool
	// failureSaid is when the last failure line went out.
	failureSaid time.Time
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
	memoryPreamble = "What the person has had you keep. Each line holds in every conversation in its scope until it is forgotten; the person's own words in this conversation outrank a line. A line you keep or forget is shown to the person."
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
// NO AGES, AND THE ORDER IS THE STORE'S ([store.Store.MemoriesFor]): the block
// rides the cached prefix, so nothing here may depend on a clock. Which lines
// ride when they do not all fit is [store.FitMemories]'s, in that same order.
func renderMemory(memories []store.Memory, closing string) string {
	fit, _ := store.FitMemories(memories)
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
	if shown == 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(memoryHeading + ":\n\n" + memoryPreamble + "\n\n")
	out.WriteString(lines.String())
	if more := len(memories) - shown; more > 0 {
		out.WriteString("…" + strconv.Itoa(more) + " more kept lines did not fit here\n")
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
	memories, err := st.MemoriesFor(owners)
	if err != nil {
		return ""
	}
	return renderMemory(memories, memoryReport)
}

// memoryWorld is the conversation answering for a piece of its own work, from
// the brain a read may use, for the owners this conversation sees — so a
// conversation and the work it hands out cannot disagree about what is kept.
func (a *Agent) memoryWorld() string {
	return MemoryWorld(a.config.bindingBrain(), a.memoryOwners())
}

// memoryWorld is the section a starting node's brief closes on.
func (g *TaskGraph) memoryWorld() string {
	if g.home == nil {
		return ""
	}
	return g.home.memoryWorld()
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
	memories, err := st.MemoriesFor(MemoryOwners(a.config.MemoryProjectKey))
	if err != nil {
		return
	}
	a.memoryText = memoryBlock(memories)
	a.noteMemoryOffUpgradeLocked()
	// A STORE AN EARLIER BUILD FILLED PAST THE BOUND IS SAID ONCE, in this
	// conversation, rather than only to the model: the person is the one who
	// can choose what to forget. The line is held for the stream the turn is
	// about to open ([Agent.flushMemoryNotices]) — this runs under a.mu, where
	// saying it here would take the lock twice.
	if _, left := store.FitMemories(memories); left > 0 && a.memory != nil && !a.config.bindingOnlyMemory {
		a.memory.mu.Lock()
		if !a.memory.overflowSaid {
			a.memory.overflowSaid = true
			a.memory.said = append(a.memory.said, memoryOverflowWords(left))
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
	return lines + " fit in front of the model here · forget some on the Memories tab"
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
	return a.keepLine(text, scope, origin, replaced, false)
}

// keepLine is [Agent.keep] with the one exception spelled out: unbounded is the
// legacy import's, whose lines were the person's before the bound existed
// ([store.WriteRequest.Unbounded]). Every other caller is measured, by the
// store, inside the write.
func (a *Agent) keepLine(text, scope, origin string, replaced *store.Memory, unbounded bool) (store.Memory, string, bool, error) {
	if !a.memoryWritable() {
		return store.Memory{}, "", false, errors.New("this conversation keeps no memory")
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
			Owner: owner, Type: memoryTypeOf(text), Text: text,
			Origin: origin, ProjectName: projectName, SourceSession: a.memorySourceSession(),
		})
		if err != nil {
			return store.Memory{}, "", false, err
		}
		return fresh, reach, added, nil
	}
	result, err := a.memory.store.Write(store.WriteRequest{
		Owner: owner, Type: memoryTypeOf(text), Text: text,
		Origin: origin, ProjectName: projectName, SourceSession: a.memorySourceSession(),
		Unbounded: unbounded,
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
	go func() {
		defer a.memoryJobs.Done()
		a.importMemoryFile()
		a.noticeTurn(words, answer, askMemory)
	}()
}

// noticeTurn is the reading itself: the lines this conversation can see,
// numbered so the reading can say one is corrected, the one call, and the
// state card it always answers.
func (a *Agent) noticeTurn(words, answer string, askMemory bool) {
	client := a.reflexClient()
	if client == nil {
		return
	}
	var kept []store.Memory
	if askMemory {
		visible, err := a.memory.store.MemoriesFor(a.memoryOwners())
		if err != nil {
			a.journalMemoryFailure("notice", err)
			askMemory = false
		}
		kept = visible
	}
	texts := make([]string, len(kept))
	for index, memory := range kept {
		texts[index] = memory.Text
	}
	found, err := reflex.Notice(a.memoryCtx, client, words, answer, texts, askMemory)
	if err != nil {
		a.journalMemoryFailure("notice", err)
		return
	}
	if found.State != nil {
		a.mergeStateCard(*found.State)
	}
	if found.Keep == "" {
		return
	}
	var replaced *store.Memory
	if found.Replaces > 0 && found.Replaces <= len(kept) {
		line := kept[found.Replaces-1]
		replaced = &line
	}
	a.keepNoticed(found.Keep, found.Scope, replaced)
}

// keepNoticed writes what the reading found through the one door and SAYS SO:
// a line kept on the person's behalf is never kept in silence.
func (a *Agent) keepNoticed(text, scope string, replaced *store.Memory) {
	memory, reach, added, err := a.keep(text, scope, store.OriginNoticed, replaced)
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
	return a.memory.reflex.Bind(billedCompleter{agent: a, inner: routed}, named, fallback, a.sayMemory)
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
	rememberDescription = "Keep one line in memory: a preference, standing rule, correction, decision or lasting fact the person wants in every future conversation. It rides every conversation in its scope and the person is shown it. Anything with a time component (a reminder, a schedule, a watch) is an automation, not a memory."
	rememberSchemaJSON  = `{"type":"object","properties":{"text":{"type":"string","description":"The one line, in the person's terms"},"scope":{"type":"string","enum":["user","project","env"],"description":"project by default; user only for a rule the person gave for all their work; env for this machine"}},"required":["text"],"additionalProperties":false}`
	forgetDescription   = "Forget one remembered line, named by its words under Memory. The person is shown it and can put it back."
	forgetSchemaJSON    = `{"type":"object","properties":{"text":{"type":"string","description":"The line's words"}},"required":["text"],"additionalProperties":false}`
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
// session opened — a task's landing, a job's ending — is not armed, with one
// exception: the empty turn started to answer a line the person typed in the
// last seconds of the turn before ([Agent.heardThePerson]), which is answering
// them.
func (a *Agent) armMemoryVerbs(user userMessage) {
	if a.memory != nil {
		heard := a.memory.heard.Swap(false)
		a.memory.typed.Store(personTyped(user) || heard && user.wake && user.empty())
	}
}

// heardThePerson is a line the person typed reaching the transcript
// ([Agent.drainQueuedLocked]). Mid-turn it arms the verbs for the rest of the
// turn, so a steer typed into a woken turn still has them; after the turn's
// loop has ended it arms the turn started to answer it.
func (a *Agent) heardThePerson() {
	if a.memory != nil {
		a.memory.typed.Store(true)
		a.memory.heard.Store(true)
	}
}

// endMemoryVerbs is said when a turn's loop ends: a line the person typed
// during the turn was answered by it, so only one drained after this point is
// owed the next turn's verbs.
func (a *Agent) endMemoryVerbs() {
	if a.memory != nil {
		a.memory.heard.Store(false)
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
			if !a.memory.typed.Load() {
				return memoryNotTypedWords, true, nil
			}
			memory, reach, added, err := a.keep(parsed.Text, parsed.Scope, store.OriginModel, nil)
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
		Execute: func(_ context.Context, args json.RawMessage) (string, bool, error) {
			var parsed struct {
				Text string `json:"text"`
			}
			if err := decodeToolArguments(args, &parsed); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
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
	event := Event{Kind: EventNotice, Text: text, Unrecorded: true}
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
		a.sayMemory("couldn't update memory just now · " + clipWords(reason, 120))
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
	if a.closed || a.memoryCtx == nil {
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

// ── message[0], the state card, and close ───────────────────────────────────

// refreshSystemLocked rebuilds message[0] from the base prompt, the folders
// somebody attached, what this conversation keeps, and its record of what it
// has decided.
//
// WHAT LIVES HERE IS WHAT MOVES ONLY ON A DELIBERATE ACT, and that is the whole
// rule. message[0] sits in front of every message there is, so one changed byte
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
