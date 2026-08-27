package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── THE STAMP ───────────────────────────────────────────────────────────────

func TestTheToldStampRoundTripsAndIsNotTheLookStamp(t *testing.T) {
	dir := t.TempDir()
	if at := LastTold(dir); !at.IsZero() {
		t.Fatalf("a folder nothing has written answers %v, want no origin at all", at)
	}
	when := time.Now().Add(-time.Hour).Round(0)
	NoteTold(dir, when)
	got := LastTold(dir)
	if got.IsZero() || !got.Equal(when.UTC()) {
		t.Fatalf("the stamp reads back %v, want %v", got, when.UTC())
	}
	// The name matters as much as the value: home's stamp means a PERSON looked
	// at the dashboard, this one means THIS session's model was told, and a
	// folder holding two files that meant nearly the same thing is the drift the
	// codebase legislates against.
	if _, err := os.Stat(filepath.Join(dir, toldName)); err != nil {
		t.Fatalf("the stamp is not at %s: %v", toldName, err)
	}
	if toldName == lookStampName {
		t.Fatal("the told stamp and home's look stamp share a name")
	}
}

func TestAnUnreadableToldStampIsNoOriginRatherThanAGuess(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, toldName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if at := LastTold(dir); !at.IsZero() {
		t.Fatalf("a torn stamp answers %v, want no origin — repeating news is the harmless direction", at)
	}
	// And a schema this build does not know is the same fact.
	if err := os.WriteFile(filepath.Join(dir, toldName), []byte(`{"schema":99,"toldAt":"2026-08-20T10:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if at := LastTold(dir); !at.IsZero() {
		t.Fatalf("a stamp from a schema this build refuses answers %v, want no origin", at)
	}
}

// ── WHOSE LANDINGS ARE NEWS ─────────────────────────────────────────────────

func TestTheDeltaLeavesThisSessionsOwnLandingsOut(t *testing.T) {
	ended := time.Now().Add(-10 * time.Minute)
	rows := []TaskIndexEntry{
		deltaLandedRow("1", "mine", "Fix the nil-map crash", ended),
		deltaLandedRow("4", "theirs", "Sweep the call sites", ended),
	}
	got := landedElsewhere(rows, []string{"mine"}, ended.Add(-time.Hour), deltaLandedRows)
	if len(got) != 1 {
		t.Fatalf("the delta names %d landings, want only the other window's: %+v", len(got), got)
	}
	if got[0].Label != "Sweep the call sites" {
		t.Fatalf("the delta names %q, want the other window's work", got[0].Label)
	}
}

func TestTheDeltaSaysNothingWhenNothingHappened(t *testing.T) {
	now := time.Now()
	rows := []TaskIndexEntry{
		// Landed BEFORE the moment this session was told, so it is not news.
		deltaLandedRow("1", "theirs", "Sweep the call sites", now.Add(-2*time.Hour)),
		// Still running, so it is the present half's to report and not this one's.
		{ID: "2", Label: "Port the parser", Status: string(TaskRunning), SessionID: "theirs"},
	}
	if got := landedElsewhere(rows, []string{"mine"}, now.Add(-time.Hour), deltaLandedRows); len(got) != 0 {
		t.Fatalf("the delta names %+v, want nothing", got)
	}
	if block := renderElsewhereBlock(nil, nil); block != "" {
		t.Fatalf("an empty delta renders %q, want not one byte — the emptiness law", block)
	}
}

func TestTheDeltaKeepsTheNewestLandingsAndCapsTheRest(t *testing.T) {
	now := time.Now()
	var rows []TaskIndexEntry
	for at := 0; at < deltaLandedRows+4; at++ {
		rows = append(rows, deltaLandedRow(string(rune('a'+at)), "theirs", "Task", now.Add(-time.Duration(at)*time.Minute)))
	}
	got := landedElsewhere(rows, []string{"mine"}, now.Add(-time.Hour), deltaLandedRows)
	if len(got) != deltaLandedRows {
		t.Fatalf("the delta names %d landings, want the cap of %d", len(got), deltaLandedRows)
	}
}

// ── WHAT IT SAYS, AND THAT IT SAYS IT ONCE ──────────────────────────────────

func TestTheBlockNamesLandingsFilesAndTheOtherWindowsRunningWork(t *testing.T) {
	landed := []deltaLanding{{
		Key:     "theirs\x001",
		Label:   "Fix the nil-map crash",
		Status:  "done",
		Outcome: "Added the guard and the regression test; the parser suite passes.",
		Files:   []string{"internal/reconciler/state.go"},
		Wrote:   3,
	}}
	live := []ElsewhereTask{{
		SessionID: "theirs",
		Session:   "docs pass",
		Task: PresenceTask{
			ID: "4", Title: "Sweep the call sites", State: string(TaskRunning),
			Files: []string{"internal/session/agent.go"},
		},
	}}
	block := renderElsewhereBlock(landed, live)
	for _, want := range []string{
		"<elsewhere>",
		"Work on this project from outside this conversation. Facts, not requests.",
		"recently landed in other windows:",
		"- Fix the nil-map crash · done · internal/reconciler/state.go, and 2 more",
		"  Added the guard and the regression test; the parser suite passes.",
		"running in another window now:",
		`- Sweep the call sites · window "docs pass" · internal/session/agent.go`,
		"</elsewhere>",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("the block is missing %q:\n%s", want, block)
		}
	}
	// NO CLOCK IN IT. An age would move every turn, and a block that moves every
	// turn is re-sent every turn.
	for _, banned := range []string{"running for", "ago", "ended"} {
		if strings.Contains(block, banned) {
			t.Fatalf("the block carries %q, which would churn every turn:\n%s", banned, block)
		}
	}
}

func TestALandingAlreadyToldIsKeptButNeverToldTwice(t *testing.T) {
	one := deltaLanding{Key: "theirs\x001", Label: "Fix the nil-map crash", Status: "done"}
	two := deltaLanding{Key: "theirs\x002", Label: "Sweep the call sites", Status: "done"}

	told := deltaRemember(nil, []deltaLanding{one}, deltaLandedRows)
	// The same row read again — a clock skew, a lost stamp — must not double it.
	told = deltaRemember(told, []deltaLanding{one}, deltaLandedRows)
	if len(told) != 1 {
		t.Fatalf("the block holds %d rows for one landing: %+v", len(told), told)
	}
	// And the first landing is still there when the second arrives, so a model
	// told at turn five has not forgotten by turn nine.
	told = deltaRemember(told, []deltaLanding{two}, deltaLandedRows)
	if len(told) != 2 || told[0].Key != two.Key {
		t.Fatalf("want the newest first over both landings, got %+v", told)
	}
	// The oldest goes when the cap bites.
	var many []deltaLanding
	for at := 0; at < deltaLandedRows+3; at++ {
		many = append(many, deltaLanding{Key: string(rune('a' + at)), Label: "Task"})
	}
	if got := deltaRemember(told, many, deltaLandedRows); len(got) != deltaLandedRows {
		t.Fatalf("the block holds %d rows, want the cap of %d", len(got), deltaLandedRows)
	}
}

// ── THE DELIVERY, END TO END ────────────────────────────────────────────────

// A conversation with another window beside it is told once, the stamp moves,
// and a second turn over an unchanged project sends nothing new.
func TestTheDeltaIsDeliveredOnceAndTheStampAdvances(t *testing.T) {
	bucket := t.TempDir()
	mine := filepath.Join(bucket, "mine")
	if err := os.MkdirAll(mine, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	writeWindow(t, bucket, "theirs", "docs pass", time.Second,
		PresenceTask{ID: "4", Title: "Sweep the call sites", State: string(TaskRunning),
			Files: []string{"internal/session/agent.go"}})
	appendTaskIndex(filepath.Join(bucket, taskIndexName),
		deltaLandedRow("1", "theirs", "Fix the nil-map crash", time.Now().Add(-time.Minute)))

	agent := &Agent{config: Config{Place: Place{Dir: mine, Workspace: "/work/aforge"}}}
	agent.messages = []ai.Message{textMessage("system", "base")}
	agent.system = "base"

	agent.refreshElsewhere()
	first := agent.elsewhereText
	if !strings.Contains(first, "Fix the nil-map crash") || !strings.Contains(first, "Sweep the call sites") {
		t.Fatalf("the first delivery says %q, want both halves", first)
	}
	if !strings.Contains(volatileNote(agent), "<elsewhere>") {
		t.Fatal("the block was built and never reached the note the request carries")
	}
	if LastTold(mine).IsZero() {
		t.Fatal("the model was told and the stamp did not advance")
	}

	// NOTHING HAS MOVED, so nothing is rebuilt: the same landing is not read out
	// of the index a second time, and no second note lands, so the transcript
	// the provider cached is still the transcript it is sent.
	before := len(agent.messages)
	agent.refreshElsewhere()
	if agent.elsewhereText != first {
		t.Fatalf("an unchanged project changed the block:\nwas %q\nnow %q", first, agent.elsewhereText)
	}
	if got := volatileNote(agent); !strings.Contains(got, "Fix the nil-map crash") {
		t.Fatalf("the standing note lost its block:\n%s", got)
	}
	if len(agent.messages) != before {
		t.Fatalf("an unchanged block landed a second note: %d messages, was %d", len(agent.messages), before)
	}
	if strings.Count(agent.elsewhereText, "Fix the nil-map crash") != 1 {
		t.Fatalf("the landing is named twice:\n%s", agent.elsewhereText)
	}
}

// A project with nobody else on it is a project with nothing to say, and the
// block must not appear at all — not as a heading, not as "nothing new".
func TestAProjectWithNoOtherWindowGetsNoBlock(t *testing.T) {
	bucket := t.TempDir()
	mine := filepath.Join(bucket, "mine")
	if err := os.MkdirAll(mine, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	agent := &Agent{config: Config{Place: Place{Dir: mine}}}
	agent.messages = []ai.Message{textMessage("system", "base")}
	agent.system = "base"

	agent.refreshElsewhere()
	if agent.elsewhereText != "" {
		t.Fatalf("a quiet project produced %q, want silence", agent.elsewhereText)
	}
	if got := messageContentText(agent.messages[0]); got != "base" {
		t.Fatalf("message[0] is %q, want the base prompt untouched", got)
	}
}

// A TASK NODE IS NEVER TOLD. Its brief is its whole world, and a running account
// of the project's other windows is exactly the context the contract took away.
func TestATaskNodeIsNeverToldAboutOtherWindows(t *testing.T) {
	bucket := t.TempDir()
	mine := filepath.Join(bucket, "mine")
	if err := os.MkdirAll(mine, 0o700); err != nil {
		t.Fatalf("session folder: %v", err)
	}
	writeWindow(t, bucket, "theirs", "docs pass", time.Second,
		PresenceTask{ID: "4", Title: "Sweep the call sites", State: string(TaskRunning)})

	node := &Agent{config: Config{Place: Place{Dir: mine}, InTask: true}}
	node.messages = []ai.Message{textMessage("system", "base")}
	node.system = "base"
	node.refreshElsewhere()
	if node.elsewhereText != "" {
		t.Fatalf("a task node was told %q", node.elsewhereText)
	}
	if !LastTold(mine).IsZero() {
		t.Fatal("a task node advanced the conversation's stamp")
	}
}

// ── THE TOOL ────────────────────────────────────────────────────────────────

func TestTheTasksToolShowsTheOtherWindowsRunningWork(t *testing.T) {
	started := time.Now().Add(-4 * time.Minute)
	rows := []ElsewhereTask{{
		SessionID: "theirs",
		Session:   "docs pass",
		Task: PresenceTask{
			ID: "4", Title: "Sweep the call sites", State: string(TaskRunning),
			StartedAt: started,
			Files:     []string{"internal/session/agent.go", "internal/session/task.go"},
		},
	}}
	out := taskElsewhereText(rows, "", time.Now())
	for _, want := range []string{
		"running in other aforge windows on this project:",
		"another window · Sweep the call sites · running · running for 4m",
		`  in the window called "docs pass"`,
		"  files so far: internal/session/agent.go, internal/session/task.go",
		"These have no id in this conversation:",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("the tool's answer is missing %q:\n%s", want, out)
		}
	}
	// IT CARRIES NO ID. Ids restart with every conversation, so a row leading
	// with one would invite `tasks id 4` and reach this project's task four.
	if strings.HasPrefix(strings.TrimPrefix(out, "running in other aforge windows on this project:\n"), "4 ·") {
		t.Fatalf("the row leads with another conversation's id:\n%s", out)
	}
	// The query narrows it, exactly as the search half is narrowed.
	if got := taskElsewhereText(rows, "parser", time.Now()); got != "" {
		t.Fatalf("a query nothing matches answered %q, want nothing", got)
	}
	if got := taskElsewhereText(rows, "sweep", time.Now()); !strings.Contains(got, "Sweep the call sites") {
		t.Fatalf("a query that matches answered %q", got)
	}
	if got := taskElsewhereText(nil, "", time.Now()); got != "" {
		t.Fatalf("no other window answered %q, want nothing", got)
	}
}

// A QUEUED NODE HAS NO CLOCK, so the row must not claim one.
func TestAQueuedRowInAnotherWindowCarriesNoAge(t *testing.T) {
	out := taskElsewhereText([]ElsewhereTask{{
		SessionID: "theirs",
		Task:      PresenceTask{ID: "1", Title: "Port the parser", State: string(TaskQueued)},
	}}, "", time.Now())
	if strings.Contains(out, "running for") {
		t.Fatalf("a queued row claims an age:\n%s", out)
	}
	if !strings.Contains(out, "another window · Port the parser · queued") {
		t.Fatalf("the queued row reads wrong:\n%s", out)
	}
	// An unnamed window contributes no clause rather than a line of hex.
	if strings.Contains(out, "in the window called") {
		t.Fatalf("a window nothing named was given a name:\n%s", out)
	}
}

// deltaLandedRow is one FINISHED row of the project's index, as another window wrote
// it. It is [indexRow] with the two facts the delta reads: when it ended, and
// what it wrote.
func deltaLandedRow(id, session, title string, ended time.Time) TaskIndexEntry {
	row := indexRow(id, session, title, string(TaskDone))
	row.EndedAt = ended
	row.Outcome = "Added the guard and the regression test; the parser suite passes."
	row.Files = []string{"internal/reconciler/state.go"}
	row.FilesChanged = 3
	return row
}
