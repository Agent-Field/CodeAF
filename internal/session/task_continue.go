package session

// First-class continuation of a settled task.
//
// F23/F25: "continue task 4" used to re-derive a fresh brief and mint a new
// node through propose_task, paying for a new worktree and losing the failed
// worker's journal. The durable pieces were already on the node — the brief,
// the acceptance, the branch, the working-copy path, the journal, the
// checker's report. What was missing was a door that put THAT node back on
// the frontier.
//
// Continue is that door. It is [TaskGraph.interrupt]'s transformation reached
// by a person (or the model on their behalf) for any settled ending: same id,
// same spec, same tree, the last report handed back as this round's finding.
// A new propose_task is a different piece of work.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// continueFindingLead opens the section a continued node is handed: what the
// last attempt found, in the checker's or the landing's own words. It is a
// FINDING, not a new assignment — spec.brief stays the original, and a second
// continue replaces this block rather than wrapping it.
const continueFindingLead = "What the last attempt found"

// continueAskedLead opens the person's own words when they arrived with the
// continue. Same shape as the finding: this round only, never edited into
// the assignment.
const continueAskedLead = "What they asked this time"

// ContinueTask re-arms a settled node so the frontier runs it again. Unknown
// id, a node that is still going, and a design or a saved-shape run are each
// an error naming which.
//
// THE ASSIGNMENT NEVER CHANGES. words, if any, arrive under
// [continueAskedLead] as this round's finding, beside the last report. The
// brief and the acceptance the node was admitted with stay the ones the work
// is graded against.
func (a *Agent) ContinueTask(id uint64, words string) error {
	node := a.taskNode(id)
	if node == nil {
		return fmt.Errorf("no task %d in this session", id)
	}
	return a.tasker().reopen(node, words)
}

// reopen puts a settled node back on the frontier as queued work of ITS OWN
// id. It is the transformation [interrupt] already performs for a process
// death, reached here by a person for any ending.
//
// IT IS NOT [TaskGraph.admit]. admit reserves a new id and writes a new
// spec; this keeps both. And it is not [TaskGraph.resettle]: resettle moves
// a landed node to another final state and must not close `done` a second
// time. reopen opens a new `done` so the next landing has a channel to
// close, and resets the flags a second run cannot inherit — claimed,
// stopped, the first-cause ending — because those are facts about the
// attempt that just ended.
func (g *TaskGraph) reopen(node *TaskNode, words string) error {
	if g == nil || node == nil {
		return errors.New("no task to continue")
	}
	g.mu.Lock()
	if node.graph != g || g.nodes[node.id] != node {
		g.mu.Unlock()
		return fmt.Errorf("no task %d in this session", node.id)
	}
	switch node.kind {
	case TaskKindHarness, TaskKindSubharness:
		kind := TaskKindWord(node.kind)
		g.mu.Unlock()
		return fmt.Errorf("task %d is %s, not a run that can be continued", node.id, kind)
	}
	switch node.state {
	case TaskRunning:
		g.mu.Unlock()
		return fmt.Errorf("task %d is still running", node.id)
	case TaskQueued:
		g.mu.Unlock()
		return fmt.Errorf("task %d has not started yet", node.id)
	case TaskDone, TaskFailed, TaskUnverified:
	default:
		g.mu.Unlock()
		return fmt.Errorf("task %d is %s, not a task that has ended", node.id, node.state)
	}
	if g.quitting {
		g.mu.Unlock()
		return errors.New("this session is closing")
	}
	// The finding is the last attempt's answer and not only its card: a second
	// attempt told "wrote the three files" has to rediscover what the first one
	// produced (task_result.go). The report still leads, because what the check
	// said is how a continuation knows what to change.
	// THE WORDS ARE THE PERSON'S OWN AND THEY ARE RECORDED AS SUCH. A continue
	// that says "CSV instead of JSON" is the same correction the room's steer
	// carries, arriving at a task that has already stopped — so it goes on the
	// node's record as a direction the next attempt may fold into the assignment
	// (assignment.go), rather than being a finding this attempt is handed and the
	// next check grades against the original request anyway. It is carried into
	// the finding below either way: recording it is what gives it a road to the
	// done-condition, not a second copy of the words.
	if strings.TrimSpace(words) != "" {
		node.assignment.hear(words, directionFromPerson, time.Now())
	}
	// Everything on the record is now in front of the next attempt: the words
	// above, and anything said while the last one was finishing that no worker
	// read. The block below carries the second kind, which the finding has no
	// other place for; the first kind is the finding's own section, named with
	// the id so the worker can cite it.
	// Everything waiting is written into the finding and STAYS PENDING until the
	// attempt's opening request carries it (assignment.go): a continue is a node
	// queued, not a node that has read anything.
	said := node.assignment.pendingFrom(directionFromPerson)
	node.carried = directionIDs(said)
	node.finding = withReport(composeContinueFinding(node.deliveredLocked(), words, latestDirectionID(said, words)),
		directionBlock(otherDirections(said, words)))
	node.publishing = false
	node.continuing = true
	node.state = TaskQueued
	node.claimed = false
	node.stopped = false
	node.ending = ""
	// THE LIFE OF THE WORK MOVES ON HERE, and the announcement marks are cleared
	// with it in the same locked step: a delivery of the ending that just closed
	// may still be in flight, and it must not be able to record this new attempt
	// as already announced ([TaskNode.claimNote]).
	node.attempt++
	node.noted = false
	node.notedRead = false
	node.notedState = ""
	node.noting = false
	node.notingClaim = noteClaim{}
	node.queuedSaid = false
	node.held = ""
	node.parked = false
	node.checked = ""
	node.repairs = 0
	node.blockedBy = ""
	node.ctx = nil
	node.cancel = nil
	node.done = make(chan struct{})
	g.mu.Unlock()

	g.checkpoint()
	g.announce(node)
	g.runFrontier()
	return nil
}

// composeContinueFinding is the one block a continuation adds to the brief.
// Empty halves are dropped (the emptiness law): a landing that wrote no
// report and a continue that carried no words compose nothing, and the
// worker is handed the original assignment alone.
// direction is the receipt those words were written onto the node's record as,
// and 0 when there were none. It is named in the heading because a worker
// cannot fold a correction into the assignment without an id to cite
// (assignment.go).
func composeContinueFinding(report, words string, direction uint64) string {
	report = strings.TrimSpace(report)
	words = strings.TrimSpace(words)
	var parts []string
	if report != "" {
		parts = append(parts, continueFindingLead+"\n"+report)
	}
	if words != "" {
		lead := continueAskedLead
		if direction != 0 {
			lead = fmt.Sprintf("%s (direction %d — if it changes what this work is FOR, fold it in with revise_assignment citing %d)",
				lead, direction, direction)
		}
		parts = append(parts, lead+"\n"+words)
	}
	return strings.Join(parts, "\n\n")
}

// latestDirectionID picks the receipt minted for THIS continue's words out of
// what was waiting, and 0 when the continue carried none.
func latestDirectionID(said []taskDirection, words string) uint64 {
	words = strings.TrimSpace(words)
	if words == "" {
		return 0
	}
	for index := len(said) - 1; index >= 0; index-- {
		if said[index].words == words {
			return said[index].id
		}
	}
	return 0
}

// otherDirections is everything waiting EXCEPT this continue's own words, which
// the finding has already set out under their own heading. A document that said
// them twice would be a document arguing with itself about which copy is the
// instruction.
func otherDirections(said []taskDirection, words string) []taskDirection {
	words = strings.TrimSpace(words)
	rest := make([]taskDirection, 0, len(said))
	for _, one := range said {
		if words != "" && one.words == words {
			continue
		}
		rest = append(rest, one)
	}
	return rest
}

// resumeContinuedTree reattaches the working copy a settled landing left.
//
// Where the copy is still a registered worktree — an interrupt that a
// person then continued — it is taken as-is, the way [TaskNode.resumeTree]
// already does. Where the landing unregistered it ([taskTree.releaseKept])
// the branch still holds the work, and this checks that same directory
// back out onto that branch so the next run does not cut a fresh one
// (F23/F25). A finished task whose tree came home and was deleted falls
// through and lets [Agent.openTaskWorld] cut a new copy from the ground
// the work already landed on.
func (n *TaskNode) resumeContinuedTree(place Place, workspace string) (taskTree, bool) {
	n.graph.mu.Lock()
	dir, branch, merge := n.worktree, n.branch, n.merge
	n.graph.mu.Unlock()
	ground, mode := n.groundNow()
	if merge == mergeInPlace {
		if strings.TrimSpace(dir) == "" {
			return taskTree{}, false
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			return taskTree{}, false
		}
		return openFamilyTree(taskTree{dir: dir, merge: mergeInPlace, ground: ground, mode: mode}), true
	}
	if strings.TrimSpace(branch) == "" || strings.TrimSpace(dir) == "" {
		return taskTree{}, false
	}
	root, ok := repositoryRoot(ground)
	if !ok {
		if root, ok = repositoryRoot(workspace); !ok {
			return taskTree{}, false
		}
	}
	tree, ok := reattachKeptWorktree(place, root, dir, branch, ground, mode)
	if !ok {
		return taskTree{}, false
	}
	return n.ladderRecord(tree), true
}

// reattachKeptWorktree puts a settled node's directory back on its kept
// branch. A directory that is already a worktree is left alone. Leftover
// ordinary files from [taskTree.releaseKept] are moved aside so `git
// worktree add` can take the path; the branch already holds the commit
// that landing made.
func reattachKeptWorktree(place Place, root, dir, branch, ground string, mode TaskMode) (taskTree, bool) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(dir) == "" || strings.TrimSpace(branch) == "" {
		return taskTree{}, false
	}
	if !branchOnDisk(root, branch) {
		return taskTree{}, false
	}
	defer lockGitRoot(place, root)()
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return taskTree{dir: dir, root: root, branch: branch, place: place, ground: ground, mode: mode}, true
		}
		aside := dir + ".prior"
		_ = os.RemoveAll(aside)
		if err := os.Rename(dir, aside); err != nil {
			return taskTree{}, false
		}
		defer func() {
			if _, err := os.Stat(dir); err == nil {
				_ = os.RemoveAll(aside)
				return
			}
			_ = os.Rename(aside, dir)
		}()
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return taskTree{}, false
	}
	if _, err := git(root, "worktree", "add", dir, branch); err != nil {
		return taskTree{}, false
	}
	return taskTree{dir: dir, root: root, branch: branch, place: place, ground: ground, mode: mode}, true
}
