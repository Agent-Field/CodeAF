package session

// A FAMILY THAT DIVIDES COMMITS ITS WORK FIRST, AND THAT COMMIT IS THE ONE WORLD
// EVERY PART OF IT STARTS FROM.
//
// ── WHAT THIS REPAIRS, AND IT IS TWO THINGS ──
//
// FIRST, THE PARTS COULD NOT SEE THE PARENT'S WORK. Workers are told never to
// commit — `write` and `edit` put files on disk and the harness commits them,
// once, at the landing (task_run.go's [commitTaskWork]). That is right for a
// task that runs alone and wrong the moment one divides mid-run: everything the
// parent has written by then — a repro it built, a failing test it wrote, the
// material it gathered, the half-drafted section a part is meant to continue —
// is uncommitted work in a directory the parts are about to branch off. The
// parent's briefs can DESCRIBE those findings; nothing handed over the artefacts
// themselves, and for a part that has to RUN the failing test that is the job.
//
// SECOND, THE SIBLINGS DID NOT EVEN SEE THE SAME WORLD AS EACH OTHER. A part's
// working copy is prepared lazily, when the frontier starts it, and a parent
// goes on working while its parts run (the parent-stays law). So two parts cut a
// minute apart each inherited whatever the parent's directory happened to hold
// at that instant, and the division that drew their boundaries had described
// neither of those worlds.
//
// One commit answers both. Before a single part is admitted, the harness stages
// the parent's ledger and commits it onto the family branch, and the commit the
// family tree then stands at is written onto every part as it is admitted. The
// harness commits, never the worker, so "you never run `git add`" stays as true
// as it was.
//
// ── WHY A COMMIT AND NOT THE GROUND LADDER'S SEAL ──
//
// This is worth answering out loud, because the ground ladder already carries a
// parent's uncommitted world to a child: the snapshot rung writes a machine
// commit of the parent's tree and carves the child's worktree from it
// (groundladder.go's [sealGroundWork]), and the landing rebases it back out
// ([taskTree.replayOwnWork]). From far enough away the two look like one job
// done twice.
//
// THEY ARE DIFFERENT ANSWERS TO DIFFERENT QUESTIONS.
//
//   - The seal answers WHAT WORLD DOES THIS ONE CHILD STAND IN. It is
//     scaffolding: written with `commit-tree`, which moves no ref at all, so it
//     belongs to no branch and is reachable only from the child it was made for
//     — and it is deliberately lifted out again at that child's landing, so that
//     what comes home is the child's own work and not its inheritance.
//   - This commit answers WHAT DOES THE FAMILY'S TREE HOLD. It is history: on
//     the family branch, made once, and it stays. The parent's work comes home
//     with the family's, inside the one merge the person sees.
//
// Leaving the second job to the seal would mean a family whose shared material
// exists only inside each part's private branch, in a commit the landing is
// written to discard, made once per part off whatever the parent's directory
// held at that moment. That is not a family tree; it is one copy per part and no
// record of any of them. A `git log` of the family branch would show none of it,
// and a crash between the division and the landing would leave the parent's work
// in an object nothing references.
//
// SO THERE IS ONE ROAD AND THIS IS THE HEAD OF IT. Because the commit is on the
// branch, a part is carved straight from it and the seal is not asked at all
// (groundladder.go's [snapshotRung]) — which also means there is no `base` to
// rebase out, so a part's landing is an ordinary merge onto a shared ancestor
// and one failure mode fewer. The seal keeps its own job, unchanged, for every
// task that is nobody's part.
//
// ── THE GUARDS, AND WHY EACH IS THERE ──
//
//   - THE HARNESS MAY ONLY COMMIT IN A TREE OF ITS OWN. A family told to work
//     `here` — in place, or a folder with nowhere else to stand — works in the
//     person's own directory, and a commit there would be machinery writing onto
//     the branch they are sitting on. That is the one thing this must never do,
//     so it is asked structurally rather than by listing modes
//     ([harnessOwnsThisTree]). A family with no tree of its own freezes nothing
//     and its parts go on sharing the parent's directory, which is what "here"
//     already meant.
//   - NOTHING WRITTEN, NOTHING COMMITTED — but the world is still pinned. An
//     empty ledger is the emptiness law and the whole of the sketch road's
//     answer: a division drawn before the first request has nothing on disk to
//     commit. THE FREEZE IS STILL TAKEN, at the family tree's HEAD as it stands,
//     because the second defect above does not need the parent to have written
//     anything — a parent that writes AFTER the division would otherwise reach
//     the parts that start late and not the ones that started early.
//
// ── AND A FREEZE THAT WOULD NOT GO REFUSES THE DIVISION ──
//
// This is the one road here that answers anybody, and it is loud because the
// alternative is the defect. A family tree that cannot be committed to, or
// cannot say where its HEAD is, is a family whose parts would be admitted onto a
// world nobody chose — the exact silence this file exists to end. So nothing is
// handed out, the worker is told in one line, and it carries on with the work in
// its own hands, which is what every other refusal on this road leaves it doing.

import "strings"

// startTheParts is the whole of a division coming into existence: the family's
// world is frozen, and every part is admitted onto it.
//
// IT IS ONE OPERATION AND THAT IS THE POINT. [TaskGraph.admit] puts a node on
// the frontier, which starts it — so a freeze taken after the first admission is
// a freeze the first part may already have raced past, and a freeze taken
// without the parts carrying it is a fact nobody reads. Both halves are here, in
// this order, and [Agent.divideOnce] holds none of it.
//
// It answers the ids and titles admitted, or the one sentence to hand the worker
// when the family's world could not be frozen and nothing was handed out.
func (a *Agent) startTheParts(node *TaskNode, parts []dividePart, line *journalDivision) ([]uint64, []string, string) {
	graph := a.graph()
	// THE SLOTS ARE TAKEN FOR THE WHOLE DIVISION BEFORE ANY OF IT EXISTS. A
	// division is ONE decision: three parts admitted and a fourth refused by the
	// fan cap would leave the worker holding a shape nobody chose, so the cap is
	// met before anything exists rather than halfway through
	// ([TaskGraph.claimChild] states why the claim and not the count). The hands
	// go back on every road out of here, which is why they are held by a value
	// with one release rather than by a counter each ending has to remember.
	hands := divisionHands{graph: graph, parent: a.config.taskID}
	defer hands.release()
	if refusal := hands.take(len(parts)); refusal != "" {
		line.Decision = divisionRefusedCap
		return nil, nil, refusal
	}

	frozen, checkpoint, problem := a.freezeFamilyWorld(node)
	if problem != "" {
		line.Decision = divisionRefusedFreeze
		return nil, nil, divisionWorldNotFrozen(problem)
	}
	line.Frozen, line.Checkpoint = frozen, checkpoint

	// THE MODEL, THE OTHER MODEL, THE RATINGS AND THE PERSON'S SENTENCE ARE ALL
	// RESOLVED ONCE, ABOVE THE LOOP. The ladder reads settings and the live
	// conversation model and the store is the person's rather than this session's
	// — a division whose third part resolved differently from its first because
	// something moved underneath it would be a family nobody could account for
	// afterwards. [Agent.divideOnce]'s own notes say why each of them is read.
	model := a.resolveTaskModel("").model
	careful := a.carefulModel(model)
	grades := graph.grades.reader(model)
	request := a.taskRequest()
	parent := a.config.taskID
	// AND WHAT EVERY PART IS TOLD ABOUT THE FAMILY IS COMPOSED ONCE, HERE, FOR
	// THE WHOLE DIVISION (task_divide_compose.go). A part's brief is two halves
	// with two authors — the work being divided and the map of who owns what,
	// which the harness holds, and the scope, which only the worker in the
	// material could write. It is composed above the loop for the reason the two
	// models are: the parts of one division must read one document, not five
	// fittings of it.
	family := familyOf(request, node.inheritedBrief(), parts)

	ids := make([]uint64, 0, len(parts))
	titles := make([]string, 0, len(parts))
	for index, part := range parts {
		id := graph.reserve()
		graph.admit(id, taskSpec{
			title: part.Title,
			// A MODEL WROTE THIS TITLE as a name, so the namer leaves it alone
			// (taskname.go).
			named:      true,
			summary:    part.Summary,
			request:    request,
			brief:      family.partBrief(index, part.Brief),
			acceptance: part.Acceptance,
			expects:    part.Expects,
			model:      a.partModel(part, model, careful, grades, line),
			parent:     parent,
			depth:      a.config.taskDepth + 1,
			owner:      a,
			// AND THE WORLD IT STARTS IN, carried on the spec so that it is written
			// onto the node and checkpointed in the same breath the node is admitted
			// in ([TaskGraph.admit]). A part that existed for an instant without
			// knowing its world is a part the frontier could start on the wrong one.
			frozen: frozen,
		})
		// The node counts itself from here, so the hand its admission was holding
		// has already gone back ([TaskGraph.admit] releases it under the graph's
		// own lock) and this is the book agreeing with it.
		hands.held--
		ids = append(ids, id)
		titles = append(titles, part.Title)
	}
	line.Decision, line.Admitted = divisionAdmitted, len(ids)
	return ids, titles, ""
}

// divisionHands is the free hands one division is holding, and the ONE place
// they are given back.
//
// IT IS A VALUE RATHER THAN A COUNTER because the alternative is a `taken--`
// that every ending in this function has to remember — and the ending that
// forgets is a hand held for a part that will never exist, which is a lane the
// person's cap will not give out again for the life of the session.
type divisionHands struct {
	graph  *TaskGraph
	parent uint64
	held   int
}

// take claims one hand per part, or answers the cap's own refusal having given
// back whatever it managed to claim first.
func (h *divisionHands) take(parts int) string {
	for i := 0; i < parts; i++ {
		if refusal := h.graph.claimChild(h.parent); refusal != "" {
			return refusal
		}
		h.held++
	}
	return ""
}

func (h *divisionHands) release() {
	for ; h.held > 0; h.held-- {
		h.graph.releaseChild(h.parent)
	}
}

// partModel is which tier one part is minted on, and it is a function of its own
// because the answer has three readers and only one of them is the part's word.
//
// A part inherits the parent task's model, which is what every child on this
// road has always done — same worker, same job, somewhere quieter. A part GRADED
// CAREFUL is minted on the high tier instead ([roles.RoleCareful]), because the
// parts of one division are not one kind of work.
//
// AND THE EVIDENCE OVERRULES THE WORD, IN ONE DIRECTION ONLY. A part the worker
// called careful is never demoted by a store — the worker read the material and
// the store did not — while a part it called ordinary is lifted where the record
// says ordinary is not what happens to work of this shape (taskgrade.go). The
// lift is skipped entirely where the careful tier resolves to the model the task
// is already on, because then there is nothing to lift it to and the record would
// claim a decision nobody made.
func (a *Agent) partModel(part dividePart, model, careful string, grades taskGradeReader, line *journalDivision) string {
	switch {
	case part.careful():
		return careful
	case careful != model && grades.saysCareful(taskKindOf(part.Title)):
		line.Lifted = append(line.Lifted, part.Title)
		return careful
	}
	return model
}

// freezeFamilyWorld commits the parent's ledger onto the family branch and
// answers the commit the family tree then stands at, the checkpoint it wrote if
// it wrote one, and the one line to quote about a family whose world could not
// be frozen at all.
//
// THE TWO COMMITS IT ANSWERS ARE NOT THE SAME FACT. The FREEZE is where the
// parts branch from and it exists for every family with a tree of its own, a
// parent that has written nothing included — that parent is still going to write
// something in a minute, and a part that starts after it would otherwise inherit
// work its division never mentioned. The CHECKPOINT is the commit this call
// wrote, and it is empty whenever the parent's ledger was empty. Recording them
// as one field would leave nobody able to tell a clean parent from a family that
// never froze.
func (a *Agent) freezeFamilyWorld(node *TaskNode) (frozen, checkpoint, problem string) {
	dir := canonicalPath(strings.TrimSpace(a.config.Workspace))
	ground, _ := node.groundNow()
	if dir == "" || !harnessOwnsThisTree(dir, ground) {
		// A family standing in the person's own folder has no tree of its own to
		// freeze and never had. It is not a failure and there is nothing to say
		// about it: its parts share the directory, which is what "here" means.
		return "", "", ""
	}
	if ledger := node.rememberedWrites(); len(ledger) > 0 {
		_, commit, err := commitTaskWorkAs(dir, wipCheckpointMessage(node.title()), ledger)
		if err != nil {
			return "", "", err.Error()
		}
		// AND THE CHECKPOINT IS ASKED WHETHER IT REALLY HOLDS THE LEDGER. A commit
		// that ran is not a commit that carried: [stageTaskWork] steps over a path
		// git refuses one at a time, which is right for a landing and wrong here —
		// a ledger path left on the floor is a part opening on a brief that names
		// a file its disk does not have. So the tree is asked, and a division whose
		// material did not all go in is refused rather than handed out short.
		if unheld := unheldLedgerPaths(dir, ledger); len(unheld) > 0 {
			return "", "", "the work so far is not all in it: " + namedFew(unheld, leftBehindNamesShown)
		}
		if commit != "" {
			return commit, commit, ""
		}
	}
	// Nothing of the ledger reached a commit — the parent has written nothing
	// yet, or everything it wrote was already committed by a round before this
	// one. HEAD is the family's world either way, and pinning it is the half of
	// this that a clean parent still needs.
	head, err := git(dir, "rev-parse", "HEAD")
	if err != nil {
		return "", "", familyTreeProblem(head, err)
	}
	return strings.TrimSpace(head), "", ""
}

// harnessOwnsThisTree reports that a commit made in this directory lands on the
// FAMILY'S OWN BRANCH and in nobody else's history.
//
// Both halves are needed and neither implies the other. A directory that is not
// the ground is a copy the harness made — a worktree of the person's repository,
// or the mirror of their folder (task_tree_mirror.go) — and a directory that is
// the top of its own repository is one whose HEAD is the family's. A task
// working in place fails the first: it stands in the person's folder, which may
// very well be the top of their repository, and a commit there would go onto the
// branch they are sitting on. A node handed a bare folder of its own underneath
// a repository fails the second: its directory is not the ground, but the
// repository around it is the person's and the commit would land there.
func harnessOwnsThisTree(dir, ground string) bool {
	if ground = strings.TrimSpace(ground); ground == "" || canonicalPath(ground) == dir {
		return false
	}
	root, ok := repositoryRoot(dir)
	return ok && root == dir
}

// wipCheckpointMessage is what the checkpoint says it is, in a person's words,
// for whoever reads this history afterwards — and it says WHY there is a commit
// nobody made by hand sitting in the middle of the work, which is the only
// question that line has to answer. It is the same voice as the ground ladder's
// [groundCommitMessage] and the family tree's own baseline, and clipped for the
// reason they are: `git log --oneline` must not lose the why off the end.
func wipCheckpointMessage(title string) string {
	title = clip(firstLine(strings.TrimSpace(title)), 60)
	if title == "" {
		return "the work so far, before its parts were handed out"
	}
	return "the work so far on " + title + ", before its parts were handed out"
}

// divisionWorldNotFrozen is the answer to a division whose family tree would not
// take the commit its parts have to start from.
//
// IT REFUSES, AND THAT IS THE WHOLE POINT OF IT. Handing the parts out anyway
// would put every one of them on a world nobody chose — which is the silence
// this seam exists to end, arriving through the door meant to close it. So it
// reads like the road's other refusals: what happened, and the one thing the
// worker does next, which is carry on with the work in its own hands.
//
// IT QUOTES GIT'S OWN WORDS because the reasons are the machine's rather than
// the work's — a disk that has gone read-only, a repository somebody broke — and
// a sentence that dropped them would leave a person reading a division that was
// refused for no stated reason.
func divisionWorldNotFrozen(problem string) string {
	return "not split: the work you have done so far could not be put where the parts would pick it up (" +
		strings.TrimSpace(problem) +
		"), and handing them out without it would start every one of them on material you do not have. Nothing is cancelled; carry on with the work in your own hands."
}
