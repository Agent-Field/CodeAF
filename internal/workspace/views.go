package workspace

import (
	"context"
	"database/sql"
	"fmt"
)

// The bounds of the reads below. Each read is bounded by itself, whatever the
// caller asks for, because a collection can hold collections and a person can
// file thousands of references: nothing here walks or answers without limit.
const (
	// ListMax is the most collections one listing answers.
	ListMax = 1000
	// LevelMax is the most members one level of a tree answers.
	LevelMax = 500
	// TreeDepthMax is how many levels below the top a tree opens.
	TreeDepthMax = 2
	// TreeNodeMax is the most nodes one tree answers in all.
	TreeNodeMax = 1500
	// ExpandDepthMax is how many levels an expansion reads.
	ExpandDepthMax = 3
	// ExpandMax is the most references one expansion answers, before any
	// owner is asked whether they are still there.
	ExpandMax = 2000
)

// Summary is a collection with what a listing says of it: when it was made
// (0 unknown), how many direct members it has, when the newest was added (0
// none or unknown), and, when the listing was asked about one reference,
// whether it directly holds it.
type Summary struct {
	Collection
	CreatedAt   int64
	Count       int
	LastAddedAt int64
	Holds       *bool
}

// List answers every collection, oldest first, at most ListMax. With holds it
// also says whether each directly holds that reference. A blank file is no
// collections, and reading it leaves it blank.
func (s *Store) List(ctx context.Context, holds *Ref) ([]Summary, error) {
	if holds != nil {
		if err := holds.Validate(); err != nil {
			return nil, storeError(err)
		}
	}
	version, err := s.readyForRead(ctx)
	if err != nil {
		return nil, storeError(err)
	}
	if version == 0 {
		return make([]Summary, 0), nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, storeError(err)
	}
	defer tx.Rollback()
	out, err := summaries(ctx, tx, version, holds, "", ListMax)
	return out, storeError(err)
}

// Describe is one collection as List says it.
func (s *Store) Describe(ctx context.Context, id string) (Summary, error) {
	version, err := s.readyForRead(ctx)
	if err != nil {
		return Summary{}, storeError(err)
	}
	if version == 0 {
		return Summary{}, storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Summary{}, storeError(err)
	}
	defer tx.Rollback()
	out, err := summaries(ctx, tx, version, nil, id, 1)
	if err != nil {
		return Summary{}, storeError(err)
	}
	if len(out) == 0 {
		return Summary{}, storeError(ErrNotFound)
	}
	return out[0], nil
}

// summaries reads collections as Summary through one view: every one, or the
// one named by only.
func summaries(ctx context.Context, tx *sql.Tx, version int, holds *Ref, only string, limit int) ([]Summary, error) {
	created, added := columns(version)
	held, where := "0", ""
	args := []any{}
	if holds != nil {
		held = "EXISTS(SELECT 1 FROM memberships h WHERE h.collection_id=c.id AND h.kind=? AND h.ref_id=? AND h.session_id=?)"
		args = append(args, holds.Kind, holds.ID, holds.SessionID)
	}
	if only != "" {
		where = "WHERE c.id=?"
		args = append(args, only)
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT c.id,c.name,%s,
 (SELECT count(*) FROM memberships m WHERE m.collection_id=c.id),
 (SELECT coalesce(max(%s),0) FROM memberships m WHERE m.collection_id=c.id),
 %s
FROM collections c %s ORDER BY c.seq LIMIT ?`, created, added, held, where), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Summary, 0)
	for rows.Next() {
		var c Summary
		var h bool
		if err := rows.Scan(&c.ID, &c.Name, &c.CreatedAt, &c.Count, &c.LastAddedAt, &h); err != nil {
			return nil, err
		}
		if holds != nil {
			c.Holds = &h
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Node is one member of a tree. For a collection reference, Found says the
// collection is still there, Name is its name and Count its direct members;
// Children are its own members when the tree opened it.
type Node struct {
	Member
	Name     string
	Count    int
	Found    bool
	Children *Level
}

// Level is one collection's members as a tree answers them, newest added
// first, and whether there were more than it answered.
type Level struct {
	Members []Node
	More    bool
}

// Tree is a collection and its members, opened to a depth.
type Tree struct {
	Collection Summary
	Level
}

// Tree opens a collection: its members newest added first, at most limit per
// level, and the members of each collection member down to depth levels below
// the top (0 opens none), at most TreeNodeMax nodes in all.
//
// A COLLECTION ALREADY ON THE PATH IS NEVER OPENED AGAIN. Writes refuse a
// cycle, so none should exist, but a tree is a read that must end whatever
// the file says, so it carries its own path rather than trusting the writer.
func (s *Store) Tree(ctx context.Context, id string, depth, limit int) (Tree, error) {
	depth = max(0, min(depth, TreeDepthMax))
	if limit <= 0 || limit > LevelMax {
		limit = LevelMax
	}
	version, err := s.readyForRead(ctx)
	if err != nil {
		return Tree{}, storeError(err)
	}
	if version == 0 {
		return Tree{}, storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Tree{}, storeError(err)
	}
	defer tx.Rollback()
	top, err := summaries(ctx, tx, version, nil, id, 1)
	if err != nil {
		return Tree{}, storeError(err)
	}
	if len(top) == 0 {
		return Tree{}, storeError(ErrNotFound)
	}
	budget := TreeNodeMax
	path := map[string]bool{id: true}
	var open func(id string, level int) (*Level, error)
	open = func(id string, level int) (*Level, error) {
		nodes, more, err := levelOf(ctx, tx, version, id, min(limit, budget))
		if err != nil {
			return nil, err
		}
		budget -= len(nodes)
		out := &Level{Members: nodes, More: more}
		if level > depth {
			return out, nil
		}
		for i := range out.Members {
			n := &out.Members[i]
			if n.Kind != CollectionKind || !n.Found || path[n.ID] || budget <= 0 {
				continue
			}
			path[n.ID] = true
			children, err := open(n.ID, level+1)
			delete(path, n.ID)
			if err != nil {
				return nil, err
			}
			n.Children = children
		}
		return out, nil
	}
	level, err := open(id, 1)
	if err != nil {
		return Tree{}, storeError(err)
	}
	return Tree{Collection: top[0], Level: *level}, nil
}

// levelOf reads one collection's members, newest added first, at most limit,
// with the name and count of each collection member, and whether there were
// more. A row with no known added_at sorts after every known one.
func levelOf(ctx context.Context, tx *sql.Tx, version int, id string, limit int) ([]Node, bool, error) {
	nodes := make([]Node, 0)
	if limit <= 0 {
		var held bool
		err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM memberships WHERE collection_id=?)", id).Scan(&held)
		return nodes, held, err
	}
	_, added := columns(version)
	// A schema-1 file knows no moments, so its order is insertion alone; a
	// constant in ORDER BY would be read as a column number.
	order := "m.seq DESC"
	if version >= schemaVersion {
		order = "m.added_at DESC, m.seq DESC"
	}
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT m.kind,m.ref_id,m.session_id,%s,coalesce(c.name,''),c.id IS NOT NULL,
 CASE WHEN m.kind='collection' THEN (SELECT count(*) FROM memberships x WHERE x.collection_id=m.ref_id) ELSE 0 END
FROM memberships m LEFT JOIN collections c ON m.kind='collection' AND c.id=m.ref_id
WHERE m.collection_id=? ORDER BY %s LIMIT ?`, added, order), id, limit+1)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	more := false
	for rows.Next() {
		if len(nodes) == limit {
			more = true
			break
		}
		var n Node
		if err := rows.Scan(&n.Kind, &n.ID, &n.SessionID, &n.AddedAt, &n.Name, &n.Found, &n.Count); err != nil {
			return nil, false, err
		}
		nodes = append(nodes, n)
	}
	return nodes, more, rows.Err()
}

// Expansion is every reference a collection holds through its nested
// collections, each once, outer levels first; Collections is how many nested
// collections were read, and More says ExpandMax cut it short.
type Expansion struct {
	Collection  Summary
	Refs        []Member
	Collections int
	More        bool
}

// Expand flattens a collection to depth levels (1 reads only its own
// members): every reference that is not a collection, each once at its
// outermost place, newest added first within a collection. Each nested
// collection is read once however many collections hold it.
func (s *Store) Expand(ctx context.Context, id string, depth int) (Expansion, error) {
	depth = max(1, min(depth, ExpandDepthMax))
	version, err := s.readyForRead(ctx)
	if err != nil {
		return Expansion{}, storeError(err)
	}
	if version == 0 {
		return Expansion{}, storeError(ErrNotFound)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Expansion{}, storeError(err)
	}
	defer tx.Rollback()
	top, err := summaries(ctx, tx, version, nil, id, 1)
	if err != nil {
		return Expansion{}, storeError(err)
	}
	if len(top) == 0 {
		return Expansion{}, storeError(ErrNotFound)
	}
	out := Expansion{Collection: top[0], Refs: make([]Member, 0)}
	seen := map[string]bool{}
	read := map[string]bool{id: true}
	frontier := []string{id}
	for level := 1; level <= depth && len(frontier) > 0 && !out.More; level++ {
		var next []string
		for _, cid := range frontier {
			if cid != id {
				out.Collections++
			}
			nodes, more, err := levelOf(ctx, tx, version, cid, ExpandMax+1)
			if err != nil {
				return Expansion{}, storeError(err)
			}
			if more {
				out.More = true
			}
			for _, n := range nodes {
				if n.Kind == CollectionKind {
					if n.Found && !read[n.ID] && level < depth {
						read[n.ID] = true
						next = append(next, n.ID)
					}
					continue
				}
				if seen[n.key()] {
					continue
				}
				if len(out.Refs) == ExpandMax {
					out.More = true
					break
				}
				seen[n.key()] = true
				out.Refs = append(out.Refs, n.Member)
			}
			if out.More {
				break
			}
		}
		frontier = next
	}
	return out, nil
}
