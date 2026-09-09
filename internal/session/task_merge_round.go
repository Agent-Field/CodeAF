package session

// ── THE MERGE ROUND: ONE ATTEMPT AT A CONFLICT BEFORE IT IS A PERSON'S CALL ──
//
// A node whose work HOLDS and whose branch will not fasten is not a question
// about the work. Somebody changed the same lines on the person's branch while
// the node was working, and the two versions have to be brought together — which
// is ordinary work, of exactly the kind the node just did, and which the person
// was being handed because nothing here had ever tried it.
//
// So it is tried, once. The person's branch is merged INTO THE TASK'S BRANCH,
// inside the task's own working copy, where a conflict marker can be written
// without anybody's checkout being touched. A worker is put in front of the
// markers with the brief and both sides. The check runs again on what it left.
// The landing is retried. Only a round that fails reaches the card.
//
// ── THE THREE LAWS THIS FILE KEEPS ──
//
// NOTHING IS REWRITTEN IN PLACE. Before the merge, the task branch's tip is kept
// under a ref of its own ([beforeMergeRef]) — so whatever this round does, the
// branch the node actually produced is still nameable afterwards, by a person at
// their own terminal and by a second round.
//
// A CONFLICT MARKER NEVER REACHES THE PERSON'S CHECKOUT. Everything below runs
// in the node's worktree, on the node's branch; the person's tree is not read,
// not stashed and not merged into until the ordinary landing is retried
// (task_ledger.go's [landHome], groundcarry.go).
//
// AND A CONFLICT IS NEVER THE MODEL'S TO ACCEPT. The round is the ENGINE's, and
// what it produces is either a branch that merges — which lands the ordinary way,
// through the ordinary check — or a card with the files named. The one thing it
// may not do is decide on somebody's behalf that two versions of a file were
// really one.

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// mergeRoundLimit is how many resolver rounds one node buys itself. ONE — the
// automatic one, spent at the landing. A person who reads the card and asks for
// another gets it through [Agent.ResolveConflict], which spends one more on
// demand; that is a decision somebody took, and it is not this bound.
const mergeRoundLimit = 1

// beforeMergeSuffix is what the ref holding the task branch's pre-merge tip is
// named with. It hangs off the branch's OWN name — `task/<slug>-<id>` becomes
// `task/<slug>-<id>-before-merge` — because that name is what every other
// sentence about this node's work already says, and a second naming scheme would
// be a ref a person could not connect to the branch on their card.
const beforeMergeSuffix = "-before-merge"

// beforeMergeRef is the ref this round keeps the branch's tip under.
func beforeMergeRef(branch string) string {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return ""
	}
	return branch + beforeMergeSuffix
}

// mergeRoundOutcome is what one round did, in the two facts anybody downstream
// needs: whether the branch will now fasten, and the files that stood in the way
// when it will not.
type mergeRoundOutcome struct {
	// resolved says the task branch now holds the person's branch merged into it
	// with no markers left, so the landing is worth retrying.
	resolved bool
	// files are the paths that conflicted, named for the card. They are read
	// while the conflicted index still holds them, which is the only moment they
	// exist ([conflictedPaths]).
	files []string
	// changed is the node's ledger with whatever the resolver wrote folded in.
	changed []string
	// tree is the working copy the round actually ran in, which is not always the
	// one it was handed: a copy the landing already unregistered is picked back up
	// on the way in, and that reading may correct the branch's own name
	// ([taskTree.reopenReleased]). Everything after the round — the check, the
	// retried landing, the undo — reads this one.
	tree taskTree
}

// mergeRoundFailedSentence is what a person reads under a conflict the round
// could not settle: that it was tried, which files stood in the way, and where
// the branch as the node left it can still be found.
//
// IT NAMES THE REF, because the ref is the only part of this a person cannot
// discover by looking. The files are on the row already; `task/…-before-merge`
// is a thing this round made, and a ref nobody was told about is a ref that
// looks like litter in six weeks.
func mergeRoundFailedSentence(branch string, files []string) string {
	if len(files) == 0 {
		return ""
	}
	return "a round was spent trying to bring the two versions of " +
		namedFew(files, conflictNamesShown) + " together and could not; " + branch +
		" as this task left it is kept on " + beforeMergeRef(branch)
}

// mergeRoundAtLanding is the automatic round: it is asked at the moment a
// landing finds the branch would not fasten, and it answers the state the node
// lands in when it carried the work home.
//
// FALSE IS EVERY OTHER ROAD, and the caller lands the node exactly as it did
// before this existed. A round is not attempted at all where there is nothing to
// merge into — a folder ground, a mirror, a copy that is gone — and a round that
// ran and did not resolve is a card with the files named, which is the whole of
// what the design gives the person.
func (a *Agent) mergeRoundAtLanding(ctx context.Context, node *TaskNode, tree taskTree, changed []string, report string, log io.Writer) (TaskState, bool, string) {
	if !node.mergeRoundLeft() {
		return "", false, ""
	}
	outcome, ran := a.spendMergeRound(ctx, node, tree, changed, log)
	if !ran {
		return "", false, ""
	}
	tree = outcome.tree
	if !outcome.resolved {
		return "", false, mergeRoundFailedSentence(tree.branch, outcome.files)
	}
	// THE CHECK RUNS AGAIN ON THE RESULT. What is on the branch now is not what
	// anybody checked: a worker has just edited the very files the deliverable is
	// made of, and landing that on the strength of the check the round STARTED
	// from would be merging unread work under a verdict about something else.
	verdict := a.auditNode(ctx, node, tree, outcome.changed, report, log)
	if !verdict.verified {
		fmt.Fprintf(log, "merge round: the check did not pass what the round left — %s\n", verdict.report())
		a.undoMergeRound(tree, log)
		return "", false, mergeRoundFailedSentence(tree.branch, outcome.files)
	}
	// AND THE LANDING IS RETRIED, through the one road every landing takes
	// (task_ledger.go's [landHome]). It is retried HERE rather than by calling the
	// finishing line again, because the finishing line is what called this: one
	// round is one round, and a recursion through it would be a node that merged
	// its way round the bound.
	landed, merge, detail, _ := landHome(node, tree, outcome.changed)
	if !cameHome(merge) {
		fmt.Fprintf(log, "merge round: it still would not land — %s\n", detail)
		return "", false, mergeRoundFailedSentence(tree.branch, outcome.files)
	}
	fmt.Fprintf(log, "merge round: resolved, and %s landed\n", tree.branch)
	node.finish(withReport(report, withReport(verdict.doneOutcome(), detail)), landed, tree.branch, merge)
	return TaskDone, true, ""
}

// spendMergeRound is the round itself, and it is the same body whether the
// engine spent it at a landing or a person asked for it on a card.
//
// It never returns an error. Every way this can go wrong is a round that did not
// resolve the conflict, which is a fact the caller already knows what to do
// with: the card, with the files named.
func (a *Agent) spendMergeRound(ctx context.Context, node *TaskNode, tree taskTree, changed []string, log io.Writer) (mergeRoundOutcome, bool) {
	tree, ok := resolvableTree(tree)
	if !ok {
		return mergeRoundOutcome{tree: tree}, false
	}
	home := currentBranch(tree.root)
	if home == "" || strings.EqualFold(home, tree.branch) {
		// A detached ground has no branch to merge, and a ground somehow standing
		// on the task's own branch has nothing to bring together.
		return mergeRoundOutcome{tree: tree}, false
	}
	// THE SURFACE HEARS THE NODE STILL FINISHING. Nothing has landed and nothing
	// was undone; a round spent bringing two versions of a file together is the
	// end of a run exactly as a repair round is (task_beat.go).
	defer a.enterPhase(node, taskBeatRepairing, 0, 0, mergeRoundGap)()
	node.mending(mergeRoundGap)
	defer node.mending("")

	// THE TIP IS KEPT BEFORE ANYTHING MOVES. Everything after this line is
	// reversible by naming this ref, which is what "nothing is rewritten in
	// place" means when the thing being rewritten is a branch.
	kept := beforeMergeRef(tree.branch)
	if err := keepBeforeMerge(tree, kept); err != nil {
		fmt.Fprintf(log, "merge round: %s could not be kept, so nothing was merged — %v\n", kept, err)
		return mergeRoundOutcome{tree: tree}, false
	}
	node.spendMergeRoundCount()

	out, err := git(tree.dir, append(aforgeGitIdentity(), "merge", "--no-edit", home)...)
	if err == nil {
		// The two branches had nothing to argue about after all — a merge git
		// could do by itself, which is the cheapest possible round.
		fmt.Fprintf(log, "merge round: %s merged into %s with nothing to resolve\n", home, tree.branch)
		return mergeRoundOutcome{resolved: true, changed: changed, tree: tree}, true
	}
	files := conflictedPaths(tree.dir)
	if len(files) == 0 {
		// A merge git refused before it touched the index leaves no conflicted
		// index and nothing a worker could resolve ([overwrittenPaths] says what
		// that shape is). It is not this round's to fix.
		fmt.Fprintf(log, "merge round: %s would not open on %s — %s\n", home, tree.branch, firstLine(out))
		abandonMerge(tree.dir)
		return mergeRoundOutcome{tree: tree}, false
	}
	fmt.Fprintf(log, "merge round: %s conflicts with %s in %s — one round to resolve it\n",
		tree.branch, home, namedFew(files, conflictNamesShown))

	wrote := a.runResolver(ctx, node, tree, home, files, changed, log)
	if problem := settleResolvedMerge(tree, files); problem != "" {
		fmt.Fprintf(log, "merge round: %s\n", problem)
		abandonMerge(tree.dir)
		return mergeRoundOutcome{files: files, changed: mergePaths(changed, wrote), tree: tree}, true
	}
	return mergeRoundOutcome{resolved: true, files: files, changed: mergePaths(changed, wrote), tree: tree}, true
}

// mergeRoundGap is what a person watching the card reads while the round runs.
// It is the plain sentence the row already carries for work that is finishing,
// and it names no machinery.
const mergeRoundGap = "bringing the two versions together"

// resolvableTree answers whether there is anything here a merge round could
// happen in, and picks a released working copy back up where there is.
//
// A folder ground, a mirror and a node that ran in the person's own tree all
// answer no: there is no branch, so there was never a merge to fail. A copy the
// landing already unregistered is reopened the way every other road that arrives
// after a settle reopens one ([taskTree.reopenReleased]).
func resolvableTree(tree taskTree) (taskTree, bool) {
	if tree.merge == mergeInPlace || tree.mode == TaskModeMirror {
		return tree, false
	}
	if strings.TrimSpace(tree.root) == "" || strings.TrimSpace(tree.branch) == "" || strings.TrimSpace(tree.dir) == "" {
		return tree, false
	}
	switch reopened, back, problem := tree.reopenReleased(); {
	case back:
		tree = reopened
	case problem != "":
		return tree, false
	}
	if info, err := os.Stat(tree.dir); err != nil || !info.IsDir() {
		return tree, false
	}
	if root, ok := repositoryRoot(tree.dir); !ok || root != canonicalPath(tree.dir) {
		return tree, false
	}
	return tree, true
}

// keepBeforeMerge writes the ref that makes this round reversible. It is a
// branch rather than a tag because a person recovering from it wants to check it
// out, and it is forced because a second round on the same node must keep the
// tip it is about to move rather than the one the first round moved.
//
// IT TAKES THE GROUND'S LOCK, briefly. A ref lives in the repository every
// worktree of it shares, so writing one races a sibling landing exactly as a
// merge does (task_lock.go).
func keepBeforeMerge(tree taskTree, ref string) error {
	if strings.TrimSpace(ref) == "" {
		return fmt.Errorf("the branch has no name to keep")
	}
	defer lockGitRoot(tree.place, tree.root)()
	out, err := git(tree.root, "branch", "-f", ref, tree.branch)
	if err != nil {
		return fmt.Errorf("%s", firstLine(out))
	}
	return nil
}

// settleResolvedMerge closes the merge the round opened, and answers the one
// sentence saying why it could not be closed.
//
// THE MARKERS ARE READ FROM THE FILES AND NOT FROM THE MODEL. A worker that says
// it resolved everything and left `<<<<<<<` in a file has produced a branch that
// compiles to nothing, and taking its word for it is exactly how a landing comes
// to carry a patch that is an unresolved merge (task_run.go's
// [Agent.landConflicted] was written from one).
func settleResolvedMerge(tree taskTree, files []string) string {
	if left := markedFiles(tree.dir, files); len(left) > 0 {
		return "conflict markers are still in " + namedFew(left, conflictNamesShown)
	}
	if out, err := git(tree.dir, append([]string{"add", "--"}, files...)...); err != nil {
		return "the resolved files could not be staged — " + firstLine(out)
	}
	if still := conflictedPaths(tree.dir); len(still) > 0 {
		return "git still holds " + namedFew(still, conflictNamesShown) + " as unresolved"
	}
	if out, err := git(tree.dir, append(aforgeGitIdentity(), "commit", "--no-edit")...); err != nil {
		return "the merge could not be committed — " + firstLine(out)
	}
	return ""
}

// markedFiles are the paths that still hold a conflict marker. Both fences are
// asked for, and with the trailing space git writes, because a line of seven
// angle brackets is a thing a person's own file may legitimately contain and a
// marker is not.
func markedFiles(dir string, files []string) []string {
	var left []string
	for _, name := range files {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			// A file the resolver DELETED is a resolution: one side of the merge
			// won outright. There is nothing left in it to hold a marker.
			continue
		}
		text := string(body)
		if strings.Contains(text, "<<<<<<< ") || strings.Contains(text, ">>>>>>> ") {
			left = append(left, name)
		}
	}
	return left
}

// undoMergeRound puts the branch back on the tip the round kept, for the one
// road where the round succeeded at git and failed at the work: the markers came
// out, the merge committed, and the check then found the result does not hold.
//
// The ref stays where it is. It is the record of what the node produced, and a
// person reading the card is owed it whether or not this round put the branch
// back onto it.
func (a *Agent) undoMergeRound(tree taskTree, log io.Writer) {
	ref := beforeMergeRef(tree.branch)
	if strings.TrimSpace(ref) == "" {
		return
	}
	if out, err := git(tree.dir, "reset", "--hard", ref); err != nil {
		fmt.Fprintf(log, "merge round: %s could not be put back on %s — %s\n", tree.branch, ref, firstLine(out))
		return
	}
	fmt.Fprintf(log, "merge round: %s is back on %s\n", tree.branch, ref)
}

// ── the resolver ────────────────────────────────────────────────────────────

// runResolver puts a worker in front of the markers and answers what it wrote.
//
// IT IS THE REPAIR ROUND'S SHAPE (task_audit.go's [Agent.repairNode]): a fresh
// worker in the node's own working copy, on the node's own model, spoken to
// once, its spend folded onto the node. What differs is the instruction and only
// the instruction — this one is not being told what is missing from the work, it
// is being told that two people wrote the same lines.
func (a *Agent) runResolver(ctx context.Context, node *TaskNode, tree taskTree, home string, files, changed []string, log io.Writer) []string {
	child, err := a.newTaskAgentOn(ctx, tree.dir, node, "-resolve", "")
	if err != nil {
		fmt.Fprintf(log, "merge round: could not start a worker — %v\n", err)
		return nil
	}
	defer func() {
		_ = child.Close()
		a.foldTaskUsage(node, child)
	}()
	// The room follows the work, exactly as it does for a repair round: somebody
	// watching this node came to watch the node, and this is the node still
	// finishing (task_room.go).
	room := node.openRoom()
	spoke := room.speaker()
	room.speaking(child)
	defer room.speaking(spoke)

	wrote, stopped, runErr := runTaskChild(ctx, child, node, resolveInstruction(node, tree, home, files, changed), tree.dir, a.taskLimits(node), room, log)
	switch {
	case stopped != "":
		fmt.Fprintf(log, "merge round: %s\n", stopped)
	case runErr != nil:
		fmt.Fprintf(log, "merge round: the worker ended with an error — %v\n", runErr)
	}
	return wrote
}

// resolveInstruction is what the resolving worker is asked.
//
// THE BRIEF LEADS, because the question "which of these two versions is right"
// is unanswerable without knowing what the work is FOR — and the node's own
// assembled brief, bound to this working copy, is the only place that is written
// down ([TaskNode.instructionOn]).
//
// THE MERGE IS ALREADY OPEN AND THE WORKER MAY NOT TOUCH IT. A task worker's git
// may read anything and move nothing (taskgit.go), so a worker that reached for
// `git merge` or `git commit` would be refused mid-round and spend the rest of
// its turn arguing with a guard. The instruction says so plainly, in the same
// breath as what it IS being asked to do, because a rule stated without the
// alternative is a rule a model routes around.
func resolveInstruction(node *TaskNode, tree taskTree, home string, files, changed []string) string {
	var out strings.Builder
	out.WriteString(node.instructionOn(tree))
	if ground := repairGround(tree, changed); ground != "" {
		out.WriteString("\n\n" + repairSawHeading + "\n" + ground)
	}
	out.WriteString("\n\n" + resolveHeading + "\n")
	fmt.Fprintf(&out, "Your branch %s and %s both changed the same lines. %s has been merged into "+
		"this working copy and git could not settle these files:\n", tree.branch, home, home)
	for _, name := range files {
		out.WriteString(name + "\n")
	}
	out.WriteString("\n" + resolveRule)
	return out.String()
}

const (
	// resolveHeading opens the one section of the document that is about THIS
	// round, in the same register the repair round's finding heading uses.
	resolveHeading = "## The two versions to bring together"
	// resolveRule is what the worker may and may not do, said once. The verbs it
	// is refused are named so it does not spend a turn discovering them.
	resolveRule = "Open each of those files and write the version that keeps BOTH changes: the work " +
		"this task was asked for, and whatever the other side changed for its own reasons. Remove every " +
		"`<<<<<<<`, `=======` and `>>>>>>>` line as you go — a file left holding one is a file that " +
		"compiles to nothing. Where the two changes genuinely cannot both stand, keep the one the brief " +
		"above asks for and say in your reply which the other was.\n\n" +
		"Do not run `git merge`, `git commit`, `git add`, `git rebase`, `git checkout` or `git reset`: " +
		"the merge is already open and it is closed for you when you are done. Edit the files and " +
		"nothing else."
)

// ── the counters, and the door a person reaches ─────────────────────────────

// mergeRoundLeft reports whether this node still has its automatic round.
func (n *TaskNode) mergeRoundLeft() bool {
	if n == nil || n.graph == nil {
		return false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	return n.mergeRounds < mergeRoundLimit && !n.resolving
}

// spendMergeRoundCount records that a round has been bought, at the line that
// buys it — the ref is written, the branch is about to move, and a counter that
// rose before that would hold a node against a round nothing spent.
func (n *TaskNode) spendMergeRoundCount() {
	if n == nil || n.graph == nil {
		return
	}
	n.graph.mu.Lock()
	n.mergeRounds++
	n.graph.mu.Unlock()
}

// claimResolving takes the one round a node may have in flight, and answers
// false when one already is. It is the second half of the refusal
// [Agent.ResolveConflict] owes a surface: a person pressing the key twice must
// read one plain line rather than start a second worker in the same working copy
// as the first.
func (n *TaskNode) claimResolving() bool {
	if n == nil || n.graph == nil {
		return false
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	if n.resolving {
		return false
	}
	n.resolving = true
	return true
}

func (n *TaskNode) releaseResolving() {
	if n == nil || n.graph == nil {
		return
	}
	n.graph.mu.Lock()
	n.resolving = false
	n.graph.mu.Unlock()
}

// ResolveConflict spends ONE MORE merge round on a node whose branch would not
// fasten, on the person's word — the `[a] resolve it` of the card.
//
// IT RETURNS BEFORE THE ROUND DOES, for [Agent.reauditTask]'s reason: a round
// buys a model call, and a keypress that blocked on one would be a wedged
// surface. The node stays exactly where it is — needing a look — until the round
// lands, and when it does the person and the model hear about it on the same
// lane every other landing rides.
//
// IT REFUSES IN ONE LINE, which is the whole of what a surface can draw. There
// are two refusals and they are different facts: a node with no working copy left
// has nothing to resolve IN, and a node whose round is already running must not
// be given a second worker in the same directory as the first.
func (a *Agent) ResolveConflict(id uint64) error {
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	// THE ROUND IN FLIGHT IS ASKED ABOUT FIRST, and it is not tidiness: the
	// question "is somebody already editing that working copy" has to be answered
	// before anybody goes looking at the working copy, or two rounds race through
	// the look and both start.
	if !node.claimResolving() {
		return fmt.Errorf("task %d is already being resolved — wait for that round to land", id)
	}
	tree, err := node.workingCopy(a.familyPlace(node), a.config.Workspace)
	if err != nil {
		node.releaseResolving()
		return err
	}
	if _, ok := resolvableTree(tree); !ok {
		node.releaseResolving()
		return fmt.Errorf("task %d has no working copy to resolve in — its work is on %s, and merging it is yours to do",
			id, strings.TrimSpace(tree.branch))
	}
	ctx, cancel := context.WithCancel(context.Background())
	// NO JOB ROW, NO ROUND. The registry is where the cancel is registered, so a
	// goroutine started without one would run on a bare context: no `jobs kill`,
	// no death at [Agent.Close], and a worker still editing a working copy in a
	// session that has gone (task_audit.go's [Agent.reauditTask] states the whole
	// argument).
	listed, err := a.jobs.startTask(node.id, "resolve · "+node.title(), cancel)
	if err != nil {
		cancel()
		node.releaseResolving()
		return fmt.Errorf("the merge round could not be started: %w — accept it or drop it instead", err)
	}
	report, changed, _, _ := node.leavings()
	go func() {
		defer cancel()
		defer node.releaseResolving()
		defer listed.settle(0)
		a.landResolved(ctx, node, tree, changed, report, taskLog(listed))
	}()
	return nil
}

// landResolved is what an on-demand round does with what it produced, and it is
// the landing road rather than the run's: this node has already settled once, so
// what a resolved branch reaches is a RESETTLE, exactly as an accept and a late
// verdict do (task_audit.go's [Agent.landAudit]).
//
// A round that did not resolve leaves the node precisely as it was. There is
// nothing new to say — the card already names the files — and a second card
// saying the same thing in the same words is the noise the design's one-question
// law exists against.
func (a *Agent) landResolved(ctx context.Context, node *TaskNode, tree taskTree, changed []string, report string, log io.Writer) {
	outcome, ran := a.spendMergeRound(ctx, node, tree, changed, log)
	if !ran || !outcome.resolved || ctx.Err() != nil {
		return
	}
	tree = outcome.tree
	verdict := a.auditNode(ctx, node, tree, outcome.changed, report, log)
	if ctx.Err() != nil {
		return
	}
	if !verdict.verified {
		fmt.Fprintf(log, "merge round: the check did not pass what the round left — %s\n", verdict.report())
		a.undoMergeRound(tree, log)
		return
	}
	landed, merge, detail, _ := landHome(node, tree, outcome.changed)
	if !cameHome(merge) {
		fmt.Fprintf(log, "merge round: it still would not land — %s\n", detail)
		return
	}
	fmt.Fprintf(log, "merge round: resolved, and %s landed\n", tree.branch)
	node.checkSaid(auditGrade(verdict), 0)
	node.finish(withReport(report, withReport(verdict.doneOutcome(), detail)), landed, tree.branch, merge)
	node.graph.resettle(node, TaskDone)
}
