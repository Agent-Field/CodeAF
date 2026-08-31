package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// WHAT THE COLUMN SAYS WHILE THE WORKER IS NOT THE ONE WORKING.
//
// A node's state stays `running` across its worker, the check that reads what
// the worker left, and every repair round that closes what the check found — and
// this surface drew a clock for all of it. The run that made the case for this
// file spent four minutes under a check and six in a repair round with nothing
// on the row but the seconds going up, and the person watching concluded their
// work had hung.
//
// So: the life the node is in, the round it is on, and the finding that sent it
// back — and, for the ordinary life, nothing at all.

// phaseMove is the event the engine sends when a running node moves between its
// three lives (session's EventTaskPhase).
func phaseMove(id uint64, phase string, round, rounds int, text string) session.Event {
	return session.Event{Kind: session.EventTaskPhase, Tool: "propose_task",
		TaskPhase: &session.TaskPhaseNotice{ID: id, Phase: phase, Round: round, Rounds: rounds, Text: text}}
}

// runningNotice is a plainly running node: no gap, no hold, no named phase of
// its own — the node the silence was measured on.
func runningNotice() session.TaskNotice {
	return session.TaskNotice{Model: "openai/gpt-5", CostUSD: 0.31}
}

// checkedNode is a running node with the check under way.
func checkedNode(t *testing.T) (*app, *taskNode) {
	t.Helper()
	a, _, advance := taskApp(t)
	drive(t, a, streamEventMsg{gen: a.gen, ev: update(7, "Write the report", session.TaskRunning, runningNotice())})
	node := a.tasks[7]
	node.tokens = 9_900
	advance(42 * time.Second)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseChecking, 0, 0, "")})
	return a, node
}

// A NODE UNDER THE CHECK SAYS SO, AND KEEPS ITS TELEMETRY. The check has one
// thing to say and it says it in one row, so the standing figures a person opens
// this column for keep the row underneath.
func TestANodeUnderTheCheckSaysWhatIsHappeningAtEveryWidth(t *testing.T) {
	a, node := checkedNode(t)

	for _, tc := range []struct {
		width int
		want  []string
	}{
		{underWidth(railWideCols), []string{taskCheckingWord, "42s · 9.9k · $0.31 · gpt-5"}},
		{underWidth(railCols), []string{taskCheckingWord, "42s · 9.9k · $0.31 · gpt-5"}},
		{underWidth(railSlimCols), []string{"checking what it le…", "42s · 9.9k · $0.31"}},
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

	// And through the column a person actually reads, at the widths the rail
	// itself narrows to — where the row is cut from the right and the words that
	// name the moment are the ones that survive.
	for width, want := range map[int]string{200: "checking what it le", 110: "checking what it le"} {
		a.width = width
		roster := rosterText(a, 12)
		if !strings.Contains(roster, want) {
			t.Fatalf("the roster at %d columns does not say the work is being checked:\n%s", width, roster)
		}
	}
	a.width = 200
}

// A REPAIR ROUND SAYS WHICH ROUND IT IS ON AND WHAT THE CHECK FOUND. The two
// rows spend the whole under-block, which is the trade: the clock and the bill
// are true every second and this is the one thing that explains why work
// somebody thought was finished is being done again.
func TestARepairRoundSaysTheRoundAndTheFinding(t *testing.T) {
	a, node := checkedNode(t)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseRepairing, 1, 1,
		"not done — go test reports no test files")})

	rows := a.railUnder(node, underWidth(railWideCols))
	want := []string{"closing gaps · round 1 of 1", "not done — go test reports no test files"}
	if len(rows) != len(want) {
		t.Fatalf("the under-block is %d rows, want %d:\n%q", len(rows), len(want), rows)
	}
	for i, line := range want {
		if got := plain(rows[i]); got != line {
			t.Fatalf("row %d is %q, want %q", i, got, line)
		}
	}
	// THE ROUND OUTRANKS THE OLDER GAP LINE, and says more than it could: an
	// engine that sends both is an engine whose repair round is running, and
	// "closing gaps · round 1 of 1" is that same news with the round on it.
	node.mending = "go test reports no test files"
	if got := plain(a.railUnder(node, underWidth(railWideCols))[0]); got != "closing gaps · round 1 of 1" {
		t.Fatalf("the gap line took the row back: %q", got)
	}

	// And through the column a person actually reads, where the rail's own width
	// cuts both rows from the right — the round and the opener, which are the
	// halves that name the moment, always survive.
	a.width = 200
	roster := rosterText(a, 12)
	for _, line := range []string{"closing gaps · round 1 of", "not done — go test report"} {
		if !strings.Contains(roster, line) {
			t.Fatalf("the roster does not carry %q:\n%s", line, roster)
		}
	}
	// AND NONE OF THE MACHINERY IS IN IT. There is a gate and a judgement behind
	// this row and a person watching their own work has no use for either.
	for _, banned := range []string{"audit", "verdict", "verified", "refuted", "repair", "check the"} {
		if strings.Contains(strings.ToLower(roster), banned) {
			t.Fatalf("the roster says %q:\n%s", banned, roster)
		}
	}
}

// AND WHEN THE NODE IS BACK AT ITS OWN WORK THE ROW IS WHAT IT ALWAYS WAS. The
// ordinary life is what this column has always drawn, and a row that added
// "working" to it would be the surface narrating its own default.
func TestANodeBackAtWorkDrawsNothingExtra(t *testing.T) {
	a, node := checkedNode(t)
	if word := taskPhaseLine(node); word == "" {
		t.Fatal("the fixture drew no phase to clear")
	}

	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseWorking, 0, 0, "")})
	rows := a.railUnder(node, underWidth(railCols))
	if len(rows) != 1 {
		t.Fatalf("a working node's under-block is %d rows, want the telemetry alone:\n%q", len(rows), rows)
	}
	if got := plain(rows[0]); got != "42s · 9.9k · $0.31 · gpt-5" {
		t.Fatalf("the telemetry row is %q", got)
	}
	if word := taskPhaseLine(node); word != "" {
		t.Fatalf("a working node draws %q, want nothing", word)
	}

	// AND A LANDING CLEARS IT even though the landing is not a phase move: a node
	// that settles under a check sends no way out.
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseChecking, 0, 0, "")})
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: update(7, "Write the report", session.TaskDone, runningNotice())})
	if node.phase != "" || node.phaseFinding != "" {
		t.Fatalf("a landed node still says %q / %q", node.phase, node.phaseFinding)
	}
}

// THE ROOM SAYS THE SAME LIFE IN ITS OWN LINE. A person standing inside a node's
// page and watching it go quiet reads the header, and "working" is the answer
// that sends them looking for a fault that is not there.
func TestTheRoomHeaderSaysWhichLifeTheNodeIsIn(t *testing.T) {
	a, node := checkedNode(t)

	if got := a.roomStateWord(node); got != taskCheckingWord {
		t.Fatalf("the header calls a checked node %q, want %q", got, taskCheckingWord)
	}
	a.room = &taskRoom{id: 7, title: "Write the report", unfolded: map[int]bool{}, live: -1, think: -1}
	head := plain(a.roomHeadWord(120))
	if !strings.Contains(head, roomCrumbRoot+roomCrumbSep+"Write the report · "+taskCheckingWord) {
		t.Fatalf("the room header is %q", head)
	}
	if strings.Contains(head, stateWorking.String()) {
		t.Fatalf("the room header calls a checked node working: %q", head)
	}

	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseRepairing, 1, 1, "not done — nothing was tested")})
	if got := a.roomStateWord(node); got != "closing gaps · round 1 of 1" {
		t.Fatalf("the header calls a repairing node %q", got)
	}

	// AND IT GOES BACK TO WORKING, because nothing about the node's state ever
	// moved: it was running through all of it.
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(7, session.TaskPhaseWorking, 0, 0, "")})
	if got := a.roomStateWord(node); got != stateWorking.String() {
		t.Fatalf("a node back at work says %q, want %q", got, stateWorking.String())
	}
}

// A MOVE ABOUT A NODE NOBODY HAS HEARD OF OPENS NOTHING. The phase is news about
// a row an update put there, and a second door onto the roster is a row with no
// title, no state and no clock.
func TestAPhaseAboutAnUnknownNodeOpensNoRow(t *testing.T) {
	a, _, _ := taskApp(t)
	drive(t, a, taskEventMsg{gen: a.taskGen, ev: phaseMove(41, session.TaskPhaseChecking, 0, 0, "")})
	if _, ok := a.tasks[41]; ok {
		t.Fatal("a phase move opened a row of its own")
	}
}

// AND HOME'S OWN ROW SAYS IT TOO. The left column's task row quotes what the
// node's room last did, and a check runs outside the room — so through the
// minutes of one, the row that is meant to say what is happening quotes a call
// that finished before the check started.
func TestAHomeRowSaysTheNodeIsBeingChecked(t *testing.T) {
	a := newTestApp(nil)
	entry := session.TaskIndexEntry{
		Label: "Write the report", Status: string(session.TaskRunning),
		Activity: "bash go test ./... · 24s", Phase: session.TaskPhaseChecking,
	}
	row := session.SessionRow{Open: true, Tasks: session.TaskRollup{Rows: []session.TaskIndexEntry{entry}}}

	under := plain(strings.Join(homeWorkUnder(entry, row, 80, a.pal), "\n"))
	if !strings.Contains(under, taskCheckingWord) {
		t.Fatalf("the home row does not say the work is being checked: %q", under)
	}
	if strings.Contains(under, "go test ./...") {
		t.Fatalf("the home row still quotes the call the check replaced: %q", under)
	}

	// AND A NODE AT ITS OWN WORK KEEPS THE CALL, which is what this row has
	// always said and is still the most specific thing true of it.
	entry.Phase = session.TaskPhaseWorking
	under = plain(strings.Join(homeWorkUnder(entry, row, 80, a.pal), "\n"))
	if !strings.Contains(under, "go test ./...") {
		t.Fatalf("the home row lost the call a working node is inside: %q", under)
	}
}
