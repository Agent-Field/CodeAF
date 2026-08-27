package session

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// landedRow is one finished piece of work in the project's record, spelled the
// short way these tests need it: who, when, and which files it left behind.
func landedRow(id, session, title string, ended time.Time, files ...string) TaskIndexEntry {
	return TaskIndexEntry{
		ID:           id,
		SessionID:    session,
		Title:        title,
		Status:       string(TaskDone),
		EndedAt:      ended,
		Files:        files,
		FilesChanged: len(files),
	}
}

// windowWithTask is another window with one task out, holding the files it names.
func windowWithTask(session, id, title string, now time.Time, files ...string) SessionPresence {
	return SessionPresence{
		Schema: presenceSchema, SessionID: session, UpdatedAt: now, State: PresenceWorking,
		RunningTasks: []PresenceTask{{ID: id, Title: title, State: string(TaskRunning), Files: files}},
	}
}

// ── THE GROUND MOVED WHILE THE WORK RAN ─────────────────────────────────────

func TestWorkThatLandedInTheseFilesDuringTheRunIsSaidPlainly(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("4", "another-window", "rail permanence", start.Add(20*time.Minute), "internal/tui3/home.go"),
	}
	reason := groundShiftReason(rows, Elsewhere{}, []string{"internal/tui3/home.go", "internal/tui3/task.go"}, start)

	want := `"rail permanence" changed internal/tui3/home.go while this ran`
	if reason != want {
		t.Fatalf("the reason is %q, want %q", reason, want)
	}
	// The words that must never be in it, whatever else changes about the wording.
	for _, banned := range []string{"conflict", "stale", "verdict", "audit", "refuted"} {
		if strings.Contains(strings.ToLower(reason), banned) {
			t.Fatalf("the reason says %q: %s", banned, reason)
		}
	}
}

func TestWorkThatLandedBeforeTheRunStartedIsNotTheGroundMoving(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		// Landed a full hour before this node was even admitted: the run READ it,
		// it did not get written over by it.
		landedRow("4", "another-window", "rail permanence", start.Add(-time.Hour), "internal/tui3/home.go"),
	}
	if reason := groundShiftReason(rows, Elsewhere{}, []string{"internal/tui3/home.go"}, start); reason != "" {
		t.Fatalf("work that finished before the run began was flagged: %s", reason)
	}
}

// A ROW THAT NAMED NO FILES IS NOT A ROW THAT TOUCHED THESE ONES. Rows written
// by an older build, and rows for work that genuinely wrote nothing, look
// exactly alike — and at land time a flag raised on that silence would fire on
// most of the file (taskground.go's first law).
func TestARowThatNamedNoFilesRaisesNothing(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		{ID: "4", SessionID: "another-window", Title: "rail permanence",
			Status: string(TaskDone), EndedAt: start.Add(20 * time.Minute)},
	}
	if reason := groundShiftReason(rows, Elsewhere{}, []string{"internal/tui3/home.go"}, start); reason != "" {
		t.Fatalf("a row that said nothing about files was read as overlap: %s", reason)
	}
}

func TestWorkInOtherFilesEntirelyChangesNothingAboutTheLanding(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("4", "another-window", "rail permanence", start.Add(20*time.Minute), "internal/session/rail.go"),
	}
	away := NewElsewhere(time.Now(), nil,
		windowWithTask("third", "9", "the manual sweep", time.Now(), "internal/manual/chat/tasks.md"))
	if reason := groundShiftReason(rows, away, []string{"internal/tui3/home.go"}, start); reason != "" {
		t.Fatalf("a landing nobody was near was flagged: %s", reason)
	}
}

// ── AND THE WINDOW THAT IS STILL IN THERE ───────────────────────────────────

// A live claim gets the PRESENT tense and no more. Nothing on a claim says when
// those paths were written, so "changed while this ran" would be a sentence the
// file cannot support — what it can support is that somebody is in there now.
func TestAWindowStillWritingTheseFilesIsSaidInThePresentTense(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, map[string]string{"theirs": "the other window"},
		windowWithTask("theirs", "3", "drop-up nearest", now, "internal/tui3/home.go"))
	reason := groundShiftReason(nil, away, []string{"internal/tui3/home.go"}, now.Add(-time.Hour))

	want := `"drop-up nearest" is also working in internal/tui3/home.go`
	if reason != want {
		t.Fatalf("the reason is %q, want %q", reason, want)
	}
}

// A claim from a window whose task nothing named still says the one true thing
// about it — that it is another window — rather than a pair of empty quotes.
func TestAnUnnamedClaimIsCalledAnotherWindow(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil,
		windowWithTask("theirs", "3", "", now, "internal/tui3/home.go"))
	reason := groundShiftReason(nil, away, []string{"internal/tui3/home.go"}, now.Add(-time.Hour))

	want := "another window is also working in internal/tui3/home.go"
	if reason != want {
		t.Fatalf("the reason is %q, want %q", reason, want)
	}
}

// A claim that named no files at all is the same silence a row's absent
// citations are, and it is answered the same way.
func TestAClaimThatNamedNoFilesRaisesNothing(t *testing.T) {
	now := time.Now()
	away := NewElsewhere(now, nil, windowWithTask("theirs", "3", "drop-up nearest", now))
	if reason := groundShiftReason(nil, away, []string{"internal/tui3/home.go"}, now.Add(-time.Hour)); reason != "" {
		t.Fatalf("a claim that said nothing about files was read as overlap: %s", reason)
	}
}

// BOTH SOURCES, TWO SENTENCES. They are two different facts about two different
// tenses, and one line over the pair would have to be wrong about one of them.
func TestTheRecordAndTheLiveWindowsAreSaidSeparately(t *testing.T) {
	now := time.Now()
	start := now.Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("4", "another-window", "rail permanence", start.Add(20*time.Minute), "internal/tui3/home.go"),
	}
	away := NewElsewhere(now, nil, windowWithTask("theirs", "3", "drop-up nearest", now, "internal/tui3/task.go"))
	reason := groundShiftReason(rows, away, []string{"internal/tui3/home.go", "internal/tui3/task.go"}, start)

	lines := strings.Split(reason, "\n")
	if len(lines) != 2 {
		t.Fatalf("the reason is %q, want one line per source", reason)
	}
	if lines[0] != `"rail permanence" changed internal/tui3/home.go while this ran` {
		t.Fatalf("the record's line is %q", lines[0])
	}
	if lines[1] != `"drop-up nearest" is also working in internal/tui3/task.go` {
		t.Fatalf("the live line is %q", lines[1])
	}
}

// ── THE LIST STOPS COUNTING OUT LOUD ────────────────────────────────────────

func TestALongOverlapNamesTwoFilesAndCountsTheRest(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	var mine []string
	for i := range 6 {
		mine = append(mine, fmt.Sprintf("internal/tui3/pane%d.go", i))
	}
	rows := []TaskIndexEntry{landedRow("4", "another-window", "the pane sweep", start.Add(time.Minute), mine...)}
	reason := groundShiftReason(rows, Elsewhere{}, mine, start)

	want := `"the pane sweep" changed internal/tui3/pane0.go, internal/tui3/pane1.go and 4 more while this ran`
	if reason != want {
		t.Fatalf("the reason is %q, want %q", reason, want)
	}
}

func TestSeveralPiecesOfWorkAreNamedTwiceAndThenCounted(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("4", "a", "rail permanence", start.Add(time.Minute), "internal/tui3/home.go"),
		landedRow("5", "b", "drop-up nearest", start.Add(2*time.Minute), "internal/tui3/home.go"),
		landedRow("6", "c", "the brief shaper", start.Add(3*time.Minute), "internal/tui3/home.go"),
	}
	reason := groundShiftReason(rows, Elsewhere{}, []string{"internal/tui3/home.go"}, start)

	want := `"rail permanence", "drop-up nearest" and 1 more changed internal/tui3/home.go while this ran`
	if reason != want {
		t.Fatalf("the reason is %q, want %q", reason, want)
	}
}

// One file, two other tasks in it, is ONE file — the paths are the node's own,
// deduped, not one copy per source.
func TestOneFileTwoOtherTasksIsStillOneFile(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("4", "a", "rail permanence", start.Add(time.Minute), "internal/tui3/home.go"),
		landedRow("5", "b", "drop-up nearest", start.Add(2*time.Minute), "internal/tui3/home.go"),
	}
	reason := groundShiftReason(rows, Elsewhere{}, []string{"internal/tui3/home.go"}, start)
	if strings.Count(reason, "internal/tui3/home.go") != 1 {
		t.Fatalf("the file is named more than once: %s", reason)
	}
}

// ── WHOSE WORK COUNTS AS SOMEBODY ELSE'S ────────────────────────────────────

// A sub-task branches off its parent's worktree and merges back into it, so a
// child landing in a file its parent also wrote is the design working.
func TestANodesOwnChildrenAreNotTheGroundMoving(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{1, 2, 3}}
	parent := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "the whole job"}}
	kid := &TaskNode{graph: graph, id: 2, parent: 1, spec: taskSpec{title: "a piece of it"}}
	grandkid := &TaskNode{graph: graph, id: 3, parent: 2, spec: taskSpec{title: "a piece of the piece"}}
	graph.nodes[1], graph.nodes[2], graph.nodes[3] = parent, kid, grandkid

	family := parent.family()
	for _, id := range []string{"1", "2", "3"} {
		if !family[id] {
			t.Fatalf("task %s is not counted as the node's own: %v", id, family)
		}
	}

	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("2", "mine", "a piece of it", start.Add(time.Minute), "internal/tui3/home.go"),
		landedRow("3", "mine", "a piece of the piece", start.Add(2*time.Minute), "internal/tui3/home.go"),
		landedRow("7", "mine", "somebody else's lane", start.Add(3*time.Minute), "internal/tui3/task.go"),
	}
	kept := groundOthers(rows, "mine", family)
	if len(kept) != 1 || kept[0].ID != "7" {
		t.Fatalf("the family was not dropped: %+v", kept)
	}
	// And a task with the same id in ANOTHER session is not this node's child.
	other := groundOthers([]TaskIndexEntry{
		landedRow("2", "theirs", "their own task 2", start.Add(time.Minute), "internal/tui3/home.go"),
	}, "mine", family)
	if len(other) != 1 {
		t.Fatalf("another window's task 2 was mistaken for this node's child: %+v", other)
	}
}

// ── THE RUN WINDOW ──────────────────────────────────────────────────────────

func TestTheRunWindowIsTheNodesOwnStart(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	began := time.Now().Add(-30 * time.Minute)
	node := &TaskNode{graph: graph, id: 1, started: began}
	if got := node.runStart(); !got.Equal(began) {
		t.Fatalf("the window starts at %s, want %s", got, began)
	}
}

// A NODE REHYDRATED FROM A CHECKPOINT HAS NO START, only the age it had. The
// window is measured back from that, which lands later than the truth and can
// only ever miss an overlap rather than invent one.
func TestARehydratedNodeMeasuresItsWindowBackFromItsAge(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, elapsed: 20 * time.Minute}
	got := node.runStart()
	if got.IsZero() {
		t.Fatal("a node with an age answered no window at all")
	}
	if age := time.Since(got); age < 19*time.Minute || age > 21*time.Minute {
		t.Fatalf("the window is %s old, want about twenty minutes", age)
	}
}

// AND A NODE NOTHING CAN DATE ASKS NOTHING. A check that guessed the moment
// would flag work on the strength of an invented clock.
func TestANodeWithNoWindowIsNeverFlagged(t *testing.T) {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1}
	if got := node.runStart(); !got.IsZero() {
		t.Fatalf("a node nothing can date answered %s", got)
	}
	rows := []TaskIndexEntry{
		landedRow("4", "another-window", "rail permanence", time.Now(), "internal/tui3/home.go"),
	}
	if reason := groundShiftReason(rows, Elsewhere{}, []string{"internal/tui3/home.go"}, time.Time{}); reason != "" {
		t.Fatalf("a node with no window was flagged anyway: %s", reason)
	}
}

// A node that wrote nothing cannot have written over anybody.
func TestANodeThatWroteNothingIsNeverFlagged(t *testing.T) {
	start := time.Now().Add(-time.Hour)
	rows := []TaskIndexEntry{
		landedRow("4", "another-window", "rail permanence", start.Add(time.Minute), "internal/tui3/home.go"),
	}
	if reason := groundShiftReason(rows, Elsewhere{}, nil, start); reason != "" {
		t.Fatalf("a node that wrote nothing was flagged: %s", reason)
	}
}

// ── WHAT THE PERSON AND THE MODEL EACH READ ─────────────────────────────────

// The reason rides in the REPORT, which is the one place that reaches both
// readers: the card quotes the first line, and the landing note prints the
// report whole. And it composes with the settle policy rather than replacing it
// — the ask tail and the auto tail still land underneath.
func TestTheReasonReachesTheModelUnderneathTheSettlePolicy(t *testing.T) {
	reason := `"rail permanence" changed internal/tui3/home.go while this ran`
	notice := TaskNotice{
		ID:     12,
		Title:  "port the parser",
		State:  TaskUnverified,
		Report: needsLookLead + reason + "\nThe parser now reads the new header.",
	}
	for _, settle := range []TaskSettle{TaskSettleAsk, TaskSettleAuto} {
		note := taskNote(notice, "", settle, landingAddress{person: true})
		if !strings.Contains(note, "task 12 needs your look") {
			t.Fatalf("under %s the note does not say what state it is in:\n%s", settle, note)
		}
		if !strings.Contains(note, reason) {
			t.Fatalf("under %s the note lost the reason:\n%s", settle, note)
		}
		if !strings.Contains(note, "resolve ") {
			t.Fatalf("under %s the reason clobbered the settle clause:\n%s", settle, note)
		}
	}
	// Under ask the decision stays with the person; under auto the model is told
	// to make it. Neither sentence is written by the ground-shift check.
	if ask := taskNote(notice, "", TaskSettleAsk, landingAddress{person: true}); !strings.Contains(ask, settleAskTail) {
		t.Fatalf("the ask tail is gone:\n%s", ask)
	}
	if auto := taskNote(notice, "", TaskSettleAuto, landingAddress{person: true}); !strings.Contains(auto, settleAutoTail) {
		t.Fatalf("the auto tail is gone:\n%s", auto)
	}
}

// ── THE WHOLE ASK, OVER REAL FILES ──────────────────────────────────────────

// One reading, taken the way a landing takes it: the project's own tasks.jsonl
// for what finished, the bucket's presence files for who is still in there, and
// this node's own family dropped out of both.
func TestALandingAsksTheProjectsOwnFilesAndDropsItsOwnFamily(t *testing.T) {
	bucket := t.TempDir()
	agent, _ := newPresenceSession(t, bucket, "mine")

	// The graph is built by hand rather than admitted, because admitting a node
	// starts it: what is under test is the reading a landing takes, not a run.
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{1, 2}}
	parent := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "the whole job"},
		started: time.Now().Add(-time.Hour)}
	kid := &TaskNode{graph: graph, id: 2, parent: 1, spec: taskSpec{title: "a piece of it"}}
	graph.nodes[1], graph.nodes[2] = parent, kid

	index := filepath.Join(bucket, taskIndexName)
	agent.mu.Lock()
	mine := agent.sessionID()
	agent.mu.Unlock()
	// This node's own child, in the same file — the design working, not the
	// ground moving.
	appendTaskIndex(index, landedRow(strconv.FormatUint(kid.id, 10), mine, "a piece of it",
		time.Now().Add(-30*time.Minute), "internal/tui3/home.go"))
	// Another window's work, landed in the same file inside the run window.
	appendTaskIndex(index, landedRow("4", "theirs", "rail permanence",
		time.Now().Add(-20*time.Minute), "internal/tui3/home.go"))
	// And one that landed long before the run began.
	appendTaskIndex(index, landedRow("5", "theirs", "the old sweep",
		time.Now().Add(-3*time.Hour), "internal/tui3/task.go"))
	// A third window, still writing a third file.
	writeWindow(t, bucket, "third", "the manual sweep", 0,
		PresenceTask{ID: "9", Title: "the manual pass", State: string(TaskRunning),
			Files: []string{"internal/tui3/rail.go"}})

	reason := agent.groundShift(parent, []string{
		"internal/tui3/home.go", "internal/tui3/task.go", "internal/tui3/rail.go",
	})
	if !strings.Contains(reason, `"rail permanence" changed internal/tui3/home.go while this ran`) {
		t.Fatalf("the other window's landing is missing:\n%s", reason)
	}
	if !strings.Contains(reason, `"the manual pass" is also working in internal/tui3/rail.go`) {
		t.Fatalf("the live claim is missing:\n%s", reason)
	}
	if strings.Contains(reason, "a piece of it") {
		t.Fatalf("the node's own child was called somebody else:\n%s", reason)
	}
	if strings.Contains(reason, "the old sweep") || strings.Contains(reason, "task.go") {
		t.Fatalf("work that finished before the run began was flagged:\n%s", reason)
	}
}

// AND NOTHING FOUND CHANGES NOTHING. A project where nobody else has been near
// these paths answers with silence, which is what leaves the landing exactly
// the landing it was.
func TestALandingNobodyIsNearAsksAndHearsNothing(t *testing.T) {
	bucket := t.TempDir()
	agent, _ := newPresenceSession(t, bucket, "mine")
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}, order: []uint64{1}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{title: "port the parser"},
		started: time.Now().Add(-time.Hour)}
	graph.nodes[1] = node

	appendTaskIndex(filepath.Join(bucket, taskIndexName),
		landedRow("4", "theirs", "rail permanence", time.Now().Add(-20*time.Minute), "internal/tui3/home.go"))

	if reason := agent.groundShift(node, []string{"internal/parse/row.go"}); reason != "" {
		t.Fatalf("a landing nobody was near was flagged: %s", reason)
	}
}

// THE FIRST LINE IS THE NEWS. It is what the settle card quotes and what the
// project's index keeps as the row's outcome, so the reason has to be in it.
func TestTheOutcomeALandingKeepsIsTheReasonItself(t *testing.T) {
	reason := `"rail permanence" changed internal/tui3/home.go while this ran`
	report := withReport(needsLookLead+reason, "The parser now reads the new header.")
	if got := taskOutcome(report); got != needsLookLead+reason {
		t.Fatalf("the row's outcome is %q, want the reason", got)
	}
}
