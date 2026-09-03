package main

// seed_room.go is the WORK ONE CONVERSATION HAS OUT: the task graph it
// checkpointed, the journal each node wrote, and the background jobs it started.
//
// WHY IT IS HERE AT ALL. Three of v3's surfaces had no frame in the polish
// audit, because nothing in this fixture put anything on them: the jobs section
// in the home column, a job's own page, and a task ROOM. Each of them is drawn
// from a conversation's own record rather than from the project index the rest
// of this program writes, so a demo home with twelve conversations and ten
// pieces of landed work still opened every one of them empty.
//
// ── WHAT THE PRODUCT CAN AND CANNOT KEEP ────────────────────────────────────
//
// EXECUTION DOES NOT SURVIVE THE PROCESS, AND THAT IS A LAW RATHER THAN A GAP.
// A background job is a child of the aforge that forked it and dies with it
// (internal/session's jobrow.go); a task node names a worker in a working copy,
// and the moment the process ends there is nobody in it. So the two files that
// keep this work — the session folder's tasks.json — are read back with that
// applied: a node the checkpoint calls RUNNING comes back queued and paused
// (task_store.go's [interrupt]), and a job's row comes back STOPPED
// ([runRowNotice]).
//
// THEREFORE THIS SEEDS NO RUNNING JOB AND NO RUNNING NODE, because there is no
// such record to write. A file claiming either would be a fixture faking a shape
// the product does not produce, and every frame captured on it would be a frame
// of something that cannot happen. What it seeds instead is every state that IS
// keepable, including the two the audit most wanted and the fixture had none of:
// work parked on a person ([session.TaskUnverified] — "needs your look") and
// work waiting behind it.
//
// AND NOTHING HERE MAY RE-ENTER THE FRONTIER. A resumed session runs its queued
// work (task_store.go's [Agent.recoverTasks] turns the frontier at the end of
// recovery), so a fixture that seeded a bare queued node would spend real money
// on invented work the first time somebody opened the demo. Every queued node
// below waits on the unverified one, directly or through another queued node,
// and an unverified prerequisite parks its dependants until a PERSON resolves it
// (task_run.go's [TaskGraph.readinessLocked]). That is what makes this fixture
// safe to open, and it is checked by the test beside this program.
//
// ── THE ONE SHAPE THIS FILE SPELLS FOR ITSELF ───────────────────────────────
//
// The checkpoint's records are internal/session's `taskRecord`, `runRecord` and
// `taskDocument`, and all three are unexported: there is no seam for "write me a
// graph that already happened", exactly as there is none for a conversation that
// already happened (seed_talk.go's [journalLine] states the same bargain). So
// the fields are spelled here with their own json tags — and every ENUM in them
// is asked for by name from internal/session, so a renamed state or kind breaks
// this build rather than this fixture. The test beside this program builds a
// real [session.Agent] on the seeded folder and reads the whole graph back off
// its own roster lane, which is the reader the surface uses.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// roomTalkTitle is the conversation the graph and the jobs hang off. It is
// spelled once and joined by title, exactly as the project index's rows are:
// a graph written into a folder no conversation owns is a graph nothing opens.
const roomTalkTitle = "Sweeping the Frame Budget"

// The names inside a session folder that this program has to write into. They
// are internal/session's placeTasks, placeNodeJournals, placeLogs and the job
// registry's own dropping kind, none of which are exported — and every one of
// them is proved by the test, which asks the engine for the same paths.
const (
	roomTasksFile   = "tasks.json"
	roomJournalDir  = "tasks"
	roomLogsDir     = "logs"
	roomJobsLogsDir = "jobs"
)

// taskDocumentType and taskFileVersion are the file's own tag and version. A
// document carrying any other pair is IGNORED outright rather than migrated
// (task_store.go's [decodeTasks]), so getting these wrong is a demo home whose
// task column is silently empty.
const (
	roomDocumentType = "tasks"
	roomDocumentSeq  = 12
	roomFileVersion  = 1
	roomJournalStamp = "20060102-150405.000000"
	roomJobLogHandle = "job "
	roomJobLogSep    = " · log "
)

// ── the file, as this program writes it ─────────────────────────────────────

// roomDocument is internal/session's `taskDocument`: a type tag, a version, the
// id counter, the nodes in admission order, and the rows that are not nodes.
type roomDocument struct {
	Type    string           `json:"type"`
	Version int              `json:"version"`
	Seq     uint64           `json:"seq"`
	Nodes   []roomNodeRecord `json:"nodes"`
	Runs    []roomRunRecord  `json:"runs,omitempty"`
}

// roomNodeRecord is one node as it survives the process — the subset of
// internal/session's `taskRecord` that a resumed graph and the surfaces above it
// actually read. Title, Brief and Acceptance are not optional: the decoder
// refuses a whole document over any node missing one.
type roomNodeRecord struct {
	ID         uint64            `json:"id"`
	Title      string            `json:"title"`
	Summary    string            `json:"summary,omitempty"`
	Brief      string            `json:"brief"`
	Acceptance string            `json:"acceptance"`
	DependsOn  []uint64          `json:"depends_on,omitempty"`
	Parent     uint64            `json:"parent,omitempty"`
	Depth      int               `json:"depth,omitempty"`
	State      session.TaskState `json:"state"`
	Report     string            `json:"report,omitempty"`
	Changed    []string          `json:"changed,omitempty"`
	Merge      string            `json:"merge,omitempty"`
	Journal    string            `json:"journal,omitempty"`
	Model      string            `json:"model,omitempty"`
	ElapsedMS  int64             `json:"elapsed_ms,omitempty"`
	CostUSD    float64           `json:"costUsd,omitempty"`
	Input      int               `json:"input,omitempty"`
	Output     int               `json:"output,omitempty"`
	Noted      bool              `json:"noted,omitempty"`
	Kind       session.TaskKind  `json:"kind,omitempty"`
}

// roomRunRecord is one row that is not a node: here, always a background job.
// The log's path travels inside Report, because that sentence is the storage
// format a job's row has always had and [session.JobNotice] is cut back out of
// it on the way to a surface (jobnotice.go's `jobNoticeFromRow`).
type roomRunRecord struct {
	ID        uint64            `json:"id"`
	Title     string            `json:"title,omitempty"`
	Kind      session.TaskKind  `json:"kind,omitempty"`
	State     session.TaskState `json:"state"`
	Stopped   bool              `json:"stopped,omitempty"`
	Report    string            `json:"report,omitempty"`
	ElapsedMS int64             `json:"elapsed_ms,omitempty"`
}

// ── what the demo's one graph holds ─────────────────────────────────────────

// demoNode is one piece of work in that graph, with the transcript it wrote.
type demoNode struct {
	record roomNodeRecord
	// journal is the node's own transcript, replayed by its room. A node with
	// none — one that never started — is given no file and no path, which is what
	// the room correctly draws as an empty page.
	journal []journalLine
}

// The long title is 101 cells and it is deliberate: every name in this fixture
// used to be comfortably short, so nothing on the demo could show what a list
// does when a row does not fit — which is the whole subject of the polish wave
// (internal/tui3's rowfit.go states the law it is judged against).
const roomLongTitle = "Cut every list on the task surface over to the shared row fitter so a name is never cut to nine cells"

// demoGraph is the family, in admission order. The shape is the one a person
// actually watches: a plan at the top, finished pieces under it, one piece that
// nobody could sign off, and the work parked behind that piece.
//
// THE STATES ARE CHOSEN SO THAT NOTHING STARTS. See the header: node 3 is
// unverified, and 4 and 6 wait on it through a chain, so a resumed frontier
// finds nothing ready and spends nothing.
var demoGraph = []demoNode{
	{
		record: roomNodeRecord{
			ID: 1, Depth: 1, State: session.TaskDone,
			Title:      "Measure the frame budget in every package that draws a row",
			Summary:    "A reading per package, and the order the four over the cap should be taken in.",
			Brief:      "Measure how long a frame costs in each package that draws a row, and write down which are over the cap in SIZE-BUDGET.",
			Acceptance: "A table of every package with a measured figure, and a named order for the ones over the cap.",
			Report:     "Eleven packages measured. Four are over the cap; the plan takes the tab bar first because everything else reads its counts.",
			Changed:    []string{"docs/design/polish/budget.md"},
			Merge:      "merged", Model: "anthropic/claude-sonnet-4",
			ElapsedMS: 4 * 60 * 1000, CostUSD: 0.21, Input: 24_100, Output: 3_900,
			Noted: true,
		},
		journal: roomPlanJournal,
	},
	{
		record: roomNodeRecord{
			ID: 2, Parent: 1, Depth: 2, DependsOn: []uint64{1}, State: session.TaskDone,
			Title:      "Move the tab bar onto one reading of the world",
			Brief:      "The bar and the list take two readings a few hundred milliseconds apart. Give them one.",
			Acceptance: "One reading, shared, and the two counts cannot disagree in a test that moves the world between them.",
			Report:     "One reading, shared by the bar and the list. The test moves the world between the two draws and both counts follow it.",
			Changed:    []string{"internal/tui3/home.go", "internal/tui3/homebands.go", "internal/session/world.go"},
			Merge:      "merged", Model: "anthropic/claude-sonnet-4",
			ElapsedMS: 7 * 60 * 1000, CostUSD: 0.34, Input: 41_800, Output: 6_200,
			Noted: true,
		},
		journal: roomTabBarJournal,
	},
	{
		record: roomNodeRecord{
			ID: 3, Parent: 1, Depth: 2, DependsOn: []uint64{1}, State: session.TaskUnverified,
			Title:      roomLongTitle,
			Brief:      "Four lists on the task surface do their own width arithmetic. Put all four on rowfit's plan so the identity is whole before any fact is spelled.",
			Acceptance: "Every one of the four goes through rowHalves, and a ninety-cell name is still readable at sixty columns.",
			Report:     "The four lists are on the fitter and the frames read right at 160 and 120. Nothing here could prove the sixty-cell case — the only terminal that narrow is yours — so it wants your eye before it lands.",
			Changed: []string{
				"internal/tui3/tasksplace.go", "internal/tui3/jobsview.go",
				"internal/tui3/deliverables.go", "internal/tui3/taskrecord.go",
			},
			Merge: "merged", Model: "anthropic/claude-opus-4.1",
			ElapsedMS: 12 * 60 * 1000, CostUSD: 0.52, Input: 68_400, Output: 9_100,
			Noted: true,
		},
		journal: roomFitterJournal,
	},
	{
		record: roomNodeRecord{
			ID: 4, Parent: 3, Depth: 3, DependsOn: []uint64{3}, State: session.TaskQueued,
			Title:      "Fold the settled work on the task page",
			Brief:      "Once the lists are on the fitter, the task page folds everything that has settled and keeps the live frontier open.",
			Acceptance: "A page of thirty settled rows opens as one chip, and the running rows are never folded.",
		},
	},
	{
		record: roomNodeRecord{
			ID: 5, Parent: 1, Depth: 2, DependsOn: []uint64{1}, State: session.TaskFailed,
			Title:      "Rebuild the frame budget report",
			Brief:      "Regenerate the report the plan measured against, so the numbers in it are this week's.",
			Acceptance: "docs/design/polish/budget.md carries a figure for every package, dated today.",
			Report:     "The sweep it re-runs needs a build that is not on this machine, so the report could not be regenerated. Nothing was written.",
			Merge:      "aborted", Model: "anthropic/claude-sonnet-4",
			ElapsedMS: 60 * 1000, CostUSD: 0.04, Input: 5_400, Output: 700,
			Noted: true,
		},
		journal: roomReportJournal,
	},
	{
		record: roomNodeRecord{
			ID: 6, Parent: 4, Depth: 4, DependsOn: []uint64{4}, State: session.TaskQueued,
			Title:      "Write the change entry and open the pull request against dev",
			Brief:      "One change entry under docs/changes/unreleased/, then a pull request against dev naming every file the family touched.",
			Acceptance: "A change entry exists and the pull request is open against dev, never main.",
		},
	},
}

// ── the background jobs ─────────────────────────────────────────────────────

// demoJob is one background job as the conversation kept it, with the log it
// spooled. A JOB'S LOG IS ITS WHOLE RECORD: it has no transcript, no branch and
// no report, so when it is over the file is the only thing left to go and look
// at, and the job page reads it back off the disk.
type demoJob struct {
	// row is the id the roster drew it under, and job is the id a PERSON says
	// out loud — `jobs kill 3`. They are two different numbers on purpose: the
	// row's comes from the graph's sequence so it cannot collide with a node's,
	// and the job's restarts at one in every window (jobrow.go).
	row, job uint64
	name     string
	state    session.TaskState
	stopped  bool
	elapsed  time.Duration
	log      string
}

var demoJobs = []demoJob{
	{
		// The one that was still going when aforge closed. It comes back STOPPED
		// and not running, because a process cannot outlive the program that
		// forked it — and its log is long, so the page has something to scroll.
		row: 7, job: 1, name: "npm run dev", state: session.TaskRunning,
		elapsed: 51*time.Minute + 12*time.Second, log: roomDevServerLog(),
	},
	{
		row: 8, job: 2, name: "go test ./internal/tui3/", state: session.TaskDone,
		elapsed: 8*time.Minute + 12*time.Second, log: roomTestLog(),
	},
	{
		// The one that failed and said why, which is the whole reason a person
		// opens a job page at all.
		row: 9, job: 3, name: "make check", state: session.TaskFailed,
		elapsed: 49 * time.Second, log: roomCheckLog(),
	},
}

// ── writing it down ─────────────────────────────────────────────────────────

// writeTaskGraphs writes the graph, its journals, and the jobs with their logs
// into the conversation that ran them, and answers how many of each it wrote.
//
// A conversation this program did not seed is a hard error rather than a skip:
// the whole file is joined by title, and a silent miss is a demo home whose task
// column is empty for a reason nobody can see.
func writeTaskGraphs(projects map[string]*demoProject, ids map[string]string, now time.Time) (nodes, jobs int, err error) {
	id, ok := ids[roomTalkTitle]
	if !ok {
		return 0, 0, fmt.Errorf("the work graph names no conversation %q", roomTalkTitle)
	}
	project, ok := projects[firstProjectName]
	if !ok {
		return 0, 0, fmt.Errorf("the work graph names no project %q", firstProjectName)
	}
	dir := filepath.Join(project.bucket, id)

	document := roomDocument{Type: roomDocumentType, Version: roomFileVersion, Seq: roomDocumentSeq}
	for index, node := range demoGraph {
		record := node.record
		if len(node.journal) > 0 {
			// The name is minted the way the engine mints it — a microsecond
			// stamp and the node's id — and it is WRITTEN ONTO THE RECORD,
			// because the stamp cannot be recomputed later and a room whose node
			// forgot its path opens on an empty page with the transcript sitting
			// beside it (task_store.go's taskRecord.Journal).
			stamp := now.Add(-time.Duration(len(demoGraph)-index) * 3 * time.Minute)
			path := filepath.Join(dir, roomJournalDir,
				fmt.Sprintf("%s_%d.jsonl", stamp.Format(roomJournalStamp), record.ID))
			if err := writeJournal(path, node.journal, record.Title, project.dir, stamp); err != nil {
				return nodes, jobs, err
			}
			record.Journal = path
		}
		document.Nodes = append(document.Nodes, record)
		nodes++
	}

	logs := filepath.Join(dir, roomLogsDir, roomJobsLogsDir)
	for _, job := range demoJobs {
		path := filepath.Join(logs, fmt.Sprintf("%d.log", job.job))
		if err := os.MkdirAll(logs, 0o700); err != nil {
			return nodes, jobs, fmt.Errorf("make %s: %w", logs, err)
		}
		if err := os.WriteFile(path, []byte(job.log), 0o600); err != nil {
			return nodes, jobs, fmt.Errorf("write %s: %w", path, err)
		}
		document.Runs = append(document.Runs, roomRunRecord{
			ID: job.row, Title: job.name, Kind: session.TaskKindJob,
			State: job.state, Stopped: job.stopped,
			Report:    fmt.Sprintf("%s%d%s%s", roomJobLogHandle, job.job, roomJobLogSep, path),
			ElapsedMS: job.elapsed.Milliseconds(),
		})
		jobs++
	}

	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nodes, jobs, fmt.Errorf("write the work graph of %q: %w", roomTalkTitle, err)
	}
	path := filepath.Join(dir, roomTasksFile)
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		return nodes, jobs, fmt.Errorf("write %s: %w", path, err)
	}
	return nodes, jobs, nil
}

// writeJournal writes one node's own transcript: the header the reader needs to
// know what version wrote the file, then the lines as they happened.
//
// The stamps are spread backwards from `at` a minute at a time, which is what
// makes a room read as something that took a while rather than as a page that
// arrived all at once.
func writeJournal(path string, lines []journalLine, title, workspace string, at time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(path), err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer file.Close()
	out := make([]journalLine, 0, len(lines)+1)
	out = append(out, journalLine{
		Type: "session", Version: 1, ID: session.NewSessionID(), Cwd: workspace,
		Model: "anthropic/claude-sonnet-4",
	})
	out = append(out, lines...)
	for index := range out {
		out[index].Timestamp = at.Add(time.Duration(index) * time.Minute).Format(time.RFC3339Nano)
		raw, err := json.Marshal(out[index])
		if err != nil {
			return fmt.Errorf("write %s (%s): %w", path, title, err)
		}
		if _, err := file.Write(append(raw, '\n')); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return file.Close()
}

// call is one tool call on an assistant line, and answer is the result that
// came back for it. They are a pair and they are spelled together, because the
// id is the only thing that joins the two halves — a result whose call id names
// nothing is a row the room draws as still running, forever
// ([session.DisplayEntry.Answered]).
func call(id, name string, arguments map[string]any) ai.ToolCall {
	raw, _ := json.Marshal(arguments)
	return ai.ToolCall{ID: id, Type: "function", Function: ai.ToolCallFunction{
		Name: name, Arguments: string(raw),
	}}
}

func answer(id, output string) journalLine {
	return journalLine{Type: "message", Role: "tool", ToolCallID: id, Content: output}
}

// ── the four node transcripts ───────────────────────────────────────────────

// A node's journal opens with the brief it was handed — a user-role line,
// because that is the only role a model can be told something in — and then runs
// as an ordinary loop: the worker says what it is about to do, calls tools, and
// finishes with its report.

var roomPlanJournal = []journalLine{
	{Type: "message", Role: "user", Content: "Measure how long a frame costs in each package that draws a row, and write down which are over the cap in SIZE-BUDGET."},
	{Type: "message", Role: "assistant", Content: "Starting from the budget file, so the cap I measure against is the one that is checked in.",
		ToolCalls: []ai.ToolCall{call("c1", "read", map[string]any{"path": "SIZE-BUDGET"})}},
	answer("c1", "tui3 12ms\ntui2 6ms\nsession 4ms\nstanding 3ms\n"),
	{Type: "message", Role: "assistant", Content: "Now the measurement itself, once per package.",
		ToolCalls: []ai.ToolCall{call("c2", "bash", map[string]any{"command": "go test -run TestFrameBudget -bench . ./internal/... | tee /tmp/budget.txt"})}},
	answer("c2", "internal/tui3      14.2ms/frame  OVER\ninternal/tui2       5.9ms/frame\ninternal/session    4.1ms/frame\ninternal/standing   3.0ms/frame\ninternal/head      11.8ms/frame  OVER\ninternal/resident  10.4ms/frame  OVER\ninternal/remote     9.7ms/frame  OVER\nok\n"),
	{Type: "message", Role: "assistant", Content: "Four are over. Writing the order down: the tab bar first, because every other count on the surface is read from it.",
		ToolCalls: []ai.ToolCall{call("c3", "write", map[string]any{"path": "docs/design/polish/budget.md"})}},
	answer("c3", "wrote docs/design/polish/budget.md (48 lines)"),
	{Type: "message", Role: "assistant", Content: "Eleven packages measured. Four are over the cap; the plan takes the tab bar first because everything else reads its counts."},
}

var roomTabBarJournal = []journalLine{
	{Type: "message", Role: "user", Content: "The bar and the list take two readings a few hundred milliseconds apart. Give them one."},
	{Type: "message", Role: "assistant", Content: "Both readings come from world.go. If the bar takes the list's, the two cannot drift.",
		ToolCalls: []ai.ToolCall{call("t1", "read", map[string]any{"path": "internal/session/world.go"})}},
	answer("t1", "func ReadWorld(root string) World { … }\n"),
	{Type: "message", Role: "assistant", Content: "Threading one reading through the bar now.",
		ToolCalls: []ai.ToolCall{call("t2", "write", map[string]any{"path": "internal/tui3/homebands.go"})}},
	answer("t2", "wrote internal/tui3/homebands.go (3 hunks)"),
	{Type: "message", Role: "assistant", Content: "And a test that moves the world between the two draws, so a second reading would show up as a failure rather than as a flicker.",
		ToolCalls: []ai.ToolCall{call("t3", "bash", map[string]any{"command": "go test ./internal/tui3/ -run TestTheBarAndTheListCount"})}},
	answer("t3", "ok  \tgithub.com/Agent-Field/aforge-v2/internal/tui3\t2.104s\n"),
	{Type: "message", Role: "assistant", Content: "One reading, shared by the bar and the list. The test moves the world between the two draws and both counts follow it."},
}

// The fitter node's journal is the LONGEST on purpose: it is the room a polish
// wave opens, and a room with four lines in it says nothing about what a room
// does with a screenful.
var roomFitterJournal = []journalLine{
	{Type: "message", Role: "user", Content: "Four lists on the task surface do their own width arithmetic. Put all four on rowfit's plan so the identity is whole before any fact is spelled."},
	{Type: "message", Role: "assistant",
		Reasoning: "The law is in rowfit.go: the identity goes whole first, then the facts in a ranked prefix. Four files re-implement it. Take them one at a time, and check each against the sixty-cell case.",
		Content:   "rowfit.go states the law; I will read it first so the four rewrites all mean the same thing.",
		ToolCalls: []ai.ToolCall{call("f1", "read", map[string]any{"path": "internal/tui3/rowfit.go"})}},
	answer("f1", "// THE LAW: the identity is drawn WHOLE before any fact is spelled, and the\n// facts that follow are a ranked prefix of what is left.\nfunc rowHalves(plan rowPlan, width int) (string, string) { … }\n"),
	{Type: "message", Role: "assistant", Content: "And the worst of the four, which cuts the name to eight cells and gives every fact its full spelling.",
		ToolCalls: []ai.ToolCall{call("f2", "read", map[string]any{"path": "internal/tui3/tasksplace.go"})}},
	answer("f2", "\tif tasksFixedWidth(parts)+lead+8 > width { … }\n\tlabel := clip(tasksLabel(entry), width-lead-fixed)\n"),
	{Type: "message", Role: "assistant", Content: "That is the law inverted exactly. Rewriting it as a rowPlan.",
		ToolCalls: []ai.ToolCall{call("f3", "write", map[string]any{"path": "internal/tui3/tasksplace.go"})}},
	answer("f3", "wrote internal/tui3/tasksplace.go (2 hunks)"),
	{Type: "message", Role: "assistant", Content: "Same shape for the jobs column, the made-for-you note and the task card.",
		ToolCalls: []ai.ToolCall{
			call("f4", "write", map[string]any{"path": "internal/tui3/jobsview.go"}),
			call("f5", "write", map[string]any{"path": "internal/tui3/deliverables.go"}),
			call("f6", "write", map[string]any{"path": "internal/tui3/taskrecord.go"}),
		}},
	answer("f4", "wrote internal/tui3/jobsview.go (1 hunk)"),
	answer("f5", "wrote internal/tui3/deliverables.go (1 hunk)"),
	answer("f6", "wrote internal/tui3/taskrecord.go (1 hunk)"),
	{Type: "message", Role: "assistant", Content: "Running the surface's own suite over the four.",
		ToolCalls: []ai.ToolCall{call("f7", "bash", map[string]any{"command": "go test -timeout 15m ./internal/tui3/"})}},
	answer("f7", "ok  \tgithub.com/Agent-Field/aforge-v2/internal/tui3\t151.402s\n"),
	{Type: "message", Role: "assistant", Content: "The four lists are on the fitter and the frames read right at 160 and 120. Nothing here could prove the sixty-cell case — the only terminal that narrow is yours — so it wants your eye before it lands."},
}

var roomReportJournal = []journalLine{
	{Type: "message", Role: "user", Content: "Regenerate the report the plan measured against, so the numbers in it are this week's."},
	{Type: "message", Role: "assistant", Content: "The report is generated by the sweep, so I will run that.",
		ToolCalls: []ai.ToolCall{call("r1", "bash", map[string]any{"command": "make frame-budget-report"})}},
	answer("r1", "make: *** No rule to make target 'frame-budget-report'.  Stop.\nexit status 2\n"),
	{Type: "message", Role: "assistant", Content: "The sweep it re-runs needs a build that is not on this machine, so the report could not be regenerated. Nothing was written."},
}

// ── the three job logs ──────────────────────────────────────────────────────

// roomDevServerLog is long ON PURPOSE. A job page draws the tail of a log and
// lets a person scroll it, and a fixture whose longest log is four lines cannot
// show whether the scroll works, where it starts, or what it does at sixty
// columns.
func roomDevServerLog() string {
	var out strings.Builder
	out.WriteString("> aforge-web@0.1.0 dev\n> vite --host 0.0.0.0 --port 5173\n\n")
	out.WriteString("  VITE v5.4.2  ready in 412 ms\n\n")
	out.WriteString("  ➜  Local:   http://localhost:5173/\n")
	out.WriteString("  ➜  Network: http://192.168.1.24:5173/\n\n")
	routes := []string{"/", "/home", "/tasks", "/tasks/3", "/jobs", "/jobs/1", "/spend", "/standing", "/memory", "/search"}
	for tick := 0; tick < 24; tick++ {
		for _, route := range routes {
			fmt.Fprintf(&out, "%02d:%02d:%02d [vite] GET %s 200 in %dms\n",
				9+(tick/12), (tick*7)%60, (tick*11)%60, route, 3+(tick*3)%40)
		}
		if tick%6 == 5 {
			fmt.Fprintf(&out, "%02d:%02d:%02d [vite] hmr update /src/pages/Tasks.tsx\n", 9+(tick/12), (tick*7)%60, (tick*13)%60)
		}
	}
	out.WriteString("09:58:04 [vite] page reload src/main.tsx\n")
	return out.String()
}

func roomTestLog() string {
	var out strings.Builder
	out.WriteString("=== RUN   TestTheBarAndTheListCountTheSameWorld\n--- PASS: TestTheBarAndTheListCountTheSameWorld (0.21s)\n")
	for _, name := range []string{
		"TestAnUnknownFigureDrawsNothing", "TestTheRowKeepsItsNameWhole",
		"TestTheTaskRoomFoldsSettledPhases", "TestAJobPageNamesItsLog",
		"TestTheFilterSaysWhatItMatched", "TestTheFoldSurvivesAResize",
	} {
		fmt.Fprintf(&out, "=== RUN   %s\n--- PASS: %s (%d.%02ds)\n", name, name, len(name)%9, len(name)%97)
	}
	out.WriteString("PASS\nok  \tgithub.com/Agent-Field/aforge-v2/internal/tui3\t492.118s\n")
	return out.String()
}

func roomCheckLog() string {
	return strings.Join([]string{
		"go vet ./...",
		"# github.com/Agent-Field/aforge-v2/internal/tui3",
		"internal/tui3/tasksplace.go:722:14: fmt.Sprintf format %d has arg label of wrong type string",
		"make: *** [Makefile:41: vet] Error 1",
		"exit status 1",
		"",
	}, "\n")
}
