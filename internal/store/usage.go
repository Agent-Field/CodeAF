package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

// Usage accounting lives in the same journal as the work it measures. One
// event per executed node, materialized into a running total, so any lens can
// answer "what has this graph cost" without replaying history.

// NodeUsage is what one node's execution spent.
//
// Model is the model that actually served the call, which is not the model
// anybody asked for: an escalation moves a leaf to a stronger rung, a cascade
// picks by class, and the only durable record of who did the work used to be
// Provenance.WorkModel — the pin the user requested, empty on almost every job.
// So "which model wrote this?" was unanswerable from every surface. It is
// recorded here rather than on the node because a node can run more than once
// and each run has its own answer, and because the row that carries the money
// is the row that should carry the name.
//
// CachedTokens is the part of PromptTokens the provider billed at the cached
// rate. It was computed in three places and persisted in none, which is not a
// missing nicety: it is the reason a whole class of defect went unnoticed. The
// harness spends real effort on cache shape — a byte-stable prefix, decay that
// fires in batches so most turns leave the transcript untouched, a tool block
// that never moves — and a run key that was never set produces exactly the same
// journal as a run whose prefix was perfect. The number that separates them is
// this one, and until it was written down nobody could tell the two apart.
type NodeUsage struct {
	NodeID           string  `json:"node_id"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CachedTokens     int     `json:"cached_tokens,omitempty"`
	Cost             float64 `json:"cost"`
	Model            string  `json:"model,omitempty"`
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
    cached_tokens     INTEGER NOT NULL DEFAULT 0,
    cost              REAL NOT NULL DEFAULT 0,
    model             TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS usage_node ON usage (node_id);
CREATE INDEX IF NOT EXISTS usage_ts ON usage (ts);
`

// migrateUsageSchema adds the served model and the cached share of the prompt to
// an existing journal. Rows written before either existed keep the column
// default — the empty string, which reads as "not recorded" rather than as a
// model named "", and zero, which reads the same way for a number nobody was
// keeping. That is the distinction every other backfilled column here draws.
// Replay needs nothing: the event payload is JSON and an absent field decodes
// to exactly the value the column defaults to.
func migrateUsageSchema(db *sql.DB) error {
	hasModel, err := tableHasColumn(db, "usage", "model")
	if err != nil {
		return err
	}
	if !hasModel {
		if _, err := db.Exec(`ALTER TABLE usage ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	hasCached, err := tableHasColumn(db, "usage", "cached_tokens")
	if err != nil {
		return err
	}
	if !hasCached {
		if _, err := db.Exec(`ALTER TABLE usage ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	_, err = db.Exec(`CREATE INDEX IF NOT EXISTS usage_ts ON usage (ts)`)
	return err
}

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

// SpendWindow is what one arbitrary stretch of time cost. It is the shape
// every "what did I spend this week" question wants and the shape no query in
// this package could answer: localDayBounds was the only bucketing helper
// anywhere, so every user-facing number was either today or all time.
//
// SelfReceipts(since) had already proved the signature — for the resident's own
// practice, and unreachable from any user surface. The user's jobs get the same
// courtesy here.
type SpendWindow struct {
	Since            time.Time
	Until            time.Time
	Runs             int
	PromptTokens     int
	CompletionTokens int
	Cost             float64
}

// JobSpend is one job root's share of a window, named well enough to read
// aloud: what it was for, what it cost, and when it last spent anything.
type JobSpend struct {
	JobID  string
	Title  string
	Runs   int
	Cost   float64
	Last   time.Time
	Models []string
}

// SpendBetween sums recorded cost over an arbitrary window. A zero Until means
// now; a zero Since means the beginning of the journal. Both bounds are
// half-open the way every other range read here is: [since, until).
func (s *Store) SpendBetween(since, until time.Time) (SpendWindow, error) {
	since, until = spendBounds(since, until)
	window := SpendWindow{Since: since, Until: until}
	err := s.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(prompt_tokens), 0),
		       COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(cost), 0)
		FROM usage WHERE ts >= ? AND ts < ?`,
		formatTime(since), formatTime(until)).Scan(&window.Runs, &window.PromptTokens,
		&window.CompletionTokens, &window.Cost)
	if err != nil {
		return SpendWindow{}, fmt.Errorf("spend between: %w", err)
	}
	return window, nil
}

// SpendByJob groups a window's cost under the job root each run belongs to,
// heaviest first. Grouping needs no new taxonomy: the graph already knows which
// root a node descends from, and that root's title is what the user called the
// work. A limit of zero returns every job that spent anything.
//
// A run under no job root at all — planning charged to the spine, a voice
// transcription — is deliberately absent rather than bucketed into a fake job.
// The window total from SpendBetween is the authority on the whole bill, and
// the difference between it and the sum of these rows is exactly the overhead
// that belongs to no single errand.
func (s *Store) SpendByJob(since, until time.Time, limit int) ([]JobSpend, error) {
	since, until = spendBounds(since, until)
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
		)
		SELECT descendants.job_id, COUNT(usage.seq), COALESCE(SUM(usage.cost), 0),
		       COALESCE(MAX(usage.ts), ''),
		       COALESCE(GROUP_CONCAT(DISTINCT usage.model), '')
		FROM descendants
		JOIN usage ON usage.node_id = descendants.node_id
		WHERE usage.ts >= ? AND usage.ts < ?
		GROUP BY descendants.job_id
		ORDER BY SUM(usage.cost) DESC, descendants.job_id`,
		TerritoryGroup, RootID, TerritoryGroup, formatTime(since), formatTime(until))
	if err != nil {
		return nil, fmt.Errorf("spend by job: %w", err)
	}
	defer rows.Close()

	spends := make([]JobSpend, 0)
	for rows.Next() {
		var spend JobSpend
		var last, models string
		if err := rows.Scan(&spend.JobID, &spend.Runs, &spend.Cost, &last, &models); err != nil {
			return nil, fmt.Errorf("spend by job: %w", err)
		}
		if last != "" {
			if at, err := parseTime(last); err == nil {
				spend.Last = at
			}
		}
		for _, model := range strings.Split(models, ",") {
			if model = strings.TrimSpace(model); model != "" {
				spend.Models = append(spend.Models, model)
			}
		}
		sort.Strings(spend.Models)
		spends = append(spends, spend)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spend by job: %w", err)
	}
	if limit > 0 && len(spends) > limit {
		spends = spends[:limit]
	}
	for index := range spends {
		if node, found, err := s.Node(spends[index].JobID); err == nil && found {
			spends[index].Title = strings.TrimSpace(node.Title)
			if spends[index].Title == "" {
				spends[index].Title = strings.TrimSpace(strings.SplitN(node.Brief, "\n", 2)[0])
			}
		}
	}
	return spends, nil
}

// SpendSlice is one class of runs inside a window: how many there were, what
// they cost, and the tokens they moved. It is a slice of a bill rather than a
// bill, because a room's bill has two parts the journal knows differently well
// and reporting them as one number would be reporting the weaker half's
// certainty as the stronger half's.
type SpendSlice struct {
	Runs             int
	Cost             float64
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
}

func (slice *SpendSlice) add(other SpendSlice) {
	slice.Runs += other.Runs
	slice.Cost += other.Cost
	slice.PromptTokens += other.PromptTokens
	slice.CompletionTokens += other.CompletionTokens
	slice.CachedTokens += other.CachedTokens
}

// RoomSpend is what one room of conversation has cost, split by how well the
// journal can say so.
//
// "A usage row carries a node and a time, never a session" was true when it was
// written and is now true of exactly half the bill:
//
//   - Work commissioned FROM a room carries the room on its node. The splice
//     stamps Provenance.SessionID on every node it admits, and every repair,
//     revision and retry underneath copies it forward, so summing usage rows
//     whose node names this room is exact — no window, no guess, no double
//     counting.
//   - The head's OWN calls — routing a message, compiling an instruction,
//     writing the reply — bill the spine, which belongs to no room at all
//     (pool.SpendNode defaults to RootID for precisely that reason). Nothing
//     in the row says which room was being answered. Only the window says it,
//     and only while one room is talking.
//
// So this carries two figures and a warning rather than one number. Work is
// this room's, exactly. Spine is what conversation cost inside the window,
// which is this room's alone only when this room was the only one live —
// Shared says it was not, and a renderer that quotes Spine anyway is quoting
// an upper bound.
type RoomSpend struct {
	SessionID string
	// SinceSeq and Since are the journal row the window opens at: the room's
	// first message for a whole-room read, the room's newest user message for
	// a turn. Zero means there was nothing to open at.
	SinceSeq int64
	Since    time.Time
	// Last is the newest usage row inside the window, or the zero time when
	// there is none. It is the journal's own timestamp rather than a clock
	// read here, so two lenses asking the same question agree.
	Last time.Time

	Work  SpendSlice
	Spine SpendSlice

	// Shared says another room was live inside this window — it spent against
	// its own nodes, or it was spoken in. Spine is then a ceiling on this
	// room's conversational cost and not its bill.
	Shared bool

	// SpinePromptHighWater is the largest prompt_tokens on any single spine row
	// inside the window. It is named for what it measures rather than for what
	// it is wanted for, because those are not quite the same thing and the gap
	// is the whole of what 5.9 still owes.
	//
	// What it is wanted for: the head's context occupancy, the numerator of
	// ctx%. Most spine rows are written one per provider call by
	// pool.recordStructuringSpend, so such a row's prompt_tokens IS that call's
	// whole context — and the answering call's prompt dominates the routing and
	// compiling calls beside it, which see one instruction rather than the
	// thread. For a turn that was pure conversation, this is the head's window,
	// exactly.
	//
	// Where it stops being exact: three call sites journal ONE spine row for
	// MANY calls, summing their prompts — the planner's passes
	// (journalPlanSpend) and headless preparation and run totals. A summed
	// prompt is not a context occupancy, so a window containing one of those
	// makes this an upper bound. That is the safe direction for a health signal
	// — a context gauge that errs toward alarm sends a person to look, while
	// one that errs toward calm is why nobody could answer "why did it get
	// dumber" — but it is an error and it is written down here rather than
	// dressed up at the seam.
	//
	// It is deliberately measured over Spine alone. A leaf's row is a whole
	// tool loop summed by construction, so the same maximum over Work rows
	// would be a number with no referent at all.
	//
	// It is NOT 5.9's durable context figure and must never be labelled as one.
	// That one needs executors to journal window-size high-water marks — the
	// window a call actually occupied, per call, as its own fact — which no
	// executor does yet. Until they do, this is what the journal can say, it
	// can only say it about the conversation, and it says nothing at all when
	// Shared is true.
	SpinePromptHighWater int
}

// Recorded reports whether the journal has a single run to show for the
// window. It is the distinction a bare float64 cannot make and the one the
// renderer's missing-data law (8.2.20) turns on: false means nothing has been
// billed here — absence, drawn as — — while true with a zero Cost means runs
// that genuinely cost nothing, which is $0.00 and a different sentence.
func (spend RoomSpend) Recorded() bool { return spend.Work.Runs+spend.Spine.Runs > 0 }

// Runs is every run the window counted, room work and conversation together.
func (spend RoomSpend) Runs() int { return spend.Work.Runs + spend.Spine.Runs }

// Cost is the whole window's measured money. It folds the ambiguous half in,
// so a caller that shows it while Shared is true is showing a ceiling; a
// caller that wants only what it can defend shows Work.Cost.
func (spend RoomSpend) Cost() float64 { return spend.Work.Cost + spend.Spine.Cost }

// SessionSpend is one room's whole bill, from its first message to now.
//
// The second return is presence, not emptiness: false means this room has
// never been spoken in, so there is no window and nothing to say. A room that
// exists and has billed nothing returns true with Recorded false, which is a
// different fact and renders as a different glyph.
func (s *Store) SessionSpend(sessionID string) (RoomSpend, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return RoomSpend{}, false, nil
	}
	seq, at, found, err := firstMessageOf(s.db, sessionID)
	if err != nil {
		return RoomSpend{}, false, fmt.Errorf("session spend %q: %w", sessionID, err)
	}
	if !found {
		return RoomSpend{}, false, nil
	}
	spend, err := roomSpendSince(s.db, sessionID, seq)
	if err != nil {
		return RoomSpend{}, false, fmt.Errorf("session spend %q: %w", sessionID, err)
	}
	spend.Since = at
	return spend, true, nil
}

// TurnSpend is what the room's newest turn has cost so far: the window that
// opens at the room's newest user message and runs to the end of the journal.
//
// A turn is bounded by the message that asked for it because that is the only
// boundary the journal draws for one. There is no turn row and no turn id —
// the head answers a message, spends against the spine while it does, and the
// work it commissions bills its own nodes. The user's last word is where all
// three start.
//
// The second return is false when the room holds no user message: nothing has
// been asked, so no turn exists to cost. That is absence and not zero.
func (s *Store) TurnSpend(sessionID string) (RoomSpend, bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return RoomSpend{}, false, nil
	}
	seq, at, found, err := latestUserMessageOf(s.db, sessionID)
	if err != nil {
		return RoomSpend{}, false, fmt.Errorf("turn spend %q: %w", sessionID, err)
	}
	if !found {
		return RoomSpend{}, false, nil
	}
	spend, err := roomSpendSince(s.db, sessionID, seq)
	if err != nil {
		return RoomSpend{}, false, fmt.Errorf("turn spend %q: %w", sessionID, err)
	}
	spend.Since = at
	return spend, true, nil
}

// firstMessageOf and latestUserMessageOf open the two windows above. Both read
// one row off messages_session_seq / messages_session_role_seq, which carry
// every column they touch, so opening a window is an index seek rather than a
// scan of the room.
func firstMessageOf(query rowQuerier, sessionID string) (int64, time.Time, bool, error) {
	return messageBound(query, `
		SELECT seq, ts FROM messages WHERE session_id = ?
		ORDER BY seq LIMIT 1`, sessionID)
}

func latestUserMessageOf(query rowQuerier, sessionID string) (int64, time.Time, bool, error) {
	return messageBound(query, `
		SELECT seq, ts FROM messages WHERE session_id = ? AND role = ?
		ORDER BY seq DESC LIMIT 1`, sessionID, string(RoleUser))
}

func messageBound(query rowQuerier, statement string, args ...any) (int64, time.Time, bool, error) {
	var seq int64
	var stamp string
	err := query.QueryRow(statement, args...).Scan(&seq, &stamp)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, time.Time{}, false, nil
	}
	if err != nil {
		return 0, time.Time{}, false, err
	}
	at, parseErr := parseTime(stamp)
	if parseErr != nil {
		at = time.Time{}
	}
	return seq, at, true, nil
}

// roomSpendSince is the one query both reads above are made of.
//
// Shape notes, against 12.1.6's perf ledger. The outer table is usage under a
// range constraint on its own primary key (`seq` IS the rowid), so the scan is
// the tail of the journal from the window's first row and never the whole
// table; nodes is entered by primary key, once per row in that tail, through a
// LEFT JOIN, which additionally fixes the join order the way the subtree
// query's CROSS JOIN has to fix it by hand. No index is added for this: the
// two it uses already exist, and an index that measured nothing is a write
// tax on every executed node.
func roomSpendSince(query rowQuerier, sessionID string, sinceSeq int64) (RoomSpend, error) {
	spend := RoomSpend{SessionID: sessionID, SinceSeq: sinceSeq}
	args := []any{sessionID, sessionID, sessionID, sessionID, sessionID, sessionID, sinceSeq}
	var elsewhere int
	var last string
	if err := query.QueryRow(roomSpendQuery, args...).Scan(
		&spend.Work.Runs, &spend.Work.Cost, &spend.Work.PromptTokens,
		&spend.Work.CompletionTokens, &spend.Work.CachedTokens,
		&spend.Spine.Runs, &spend.Spine.Cost, &spend.Spine.PromptTokens,
		&spend.Spine.CompletionTokens, &spend.Spine.CachedTokens,
		&spend.SpinePromptHighWater, &elsewhere, &last,
	); err != nil {
		return RoomSpend{}, err
	}
	if last != "" {
		if at, err := parseTime(last); err == nil {
			spend.Last = at
		}
	}
	spend.Shared = elsewhere != 0
	if !spend.Shared {
		// A room that spent nothing can still have been talked over: the other
		// room's own conversation bills the spine under no name at all, so the
		// only trace of it inside this window is that somebody else was
		// speaking. One primary-key range read over the same tail answers it.
		spoken, err := otherRoomsSpokeSince(query, sessionID, sinceSeq)
		if err != nil {
			return RoomSpend{}, err
		}
		spend.Shared = spoken
	}
	if spend.Shared {
		// The head-window figure is the one number here that cannot be a
		// ceiling and still mean anything: half a context is not a context.
		// Two rooms in the window means the largest prompt may be the other
		// room's, so this room says nothing rather than something borrowed.
		spend.SpinePromptHighWater = 0
	}
	return spend, nil
}

// roomSpendQuery separates the room's own rows from the spine's with one CASE
// in one pass rather than with two statements, because two statements over the
// same tail read it twice and can straddle a write. It is named rather than
// inlined so the plan test can explain the statement the code actually runs.
const roomSpendQuery = `
		SELECT
		    COALESCE(SUM(CASE WHEN nodes.session_id = ? THEN 1 ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN nodes.session_id = ? THEN usage.cost ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN nodes.session_id = ? THEN usage.prompt_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN nodes.session_id = ? THEN usage.completion_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN nodes.session_id = ? THEN usage.cached_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN COALESCE(nodes.session_id, '') = '' THEN 1 ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN COALESCE(nodes.session_id, '') = '' THEN usage.cost ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN COALESCE(nodes.session_id, '') = '' THEN usage.prompt_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN COALESCE(nodes.session_id, '') = '' THEN usage.completion_tokens ELSE 0 END), 0),
		    COALESCE(SUM(CASE WHEN COALESCE(nodes.session_id, '') = '' THEN usage.cached_tokens ELSE 0 END), 0),
		    COALESCE(MAX(CASE WHEN COALESCE(nodes.session_id, '') = '' THEN usage.prompt_tokens ELSE 0 END), 0),
		    COALESCE(MAX(CASE WHEN COALESCE(nodes.session_id, '') NOT IN ('', ?) THEN 1 ELSE 0 END), 0),
		    COALESCE(MAX(usage.ts), '')
		FROM usage LEFT JOIN nodes ON nodes.id = usage.node_id
		WHERE usage.seq >= ?`

// otherRoomsSpokeSince reports whether any room but this one was spoken in
// after seq. Messages are keyed by seq, so this is the tail of one index.
func otherRoomsSpokeSince(query rowQuerier, sessionID string, sinceSeq int64) (bool, error) {
	var spoke int
	if err := query.QueryRow(`
		SELECT EXISTS(
		    SELECT 1 FROM messages
		    WHERE seq >= ? AND session_id <> '' AND session_id <> ?
		)`, sinceSeq, sessionID).Scan(&spoke); err != nil {
		return false, err
	}
	return spoke != 0, nil
}

// NodeModels names every model that served one node's subtree, most expensive
// first. This is the read behind "which model produced this?" — a question that
// had no answer on any surface until the usage row started carrying the name.
func (s *Store) NodeModels(id string) ([]string, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE descendants(id) AS (
			SELECT id FROM nodes WHERE id = ?
			UNION ALL
			SELECT child.id FROM nodes AS child
			JOIN descendants ON child.parent_id = descendants.id
		)
		SELECT usage.model
		FROM usage JOIN descendants ON descendants.id = usage.node_id
		WHERE usage.model <> ''
		GROUP BY usage.model
		ORDER BY SUM(usage.cost) DESC, usage.model`, id)
	if err != nil {
		return nil, fmt.Errorf("node models for %q: %w", id, err)
	}
	defer rows.Close()
	models := make([]string, 0)
	for rows.Next() {
		var model string
		if err := rows.Scan(&model); err != nil {
			return nil, fmt.Errorf("node models for %q: %w", id, err)
		}
		models = append(models, model)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("node models for %q: %w", id, err)
	}
	return models, nil
}

// MeasuredCostPerRun is what one executed node has actually cost on this
// machine, taken as the median of every priced run so one runaway job cannot
// set the price of the next one. Zero means nothing has been measured yet, and
// a caller with no measurement must say nothing rather than guess.
func (s *Store) MeasuredCostPerRun() (float64, error) {
	rows, err := s.db.Query(`SELECT cost FROM usage WHERE cost > 0 ORDER BY cost`)
	if err != nil {
		return 0, fmt.Errorf("measured cost per run: %w", err)
	}
	defer rows.Close()
	var costs []float64
	for rows.Next() {
		var cost float64
		if err := rows.Scan(&cost); err != nil {
			return 0, fmt.Errorf("measured cost per run: %w", err)
		}
		costs = append(costs, cost)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("measured cost per run: %w", err)
	}
	if len(costs) == 0 {
		return 0, nil
	}
	return costs[len(costs)/2], nil
}

func spendBounds(since, until time.Time) (time.Time, time.Time) {
	if until.IsZero() {
		until = time.Now()
	}
	if since.After(until) {
		since = until
	}
	return since.UTC(), until.UTC()
}

func applyUsageView(tx *sql.Tx, usage NodeUsage, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO usage (seq, ts, node_id, prompt_tokens, completion_tokens, cached_tokens, cost, model)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), usage.NodeID, usage.PromptTokens, usage.CompletionTokens,
		usage.CachedTokens, usage.Cost, strings.TrimSpace(usage.Model))
	return err
}

func applySurpriseView(tx *sql.Tx, surprise NodeSurprise, seq int64, at time.Time) error {
	_, err := tx.Exec(`
		INSERT INTO surprises (seq, ts, node_id, actual_tokens, expected_tokens, surprise)
		VALUES (?, ?, ?, ?, ?, ?)`,
		seq, formatTime(at), surprise.NodeID, surprise.ActualTokens, surprise.ExpectedTokens, surprise.Surprise)
	return err
}
