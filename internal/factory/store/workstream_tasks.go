package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── A WORKSTREAM'S TASKS, KEPT ─────────────────────────────────────────────
//
// Two tables in WorkstreamsDB beside the workstreams (Workstreams v5, SPEC
// §2.2):
//
//   - tasks: one row per task, the task as JSON with its own rev. A Factory
//     job's task is NOT kept here: it is derived from the job on every read.
//     What a person sets on one (its owner, heading, waits, due date) is kept
//     as a row under the job's id, "job:<n>", the job's overlay.
//   - task_events: a task's thread, oldest first. An event is appended, and
//     changed in place only for an offer someone takes, dismisses or undoes.
//
// The tables are made by CREATE IF NOT EXISTS on every open, which is this
// database's migration: a server that never kept a task gains them empty.
// Deleting a workstream deletes its tasks and threads with it.
//
// The rules are internal/factory's workstream_task.go: this file keeps rows,
// and a change is made inside ONE transaction ([Store.EditWorkstreamTasks]) so
// a tick and the tasks it releases, or a move and the waits it cuts, land
// together or not at all.

const workstreamTasksSchema = `
CREATE TABLE IF NOT EXISTS task_deletions (ws TEXT NOT NULL, id TEXT NOT NULL, doc TEXT NOT NULL, PRIMARY KEY(ws,id));
CREATE TABLE IF NOT EXISTS tasks (
	ws  TEXT NOT NULL,
	id  TEXT NOT NULL,
	rev INTEGER NOT NULL,
	doc TEXT NOT NULL,
	PRIMARY KEY (ws, id)
);
CREATE TABLE IF NOT EXISTS task_events (
	seq  INTEGER PRIMARY KEY AUTOINCREMENT,
	ws   TEXT NOT NULL,
	task TEXT NOT NULL,
	id   TEXT NOT NULL UNIQUE,
	doc  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS task_events_task ON task_events (ws, task, seq);
CREATE TABLE IF NOT EXISTS task_reads (
	ws     TEXT NOT NULL,
	task   TEXT NOT NULL,
	person TEXT NOT NULL,
	seq    INTEGER NOT NULL,
	PRIMARY KEY (ws, task, person)
);
`

// ErrTaskStale is a task write whose rev is not the one kept.
var ErrTaskStale = errors.New("this task changed since you read it; read it again, then make the change")

// ErrTaskGone is a task, or a line of its thread, that is not kept.
var ErrTaskGone = errors.New("task not found")

type rowQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
}

// WorkstreamTasks are the tasks kept for ws (Factory overlays among them),
// oldest first.
func (st *Store) WorkstreamTasks(ws string) ([]factory.WorkstreamTask, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []factory.WorkstreamTask{}, err
	}
	return tasksIn(db, ws)
}

func tasksIn(q rowQuerier, ws string) ([]factory.WorkstreamTask, error) {
	rows, err := q.Query(`SELECT rev, doc FROM tasks WHERE ws = ? AND NOT EXISTS (SELECT 1 FROM task_deletions d WHERE d.ws=tasks.ws AND d.id=tasks.id) ORDER BY rowid`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []factory.WorkstreamTask{}
	for rows.Next() {
		var rev int
		var doc string
		if err := rows.Scan(&rev, &doc); err != nil {
			return nil, err
		}
		var t factory.WorkstreamTask
		if err := json.Unmarshal([]byte(doc), &t); err != nil {
			return nil, err
		}
		t.Rev = rev
		out = append(out, t)
	}
	return out, rows.Err()
}

// WorkstreamTaskEvents is task's thread in ws, oldest first.
func (st *Store) WorkstreamTaskEvents(ws, task string) ([]factory.WorkstreamTaskEvent, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []factory.WorkstreamTaskEvent{}, err
	}
	return eventsIn(db, ws, task)
}

func eventsIn(q rowQuerier, ws, task string) ([]factory.WorkstreamTaskEvent, error) {
	rows, err := q.Query(`SELECT doc FROM task_events WHERE ws = ? AND task = ? ORDER BY seq`, ws, task)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []factory.WorkstreamTaskEvent{}
	for rows.Next() {
		var doc string
		if err := rows.Scan(&doc); err != nil {
			return nil, err
		}
		var e factory.WorkstreamTaskEvent
		if err := json.Unmarshal([]byte(doc), &e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// TaskWrite is one transaction over the tasks, handed to the edit of
// [Store.EditWorkstreamTasks].
type TaskWrite struct{ tx *sql.Tx }

// Tasks are the tasks kept for ws, as this transaction sees them.
func (w TaskWrite) Tasks(ws string) ([]factory.WorkstreamTask, error) { return tasksIn(w.tx, ws) }

// Events is a task's thread, as this transaction sees it.
func (w TaskWrite) Events(ws, task string) ([]factory.WorkstreamTaskEvent, error) {
	return eventsIn(w.tx, ws, task)
}

// Put keeps t under its own rev. rev is the rev the writer read: 0 makes a new
// task (refused when one is kept under that id), anything else must be the
// rev kept. t.Rev is what the row then holds.
func (w TaskWrite) Put(t factory.WorkstreamTask, rev int) error {
	var deleted int
	if err := w.tx.QueryRow(`SELECT count(*) FROM task_deletions WHERE ws=? AND id=?`, t.WS, t.ID).Scan(&deleted); err != nil {
		return err
	}
	if deleted > 0 {
		return ErrTaskGone
	}
	var cur int
	err := w.tx.QueryRow(`SELECT rev FROM tasks WHERE ws = ? AND id = ?`, t.WS, t.ID).Scan(&cur)
	switch {
	case errors.Is(err, sql.ErrNoRows) && rev != 0:
		return ErrTaskGone
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	case cur != rev:
		return ErrTaskStale
	}
	// What a task holds for one reader is filled when it is read, never kept.
	t.ForYou, t.Run, t.Queue = nil, nil, nil
	t.AsksFirst = ""
	if t.Ask != nil && t.Ask.CoveredBy != nil {
		a := *t.Ask
		a.CoveredBy = nil
		t.Ask = &a
	}
	if len(t.Asks) > 0 {
		t.Asks = append([]factory.TaskAsk(nil), t.Asks...)
		for i := range t.Asks {
			t.Asks[i].CoveredBy = nil
		}
	}
	doc, err := json.Marshal(t)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(`INSERT INTO tasks (ws, id, rev, doc) VALUES (?, ?, ?, ?)
		ON CONFLICT (ws, id) DO UPDATE SET rev = excluded.rev, doc = excluded.doc`, t.WS, t.ID, t.Rev, string(doc))
	return err
}

// Delete forgets task id of ws; its thread stays only when the task moves
// (Move), never here.
func (w TaskWrite) Delete(ws, id string) error {
	for _, q := range []string{`DELETE FROM tasks WHERE ws = ? AND id = ?`, `DELETE FROM task_events WHERE ws = ? AND task = ?`, `DELETE FROM task_reads WHERE ws = ? AND task = ?`, `DELETE FROM task_runs WHERE ws = ? AND task = ?`} {
		if _, err := w.tx.Exec(q, ws, id); err != nil {
			return err
		}
	}
	return nil
}

// Move carries task id, and its whole thread, from one workstream to another.
func (w TaskWrite) Move(from, to, id string) error {
	if _, err := w.tx.Exec(`DELETE FROM tasks WHERE ws = ? AND id = ?`, from, id); err != nil {
		return err
	}
	if _, err := w.tx.Exec(`UPDATE task_events SET ws = ? WHERE ws = ? AND task = ?`, to, from, id); err != nil {
		return err
	}
	_, err := w.tx.Exec(`UPDATE task_reads SET ws = ? WHERE ws = ? AND task = ?`, to, from, id)
	return err
}

// Append adds events to the threads they name, in order.
func (w TaskWrite) Append(ws string, events ...factory.WorkstreamTaskEvent) error {
	for _, e := range events {
		doc, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := w.tx.Exec(`INSERT INTO task_events (ws, task, id, doc) VALUES (?, ?, ?, ?)`, ws, e.Task, e.ID, string(doc)); err != nil {
			return err
		}
	}
	return nil
}

// Replace keeps e in place of the event of its id (an offer taken or undone).
func (w TaskWrite) Replace(ws string, e factory.WorkstreamTaskEvent) error {
	doc, err := json.Marshal(e)
	if err != nil {
		return err
	}
	res, err := w.tx.Exec(`UPDATE task_events SET doc = ? WHERE ws = ? AND id = ?`, string(doc), ws, e.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTaskGone
	}
	return nil
}

// EditWorkstreamTasks runs edit in one transaction under the workstreams'
// cross-process lock. An error from edit writes nothing.
func (st *Store) EditWorkstreamTasks(edit func(w TaskWrite) error) error {
	return st.wsTx(func(tx *sql.Tx) error { return edit(TaskWrite{tx: tx}) })
}

// CountWorkstreamTasks is how many tasks ws keeps, for the cap.
func CountWorkstreamTasks(tasks []factory.WorkstreamTask) int {
	return len(slices.DeleteFunc(slices.Clone(tasks), func(t factory.WorkstreamTask) bool {
		_, job := factory.IsJobTask(t.ID)
		return job
	}))
}

// ── WHAT EACH PERSON HAS READ (SPEC "Comments for you") ──
//
// task_reads keeps, per task and person, the last line of its thread that
// person has read. A comment after it is unread; reading the thread from a
// screen moves the mark to its end ([Store.MarkTaskRead]).

// TaskComment is one comment with the task it is on.
type TaskComment struct {
	Task  string
	Event factory.WorkstreamTaskEvent
}

// UnreadComments are the comments in ws that person has not read, oldest
// first, except their own and those older than `since` (RFC 3339): a
// person who never opened a task is not handed its whole history.
func (st *Store) UnreadComments(ws, person, since string) ([]TaskComment, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	person = factory.NormalizeHandle(person)
	rows, err := db.Query(`SELECT e.task, e.doc FROM task_events e
		LEFT JOIN task_reads r ON r.ws = e.ws AND r.task = e.task AND r.person = ?
		WHERE e.ws = ? AND e.seq > COALESCE(r.seq, 0) AND e.doc LIKE '%"kind":"comment"%'
		ORDER BY e.seq`, person, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskComment
	for rows.Next() {
		var task, doc string
		if err := rows.Scan(&task, &doc); err != nil {
			return nil, err
		}
		var e factory.WorkstreamTaskEvent
		if err := json.Unmarshal([]byte(doc), &e); err != nil {
			return nil, err
		}
		if e.Kind != factory.TaskEventComment || e.At < since || (e.By.Kind == factory.TaskByPerson && factory.NormalizeHandle(e.By.ID) == person) {
			continue
		}
		out = append(out, TaskComment{Task: task, Event: e})
	}
	return out, rows.Err()
}

// HandedUnopened are the tasks in ws handed to someone since person last
// opened them: a "handed" line in the thread after their read mark. Work's
// "Handed to you" shows each once, until it is opened.
func (st *Store) HandedUnopened(ws, person string) (map[string]bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	person = factory.NormalizeHandle(person)
	rows, err := db.Query(`SELECT DISTINCT e.task FROM task_events e
		LEFT JOIN task_reads r ON r.ws = e.ws AND r.task = e.task AND r.person = ?
		WHERE e.ws = ? AND e.seq > COALESCE(r.seq, 0) AND e.doc LIKE '%"kind":"handed"%'`, person, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var task string
		if err := rows.Scan(&task); err != nil {
			return nil, err
		}
		out[task] = true
	}
	return out, rows.Err()
}

// MarkTaskRead records that person has read task's thread in ws to its end.
func (st *Store) MarkTaskRead(ws, task, person string) error {
	person = factory.NormalizeHandle(person)
	if person == "" {
		return nil
	}
	return st.wsTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO task_reads (ws, task, person, seq)
			SELECT ?, ?, ?, COALESCE(MAX(seq), 0) FROM task_events WHERE ws = ? AND task = ?
			ON CONFLICT (ws, task, person) DO UPDATE SET seq = MAX(task_reads.seq, excluded.seq)`, ws, task, person, ws, task)
		return err
	})
}

// TaskWaitBefore keeps only the fields Undo may restore, never a second copy of a task's content.
type TaskWaitBefore struct {
	ID        string   `json:"id"`
	Rev       int      `json:"rev"`
	WaitingOn []string `json:"waiting_on"`
	Starts    string   `json:"starts,omitempty"`
}

// TaskDeletion keeps the short Undo receipt beside the hidden task, so a restart never loses it.
type TaskDeletion struct {
	Task  factory.WorkstreamTask `json:"task"`
	By    string                 `json:"by"`
	Until string                 `json:"until"`
	Waits []TaskWaitBefore       `json:"waits"`
}

// HiddenTaskIDs includes hidden derived jobs without loading historical Undo snapshots on every read.
func (st *Store) HiddenTaskIDs(ws string) (map[string]bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return map[string]bool{}, err
	}
	return hiddenTaskIDsIn(db, ws)
}
func hiddenTaskIDsIn(q rowQuerier, ws string) (map[string]bool, error) {
	rows, err := q.Query(`SELECT id FROM task_deletions WHERE ws=?`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// HiddenTaskIDs reads hidden identities under the task transaction's lock.
func (w TaskWrite) HiddenTaskIDs(ws string) (map[string]bool, error) {
	return hiddenTaskIDsIn(w.tx, ws)
}

// Deletion reads one receipt only when someone asks to undo it.
func (w TaskWrite) Deletion(ws, id string) (TaskDeletion, error) {
	var d TaskDeletion
	var doc string
	err := w.tx.QueryRow(`SELECT doc FROM task_deletions WHERE ws=? AND id=?`, ws, id).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrTaskGone
	}
	if err != nil {
		return d, err
	}
	err = json.Unmarshal([]byte(doc), &d)
	return d, err
}

// Hide takes a task off every live read without deleting its thread, runs or files.
func (w TaskWrite) Hide(ws, id string, d TaskDeletion) error {
	doc, err := json.Marshal(d)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(`INSERT INTO task_deletions(ws,id,doc) VALUES(?,?,?)`, ws, id, string(doc))
	return err
}

// Reveal withdraws a receipt before its task is written back with a fresh revision.
func (w TaskWrite) Reveal(ws, id string) error {
	_, err := w.tx.Exec(`DELETE FROM task_deletions WHERE ws=? AND id=?`, ws, id)
	return err
}
