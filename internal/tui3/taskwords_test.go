package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE WORDS A PERSON READS ABOUT A TASK, AND THE ONES THEY NEVER DO.
//
// A task, on this surface, is one of four things: it is running (which includes
// every private round the engine runs on it to close a gap), it is done, it is
// incomplete with the gaps named, or — the rare case — it finished and needs a
// person's look because nothing came back that could call it either way. That
// list has no fifth member and it has no machinery in it.
//
// The two halves of this file are the two halves of that law. The first is what
// a node closing a gap SAYS while it does it: one plain line, in the one place a
// person is watching, and gone the moment the gap is closed. The second is the
// sweep — every surface a landed node reaches, every terminal state, and not one
// word of the apparatus that decided anything.

// ── the finishing line ──────────────────────────────────────────────────────

// mendingNotice is the update the engine sends while a node closes a named gap:
// still RUNNING, because it is running, with the one line that says what is left
// (session's TaskNotice.Mending).
func mendingNotice(mending string) session.TaskNotice {
	return session.TaskNotice{Mending: mending, Model: "openai/gpt-5", CostUSD: 0.31}
}

// THE FINISHING LINE TAKES THE ROW ABOVE THE TELEMETRY, AT EVERY WIDTH. It is
// the most specific thing this column will ever know about a node — the end is
// in sight and this is what the end is missing — so it leads the under-block and
// the standing figures keep the row beneath it, shortened by their own rule.
func TestAFinishingNodeSaysWhatItIsClosingAtEveryWidth(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning,
		mendingNotice("adding amp-labs to the report"))})
	node := a.tasks[7]
	node.tokens = 9_900
	advance(42 * time.Second)

	// ONE RULE AT BOTH COLUMNS: the line is cut from the right and the word that
	// names what is happening is the half that always survives, because a person
	// who can read only the first two cells of this row has still been told the
	// node is nearly home.
	for _, tc := range []struct {
		width int
		want  []string
	}{
		{underWidth(railCols), []string{"finishing · adding amp-la…", "42s · 9.9k · $0.31 · gpt-5"}},
		{underWidth(railSlimCols), []string{"finishing · adding …", "42s · 9.9k · $0.31"}},
	} {
		rows := a.railUnder(node, tc.width)
		if len(rows) != len(tc.want) {
			t.Fatalf("at %d cells the under-block is %d rows, want %d:\n%q", tc.width, len(rows), len(tc.want), rows)
		}
		for i, want := range tc.want {
			if got := plain(rows[i]); got != want {
				t.Fatalf("at %d cells row %d is %q, want %q", tc.width, i, got, want)
			}
			if w := ansi.StringWidth(plain(rows[i])); w > tc.width {
				t.Fatalf("at %d cells row %d is %d cells wide", tc.width, i, w)
			}
		}
	}

	// A LIVE CALL DOES NOT GET A ROW WHILE THIS ONE IS SET. The block is capped
	// at two ([railUnderRows]) and the call the node happens to be inside of says
	// nothing the finishing line and the telemetry do not already say better.
	node.tool, node.toolBegan = "bash go test ./...", a.now().Add(-24*time.Second)
	rows := a.railUnder(node, underWidth(railCols))
	if len(rows) != railUnderRows || plain(rows[0]) != "finishing · adding amp-la…" {
		t.Fatalf("a live call displaced the finishing line:\n%q", rows)
	}
	node.tool, node.toolBegan = "", time.Time{}

	// AND THE COLUMN ITSELF DRAWS IT, at both widths the roster has. The rows
	// above are the block; this is the block on screen, under the node's own name
	// and in the running group.
	for _, width := range []int{200, 110} {
		a.width = width
		roster := rosterText(a, 12)
		if !strings.Contains(roster, "finishing · adding") {
			t.Fatalf("the roster at %d columns does not say what the node is finishing:\n%s", width, roster)
		}
	}
	a.width = 200
}

// AND IT GOES THE MOMENT THE GAP IS CLOSED. The line is a report of what is
// happening right now, not a fact about the work, so the update that stops
// carrying it takes it off the column — and the row it displaced comes back.
func TestTheFinishingLineDropsWhenTheGapIsClosed(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning,
		mendingNotice("adding amp-labs to the report"))})
	node := a.tasks[7]
	advance(42 * time.Second)
	node.tool, node.toolBegan = "bash go test ./...", a.now().Add(-24*time.Second)
	if got := plain(a.railUnder(node, underWidth(railCols))[0]); !strings.HasPrefix(got, taskFinishingWord) {
		t.Fatalf("the finishing line is not on the node at all: %q", got)
	}

	// THE SECOND UPDATE IS THE SAME STATE, and it must not be swallowed as a
	// duplicate: a node that stopped mending is news exactly the way a node that
	// started mending is (task.go's [app.taskUpdate] de-dup).
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning,
		session.TaskNotice{Model: "openai/gpt-5", CostUSD: 0.31})})
	if node.mending != "" {
		t.Fatalf("the node still carries %q", node.mending)
	}
	rows := a.railUnder(node, underWidth(railCols))
	if len(rows) != railUnderRows {
		t.Fatalf("the under-block is %d rows after the gap closed:\n%q", len(rows), rows)
	}
	if got := plain(rows[0]); got != "bash go test ./... · 24s" {
		t.Fatalf("the call row did not come back: %q", got)
	}
	if strings.Contains(rosterText(a, 12), taskFinishingWord) {
		t.Fatalf("the roster is still finishing:\n%s", rosterText(a, 12))
	}
}

// THE ROOM SAYS THE SAME WORD IN ITS OWN LINE. A person standing inside a node's
// page is owed the difference between "this is under way" and "this is being
// tied off", and the header has one line to say it in.
func TestTheRoomHeaderSaysFinishingWhileAGapIsBeingClosed(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning,
		mendingNotice("adding amp-labs to the report"))})
	node := a.tasks[7]

	if got := a.roomStateWord(node); got != taskFinishingWord {
		t.Fatalf("the header calls a finishing node %q, want %q", got, taskFinishingWord)
	}
	// The header is asserted through the rows a person actually reads, not only
	// through the word: the trail is one row and the state, the clock and the
	// spend are the row under it (room.go).
	a.room = a.newRoom(7, "Write the report")
	head := plain(roomHeadAll(a, 120))
	if !strings.Contains(head, roomCrumbRoot+roomCrumbSep+"Write the report") ||
		!strings.Contains(head, taskFinishingWord) {
		t.Fatalf("the room header is %q", head)
	}
	if strings.Contains(head, stateWorking.String()) {
		t.Fatalf("the room header calls a finishing node working: %q", head)
	}

	// AND IT GOES BACK TO WORKING when the gap is closed, because nothing about
	// the node's state ever moved.
	node.mending = ""
	if got := a.roomStateWord(node); got != stateWorking.String() {
		t.Fatalf("a node with no gap left says %q, want %q", got, stateWorking.String())
	}
}

// ── the waiting line ────────────────────────────────────────────────────────

// heldNotice is the update the engine sends about a node that is not spending
// its time on the work: the state it was already in, and the one word that says
// what is holding it (session's TaskNotice.Waiting).
func heldNotice(why string) session.TaskNotice {
	return session.TaskNotice{Waiting: why, Model: "openai/gpt-5", CostUSD: 0.31}
}

// A QUEUED NODE SAYS WHAT IS HOLDING IT, AT EVERY WIDTH. A row that has sat
// still for four minutes is either stuck or waiting its turn, and those are
// opposite news wearing the same row — so the column says which, in the engine's
// own reason, cut from the right so that the word that names the moment always
// survives.
func TestAHeldNodeSaysWhatIsHoldingItAtEveryWidth(t *testing.T) {
	for _, tc := range []struct{ why, full, slim string }{
		{waitWordMachine, "waiting · machine busy", "waiting · machine b…"},
		{waitWordSlot, "waiting · slot", "waiting · slot"},
	} {
		t.Run(tc.why, func(t *testing.T) {
			a, _, _ := taskApp(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskQueued,
				heldNotice(tc.why))})
			node := a.tasks[7]
			if node.waiting != tc.why {
				t.Fatalf("the node carries %q, want %q", node.waiting, tc.why)
			}
			for _, w := range []struct {
				width int
				want  string
			}{{underWidth(railCols), tc.full}, {underWidth(railSlimCols), tc.slim}} {
				rows := a.railUnder(node, w.width)
				if len(rows) != 1 || plain(rows[0]) != w.want {
					t.Fatalf("at %d cells the under-block is %q, want one row %q", w.width, rows, w.want)
				}
				if got := ansi.StringWidth(plain(rows[0])); got > w.width {
					t.Fatalf("at %d cells the row is %d cells wide", w.width, got)
				}
			}
			// AND THE COLUMN ITSELF DRAWS IT, at both widths the roster has.
			for _, width := range []int{200, 110} {
				a.width = width
				if roster := rosterText(a, 12); !strings.Contains(roster, taskHeldWord+" · ") {
					t.Fatalf("the roster at %d columns does not say the node is held:\n%s", width, roster)
				}
			}
			a.width = 200

			// AND IT GOES THE MOMENT THE HOLD DOES. The word is a report of right
			// now, and the update that stops carrying it is the same state as the one
			// before it — so it must not be swallowed as a duplicate (task.go's
			// [app.taskUpdate] de-dup).
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskQueued,
				session.TaskNotice{Model: "openai/gpt-5", CostUSD: 0.31})})
			if node.waiting != "" {
				t.Fatalf("the node still carries %q", node.waiting)
			}
			if rows := a.railUnder(node, underWidth(railCols)); len(rows) != 0 {
				t.Fatalf("the under-block outlived the hold: %q", rows)
			}
			if strings.Contains(rosterText(a, 12), taskHeldWord) {
				t.Fatalf("the roster is still waiting:\n%s", rosterText(a, 12))
			}
		})
	}
}

// A PACED NODE NEVER CLAIMS A LIVE CALL. While the provider is holding this
// node's calls back the tool line names a call that has already finished, so the
// hold takes that row instead and the telemetry keeps the one beneath it: the
// clock and the bill go on being the clock and the bill.
func TestAPacedNodeWaitsWhereItsToolLineWouldBe(t *testing.T) {
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning,
		heldNotice(waitWordRate))})
	node := a.tasks[7]
	node.tokens = 9_900
	advance(42 * time.Second)
	node.tool, node.toolBegan = "bash go test ./...", a.now().Add(-24*time.Second)

	for _, tc := range []struct {
		width int
		want  []string
	}{
		{underWidth(railCols), []string{"waiting · rate limited", "42s · 9.9k · $0.31 · gpt-5"}},
		{underWidth(railSlimCols), []string{"waiting · rate limi…", "42s · 9.9k · $0.31"}},
	} {
		rows := a.railUnder(node, tc.width)
		if len(rows) != len(tc.want) {
			t.Fatalf("at %d cells the under-block is %d rows, want %d:\n%q", tc.width, len(rows), len(tc.want), rows)
		}
		for i, want := range tc.want {
			if got := plain(rows[i]); got != want {
				t.Fatalf("at %d cells row %d is %q, want %q", tc.width, i, got, want)
			}
			if w := ansi.StringWidth(plain(rows[i])); w > tc.width {
				t.Fatalf("at %d cells row %d is %d cells wide", tc.width, i, w)
			}
		}
	}
	if roster := rosterText(a, 12); strings.Contains(roster, "go test") {
		t.Fatalf("a paced node claimed a call that is not happening:\n%s", roster)
	}

	// AND THE CALL COMES BACK when the wire does, on an update that moves nothing
	// but the hold.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning,
		session.TaskNotice{Model: "openai/gpt-5", CostUSD: 0.31})})
	rows := a.railUnder(node, underWidth(railCols))
	if len(rows) != railUnderRows || plain(rows[0]) != "bash go test ./... · 24s" {
		t.Fatalf("the call row did not come back:\n%q", rows)
	}
}

// THE DEPENDENCY OUTRANKS THE HOLD, on the one row a queued node gets. Both are
// true of a node behind other work AND behind a full cap, and only one of them
// names something a person can act on.
func TestAWaitingDependencyOutranksTheHoldWord(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a,
		streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskRunning, session.TaskNotice{})},
		streamEventMsg{gen: a.gen, ev: update(2, "Mix audio", session.TaskQueued, session.TaskNotice{
			DependsOn: []uint64{1}, Waiting: waitWordSlot,
		})},
	)
	node := a.tasks[2]
	got := plain(strings.Join(a.railUnder(node, underWidth(railCols)), "\n"))
	if !strings.Contains(got, "waits: Collect sources") {
		t.Fatalf("the blocked node says %q, want the prerequisite it waits on", got)
	}
	if strings.Contains(got, taskHeldWord) {
		t.Fatalf("the hold word took the row the dependency owns: %q", got)
	}
	if word := a.roomStateWord(node); word != "waits: Collect sources" {
		t.Fatalf("the room header says %q, want the prerequisite", word)
	}

	// AND THE HOLD IS WHAT IS LEFT once the prerequisite lands: the node is
	// unblocked, it is still not running, and the row says why.
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(1, "Collect sources", session.TaskDone,
		session.TaskNotice{Merge: mergeWordMerged})})
	if got := plain(strings.Join(a.railUnder(node, underWidth(railCols)), "\n")); got != "waiting · slot" {
		t.Fatalf("the unblocked node says %q, want the hold", got)
	}
}

// THE ROOM SAYS THE SAME WORD IN ITS OWN LINE. A person standing inside a node's
// page and watching nothing move is owed the difference between "this is under
// way" and "this is waiting its turn", and the header has one line to say it in.
func TestTheRoomHeaderSaysWaitingWhileANodeIsHeld(t *testing.T) {
	for _, tc := range []struct {
		what  string
		state session.TaskState
		why   string
		back  string
	}{
		{"a queued node the machine has no room for", session.TaskQueued, waitWordMachine, roomQueuedWord},
		{"a running node whose calls are paced", session.TaskRunning, waitWordRate, stateWorking.String()},
	} {
		t.Run(tc.what, func(t *testing.T) {
			a, _, _ := taskApp(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", tc.state, heldNotice(tc.why))})
			node := a.tasks[7]
			if got := a.roomStateWord(node); got != taskHeldWord {
				t.Fatalf("the header calls a held node %q, want %q", got, taskHeldWord)
			}
			// The header is asserted through the rows a person actually reads: the
			// trail is one row and the state, the clock and the spend are the row
			// under it (room.go).
			a.room = a.newRoom(7, "Write the report")
			head := plain(roomHeadAll(a, 120))
			if !strings.Contains(head, roomCrumbRoot+roomCrumbSep+"Write the report") ||
				!strings.Contains(head, taskHeldWord) {
				t.Fatalf("the room header is %q", head)
			}

			// AND IT GOES BACK to whatever the node was doing when the hold clears,
			// because nothing about the node's state ever moved.
			node.waiting = ""
			if got := a.roomStateWord(node); got != tc.back {
				t.Fatalf("a node with nothing holding it says %q, want %q", got, tc.back)
			}
		})
	}
}

// ── the sweep ───────────────────────────────────────────────────────────────

// bannedWords is the vocabulary of the machinery that judges finished work and
// of the machinery that decides when it may run, and NONE OF IT IS A PERSON'S
// BUSINESS. A person delegated a piece of work; what they are owed back is the
// work, its gaps, the news that somebody has to look, or the plain reason it is
// waiting — never the org chart of the thing that decided any of it. The check
// is case-insensitive because a shouted verdict is still a verdict.
//
// THE HOLD WORDS ARE HERE FOR THE SAME REASON THE VERDICT WORDS ARE. "the
// machine is busy" is a fact about the person's own computer and "rate limited"
// is a fact about the provider they are paying; a scheduler's own nouns for the
// same two facts are this program describing itself to somebody who asked it for
// a report.
var bannedWords = []string{"auditor", "audit", "verdict", "verified", "unverified", "refuted",
	"throttle", "semaphore", "backoff", "429"}

// landing is one state a person is shown, as the engine reports it — with the
// outcome sentence already in a person's words, which is the engine branch's
// half of this same law.
type landing struct {
	what   string
	state  session.TaskState
	notice session.TaskNotice
}

// EVERY TERMINAL STATE, ON EVERY SURFACE IT REACHES. The card is drawn open, so
// the report, the brief and the facts block are all on screen; the roster is
// drawn with every group unfolded, so no row is hiding behind a heading. If any
// of the machinery's words survives anywhere in this program, it is in this
// output.
func TestNoTerminalStateEverSpeaksOfTheMachinery(t *testing.T) {
	for _, tc := range []landing{
		{"a clean merge", session.TaskDone, session.TaskNotice{
			Elapsed: 4 * time.Minute, Merge: mergeWordMerged, CostUSD: 0.42,
			Report:  "the guard is in and the regression test passes",
			Changed: []string{"internal/parse/keys.go"},
		}},
		{"a kept branch", session.TaskDone, session.TaskNotice{
			Elapsed: 4 * time.Minute, Merge: mergeWordConflicted, Branch: "task/parser",
			Report:  "the parser is ported; two files clash with work that landed since",
			Changed: []string{"internal/parse/keys.go"},
		}},
		{"work that came back short", session.TaskFailed, session.TaskNotice{
			Elapsed: 2 * time.Minute, Merge: mergeWordAborted, Branch: "task/parser",
			Report: "incomplete — the key table is ported and the escape table is not",
		}},
		{"a landing that needs a person", session.TaskUnverified, session.TaskNotice{
			Elapsed: 6 * time.Minute, Merge: mergeWordAborted, Branch: "task/parser",
			Report:  "finished, but needs your look — nothing came back either way",
			Changed: []string{"internal/parse/keys.go", "internal/parse/keys_test.go"},
		}},
		{"a node still closing a gap", session.TaskRunning, mendingNotice("adding amp-labs to the report")},
		// AND THE MOMENTS A NODE IS NOT WORKING AT ALL. A hold is drawn in the
		// engine's own reason and the engine's own reason is a plain fact about a
		// machine or a provider — never the name of whatever is keeping the count.
		{"a node the machine has no room for", session.TaskQueued, heldNotice(waitWordMachine)},
		{"a node behind the cap", session.TaskQueued, heldNotice(waitWordSlot)},
		{"a node whose calls are being paced", session.TaskRunning, heldNotice(waitWordRate)},
	} {
		t.Run(tc.what, func(t *testing.T) {
			a, _, _ := taskApp(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", tc.state, tc.notice)})
			// The card opens onto everything behind it, and the roster draws every
			// node it has — one node with no family around it is one flat row.
			if card := a.doneCardAt(len(a.entries) - 1); card != nil {
				card.open = true
				a.touch()
			}
			seen := strings.ToLower(taskText(a) + "\n" + rosterText(a, 20))
			// A SWEEP OVER AN EMPTY SCREEN PASSES EVERYTHING, so the screen is
			// proved to have the node on it before it is proved to be clean.
			if !strings.Contains(seen, "port the parser") {
				t.Fatalf("%s drew nothing to sweep:\n%s", tc.what, seen)
			}
			for _, banned := range bannedWords {
				if strings.Contains(seen, banned) {
					t.Fatalf("%s says %q to a person:\n%s", tc.what, banned, seen)
				}
			}
		})
	}
}

// THE THREE WORDS THE THIRD STATE IS SPELLED WITH say what is true of it from
// the outside and nothing about what put it there: it finished, and it is on you
// to look. They are asserted as literals because the whole point of them is the
// wording — a constant renamed is a refactor, a constant reworded is a decision.
func TestTheStateNobodyCouldJudgeReadsAsNeedingYourLook(t *testing.T) {
	for _, tc := range []struct{ got, want string }{
		{taskUnverifiedWord, "needs your look"},
		{taskUnverifiedWaits, "finished — look it over"},
		{taskUnverifiedGloss, "finished, but needs your look"},
	} {
		if tc.got != tc.want {
			t.Fatalf("the state is spelled %q, want %q", tc.got, tc.want)
		}
	}

	// AND THE GROUP HEADING ALREADY COMPLIED: a column that files this under
	// "needs you" and then calls the row "unverified" was saying one thing twice
	// and getting one of them wrong.
	if railGroupWords[railAttention] != "needs you" {
		t.Fatalf("the attention group is headed %q", railGroupWords[railAttention])
	}
}

// ── NO ENGINE TOKEN REACHES A SCREEN ────────────────────────────────────────
//
// THE DEFECT THIS CLOSES, found on a real run: a task started in a folder with
// no repository lands with `merge: inplace`, and the room's header, the settled
// card's tail and its expansion, and the roster's row all printed that word —
// `✓ run these shell · inplace · 55s · $0.0029`. `inplace` is internal/session's
// note to itself about what it did with a branch it could not cut; it is the
// machinery's vocabulary on a person's screen, and it was reachable on an
// ordinary launch rather than on an edge.
//
// The fix was not a fifth case. Four readers each had a switch with a
// fall-through onto [taskNode.merge], so every merge word somebody had not
// thought about was drawn raw; they now go through one table
// (task.go's [mergeScreenWords]).
//
// THIS TEST IS WHAT MAKES THE TABLE A GUARANTEE. It reads the engine's OWN const
// block — the strings [session.TaskNotice].Merge can carry, spelled once in
// internal/session — and fails when any of them has no line in the table. A
// fifth outcome added to the engine breaks this test by name on the day it is
// added, rather than reaching somebody's screen as a word nobody can read.
func TestEveryMergeWordTheEngineCanPublishHasAScreenWord(t *testing.T) {
	for _, merge := range engineMergeWords(t) {
		word, ok := mergeScreenWords[merge]
		if !ok {
			t.Errorf("internal/session can publish merge %q and this surface has no screen word for it.\n"+
				"Add a line to mergeScreenWords (task.go) saying what a PERSON reads when work lands that way.\n"+
				"Do not draw the engine's word: %q is a note internal/session makes to itself.", merge, merge)
			continue
		}
		if strings.TrimSpace(word) == "" {
			t.Errorf("merge %q maps to nothing; every landing has something true to say about it", merge)
		}
	}
	// AND THE TABLE HOLDS NOTHING THE ENGINE CANNOT SEND, so a line here is
	// never a translation of a word that stopped existing.
	engine := map[string]bool{}
	for _, merge := range engineMergeWords(t) {
		engine[merge] = true
	}
	for merge := range mergeScreenWords {
		if !engine[merge] {
			t.Errorf("mergeScreenWords translates %q, which internal/session no longer publishes — delete the line", merge)
		}
	}
}

// AND NO READER GOES ROUND THE TABLE. The four sites that draw where work landed
// are asked with the one merge word that is not a screen word, and none of them
// may say it.
func TestNoSurfaceDrawsTheEnginesOwnMergeWord(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Port the loader")
	node := a.roomNode()
	if node == nil {
		t.Fatal("the roster has no row for the open room")
	}
	node.state, node.merge, node.branch = session.TaskDone, mergeWordInPlace, ""
	node.cost, node.elapsed = 0.03, 55*time.Second

	said := map[string]string{
		"the room's header": plain(roomHeadAll(a, 160)),
		"the roster's row":  plain(strings.Join(a.railUnder(node, 60), "\n")),
	}
	card := &taskDone{merge: mergeWordInPlace, branch: "work/7", open: true, span: 55 * time.Second}
	card.open = false
	said["the settled card's collapsed tail"] = plain(a.doneTail(card))
	card.open = true
	said["the settled card's expansion"] = plain(strings.Join(a.doneDetail(card, 80), "\n"))

	for where, line := range said {
		if strings.Contains(line, mergeWordInPlace) {
			t.Errorf("%s draws the engine's own word:\n%s", where, line)
		}
		if !strings.Contains(line, taskInPlaceLanding) {
			t.Errorf("%s does not say where the work landed (%q):\n%s", where, taskInPlaceLanding, line)
		}
	}
}

// engineMergeWords is the strings [session.TaskNotice].Merge can carry, read
// out of internal/session's own const block.
//
// IT PARSES THE SOURCE rather than importing them, because they are unexported —
// they are the engine's private vocabulary, which is the whole point — and a
// list retyped here would be a second source of truth that drifts silently,
// which is the class of defect this file's test exists to end. The block is
// found by the value's shape, not by a comment: every `merge…= "word"` constant
// in that file is one of them.
func engineMergeWords(t *testing.T) []string {
	t.Helper()
	const source = "../session/task_run.go"
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, source, nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", source, err)
	}
	var words []string
	for _, decl := range parsed.Decls {
		block, ok := decl.(*ast.GenDecl)
		if !ok || block.Tok != token.CONST {
			continue
		}
		for _, spec := range block.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || len(value.Values) != 1 {
				continue
			}
			if !strings.HasPrefix(value.Names[0].Name, "merge") {
				continue
			}
			literal, ok := value.Values[0].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				continue
			}
			word, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("%s: %s = %s: %v", source, value.Names[0].Name, literal.Value, err)
			}
			words = append(words, word)
		}
	}
	sort.Strings(words)
	// A READING THAT FOUND NOTHING IS A BROKEN READING, not an empty engine. The
	// const block moving to another file must fail here loudly rather than pass
	// this test by having nothing to check.
	if len(words) < 5 {
		t.Fatalf("%s: found %v, and internal/session publishes five merge words —\n"+
			"the const block has moved and this reader has to be pointed at it", source, words)
	}
	return words
}

// C13: a finished branch kept off a protected checkout is named the same way
// on the settled card, the rail and the room, and the engine's bare token never
// becomes a person-facing label.
func TestC13AKeptLandingSaysBranchKeptEverywhere(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Protect the checkout", session.TaskDone,
		session.TaskNotice{Merge: mergeWordKept, Branch: "task/protect"})})
	node := a.tasks[7]
	want := taskBranchKept + " · task/protect"

	if got := plain(strings.Join(a.railUnder(node, 60), "\n")); got != want {
		t.Fatalf("the rail says %q, want %q", got, want)
	}
	card := &taskDone{merge: mergeWordKept, branch: "task/protect"}
	if got := plain(a.doneTail(card)); !strings.Contains(got, " · "+want) {
		t.Fatalf("the settled card says %q, want it to contain %q", got, want)
	}
	if got := a.roomStateWord(node); got != taskBranchKept {
		t.Fatalf("the room header says %q, want %q", got, taskBranchKept)
	}
	for where, got := range map[string]string{
		"rail": plain(strings.Join(a.railUnder(node, 60), "\n")),
		"card": plain(a.doneTail(card)),
		"room": a.roomStateWord(node),
	} {
		if got == mergeWordKept || strings.Contains(got, " · "+mergeWordKept+" · ") {
			t.Fatalf("the %s draws the engine token on its own: %q", where, got)
		}
	}
}
