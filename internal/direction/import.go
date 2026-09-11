package direction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// LegacyStore names an old store a record is imported from.
type LegacyStore string

const (
	LegacyStanding LegacyStore = "standing"
	LegacyContexts LegacyStore = "contexts"
	LegacyMemory   LegacyStore = "memory"
)

// Legacy identifies one source document by its content: the same id with the
// same hash is the same import, however often it is run.
type Legacy struct {
	Store   LegacyStore
	ID      string
	Version string
	SHA256  string
}

// ImportRun is one run of an importer, recorded once however many items it
// writes.
type ImportRun struct {
	ID           string
	Mode         string
	Binary       string
	ReportSHA256 string
}

// ImportRevision is one revision as the old store had it. The receipt is the
// one the old store carried, copied and never promoted: a hold a delegated
// principal approved stays legacy_delegated.
type ImportRevision struct {
	Draft       Draft
	State       State
	StateReason string
	Receipt     Receipt
	WrittenAt   time.Time // when the old store wrote it; zero stamps the import's time
}

// ImportItem is one source document. ID, when set, keeps an identity the old
// store already gave out, so a receipt that cites it still resolves.
type ImportItem struct {
	Legacy    Legacy
	ID        string
	Revisions []ImportRevision
}

// ImportOutcome says what an import did with one item.
type ImportOutcome string

const (
	Imported  ImportOutcome = "imported"
	Unchanged ImportOutcome = "unchanged"
	Appended  ImportOutcome = "appended"
)

// ImportResult is the record an item maps to and what happened to it.
type ImportResult struct {
	Outcome  ImportOutcome
	Record   string
	Revision int
}

// Import writes one legacy document as a record, IDEMPOTENT BY CONTENT: the key
// is (store, source id, source hash), so re-running with an unchanged source
// changes nothing and two importers racing on one item converge. A source
// that changed since it was imported appends its newest revision to the record
// it already maps to. Every revision is written by author class migration with
// the old store's own receipt; nothing here mints a person.
func (s *Store) Import(ctx context.Context, run ImportRun, item ImportItem) (ImportResult, error) {
	if err := run.validate(); err != nil {
		return ImportResult{}, err
	}
	if err := item.validate(); err != nil {
		return ImportResult{}, err
	}
	author := Author{Class: AuthorMigration, Ref: run.ID}
	var result ImportResult
	err := s.write(ctx, func(w *writeTx) error {
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO direction_import_runs(id,mode,binary,report_sha256,started_at)
 VALUES (?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, run.ID, run.Mode, run.Binary, run.ReportSHA256, stamp(w.at)); err != nil {
			return err
		}
		key := item.Legacy
		err := w.tx.QueryRowContext(ctx, `SELECT record_id,revision FROM direction_legacy
 WHERE source_store=? AND source_id=? AND source_sha256=?`, key.Store, key.ID, key.SHA256).Scan(&result.Record, &result.Revision)
		if err == nil {
			result.Outcome = Unchanged
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var mapped string
		err = w.tx.QueryRowContext(ctx, `SELECT record_id FROM direction_legacy WHERE source_store=? AND source_id=?
 ORDER BY revision DESC LIMIT 1`, key.Store, key.ID).Scan(&mapped)
		switch {
		case err == nil:
			result.Outcome, result.Record = Appended, mapped
			result.Revision, err = w.importAppend(mapped, item.Revisions[len(item.Revisions)-1], author)
		case errors.Is(err, sql.ErrNoRows):
			result.Outcome = Imported
			result.Record, result.Revision, err = w.importNew(item, author)
		}
		if err != nil {
			return err
		}
		_, err = w.tx.ExecContext(ctx, `INSERT INTO direction_legacy(source_store,source_id,source_version,source_sha256,record_id,revision,import_run)
 VALUES (?,?,?,?,?,?,?)`, key.Store, key.ID, key.Version, key.SHA256, result.Record, result.Revision, run.ID)
		return err
	})
	return result, err
}

func (w *writeTx) importNew(item ImportItem, author Author) (string, int, error) {
	id := item.ID
	if id == "" {
		minted, err := workspace.NewID()
		if err != nil {
			return "", 0, err
		}
		id = minted
	} else if _, err := pointer(w.ctx, w.tx, id); err == nil {
		return "", 0, fmt.Errorf("%w: %s already exists and was not imported from this source", ErrConflict, id)
	} else if !errors.Is(err, ErrNotFound) {
		return "", 0, err
	}
	var prev *Revision
	for i, ir := range item.Revisions {
		r, err := w.importRevision(ir, id, i+1, author, prev)
		if err != nil {
			return "", 0, err
		}
		if err := w.put(r, prev); err != nil {
			return "", 0, err
		}
		prev = &r
	}
	return id, len(item.Revisions), nil
}

func (w *writeTx) importAppend(id string, ir ImportRevision, author Author) (int, error) {
	cur, err := current(w.ctx, w.tx, id)
	if err != nil {
		return 0, err
	}
	r, err := w.importRevision(ir, id, cur.Revision+1, author, &cur)
	if err != nil {
		return 0, err
	}
	return r.Revision, w.put(r, &cur)
}

func (w *writeTx) importRevision(ir ImportRevision, id string, revision int, author Author, prev *Revision) (Revision, error) {
	r, err := w.compose(ir.Draft, id, revision, ir.State, author, prev)
	if err != nil {
		return Revision{}, err
	}
	r.StateReason = ir.StateReason
	r.Receipt = ir.Receipt
	r.WrittenAt = ir.WrittenAt.UTC()
	if r.Receipt != (Receipt{}) && r.Receipt.At.IsZero() {
		// An old receipt that recorded no time is stamped with the import's.
		r.Receipt.At = w.at
	}
	return r, nil
}

func (r ImportRun) validate() error {
	for _, field := range []string{r.ID, r.Mode, r.Binary} {
		if !workspace.ValidLine(field, maxRef) {
			return invalid("an import run names itself, its mode and its binary")
		}
	}
	if r.ReportSHA256 != "" && !isHex(r.ReportSHA256, 64) {
		return invalid("a report hash is 64 lowercase hex characters")
	}
	return nil
}

func (i ImportItem) validate() error {
	switch i.Legacy.Store {
	case LegacyStanding, LegacyContexts, LegacyMemory:
	default:
		return invalid("unknown legacy store %q", i.Legacy.Store)
	}
	if !workspace.ValidLine(i.Legacy.ID, maxRef) || !isHex(i.Legacy.SHA256, 64) || !workspace.ValidLine(i.Legacy.Version, maxRef) {
		return invalid("a legacy source names its id, version and content hash")
	}
	if i.ID != "" && !isRecordID(i.ID) {
		return invalid("a kept identity is a record id")
	}
	if len(i.Revisions) == 0 {
		return invalid("an import carries at least one revision")
	}
	for _, r := range i.Revisions {
		if r.StateReason != "" && !workspace.ValidLine(r.StateReason, maxReason) {
			return invalid("a state reason is at most %d bytes", maxReason)
		}
	}
	return nil
}

// FinishImportRun records that a run ended, with its report and counts.
func (s *Store) FinishImportRun(ctx context.Context, id, reportSHA256, counts string) error {
	if reportSHA256 != "" && !isHex(reportSHA256, 64) {
		return invalid("a report hash is 64 lowercase hex characters")
	}
	return s.write(ctx, func(w *writeTx) error {
		result, err := w.tx.ExecContext(ctx, `UPDATE direction_import_runs SET finished_at=?, report_sha256=?, counts=? WHERE id=?`,
			stamp(w.at), reportSHA256, counts, id)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return fmt.Errorf("%w: import run %s", ErrNotFound, id)
		}
		return nil
	})
}
