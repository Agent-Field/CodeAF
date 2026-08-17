package session

// THE TASK INDEX: WHAT THIS PROJECT'S WORK WAS, AFTER THE CONVERSATION THAT
// ASKED FOR IT IS GONE.
//
// task_store.go keeps ONE conversation's graph, beside that conversation's
// journal, and it is right to: a graph is a live thing with a frontier and a
// scheduler, and it belongs to the session that turns it. This file keeps the
// other half — the flat, finished record of every node this PROJECT has ever
// run, in one append-only file the whole directory shares.
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
//   - JSONL, APPEND-ONLY, ONE ROW PER LANDED NODE. Not a JSON document like the
//     checkpoint: the checkpoint is rewritten whole after every transition and
//     is one conversation's, while this is written once per node, forever,
//     by every window open on the directory. An append is the one write two
//     processes can make to the same file without a coordinator.
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
	// Status is the node's final state — "done", "failed", "unverified" — or its
	// live one ("running", "queued") on a row merged in from a graph that is
	// still turning.
	Status string `json:"status"`
	// Outcome is the first sentence of the node's report: what it did, or what
	// stopped it. Empty for work that has not landed.
	Outcome string `json:"outcome"`
	// FilesChanged is how many files the node wrote. A count and not the list:
	// the list is in the transcript, and a row that carried forty paths would be
	// the thing this index refuses to be.
	FilesChanged int `json:"filesChanged"`
	// Cost is what the node spent, in dollars, or 0 when nobody could say.
	Cost float64 `json:"cost,omitempty"`
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
	if len(rows) > taskIndexRows {
		rows = rows[:taskIndexRows]
	}
	return rows
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
	graph, session := a.tasks, a.sessionID()
	a.mu.Unlock()
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
	appendTaskIndex(path, entry)
}

// indexEntryLocked is one node as a row, with the graph held.
func (n *TaskNode) indexEntryLocked(session string) TaskIndexEntry {
	elapsed := n.elapsed
	if elapsed == 0 && !n.started.IsZero() {
		elapsed = time.Since(n.started)
	}
	entry := TaskIndexEntry{
		ID:           strconv.FormatUint(n.id, 10),
		Name:         TaskSlug(n.spec.title),
		Label:        taskLabel(n.spec.title),
		Title:        strings.TrimSpace(n.spec.title),
		Status:       string(n.state),
		Outcome:      taskOutcome(n.report),
		FilesChanged: len(n.changed),
		// The FROZEN figure, read straight off the node: this runs with the graph
		// held and [TaskNode.spend] takes that lock itself. A row for a node still
		// running carries no price, which is what it has always carried.
		Cost:          n.cost,
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
