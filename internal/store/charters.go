package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// CharterWatch preserves the user's cadence words beside their executable
// schedule. Surfaces speak Cadence; the standing engine consumes Schedule.
type CharterWatch struct {
	Kind     WatchKind `json:"kind"`
	Cadence  string    `json:"cadence"`
	Schedule string    `json:"schedule"`
}

// CharterSpec is the compiled, still-inert form of standing intent.
type CharterSpec struct {
	Invariant string       `json:"invariant"`
	Watch     CharterWatch `json:"watch"`
	Sentinel  string       `json:"sentinel"`
	Action    string       `json:"action"`
	Rails     CharterRails `json:"rails"`
}

const legacyCharterSchema = `
CREATE TABLE IF NOT EXISTS legacy_charters (
    id                         TEXT PRIMARY KEY,
    session_id                 TEXT NOT NULL DEFAULT '',
    status                     TEXT NOT NULL CHECK (status IN ('draft', 'active', 'paused', 'retired')),
    invariant                  TEXT NOT NULL,
    watch_kind                 TEXT NOT NULL CHECK (watch_kind IN ('cron', 'file', 'graph', 'poll')),
    cadence                    TEXT NOT NULL,
    schedule                   TEXT NOT NULL,
    sentinel                   TEXT NOT NULL,
    action                     TEXT NOT NULL,
    estimated_cost_usd         REAL NOT NULL CHECK (estimated_cost_usd >= 0),
    max_per_day                INTEGER NOT NULL CHECK (max_per_day > 0),
    max_per_day_justification  TEXT NOT NULL,
    expiry                     TEXT NOT NULL,
    source_command_seq         INTEGER NOT NULL DEFAULT 0,
    created_seq                INTEGER NOT NULL REFERENCES events(seq),
    updated_seq                INTEGER NOT NULL REFERENCES events(seq),
    created_at                 TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS legacy_charters_status_created ON legacy_charters (status, created_seq);
CREATE VIRTUAL TABLE IF NOT EXISTS legacy_charters_fts USING fts5(
    charter_id UNINDEXED, invariant, tokenize='porter unicode61'
);
`

// migrateLegacyCharterSchema moves the pre-M3 standing-draft projection out
// of the charters table before the standing engine creates its own projection
// under that name. The event journal remains the source of truth; this only
// preserves the existing materialized view across an in-place upgrade.
func migrateLegacyCharterSchema(db *sql.DB) error {
	var definition string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='charters'`).Scan(&definition)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(definition), "watch_kind") {
		return nil
	}
	var legacyExists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='legacy_charters'`).Scan(&legacyExists); err != nil {
		return err
	}
	if legacyExists != 0 {
		return fmt.Errorf("both legacy charter projections exist")
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DROP TABLE IF EXISTS charters_fts`); err != nil {
		return err
	}
	if _, err := tx.Exec(`ALTER TABLE charters RENAME TO legacy_charters`); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := db.Exec(legacyCharterSchema); err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO legacy_charters_fts (charter_id, invariant)
		SELECT id, invariant FROM legacy_charters`)
	return err
}

type charterDraftedPayload struct {
	ID               string      `json:"id"`
	SessionID        string      `json:"session_id,omitempty"`
	Spec             CharterSpec `json:"spec"`
	SourceCommandSeq int64       `json:"source_command_seq,omitempty"`
}

type charterTransitionPayload struct {
	ID string `json:"id"`
}

type charterCadencePayload struct {
	ID       string `json:"id"`
	Cadence  string `json:"cadence"`
	Schedule string `json:"schedule"`
}

// DraftCharter journals an inert charter. id is assigned by the reconciler
// from the source command, making retries converge on the same identity.
func (s *Store) DraftCharter(id, sessionID string, sourceCommandSeq int64, spec CharterSpec) (Charter, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return Charter{}, fmt.Errorf("draft charter: %w: empty id", ErrInvalid)
	}
	if sourceCommandSeq < 0 {
		return Charter{}, fmt.Errorf("draft charter: %w: invalid source command", ErrInvalid)
	}
	if err := validateCharterSpec(spec); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	payload := charterDraftedPayload{
		ID: id, SessionID: strings.TrimSpace(sessionID), Spec: spec, SourceCommandSeq: sourceCommandSeq,
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	defer tx.Rollback()
	seq, at, err := appendEvent(tx, "", EventCharterDrafted, payload)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if err := applyCharterDraft(tx, payload, seq, at); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	return Charter{
		ID: id, SessionID: payload.SessionID, Status: CharterDraft, Spec: spec,
		SourceCommandSeq: sourceCommandSeq, CreatedSeq: seq, UpdatedSeq: seq, CreatedAt: at,
	}, nil
}

// RatifyCharter is the only transition that arms a draft.
func (s *Store) RatifyCharter(id string) error {
	return s.transitionCharter(id, EventCharterRatified, CharterDraft, CharterActive)
}

// PauseCharter makes an active charter inert without discarding its history.
func (s *Store) PauseCharter(id string) error {
	return s.transitionCharter(id, EventCharterPaused, CharterActive, CharterPaused)
}

// RetireCharter permanently disarms either a draft or an armed charter.
func (s *Store) RetireCharter(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("retire charter: %w: empty id", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("retire charter: %w", err)
	}
	defer tx.Rollback()
	var status CharterStatus
	if err := tx.QueryRow(`SELECT status FROM legacy_charters WHERE id = ?`, id).Scan(&status); err != nil {
		return fmt.Errorf("retire charter: %w", charterLookupError(err, id))
	}
	if status == CharterRetired {
		return fmt.Errorf("retire charter: %w: charter %q is already retired", ErrInvalid, id)
	}
	payload := charterTransitionPayload{ID: id}
	seq, _, err := appendEvent(tx, "", EventCharterRetired, payload)
	if err != nil {
		return fmt.Errorf("retire charter: %w", err)
	}
	if err := applyCharterTransition(tx, id, "", CharterRetired, seq); err != nil {
		return fmt.Errorf("retire charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("retire charter: %w", err)
	}
	return nil
}

func (s *Store) transitionCharter(id string, kind EventKind, from, to CharterStatus) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("%s: %w: empty id", kind, ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	defer tx.Rollback()
	payload := charterTransitionPayload{ID: id}
	seq, _, err := appendEvent(tx, "", kind, payload)
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	if err := applyCharterTransition(tx, id, from, to, seq); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return nil
}

// EditCharterCadence journals the conversational rewrite of a watch schedule.
func (s *Store) EditCharterCadence(id, cadence, schedule string) error {
	id = strings.TrimSpace(id)
	cadence = strings.TrimSpace(cadence)
	schedule = strings.TrimSpace(schedule)
	if id == "" || cadence == "" || schedule == "" {
		return fmt.Errorf("edit charter cadence: %w: id, cadence, and schedule are required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	defer tx.Rollback()
	var status CharterStatus
	if err := tx.QueryRow(`SELECT status FROM legacy_charters WHERE id = ?`, id).Scan(&status); err != nil {
		return fmt.Errorf("edit charter cadence: %w", charterLookupError(err, id))
	}
	if status == CharterRetired {
		return fmt.Errorf("edit charter cadence: %w: charter %q is retired", ErrInvalid, id)
	}
	payload := charterCadencePayload{ID: id, Cadence: cadence, Schedule: schedule}
	seq, _, err := appendEvent(tx, "", EventCharterCadenceEdited, payload)
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	if err := applyCharterCadence(tx, payload, seq); err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	return nil
}

// CharterByID returns one charter from the materialized view.
func (s *Store) CharterByID(id string) (Charter, bool, error) {
	row := s.db.QueryRow(legacyCharterSelect+` WHERE id = ?`, strings.TrimSpace(id))
	charter, err := scanLegacyCharter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Charter{}, false, nil
	}
	if err != nil {
		return Charter{}, false, fmt.Errorf("read charter: %w", err)
	}
	return charter, true, nil
}

// Charters returns charters newest first. With no statuses it returns all.
func (s *Store) legacyCharters(statuses ...CharterStatus) ([]Charter, error) {
	query := legacyCharterSelect
	args := make([]any, 0, len(statuses))
	if len(statuses) > 0 {
		marks := make([]string, 0, len(statuses))
		for _, status := range statuses {
			marks = append(marks, "?")
			args = append(args, status)
		}
		query += ` WHERE status IN (` + strings.Join(marks, ",") + `)`
	}
	query += ` ORDER BY created_seq DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list charters: %w", err)
	}
	defer rows.Close()
	var charters []Charter
	for rows.Next() {
		charter, err := scanLegacyCharter(rows)
		if err != nil {
			return nil, fmt.Errorf("list charters: %w", err)
		}
		charters = append(charters, charter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list charters: %w", err)
	}
	return charters, nil
}

// ActiveCharters is the plain standing list used by conversation and /standing.
func (s *Store) ActiveCharters() ([]Charter, error) {
	return s.legacyCharters(CharterActive)
}

// SearchActiveCharters uses SQLite's BM25 rank over invariant text. An
// empty reference deliberately returns every active charter so pronouns can
// resolve when there is exactly one and ask back when there is more than one.
func (s *Store) SearchActiveCharters(reference string) ([]Charter, error) {
	terms := charterSearchTerms(reference)
	if len(terms) == 0 {
		return s.ActiveCharters()
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	rows, err := s.db.Query(legacyCharterSelect+`
		JOIN legacy_charters_fts ON legacy_charters_fts.charter_id = legacy_charters.id
		WHERE legacy_charters.status = ? AND legacy_charters_fts MATCH ?
		ORDER BY bm25(legacy_charters_fts), legacy_charters.created_seq DESC`,
		CharterActive, strings.Join(quoted, " OR "))
	if err != nil {
		return nil, fmt.Errorf("search active charters: %w", err)
	}
	defer rows.Close()
	var charters []Charter
	for rows.Next() {
		charter, err := scanLegacyCharter(rows)
		if err != nil {
			return nil, fmt.Errorf("search active charters: %w", err)
		}
		charters = append(charters, charter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search active charters: %w", err)
	}
	return charters, nil
}

const legacyCharterSelect = `SELECT legacy_charters.id, legacy_charters.session_id, legacy_charters.status,
    legacy_charters.invariant, legacy_charters.watch_kind, legacy_charters.cadence, legacy_charters.schedule,
    legacy_charters.sentinel, legacy_charters.action, legacy_charters.estimated_cost_usd,
    legacy_charters.max_per_day, legacy_charters.max_per_day_justification, legacy_charters.expiry,
    legacy_charters.source_command_seq, legacy_charters.created_seq, legacy_charters.updated_seq,
    legacy_charters.created_at FROM legacy_charters`

type charterScanner interface {
	Scan(dest ...any) error
}

func scanLegacyCharter(scanner charterScanner) (Charter, error) {
	var charter Charter
	var created string
	err := scanner.Scan(&charter.ID, &charter.SessionID, &charter.Status,
		&charter.Spec.Invariant, &charter.Spec.Watch.Kind, &charter.Spec.Watch.Cadence,
		&charter.Spec.Watch.Schedule, &charter.Spec.Sentinel, &charter.Spec.Action,
		&charter.Spec.Rails.EstimatedCostUSD, &charter.Spec.Rails.MaxPerDay,
		&charter.Spec.Rails.MaxPerDayJustification, &charter.Spec.Rails.Expiry,
		&charter.SourceCommandSeq, &charter.CreatedSeq, &charter.UpdatedSeq, &created)
	if err != nil {
		return Charter{}, err
	}
	charter.CreatedAt, err = parseTime(created)
	return charter, err
}

func validateCharterSpec(spec CharterSpec) error {
	if strings.TrimSpace(spec.Invariant) == "" || strings.TrimSpace(spec.Sentinel) == "" ||
		strings.TrimSpace(spec.Action) == "" {
		return fmt.Errorf("%w: invariant, sentinel, and action are required", ErrInvalid)
	}
	switch spec.Watch.Kind {
	case WatchCron, WatchFile, WatchGraph, WatchPoll:
	default:
		return fmt.Errorf("%w: unknown watch kind %q", ErrInvalid, spec.Watch.Kind)
	}
	if strings.TrimSpace(spec.Watch.Cadence) == "" || strings.TrimSpace(spec.Watch.Schedule) == "" {
		return fmt.Errorf("%w: cadence and schedule are required", ErrInvalid)
	}
	rails := spec.Rails
	if rails.EstimatedCostUSD < 0 || math.IsNaN(rails.EstimatedCostUSD) || math.IsInf(rails.EstimatedCostUSD, 0) ||
		rails.MaxPerDay <= 0 || strings.TrimSpace(rails.MaxPerDayJustification) == "" ||
		strings.TrimSpace(rails.Expiry) == "" {
		return fmt.Errorf("%w: complete non-negative rails are required", ErrInvalid)
	}
	return nil
}

func applyCharterDraft(tx *sql.Tx, payload charterDraftedPayload, seq int64, at time.Time) error {
	_, err := tx.Exec(`INSERT INTO legacy_charters (
        id, session_id, status, invariant, watch_kind, cadence, schedule,
        sentinel, action, estimated_cost_usd, max_per_day,
        max_per_day_justification, expiry, source_command_seq, created_seq,
        updated_seq, created_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		payload.ID, payload.SessionID, CharterDraft, payload.Spec.Invariant,
		payload.Spec.Watch.Kind, payload.Spec.Watch.Cadence, payload.Spec.Watch.Schedule,
		payload.Spec.Sentinel, payload.Spec.Action, payload.Spec.Rails.EstimatedCostUSD,
		payload.Spec.Rails.MaxPerDay, payload.Spec.Rails.MaxPerDayJustification,
		payload.Spec.Rails.Expiry, payload.SourceCommandSeq, seq, seq, formatTime(at))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO legacy_charters_fts (charter_id, invariant) VALUES (?, ?)`,
		payload.ID, payload.Spec.Invariant)
	return err
}

func applyCharterTransition(tx *sql.Tx, id string, from, to CharterStatus, seq int64) error {
	query := `UPDATE legacy_charters SET status = ?, updated_seq = ? WHERE id = ?`
	args := []any{to, seq, id}
	if from != "" {
		query += ` AND status = ?`
		args = append(args, from)
	}
	result, err := tx.Exec(query, args...)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: charter %q is missing or not %s", ErrInvalid, id, from)
	}
	return nil
}

func applyCharterCadence(tx *sql.Tx, payload charterCadencePayload, seq int64) error {
	result, err := tx.Exec(`UPDATE legacy_charters SET cadence = ?, schedule = ?, updated_seq = ?
        WHERE id = ? AND status != ?`, payload.Cadence, payload.Schedule, seq, payload.ID, CharterRetired)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return fmt.Errorf("%w: charter %q is missing or retired", ErrInvalid, payload.ID)
	}
	return nil
}

func requireCharter(tx *sql.Tx, id string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM legacy_charters WHERE id = ?`, id).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: charter %q", ErrNotFound, id)
	}
	return nil
}

func charterLookupError(err error, id string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: charter %q", ErrNotFound, id)
	}
	return err
}

func charterSearchTerms(reference string) []string {
	seen := make(map[string]bool)
	var terms []string
	for _, term := range strings.FieldsFunc(strings.ToLower(reference), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	}) {
		if len(term) < 2 || seen[term] {
			continue
		}
		seen[term] = true
		terms = append(terms, term)
	}
	return terms
}

// ScheduleForCadence maps conversational cadence words to the small schedule
// representation consumed by the standing engine. Unknown words remain a
// structured poll interval instead of being treated as cron syntax.
func ScheduleForCadence(cadence string) string {
	lower := strings.ToLower(strings.TrimSpace(cadence))
	switch {
	case strings.Contains(lower, "weekday"):
		hour := cadenceHour(lower, 9)
		return fmt.Sprintf("%d %d * * 1-5", cadenceMinute(lower), hour)
	case strings.Contains(lower, "morning"):
		return "0 9 * * *"
	case strings.Contains(lower, "afternoon"):
		return "0 13 * * *"
	case strings.Contains(lower, "evening"):
		return "0 18 * * *"
	case strings.Contains(lower, "hourly") || strings.Contains(lower, "every hour"):
		return "0 * * * *"
	case strings.Contains(lower, "daily") || strings.Contains(lower, "every day"):
		return "0 9 * * *"
	case strings.Contains(lower, "weekly") || strings.Contains(lower, "every week"):
		return "0 9 * * 1"
	case strings.Contains(lower, "tomorrow"):
		return fmt.Sprintf("at:tomorrowT%02d:%02d", cadenceHour(lower, 9), cadenceMinute(lower))
	}
	fields := strings.Fields(lower)
	for index, field := range fields {
		if index+1 >= len(fields) {
			continue
		}
		count, err := strconv.Atoi(field)
		if err != nil || count <= 0 {
			continue
		}
		switch strings.Trim(fields[index+1], ".,;:") {
		case "minute", "minutes":
			if count < 60 {
				return fmt.Sprintf("*/%d * * * *", count)
			}
		case "hour", "hours":
			if count < 24 {
				return fmt.Sprintf("0 */%d * * *", count)
			}
		}
	}
	return "*/2 * * * *"
}

func cadenceHour(cadence string, fallback int) int {
	fields := strings.FieldsFunc(cadence, func(r rune) bool { return !unicode.IsNumber(r) })
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err == nil && 0 <= value && value <= 23 {
			return value
		}
	}
	return fallback
}

func cadenceMinute(cadence string) int {
	if colon := strings.IndexByte(cadence, ':'); colon >= 0 {
		rest := cadence[colon+1:]
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if minute, err := strconv.Atoi(rest[:end]); err == nil && minute >= 0 && minute <= 59 {
			return minute
		}
	}
	return 0
}
