package session

import (
	"context"
	"fmt"
	"io"
)

// task_ledger.go is one law, said once, for every family that lands.
//
// ── THE LEDGER IS THE CONTRACT OF WHAT SHIPS; THE TREE IS ONLY THE MEDIUM ──
//
// A landing carries the paths a node wrote and nothing else. [stageTaskWork]
// stages that list, [taskTree.landMirror] lays it back over a folder ground by
// name, and everything downstream — the card, the project index, the check —
// reads the same list. Walking the directory instead would carry home every
// virtualenv, cache and build output a run left lying beside the work, which is
// the defect the whole of task_landing_test.go exists about.
//
// A NODE THAT HANDED WORK OUT BREAKS THAT LIST IN HALF. Its parts worked in ITS
// tree and wrote their paths onto THEIR OWN ledgers, so the parent's list names
// the parent's slice of a deliverable the parent no longer wrote most of. On a
// repository ground the gap is invisible, because a part's work arrives as
// commits on the very tree the parent merges — the ledger composes there through
// git rather than through anything written down. On a folder ground nothing
// composes it: the mirror held all three files, the ledger named one, and the
// person's folder got one. The task said done, the check passed against the
// mirror, and the parts' work was lost in silence (#229).
//
// SO THE LEDGER COMPOSES EVERYWHERE, and it composes at the LANDING rather than
// in the copier: [taskTree.comeHome] keeps reading exactly one list, and what
// changed is that the list is now complete. That also makes the fold recursive
// for free — a part absorbs its own parts before it settles, so by the time a
// root reads its children's leavings a grandchild's paths are already on them —
// and it leaves a repository ground where it was, since the paths it adds there
// are paths git is already carrying.
//
// A PART THAT WROTE NOTHING ABSORBS NOTHING, and a part that never landed is not
// on the ledger at all: work that is not in the parent's tree may not be claimed
// by the parent's landing ([landingFilesFor] holds both).

// absorbedLedger is a node's effective ledger: what its own worker wrote, with
// every landed part's paths folded in, in the order they were first written and
// never named twice.
//
// It is [landingFiles] read as ONE list rather than as two. The two halves exist
// for the checker's packet alone, where "it wrote" has to stay a true claim
// about this node and its parts are named in a sentence of their own
// (task_audit.go's [auditQuestion]). A landing has no such question to answer —
// what ships is what the family made — so it reads the whole of it.
//
// IT IS IDEMPOTENT, which is what lets a road that runs after a landing call it
// again without knowing whether an earlier one already did: the parts come from
// the graph and the rest of the list is left where it is ([landingFilesFor]).
func absorbedLedger(node *TaskNode, changed []string) []string {
	return landingFilesFor(node, changed).all()
}

// landHome is THE ONE PLACE A NODE'S WORK COMES HOME: finalize the ledger, then
// land it. Every road that merges goes through it — the ordinary finishing line,
// the threshold's ([Agent.landStopped]), a person's accept and a late verdict
// (task_audit.go) — and it answers the finalized ledger so that the [TaskNode.finish]
// under each of them writes the SAME list onto the node.
//
// THAT LAST HALF IS NOT A CONVENIENCE. A node's ledger outlives its run: a
// family that landed unverified is settled hours later by somebody typing
// `accept`, and that road has nothing to read but what the first landing wrote
// down. While the fold lived at the merge alone, an accepted mirror family laid
// the parent's slice over the person's folder and dropped every part's file —
// the same loss as before, one road further along.
func landHome(node *TaskNode, tree taskTree, changed []string) ([]string, string, string, landingRefusal) {
	ledger := absorbedLedger(node, changed)
	// AND WHY IT DID NOT COME HOME TRAVELS WITH THE OUTCOME. A landing that failed
	// is answered by somebody, and whether asking them again could change anything
	// is decided where the refusal happened, not read back out of the sentence
	// afterwards (task_land_unsaved.go's [landingRefusal]).
	merge, detail, clashing, why := tree.comeHome(node.title(), ledger)
	// AND THE NAMES ARE KEPT ON THE NODE, at the one moment they exist. git's index
	// held them while the refused merge stood and was made to give them back before
	// the merge was abandoned (groundcarry.go's [taskTree.refuseMerge]); a row drawn
	// an hour later has nowhere else to read them from, and a surface parsing them
	// back out of the report's prose would be this program reading its own writing.
	node.clashesWith(clashing)
	return ledger, merge, detail, why
}

// landUnreadDirections is the end of a run whose person kept changing the work:
// nothing merges, the branch and the working copy stay exactly as they are, and
// the node settles unverified — the state this package already has for finished
// work nobody can stand behind. It is not `done`, because what the person last
// asked for was never carried out, and it is not `failed`, because nobody made a
// finding against the work itself.
func (a *Agent) landUnreadDirections(node *TaskNode, tree taskTree, changed []string, report string, claim publicationClaim, log io.Writer) TaskState {
	note := unreadDirectionsNote(claim.unread)
	if note == "" {
		note = "this did not land: what it was checked against is no longer what this task is for. Its work is kept on its branch — continue this task to have it taken up"
	}
	fmt.Fprintf(log, "not landing: %s\n", note)
	merge, kept := keepHome(node, tree, changed)
	node.finish(withReport(note, report), kept, tree.branch, merge)
	return TaskUnverified
}

// keepHome is [landHome]'s counterpart for a node that settles WITHOUT merging —
// stopped, errored, turned back at the gate, or landing onto a ground that moved
// under it. The ledger is finalized for the same two reasons: the branch a
// person is being offered has to hold the whole family's work ([keptWork] is
// what commits it), and the list the node settles with is what a later accept
// will land.
func keepHome(node *TaskNode, tree taskTree, changed []string) (string, []string) {
	return keptWork(tree, node.title(), absorbedLedger(node, changed))
}

// landFinished is THE ONE ENDING FOR WORK THAT HOLDS, and it is one function
// because it was three.
//
// A node whose deliverable stands has the same three things left to do whoever
// decided that: ask whether the ground moved under it, merge, and ask of the
// outcome whether anything actually landed before it settles. That
// sequence was written out three times — once on the ordinary finishing line,
// once for a family whose verification is switched off, and once for a node the
// counter stopped whose work was checked anyway ([Agent.landStopped]) — and each
// copy had to remember the ground check, the conflict test and the ledger the
// merge answers with. THE MEMORY IS EXACTLY WHAT FAILED: the checks the family
// audit filed (#255, #256, #258) each had to be added to every copy, and the one
// that was missed was the one nobody was looking at. Written once, a check added
// here is a check every road home gets.
//
// ── THE TWO HALVES OF THE REPORT, AND WHY THEY ARE PARAMETERS ──
//
// Every road home composes the same report out of two sentences in a fixed
// relationship: `head` is what leads, `tail` stands under it, and the merge's own
// detail goes under both. On the ordinary line the node's own account leads and
// what it was checked on stands under it; on a family with the check switched off
// the sentence saying so leads and the node's account stands under it. The
// composition never varies, only which sentence takes which place — so it is the
// caller's answer, and the folding is stated here once.
//
// `note` is what the log line says about this particular road, and it is empty
// for the ordinary one. The person's report never carries it: a threshold that
// fired is machinery, and a reader of a card that says done has no use for it
// ([Agent.landStopped] states the whole of that argument).
func (a *Agent) landFinished(ctx context.Context, node *TaskNode, tree taskTree, changed []string, head, tail, note string, log io.Writer) TaskState {
	// ── THE PUBLICATION BOUNDARY, TAKEN BEFORE ANYTHING LEAVES THIS PROCESS ──
	//
	// This is the one road on which a task's work is PUBLISHED: it merges onto
	// the person's branch and the node settles done. So it is also the road that
	// has to answer whether the person has changed what "done" means since the
	// worker stopped reading — a correction typed while the gate was reading the
	// tree is held on the node's record ([Agent.SteerTask]), and work that landed
	// as done over one would be this harness deciding their last word did not
	// count.
	//
	// THE ANSWER IS AN INSTANT AND NOT A LOOK. [TaskNode.claimPublication] reads
	// the unread directions and takes the boundary in one locked step, so a
	// direction is either in before the claim — nothing publishes, and the work
	// goes round again with their words — or after it, and it belongs to the next
	// round rather than to a merge that is already going out (assignment.go).
	claim := node.claimPublication()
	if !claim.granted {
		if claim.again {
			fmt.Fprintf(log, "not landing: the person has said something this work has not read\n")
			return taskRunAgain
		}
		// AND WHEN THERE IS NO ROUND LEFT, THE WORK STILL DOES NOT PUBLISH. A run
		// that has been round for corrections as often as one run may be
		// ([directedRoundLimit]) stops here rather than merging work the person has
		// moved on from and calling it done. The branch is kept, nothing goes to
		// their checkout, and the card says what is waiting.
		return a.landUnreadDirections(node, tree, changed, withReport(head, tail), claim, log)
	}
	// THE GROUND IS CHECKED WHEREVER WORK WOULD MERGE. The check that passed was
	// run inside this node's own working copy, which is a copy of the world as it
	// was when the node started — so it says nothing at all about a file another
	// window has landed in since. That is the one question left before a merge,
	// and taskground.go is where it is asked.
	if shift := a.groundShift(node, changed); shift != "" {
		return a.landShifted(node, tree, changed, withReport(head, tail), shift, log)
	}
	landed, merge, detail, _ := landHome(node, tree, changed)
	fmt.Fprintf(log, "merge: %s %s%s\n", merge, detail, note)
	// AND THE ONE QUESTION EVERY ROAD ASKS OF THE OUTCOME: did the work get where
	// the person can see it ([cameHome], task_land_unsaved.go)? A branch that
	// would not merge and work that could not be committed at all are two reasons
	// and one answer — nothing landed — and the MARK is carried through rather
	// than made here, because the completion note and the row read it to tell the
	// two apart. Testing for a conflict by hand is exactly how the second reason
	// walked past all five of these roads (#255).
	if !cameHome(merge) {
		return a.landConflicted(ctx, node, tree, landed, withReport(head, tail), merge, detail, log)
	}
	// THE WORK'S OWN ACCOUNT LEADS, AND WHAT IT WAS CHECKED ON STANDS UNDER IT.
	// Everything downstream reads this report from the top: the settle card quotes
	// its first sentence as what came of the work, the project's index keeps that
	// same line as the row's outcome (task_index.go's taskOutcome), and the chat
	// model reads it before writing the paragraph the person actually asked for.
	// With the check's evidence in front, all three carried a verification command
	// — "`git diff --cached --stat` shows staged new file …" — where what the work
	// FOUND belonged, and a model handed proof-of-check as the headline grades the
	// deliverable instead of delivering it (prompts/system.md's rule for the moment
	// work lands). The evidence is still here, because a finished card is owed what
	// was checked; it is simply not the news. The state says "done" — nothing here
	// says it a second time in the harness's own vocabulary.
	node.finish(withReport(head, withReport(tail, detail)), landed, tree.branch, merge)
	return TaskDone
}

// clashesWith records the files that stopped this node's branch fastening onto
// the person's. An empty list clears nothing: a landing road that named no files
// — git would not say which they were, or the branch never reached the person's
// repository at all — leaves whatever an earlier attempt found, because the
// emptiness law says an absence is not evidence that nothing clashed.
func (n *TaskNode) clashesWith(files []string) {
	if n == nil || n.graph == nil || len(files) == 0 {
		return
	}
	n.graph.mu.Lock()
	defer n.graph.mu.Unlock()
	n.clashing = append([]string(nil), files...)
}
