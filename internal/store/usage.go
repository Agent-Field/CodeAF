package store

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"
)

// Usage accounting lives in the same journal as the work it measures. One
// event per executed node, materialized into a running total, so any lens can
// answer "what has this graph cost" without replaying history.

// NodeUsage is what one node's execution spent.
type NodeUsage struct {
	NodeID           string  `json:"node_id"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	Cost             float64 `json:"cost"`
}

// NodeSurprise is the prediction attached to one leaf when its profile record
// lands. ActualTokens is repeated here so job prediction comparisons exclude
// leaves whose expectation was undefined.
type NodeSurprise struct {
	NodeID         string  `json:"node_id"`
	ActualTokens   int     `json:"actual_tokens"`
	ExpectedTokens int     `json:"expected_tokens"`
	Surprise       float64 `json:"surprise"`
}

// TotalUsage is the graph-wide running total.
type TotalUsage struct {
	Nodes            int
	PromptTokens     int
	CompletionTokens int
	Cost             float64
}

// JobUsage is one top-level job's full subtree size and measured spend.
// NodeCount is graph structure; Runs is the number of recorded executions,
// which may exceed NodeCount when a node is attempted more than once.
type JobUsage struct {
	NodeCount        int
	Runs             int
	PromptTokens     int
	CompletionTokens int
	Cost             float64
	SurpriseSamples  int
	SurpriseTokens   int
	ExpectedTokens   int
	Surprise         *float64
}

// RailAdjustment is one journaled increase to today's dollar ceiling.
type RailAdjustment struct {
	Amount    float64 `json:"amount"`
	Origin    string  `json:"origin"`
	Unlimited bool    `json:"unlimited,omitempty"`
}

// DailyRail is today's policy state. Base zero is unlimited; Raised remains
// visible as journal history but cannot make an unlimited rail more unlimited.
//
// Spend is everything the rail is deciding against, which is not always what
// the journal has seen: Pending is the part of it that has not been recorded —
// in-process cost a headless run will journal at exit, or the catalog price of
// a generation that has not happened yet. The split exists so a question can
// say which is which, because the two are consented to by different arithmetic.
type DailyRail struct {
	Base      float64
	Raised    float64
	Spend     float64
	Pending   float64
	Ceiling   float64
	Unlimited bool
	Reached   bool
}

// DailyRailQuestionPrefix is stable because the journaled message is also the
// durable once-per-raise question marker.
const DailyRailQuestionPrefix = "Daily budget reached -- "

// RaiseAmount restores one configured budget unit of headroom. Concurrent
// leaves may overshoot the old rail while landing, so the raise also covers
// that overshoot instead of immediately asking the same question again.
func (rail DailyRail) RaiseAmount() float64 {
	if rail.Unlimited || rail.Base <= 0 {
		return 0
	}
	amount := rail.Base + math.Max(0, rail.Spend-rail.Ceiling)
	return math.Ceil(amount*100) / 100
}

const usageSchema = `
CREATE TABLE IF NOT EXISTS usage (
    seq               INTEGER PRIMARY KEY REFERENCES events(seq),
    ts                TEXT NOT NULL,
    node_id           TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cost              REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS usage_node ON usage (node_id);
`

const surpriseSchema = `
CREATE TABLE IF NOT EXISTS surprises (
    seq             INTEGER PRIMARY KEY REFERENCES events(seq),
    ts              TEXT NOT NULL,
    node_id         TEXT NOT NULL,
    actual_tokens   INTEGER NOT NULL DEFAULT 0,
    expected_tokens INTEGER NOT NULL DEFAULT 0,
    surprise        REAL NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX IF NOT EXISTS surprises_node ON surprises (node_id);
`

// RecordUsage appends one node's spend to the journal. Zero-valued usage is
// recorded too: "this ran and cost nothing measurable" is information.
func (s *Store) RecordUsage(usage NodeUsage) error {
	if usage.NodeID == "" {
		return fmt.Errorf("record usage: %w: empty node id", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	defer tx.Rollback()

	if err := requireNode(tx, usage.NodeID); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	seq, at, err := appendEvent(tx, usage.NodeID, EventUsageRecorded, usage)
	if err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	if err := applyUsageView(tx, usage, seq, at); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record usage: %w", err)
	}
	return nil
}

// RecordSurprise attaches a defined profile prediction residual to one node.
// Undefined expectations produce no event, preserving absent versus zero.
func (s *Store) RecordSurprise(surprise NodeSurprise) error {
	if surprise.NodeID == "" {
		return fmt.Errorf("record surprise: %w: empty node id", ErrInvalid)
	}
	if surprise.ActualTokens < 0 || surprise.ExpectedTokens < 0 || surprise.Surprise < 0 ||
		math.IsNaN(surprise.Surprise) || math.IsInf(surprise.Surprise, 0) {
		return fmt.Errorf("record surprise: %w: invalid measurement", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	defer tx.Rollback()

	if err := requireNode(tx, surprise.NodeID); err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	seq, at, err := appendEvent(tx, surprise.NodeID, EventSurpriseRecorded, surprise)
	if err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	if err := applySurpriseView(tx, surprise, seq, at); err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record surprise: %w", err)
	}
	return nil
}

// Usage returns the graph-wide total.
func (s *Store) Usage() (TotalUsage, error) {
	var total TotalUsage
	err := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(cost), 0)
		FROM usage`).Scan(&total.Nodes, &total.PromptTokens, &total.CompletionTokens, &total.Cost)
	if err != nil {
		return TotalUsage{}, fmt.Errorf("total usage: %w", err)
	}
	return total, nil
}

// SpendToday sums recorded execution cost since local midnight. Event times
// are UTC on disk; the boundary is local policy time converted to UTC, so DST
// and non-UTC operators get the day they actually mean.
func (s *Store) SpendToday() (float64, error) {
	return s.spendTodayAt(time.Now())
}

func (s *Store) spendTodayAt(now time.Time) (float64, error) {
	start, end := localDayBounds(now)
	var spend float64
	if err := s.db.QueryRow(`
		SELECT COALESCE(SUM(cost), 0) FROM usage WHERE ts >= ? AND ts < ?`,
		formatTime(start), formatTime(end)).Scan(&spend); err != nil {
		return 0, fmt.Errorf("spend today: %w", err)
	}
	return spend, nil
}

// DailyRailToday combines the configured base with today's journaled raises.
func (s *Store) DailyRailToday(base float64) (DailyRail, error) {
	return dailyRailAt(s.db, base, time.Now())
}

// WithAdditionalSpend includes not-yet-journaled in-process cost in a rail
// check. Headless scheduling uses it between landed leaves, then journals the
// aggregate before exit.
func (rail DailyRail) WithAdditionalSpend(amount float64) DailyRail {
	rail.Spend += amount
	rail.Pending += amount
	rail.Reached = !rail.Unlimited && rail.Spend >= rail.Ceiling
	return rail
}

// journaled is this rail as the journal alone describes it: the same day, the
// same ceiling, minus whatever has not been recorded yet.
func (rail DailyRail) journaled() DailyRail {
	rail.Spend = math.Max(0, rail.Spend-rail.Pending)
	rail.Pending = 0
	rail.Reached = !rail.Unlimited && rail.Spend >= rail.Ceiling
	return rail
}

// Question renders the one user-visible policy stop. Resource units stay
// backstage; only today's spend, ceiling, and exact effect of consent appear.
//
// This is the wording for a caller that consents on the very rail it was
// shown — the headless prompt, which raises RaiseAmount() of this same figure
// the moment the operator says yes. There the pending part is money already
// spent in this process and merely not yet journaled, so folding it into the
// total is the honest thing to say.
func (rail DailyRail) Question() string {
	return fmt.Sprintf("%s$%.2f spent of $%.2f. Say the word and I'll continue (raises today's rail by $%.2f).",
		DailyRailQuestionPrefix, rail.Spend, rail.Ceiling, rail.RaiseAmount())
}

// postedQuestion is the wording of the durable question the pause writes into
// the thread, and it is deliberately a different sentence.
//
// Consent to that one arrives later and from somewhere else: the head reads the
// rail back from journaled spend alone and raises that. A question worded from
// a total the journal has never seen would promise a number consent cannot
// deliver — "$34.00 spent … raises by $34.00" answered by a $20.00 raise. So
// the posted wording quotes what the journal knows, states the raise consent
// will actually make, and names the pending item separately as the reason the
// work stopped here rather than folding it into the total.
func (rail DailyRail) postedQuestion() string {
	journaled := rail.journaled()
	if rail.Pending <= 0 {
		return journaled.Question()
	}
	return fmt.Sprintf("%s$%.2f spent of $%.2f, and the next step costs $%.2f. Say the word and I'll continue (raises today's rail by $%.2f).",
		DailyRailQuestionPrefix, journaled.Spend, journaled.Ceiling, rail.Pending, journaled.RaiseAmount())
}

// RaiseDailyRail journals consent to extend today's ceiling.
func (s *Store) RaiseDailyRail(amount float64, origin string) error {
	origin = strings.TrimSpace(origin)
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || origin == "" {
		return fmt.Errorf("raise daily rail: %w: positive amount and origin are required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("raise daily rail: %w", err)
	}
	defer tx.Rollback()
	if _, _, err := appendEvent(tx, "", EventRailRaised, RailAdjustment{Amount: amount, Origin: origin}); err != nil {
		return fmt.Errorf("raise daily rail: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("raise daily rail: %w", err)
	}
	return nil
}

// RaiseDailyRailUnlimited journals consent to remove today's ceiling. The
// configured default remains unchanged and returns at local midnight.
func (s *Store) RaiseDailyRailUnlimited(origin string) error {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return fmt.Errorf("raise daily rail unlimited: %w: origin is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("raise daily rail unlimited: %w", err)
	}
	defer tx.Rollback()
	payload := RailAdjustment{Origin: origin, Unlimited: true}
	if _, _, err := appendEvent(tx, "", EventRailRaised, payload); err != nil {
		return fmt.Errorf("raise daily rail unlimited: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("raise daily rail unlimited: %w", err)
	}
	return nil
}

// PauseDailyRail checks policy immediately before a claim or replan. At the
// rail it atomically posts at most one agent question since the latest raise;
// callers simply stop claiming and try again on their next tick.
func (s *Store) PauseDailyRail(base float64, sessionID string) (DailyRail, bool, error) {
	return s.PauseDailyRailWithAdditionalSpend(base, sessionID, 0)
}

// PauseDailyRailWithAdditionalSpend applies the same durable gate while also
// considering a known cost that has not happened yet. Generation tools use it
// for catalog-priced jobs; zero retains the ordinary pre-call gate path.
func (s *Store) PauseDailyRailWithAdditionalSpend(base float64, sessionID string, additional float64) (DailyRail, bool, error) {
	if additional < 0 || math.IsNaN(additional) || math.IsInf(additional, 0) {
		return DailyRail{}, false, fmt.Errorf("pause daily rail: %w: invalid additional spend", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return DailyRail{}, false, fmt.Errorf("pause daily rail: %w", err)
	}
	defer tx.Rollback()
	now := time.Now()
	rail, err := dailyRailAt(tx, base, now)
	if err != nil {
		return DailyRail{}, false, fmt.Errorf("pause daily rail: %w", err)
	}
	rail = rail.WithAdditionalSpend(additional)
	if !rail.Reached {
		return rail, false, nil
	}
	posted, err := pauseDailyRailTx(tx, rail, sessionID, now)
	if err != nil {
		return DailyRail{}, false, fmt.Errorf("pause daily rail: %w", err)
	}
	if !posted {
		return rail, false, nil
	}
	if err := tx.Commit(); err != nil {
		return DailyRail{}, false, fmt.Errorf("pause daily rail: %w", err)
	}
	return rail, true, nil
}

// pauseDailyRailTx is shared by ordinary claims and charter preflight. A
// charter may mark rail.Reached from projected per-firing spend before the
// recorded spend itself reaches the ceiling.
//
// The suppression is one question per session per raise, not one question in
// total. Global suppression reads as thrift and behaves as silence: with a TUI
// and a `serve` browser both working, whichever session reached the rail first
// got the only question, and the other session's work stopped dead with nothing
// in its thread to explain it and no sentence it could say to consent — the
// head's interception is session-scoped, so a "yes" typed there fell through to
// the ordinary router. Every session that hits the rail is asked in its own
// thread; the raise that any one of them consents to is journaled globally, so
// it lifts the ceiling for all of them at once and makes the other questions
// moot rather than requiring an answer each.
func pauseDailyRailTx(tx *sql.Tx, rail DailyRail, sessionID string, now time.Time) (bool, error) {
	questionSeq, raiseSeq, err := latestRailMarkers(tx, now, sessionID)
	if err != nil {
		return false, err
	}
	if questionSeq > raiseSeq {
		return false, nil
	}
	payload := messagePayload{SessionID: sessionID, Role: RoleAgent, Body: rail.postedQuestion()}
	seq, at, err := appendEvent(tx, "", EventMessagePosted, payload)
	if err != nil {
		return false, err
	}
	if err := applyMessageView(tx, payload, seq, at); err != nil {
		return false, err
	}
	return true, nil
}

// PendingDailyRailApproval reports whether this session's most recent rail
// question still awaits a raise. The head uses it to intercept a plain "yes"
// without spending a model call or inventing a graph command.
//
// It stays session-scoped on purpose. Falling back to another session's
// question would let a bare "yes" meant for something else entirely raise the
// day's ceiling; the pause is what guarantees this session was asked in the
// first place, and a raise by any session clears the rail for everyone, which
// turns an unanswered question into a moot one rather than a stuck one.
func (s *Store) PendingDailyRailApproval(base float64, sessionID string) (DailyRail, bool, error) {
	now := time.Now()
	rail, err := dailyRailAt(s.db, base, now)
	if err != nil {
		return DailyRail{}, false, err
	}
	questionSeq, raiseSeq, err := latestRailMarkers(s.db, now, sessionID)
	if err != nil {
		return DailyRail{}, false, err
	}
	pending := questionSeq > raiseSeq
	if pending {
		rail.Reached = true
	}
	return rail, pending, nil
}

type rowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func dailyRailAt(query rowQuerier, base float64, now time.Time) (DailyRail, error) {
	if base < 0 || math.IsNaN(base) || math.IsInf(base, 0) {
		return DailyRail{}, fmt.Errorf("daily rail: %w: invalid base", ErrInvalid)
	}
	start, end := localDayBounds(now)
	var spend, raised float64
	if err := query.QueryRow(`
		SELECT COALESCE(SUM(cost), 0) FROM usage WHERE ts >= ? AND ts < ?`,
		formatTime(start), formatTime(end)).Scan(&spend); err != nil {
		return DailyRail{}, fmt.Errorf("read spend: %w", err)
	}
	var unlimited int
	if err := query.QueryRow(`
		SELECT COALESCE(SUM(CAST(json_extract(payload, '$.amount') AS REAL)), 0),
		       COALESCE(MAX(CASE WHEN json_extract(payload, '$.unlimited') = 1 THEN 1 ELSE 0 END), 0)
		FROM events WHERE kind = ? AND ts >= ? AND ts < ?`,
		EventRailRaised, formatTime(start), formatTime(end)).Scan(&raised, &unlimited); err != nil {
		return DailyRail{}, fmt.Errorf("read raises: %w", err)
	}
	rail := DailyRail{
		Base: base, Raised: raised, Spend: spend, Ceiling: base + raised,
		Unlimited: base == 0 || unlimited != 0,
	}
	rail.Reached = !rail.Unlimited && rail.Spend >= rail.Ceiling
	return rail, nil
}

// latestRailMarkers pairs the newest rail question with the newest raise, both
// inside today. An empty sessionID asks about the whole day rather than one
// thread: a caller with no session cannot be addressed, so the conservative
// reading — any question anywhere counts as having asked — is the right one for
// it, and no session-addressed lookup is answered by it.
func latestRailMarkers(query rowQuerier, now time.Time, sessionID string) (questionSeq, raiseSeq int64, err error) {
	start, end := localDayBounds(now)
	if err = query.QueryRow(`
		SELECT COALESCE(MAX(seq), 0) FROM events
		WHERE kind = ? AND ts >= ? AND ts < ?`,
		EventRailRaised, formatTime(start), formatTime(end)).Scan(&raiseSeq); err != nil {
		return 0, 0, err
	}
	statement := `SELECT COALESCE(MAX(seq), 0) FROM messages
		WHERE role = ? AND body LIKE ? AND ts >= ? AND ts < ?`
	args := []any{RoleAgent, DailyRailQuestionPrefix + "%", formatTime(start), formatTime(end)}
	if sessionID != "" {
		statement += ` AND session_id = ?`
		args = append(args, sessionID)
	}
	if err = query.QueryRow(statement, args...).Scan(&questionSeq); err != nil {
		return 0, 0, err
	}
	return questionSeq, raiseSeq, nil
}

func localDayBounds(now time.Time) (time.Time, time.Time) {
	local := now.In(time.Local)
	start := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	return start.UTC(), start.AddDate(0, 0, 1).UTC()
}

// TopLevelJobUsage joins every node and usage event to its job root. A job
// remains a job root when a territory moves it one level below the spine.
// Folded history therefore keeps its original node count and measured cost.
func (s *Store) TopLevelJobUsage() (map[string]JobUsage, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE job_roots(id) AS (
			SELECT node.id
			FROM nodes AS node
			LEFT JOIN nodes AS parent ON parent.id = node.parent_id
			WHERE node.grp <> ?
			  AND (node.parent_id = ? OR parent.grp = ?)
		), descendants(job_id, node_id) AS (
			SELECT id, id FROM job_roots
			UNION ALL
			SELECT descendants.job_id, child.id
			FROM descendants
			JOIN nodes AS child ON child.parent_id = descendants.node_id
		), node_counts AS (
			SELECT job_id, COUNT(*) AS node_count
			FROM descendants
			GROUP BY job_id
		), spends AS (
			SELECT descendants.job_id, COUNT(usage.seq) AS runs,
			       COALESCE(SUM(usage.prompt_tokens), 0) AS prompt_tokens,
			       COALESCE(SUM(usage.completion_tokens), 0) AS completion_tokens,
			       COALESCE(SUM(usage.cost), 0) AS cost
			FROM descendants
			LEFT JOIN usage ON usage.node_id = descendants.node_id
			GROUP BY descendants.job_id
		), mispredictions AS (
			SELECT descendants.job_id, COUNT(surprises.seq) AS surprise_samples,
			       COALESCE(SUM(surprises.actual_tokens), 0) AS actual_tokens,
			       COALESCE(SUM(surprises.expected_tokens), 0) AS expected_tokens,
			       COALESCE(AVG(surprises.surprise), 0) AS surprise
			FROM descendants
			LEFT JOIN surprises ON surprises.node_id = descendants.node_id
			GROUP BY descendants.job_id
		)
		SELECT node_counts.job_id, node_counts.node_count, spends.runs,
		       spends.prompt_tokens, spends.completion_tokens, spends.cost,
		       mispredictions.surprise_samples, mispredictions.actual_tokens,
		       mispredictions.expected_tokens, mispredictions.surprise
		FROM node_counts
		JOIN spends ON spends.job_id = node_counts.job_id
		JOIN mispredictions ON mispredictions.job_id = node_counts.job_id`, TerritoryGroup, RootID, TerritoryGroup)
	if err != nil {
		return nil, fmt.Errorf("top-level job usage: %w", err)
	}
	defer rows.Close()

	result := make(map[string]JobUsage)
	for rows.Next() {
		var jobID string
		var usage JobUsage
		var usageSurprise float64
		if err := rows.Scan(&jobID, &usage.NodeCount, &usage.Runs, &usage.PromptTokens,
			&usage.CompletionTokens, &usage.Cost, &usage.SurpriseSamples,
			&usage.SurpriseTokens, &usage.ExpectedTokens, &usageSurprise); err != nil {
			return nil, fmt.Errorf("top-level job usage: %w", err)
		}
		if usage.SurpriseSamples > 0 {
			usage.Surprise = &usageSurprise
		}
		result[jobID] = usage
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("top-level job usage: %w", err)
	}
	return result, nil
}

func applyUsageView(tx *sql.Tx, usage NodeUsage, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO usage (seq, ts, node_id, prompt_tokens, completion_tokens, cost)
		VALUES (?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), usage.NodeID, usage.PromptTokens, usage.CompletionTokens, usage.Cost)
	return err
}

func applySurpriseView(tx *sql.Tx, surprise NodeSurprise, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO surprises (seq, ts, node_id, actual_tokens, expected_tokens, surprise)
		VALUES (?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), surprise.NodeID, surprise.ActualTokens, surprise.ExpectedTokens, surprise.Surprise)
	return err
}
