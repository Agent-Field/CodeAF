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
	// will read. NotePageFixedMax is the largest note id, timestamp or sender:
	// all of them are short by construction. It is also the longest cursor a
	// page will look up, and the length up to which a task id is taken to the
	// database without asking validID first.
	NotePageMetaMax  = 4 << 10
	NotePageFixedMax = 64
	// NotePageTaskIDMax is the longest task id a page will take to the
	// database at all, checked BEFORE validID and before any SQL: binding a
	// string copies all of it into SQLite's own heap, and validID trims spaces
	// before it measures, so without this a padded id of any length would be
	// copied. A canonical id is at most 64 bytes and a root keeps its "t-"
	// (66), so this leaves room for a root stored with padding. A longer id is
	// ErrNoteTaskIDTooLarge, not ErrTaskNotFound: such a root can exist.
	NotePageTaskIDMax = 256
	// NotePageRowMax is the largest row a page can admit, the sum of the
	// ceilings of the eight columns in noteCols: the body, four meta fields
	// (agent, project, chat, client key) and three fixed ones (id, timestamp,
	// sender). The task id is not among them because it is never read back (see
	// noteCols). A test holds this figure to that sum, because a constant
	// cannot add up an array. A row within its field ceilings is therefore
	// always within this, and a byte budget of at least this much always has
	// room for one row, which is what lets a page make progress without
	// exempting any row from the budget.
	NotePageRowMax = NotePageBodyMax + 4*NotePageMetaMax + 3*NotePageFixedMax
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
// a note of another task, or one longer than any id can be (an id that names
// more than one row is [ErrDuplicateNoteID], which matches this too). IT IS
// NEVER AN EMPTY PAGE. A cursor that silently read
// as "nothing after it" would end a delivery loop as though the inbox were
// empty, which is the failure this whole primitive exists to remove.
var ErrNoteCursor = errors.New("note cursor is not a note of this task")

// ErrDuplicateNoteID is what a cursor answers when its id names more than one
// row, whichever tasks they belong to. The notes table does not make ids
// unique and the writer mints each one once, so this is a damaged store, and
// it says so rather than claiming the note is another task's. A page may END on
// such an id, because a page hands out rows by position; the call that starts
// from it then answers this every time until the store is repaired. IT CANNOT
// BE STEPPED OVER and is never a skip or an empty page. It matches
// ErrNoteCursor under errors.Is, so a caller that only tells good cursors from
// bad ones needs nothing new.
var ErrDuplicateNoteID error = duplicateNoteID{}

type duplicateNoteID struct{}

func (duplicateNoteID) Error() string        { return "note cursor names more than one note" }
func (duplicateNoteID) Is(target error) bool { return target == ErrNoteCursor }

// ErrNoteBusy is what a page answers when the store's lock is not free to take
// at the moment of asking: held by anything else in this process (a writer,
// another reader, Close), or, while the mutex is in starvation mode, being
// handed from its holder straight to a queued waiter, when it is briefly
// unlocked but promised to that waiter. Both are real contention; an
// uncontended lock never answers this. IT IS NOT AN EMPTY PAGE AND IT DOES NOT WAIT: no page comes back, so
// the caller's cursor has not moved, and the caller asks again from the same
// After on its own schedule. A blocking wait here would sit behind a writer's
// whole load and save of the store, which no context can interrupt.
var ErrNoteBusy = errors.New("plan store is in use; ask for the note page again")

// ErrNoteTaskIDTooLarge is what a page answers for a task id longer than
// NotePageTaskIDMax, before validID and before the database is asked. IT IS NOT
// A CLAIM THAT THE TASK IS ABSENT: a root id is stored as given, so a padded
// root that long may be live; this says only that a page will not look it up.
// The id is neither echoed nor kept, and nothing is trimmed to make it fit.
var ErrNoteTaskIDTooLarge = errors.New("task id is longer than a note page will look up")

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
	// TaskID names the task exactly as the store keeps it, the way Notes
	// matches it: no "t-" is stripped and nothing is trimmed. Longer than
	// NotePageTaskIDMax is ErrNoteTaskIDTooLarge, whether or not it exists.
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
	// every field the page reads from the database for every note (the task id
	// is the caller's own and is not counted): 0 for NotePageBytesMax, otherwise
	// NotePageRowMax..NotePageBytesMax. A note is added only if the page stays
	// within it.
	MaxBytes int
	// Skip drops a note BEFORE it counts toward Limit and MaxBytes, which is
	// how a source's own notes and the already-accepted ones stay out of the
	// batch without a list of ids in the SQL. It runs AFTER the database read
	// has finished and the store's lock is released, on rows already in
	// memory, so it may call the store. A skipped note still moves Cursor. It
	// runs at most ScanMax times per call, and it may run on the one row after
	// a full page: false offers that row again next call, true moves the cursor
	// past it, which is right because a skipped row is never delivered. ITS
	// OWN COST IS THE CALLER'S: the page bounds how often it runs, not how
	// long it takes, and the caller's context is checked after it.
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
// the ones assembled into a Note. task_id IS NOT READ: the query's own
// task_id = ? proves the stored value is the caller's TaskID byte for byte, so
// the note carries the caller's string. That is also why a legal root id longer
// than NotePageFixedMax (the root is stored as given, see validID) is not
// refused as an oversized field.
var noteCols = [...]noteCol{
	{"id", NotePageFixedMax}, {"agent", NotePageMetaMax},
	{"body", NotePageBodyMax}, {"at", NotePageFixedMax}, {"project", NotePageMetaMax},
	{"chat", NotePageMetaMax}, {`"from"`, NotePageFixedMax}, {"client_id", NotePageMetaMax},
}

const (
	colID = iota
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
// LENGTH IS MEASURED IN THE QUERY, BEFORE THE VALUE IS COPIED: a value over its
// ceiling comes back as NULL, so the Go side never allocates it. octet_length
// counts bytes, not characters, and on a bare column SQLite (3.43 and later;
// the pinned driver bundles 3.45.1 on every release target) takes it from the
// record header without reading the value's overflow pages, and the CASE reads
// the value only when it fits, so THE SELECT LIST never loads an oversized
// value. That is the select list only: the WHERE comparisons on id and task_id
// load those two columns in full for every row they scan (see NotesPage). A
// driver pin without octet_length fails the query loudly, never silently.
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
		fmt.Fprintf(&b, "octet_length(%[1]s), CASE WHEN octet_length(%[1]s) <= %[2]d THEN %[1]s END", col.name, col.max)
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
// bytes buffered, the bytes copied out of any one field (a field over its
// ceiling is never copied out, and the select list measures it from its record
// header without loading it), the task id and cursor bound into the query, the
// size of every error, and the cursor's length. NOT bounded: the rest of the
// work SQLite does inside the read. `notes` has no index, so resolving a
// cursor (WHERE id = ?) and walking one task's rows (WHERE task_id = ?) scan
// the table, and those comparisons load the id and task_id of every row they
// scan in full, oversized ones included. That work is limited only by what the
// driver does with the caller's context; this is not a bounded-CPU walk. A (task_id, seq) index would shorten it and is separate engineering:
// it is a schema write, not made here.
//
// THE LOCK IS NEVER WAITED FOR. The store's mutex is held only while the
// database is read, which is what orders the read against Close: a closed
// store answers ErrClosed, and Close cannot release the handle under a read.
// The mutex is not context-aware, and whoever holds it may be a writer in the
// middle of loading and saving the whole store, so this call TRIES it once and
// answers ErrNoteBusy if anything holds it, with no page and the cursor where
// the caller left it. The price is stated: under steady traffic in this
// process a page can answer ErrNoteBusy where a blocking lock would have
// waited and succeeded. There is no retry, poll, timer, goroutine or pool in
// here. The context is checked before the lock is tried (a cancelled context
// answers its own error, never ErrNoteBusy), is handed to the read unchanged,
// and is checked again after Skip.
//
// A note over a field ceiling is ErrNoteTooLarge: delivered notes before it
// come back first, with More true and the cursor before it; the call that
// starts at it fails. Every failure returns an error and no page.
func (s *Store) NotesPage(ctx context.Context, ask NotePageAsk) (NotePage, error) {
	// The hard cap comes first, before validID reads the id and before any
	// bind copies it. Within the cap, an id the length of any id goes to the
	// database as it is; a longer one goes only if validID admits it, because
	// the root id is stored as given and validID trims a "t-" and spaces
	// before it measures, so such a root is live and must not be answered
	// not-found. Every stored task id passed validID when it was written, so
	// a long id it refuses cannot be found.
	if len(ask.TaskID) > NotePageTaskIDMax {
		return NotePage{}, ErrNoteTaskIDTooLarge
	}
	if ask.TaskID == "" || (len(ask.TaskID) > NotePageFixedMax && !validID(ask.TaskID)) {
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
	// TryLock, once, on purpose: see THE LOCK on NotesPage.
	if !s.mu.TryLock() {
		return nil, false, nil, ErrNoteBusy
	}
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
	floor, after, err := resolveNoteCursor(ctx, tx, ask.TaskID, ask.After)
	if err != nil {
		return nil, false, nil, err
	}
	return queryNoteRows(ctx, tx, ask.TaskID, floor, after, rowCap, byteCap)
}

// queryNoteRows reads up to rowCap notes of one task, after a table position
// when after is true and from the task's first row when it is not, stopping
// early at the byte cap or at a note over a field ceiling. A note read is
// complete; a note refused is never partly read.
func queryNoteRows(ctx context.Context, tx *sql.Tx, taskID string, floor int64, after bool, rowCap, byteCap int) (rows []noteRow, more bool, refused, err error) {
	keyed, err := hasColumn(tx, "notes", "client_id")
	if err != nil {
		return nil, false, nil, err
	}
	// The first page has NO position predicate rather than a floor below the
	// first row: seq is any integer a writer stored, and a guessed floor would
	// hide whatever sits under it. One row past the cap is asked for only to
	// learn whether more remain.
	where, args := `task_id = ?`, []any{taskID}
	if after {
		where, args = where+` AND seq > ?`, append(args, floor)
	}
	rs, err := tx.QueryContext(ctx, `SELECT `+noteSelect(keyed)+` FROM notes WHERE `+where+` ORDER BY seq LIMIT ?`,
		append(args, rowCap+1)...)
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
			ID: values[colID].String, TaskID: taskID, Agent: values[colAgent].String,
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
// which to read, inside the caller's snapshot, and reports whether there is
// one: "" is the start, with no position at all. An id that is absent or
// belongs to another task is ErrNoteCursor, and one that names two rows is
// ErrDuplicateNoteID whoever owns them. The position is only meaningful in the
// snapshot it was read in, and it is never returned to the caller. The owner
// is compared in SQL so that no stored task id, of whatever length, is copied
// out to be compared.
func resolveNoteCursor(ctx context.Context, tx *sql.Tx, taskID, after string) (floor int64, bounded bool, err error) {
	if after == "" {
		return 0, false, nil
	}
	rs, err := tx.QueryContext(ctx, `SELECT seq, task_id = ? FROM notes WHERE id = ? ORDER BY seq LIMIT 2`, taskID, after)
	if err != nil {
		return 0, false, err
	}
	defer rs.Close()
	var (
		seq   int64
		mine  bool
		found int
	)
	for rs.Next() {
		if err := rs.Scan(&seq, &mine); err != nil {
			return 0, false, err
		}
		found++
	}
	if err := rs.Err(); err != nil {
		return 0, false, err
	}
	switch {
	case found > 1:
		return 0, false, ErrDuplicateNoteID
	case found == 0 || !mine:
		return 0, false, ErrNoteCursor
	}
	return seq, true, nil
}
