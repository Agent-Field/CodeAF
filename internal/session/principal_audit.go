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
//   - THE CHECKS THE WORK ITSELF NAMED, RE-RUN FROM CLEAN. Not a list this file
//     knows — task_checks.go already settled that law, and the same
//     [declaredChecks] reading is used here so a session and its nodes can never
//     disagree about what a check is. "From clean" means A FRESH PROCESS IN THE
//     DELIVERABLE TREE: no shell the turn had open, no environment a tool call
//     had edited, nothing cached from the run. It is what a person typing the
//     command in a new terminal would get, which is exactly the reading that was
//     never taken.
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
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
// THE SESSION'S OWN DOCUMENTS ARE THE FIRST SOURCE. The ask in the person's own
// words and the acceptance written for the whole of it are what a session has
// instead of a node's brief, and they are read with [declaredChecks] — the same
// reading a unit of work's own document gets, so the two can never disagree
// about what a check is.
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
func (a *Agent) sessionChecks() []string {
	principal := a.who()
	// The deliverable tree is where [Agent.runSessionChecks] will start every one
	// of these, so it is the directory a declared check has to be runnable in —
	// the same tree, asked the same question, as the one the checks are run in.
	tree := a.deliverableTree()
	checks := declaredChecks(principal.Ask()+"\n"+principal.Acceptance(), tree)
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
		checks = appendChecks(checks, auditDoorFor(node, auditPlace{ground: tree, ran: tree}).checks)
	}
	return trimChecks(checks)
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

// runSessionChecks runs the session's declared checks from clean and reports
// what each one said.
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
		out = append(out, runOneCheck(ctx, tree, check))
	}
	return out
}

func runOneCheck(ctx context.Context, tree, check string) CheckRun {
	ctx, done := context.WithTimeout(ctx, sessionCheckWindow)
	defer done()
	command := exec.CommandContext(ctx, "bash", "-c", check)
	command.Dir = tree
	output, err := command.CombinedOutput()
	return CheckRun{
		Command: check,
		Passed:  err == nil,
		Tail:    checkTail(string(output)),
	}
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
func (a *Agent) terminalAudit(ctx context.Context) ([]CheckRun, reconciliation) {
	ran := a.runSessionChecks(ctx, a.sessionChecks())
	found := reconcile(a.createdList(), a.deliverableTree())
	a.journalChecks(ran)
	return ran, found
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
func (a *Agent) journalChecks(ran []CheckRun) {
	if len(ran) == 0 {
		return
	}
	moment := journalPrincipal{Who: principalWord(a.who()), Event: "checked"}
	for _, check := range ran {
		moment.Checks = append(moment.Checks, check.Command)
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
