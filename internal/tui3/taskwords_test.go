package tui3

import (
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
	// The header is asserted through the line a person actually reads, not only
	// through the word: the trail, the state, the clock and the spend are one
	// sentence and the state is the second thing in it.
	a.room = &taskRoom{id: 7, title: "Write the report", unfolded: map[int]bool{}, live: -1, think: -1}
	head := plain(a.roomHeadWord(120))
	if !strings.Contains(head, roomCrumbRoot+roomCrumbSep+"Write the report · "+taskFinishingWord) {
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

// ── the sweep ───────────────────────────────────────────────────────────────

// bannedWords is the vocabulary of the machinery that judges finished work, and
// NONE OF IT IS A PERSON'S BUSINESS. A person delegated a piece of work; what
// they are owed back is the work, its gaps, or the news that somebody has to
// look — never the org chart of the thing that decided which. The check is
// case-insensitive because a shouted verdict is still a verdict.
var bannedWords = []string{"auditor", "audit", "verdict", "verified", "unverified", "refuted"}

// landing is one terminal state, as the engine reports it — with the outcome
// sentence already in a person's words, which is the engine branch's half of
// this same law.
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
	} {
		t.Run(tc.what, func(t *testing.T) {
			a, _, _ := taskApp(t)
			drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Port the parser", tc.state, tc.notice)})
			// The card opens onto everything behind it, and every group opens so
			// that the sweep reads rows rather than headings.
			if card := a.doneCardAt(len(a.entries) - 1); card != nil {
				card.open = true
				a.touch()
			}
			for g := railGroup(0); g < railGroupCount; g++ {
				a.railSetOpen(g, true)
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
