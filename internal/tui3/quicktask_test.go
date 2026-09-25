package tui3

// ── A QUICK NODE, AS THIS SURFACE DRAWS IT ──────────────────────────────────
//
// A quick task (session's TaskKindQuick, docs/design/quick-task/DESIGN.md) is
// work that runs WHERE THE PERSON IS: the caller's own folder, no worktree, no
// branch, no merge and no landing of its own — its last message is its answer.
// So everything this column and this card usually say about a delivery is a
// claim about machinery a quick node never went near, and these pin the two
// halves of that: what the row DOES say while it runs, and what the card must
// NOT say when it lands.
//
// The row needs no code of its own, which is the point of the first test: the
// engine's `Doing` line already replaces the state word for every kind that
// names its own moments (session's TaskNotice.Doing, drawn by [app.railDoing]),
// and a quick node's phases are `quick · 2/4 · <next item>`.

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// quickDoing is the line the engine publishes on a quick node while it works
// through its items, spelled here the way session's own quickDoing spells it so
// a change to that shape shows up as a failure and not as a silent difference.
const quickDoing = "quick · 1/3 · read the second file"

// quickNotice is one quick node as the engine publishes it: the kind, the doing
// line, the in-place mode — and nothing at all about a branch or a merge.
func quickNotice() session.TaskNotice {
	return session.TaskNotice{
		Kind:  session.TaskKindQuick,
		Doing: quickDoing,
		Mode:  session.TaskModeInPlace,
	}
}

// THE ROW IS THE DOING LINE, AND IT NAMES NO BRANCH. What a person watching the
// column wants off a quick node is which of its items it is on; what they must
// never be told is that a branch is waiting for them somewhere.
func TestAQuickRowSaysWhatItIsDoingAndNamesNoBranch(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(7, "compare the four files", session.TaskRunning, quickNotice()))

	node := a.tasks[7]
	if node == nil {
		t.Fatal("the quick node never reached the roster")
	}
	if node.kind != session.TaskKindQuick {
		t.Fatalf("the row was filed as kind %q", node.kind)
	}
	rows := a.railUnder(node, 60)
	if len(rows) == 0 || !strings.Contains(plain(rows[0]), quickDoing) {
		t.Fatalf("the row does not say %q:\n%q", quickDoing, plain(strings.Join(rows, " / ")))
	}
	// AND IT OPENS WITH THE DOING LINE ITSELF. `working · ` and `finishing · `
	// are this surface's words for which part of running a node is in; a quick
	// node's phase IS what it is doing, so nothing of this surface's own stands
	// in front of it (the same law [TestADesignsPhaseIsWhatTheSurfaceSays] pins).
	if got := plain(rows[0]); !strings.HasPrefix(got, "quick") {
		t.Fatalf("the doing line was prefixed with a word about running: %q", got)
	}
	drawn := strings.Join(railText(a, a.viewHeight()), "\n")
	for _, never := range []string{"branch", taskBranchKept, mergeScreenWord(mergeWordMerged)} {
		if strings.Contains(drawn, never) {
			t.Fatalf("the column says %q over work that has no branch:\n%s", never, drawn)
		}
	}
}

// AND A QUICK NODE STARTED FROM A TASK IS FILED UNDER THAT TASK. Nesting is
// [session.TaskNotice.Parent] and nothing else, so a quick node the engine
// files under its caller carries its caller with no surface change. The column
// draws what each piece of work is doing rather than a tree, so the quick node
// is its own one-line row in the running group beside its caller.
func TestAQuickNodeUnderATaskIsFiledUnderItsParent(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(1, "Ship the port", session.TaskRunning, session.TaskNotice{}))
	notice := quickNotice()
	notice.Parent = 1
	a.taskUpdate(update(2, "compare files", session.TaskRunning, notice))

	if got := a.tasks[2].parent; got != "1" {
		t.Fatalf("the quick node's parent is %q, want the task it was started from", got)
	}
	rows := railText(a, a.viewHeight())
	for _, title := range []string{"compare files", "Ship the port"} {
		row, ok := railRowFor(a, a.viewHeight(), title)
		if !ok {
			t.Fatalf("%q has no row:\n%s", title, strings.Join(rows, "\n"))
		}
		if strings.Contains(row, "└─") || strings.Contains(row, "├─") {
			t.Fatalf("the column drew a tree:\n%s", strings.Join(rows, "\n"))
		}
	}
}

// THE CARD IS ITS ANSWER, AND IT IS NOTHING ELSE. A quick node lands with its
// worker's last message and no branch, no merge word and — unless it wrote —
// no changed list. Every one of those rows is drawn only when there is
// something to draw (the emptiness law), and this is the test that says so out
// loud, because "the branch row is missing" is exactly the sort of absence
// somebody later mistakes for a bug and fills in.
func TestAQuickLandingShowsItsAnswerAndNoBranchOrMergeRow(t *testing.T) {
	a, _, advance := roomApp(t)
	a.width = 80
	advance(42 * time.Second)
	notice := quickNotice()
	notice.Doing = ""
	notice.Result = "keys.go and keys_test.go agree; parse.go is the odd one out."
	notice.ResultWhole = notice.Result
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "compare the four files", session.TaskDone, notice)})
	clickHit(t, a, hitDone)

	text := taskText(a)
	if !strings.Contains(text, "parse.go is the odd one out.") {
		t.Fatalf("the open card does not carry the worker's last message:\n%s", text)
	}
	for _, never := range []string{doneBranchLabel, doneChangedLabel, taskBranchKept,
		mergeScreenWord(mergeWordMerged), mergeScreenWord(mergeWordConflicted)} {
		if never == "" {
			continue
		}
		if strings.Contains(text, never) {
			t.Fatalf("the card says %q over work that had no branch:\n%s", never, text)
		}
	}
	// AND THE FACTS THE CARD IS BUILT FROM CARRY THE KIND, so every reading made
	// off them — the head's word, the tier, the question — is made about a quick
	// node and not about an ordinary one.
	node := a.tasks[7]
	if node == nil {
		t.Fatal("the landing left no node behind")
	}
	if got := doneNodeFacts(node).Kind; got != session.TaskKindQuick {
		t.Fatalf("the card's facts carry kind %q", got)
	}
}

// AND A QUICK NODE THAT WROTE SOMETHING SAYS SO. The `changed · ` row is silent
// because there is usually nothing in it, not because the card refuses to draw
// it for the kind — a quick node may name files it will write, and when it
// writes them the row is owed.
func TestAQuickLandingThatWroteFilesNamesThem(t *testing.T) {
	a, _, advance := roomApp(t)
	a.width = 80
	advance(42 * time.Second)
	notice := quickNotice()
	notice.Doing = ""
	notice.Result = "the note is written."
	notice.Changed = []string{"notes/summary.md"}
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "write the summary", session.TaskDone, notice)})
	clickHit(t, a, hitDone)

	text := taskText(a)
	if !strings.Contains(text, doneChangedLabel+"notes/summary.md") {
		t.Fatalf("the card never names the file it wrote:\n%s", text)
	}
	if strings.Contains(text, doneBranchLabel) {
		t.Fatalf("the card named a branch for work done in place:\n%s", text)
	}
}

// STOPPING A QUICK TASK PROMISES WHAT IS ACTUALLY TRUE OF IT. An ordinary
// node's card says the branch it wrote on is kept; a quick node has no branch
// and never had one — it works in the folder the person is already in — so the
// honest sentence is where its work already is.
func TestStoppingAQuickTaskDoesNotPromiseABranch(t *testing.T) {
	a, _, _ := taskApp(t)
	a.taskUpdate(update(7, "compare the four files", session.TaskRunning, quickNotice()))

	target := a.stopTaskTarget(a.tasks[7])
	if target.empty() {
		t.Fatal("a running quick task cannot be stopped")
	}
	if target.id != session.CancelTask+":7" {
		t.Fatalf("the stop is aimed at %q", target.id)
	}
	if target.detail != stopQuickDetail {
		t.Fatalf("the card says %q", target.detail)
	}
	if strings.Contains(target.detail, "branch") {
		t.Fatalf("the quick card still promises a branch: %q", target.detail)
	}
	// And an ordinary node is untouched by any of this.
	a.taskUpdate(update(8, "Fix the crash", session.TaskRunning, session.TaskNotice{}))
	if got := a.stopTaskTarget(a.tasks[8]).detail; got != stopTaskDetail {
		t.Fatalf("an ordinary task's card says %q", got)
	}
}

// AND A SETTLED QUICK NODE IS OFFERED NO SECOND ATTEMPT. The room's model and
// effort pickers exist to point a settled task at different hands and run it
// again; a quick node has no shape left to re-run, exactly as a saved shape
// being made or being run has none (roompanel.go's [taskSetupAvailable]).
func TestASettledQuickNodeOffersNoSecondAttempt(t *testing.T) {
	a, _, _ := taskApp(t)
	notice := quickNotice()
	notice.Doing = ""
	a.taskUpdate(update(7, "compare the four files", session.TaskDone, notice))
	if taskSetupAvailable(a.tasks[7]) {
		t.Fatal("a landed quick node is offered a re-run it cannot have")
	}
	// AND AN ORDINARY LANDING STILL IS, so the exclusion is the kind's and not a
	// gate that closed over every settled node.
	a.taskUpdate(update(8, "Fix the crash", session.TaskDone, session.TaskNotice{}))
	if !taskSetupAvailable(a.tasks[8]) {
		t.Fatal("an ordinary landing lost its picker")
	}
	// A RUNNING QUICK NODE IS UNTOUCHED: it is live work, and the pickers over
	// live work are a different question this predicate answers one clause up.
	a.taskUpdate(update(9, "compare two more", session.TaskRunning, quickNotice()))
	if !taskSetupAvailable(a.tasks[9]) {
		t.Fatal("a running quick node was excluded as though it had settled")
	}
}
