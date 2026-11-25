package store

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ── COMMENTS ON A PASSAGE OF A PAGE, KEPT ──────────────────────────────────
//
// Two tables in WorkstreamsDB beside the tasks:
//
//   - page_comments: one row per thread, the thread as JSON (its anchor as
//     posted, every comment, who resolved it), with whether it is resolved
//     beside it so a file's open threads are counted without decoding them.
//   - page_comment_reads: per thread and person, how many of its comments
//     that person has read. A comment after them by someone else is unread.
//
// The tables are made by CREATE IF NOT EXISTS on every open, which is this
// database's migration. Deleting a workstream deletes its threads with it.
// The rules are serve's workstream_page_comments.go: this file keeps rows,
// and every change is ONE transaction ([Store.EditPageComments]), so a reply
// and the reopening it causes land together.

const workstreamPageCommentsSchema = `
CREATE TABLE IF NOT EXISTS page_comments (
	seq      INTEGER PRIMARY KEY AUTOINCREMENT,
	ws       TEXT NOT NULL,
	page     TEXT NOT NULL,
	id       TEXT NOT NULL UNIQUE,
	resolved INTEGER NOT NULL DEFAULT 0,
	doc      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS page_comments_page ON page_comments (ws, page, seq);
CREATE TABLE IF NOT EXISTS page_comment_reads (
	ws     TEXT NOT NULL,
	thread TEXT NOT NULL,
	person TEXT NOT NULL,
	seen   INTEGER NOT NULL,
	PRIMARY KEY (ws, thread, person)
);
`

// ErrPageCommentGone is a thread that is not kept in the workstream named.
var ErrPageCommentGone = errors.New("comment not found")

// ErrPageCommentsFull is a page that already holds as many threads as one may.
var ErrPageCommentsFull = errors.New("a page holds at most 2000 comment threads")

// PageComments are the threads on page of ws, oldest first, each with
// Unread filled for person.
func (st *Store) PageComments(ws, page, person string) ([]factory.PageCommentThread, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []factory.PageCommentThread{}, err
	}
	return threadsIn(db, person, `t.ws = ? AND t.page = ?`, ws, page)
}

// UnreadPageComments are the open threads in ws with something in them
// person has not read, oldest first.
func (st *Store) UnreadPageComments(ws, person string) ([]factory.PageCommentThread, error) {
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return []factory.PageCommentThread{}, err
	}
	all, err := threadsIn(db, person, `t.ws = ? AND t.resolved = 0`, ws)
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for _, t := range all {
		if t.Unread {
			out = append(out, t)
		}
	}
	return out, nil
}

// OpenPageComments is how many open threads each page of ws holds; a page
// with none is absent.
func (st *Store) OpenPageComments(ws string) (map[string]int, error) {
	out := map[string]int{}
	db, err := st.wsDB(false)
	if err != nil || db == nil {
		return out, err
	}
	rows, err := db.Query(`SELECT page, count(*) FROM page_comments WHERE ws = ? AND resolved = 0 GROUP BY page`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var page string
		var n int
		if err := rows.Scan(&page, &n); err != nil {
			return nil, err
		}
		out[page] = n
	}
	return out, rows.Err()
}

func threadsIn(q rowQuerier, person, where string, args ...any) ([]factory.PageCommentThread, error) {
	rows, err := q.Query(`SELECT t.doc, COALESCE(r.seen, 0) FROM page_comments t
		LEFT JOIN page_comment_reads r ON r.ws = t.ws AND r.thread = t.id AND r.person = ?
		WHERE `+where+` ORDER BY t.seq`, append([]any{factory.NormalizeHandle(person)}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []factory.PageCommentThread{}
	for rows.Next() {
		var doc string
		var seen int
		if err := rows.Scan(&doc, &seen); err != nil {
			return nil, err
		}
		var t factory.PageCommentThread
		if err := json.Unmarshal([]byte(doc), &t); err != nil {
			return nil, err
		}
		t.Unread = t.UnreadFor(person, seen)
		out = append(out, t)
	}
	return out, rows.Err()
}

// PageCommentWrite is one transaction over the threads, handed to the edit
// of [Store.EditPageComments].
type PageCommentWrite struct{ tx *sql.Tx }

// Thread is thread id of ws as this transaction sees it, Unread filled for
// person.
func (w PageCommentWrite) Thread(ws, id, person string) (factory.PageCommentThread, error) {
	all, err := threadsIn(w.tx, person, `t.ws = ? AND t.id = ?`, ws, id)
	if err != nil {
		return factory.PageCommentThread{}, err
	}
	if len(all) == 0 {
		return factory.PageCommentThread{}, ErrPageCommentGone
	}
	return all[0], nil
}

// Add keeps a new thread, refused when its page already holds as many as one
// may.
func (w PageCommentWrite) Add(t factory.PageCommentThread) error {
	var n int
	if err := w.tx.QueryRow(`SELECT count(*) FROM page_comments WHERE ws = ? AND page = ?`, t.WS, t.PageID).Scan(&n); err != nil {
		return err
	}
	if n >= factory.PageCommentThreadsMost {
		return ErrPageCommentsFull
	}
	doc, err := threadDoc(t)
	if err != nil {
		return err
	}
	_, err = w.tx.Exec(`INSERT INTO page_comments (ws, page, id, resolved, doc) VALUES (?, ?, ?, ?, ?)`,
		t.WS, t.PageID, t.ID, t.Resolved != nil, doc)
	return err
}

// Put keeps t in place of the thread of its id.
func (w PageCommentWrite) Put(t factory.PageCommentThread) error {
	doc, err := threadDoc(t)
	if err != nil {
		return err
	}
	res, err := w.tx.Exec(`UPDATE page_comments SET resolved = ?, doc = ? WHERE ws = ? AND id = ?`, t.Resolved != nil, doc, t.WS, t.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPageCommentGone
	}
	return nil
}

// Read records that person has read thread t to its end.
func (w PageCommentWrite) Read(t factory.PageCommentThread, person string) error {
	person = factory.NormalizeHandle(person)
	if person == "" {
		return nil
	}
	_, err := w.tx.Exec(`INSERT INTO page_comment_reads (ws, thread, person, seen) VALUES (?, ?, ?, ?)
		ON CONFLICT (ws, thread, person) DO UPDATE SET seen = MAX(page_comment_reads.seen, excluded.seen)`, t.WS, t.ID, person, len(t.Comments))
	return err
}

// threadDoc is what a row keeps: the thread without what is filled per reader.
func threadDoc(t factory.PageCommentThread) (string, error) {
	t.Unread = false
	doc, err := json.Marshal(t)
	return string(doc), err
}

// EditPageComments runs edit in one transaction under the workstreams'
// cross-process lock. An error from edit writes nothing.
func (st *Store) EditPageComments(edit func(w PageCommentWrite) error) error {
	return st.wsTx(func(tx *sql.Tx) error { return edit(PageCommentWrite{tx: tx}) })
}
