package session

// THE TASK INDEX: WHAT THIS PROJECT'S WORK WAS, AFTER THE CONVERSATION THAT
// ASKED FOR IT IS GONE.
//
// task_store.go keeps ONE conversation's graph, beside that conversation's
// journal, and it is right to: a graph is a live thing with a frontier and a
// scheduler, and it belongs to the session that turns it. This file keeps the
// other half — the compact record of every task family and landed node this
// PROJECT has ever run, in one append-only file the whole directory shares.
//
// The two are not the same fact and neither can stand in for the other. Ask
// "what is running" and the answer is the graph. Ask "what did we do about the
// nil-map crash last Tuesday" and the graph cannot answer at all: it died with
// the window. That question is the whole reason this file exists, and it is
// asked by two people —
//
//   - THE PERSON, by typing "@" (internal/tui3's task mentions). They half
//     remember a title, they want the row, and what they get back is a pointer
//     into the prompt rather than the work itself.
//   - THE MODEL, by calling the `tasks` tool (tools_tasks.go), when the person
//     referred to earlier work and pointed at nothing.
//
// Both read the same rows through the same search, because a person and a model
// disagreeing about which task "the reconciler one" was is the defect the index
// exists to prevent.
//
// ── THREE RULES ──
//
//   - JSONL, APPEND-ONLY, ONE ROW PER RUN ROOT AND LANDED NODE. Not a JSON
//     document like the checkpoint: the checkpoint is rewritten whole after
//     every transition and is one conversation's, while this is written once
//     per indexed row, forever, by every window open on the directory. An
//     append is the one write two processes can make to the same file without
//     a coordinator.
//   - A BAD LINE IS SKIPPED, NEVER FATAL. Two processes appending can, in the
//     limit, interleave a large row; a half-written line then costs exactly one
//     task's row. Refusing to complete an "@" because of it would cost the
//     feature.
//   - IT IS AN INDEX, NOT AN ARCHIVE. Every row is small and carries URIs
//     rather than content: the transcript is a file on disk, the artifact is a
//     worktree or a branch, and both are things the model already has hands to
//     read. A row that inlined an outcome in full would be this build's own
//     16KB-transport defect, one layer down.
//
// ── WHERE IT LIVES ──
//
// In the PROJECT BUCKET — ~/.aforge/v3/projects/<workspace>/ — which is the
// directory holding this workspace's session folders (place.go, Decision 26).
// The scope is the project and not the conversation: every window open on the
// repository appends to one file, which is what makes "what work has this
// project had done" a question with one answer.
//
// The path is DERIVED rather than resolved a second way, exactly as
// [taskCheckpointPath] is: from the session's [Place] when it has one — the
// parent of the folder is the bucket — and from the session file's own
// directory for the legacy flat layout, where the workspace directory WAS the
// project scope. Deriving it from the workspace instead would be a second
// definition of "this project" to disagree with the first.

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// taskIndexName is the file, in the session directory. One name, shared by
// every window open on the workspace.
const taskIndexName = "tasks.jsonl"

// The bounds one row is held to. They are what keeps an index of a thousand
// tasks a file a person can open and a list a model can afford to read.
const (
	// taskOutcomeLimit caps the outcome sentence. Two lines of prose; the whole
	// report is in the transcript the row points at.
	taskOutcomeLimit = 240
	// taskLabelLimit caps the display label — a title that fits a drop-up row
	// beside a glyph and an age.
	taskLabelLimit = 56
	// taskIndexRows caps ONE read of the file. A project that has run more tasks
	// than this keeps the newest, which is what every question asked of this
	// index is about.
	taskIndexRows = 2000
	// taskSearchLimit is the default number of rows a search answers with, and
	// taskSearchCeiling the most any caller may ask for.
	taskSearchLimit   = 10
	taskSearchCeiling = 50
	// taskFilesLimit caps how many PATHS one row — or one live claim — names.
	// Two hundred is past what any node this build runs has ever written, and it
	// is the point where a list of citations would become a manifest, which is
	// the thing this index refuses to be (see the third rule in the header).
	// Past it the TAIL IS DROPPED AND THE COUNT IS KEPT WHOLE, so a row that
	// names two hundred files and says it wrote four hundred is telling the
	// truth about both.
	taskFilesLimit = 200
)

// TaskIndexEntry is one node as the project remembers it.
//
// It is a SEPARATE type from [TaskNotice] and from taskRecord, and deliberately:
// a notice is what is happening now, a record is a node's resumable state, and
// this is a citation — the smallest thing that lets a person or a model find
// the work again. The fields that are here and nowhere else are the two URIs
// and the cost, because those are the three questions asked about work that is
// already over.
type TaskIndexEntry struct {
	// ID is the node's id inside the session that ran it, decimal. It is NOT
	// unique across the file — ids restart with every conversation — which is
	// why SessionID sits beside it and why the pair is what identifies a row.
	// It is a string because it is a handle a person types and a model quotes,
	// not a number anything does arithmetic on.
	ID string `json:"id"`
	// Parent is the id of the family root inside this session, or empty for a
	// root. It is additive: rows written before families entered the index
	// decode as roots, which is exactly what they were.
	Parent string `json:"parent,omitempty"`
	// Name is the slug an "@" mention resolves: the title, kebab-cased
	// ([TaskSlug]). Two tasks may share one — a project that fixed the same
	// crash twice — and the newest wins, because "the nil-map task" said out
	// loud means the last one.
	Name string `json:"name"`
	// Label is the title as a ROW shows it: cut to [taskLabelLimit] once, here,
	// so that every surface drawing this index draws the same words. Title is
	// the title as it was groomed, uncut, for the pointer block and the search.
	Label string `json:"label"`
	Title string `json:"title"`
	// Kind is what SORT of work this row was: an adaptive run, a saved shape
	// running, a saved shape being made — or empty for ordinary work, which is
	// most of the file ([TaskKind], and [TaskKindWord] for the word a person
	// reads).
	//
	// IT IS ADDITIVE AND ABSENCE IS ORDINARY. Rows written before this field
	// existed decode with none, which is exactly what almost all of them were;
	// unlike [TaskIndexEntry.Files], where absence is unknown, there is nothing
	// here for a reader to be careful about — a blank kind and a plain task are
	// drawn the same way on purpose.
	//
	// IT IS HERE BECAUSE THE KNOWLEDGE WAS BEING THROWN AWAY ON THE WAY TO THE
	// FILE. A node has carried its kind since it was admitted ([TaskNode.kind],
	// from [taskSpec.kind]) and an adaptive run has always known it was one, and
	// a surface that wanted to say "adaptive" on a row could only string-match
	// the title or sniff the shape of the transcript's path — both of which are
	// guesses about a fact the engine held.
	//
	// A BACKGROUND JOB NEVER REACHES THIS FILE, and the constant existing does
	// not change that: jobrow.go's own law is that a job publishes a roster row
	// and no project index row, no landing note and no card. [TaskKindJob] is
	// spelled in [TaskKindWord] so that a live row merged in from a graph reads
	// the same way as a landed one, not because the file holds any.
	Kind TaskKind `json:"kind,omitempty"`
	// Status is the node's final state — "done", "failed", "unverified" — or its
	// live one ("running", "queued") on a row merged in from a graph that is
	// still turning.
	Status string `json:"status"`
	// Outcome is the first sentence of the node's report: what it did, or what
	// stopped it. Empty for work that has not landed.
	Outcome string `json:"outcome"`
	// FilesChanged is how many files the node wrote, and it is the figure every
	// surface draws. It is also the HONEST TOTAL: Files beside it is capped at
	// [taskFilesLimit], so a node that wrote more has a count larger than its
	// list, and the count is what says so.
	FilesChanged int `json:"filesChanged"`
	// Files are those same paths, repo-relative and slash-spelled, in the order
	// the node first wrote them, capped at [taskFilesLimit].
	//
	// THE COUNT IS FOR READING AND THE LIST IS FOR ASKING. This row used to carry
	// the count alone, on the argument that the list was in the transcript — and
	// it is, but only as prose in a journal, which is no use to the question the
	// list is here for: "did anybody else land work in these files, and when".
	// Answering that off the transcripts would mean opening every one of them.
	//
	// BOTH ARE WRITTEN FROM ONE PLACE ([taskFileCitations]) so the count and the
	// list can never drift apart.
	//
	// IT IS ADDITIVE, AND ABSENCE IS UNKNOWN. Rows written before this field
	// existed decode with none, and none does NOT mean the node touched nothing:
	// a reader that cannot see a list must say it cannot see one rather than
	// invent an answer (the emptiness law), which is why [LandedTouching] answers
	// with two lists instead of one.
	Files []string `json:"files,omitempty"`
	// MaySplit is WHETHER THIS WORK WAS EVER ALLOWED TO HAND ITS PARTS OUT, and
	// which reader allowed it: "wide" for a model's own judgement of breadth,
	// "judged" for the sizing call at the typed door, "counted" for a brief that
	// named enough separate items on its own (task_divide.go's arming words).
	// ABSENT MEANS THE VERB WAS NEVER ON THE BELT.
	//
	// IT IS HERE BECAUSE THE ABSENCE OF PARTS IS THREE DIFFERENT FACTS. A row for
	// a task that ran alone can mean the worker was never given `divide_work`,
	// or had it and never reached for it, or asked and was told no — and until
	// this field existed the file said the same thing about all three, so anybody
	// reading the record to find out whether the road was working could only
	// count parts and guess. The reason word separates the first from the other
	// two, and separates a road nobody armed from a road nobody used.
	//
	// IT IS THE READING AND NOT THE OUTCOME. A task armed and never divided still
	// says so, because what this answers is what the task was ALLOWED to do; the
	// parts themselves are rows of their own, carrying this node's id as Parent.
	MaySplit string `json:"maySplit,omitempty"`
	// Cost is what the node spent, in dollars, or 0 when nobody could say.
	Cost float64 `json:"cost,omitempty"`
	// Model is what the node ran on, and empty when it simply took the
	// conversation's. It is here because the file that carries the COST is the
	// file that has to be able to answer "at what rate": the id lives on the
	// checkpoint and in the node journal's header too, and a row without it made
	// re-pricing a landed task a three-file join.
	Model string `json:"model,omitempty"`
	// RepairedOn is the model a REPAIR ROUND ran on, and it is here only when
	// that was not the model beside it: work the checker sent back is handed to
	// the careful tier (internal/session's repair_role.go), and this is the one
	// row in this file that says an escalation was bought.
	//
	// IT IS THE COMPANION TO Cost AND IT ANSWERS THE SAME QUESTION Model DOES,
	// one layer down. A node's bill is the sum of every agent it took — the
	// worker, each correction round, each check — so a row carrying one model and
	// one figure cannot say whether an expensive total was an expensive task or a
	// cheap task that needed rescuing, and those are different facts about a
	// crew's economics. Additive, and absent means the ladder floored: either
	// nothing was sent back, or the careful tier resolves to the model the work
	// was already on.
	RepairedOn string `json:"repairedOn,omitempty"`
	// Tokens is input plus output, as ONE sum. The index carries citations, and
	// the four-way split — with the cache share in it — lives in the journal
	// this row's TranscriptURI names; a row that spelled out all four would be
	// the thing this index refuses to be. Zero means nobody counted, never that
	// the work was free.
	Tokens int `json:"tokens,omitempty"`
	// DurationMS is how long it ran.
	DurationMS int64 `json:"durationMs,omitempty"`
	// EndedAt is when it landed, and it is zero for a row merged in live. Every
	// ordering in this file is on it (see [taskIndexAt]).
	EndedAt time.Time `json:"endedAt"`
	// SessionID is the conversation that ran it — the id in the journal's
	// header, which is also the directory a node's own transcript sits under.
	SessionID string `json:"sessionId"`
	// ArtifactURI is where the WORK is: the node's worktree while one is on
	// disk, else the branch it was kept on, else empty for a node whose changes
	// went straight into the person's tree.
	ArtifactURI string `json:"artifactUri,omitempty"`
	// TranscriptURI is where the STORY is: the node's own session journal, which
	// is a real session file the read tool can open (task_run.go's
	// taskJournalPath).
	TranscriptURI string `json:"transcriptUri,omitempty"`
	// Activity is what a RUNNING node is doing at the instant this row was
	// built, in one line: the call in flight and how long it has been in flight,
	// or the gap between calls with the step count beside it (task_live.go). It
	// is empty on every landed row.
	//
	// IT IS NEVER WRITTEN TO THE FILE. The index is what work CAME TO, and a row
	// on disk claiming a call in flight would be this project's record
	// remembering a present that ended seconds after it was recorded — which is
	// the one thing an append-only history must not do.
	Activity string `json:"-"`
}

// taskFileCitations is the pair a row carries about what a node wrote: the
// paths it names, capped at [taskFilesLimit], and the honest total beside them.
//
// ONE SOURCE OF TRUTH. The count is derived from the list here and in no other
// place, so no row can ever say it wrote nine files and then name ten. Where the
// cap bites, the tail goes and the count stays whole — a truncated list beside a
// truncated count would hide the truncation itself, and a reader would take a
// partial answer for a complete one.
func taskFileCitations(written []string) ([]string, int) {
	count := len(written)
	if count == 0 {
		return nil, 0
	}
	if count > taskFilesLimit {
		written = written[:taskFilesLimit]
	}
	return append([]string(nil), written...), count
}

// Live reports whether this row is a node that is still going.
func (e TaskIndexEntry) Live() bool {
	return e.Status == string(TaskRunning) || e.Status == string(TaskQueued)
}

// Duration is DurationMS as a duration.
func (e TaskIndexEntry) Duration() time.Duration {
	return time.Duration(e.DurationMS) * time.Millisecond
}

// taskIndexAt is the one time every ordering in this file sorts on: when the
// node landed, and NOW for a node that has not. A running task is the most
// recent thing there is — it is happening — and sorting it by a zero EndedAt
// would file this morning's live work behind last month's finished work.
func taskIndexAt(entry TaskIndexEntry, now time.Time) time.Time {
	if entry.EndedAt.IsZero() {
		return now
	}
	return entry.EndedAt
}

// TaskIndexPath is the project's index for one session file, or "" for a
// session with no file at all — a memory-only conversation has no project
// directory to keep a project's record in.
//
// A SESSION FOLDER'S INDEX IS ITS BUCKET'S. A journal called transcript.jsonl
// is a folder's (place.go), the folder is one conversation, and the project is
// the directory ABOVE it — so the index climbs one level rather than landing
// inside a single conversation, where every window would keep a private list of
// the same project's work. A legacy flat transcript keeps the index beside it,
// which for that layout is the same directory.
func TaskIndexPath(sessionFile string) string {
	sessionFile = strings.TrimSpace(sessionFile)
	if sessionFile == "" {
		return ""
	}
	directory := filepath.Dir(sessionFile)
	if directory == "" || directory == "." {
		return ""
	}
	if filepath.Base(sessionFile) == placeTranscript {
		bucket := filepath.Dir(directory)
		if bucket == "" || bucket == "." {
			return ""
		}
		return filepath.Join(bucket, taskIndexName)
	}
	return filepath.Join(directory, taskIndexName)
}

// taskIndexFile is where THIS session's project keeps its index: the bucket
// above the session folder when the session has a [Place], and the session
// file's own directory for the legacy flat layout the zero Place stands for.
func (c Config) taskIndexFile() string {
	if dir := strings.TrimSpace(c.Place.Dir); dir != "" {
		bucket := filepath.Dir(dir)
		if bucket == "" || bucket == "." {
			return ""
		}
		return filepath.Join(bucket, taskIndexName)
	}
	return TaskIndexPath(c.SessionFile)
}

// taskIndexMu serializes this process's appends. Two windows on the same
// project are two processes and are not serialized by it — they are serialized
// by O_APPEND, which is what makes an append-only file the right shape here —
// but two nodes of ONE session landing at the same instant are two goroutines,
// and they are.
var taskIndexMu sync.Mutex

// appendTaskIndex writes one row. Every failure is silence: the caller is a
// node that has just finished real work, and there is nothing it could usefully
// do with the news that a lookup file could not be written.
func appendTaskIndex(path string, entry TaskIndexEntry) {
	if strings.TrimSpace(path) == "" {
		return
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	taskIndexMu.Lock()
	defer taskIndexMu.Unlock()
	if directory := filepath.Dir(path); directory != "" && directory != "." {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return
		}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	// ONE write, so O_APPEND's atomic offset covers the whole row: a line
	// assembled by two writes is a line another process may split.
	_, _ = file.Write(append(line, '\n'))
}

// ReadTaskIndex reads the rows at path, NEWEST FIRST, and tolerates everything.
//
// A missing file is the ordinary case — a project that has never run a task —
// and answers nil. A line that does not parse, or that parses into a row with
// no title, is skipped: see this file's header for why that is a rule and not a
// defect.
func ReadTaskIndex(path string) []TaskIndexEntry {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	var rows []TaskIndexEntry
	scanner := bufio.NewScanner(file)
	// A row is small by construction, but an outcome plus two URIs plus a title
	// can pass the scanner's default line budget on a pathological title.
	scanner.Buffer(make([]byte, 0, 4<<10), 256<<10)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry TaskIndexEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if strings.TrimSpace(entry.Title) == "" {
			continue
		}
		rows = append(rows, entry)
	}
	// scanner.Err() is deliberately unread: a truncated tail is the same
	// tolerated case as an unparseable line, and the rows before it are good.
	sortTaskIndex(rows)
	rows = newestPerNode(rows)
	if len(rows) > taskIndexRows {
		rows = rows[:taskIndexRows]
	}
	return rows
}

// newestPerNode keeps ONE row per node: the last thing the file says about it.
//
// The file is append-only and a node can be written more than once — a run's
// root takes a row when it starts and another when it settles, a node that
// needed somebody's look takes a second row when they give it — and this is the
// half of that arrangement that makes the later row MEAN anything. Without it
// both rows are in every answer, and a reader that takes the first one it sees
// gets whichever the sort happened to put there: the drop-up drew "running"
// beside a run that had ended, because the row saying so was still in the list.
//
// IT IS AN INDEX, NOT AN ARCHIVE (see this file's header). The transitions a
// node went through are in its transcript; what the index is asked is what the
// work CAME TO, and that is one answer per node.
func newestPerNode(rows []TaskIndexEntry) []TaskIndexEntry {
	seen := make(map[string]bool, len(rows))
	kept := rows[:0]
	for _, row := range rows {
		id := strings.TrimSpace(row.ID)
		if id == "" {
			// A row with no id names no node, so nothing can replace it and it can
			// replace nothing. It is kept as it is rather than folded into a bucket
			// with every other id-less row.
			kept = append(kept, row)
			continue
		}
		key := row.SessionID + "\x00" + id
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, row)
	}
	return kept
}

// sortTaskIndex puts the rows in the order every reader wants them: newest
// first, and — for two rows that landed in the same instant, which is what a
// zero EndedAt on two live nodes looks like — the higher id first, so the order
// is stable rather than the order of a map.
func sortTaskIndex(rows []TaskIndexEntry) {
	now := time.Now()
	sort.SliceStable(rows, func(a, b int) bool {
		left, right := taskIndexAt(rows[a], now), taskIndexAt(rows[b], now)
		if !left.Equal(right) {
			return left.After(right)
		}
		return taskIDNumber(rows[a].ID) > taskIDNumber(rows[b].ID)
	})
}

// taskIDNumber reads a row's id back as a number, or 0 for one that is not.
func taskIDNumber(id string) uint64 {
	value, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

// ── the index, as this session sees it ──────────────────────────────────────

// TaskIndex is the project's task index with THIS conversation's live graph
// merged over it: every landed node the directory has ever recorded, plus the
// nodes running right now, which by definition are not in the file yet.
//
// THE MERGE IS HERE AND NOWHERE ELSE. Both readers — the "@" drop-up and the
// `tasks` tool — need running work in the list, and a surface that merged its
// own copy of the live registry would be a second answer to "what is running"
// with a different set of rules for keeping it fresh.
func (a *Agent) TaskIndex() []TaskIndexEntry {
	rows := ReadTaskIndex(a.config.taskIndexFile())
	live := a.liveTaskRows()
	if len(live) == 0 {
		return rows
	}
	// A live row REPLACES the file's row for the same node: a node that landed
	// and was recorded, and is somehow still in the graph, is described more
	// truthfully by the graph.
	seen := make(map[string]bool, len(live))
	for _, entry := range live {
		seen[entry.SessionID+"/"+entry.ID] = true
	}
	merged := make([]TaskIndexEntry, 0, len(rows)+len(live))
	merged = append(merged, live...)
	for _, entry := range rows {
		if seen[entry.SessionID+"/"+entry.ID] {
			continue
		}
		merged = append(merged, entry)
	}
	sortTaskIndex(merged)
	return merged
}

// liveTaskRows is this session's graph as index rows. It reads the graph
// WITHOUT building one: most conversations never groom a task, and a question
// about the index must not be the thing that gives them a scheduler.
func (a *Agent) liveTaskRows() []TaskIndexEntry {
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	rows := make([]TaskIndexEntry, 0, len(graph.order))
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil {
			continue
		}
		rows = append(rows, node.indexEntryLocked(session))
	}
	return rows
}

// recordTaskIndex writes one landed node into the project's index. It is called
// from the graph's report hook (task_run.go), which is the one place a node
// reaching a final state is a fact rather than a guess.
func (a *Agent) recordTaskIndex(node *TaskNode) {
	path := a.config.taskIndexFile()
	if path == "" || node == nil {
		return
	}
	a.mu.Lock()
	session := a.sessionID()
	a.mu.Unlock()

	node.graph.mu.Lock()
	entry := node.indexEntryLocked(session)
	node.graph.mu.Unlock()
	if entry.EndedAt.IsZero() {
		entry.EndedAt = time.Now()
	}
	a.recordTaskIndexEntry(entry)
}

// recordTaskIndexEntry is the one append door for every kind of task row. A
// regular task reaches it through [Agent.recordTaskIndex]; an adaptive run has
// no TaskNode, so its completion seam supplies the same citation directly.
func (a *Agent) recordTaskIndexEntry(entry TaskIndexEntry) {
	path := a.config.taskIndexFile()
	if path == "" || strings.TrimSpace(entry.Title) == "" {
		return
	}
	appendTaskIndex(path, entry)
}

// ── rows a process left in flight ───────────────────────────────────────────

// taskInterruptedOutcome is what a row says about work this session was doing
// when its process went away. It is not a finding about the work — nobody was
// there to make one — it is the plain fact that nothing finished it.
const taskInterruptedOutcome = "incomplete — aforge closed while this was still running"

// closeInflightTaskIndexRows settles the rows THIS session left saying
// "running" when its last process ended.
//
// A ROW THAT SAYS RUNNING IS A CLAIM ABOUT A PROCESS, and the only process that
// could still be making it is the one that wrote it. So opening the session
// again is the moment the claim becomes false, exactly as reopening the journal
// is the moment a checkpoint's running node becomes an interrupt
// (task_store.go's [interrupt], whose reasoning this is a second application
// of). The two are not the same file and neither can close the other's rows: a
// checkpoint holds the graph of one conversation, and this holds the project's
// record of everything every window ever ran.
//
// IT CLOSES OUR OWN ROWS AND NOBODY ELSE'S. The index is shared by every window
// open on the project, and another session's running row is another process's
// live work — one flock, one session folder, so a row carrying THIS session's id
// was written by a process that is gone. Rows the live graph still holds are
// left alone: a resumed node is described by the graph, and [Agent.TaskIndex]
// already draws the graph over the file for exactly that reason.
//
// The close is an APPEND, like every other write to this file, so nothing is
// rewritten and a crash during it costs at most one row.
func (a *Agent) closeInflightTaskIndexRows() {
	// A node's own agent shares its parent's project directory and has no
	// business closing the conversation's rows (the argument [Agent.recoverTasks]
	// makes about the checkpoint).
	if a.config.InTask {
		return
	}
	path := a.config.taskIndexFile()
	if path == "" {
		return
	}
	a.mu.Lock()
	session := strings.TrimSpace(a.sessionID())
	a.mu.Unlock()
	if session == "" {
		return
	}
	held := a.heldTaskIDs()
	now := time.Now()
	for _, row := range ReadTaskIndex(path) {
		if row.SessionID != session || !row.Live() || held[strings.TrimSpace(row.ID)] {
			continue
		}
		closed := row
		closed.Status = string(TaskFailed)
		closed.Outcome = taskInterruptedOutcome
		closed.EndedAt = now
		appendTaskIndex(path, closed)
	}
}

// heldTaskIDs is the set of node ids this session's graph is holding, or nil for
// a session that never built one. It reads the graph WITHOUT constructing one,
// on [Agent.liveTaskRows]'s own terms.
func (a *Agent) heldTaskIDs() map[string]bool {
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	held := make(map[string]bool, len(graph.order))
	for _, id := range graph.order {
		held[strconv.FormatUint(id, 10)] = true
	}
	return held
}

// indexEntryLocked is one node as a row, with the graph held.
func (n *TaskNode) indexEntryLocked(session string) TaskIndexEntry {
	elapsed := n.elapsed
	if elapsed == 0 && !n.started.IsZero() {
		elapsed = time.Since(n.started)
	}
	// The list and the count come out of the SAME call, which is what keeps them
	// from disagreeing (see [taskFileCitations]). They are the node's LEAVINGS
	// and not its live tally: a row is what the work came to, and what a node has
	// written so far while it is still running is presence's to report
	// ([Agent.presenceTasks]), not this file's.
	files, wrote := taskFileCitations(n.changed)
	entry := TaskIndexEntry{
		ID:     strconv.FormatUint(n.id, 10),
		Parent: taskIndexParent(n.parent),
		Name:   TaskSlug(n.spec.title),
		Label:  taskLabel(n.spec.title),
		Title:  strings.TrimSpace(n.spec.title),
		// The node's OWN kind, which was settled at admission and never moves
		// (task_contract.go says so out loud): reading n.kind rather than
		// re-deriving it from the spec is what keeps the row and the roster from
		// ever disagreeing about one piece of work.
		Kind:         n.kind,
		Status:       string(n.state),
		Outcome:      taskOutcome(n.report),
		FilesChanged: wrote,
		Files:        files,
		// WHETHER THIS WORK COULD EVER HAVE SPLIT ITSELF, straight off the spec's
		// own arming word — the graph is already held here, which is the lock
		// [TaskNode.armedBy] would otherwise take.
		MaySplit: n.spec.armed,
		// The FROZEN figure, read straight off the node: this runs with the graph
		// held and [TaskNode.spend] takes that lock itself. A row for a node still
		// running carries no price, which is what it has always carried.
		Cost: n.cost,
		// The model the node was ADMITTED on, which is the model its bill was
		// run up at, and empty when it took the conversation's. Tokens is the
		// same frozen tally as the cost beside it, summed to the one figure a
		// citation carries.
		Model: n.spec.model,
		// AND WHERE THE ESCALATION WENT, straight off the node, written only when
		// a correction round really did move ([TaskNode.repairedOn] holds that
		// rule so no reader has to).
		RepairedOn:    n.repaired,
		Tokens:        n.input + n.output,
		DurationMS:    elapsed.Milliseconds(),
		SessionID:     session,
		ArtifactURI:   taskArtifactURI(n.worktree, n.branch),
		TranscriptURI: taskURI(n.journal),
	}
	// A LIVE ROW SAYS WHAT IS HAPPENING IN IT. The recorder is read here, under
	// the graph's lock, because this is the one place a row is built and both
	// readers of the index — the "@" drop-up and the `tasks` tool — must not
	// each grow their own way of asking (see [Agent.TaskIndex]). The recorder
	// takes only its own lock, so nothing waits on the graph for it.
	if !n.state.settled() {
		entry.Activity = n.room.recorder().activity()
	}
	if n.state.settled() {
		// A landed node's EndedAt is now minus nothing: the report hook runs at
		// the transition. A row rebuilt later — the live merge over a graph that
		// still holds finished nodes — keeps the file's row instead, which is
		// where the original stamp is.
		//
		// An UNVERIFIED node is landed by this measure and by every other one in
		// this file: its run is over, its cost is frozen, and the row it writes
		// is the project's record that the work happened and nobody could judge
		// it. A resolution later writes a second row, which is what an
		// append-only history is for.
		entry.EndedAt = time.Now()
	}
	return entry
}

func taskIndexParent(parent uint64) string {
	if parent == 0 {
		return ""
	}
	return strconv.FormatUint(parent, 10)
}

// taskArtifactURI names where the node's work IS, preferring the thing a person
// can open over the thing they would have to check out.
//
// It CHECKS the worktree rather than trusting the checkpoint, for the reason
// [interrupt] checks a branch: a worktree that was merged and pruned is a
// directory that is not there, and a row promising one would send both readers
// of this index at a path that does not exist.
func taskArtifactURI(worktree, branch string) string {
	if worktree = strings.TrimSpace(worktree); worktree != "" {
		if info, err := os.Stat(worktree); err == nil && info.IsDir() {
			return taskURI(worktree)
		}
	}
	if branch = strings.TrimSpace(branch); branch != "" {
		return "git:" + branch
	}
	return ""
}

// taskURI spells a path as one. It is file:// and not a bare path because the
// row carries two of these and one of them is sometimes a branch: a reader
// should not have to guess which kind of thing it is holding.
func taskURI(path string) string {
	if path = strings.TrimSpace(path); path != "" {
		return "file://" + path
	}
	return ""
}

// taskOutcome is the report's first sentence, capped. The whole report is in
// the transcript this row points at.
func taskOutcome(report string) string {
	line := strings.TrimSpace(firstLine(report))
	if len(line) > taskOutcomeLimit {
		line = strings.TrimSpace(line[:taskOutcomeLimit]) + "…"
	}
	return line
}

// taskLabel is the title as a row draws it.
func taskLabel(title string) string {
	title = strings.Join(strings.Fields(title), " ")
	if len(title) > taskLabelLimit {
		return strings.TrimSpace(title[:taskLabelLimit-1]) + "…"
	}
	return title
}

// TaskSlug is a title as an "@" token: lower case, words joined by hyphens,
// everything that is not a letter or a digit dropped.
//
// It is the mention's whole addressing scheme, and it is derived rather than
// stored-and-minted because a person typing "@fix-the-nil-map" is typing what
// they can SEE — the title — and any scheme that gave the task a name they
// could not derive from its title would be a name they have to look up first.
func TaskSlug(title string) string {
	var out strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && out.Len() > 0 {
				out.WriteByte('-')
			}
			dash = false
			out.WriteRune(r)
		default:
			dash = out.Len() > 0
		}
	}
	slug := out.String()
	if len(slug) > taskLabelLimit {
		slug = strings.TrimRight(slug[:taskLabelLimit], "-")
	}
	return slug
}

// ── the search both readers use ─────────────────────────────────────────────

// SearchTaskIndex ranks rows against a query and returns at most limit of them.
//
// An EMPTY QUERY is not an error and not everything: it is "the most recent
// work", which is what both callers want when the person has typed "@" and
// nothing after it, or when the model asked what has been going on.
//
// The needle is matched against the TITLE, the ID and the OUTCOME, in that
// order of worth. Those three are what a person half-remembers: what it was
// called, which number it was, and what it turned out to be. The brief and the
// files are deliberately not searched — a query that matched every task that
// ever touched session.go would be a search that answers "all of them".
func SearchTaskIndex(rows []TaskIndexEntry, query string, limit int) []TaskIndexEntry {
	switch {
	case limit <= 0:
		limit = taskSearchLimit
	case limit > taskSearchCeiling:
		limit = taskSearchCeiling
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		if len(rows) > limit {
			return rows[:limit]
		}
		return rows
	}
	type ranked struct {
		entry TaskIndexEntry
		score int
		at    int
	}
	var hits []ranked
	for at, entry := range rows {
		score, ok := taskScore(entry, needle)
		if !ok {
			continue
		}
		hits = append(hits, ranked{entry: entry, score: score, at: at})
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score < hits[b].score
		}
		// rows arrive newest first, so the earlier index is the newer task: a
		// tie between two equally good matches goes to the one that happened
		// most recently.
		return hits[a].at < hits[b].at
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]TaskIndexEntry, 0, len(hits))
	for _, hit := range hits {
		out = append(out, hit.entry)
	}
	return out
}

// The tiers one row is scored in, lowest wins. They are spaced so that no
// within-tier offset can reach the tier below, which is [pathScore]'s own
// arrangement in internal/tui3 — the same idea, over a different haystack.
const (
	taskTierID        = 0
	taskTierTitle     = 1 << 16
	taskTierSlug      = 1 << 17
	taskTierSubstring = 1 << 18
	taskTierSequence  = 1 << 19
	taskTierOutcome   = 1 << 20
)

// taskScore ranks one row against a lowercased needle.
//
// THE ID IS AN EXACT MATCH OR NOTHING. "7" must find task 7, and it must not
// find every task whose outcome mentions seven files: a number typed at this
// search is somebody quoting an id, and a fuzzy id is an id that answers the
// wrong task.
func taskScore(entry TaskIndexEntry, needle string) (int, bool) {
	if strings.TrimSpace(entry.ID) == needle {
		return taskTierID, true
	}
	title := strings.ToLower(entry.Title)
	slug := entry.Name
	switch {
	case strings.HasPrefix(title, needle):
		return taskTierTitle + len(title), true
	case strings.HasPrefix(slug, needle):
		return taskTierSlug + len(slug), true
	}
	if at := strings.Index(title, needle); at >= 0 {
		return taskTierSubstring + at<<8 + len(title), true
	}
	if span, ok := subsequenceSpan(title, needle); ok {
		return taskTierSequence + span<<8 + len(title), true
	}
	if at := strings.Index(strings.ToLower(entry.Outcome), needle); at >= 0 {
		return taskTierOutcome + at, true
	}
	return 0, false
}

// ── the shared ladder ───────────────────────────────────────────────────────

// The rungs [MatchQuality] answers with, HIGHEST IS BEST. They are spaced two
// hundred apart so that a caller may add its own weighting between them —
// recency, or how much a row wants somebody — without any of it reaching the
// rung below (internal/tui3's home.go does exactly that).
const (
	// MatchWord is the query standing as a whole word in the text: "auth" in
	// "fix the auth test". It is the top rung because it is the one a person
	// means when they type a word and expect the thing they named.
	MatchWord = 1000
	// MatchPrefix is the text STARTING with the query — "pric" over "pricing
	// research". A thing whose name begins with what you typed is the thing you
	// were typing the name of.
	MatchPrefix = 800
	// MatchWordStart is some later word starting with it: "res" in "pricing
	// research".
	MatchWordStart = 600
	// MatchInside is the query somewhere in the text at all.
	MatchInside = 400
	// MatchScattered is the query's letters appearing in order with anything
	// between them — "prr" over "pricing research". It is the bottom rung
	// because it is the one that finds things nobody was looking for.
	MatchScattered = 200
)

// MatchQuality is HOW WELL one query matches one piece of text, and whether it
// matches at all. It is the ladder above, and it is exported because it is the
// one fuzzy matcher in this program that more than one surface ranks with.
//
// IT IS NOT [taskScore], AND THE DIFFERENCE IS DELIBERATE. That one ranks
// FIELD-MAJOR — every title-prefix beats every slug-prefix, which beats every
// substring — because an "@" mention is resolving one token to one task and the
// field it matched is most of the answer. This one ranks RUNG-MAJOR, because a
// person searching a whole machine cares how well the words matched and not
// which column they landed in; the caller weights the columns itself. Two
// orderings, one matcher underneath, and both of them say so.
//
// text and needle are both expected lowercased; a caller folding case twice
// per row over a thousand rows is the one cost this refuses to pay for it.
func MatchQuality(text, needle string) (int, bool) {
	if needle == "" {
		return 0, false
	}
	at := strings.Index(text, needle)
	if at < 0 {
		if _, ok := subsequenceSpan(text, needle); ok {
			return MatchScattered, true
		}
		return 0, false
	}
	// A whole word: nothing alphanumeric immediately either side of it.
	before := at == 0 || !matchWordRune(rune(text[at-1]))
	end := at + len(needle)
	after := end == len(text) || !matchWordRune(rune(text[end]))
	switch {
	case before && after:
		return MatchWord, true
	case at == 0:
		return MatchPrefix, true
	case before:
		return MatchWordStart, true
	}
	return MatchInside, true
}

// matchWordRune reports whether a byte is part of a word for [MatchQuality]'s
// boundary test. It is deliberately ASCII-only and deliberately crude: the
// question is "did the match start where a word starts", and a separator is any
// of the space, punctuation and path characters that titles are actually
// written with.
func matchWordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return false
}

// subsequenceSpan reports whether needle's runes appear in order in text, and
// how far apart the first and last of them landed — the span, which is what
// tells a tight match from a coincidence.
func subsequenceSpan(text, needle string) (int, bool) {
	first, last, at := -1, -1, 0
	runes := []rune(needle)
	for i, r := range text {
		if at >= len(runes) {
			break
		}
		if r == runes[at] {
			if first < 0 {
				first = i
			}
			last = i
			at++
		}
	}
	if at < len(runes) {
		return 0, false
	}
	return last - first, true
}

// TaskMatches reports whether one row of the index answers a query at all.
//
// IT IS THE SAME LADDER [SearchTaskIndex] RANKS WITH, asked for the yes and not
// for the place ([taskScore] is the one implementation of both). A reader that
// wants the index FILTERED rather than RANKED — the task page keeps the record
// in the order it happened and merely takes rows out of it — would otherwise
// have to spell the ladder out a second time, and two spellings of "does this
// task match" is two lists that disagree about which tasks exist.
//
// AN EMPTY QUERY MATCHES EVERYTHING, which is the other half of the bargain
// [SearchTaskIndex] makes with one: nothing typed is not a filter that excludes
// everything, it is no filter at all.
func TaskMatches(entry TaskIndexEntry, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	_, ok := taskScore(entry, needle)
	return ok
}

// TaskWordsMatch is the same question asked of WORDS rather than of a row: does
// this title answer this query.
//
// It exists because a task that is still running in THIS conversation is a node
// of a live graph and not a row of the file — the page filters both halves of
// itself with one query, and a live node has a title where a row has six fields.
// What it shares with [taskScore] is the part that is about the words: the
// substring first, then the subsequence, so that "prsr" finds "Port the parser"
// on both halves of the page or on neither.
func TaskWordsMatch(text, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	words := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(words, needle) {
		return true
	}
	_, ok := subsequenceSpan(words, needle)
	return ok
}

// LookupTask resolves one "@" token — a slug, or an id — against the index.
//
// The NEWEST match wins. Slugs are derived from titles and titles repeat: a
// project that fixed the same crash in March and again in August has two rows
// called fix-the-nil-map-crash, and "the nil-map task" said out loud in
// September means the August one.
func LookupTask(rows []TaskIndexEntry, token string) (TaskIndexEntry, bool) {
	token = strings.ToLower(strings.TrimSpace(token))
	if token == "" {
		return TaskIndexEntry{}, false
	}
	for _, entry := range rows {
		if entry.Name == token || strings.TrimSpace(entry.ID) == token {
			return entry, true
		}
	}
	return TaskIndexEntry{}, false
}
