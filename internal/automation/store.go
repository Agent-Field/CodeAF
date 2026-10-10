package automation

// store.go is the one place automations and their runs are kept: a small SQLite
// database beside the presence folder and the clock's lock.
//
// IT IS A DATABASE AND NOT A FOLDER OF FILES, and that is the lesson of the
// feature this replaces. Its state lived in one JSON file per item, rewritten by
// whichever of several processes was looking, and most of its 6,000 lines were
// fences against those writers disagreeing — revision guards, pending-delivery
// markers, four schema barriers. Here a run is inserted and its slot advanced
// in ONE transaction, a slot can be dispatched at most once because the table
// says so, and every window reads the same rows.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// applicationID marks the file as this store's, so a database belonging to
// anything else is refused rather than read as empty. "AUTO".
const applicationID = 0x4155544f

// schemaVersion is the shape below. A newer file is refused, never rewritten.
const schemaVersion = 1

// busyTimeout bounds how long a write waits behind another process's. Every
// write here is small, so ten seconds is a stuck peer, not a slow one.
const busyTimeout = 10 * time.Second

// ErrNotFound is an automation or a run that is not in the store.
var ErrNotFound = errors.New("no such automation")

// errChanged is a clock write refused because the person changed the
// automation while the clock was holding an older copy. Their act is newer.
var errChanged = errors.New("the automation changed while it was being run")

// Store is the automations database.
type Store struct {
	db   *sql.DB
	root string
	now  func() time.Time
}

// DatabaseName is the file inside the root.
const DatabaseName = "automations.db"

// Open opens, or creates, the store under root (normally
// ~/.codeaf/v3/automations).
func Open(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("automations: no folder")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absolute, 0o700); err != nil {
		return nil, fmt.Errorf("automations: %w", err)
	}
	path := filepath.Join(absolute, DatabaseName)
	// THE FILE IS MADE PRIVATE BEFORE SQLITE OPENS IT, so SQLite never creates
	// it with its own default permissions.
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err != nil {
		return nil, fmt.Errorf("automations: %w", err)
	} else if err := f.Close(); err != nil {
		return nil, fmt.Errorf("automations: %w", err)
	}
	u := url.URL{Scheme: "file", Path: path}
	q := u.Query()
	q.Set("mode", "rw")
	q.Add("_pragma", fmt.Sprintf("busy_timeout(%d)", busyTimeout.Milliseconds()))
	// WAL lets every window read while the clock writes; FULL keeps a run that
	// was recorded recorded across a power cut.
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(FULL)")
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("automations: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, root: absolute, now: time.Now}
	if err := s.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Root is the folder the store, the presence files and the clock lock live in.
func (s *Store) Root() string { return s.root }

// Close releases the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	defer tx.Rollback()
	var app, version int
	if err := tx.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	switch {
	case app == applicationID && version == schemaVersion:
		return tx.Commit()
	case app != 0 || version != 0:
		return fmt.Errorf("automations: %s is not an automations database this build can read (application %d, version %d)", filepath.Join(s.root, DatabaseName), app, version)
	}
	var tables int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'").Scan(&tables); err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	if tables != 0 {
		return fmt.Errorf("automations: %s belongs to something else", filepath.Join(s.root, DatabaseName))
	}
	_, err = tx.ExecContext(ctx, `
CREATE TABLE counter (id INTEGER PRIMARY KEY CHECK (id = 1), seq INTEGER NOT NULL);
INSERT INTO counter (id, seq) VALUES (1, 0);
CREATE TABLE automations (
 id TEXT PRIMARY KEY,
 doc TEXT NOT NULL,
 status TEXT NOT NULL,
 next_ms INTEGER NOT NULL DEFAULT 0,
 seen TEXT NOT NULL DEFAULT '',
 memo TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1,
 seq INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX automations_due ON automations(status, next_ms);
CREATE TABLE runs (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 automation_id TEXT NOT NULL,
 due_ms INTEGER NOT NULL,
 why TEXT NOT NULL,
 phase TEXT NOT NULL,
 started_ms INTEGER NOT NULL DEFAULT 0,
 finished_ms INTEGER NOT NULL DEFAULT 0,
 outcome TEXT NOT NULL DEFAULT '',
 line TEXT NOT NULL DEFAULT '',
 detail TEXT NOT NULL DEFAULT '',
 usd REAL NOT NULL DEFAULT 0,
 transcript TEXT NOT NULL DEFAULT '',
 stop INTEGER NOT NULL DEFAULT 0,
 seq INTEGER NOT NULL DEFAULT 0,
 UNIQUE (automation_id, due_ms, why)
);
CREATE INDEX runs_by_automation ON runs(automation_id, id);
CREATE INDEX runs_by_phase ON runs(phase);
CREATE INDEX runs_by_seq ON runs(seq);
`)
	if err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA application_id=%d; PRAGMA user_version=%d", applicationID, schemaVersion)); err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	return tx.Commit()
}

// ── automations ─────────────────────────────────────────────────────────────

// Create saves a new automation the person agreed to, and works out when it
// first wakes.
func (s *Store) Create(a Automation) (Automation, error) {
	now := s.now()
	a.ID = strings.TrimSpace(a.ID)
	if a.ID == "" {
		a.ID = newID()
	}
	a.Status = StatusActive
	a.Created, a.Updated = now, now
	a.Revision = 1
	a.Seen, a.Memo = "", ""
	if a.Schedule.Repeats() && a.Schedule.Anchor.IsZero() && a.Schedule.Interval() > 0 {
		a.Schedule.Anchor = now
	}
	if err := a.Validate(); err != nil {
		return Automation{}, err
	}
	first, err := a.Schedule.First(now)
	if err != nil {
		return Automation{}, err
	}
	a.Next = first
	doc, err := json.Marshal(a)
	if err != nil {
		return Automation{}, err
	}
	err = s.write(func(tx *sql.Tx, seq int64) error {
		_, err := tx.Exec(`INSERT INTO automations (id, doc, status, next_ms, seen, revision, seq) VALUES (?, ?, ?, ?, '', 1, ?)`,
			a.ID, string(doc), string(a.Status), ms(a.Next), seq)
		return err
	})
	if err != nil {
		return Automation{}, err
	}
	return a, nil
}

// Get reads one automation.
func (s *Store) Get(id string) (Automation, error) {
	row := s.db.QueryRow(`SELECT doc, status, next_ms, seen, memo, revision FROM automations WHERE id = ?`, id)
	a, err := scanAutomation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Automation{}, ErrNotFound
	}
	return a, err
}

// List is every automation, the ones waiting on the person first, then the
// soonest to wake, then the rest.
func (s *Store) List() ([]Automation, error) {
	rows, err := s.db.Query(`SELECT doc, status, next_ms, seen, memo, revision FROM automations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var all []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		all = append(all, a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortAutomations(all)
	return all, nil
}

// Update saves the person's change to an automation's own half — its title,
// schedule, look, action, place and limits. Its status is left alone (that is
// [Store.SetStatus]'s door). A changed schedule is worked out again from now,
// and a finished one-time automation given a new moment wakes again. A watch
// whose look or condition changed forgets what it had seen, so its first new
// "yes" speaks.
func (s *Store) Update(a Automation) (Automation, error) {
	now := s.now()
	var saved Automation
	err := s.write(func(tx *sql.Tx, seq int64) error {
		current, err := scanAutomation(tx.QueryRow(`SELECT doc, status, next_ms, seen, memo, revision FROM automations WHERE id = ?`, a.ID))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		a.Created, a.Updated = current.Created, now
		a.Revision = current.Revision + 1
		a.Status, a.Next, a.Seen, a.Memo = current.Status, current.Next, current.Seen, current.Memo
		if lookChanged(current.Look, a.Look) {
			a.Seen, a.Memo = "", ""
		}
		if scheduleChanged(current.Schedule, a.Schedule) {
			if a.Schedule.Repeats() && a.Schedule.Interval() > 0 {
				a.Schedule.Anchor = now
			}
			first, err := a.Schedule.First(now)
			if err != nil {
				return err
			}
			a.Next = first
			if a.Status == StatusFinished {
				a.Status = StatusActive
			}
		} else {
			a.Schedule.Anchor = current.Schedule.Anchor
		}
		if err := a.Validate(); err != nil {
			return err
		}
		doc, err := json.Marshal(a)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE automations SET doc = ?, status = ?, next_ms = ?, seen = ?, memo = ?, revision = ?, seq = ? WHERE id = ?`,
			string(doc), string(a.Status), ms(a.Next), a.Seen, a.Memo, a.Revision, seq, a.ID)
		saved = a
		return err
	})
	return saved, err
}

// SetStatus pauses or resumes an automation, as the person's change. Resuming
// a rhythm starts from its next slot after now: a pause is not a missed run, so
// nothing catches up for the time it was paused. A one-time automation resumed
// after its moment runs once, late.
func (s *Store) SetStatus(id string, status Status) (Automation, error) {
	if status != StatusActive && status != StatusPaused {
		return Automation{}, fmt.Errorf("a person cannot set the status %q", status)
	}
	now := s.now()
	var saved Automation
	err := s.write(func(tx *sql.Tx, seq int64) error {
		a, err := scanAutomation(tx.QueryRow(`SELECT doc, status, next_ms, seen, memo, revision FROM automations WHERE id = ?`, id))
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if a.Status == status || a.Status == StatusFinished {
			saved = a
			return nil
		}
		a.Status = status
		if status == StatusActive {
			if a.Schedule.Repeats() {
				if a.Next, err = a.Schedule.Next(now); err != nil {
					return err
				}
			} else {
				a.Next = a.Schedule.At
			}
		}
		a.Revision++
		a.Updated = now
		doc, err := json.Marshal(a)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE automations SET doc = ?, status = ?, next_ms = ?, revision = ?, seq = ? WHERE id = ?`,
			string(doc), string(a.Status), ms(a.Next), a.Revision, seq, id)
		saved = a
		return err
	})
	return saved, err
}

// Delete removes an automation and its history.
func (s *Store) Delete(id string) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		res, err := tx.Exec(`DELETE FROM automations WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(`DELETE FROM runs WHERE automation_id = ?`, id)
		return err
	})
}

// ── the clock's own writes ──────────────────────────────────────────────────
//
// THE CLOCK NEVER REWRITES THE DOCUMENT. Its copy of an automation can be
// minutes old by the time a look or a piece of work ends, and writing that copy
// back would put an older slot over the one [Store.Take] already advanced. So
// the clock writes columns, by name, and only while the person's revision is
// still the one it read.

// SetSeen records a watch's newest decided judgment, and what its look keeps
// for the next one.
func (s *Store) SetSeen(id string, revision int64, seen, memo string) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		res, err := tx.Exec(`UPDATE automations SET seen = ?, memo = ?, seq = ? WHERE id = ? AND revision = ?`, seen, memo, seq, id, revision)
		return changedUnless(res, err)
	})
}

// FinishWatch ends a once-watch that has spoken.
func (s *Store) FinishWatch(id string, revision int64) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		res, err := tx.Exec(`UPDATE automations SET status = ?, next_ms = 0, seq = ? WHERE id = ? AND revision = ?`,
			string(StatusFinished), seq, id, revision)
		return changedUnless(res, err)
	})
}

// changedUnless turns a guarded update that matched no row into errChanged:
// the person's revision moved, or the automation is gone.
func changedUnless(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return errChanged
	}
	return nil
}

// ── runs ────────────────────────────────────────────────────────────────────

// Due is every active automation whose slot has come by now.
func (s *Store) Due(now time.Time) ([]Automation, error) {
	rows, err := s.db.Query(`SELECT doc, status, next_ms, seen, memo, revision FROM automations WHERE status = ? AND next_ms > 0 AND next_ms <= ? ORDER BY next_ms`,
		string(StatusActive), ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var due []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, err
		}
		due = append(due, a)
	}
	return due, rows.Err()
}

// NextWake is the earliest slot of any active automation, and zero when none.
func (s *Store) NextWake() (time.Time, error) {
	var next sql.NullInt64
	err := s.db.QueryRow(`SELECT min(next_ms) FROM automations WHERE status = ? AND next_ms > 0`, string(StatusActive)).Scan(&next)
	if err != nil || !next.Valid {
		return time.Time{}, err
	}
	return fromMS(next.Int64), nil
}

// Take turns a due automation's slot into a queued run and moves the
// automation on, in one transaction: its next slot after now, or — for a
// one-time automation — finished. However many slots were missed, ONE run is
// queued and it is marked late; the rest are not run, because a person coming
// back after a weekend wants the weekly report once, not twice.
//
// The take is refused (errChanged) when the person changed the automation since
// the clock read it, and the UNIQUE slot makes a second take of the same slot
// impossible however the clock is interrupted.
func (s *Store) Take(a Automation, now time.Time) (Run, error) {
	why := WhyOnTime
	if now.Sub(a.Next) > LateAfter {
		why = WhyLate
	}
	run := Run{AutomationID: a.ID, Due: a.Next, Why: why, Phase: PhaseQueued}
	next := time.Time{}
	status := a.Status
	if a.Schedule.Repeats() {
		var err error
		if next, err = a.Schedule.Next(now); err != nil {
			return Run{}, err
		}
	} else {
		status = StatusFinished
	}
	err := s.write(func(tx *sql.Tx, seq int64) error {
		var revision, nextMS int64
		var current string
		if err := tx.QueryRow(`SELECT revision, next_ms, status FROM automations WHERE id = ?`, a.ID).Scan(&revision, &nextMS, &current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if revision != a.Revision || nextMS != ms(a.Next) || current != string(StatusActive) {
			return errChanged
		}
		res, err := tx.Exec(`INSERT INTO runs (automation_id, due_ms, why, phase, seq) VALUES (?, ?, ?, ?, ?)`,
			a.ID, ms(run.Due), string(run.Why), string(PhaseQueued), seq)
		if err != nil {
			return err
		}
		run.ID, _ = res.LastInsertId()
		_, err = tx.Exec(`UPDATE automations SET next_ms = ?, status = ?, seq = ? WHERE id = ?`,
			ms(next), string(status), seq, a.ID)
		return err
	})
	if err != nil {
		return Run{}, err
	}
	return run, nil
}

// advance moves an automation past a slot without queuing a run for it, as the
// clock's own guarded write.
func (s *Store) advance(a Automation, next time.Time) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		res, err := tx.Exec(`UPDATE automations SET next_ms = ?, seq = ? WHERE id = ? AND revision = ? AND next_ms = ? AND status = ?`,
			ms(next), seq, a.ID, a.Revision, ms(a.Next), string(StatusActive))
		return changedUnless(res, err)
	})
}

// QueueNow asks for a run outside the schedule. Any window may ask; the clock
// takes it on its next look, within seconds.
func (s *Store) QueueNow(id string) (Run, error) {
	if _, err := s.Get(id); err != nil {
		return Run{}, err
	}
	now := s.now()
	run := Run{AutomationID: id, Due: now, Why: WhyNow, Phase: PhaseQueued}
	err := s.write(func(tx *sql.Tx, seq int64) error {
		var waiting int
		if err := tx.QueryRow(`SELECT count(*) FROM runs WHERE automation_id = ? AND phase != ?`, id, string(PhaseOver)).Scan(&waiting); err != nil {
			return err
		}
		if waiting > 0 {
			return errors.New("it is already running")
		}
		res, err := tx.Exec(`INSERT INTO runs (automation_id, due_ms, why, phase, seq) VALUES (?, ?, ?, ?, ?)`,
			id, ms(now), string(WhyNow), string(PhaseQueued), seq)
		if err != nil {
			return err
		}
		run.ID, _ = res.LastInsertId()
		return nil
	})
	return run, err
}

// Queued is every run waiting for the clock, oldest first.
func (s *Store) Queued() ([]Run, error) {
	return s.queryRuns(`WHERE phase = ? ORDER BY id`, string(PhaseQueued))
}

// Active is every run queued or in hand.
func (s *Store) Active() ([]Run, error) {
	return s.queryRuns(`WHERE phase != ? ORDER BY id`, string(PhaseOver))
}

// Start marks a run as in hand.
func (s *Store) Start(id int64) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		_, err := tx.Exec(`UPDATE runs SET phase = ?, started_ms = ?, seq = ? WHERE id = ?`,
			string(PhaseRunning), ms(s.now()), seq, id)
		return err
	})
}

// Finish records what a run came to.
func (s *Store) Finish(run Run) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		_, err := tx.Exec(`UPDATE runs SET phase = ?, finished_ms = ?, outcome = ?, line = ?, detail = ?, usd = ?, transcript = ?, seq = ? WHERE id = ?`,
			string(PhaseOver), ms(s.now()), string(run.Outcome), run.Line, run.Detail, run.USD, run.Transcript, seq, run.ID)
		return err
	})
}

// RequestStop asks the clock to stop a run. Any window may ask; the clock sees
// it within seconds and the run ends as stopped.
func (s *Store) RequestStop(id int64) error {
	return s.write(func(tx *sql.Tx, seq int64) error {
		_, err := tx.Exec(`UPDATE runs SET stop = 1, seq = ? WHERE id = ? AND phase != ?`, seq, id, string(PhaseOver))
		return err
	})
}

// StopRequested is the set of run ids somebody asked to stop.
func (s *Store) StopRequested() (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT id FROM runs WHERE stop = 1 AND phase != ?`, string(PhaseOver))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stops := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		stops[id] = true
	}
	return stops, rows.Err()
}

// Abandoned closes runs a previous clock left in hand. A clock is only ever
// taken from a process that is gone — the lock dies with it — so a run still
// "running" is one nobody is running; it is recorded as stopped and not run
// again, which is the rule for any run codeaf closing ended.
func (s *Store) Abandoned(line string) (int, error) {
	var closed int
	err := s.write(func(tx *sql.Tx, seq int64) error {
		res, err := tx.Exec(`UPDATE runs SET phase = ?, finished_ms = ?, outcome = ?, line = ?, seq = ? WHERE phase = ?`,
			string(PhaseOver), ms(s.now()), string(OutcomeStopped), line, seq, string(PhaseRunning))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		closed = int(n)
		return nil
	})
	return closed, err
}

// Runs is an automation's history, newest first, at most limit long.
func (s *Store) Runs(id string, limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.queryRuns(`WHERE automation_id = ? ORDER BY id DESC LIMIT ?`, id, limit)
}

// Run reads one run.
func (s *Store) Run(id int64) (Run, error) {
	runs, err := s.queryRuns(`WHERE id = ?`, id)
	if err != nil {
		return Run{}, err
	}
	if len(runs) == 0 {
		return Run{}, ErrNotFound
	}
	return runs[0], nil
}

// Changes is every run that changed after the cursor, in the order it changed,
// and the cursor to ask from next time. A window keeps the cursor and reads
// what is new; nobody has to be the process that ran it.
func (s *Store) Changes(after int64) ([]Run, int64, error) {
	runs, err := s.queryRuns(`WHERE seq > ? ORDER BY seq`, after)
	if err != nil {
		return nil, after, err
	}
	cursor := after
	var seq int64
	if err := s.db.QueryRow(`SELECT seq FROM counter WHERE id = 1`).Scan(&seq); err == nil && seq > cursor {
		cursor = seq
	}
	return runs, cursor, nil
}

// Cursor is the store's change counter now: a window that wants only what
// happens from here on starts from it.
func (s *Store) Cursor() (int64, error) {
	var seq int64
	err := s.db.QueryRow(`SELECT seq FROM counter WHERE id = 1`).Scan(&seq)
	return seq, err
}

func (s *Store) queryRuns(where string, args ...any) ([]Run, error) {
	rows, err := s.db.Query(`SELECT id, automation_id, due_ms, why, phase, started_ms, finished_ms, outcome, line, detail, usd, transcript FROM runs `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var r Run
		var due, started, finished int64
		var why, phase, outcome string
		if err := rows.Scan(&r.ID, &r.AutomationID, &due, &why, &phase, &started, &finished, &outcome, &r.Line, &r.Detail, &r.USD, &r.Transcript); err != nil {
			return nil, err
		}
		r.Due, r.Started, r.Finished = fromMS(due), fromMS(started), fromMS(finished)
		r.Why, r.Phase, r.Outcome = Why(why), Phase(phase), Outcome(outcome)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// write runs change inside one immediate transaction with a fresh value of the
// change counter, which every row it touches is stamped with.
func (s *Store) write(change func(tx *sql.Tx, seq int64) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRow(`UPDATE counter SET seq = seq + 1 WHERE id = 1 RETURNING seq`).Scan(&seq); err != nil {
		return fmt.Errorf("automations: %w", err)
	}
	if err := change(tx, seq); err != nil {
		return err
	}
	return tx.Commit()
}

type scanner interface{ Scan(dest ...any) error }

func scanAutomation(row scanner) (Automation, error) {
	var doc, status, seen, memo string
	var next, revision int64
	if err := row.Scan(&doc, &status, &next, &seen, &memo, &revision); err != nil {
		return Automation{}, err
	}
	var a Automation
	if err := json.Unmarshal([]byte(doc), &a); err != nil {
		return Automation{}, fmt.Errorf("automations: a damaged row: %w", err)
	}
	// THE COLUMNS ARE THE TRUTH FOR THE CLOCK'S STATE; the document carries the
	// person's half and is rewritten with them, so it can only lag, never lead.
	a.Status = Status(status)
	a.Next = fromMS(next)
	a.Seen = seen
	a.Memo = memo
	a.Revision = revision
	return a, nil
}

func sortAutomations(all []Automation) {
	rank := func(a Automation) int {
		switch a.Status {
		case StatusActive:
			return 0
		case StatusPaused:
			return 1
		}
		return 2
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0; j-- {
			a, b := all[j-1], all[j]
			ra, rb := rank(a), rank(b)
			swap := ra > rb
			if ra == rb {
				switch {
				case a.Next.IsZero() && !b.Next.IsZero():
					swap = true
				case !a.Next.IsZero() && !b.Next.IsZero():
					swap = a.Next.After(b.Next)
				case a.Next.IsZero() && b.Next.IsZero():
					swap = a.Created.Before(b.Created)
				}
			}
			if !swap {
				break
			}
			all[j-1], all[j] = b, a
		}
	}
}

func lookChanged(a, b *Look) bool {
	if (a == nil) != (b == nil) {
		return true
	}
	if a == nil {
		return false
	}
	return a.Command != b.Command || a.Files != b.Files || a.Tool != b.Tool || string(a.Args) != string(b.Args) || a.Condition != b.Condition
}

func scheduleChanged(a, b Schedule) bool {
	return !a.At.Equal(b.At) || strings.TrimSpace(a.Every) != strings.TrimSpace(b.Every) || a.Zone != b.Zone
}

func newID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}

func ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromMS(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}
