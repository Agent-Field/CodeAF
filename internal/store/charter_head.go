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

// CharterWatch preserves the user's cadence words beside the structured watch
// consumed by the resident engine.
type CharterWatch struct {
	Kind     WatchKind `json:"kind"`
	Cadence  string    `json:"cadence"`
	Schedule string    `json:"schedule"`
}

// CharterSpec is the ratification-card form produced by the conversational
// compiler. DraftCharter converts it into the engine's bounded representation.
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
	ID           string       `json:"id"`
	Ratification Ratification `json:"ratification,omitempty"`
	Reason       string       `json:"reason,omitempty"`
}

type charterCadencePayload struct {
	ID       string      `json:"id"`
	Cadence  string      `json:"cadence"`
	Schedule string      `json:"schedule"`
	Spec     CharterSpec `json:"spec,omitempty"`
	Watch    WatchSpec   `json:"watch,omitempty"`
	NextDue  time.Time   `json:"next_due,omitempty"`
}

// DraftCharter journals an inert charter through the same materializer used by
// directly constructed engine charters. No origin can bypass probation.
func (s *Store) DraftCharter(id, sessionID string, sourceCommandSeq int64, spec CharterSpec) (Charter, error) {
	id = strings.TrimSpace(id)
	if id == "" || sourceCommandSeq < 0 {
		return Charter{}, fmt.Errorf("draft charter: %w: id and non-negative source command are required", ErrInvalid)
	}
	if err := validateCharterSpec(spec); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	payload := charterDraftedPayload{
		ID: id, SessionID: strings.TrimSpace(sessionID), Spec: spec, SourceCommandSeq: sourceCommandSeq,
	}
	charter, err := charterFromDraft(payload, time.Now())
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id=?`, id).Scan(&exists); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if exists != 0 {
		return Charter{}, fmt.Errorf("draft charter: %w: id %q already exists", ErrInvalid, id)
	}
	seq, at, err := appendEvent(tx, id, EventCharterDrafted, payload)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	charter.CreatedSeq, charter.UpdatedSeq, charter.CreatedAt = seq, seq, at
	if err := applyCharterCreated(tx, charterToRecord(charter), seq, at); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	return charter, nil
}

func charterFromDraft(payload charterDraftedPayload, now time.Time) (Charter, error) {
	watch, err := watchSpecFromCard(payload.Spec, WatchSpec{})
	if err != nil {
		return Charter{}, err
	}
	rails := normalizeCharterRails(payload.Spec.Rails)
	if expires, ok := parseCharterExpiry(rails.Expiry, now); ok {
		rails.ExpiresAt = expires
	}
	action := CharterAction{Template: payload.Spec.Action, SayOnly: isReminderSpec(payload.Spec)}
	charter, err := NewCharter(payload.ID, payload.Spec.Invariant, watch, payload.Spec.Sentinel,
		action, rails, CharterProposed, Ratification{})
	if err != nil {
		return Charter{}, err
	}
	charter.SessionID = payload.SessionID
	charter.Spec = payload.Spec
	charter.Spec.Rails = rails
	charter.SourceCommandSeq = payload.SourceCommandSeq
	charter.CreatedAt = now
	charter.NextDue, err = initialCharterDue(watch, now)
	return charter, err
}

func applyCharterDraft(tx *sql.Tx, payload charterDraftedPayload, seq int64, at time.Time) error {
	charter, err := charterFromDraft(payload, at)
	if err != nil {
		return err
	}
	charter.CreatedSeq, charter.UpdatedSeq = seq, seq
	return applyCharterCreated(tx, charterToRecord(charter), seq, at)
}

// RatifyCharter is retained for callers that do not carry the selected label.
func (s *Store) RatifyCharter(id string) error {
	return s.RatifyCharterWithEvidence(id, "yes, stand this up")
}

func (s *Store) RatifyCharterWithEvidence(id, evidence string) error {
	charter, found, err := s.Charter(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("ratify charter: %w: %q", ErrNotFound, id)
	}
	ratification := Ratification{Origin: OriginUser, SessionID: charter.SessionID, Evidence: strings.TrimSpace(evidence)}
	if ratification.Evidence == "" {
		ratification.Evidence = "yes, stand this up"
	}
	return s.transitionCharter(id, EventCharterRatified, CharterProposed, CharterActive,
		ratification, "user ratified standing spend")
}

func (s *Store) PauseCharter(id string) error {
	return s.transitionCharter(id, EventCharterPaused, CharterActive, CharterPaused,
		Ratification{}, "user paused charter")
}

func (s *Store) RetireCharter(id string) error {
	charter, found, err := s.Charter(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("retire charter: %w: %q", ErrNotFound, id)
	}
	if charter.Status == CharterRetired {
		return fmt.Errorf("retire charter: %w: charter %q is already retired", ErrInvalid, id)
	}
	return s.transitionCharter(id, EventCharterRetired, charter.Status, CharterRetired,
		Ratification{}, "user retired charter")
}

func (s *Store) transitionCharter(id string, kind EventKind, from, to CharterStatus,
	ratification Ratification, reason string) error {
	id = strings.TrimSpace(id)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	defer tx.Rollback()
	current, err := charterInTx(tx, id)
	if err != nil {
		return fmt.Errorf("%s: %w", kind, charterLookupError(err, id))
	}
	if current.Status != from {
		return fmt.Errorf("%s: %w: charter %q is %s", kind, ErrInvalid, id, current.Status)
	}
	if to == CharterActive {
		if !validRatification(ratification) {
			return fmt.Errorf("%s: %w: ratification is required", kind, ErrInvalid)
		}
	} else {
		ratification = current.Ratification
	}
	payload := charterTransitionPayload{ID: id, Ratification: ratification, Reason: reason}
	seq, _, err := appendEvent(tx, id, kind, payload)
	if err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	if err := applyCharterStatus(tx, id, charterStatusPayload{
		Status: to, Ratification: ratification, Reason: reason,
	}, seq); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", kind, err)
	}
	return nil
}

func applyCharterTransition(tx *sql.Tx, payload charterTransitionPayload, from, to CharterStatus, seq int64) error {
	current, err := charterInTx(tx, payload.ID)
	if err != nil {
		return err
	}
	if from != "" && current.Status != from {
		return fmt.Errorf("%w: charter %q is %s", ErrInvalid, payload.ID, current.Status)
	}
	ratification := payload.Ratification
	if !validRatification(ratification) {
		ratification = current.Ratification
	}
	if to == CharterActive && !validRatification(ratification) {
		ratification = Ratification{Origin: OriginUser, SessionID: current.SessionID, Evidence: "legacy ratification"}
	}
	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		reason = "legacy charter transition"
	}
	return applyCharterStatus(tx, payload.ID, charterStatusPayload{
		Status: to, Ratification: ratification, Reason: reason,
	}, seq)
}

func (s *Store) EditCharterCadence(id, cadence, schedule string) error {
	current, found, err := s.Charter(id)
	if err != nil {
		return err
	}
	if !found || current.Status == CharterRetired {
		return fmt.Errorf("edit charter cadence: %w: charter %q is missing or retired", ErrInvalid, id)
	}
	cadence, schedule = strings.TrimSpace(cadence), strings.TrimSpace(schedule)
	if cadence == "" || schedule == "" {
		return fmt.Errorf("edit charter cadence: %w: cadence and schedule are required", ErrInvalid)
	}
	spec := current.Spec
	spec.Watch.Cadence, spec.Watch.Schedule = cadence, schedule
	watch, err := watchSpecFromCard(spec, current.Watch)
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	next, err := initialCharterDue(watch, time.Now())
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	payload := charterCadencePayload{ID: id, Cadence: cadence, Schedule: schedule, Spec: spec, Watch: watch, NextDue: next}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	defer tx.Rollback()
	seq, _, err := appendEvent(tx, id, EventCharterCadenceEdited, payload)
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	if err := applyCharterCadence(tx, payload, seq); err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	return tx.Commit()
}

func applyCharterCadence(tx *sql.Tx, payload charterCadencePayload, seq int64) error {
	current, err := charterInTx(tx, payload.ID)
	if err != nil {
		return err
	}
	if current.Status == CharterRetired {
		return fmt.Errorf("%w: charter %q is retired", ErrInvalid, payload.ID)
	}
	if strings.TrimSpace(payload.Spec.Invariant) == "" {
		payload.Spec = current.Spec
		payload.Spec.Watch.Cadence = payload.Cadence
		payload.Spec.Watch.Schedule = payload.Schedule
	}
	if payload.Watch.Kind == "" {
		payload.Watch, err = watchSpecFromCard(payload.Spec, current.Watch)
		if err != nil {
			return err
		}
	}
	if payload.NextDue.IsZero() {
		payload.NextDue, err = initialCharterDue(payload.Watch, time.Now())
		if err != nil {
			return err
		}
	}
	spec, _ := json.Marshal(payload.Spec)
	watch, _ := json.Marshal(payload.Watch)
	result, err := tx.Exec(`UPDATE charters SET spec=?, watch=?, next_due=?, wake_pending=0,
		sentinel_yes=0, updated_seq=? WHERE id=? AND status!=?`, string(spec), string(watch),
		nullTime(payload.NextDue), seq, payload.ID, CharterRetired)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: charter %q is missing or retired", ErrInvalid, payload.ID)
	}
	return nil
}

func (s *Store) CharterByID(id string) (Charter, bool, error) { return s.Charter(id) }

func (s *Store) ActiveCharters() ([]Charter, error) {
	rows, err := s.db.Query(`SELECT `+charterColumns+` FROM charters WHERE status=? ORDER BY created_seq DESC`, CharterActive)
	if err != nil {
		return nil, fmt.Errorf("list active charters: %w", err)
	}
	defer rows.Close()
	var result []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, charter)
	}
	return result, rows.Err()
}

func (s *Store) SearchActiveCharters(reference string) ([]Charter, error) {
	terms := charterSearchTerms(reference)
	if len(terms) == 0 {
		return s.ActiveCharters()
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	rows, err := s.db.Query(`SELECT `+qualifiedCharterColumns()+` FROM charters
		JOIN charters_fts ON charters_fts.charter_id=charters.id
		WHERE charters.status=? AND charters_fts MATCH ?
		ORDER BY bm25(charters_fts), charters.created_seq DESC`, CharterActive, strings.Join(quoted, " OR "))
	if err != nil {
		return nil, fmt.Errorf("search active charters: %w", err)
	}
	defer rows.Close()
	var result []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, charter)
	}
	return result, rows.Err()
}

func qualifiedCharterColumns() string {
	columns := strings.Join(strings.Fields(charterColumns), " ")
	return "charters." + strings.ReplaceAll(columns, ", ", ", charters.")
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
	rails := normalizeCharterRails(spec.Rails)
	if rails.PerFiringBudgetUSD <= 0 || math.IsNaN(rails.PerFiringBudgetUSD) || math.IsInf(rails.PerFiringBudgetUSD, 0) ||
		rails.MaxFiringsPerDay <= 0 || strings.TrimSpace(rails.MaxPerDayJustification) == "" ||
		strings.TrimSpace(rails.Expiry) == "" {
		return fmt.Errorf("%w: complete positive rails are required", ErrInvalid)
	}
	return nil
}

func charterSpecFromCanonical(charter Charter) CharterSpec {
	rails := normalizeCharterRails(charter.guardrails)
	return CharterSpec{
		Invariant: charter.Invariant,
		Watch:     CharterWatch{Kind: charter.Watch.Kind, Cadence: charter.Watch.String(), Schedule: charter.Watch.String()},
		Sentinel:  charter.SentinelHint,
		Action:    charter.Action.Template,
		Rails:     rails,
	}
}

func charterSpecFromRecord(record charterRecord) CharterSpec {
	return charterSpecFromCanonical(Charter{
		Invariant: record.Invariant, Watch: record.Watch, SentinelHint: record.SentinelHint,
		Action: record.Action, guardrails: record.Rails,
	})
}

func watchSpecFromCard(spec CharterSpec, fallback WatchSpec) (WatchSpec, error) {
	cadence := cadenceDuration(spec.Watch.Cadence)
	switch spec.Watch.Kind {
	case WatchCron:
		return WatchSpec{Kind: WatchCron, Cron: cronFromCadence(spec.Watch.Cadence, spec.Watch.Schedule)}, nil
	case WatchFile:
		glob := strings.TrimSpace(spec.Watch.Schedule)
		if glob == "" || strings.EqualFold(glob, "event") {
			if fallback.File != nil {
				glob = fallback.File.Glob
			} else {
				glob = "."
			}
		}
		return WatchSpec{Kind: WatchFile, File: &FileWatch{Glob: glob, Cadence: cadence}}, nil
	case WatchGraph:
		graph := GraphWatch{Predicate: GraphNodeSettled, Title: spec.Invariant, Cadence: cadence}
		if fallback.Graph != nil {
			graph = *fallback.Graph
			graph.Cadence = cadence
		} else if strings.Contains(strings.ToLower(spec.Watch.Schedule+" "+spec.Sentinel), "fail") {
			graph.Predicate = GraphNodeFailed
		}
		return WatchSpec{Kind: WatchGraph, Graph: &graph}, nil
	case WatchPoll:
		condition := strings.TrimSpace(spec.Sentinel)
		if fallback.Poll != nil && condition == "" {
			condition = fallback.Poll.Condition
		}
		return WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: condition, Cadence: cadence}}, nil
	default:
		return WatchSpec{}, fmt.Errorf("%w: unknown watch kind %q", ErrInvalid, spec.Watch.Kind)
	}
}

func cadenceDuration(cadence string) time.Duration {
	lower := strings.ToLower(cadence)
	fields := strings.Fields(lower)
	for index, field := range fields {
		if index+1 >= len(fields) {
			continue
		}
		value, err := strconv.Atoi(strings.Trim(field, "~.,;:"))
		if err != nil || value <= 0 {
			continue
		}
		switch strings.Trim(fields[index+1], ".,;:") {
		case "minute", "minutes":
			return time.Duration(value) * time.Minute
		case "hour", "hours":
			return time.Duration(value) * time.Hour
		case "day", "days":
			return time.Duration(value) * 24 * time.Hour
		}
	}
	switch {
	case strings.Contains(lower, "hour"):
		return time.Hour
	case strings.Contains(lower, "week"):
		return 7 * 24 * time.Hour
	case strings.Contains(lower, "day"), strings.Contains(lower, "morning"), strings.Contains(lower, "tomorrow"):
		return 24 * time.Hour
	default:
		return 2 * time.Minute
	}
}

func cronFromCadence(cadence, schedule string) *CronSchedule {
	lower := strings.ToLower(cadence + " " + schedule)
	if strings.Contains(lower, "weekday") || strings.Contains(schedule, "1-5") {
		return &CronSchedule{Kind: CronWeekdays, Hour: cadenceHour(lower, 9), Minute: cadenceMinute(lower)}
	}
	if strings.Contains(lower, "*/") {
		parts := strings.SplitN(strings.TrimPrefix(strings.Fields(schedule)[0], "*/"), " ", 2)
		if value, err := strconv.Atoi(parts[0]); err == nil && value > 0 {
			return &CronSchedule{Kind: CronEveryMinutes, Interval: value}
		}
	}
	if strings.Contains(lower, "hour") || strings.Contains(schedule, "* * * *") {
		return &CronSchedule{Kind: CronEveryHours, Interval: 1}
	}
	return &CronSchedule{Kind: CronDaily, Hour: cadenceHour(lower, 9), Minute: cadenceMinute(lower)}
}

func parseCharterExpiry(raw string, now time.Time) (*time.Time, bool) {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" || raw == "never" || raw == "once" {
		return nil, true
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, true
	}
	if strings.HasPrefix(raw, "in ") {
		if duration := cadenceDuration(strings.TrimPrefix(raw, "in ")); duration > 0 {
			expires := now.Add(duration)
			return &expires, true
		}
	}
	return nil, false
}

func isReminderSpec(spec CharterSpec) bool {
	lower := strings.ToLower(spec.Invariant)
	return spec.Rails.Expiry == "once" || strings.Contains(lower, "remind me") ||
		strings.Contains(lower, "notify me") || strings.Contains(lower, "alert me")
}

func requireCharter(tx *sql.Tx, id string) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM charters WHERE id=?`, id).Scan(&count); err != nil {
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
		if len(term) >= 2 && !seen[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	return terms
}

// ScheduleForCadence maps conversational words to the stable card spelling.
func ScheduleForCadence(cadence string) string {
	lower := strings.ToLower(strings.TrimSpace(cadence))
	switch {
	case strings.Contains(lower, "weekday"):
		return fmt.Sprintf("%d %d * * 1-5", cadenceMinute(lower), cadenceHour(lower, 9))
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
		count, err := strconv.Atoi(strings.Trim(field, "~.,;:"))
		if err != nil || count <= 0 {
			continue
		}
		switch strings.Trim(fields[index+1], ".,;:") {
		case "minute", "minutes":
			return fmt.Sprintf("*/%d * * * *", count)
		case "hour", "hours":
			return fmt.Sprintf("0 */%d * * *", count)
		}
	}
	return "*/2 * * * *"
}

func cadenceHour(cadence string, fallback int) int {
	fields := strings.FieldsFunc(cadence, func(r rune) bool { return !unicode.IsNumber(r) })
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err == nil && value >= 0 && value <= 23 {
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
