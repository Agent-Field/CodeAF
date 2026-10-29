package teams

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Revision is the store's stamp ([Stamp]) a read was made at.
type Revision string

// ErrNoTeam is a page asked of a team the store does not hold.
var ErrNoTeam = errors.New("teams: there is no such team")

// ReadSnapshot is the teams exactly as the store holds them, for a reader that
// must not change anything: no lock, no tidy, no colour and no write of any
// kind. [Load] repairs and writes, so a read-only door cannot use it.
//
// It is one read transaction, so the teams and the revision agree. A missing
// store is an empty File at [MissingStamp] and no error; a store that is not
// this build's is [ErrUnreadable]. The File's teams are as stored, without
// their members' briefs.
func ReadSnapshot(ctx context.Context, profileDir string) (*File, Revision, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	s, err := readPath(ctx, Path(profileDir))
	if err != nil {
		return nil, "", err
	}
	if s == nil {
		return &File{Version: Version, localIdentities: true}, MissingStamp, nil
	}
	return s.file(), Revision(s.stamp()), ctx.Err()
}

// ── PAGES, CUT IN SQL ───────────────────────────────────────────────────────

// PageAt is where a page starts, asked once the collection is known: the
// revision it is read at, the team it is under (the zero Team for the top of a
// store with no root) and how many it holds. An error ends the read with it.
type PageAt func(rev Revision, parent Team, total int) (offset int, err error)

// TeamsPage is one page of the open teams directly under one parent.
type TeamsPage struct {
	Revision Revision
	// Root is the team that holds every other, nil for none.
	Root *Team
	// Parent is the team the page is under: the root when none was asked and
	// there is one, else "" for the top.
	Parent string
	Total  int
	Teams  []Team
	// Open is the count of direct open, non-root children of the root and of
	// each team on the page.
	Open map[string]int
}

// MembersPage is one page of one team's members, once per conversation.
type MembersPage struct {
	Revision Revision
	Root     *Team
	Team     Team
	Total    int
	Members  []Member
	Open     map[string]int
}

// PageTeams is the open teams directly under parent, newest made first and
// then by id, leaving out the root and every team the factory made, cut to
// at most limit from where at says. A team on the page, and the root, carry
// their members (without briefs). Nothing outside the page is read: the
// count and the cut are the store's index, so a page costs the same however
// many teams there are.
//
// A parent that is not there is [ErrNoTeam]; one that is closed, [ErrClosed].
func PageTeams(ctx context.Context, profileDir, parent string, limit int, at PageAt) (*TeamsPage, error) {
	out := &TeamsPage{Revision: MissingStamp, Parent: parent, Open: map[string]int{}}
	err := readPage(ctx, profileDir, func(r *pageReader) error {
		var err error
		if out.Revision, out.Root, err = r.head(); err != nil {
			return err
		}
		if out.Parent == "" && out.Root != nil {
			out.Parent = out.Root.ID
		}
		var under Team
		if out.Parent != "" {
			if under, err = r.openTeam(out.Parent); err != nil {
				return err
			}
		}
		const where = "parent = ? AND closed = 0 AND root = 0 AND factory = 0"
		if !r.missing {
			if err := r.q.QueryRowContext(r.ctx, "SELECT count(*) FROM teams WHERE "+where, out.Parent).Scan(&out.Total); err != nil {
				return r.fail(err)
			}
		}
		offset, err := at(out.Revision, under, out.Total)
		if err != nil {
			return err
		}
		ids, err := r.strings("SELECT id FROM teams WHERE "+where+" ORDER BY made_ns DESC, id LIMIT ? OFFSET ?", out.Parent, limit, offset)
		if err != nil {
			return err
		}
		if out.Teams, err = r.teams(ids); err != nil {
			return err
		}
		if out.Root != nil {
			ids = append(ids, out.Root.ID)
		}
		out.Open, err = r.open(ids)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// PageMembers is team id's members, once per conversation key, first stored
// first, leaving out a member with no key, cut to at most limit from where at
// says. The team must be there ([ErrNoTeam]) and open ([ErrClosed]).
func PageMembers(ctx context.Context, profileDir, id string, limit int, at PageAt) (*MembersPage, error) {
	out := &MembersPage{Revision: MissingStamp, Open: map[string]int{}}
	err := readPage(ctx, profileDir, func(r *pageReader) error {
		var err error
		if out.Revision, out.Root, err = r.head(); err != nil {
			return err
		}
		if out.Team, err = r.openTeam(id); err != nil {
			return err
		}
		const distinct = "FROM (SELECT doc, pos, row_number() OVER (PARTITION BY key ORDER BY pos) AS n FROM members WHERE team_id = ? AND former = 0 AND key <> '') WHERE n = 1"
		if err := r.q.QueryRowContext(r.ctx, "SELECT count(*) "+distinct, id).Scan(&out.Total); err != nil {
			return r.fail(err)
		}
		offset, err := at(out.Revision, out.Team, out.Total)
		if err != nil {
			return err
		}
		docs, err := r.strings("SELECT doc "+distinct+" ORDER BY pos LIMIT ? OFFSET ?", id, limit, offset)
		if err != nil {
			return err
		}
		for _, doc := range docs {
			var m Member
			if err := json.Unmarshal([]byte(doc), &m); err != nil {
				return fmt.Errorf("%w: a member of team %s: %w", ErrUnreadable, id, err)
			}
			out.Members = append(out.Members, m)
		}
		if out.Root != nil {
			out.Open, err = r.open([]string{out.Root.ID})
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// pageReader is one read transaction of a page.
type pageReader struct {
	ctx  context.Context
	q    *sql.Tx
	path string
	// missing is a store that is not there: every team asked of it is not.
	missing bool
}

func readPage(ctx context.Context, profileDir string, fn func(*pageReader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := Path(profileDir)
	h, err := acquire(path, false)
	if err != nil {
		return err
	}
	if h == nil {
		return fn(&pageReader{ctx: ctx, path: path, missing: true})
	}
	defer h.release()
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return unreadable(err, path)
	}
	defer tx.Rollback()
	if err := fn(&pageReader{ctx: ctx, q: tx, path: path}); err != nil {
		return err
	}
	return ctx.Err()
}

func (r *pageReader) fail(err error) error { return unreadable(err, r.path) }

// head is the revision and the root, with its members.
func (r *pageReader) head() (Revision, *Team, error) {
	if r.missing {
		return MissingStamp, nil, nil
	}
	var store string
	var rev int64
	if err := r.q.QueryRowContext(r.ctx, "SELECT store, rev FROM meta WHERE id = 1").Scan(&store, &rev); err != nil {
		return "", nil, r.fail(err)
	}
	stamp := Revision((&snapshot{store: store, rev: rev}).stamp())
	ids, err := r.strings("SELECT id FROM teams WHERE root = 1 ORDER BY pos LIMIT 1")
	if err != nil || len(ids) == 0 {
		return stamp, nil, err
	}
	root, err := r.teams(ids)
	if err != nil || len(root) == 0 {
		return stamp, nil, err
	}
	return stamp, &root[0], nil
}

// openTeam is team id with its members, which must be there and open.
func (r *pageReader) openTeam(id string) (Team, error) {
	if r.missing {
		return Team{}, ErrNoTeam
	}
	found, err := r.teams([]string{id})
	if err != nil {
		return Team{}, err
	}
	if len(found) == 0 {
		return Team{}, ErrNoTeam
	}
	if found[0].Closed() {
		return found[0], ErrClosed
	}
	return found[0], nil
}

// teams is the teams ids name, in that order, each with its members.
func (r *pageReader) teams(ids []string) ([]Team, error) {
	if len(ids) == 0 || r.missing {
		return nil, nil
	}
	marks, args := placeholders(ids)
	s := &snapshot{rows: map[string]*teamRows{}}
	rows, err := r.q.QueryContext(r.ctx, "SELECT id, doc FROM teams WHERE id IN ("+marks+")", args...)
	if err != nil {
		return nil, r.fail(err)
	}
	byID := map[string]Team{}
	for rows.Next() {
		var id, doc string
		if err := rows.Scan(&id, &doc); err != nil {
			rows.Close()
			return nil, r.fail(err)
		}
		var t Team
		if err := json.Unmarshal([]byte(doc), &t); err != nil {
			rows.Close()
			return nil, fmt.Errorf("%w: team %s: %w", ErrUnreadable, id, err)
		}
		t.ID, t.Members, t.FormerMembers = id, nil, nil
		byID[id] = t
	}
	if err := rows.Close(); err != nil {
		return nil, r.fail(err)
	}
	for _, id := range ids {
		if t, ok := byID[id]; ok {
			s.rows[id] = &teamRows{at: len(s.teams)}
			s.teams = append(s.teams, t)
		}
	}
	err = readMembers(r.ctx, r.q, r.path, s, "SELECT team_id, former, key, doc FROM members WHERE former = 0 AND team_id IN ("+marks+") ORDER BY team_id, pos", args...)
	return s.teams, err
}

// open counts each of ids' direct open, non-root children.
func (r *pageReader) open(ids []string) (map[string]int, error) {
	out := map[string]int{}
	if len(ids) == 0 || r.missing {
		return out, nil
	}
	marks, args := placeholders(ids)
	rows, err := r.q.QueryContext(r.ctx, "SELECT parent, count(*) FROM teams WHERE closed = 0 AND root = 0 AND parent IN ("+marks+") GROUP BY parent", args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	for rows.Next() {
		var parent string
		var n int
		if err := rows.Scan(&parent, &n); err != nil {
			return nil, r.fail(err)
		}
		out[parent] = n
	}
	return out, r.fail(rows.Err())
}

func (r *pageReader) strings(query string, args ...any) ([]string, error) {
	if r.missing {
		return nil, nil
	}
	rows, err := r.q.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, r.fail(err)
		}
		out = append(out, s)
	}
	return out, r.fail(rows.Err())
}

func placeholders(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
