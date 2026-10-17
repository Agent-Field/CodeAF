package automation

import (
	"context"
	"fmt"
)

// MadeInTeamMax is the most automations one [Store.MadeInTeam] page returns.
// It is the wire's limit too: internal/appwire's OrdersMadeLimitMax must equal it.
const MadeInTeamMax = 100

// MadeInScanMax is the most stored rows one [Store.MadeInTeam] page reads,
// whatever its limit. THE STORE'S I/O IS BOUNDED, NOT ONLY THE ANSWER: a page
// that finds few matches still stops here and says where it stopped.
const MadeInScanMax = 500

// madeInHit is the SQL for "this row's recorded creation team is exactly ?",
// 1 or 0. It reads ONLY the recorded [OriginTeam]. The CASE runs json_valid
// first, so a document that is not JSON is a 0 here rather than an error that
// fails the whole read; it is counted unknown when the page decodes it.
const madeInHit = `IFNULL(CASE WHEN json_valid(doc) THEN (json_extract(doc, '$.origin.team.known') = 1 AND json_extract(doc, '$.origin.team.id') = ?) ELSE 0 END, 0)`

// MadeInPage is one page of [Store.MadeInTeam].
type MadeInPage struct {
	// Rows are the automations whose recorded creation team is the one asked
	// for, in id order. Last is nil: runs are not read.
	Rows []Automation
	// Unknown is how many rows IN THE RANGE THIS PAGE COVERED have no recorded
	// creation team: saved before the field, never marked known, or unreadable.
	// Pages cover disjoint ranges, so the sum over a whole walk is the store's.
	Unknown int
	// Next is the last stored id this page covered, to pass as after; "" ONLY
	// when the page reached the end of the store.
	Next string
}

// MadeInTeam is one page of the automations whose recorded creation team is
// exactly teamID, after the id `after` ("" for the first page), and how many
// rows in the same range have no recorded team. It reads the recorded
// [Origin.Team] and nothing else: never the crew's membership, its manager
// now, or [Automation.Crew].
//
// ONE STATEMENT, ONE SNAPSHOT: the rows and the unknown count come from the
// same read of at most [MadeInScanMax] stored rows, walked by primary key. The
// page is bounded three times — limit rows (at most [MadeInTeamMax]), the scan
// window, and ctx, which cancels the query itself.
//
// A ROW THAT MATCHES BUT CANNOT BE DECODED REFUSES THE PAGE: answering without
// it would be a list that silently lacks one of the team's automations. A row
// that does not match and cannot be decoded is counted unknown, because nothing
// can say whose it is.
func (s *Store) MadeInTeam(ctx context.Context, teamID, after string, limit int) (MadeInPage, error) {
	if teamID == "" {
		return MadeInPage{}, fmt.Errorf("automations: no team asked for")
	}
	if limit < 1 || limit > MadeInTeamMax {
		return MadeInPage{}, fmt.Errorf("automations: a page is 1 to %d rows", MadeInTeamMax)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, `+madeInHit+`, doc, status, next_ms, seen, memo, revision FROM automations WHERE id > ? ORDER BY id LIMIT ?`, teamID, after, MadeInScanMax)
	if err != nil {
		return MadeInPage{}, err
	}
	defer rows.Close()
	var page MadeInPage
	scanned, last, full := 0, "", false
	for rows.Next() {
		var id, doc, status, seen, memo string
		var hit int
		var next, revision int64
		if err := rows.Scan(&id, &hit, &doc, &status, &next, &seen, &memo, &revision); err != nil {
			return MadeInPage{}, err
		}
		scanned, last = scanned+1, id
		a, err := decodeAutomation(doc, status, next, seen, memo, revision)
		switch {
		case err != nil && hit == 1:
			return MadeInPage{}, fmt.Errorf("automations: %s was made in this team and cannot be read: %w", id, err)
		case err != nil, a.Origin.Team == nil, !a.Origin.Team.Known:
			page.Unknown++
		case a.Origin.Team.ID == teamID:
			page.Rows = append(page.Rows, a)
		}
		if len(page.Rows) == limit {
			full = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return MadeInPage{}, err
	}
	if full || scanned == MadeInScanMax {
		page.Next = last
	}
	return page, nil
}
