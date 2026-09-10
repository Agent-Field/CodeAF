//go:build e2e

package e2e

// taskstates_e2e_test.go IS THE ACCEPTANCE OF docs/design/task-states/DESIGN.md
// ON A REAL SCREEN.
//
// The ruling is one sentence: every task row, card, rail line and roster entry
// answers ONE question before it says anything else — do I need to do anything —
// and there are exactly three answers, each with one glyph and one word. Six
// shapes carry the whole of it, and every one of them is measured here through
// the real binary, in a real terminal, against the model the suite is pinned to.
//
// WHY IT IS NOT ANOTHER TABLE OF UNIT TESTS. internal/tui3 and internal/session
// both pin these words against a struct they built themselves, which is the
// right gate and cannot answer the question this file exists for: a person at a
// terminal, looking at a landing, reads ONE spelling of one state. Three of the
// four surfaces that disagreed before this wave were each green in their own
// package on the day they disagreed.
//
// WHICH SHAPES PAY FOR A MODEL AND WHICH DO NOT. A shape whose whole subject is
// the DRAWING of a landing is seeded as the graph a finished process left behind
// (tmux_test.go's seedDecidedFamily says why) and settled through the real engine
// door by a real keystroke — no model is asked anything, and what is measured is
// the surface and the engine. A shape whose subject is the WORK — a task that
// really runs, really writes a file and really lands — is run against
// deepseek-v4-flash, because there is no other way to find out what the card says
// about a landing nobody staged.
//
//	go test -tags e2e -run TestTaskStatesE2E -count=1 -timeout 40m -v ./internal/e2e/

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTaskStatesE2E(t *testing.T) {
	requireTmuxAndKey(t)

	t.Run("a_landing_that_worked_says_done_once", testStatesDone)
	t.Run("your_call_asks_in_three_columns_and_a_accepts_it", testStatesYourCall)
	t.Run("a_branch_that_clashes_with_yours", testStatesConflict)
	t.Run("a_run_out_of_steps_is_incomplete_with_its_reason", testStatesIncomplete)
	t.Run("the_auto_settle_floor_hands_a_restart_back", testStatesAutoFloorAcrossARestart)
	t.Run("the_auto_settle_floor_hands_it_back", testStatesAutoFloor)
	t.Run("the_rail_and_the_roster_say_the_same_word", testStatesRailAndRoster)
}

// ── 1 · done ────────────────────────────────────────────────────────────────

// testStatesDone is the `over` tier's happy row, and it is the one shape here
// that must really run: what a landing card says about work that finished is not
// knowable from a fixture, because the span, the file count and the merge fact
// are all facts the engine publishes about work that actually happened.
//
// IT IS THE CHEAPEST TASK THERE IS — `/task solo` on a one-file brief, the shape
// [testTaskRoomKeepsSpace] already pays for, which lands in about thirty seconds.
//
// THE HEAD IS READ AS A WHOLE ROW AND NOT AS FOUR SEARCHES. The design's law is
// about ORDER — tier glyph, kind glyph, title, tier word, then span, files, merge
// fact, branch, and nothing else — so the assertions are made against the one
// line that carries the glyph, which is the only way a test can tell `done` on
// the head from `done` in a sentence the model wrote three rows above it.
func testStatesDone(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "donews", false)
	r := start(t, "afe2e_states_done", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	r.lit("/task solo write a file called hello.txt containing the word hello")
	r.keys("Enter")

	// THE CARD IS THE WAIT, not the file: a file on disk says the worker wrote
	// something and says nothing at all about what the surface then drew.
	screen := r.waitFor(6*time.Minute, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	t.Logf("the landing card of work that finished:\n%s", screen)

	head := statesHeadLine(screen, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	if head == "" {
		t.Fatalf("nothing on the screen is a head row carrying both %q and %q:\n%s",
			say(t, "taskDoneGlyph"), say(t, "taskDoneWord"), screen)
	}
	t.Logf("HEAD · %s", head)

	// THE SPAN IS ON THE HEAD AND IT IS A DURATION. It cannot be spelled here —
	// it is how long a real model really took — so what is asserted is that the
	// head carries one at all, in the shape taskSpanWord writes.
	if !statesSpan.MatchString(head) {
		t.Errorf("the head carries no span:\n\t%s", head)
	}
	// AND THE FILE COUNT IS SINGULAR, because the brief asked for one file and
	// `1 files` is the surface being sloppy about the one number on the row.
	if !strings.Contains(head, say(t, "taskOneFileWord")) {
		t.Errorf("the head does not say %q about a one-file brief:\n\t%s", say(t, "taskOneFileWord"), head)
	}
	// THE MERGE IS A FACT, AND THE ABSENCE OF ONE IS ALSO A FACT. `merged` is work
	// that branched and came home, `branch kept` is a branch still standing, and
	// nothing at all is work done in the ground itself with nowhere to land — the
	// emptiness law, not a failure. All three are the product behaving, so which
	// one arrived is recorded rather than demanded: what the ruling fixes is that
	// the head says WHERE THE WORK WENT and never says it as a state, and
	// `delivery needs attention` and `stopped — branch kept` are the phrases that
	// went (they are on [statesDeleted]).
	switch {
	case strings.Contains(head, say(t, "taskMergedFact")):
		t.Logf("the work branched and came home: the head carries %q", say(t, "taskMergedFact"))
	case strings.Contains(head, say(t, "taskBranchKeptFact")):
		t.Logf("the work is on a branch that is still standing: the head carries %q", say(t, "taskBranchKeptFact"))
	default:
		t.Logf("the work was done in place: no merge fact on the head, which is the emptiness law")
	}
	// AND THE WORDS THIS WAVE DELETED ARE NOT ON THE CARD.
	statesNoDeletedWords(t, screen)
	r.quit()
}

// ── 2 · your call, and the accept ───────────────────────────────────────────

// testStatesYourCall is the `your call` tier in full: one glyph, one word, the
// reason under it, and the three columns that answer it — then the key, spent
// through the real engine door, and the receipt it leaves.
//
// THE THREE COLUMNS ARE WAITED FOR AS ONE STRING. Waiting for `[a] accept` and
// `[n] not right` separately would pass on a card that drew them on two
// different rows, or in the other order, or with the third column missing — and
// the third column is the one the ruling is emphatic about, because a person
// with something to say who finds only yes and no presses one of them.
func testStatesYourCall(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "ask"})
	ws := newWorkspace(t, "yourcallws", false)
	seedUnchecked(t, home, ws, 0x3000000000000004)
	r := start(t, "afe2e_states_yourcall", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	screen := r.waitFor(30*time.Second,
		say(t, "unverifiedGlyph"), say(t, "taskLookWord"),
		say(t, "settleAskWord"), say(t, "settleAnswersRow"))
	t.Logf("a landing that is the person's call, asking in one frame:\n%s", screen)
	statesNoDeletedWords(t, screen)

	// AND THE HAND-OVER IS DRAWN BESIDE THEM, dimmer, as the one-time offer it
	// is. It is the row that replaced `decide these for me`, which was a standing
	// preference disguised as an answer.
	if !strings.Contains(screen, say(t, "settleHandRow")) {
		t.Errorf("the answers row does not offer %q:\n%s", say(t, "settleHandRow"), screen)
	}

	// THE KEY, ON THE SELECTED CARD, over an empty box — the guards `x` has.
	if !statesAnswerKey(t, r, "a", say(t, "settleTookLine")) {
		t.Fatalf("three presses of `a` never left %q on the card:\n%s", say(t, "settleTookLine"), r.capture())
	}
	settled := r.waitFor(30*time.Second, say(t, "settleTookLine"))
	t.Logf("the accept was spent and the card wears the receipt:\n%s", settled)

	// AND THE RESOLUTION LANDS AS ITS OWN CARD. The head of a landed card is
	// never rewritten, so what says the work is done now is a second card under
	// the first.
	after := r.waitFor(60*time.Second, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
	t.Logf("the accepted work landed again as done:\n%s", after)
	if line := statesHeadLine(after, say(t, "taskDoneGlyph"), say(t, "taskDoneWord")); line != "" {
		t.Logf("HEAD · %s", line)
	}
	r.quit()
}

// ── 3 · a conflict ──────────────────────────────────────────────────────────

// testStatesConflict is the one your-call question that is NEVER the model's:
// two versions of somebody's own file, which no policy about who decides
// unchecked work is an answer to.
//
// IT MAKES A REAL ONE. The task is sent at a file that already exists, and the
// person's own branch is moved out from under it while the work runs — which is
// exactly the shape a person creates by carrying on working in their checkout
// while a task is out. A seeded `merge: conflicted` would draw the card without
// ever proving the engine reaches it.
//
// AND THE ENGINE HAS ONE ROUND TO WIN. The design gives a clashing branch one
// resolver round before anybody is asked, so there are two honest endings: the
// round resolved it and the card says `done`, or the round failed and the card
// asks with the files named. BOTH ARE THE PRODUCT BEHAVING, so this waits for
// either, asserts the one that happened is drawn the way the ruling says, and
// logs which. A third outcome — the work landed before the person's commit ever
// reached the branch — is neither, and it is skipped out loud rather than
// reported as a pass.
func testStatesConflict(t *testing.T) {
	home := newHome(t, nil)
	ws := newWorkspace(t, "conflictws", false)
	// The file both sides will write. It is committed first so that the task's
	// branch and the person's branch have a shared parent to disagree about.
	statesCommit(t, ws, "notes.txt", "one\n", "seed the file")

	// AND THE CHECKOUT IS MOVED OFF ITS TRUNK BEFORE ANYTHING RUNS, which is what
	// makes this shape reachable at all. `main` is on the protected list
	// (internal/session's task_branch_protection.go), and a landing onto a
	// protected checkout keeps its branch and never merges — so on the repository
	// [newWorkspace] builds, nothing the person did could ever clash with
	// anything. That is the engine behaving; it is the FIXTURE that was wrong.
	statesWorkBranch(t, ws)

	r := start(t, "afe2e_states_conflict", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)
	r.lit("/task solo replace the whole contents of notes.txt with the single line: from the task")
	r.keys("Enter")

	// THE PERSON KEEPS WORKING, which is the whole of how this happens. The
	// commit goes onto their branch while the worker is still out, so the landing
	// finds a file that has moved.
	//
	// THE SIGNAL IS THE TASK'S OWN BRANCH and not the screen. A worktree is cut
	// from the person's repository, so the ref lands in their refs/heads the
	// moment the work is really out — which is a fact about the work rather than
	// a sentence a model happened to write, and it is the only moment at which
	// committing over the same file can still clash with anything.
	if !statesWaitForTaskBranch(t, ws, 4*time.Minute) {
		t.Skipf("no task branch was cut in four minutes, so there was nothing for the person's own edit to clash with:\n%s", r.capture())
	}
	// THE PERSON'S EDIT IS NOT COMMITTED, AND THAT IS THE WHOLE OF WHAT MAKES A
	// CLASH POSSIBLE. A commit on the checkout while the work is out MOVES the
	// branch, and a branch that moved since the cut is one aforge will not write
	// either ([keptLandingSentence]'s last arm) — so a committing fixture buys the
	// same `branch kept` the protected trunk did, one reason further along. An
	// open editor with unsaved-to-git changes in the file is the shape a person is
	// actually in while a task is out, and it is the shape the landing carries:
	// the work of theirs standing in the way is set aside, the branch merges, and
	// their own goes back on top (internal/session's groundcarry.go).
	t.Logf("the work is out on its own branch; writing over the same file in the person's checkout")
	statesEdit(t, ws, "notes.txt", "two, from the person\n")

	found, screen := statesAwait(r, 8*time.Minute,
		say(t, "taskConflictReason"), say(t, "taskDoneWord"))
	t.Logf("the conflict landed as %q:\n%s", found, screen)

	switch found {
	case say(t, "taskConflictReason"):
		// THE ROUND FAILED AND THE CARD ASKS. One glyph, one word, the reason with
		// the files hung off it, and a yes verb that is not an accept.
		asking := r.waitFor(60*time.Second,
			say(t, "unverifiedGlyph"), say(t, "taskLookWord"), say(t, "taskConflictReason"))
		t.Logf("a conflicted landing, asking:\n%s", asking)
		if !strings.Contains(asking, say(t, "settleConflictAnswers")) &&
			!strings.Contains(asking, say(t, "settleConflictNo")) {
			t.Errorf("a conflicted card offers neither %q nor %q:\n%s",
				say(t, "settleConflictAnswers"), say(t, "settleConflictNo"), asking)
		}
		if line := statesReasonLine(asking, say(t, "taskConflictReason")); line != "" {
			t.Logf("REASON · %s", line)
			if !strings.Contains(line, "notes.txt") {
				t.Logf("git named no files for this clash, so the sentence stops after the branch — the emptiness law")
			}
		}
		statesNoDeletedWords(t, asking)
	case say(t, "taskDoneWord"):
		// A LANDING, AND THE MERGE FACT SAYS WHETHER THERE WAS EVER A CLASH.
		head := statesHeadLine(screen, say(t, "taskDoneGlyph"), say(t, "taskDoneWord"))
		if head == "" {
			t.Skipf("the word `done` is on the screen but not on a landing head — no conflict was reached:\n%s", screen)
		}
		t.Logf("HEAD · %s", head)
		if strings.Contains(head, say(t, "taskBranchKeptFact")) {
			// THE BRANCH NEVER CAME HOME, so nothing was ever merged and nothing
			// could clash. This is NOT the merge round winning and it must not be
			// reported as one: the person's edit and the task's are still on two
			// sides that have never met.
			//
			// AND ON THIS CHECKOUT THERE IS NOTHING LEFT TO EXCUSE IT. It is on a
			// plain branch nothing has moved, so none of the four reasons a landing
			// keeps its branch applies (task_branch_protection.go) and the work had
			// a destination to go to.
			t.Fatalf("the landing kept its branch (%q) off a checkout that is on an ordinary "+
				"branch nothing has moved, so it had a destination and did not take it:\n\t%s",
				say(t, "taskBranchKeptFact"), head)
		}
		// It merged, so the clash either never happened or one round closed it —
		// and either way nobody was asked, which is what the round exists for.
		t.Logf("the branch came home and nobody was asked")
		statesNoDeletedWords(t, screen)
	default:
		t.Skipf("neither a conflict nor a landing arrived in eight minutes:\n%s", screen)
	}
	r.quit()
}

// ── 4 · incomplete ──────────────────────────────────────────────────────────

// testStatesIncomplete is `failed` being gone. A run that spends its step
// threshold is not a failure anybody found — nothing broke, the work simply ran
// out of road — so the row reads `incomplete` and hangs the reason off it, and
// the word `failed` may not be on the card at all.
//
// IT IS SEEDED, and the threshold is why. The `/task` door has no cap in its
// vocabulary — the number is a field of the model's own `task` tool
// (internal/session's task.go) — so provoking a spent threshold through the
// surface would mean asking a real model to choose a number, which is a coin
// toss dressed up as an acceptance. The ENDING is what this shape is about, and
// the ending is a fact the record carries.
func testStatesIncomplete(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "ask"})
	ws := newWorkspace(t, "stepsws", false)
	seedOutOfSteps(t, home, ws)
	r := start(t, "afe2e_states_steps", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	screen := r.waitFor(30*time.Second,
		say(t, "taskBadGlyph"), say(t, "taskIncompleteWord"), say(t, "taskStepsReason"))
	t.Logf("a run that spent its steps:\n%s", screen)
	if head := statesHeadLine(screen, say(t, "taskBadGlyph"), say(t, "taskIncompleteWord")); head != "" {
		t.Logf("HEAD · %s", head)
	} else {
		t.Errorf("nothing on the screen is a head carrying both %q and %q:\n%s",
			say(t, "taskBadGlyph"), say(t, "taskIncompleteWord"), screen)
	}
	if line := statesReasonLine(screen, say(t, "taskStepsReason")); line != "" {
		t.Logf("REASON · %s", line)
	}
	statesNoDeletedWords(t, screen)
	r.quit()
}

// ── 5 · the auto-settle floor ───────────────────────────────────────────────

// testStatesAutoFloor is the law that a task never stays unowned past the end of
// a turn.
//
// UNDER `task.settle = auto` a landing nobody could check is handed to the model,
// and the card must SAY SO rather than drawing no chips and no explanation —
// which is the exact defect the whole wave exists to close. The model then
// answers inside that turn or not at all, so there are two honest endings and
// this waits for either: the model spent a verb and a second card says what
// became of the work, or the turn ended unsettled and the floor handed the
// question back and the chips are drawn again.
//
// IT MUST REALLY RUN, and that is a fact about which half of the floor it
// measures. [testStatesAutoFloorAcrossARestart] above it seeds the holder and
// measures the half that is deterministic — a landing the record says aforge was
// deciding comes back the person's — and no fixture can catch the LIVE window
// where the model is still holding one, because that window is however long the
// model takes to answer. So this one runs the work and reads whichever of the two
// honest endings arrives; if the landing does not come home as anybody's call at
// all, there was nothing to hand over and the shape is skipped out loud.
func testStatesAutoFloor(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "auto"})
	ws := newWorkspace(t, "autows", false)
	r := start(t, "afe2e_states_auto", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	// A BRIEF WHOSE ACCEPTANCE NOBODY CAN SETTLE FROM THE FILES. What is wanted
	// is a landing that comes home unchecked; a claim about the world outside the
	// repository is the honest way to ask for one, and the work itself is still
	// one small file so the run stays cheap.
	r.lit("/task solo write a file called guess.txt holding your single best guess at what the weather will be in Reykjavik one year from today, and nothing else")
	r.keys("Enter")

	found, screen := statesAwait(r, 8*time.Minute,
		say(t, "taskAutoDecidingWord"), say(t, "settleAnswersRow"), say(t, "taskDoneWord"), say(t, "taskIncompleteWord"))
	t.Logf("the landing under `task.settle = auto` arrived as %q:\n%s", found, screen)

	switch found {
	case say(t, "taskAutoDecidingWord"):
		// THE CARD SAYS WHO IS HOLDING IT AND HOW TO TAKE IT BACK. One row, no
		// chips, and never a second prompt.
		held := r.waitFor(60*time.Second, say(t, "taskAutoDecidingWord"), say(t, "taskTakeItBackWord"))
		t.Logf("aforge is deciding, and the way back is on the row:\n%s", held)
		if line := statesReasonLine(held, say(t, "taskAutoDecidingWord")); line != "" {
			t.Logf("AUTO ROW · %s", line)
		}
		// AND THEN THE FLOOR. Either the model spends its verb and a second card
		// says what became of the work, or the turn ends and the question comes
		// back to the person with its chips drawn again.
		next, back := statesAwait(r, 4*time.Minute,
			say(t, "settleAnswersRow"), say(t, "taskDoneWord"), say(t, "taskIncompleteWord"))
		switch next {
		case say(t, "settleAnswersRow"):
			t.Logf("the turn ended unsettled and the floor handed the question back with its chips:\n%s", back)
		case "":
			t.Logf("the model never spent its verb and the turn had not ended in four minutes:\n%s", back)
		default:
			t.Logf("the model settled it inside its turn and the landing says %q:\n%s", next, back)
		}
		statesNoDeletedWords(t, back)
	case say(t, "settleAnswersRow"):
		t.Logf("the landing was the person's call from the start — the floor had already handed it back")
		statesNoDeletedWords(t, screen)
	default:
		t.Skipf("the work landed %q rather than as anybody's call, so there was nothing to hand over:\n%s", found, screen)
	}
	r.quit()
}

// testStatesAutoFloorAcrossARestart is the same law measured where it is a fact
// rather than a race: A TASK NEVER STAYS UNOWNED, and a process that died while
// aforge was holding one is the hardest case, because the turn it was going to be
// decided in died with it.
//
// THE FIXTURE IS THE RECORD A KILLED PROCESS LEAVES. The checkpoint carries who
// was holding each landing (session's taskRecord.Decider), so this seeds one that
// says `model` and opens the conversation on it. What must be on the screen is
// the chips — the floor hands the question back on the way in, before anything is
// drawn — and what must NOT be on it is `aforge is deciding`, which would be a
// card naming a decider that no longer exists and offering no way to act.
//
// IT PAYS FOR NO MODEL. Every fact this reads is one the record carries and the
// engine acts on, which is the same trade [testStatesRailAndRoster] makes.
func testStatesAutoFloorAcrossARestart(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "auto"})
	ws := newWorkspace(t, "autoloadws", false)
	seedModelHeld(t, home, ws)
	r := start(t, "afe2e_states_autoload", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	screen := r.waitFor(45*time.Second, say(t, "taskLookWord"), say(t, "settleAnswersRow"))
	t.Logf("a landing the record said aforge was deciding, after the restart:\n%s", screen)
	if head := statesHeadLine(screen, say(t, "unverifiedGlyph"), say(t, "taskLookWord")); head != "" {
		t.Logf("HEAD · %s", head)
	}
	if strings.Contains(screen, say(t, "taskAutoDecidingWord")) {
		t.Errorf("the card still says %q about a turn that died with the last process, so the question "+
			"is held by nobody and the person is offered no way to act on it:\n%s",
			say(t, "taskAutoDecidingWord"), screen)
	}
	statesNoDeletedWords(t, screen)
	r.quit()
}

// seedModelHeld writes one conversation holding a landing NOBODY COULD CHECK
// that the record says AFORGE WAS DECIDING — the checkpoint a process killed
// under `task.settle = auto` leaves behind.
//
// IT IS [seedUnchecked]'S FIXTURE PLUS ONE FIELD, and the field is the whole
// subject: `decider` is what the auto-settle floor fires on when the graph comes
// off the disk, and until it was on the record this shape could not be staged at
// all.
func seedModelHeld(t *testing.T, home, ws string) string {
	t.Helper()
	if canonical, err := filepath.EvalSymlinks(ws); err == nil {
		ws = canonical
	}
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", 0x3000000000000006)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed the model's landing: %v", err)
	}
	statesSeedTranscript(t, dir, sid, ws, "port the parser for me")
	at := time.Now().Add(-3 * time.Minute)
	writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{
		"id": sid, "title": "The landing aforge was deciding", "workspace": ws,
		"created": at.Format(time.RFC3339Nano), "lastUserAt": at.Format(time.RFC3339Nano),
	})
	writeJSON(t, filepath.Join(dir, "tasks.json"), map[string]any{
		"type": "tasks", "version": 1, "seq": 1,
		"nodes": []map[string]any{{
			"id": 1, "title": "Port the parser", "brief": "port it", "acceptance": "it parses",
			"state": "unverified", "merge": "inplace", "decider": "model",
			"report":  "the parser is ported and its tests run",
			"changed": []string{"parser.go"},
			"ground":  ws, "groundMode": "folder", "elapsed_ms": 42000,
		}},
	})
	return dir
}

// ── 6 · the rail and the roster ─────────────────────────────────────────────

// testStatesRailAndRoster is the other half of "one word per state across every
// surface": the same landing, read on the column beside the conversation and on
// the page `/history` opens, must say the same word the card says.
//
// A DISAGREEMENT HERE IS THE DEFECT THIS WAVE CLOSED. One landing was called
// `needs your look` on the card, `awaiting review` on the roster and
// `unverified` on the rail, a keypress apart, because three surfaces each kept a
// table of their own.
func testStatesRailAndRoster(t *testing.T) {
	home := newHome(t, map[string]any{"task.settle": "ask"})
	ws := newWorkspace(t, "railws", false)
	seedUnchecked(t, home, ws, 0x3000000000000005)
	r := start(t, "afe2e_states_rail", home, ws, tuiWide, 40)
	statesPastTheDoor(t, r)

	screen := r.waitFor(30*time.Second, say(t, "taskLookWord"), say(t, "settleAskWord"))
	t.Logf("the landing, before the column is asked for:\n%s", screen)

	// THE COLUMN, whichever side of the toggle this window opened on. `ctrl+g`
	// closes the roster's column or brings it back and the answer is remembered,
	// so the column is looked for FIRST and the key is spent only when there is no
	// column at all — a press on a window that already has one takes it away, and
	// the first measured run of this subtest did exactly that and then reported
	// the empty frame it had just made.
	rail := statesRail(t, r, say(t, "unverifiedGlyph"))
	if rail == "" {
		t.Fatalf("no row of the column carries %q at all:\n%s", say(t, "unverifiedGlyph"), r.capture())
	}
	t.Logf("RAIL · %s", rail)

	// AND THE ROW SAYS THE WORD. `<tier glyph> <title> · <word or reason>`, cut
	// from the right, and THE VERB IS NEVER WHAT GOES: the title gives ground down
	// to about one word and the reason's file list goes before either, because the
	// row is read to find out whether it needs anything and the word is the half
	// that answers that (docs/design/task-states/DESIGN.md).
	//
	// THE ROW IS READ WITH ITS BLOCK ([statesRailRow] says why). Thirty cells will
	// not hold a name and a reason side by side, so the reading is laid over the
	// row and the lines under it — and a reader that took the first line alone was
	// what filed issue #707 against a column that was saying the word all along.
	if !strings.Contains(rail, say(t, "taskLookWord")) {
		t.Errorf("the column's row says nothing about %q — it names the work and stops, while "+
			"the card in the same frame says the word, which leaves the one question every row "+
			"is read to answer unanswered on the surface that exists to answer it at a glance:"+
			"\n\t%s\n%s", say(t, "taskLookWord"), rail, r.capture())
	}
	if !strings.Contains(rail, say(t, "settleAskWord")) {
		t.Errorf("the column's row says %q and never what for; the reason is the half a person "+
			"can act on:\n\t%s", say(t, "taskLookWord"), rail)
	}

	// AND THE ROSTER SAYS THE SAME WORD. `/history` is the door; the chord the
	// page names is not one a suite may press (tui_e2e_test.go says why).
	r.lit("/history")
	time.Sleep(700 * time.Millisecond)
	r.keys("Enter")
	roster := r.waitFor(60*time.Second, say(t, "taskLookWord"))
	t.Logf("the roster, saying the same word:\n%s", roster)
	statesNoDeletedWords(t, roster)
	r.quit()
}

// ── the doors this file shares ──────────────────────────────────────────────

// statesPastTheDoor is the launch every subtest here begins with: whichever
// screen this state root opens on, waited for so that the keystrokes after it
// land on a window that has finished drawing.
//
// THE SETUP IS ALREADY BEHIND IT. [start] presses past the first-run flow for
// every rig in this package ([rig.skipSetup] says why), so what is waited for
// here is the screen under it — home on a machine with other projects, the
// greeting on a fresh conversation, or the landing itself where a graph has
// already been replayed into one. All three are the product behaving.
func statesPastTheDoor(t *testing.T, r *rig) {
	t.Helper()
	r.skipSetup(t)
	r.waitForAny(25*time.Second,
		say(t, "homeFootWord"), say(t, "starterTaskWord"), say(t, "setupTitleWord"),
		say(t, "setupSkipWord"), say(t, "landingKeysWord"), say(t, "taskLookWord"),
		say(t, "welcomeStarterKeysWord"))
}

// statesAnswerKey presses one of the card's letters and waits for what it
// leaves, THROUGH THE GREETING, and retrying the way a person does.
//
// THE LETTERS HAVE THREE GUARDS AND ALL THREE ARE ABOUT NOT ANSWERING BY
// ACCIDENT (tasksettle.go's [app.settleCardKey] keeps the same guards `x` has):
// the card must be the SELECTED one, the message box must be empty, and no
// overlay may be up.
//
// AND A CONVERSATION NOBODY HAS TYPED IN YET IS STANDING ON ITS GREETING, whose
// starting-point list holds ↑ and ↓ over an empty box (welcome.go's
// [app.welcomeStarterKey] — a person cannot pick a row with an arrow if the arrow
// closes the list). So a blind `↑` there walks the STARTING POINTS and never
// reaches the card, and the letter after it is read as somebody beginning to
// type. That is the greeting behaving exactly as it is written to; it is also
// why this door exists rather than two bare keystrokes.
//
// The way through is the greeting's own contract: EVERY OTHER KEY DISMISSES IT.
// So one harmless character puts the list away, ctrl+u gives the empty box the
// letters need back, and only then is ↑ the walk through the conversation. The
// character is a full stop on purpose — `x` is the stop key and a,n,s,d,t are the
// answers, and a fixture that reached for one of those would be answering the
// question it came to read.
func statesAnswerKey(t *testing.T, r *rig, key, want string) bool {
	t.Helper()
	for attempt := 1; attempt <= 2; attempt++ {
		r.lit(".")
		r.keys("C-u")
		time.Sleep(400 * time.Millisecond)
		// AND ↑ IS WALKED RATHER THAN PRESSED ONCE. The letters answer the SELECTED
		// card, the selection starts at the foot of the conversation, and what sits
		// at the foot is whatever the window wrote last — the note the greeting
		// leaves on its way out, a line the model added, the card itself. A single
		// ↑ therefore lands on the card only when the card happens to be last, and
		// a key refused on the wrong row falls through and types itself, which is
		// exactly what a person watching the box would see and correct by pressing
		// ↑ again.
		for up := 1; up <= statesWalkUp; up++ {
			r.keys("Up")
			r.lit(key)
			if _, ok := r.glimpse(6*time.Second, want); ok {
				t.Logf("the %q was spent on attempt %d, %d rows up", key, attempt, up)
				return true
			}
			// Whatever the letter typed instead, so the next ↑ is read as a walk
			// and not as a list being filtered.
			r.keys("C-u")
		}
		t.Logf("attempt %d: %q left no %q anywhere in %d rows of the conversation:\n%s",
			attempt, key, want, statesWalkUp, r.capture())
	}
	return false
}

// statesWalkUp is how far up the conversation one answer is looked for. Six rows
// is every entry these fixtures write and then some; a card further up than that
// is a conversation this suite did not build.
const statesWalkUp = 6

// statesAwait is [rig.waitForAny] WITHOUT THE FAILURE: it answers which of
// several strings arrived and "" when none did.
//
// IT EXISTS BECAUSE TWO SHAPES HERE HAVE AN HONEST NOTHING. A merge round the
// engine won and a landing the model settled inside its own turn are both the
// product behaving, and so is a run that simply did not produce the shape a
// subtest was hoping to provoke — which is a SKIP with its reason written down,
// not a red test. waitForAny records an error on the way to answering, so a
// subtest that used it and then skipped would be marked failed before it ever
// reached the skip.
func statesAwait(r *rig, within time.Duration, want ...string) (string, string) {
	deadline := time.Now().Add(within)
	screen := ""
	for {
		screen = r.capture()
		for _, sub := range want {
			if strings.Contains(screen, sub) {
				return sub, screen
			}
		}
		if time.Now().After(deadline) {
			return "", screen
		}
		time.Sleep(pollEvery)
	}
}

// statesSpan is the shape taskSpanWord writes a duration in — `42s`, `6m40s`,
// `1h02m` — which is the only way a test can assert the head carries one without
// spelling how long a real model really took.
var statesSpan = regexp.MustCompile(`\b(\d+h\d+m|\d+m\d+s|\d+m|\d+s)\b`)

// statesHeadLine is the one line on the screen carrying both a tier glyph and a
// tier word IN THE CONVERSATION, which is what a landing card's head is.
//
// IT IS SOUGHT AS A LINE AND NOT AS TWO SUBSTRINGS because `done` is an ordinary
// English word: a model that writes "the task is done" three rows above the card
// would satisfy a pair of screen-wide searches perfectly while the head said
// something else entirely.
//
// AND THE GLYPH MUST STAND NEAR THE LEFT MARGIN, which is the other half of the
// same care. The rail is a column on the RIGHT of these very lines and it draws
// the same glyph and the same word about the same node — so a search that took
// any matching line would keep reading the rail's row, find no span and no file
// count on it, and report a card that is perfectly correct as broken.
func statesHeadLine(screen, glyph, word string) string {
	for _, line := range strings.Split(screen, "\n") {
		at := strings.Index(line, glyph)
		if at < 0 || !strings.Contains(line, word) {
			continue
		}
		if len([]rune(line[:at])) > statesHeadMargin {
			continue
		}
		return strings.TrimSpace(line)
	}
	return ""
}

// statesHeadMargin is how far from the left a card's own head may begin: the
// transcript's gutter and the card's two marks, with room to spare. Anything
// further right on one of these lines belongs to the rail.
const statesHeadMargin = 40

// statesReasonLine is the row a reason sentence stands on, whole, for the log.
func statesReasonLine(screen, needle string) string {
	for _, line := range strings.Split(screen, "\n") {
		if strings.Contains(line, needle) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

// statesDeleted is every word the ruling DELETES as person-facing text. They are
// asserted absent rather than merely un-waited-for, because the gates that pin
// this vocabulary check that a name is MENTIONED and never that the claim around
// it is true: a surface that kept its old word beside the new one would pass
// every one of them.
var statesDeleted = []string{
	"awaiting review", "unverified", "needs your look",
	"delivery needs attention", "stopped — branch kept",
	"what it produced was not taken as done",
}

// statesNoDeletedWords fails the subtest on any of them.
//
// `failed` IS NOT ON THE LIST and that is deliberate: it is still the record
// page's word for a fault, and it is a word a model may perfectly well write in
// a sentence of its own about work that did not go well. What the ruling deletes
// is the LANDING wearing it, which [statesHeadLine] is the assertion for.
func statesNoDeletedWords(t *testing.T, screen string) {
	t.Helper()
	for _, gone := range statesDeleted {
		if strings.Contains(screen, gone) {
			t.Errorf("the screen still says %q, which docs/design/task-states/DESIGN.md deletes:\n%s", gone, screen)
		}
	}
}

// statesRail is the roster COLUMN'S row for one node — not the landing card's
// head — and the difference is the whole point of the subtest: the card in the
// conversation says `your call` too, and a search across the frame would find it
// there and never look at the column at all.
//
// THE TWO ARE TOLD APART BY THE KIND MARK. A card's head carries the node's own
// ident glyph beside the tier cell (taskident.go's `◆`); a rail row is the tier
// cell, the title and the word and nothing else (tasktier.go's tierRow). That is
// a fact about what each row IS rather than about where it happens to sit, so it
// holds whichever side of the frame the column is on and at every width.
//
// IT IS FOUND BY THE TIER CELL ALONE, and that is deliberate. Looking for the
// cell AND the word would make a column that has stopped saying the word
// indistinguishable from a column that is not there — and those are two different
// findings, one of which is answered by pressing ctrl+g and the other of which is
// the defect this subtest exists to catch.
func statesRail(t *testing.T, r *rig, glyph string) string {
	t.Helper()
	if row := statesRailRow(t, r.capture(), glyph); row != "" {
		return row
	}
	// No column at all, so ask for it back. The answer is remembered per machine
	// — a state root copied from a machine whose column is closed opens closed,
	// which is how this ran green on one box and failed on another — so the key
	// is spent only after looking, and looking is the whole of the guard: a press
	// on a window that already has a column takes it away.
	r.keys("C-g")
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if row := statesRailRow(t, r.capture(), glyph); row != "" {
			return row
		}
		time.Sleep(pollEvery)
	}
	return ""
}

// statesRailRow is ONE ROW OF THE COLUMN, WITH THE BLOCK UNDER IT, as one
// string: the tier cell, the name, and whatever the rows beneath it say about
// the same node.
//
// IT READS THE COLUMN AND NOT WHATEVER LINE HAPPENS TO START WITH THE GLYPH,
// and the first measured run of this file is why. The column stands to the RIGHT
// of the conversation, so no line of a capture that has one ever begins with a
// tier cell — every one of them begins with the transcript. A reader that
// matched on the whole line therefore found nothing, [statesRail] then pressed
// ctrl+g "because there was no column", and what it measured was the tab strip
// that appears in the column's place: one chip, one glyph, one name. The column
// itself was saying `your call · nobody could check it` the whole time, one row
// below the name, and that misreading is the whole of what issue #707 recorded.
//
// THE BLOCK IS PART OF THE ROW because the column is thirty cells wide and the
// reading does not fit beside the name at that width. `<tier glyph> <title> ·
// <word or reason>`, cut from the right, is laid over the row and the two lines
// under it (internal/tui3's [app.railUnder] wraps them), so the row and its
// block are read together or the word is not read at all.
// IT IS THE COLUMN OR IT IS NOTHING. An empty answer means this frame has no
// column on it, and the caller's job is then to ask for one — never to settle
// for the strip that stands in its place, which is a tab bar with one glyph and
// one name on it and is not the surface this is about. Reading the strip and
// calling it the column is precisely what filed #707.
func statesRailRow(t *testing.T, screen, glyph string) string {
	t.Helper()
	return statesRailBlock(statesRailColumn(screen), glyph)
}

// statesRailColumn is the column's own cells, cut off the right of the seam it
// is drawn behind. An empty answer means this frame has no column on it.
func statesRailColumn(screen string) []string {
	var out []string
	for _, line := range strings.Split(screen, "\n") {
		at := strings.Index(line, statesRailSeam)
		if at < 0 {
			continue
		}
		out = append(out, strings.TrimRight(line[at+len(statesRailSeam):], " "))
	}
	return out
}

// statesRailSeam is the rule the column is drawn behind (internal/tui3's
// [app.railJoin]).
const statesRailSeam = "│"

// statesRailBlock is one node's row and the rows indented under it, joined.
// The row itself sits one cell in from the seam and its block sits further in,
// which is what ends the block: the next row of work starts where this one did.
func statesRailBlock(column []string, glyph string) string {
	for i, line := range column {
		text := strings.TrimSpace(line)
		if text == "" || !strings.HasPrefix(text, glyph) {
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " "))
		for _, under := range column[i+1:] {
			said := strings.TrimSpace(under)
			if said == "" || len(under)-len(strings.TrimLeft(under, " ")) <= lead {
				break
			}
			text += " " + said
		}
		return text
	}
	return ""
}

// seedUnchecked writes one conversation holding a top-level landing NOBODY COULD
// CHECK: the record a finished process leaves behind, in the state a person's
// answer moves and nothing else does.
//
// IT KEEPS ITS OWN FIXTURE RATHER THAN BORROWING [seedUndecidedRoot], and the
// report is why. That one's report is the surface's OLD sentence — "finished, but
// needs your look" — which is one of the phrases this ruling deletes, and a
// subtest asserting the deleted words are off the screen would be failing on its
// own fixture rather than on the product.
func seedUnchecked(t *testing.T, home, ws string, id uint64) string {
	t.Helper()
	if canonical, err := filepath.EvalSymlinks(ws); err == nil {
		ws = canonical
	}
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", id)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed unchecked: %v", err)
	}
	statesSeedTranscript(t, dir, sid, ws, "port the parser for me")
	at := time.Now().Add(-3 * time.Minute)
	writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{
		"id": sid, "title": "The landing that is your call", "workspace": ws,
		"created": at.Format(time.RFC3339Nano), "lastUserAt": at.Format(time.RFC3339Nano),
	})
	writeJSON(t, filepath.Join(dir, "tasks.json"), map[string]any{
		"type": "tasks", "version": 1, "seq": 1,
		"nodes": []map[string]any{{
			"id": 1, "title": "Port the parser", "brief": "port it", "acceptance": "it parses",
			"state": "unverified", "merge": "inplace",
			"report":  "the parser is ported and its tests run",
			"changed": []string{"parser.go"},
			"ground":  ws, "groundMode": "folder", "elapsed_ms": 42000,
		}},
	})
	return dir
}

// statesSeedTranscript writes the journal of a conversation SOMEBODY HAS
// ACTUALLY SPOKEN IN: the header line, the person's sentence, and the answer.
//
// AN EMPTY JOURNAL IS NOT A CHEAPER FIXTURE, IT IS A DIFFERENT SCREEN, and the
// first measured run of this file is what found it. A conversation with nothing
// in it opens on the greeting, and on the machine's FIRST conversation the
// greeting deliberately stands through typing (welcome.go's
// [app.welcomeStandsThroughTyping]) and stands the chord keys down while it is up
// (stop.go) — so the card's own answer letters are refused, which is the greeting
// behaving correctly about a conversation that does not exist.
//
// It is also nothing like the shape these subtests are about. A landing that is
// somebody's call arrives in a conversation they started the work from, which is
// a conversation with their words in it. So the fixture has their words in it.
func statesSeedTranscript(t *testing.T, dir, id, ws, said string) {
	t.Helper()
	at := time.Now().Add(-5 * time.Minute)
	lines := []map[string]any{
		{"type": "session", "version": 1, "id": id, "cwd": ws,
			"model": "deepseek/deepseek-v4-flash", "timestamp": at.Format(time.RFC3339Nano)},
		{"type": "message", "role": "user", "content": said,
			"timestamp": at.Format(time.RFC3339Nano)},
		{"type": "message", "role": "assistant", "content": "I have put that out as a task.",
			"timestamp": at.Add(time.Second).Format(time.RFC3339Nano)},
	}
	var b strings.Builder
	for _, line := range lines {
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatalf("seed transcript: %v", err)
		}
		b.Write(raw)
		b.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatalf("seed transcript: %v", err)
	}
}

// statesWaitForTaskBranch waits until a task's own branch stands in the person's
// repository, and answers whether one ever did.
//
// IT IS THE SIGNAL A CONFLICT IS TIMED AGAINST, and it is a fact about the work
// rather than a sentence a model happened to write: a worktree is cut from THIS
// repository, so the ref lands in its refs/heads the moment the work is really
// out, which is the only moment at which committing over the same file can still
// clash with anything.
func statesWaitForTaskBranch(t *testing.T, ws string, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		command := exec.Command("git", "for-each-ref", "--format=%(refname:short)", "refs/heads/task")
		command.Dir = ws
		if out, err := command.Output(); err == nil && strings.TrimSpace(string(out)) != "" {
			t.Logf("the task branches are %s", strings.Join(strings.Fields(string(out)), " "))
			return true
		}
		time.Sleep(time.Second)
	}
	return false
}

// statesEdit writes one file in the person's own checkout and LEAVES IT
// UNCOMMITTED — a person carrying on working while a task is out, which is the
// shape a real clash is made in ([testStatesConflict] says why a commit is not).
func statesEdit(t *testing.T, ws, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// statesWorkBranch moves the person's checkout onto a branch tasks are allowed
// to merge into. Every name on internal/session's protected list is one a
// landing keeps its branch off rather than writing, and `main` — which
// [newWorkspace] builds on — is the first entry.
func statesWorkBranch(t *testing.T, ws string) {
	t.Helper()
	command := exec.Command("git", "checkout", "-q", "-b", "work")
	command.Dir = ws
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b work: %v\n%s", err, out)
	}
}

// statesCommit writes one file in the person's own checkout and commits it — the
// person carrying on working while a task is out, which is the only way a real
// conflict is made.
func statesCommit(t *testing.T, ws, name, body, message string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	for _, args := range [][]string{
		{"add", name},
		{"-c", "user.email=e2e@example.com", "-c", "user.name=e2e", "commit", "-qm", message},
	} {
		command := exec.Command("git", args...)
		command.Dir = ws
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
}

// seedOutOfSteps writes one conversation holding a landing that spent its step
// threshold: the record a finished process leaves behind, with the ending that
// makes the reason sentence knowable ([session.TaskReasonOf]).
//
// IT IS THE ENDING AND NOT THE REPORT that the row is read out of, which is the
// point of seeding it: a surface that spelled its own sentence here would pass
// with any prose at all in the report field.
func seedOutOfSteps(t *testing.T, home, ws string) string {
	t.Helper()
	if canonical, err := filepath.EvalSymlinks(ws); err == nil {
		ws = canonical
	}
	bucket := strings.ReplaceAll(filepath.Clean(ws), string(filepath.Separator), "-")
	sid := fmt.Sprintf("%016x", 0x3000000000000003)
	dir := filepath.Join(home, "v3", "projects", bucket, sid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("seed out of steps: %v", err)
	}
	statesSeedTranscript(t, dir, sid, ws, "port the parser for me")
	at := time.Now().Add(-4 * time.Minute)
	writeJSON(t, filepath.Join(dir, "meta.json"), map[string]any{
		"id": sid, "title": "The run that ran out", "workspace": ws,
		"created": at.Format(time.RFC3339Nano), "lastUserAt": at.Format(time.RFC3339Nano),
	})
	writeJSON(t, filepath.Join(dir, "tasks.json"), map[string]any{
		"type": "tasks", "version": 1, "seq": 1,
		"nodes": []map[string]any{{
			"id": 1, "title": "Port the parser", "brief": "port it", "acceptance": "it parses",
			"state": "failed", "ending": "steps", "merge": "kept", "branch": "task/parser",
			"report":  "it stopped at its step threshold with the port half done",
			"changed": []string{"parser.go"},
			"ground":  ws, "groundMode": "folder", "elapsed_ms": 242000,
		}},
	})
	return dir
}
