package teams

// conv.go is which teams a conversation is in, answered by an index.
//
// A CONVERSATION IS NAMED BY ITS FOLDER: the interface calls an agent by the
// name of the folder its transcript lives in, and the session package is what
// knows how folders are laid out. It says so once, at start
// ([SetConversationName]); every membership row then carries that name, and
// so does every team for the conversation it was made under, so "the teams
// this agent is in" and "the teams it founded" are an index lookup however
// many teams there are.
//
// A store an earlier build of this schema wrote has rows without the name.
// The store's meta row says which revision its names were last made at
// (indexed); a lookup on a store written since by such a build reads every
// row instead, and the next write makes the names again ([reindexIn]).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
)

var conversationName atomic.Pointer[func(string) string]

// SetConversationName says how a transcript's path names its conversation.
// Until it is called a conversation is named by its transcript's directory.
func SetConversationName(name func(transcript string) string) { conversationName.Store(&name) }

// ConversationName is the name of the conversation whose transcript is at path.
func ConversationName(transcript string) string {
	if transcript = strings.TrimSpace(transcript); transcript == "" {
		return ""
	}
	if fn := conversationName.Load(); fn != nil {
		return (*fn)(transcript)
	}
	return filepath.Base(filepath.Dir(filepath.Clean(transcript)))
}

// memberConv is the conversation a membership names: its file, or its key
// when it keeps no file. "-" is a membership that names none, so a stored
// name is never empty and an empty one is a row not yet named.
func memberConv(m Member) string {
	transcript := m.File
	if strings.TrimSpace(transcript) == "" {
		transcript = m.Key
	}
	if c := ConversationName(transcript); c != "" {
		return c
	}
	return "-"
}

// founderConv is the conversation team t was made under, "-" for none.
func founderConv(t Team) string {
	if t.Founder == "" {
		return "-"
	}
	for _, list := range [][]Member{t.Members, t.FormerMembers} {
		for _, m := range list {
			if m.Key == t.Founder {
				return memberConv(m)
			}
		}
	}
	if c := ConversationName(t.Founder); c != "" {
		return c
	}
	return "-"
}

// reindexIn names every row again, in the caller's write transaction.
func reindexIn(ctx context.Context, tx *sql.Tx) error {
	type row struct {
		team   string
		former bool
		pos    int64
		conv   string
		m      Member
	}
	list, err := tx.QueryContext(ctx, "SELECT team_id, former, pos, conv, doc FROM members")
	if err != nil {
		return err
	}
	var rows []row
	members := map[string][]Member{}
	for list.Next() {
		var r row
		var doc string
		if err := list.Scan(&r.team, &r.former, &r.pos, &r.conv, &doc); err != nil {
			list.Close()
			return err
		}
		if json.Unmarshal([]byte(doc), &r.m) != nil {
			continue
		}
		rows = append(rows, r)
		members[r.team] = append(members[r.team], r.m)
	}
	if err := errors.Join(list.Err(), list.Close()); err != nil {
		return err
	}
	for _, r := range rows {
		if c := memberConv(r.m); c != r.conv {
			if _, err := tx.ExecContext(ctx, "UPDATE members SET conv = ? WHERE team_id = ? AND former = ? AND pos = ?", c, r.team, r.former, r.pos); err != nil {
				return err
			}
		}
	}
	teams, err := tx.QueryContext(ctx, "SELECT id, founder, doc FROM teams")
	if err != nil {
		return err
	}
	type found struct{ id, founder, now string }
	var changed []found
	for teams.Next() {
		var id, founder, doc string
		if err := teams.Scan(&id, &founder, &doc); err != nil {
			teams.Close()
			return err
		}
		var t Team
		if json.Unmarshal([]byte(doc), &t) != nil {
			continue
		}
		t.Members = members[id]
		if c := founderConv(t); c != founder {
			changed = append(changed, found{id, founder, c})
		}
	}
	if err := errors.Join(teams.Err(), teams.Close()); err != nil {
		return err
	}
	for _, f := range changed {
		if _, err := tx.ExecContext(ctx, "UPDATE teams SET founder = ? WHERE id = ?", f.now, f.id); err != nil {
			return err
		}
	}
	return nil
}

// LoadTeamsOf is every team, open or closed, that conversation conv is a
// member of (or was, with former), in the store's order.
func LoadTeamsOf(profileDir, conv string, former bool) (*File, error) {
	query := "SELECT DISTINCT team_id FROM members WHERE conv = ? AND former = 0"
	if former {
		query = "SELECT DISTINCT team_id FROM members WHERE conv = ?"
	}
	return loadIndexed(profileDir, conv, query, func(q querier, ctx context.Context) ([]string, error) {
		return scanMembers(ctx, q, conv, former)
	})
}

// LoadFoundedBy is every team made under conversation conv, in the store's
// order.
func LoadFoundedBy(profileDir, conv string) (*File, error) {
	return loadIndexed(profileDir, conv, "SELECT id FROM teams WHERE founder = ?", func(q querier, ctx context.Context) ([]string, error) {
		return scanFounders(ctx, q, conv)
	})
}

// loadIndexed is [LoadTeams] of the ids query answers for conv, or, on a
// store whose names an earlier build left behind, of the ids scan finds.
func loadIndexed(profileDir, conv, query string, scan func(querier, context.Context) ([]string, error)) (*File, error) {
	out := &File{Version: Version, localIdentities: true}
	if strings.TrimSpace(conv) == "" {
		return out, nil
	}
	path := Path(profileDir)
	h, err := acquire(path, false)
	if err != nil || h == nil {
		return out, err
	}
	defer h.release()
	ctx := context.Background()
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, unreadable(err, path)
	}
	defer tx.Rollback()
	var rev, indexed int64
	if err := tx.QueryRowContext(ctx, "SELECT rev, indexed FROM meta WHERE id = 1").Scan(&rev, &indexed); err != nil {
		return out, unreadable(err, path)
	}
	var ids []string
	if rev == indexed {
		r := &pageReader{ctx: ctx, q: tx, path: path}
		ids, err = r.strings(query, conv)
	} else {
		ids, err = scan(tx, ctx)
	}
	if err != nil {
		return out, unreadable(err, path)
	}
	if len(ids) == 0 {
		return out, nil
	}
	_ = tx.Rollback()
	return LoadTeams(profileDir, ids...)
}

func scanMembers(ctx context.Context, q querier, conv string, former bool) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT team_id, former, doc FROM members")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var ids []string
	for rows.Next() {
		var team, doc string
		var was bool
		if err := rows.Scan(&team, &was, &doc); err != nil {
			return nil, err
		}
		var m Member
		if (was && !former) || seen[team] || json.Unmarshal([]byte(doc), &m) != nil || memberConv(m) != conv {
			continue
		}
		seen[team] = true
		ids = append(ids, team)
	}
	return ids, rows.Err()
}

func scanFounders(ctx context.Context, q querier, conv string) ([]string, error) {
	members := map[string][]Member{}
	rows, err := q.QueryContext(ctx, "SELECT team_id, doc FROM members")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var team, doc string
		if err := rows.Scan(&team, &doc); err != nil {
			rows.Close()
			return nil, err
		}
		var m Member
		if json.Unmarshal([]byte(doc), &m) == nil {
			members[team] = append(members[team], m)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	teams, err := q.QueryContext(ctx, "SELECT id, doc FROM teams ORDER BY pos")
	if err != nil {
		return nil, err
	}
	defer teams.Close()
	var ids []string
	for teams.Next() {
		var id, doc string
		if err := teams.Scan(&id, &doc); err != nil {
			return nil, err
		}
		var t Team
		if json.Unmarshal([]byte(doc), &t) != nil {
			continue
		}
		t.Members = members[id]
		if founderConv(t) == conv {
			ids = append(ids, id)
		}
	}
	return ids, teams.Err()
}

// InspectConversation answers, under the writer's lock as [Inspect] does,
// whether any team, open or closed, names the conversation whose transcript
// is at transcript ([File.ReferencedConversations]), and runs fn with that.
// Only the teams the store's index finds for that conversation are read.
func InspectConversation(profileDir, transcript string, fn func(referenced bool) error) error {
	return strictlyLockedAt(lockPath(profileDir), lockWait, func() error {
		f, err := LoadTeamsOf(profileDir, ConversationName(transcript), true)
		if err != nil {
			return err
		}
		return fn(f.ReferencedConversations()[ConversationIdentity(transcript)])
	})
}

// ConversationReferenced is [InspectConversation]'s answer without the lock:
// whether any team names the conversation whose transcript is at transcript.
func ConversationReferenced(profileDir, transcript string) (bool, error) {
	f, err := LoadTeamsOf(profileDir, ConversationName(transcript), true)
	if err != nil {
		return false, err
	}
	return f.ReferencedConversations()[ConversationIdentity(transcript)], nil
}

// LoadConversationView is what a conversation's own view of its teams reads
// (convs are the names its keys spell, [ConversationName]):
// every team it is a member of, open or closed (a closed one tells it its
// membership ended), the other open teams its teams' members are in (where a
// member's home, and so whose it is, is said), every team above all of those
// (what its settings inherit and its questions and homes climb), the open
// teams below its own (what it manages), and every direct child of its teams
// whatever its state (whether one can be dissolved). It costs that conversation's corner of the tree, however many
// teams there are elsewhere. A store whose names an earlier build left behind
// ([reindexIn]) is read whole.
func LoadConversationView(profileDir string, convs ...string) (*File, error) {
	convs = distinct(convs)
	if len(convs) == 0 {
		return &File{Version: Version, localIdentities: true}, nil
	}
	marks, args := placeholders(convs)
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
 mine(id) AS (SELECT DISTINCT team_id FROM members WHERE conv IN (`+marks+`) AND former = 0),
 others(id) AS (SELECT DISTINCT o.team_id FROM members m JOIN members o ON o.conv = m.conv AND o.former = 0
  JOIN teams t ON t.id = o.team_id AND t.closed = 0
  WHERE m.former = 0 AND m.conv <> '-' AND m.team_id IN (SELECT id FROM mine)),
 up(id) AS (SELECT id FROM mine UNION SELECT id FROM others
  UNION SELECT t.parent FROM teams t JOIN up ON t.id = up.id WHERE t.parent <> ''),
 down(id) AS (SELECT t.id FROM teams t WHERE t.id IN (SELECT id FROM mine) AND t.closed = 0
  UNION SELECT t.id FROM teams t JOIN down ON t.parent = down.id WHERE t.closed = 0)
SELECT id FROM mine UNION SELECT id FROM up UNION SELECT id FROM down
 UNION SELECT id FROM teams WHERE parent IN (SELECT id FROM mine)`, args...)
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

// AnyManager reports whether any team, open or closed, has a manager. It
// stops at the first one.
func AnyManager(profileDir string) (bool, error) {
	any := false
	err := readPage(context.Background(), profileDir, func(r *pageReader) error {
		if r.missing {
			return nil
		}
		ids, err := r.strings("SELECT id FROM teams WHERE json_extract(doc, '$.manager') <> '' LIMIT 1")
		any = len(ids) > 0
		return err
	})
	return any, err
}
