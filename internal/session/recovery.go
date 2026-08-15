package session

// Revert-then-refix: the recovery move a stuck turn is offered, and the ledger
// that makes it possible.
//
// The loop detector (looped.go) can say a turn is going in circles. Until this
// file, everything it could then do was WORDS: a note to the model, and past the
// ceiling a question to the person whose two answers were also notes. Words are
// the right first move — a model told what it has been doing often stops doing it
// — and they are a poor third one, because by the third repetition the thing
// standing between the model and a working approach is usually not advice. It is
// the half-finished edit it made on the way in, which it is now reading back,
// reasoning about, and editing again.
//
// PMCoder (https://arxiv.org/abs/2608.06811) names the move: on a deterministic
// stuck signal, RESTORE THE EDITED FILES AND RE-FIX FROM A CLEAN BASE. The model
// keeps what it learned — the transcript is untouched, every result it read is
// still there — and loses only the mutations that learning was made of. That
// asymmetry is the point. Context is cheap to keep and expensive to rebuild; a
// broken working tree is the opposite.
//
// ── WHO DECIDES ──
//
// The person, always, and only when one is there. This is destructive: it
// deletes files and throws away edits, on the judgement of a counter that says
// three. So it rides the escalation that already exists — the third repetition
// in prompt mode goes to the consent lane — and it rides it as the QUESTION's
// own offer rather than as something that happens when nobody answers. Nothing
// here runs on a timer, in a task node, or in a headless session; every one of
// those keeps the note it got before this file existed.
//
// ── WHAT IT CAN PUT BACK, AND WHAT IT SAYS INSTEAD ──
//
// Two kinds of change, two mechanisms, and one honest refusal:
//
//   - A file this turn CREATED is deleted (os.Remove). The ledger knows it was
//     created because pre-action stat'd the path before the write ran, which is
//     the only moment anybody could have known.
//   - A file this turn MODIFIED and git tracks is restored (git checkout --).
//   - Anything else — no repository, an untracked file, a path outside the
//     workspace — is NOT touched and is NAMED: "not under git — restore by
//     hand", in the answer the model reads and the person sees. A recovery that
//     quietly did nothing about half the damage would be worse than one that was
//     never offered, because the model would re-attempt believing the base is
//     clean.
//
// ── WHY THE LEDGER IS NOT THE JOURNAL ──
//
// The session file records tool calls and their results; the paths are in there,
// in the arguments, and a reader could mine them. This does not, for the reason
// PMCoder grounds memory in execution rather than narration: what matters is
// which calls ACTUALLY RAN AND SUCCEEDED and what the disk looked like before
// each one — a fact that exists for exactly one instant, at pre-action, and can
// never be recovered from the record afterwards. So the ledger is written from
// the two hooks that straddle the execution, and it holds nothing else.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// mutatingTools are the belt hands whose effect on the disk is a known path.
//
// It is a LIST, exactly as the early-start law's is (loop.go), and it is short
// for the same reason: membership is a claim this file has to be able to make
// good on. bash is deliberately absent — a shell command's effects are whatever
// it did, and a ledger that guessed at them would offer to revert a set of files
// that is not the set that changed.
var mutatingTools = map[string]bool{
	"edit":  true,
	"write": true,
}

// fileChange is one file this turn touched.
type fileChange struct {
	// path is absolute: the ledger's key, and what os.Remove is given.
	path string
	// shown is the path as a person reads it — relative to the workspace when it
	// is under it, which is how the model asked for it and how the tool rows
	// already draw it.
	shown string
	// created says the file did not exist when the call that made it was
	// approved. A created file is deleted by a revert; a modified one is
	// restored, and the two are not interchangeable in either direction.
	created bool
}

// fileLedger is one turn's record of what changed on disk.
//
// It is written from two hooks and read from a third, and the batch's calls run
// in parallel, so it holds its own lock. It is never read with a.mu held: the
// revert runs git, and a session lock held across a subprocess is the lock
// Interrupt could not take.
type fileLedger struct {
	mu sync.Mutex
	// existed is what pre-action saw: absolute path → the file was already
	// there. The FIRST sighting stands, so a file created and then edited five
	// times is still a created file.
	existed map[string]bool
	// order is the paths in first-touch order, and changes is what is known
	// about each. Two structures rather than a map with an index because the
	// answer a person reads names files in the order the turn touched them.
	order   []string
	changes map[string]fileChange
}

func newFileLedger() *fileLedger {
	return &fileLedger{
		existed: make(map[string]bool, 4),
		changes: make(map[string]fileChange, 4),
	}
}

// note records what pre-action saw. Only the first sighting of a path is kept.
func (l *fileLedger) note(path string, existed bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, known := l.existed[path]; known {
		return
	}
	l.existed[path] = existed
}

// touched records one successful mutation. The created flag comes from the
// pre-action sighting; a path with no sighting at all is recorded as MODIFIED,
// which is the conservative reading — a revert restores it if git can and says
// so if it cannot, and in neither case does it delete a file whose history
// nobody watched.
func (l *fileLedger) touched(path, shown string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, already := l.changes[path]; already {
		return
	}
	existed, known := l.existed[path]
	l.order = append(l.order, path)
	l.changes[path] = fileChange{path: path, shown: shown, created: known && !existed}
}

// list is the turn's changes in first-touch order.
func (l *fileLedger) list() []fileChange {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]fileChange, 0, len(l.order))
	for _, path := range l.order {
		out = append(out, l.changes[path])
	}
	return out
}

// ── the ledger as a hook citizen ────────────────────────────────────────────

// changeLedger is the ledger's three hooks: it opens at episode-init, stats the
// target at pre-action, and records the successful ones at post-feedback.
//
// It NEVER vetoes. A citizen that both watches the environment and can stop a
// call is a citizen whose bookkeeping bug is an outage; this one's worst failure
// is an offer that names the wrong number of files.
type changeLedger struct{ agent *Agent }

func (*changeLedger) Name() string { return "changes" }

func (*changeLedger) EpisodeInit(ep *episode) { ep.changes = newFileLedger() }

// PreAction takes the one measurement that cannot be taken later: whether the
// file exists BEFORE the call that is about to write it.
func (c *changeLedger) PreAction(_ context.Context, ep *episode, _ *eventHub, call ai.ToolCall) (ai.ToolCall, toolResult, bool) {
	if path, _, ok := c.agent.mutatingPath(call); ok {
		_, err := os.Stat(path)
		ep.changes.note(path, err == nil)
	}
	return call, toolResult{}, true
}

// PostFeedback records the calls that actually changed something: a mutation
// that failed changed nothing, and a revert that "restored" a file no call ever
// wrote would be the recovery move causing the damage.
func (c *changeLedger) PostFeedback(_ context.Context, ep *episode, _ *eventHub, calls []ai.ToolCall, results []toolResult) {
	for index, call := range calls {
		if index >= len(results) || results[index].isError {
			continue
		}
		if path, shown, ok := c.agent.mutatingPath(call); ok {
			ep.changes.touched(path, shown)
		}
	}
}

// mutatingPath reports the absolute path one call is about to change, and the
// same path as a person reads it.
//
// The resolution mirrors bare's resolveToCwd (internal/exec/bare/tools.go) for
// the ordinary shapes — an absolute path is itself, a relative one hangs off the
// workspace — and declines everything else: no workspace, no path argument,
// arguments that do not parse. A path this cannot resolve is a change this
// cannot offer to revert, which is the correct amount of ambition.
func (a *Agent) mutatingPath(call ai.ToolCall) (string, string, bool) {
	if !mutatingTools[call.Function.Name] {
		return "", "", false
	}
	workspace := strings.TrimSpace(a.config.Workspace)
	if workspace == "" {
		return "", "", false
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		return "", "", false
	}
	path := strings.TrimSpace(args.Path)
	if path == "" {
		return "", "", false
	}
	if !filepath.IsAbs(path) {
		return filepath.Clean(filepath.Join(workspace, path)), filepath.ToSlash(filepath.Clean(path)), true
	}
	path = filepath.Clean(path)
	if relative, err := filepath.Rel(workspace, path); err == nil && !strings.HasPrefix(relative, "..") {
		return path, filepath.ToSlash(relative), true
	}
	return path, path, true
}

// ── the answer ──────────────────────────────────────────────────────────────

// RecoveryChoice is one answer to the stuck question. It is the third option a
// bool cannot carry: a person looking at a turn that has repeated itself three
// times has three sensible things to say, and two of them are not "yes".
type RecoveryChoice string

const (
	// RecoveryRevert restores what this turn changed and tells the model to
	// re-attempt from the clean base.
	RecoveryRevert RecoveryChoice = "revert"
	// RecoveryContinue vouches for the approach: the model keeps going, having
	// been asked to say what it expects to be different.
	RecoveryContinue RecoveryChoice = "continue"
	// RecoveryStop ends the attempt: the model says what it found, what is
	// blocking it, and what it needs.
	RecoveryStop RecoveryChoice = "stop"
)

// ResolveRecovery answers one stuck question with all three options available.
//
// A surface that has only yes and no answers with [Agent.ResolveConsent] and the
// mapping in [Agent.askAboutLoop] applies: yes takes the offer the question
// made, no stops. This is the wider door, for a surface that draws three keys.
//
// An id nobody is waiting on is ignored, exactly as ResolveConsent's is.
func (a *Agent) ResolveRecovery(id uint64, choice RecoveryChoice) {
	switch choice {
	case RecoveryRevert, RecoveryContinue, RecoveryStop:
	default:
		// An unknown answer is read as the narrowest one, for the reason an
		// unknown consent scope is: a typo must never widen what happens, and
		// "stop and explain" is the answer that changes nothing on disk.
		choice = RecoveryStop
	}
	a.deliverConsent(id, consentAnswer{allow: choice == RecoveryRevert, choice: choice})
}

// ── the offer ───────────────────────────────────────────────────────────────

// recoveryOffer is the revert as it will be put to the person: what would be
// restored, and whether there is anything to restore at all.
type recoveryOffer struct {
	changes []fileChange
}

// available reports whether there is a move to offer. With nothing changed the
// question is the one it always was — advice or stop — because "revert the 0
// files this turn touched" is an offer of nothing dressed as a choice.
func (o recoveryOffer) available() bool { return len(o.changes) > 0 }

// rule is the question's own words, in the slot the approval policy's wording
// usually occupies (looped.go's loopRule).
func (o recoveryOffer) rule(n nudge) string {
	if !o.available() {
		return loopRule(n)
	}
	return fmt.Sprintf("%s — revert the %s this turn touched and retry from clean?",
		loopRule(n), countedFiles(len(o.changes)))
}

func countedFiles(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}

// offerFor is what a revert would do right now.
func (ep *episode) offerFor() recoveryOffer {
	if ep == nil {
		return recoveryOffer{}
	}
	return recoveryOffer{changes: ep.changes.list()}
}

// ── the escalation ──────────────────────────────────────────────────────────

// askAboutLoop puts the loop to the person as an ordinary consent question,
// carrying the recovery offer when there is one, and turns whichever answer
// comes back into something the model reads.
//
// TWO OF THE THREE ANSWERS ARE NOTES, and that is deliberate: this machinery
// observes a turn, it does not drive one. Only the revert acts, and only because
// somebody said so about a named set of files.
//
// THE BINARY MAPPING. A surface with yes and no keys (internal/tui3 today) says
// yes to THE QUESTION AS ASKED — which is the revert when one was offered, and
// "carry on" when none was, because that is what the sentence the person read
// said. No is stop in both cases. A surface that draws a third key calls
// [Agent.ResolveRecovery] and says exactly which of the three it means.
//
// An interrupt or a turn that ends while the question is open leaves nothing
// behind — the same thing an unanswered consent request already does.
func (a *Agent) askAboutLoop(ctx context.Context, hub *eventHub, ep *episode, looping nudge) {
	offer := ep.offerFor()
	// The question NEVER writes a consent memo. "Don't ask me again" is an answer
	// about a TOOL, and this question is not about a tool: a person answering
	// "yes, and stop asking" to a stuck prompt would otherwise have silently
	// approved every later call to whatever the model happened to be repeating.
	answer, err := a.askAnswer(ctx, hub, looping.call, approval.Decision{
		Action: approval.ActionPrompt,
		Rule:   offer.rule(looping),
	}, false)
	if err != nil {
		return
	}

	choice := answer.choice
	if choice == "" {
		// A binary surface answered. Yes means the offer the question made.
		switch {
		case answer.allow && offer.available():
			choice = RecoveryRevert
		case answer.allow:
			choice = RecoveryContinue
		default:
			choice = RecoveryStop
		}
	}
	if choice == RecoveryRevert && !offer.available() {
		// Nothing to put back — a surface asking for a revert of nothing gets the
		// honest version rather than a note claiming files were restored.
		choice = RecoveryContinue
	}

	switch choice {
	case RecoveryRevert:
		a.enqueueSteering(a.revertNote(offer))
	case RecoveryContinue:
		a.enqueueSteering("[stuck] I asked the person about this repetition and they said to carry on. " +
			"Keep going, but say what you expect to be different this time.")
	default:
		a.enqueueSteering("[stuck] I asked the person about this repetition and they said no. " +
			"Stop repeating " + looping.tool + ": say what you have found, what is blocking you, " +
			"and what you need from them.")
	}
}

// revertNote performs the revert and writes what happened, as the model reads
// it. The note is the journal entry for the person's choice: it says who
// decided, what moved, and what did not.
func (a *Agent) revertNote(offer recoveryOffer) string {
	outcome := a.revert(offer)

	var note strings.Builder
	note.WriteString("[stuck] I asked the person about this repetition and they chose to revert. ")
	if done := append(append([]string{}, outcome.restored...), outcome.removed...); len(done) > 0 {
		note.WriteString(fmt.Sprintf("The turn's changes to %s were reverted; "+
			"re-attempt from the clean base with what you learned.", strings.Join(done, ", ")))
	} else {
		note.WriteString("Nothing could be put back automatically; " +
			"re-attempt from what is on disk now, with what you learned.")
	}
	if len(outcome.manual) > 0 {
		note.WriteString(" NOT reverted — not under git, restore by hand: " +
			strings.Join(outcome.manual, ", ") + ".")
	}
	if len(outcome.failed) > 0 {
		note.WriteString(" Could not be reverted: " + strings.Join(outcome.failed, ", ") + ".")
	}
	return note.String()
}

// ── the revert ──────────────────────────────────────────────────────────────

// revertOutcome is what a revert managed, in the four categories a person and a
// model both need kept apart.
type revertOutcome struct {
	// restored is the tracked files git put back.
	restored []string
	// removed is the files this turn created and this revert deleted.
	removed []string
	// manual is the files nothing here could restore — no repository, untracked,
	// or outside the workspace — left exactly as they are and named out loud.
	manual []string
	// failed is the ones it tried and could not, with the reason.
	failed []string
}

// revert restores what the offer named.
//
// THE WORKSPACE IS THE BOUNDARY. A path outside it is never deleted and never
// checked out, whatever the ledger says: the model asked for it with an absolute
// path, the turn wrote it, and undoing a write outside the directory this
// session was pointed at is not a recovery move, it is a second incident.
//
// The git commands are the plumbing ones and they run in the workspace. A
// modified file is restored from the INDEX (git checkout -- <path>), which is
// where the file was before this turn edited it; restoring from HEAD instead
// would throw away staged work the person did before the session started, which
// this was never given permission to touch.
func (a *Agent) revert(offer recoveryOffer) revertOutcome {
	var outcome revertOutcome
	workspace := strings.TrimSpace(a.config.Workspace)
	if workspace == "" {
		for _, change := range offer.changes {
			outcome.manual = append(outcome.manual, change.shown)
		}
		return outcome
	}
	_, repository := repositoryRoot(workspace)

	for _, change := range offer.changes {
		relative, inside := insideWorkspace(workspace, change.path)
		if !inside {
			outcome.manual = append(outcome.manual, change.shown)
			continue
		}
		if change.created {
			if err := os.Remove(change.path); err != nil && !os.IsNotExist(err) {
				outcome.failed = append(outcome.failed, change.shown+" ("+err.Error()+")")
				continue
			}
			outcome.removed = append(outcome.removed, change.shown)
			continue
		}
		if !repository || !tracked(workspace, relative) {
			outcome.manual = append(outcome.manual, change.shown)
			continue
		}
		if out, err := git(workspace, "checkout", "--", relative); err != nil {
			outcome.failed = append(outcome.failed, change.shown+" ("+firstLine(out)+")")
			continue
		}
		outcome.restored = append(outcome.restored, change.shown)
	}
	return outcome
}

// insideWorkspace reports the workspace-relative path, and whether the file is
// under the workspace at all.
func insideWorkspace(workspace, path string) (string, bool) {
	relative, err := filepath.Rel(workspace, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return relative, true
}

// tracked reports whether git knows this path. An untracked file has no version
// to be restored TO, so it is named for the person rather than deleted: it may
// be a file they wrote themselves before the session started.
func tracked(workspace, relative string) bool {
	_, err := git(workspace, "ls-files", "--error-unmatch", "--", relative)
	return err == nil
}
