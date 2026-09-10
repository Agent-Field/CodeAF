package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// YOUR OWN COPIES OF THE FILES THE TASK WROTE.
//
// 2026-09-09: a divided task wrote fifteen files, and the same fifteen files
// were already sitting UNTRACKED in the person's folder — the model had copied
// them there during its own turn. The merge was refused by git before it
// started, the node landed asking `conflicts with your branch`, which was not
// what happened, and the one answer on the card — `[a] resolve it` — spent a
// merge round that merges BRANCHES and could not see an untracked file at all,
// so it would have refused in exactly the same words a second time (#767).

// THE ROAD HAS ITS OWN SENTENCE, and it is a fact on the landing rather than a
// reading of the report's prose.
func TestYourOwnUntrackedCopiesAskTheirOwnQuestion(t *testing.T) {
	held := ProjectTask(TaskFacts{
		State: TaskUnverified, Merge: mergeConflicted, GroundHeld: true,
		Conflicts: []string{"leads.md", "research/method.md"},
	})
	if held.Ask.Kind != TaskAskConflict {
		t.Fatalf("the road asks %q, want the conflict's one question", held.Ask.Kind)
	}
	if want := taskAskGroundReason + ": leads.md, research/method.md"; held.Ask.Reason != want {
		t.Fatalf("the reason reads %q, want %q", held.Ask.Reason, want)
	}
	if held.Ask.Yes != taskAskConflictYes || held.Ask.No != taskAskConflictNo {
		t.Fatalf("the answers are %q / %q, want the conflict's two", held.Ask.Yes, held.Ask.No)
	}
	// AND THE YES SAYS WHAT IT WILL DO, because `resolve it` means a merge round
	// everywhere else on this table and here it moves files of the person's own.
	if held.Ask.Consequence == "" || !strings.Contains(held.Ask.Consequence, ".yours") {
		t.Fatalf("the yes does not say what it will do to their files: %q", held.Ask.Consequence)
	}
	// AND EVERY OTHER ROAD LEAVES IT EMPTY — the emptiness law, said about a
	// sentence.
	plain := ProjectTask(TaskFacts{State: TaskUnverified, Merge: mergeConflicted,
		Conflicts: []string{"parser.go"}})
	if plain.Ask.Consequence != "" {
		t.Fatalf("a branch conflict carries a consequence it does not need: %q", plain.Ask.Consequence)
	}
	if !strings.HasPrefix(plain.Ask.Reason, taskAskConflictReason) {
		t.Fatalf("a branch conflict stopped saying so: %q", plain.Ask.Reason)
	}
}

// THE LANDING REFUSES AND SAYS WHICH ROAD IT REFUSED ON. A landing may not move
// somebody's unfinished work without being asked, so it names the files, leaves
// the tree exactly as it was, and marks the road the person's own answer can
// spend (groundcarry.go).
func TestALandingWillNotMoveYourUntrackedCopiesByItself(t *testing.T) {
	place, repo := newOwnedPlace(t)
	tree, err := prepareTaskTree(place, repo, "eeee5555eeee5555", 1, "write the sheet")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "sheet.md"), "the task's sheet\n")
	// The person's own copy, never committed and never staged: git is not
	// watching it at all, which is the whole of the shape.
	writeFile(t, filepath.Join(repo, "sheet.md"), "my own draft\n")

	merge, detail, clashing, why := tree.comeHome("write the sheet", []string{"sheet.md"})
	if merge != mergeConflicted {
		t.Fatalf("merge = %q (%s), want it refused", merge, detail)
	}
	if why != refusedByYourFiles {
		t.Fatalf("the refusal is %v, want the one only the person can get past", why)
	}
	if !containsString(clashing, "sheet.md") {
		t.Fatalf("the refusal names %v, want the file it was about", clashing)
	}
	if got := readFile(t, filepath.Join(repo, "sheet.md")); got != "my own draft\n" {
		t.Fatalf("the person's own copy reads %q — a landing wrote over it", got)
	}
}

// AND THEIR OWN WORD CARRIES THEM ASIDE. Nothing of the person's is deleted:
// the task's version lands where it was written and their copy is kept beside
// it wearing `.yours`.
func TestTheirWordCarriesTheirCopiesAsideAndKeepsBoth(t *testing.T) {
	place, repo := newOwnedPlace(t)
	tree, err := prepareTaskTree(place, repo, "ffff6666ffff6666", 1, "write the sheet")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "sheet.md"), "the task's sheet\n")
	writeFile(t, filepath.Join(tree.dir, "notes.md"), "the task's notes\n")
	writeFile(t, filepath.Join(repo, "sheet.md"), "my own draft\n")
	writeFile(t, filepath.Join(repo, "notes.md"), "my own notes\n")

	merge, detail, _, _ := carryOnTheirWord(tree).comeHome("write the sheet", []string{"sheet.md", "notes.md"})
	if !cameHome(merge) {
		t.Fatalf("merge = %q (%s), want it home on their word", merge, detail)
	}
	if got := readFile(t, filepath.Join(repo, "sheet.md")); got != "the task's sheet\n" {
		t.Fatalf("the task's version did not land: %q", got)
	}
	if got := readFile(t, filepath.Join(repo, "sheet.md"+groundYoursSuffix)); got != "my own draft\n" {
		t.Fatalf("the person's own copy was not kept beside it: %q", got)
	}
	if got := readFile(t, filepath.Join(repo, "notes.md"+groundYoursSuffix)); got != "my own notes\n" {
		t.Fatalf("the second copy was not kept: %q", got)
	}
	if !strings.Contains(detail, groundYoursSuffix) {
		t.Fatalf("the report does not say where their copies went:\n%s", detail)
	}
}

// AND A COPY THE TASK DID NOT WRITE GOES BACK EXACTLY WHERE IT WAS.
func TestACarriedCopyThatDoesNotClashGoesStraightBack(t *testing.T) {
	place, repo := newOwnedPlace(t)
	tree, err := prepareTaskTree(place, repo, "aaaa7777aaaa7777", 1, "write the sheet")
	if err != nil {
		t.Fatalf("prepareTaskTree: %v", err)
	}
	writeFile(t, filepath.Join(tree.dir, "sheet.md"), "the task's sheet\n")
	writeFile(t, filepath.Join(repo, "sheet.md"), "my own draft\n")

	// The merge is offered their word, but the branch it lands is one that never
	// touched the file: the copy has nothing to stand beside, so it stands where
	// it was.
	mustGit(t, tree.dir, "rm", "-f", "--cached", "--ignore-unmatch", "sheet.md")
	_ = os.Remove(filepath.Join(tree.dir, "sheet.md"))
	writeFile(t, filepath.Join(tree.dir, "other.md"), "the task's other file\n")

	merge, detail, _, _ := carryOnTheirWord(tree).comeHome("write the sheet", []string{"other.md"})
	if !cameHome(merge) {
		t.Fatalf("merge = %q (%s), want it home", merge, detail)
	}
	if got := readFile(t, filepath.Join(repo, "sheet.md")); got != "my own draft\n" {
		t.Fatalf("the person's untouched copy reads %q", got)
	}
	if _, err := os.Lstat(filepath.Join(repo, "sheet.md"+groundYoursSuffix)); err == nil {
		t.Fatal("a copy nothing clashed with was renamed anyway")
	}
}

// ── what the model is told it did ───────────────────────────────────────────

// A TOOL REPLY IS A READING OF THE WORLD AND NEVER A RESTATEMENT OF THE REQUEST.
// The model called `tasks 1 resolve accept` on a node whose branch would not
// land, and was told "Its branch has come home and anything waiting on it can
// start" — so it told the person the task was done. It was not (#767).
func TestTheAcceptReplySaysWhatActuallyHappened(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "write the sheet"}}
	graph.mu.Lock()
	graph.nodes[1] = node
	graph.order = append(graph.order, 1)
	graph.mu.Unlock()

	// The landing was refused by the person's own copies: still their call.
	node.heldByYourFiles()
	node.clashesWith([]string{"sheet.md"})
	node.finish("", []string{"sheet.md"}, "task/write-the-sheet", mergeConflicted)
	graph.mu.Lock()
	node.state = TaskUnverified
	graph.mu.Unlock()

	said := resolvedReply("1", node, TaskAccept)
	if !strings.Contains(said, "still your call") {
		t.Fatalf("the reply does not say the question is standing:\n%s", said)
	}
	if !strings.Contains(said, taskAskGroundReason) {
		t.Fatalf("the reply does not carry the reason the row carries:\n%s", said)
	}
	if !strings.Contains(said, "task/write-the-sheet was kept") {
		t.Fatalf("the reply does not say where the work is:\n%s", said)
	}
	for _, banned := range []string{"come home", "merged into yours"} {
		if strings.Contains(said, banned) {
			t.Fatalf("the reply claims %q about a branch that did not land:\n%s", banned, said)
		}
	}

	// AND A LANDING THAT DID COME HOME SAYS SO, with the branch named.
	graph.mu.Lock()
	node.state = TaskDone
	node.merge = mergeMerged
	graph.mu.Unlock()
	home := resolvedReply("1", node, TaskAccept)
	if !strings.Contains(home, "task/write-the-sheet merged into yours") {
		t.Fatalf("a landing that came home does not say so:\n%s", home)
	}
}

// AND A REFUTE THAT DID NOT TAKE SAYS SO TOO, in the words every surface uses:
// `incomplete`, never the courtroom's.
func TestTheNotRightReplyUsesThePersonFacingWords(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 2, spec: taskSpec{title: "port the parser"}}
	graph.mu.Lock()
	graph.nodes[2] = node
	graph.order = append(graph.order, 2)
	node.state = TaskFailed
	node.branch = "task/port-the-parser"
	graph.mu.Unlock()

	said := resolvedReply("2", node, TaskRefute)
	if !strings.Contains(said, "incomplete on your word") {
		t.Fatalf("the reply does not say what became of it:\n%s", said)
	}
	for _, banned := range []string{"refuted", "failed"} {
		if strings.Contains(said, banned) {
			t.Fatalf("the reply uses the machinery's word %q:\n%s", banned, said)
		}
	}
}

// THE RECEIPT SAYS WHO TOOK IT. Five children were accepted by the MODEL under
// `task.settle = auto` and every one of them carried "you looked at this
// yourself and took it as done" into the person's own transcript.
func TestTheAcceptReceiptSaysWhoTookIt(t *testing.T) {
	if got := acceptedLine("", TaskAskOwnerPerson); got != acceptedByYou {
		t.Fatalf("the person's own press reads %q, want %q", got, acceptedByYou)
	}
	if got := acceptedLine("", TaskAskOwnerModel); got != acceptedByAforge {
		t.Fatalf("the model's own verb reads %q, want %q", got, acceptedByAforge)
	}
	if got := acceptedLine("I read the diff", TaskAskOwnerModel); !strings.HasPrefix(got, acceptedByAforge+": ") {
		t.Fatalf("the reason lost its voice: %q", got)
	}
	if got := refutedLine("", TaskAskOwnerModel); !strings.Contains(got, notRightByAforge) {
		t.Fatalf("the not-right receipt reads %q, want it to name aforge", got)
	}
	if got := refutedLine("", TaskAskOwnerPerson); !strings.Contains(got, notRightByYou) {
		t.Fatalf("the person's own not-right reads %q", got)
	}
}

// ── the question, put to somebody ───────────────────────────────────────────

// EVERY MOVE OF A NODE IS A MOVE OF ITS QUESTION. Nothing published a landing
// question at all before this: [Agent.OpenQuestions] derived one, and the only
// thing that ever put one in front of a surface was the replay a watcher gets
// when it attaches — so a landing that happened, or changed shape, while a
// window was already open reached nobody (#767).
func TestALandingRaisesItsQuestionAndTakesItBack(t *testing.T) {
	agent := &Agent{}
	lane, stop := agent.WatchQuestions()
	defer stop()

	agent.publishLandingQuestion(TaskNotice{
		ID: 1, Title: "write the sheet", State: TaskUnverified,
		Merge: mergeConflicted, GroundHeld: true, Conflicts: []string{"sheet.md"},
	})
	ev := <-lane
	if ev.Kind != EventQuestion || ev.Question == nil {
		t.Fatalf("a landing raised %v", ev.Kind)
	}
	if ev.Question.Kind != QuestionConflict {
		t.Fatalf("the question is on the %q lane, want the conflict's", ev.Question.Kind)
	}
	if !strings.Contains(ev.Question.Reason, taskAskGroundReason) {
		t.Fatalf("the question does not carry the row's own sentence: %q", ev.Question.Reason)
	}
	if len(ev.Question.Options) != 3 || ev.Question.Options[0].Key != LandingYesKey {
		t.Fatalf("the answers are not task-states' own: %+v", ev.Question.Options)
	}
	if ev.Question.Options[0].Consequence == "" {
		t.Fatalf("the yes does not say what it will do: %+v", ev.Question.Options[0])
	}

	// AND IT IS TAKEN BACK WHEN THE WORK SETTLES, in the kind it was raised in.
	agent.publishLandingQuestion(TaskNotice{ID: 1, Title: "write the sheet", State: TaskDone, Merge: mergeMerged})
	gone := <-lane
	if gone.Kind != EventQuestionWithdrawn || gone.Question.Kind != QuestionConflict {
		t.Fatalf("the settle retired %v on the %q lane", gone.Kind, gone.Question.Kind)
	}
	// AND A SETTLED NODE RAISES NOTHING FURTHER.
	agent.publishLandingQuestion(TaskNotice{ID: 1, Title: "write the sheet", State: TaskDone, Merge: mergeMerged})
	select {
	case again := <-lane:
		t.Fatalf("a settled node published %v again", again.Kind)
	default:
	}
}

// AND WHAT THE LANDING WAS ASKING ABOUT SURVIVES THE PROCESS. The names were
// read out of an index git has since thrown away and the road is a fact about a
// merge that has already happened, so a checkpoint without them came back with
// the question intact and its sentence hollowed out.
func TestTheLandingsRoadAndFilesRideTheCheckpoint(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "write the sheet"}}
	graph.mu.Lock()
	graph.nodes[1] = node
	graph.order = append(graph.order, 1)
	graph.mu.Unlock()
	node.clashesWith([]string{"sheet.md", "notes.md"})
	node.heldByYourFiles()

	graph.mu.Lock()
	record := node.recordLocked()
	graph.mu.Unlock()
	if !record.GroundHeld || len(record.Clashing) != 2 {
		t.Fatalf("the checkpoint forgot the road or the files: %+v", record)
	}
	back := restoreNode(graph, record)
	if !back.groundHeld || !containsString(back.clashing, "sheet.md") {
		t.Fatalf("the restored node lost the road or the files: %+v", back)
	}
}

// THE LANDING ASKS ON ONE ROW, AND PROMOTES ONLY WHERE IT HAS SOMETHING MORE TO
// SAY.
//
// docs/design/questions/DESIGN.md's defaults table says `card / room` beside
// this kind AND says why in the same cell — `task-states row unchanged`. That
// row is `[a] <yes> · [n] <no> · [s] tell it`, three columns in one order, which
// is the LINE form: the card spends a row per answer and never composes it. The
// head, the facts and the reason of a landing are already drawn by the card the
// surface lands in the transcript, so a second head above the box would be two
// renderings of one thing rather than more evidence.
//
// The one road that promotes is the one with a consequence beside its yes — the
// person's own uncommitted copies, whose `resolve it` MOVES FILES OF THEIRS —
// because a consequence is drawn beside its answer on the card and nowhere on a
// row, and forms promote and never demote.
func TestALandingAsksOnOneRowAndPromotesForAConsequence(t *testing.T) {
	plain := ProjectTask(TaskFacts{State: TaskUnverified, Merge: mergeAborted})
	if got := landingForm(plain.Ask); got != FormLine {
		t.Fatalf("an ordinary landing asks in the %q form, want %q", got, FormLine)
	}
	conflict := ProjectTask(TaskFacts{State: TaskUnverified, Merge: mergeConflicted,
		Conflicts: []string{"parser.go"}})
	if got := landingForm(conflict.Ask); got != FormLine {
		t.Fatalf("a branch conflict asks in the %q form, want %q", got, FormLine)
	}
	held := ProjectTask(TaskFacts{State: TaskUnverified, Merge: mergeConflicted,
		GroundHeld: true, Conflicts: []string{"leads.md"}})
	if got := landingForm(held.Ask); got != FormCard {
		t.Fatalf("the road that moves the person's own files asks in the %q form, want %q", got, FormCard)
	}
	// AND THE CONSEQUENCE RIDES THE ANSWER IT BELONGS TO, once, on the yes — so
	// the card draws it beside `resolve it` and nothing anywhere restates it.
	for _, option := range landingOptions(held.Ask) {
		if option.Key == LandingYesKey && option.Consequence != held.Ask.Consequence {
			t.Fatalf("the yes carries %q, want %q", option.Consequence, held.Ask.Consequence)
		}
		if option.Key != LandingYesKey && option.Consequence != "" {
			t.Fatalf("%q carries a consequence it did not earn: %q", option.Key, option.Consequence)
		}
	}
}
