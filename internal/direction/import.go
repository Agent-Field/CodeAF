package direction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
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
// same hash is the same import, however often it is run. Version is the old
// store's own counters for the document — one or more non-negative integers
// joined by "/", such as a hold's "<Revision>/<SpecRevision>" or a context's
// revision count — compared in order, so an import can tell a newer source
// from an older one it was handed late.
type Legacy struct {
	Store   LegacyStore
	ID      string
	Version string
	SHA256  string
}

// ImportRun is one run of an importer, recorded once however many items it
// writes. Runs are kept forever, like the reports they name (design §1.7).
type ImportRun struct {
	ID           string
	Mode         string
	Binary       string
	ReportSHA256 string
}

// ImportRevision is one revision as the old store had it.
type ImportRevision struct {
	Draft       Draft
	State       State
	StateReason string
	// Receipt is the old store's evidence of who gave the revision authority,
	// copied and never promoted: its actor is legacy_person (a person adopted
	// it through the old store's own door), legacy_delegated or
	// legacy_unknown, and NEVER the person. A person receipt is made only by
	// a PersonReceipt constructor (C24, design F9), so an item that claims
	// one is refused whole.
	Receipt Receipt
	// WrittenAt is when the old store wrote the revision. A zero time is
	// stamped with the import's, and a receipt or exclusion with no time of
	// its own takes the revision's.
	WrittenAt time.Time
}

// ImportItem is one source document.
//
// ID, when set, keeps an identity the old store already gave out AND its
// revision numbers: Revisions[i] is revision i+1, each with its own
// WrittenAt, so a receipt citing (id, revision) reads what the old store's
// revision said. A later import of the same document must agree with every
// revision it repeats, and appends only the ones after them. Without ID the
// record is new, and a changed document appends its current state as one
// revision (design §3.2).
type ImportItem struct {
	Legacy    Legacy
	ID        string
	Revisions []ImportRevision
}

// ImportOutcome says what an import did with one item.
type ImportOutcome string

const (
	Imported  ImportOutcome = "imported"  // a new record
	Unchanged ImportOutcome = "unchanged" // this exact content was imported before
	Appended  ImportOutcome = "appended"  // a newer version, appended to its record
	// Stale is a version no newer than the one the record already carries:
	// an importer that read the source before another importer wrote a newer
	// one. Nothing is written.
	Stale ImportOutcome = "stale"
	// Refused is an item the record cannot take without losing something: a
	// person changed it since it was imported, or the old store's history now
	// disagrees with what was imported. Nothing is written, and Import returns
	// ErrConflict.
	Refused ImportOutcome = "refused"
)

// ImportResult is the record an item maps to, what happened to it, and — for
// an item that wrote nothing — the line the importer's report carries.
type ImportResult struct {
	Outcome  ImportOutcome
	Record   string
	Revision int
	Reason   string
}

// Import writes one legacy document as a record, IDEMPOTENT BY CONTENT: the key
// is (store, source id, source hash), so re-running with an unchanged source
// changes nothing and two importers racing on one item converge.
//
// A document already imported is FENCED ON ITS VERSION: only a newer version
// writes, and it writes only while the record still stands at the revision
// the last import left and was written by that import. A stale version is a
// no-op with a report line; a record a person has changed since, or a history
// that disagrees with what was imported, is refused with ErrConflict. Every
// revision is written by author class migration with the old store's own
// evidence; nothing here mints a person.
func (s *Store) Import(ctx context.Context, run ImportRun, item ImportItem) (ImportResult, error) {
	if err := run.validate(); err != nil {
		return ImportResult{}, err
	}
	version, err := item.validate()
	if err != nil {
		return ImportResult{}, err
	}
	author := Author{Class: AuthorMigration, Ref: run.ID}
	var result ImportResult
	var refusal error
	err = s.write(ctx, func(w *writeTx) error {
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO direction_import_runs(id,mode,binary,report_sha256,started_at)
 VALUES (?,?,?,?,?) ON CONFLICT(id) DO NOTHING`, run.ID, run.Mode, run.Binary, run.ReportSHA256, stamp(w.at)); err != nil {
			return err
		}
		key := item.Legacy
		err := w.tx.QueryRowContext(ctx, legacyByContent, key.Store, key.ID, key.SHA256).Scan(&result.Record, &result.Revision)
		if err == nil {
			result.Outcome = Unchanged
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		last, found, err := w.lastImport(key)
		if err != nil {
			return err
		}
		if !found {
			result.Outcome = Imported
			result.Record, result.Revision, err = w.importNew(item, author)
		} else {
			result, err = w.importNewer(item, version, last, author)
		}
		if err != nil {
			return err
		}
		if result.Outcome == Stale || result.Outcome == Refused {
			// The run row stays: the run happened, and its report says why
			// this item wrote nothing.
			if result.Outcome == Refused {
				refusal = fmt.Errorf("%w: %s %s: %s", ErrConflict, key.Store, key.ID, result.Reason)
			}
			return nil
		}
		_, err = w.tx.ExecContext(ctx, `INSERT INTO direction_legacy(source_store,source_id,source_version,source_sha256,record_id,revision,import_run)
 VALUES (?,?,?,?,?,?,?)`, key.Store, key.ID, key.Version, key.SHA256, result.Record, result.Revision, run.ID)
		return err
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, refusal
}

// The import path's lookups, named so the plan test reads the same text.
const (
	legacyByContent = `SELECT record_id,revision FROM direction_legacy WHERE source_store=? AND source_id=? AND source_sha256=?`
	// legacyNewestBySource finds a document's record by the primary key, then
	// its newest mapping by (record_id, revision) read backwards: two seeks,
	// however often the document was imported.
	legacyNewestBySource = `SELECT g.record_id,g.revision,g.source_version FROM direction_legacy g
 WHERE g.record_id=(SELECT record_id FROM direction_legacy WHERE source_store=? AND source_id=? LIMIT 1)
 ORDER BY g.revision DESC LIMIT 1`
)

// imported is the newest mapping of a document: the record it went to, the
// revision the last import left it at, and the version that import carried.
type imported struct {
	record   string
	revision int
	version  []int
	spelled  string
}

func (w *writeTx) lastImport(key Legacy) (imported, bool, error) {
	var last imported
	err := w.tx.QueryRowContext(w.ctx, legacyNewestBySource, key.Store, key.ID).Scan(&last.record, &last.revision, &last.spelled)
	if errors.Is(err, sql.ErrNoRows) {
		return imported{}, false, nil
	}
	if err != nil {
		return imported{}, false, err
	}
	var ok bool
	if last.version, ok = parseVersion(last.spelled); !ok {
		return imported{}, false, fmt.Errorf("%w: %s %s was imported at version %q, which does not order", ErrInvalid, key.Store, key.ID, last.spelled)
	}
	return last, true, nil
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
	if err := w.importRevisions(item.Revisions, id, nil, author); err != nil {
		return "", 0, err
	}
	return id, len(item.Revisions), nil
}

// importNewer takes a changed version of a document already imported.
func (w *writeTx) importNewer(item ImportItem, version []int, last imported, author Author) (ImportResult, error) {
	result := ImportResult{Record: last.record, Revision: last.revision}
	if compareVersions(version, last.version) <= 0 {
		result.Outcome = Stale
		result.Reason = fmt.Sprintf("version %s is not newer than version %s, already imported at revision %d",
			item.Legacy.Version, last.spelled, last.revision)
		return result, nil
	}
	if item.ID != "" && item.ID != last.record {
		return ImportResult{}, invalid("%s %s was imported as %s, not %s", item.Legacy.Store, item.Legacy.ID, last.record, item.ID)
	}
	cur, err := current(w.ctx, w.tx, last.record)
	if err != nil {
		return ImportResult{}, err
	}
	if cur.Revision != last.revision || cur.Author.Class != AuthorMigration {
		result.Outcome, result.Revision = Refused, cur.Revision
		result.Reason = fmt.Sprintf("revision %d (%s, by %s) was written after the import left revision %d; that change stands",
			cur.Revision, cur.State, cur.Author.Class, last.revision)
		return result, nil
	}
	if item.ID == "" {
		// A document without numbered history appends its current state.
		r, err := w.importRevision(item.Revisions[len(item.Revisions)-1], cur.ID, cur.Revision+1, author, &cur)
		if err != nil {
			return ImportResult{}, err
		}
		result.Outcome, result.Revision = Appended, r.Revision
		return result, w.put(r, &cur)
	}
	// A numbered history must agree with every revision already imported,
	// and appends the ones after them under their own numbers.
	if diverged, err := w.divergence(item, cur); err != nil || diverged != "" {
		result.Outcome, result.Reason = Refused, diverged
		return result, err
	}
	if len(item.Revisions) <= cur.Revision {
		result.Outcome = Stale
		result.Reason = fmt.Sprintf("version %s carries no revision after %d", item.Legacy.Version, cur.Revision)
		return result, nil
	}
	if err := w.importRevisions(item.Revisions[cur.Revision:], cur.ID, &cur, author); err != nil {
		return ImportResult{}, err
	}
	result.Outcome, result.Revision = Appended, len(item.Revisions)
	return result, nil
}

// importRevisions writes revisions in order after prev (nil for a new record).
func (w *writeTx) importRevisions(revs []ImportRevision, id string, prev *Revision, author Author) error {
	next := 1
	if prev != nil {
		next = prev.Revision + 1
	}
	for i, ir := range revs {
		r, err := w.importRevision(ir, id, next+i, author, prev)
		if err != nil {
			return err
		}
		if err := w.put(r, prev); err != nil {
			return err
		}
		prev = &r
	}
	return nil
}

// divergence compares a numbered history with the revisions already stored
// under the same numbers, and says where they first disagree.
func (w *writeTx) divergence(item ImportItem, cur Revision) (string, error) {
	var prev *Revision
	for n := 1; n <= cur.Revision && n <= len(item.Revisions); n++ {
		stored, err := load(w.ctx, w.tx, cur.ID, n)
		if err != nil {
			return "", err
		}
		supplied, err := w.importRevision(item.Revisions[n-1], cur.ID, n, Author{Class: AuthorMigration}, prev)
		if err != nil {
			return "", err
		}
		if differ := sameContent(stored, supplied); differ != nil {
			return fmt.Sprintf("the old store's revision %d no longer says what revision %d was imported as: %v", n, n, differ), nil
		}
		prev = &stored
	}
	return "", nil
}

func (w *writeTx) importRevision(ir ImportRevision, id string, revision int, author Author, prev *Revision) (Revision, error) {
	at := ir.WrittenAt.UTC()
	if ir.WrittenAt.IsZero() {
		at = w.at
	}
	d, err := ir.Draft.normalize(true, inheritedFrom(prev), at)
	if err != nil {
		return Revision{}, err
	}
	r := Revision{ID: id, Revision: revision, Kind: d.Kind, State: ir.State, StateReason: ir.StateReason, Title: d.Title,
		Text: d.Text, Quote: d.Quote, QuoteOrigin: d.QuoteOrigin, Source: d.Source, Author: author, Receipt: ir.Receipt,
		Targets: d.Targets, Exclusions: d.Exclusions, Links: d.Links, WrittenAt: at}
	if r.Receipt != (Receipt{}) && r.Receipt.At.IsZero() {
		r.Receipt.At = at
	}
	return r, nil
}

// sameContent reports how two revisions of one record differ in what they
// say, or nil. Which run wrote them, their sequence and derived hash do not
// count.
func sameContent(a, b Revision) error {
	canonical := func(r Revision) string {
		ex := append([]Exclusion(nil), r.Exclusions...)
		sort.Slice(ex, func(i, j int) bool {
			return fmt.Sprint(ex[i].Kind, ex[i].Ref, ex[i].Session) < fmt.Sprint(ex[j].Kind, ex[j].Ref, ex[j].Session)
		})
		links := append([]Link(nil), r.Links...)
		sort.Slice(links, func(i, j int) bool {
			return fmt.Sprint(links[i].Kind, links[i].To) < fmt.Sprint(links[j].Kind, links[j].To)
		})
		exclusions := make([]string, len(ex))
		for i, e := range ex {
			exclusions[i] = fmt.Sprint(e.Kind, "|", e.Ref, "|", e.Session, "|", stamp(e.At))
		}
		return fmt.Sprintf("%s|%s|%s|%q|%q|%q|%s|%+v|%s|%s|%s|%q|%s|%+v|%v|%+v|%s", r.Kind, r.State, r.StateReason, r.Title, r.Text,
			r.Quote, r.QuoteOrigin, r.Source, r.Author.Class, r.Receipt.Actor, r.Receipt.Door, r.Receipt.Ref, stamp(r.Receipt.At),
			r.Targets, exclusions, links, stamp(r.WrittenAt))
	}
	if ca, cb := canonical(a), canonical(b); ca != cb {
		return fmt.Errorf("stored %s, supplied %s", truncate(ca, 160), truncate(cb, 160))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// parseVersion reads a version's counters.
func parseVersion(v string) ([]int, bool) {
	parts := strings.Split(v, "/")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" || len(p) > 18 || strings.TrimLeft(p, "0123456789") != "" {
			return nil, false
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// compareVersions orders two versions counter by counter; a version that is
// a prefix of another is the older.
func compareVersions(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return len(a) - len(b)
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

// validate refuses a malformed item whole and returns its parsed version.
func (i ImportItem) validate() ([]int, error) {
	switch i.Legacy.Store {
	case LegacyStanding, LegacyContexts, LegacyMemory:
	default:
		return nil, invalid("unknown legacy store %q", i.Legacy.Store)
	}
	if !workspace.ValidLine(i.Legacy.ID, maxRef) || !isHex(i.Legacy.SHA256, 64) {
		return nil, invalid("a legacy source names its id and content hash")
	}
	version, ok := parseVersion(i.Legacy.Version)
	if !ok {
		return nil, invalid("a legacy version is non-negative counters joined by \"/\", not %q", i.Legacy.Version)
	}
	if i.ID != "" && !isRecordID(i.ID) {
		return nil, invalid("a kept identity is a record id")
	}
	if len(i.Revisions) == 0 {
		return nil, invalid("an import carries at least one revision")
	}
	for n, r := range i.Revisions {
		if r.StateReason != "" && !workspace.ValidLine(r.StateReason, maxReason) {
			return nil, invalid("a state reason is at most %d bytes", maxReason)
		}
		if r.Receipt.Actor == ActorPerson {
			return nil, invalid("revision %d claims a person receipt; an import copies legacy evidence and never spells the person", n+1)
		}
		if i.ID != "" && r.WrittenAt.IsZero() {
			return nil, invalid("revision %d of a kept identity carries the time the old store wrote it", n+1)
		}
	}
	return version, nil
}

// maxCounts bounds what a finished run records about itself.
const maxCounts = 4096

// FinishImportRun records that a run ended, with its report and counts: a
// JSON object of at most maxCounts bytes. The row is kept forever with the
// report it names (design §1.7).
func (s *Store) FinishImportRun(ctx context.Context, id, reportSHA256, counts string) error {
	if !workspace.ValidLine(id, maxRef) {
		return invalid("an import run is named")
	}
	if reportSHA256 != "" && !isHex(reportSHA256, 64) {
		return invalid("a report hash is 64 lowercase hex characters")
	}
	if len(counts) > maxCounts || !json.Valid([]byte(counts)) || !strings.HasPrefix(strings.TrimSpace(counts), "{") {
		return invalid("a run's counts are a JSON object of at most %d bytes", maxCounts)
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
