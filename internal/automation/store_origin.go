package automation

import (
	"context"
	"fmt"
)

// MadeInTeamMax is the most rows one [Store.MadeInTeam] page returns.
// It is the wire's limit too: internal/appwire's OrdersMadeLimitMax must equal it.
const MadeInTeamMax = 100

// madeInTeam is the SQL for "this row's creation team is exactly ?". It reads
// ONLY the recorded [OriginTeam]: a row whose team was never recorded, or is
// not marked known, matches nothing. A damaged document matches nothing too,
// and is counted unknown below.
const madeInTeam = `json_valid(doc) AND json_extract(doc, '$.origin.team.known') = 1 AND json_extract(doc, '$.origin.team.id') = ?`

// teamUnknown is the SQL for "this row's creation team cannot be told": it was
// saved before the field existed, was made outside an accepted person message,
// or its document cannot be read.
const teamUnknown = `NOT json_valid(doc) OR COALESCE(json_extract(doc, '$.origin.team.known'), 0) != 1`

// MadeInTeam is one page of the automations whose recorded creation team is
// exactly teamID, in id order after the id `after` ("" for the first page),
// with whether more follow. It reads the recorded [Origin.Team] and nothing
// else: never the crew's membership, its manager now, or [Automation.Crew].
//
// THE PAGE IS BOUNDED TWICE: limit rows (at most [MadeInTeamMax]) and ctx,
// which cancels the query itself. Runs are not read, so Last is nil.
func (s *Store) MadeInTeam(ctx context.Context, teamID, after string, limit int) ([]Automation, bool, error) {
	if teamID == "" {
		return nil, false, fmt.Errorf("automations: no team asked for")
	}
	if limit < 1 || limit > MadeInTeamMax {
		return nil, false, fmt.Errorf("automations: a page is 1 to %d rows", MadeInTeamMax)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT doc, status, next_ms, seen, memo, revision FROM automations WHERE `+madeInTeam+` AND id > ? ORDER BY id LIMIT ?`, teamID, after, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	var page []Automation
	for rows.Next() {
		a, err := scanAutomation(rows)
		if err != nil {
			return nil, false, err
		}
		page = append(page, a)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(page) > limit
	if more {
		page = page[:limit]
	}
	return page, more, nil
}

// TeamUnknownCount is how many automations have no recorded creation team. They
// belong to no team's list and to no team's absence: a reader shows this count
// beside a team's rows so "none made here" is never read from rows that were
// never labelled.
func (s *Store) TeamUnknownCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM automations WHERE `+teamUnknown).Scan(&n)
	return n, err
}
