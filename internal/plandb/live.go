package plandb

// The live step is the one present-tense reading of a task: the step whose
// command is running right now. THE LAW IS THE WHOLE OF IT — A LIVE STEP IS
// TRUE ONLY WHILE ITS COMMAND RUNS — so it is written when the command begins,
// cleared the moment the step ends, and cleared again by every ending of the
// worker's loop. A task that is not running a command claims no present: its
// live row is absent, and absence is what a reader draws as nothing.
//
// IT IS A ROW BESIDE THE LEDGER, NOT A COLUMN ON THE TASK. The trajectory is
// the record of steps that finished (internal/run's file); the live row is the
// step that has not, and it is gone by the time the record has it. Keeping it
// in its own table, keyed by the task and written through the same
// BEGIN IMMEDIATE transaction as a spend row, means the writers that race a
// worker — another process's CLI, the chat's steer verbs — never read it out of
// the plan's own state, and a cleared row leaves nothing behind to reinterpret.

import (
	"strings"
	"time"
)

// LiveStep is the step a task has in flight: the number the worker gave it
// (counted from one, the same number the trajectory will record), the command
// it asked the belt to run, and the moment the command started. The zero value
// is the honest answer for a task that is running nothing, which is why a
// reader draws nothing rather than a zero step.
type LiveStep struct {
	Step    int
	Command string
	Since   time.Time
}

// Empty answers whether there is no live step at all — the zero value, and the
// one question a surface asks before it draws a line. It is a method so the
// emptiness law has one spelling: a live step with no number is no live step,
// whatever a zero Command and Since might tempt a reader to draw.
func (l LiveStep) Empty() bool { return l.Step == 0 }

// SetLive records the step a task has in flight: its number, the command, and
// the moment the command began. It is written the way a spend row is — one
// BEGIN IMMEDIATE transaction under the store's own lock — so a live step and
// the ledger that accounts for the same call are published by one discipline,
// and a second begin for the same task rewrites the row rather than stacking
// one behind another: there is one live step per task, and a new one replaces
// the last.
//
// The moment is the store's own clock, the same one a spend row is stamped
// with, so two readings of one run's present cannot disagree about now.
func (s *Store) SetLive(taskID string, step int, command string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO live (task_id, step, command, since) VALUES (?, ?, ?, ?)
		ON CONFLICT(task_id) DO UPDATE SET step = excluded.step, command = excluded.command, since = excluded.since`,
		bareTaskID(taskID), step, command, formatTime(s.now().UTC())); err != nil {
		return err
	}
	return tx.Commit()
}

// ClearLive forgets a task's live step. It is the deletion every ending runs —
// the step's own end line, the cap, the wall, an error — so a stopped task
// stops claiming a present. Clearing a task that has no live row is not an
// error: the honest answer to "clear it" is the same whether the row was there
// or already gone.
func (s *Store) ClearLive(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.beginWrite()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM live WHERE task_id = ?`, bareTaskID(taskID)); err != nil {
		return err
	}
	return tx.Commit()
}

// Live answers one task's live step, and the zero value when the task is
// running nothing. The read is narrowed to one id; a caller drawing a whole
// pane reads [LiveSteps] once instead of one row at a time.
func (s *Store) Live(taskID string) LiveStep {
	return s.LiveSteps()[bareTaskID(taskID)]
}

// LiveSteps answers every task's live step, keyed by the store's own bare id —
// what a pane reads in one pass before it draws a column of rows. It reads
// through the store's read handle, so it answers the last committed live rows
// — another process's worker included — and runs beside a writer rather than
// behind it. A store already closed answers no steps rather than reaching for
// a handle that is gone, the way the other no-error reads do.
func (s *Store) LiveSteps() map[string]LiveStep {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]LiveStep{}
	if s.closed || s.rdb == nil {
		return out
	}
	rows, err := s.rdb.Query(`SELECT task_id, step, command, since FROM live`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id, command, since string
		var step int
		if rows.Scan(&id, &step, &command, &since) != nil {
			return out
		}
		moment, err := parseTime(since)
		if err != nil {
			continue
		}
		out[id] = LiveStep{Step: step, Command: command, Since: moment}
	}
	return out
}

// bareTaskID is the id the store keys a task by, from either spelling — the
// `t-` a surface hands back and the bare id the store holds — so a live row
// written by one reader and read by another meets on one key. It is the same
// normalisation a spend row's task_id already takes.
func bareTaskID(id string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "t-"))
}
