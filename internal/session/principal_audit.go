package session

// THE TERMINAL AUDIT: the last thing an unattended session does before it is
// allowed to say the ask is finished.
//
// ── WHAT WAS MISSING ────────────────────────────────────────────────────────
//
// Every reading this engine takes of "is it done" is a reading of what the
// SESSION SAID. The mark reader is shown a digest of the transcript; a task's
// auditor is shown that node's own acceptance and that node's own claim. Nobody
// ever looked at the tree afterwards, and nobody ever looked at what the session
// had left lying around it. Two measured runs held a good result and were
// zeroed at the end by scratch data files the session had written beside the
// deliverable and never picked up; a third ended on a tree that did not build,
// with the conversation confidently finished.
//
// So a Steward gets two readings a person would have taken for themselves:
//
//   - THE CHECKS THE WORK ITSELF NAMED, READ FROM CLEAN. Not a list this file
//     knows — task_checks.go already settled that law, and the same
//     [declaredChecks] reading is used here so a session and its nodes can never
//     disagree about what a check is. "From clean" means A FRESH PROCESS IN THE
//     DELIVERABLE TREE: no shell the turn had open, no environment a tool call
//     had edited, nothing cached from the run. It is what a person typing the
//     command in a new terminal would get. An answer already taken over the
//     same unchanged tree is that reading and is reused rather than paid for
//     again.
//
//   - AND A RECONCILIATION OF EVERYTHING THE SESSION CREATED. Every path is
//     either inside the deliverable tree — where it is part of the answer — or
//     it is scratch, and scratch is removed and written down.
//
// ── THE LAW THIS FILE MUST NOT BREAK ────────────────────────────────────────
//
// NOTHING THE SESSION DID NOT CREATE IS EVER TOUCHED. Not a file it modified,
// not a file it read, not a directory it happened to write into. The ledger
// this walks holds only paths whose non-existence was MEASURED before the call
// that made them (recovery.go's [fileLedger] takes that measurement, and it is
// the only moment it can be taken); a path with no such measurement is recorded
// as modified and never reaches here.
//
// AND A PERSON'S SESSION DELETES NOTHING. [Person] is offered the list and that
// is all: somebody who is sitting there can see their own directory, and a
// harness quietly removing files behind them is the opposite of what the
// emptiness of that implementation means everywhere else.

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/verify"
)

const (
	// sessionCheckWindow bounds ONE check. It is the auditor's own deadline
	// (task_audit.go's auditDeadline) rather than a second number: a check is a
	// check whoever is running it, and two answers to "how long may a build
	// take" is how a session and its nodes come to disagree about a tree.
	sessionCheckWindow = 5 * time.Minute
	// sessionCheckCount is how many checks are run. It is [auditCheckCount],
	// for sessionCheckWindow's reason.
	sessionCheckCount = auditCheckCount
	// sessionCheckTail is how much of a failed check's output is kept. It is
	// read by a model as the reason to carry on, so it is the END of the output
	// ([checkpointResultTail]'s law: what a check concluded is in its last
	// lines).
	sessionCheckTail = 1200
)

// sessionChecks are the commands this session's work names, in the order a
// person would read them.
//
// NOTHING HARVESTED FROM THE ASK IS EXECUTED AGAINST THE TREE. The acceptance
// the session composed is the whole work's own document, while the ask is the
// person's account of what they saw; a pasted reproduction is evidence about
// the bug, not a promise that repeating it proves the work. In one measured tox
// session the baseline check harvested and ran `chmod 000 tox.ini` from the
// pasted issue. When nobody could write an acceptance, [routeAcceptance] frames
// the person's words as one; that fallback is still read as the ask rather than
// allowed through in the acceptance's coat. The nodes' own doors are gathered
// below.
//
// AND EVERY UNIT OF WORK THAT LANDED CONTRIBUTES ITS OWN DOOR, which is BOTH of
// task_checks.go's sources at once ([auditDoorFor]): the checks that node's
// document named AND the ones its worker actually ran. The second is the one
// worth having — a worker that hammered a build for an hour has said what the
// check is more clearly than any document — and reading it through the same
// door the node's auditor used is what keeps the session and its nodes checking
// the same things.
//
// IT NAMES NO COMMAND OF ITS OWN. A list this file knew would be the constant
// task_checks.go was written to replace, and it would be wrong in exactly the
// places this build is meant to be general: the work says how it is checked.
//
// AND EVERY ONE OF THEM COMES BACK AS A COMMAND RATHER THAN AS A SPAN OF PROSE
// ([invocableChecks]). What is harvested here is run under a shell, so a check
// that names a file has to be opened the way that FILE opens — which is the fact
// a node's own door already computes and this harvest used to throw away.
func (a *Agent) sessionChecks() []string {
	principal := a.who()
	// The deliverable tree is where [Agent.runSessionChecks] will start every one
	// of these, so it is the directory a declared check has to be runnable in —
	// the same tree, asked the same question, as the one the checks are run in.
	tree := a.deliverableTree()
	acceptance := principal.Acceptance()
	from := checksFromWork
	if acceptanceIsAsk(acceptance) {
		from = checksFromAsk
	}
	checks := invocableChecks(tree, declaredChecks(acceptance, tree, from))
	graph := a.tasker()
	if graph == nil {
		return trimChecks(checks)
	}
	// The nodes are taken under the graph lock and read without it, for
	// [Agent.landings]'s reason: every accessor below takes that same lock.
	graph.mu.Lock()
	nodes := make([]*TaskNode, 0, len(graph.order))
	for _, id := range graph.order {
		if node := graph.nodes[id]; node != nil {
			nodes = append(nodes, node)
		}
	}
	graph.mu.Unlock()
	for _, node := range nodes {
		if !node.stateNow().settled() {
			continue
		}
		checks = appendChecks(checks,
			invocableChecks(tree, auditDoorFor(node, auditPlace{ground: tree, ran: tree}).checks))
	}
	return trimChecks(checks)
}

// acceptanceIsAsk recognizes the one frame [routeAcceptance] writes when no
// one could compose a done-condition and the person's own words have to stand.
// The frame is the source of truth, so this reading cannot drift from its only
// writer into treating a pasted reproduction as the work's promise.
//
// THE PREFIX IS LOAD-BEARING. Every writer of an acceptance either hands the
// frame over untouched or trims its whitespace, so the frame is still at the
// front by the time this reads it — [Steward.setAcceptance] only trims, the
// node's own path is verbatim, restore is verbatim, and [TaskNode.checkTexts]
// deliberately tests the raw acceptance rather than the composed one. A future
// composer that puts ANYTHING in front of the frame turns this silently false
// and puts the pasted reproduction back on the door.
func acceptanceIsAsk(acceptance string) bool {
	return strings.HasPrefix(acceptance, routeAskAcceptance)
}

// invocableChecks turns one source's declared spans into the commands that
// actually START them, and DROPS THE ONES NOTHING CAN START.
//
// THE READING IS [checkCommand]'S AND NOT A SECOND ONE. A node's auditor is told
// how to open a file check off the very same facts (task_checks.go), so a session
// and its nodes cannot come to disagree about what running a check means — which
// is the law the whole of [Agent.sessionChecks] is built on.
//
// A SPAN THAT CANNOT BE INVOKED IS NOT A FAILING CHECK, IT IS NOT A CHECK. It is
// dropped here rather than run and reported, because "does not pass" is a
// sentence about something that RAN, and a span that never could run would repeat
// that sentence for the life of the session ([Remains.unmet] re-reads the same
// list at the end of every turn).
func invocableChecks(tree string, checks []string) []string {
	out := make([]string, 0, len(checks))
	for _, check := range checks {
		if command := checkCommand(tree, check); command != "" {
			out = append(out, command)
		}
	}
	return out
}

// trimChecks bounds the list and drops what a check cannot be. The vouching is
// [approval.Vouchable]'s, reached through the same [commandLike] reading
// declaredChecks already applied — this only holds the count.
func trimChecks(checks []string) []string {
	if len(checks) > sessionCheckCount {
		return checks[:sessionCheckCount]
	}
	return checks
}

// runSessionChecks reads the session's declared checks from clean and reports
// what each one said, reusing an answer already taken over the same tree state.
//
// EACH ONE IS ITS OWN PROCESS, in the deliverable tree, with a deadline of its
// own. There is no shell state carried between them and none carried in from
// the turn, which is the whole of what "from clean" can honestly mean at the
// level of a whole session — a session's deliverable is the tree as it now
// stands, with every unit of work merged into it, and re-cutting that tree from
// a branch would be auditing something nobody asked for.
//
// A CHECK THAT COULD NOT BE STARTED IS A CHECK THAT DID NOT PASS. The
// alternative is a session that declares itself finished because its build
// command was misspelled, which is the failure this whole road exists to catch
// pointing the other way.
func (a *Agent) runSessionChecks(ctx context.Context, checks []string) []CheckRun {
	tree := a.deliverableTree()
	out := make([]CheckRun, 0, len(checks))
	for _, check := range checks {
		if ctx.Err() != nil {
			return out
		}
		run, _ := a.checkNow(ctx, tree, check)
		out = append(out, run)
	}
	return out
}

// checkNow is the one road every session-side check execution takes. An answer
// over the tree's present state is returned without starting a process; a fresh
// run records its pace even when the command itself moved the tree and made its
// answer unusable.
func (a *Agent) checkNow(ctx context.Context, tree, check string) (CheckRun, bool) {
	memory := a.sessionCheckMemories()
	if memory != nil {
		// THE SERIALIZATION INCLUDES THE PROCESS. Two ending readers over one
		// tree must not both miss the answer before either has had time to store
		// it, or the memory would reproduce the duplicate run under contention.
		memory.execution.Lock()
		defer memory.execution.Unlock()
	}
	state := treeStateNow(tree)
	if answer, ok := memory.answerFor(tree, check, state); ok {
		return answer, false
	}
	window, fits := a.checkWindow(tree, check)
	if !fits {
		return CheckRun{Command: check, Unread: notEnoughTimeToRun + check}, false
	}
	started := time.Now()
	run := runOneCheck(ctx, tree, check, window)
	took := time.Since(started)
	after := treeStateNow(tree)
	moved := after != state
	memory.remember(tree, check, state, run, took, moved)
	return run, moved
}

// checkWindow decides whether one session-side check fits the wall-bounded
// duration. A wall nobody named preserves the five-minute window exactly; a
// named wall refuses a reading too short to be useful.
func (a *Agent) checkWindow(tree, check string) (time.Duration, bool) {
	steward := a.steward()
	if steward == nil {
		return sessionCheckWindow, true
	}
	budget := steward.Budget()
	if budget.Wall == 0 {
		return sessionCheckWindow, true
	}
	window := a.wallBoundedWindow(sessionCheckWindow)
	need := verify.ShortestUsefulReading
	if pace, remembered := a.sessionCheckMemories().paceFor(tree, check); remembered {
		need = pace
	}
	return window, window >= need
}

// wallBoundedWindow is the one place a session-side reading consults the wall.
// It preserves the caller's window when there is no Steward or no wall, and
// otherwise gives the reading no more than the hours that remain on the
// Steward's own clock.
func (a *Agent) wallBoundedWindow(window time.Duration) time.Duration {
	steward := a.steward()
	if steward == nil {
		return window
	}
	budget := steward.Budget()
	if budget.Wall == 0 {
		return window
	}
	left, _ := budget.Left()
	return min(window, left)
}

// runOneCheck also serves task-baseline callers, whose context already carries
// their checking deadline. Session callers supply the smaller wall-sized window.
func runOneCheck(ctx context.Context, tree, check string, windows ...time.Duration) CheckRun {
	window := sessionCheckWindow
	if len(windows) > 0 {
		window = windows[0]
	}
	ctx, done := context.WithTimeout(ctx, window)
	defer done()
	command := exec.CommandContext(ctx, "bash", "-c", check)
	command.Dir = tree
	output, err := command.CombinedOutput()
	// A COMMAND THAT RAN AND A COMMAND THAT COULD NOT BE RUN ARE DIFFERENT NEWS.
	// An exit status — whatever it was — is the shell having executed the thing
	// and answered; anything else is the command not being there, the window
	// closing over it, or the process never starting, and none of those is a
	// reading of the tree ([CheckRun.Ran]).
	return CheckRun{
		Command: check,
		Passed:  err == nil,
		Ran:     checkActuallyRan(ctx, err),
		Tail:    checkTail(string(output)),
	}
}

// checkActuallyRan answers [CheckRun.Ran]: did this command START AND FINISH,
// or did something stop it from ever answering?
//
// THE CLOCK IS ASKED FIRST, because a command the window killed comes back
// wearing an exit status like any other — the signal that stopped it IS an exit
// — and a deadline read as a failing check is exactly the silence this exists to
// prevent.
//
// AND THE SHELL'S TWO "I COULD NOT RUN IT" CODES ARE READ AS WHAT THEY ARE. 127
// is a command that is not there and 126 is one that would not execute; both are
// bash answering about ITSELF rather than the check answering about the tree, and
// a baseline that wrote either down as already-red would silence a real failure
// on that command the day somebody installed it. They are exit codes, read off
// the typed error — not words out of anybody's output.
func checkActuallyRan(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	if err == nil {
		return true
	}
	var exited *exec.ExitError
	if !errors.As(err, &exited) {
		return false
	}
	switch exited.ExitCode() {
	case 126, 127:
		return false
	}
	return true
}

// checkTail keeps the END of what a check printed, bounded. It is
// [checkpointResultTail]'s rule with this file's own bound: a check kept from
// the head would show a reader that a suite had started and never that it had
// failed.
func checkTail(output string) string {
	output = strings.TrimSpace(output)
	if len(output) <= sessionCheckTail {
		return output
	}
	cut := len(output) - sessionCheckTail + len("…")
	for cut < len(output) && !utf8RuneStart(output[cut]) {
		cut++
	}
	return "…" + output[cut:]
}

// deliverableTree is where the answer lives: the session's workspace.
//
// IT IS THE WORKSPACE AND NOT THE SESSION FOLDER. The folder holds the
// harness's own litter — transcripts, job logs, the worktrees the units of work
// were built in (place.go) — and none of it is the deliverable; the workspace is
// the directory the person opened aforge in and the one every relative path a
// tool was given resolves against.
func (a *Agent) deliverableTree() string {
	return strings.TrimSpace(a.config.Workspace)
}

// ── WHAT THE SESSION LEFT LYING ABOUT ───────────────────────────────────────

// rememberCreated folds one created file into the session's own ledger and
// writes it down, so a resumed session still knows what it made.
//
// IT KEEPS ONLY CREATED FILES. A modified one is somebody else's file with our
// changes in it, and nothing in this build may remove one — recording it here
// would put it one bug away from being swept.
func (a *Agent) rememberCreated(change fileChange) {
	if !change.created || strings.TrimSpace(change.path) == "" {
		return
	}
	a.mu.Lock()
	for _, known := range a.createdFiles {
		if known.path == change.path {
			a.mu.Unlock()
			return
		}
	}
	a.createdFiles = append(a.createdFiles, change)
	file := a.file
	a.mu.Unlock()
	file.appendCreated(journalCreated{Path: change.path, Shown: change.shown})
}

// createdList is everything this session made, in first-touch order.
func (a *Agent) createdList() []fileChange {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]fileChange(nil), a.createdFiles...)
}

// rememberChange folds one write the turn's ledger saw into the session's own:
// a file that was not there before goes to the created ledger the tidy may act
// on, a file that was goes to the changed ledger nothing acts on.
func (a *Agent) rememberChange(change fileChange) {
	if change.created {
		a.rememberCreated(change)
		return
	}
	a.rememberChanged(change)
}

// rememberChanged keeps one modified file — AND WHAT WAS IN IT BEFORE
// ([fileChange.before]) — so the session knows it put work on the deliverable
// with its own hands, and can still tell later whether that work is there.
//
// THE FIRST SIGHTING OF A PATH IS THE ONE THAT STANDS, which is why the loop
// below returns rather than overwriting. A turn's ledger is minted fresh at
// episode-init, so the second turn to edit a file digests the FIRST turn's
// result as its before; keeping the earliest entry keeps the digest that was
// taken before this session had written anything there at all.
//
// IT IS NEVER WRITTEN TO THE JOURNAL'S CREATED LINE, whose whole value is the
// word created ([journalCreated]), and it is not journaled at all: the only
// question it answers is asked of the running session ([Remains.Made]), and a
// session resumed from its file has a tree and a graph to read instead.
//
// Measured (#513): a cell fixed its issue with one edit to a file the project
// already had, went green, and was told `nothing has been finished yet` at every
// ending until the standstill stopped it — the created ledger was empty, because
// nothing had been created.
func (a *Agent) rememberChanged(change fileChange) {
	if change.created || strings.TrimSpace(change.path) == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, known := range a.changedFiles {
		if known.path == change.path {
			return
		}
	}
	a.changedFiles = append(a.changedFiles, change)
}

// createdInDeliverable says whether a file this session created is under the
// deliverable tree, is still a regular file there, AND HAS CONTENT IN IT. A
// path outside the tree is scratch, not the work; a path that has since gone or
// become a directory is not the file the session made; and an empty file holds
// none of the work the run meant to put there.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// A run wrote `fix.py`, and a later one of its own steps blanked it. The
// created ledger still held the path, so Made was true and the door said the
// work was made over a tree holding an empty file.
//
// A CREATED FILE IS MEASURED BY SIZE, NOT BY ITS BEFORE-DIGEST. Its before is
// "" because it was absent, while the digest of an empty regular file is the
// sha256 of zero bytes rather than "" ([fileDigest]); comparing those values
// would call the empty file changed and silently restore this failure.
func (a *Agent) createdInDeliverable() bool {
	tree := a.deliverableTree()
	if tree == "" {
		return false
	}
	for _, change := range a.createdList() {
		if !change.created || !underTree(tree, change.path) {
			continue
		}
		// THE PATH IS RESOLVED BEFORE IT IS READ for the same reason as in
		// [Agent.changedInDeliverable]: a link under the workspace to an in-tree
		// regular file is the file it points at, while [underTree] has already
		// excluded a link that points out of the workspace.
		resolved := canonicalPath(change.path)
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			continue
		}
		return true
	}
	return false
}

// changedInDeliverable says whether a file this session modified is under the
// deliverable tree, still a file there, AND STILL HOLDING DIFFERENT CONTENT
// FROM WHAT IT HELD BEFORE THE WRITE. A path outside the tree is a note or a
// scratch file, not the work; a path that has since gone is not work anybody
// can point at; and a path whose content is back where it started is not work
// either, whatever the ledger remembers about it having been written.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// This used to ask about the PATH alone, and a path is not a change. The attrs
// cell (canary 2026-09-03) edited `src/attr/_make.py`, ran the suite, then ran
// `git stash` to compare its work against the baseline and never popped it. The
// tree at the end held none of the fix; the ledger still held the path; Made was
// true; the door said `finishing here · what was asked is done` over zero changed
// files. A revert and an edit that writes a file back to what it was fail the
// same way, silently, and always did.
//
// A FILE WITH NO BEFORE-DIGEST IS STILL COUNTED, which is the conservative side
// and the deliberate one. "" is what an absent file, an unreadable one and a
// path no pre-action ever saw all digest as ([fileDigest]), and a file that has
// content now differs from all three. The reading this must never give is a
// session that really did the work being told it made nothing.
func (a *Agent) changedInDeliverable() bool {
	tree := a.deliverableTree()
	if tree == "" {
		return false
	}
	a.mu.Lock()
	changed := append([]fileChange(nil), a.changedFiles...)
	a.mu.Unlock()
	for _, change := range changed {
		// THE SAME CONTAINMENT THE CREATED LEDGER USES ([underTree]): canonical
		// paths, so a symlink under the workspace pointing out of it is not
		// counted as work on the deliverable.
		if !underTree(tree, change.path) {
			continue
		}
		// AND THE PATH IS RESOLVED BEFORE IT IS READ, because the ledger's own
		// digest was taken through any link there is: os.Stat and os.Open follow
		// one, so a write through an in-tree symlink to an in-tree regular file
		// has a perfectly good before-digest, and an Lstat here rejected it as
		// "not a regular file" and never counted it. CONTAINMENT IS STILL
		// UNAFFECTED: [underTree] canonicalises both sides, so a link under the
		// workspace pointing OUT of it was already excluded above.
		resolved := canonicalPath(change.path)
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if fileDigest(resolved) == change.before {
			continue
		}
		return true
	}
	return false
}

// stashEntry is one line of `git stash list` as this reading needs to read it:
// the entry's own commit, which is what tells one entry from another across two
// readings, and the message it was pushed with, which is what tells the
// harness's own entries from a person's.
type stashEntry struct {
	sha     string
	subject string
}

// stashList is the deliverable tree's stash, whole, and WHETHER IT WAS READ AT
// ALL.
//
// THE SECOND ANSWER IS NOT A FORMALITY. A reading that failed and a repository
// with no stash in it are the same empty list, and they mean opposite things: an
// empty list from a tree that answered is knowledge, and an empty list from a
// git that is not installed, a directory that is not a repository yet, or a call
// that fell over is the absence of it. Collapsed into one value, a failed
// BEFORE-reading marked the baseline as taken and every entry the run later
// found counted as its own ([Agent.readStashBefore]).
//
// NOT A REPOSITORY, OR NO GIT AT ALL, IS THEREFORE `nil, false` AND SAYS
// NOTHING: neither is evidence about anybody's work, and the honest answer is
// silence rather than a sentence about a stash nobody has.
//
// THE FORMAT IS ASKED FOR RATHER THAN PARSED OUT OF THE DEFAULT LINE, which
// spells the message after a colon inside a subject that already holds one
// (`stash@{0}: On main: fixing it`). A tab cannot appear in a sha and does not
// survive into a stash subject, so it is the one separator that cannot be
// mistaken for content.
func stashList(tree string) ([]stashEntry, bool) {
	if strings.TrimSpace(tree) == "" {
		return nil, false
	}
	out, err := git(tree, "stash", "list", "--format=%H%x09%s")
	if err != nil {
		return nil, false
	}
	var entries []stashEntry
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		sha, subject, _ := strings.Cut(line, "\t")
		entries = append(entries, stashEntry{sha: strings.TrimSpace(sha), subject: subject})
	}
	return entries, true
}

// readStashBefore photographs the stash the tree ALREADY HELD, at the same
// moment the checks are photographed ([Agent.openBaseline]).
//
// IT IS TAKEN IN FRONT OF THE TURN AND THE CHECKS ARE NOT, and what separates
// them is what each costs. A declared check is an arbitrary shell command and
// can take as long as a suite takes, so it runs on its own and the turn starts.
// This is one ref read, and the whole of what it is worth is being certainly
// BEFORE the work: a reading taken on a goroutine could land after the session's
// own first `git stash`, which is the one entry it exists to be able to
// subtract.
func (a *Agent) readStashBefore() {
	entries, read := stashList(a.deliverableTree())
	if !read {
		// A READING THAT DID NOT HAPPEN IS NOT AN EMPTY STASH. Leaving the flag
		// down is what makes the terminal reading say nothing at all, rather
		// than treat everything it finds later as this run's own doing.
		return
	}
	held := make(map[string]bool, len(entries))
	for _, entry := range entries {
		held[entry.sha] = true
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stashBefore, a.stashBeforeRead = held, true
}

// stashedWork counts the stash entries THIS RUN PUT THERE: work the session
// took out of the deliverable tree and did not put back.
//
// IT IS ASKED ONCE, AT THE TERMINAL READING, and never on the turn loop
// ([Agent.terminalAudit]). It is a process, and the only moment its answer can
// change anything is the one where a principal is about to say the ask is
// finished.
//
// ── WHAT IS SUBTRACTED, AND WHY EACH ────────────────────────────────────────
//
// A STASH THAT WAS ALREADY THERE IS THE PERSON'S AND NOT OURS. It is the law the
// checks already live under ([Remains.WasFailing]) — a run may only be held to
// what it did itself — and without it a repository whose owner stashed something
// last week would be told at every single ending that work was left outside the
// tree, and could never say done. That is a false positive that bites somebody
// who did nothing wrong, so the before-reading is subtracted by sha.
//
// AND THE GROUND LADDER'S OWN ENTRIES ARE NOT WORK LEFT LYING ABOUT. A landing
// that has to merge into a ground holding uncommitted work sets that work aside
// with `git stash push` and puts it back on every road out of there
// ([taskTree.carryGroundWork]). The window is short and every road pops, but a
// terminal reading taken inside it would see the harness's own entry and answer
// carry on over a finished run. They are told apart by the message the push
// itself was given — [groundStashMessage], reused rather than copied, so a
// respelling cannot make this quietly stop matching.
//
// AND WITH NO BEFORE-READING, NOTHING IS COUNTED. Nobody then knows which
// entries appeared since, so the honest answer about the stash is silence rather
// than a guess — which is [Remains.BaselineRead]'s own law, applied to the other
// reading this session takes of the tree it started with.
func (a *Agent) stashedWork() int {
	a.mu.Lock()
	before, read := a.stashBefore, a.stashBeforeRead
	a.mu.Unlock()
	if !read {
		return 0
	}
	found, read := stashList(a.deliverableTree())
	if !read {
		return 0
	}
	entries := 0
	for _, entry := range found {
		if before[entry.sha] || isGroundStash(entry.subject) {
			continue
		}
		entries++
	}
	return entries
}

// isGroundStash says an entry is one the ground ladder pushed for a landing.
//
// IT IS THE SHAPE OF THE SUBJECT AND NOT THE WORDS ANYWHERE IN IT. A person
// whose own stash message happens to quote the sentence — "before I ask aforge:
// your own work, set aside to land the parser" — is describing their own work,
// and swallowing it would be this reading going quiet about exactly the entry it
// exists to name. So the match is anchored:
//
//   - `git stash push -m <message>` writes the subject `On <branch>: <message>`,
//     and `On (no branch): <message>` on a detached head. Measured against git
//     rather than assumed. A ref name cannot contain a colon, so the first `: `
//     ends the lead-in.
//   - What follows it has to BEGIN with [groundStashMessage]'s invariant head —
//     the constant reused, never copied, so a respelling cannot make this
//     quietly stop matching. The branch name is that message's tail and is not
//     known here.
//   - A plain `git stash` writes `WIP on <branch>: <sha> <subject>`, which does
//     not open with `On ` and is therefore never stripped and never matched.
func isGroundStash(subject string) bool {
	subject = strings.TrimSpace(subject)
	if lead, rest, found := strings.Cut(subject, ": "); found && strings.HasPrefix(lead, "On ") {
		subject = rest
	}
	return strings.HasPrefix(subject, groundStashMessage(""))
}

// reconciliation is what the sweep found: what belongs to the answer, and what
// was left lying beside it.
//
// Both lists name files as a person reads them, sorted, because the only reader
// of either is a person or a model reading over their shoulder.
type reconciliation struct {
	kept    []string
	scratch []string
	removed []string
	failed  []string
}

// reconcile sorts everything the session created into the deliverable and the
// scratch, and it TOUCHES NOTHING.
//
// Three questions per path, in this order, and a no to any of them leaves the
// file exactly where it is:
//
//  0. DID THIS SESSION CREATE IT? The ledger this is handed already holds only
//     created files ([Agent.rememberCreated]), and it is asked again here
//     anyway. This is the last function before os.Remove, the fact it turns on
//     can only be measured at a moment that has already passed, and a defence
//     that lives in one place is a defence one refactor away from being gone.
//
//  1. IS IT STILL THERE? A path the session made and then removed itself is not
//     scratch and is not anybody's business; a path that has become a directory
//     is not the file we wrote and is left alone.
//
//  2. IS IT INSIDE THE DELIVERABLE TREE? If it is, it is part of the answer,
//     whatever it looks like — a harness deciding which of somebody's files are
//     really deliverables is exactly the judgement it must not make. If it is
//     not, it is scratch: this session put a file somewhere nobody will look for
//     it, and leaving it there is the failure that zeroed two measured runs.
func reconcile(created []fileChange, tree string) reconciliation {
	var out reconciliation
	tree = strings.TrimSpace(tree)
	for _, change := range created {
		if !change.created {
			continue
		}
		info, err := os.Lstat(change.path)
		if err != nil || info.IsDir() {
			continue
		}
		name := change.shown
		if strings.TrimSpace(name) == "" {
			name = change.path
		}
		if tree != "" && underTree(tree, change.path) {
			out.kept = append(out.kept, name)
			continue
		}
		out.scratch = append(out.scratch, change.path)
	}
	sort.Strings(out.kept)
	sort.Strings(out.scratch)
	return out
}

// underTree reports that a path sits inside a directory. It is a comparison of
// canonical paths and never a prefix test on strings: `/work-2` is not inside
// `/work`, and a string prefix says it is. Resolving both sides also keeps a
// task tree reached through a symlink from having its deliverables mistaken for
// scratch and removed.
func underTree(tree, path string) bool {
	relative, err := filepath.Rel(canonicalPath(tree), canonicalPath(path))
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// sweepScratch removes what the sweep found outside the deliverable, and says
// what it removed and what it could not.
//
// A REMOVAL THAT FAILS IS REPORTED AND NEVER RETRIED. Whatever stopped it — a
// permission, a mount that went away — is not something this file can fix, and
// a sweep that fought the filesystem would be a session ending on an error
// about its own tidying rather than on its work.
func sweepScratch(found reconciliation) reconciliation {
	for _, path := range found.scratch {
		if err := os.Remove(path); err != nil {
			found.failed = append(found.failed, path)
			continue
		}
		found.removed = append(found.removed, path)
	}
	return found
}

// terminalAudit is the whole of the last reading, taken at the one moment its
// answer can change anything: after the principal has said the ask is met.
//
// IT RETURNS THE READINGS AND LEAVES THE DECIDING TO THE PRINCIPAL. This file
// takes readings; whether an unmet check means carry on or stop is
// [Steward.Decide]'s to say, and putting that judgement here would be a second
// policy over the same facts.
//
// THE SWEEP IS SORTED HERE AND CARRIED OUT ELSEWHERE ([Agent.sweepSession]),
// and the seam is not tidiness. A stopped turn is not necessarily an ENDING —
// the principal may read the checks and carry on — and a session that is about
// to carry on may be about to read the very file this pass is looking at. A
// sweep on every stopped turn would be this feature deleting the run's own
// working material halfway through, which is a worse failure than the one it
// was built to fix.
// openBaseline reads WHAT WAS ALREADY RED before this session did any work,
// once, in the background, at the start of an unattended run.
//
// ── THE MEASURED FAILURE ────────────────────────────────────────────────────
//
// The attrs cell's acceptance was "the existing test suite passes (run
// `tox -e py`)" over a suite that had one failing test before anybody touched
// anything. That sentence could never come true, so the goal owner read the
// project's own red as work still to do and carried the run on into it until the
// wall. A run cannot be asked to finish something that was not started.
//
// ── WHY HERE, AND NOT AT THE FIRST WRITE ────────────────────────────────────
//
// The first write is the other candidate and it is the wrong one: the write seam
// learns that a call wrote at POST-FEEDBACK, which is after the file changed, and
// a reading taken then already holds this session's own work. What a baseline has
// to be is the tree BEFORE, and the only moment that is certainly before is the
// one this shares with [Agent.openAcceptance] — the start of the first turn.
//
// ── AND IT DOES NOT HOLD THE TURN, WHICH IS THE HALF THAT HAD TO CHANGE ─────
//
// A declared check is an ARBITRARY SHELL COMMAND. Run in front of the person's
// first turn it can take as long as a suite takes, and several of them in a row
// can take several suites — so it runs on its own and the turn starts. Until it
// lands there is no baseline, and what a reading with no baseline does is count
// NOTHING as this run's own red ([Remains.BaselineRead]): naming a check before
// anybody knows whether it was already failing is the exact mistake this exists
// to stop, in a hurry.
//
// AND THE WHOLE READING SHARES ONE WINDOW ([sessionCheckWindow]), not one each. A
// baseline is a photograph and a photograph has an exposure; four checks with
// five minutes apiece would be twenty minutes of somebody else's suite running
// beside a run that is already going. What does not fit is simply not read.
//
// AND A CHECK THAT CHANGED THE TREE IS NOT A BASELINE. A declared "check" can
// build, format, migrate or install; run before the work it would be this
// session's own first edit, made by the harness, and its answer would be a
// reading of a tree nobody asked for. The tree is photographed either side of
// each one ([verify.TreeState]) and a check that moved it has its result thrown
// away and the fact written down.
func (a *Agent) openBaseline(ctx context.Context) {
	if a.steward() == nil {
		return
	}
	a.mu.Lock()
	taken := a.baselineTaken
	a.baselineTaken = true
	if a.baselineDone == nil {
		a.baselineDone = make(chan struct{})
	}
	a.mu.Unlock()
	if taken {
		return
	}
	// AND THE STASH THE TREE ALREADY HELD IS PHOTOGRAPHED HERE, in front of the
	// turn, because it is one ref read and because the whole of what it is worth
	// is being certainly before the work ([Agent.readStashBefore]). The checks
	// below cannot be taken in front of the turn, and are not.
	a.readStashBefore()
	checks := a.sessionChecks()
	if len(checks) == 0 {
		// NOTHING TO READ IS A FINISHED READING. A session whose ask declares no
		// runnable check has no baseline to wait for, and leaving the reading
		// permanently open would mean no check ever counted as this run's own.
		a.closeBaseline(nil, nil)
		return
	}
	go func() {
		defer guard.Recover("session baseline checks")
		a.readBaseline(ctx, checks)
	}()
}

// readBaseline is the reading itself, on its own goroutine.
func (a *Agent) readBaseline(ctx context.Context, checks []string) {
	tree := a.deliverableTree()
	// ONE WINDOW FOR THE WHOLE PHOTOGRAPH. [Agent.runSessionChecks] bounds each
	// call it makes as well, so a single check still cannot outlive the window
	// on its own; what this adds is that the SET cannot either. The wall's one
	// sizing door bounds this photograph without inventing a command for it.
	window := a.wallBoundedWindow(sessionCheckWindow)
	ctx, done := context.WithTimeout(ctx, window)
	defer done()

	var red, unread, moved []string
	for index, check := range checks {
		if ctx.Err() != nil {
			// The window closed. Everything it did not reach was not read, and
			// says so rather than passing for green.
			unread = append(unread, checks[index:]...)
			break
		}
		run, changed := a.checkNow(ctx, tree, check)
		if changed {
			// THE CHECK WROTE. Whatever it answered is an answer about a tree it
			// changed itself, so it is not a photograph of anything and it is
			// discarded rather than trusted.
			moved = append(moved, check)
			unread = append(unread, check)
			continue
		}
		if run.Unread != "" {
			unread = append(unread, check)
			continue
		}
		// AND A CHECK THAT COULD NOT BE RUN IS NOT A CHECK THAT FAILED. A command
		// that would not start, or that the window cut off, taught nobody
		// anything about the tree — and recording it as already-red would SILENCE
		// a real failure on it later, which is the opposite of this law. Only a
		// check that ran to an answer and answered red is baseline-red.
		if !run.Ran {
			unread = append(unread, check)
			continue
		}
		if !run.Passed {
			red = append(red, check)
		}
	}
	a.closeBaseline(red, unread)
	a.journalBaseline(red, unread, moved)
}

// treeStateNow photographs the deliverable tree, bounded, for the one question
// [Agent.readBaseline] asks of it: did that command change anything here?
func treeStateNow(tree string) string {
	if strings.TrimSpace(tree) == "" {
		return ""
	}
	return verify.TreeState(tree, treeRecord(tree))
}

// treeRecord lists the tree's own files for [treeStateNow], bounded by the same
// ceiling a claim hunt uses ([claimScanFiles]).
//
// ── WHAT IT LEAVES OUT IS THE WHOLE OF WHETHER THIS WORKS ───────────────────
//
// The question being asked is "did that command change the DELIVERABLE", and
// almost every real check writes something that is not one: a cache, a coverage
// file, a lock, a log. The attrs cell's only declared check was
// `python -m pytest tests/`, pytest wrote `.pytest_cache/` and `.hypothesis/`,
// and the reading was thrown away — leaving a baseline that said nothing was
// already failing over a project with 85 pre-existing failures (#513).
//
// THE REPOSITORY'S OWN ANSWER IS THE ONE THAT COUNTS, because the person has
// already written it down. Where the tree is a git worktree, the record is what
// `ls-files --cached --others --exclude-standard` lists: everything tracked, plus
// everything untracked that `.gitignore` does NOT cover — which is exactly "the
// files this project considers its own", asked in one call rather than one per
// path. A new source file a check writes still shows up, because it is untracked
// and not ignored.
//
// AND WHERE THERE IS NO REPOSITORY, [verify.SkipTree] IS THE WHOLE RULE — the
// one list this codebase keeps of what is not a deliverable. It is coarser: a
// build directory or a cache with no dot in its name is a change here, and a
// check that writes one has its reading discarded. That is the safe side (a
// discarded reading is never counted against the run) and it is said out loud
// rather than papered over.
//
// IT IS A LIST OF NAMES AND THE STATE IS BUILT FROM THE DISK, which is
// [verify.TreeState]'s own law: a name is not a state, so each path is settled
// against its size and its modification time there.
func treeRecord(tree string) []string {
	if record, ours := treeRecordFromGit(tree); ours {
		return record
	}
	var record []string
	_ = filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if verify.SkipTree(entry.Name()) && path != tree {
				return fs.SkipDir
			}
			return nil
		}
		if len(record) >= claimScanFiles {
			return fs.SkipAll
		}
		if verify.SkipTree(entry.Name()) {
			return nil
		}
		record = append(record, path)
		return nil
	})
	return record
}

// treeRecordFromGit asks the repository which files are its own, and answers
// false where there is no repository to ask.
func treeRecordFromGit(tree string) ([]string, bool) {
	out, err := git(tree, "ls-files", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, false
	}
	var record []string
	for _, line := range strings.Split(out, "\n") {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		if len(record) >= claimScanFiles {
			break
		}
		record = append(record, name)
	}
	return record, true
}

// closeBaseline publishes the reading and says it has happened, which are one
// step: a reader that saw the list before the flag would count nothing, and one
// that saw the flag before the list would count everything.
func (a *Agent) closeBaseline(red, unread []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.baselineRead {
		return
	}
	a.baselineRed, a.baselineUnread = red, unread
	a.baselineRead = true
	if a.baselineDone != nil {
		close(a.baselineDone)
	}
}

// awaitBaseline waits for the reading, and it is called at ONE moment: the
// terminal one, where the checks are about to be run and subtracted.
//
// EVERY OTHER READING GOES ON WITHOUT IT. The reading is in the background so
// nothing waits for it ([Agent.openBaseline]), and a mid-run reading with no
// baseline simply counts no check either way. But the terminal reading is the
// one that can say DONE, and saying it over a red check nobody could attribute
// would ship red work as finished — so this is the moment to pay for the answer,
// and the run is ending anyway.
//
// A SESSION THAT NEVER STARTED ONE CLOSES IT EMPTY AND CARRIES ON. A watched
// session, a unit test, an ask with no runnable check: there is nothing coming,
// so waiting would be waiting forever. Empty means nothing is KNOWN to have been
// already red, and every red then counts — which is the safe side of a terminal
// answer.
func (a *Agent) awaitBaseline(ctx context.Context) {
	a.mu.Lock()
	read, started, done := a.baselineRead, a.baselineTaken, a.baselineDone
	a.mu.Unlock()
	if read {
		return
	}
	if !started || done == nil {
		a.closeBaseline(nil, nil)
		return
	}
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// baselineRedChecks is what this session found already failing before it worked,
// and whether the reading has landed at all ([Remains.WasFailing]).
func (a *Agent) baselineRedChecks() ([]string, []string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.baselineRed...),
		append([]string(nil), a.baselineUnread...), a.baselineRead
}

// journalBaseline writes the baseline down, INCLUDING WHEN IT WAS ALL GREEN.
//
// A run that carried on into somebody else's red and a run whose tree was clean
// read identically in the file before this, so the one fact that explains a whole
// evening was the one fact nowhere on disk. The row is written whichever way it
// came out — an empty `failed` on a green tree is the reading having happened,
// not the reading being missing — and a check that moved the tree is named
// beside it, because a declared check that writes is worth somebody knowing
// about whatever else it answered.
func (a *Agent) journalBaseline(red, unread, moved []string) {
	a.journalFile().appendPrincipal(journalPrincipal{
		Who: principalWord(a.who()), Event: "baseline",
		Failed: red, Checks: unread, Removed: moved,
	})
}

// AND IT READS THE STASH, which is the third thing a person would have looked
// at. Work the session pushed onto `git stash` is work that is not in the tree,
// and every other reading here — the checks, the reconciliation, the ledger —
// reads a tree it is missing from without noticing ([stashedWork]).
func (a *Agent) terminalAudit(ctx context.Context) ([]CheckRun, reconciliation, int) {
	// THE BEFORE-READING IS WAITED FOR HERE AND NOWHERE ELSE. What these checks
	// answer is about to be subtracted from it, and a terminal answer of done
	// taken over a red check nobody could attribute would ship red work as
	// finished ([Agent.awaitBaseline]).
	a.awaitBaseline(ctx)
	ran := a.runSessionChecks(ctx, a.sessionChecks())
	var unread []string
	for _, check := range ran {
		if check.Unread != "" {
			unread = append(unread, check.Command)
		}
	}
	a.rememberTerminalUnread(unread)
	tree := a.deliverableTree()
	found := reconcile(a.createdList(), tree)
	stashed := a.stashedWork()
	a.journalChecks(ran, stashed)
	return ran, found, stashed
}

// sweepSession carries out what [reconcile] sorted, and it is called at the END
// — the turn on which the principal said done, or said stop.
//
// A PERSON'S SESSION SWEEPS NOTHING. They are offered the list and that is all:
// somebody who is sitting there can see their own directory, and a harness
// quietly removing files behind them is the opposite of what the emptiness of
// [Person] means everywhere else.
func (a *Agent) sweepSession(found reconciliation) reconciliation {
	if a.steward() != nil {
		found = sweepScratch(found)
	}
	a.journalReconciliation(found)
	return found
}

// journalChecks writes down what the tree said about itself, because a session
// that was checked and a session that was taken at its word read identically in
// the journal before this line existed.
//
// THE STASH RIDES ON THIS ROW RATHER THAN ON ONE OF ITS OWN. It is a fact from
// the same reading, taken at the same moment, and a second row kind for one
// integer would make a reader join two lines to learn what one terminal audit
// found. A session with no checks and a stash still writes the row: the stash is
// the news, and the empty check list beside it is true.
func (a *Agent) journalChecks(ran []CheckRun, stashed int) {
	if len(ran) == 0 && stashed == 0 {
		return
	}
	moment := journalPrincipal{Who: principalWord(a.who()), Event: "checked", Stashed: stashed}
	for _, check := range ran {
		moment.Checks = append(moment.Checks, check.Command)
		if check.Unread != "" {
			moment.Unread = append(moment.Unread, check.Command)
			continue
		}
		if !check.Passed {
			moment.Failed = append(moment.Failed, check.Command)
		}
	}
	a.journalFile().appendPrincipal(moment)
}

// journalReconciliation writes down what the session left behind and what
// became of it, because a run that ended clean and a run that ended after
// deleting eleven files read identically in the journal before this line
// existed.
func (a *Agent) journalReconciliation(found reconciliation) {
	if len(found.kept) == 0 && len(found.scratch) == 0 {
		return
	}
	a.journalFile().appendPrincipal(journalPrincipal{
		Who:     principalWord(a.who()),
		Event:   "reconciled",
		Kept:    found.kept,
		Removed: found.removed,
		Failed:  found.failed,
	})
}

// journalFile is this session's journal, or nil. Every append door in
// sessionfile.go declines a nil receiver, which is what lets the callers above
// read as one line each.
func (a *Agent) journalFile() *sessionFile {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.file
}

// principalWord names which principal a journal line belongs to, in the
// vocabulary this package uses for the two of them.
func principalWord(principal Principal) string {
	if _, steward := principal.(*Steward); steward {
		return "steward"
	}
	return "person"
}
