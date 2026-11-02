package automation

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// MadeInTeamMax is the most automations one [Store.MadeInTeam] page returns.
// It is the wire's limit too: internal/appwire's OrdersMadeLimitMax equals it,
// and a test pins the two.
const MadeInTeamMax = 100

// MadeInScanMax is the most stored rows one [Store.MadeInTeam] page covers.
const MadeInScanMax = 500

// MadeInScanBytesMax is the most stored document bytes one page reads,
// whatever its limit. THE STORE'S I/O IS BOUNDED, NOT ONLY THE ANSWER: the
// sizes are read first, from the rows' headers, and only the prefix of rows
// that fits is classified and decoded.
const MadeInScanBytesMax = 2 << 20

// MadeInCursorMax is the longest cursor a page answers, in bytes.
const MadeInCursorMax = 128

// MadeInTeamIDMax is the longest team id, the teams directory-name law's.
const MadeInTeamIDMax = 64

// madeInRevDigitsMax is the most decimal digits a revision takes.
const madeInRevDigitsMax = 19

// MadeInIDMax is the longest automation id a page reads, in bytes: the most a
// cursor can carry beside its version, the revision and the longest team id.
// An id longer than this is never copied out of the store, encoded or echoed;
// the page ends before its row, and a page that would start at it is refused.
const MadeInIDMax = (MadeInCursorMax - len(madeInCursorVersion) - 1 - madeInRevDigitsMax - 1 - MadeInTeamIDMax - 1) * 3 / 4

// madeInCursorVersion is the cursor's first part. m1 was never released.
const madeInCursorVersion = "m2"

// madeInStatusMax is the longest status column a page reads; every state the
// store writes is shorter.
const madeInStatusMax = 16

var (
	// ErrMadeInStale refuses a page whose cursor was answered by a store that
	// has since gained, lost or retargeted an automation: the walk must start
	// again, or it could end complete while missing a row.
	ErrMadeInStale = errors.New("automations: the orders changed while they were being read; start again from the first page")
	// ErrMadeInTooLarge refuses a page whose first row cannot be carried. The
	// walk does not move past it.
	ErrMadeInTooLarge = errors.New("automations: an order is too large to read here")
	// ErrMadeInCursor refuses a cursor no page of this team's walk gave.
	ErrMadeInCursor = errors.New("automations: the cursor is not one this read gave")
	// ErrMadeInDamaged refuses a page the store cannot answer honestly: the
	// walk revision is missing, or a row made in the team holds a status, a
	// wake time or a document this build cannot read.
	ErrMadeInDamaged = errors.New("automations: the store cannot be read here")
)

// madeInHit is the SQL for "this row's recorded creation team is exactly ?4",
// 1 or 0, on the joined row a. The CASE runs json_valid first, so a document
// that is not JSON is a 0 here rather than an error that fails the read.
const madeInHit = `IFNULL(CASE WHEN json_valid(a.doc) THEN (json_extract(a.doc, '$.origin.team.known') = 1 AND json_extract(a.doc, '$.origin.team.id') = ?4) ELSE 0 END, 0)`

// madeInUnknown is the SQL for "this row has no recorded creation team": not
// JSON, never recorded, or not marked known.
const madeInUnknown = `IFNULL(CASE WHEN json_valid(a.doc) THEN COALESCE(json_extract(a.doc, '$.origin.team.known'), 0) != 1 ELSE 1 END, 1)`

// madeInInspected is the SQL for "this row may be looked into at all": its id
// fits a cursor and its document is inside the page's scan budget, both read
// from the row's header before anything else of it is.
const madeInInspected = `s.idlen <= ?6 AND s.running <= ?3`

// madeInProjection is the SQL for the part of a matching row's document an
// order's view reads, and nothing else: no words, memo, seen mark, revision,
// update time, grant, last run or the look's content. Each value is taken
// with ->, which keeps it JSON, so json_object embeds it as it was. A watch's
// memo — a column, and a copy of it an edit leaves in the document — never
// crosses into Go, so an ordinary large memo cannot block a small order.
// ADD A FIELD HERE IN THE SAME CHANGE that makes the order's view read it; a
// test pins the two.
const madeInProjection = `json_object(
 'title', a.doc -> '$.title', 'crew', a.doc -> '$.crew', 'workspace', a.doc -> '$.workspace',
 'schedule', a.doc -> '$.schedule', 'wont', a.doc -> '$.wont', 'answersIn', a.doc -> '$.answersIn',
 'trust', a.doc -> '$.trust', 'approved', a.doc -> '$.approved', 'created', a.doc -> '$.created',
 'limits', a.doc -> '$.limits',
 'look', CASE WHEN json_type(a.doc, '$.look') = 'object' THEN json('{}') END,
 'action', json_object('do', a.doc -> '$.action.do', 'say', a.doc -> '$.action.say'),
 'origin', json_object('sessionId', a.doc -> '$.origin.sessionId', 'transcript', a.doc -> '$.origin.transcript', 'team', a.doc -> '$.origin.team'))`

// madeInCarried is the SQL for "this matching row's projection is carried":
// it was made, and the projections up to it fit the page's budget (?5).
const madeInCarried = `(c.p IS NOT NULL AND c.prun <= ?5)`

// madeInQuery is one page in ONE statement, so the revision, the sizes, the
// classification and the carried rows are one snapshot of the store.
//
// SIZES BEFORE CONTENT. win reads ids and sizes only: octet_length answers a
// column's byte length from the row's record header, so no document is read to
// size it. A row is inspected (json) only while its id fits and the running
// document total is within the scan budget (?3); a matching row is projected
// only while it is within the limit (?7); and a projection crosses into Go only
// while the running projection total is within ?5. The status and the wake
// time are read only for a carried row, the status only when its header says
// it is short. The anchor (SELECT 1) makes the statement answer one row even
// for an empty window, so a missing revision row is an error, never an end.
const madeInQuery = `
WITH win AS (
 SELECT rowid AS rid, id, octet_length(id) AS idlen, octet_length(doc) AS doclen
 FROM automations WHERE id > ?1 ORDER BY id LIMIT ?2
), sized AS (
 SELECT rid, id, idlen, doclen, SUM(doclen) OVER (ORDER BY id ROWS UNBOUNDED PRECEDING) AS running FROM win
), judged AS (
 SELECT s.rid, s.id, s.idlen, s.doclen, s.running,
  CASE WHEN ` + madeInInspected + ` THEN ` + madeInHit + ` ELSE 0 END AS hit,
  CASE WHEN ` + madeInInspected + ` THEN ` + madeInUnknown + ` ELSE 0 END AS unk
 FROM sized s JOIN automations a ON a.rowid = s.rid
), ranked AS (
 SELECT j.*, SUM(j.hit) OVER (ORDER BY j.id ROWS UNBOUNDED PRECEDING) AS rank FROM judged j
), projected AS MATERIALIZED (
 SELECT r.*, CASE WHEN r.hit = 1 AND r.rank <= ?7 THEN ` + madeInProjection + ` END AS p
 FROM ranked r JOIN automations a ON a.rowid = r.rid
), carried AS (
 SELECT q.*, SUM(octet_length(q.p)) OVER (ORDER BY q.id ROWS UNBOUNDED PRECEDING) AS prun FROM projected q
)
SELECT (SELECT CASE WHEN typeof(rev) = 'integer' THEN rev END FROM origin_revision WHERE id = 1),
 c.rid IS NOT NULL, c.idlen, c.doclen, c.running,
 CASE WHEN c.idlen <= ?6 THEN c.id END,
 c.hit, c.unk, c.rank, ` + madeInCarried + `,
 CASE WHEN ` + madeInCarried + ` THEN c.p END,
 CASE WHEN ` + madeInCarried + ` AND octet_length(a.status) <= ?8 AND a.status IN ('active', 'paused', 'finished', 'proposed', 'stopped') THEN a.status END,
 CASE WHEN ` + madeInCarried + ` AND typeof(a.next_ms) = 'integer' THEN a.next_ms END
FROM (SELECT 1) one
LEFT JOIN carried c ON 1
LEFT JOIN automations a ON a.rowid = c.rid
ORDER BY c.id`

// MadeInEntry is one stored row a page covered, in id order.
type MadeInEntry struct {
	// ID is the row's id, at most [MadeInIDMax] bytes.
	ID string
	// Size is the row's stored document bytes, from its header.
	Size int64
	// Unknown says the row has no recorded creation team.
	Unknown bool
	// Row is the automation when its recorded creation team is the one asked
	// for; nil otherwise. It holds ONLY what an order's view reads: Seen, Memo,
	// Revision and Last are zero, because they are never read.
	Row *Automation
}

// MadeInPage is one page of [Store.MadeInTeam].
type MadeInPage struct {
	// Entries are the rows covered, in id order. A reader may stop early — at
	// a byte budget of its own — and end its page at any entry's id with
	// [MadeInPage.CursorAt]; it must then count only the entries it kept.
	Entries []MadeInEntry
	// Team is the team the page was read for; its cursors carry it.
	Team string
	// Revision is the walk revision this page was read at.
	Revision int64
	// End says the covered rows reached the end of the store. IT IS NEVER SET
	// BY A STOP: a page that ended at a budget, a limit or a full window is
	// not the end, whatever follows it.
	End bool
	// Stopped says the page ended before a row it could not carry — an id too
	// long to read, a document past the scan budget, or a match past the
	// projection budget. The next page starts at that row.
	Stopped bool
	// StopID is that row's id when it can be read, "" when the id itself is
	// what is too long.
	StopID string
}

// CursorAt is the cursor that continues after the entry with id, bound to
// this page's team and revision, or an error when it cannot be one.
func (p MadeInPage) CursorAt(id string) (string, error) {
	if id == "" || len(id) > MadeInIDMax {
		return "", fmt.Errorf("%w: an order's id is too long for a cursor", ErrMadeInTooLarge)
	}
	cursor := madeInCursorVersion + "." + strconv.FormatInt(p.Revision, 10) + "." + p.Team + "." + base64.RawURLEncoding.EncodeToString([]byte(id))
	if len(cursor) > MadeInCursorMax {
		return "", fmt.Errorf("%w: an order's id is too long for a cursor", ErrMadeInTooLarge)
	}
	return cursor, nil
}

// parseMadeInCursor is a cursor's revision and the id it continues after. A
// cursor is one team's: ANOTHER TEAM'S IS REFUSED, never read as this one's.
func parseMadeInCursor(cursor, teamID string) (int64, string, error) {
	if len(cursor) > MadeInCursorMax {
		return 0, "", ErrMadeInCursor
	}
	parts := strings.Split(cursor, ".")
	if len(parts) != 4 || parts[0] != madeInCursorVersion || parts[2] != teamID {
		return 0, "", ErrMadeInCursor
	}
	if len(parts[1]) == 0 || len(parts[1]) > madeInRevDigitsMax {
		return 0, "", ErrMadeInCursor
	}
	rev, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || rev < 0 {
		return 0, "", ErrMadeInCursor
	}
	id, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(id) == 0 || len(id) > MadeInIDMax {
		return 0, "", ErrMadeInCursor
	}
	return rev, string(id), nil
}

// madeInTeamOK is the teams directory-name law (letters, digits, _ and -, at
// most [MadeInTeamIDMax] bytes): a team id a cursor can carry.
func madeInTeamOK(id string) bool {
	if id == "" || len(id) > MadeInTeamIDMax {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// MadeInTeam is one page of the automations whose recorded creation team is
// exactly teamID, continuing cursor ("" for the first page). It reads the
// recorded [Origin.Team] and nothing else: never the crew's membership, its
// manager now, or [Automation.Crew].
//
// ONE STATEMENT, ONE SNAPSHOT, BOUNDED BEFORE IT IS READ: the page covers at
// most [MadeInScanMax] rows and [MadeInScanBytesMax] stored document bytes,
// sized from the rows' headers before any of them is looked into; ids longer
// than [MadeInIDMax] are never read out; it holds at most limit matches; only
// the part of a match an order's view reads is projected, and at most projMax
// bytes of projections cross into Go; and ctx cancels the query itself.
//
// A CURSOR FROM ANOTHER REVISION IS REFUSED as [ErrMadeInStale], one from
// another team or of another shape as [ErrMadeInCursor]. A row that cannot be
// carried is never skipped and never mistaken for the end: a page that covered
// rows before it ends there ([MadeInPage.Stopped]), and a page that would
// start at it is refused with [ErrMadeInTooLarge], so the cursor never moves
// past it. THIS INCLUDES ANOTHER TEAM'S DOCUMENT LARGER THAN THE SCAN BUDGET:
// every team's walk stops at it until it shrinks, which is the honest cost of
// a read that inspects documents within a bound. A missing walk revision, or
// a match whose status, wake time or projection cannot be read, refuses the
// page with [ErrMadeInDamaged].
func (s *Store) MadeInTeam(ctx context.Context, teamID, cursor string, limit int, projMax int64) (MadeInPage, error) {
	if err := madeInAsk(teamID, limit, projMax); err != nil {
		return MadeInPage{}, err
	}
	walk := madeInWalk{page: MadeInPage{Team: teamID}, limit: limit, continuing: cursor != ""}
	after := ""
	if walk.continuing {
		var err error
		if walk.rev, after, err = parseMadeInCursor(cursor, teamID); err != nil {
			return MadeInPage{}, err
		}
	}
	rows, err := s.db.QueryContext(ctx, madeInQuery, after, MadeInScanMax, MadeInScanBytesMax, teamID, projMax, MadeInIDMax, limit, madeInStatusMax)
	if err != nil {
		return MadeInPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var row madeInScanned
		if err := row.scan(rows); err != nil {
			return MadeInPage{}, err
		}
		done, err := walk.read(row)
		if err != nil {
			return MadeInPage{}, err
		}
		if done {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return MadeInPage{}, err
	}
	return walk.finish(after)
}

// madeInAsk refuses a page asked for with no readable team, a limit outside
// one page or a projection budget outside the scan budget.
func madeInAsk(teamID string, limit int, projMax int64) error {
	if !madeInTeamOK(teamID) {
		return fmt.Errorf("automations: no team asked for")
	}
	if limit < 1 || limit > MadeInTeamMax {
		return fmt.Errorf("automations: a page is 1 to %d rows", MadeInTeamMax)
	}
	if projMax < 1 || projMax > MadeInScanBytesMax {
		return fmt.Errorf("automations: a projection budget is 1 to %d bytes", MadeInScanBytesMax)
	}
	return nil
}

// madeInScanned is one row of [madeInQuery], column for column.
type madeInScanned struct {
	revision, idlen, doclen, running, rank, hit, unknown, carry, next sql.NullInt64
	present                                                           bool
	id, projection, status                                            sql.NullString
}

func (r *madeInScanned) scan(rows *sql.Rows) error {
	return rows.Scan(&r.revision, &r.present, &r.idlen, &r.doclen, &r.running, &r.id, &r.hit, &r.unknown, &r.rank, &r.carry, &r.projection, &r.status, &r.next)
}

// madeInWalk is one page being read from the statement's rows: what it has
// covered so far and whether something has already ended it.
type madeInWalk struct {
	page       MadeInPage
	rev        int64
	continuing bool
	limit      int
	answered   bool
	window     int
	matched    int
	stopped    bool
}

// read takes one answered row. Every row carries the walk revision, so the
// first one is enough to refuse a missing or stale revision; done says the row
// was the anchor alone, with nothing after the cursor.
func (w *madeInWalk) read(row madeInScanned) (done bool, err error) {
	w.answered = true
	if !row.revision.Valid {
		return false, fmt.Errorf("%w: the walk revision is missing", ErrMadeInDamaged)
	}
	w.page.Revision = row.revision.Int64
	if w.continuing && row.revision.Int64 != w.rev {
		return false, ErrMadeInStale
	}
	if !row.present {
		return true, nil
	}
	w.window++
	if w.stopped {
		return false, nil
	}
	return false, w.cover(row)
}

// cover adds one row of the window to the page, or ends the page before it
// when the row cannot be carried.
func (w *madeInWalk) cover(row madeInScanned) error {
	matches := row.hit.Int64 == 1
	if !row.id.Valid || row.running.Int64 > MadeInScanBytesMax || (matches && row.carry.Int64 != 1) {
		w.stopBefore(row.id)
		return nil
	}
	entry := MadeInEntry{ID: row.id.String, Size: row.doclen.Int64, Unknown: row.unknown.Int64 == 1}
	if matches {
		a, err := madeInRow(row.id.String, row.projection, row.status, row.next, w.page.Team)
		if err != nil {
			return err
		}
		entry.Row = &a
		w.matched++
	}
	w.page.Entries = append(w.page.Entries, entry)
	if w.matched == w.limit {
		w.stopped = true
	}
	return nil
}

// stopBefore ends the page before a row it cannot carry: THE PAGE ENDS BEFORE
// THIS ROW, never past it and never by skipping it. Its id is named only when
// it can be read.
func (w *madeInWalk) stopBefore(id sql.NullString) {
	w.page.Stopped, w.stopped = true, true
	if id.Valid {
		w.page.StopID = id.String
	}
}

// finish is the page once its rows are read, or the refusal of a page that
// could not be answered or would start at a row it cannot carry.
func (w *madeInWalk) finish(after string) (MadeInPage, error) {
	if !w.answered {
		return MadeInPage{}, fmt.Errorf("%w: the walk revision is missing", ErrMadeInDamaged)
	}
	if len(w.page.Entries) == 0 && w.page.Stopped {
		return MadeInPage{}, madeInTooLarge(w.page.StopID, after)
	}
	// The end is reached only when nothing stopped the page, the window held
	// fewer rows than it could, and every row it held was covered.
	w.page.End = !w.stopped && w.window < MadeInScanMax && len(w.page.Entries) == w.window
	return w.page, nil
}

// madeInRow is a carried match as an automation, from its projection and the
// two columns the clock owns, or [ErrMadeInDamaged] naming it.
func madeInRow(id string, projection, status sql.NullString, next sql.NullInt64, teamID string) (Automation, error) {
	if !projection.Valid || !status.Valid || !next.Valid {
		return Automation{}, fmt.Errorf("%w: %s was made in this team and its status, wake time or document cannot be read", ErrMadeInDamaged, id)
	}
	var a Automation
	if err := json.Unmarshal([]byte(projection.String), &a); err != nil {
		return Automation{}, fmt.Errorf("%w: %s was made in this team and its document cannot be read", ErrMadeInDamaged, id)
	}
	if a.Origin.Team == nil || !a.Origin.Team.Known || a.Origin.Team.ID != teamID {
		return Automation{}, fmt.Errorf("%w: %s was made in this team and its document says otherwise", ErrMadeInDamaged, id)
	}
	a.ID, a.Status, a.Next = id, Status(status.String), fromMS(next.Int64)
	return a, nil
}

// madeInTooLarge is the refusal of a page that would start at a row it cannot
// carry. It names the row by id when the id can be read, else by where it is —
// after the cursor's id, at most [MadeInIDMax] bytes — and never echoes an id
// too long to read.
func madeInTooLarge(stopID, after string) error {
	switch {
	case stopID != "":
		return fmt.Errorf("%w: %s cannot be carried by one page", ErrMadeInTooLarge, stopID)
	case after != "":
		return fmt.Errorf("%w: the order after %s has an id too long to read", ErrMadeInTooLarge, after)
	}
	return fmt.Errorf("%w: the first order has an id too long to read", ErrMadeInTooLarge)
}
