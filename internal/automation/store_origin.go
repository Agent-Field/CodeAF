package automation

import (
	"context"
	"database/sql"
	"encoding/base64"
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

var (
	// ErrMadeInStale refuses a page whose cursor was answered by a store that
	// has since gained, lost or retargeted an automation: the walk must start
	// again, or it could end complete while missing a row.
	ErrMadeInStale = errors.New("automations: the orders changed while they were being read; start again from the first page")
	// ErrMadeInTooLarge refuses a page whose next row cannot be carried. The
	// walk does not move past it.
	ErrMadeInTooLarge = errors.New("automations: an order is too large to read here")
	// ErrMadeInCursor refuses a cursor no page gave.
	ErrMadeInCursor = errors.New("automations: the cursor is not one this read gave")
)

// madeInHit is the SQL for "this row's recorded creation team is exactly ?4",
// 1 or 0, on the joined row a. The CASE runs json_valid first, so a document
// that is not JSON is a 0 here rather than an error that fails the read.
const madeInHit = `IFNULL(CASE WHEN json_valid(a.doc) THEN (json_extract(a.doc, '$.origin.team.known') = 1 AND json_extract(a.doc, '$.origin.team.id') = ?4) ELSE 0 END, 0)`

// madeInUnknown is the SQL for "this row has no recorded creation team": not
// JSON, never recorded, or not marked known.
const madeInUnknown = `IFNULL(CASE WHEN json_valid(a.doc) THEN COALESCE(json_extract(a.doc, '$.origin.team.known'), 0) != 1 ELSE 1 END, 1)`

// madeInQuery is one page in ONE statement, so the revision, the sizes, the
// classification and the matching rows are one snapshot of the store.
//
// win reads ids and sizes only: octet_length answers a column's byte length
// from the row's record header, so no document is read to size it. sized adds
// a running total. A row is classified only while the running total is within
// the scan budget (?3), so no document past the budget is looked into; and a
// row's columns cross into Go only when it matches AND its own size is within
// the caller's decode budget (?5), so a document no page could carry is never
// copied out of SQLite at all. The revision row is the left side of the join,
// so an empty page still answers the revision.
const madeInQuery = `
WITH win AS (
 SELECT id, octet_length(doc) + octet_length(seen) + octet_length(memo) AS size FROM automations WHERE id > ?1 ORDER BY id LIMIT ?2
), sized AS (
 SELECT id, size, SUM(size) OVER (ORDER BY id ROWS UNBOUNDED PRECEDING) AS running FROM win
), judged AS (
 SELECT s.id, s.size, s.running,
  CASE WHEN s.running <= ?3 THEN ` + madeInHit + ` ELSE 0 END AS hit,
  CASE WHEN s.running <= ?3 THEN ` + madeInUnknown + ` ELSE 0 END AS unknown
 FROM sized s JOIN automations a ON a.id = s.id
)
SELECT r.rev, j.id, j.size, j.running, j.hit, j.unknown,
 CASE WHEN j.hit = 1 AND j.size <= ?5 THEN a.doc END,
 CASE WHEN j.hit = 1 AND j.size <= ?5 THEN a.status END,
 CASE WHEN j.hit = 1 AND j.size <= ?5 THEN a.next_ms END,
 CASE WHEN j.hit = 1 AND j.size <= ?5 THEN a.seen END,
 CASE WHEN j.hit = 1 AND j.size <= ?5 THEN a.memo END,
 CASE WHEN j.hit = 1 AND j.size <= ?5 THEN a.revision END
FROM origin_revision r
LEFT JOIN judged j ON 1
LEFT JOIN automations a ON a.id = j.id
WHERE r.id = 1
ORDER BY j.id`

// MadeInEntry is one stored row a page covered, in id order.
type MadeInEntry struct {
	ID string
	// Size is the row's stored bytes: its document, seen mark and memo.
	Size int64
	// Unknown says the row has no recorded creation team.
	Unknown bool
	// Row is the automation when its recorded creation team is the one asked
	// for; nil otherwise. Last is nil: runs are not read.
	Row *Automation
}

// MadeInPage is one page of [Store.MadeInTeam].
type MadeInPage struct {
	// Entries are the rows covered, in id order. A reader may stop early — at
	// a byte budget of its own — and end its page at any entry's id with
	// [MadeInPage.CursorAt]; it must then count only the entries it kept.
	Entries []MadeInEntry
	// Revision is the walk revision this page was read at.
	Revision int64
	// End says the covered rows reached the end of the store. IT IS NEVER SET
	// BY A STOP: a page that ended at a budget, a limit or a full window is
	// not the end, whatever follows it.
	End bool
	// Stop is the id of the row the page ended before because it could not be
	// carried — past the scan budget, or a match larger than the decode
	// budget — and "" when no row stopped it. The next page starts at it.
	Stop string
}

// CursorAt is the cursor that continues after the entry with id, at this
// page's revision, or an error when the id cannot fit in a cursor.
func (p MadeInPage) CursorAt(id string) (string, error) {
	cursor := "m1." + strconv.FormatInt(p.Revision, 10) + "." + base64.RawURLEncoding.EncodeToString([]byte(id))
	if len(cursor) > MadeInCursorMax {
		return "", fmt.Errorf("%w: %s's id is too long for a cursor", ErrMadeInTooLarge, id)
	}
	return cursor, nil
}

// parseMadeInCursor is a cursor's revision and the id it continues after.
func parseMadeInCursor(cursor string) (int64, string, error) {
	if len(cursor) > MadeInCursorMax {
		return 0, "", ErrMadeInCursor
	}
	parts := strings.Split(cursor, ".")
	if len(parts) != 3 || parts[0] != "m1" {
		return 0, "", ErrMadeInCursor
	}
	rev, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || rev < 0 {
		return 0, "", ErrMadeInCursor
	}
	id, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(id) == 0 {
		return 0, "", ErrMadeInCursor
	}
	return rev, string(id), nil
}

// MadeInTeam is one page of the automations whose recorded creation team is
// exactly teamID, continuing cursor ("" for the first page). It reads the
// recorded [Origin.Team] and nothing else: never the crew's membership, its
// manager now, or [Automation.Crew].
//
// ONE STATEMENT, ONE SNAPSHOT, BOUNDED BEFORE IT IS READ: the page covers at
// most [MadeInScanMax] rows and [MadeInScanBytesMax] stored bytes, sized from
// the rows' headers before any of them is looked into; a match crosses into Go
// only when its own stored bytes are at most docMax; it holds at most limit
// matches; and ctx cancels the query itself.
//
// A CURSOR FROM ANOTHER REVISION IS REFUSED as [ErrMadeInStale]. A row that
// cannot be carried is never skipped and never mistaken for the end: a page
// that covered rows before it ends there ([MadeInPage.Stop] names it), and a
// page that would start at it is refused with [ErrMadeInTooLarge], so the
// cursor never moves past it. A row that matches but cannot be decoded
// refuses the page.
func (s *Store) MadeInTeam(ctx context.Context, teamID, cursor string, limit int, docMax int64) (MadeInPage, error) {
	if teamID == "" {
		return MadeInPage{}, fmt.Errorf("automations: no team asked for")
	}
	if limit < 1 || limit > MadeInTeamMax {
		return MadeInPage{}, fmt.Errorf("automations: a page is 1 to %d rows", MadeInTeamMax)
	}
	if docMax < 1 || docMax > MadeInScanBytesMax {
		return MadeInPage{}, fmt.Errorf("automations: a decode budget is 1 to %d bytes", MadeInScanBytesMax)
	}
	after, rev, continuing := "", int64(0), cursor != ""
	if continuing {
		var err error
		if rev, after, err = parseMadeInCursor(cursor); err != nil {
			return MadeInPage{}, err
		}
	}
	rows, err := s.db.QueryContext(ctx, madeInQuery, after, MadeInScanMax, MadeInScanBytesMax, teamID, docMax)
	if err != nil {
		return MadeInPage{}, err
	}
	defer rows.Close()
	var page MadeInPage
	window, matched, stopped := 0, 0, false
	for rows.Next() {
		var revision int64
		var id, doc, status, seen, memo sql.NullString
		var size, running, next, rowRevision sql.NullInt64
		var hit, unknown int
		if err := rows.Scan(&revision, &id, &size, &running, &hit, &unknown, &doc, &status, &next, &seen, &memo, &rowRevision); err != nil {
			return MadeInPage{}, err
		}
		page.Revision = revision
		if continuing && revision != rev {
			return MadeInPage{}, ErrMadeInStale
		}
		if !id.Valid {
			// The revision row alone: nothing after the cursor.
			break
		}
		window++
		if stopped {
			continue
		}
		if running.Int64 > MadeInScanBytesMax || (hit == 1 && !doc.Valid) {
			// THE BUDGET ENDS THE PAGE BEFORE THIS ROW, never past it and never
			// by skipping it.
			page.Stop, stopped = id.String, true
			continue
		}
		entry := MadeInEntry{ID: id.String, Size: size.Int64, Unknown: unknown == 1}
		if hit == 1 {
			a, err := decodeAutomation(doc.String, status.String, next.Int64, seen.String, memo.String, rowRevision.Int64)
			if err != nil || a.Origin.Team == nil || a.Origin.Team.ID != teamID {
				return MadeInPage{}, fmt.Errorf("automations: %s was made in this team and cannot be read: %v", id.String, err)
			}
			entry.Row = &a
			matched++
		}
		page.Entries = append(page.Entries, entry)
		if matched == limit {
			stopped = true
		}
	}
	if err := rows.Err(); err != nil {
		return MadeInPage{}, err
	}
	if len(page.Entries) == 0 && page.Stop != "" {
		return MadeInPage{}, fmt.Errorf("%w: %s holds more than this read can carry", ErrMadeInTooLarge, page.Stop)
	}
	// The end is reached only when nothing stopped the page, the window held
	// fewer rows than it could, and every row it held was covered.
	page.End = !stopped && window < MadeInScanMax && len(page.Entries) == window
	return page, nil
}
