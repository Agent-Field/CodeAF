package session

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
func absorbedLedger(node *TaskNode, changed []string) []string {
	return landingFilesFor(node, changed).all()
}
