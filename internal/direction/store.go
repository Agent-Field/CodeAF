package direction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// Store is the direction half of a collections database. It shares the
// workspace store's one handle, so a resolve reads placements and direction in
// one snapshot and a write fences against both.
type Store struct {
	ws  *workspace.Store
	now func() time.Time
}

// Open opens or creates a collections database and brings it to the version
// that holds direction records.
func Open(path string) (*Store, error) { return open(workspace.Open, path) }

// OpenExisting opens a collections database that must already exist. Like the
// workspace door of the same name it never creates the file; it does add the
// direction tables to a store that lacks them, because an upgrade adds to a
// database that exists.
func OpenExisting(path string) (*Store, error) { return open(workspace.OpenExisting, path) }

func open(opener func(string) (*workspace.Store, error), path string) (*Store, error) {
	ws, err := opener(path)
	if err != nil {
		return nil, err
	}
	if err := ws.EnsureDirection(); err != nil {
		_ = ws.Close()
		return nil, fmt.Errorf("open direction: %w", err)
	}
	return &Store{ws: ws, now: time.Now}, nil
}

// Close releases the handle.
func (s *Store) Close() error { return s.ws.Close() }

// Workspace is the organization half of the same handle.
func (s *Store) Workspace() *workspace.Store { return s.ws }

// Current returns a record's current revision, whatever its state.
func (s *Store) Current(ctx context.Context, id string) (Revision, error) {
	var rev Revision
	err := s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
		var err error
		rev, err = current(ctx, tx, id)
		return err
	})
	return rev, err
}

// At returns one exact revision: the wording and the authority a run that
// cited (id, revision) saw.
func (s *Store) At(ctx context.Context, id string, revision int) (Revision, error) {
	var rev Revision
	err := s.ws.ReadSnapshot(ctx, func(tx *sql.Tx) error {
		var err error
		rev, err = load(ctx, tx, id, revision)
		return err
	})
	return rev, err
}

// querier is the read surface of a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func pointer(ctx context.Context, q querier, id string) (int, error) {
	var revision int
	err := q.QueryRowContext(ctx, "SELECT revision FROM direction_records WHERE id=?", id).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return revision, err
}

func current(ctx context.Context, q querier, id string) (Revision, error) {
	revision, err := pointer(ctx, q, id)
	if err != nil {
		return Revision{}, err
	}
	return load(ctx, q, id, revision)
}

// revisionColumnNames is the column list every body read uses, in
// scanRevision's order.
var revisionColumnNames = []string{"record_id", "revision", "seq", "kind", "state", "state_reason", "title", "text",
	"text_sha256", "quote", "quote_origin", "source_class", "source_id", "source_session", "source_hint", "source_sha256",
	"author_class", "author_ref", "receipt_actor", "receipt_door", "receipt_ref", "receipt_at", "written_at"}

// revisionColumns spells the list, qualified by a table alias when given.
func revisionColumns(alias string) string {
	if alias == "" {
		return strings.Join(revisionColumnNames, ",")
	}
	return alias + "." + strings.Join(revisionColumnNames, ","+alias+".")
}

type scanner interface{ Scan(dest ...any) error }

func scanRevision(row scanner) (Revision, error) {
	var r Revision
	var receiptAt, writtenAt string
	err := row.Scan(&r.ID, &r.Revision, &r.Seq, &r.Kind, &r.State, &r.StateReason, &r.Title, &r.Text, &r.TextSHA256,
		&r.Quote, &r.QuoteOrigin, &r.Source.Class, &r.Source.ID, &r.Source.Session, &r.Source.Hint, &r.Source.SHA256,
		&r.Author.Class, &r.Author.Ref, &r.Receipt.Actor, &r.Receipt.Door, &r.Receipt.Ref, &receiptAt, &writtenAt)
	if err != nil {
		return Revision{}, err
	}
	if r.Receipt.At, err = parseStamp(receiptAt); err != nil {
		return Revision{}, err
	}
	if r.WrittenAt, err = parseStamp(writtenAt); err != nil {
		return Revision{}, err
	}
	return r, nil
}

// load reads one revision with its targets, exclusions and links.
func load(ctx context.Context, q querier, id string, revision int) (Revision, error) {
	r, err := scanRevision(q.QueryRowContext(ctx, "SELECT "+revisionColumns("")+
		" FROM direction_revisions WHERE record_id=? AND revision=?", id, revision))
	if errors.Is(err, sql.ErrNoRows) {
		return Revision{}, fmt.Errorf("%w: %s revision %d", ErrNotFound, id, revision)
	}
	if err != nil {
		return Revision{}, err
	}
	if r.Targets, err = collect(ctx, q, `SELECT target_kind,ref_id,session_id,reach FROM direction_targets
 WHERE record_id=? AND revision=? ORDER BY position`, []any{id, revision}, func(row scanner) (Target, error) {
		var t Target
		return t, row.Scan(&t.Kind, &t.Ref, &t.Session, &t.Reach)
	}); err != nil {
		return Revision{}, err
	}
	if r.Exclusions, err = collect(ctx, q, `SELECT target_kind,ref_id,session_id,at FROM direction_exclusions
 WHERE record_id=? AND revision=? ORDER BY target_kind,ref_id,session_id`, []any{id, revision}, func(row scanner) (Exclusion, error) {
		var e Exclusion
		var at string
		if err := row.Scan(&e.Kind, &e.Ref, &e.Session, &at); err != nil {
			return e, err
		}
		var err error
		e.At, err = parseStamp(at)
		return e, err
	}); err != nil {
		return Revision{}, err
	}
	if r.Links, err = collect(ctx, q, `SELECT link_kind,to_ref,to_revision FROM direction_links
 WHERE record_id=? AND revision=? ORDER BY link_kind,to_ref`, []any{id, revision}, func(row scanner) (Link, error) {
		var l Link
		return l, row.Scan(&l.Kind, &l.To, &l.ToRevision)
	}); err != nil {
		return Revision{}, err
	}
	return r, nil
}

// collect runs a query and scans every row, closing the rows before it
// returns: this store keeps one connection, so a read inside an open
// iteration would wait on the connection the iteration holds.
func collect[T any](ctx context.Context, q querier, query string, args []any, scan func(scanner) (T, error)) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]T, 0)
	for rows.Next() {
		item, err := scan(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

// Times are stored as UTC text that sorts in time order.
const stampLayout = "2006-01-02T15:04:05.000000000Z"

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(stampLayout)
}

func parseStamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	return time.Parse(stampLayout, s)
}
