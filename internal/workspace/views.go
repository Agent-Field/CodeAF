package workspace

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// The bounds of the reads below. Each read is bounded by itself, whatever the
// caller asks for, because a collection can hold collections and a person can
// file thousands of references: nothing here walks or answers without limit.
const (
	// ListMax is the most collections one page of a listing answers. A
	// listing is never cut silently: a page that stops short of the end says
	// where the next one starts.
	ListMax = 1000
	// LevelMax is the most members one level of a tree answers; the top
	// level of a tree continues from where a page stopped.
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
	// seq is the row's place in the file, which a page cursor names.
	seq int64
}

// Page is one page of a listing: at most the limit asked, Next the cursor
// the following page starts from (empty when this page reached the end), and
// Total every collection in the file as the same read saw it.
type Page struct {
	Collections []Summary
	Next        string
	Total       int
}

// ── page cursors ───────────────────────────────────────────────────────────
//
// A CURSOR NAMES THE LAST ROW A PAGE ANSWERED, NEVER AN OFFSET. An offset
// moves under a peer's create or delete, which is how a page repeats a row or
// silently steps over one; a row's own key does not move. A listing's key is
// the collection's seq (AUTOINCREMENT, so never reused and always growing),
// and an opened collection's is the order it is read in, (added_at, seq),
// with the collection it belongs to so a cursor cannot be spent on another.
// The encoding is opaque to callers and checked strictly here: a cursor this
// store did not give is refused as invalid rather than read as a guess.

const (
	listCursor = "list"
	openCursor = "open"
	// cursorMax is the longest cursor read; one this store gives is far
	// shorter whatever the collection id.
	cursorMax = 8192
)

type cursor struct {
	K string `json:"k"`
	C string `json:"c,omitempty"`
	A int64  `json:"a,omitempty"`
	S int64  `json:"s"`
}

var errCursor = fmt.Errorf("%w: that page cursor was not given by this listing", ErrInvalid)

func encodeCursor(c cursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor reads a cursor of the kind named, or errCursor.
func decodeCursor(text, kind string) (cursor, error) {
	if text == "" || len(text) > cursorMax {
		return cursor{}, errCursor
	}
	raw, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return cursor{}, errCursor
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c cursor
	if err := dec.Decode(&c); err != nil || dec.More() {
		return cursor{}, errCursor
	}
	if c.K != kind || c.S <= 0 || c.A < 0 || (kind == listCursor && (c.C != "" || c.A != 0)) || (kind == openCursor && c.C == "") {
		return cursor{}, errCursor
	}
	return c, nil
}

// ListPage answers one page of collections, oldest first: at most limit
// (ListMax when limit is outside 1–ListMax), after the cursor a previous page
// gave (from the start when after is empty). With holds it also says whether
// each directly holds that reference. A blank file is no collections, and
// reading it leaves it blank.
//
// A WALK OF PAGES NEVER REPEATS OR SKIPS A COLLECTION THAT EXISTED WHEN IT
// BEGAN, whatever peers create or delete meanwhile: one created during the
// walk lands after every page already read, and one deleted is simply not
// there to answer.
func (s *Store) ListPage(ctx context.Context, holds *Ref, after string, limit int) (Page, error) {
	if holds != nil {
		if err := holds.Validate(); err != nil {
			return Page{}, storeError(err)
		}
	}
	if limit <= 0 || limit > ListMax {
		limit = ListMax
	}
	var from int64
	if after != "" {
		c, err := decodeCursor(after, listCursor)
		if err != nil {
			return Page{}, err
		}
		from = c.S
	}
	version, err := s.readyForRead(ctx)
	if err != nil {
		return Page{}, storeError(err)
	}
	if version == 0 {
		return Page{Collections: make([]Summary, 0)}, nil
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Page{}, storeError(err)
	}
	defer tx.Rollback()
	var page Page
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM collections").Scan(&page.Total); err != nil {
		return Page{}, storeError(err)
	}
	out, err := summaries(ctx, tx, version, holds, "", from, limit+1)
	if err != nil {
		return Page{}, storeError(err)
	}
	if len(out) > limit {
		out = out[:limit]
		page.Next = encodeCursor(cursor{K: listCursor, S: out[limit-1].seq})
	}
	page.Collections = out
	return page, nil
}

// List answers every collection, oldest first, walking ListPage to the end.
func (s *Store) List(ctx context.Context, holds *Ref) ([]Summary, error) {
	all := make([]Summary, 0)
	after := ""
	for {
		page, err := s.ListPage(ctx, holds, after, ListMax)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Collections...)
		if page.Next == "" {
			return all, nil
		}
		after = page.Next
	}
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
	out, err := summaries(ctx, tx, version, nil, id, 0, 1)
	if err != nil {
		return Summary{}, storeError(err)
	}
	if len(out) == 0 {
		return Summary{}, storeError(ErrNotFound)
	}
	return out[0], nil
}

// summaries reads collections as Summary through one view: every one after
// the seq afterSeq (0 from the start), or the one named by only.
func summaries(ctx context.Context, tx *sql.Tx, version int, holds *Ref, only string, afterSeq int64, limit int) ([]Summary, error) {
	created, added := columns(version)
	held := "0"
	args := []any{}
	if holds != nil {
		held = "EXISTS(SELECT 1 FROM memberships h WHERE h.collection_id=c.id AND h.kind=? AND h.ref_id=? AND h.session_id=?)"
		args = append(args, holds.Kind, holds.ID, holds.SessionID)
	}
	var where []string
	if only != "" {
		where = append(where, "c.id=?")
		args = append(args, only)
	}
	if afterSeq > 0 {
		where = append(where, "c.seq>?")
		args = append(args, afterSeq)
	}
	clause := ""
	if len(where) > 0 {
		clause = "WHERE " + strings.Join(where, " AND ")
	}
	args = append(args, limit)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT c.seq,c.id,c.name,%s,
 (SELECT count(*) FROM memberships m WHERE m.collection_id=c.id),
 (SELECT coalesce(max(%s),0) FROM memberships m WHERE m.collection_id=c.id),
 %s
FROM collections c %s ORDER BY c.seq LIMIT ?`, created, added, held, clause), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Summary, 0)
	for rows.Next() {
		var c Summary
		var h bool
		if err := rows.Scan(&c.seq, &c.ID, &c.Name, &c.CreatedAt, &c.Count, &c.LastAddedAt, &h); err != nil {
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
	// seq is the row's place in the file, which a page cursor names.
	seq int64
}

// Level is one collection's members as a tree answers them, newest added
// first, and whether there were more than it answered.
type Level struct {
	Members []Node
	More    bool
}

// Tree is a collection and its members, opened to a depth. More says
// something was cut: the top level stopped at its limit, or the node budget
// cut a list below it. Next is the cursor the top level continues from, empty
// when the top level reached its end.
type Tree struct {
	Collection Summary
	Level
	Next string
}

// levelAfter is where a page of one level starts: after the row with this
// added_at and seq in the level's order.
type levelAfter struct {
	added, seq int64
}

// Tree opens a collection: its members newest added first, at most limit per
// level, and the members of each collection member down to depth levels below
// the top (0 opens none), at most TreeNodeMax nodes in all. after is a cursor
// a previous Tree of the same collection gave as Next, and continues its top
// level; a cursor of another collection, or of a listing, is ErrInvalid.
//
// A COLLECTION ALREADY ON THE PATH IS NEVER OPENED AGAIN. Writes refuse a
// cycle, so none should exist, but a tree is a read that must end whatever
// the file says, so it carries its own path rather than trusting the writer.
//
// THE NODE BUDGET NEVER STRANDS THE TOP LEVEL. The top level is read first
// and its cursor is its own last row, so a page whose children were cut short
// still continues exactly where its top level stopped. A child list the
// budget cut, or a collection within depth the budget left unopened, says
// More, and so does the tree.
func (s *Store) Tree(ctx context.Context, id string, depth, limit int, after string) (Tree, error) {
	depth = max(0, min(depth, TreeDepthMax))
	if limit <= 0 || limit > LevelMax {
		limit = LevelMax
	}
	var start *levelAfter
	if after != "" {
		c, err := decodeCursor(after, openCursor)
		if err != nil {
			return Tree{}, err
		}
		if c.C != id {
			return Tree{}, errCursor
		}
		start = &levelAfter{added: c.A, seq: c.S}
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
	top, err := summaries(ctx, tx, version, nil, id, 0, 1)
	if err != nil {
		return Tree{}, storeError(err)
	}
	if len(top) == 0 {
		return Tree{}, storeError(ErrNotFound)
	}
	budget := TreeNodeMax
	cut := false
	path := map[string]bool{id: true}
	var open func(id string, level int, start *levelAfter) (*Level, error)
	open = func(id string, level int, start *levelAfter) (*Level, error) {
		want := min(limit, budget)
		nodes, more, err := levelOf(ctx, tx, version, id, want, start)
		if err != nil {
			return nil, err
		}
		if more && want < limit {
			cut = true
		}
		budget -= len(nodes)
		out := &Level{Members: nodes, More: more}
		if level > depth {
			return out, nil
		}
		for i := range out.Members {
			n := &out.Members[i]
			if n.Kind != CollectionKind || !n.Found || path[n.ID] {
				continue
			}
			if budget <= 0 {
				// Within depth and not opened: the list is cut, not empty.
				n.Children = &Level{Members: make([]Node, 0), More: n.Count > 0}
				cut = cut || n.Count > 0
				continue
			}
			path[n.ID] = true
			children, err := open(n.ID, level+1, nil)
			delete(path, n.ID)
			if err != nil {
				return nil, err
			}
			n.Children = children
		}
		return out, nil
	}
	level, err := open(id, 1, start)
	if err != nil {
		return Tree{}, storeError(err)
	}
	tree := Tree{Collection: top[0], Level: *level}
	if level.More && len(level.Members) > 0 {
		last := level.Members[len(level.Members)-1]
		tree.Next = encodeCursor(cursor{K: openCursor, C: id, A: last.AddedAt, S: last.seq})
	}
	tree.More = level.More || cut
	return tree, nil
}

// levelOf reads one collection's members, newest added first, at most limit,
// after start when it is given, with the name and count of each collection
// member, and whether there were more. A row with no known added_at (0)
// sorts after every known one, which is what the plain descending order on
// (added_at, seq) already says.
func levelOf(ctx context.Context, tx *sql.Tx, version int, id string, limit int, start *levelAfter) ([]Node, bool, error) {
	nodes := make([]Node, 0)
	_, added := columns(version)
	from, args := "", []any{id}
	if start != nil {
		// The keyset condition on the same (added_at, seq) the order reads.
		// On a schema-1 file added is the constant 0 and so is every
		// cursor's, which leaves seq alone deciding, as the order does.
		from = fmt.Sprintf(" AND (%[1]s<? OR (%[1]s=? AND m.seq<?))", added)
		args = append(args, start.added, start.added, start.seq)
	}
	if limit <= 0 {
		var held bool
		err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM memberships m WHERE m.collection_id=?"+from+")", args...).Scan(&held)
		return nodes, held, err
	}
	// A schema-1 file knows no moments, so its order is insertion alone; a
	// constant in ORDER BY would be read as a column number.
	order := "m.seq DESC"
	if version >= schemaVersion {
		order = "m.added_at DESC, m.seq DESC"
	}
	args = append(args, limit+1)
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT m.seq,m.kind,m.ref_id,m.session_id,%s,coalesce(c.name,''),c.id IS NOT NULL,
 CASE WHEN m.kind='collection' THEN (SELECT count(*) FROM memberships x WHERE x.collection_id=m.ref_id) ELSE 0 END
FROM memberships m LEFT JOIN collections c ON m.kind='collection' AND c.id=m.ref_id
WHERE m.collection_id=?%s ORDER BY %s LIMIT ?`, added, from, order), args...)
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
		if err := rows.Scan(&n.seq, &n.Kind, &n.ID, &n.SessionID, &n.AddedAt, &n.Name, &n.Found, &n.Count); err != nil {
			return nil, false, err
		}
		nodes = append(nodes, n)
	}
	return nodes, more, rows.Err()
}

// Expansion is every reference a collection holds through its nested
// collections, each once, outer levels first. Collections is how many nested
// collections were read; Deleted how many nested collection references name
// a collection that is gone (each once); Unread how many nested collections
// sat deeper than the depth asked and were not read (each once); More says
// ExpandMax cut it short.
type Expansion struct {
	Collection  Summary
	Refs        []Member
	Collections int
	Deleted     int
	Unread      int
	More        bool
}

// Expand flattens a collection to depth levels (1 reads only its own
// members): every reference that is not a collection, each once at its
// outermost place, newest added first within a collection. Each nested
// collection is read once however many collections hold it.
//
// NOTHING IS DROPPED WITHOUT BEING COUNTED. A nested collection that is gone
// is Deleted, one past the depth is Unread, and a cut is More, so a caller can
// always say what it is not showing.
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
	top, err := summaries(ctx, tx, version, nil, id, 0, 1)
	if err != nil {
		return Expansion{}, storeError(err)
	}
	if len(top) == 0 {
		return Expansion{}, storeError(ErrNotFound)
	}
	out := Expansion{Collection: top[0], Refs: make([]Member, 0)}
	seen := map[string]bool{}
	read := map[string]bool{id: true}
	gone, unread := map[string]bool{}, map[string]bool{}
	frontier := []string{id}
	for level := 1; level <= depth && len(frontier) > 0 && !out.More; level++ {
		var next []string
		for _, cid := range frontier {
			if cid != id {
				out.Collections++
			}
			nodes, more, err := levelOf(ctx, tx, version, cid, ExpandMax+1, nil)
			if err != nil {
				return Expansion{}, storeError(err)
			}
			if more {
				out.More = true
			}
			for _, n := range nodes {
				if n.Kind == CollectionKind {
					switch {
					case !n.Found:
						gone[n.ID] = true
					case read[n.ID]:
					case level < depth:
						read[n.ID] = true
						next = append(next, n.ID)
					default:
						unread[n.ID] = true
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
	out.Deleted, out.Unread = len(gone), len(unread)
	return out, nil
}
