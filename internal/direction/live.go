package direction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// ErrDrift reports that the derived live index disagrees with the revisions
// it is derived from. The revisions are the truth; Rebuild repairs the index.
var ErrDrift = errors.New("the live direction index disagrees with the records")

// liveRow is one row of direction_live.
type liveRow struct {
	kind          TargetKind
	ref, session  string
	lane          Lane
	record        string
	revision      int
	reach         Reach
	hasExclusions bool
	hasLinks      bool
	writtenAt     string
}

// current revision facts the live index is derived from, without bodies.
type liveSource struct {
	id            string
	revision      int
	kind          Kind
	state         State
	writtenAt     string
	hasExclusions bool
	hasLinks      bool
	targets       []Target
	dangling      bool // the pointer names a revision that does not exist
}

// liveRows IS THE ONE DERIVATION of the live index from a current revision,
// used by every write and by Rebuild, so the two cannot disagree about what a
// row is. A revision outside every lane has no rows; a record with no targets
// has none either — it reaches nothing until someone says where it applies.
func liveRows(src liveSource) []liveRow {
	l := lane(src.kind, src.state)
	if l == laneNone {
		return nil
	}
	rows := make([]liveRow, 0, len(src.targets))
	for _, t := range src.targets {
		rows = append(rows, liveRow{kind: t.Kind, ref: t.Ref, session: t.Session, lane: l, record: src.id,
			revision: src.revision, reach: t.Reach, hasExclusions: src.hasExclusions, hasLinks: src.hasLinks,
			writtenAt: src.writtenAt})
	}
	return rows
}

func sourceOf(r Revision) liveSource {
	src := liveSource{id: r.ID, revision: r.Revision, kind: r.Kind, state: r.State, writtenAt: stamp(r.WrittenAt),
		hasExclusions: len(r.Exclusions) > 0, targets: r.Targets}
	for _, l := range r.Links {
		src.hasLinks = src.hasLinks || l.Kind.resolved()
	}
	return src
}

// liveRefresh drops a record's live rows by the direction_live_record index.
var liveRefresh = named("live-refresh", "DELETE FROM direction_live WHERE record_id=?")

// refreshLive replaces the record's rows with those of the revision that is
// now current, inside the transaction that moved the pointer.
func (w *writeTx) refreshLive(r Revision) error {
	if _, err := w.tx.ExecContext(w.ctx, liveRefresh, r.ID); err != nil {
		return err
	}
	return insertLive(w.ctx, w.tx, liveRows(sourceOf(r)))
}

func insertLive(ctx context.Context, tx *sql.Tx, rows []liveRow) error {
	if len(rows) == 0 {
		return nil
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO direction_live(target_kind,ref_id,session_id,lane,record_id,revision,
 reach,has_exclusions,has_links,written_at) VALUES (?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, row := range rows {
		if _, err := stmt.ExecContext(ctx, row.kind, row.ref, row.session, row.lane, row.record, row.revision,
			row.reach, row.hasExclusions, row.hasLinks, row.writtenAt); err != nil {
			return err
		}
	}
	return nil
}

// resolvedLinkKinds is the SQL spelling of LinkKind.resolved, so Rebuild's set
// query and a write's Go derivation count the same links.
func resolvedLinkKinds() string {
	var kinds []string
	for _, k := range []LinkKind{Supersedes, Overrides, ConflictsWith, DerivedFrom} {
		if k.resolved() {
			kinds = append(kinds, "'"+string(k)+"'")
		}
	}
	return strings.Join(kinds, ",")
}

// expectedLive derives every live row from the revisions. It reads every
// current revision, so it is for Rebuild and Verify only — repair, never a
// path a conversation waits on (L6).
//
// A POINTER TO NOTHING IS DRIFT, NOT AN EMPTY RECORD. A record whose pointer
// names a revision that does not exist has no rows to derive, so an inner
// join would pass it silently while the records themselves are broken; the
// outer join counts it and both Verify and Rebuild refuse.
func expectedLive(ctx context.Context, q querier) ([]liveRow, error) {
	sources, err := collect(ctx, q, `SELECT d.id,d.revision,ifnull(r.kind,''),ifnull(r.state,''),ifnull(r.written_at,''),
 r.record_id IS NULL,
 EXISTS(SELECT 1 FROM direction_exclusions e WHERE e.record_id=d.id AND e.revision=d.revision),
 EXISTS(SELECT 1 FROM direction_links k WHERE k.record_id=d.id AND k.revision=d.revision AND k.link_kind IN (`+resolvedLinkKinds()+`))
 FROM direction_records d LEFT JOIN direction_revisions r ON r.record_id=d.id AND r.revision=d.revision`, nil,
		func(row scanner) (liveSource, error) {
			var src liveSource
			return src, row.Scan(&src.id, &src.revision, &src.kind, &src.state, &src.writtenAt, &src.dangling, &src.hasExclusions, &src.hasLinks)
		})
	if err != nil {
		return nil, err
	}
	for _, src := range sources {
		if src.dangling {
			return nil, fmt.Errorf("%w: %s points at revision %d, which does not exist", ErrDrift, src.id, src.revision)
		}
	}
	type located struct {
		id string
		t  Target
	}
	targets, err := collect(ctx, q, `SELECT t.record_id,t.target_kind,t.ref_id,t.session_id,t.reach
 FROM direction_records d JOIN direction_targets t ON t.record_id=d.id AND t.revision=d.revision
 ORDER BY t.record_id,t.position`, nil, func(row scanner) (located, error) {
		var l located
		return l, row.Scan(&l.id, &l.t.Kind, &l.t.Ref, &l.t.Session, &l.t.Reach)
	})
	if err != nil {
		return nil, err
	}
	byRecord := make(map[string][]Target, len(sources))
	for _, l := range targets {
		byRecord[l.id] = append(byRecord[l.id], l.t)
	}
	var rows []liveRow
	for _, src := range sources {
		src.targets = byRecord[src.id]
		rows = append(rows, liveRows(src)...)
	}
	return rows, nil
}

func storedLive(ctx context.Context, q querier) ([]liveRow, error) {
	return collect(ctx, q, `SELECT target_kind,ref_id,session_id,lane,record_id,revision,reach,has_exclusions,has_links,written_at
 FROM direction_live`, nil, func(row scanner) (liveRow, error) {
		var r liveRow
		return r, row.Scan(&r.kind, &r.ref, &r.session, &r.lane, &r.record, &r.revision, &r.reach, &r.hasExclusions, &r.hasLinks, &r.writtenAt)
	})
}

// Rebuild recreates the live index from the revisions in one immediate
// transaction, so no resolve sees it half built.
func (s *Store) Rebuild(ctx context.Context) error {
	return s.write(ctx, func(w *writeTx) error {
		rows, err := expectedLive(ctx, w.tx)
		if err != nil {
			return err
		}
		if _, err := w.tx.ExecContext(ctx, "DELETE FROM direction_live"); err != nil {
			return err
		}
		return insertLive(ctx, w.tx, rows)
	})
}

// Verify compares the live index with what the revisions say it must hold and
// reports ErrDrift with the number of rows that differ.
func (s *Store) Verify(ctx context.Context) error {
	return workspace.ReadSnapshot(ctx, s.ws, func(tx *sql.Tx) error {
		want, err := expectedLive(ctx, tx)
		if err != nil {
			return err
		}
		got, err := storedLive(ctx, tx)
		if err != nil {
			return err
		}
		if differ := differingRows(want, got); differ != 0 {
			return fmt.Errorf("%w: %d rows differ", ErrDrift, differ)
		}
		return nil
	})
}

func differingRows(want, got []liveRow) int {
	counts := make(map[liveRow]int, len(want))
	for _, r := range want {
		counts[r]++
	}
	for _, r := range got {
		counts[r]--
	}
	differ := 0
	for _, n := range counts {
		if n < 0 {
			n = -n
		}
		differ += n
	}
	return differ
}
