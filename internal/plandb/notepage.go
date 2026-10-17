package plandb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// The bounds of one NotesPage call. THEY ARE CEILINGS, NOT DEFAULTS TO RAISE:
// a caller that needs more asks again from the cursor. Each is one constant so
// the figure in a doc comment, a prompt or a test is read from here.
const (
	// NotePageMax is the most notes one page may carry. It is the 200 that
	// Notes already refused to exceed, now a page size rather than a window.
	NotePageMax = 200
	// NotePageBodyMax is the largest note body a page will read: the 32 KiB
	// the writer admits. A store written by anything else is not trusted to
	// have kept to it, so the read enforces it again.
	NotePageBodyMax = 32 << 10
	// NotePageMetaMax is the largest agent, project, chat or client key a page
	// will read. NotePageFixedMax is the largest id, task id, timestamp or
	// sender: all of them are short by construction.
	NotePageMetaMax  = 4 << 10
	NotePageFixedMax = 64
	// NotePageRowMax is the largest row a page can admit, the sum of every
	// field's own ceiling. A row within its field ceilings is therefore always
	// within this, and a byte budget of at least this much always has room for
	// one row, which is what lets a page make progress without exempting any
	// row from the budget.
	NotePageRowMax = NotePageBodyMax + 4*NotePageMetaMax + 5*NotePageFixedMax
	// NotePageBytesMax is the most note text one page may carry.
	NotePageBytesMax = 256 << 10
	// NotePageScanMax is the most rows one call may EXAMINE when a Skip
	// filter is set, returned or not.
	NotePageScanMax = 1000
	// NotePageScanBytesMax is the most note text one call may read from the
	// database while examining, the byte twin of NotePageScanMax.
	NotePageScanBytesMax = 1 << 20
)

// ErrNoteCursor is what a cursor that cannot be honoured answers: no such note,
// a note of another task, an id that names more than one row, or one longer
// than any id can be. IT IS NEVER AN EMPTY PAGE. A cursor that silently read
// as "nothing after it" would end a delivery loop as though the inbox were
// empty, which is the failure this whole primitive exists to remove.
var ErrNoteCursor = errors.New("note cursor is not a note of this task")

// ErrTaskNotFound is what a page of a task the database does not hold answers.
// It is read from the same snapshot as the notes, so a task deleted a moment
// ago is this error and not a page of nothing.
var ErrTaskNotFound = errors.New("task not found")

// ErrNoteTooLarge is what a note beyond the read's field ceilings answers. THE
// NOTE IS NOT TRUNCATED, SKIPPED OR READ: a page cannot step over a note it
// refuses, so the cursor stays before it and every call from there answers
// this again until the note is fixed or removed. Notes before it are still
// delivered first. Match it with errors.Is; [NoteTooLargeError] says which.
var ErrNoteTooLarge = errors.New("note is larger than a page will read")

// NoteTooLargeError names the refused note without carrying it. Every field is
// short by construction: NoteID is set only when the id itself is within its
// ceiling, Field is one of the fixed column names, Size is a count.
type NoteTooLargeError struct {
	NoteID string
	Field  string
	Size   int64
}

func (e *NoteTooLargeError) Error() string {
	if e.NoteID == "" {
		return fmt.Sprintf("note %s is %d bytes, more than a page will read", e.Field, e.Size)
	}
	return fmt.Sprintf("note %s %s is %d bytes, more than a page will read", e.NoteID, e.Field, e.Size)
}

func (e *NoteTooLargeError) Is(target error) bool { return target == ErrNoteTooLarge }

// NotePageAsk is one bounded read of one task's notes, oldest first.
type NotePageAsk struct {
	TaskID string
	// After is the exclusive cursor: the id of a note of this task, as a
	// previous page returned it in Cursor. "" reads from the first note. THE
	// ID IS OPAQUE: the store resolves it to a position inside the read's own
	// snapshot, because the position (the table's seq) is rewritten by every
	// save and by a task delete, and so is never exposed, parsed or guessed.
	After string
	// Limit is how many notes the page may carry, 1..NotePageMax. Zero or
	// above the ceiling is an error, not a default.
	Limit int
	// MaxBytes is the byte budget of the page, counted as the stored length of
	// every field of every note: 0 for NotePageBytesMax, otherwise
	// NotePageRowMax..NotePageBytesMax. A note is added only if the page stays
	// within it.
	MaxBytes int
	// Skip drops a note BEFORE it counts toward Limit and MaxBytes, which is
	// how a source's own notes and the already-accepted ones stay out of the
	// batch without a list of ids in the SQL. It runs AFTER the database read
	// has finished and the store's lock is released, on rows already in
	// memory, so it may call the store. A skipped note still moves Cursor. It
	// runs at most ScanMax times per call. ITS OWN COST IS THE CALLER'S: the
	// page bounds how often it runs, not how long it takes, and the caller's
	// context is checked after it.
	Skip func(Note) bool
	// ScanMax is the most rows this call may examine when Skip is set, 0 for
	// NotePageScanMax and otherwise 1..NotePageScanMax. Without a Skip no
	// row is read that the page does not return, plus one to learn More.
	ScanMax int
	// ScanBytes is the byte twin of ScanMax: 0 for NotePageScanBytesMax,
	// otherwise NotePageRowMax..NotePageScanBytesMax.
	ScanBytes int
}

// NotePage is one page and where to continue.
type NotePage struct {
	Notes []Note
	// Cursor is the id of the LAST ROW EXAMINED, returned or skipped, so a
	// caller whose Skip dropped a whole call's rows still advances. When
	// nothing was examined it is the After the caller sent. A row that failed
	// to read is never examined: the cursor stops before it.
	Cursor string
	// More is true when rows of this task remain after Cursor in the snapshot
	// the page was read from, a refused note included. It is false only when
	// the snapshot held none.
	More bool
}

// noteCol is one column a page reads and the ceiling on its stored length.
type noteCol struct {
	name string // as the table spells it, quoted where the language needs it
	max  int
}

// The columns, in the order the query returns them. The indexes below name
// the ones assembled into a Note.
var noteCols = [...]noteCol{
	{"id", NotePageFixedMax}, {"task_id", NotePageFixedMax}, {"agent", NotePageMetaMax},
	{"body", NotePageBodyMax}, {"at", NotePageFixedMax}, {"project", NotePageMetaMax},
	{"chat", NotePageMetaMax}, {`"from"`, NotePageFixedMax}, {"client_id", NotePageMetaMax},
}

const (
	colID = iota
	colTask
	colAgent
	colBody
	colAt
	colProject
	colChat
	colFrom
	colClient
)

// noteRow is a note and what it cost against a byte budget.
type noteRow struct {
	note   Note
	weight int
}

// noteSelect is the select list: for every column its stored length in bytes
// and then its value ONLY IF that length is within the column's ceiling. THE
// LENGTH IS MEASURED IN THE QUERY, BEFORE THE DRIVER COPIES ANYTHING: a value
// over its ceiling comes back as NULL, so the Go side never allocates it. The
// length is of the value cast to a blob so it counts bytes, not characters.
// A store written before client_id has no such column; a read-only reader never
// migrates, so it is selected as the empty key, as loadNotes does.
func noteSelect(keyed bool) string {
	var b strings.Builder
	for i, col := range noteCols {
		if i > 0 {
			b.WriteString(", ")
		}
		if i == colClient && !keyed {
			b.WriteString("0, ''")
			continue
		}
		fmt.Fprintf(&b, "length(CAST(%[1]s AS BLOB)), CASE WHEN length(CAST(%[1]s AS BLOB)) <= %[2]d THEN %[1]s END", col.name, col.max)
	}
	return b.String()
}

// NotesPage reads one bounded page of a task's notes, oldest first, from ONE
// snapshot of the database, so the cursor and the rows it selects can never
// disagree.
//
// WHY THIS EXISTS: Notes hydrates the whole plan and answers the OLDEST
// fifty, so a caller that has read those can never see the fifty-first. This
// reads only the rows asked for, through the store's read handle (rdb), and
// touches neither the handle's memory nor loadState.
//
// WHAT IS BOUNDED, and what is not. Bounded: the rows returned, the rows and
// bytes buffered, the bytes the Go driver copies out of any one field (a field
// over its ceiling is measured in SQL and never copied), the size of every
// error, and the cursor's length. NOT bounded: the work SQLite does inside the
// read. `notes` has no index, so resolving a cursor and walking one task's
// rows scan the table, and SQLite may read an oversized field's pages to
// measure it. That work is limited by the caller's context deadline and
// nothing else; this is not a bounded-CPU walk. A (task_id, seq) index would
// shorten it and is separate engineering: it is a schema write, not made here.
//
// THE LOCK. The store's mutex is held only while the database is read, which
// is what orders it against Close: a closed store answers ErrClosed, and Close
// cannot release the handle under a read. The mutex is not context-aware, so a
// writer holding it delays this call by up to the busy window (5 s); the
// context is checked before and after, and the database read honours it. There
// is no goroutine, timeout or pool in here: THE CALLER'S CONTEXT IS THE ONE
// DEADLINE.
//
// A note over a field ceiling is ErrNoteTooLarge: delivered notes before it
// come back first, with More true and the cursor before it; the call that
// starts at it fails. Every failure returns an error and no page.
func (s *Store) NotesPage(ctx context.Context, ask NotePageAsk) (NotePage, error) {
	if ask.TaskID == "" || len(ask.TaskID) > NotePageFixedMax {
		return NotePage{}, ErrTaskNotFound
	}
	if len(ask.After) > NotePageFixedMax {
		return NotePage{}, ErrNoteCursor
	}
	if ask.Limit < 1 || ask.Limit > NotePageMax {
		return NotePage{}, fmt.Errorf("note page limit %d is outside 1..%d", ask.Limit, NotePageMax)
	}
	maxBytes, err := notePageBound("MaxBytes", ask.MaxBytes, NotePageRowMax, NotePageBytesMax)
	if err != nil {
		return NotePage{}, err
	}
	scanMax, err := notePageBound("ScanMax", ask.ScanMax, 1, NotePageScanMax)
	if err != nil {
		return NotePage{}, err
	}
	scanBytes, err := notePageBound("ScanBytes", ask.ScanBytes, NotePageRowMax, NotePageScanBytesMax)
	if err != nil {
		return NotePage{}, err
	}
	if err := ctx.Err(); err != nil {
		return NotePage{}, err
	}
	// Without a Skip nothing is dropped, so the rows read are the rows the
	// page can use; with one, the scan bound is the budget.
	rowCap := ask.Limit
	if ask.Skip != nil {
		rowCap = scanMax
	}
	rows, more, refused, err := s.readNoteRows(ctx, ask, rowCap, scanBytes)
	if err != nil {
		return NotePage{}, err
	}
	if len(rows) == 0 && refused != nil {
		return NotePage{}, refused
	}

	page := NotePage{Cursor: ask.After}
	used := 0
	for i, row := range rows {
		if ask.Skip == nil || !ask.Skip(row.note) {
			if len(page.Notes) == ask.Limit || used+row.weight > maxBytes {
				page.More = true
				break
			}
			page.Notes = append(page.Notes, row.note)
			used += row.weight
		}
		page.Cursor = row.note.ID
		if i == len(rows)-1 {
			page.More = more
		}
	}
	// A cancellation that arrived while a Skip ran must not hand back the
	// rows examined so far as though they were the whole answer.
	if err := ctx.Err(); err != nil {
		return NotePage{}, err
	}
	return page, nil
}

// notePageBound resolves one optional bound: zero is the ceiling, a value in
// floor..ceiling is itself, anything else is an error.
func notePageBound(name string, value, floor, ceiling int) (int, error) {
	switch {
	case value == 0:
		return ceiling, nil
	case value < floor || value > ceiling:
		return 0, fmt.Errorf("note page %s %d is outside %d..%d", name, value, floor, ceiling)
	}
	return value, nil
}

// readNoteRows is the page's one database read, in a single deferred
// transaction on the read handle, under the store's lock.
func (s *Store) readNoteRows(ctx context.Context, ask NotePageAsk, rowCap, byteCap int) ([]noteRow, bool, error, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.rdb == nil {
		return nil, false, nil, ErrClosed
	}
	tx, err := s.rdb.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, nil, err
	}
	defer tx.Rollback()
	return readNoteWindow(ctx, tx, ask, rowCap, byteCap)
}

// readNoteWindow is the read itself, on a transaction the caller owns: the task
// check, the cursor resolution and the row query. It is a function of the
// transaction and nothing else, so a test can run its steps on one real SQLite
// snapshot and commit from another handle between them. The returned refused is
// the error of a note the read stopped at, set only beside rows that precede
// it (or none); err is a failure of the read itself.
func readNoteWindow(ctx context.Context, tx *sql.Tx, ask NotePageAsk, rowCap, byteCap int) (rows []noteRow, more bool, refused, err error) {
	var live int
	switch err := tx.QueryRowContext(ctx, `SELECT 1 FROM tasks WHERE id = ?`, ask.TaskID).Scan(&live); {
	case errors.Is(err, sql.ErrNoRows):
		return nil, false, nil, ErrTaskNotFound
	case err != nil:
		return nil, false, nil, err
	}
	floor, err := resolveNoteCursor(ctx, tx, ask.TaskID, ask.After)
	if err != nil {
		return nil, false, nil, err
	}
	return queryNoteRows(ctx, tx, ask.TaskID, floor, rowCap, byteCap)
}

// queryNoteRows reads up to rowCap notes of one task after a table position,
// stopping early at the byte cap or at a note over a field ceiling. A note read
// is complete; a note refused is never partly read.
func queryNoteRows(ctx context.Context, tx *sql.Tx, taskID string, floor int64, rowCap, byteCap int) (rows []noteRow, more bool, refused, err error) {
	keyed, err := hasColumn(tx, "notes", "client_id")
	if err != nil {
		return nil, false, nil, err
	}
	// One row past the cap is asked for only to learn whether more remain.
	rs, err := tx.QueryContext(ctx, `SELECT `+noteSelect(keyed)+` FROM notes WHERE task_id = ? AND seq > ? ORDER BY seq LIMIT ?`,
		taskID, floor, rowCap+1)
	if err != nil {
		return nil, false, nil, err
	}
	defer rs.Close()
	var (
		sizes  [len(noteCols)]sql.NullInt64
		values [len(noteCols)]sql.NullString
		dest   = make([]any, 0, 2*len(noteCols))
		bytes  int
	)
	for i := range noteCols {
		dest = append(dest, &sizes[i], &values[i])
	}
	for rs.Next() {
		if len(rows) == rowCap {
			return rows, true, nil, rs.Err()
		}
		if err := rs.Scan(dest...); err != nil {
			return nil, false, nil, err
		}
		weight := 0
		for i, col := range noteCols {
			if sizes[i].Int64 > int64(col.max) {
				refusal := &NoteTooLargeError{Field: strings.Trim(col.name, `"`), Size: sizes[i].Int64}
				if sizes[colID].Int64 <= NotePageFixedMax {
					refusal.NoteID = values[colID].String
				}
				return rows, true, refusal, rs.Err()
			}
			weight += int(sizes[i].Int64)
		}
		if bytes+weight > byteCap {
			return rows, true, nil, rs.Err()
		}
		note := Note{
			ID: values[colID].String, TaskID: values[colTask].String, Agent: values[colAgent].String,
			Body: values[colBody].String, Project: values[colProject].String, Chat: values[colChat].String,
			From: values[colFrom].String, ClientID: values[colClient].String,
		}
		if note.At, err = parseTime(values[colAt].String); err != nil {
			return nil, false, nil, err
		}
		if note.From == "" {
			note.From = NoteFromWorker
		}
		bytes += weight
		rows = append(rows, noteRow{note: note, weight: weight})
	}
	if err := rs.Err(); err != nil {
		return nil, false, nil, err
	}
	return rows, false, nil, nil
}

// resolveNoteCursor turns an opaque note id into the table position after
// which to read, inside the caller's snapshot. "" is the start. An id that is
// absent, belongs to another task, or names two rows is ErrNoteCursor: the
// position is only meaningful in the snapshot it was read in, and it is never
// returned to the caller. The owner is compared in SQL so that no stored task
// id, of whatever length, is copied out to be compared.
func resolveNoteCursor(ctx context.Context, tx *sql.Tx, taskID, after string) (int64, error) {
	if after == "" {
		// saveState numbers notes from zero; the initial exclusive floor is before it.
		return -1, nil
	}
	rs, err := tx.QueryContext(ctx, `SELECT seq, task_id = ? FROM notes WHERE id = ? ORDER BY seq LIMIT 2`, taskID, after)
	if err != nil {
		return 0, err
	}
	defer rs.Close()
	var seq int64
	found := 0
	for rs.Next() {
		var mine bool
		if err := rs.Scan(&seq, &mine); err != nil {
			return 0, err
		}
		if !mine {
			return 0, ErrNoteCursor
		}
		found++
	}
	if err := rs.Err(); err != nil {
		return 0, err
	}
	if found != 1 {
		return 0, ErrNoteCursor
	}
	return seq, nil
}
