package store

// This file is the compatibility membrane between the conversational charter
// compiler and the structured standing engine. The merge that introduced both
// originally carried two stores and two schemas; these adapters keep one
// journal and one materialized charter view.

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

// CharterWatch preserves the user's cadence wording beside the structured
// watch used by the standing engine.
type CharterWatch struct {
	Kind     WatchKind `json:"kind"`
	Cadence  string    `json:"cadence"`
	Schedule string    `json:"schedule"`
}

// CharterSpec is the inert form produced by the conversational compiler.
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

// DraftCharter journals an inert charter in the standing engine's sole view.
func (s *Store) DraftCharter(id, sessionID string, sourceCommandSeq int64, spec CharterSpec) (Charter, error) {
	id = strings.TrimSpace(id)
	sessionID = strings.TrimSpace(sessionID)
	if id == "" || sourceCommandSeq < 0 {
		return Charter{}, fmt.Errorf("draft charter: %w: invalid identity", ErrInvalid)
	}
	if err := validateCharterSpec(spec); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	payload := charterDraftedPayload{ID: id, SessionID: sessionID, Spec: spec, SourceCommandSeq: sourceCommandSeq}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	defer tx.Rollback()
	seq, at, err := appendEvent(tx, id, EventCharterDrafted, payload)
	if err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if err := applyCharterDraft(tx, payload, seq, at); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Charter{}, fmt.Errorf("draft charter: %w", err)
	}
	charter, found, err := s.Charter(id)
	if err != nil || !found {
		return Charter{}, fmt.Errorf("draft charter: read materialized charter: %w", err)
	}
	return charter, nil
}

func applyCharterDraft(tx *sql.Tx, payload charterDraftedPayload, seq int64, at time.Time) error {
	watch := structuredWatch(payload.Spec.Watch)
	next, err := initialCharterDue(watch, at)
	if err != nil {
		return err
	}
	rails := normalizedThreadRails(payload.Spec.Rails)
	record := charterRecord{
		ID: payload.ID, Invariant: payload.Spec.Invariant, Watch: watch,
		SentinelHint: payload.Spec.Sentinel, Action: CharterAction{Template: payload.Spec.Action},
		Rails: rails, Status: CharterDraft,
		Ratification:  Ratification{Origin: OriginUser, SessionID: payload.SessionID},
		ProposalShape: fmt.Sprintf("thread-command:%d", payload.SourceCommandSeq), NextDue: next,
	}
	return applyCharterCreated(tx, record, seq, at)
}

// RatifyCharter is the only conversational transition that arms a draft.
func (s *Store) RatifyCharter(id string) error {
	return s.transitionThreadCharter(id, EventCharterRatified, CharterDraft, CharterActive)
}

func (s *Store) PauseCharter(id string) error {
	return s.transitionThreadCharter(id, EventCharterPaused, CharterActive, CharterPaused)
}

func (s *Store) RetireCharter(id string) error {
	return s.transitionThreadCharter(id, EventCharterRetired, "", CharterRetired)
}

func (s *Store) transitionThreadCharter(id string, kind EventKind, from, to CharterStatus) error {
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
	seq, _, err := appendEvent(tx, id, kind, payload)
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

func applyCharterTransition(tx *sql.Tx, id string, from, to CharterStatus, seq int64) error {
	var current CharterStatus
	var ratificationRaw string
	if err := tx.QueryRow(`SELECT status, ratification FROM charters WHERE id = ?`, id).Scan(&current, &ratificationRaw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: charter %q", ErrNotFound, id)
		}
		return err
	}
	if current == CharterRetired || (from != "" && current != from) {
		return fmt.Errorf("%w: charter %q is %s, want %s", ErrInvalid, id, current, from)
	}
	var ratification Ratification
	_ = json.Unmarshal([]byte(ratificationRaw), &ratification)
	if to == CharterActive {
		ratification.Origin = OriginUser
		ratification.Evidence = "ratified in thread"
	}
	encoded, _ := json.Marshal(ratification)
	result, err := tx.Exec(`UPDATE charters SET status = ?, ratification = ?, updated_seq = ? WHERE id = ?`,
		to, string(encoded), seq, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: charter %q", ErrNotFound, id)
	}
	_, err = tx.Exec(`UPDATE nodes SET updated_seq = ? WHERE id = ?`, seq, id)
	return err
}

// EditCharterCadence journals the user's wording and deterministically updates
// the structured watch in the standing view.
func (s *Store) EditCharterCadence(id, cadence, schedule string) error {
	id, cadence, schedule = strings.TrimSpace(id), strings.TrimSpace(cadence), strings.TrimSpace(schedule)
	if id == "" || cadence == "" || schedule == "" {
		return fmt.Errorf("edit charter cadence: %w: id, cadence, and schedule are required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("edit charter cadence: %w", err)
	}
	defer tx.Rollback()
	payload := charterCadencePayload{ID: id, Cadence: cadence, Schedule: schedule}
	seq, _, err := appendEvent(tx, id, EventCharterCadenceEdited, payload)
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

func applyCharterCadence(tx *sql.Tx, payload charterCadencePayload, seq int64) error {
	row := tx.QueryRow(`SELECT `+charterColumns+` FROM charters WHERE id = ?`, payload.ID)
	charter, err := scanCharter(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: charter %q", ErrNotFound, payload.ID)
		}
		return err
	}
	if charter.Status == CharterRetired {
		return fmt.Errorf("%w: charter %q is retired", ErrInvalid, payload.ID)
	}
	compat := charter.Spec.Watch
	compat.Cadence, compat.Schedule = payload.Cadence, payload.Schedule
	watch := structuredWatch(compat)
	encoded, _ := json.Marshal(watch)
	result, err := tx.Exec(`UPDATE charters SET watch = ?, updated_seq = ? WHERE id = ?`,
		string(encoded), seq, payload.ID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("%w: charter %q", ErrNotFound, payload.ID)
	}
	return nil
}

func (s *Store) CharterByID(id string) (Charter, bool, error) {
	return s.Charter(strings.TrimSpace(id))
}

func (s *Store) ActiveCharters() ([]Charter, error) {
	all, err := s.Charters()
	if err != nil {
		return nil, err
	}
	active := make([]Charter, 0, len(all))
	for _, charter := range all {
		if charter.Status == CharterActive {
			active = append(active, charter)
		}
	}
	return active, nil
}

func (s *Store) SearchActiveCharters(reference string) ([]Charter, error) {
	active, err := s.ActiveCharters()
	if err != nil || strings.TrimSpace(reference) == "" {
		return active, err
	}
	terms := charterSearchTerms(reference)
	matches := make([]Charter, 0, len(active))
	for _, charter := range active {
		words := charterSearchTerms(charter.Invariant)
		matched := false
		for _, term := range terms {
			for _, word := range words {
				matched = matched || strings.HasPrefix(term, word) || strings.HasPrefix(word, term)
			}
		}
		if matched {
			matches = append(matches, charter)
		}
	}
	return matches, nil
}

func requireCharter(tx *sql.Tx, id string) error {
	var found bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM charters WHERE id = ?)`, id).Scan(&found); err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: charter %q", ErrNotFound, id)
	}
	return nil
}

func validateCharterSpec(spec CharterSpec) error {
	if strings.TrimSpace(spec.Invariant) == "" || strings.TrimSpace(spec.Sentinel) == "" || strings.TrimSpace(spec.Action) == "" {
		return fmt.Errorf("%w: invariant, sentinel, and action are required", ErrInvalid)
	}
	if strings.TrimSpace(spec.Watch.Cadence) == "" || strings.TrimSpace(spec.Watch.Schedule) == "" {
		return fmt.Errorf("%w: cadence and schedule are required", ErrInvalid)
	}
	switch spec.Watch.Kind {
	case WatchCron, WatchFile, WatchGraph, WatchPoll:
	default:
		return fmt.Errorf("%w: unknown watch kind %q", ErrInvalid, spec.Watch.Kind)
	}
	rails := spec.Rails
	if rails.EstimatedCostUSD < 0 || math.IsNaN(rails.EstimatedCostUSD) || math.IsInf(rails.EstimatedCostUSD, 0) ||
		rails.MaxPerDay <= 0 || strings.TrimSpace(rails.MaxPerDayJustification) == "" || strings.TrimSpace(rails.Expiry) == "" {
		return fmt.Errorf("%w: complete non-negative rails are required", ErrInvalid)
	}
	return nil
}

func normalizedThreadRails(rails CharterRails) CharterRails {
	rails.PerFiringBudgetUSD = rails.EstimatedCostUSD
	if rails.PerFiringBudgetUSD <= 0 {
		rails.PerFiringBudgetUSD = 0.000001
	}
	rails.MaxFiringsPerDay = rails.MaxPerDay
	if rails.MaxFiringsPerDay <= 0 {
		rails.MaxFiringsPerDay = 1
	}
	if rails.Expiry != "" && !strings.EqualFold(rails.Expiry, "never") {
		if parsed, err := time.Parse(time.RFC3339, rails.Expiry); err == nil {
			rails.ExpiresAt = &parsed
		}
	}
	return rails
}

func structuredWatch(watch CharterWatch) WatchSpec {
	cadence := compatibilityCadence(watch.Cadence)
	switch watch.Kind {
	case WatchCron:
		return WatchSpec{Kind: WatchCron, Cron: parseCompatibilityCron(watch.Schedule)}
	case WatchFile:
		return WatchSpec{Kind: WatchFile, File: &FileWatch{Glob: watch.Schedule, Cadence: cadence}}
	case WatchGraph:
		return WatchSpec{Kind: WatchGraph, Graph: &GraphWatch{
			Predicate: GraphNodeSettled, Title: watch.Schedule, Cadence: cadence,
		}}
	default:
		return WatchSpec{Kind: WatchPoll, Poll: &PollWatch{Condition: watch.Schedule, Cadence: cadence}}
	}
}

func parseCompatibilityCron(raw string) *CronSchedule {
	fields := strings.Fields(raw)
	if len(fields) == 5 {
		minute, minuteErr := strconv.Atoi(fields[0])
		hour, hourErr := strconv.Atoi(fields[1])
		if strings.HasPrefix(fields[0], "*/") {
			if interval, err := strconv.Atoi(strings.TrimPrefix(fields[0], "*/")); err == nil {
				return &CronSchedule{Kind: CronEveryMinutes, Interval: interval}
			}
		}
		if fields[0] == "0" && strings.HasPrefix(fields[1], "*/") {
			if interval, err := strconv.Atoi(strings.TrimPrefix(fields[1], "*/")); err == nil {
				return &CronSchedule{Kind: CronEveryHours, Interval: interval}
			}
		}
		if minuteErr == nil && hourErr == nil {
			kind := CronDaily
			if fields[4] == "1-5" {
				kind = CronWeekdays
			}
			return &CronSchedule{Kind: kind, Hour: hour, Minute: minute}
		}
	}
	return &CronSchedule{Kind: CronEveryHours, Interval: 1}
}

func compatibilityCadence(value string) time.Duration {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "minute") {
		return time.Minute
	}
	if strings.Contains(lower, "hour") {
		return time.Hour
	}
	return 24 * time.Hour
}

func populateThreadCharter(charter *Charter) {
	charter.SessionID = charter.Ratification.SessionID
	charter.Spec = CharterSpec{
		Invariant: charter.Invariant,
		Watch:     CharterWatch{Kind: charter.Watch.Kind, Cadence: compatibleCadence(charter.Watch), Schedule: compatibleSchedule(charter.Watch)},
		Sentinel:  charter.SentinelHint, Action: charter.Action.Template, Rails: charter.guardrails,
	}
	if charter.Spec.Rails.EstimatedCostUSD == 0 {
		charter.Spec.Rails.EstimatedCostUSD = charter.guardrails.PerFiringBudgetUSD
	}
	if charter.Spec.Rails.MaxPerDay == 0 {
		charter.Spec.Rails.MaxPerDay = charter.guardrails.MaxFiringsPerDay
	}
	if charter.Spec.Rails.MaxPerDayJustification == "" {
		charter.Spec.Rails.MaxPerDayJustification = "bounded by the standing charter daily cap"
	}
	if charter.Spec.Rails.Expiry == "" {
		charter.Spec.Rails.Expiry = "never"
		if charter.guardrails.ExpiresAt != nil {
			charter.Spec.Rails.Expiry = charter.guardrails.ExpiresAt.Format(time.RFC3339)
		}
	}
	if strings.HasPrefix(charter.ProposalShape, "thread-command:") {
		charter.SourceCommandSeq, _ = strconv.ParseInt(strings.TrimPrefix(charter.ProposalShape, "thread-command:"), 10, 64)
	}
}

func compatibleCadence(watch WatchSpec) string {
	if watch.Cron != nil {
		switch watch.Cron.Kind {
		case CronEveryMinutes:
			return fmt.Sprintf("every %d minutes", watch.Cron.Interval)
		case CronEveryHours:
			if watch.Cron.Interval == 1 {
				return "hourly"
			}
			return fmt.Sprintf("every %d hours", watch.Cron.Interval)
		case CronDaily:
			if watch.Cron.Hour == 9 && watch.Cron.Minute == 0 {
				return "every morning"
			}
			return fmt.Sprintf("daily at %02d:%02d", watch.Cron.Hour, watch.Cron.Minute)
		case CronWeekdays:
			return fmt.Sprintf("weekdays at %02d:%02d", watch.Cron.Hour, watch.Cron.Minute)
		}
	}
	return strings.TrimPrefix(watch.String(), string(watch.Kind)+":")
}

func compatibleSchedule(watch WatchSpec) string {
	if watch.Cron != nil {
		switch watch.Cron.Kind {
		case CronEveryMinutes:
			return fmt.Sprintf("*/%d * * * *", watch.Cron.Interval)
		case CronEveryHours:
			if watch.Cron.Interval == 1 {
				return "0 * * * *"
			}
			return fmt.Sprintf("0 */%d * * *", watch.Cron.Interval)
		case CronDaily:
			return fmt.Sprintf("%d %d * * *", watch.Cron.Minute, watch.Cron.Hour)
		case CronWeekdays:
			return fmt.Sprintf("%d %d * * 1-5", watch.Cron.Minute, watch.Cron.Hour)
		}
	}
	return watch.String()
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

// ScheduleForCadence maps the conversational cadence vocabulary to the legacy
// schedule carried in compiler output; structuredWatch normalizes it later.
func ScheduleForCadence(cadence string) string {
	lower := strings.ToLower(strings.TrimSpace(cadence))
	switch {
	case strings.Contains(lower, "tomorrow"):
		hour := 9
		for _, field := range strings.FieldsFunc(lower, func(r rune) bool { return !unicode.IsNumber(r) }) {
			if parsed, err := strconv.Atoi(field); err == nil && parsed >= 0 && parsed <= 23 {
				hour = parsed
				break
			}
		}
		return fmt.Sprintf("at:tomorrowT%02d:00", hour)
	case strings.Contains(lower, "weekday"):
		return "0 9 * * 1-5"
	case strings.Contains(lower, "hour"):
		return "0 * * * *"
	case strings.Contains(lower, "week"):
		return "0 9 * * 1"
	case strings.Contains(lower, "morning"), strings.Contains(lower, "daily"), strings.Contains(lower, "every day"):
		return "0 9 * * *"
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
			return fmt.Sprintf("*/%d * * * *", count)
		case "hour", "hours":
			return fmt.Sprintf("0 */%d * * *", count)
		}
	}
	return "*/2 * * * *"
}
