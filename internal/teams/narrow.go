package teams

// narrow.go is what a reader that does not need every team reads instead.
//
// THE STORE GROWS AND ITS HOT READERS MUST NOT. Every factory job adds a team
// and none goes away, so a whole-list [Load] costs more each month: it copies
// every team, closed and old ones included, and repairs the lot. A reader on a
// beat, or on every turn, asks for what it shows instead:
//
//   - [LoadOpen] is the open teams only. An open team's parent is open and the
//     root is never closed, so the open teams are a whole tree: settings
//     inherit, managers climb and homes resolve inside it.
//   - [LoadTeams] is the teams it names, by id, in the store's order;
//     [LoadLineage] adds every team above them, [LoadSubtree] every team
//     below one, each a walk of the parent index.
//   - [LoadTeamsOf] and [LoadFoundedBy] are a conversation's teams (conv.go).
//   - The pages ([PageTeams], [PageMembers]) are one level, cut in SQL.
//
// Each answers the teams as stored, without briefs and without a repair: the
// store tidies the whole list on every write, so what it holds is in order.
// Like [Load], each answer is the caller's own copy to change.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// LoadOpen is the store's open teams, in its order. A missing store is no
// teams and no error.
func LoadOpen(profileDir string) (*File, error) {
	s, err := readOpenPath(context.Background(), Path(profileDir))
	if err != nil || s == nil {
		return &File{Version: Version, localIdentities: true}, err
	}
	return s.file(), nil
}

// LoadTeams is the teams ids name, open or closed, in the store's order; an
// id the store does not hold is left out. A missing store is no teams.
func LoadTeams(profileDir string, ids ...string) (*File, error) {
	f, _, err := lookTeams(profileDir, "", ids...)
	return f, err
}

// LoadLineage is the teams ids name and every team above each, in the
// store's order: what a setting inherits from and a decision climbs through.
func LoadLineage(profileDir string, ids ...string) (*File, error) {
	f, _, err := lookTeams(profileDir, lineageQuery, ids...)
	return f, err
}

// LoadSubtree is team id and every team below it, open or closed, in the
// store's order: what a team's spend is summed over.
func LoadSubtree(profileDir, id string) (*File, error) {
	f, _, err := lookTeams(profileDir, subtreeQuery, id)
	return f, err
}

// The walks up and down the tree, each over the parent index. %s is the
// placeholders of the ids the walk starts from.
const (
	lineageQuery = `WITH RECURSIVE up(id) AS (SELECT id FROM teams WHERE id IN (%s)
 UNION SELECT t.parent FROM teams t JOIN up ON t.id = up.id WHERE t.parent <> '') SELECT id FROM up`
	subtreeQuery = `WITH RECURSIVE down(id) AS (SELECT id FROM teams WHERE id IN (%s)
 UNION SELECT t.id FROM teams t JOIN down ON t.parent = down.id) SELECT id FROM down`
)

// lookTeams is the teams ids name (or that walk, a query over them, finds)
// and whether there is a store at all: a writer that checks a team only when
// there are teams tells the two apart.
func lookTeams(profileDir, walk string, ids ...string) (*File, bool, error) {
	out := &File{Version: Version, localIdentities: true}
	ids = distinct(ids)
	path := Path(profileDir)
	h, err := acquire(path, false)
	if err != nil || h == nil {
		return out, false, err
	}
	defer h.release()
	if len(ids) == 0 {
		return out, true, nil
	}
	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, true, unreadable(err, path)
	}
	defer tx.Rollback()
	if walk != "" {
		marks, args := placeholders(ids)
		r := &pageReader{ctx: ctx, q: tx, path: path}
		if ids, err = r.strings(fmt.Sprintf(walk, marks), args...); err != nil {
			return out, true, err
		}
	}
	s := &snapshot{rows: map[string]*teamRows{}}
	for len(ids) > 0 {
		n := min(len(ids), 400)
		if err := loadTeamsIn(ctx, tx, path, s, ids[:n]); err != nil {
			return out, true, err
		}
		ids = ids[n:]
	}
	order := make([]int, len(s.teams))
	for i := range order {
		order[i] = i
	}
	pos := make([]int64, len(s.teams))
	for _, r := range s.rows {
		pos[r.at] = r.pos
	}
	sort.SliceStable(order, func(a, b int) bool { return pos[order[a]] < pos[order[b]] })
	for _, i := range order {
		out.Teams = append(out.Teams, s.teams[i].Clone())
	}
	return out, true, nil
}

// loadTeamsIn decodes the teams ids name into s.
func loadTeamsIn(ctx context.Context, q querier, path string, s *snapshot, ids []string) error {
	marks, args := placeholders(ids)
	list, err := q.QueryContext(ctx, "SELECT id, pos, rev FROM teams WHERE id IN ("+marks+")", args...)
	if err != nil {
		return unreadable(err, path)
	}
	var found []string
	for list.Next() {
		var id string
		var pos, rev int64
		if err := list.Scan(&id, &pos, &rev); err != nil {
			list.Close()
			return unreadable(err, path)
		}
		s.rows[id] = &teamRows{pos: pos, rev: rev, at: len(s.teams)}
		s.teams = append(s.teams, Team{ID: id})
		found = append(found, id)
	}
	if err := list.Close(); err != nil {
		return unreadable(err, path)
	}
	if len(found) == 0 {
		return nil
	}
	return decodeTeams(ctx, q, path, s, found)
}

func distinct(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := ids[:0:0]
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// LoadRoot is the team that holds every other, and false when there is none.
// Its index makes it one row however many teams there are.
func LoadRoot(profileDir string) (Team, bool, error) {
	var root *Team
	err := readPage(context.Background(), profileDir, func(r *pageReader) error {
		var err error
		_, root, err = r.head()
		return err
	})
	if err != nil || root == nil {
		return Team{}, false, err
	}
	return root.Clone(), true, nil
}

// LoadAround is what a view of one team reads: team id, every team above and
// below it, and every other team a member of those is in, where its home
// membership may be ([File.Home]). It costs what that team's own corner of
// the tree holds, however many teams there are elsewhere. A store whose
// conversation names an earlier build left behind ([reindexIn]) is read whole.
func LoadAround(profileDir, id string) (*File, error) {
	var ids []string
	err := readPage(context.Background(), profileDir, func(r *pageReader) error {
		if r.missing {
			return nil
		}
		var rev, indexed int64
		if err := r.q.QueryRowContext(r.ctx, "SELECT rev, indexed FROM meta WHERE id = 1").Scan(&rev, &indexed); err != nil {
			return r.fail(err)
		}
		if rev != indexed {
			ids = nil
			return errReadWhole
		}
		var err error
		ids, err = r.strings(`WITH RECURSIVE
 down(id) AS (SELECT id FROM teams WHERE id = ?1 UNION SELECT t.id FROM teams t JOIN down ON t.parent = down.id),
 up(id) AS (SELECT id FROM teams WHERE id = ?1 UNION SELECT t.parent FROM teams t JOIN up ON t.id = up.id WHERE t.parent <> '')
SELECT id FROM down UNION SELECT id FROM up
 UNION SELECT DISTINCT o.team_id FROM members m JOIN members o ON o.conv = m.conv AND o.former = 0
  WHERE m.former = 0 AND m.conv <> '-' AND m.team_id IN (SELECT id FROM down)`, id)
		return err
	})
	if errors.Is(err, errReadWhole) {
		return Snapshot(profileDir)
	}
	if err != nil {
		return &File{Version: Version, localIdentities: true}, err
	}
	return LoadTeams(profileDir, ids...)
}

// errReadWhole is a narrow read finding it must read the whole store.
var errReadWhole = errors.New("teams: read the whole store")

// LoadClosedCrews is every closed team the factory did not make, archived or
// not, with what its members' rows are drawn from ([LoadWithHomes]). A
// finished factory job's room is closed too, and there is one for every job
// the factory ever ran: those are read by id, where a job or a tab names one,
// never listed.
func LoadClosedCrews(profileDir string) (*File, error) {
	return loadWithHomes(profileDir, "SELECT id FROM teams WHERE closed = 1 AND factory = 0")
}

// LoadWithHomes is the teams ids name with what their members' rows are drawn
// from: every team above them (what their settings inherit), every open team
// their members are also in (where a member is also, and where its home and
// so its manager may be), and every team above those. A store whose
// conversation names an earlier build left behind ([reindexIn]) is read whole.
func LoadWithHomes(profileDir string, ids ...string) (*File, error) {
	ids = distinct(ids)
	if len(ids) == 0 {
		return &File{Version: Version, localIdentities: true}, nil
	}
	marks, args := placeholders(ids)
	return loadWithHomes(profileDir, "SELECT id FROM teams WHERE id IN ("+marks+")", args...)
}

// loadWithHomes is [LoadWithHomes] of the teams base selects.
func loadWithHomes(profileDir, base string, args ...any) (*File, error) {
	var ids []string
	err := readPage(context.Background(), profileDir, func(r *pageReader) error {
		if r.missing {
			return nil
		}
		var rev, indexed int64
		if err := r.q.QueryRowContext(r.ctx, "SELECT rev, indexed FROM meta WHERE id = 1").Scan(&rev, &indexed); err != nil {
			return r.fail(err)
		}
		if rev != indexed {
			return errReadWhole
		}
		var err error
		ids, err = r.strings(`WITH RECURSIVE
 base(id) AS (`+base+`),
 others(id) AS (SELECT DISTINCT o.team_id FROM members m JOIN members o ON o.conv = m.conv AND o.former = 0
  JOIN teams t ON t.id = o.team_id AND t.closed = 0
  WHERE m.former = 0 AND m.conv <> '-' AND m.team_id IN (SELECT id FROM base)),
 up(id) AS (SELECT id FROM base UNION SELECT id FROM others
  UNION SELECT t.parent FROM teams t JOIN up ON t.id = up.id WHERE t.parent <> '')
SELECT id FROM up`, args...)
		return err
	})
	if errors.Is(err, errReadWhole) {
		return Snapshot(profileDir)
	}
	if err != nil || len(ids) == 0 {
		return &File{Version: Version, localIdentities: true}, err
	}
	return LoadTeams(profileDir, ids...)
}

// Revisions is the store revision each of the teams ids name last changed at
// ([teamRows]), keyed by id; a team the store does not hold is left out. It
// reads no team: a reader that keeps a few teams in view asks it to learn
// which moved.
func Revisions(profileDir string, ids ...string) (map[string]int64, error) {
	out := map[string]int64{}
	ids = distinct(ids)
	if len(ids) == 0 {
		return out, nil
	}
	err := readPage(context.Background(), profileDir, func(r *pageReader) error {
		if r.missing {
			return nil
		}
		for len(ids) > 0 {
			n := min(len(ids), 400)
			marks, args := placeholders(ids[:n])
			rows, err := r.q.QueryContext(r.ctx, "SELECT id, rev FROM teams WHERE id IN ("+marks+")", args...)
			if err != nil {
				return r.fail(err)
			}
			for rows.Next() {
				var id string
				var rev int64
				if err := rows.Scan(&id, &rev); err != nil {
					rows.Close()
					return r.fail(err)
				}
				out[id] = rev
			}
			if err := rows.Close(); err != nil {
				return r.fail(err)
			}
			ids = ids[n:]
		}
		return nil
	})
	return out, err
}
