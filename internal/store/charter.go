package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// CharterStatus is the ratification lifecycle of standing intent.
type CharterStatus string

const (
	CharterProposed CharterStatus = "proposed"
	CharterActive   CharterStatus = "active"
	CharterPaused   CharterStatus = "paused"
	CharterRetired  CharterStatus = "retired"
)

// WatchKind names the deterministic mechanism that wakes a sentinel.
type WatchKind string

const (
	WatchCron  WatchKind = "cron"
	WatchFile  WatchKind = "file"
	WatchGraph WatchKind = "graph"
	WatchPoll  WatchKind = "poll"
)

// CronKind is one supported structured schedule. Raw cron expressions are
// deliberately not represented by this type.
type CronKind string

const (
	CronEveryMinutes CronKind = "every_minutes"
	CronEveryHours   CronKind = "every_hours"
	CronDaily        CronKind = "daily"
	CronWeekdays     CronKind = "weekdays"
)

// CronSchedule is a local-time schedule with no raw-cron escape hatch.
type CronSchedule struct {
	Kind     CronKind `json:"kind"`
	Interval int      `json:"interval,omitempty"`
	Hour     int      `json:"hour,omitempty"`
	Minute   int      `json:"minute,omitempty"`
}

// FileWatch is an mtime-polled glob. Cadence bounds filesystem work even when
// the resident reconciler ticks several times per second.
type FileWatch struct {
	Glob    string        `json:"glob"`
	Cadence time.Duration `json:"cadence"`
}

// GraphPredicate is one durable graph transition a charter can observe.
type GraphPredicate string

const (
	GraphNodeSettled    GraphPredicate = "node_settled"
	GraphNodeFailed     GraphPredicate = "node_failed"
	GraphSpendThreshold GraphPredicate = "spend_threshold"
)

// GraphWatch matches node transitions by title and/or notebook scope, or one
// daily spend threshold. Cadence controls how often the journal is scanned.
type GraphWatch struct {
	Predicate    GraphPredicate `json:"predicate"`
	Title        string         `json:"title,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	ThresholdUSD float64        `json:"threshold_usd,omitempty"`
	Cadence      time.Duration  `json:"cadence"`
}

// PollWatch asks the sentinel to inspect a broad external condition on a fixed
// cadence. Condition is kept verbatim for the sentinel prompt.
type PollWatch struct {
	Condition string        `json:"condition"`
	Cadence   time.Duration `json:"cadence"`
}

// WatchSpec is exactly one of cron, file, graph, or poll.
type WatchSpec struct {
	Kind  WatchKind     `json:"kind"`
	Cron  *CronSchedule `json:"cron,omitempty"`
	File  *FileWatch    `json:"file,omitempty"`
	Graph *GraphWatch   `json:"graph,omitempty"`
	Poll  *PollWatch    `json:"poll,omitempty"`
}

// String renders the internal structured schedule with the interface's stable
// watch-family prefix.
func (watch WatchSpec) String() string {
	switch watch.Kind {
	case WatchCron:
		if watch.Cron == nil {
			return "cron:"
		}
		switch watch.Cron.Kind {
		case CronEveryMinutes:
			return fmt.Sprintf("cron:every %d minutes", watch.Cron.Interval)
		case CronEveryHours:
			return fmt.Sprintf("cron:every %d hours", watch.Cron.Interval)
		case CronDaily:
			return fmt.Sprintf("cron:daily %02d:%02d", watch.Cron.Hour, watch.Cron.Minute)
		case CronWeekdays:
			return fmt.Sprintf("cron:weekdays %02d:%02d", watch.Cron.Hour, watch.Cron.Minute)
		}
	case WatchFile:
		if watch.File != nil {
			return "file:" + watch.File.Glob
		}
		return "file:"
	case WatchGraph:
		if watch.Graph != nil {
			return "graph:" + string(watch.Graph.Predicate)
		}
		return "graph:"
	case WatchPoll:
		if watch.Poll != nil {
			return "poll:" + watch.Poll.Condition
		}
		return "poll:"
	}
	return string(watch.Kind) + ":"
}

// CharterAction is re-grounded into ordinary work when a sentinel answers
// yes. SayOnly is the reminder path: it posts attention instead of a job.
type CharterAction struct {
	Template string `json:"template"`
	SayOnly  bool   `json:"say_only,omitempty"`
}

// CharterRails bound every firing. ExpiresAt nil means never.
type CharterRails struct {
	PerFiringBudgetUSD float64    `json:"per_firing_budget_usd"`
	MaxFiringsPerDay   int        `json:"max_firings_per_day"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
}

// Ratification records who accepted the standing-spend consequence.
type Ratification struct {
	Origin    Origin `json:"origin"`
	SessionID string `json:"session_id,omitempty"`
	Evidence  string `json:"evidence"`
}

// Charter is one first-class standing responsibility. Rails are private so a
// caller cannot construct a usable charter while omitting them; NewCharter is
// the only admission constructor.
type Charter struct {
	ID            string
	Invariant     string
	Watch         WatchSpec
	SentinelHint  string
	Action        CharterAction
	Status        CharterStatus
	Ratification  Ratification
	ProposalShape string

	LastWake        time.Time
	NextDue         time.Time
	WakeSeq         int64
	WakePending     bool
	SentinelYes     bool
	WakeEvidence    string
	FileFingerprint string
	GraphCursor     int64
	GraphDay        string
	GraphTriggered  bool
	CreatedSeq      int64
	UpdatedSeq      int64

	guardrails CharterRails
}

// Rails returns the immutable bounds carried by a charter.
func (c Charter) Rails() CharterRails { return c.guardrails }

// NewCharter validates every field that can make standing work unbounded.
func NewCharter(id, invariant string, watch WatchSpec, sentinelHint string,
	action CharterAction, rails CharterRails, status CharterStatus,
	ratification Ratification) (Charter, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == RootID {
		return Charter{}, fmt.Errorf("new charter: %w: non-root id is required", ErrInvalid)
	}
	if strings.TrimSpace(invariant) == "" {
		return Charter{}, fmt.Errorf("new charter: %w: invariant is required", ErrInvalid)
	}
	if err := validateWatch(watch); err != nil {
		return Charter{}, fmt.Errorf("new charter: %w", err)
	}
	action.Template = strings.TrimSpace(action.Template)
	if action.Template == "" {
		return Charter{}, fmt.Errorf("new charter: %w: action template is required", ErrInvalid)
	}
	if err := validateRails(rails); err != nil {
		return Charter{}, fmt.Errorf("new charter: %w", err)
	}
	if !validCharterStatus(status) || status == CharterRetired {
		return Charter{}, fmt.Errorf("new charter: %w: invalid initial status %q", ErrInvalid, status)
	}
	if status != CharterProposed && !validRatification(ratification) {
		return Charter{}, fmt.Errorf("new charter: %w: active standing spend requires ratification", ErrInvalid)
	}
	return Charter{
		ID: id, Invariant: invariant, Watch: watch, SentinelHint: strings.TrimSpace(sentinelHint),
		Action: action, Status: status, Ratification: ratification, guardrails: rails,
	}, nil
}

// WithProposalShape attaches the deterministic recurrence key used to suppress
// a proposal after the user declines it.
func (c Charter) WithProposalShape(shape string) Charter {
	c.ProposalShape = strings.TrimSpace(shape)
	return c
}

func validateRails(rails CharterRails) error {
	if rails.PerFiringBudgetUSD <= 0 || math.IsNaN(rails.PerFiringBudgetUSD) || math.IsInf(rails.PerFiringBudgetUSD, 0) {
		return fmt.Errorf("%w: positive per-firing dollar budget is required", ErrInvalid)
	}
	if rails.MaxFiringsPerDay <= 0 {
		return fmt.Errorf("%w: positive daily firing limit is required", ErrInvalid)
	}
	return nil
}

func validateWatch(watch WatchSpec) error {
	count := 0
	if watch.Cron != nil {
		count++
	}
	if watch.File != nil {
		count++
	}
	if watch.Graph != nil {
		count++
	}
	if watch.Poll != nil {
		count++
	}
	if count != 1 {
		return fmt.Errorf("%w: watch requires exactly one structured specification", ErrInvalid)
	}
	switch watch.Kind {
	case WatchCron:
		if watch.Cron == nil {
			return fmt.Errorf("%w: cron watch requires a schedule", ErrInvalid)
		}
		return validateCron(*watch.Cron)
	case WatchFile:
		if watch.File == nil || strings.TrimSpace(watch.File.Glob) == "" || watch.File.Cadence <= 0 {
			return fmt.Errorf("%w: file watch requires a glob and positive cadence", ErrInvalid)
		}
	case WatchGraph:
		if watch.Graph == nil || watch.Graph.Cadence <= 0 {
			return fmt.Errorf("%w: graph watch requires a predicate and positive cadence", ErrInvalid)
		}
		switch watch.Graph.Predicate {
		case GraphNodeSettled, GraphNodeFailed:
			if strings.TrimSpace(watch.Graph.Title) == "" && strings.TrimSpace(watch.Graph.Scope) == "" {
				return fmt.Errorf("%w: node graph watch requires a title or scope", ErrInvalid)
			}
		case GraphSpendThreshold:
			if watch.Graph.ThresholdUSD <= 0 || math.IsNaN(watch.Graph.ThresholdUSD) || math.IsInf(watch.Graph.ThresholdUSD, 0) {
				return fmt.Errorf("%w: spend graph watch requires a positive threshold", ErrInvalid)
			}
		default:
			return fmt.Errorf("%w: unknown graph predicate %q", ErrInvalid, watch.Graph.Predicate)
		}
	case WatchPoll:
		if watch.Poll == nil || strings.TrimSpace(watch.Poll.Condition) == "" || watch.Poll.Cadence <= 0 {
			return fmt.Errorf("%w: poll watch requires a condition and positive cadence", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown watch kind %q", ErrInvalid, watch.Kind)
	}
	return nil
}

func validateCron(schedule CronSchedule) error {
	switch schedule.Kind {
	case CronEveryMinutes, CronEveryHours:
		if schedule.Interval <= 0 {
			return fmt.Errorf("%w: cron interval must be positive", ErrInvalid)
		}
	case CronDaily, CronWeekdays:
		if schedule.Hour < 0 || schedule.Hour > 23 || schedule.Minute < 0 || schedule.Minute > 59 {
			return fmt.Errorf("%w: cron wall time is invalid", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown cron schedule %q", ErrInvalid, schedule.Kind)
	}
	return nil
}

func validCharterStatus(status CharterStatus) bool {
	return status == CharterProposed || status == CharterActive || status == CharterPaused || status == CharterRetired
}

func validRatification(r Ratification) bool {
	return validOrigin(r.Origin) && strings.TrimSpace(r.Evidence) != ""
}

const charterSchema = `
CREATE TABLE IF NOT EXISTS charters (
    id               TEXT PRIMARY KEY,
    invariant        TEXT NOT NULL,
    watch             JSON NOT NULL CHECK (json_valid(watch)),
    sentinel_hint     TEXT NOT NULL DEFAULT '',
    action            JSON NOT NULL CHECK (json_valid(action)),
    rails             JSON NOT NULL CHECK (json_valid(rails)),
    status            TEXT NOT NULL CHECK (status IN ('proposed', 'active', 'paused', 'retired')),
    ratification      JSON NOT NULL CHECK (json_valid(ratification)),
    proposal_shape    TEXT NOT NULL DEFAULT '',
    last_wake         TEXT,
    next_due          TEXT,
    wake_seq          INTEGER NOT NULL DEFAULT 0,
    wake_pending      INTEGER NOT NULL DEFAULT 0 CHECK (wake_pending IN (0, 1)),
    sentinel_yes      INTEGER NOT NULL DEFAULT 0 CHECK (sentinel_yes IN (0, 1)),
    wake_evidence     TEXT NOT NULL DEFAULT '',
    file_fingerprint  TEXT NOT NULL DEFAULT '',
    graph_cursor      INTEGER NOT NULL DEFAULT 0,
    graph_day         TEXT NOT NULL DEFAULT '',
    graph_triggered   INTEGER NOT NULL DEFAULT 0 CHECK (graph_triggered IN (0, 1)),
    created_seq       INTEGER NOT NULL REFERENCES events(seq),
    updated_seq       INTEGER NOT NULL REFERENCES events(seq)
);
CREATE INDEX IF NOT EXISTS charters_due ON charters (status, next_due, created_seq);
`

type charterRecord struct {
	ID              string        `json:"id"`
	Invariant       string        `json:"invariant"`
	Watch           WatchSpec     `json:"watch"`
	SentinelHint    string        `json:"sentinel_hint,omitempty"`
	Action          CharterAction `json:"action"`
	Rails           CharterRails  `json:"rails"`
	Status          CharterStatus `json:"status"`
	Ratification    Ratification  `json:"ratification"`
	ProposalShape   string        `json:"proposal_shape,omitempty"`
	LastWake        time.Time     `json:"last_wake,omitempty"`
	NextDue         time.Time     `json:"next_due,omitempty"`
	WakeSeq         int64         `json:"wake_seq,omitempty"`
	WakePending     bool          `json:"wake_pending,omitempty"`
	SentinelYes     bool          `json:"sentinel_yes,omitempty"`
	WakeEvidence    string        `json:"wake_evidence,omitempty"`
	FileFingerprint string        `json:"file_fingerprint,omitempty"`
	GraphCursor     int64         `json:"graph_cursor,omitempty"`
	GraphDay        string        `json:"graph_day,omitempty"`
	GraphTriggered  bool          `json:"graph_triggered,omitempty"`
}

type charterStatusPayload struct {
	Status       CharterStatus `json:"status"`
	Ratification Ratification  `json:"ratification"`
}

// CharterWatchState is the restart-safe observation cursor carried by both a
// quiet watch advance and a wake reservation.
type CharterWatchState struct {
	NextDue         time.Time `json:"next_due"`
	FileFingerprint string    `json:"file_fingerprint,omitempty"`
	GraphCursor     int64     `json:"graph_cursor,omitempty"`
	GraphDay        string    `json:"graph_day,omitempty"`
	GraphTriggered  bool      `json:"graph_triggered,omitempty"`
}

type charterWakePayload struct {
	WakeAt   time.Time         `json:"wake_at"`
	Evidence string            `json:"evidence,omitempty"`
	State    CharterWatchState `json:"state"`
}

// SentinelCheck is the journaled outcome of exactly one wake-time judgment.
type SentinelCheck struct {
	WakeSeq int64  `json:"wake_seq"`
	Yes     bool   `json:"yes"`
	Line    string `json:"line,omitempty"`
	Error   string `json:"error,omitempty"`
}

type charterFiringPayload struct {
	WakeSeq int64  `json:"wake_seq"`
	JobID   string `json:"job_id,omitempty"`
	SayOnly bool   `json:"say_only,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type charterDeclinedPayload struct {
	Shape  string `json:"shape"`
	Reason string `json:"reason,omitempty"`
}

// CreateCharter journals and materializes a charter plus its inert spine node.
func (s *Store) CreateCharter(charter Charter) error {
	if err := validateConstructedCharter(charter); err != nil {
		return err
	}
	now := time.Now()
	next, err := initialCharterDue(charter.Watch, now)
	if err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	charter.NextDue = next
	payload := charterToRecord(charter)

	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id = ?`, charter.ID).Scan(&exists); err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	if exists != 0 {
		return fmt.Errorf("create charter: %w: id %q already exists", ErrInvalid, charter.ID)
	}
	seq, at, err := appendEvent(tx, charter.ID, EventCharterCreated, payload)
	if err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	if err := applyCharterCreated(tx, payload, seq, at); err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	return nil
}

func validateConstructedCharter(charter Charter) error {
	constructed, err := NewCharter(charter.ID, charter.Invariant, charter.Watch, charter.SentinelHint,
		charter.Action, charter.guardrails, charter.Status, charter.Ratification)
	if err != nil {
		return fmt.Errorf("create charter: %w", err)
	}
	_ = constructed
	return nil
}

func initialCharterDue(watch WatchSpec, now time.Time) (time.Time, error) {
	if watch.Kind == WatchCron {
		return NextCronDue(*watch.Cron, now)
	}
	return now, nil
}

func charterToRecord(c Charter) charterRecord {
	return charterRecord{
		ID: c.ID, Invariant: c.Invariant, Watch: c.Watch, SentinelHint: c.SentinelHint,
		Action: c.Action, Rails: c.guardrails, Status: c.Status, Ratification: c.Ratification,
		ProposalShape: c.ProposalShape, LastWake: c.LastWake, NextDue: c.NextDue,
		WakeSeq: c.WakeSeq, WakePending: c.WakePending, SentinelYes: c.SentinelYes,
		WakeEvidence:    c.WakeEvidence,
		FileFingerprint: c.FileFingerprint, GraphCursor: c.GraphCursor, GraphDay: c.GraphDay,
		GraphTriggered: c.GraphTriggered,
	}
}

func applyCharterCreated(tx *sql.Tx, payload charterRecord, seq int64, at time.Time) error {
	watch, _ := json.Marshal(payload.Watch)
	action, _ := json.Marshal(payload.Action)
	encodedRails, _ := json.Marshal(payload.Rails)
	ratification, _ := json.Marshal(payload.Ratification)
	graphCursor := payload.GraphCursor
	if payload.Watch.Kind == WatchGraph && graphCursor == 0 {
		graphCursor = seq
	}
	if _, err := tx.Exec(`
		INSERT INTO charters (
		    id, invariant, watch, sentinel_hint, action, rails, status, ratification,
		    proposal_shape, last_wake, next_due, wake_seq, wake_pending, sentinel_yes, wake_evidence,
		    file_fingerprint, graph_cursor, graph_day, graph_triggered, created_seq, updated_seq
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		payload.ID, payload.Invariant, string(watch), payload.SentinelHint, string(action), string(encodedRails),
		payload.Status, string(ratification), payload.ProposalShape, nullTime(payload.LastWake),
		nullTime(payload.NextDue), payload.WakeSeq, payload.WakePending, payload.SentinelYes, payload.WakeEvidence,
		payload.FileFingerprint, graphCursor, payload.GraphDay, payload.GraphTriggered, seq, seq); err != nil {
		return err
	}
	origin := payload.Ratification.Origin
	if !validOrigin(origin) {
		origin = OriginSelf
	}
	brief := bounded("Charter: "+payload.Invariant, MaxDigestBytes)
	title := firstCharterLine(payload.Invariant)
	_, err := tx.Exec(`
		INSERT INTO nodes (
		    id, parent_id, brief, title, grp, stage, status, summary, origin, session_id,
		    intent, created_seq, created_order, updated_seq, finished_at, folded, fold_root, fold_digest
		) VALUES (?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?, 0, ?, ?, 1, 1, ?)`,
		payload.ID, RootID, brief, title, CharterGroup, Done, brief, origin,
		nullIfEmpty(payload.Ratification.SessionID), payload.Invariant, seq, seq, formatTime(at), brief)
	if err != nil {
		return err
	}
	return refreshGraphFTS(tx, payload.ID)
}

func firstCharterLine(value string) string {
	line := strings.TrimSpace(strings.SplitN(value, "\n", 2)[0])
	if len(line) > 64 {
		line = bounded(line, 64)
	}
	return line
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return formatTime(value)
}

// Charter returns one materialized charter.
func (s *Store) Charter(id string) (Charter, bool, error) {
	row := s.db.QueryRow(`SELECT `+charterColumns+` FROM charters WHERE id = ?`, id)
	charter, err := scanCharter(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Charter{}, false, nil
	}
	if err != nil {
		return Charter{}, false, fmt.Errorf("read charter %q: %w", id, err)
	}
	return charter, true, nil
}

const charterColumns = `
    id, invariant, watch, sentinel_hint, action, rails, status, ratification,
    proposal_shape, last_wake, next_due, wake_seq, wake_pending, sentinel_yes, wake_evidence,
    file_fingerprint, graph_cursor, graph_day, graph_triggered, created_seq, updated_seq`

func scanCharter(scanner rowScanner) (Charter, error) {
	var c Charter
	var watch, action, rails, ratification string
	var lastWake, nextDue sql.NullString
	if err := scanner.Scan(&c.ID, &c.Invariant, &watch, &c.SentinelHint, &action, &rails,
		&c.Status, &ratification, &c.ProposalShape, &lastWake, &nextDue, &c.WakeSeq,
		&c.WakePending, &c.SentinelYes, &c.WakeEvidence, &c.FileFingerprint, &c.GraphCursor, &c.GraphDay,
		&c.GraphTriggered, &c.CreatedSeq, &c.UpdatedSeq); err != nil {
		return Charter{}, err
	}
	if err := json.Unmarshal([]byte(watch), &c.Watch); err != nil {
		return Charter{}, err
	}
	if err := json.Unmarshal([]byte(action), &c.Action); err != nil {
		return Charter{}, err
	}
	if err := json.Unmarshal([]byte(rails), &c.guardrails); err != nil {
		return Charter{}, err
	}
	if err := json.Unmarshal([]byte(ratification), &c.Ratification); err != nil {
		return Charter{}, err
	}
	var err error
	if lastWake.Valid {
		c.LastWake, err = parseTime(lastWake.String)
		if err != nil {
			return Charter{}, err
		}
	}
	if nextDue.Valid {
		c.NextDue, err = parseTime(nextDue.String)
		if err != nil {
			return Charter{}, err
		}
	}
	return c, nil
}

// Charters lists first-class standing objects in creation order.
func (s *Store) Charters() ([]Charter, error) {
	rows, err := s.db.Query(`SELECT ` + charterColumns + ` FROM charters ORDER BY created_seq, id`)
	if err != nil {
		return nil, fmt.Errorf("list charters: %w", err)
	}
	defer rows.Close()
	var result []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
		if err != nil {
			return nil, fmt.Errorf("list charters: %w", err)
		}
		result = append(result, charter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list charters: %w", err)
	}
	return result, nil
}

// ReviseCharter replaces the editable definition while preserving identity,
// status, and ratification history. A changed watch starts from a fresh due
// calculation so old cadence state cannot leak into the revision.
func (s *Store) ReviseCharter(id, invariant string, watch WatchSpec, sentinelHint string,
	action CharterAction, rails CharterRails) error {
	current, found, err := s.Charter(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("revise charter: %w: %q", ErrNotFound, id)
	}
	if current.Status == CharterRetired {
		return fmt.Errorf("revise charter: %w: retired charter", ErrInvalid)
	}
	validated, err := NewCharter(id, invariant, watch, sentinelHint, action, rails, current.Status, current.Ratification)
	if err != nil {
		return fmt.Errorf("revise charter: %w", err)
	}
	validated.ProposalShape = current.ProposalShape
	validated.LastWake = current.LastWake
	validated.NextDue, err = initialCharterDue(watch, time.Now())
	if err != nil {
		return fmt.Errorf("revise charter: %w", err)
	}
	payload := charterToRecord(validated)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("revise charter: %w", err)
	}
	defer tx.Rollback()
	seq, _, err := appendEvent(tx, id, EventCharterRevised, payload)
	if err != nil {
		return fmt.Errorf("revise charter: %w", err)
	}
	if err := applyCharterRevision(tx, payload, seq); err != nil {
		return fmt.Errorf("revise charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("revise charter: %w", err)
	}
	return nil
}

func applyCharterRevision(tx *sql.Tx, payload charterRecord, seq int64) error {
	watch, _ := json.Marshal(payload.Watch)
	action, _ := json.Marshal(payload.Action)
	rails, _ := json.Marshal(payload.Rails)
	graphCursor := payload.GraphCursor
	if payload.Watch.Kind == WatchGraph && graphCursor == 0 {
		graphCursor = seq
	}
	result, err := tx.Exec(`UPDATE charters SET invariant=?, watch=?, sentinel_hint=?, action=?, rails=?,
		next_due=?, wake_seq=?, wake_pending=?, sentinel_yes=?, wake_evidence=?, file_fingerprint=?, graph_cursor=?,
		graph_day=?, graph_triggered=?, updated_seq=? WHERE id=?`,
		payload.Invariant, string(watch), payload.SentinelHint, string(action), string(rails),
		nullTime(payload.NextDue), payload.WakeSeq, payload.WakePending, payload.SentinelYes,
		payload.WakeEvidence, payload.FileFingerprint, graphCursor, payload.GraphDay, payload.GraphTriggered, seq, payload.ID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("charter %q is missing", payload.ID)
	}
	brief := bounded("Charter: "+payload.Invariant, MaxDigestBytes)
	if _, err := tx.Exec(`UPDATE nodes SET brief=?, title=?, summary=?, fold_digest=?, updated_seq=? WHERE id=?`,
		brief, firstCharterLine(payload.Invariant), brief, brief, seq, payload.ID); err != nil {
		return err
	}
	return refreshGraphFTS(tx, payload.ID)
}

// SetCharterStatus journals pause, activation, and retirement. Activation is
// the one transition that requires fresh explicit ratification provenance.
func (s *Store) SetCharterStatus(id string, status CharterStatus, ratification Ratification) error {
	current, found, err := s.Charter(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("set charter status: %w: %q", ErrNotFound, id)
	}
	if !validCharterTransition(current.Status, status) {
		return fmt.Errorf("set charter status: %w: %s to %s", ErrInvalid, current.Status, status)
	}
	if status == CharterActive {
		if !validRatification(ratification) {
			return fmt.Errorf("set charter status: %w: activation requires ratification", ErrInvalid)
		}
	} else {
		ratification = current.Ratification
	}
	payload := charterStatusPayload{Status: status, Ratification: ratification}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("set charter status: %w", err)
	}
	defer tx.Rollback()
	seq, _, err := appendEvent(tx, id, EventCharterStatusChanged, payload)
	if err != nil {
		return fmt.Errorf("set charter status: %w", err)
	}
	if err := applyCharterStatus(tx, id, payload, seq); err != nil {
		return fmt.Errorf("set charter status: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("set charter status: %w", err)
	}
	return nil
}

func validCharterTransition(from, to CharterStatus) bool {
	if from == to || from == CharterRetired {
		return false
	}
	switch from {
	case CharterProposed:
		return to == CharterActive || to == CharterRetired
	case CharterActive:
		return to == CharterPaused || to == CharterRetired
	case CharterPaused:
		return to == CharterActive || to == CharterRetired
	default:
		return false
	}
}

func applyCharterStatus(tx *sql.Tx, id string, payload charterStatusPayload, seq int64) error {
	ratification, _ := json.Marshal(payload.Ratification)
	clearWake := payload.Status == CharterRetired
	result, err := tx.Exec(`UPDATE charters SET status=?, ratification=?,
		wake_pending=CASE WHEN ? THEN 0 ELSE wake_pending END,
		sentinel_yes=CASE WHEN ? THEN 0 ELSE sentinel_yes END, updated_seq=? WHERE id=?`,
		payload.Status, string(ratification), clearWake, clearWake, seq, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("charter %q is missing", id)
	}
	_, err = tx.Exec(`UPDATE nodes SET updated_seq=? WHERE id=?`, seq, id)
	return err
}

// DeclineCharterProposal retires the proposal and writes a searchable notebook
// fact. The decline event itself is the durable no-reproposal key.
func (s *Store) DeclineCharterProposal(id, reason string) error {
	charter, found, err := s.Charter(id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("decline charter: %w: %q", ErrNotFound, id)
	}
	if charter.Status != CharterProposed {
		return fmt.Errorf("decline charter: %w: not proposed", ErrInvalid)
	}
	payload := charterDeclinedPayload{Shape: charter.ProposalShape, Reason: strings.TrimSpace(reason)}
	body := "Do not propose standing charter shape " + charter.ProposalShape
	if payload.Reason != "" {
		body += ": " + payload.Reason
	}
	body = bounded(body, MaxFactBytes)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("decline charter: %w", err)
	}
	defer tx.Rollback()
	seq, _, err := appendEvent(tx, id, EventCharterProposalDeclined, payload)
	if err != nil {
		return fmt.Errorf("decline charter: %w", err)
	}
	status := charterStatusPayload{Status: CharterRetired, Ratification: charter.Ratification}
	if err := applyCharterStatus(tx, id, status, seq); err != nil {
		return fmt.Errorf("decline charter: %w", err)
	}
	fact := factPayload{NodeID: id, Scope: "user", Kind: FactPreference, Body: body, Status: FactActive}
	factSeq, at, err := appendEvent(tx, id, EventFactLearned, fact)
	if err != nil {
		return fmt.Errorf("decline charter: %w", err)
	}
	if err := applyFactView(tx, fact, factSeq, at); err != nil {
		return fmt.Errorf("decline charter: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("decline charter: %w", err)
	}
	return nil
}

// CharterProposalDeclined reports whether a recurrence shape has already been
// explicitly refused.
func (s *Store) CharterProposalDeclined(shape string) (bool, error) {
	shape = strings.TrimSpace(shape)
	var found bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM events WHERE kind=? AND json_extract(payload, '$.shape')=?)`,
		EventCharterProposalDeclined, shape).Scan(&found)
	if err != nil {
		return false, fmt.Errorf("find declined charter proposal: %w", err)
	}
	return found, nil
}

// DueCharters returns active work that is due or has an interrupted wake to
// resume. Expiry is handled by the engine before any sentinel call.
func (s *Store) DueCharters(now time.Time, limit int) ([]Charter, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT `+charterColumns+` FROM charters
		WHERE status=? AND (wake_pending=1 OR next_due IS NULL OR next_due<=?)
		ORDER BY CASE WHEN wake_pending=1 THEN 0 ELSE 1 END, next_due, created_seq LIMIT ?`,
		CharterActive, formatTime(now), limit)
	if err != nil {
		return nil, fmt.Errorf("due charters: %w", err)
	}
	defer rows.Close()
	var result []Charter
	for rows.Next() {
		c, err := scanCharter(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// AdvanceCharterWatch journals a due observation that did not warrant a
// sentinel call: an initial file baseline or a graph scan with no match.
func (s *Store) AdvanceCharterWatch(id string, state CharterWatchState) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	seq, _, err := appendEvent(tx, id, EventCharterWatchAdvanced, state)
	if err != nil {
		return err
	}
	if err := applyCharterWatch(tx, id, state, seq); err != nil {
		return err
	}
	return tx.Commit()
}

func applyCharterWatch(tx *sql.Tx, id string, payload CharterWatchState, seq int64) error {
	result, err := tx.Exec(`UPDATE charters SET next_due=?, file_fingerprint=?, graph_cursor=?, graph_day=?, graph_triggered=?, updated_seq=?
		WHERE id=? AND status=? AND wake_pending=0`, nullTime(payload.NextDue), payload.FileFingerprint,
		payload.GraphCursor, payload.GraphDay, payload.GraphTriggered, seq, id, CharterActive)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("charter %q watch state changed concurrently", id)
	}
	return nil
}

// BeginCharterWake durably reserves one due occurrence. A pending reservation
// is returned unchanged after restart instead of creating another wake.
func (s *Store) BeginCharterWake(id string, at time.Time, evidence string, state CharterWatchState) (int64, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var pending bool
	var wakeSeq int64
	if err := tx.QueryRow(`SELECT wake_pending, wake_seq FROM charters WHERE id=? AND status=?`, id, CharterActive).Scan(&pending, &wakeSeq); err != nil {
		return 0, err
	}
	if pending {
		return wakeSeq, nil
	}
	payload := charterWakePayload{WakeAt: at, Evidence: bounded(evidence, MaxDigestBytes), State: state}
	seq, _, err := appendEvent(tx, id, EventCharterWoken, payload)
	if err != nil {
		return 0, err
	}
	if err := applyCharterWake(tx, id, payload, seq); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return seq, nil
}

func applyCharterWake(tx *sql.Tx, id string, payload charterWakePayload, seq int64) error {
	result, err := tx.Exec(`UPDATE charters SET last_wake=?, next_due=?, wake_seq=?, wake_pending=1,
		sentinel_yes=0, wake_evidence=?, updated_seq=? WHERE id=?`,
		formatTime(payload.WakeAt), nullTime(payload.State.NextDue), seq, payload.Evidence, seq, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("charter %q is missing", id)
	}
	_, err = tx.Exec(`UPDATE charters SET file_fingerprint=?, graph_cursor=?, graph_day=?, graph_triggered=? WHERE id=?`,
		payload.State.FileFingerprint, payload.State.GraphCursor, payload.State.GraphDay,
		payload.State.GraphTriggered, id)
	return err
}

// RecordSentinelCheck records yes, no, and provider error outcomes. A no or
// error closes the wake; yes leaves a durable firing pending.
func (s *Store) RecordSentinelCheck(id string, check SentinelCheck) error {
	check.Line = bounded(strings.TrimSpace(check.Line), MaxDigestBytes)
	check.Error = bounded(strings.TrimSpace(check.Error), MaxDigestBytes)
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var pending bool
	var wakeSeq int64
	if err := tx.QueryRow(`SELECT wake_pending, wake_seq FROM charters WHERE id=?`, id).Scan(&pending, &wakeSeq); err != nil {
		return err
	}
	if !pending || wakeSeq != check.WakeSeq {
		return fmt.Errorf("sentinel check: %w: stale wake", ErrInvalid)
	}
	seq, _, err := appendEvent(tx, id, EventSentinelChecked, check)
	if err != nil {
		return err
	}
	if err := applySentinelCheck(tx, id, check, seq); err != nil {
		return err
	}
	return tx.Commit()
}

func applySentinelCheck(tx *sql.Tx, id string, check SentinelCheck, seq int64) error {
	pending, yes := false, false
	if check.Yes && check.Error == "" {
		pending, yes = true, true
	}
	result, err := tx.Exec(`UPDATE charters SET wake_pending=?, sentinel_yes=?, updated_seq=? WHERE id=? AND wake_seq=?`,
		pending, yes, seq, id, check.WakeSeq)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("sentinel wake %d is missing", check.WakeSeq)
	}
	return nil
}

// RetireExpiredCharter journals expiry before any wake-time model call.
func (s *Store) RetireExpiredCharter(id string, now time.Time) (bool, error) {
	charter, found, err := s.Charter(id)
	if err != nil || !found {
		return false, err
	}
	expires := charter.guardrails.ExpiresAt
	if charter.Status == CharterRetired || expires == nil || now.Before(*expires) {
		return false, nil
	}
	return true, s.SetCharterStatus(id, CharterRetired, Ratification{})
}

// RetireExpiredCharters applies expiry even while a charter is paused or still
// proposed; expiry is a standing-spend boundary, not a scheduling state.
func (s *Store) RetireExpiredCharters(now time.Time) (int, error) {
	charters, err := s.Charters()
	if err != nil {
		return 0, err
	}
	retired := 0
	for _, charter := range charters {
		changed, err := s.RetireExpiredCharter(charter.ID, now)
		if err != nil {
			return retired, err
		}
		if changed {
			retired++
		}
	}
	return retired, nil
}

// FiringsToday counts admitted actions, not sentinel checks or blocked wakes.
func (s *Store) FiringsToday(id string, now time.Time) (int, error) {
	start, end := localDayBounds(now)
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND ts>=? AND ts<?`,
		id, EventCharterFired, formatTime(start), formatTime(end)).Scan(&count)
	return count, err
}

// FireDisposition is the deterministic result of trying to admit a checked
// yes wake under all three charter rails.
type FireDisposition string

const (
	FireAdmitted FireDisposition = "admitted"
	FireQuota    FireDisposition = "quota"
	FireExpired  FireDisposition = "expired"
	FireRailWait FireDisposition = "daily_rail_wait"
)

// FireCharter atomically admits either an ordinary trigger job or one
// attention message. Daily-rail waits leave sentinel_yes pending for retry.
func (s *Store) FireCharter(id string, wakeSeq int64, subtree Subtree, provenance Provenance,
	dailyBudgetUSD float64, now time.Time) (FireDisposition, error) {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	charter, err := charterInTx(tx, id)
	if err != nil {
		return "", err
	}
	if !charter.WakePending || !charter.SentinelYes || charter.WakeSeq != wakeSeq {
		return "", fmt.Errorf("fire charter: %w: no checked yes wake", ErrInvalid)
	}
	if expires := charter.guardrails.ExpiresAt; expires != nil && !now.Before(*expires) {
		payload := charterStatusPayload{Status: CharterRetired, Ratification: charter.Ratification}
		seq, _, err := appendEvent(tx, id, EventCharterStatusChanged, payload)
		if err != nil {
			return "", err
		}
		if err := applyCharterStatus(tx, id, payload, seq); err != nil {
			return "", err
		}
		if err := clearCharterWake(tx, id, seq); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return FireExpired, nil
	}
	start, end := localDayBounds(now)
	var firings int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND ts>=? AND ts<?`,
		id, EventCharterFired, formatTime(start), formatTime(end)).Scan(&firings); err != nil {
		return "", err
	}
	if firings >= charter.guardrails.MaxFiringsPerDay {
		payload := charterFiringPayload{WakeSeq: wakeSeq, Reason: "max_firings_per_day"}
		seq, _, err := appendEvent(tx, id, EventCharterFiringBlocked, payload)
		if err != nil {
			return "", err
		}
		if err := clearCharterWake(tx, id, seq); err != nil {
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return FireQuota, nil
	}
	rail, err := dailyRailAt(tx, dailyBudgetUSD, now)
	if err != nil {
		return "", err
	}
	projectedCrossing := !rail.Unlimited && rail.Spend+charter.guardrails.PerFiringBudgetUSD > rail.Ceiling
	if projectedCrossing {
		rail.Reached = true
		posted, err := pauseDailyRailTx(tx, rail, charter.Ratification.SessionID, now)
		if err != nil {
			return "", err
		}
		if posted {
			if _, _, err := appendEvent(tx, id, EventCharterFiringDeferred,
				charterFiringPayload{WakeSeq: wakeSeq, Reason: "daily_dollar_rail"}); err != nil {
				return "", err
			}
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
		return FireRailWait, nil
	}

	payload := charterFiringPayload{WakeSeq: wakeSeq, SayOnly: charter.Action.SayOnly}
	if !charter.Action.SayOnly {
		normalized, err := normalizeSubtree(RootID, subtree, provenance)
		if err != nil {
			return "", err
		}
		payload.JobID = normalized.Root
		for _, node := range normalized.Nodes {
			var exists int
			if err := tx.QueryRow(`SELECT COUNT(*) FROM nodes WHERE id=?`, node.ID).Scan(&exists); err != nil {
				return "", err
			}
			if exists != 0 {
				return "", fmt.Errorf("fire charter: %w: node %q exists", ErrInvalid, node.ID)
			}
		}
		fireSeq, _, err := appendEvent(tx, id, EventCharterFired, payload)
		if err != nil {
			return "", err
		}
		if err := applyCharterFired(tx, id, payload, fireSeq); err != nil {
			return "", err
		}
		spliceSeq, _, err := appendEvent(tx, normalized.Root, EventSubtreeSpliced, normalized)
		if err != nil {
			return "", err
		}
		if err := applySpliceView(tx, normalized, spliceSeq); err != nil {
			return "", err
		}
	} else {
		fireSeq, _, err := appendEvent(tx, id, EventCharterFired, payload)
		if err != nil {
			return "", err
		}
		if err := applyCharterFired(tx, id, payload, fireSeq); err != nil {
			return "", err
		}
		message := messagePayload{SessionID: charter.Ratification.SessionID, Role: RoleAgent,
			Body: charter.Action.Template, NodeID: id}
		seq, at, err := appendEvent(tx, id, EventMessagePosted, message)
		if err != nil {
			return "", err
		}
		if err := applyMessageView(tx, message, seq, at); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return FireAdmitted, nil
}

func charterInTx(tx *sql.Tx, id string) (Charter, error) {
	return scanCharter(tx.QueryRow(`SELECT `+charterColumns+` FROM charters WHERE id=?`, id))
}

func clearCharterWake(tx *sql.Tx, id string, seq int64) error {
	_, err := tx.Exec(`UPDATE charters SET wake_pending=0, sentinel_yes=0, updated_seq=? WHERE id=?`, seq, id)
	return err
}

func applyCharterFired(tx *sql.Tx, id string, payload charterFiringPayload, seq int64) error {
	return clearCharterWake(tx, id, seq)
}

func replayCharterEvent(tx *sql.Tx, event Event) error {
	switch event.Kind {
	case EventCharterCreated:
		var payload charterRecord
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterCreated(tx, payload, event.Seq, event.Time)
	case EventCharterRevised:
		var payload charterRecord
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterRevision(tx, payload, event.Seq)
	case EventCharterStatusChanged:
		var payload charterStatusPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterStatus(tx, event.NodeID, payload, event.Seq)
	case EventCharterWatchAdvanced:
		var payload CharterWatchState
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterWatch(tx, event.NodeID, payload, event.Seq)
	case EventCharterWoken:
		var payload charterWakePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterWake(tx, event.NodeID, payload, event.Seq)
	case EventSentinelChecked:
		var payload SentinelCheck
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applySentinelCheck(tx, event.NodeID, payload, event.Seq)
	case EventCharterFired:
		var payload charterFiringPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return applyCharterFired(tx, event.NodeID, payload, event.Seq)
	case EventCharterFiringBlocked:
		var payload charterFiringPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		return clearCharterWake(tx, event.NodeID, event.Seq)
	case EventCharterFiringDeferred:
		var payload charterFiringPayload
		return json.Unmarshal(event.Payload, &payload)
	case EventCharterProposalDeclined:
		var payload charterDeclinedPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			return err
		}
		charter, err := charterInTx(tx, event.NodeID)
		if err != nil {
			return err
		}
		return applyCharterStatus(tx, event.NodeID,
			charterStatusPayload{Status: CharterRetired, Ratification: charter.Ratification}, event.Seq)
	default:
		return nil
	}
}

// NextCronDue returns the first occurrence strictly after the supplied instant.
// Daily schedules search real instants in the local day, so nonexistent spring
// times move to the first valid minute and repeated fall times fire once.
func NextCronDue(schedule CronSchedule, after time.Time) (time.Time, error) {
	if err := validateCron(schedule); err != nil {
		return time.Time{}, err
	}
	location := after.Location()
	switch schedule.Kind {
	case CronEveryMinutes:
		return after.Add(time.Duration(schedule.Interval) * time.Minute), nil
	case CronEveryHours:
		return after.Add(time.Duration(schedule.Interval) * time.Hour), nil
	case CronDaily, CronWeekdays:
		local := after.In(location)
		for offset := 0; offset <= 8; offset++ {
			day := local.AddDate(0, 0, offset)
			if schedule.Kind == CronWeekdays && (day.Weekday() == time.Saturday || day.Weekday() == time.Sunday) {
				continue
			}
			candidate := wallClockOccurrence(day, schedule.Hour, schedule.Minute, location)
			if candidate.After(after) {
				return candidate, nil
			}
		}
	}
	return time.Time{}, fmt.Errorf("next cron due: %w: no occurrence", ErrInvalid)
}

func wallClockOccurrence(day time.Time, hour, minute int, location *time.Location) time.Time {
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 1)
	var fallback time.Time
	for instant := start; instant.Before(end); instant = instant.Add(time.Minute) {
		local := instant.In(location)
		if local.Year() != day.Year() || local.Month() != day.Month() || local.Day() != day.Day() {
			continue
		}
		if local.Hour() == hour && local.Minute() == minute {
			return instant
		}
		if fallback.IsZero() && (local.Hour() > hour || local.Hour() == hour && local.Minute() > minute) {
			fallback = instant
		}
	}
	if !fallback.IsZero() {
		return fallback
	}
	return end
}

// NextWatchDue advances one completed wake from its scheduled occurrence.
func NextWatchDue(watch WatchSpec, due time.Time) (time.Time, error) {
	switch watch.Kind {
	case WatchCron:
		// SQLite timestamps intentionally reload in UTC. Cron is local policy,
		// so restore the process location before doing wall-clock math.
		return NextCronDue(*watch.Cron, due.In(time.Local))
	case WatchFile:
		return due.Add(watch.File.Cadence), nil
	case WatchGraph:
		return due.Add(watch.Graph.Cadence), nil
	case WatchPoll:
		return due.Add(watch.Poll.Cadence), nil
	default:
		return time.Time{}, fmt.Errorf("next watch due: %w", ErrInvalid)
	}
}
