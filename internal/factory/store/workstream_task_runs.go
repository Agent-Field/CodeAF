package store

import (
	"database/sql"
	"errors"
)

// ── A TASK'S RUN, KEPT AS A LEASE (L6, DESIGN §11.3) ──────────────────────
//
// An agent's run on a task is a goroutine in the server's process, and a
// goroutine does not survive a restart. Before the lease, a restart left every
// task it was working on saying Running with nothing behind it, for as long as
// anyone cared to look (live2 WL: six of eight, for over nine minutes). So a
// run also keeps one row here for as long as it goes: which process holds it,
// who started it, when, when it last said it was alive, and how many times it
// has already been resumed. The server reads the rows back on start and
// settles every task whose run no live process holds.
//
// The row is the run's, never the task's: the task's own state stays in
// tasks, and a person's Stop or Done removes the run and its row together.

const workstreamTaskRunsSchema = `
CREATE TABLE IF NOT EXISTS task_runs (
	ws      TEXT NOT NULL,
	task    TEXT NOT NULL,
	owner   TEXT NOT NULL,
	starter TEXT NOT NULL,
	started TEXT NOT NULL,
	beat    TEXT NOT NULL,
	resumes INTEGER NOT NULL DEFAULT 0,
	words   TEXT NOT NULL DEFAULT '',
	queued  INTEGER NOT NULL DEFAULT 0,
	usd     REAL NOT NULL DEFAULT 0,
	answered INTEGER NOT NULL DEFAULT 0,
	typed   TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (ws, task)
);
`

// TaskRun is one task's run lease. Times are RFC 3339 in UTC.
type TaskRun struct {
	WS, Task string
	// Owner names the server process that holds the run; a new process has a
	// new name, so a row it does not own and that has stopped beating is a
	// run nobody holds.
	Owner string
	// Starter is the handle of the person whose rights the run works with.
	Starter string
	Started string
	Beat    string
	// Resumes is how many times this run was taken up again by itself after
	// it was cut off. A run resumes once; a second cut leaves it Interrupted.
	Resumes int
	// Words are what the run was asked, so a run taken up again is asked the
	// same thing (a revision's words included), not only the task's title.
	Words string
	// Queued is a run waiting for a free place under the limits, not yet
	// going.
	Queued bool
	// USD is what the run has spent so far.
	USD float64
	// Answered is a queued run an answer to the task's Ask started (D1): a
	// restart that queues it again still runs it as one that goes on with
	// what the earlier run made, not as a fresh start judged on its own
	// writes alone.
	Answered bool
	// Typed is what a person typed for the run (an answer's own words, a
	// revision's note): the only words in it a run taken up again may follow
	// an address in. Words is composed, partly from what the model wrote.
	Typed string
}

// PutTaskRun keeps r as its task's lease, in place of any it had.
func (st *Store) PutTaskRun(r TaskRun) error {
	return st.wsTx(func(tx *sql.Tx) error {
		return putTaskRun(tx, r)
	})
}

// BeatTaskRun says owner's run on task is still alive at beat, having spent
// usd so far. A lease that owner no longer holds is left alone.
func (st *Store) BeatTaskRun(ws, task, owner, beat string, usd float64) error {
	return st.wsTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE task_runs SET beat = ?, usd = MAX(usd, ?) WHERE ws = ? AND task = ? AND owner = ?`, beat, usd, ws, task, owner)
		return err
	})
}

// DropTaskRun forgets owner's lease on task. owner "" drops it whoever holds
// it: the run is over, settled by whoever settled it.
func (st *Store) DropTaskRun(ws, task, owner string) error {
	return st.wsTx(func(tx *sql.Tx) error { return dropTaskRun(tx, ws, task, owner) })
}

func dropTaskRun(tx *sql.Tx, ws, task, owner string) error {
	q, args := `DELETE FROM task_runs WHERE ws = ? AND task = ?`, []any{ws, task}
	if owner != "" {
		q += ` AND owner = ?`
		args = append(args, owner)
	}
	_, err := tx.Exec(q, args...)
	return err
}

// DropRun forgets task's lease inside a task write, so a settled task and
// its run's end land together.
func (w TaskWrite) DropRun(ws, task string) error { return dropTaskRun(w.tx, ws, task, "") }

// PutRun keeps r inside a task write.
func (w TaskWrite) PutRun(r TaskRun) error { return putTaskRun(w.tx, r) }

func putTaskRun(tx *sql.Tx, r TaskRun) error {
	_, err := tx.Exec(`INSERT INTO task_runs (ws, task, owner, starter, started, beat, resumes, words, queued, usd, answered, typed) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (ws, task) DO UPDATE SET owner = excluded.owner, starter = excluded.starter, started = excluded.started,
		beat = excluded.beat, resumes = excluded.resumes, words = excluded.words, queued = excluded.queued, usd = excluded.usd,
		answered = excluded.answered, typed = excluded.typed`,
		r.WS, r.Task, r.Owner, r.Starter, r.Started, r.Beat, r.Resumes, r.Words, r.Queued, r.USD, r.Answered, r.Typed)
	return err
}

// TaskRuns are every lease kept. A server that never kept a workstream has
// none.
func (st *Store) TaskRuns() ([]TaskRun, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT ws, task, owner, starter, started, beat, resumes, words, queued, usd, answered, typed FROM task_runs ORDER BY started`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TaskRun
	for rows.Next() {
		var r TaskRun
		if err := rows.Scan(&r.WS, &r.Task, &r.Owner, &r.Starter, &r.Started, &r.Beat, &r.Resumes, &r.Words, &r.Queued, &r.USD, &r.Answered, &r.Typed); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TaskRunOf is task's lease, and false when it has none.
func (st *Store) TaskRunOf(ws, task string) (TaskRun, bool, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return TaskRun{}, false, err
	}
	r := TaskRun{WS: ws, Task: task}
	err = db.QueryRow(`SELECT owner, starter, started, beat, resumes, words, queued, usd, answered, typed FROM task_runs WHERE ws = ? AND task = ?`, ws, task).
		Scan(&r.Owner, &r.Starter, &r.Started, &r.Beat, &r.Resumes, &r.Words, &r.Queued, &r.USD, &r.Answered, &r.Typed)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskRun{}, false, nil
	}
	return r, err == nil, err
}

// addTaskRunsAnswered gives a task_runs table made before answered (and
// typed) existed those columns, once.
func addTaskRunsAnswered(db *sql.DB) error {
	if err := addTaskRunsColumn(db, "answered", `ALTER TABLE task_runs ADD COLUMN answered INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	return addTaskRunsColumn(db, "typed", `ALTER TABLE task_runs ADD COLUMN typed TEXT NOT NULL DEFAULT ''`)
}

// addTaskRunsColumn runs alter when task_runs has no column called column.
func addTaskRunsColumn(db *sql.DB, column, alter string) error {
	rows, err := db.Query(`SELECT name FROM pragma_table_info('task_runs')`)
	if err != nil {
		return err
	}
	has := false
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		has = has || name == column
	}
	rows.Close()
	if has {
		return nil
	}
	_, err = db.Exec(alter)
	return err
}
