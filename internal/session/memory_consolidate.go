package session

// Dreaming: the memory file consolidates OFFLINE, at idle, conservatively.
//
// note appends and forget removes, so a memory file only ever GROWS: the same
// preference stated twice in two sessions is two lines, a fact that stopped
// being true sits beside the fact that replaced it, and nothing in the turn
// loop has any reason to notice. Left alone it becomes a 4KiB block of
// contradictions riding every request of every turn.
//
// The fix is not a bigger tool. It is a pass that runs WHEN NOBODY IS TALKING —
// MindMemOS's "dreaming" (harness-research-notes.md §4): consolidate at idle
// rather than at a capacity limit, cluster what is really one fact, mutate
// conservatively, keep provenance. Three properties follow from that, and they
// are the whole design:
//
//   - IT IS NEVER IN THE TURN'S WAY. It is armed 30 seconds after a turn
//     settles and disarmed by anything that means the person is back. The
//     provider call runs holding NO agent lock, so a turn that starts while a
//     pass is in flight does not wait for it — the swap is the only moment the
//     lock is held, and it is a rename.
//
//   - IT MAY ONLY MERGE AND DROP. The consolidator cannot invent a fact, and the
//     result is refused outright if it is longer than the input. A memory that
//     could grow while nobody was watching is a memory nobody can trust, and
//     "the model summarized my preferences into something I never said" is the
//     one failure mode with no recovery: the original lines are gone.
//
//   - TIMES SURVIVE VERBATIM. Sleeping Agent's measurement (§4) is that temporal
//     expressions survive gist compression at ~3% against ~8% for entities —
//     dates are what a summarizer drops FIRST — so time is special-cased twice
//     over: the prompt says any fact holding a time expression is copied
//     character-for-character, and [consolidationHoldsTimes] refuses a result
//     that carries a time the input never spelled that way.
//
// A failed pass, a refused pass, a provider that never answered: memory.md is
// untouched, and the only trace is that nothing happened. There is no event kind
// for it and no message in the room — title.go's reasoning applies unchanged, and
// harder: nobody asked for this work, and it runs when nobody is there.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The consolidator is a ROLE, registered from the file that makes the call —
// the pattern guardian.go states. The tier is low: this is one pass over fifty
// short lines whose whole instruction is "merge the duplicates and drop what is
// superseded", the archetypal cheap call, and a person who disagrees pins it
// (`roles.consolidate: <model>`) without either package changing.
const roleConsolidate = roles.Role("consolidate")

func init() { roles.Register(roleConsolidate, roles.TierLow) }

const (
	// idleDelay is how long the session must be quiet before a pass is even
	// considered. Thirty seconds is past the pause where a person is reading the
	// answer and about to type again, and short enough that an ordinary coffee
	// break is long enough to dream in.
	idleDelay = 30 * time.Second

	// consolidateInterval is the floor between passes. A memory file changes a
	// handful of lines a day; ten minutes is far below any rate at which a
	// second pass could find new work, and it is the rail that keeps a session
	// left open all afternoon from paying for a call every half minute.
	consolidateInterval = 10 * time.Minute

	// minConsolidateFacts is the floor below which there is nothing to gain. A
	// memory of a dozen lines has no clusters in it — a person can see the
	// duplicates from across the room — and consolidating it is a provider call
	// to save nothing.
	minConsolidateFacts = 20
)

// consolidatePrompt is the whole instruction, and every sentence in it is a
// restriction. Two moves are named, everything else is forbidden by name, and
// the time law is stated as its own paragraph in the imperative, because it is
// the one rule a summarizer breaks by default rather than by accident.
const consolidatePrompt = `You consolidate a person's durable memory: a list of standing facts about them and their work, one per line, each beginning with "- ".

Return THE SAME LIST, shorter. You have exactly two moves:

MERGE facts that are the same fact said twice into one line. The merged line keeps the OLDEST date, source or attribution the originals carried — provenance survives the merge.
DROP a fact that a later fact in the list plainly supersedes or contradicts.

You may not invent a fact, add a fact, split a fact, generalize, reword, improve, reorder or explain. Any fact you are not merging or dropping is copied character-for-character.

THE TIME LAW: any fact containing a time expression — a date, a year, a duration, a version number, "since Tuesday", "last week", "in Q3", "v2.1", "3 days" — is copied CHARACTER-FOR-CHARACTER. Never paraphrase a time, never rewrite one form of a date into another, never fold two times into a range or a "recently". If merging two facts would reword a time, DO NOT MERGE THEM — leave both lines exactly as they are.

Answer with the resulting list and nothing else: one fact per line, each beginning with "- ", no numbering, no headings, no preamble, no commentary. Your list must be SHORTER THAN OR THE SAME LENGTH AS the one you were given.`

// ── the idle timer ──────────────────────────────────────────────────────────

// idleAlarm is one armed timer, as this file uses it: something that can be
// stopped. *time.Timer is one.
type idleAlarm interface{ Stop() bool }

// idleClock is the now/timer seam, a pair of functions rather than an interface
// so a test can replace one half and leave the other alone. Production is
// [realIdleClock]; a test hands the store a clock whose alarm fires when the
// test says so, which is what makes "a new turn disarms the timer" a fact this
// package can assert instead of a sleep it can hope for.
type idleClock struct {
	now       func() time.Time
	afterFunc func(time.Duration, func()) idleAlarm
}

func realIdleClock() idleClock {
	return idleClock{
		now: time.Now,
		afterFunc: func(delay time.Duration, fire func()) idleAlarm {
			// time.AfterFunc runs fire on a goroutine of its own, which is the
			// "in a goroutine" half of the contract: the pass never runs on the
			// stack of the turn that armed it.
			return time.AfterFunc(delay, fire)
		},
	}
}

// idleState is everything the idle pass remembers between passes.
type idleState struct {
	mu    sync.Mutex
	clock idleClock
	// alarm is the armed timer, nil when disarmed. Exactly one may exist.
	alarm idleAlarm
	// last is when the last pass was ATTEMPTED, not when one last succeeded: a
	// provider having a bad ten minutes must cost one call, not one every thirty
	// seconds for the rest of the afternoon.
	last time.Time
	// digest is the file as the last SUCCESSFUL pass left it. A file that has
	// not changed since cannot consolidate any further than it already has, and
	// the second pass over it would pay for the same answer.
	digest string
	// running is one pass at a time. Two passes over one file are two swaps
	// racing, and the loser silently reinstates the facts the winner dropped.
	running bool
}

// armIdleLocked starts the idle countdown, with a.mu held. It is called at the
// end of a settled turn and nowhere else.
//
// The eligibility rules are all "is anybody living here":
//
//   - no memory file, no memory to consolidate;
//   - InTask is a task node (task_run.go): a node is one piece of work with its
//     own brief, and it has no durable memory of its own to tidy;
//   - AskConsent false is a session with NO SURFACE ATTACHED — headless --once,
//     a cron run — and a one-shot process is about to exit. Dreaming is for a
//     session that stays alive; arming a 30-second timer inside a program that
//     ends in two is either a pass that never runs or a process that lingers.
func (a *Agent) armIdleLocked() {
	if a.memory == nil || a.closed || a.config.InTask || !a.config.AskConsent || !a.config.MemoryConsolidation {
		return
	}
	store := a.memory
	store.idle.mu.Lock()
	defer store.idle.mu.Unlock()
	if store.idle.alarm != nil {
		store.idle.alarm.Stop()
	}
	store.idle.alarm = store.idle.clock.afterFunc(idleDelay, func() {
		store.idle.mu.Lock()
		store.idle.alarm = nil
		store.idle.mu.Unlock()
		// The error is dropped on purpose and this is the ONLY caller that may
		// drop it: nobody asked for this pass, nobody is watching, and the file
		// it would report about is exactly as it was.
		_ = a.ConsolidateMemory(context.Background())
	})
}

// disarmIdle cancels the countdown. It is called by everything that means the
// session is in use again — a turn starting, a steering note landing, Close —
// and it is a no-op when nothing is armed, which is most of the time.
//
// A pass ALREADY RUNNING is not cancelled by this. It holds no lock a turn
// needs and its swap is a rename; interrupting it would leave the person with
// neither the old file nor the new one.
func (a *Agent) disarmIdle() {
	if a.memory == nil {
		return
	}
	store := a.memory
	store.idle.mu.Lock()
	defer store.idle.mu.Unlock()
	if store.idle.alarm != nil {
		store.idle.alarm.Stop()
		store.idle.alarm = nil
	}
}

// ── the pass ────────────────────────────────────────────────────────────────

// ConsolidateMemory runs one consolidation pass now: read the memory file, ask
// the cheap model to merge and drop, verify the answer, swap the file.
//
// It is exported because it is a whole operation with a beginning and an end
// that a surface may want to run deliberately, and because the idle timer is
// then one caller of a method rather than the only way the behavior exists.
//
// nil means "nothing happened", and that covers both a pass this declined to
// run (no memory, too few facts, too soon, nothing changed) and a pass that
// changed the file. An error is a pass that was attempted and failed, and in
// every one of those cases memory.md is byte-for-byte what it was.
func (a *Agent) ConsolidateMemory(ctx context.Context) error {
	store := a.memory
	if store == nil {
		return nil
	}

	text, facts, digest, err := store.snapshot()
	if err != nil {
		return err
	}
	if len(facts) < minConsolidateFacts {
		return nil
	}

	if !store.beginPass(digest) {
		return nil
	}
	defer store.endPass()

	a.mu.Lock()
	model, source, closed := a.model, a.config.RolesSource, a.closed
	a.mu.Unlock()
	if closed {
		return nil
	}
	consolidator, err := roles.Resolve(roles.Source(source), roleConsolidate, model)
	if err != nil {
		return err
	}

	// The provider call happens with NO agent lock held. That is the property
	// the whole file is arranged around: a person who types while their memory
	// is being consolidated waits for nothing.
	callCtx, cancel := context.WithTimeout(ctx, providerTimeout)
	defer cancel()
	response, err := a.client.CompleteWithMessages(
		// WithoutStream for the reason the title and the summary use it: this is
		// bookkeeping, and on a stream it would type a list of the person's own
		// preferences into an empty room.
		provider.WithoutStream(callCtx),
		[]ai.Message{
			textMessage("system", consolidatePrompt),
			textMessage("user", text),
		},
		ai.WithModel(consolidator))
	if err != nil {
		return err
	}
	if response == nil {
		return fmt.Errorf("session: consolidator returned nothing")
	}
	// Paid for out of the same pocket as the title and the guardian, and not
	// charged to any turn — no turn asked for it.
	a.addAuxiliaryUsage(response)

	consolidated, err := consolidationResult(facts, response.Text())
	if err != nil {
		return err
	}
	if len(consolidated) == len(facts) && identicalFacts(consolidated, facts) {
		// A pass that changed nothing still counts as a pass: the digest is
		// recorded so the same unchanged file is not re-read tomorrow morning
		// and sent again.
		store.recordPass(digest)
		return nil
	}

	// THE SWAP, and the only moment a lock is held. a.mu orders it against the
	// per-turn prompt refresh (refreshSystemLocked reads the file under this
	// same lock), so a turn either opens on the old memory or on the new one and
	// never on a file being rewritten underneath it. Inside, the write itself is
	// a temp file and a rename — the one write that cannot half-happen, which for
	// a memory file is the difference between a bad pass and no memory at all.
	content := strings.Join(consolidated, "\n") + "\n"
	a.mu.Lock()
	err = store.write(content)
	file := a.file
	a.mu.Unlock()
	if err != nil {
		return err
	}
	store.recordPass(hashOf(content))

	// One line in the journal, because a memory that shrank while nobody was
	// looking must be a thing a person can find afterwards. It is a journal
	// entry of its own kind rather than a message: it is a fact about the FILE,
	// not something anybody said, and a replay that put it in the transcript
	// would hand the model a note about its own bookkeeping.
	if file != nil {
		file.writeLine(memoryEntry{
			Type:      "memory",
			Note:      fmt.Sprintf("memory consolidated: %d → %d facts", len(facts), len(consolidated)),
			Before:    len(facts),
			After:     len(consolidated),
			Timestamp: stamp(),
		})
	}
	return nil
}

// memoryEntry is the journal's consolidation line. Its type is one no replay
// knows, which is exactly right: replaySessionFile ignores entry types it does
// not recognize, so this line is a record for the person reading the file and
// never context for the model reading the session.
type memoryEntry struct {
	Type      string `json:"type"`
	Note      string `json:"note"`
	Before    int    `json:"before"`
	After     int    `json:"after"`
	Timestamp string `json:"timestamp"`
}

// ── the guards ──────────────────────────────────────────────────────────────

// beginPass takes the once-at-a-time claim if the two rate guards allow it, and
// stamps the attempt. False means this pass does not run.
func (m *memoryStore) beginPass(digest string) bool {
	m.idle.mu.Lock()
	defer m.idle.mu.Unlock()
	if m.idle.running {
		return false
	}
	if m.idle.digest != "" && m.idle.digest == digest {
		return false
	}
	now := m.idle.clock.now()
	if !m.idle.last.IsZero() && now.Sub(m.idle.last) < consolidateInterval {
		return false
	}
	m.idle.last = now
	m.idle.running = true
	return true
}

func (m *memoryStore) endPass() {
	m.idle.mu.Lock()
	m.idle.running = false
	m.idle.mu.Unlock()
}

// recordPass remembers the file as this pass left it.
func (m *memoryStore) recordPass(digest string) {
	m.idle.mu.Lock()
	m.idle.digest = digest
	m.idle.mu.Unlock()
}

// ── the file ────────────────────────────────────────────────────────────────

// snapshot reads the memory file for one pass: the text the model will be shown,
// the facts it holds, and a digest of the bytes.
//
// A file past memoryFileLimit is reported as empty rather than read, on forget's
// law: a memory file the size of a log is not a thing this package quietly loads,
// and it is certainly not a thing it sends to a provider.
func (m *memoryStore) snapshot() (text string, facts []string, digest string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	content, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil, "", nil
		}
		return "", nil, "", err
	}
	if len(content) > memoryFileLimit {
		return "", nil, "", nil
	}
	text = string(content)
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			facts = append(facts, strings.TrimSpace(line))
		}
	}
	return text, facts, hashOf(text), nil
}

// write replaces the file's contents atomically — temp file in the same
// directory, then rename — for the reason forget rewrites that way: a memory
// file truncated by a crash mid-write is every remembered fact gone.
func (m *memoryStore) write(content string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	temporary := m.path + ".consolidating"
	if err := os.WriteFile(temporary, []byte(content), 0o644); err != nil {
		return err
	}
	if err := os.Rename(temporary, m.path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// ── reading the answer ──────────────────────────────────────────────────────

// consolidationResult turns the model's reply into the file's next contents, or
// refuses it. Every refusal leaves memory.md exactly as it is, which is why the
// checks can afford to be strict: the cost of a false refusal is one pass that
// did not happen, and the cost of a false accept is a person's standing facts
// rewritten by a model at three in the morning.
func consolidationResult(facts []string, reply string) ([]string, error) {
	consolidated := consolidationLines(reply)
	if len(consolidated) == 0 {
		return nil, fmt.Errorf("session: consolidator returned no facts")
	}
	// THE CONSERVATIVE LAW, mechanically. Merging and dropping can only make a
	// list shorter; a longer list is a consolidator that invented something,
	// whatever it says it did.
	if len(consolidated) > len(facts) {
		return nil, fmt.Errorf("session: consolidator returned %d facts for %d — it may only merge and drop",
			len(consolidated), len(facts))
	}
	if !consolidationHoldsTimes(facts, consolidated) {
		return nil, fmt.Errorf("session: consolidator reworded a time expression")
	}
	return consolidated, nil
}

// consolidationLines takes the facts out of a reply.
//
// A line that is not a list item is DROPPED rather than kept or refused: the
// file's unit is the "- …" line, the instruction asks for those and nothing
// else, and the one thing a model reliably adds against that instruction is a
// sentence of preamble. Keeping it would make "Here is the consolidated list:"
// a fact the session remembers about the person.
func consolidationLines(reply string) []string {
	var facts []string
	for _, raw := range strings.Split(reply, "\n") {
		trimmed := strings.TrimSpace(raw)
		if !strings.HasPrefix(trimmed, "- ") && !strings.HasPrefix(trimmed, "* ") {
			continue
		}
		if line := memoryLine(strings.TrimSpace(trimmed[2:])); line != "" {
			facts = append(facts, line)
		}
	}
	return facts
}

func identicalFacts(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

// ── the time law, mechanically ──────────────────────────────────────────────

// temporalPatterns are the shapes a time expression takes in a line somebody
// wrote about their work: absolute dates, clock times, years, durations,
// quarters, weekdays, month-and-day, version numbers, and the relative phrases
// ("since Tuesday", "last quarter") that carry a date without spelling one.
//
// They are deliberately GENEROUS. A false positive costs a pass that was
// refused for being too creative near something that looked like a time; a
// false negative is a date quietly rewritten, which is the failure this whole
// mechanism exists to prevent — the asymmetry Sleeping Agent measured.
var temporalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\b\d{4}-\d{1,2}-\d{1,2}\b`),    // 2026-08-15
	regexp.MustCompile(`\b\d{1,2}/\d{1,2}/\d{2,4}\b`),  // 15/08/2026
	regexp.MustCompile(`\b\d{1,2}:\d{2}(?::\d{2})?\b`), // 14:30
	regexp.MustCompile(`\bv?\d+\.\d+(?:\.\d+)*\b`),     // v2.1, 1.20.3
	regexp.MustCompile(`\b(?:19|20)\d{2}\b`),           // 1998, 2026
	regexp.MustCompile(`(?i)\bQ[1-4]\b`),               // Q3
	regexp.MustCompile(`(?i)\b\d+\s*(?:sec|min|hr|second|minute|hour|day|week|fortnight|month|quarter|year|sprint)s?\b`),
	regexp.MustCompile(`(?i)\b(?:mon|tues|wednes|thurs|fri|satur|sun)day\b`),
	regexp.MustCompile(`(?i)\b(?:january|february|march|april|june|july|august|september|october|november|december)\b`),
	regexp.MustCompile(`(?i)\b(?:jan|feb|mar|apr|may|jun|jul|aug|sept?|oct|nov|dec)\.?\s+\d{1,2}\b`),
	regexp.MustCompile(`(?i)\b(?:yesterday|today|tomorrow|tonight|recently)\b`),
	regexp.MustCompile(`(?i)\b(?:since|until)\s+\S+`),
	regexp.MustCompile(`(?i)\b(?:last|next|this|past|coming)\s+(?:week|month|year|quarter|sprint|decade|release|cycle|monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`),
}

// consolidationHoldsTimes reports whether the result spells every time it
// carries the way the input spelled it — the same characters, up to case. Case
// is the one difference allowed, because a capital at the start of a merged line
// is a sentence, not a different date.
//
// This is the mechanical half of the time law, and its scope is precise: it
// catches a time that was REWRITTEN or INVENTED — "2026-08-15" returned as
// "Aug 15", "since Tuesday" returned as "recently", a range folded out of two
// dates — because none of those appear character-for-character in the input.
// It cannot catch a temporal fact that was dropped ENTIRELY, and it must not
// try: dropping a superseded fact is one of the two moves the consolidator is
// for, and a check that forbade it would forbid the pass. The prompt is what
// carries the rest of the law; this is the floor under it.
func consolidationHoldsTimes(facts, consolidated []string) bool {
	source := strings.ToLower(strings.Join(facts, "\n"))
	for _, fact := range consolidated {
		for _, expression := range temporalExpressions(fact) {
			if !strings.Contains(source, strings.ToLower(expression)) {
				return false
			}
		}
	}
	return true
}

// temporalExpressions is every time expression in one line, in the line's own
// characters.
func temporalExpressions(line string) []string {
	var found []string
	for _, pattern := range temporalPatterns {
		found = append(found, pattern.FindAllString(line, -1)...)
	}
	return found
}
