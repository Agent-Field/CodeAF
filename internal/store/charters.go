package store

import (
	"context"
	"database/sql"
	"encoding/json"
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
	charter, found, err := s.CharterByID(id)
	if err != nil || !found {
		return Charter{}, fmt.Errorf("draft charter: read materialized charter: %w", err)
	}
	return charter, nil
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
	if err := tx.QueryRow(`SELECT status FROM charters WHERE id = ?`, id).Scan(&status); err != nil {
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
	if err := tx.QueryRow(`SELECT status FROM charters WHERE id = ?`, id).Scan(&status); err != nil {
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
	row := s.db.QueryRow(charterSelect+` WHERE id = ?`, strings.TrimSpace(id))
	charter, err := scanCharter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Charter{}, false, nil
	}
	if err != nil {
		return Charter{}, false, fmt.Errorf("read charter: %w", err)
	}
	return charter, true, nil
}

// Charters returns charters newest first. With no statuses it returns all.
func (s *Store) Charters(statuses ...CharterStatus) ([]Charter, error) {
	query := charterSelect
	args := make([]any, 0, len(statuses))
	if len(statuses) > 0 {
		marks := make([]string, 0, len(statuses))
		for _, status := range statuses {
			marks = append(marks, "?")
			args = append(args, status)
		}
		query += ` WHERE status IN (` + strings.Join(marks, ",") + `)`
	}
	query += ` ORDER BY created_seq DESC, id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list charters: %w", err)
	}
	defer rows.Close()
	var charters []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
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
	return s.Charters(CharterActive)
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
	rows, err := s.db.Query(charterSelect+`
        JOIN charters_fts ON charters_fts.charter_id = charters.id
        WHERE charters.status = ? AND charters_fts MATCH ?
        ORDER BY bm25(charters_fts), charters.created_seq DESC`,
		CharterActive, strings.Join(quoted, " OR "))
	if err != nil {
		return nil, fmt.Errorf("search active charters: %w", err)
	}
	defer rows.Close()
	var charters []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
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

const charterSelect = `SELECT charters.* FROM charters`

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
	watch := watchSpecFromLegacy(payload.Spec)
	action := CharterAction{Template: payload.Spec.Action}
	rails := railsFromLegacy(payload.Spec.Rails)
	nextDue, _ := initialCharterDue(watch, at)
	encodedWatch, _ := json.Marshal(watch)
	encodedAction, _ := json.Marshal(action)
	encodedRails, _ := json.Marshal(rails)
	legacySpec, _ := json.Marshal(payload.Spec)
	ratification, _ := json.Marshal(Ratification{})
	_, err := tx.Exec(`INSERT INTO charters (
		id, session_id, invariant, watch, sentinel_hint, action, rails, status, ratification,
		proposal_shape, next_due, created_seq, updated_seq, legacy_spec, source_command_seq, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?, ?)`,
		payload.ID, payload.SessionID, payload.Spec.Invariant, string(encodedWatch), payload.Spec.Sentinel,
		string(encodedAction), string(encodedRails), CharterDraft, string(ratification), nullTime(nextDue),
		seq, seq, string(legacySpec), payload.SourceCommandSeq, formatTime(at))
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO charters_fts (charter_id, invariant) VALUES (?, ?)`,
		payload.ID, payload.Spec.Invariant)
	return err
}

func applyCharterTransition(tx *sql.Tx, id string, from, to CharterStatus, seq int64) error {
	query := `UPDATE charters SET status = ?, updated_seq = ?`
	args := []any{to, seq}
	if to == CharterActive {
		var sessionID string
		if err := tx.QueryRow(`SELECT session_id FROM charters WHERE id=?`, id).Scan(&sessionID); err != nil {
			return err
		}
		ratification, _ := json.Marshal(Ratification{
			Origin: OriginUser, SessionID: sessionID, Evidence: "yes, stand this up",
		})
		query += `, ratification = ?`
		args = append(args, string(ratification))
	}
	query += ` WHERE id = ?`
	args = append(args, id)
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
	var encoded string
	if err := tx.QueryRow(`SELECT legacy_spec FROM charters WHERE id=? AND status != ?`,
		payload.ID, CharterRetired).Scan(&encoded); err != nil {
		return err
	}
	var spec CharterSpec
	if err := json.Unmarshal([]byte(encoded), &spec); err != nil {
		return err
	}
	spec.Watch.Cadence = payload.Cadence
	spec.Watch.Schedule = payload.Schedule
	legacySpec, _ := json.Marshal(spec)
	watch, _ := json.Marshal(watchSpecFromLegacy(spec))
	result, err := tx.Exec(`UPDATE charters SET legacy_spec=?, watch=?, updated_seq=?
		WHERE id=? AND status != ?`, string(legacySpec), string(watch), seq, payload.ID, CharterRetired)
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

func watchSpecFromLegacy(spec CharterSpec) WatchSpec {
	cadence := strings.ToLower(strings.TrimSpace(spec.Watch.Cadence))
	interval := cadenceInterval(cadence)
	switch spec.Watch.Kind {
	case WatchCron:
		schedule := CronSchedule{Kind: CronDaily, Hour: 9}
		switch {
		case strings.Contains(cadence, "weekday"):
			schedule.Kind = CronWeekdays
		case strings.Contains(cadence, "hour"):
			schedule.Kind, schedule.Interval = CronEveryHours, max(1, interval)
		case strings.Contains(cadence, "minute"):
			schedule.Kind, schedule.Interval = CronEveryMinutes, max(1, interval)
		case strings.Contains(cadence, "afternoon"):
			schedule.Hour = 13
		case strings.Contains(cadence, "evening"):
			schedule.Hour = 18
		}
		return WatchSpec{Kind: WatchCron, Cron: &schedule}
	case WatchFile:
		glob := strings.TrimSpace(spec.Watch.Schedule)
		if glob == "" || glob == "event" {
			glob = "*"
		}
		return WatchSpec{Kind: WatchFile, File: &FileWatch{Glob: glob, Cadence: legacyCadence(interval)}}
	case WatchGraph:
		return WatchSpec{Kind: WatchGraph, Graph: &GraphWatch{
			Predicate: GraphNodeSettled, Title: firstCharterLine(spec.Invariant), Cadence: legacyCadence(interval),
		}}
	default:
		return WatchSpec{Kind: WatchPoll, Poll: &PollWatch{
			Condition: spec.Sentinel, Cadence: legacyCadence(interval),
		}}
	}
}

func railsFromLegacy(legacy CharterRails) CharterRails {
	rails := legacy
	rails.PerFiringBudgetUSD = legacy.EstimatedCostUSD
	if rails.PerFiringBudgetUSD <= 0 {
		rails.PerFiringBudgetUSD = 0.01
	}
	rails.MaxFiringsPerDay = legacy.MaxPerDay
	if rails.MaxFiringsPerDay <= 0 {
		rails.MaxFiringsPerDay = 1
	}
	return rails
}

func legacySpecFromCharter(charter Charter) CharterSpec {
	watch := CharterWatch{Kind: charter.Watch.Kind, Cadence: charter.Watch.String(), Schedule: charter.Watch.String()}
	rails := charter.guardrails
	if rails.EstimatedCostUSD == 0 {
		rails.EstimatedCostUSD = rails.PerFiringBudgetUSD
	}
	if rails.MaxPerDay == 0 {
		rails.MaxPerDay = rails.MaxFiringsPerDay
	}
	if rails.MaxPerDayJustification == "" {
		rails.MaxPerDayJustification = "first-class charter guardrail"
	}
	if rails.Expiry == "" {
		rails.Expiry = "never"
	}
	return CharterSpec{
		Invariant: charter.Invariant, Watch: watch, Sentinel: charter.SentinelHint,
		Action: charter.Action.Template, Rails: rails,
	}
}

func cadenceInterval(cadence string) int {
	for _, field := range strings.Fields(cadence) {
		if value, err := strconv.Atoi(strings.Trim(field, ",.;:")); err == nil && value > 0 {
			return value
		}
	}
	return 2
}

func legacyCadence(interval int) time.Duration {
	return time.Duration(max(1, interval)) * time.Minute
}

func requireCharter(tx *sql.Tx, id string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM charters WHERE id = ?`, id).Scan(&count); err != nil {
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
