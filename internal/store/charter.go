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
	"unicode"
)

// CharterStatus is the ratification lifecycle of standing intent.
type CharterStatus string

const (
	// CharterDraft is the older compiler-facing inert standing draft. It shares
	// the public lifecycle type with the M3 engine while remaining in its own
	// materialized view.
	CharterDraft    CharterStatus = "draft"
	CharterProposed CharterStatus = "proposed"
	CharterActive   CharterStatus = "active"
	CharterPaused   CharterStatus = "paused"
	CharterRetired  CharterStatus = "retired"
)

// CharterAutonomy is the earned right to turn a checked wake into work
// without asking again. It is deliberately independent of CharterStatus:
// status says whether a charter is armed; autonomy says how it may fire.
type CharterAutonomy string

const (
	CharterProbation CharterAutonomy = "probation"
	CharterTenured   CharterAutonomy = "tenured"
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
//
// Every wall-clock kind here is read in the PROCESS'S LOCAL ZONE — the zone the
// machine running aforge is set to. "Sunday at 9" means nine in the morning
// where the user is sitting, and a schedule that survives a restart is
// recomputed in that same zone (NextWatchDue restores time.Local before doing
// wall-clock math, because SQLite hands timestamps back in UTC).
type CronKind string

const (
	CronEveryMinutes CronKind = "every_minutes"
	CronEveryHours   CronKind = "every_hours"
	CronDaily        CronKind = "daily"
	CronWeekdays     CronKind = "weekdays"
	// CronWeekly is one named weekday at one wall time — "every Sunday",
	// "Tuesdays at 8pm". It exists because it is the commonest standing rule a
	// person states and the only one the engine could not hold: before it,
	// "every Sunday" degraded to an interval measured from the ratification
	// instant, which drifts off the named day immediately.
	CronWeekly CronKind = "weekly"
	// CronAt is a single wall-clock instant: the reminder schedule. After it
	// fires once, its expiry rail retires the charter.
	CronAt CronKind = "at"
)

// CronSchedule is a local-time schedule with no raw-cron escape hatch.
type CronSchedule struct {
	Kind     CronKind `json:"kind"`
	Interval int      `json:"interval,omitempty"`
	Hour     int      `json:"hour,omitempty"`
	Minute   int      `json:"minute,omitempty"`
	// Weekday is read only by CronWeekly. Sunday is the zero value, so a
	// weekly schedule on a Sunday round-trips through JSON without the field.
	Weekday time.Weekday `json:"weekday,omitempty"`
	At      time.Time    `json:"at,omitempty"`
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

// WatchSpec is exactly one of cron, file, graph, or poll. Cadence preserves
// the user's own cadence words for surfaces; the typed fields are what the
// engine executes.
type WatchSpec struct {
	Kind    WatchKind     `json:"kind"`
	Cadence string        `json:"cadence,omitempty"`
	Cron    *CronSchedule `json:"cron,omitempty"`
	File    *FileWatch    `json:"file,omitempty"`
	Graph   *GraphWatch   `json:"graph,omitempty"`
	Poll    *PollWatch    `json:"poll,omitempty"`

	// CadenceGuessed marks a rhythm nobody said. Cadence used to hold the
	// user's words OR a default backfilled when nothing was recognised, and the
	// two were indistinguishable downstream — so a card told a user her weekly
	// reminder fired "about every 2 minutes" in the same flat voice it would
	// have used for words she actually said. A guess is a question, and this
	// flag is what lets the ratification card ask it.
	CadenceGuessed bool `json:"cadence_guessed,omitempty"`
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
		case CronWeekly:
			return fmt.Sprintf("cron:weekly %s %02d:%02d",
				watch.Cron.Weekday, watch.Cron.Hour, watch.Cron.Minute)
		case CronAt:
			return "cron:at " + watch.Cron.At.Local().Format("2006-01-02 15:04")
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

// Spoken renders the same schedule the way a person says it: "Sundays at 9am",
// "every day at 8pm", "about every 2 minutes". It is the only spelling any
// surface a user reads should use — String is the log's spelling and carries a
// watch-family prefix and a colon, which is machinery wearing a schedule's
// clothes ("fires: about every 2 minutes (cron:every 2 minutes)" was a real
// card). Times are in the process's local zone, the same zone the schedule runs
// in.
func (watch WatchSpec) Spoken() string {
	switch watch.Kind {
	case WatchCron:
		if watch.Cron == nil {
			break
		}
		schedule := *watch.Cron
		switch schedule.Kind {
		case CronEveryMinutes:
			return "about " + everyPhrase(schedule.Interval, "minute")
		case CronEveryHours:
			if schedule.Interval > 0 && schedule.Interval%24 == 0 {
				return everyPhrase(schedule.Interval/24, "day")
			}
			return everyPhrase(schedule.Interval, "hour")
		case CronDaily:
			return "every day at " + spokenClock(schedule.Hour, schedule.Minute)
		case CronWeekdays:
			return "weekdays at " + spokenClock(schedule.Hour, schedule.Minute)
		case CronWeekly:
			return schedule.Weekday.String() + "s at " + spokenClock(schedule.Hour, schedule.Minute)
		case CronAt:
			at := schedule.At.Local()
			return at.Format("Monday 2 January") + " at " + spokenClock(at.Hour(), at.Minute())
		}
	case WatchFile:
		if watch.File != nil {
			return "whenever " + watch.File.Glob + " changes"
		}
	case WatchGraph:
		if watch.Graph != nil && watch.Graph.Predicate == GraphSpendThreshold {
			return fmt.Sprintf("when spending passes $%.2f", watch.Graph.ThresholdUSD)
		}
		return "when the work changes"
	case WatchPoll:
		if watch.Poll != nil && watch.Poll.Cadence > 0 {
			return "about " + spokenInterval(watch.Poll.Cadence)
		}
	}
	if cadence := strings.TrimSpace(watch.Cadence); cadence != "" {
		return cadence
	}
	return "on its own schedule"
}

func everyPhrase(count int, unit string) string {
	if count <= 1 {
		return "every " + unit
	}
	return fmt.Sprintf("every %d %ss", count, unit)
}

// spokenInterval says a duration the way a person says one. It rounds to the
// unit the number lives in rather than printing 2h0m0s at a user.
func spokenInterval(d time.Duration) string {
	switch {
	case d >= 7*24*time.Hour && d%(7*24*time.Hour) == 0:
		return everyPhrase(int(d/(7*24*time.Hour)), "week")
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		return everyPhrase(int(d/(24*time.Hour)), "day")
	case d >= time.Hour && d%time.Hour == 0:
		return everyPhrase(int(d/time.Hour), "hour")
	case d >= time.Minute:
		return everyPhrase(int(d/time.Minute), "minute")
	default:
		return "every " + d.String()
	}
}

// spokenClock is a 24-hour wall time in the spelling people use out loud.
func spokenClock(hour, minute int) string {
	suffix := "am"
	display := hour
	switch {
	case hour == 0:
		display = 12
	case hour == 12:
		suffix = "pm"
	case hour > 12:
		display, suffix = hour-12, "pm"
	}
	if minute == 0 {
		return fmt.Sprintf("%d%s", display, suffix)
	}
	return fmt.Sprintf("%d:%02d%s", display, minute, suffix)
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

	// Compiler-facing standing drafts predate the M3 execution rails. These
	// fields preserve that public contract; NewCharter uses the fields above.
	EstimatedCostUSD       float64 `json:"estimated_cost_usd,omitempty"`
	MaxPerDay              int     `json:"max_per_day,omitempty"`
	MaxPerDayJustification string  `json:"max_per_day_justification,omitempty"`
	Expiry                 string  `json:"expiry,omitempty"`
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
	Autonomy      CharterAutonomy
	GreenFirings  int
	Demotions     int
	Ratification  Ratification
	ProposalShape string

	// Spec and the adjacent fields are the head's lossless ratification card.
	// The structured engine fields above remain the executable source of truth.
	SessionID        string
	Spec             CharterSpec
	SourceCommandSeq int64
	CreatedAt        time.Time

	LastWake time.Time
	// LastChecked and LastCheckLine are the quiet half of a standing watch: the
	// moment a sentinel last looked, and the sentence it wrote when it did.
	// Both were journaled and neither was ever materialized or read, so a watch
	// that had checked faithfully for thirty mornings and correctly found
	// nothing rendered byte-identically to a watch that had never run once —
	// "last fired never · 0 today". Diligence and death are not the same state
	// and the user must be able to tell them apart without being told anything
	// on the days there is nothing to tell.
	LastChecked     time.Time
	LastCheckLine   string
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
	rails = normalizeCharterRails(rails)
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
		Action: action, Status: status, Autonomy: CharterProbation,
		Ratification: ratification, SessionID: ratification.SessionID, guardrails: rails,
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

func normalizeCharterRails(rails CharterRails) CharterRails {
	if rails.PerFiringBudgetUSD <= 0 {
		rails.PerFiringBudgetUSD = rails.EstimatedCostUSD
	}
	if rails.EstimatedCostUSD <= 0 {
		rails.EstimatedCostUSD = rails.PerFiringBudgetUSD
	}
	if rails.MaxFiringsPerDay <= 0 {
		rails.MaxFiringsPerDay = rails.MaxPerDay
	}
	if rails.MaxPerDay <= 0 {
		rails.MaxPerDay = rails.MaxFiringsPerDay
	}
	if rails.Expiry == "" {
		if rails.ExpiresAt == nil {
			rails.Expiry = "never"
		} else {
			rails.Expiry = rails.ExpiresAt.Format(time.RFC3339)
		}
	}
	return rails
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
	case CronWeekly:
		if schedule.Hour < 0 || schedule.Hour > 23 || schedule.Minute < 0 || schedule.Minute > 59 {
			return fmt.Errorf("%w: cron wall time is invalid", ErrInvalid)
		}
		if schedule.Weekday < time.Sunday || schedule.Weekday > time.Saturday {
			return fmt.Errorf("%w: cron weekday is invalid", ErrInvalid)
		}
	case CronAt:
		if schedule.At.IsZero() {
			return fmt.Errorf("%w: cron at-schedule requires an instant", ErrInvalid)
		}
	default:
		return fmt.Errorf("%w: unknown cron schedule %q", ErrInvalid, schedule.Kind)
	}
	return nil
}

func validCharterStatus(status CharterStatus) bool {
	return status == CharterProposed || status == CharterActive || status == CharterPaused || status == CharterRetired
}

func validCharterAutonomy(autonomy CharterAutonomy) bool {
	return autonomy == CharterProbation || autonomy == CharterTenured
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
	autonomy          TEXT NOT NULL DEFAULT 'probation' CHECK (autonomy IN ('probation', 'tenured')),
	green_firings     INTEGER NOT NULL DEFAULT 0 CHECK (green_firings >= 0),
	demotions         INTEGER NOT NULL DEFAULT 0 CHECK (demotions >= 0),
    ratification      JSON NOT NULL CHECK (json_valid(ratification)),
	session_id        TEXT NOT NULL DEFAULT '',
	spec              JSON NOT NULL DEFAULT '{}' CHECK (json_valid(spec)),
	source_command_seq INTEGER NOT NULL DEFAULT 0,
    proposal_shape    TEXT NOT NULL DEFAULT '',
    last_wake         TEXT,
    next_due          TEXT,
    wake_seq          INTEGER NOT NULL DEFAULT 0,
    wake_pending      INTEGER NOT NULL DEFAULT 0 CHECK (wake_pending IN (0, 1)),
    sentinel_yes      INTEGER NOT NULL DEFAULT 0 CHECK (sentinel_yes IN (0, 1)),
    wake_evidence     TEXT NOT NULL DEFAULT '',
    last_checked      TEXT,
    last_check_line   TEXT NOT NULL DEFAULT '',
    file_fingerprint  TEXT NOT NULL DEFAULT '',
    graph_cursor      INTEGER NOT NULL DEFAULT 0,
    graph_day         TEXT NOT NULL DEFAULT '',
    graph_triggered   INTEGER NOT NULL DEFAULT 0 CHECK (graph_triggered IN (0, 1)),
    created_seq       INTEGER NOT NULL REFERENCES events(seq),
    updated_seq       INTEGER NOT NULL REFERENCES events(seq),
	created_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS charters_due ON charters (status, next_due, created_seq);
CREATE VIRTUAL TABLE IF NOT EXISTS charters_fts USING fts5(
    charter_id UNINDEXED, invariant, tokenize='porter unicode61'
);
`

type charterRecord struct {
	ID               string          `json:"id"`
	Invariant        string          `json:"invariant"`
	Watch            WatchSpec       `json:"watch"`
	SentinelHint     string          `json:"sentinel_hint,omitempty"`
	Action           CharterAction   `json:"action"`
	Rails            CharterRails    `json:"rails"`
	Status           CharterStatus   `json:"status"`
	Autonomy         CharterAutonomy `json:"autonomy,omitempty"`
	GreenFirings     int             `json:"green_firings,omitempty"`
	Demotions        int             `json:"demotions,omitempty"`
	Ratification     Ratification    `json:"ratification"`
	SessionID        string          `json:"session_id,omitempty"`
	Spec             CharterSpec     `json:"spec,omitempty"`
	SourceCommandSeq int64           `json:"source_command_seq,omitempty"`
	ProposalShape    string          `json:"proposal_shape,omitempty"`
	LastWake         time.Time       `json:"last_wake,omitempty"`
	LastChecked      time.Time       `json:"last_checked,omitempty"`
	LastCheckLine    string          `json:"last_check_line,omitempty"`
	NextDue          time.Time       `json:"next_due,omitempty"`
	WakeSeq          int64           `json:"wake_seq,omitempty"`
	WakePending      bool            `json:"wake_pending,omitempty"`
	SentinelYes      bool            `json:"sentinel_yes,omitempty"`
	WakeEvidence     string          `json:"wake_evidence,omitempty"`
	FileFingerprint  string          `json:"file_fingerprint,omitempty"`
	GraphCursor      int64           `json:"graph_cursor,omitempty"`
	GraphDay         string          `json:"graph_day,omitempty"`
	GraphTriggered   bool            `json:"graph_triggered,omitempty"`
	CreatedAt        time.Time       `json:"created_at,omitempty"`
}

type charterStatusPayload struct {
	Status       CharterStatus `json:"status"`
	Ratification Ratification  `json:"ratification"`
	Reason       string        `json:"reason"`
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
	WakeSeq           int64  `json:"wake_seq"`
	JobID             string `json:"job_id,omitempty"`
	SayOnly           bool   `json:"say_only,omitempty"`
	ProbationApproved bool   `json:"probation_approved,omitempty"`
	Reason            string `json:"reason,omitempty"`
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
	// Admission is the safety boundary: even a manually assembled value cannot
	// smuggle tenure or prior evidence into a newly created capability.
	charter.Autonomy, charter.GreenFirings, charter.Demotions = CharterProbation, 0, 0
	now := time.Now()
	charter.CreatedAt = now
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
	autonomy := c.Autonomy
	if !validCharterAutonomy(autonomy) {
		autonomy = CharterProbation
	}
	sessionID := strings.TrimSpace(c.SessionID)
	if sessionID == "" {
		sessionID = c.Ratification.SessionID
	}
	spec := c.Spec
	if strings.TrimSpace(spec.Invariant) == "" {
		spec = charterSpecFromCanonical(c)
	}
	return charterRecord{
		ID: c.ID, Invariant: c.Invariant, Watch: c.Watch, SentinelHint: c.SentinelHint,
		Action: c.Action, Rails: normalizeCharterRails(c.guardrails), Status: c.Status,
		Autonomy: autonomy, GreenFirings: c.GreenFirings, Demotions: c.Demotions,
		Ratification: c.Ratification, SessionID: sessionID, Spec: spec,
		SourceCommandSeq: c.SourceCommandSeq,
		ProposalShape:    c.ProposalShape, LastWake: c.LastWake,
		LastChecked: c.LastChecked, LastCheckLine: c.LastCheckLine, NextDue: c.NextDue,
		WakeSeq: c.WakeSeq, WakePending: c.WakePending, SentinelYes: c.SentinelYes,
		WakeEvidence:    c.WakeEvidence,
		FileFingerprint: c.FileFingerprint, GraphCursor: c.GraphCursor, GraphDay: c.GraphDay,
		GraphTriggered: c.GraphTriggered, CreatedAt: c.CreatedAt,
	}
}

func applyCharterCreated(tx *sql.Tx, payload charterRecord, seq int64, at time.Time) error {
	if payload.Status == "draft" {
		payload.Status = CharterProposed
	}
	if !validCharterAutonomy(payload.Autonomy) {
		payload.Autonomy = CharterProbation
	}
	payload.Rails = normalizeCharterRails(payload.Rails)
	if payload.CreatedAt.IsZero() {
		payload.CreatedAt = at
	}
	if payload.SessionID == "" {
		payload.SessionID = payload.Ratification.SessionID
	}
	if strings.TrimSpace(payload.Spec.Invariant) == "" {
		payload.Spec = charterSpecFromRecord(payload)
	}
	watch, _ := json.Marshal(payload.Watch)
	action, _ := json.Marshal(payload.Action)
	encodedRails, _ := json.Marshal(payload.Rails)
	ratification, _ := json.Marshal(payload.Ratification)
	spec, _ := json.Marshal(payload.Spec)
	graphCursor := payload.GraphCursor
	if payload.Watch.Kind == WatchGraph && graphCursor == 0 {
		graphCursor = seq
	}
	if _, err := tx.Exec(`
		INSERT INTO charters (
		    id, invariant, watch, sentinel_hint, action, rails, status, autonomy,
		    green_firings, demotions, ratification, session_id, spec, source_command_seq,
		    proposal_shape, last_wake, next_due, wake_seq, wake_pending, sentinel_yes, wake_evidence,
		    last_checked, last_check_line,
		    file_fingerprint, graph_cursor, graph_day, graph_triggered, created_seq, updated_seq, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		payload.ID, payload.Invariant, string(watch), payload.SentinelHint, string(action), string(encodedRails),
		payload.Status, payload.Autonomy, payload.GreenFirings, payload.Demotions, string(ratification),
		payload.SessionID, string(spec), payload.SourceCommandSeq, payload.ProposalShape, nullTime(payload.LastWake),
		nullTime(payload.NextDue), payload.WakeSeq, payload.WakePending, payload.SentinelYes, payload.WakeEvidence,
		nullTime(payload.LastChecked), payload.LastCheckLine,
		payload.FileFingerprint, graphCursor, payload.GraphDay, payload.GraphTriggered, seq, seq,
		formatTime(payload.CreatedAt)); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO charters_fts (charter_id, invariant) VALUES (?, ?)`, payload.ID, payload.Invariant); err != nil {
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
		nullIfEmpty(payload.SessionID), payload.Invariant, seq, seq, formatTime(at), brief)
	if err != nil {
		return err
	}
	if err := refreshCharterFTS(tx, payload.ID, payload.Invariant); err != nil {
		return err
	}
	return refreshGraphFTS(tx, payload.ID)
}

// refreshCharterFTS keeps the BM25 invariant index in step with the charters
// view on every event that can change an invariant.
func refreshCharterFTS(tx *sql.Tx, id, invariant string) error {
	if _, err := tx.Exec(`DELETE FROM charters_fts WHERE charter_id = ?`, id); err != nil {
		return err
	}
	_, err := tx.Exec(`INSERT INTO charters_fts (charter_id, invariant) VALUES (?, ?)`, id, invariant)
	return err
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
    id, invariant, watch, sentinel_hint, action, rails, status, autonomy,
    green_firings, demotions, ratification, session_id, spec, source_command_seq,
    proposal_shape, last_wake, next_due, wake_seq, wake_pending, sentinel_yes, wake_evidence,
    last_checked, last_check_line,
    file_fingerprint, graph_cursor, graph_day, graph_triggered, created_seq, updated_seq, created_at`

// qualifiedCharterColumns prefixes every charter column for queries that join
// tables sharing column names, such as the invariant FTS index.
func qualifiedCharterColumns(table string) string {
	columns := strings.Split(charterColumns, ",")
	for index, column := range columns {
		columns[index] = table + "." + strings.TrimSpace(column)
	}
	return strings.Join(columns, ", ")
}

func scanCharter(scanner rowScanner) (Charter, error) {
	var c Charter
	var watch, action, rails, ratification, spec, createdAt string
	var lastWake, nextDue, lastChecked sql.NullString
	if err := scanner.Scan(&c.ID, &c.Invariant, &watch, &c.SentinelHint, &action, &rails,
		&c.Status, &c.Autonomy, &c.GreenFirings, &c.Demotions, &ratification,
		&c.SessionID, &spec, &c.SourceCommandSeq, &c.ProposalShape, &lastWake, &nextDue, &c.WakeSeq,
		&c.WakePending, &c.SentinelYes, &c.WakeEvidence, &lastChecked, &c.LastCheckLine,
		&c.FileFingerprint, &c.GraphCursor, &c.GraphDay,
		&c.GraphTriggered, &c.CreatedSeq, &c.UpdatedSeq, &createdAt); err != nil {
		return Charter{}, err
	}
	if c.Status == "draft" {
		c.Status = CharterProposed
	}
	if !validCharterAutonomy(c.Autonomy) {
		c.Autonomy = CharterProbation
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
	c.guardrails = normalizeCharterRails(c.guardrails)
	if err := json.Unmarshal([]byte(ratification), &c.Ratification); err != nil {
		return Charter{}, err
	}
	if err := json.Unmarshal([]byte(spec), &c.Spec); err != nil {
		return Charter{}, err
	}
	var err error
	c.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Charter{}, err
	}
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
	if lastChecked.Valid {
		c.LastChecked, err = parseTime(lastChecked.String)
		if err != nil {
			return Charter{}, err
		}
	}
	if strings.TrimSpace(c.Spec.Invariant) == "" {
		c.Spec = charterSpecFromCanonical(c)
	}
	return c, nil
}

// Charters lists first-class standing objects in creation order.
func (s *Store) Charters(statuses ...CharterStatus) ([]Charter, error) {
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
	if len(statuses) > 0 {
		wanted := make(map[CharterStatus]bool, len(statuses))
		for _, status := range statuses {
			wanted[status] = true
		}
		filtered := result[:0]
		for _, charter := range result {
			if wanted[charter.Status] {
				filtered = append(filtered, charter)
			}
		}
		result = filtered
	}
	// The ORDER BY above is already (created_seq, id); re-sorting the decoded
	// rows only repeats work SQLite has done.
	return result, nil
}

// ActiveCharters is the plain standing list used by conversation and
// /standing: ratified work only, newest first.
func (s *Store) ActiveCharters() ([]Charter, error) {
	rows, err := s.db.Query(`SELECT `+charterColumns+` FROM charters WHERE status = ? ORDER BY created_seq DESC`,
		CharterActive)
	if err != nil {
		return nil, fmt.Errorf("list active charters: %w", err)
	}
	defer rows.Close()
	var result []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
		if err != nil {
			return nil, fmt.Errorf("list active charters: %w", err)
		}
		result = append(result, charter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active charters: %w", err)
	}
	return result, nil
}

// SearchActiveCharters uses SQLite's BM25 rank over invariant text. An empty
// reference deliberately returns every active charter so pronouns can resolve
// when there is exactly one and ask back when there is more than one.
func (s *Store) SearchActiveCharters(reference string) ([]Charter, error) {
	terms := charterSearchTerms(reference)
	if len(terms) == 0 {
		return s.ActiveCharters()
	}
	quoted := make([]string, 0, len(terms))
	for _, term := range terms {
		quoted = append(quoted, `"`+strings.ReplaceAll(term, `"`, `""`)+`"`)
	}
	rows, err := s.db.Query(`SELECT `+qualifiedCharterColumns("charters")+` FROM charters
		JOIN charters_fts ON charters_fts.charter_id = charters.id
		WHERE charters.status = ? AND charters_fts MATCH ?
		ORDER BY bm25(charters_fts), charters.created_seq DESC`,
		CharterActive, strings.Join(quoted, " OR "))
	if err != nil {
		return nil, fmt.Errorf("search active charters: %w", err)
	}
	defer rows.Close()
	var result []Charter
	for rows.Next() {
		charter, err := scanCharter(rows)
		if err != nil {
			return nil, fmt.Errorf("search active charters: %w", err)
		}
		result = append(result, charter)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search active charters: %w", err)
	}
	return result, nil
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
	validated.Autonomy = current.Autonomy
	validated.GreenFirings = current.GreenFirings
	validated.Demotions = current.Demotions
	validated.SessionID = current.SessionID
	validated.SourceCommandSeq = current.SourceCommandSeq
	validated.CreatedAt = current.CreatedAt
	validated.ProposalShape = current.ProposalShape
	validated.LastWake = current.LastWake
	validated.Spec = charterSpecFromCanonical(validated)
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
	spec, _ := json.Marshal(payload.Spec)
	graphCursor := payload.GraphCursor
	if payload.Watch.Kind == WatchGraph && graphCursor == 0 {
		graphCursor = seq
	}
	result, err := tx.Exec(`UPDATE charters SET invariant=?, watch=?, sentinel_hint=?, action=?, rails=?, spec=?,
		next_due=?, wake_seq=?, wake_pending=?, sentinel_yes=?, wake_evidence=?, file_fingerprint=?, graph_cursor=?,
		graph_day=?, graph_triggered=?, updated_seq=? WHERE id=?`,
		payload.Invariant, string(watch), payload.SentinelHint, string(action), string(rails), string(spec),
		nullTime(payload.NextDue), payload.WakeSeq, payload.WakePending, payload.SentinelYes,
		payload.WakeEvidence, payload.FileFingerprint, graphCursor, payload.GraphDay, payload.GraphTriggered, seq, payload.ID)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("charter %q is missing", payload.ID)
	}
	brief := bounded("Charter: "+payload.Invariant, MaxDigestBytes)
	if _, err := tx.Exec(`DELETE FROM charters_fts WHERE charter_id=?`, payload.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO charters_fts (charter_id, invariant) VALUES (?, ?)`, payload.ID, payload.Invariant); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE nodes SET brief=?, title=?, summary=?, fold_digest=?, updated_seq=? WHERE id=?`,
		brief, firstCharterLine(payload.Invariant), brief, brief, seq, payload.ID); err != nil {
		return err
	}
	if err := refreshCharterFTS(tx, payload.ID, payload.Invariant); err != nil {
		return err
	}
	return refreshGraphFTS(tx, payload.ID)
}

// SetCharterStatus journals pause, activation, and retirement. Activation is
// the one transition that requires fresh explicit ratification provenance.
func (s *Store) SetCharterStatus(id string, status CharterStatus, ratification Ratification) error {
	return s.SetCharterStatusWithReason(id, status, ratification, "status changed to "+string(status))
}

// SetCharterStatusWithReason is the policy-bearing form used by conversational
// management and automatic pauses. The reason is part of the journal event.
func (s *Store) SetCharterStatusWithReason(id string, status CharterStatus, ratification Ratification, reason string) error {
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
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return fmt.Errorf("set charter status: %w: transition reason is required", ErrInvalid)
	}
	payload := charterStatusPayload{Status: status, Ratification: ratification, Reason: bounded(reason, MaxDigestBytes)}
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
	clearWake := payload.Status != CharterActive
	result, err := tx.Exec(`UPDATE charters SET status=?, ratification=?,
		session_id=CASE WHEN ? THEN ? ELSE session_id END,
		wake_pending=CASE WHEN ? THEN 0 ELSE wake_pending END,
		sentinel_yes=CASE WHEN ? THEN 0 ELSE sentinel_yes END, updated_seq=? WHERE id=?`,
		payload.Status, string(ratification), payload.Status == CharterActive,
		payload.Ratification.SessionID, clearWake, clearWake, seq, id)
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
	seq, at, err := appendEvent(tx, id, EventSentinelChecked, check)
	if err != nil {
		return err
	}
	if err := applySentinelCheck(tx, id, check, at, seq); err != nil {
		return err
	}
	return tx.Commit()
}

// SentinelJudgment is one past wake judgment together with what became of it.
// Line and Error had no reader anywhere: the sentinel wrote down its reasoning
// every wake and never saw a word of it again.
type SentinelJudgment struct {
	Yes  bool
	Line string
	// Outcome is what happened after the judgment — the firing it caused, or
	// the user's refusal of that firing. Empty means nothing followed, which is
	// itself worth reading: a yes that led nowhere.
	Outcome string
	Error   string
}

// RecentSentinelJudgments returns a charter's last judgments, newest first.
//
// A poll charter's evidence is the constant condition string, so the sentinel's
// input is byte-identical every wake — which means a firing the user has
// already declined will be judged the same way, forever, by a call that has no
// way of knowing it ever happened before. This is that memory: one bounded
// read of the charter's own journal, shaped like DeliveryGateFor, with no new
// table and no new event behind it.
//
// The outcome is paired in the same pass rather than queried per judgment. The
// journal is ordered, so walking backwards means every firing or refusal is
// seen before the check that produced it.
func (s *Store) RecentSentinelJudgments(id string, limit int) ([]SentinelJudgment, error) {
	if limit <= 0 {
		return nil, nil
	}
	// Three kinds per judgment at the very most, so this window cannot run out
	// of checks before it has found `limit` of them.
	rows, err := s.db.Query(`
		SELECT kind, payload FROM events
		WHERE node_id = ? AND kind IN (?, ?, ?)
		ORDER BY seq DESC LIMIT ?`,
		id, EventSentinelChecked, EventCharterFired, EventCharterFiringDeclined, limit*3)
	if err != nil {
		return nil, fmt.Errorf("read sentinel judgments: %w", err)
	}
	defer rows.Close()
	judgments := make([]SentinelJudgment, 0, limit)
	outcome := ""
	for rows.Next() {
		var kind EventKind
		var payload string
		if err := rows.Scan(&kind, &payload); err != nil {
			return nil, fmt.Errorf("read sentinel judgments: %w", err)
		}
		switch kind {
		case EventCharterFired:
			outcome = "it fired"
			continue
		case EventCharterFiringDeclined:
			outcome = "it fired and the user declined the work"
			continue
		}
		var check SentinelCheck
		if err := json.Unmarshal([]byte(payload), &check); err != nil {
			return nil, fmt.Errorf("read sentinel judgments: %w", err)
		}
		judgments = append(judgments, SentinelJudgment{
			Yes: check.Yes, Line: check.Line, Error: check.Error, Outcome: outcome,
		})
		outcome = ""
		if len(judgments) == limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read sentinel judgments: %w", err)
	}
	return judgments, nil
}

func applySentinelCheck(tx *sql.Tx, id string, check SentinelCheck, at time.Time, seq int64) error {
	pending, yes := false, false
	if check.Yes && check.Error == "" {
		pending, yes = true, true
	}
	// The check itself is the state. A sentinel that answers no closes its wake
	// and used to leave nothing behind but an event nobody read; the moment and
	// the reason now live on the row every surface already loads.
	result, err := tx.Exec(`UPDATE charters SET wake_pending=?, sentinel_yes=?,
		last_checked=?, last_check_line=?, updated_seq=? WHERE id=? AND wake_seq=?`,
		pending, yes, formatTime(at), check.Line, seq, id, check.WakeSeq)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return fmt.Errorf("sentinel wake %d is missing", check.WakeSeq)
	}
	return nil
}

// CharterLastFired is when this watch last did anything at all. A zero time
// means it never has, which is the sharpest form of the same answer.
func (s *Store) CharterLastFired(id string) (time.Time, error) {
	var timestamp string
	err := s.db.QueryRow(`SELECT ts FROM events WHERE node_id=? AND kind=?
		ORDER BY seq DESC LIMIT 1`, id, EventCharterFired).Scan(&timestamp)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("read charter last firing: %w", err)
	}
	at, err := parseTime(timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("read charter last firing: %w", err)
	}
	return at, nil
}

// CharterCheckCount is how many times a sentinel has looked since a moment. It
// is the denominator of usefulness: without it "this watch has found nothing"
// cannot be told apart from "this watch has never run".
func (s *Store) CharterCheckCount(id string, since time.Time) (int, error) {
	var checks int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE node_id=? AND kind=? AND ts>=?`,
		id, EventSentinelChecked, formatTime(since)).Scan(&checks)
	if err != nil {
		return 0, fmt.Errorf("count charter checks: %w", err)
	}
	return checks, nil
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
	expiries, err := s.charterExpiries()
	if err != nil {
		return 0, err
	}
	retired := 0
	for _, expiry := range expiries {
		if now.Before(expiry.At) {
			continue
		}
		changed, err := s.RetireExpiredCharter(expiry.ID, now)
		if err != nil {
			return retired, err
		}
		if changed {
			retired++
		}
	}
	return retired, nil
}

// CharterClockDeadline reports the earliest instant at which the passage of
// time alone could change charter state: a reserved wake still pending, a
// scheduled next_due arriving, or an expiry rail falling due. The second result
// is false when no charter is waiting on the clock at all, which is the normal
// answer for a store with no standing work.
func (s *Store) CharterClockDeadline(now time.Time) (time.Time, bool, error) {
	var immediate bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM charters
		WHERE status = ? AND (wake_pending = 1 OR next_due IS NULL))`, CharterActive).Scan(&immediate); err != nil {
		return time.Time{}, false, fmt.Errorf("charter clock deadline: %w", err)
	}
	if immediate {
		return now, true, nil
	}
	deadline := time.Time{}
	// next_due is written by formatTime, so it is UTC and its text order is its
	// time order; MIN can safely stay in SQL.
	var scheduled sql.NullString
	if err := s.db.QueryRow(`SELECT MIN(next_due) FROM charters
		WHERE status = ? AND next_due IS NOT NULL`, CharterActive).Scan(&scheduled); err != nil {
		return time.Time{}, false, fmt.Errorf("charter clock deadline: %w", err)
	}
	if scheduled.Valid {
		at, err := parseTime(scheduled.String)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("charter clock deadline: %w", err)
		}
		deadline = at
	}
	expiries, err := s.charterExpiries()
	if err != nil {
		return time.Time{}, false, err
	}
	for _, expiry := range expiries {
		if deadline.IsZero() || expiry.At.Before(deadline) {
			deadline = expiry.At
		}
	}
	return deadline, !deadline.IsZero(), nil
}

// charterExpiry is one live expiry rail, named rather than decoded: the whole
// charter row costs five JSON decodes and the expiry question needs one field.
type charterExpiry struct {
	ID string
	At time.Time
}

// charterExpiries reads every live expiry rail. Comparison stays in Go because
// an expiry rail is written by encoding/json in whatever zone the user named it
// in, so the stored strings are not ordered by their text the way the
// UTC-normalized columns are.
func (s *Store) charterExpiries() ([]charterExpiry, error) {
	rows, err := s.db.Query(`SELECT id, json_extract(rails, '$.expires_at') FROM charters
		WHERE status <> ? AND json_extract(rails, '$.expires_at') IS NOT NULL
		ORDER BY created_seq, id`, CharterRetired)
	if err != nil {
		return nil, fmt.Errorf("list expiring charters: %w", err)
	}
	defer rows.Close()
	var expiries []charterExpiry
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return nil, fmt.Errorf("list expiring charters: %w", err)
		}
		expires, err := parseTime(at)
		if err != nil {
			return nil, fmt.Errorf("parse charter %q expiry: %w", id, err)
		}
		if expires.IsZero() {
			continue
		}
		expiries = append(expiries, charterExpiry{ID: id, At: expires})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list expiring charters: %w", err)
	}
	return expiries, nil
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
	return s.fireCharter(id, wakeSeq, subtree, provenance, dailyBudgetUSD, now, nil, false)
}

// FireApprovedCharter follows the same journaled admission path after a user
// approves one probation proposal. It is the only probation bypass.
func (s *Store) FireApprovedCharter(id string, wakeSeq int64, subtree Subtree, provenance Provenance,
	dailyBudgetUSD float64, now time.Time) (FireDisposition, error) {
	return s.fireCharter(id, wakeSeq, subtree, provenance, dailyBudgetUSD, now, nil, true)
}

type practiceAdmission struct {
	QuestionSeq      int64
	BaselineSurprise float64
	ExpectedTokens   int
}

// FirePracticeCharter is the self-origin variant of FireCharter. It preserves
// the same wake, expiry, firing-cap, and global-rail admission, adds the
// practice charter's own daily dollar carve-out, and atomically marks the
// selected question practicing after the subtree splice lands.
func (s *Store) FirePracticeCharter(id string, wakeSeq int64, subtree Subtree,
	questionSeq int64, baselineSurprise float64, expectedTokens int,
	dailyBudgetUSD float64, now time.Time) (FireDisposition, error) {
	intent := "practice knowledge gap"
	for _, node := range subtree.Nodes {
		if strings.TrimSpace(node.Parent) == "" && strings.TrimSpace(node.Brief) != "" {
			intent = node.Brief
			break
		}
	}
	provenance := Provenance{Origin: OriginSelf, Intent: intent}
	practice := &practiceAdmission{QuestionSeq: questionSeq,
		BaselineSurprise: baselineSurprise, ExpectedTokens: expectedTokens}
	// A practice charter is self-tenured by construction: its rails and idle
	// gate are the approval boundary.
	return s.fireCharter(id, wakeSeq, subtree, provenance, dailyBudgetUSD, now, practice, true)
}

// fireCharter atomically admits work after the autonomy boundary is satisfied.
func (s *Store) fireCharter(id string, wakeSeq int64, subtree Subtree, provenance Provenance,
	dailyBudgetUSD float64, now time.Time, practice *practiceAdmission, probationApproved bool) (FireDisposition, error) {
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
	if practice != nil {
		if !IsPracticeCharter(charter) || charter.Action.SayOnly || practice.QuestionSeq <= 0 ||
			practice.BaselineSurprise <= 0 || math.IsNaN(practice.BaselineSurprise) ||
			math.IsInf(practice.BaselineSurprise, 0) || practice.ExpectedTokens < 0 {
			return "", fmt.Errorf("fire practice charter: %w: invalid practice admission", ErrInvalid)
		}
		provenance.Origin = OriginSelf
		provenance.SessionID = ""
		provenance.CharterID = ""
	}
	if charter.Autonomy != CharterTenured && !probationApproved {
		return "", fmt.Errorf("fire charter: %w: probation firing requires approval", ErrInvalid)
	}
	if probationApproved && practice == nil {
		instruction := "wake:" + fmt.Sprint(wakeSeq)
		var authorized bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM commands
			WHERE target=? AND kind=? AND instruction=? AND status IN (?, ?))`,
			id, CommandCharterFire, instruction, CommandPending, CommandApplied).Scan(&authorized); err != nil {
			return "", err
		}
		if !authorized {
			return "", fmt.Errorf("fire charter: %w: probation wake has no journaled approval command", ErrInvalid)
		}
	}
	if expires := charter.guardrails.ExpiresAt; expires != nil && !now.Before(*expires) {
		payload := charterStatusPayload{Status: CharterRetired, Ratification: charter.Ratification,
			Reason: "charter expired before firing"}
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
	if practice != nil {
		var practiceSpend float64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(usage.cost), 0)
			FROM usage JOIN nodes ON nodes.id=usage.node_id
			WHERE nodes.grp=? AND usage.ts>=? AND usage.ts<?`, PracticeGroup,
			formatTime(start), formatTime(end)).Scan(&practiceSpend); err != nil {
			return "", err
		}
		// Admission has to reserve, not merely observe. Journaled spend arrives
		// long after admission — a firing costs nothing measurable until its
		// leaves land — so a carve-out read from the usage table alone lets
		// every firing of the day in before any of them has recorded a cent,
		// and the ceiling only ever bites the day after it was breached. Each
		// firing already admitted today therefore holds its own charter's
		// per-firing budget against the group's ceiling.
		//
		// The two are combined by taking the larger rather than the sum: a
		// firing that has already journaled more than it reserved is counted at
		// what it truly cost, and one that has journaled less is still counted
		// at what it was allowed to cost, without any firing being charged
		// twice.
		var reserved float64
		if err := tx.QueryRow(`SELECT COALESCE(SUM(CAST(
			json_extract(charters.rails, '$.per_firing_budget_usd') AS REAL)), 0)
			FROM events JOIN charters ON charters.id=events.node_id
			WHERE events.kind=? AND events.ts>=? AND events.ts<? AND charters.proposal_shape=?`,
			EventCharterFired, formatTime(start), formatTime(end),
			PracticeCharterShape).Scan(&reserved); err != nil {
			return "", err
		}
		committed := math.Max(practiceSpend, reserved)
		practiceCeiling := charter.guardrails.PerFiringBudgetUSD *
			float64(charter.guardrails.MaxFiringsPerDay)
		if committed+charter.guardrails.PerFiringBudgetUSD > practiceCeiling {
			payload := charterFiringPayload{WakeSeq: wakeSeq, Reason: "practice_daily_dollar_rail"}
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
	}
	rail, err := dailyRailAt(tx, dailyBudgetUSD, now)
	if err != nil {
		return "", err
	}
	projectedCrossing := !rail.Unlimited && rail.Spend+charter.guardrails.PerFiringBudgetUSD > rail.Ceiling
	if projectedCrossing {
		rail.Reached = true
		posted := false
		if practice == nil {
			posted, err = pauseDailyRailTx(tx, rail, charter.Ratification.SessionID, now)
			if err != nil {
				return "", err
			}
		} else {
			if err := tx.QueryRow(`SELECT NOT EXISTS(SELECT 1 FROM events
				WHERE node_id=? AND kind=? AND json_extract(payload, '$.wake_seq')=?)`,
				id, EventCharterFiringDeferred, wakeSeq).Scan(&posted); err != nil {
				return "", err
			}
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

	payload := charterFiringPayload{WakeSeq: wakeSeq, SayOnly: charter.Action.SayOnly,
		ProbationApproved: probationApproved}
	if !charter.Action.SayOnly {
		normalized, err := normalizeSubtree(RootID, subtree, provenance)
		if err != nil {
			return "", err
		}
		payload.JobID = normalized.Root
		admitted := make([]string, 0, len(normalized.Nodes))
		for _, node := range normalized.Nodes {
			admitted = append(admitted, node.ID)
		}
		present, err := existingNodeIDs(tx, admitted)
		if err != nil {
			return "", err
		}
		for _, node := range normalized.Nodes {
			if present[node.ID] {
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
		if practice != nil {
			started := QuestionPracticeStarted{QuestionSeq: practice.QuestionSeq,
				JobID: normalized.Root, BaselineSurprise: practice.BaselineSurprise,
				ExpectedTokens: practice.ExpectedTokens}
			practiceSeq, _, err := appendEvent(tx, normalized.Root, EventQuestionPracticeStarted, started)
			if err != nil {
				return "", err
			}
			if err := applyQuestionPracticeStarted(tx, started, practiceSeq); err != nil {
				return "", err
			}
		}
	} else {
		payload.JobID = "say:" + charter.ID + ":" + fmt.Sprint(wakeSeq)
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
		return applySentinelCheck(tx, event.NodeID, payload, event.Time, event.Seq)
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
	case EventCharterFiringProposed, EventCharterFiringDeclined, EventCharterFiringReviewed,
		EventCharterPromoted, EventCharterDemoted:
		return replayTenureEvent(tx, event)
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
	case CronAt:
		// One occurrence. After it has passed, park the next due far beyond
		// the reminder's expiry rail so the watch engine never re-wakes it.
		if after.Before(schedule.At) {
			return schedule.At, nil
		}
		return schedule.At.AddDate(1, 0, 0), nil
	case CronDaily, CronWeekdays, CronWeekly:
		local := after.In(location)
		for offset := 0; offset <= 8; offset++ {
			day := local.AddDate(0, 0, offset)
			if schedule.Kind == CronWeekdays && (day.Weekday() == time.Saturday || day.Weekday() == time.Sunday) {
				continue
			}
			// A weekly rule keeps its named day forever: the search walks days
			// and only a matching weekday can answer, so the occurrence after
			// one Sunday is the next Sunday and never seven days from whenever
			// the last one happened to run.
			if schedule.Kind == CronWeekly && day.Weekday() != schedule.Weekday {
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
