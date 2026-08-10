package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

// A task ceiling is the daily rail's arithmetic scoped to one subtree. The day
// is shared by everything running on this machine, so reaching its rail stops
// the whole graph; a task is one root and its descendants, so reaching its rail
// stops only the work that belongs to it and leaves the rest claimable.
//
// The ceiling is durable policy and therefore journaled: the event is the
// record and task_budgets is the projection that makes "is any ceiling above
// this node" answerable in one indexed read. That read is the entire reason the
// projection exists — the check sits in front of every claim, and on a machine
// where nobody has set a ceiling it must cost one probe of an empty table and
// nothing else. Nothing in the product sets one yet; the engine capability
// lands first and the surfaces that set it come later.

const taskBudgetSchema = `
CREATE TABLE IF NOT EXISTS task_budgets (
    root    TEXT PRIMARY KEY,
    ceiling REAL NOT NULL,
    origin  TEXT NOT NULL DEFAULT '',
    seq     INTEGER NOT NULL,
    ts      TEXT NOT NULL
);
`

// TaskCeiling is one journaled decision about what a task may spend.
//
// Cleared says the root is no longer governed at all, which is not a ceiling of
// zero: a zero ceiling would stop the task on its first recorded cent, so the
// two must be different rows in the journal rather than the same number.
type TaskCeiling struct {
	Root    string  `json:"root"`
	Ceiling float64 `json:"ceiling"`
	Origin  string  `json:"origin"`
	Cleared bool    `json:"cleared,omitempty"`
}

// TaskRailAsk is the durable "this task has already been asked" marker.
//
// The daily rail finds its own question by matching the message prefix, which
// works because its marker is the whole day. A per-root marker needs a key, and
// node ids are not safe LIKE patterns, so the ask is journaled against the root
// node instead — one indexed lookup on (node_id, seq) rather than a scan of
// every message ever posted.
type TaskRailAsk struct {
	Root      string  `json:"root"`
	SessionID string  `json:"session_id,omitempty"`
	Spend     float64 `json:"spend"`
	Ceiling   float64 `json:"ceiling"`
}

// TaskRail is one subtree's policy state, in the shape DailyRail already uses:
// Spend is everything the rail decides against and Pending is the part of it
// the journal has not recorded yet. Set separates "no ceiling governs this
// node" from "a ceiling that happens to be small", and only a set rail can be
// Reached — an ungoverned node is never stopped by this rail.
type TaskRail struct {
	Root    string
	Ceiling float64
	Spend   float64
	Pending float64
	Set     bool
	Reached bool
}

// TaskRailQuestionPrefix is stable for the same reason the daily one is: the
// journaled message is also what a later reader matches on to recognize the
// question it is answering.
const TaskRailQuestionPrefix = "Task budget reached -- "

// WithAdditionalSpend includes a known cost that has not happened yet, the way
// the daily rail does for catalog-priced work. An ungoverned rail absorbs
// nothing: there is no ceiling for the addition to be measured against.
func (rail TaskRail) WithAdditionalSpend(amount float64) TaskRail {
	if !rail.Set || amount <= 0 {
		return rail
	}
	rail.Spend += amount
	rail.Pending += amount
	rail.Reached = rail.Spend >= rail.Ceiling
	return rail
}

// RaiseAmount restores one ceiling's worth of headroom plus whatever landed
// past the ceiling while concurrent leaves were finishing, so consent does not
// immediately face the same question again.
//
// It is computed from journaled spend on purpose. Consent arrives later and
// from somewhere else, and it can only raise against the figure the journal
// holds — quoting a total that includes unrecorded cost promises a number
// consent cannot deliver, which is the lesson DailyRail.postedQuestion records.
func (rail TaskRail) RaiseAmount() float64 {
	if !rail.Set || rail.Ceiling <= 0 {
		return 0
	}
	journaled := math.Max(0, rail.Spend-rail.Pending)
	return ceilCents(rail.Ceiling + math.Max(0, journaled-rail.Ceiling))
}

// ceilCents rounds up to the next cent, and settles the sum to a millionth of
// a dollar first. Without that step the arithmetic's own noise buys a whole
// cent: $1.10 over a $1.00 ceiling leaves an overshoot of
// 0.10000000000000009, and rounding that up quotes the user $1.11.
func ceilCents(amount float64) float64 {
	cents := math.Round(amount*100*1e4) / 1e4
	return math.Ceil(cents) / 100
}

// Question is the one user-visible stop a task rail produces. It names the task
// because, unlike the day, there can be several of them; it quotes what the
// journal knows and names any pending item separately rather than folding it
// into the total.
func (rail TaskRail) Question() string {
	journaled := math.Max(0, rail.Spend-rail.Pending)
	if rail.Pending <= 0 {
		return fmt.Sprintf("%s%s has spent $%.2f of $%.2f. Say the word and I'll continue (raises this task's ceiling by $%.2f).",
			TaskRailQuestionPrefix, rail.Root, journaled, rail.Ceiling, rail.RaiseAmount())
	}
	return fmt.Sprintf("%s%s has spent $%.2f of $%.2f, and the next step costs $%.2f. Say the word and I'll continue (raises this task's ceiling by $%.2f).",
		TaskRailQuestionPrefix, rail.Root, journaled, rail.Ceiling, rail.Pending, rail.RaiseAmount())
}

// SetTaskCeiling journals a dollar ceiling over one task's subtree. Setting it
// again replaces the figure, and the replacement re-arms the question: a task
// that was stopped and then given more room is a task that may ask once more.
func (s *Store) SetTaskCeiling(root string, ceiling float64, origin string) error {
	root = strings.TrimSpace(root)
	origin = strings.TrimSpace(origin)
	if root == "" || origin == "" {
		return fmt.Errorf("set task ceiling: %w: root and origin are required", ErrInvalid)
	}
	if ceiling <= 0 || math.IsNaN(ceiling) || math.IsInf(ceiling, 0) {
		return fmt.Errorf("set task ceiling: %w: ceiling must be a positive amount", ErrInvalid)
	}
	return s.journalTaskCeiling(TaskCeiling{Root: root, Ceiling: ceiling, Origin: origin})
}

// RaiseTaskCeiling lifts an existing ceiling by amount. It is the verb the
// question promises, so it exists beside the setter rather than leaving consent
// to reconstruct the new total from a figure that may have moved underneath it.
func (s *Store) RaiseTaskCeiling(root string, amount float64, origin string) error {
	root = strings.TrimSpace(root)
	origin = strings.TrimSpace(origin)
	if root == "" || origin == "" {
		return fmt.Errorf("raise task ceiling: %w: root and origin are required", ErrInvalid)
	}
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return fmt.Errorf("raise task ceiling: %w: positive amount is required", ErrInvalid)
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("raise task ceiling: %w", err)
	}
	defer tx.Rollback()
	var current float64
	if err := tx.QueryRow(`SELECT ceiling FROM task_budgets WHERE root = ?`, root).Scan(&current); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("raise task ceiling: %w: %q has no ceiling", ErrNotFound, root)
		}
		return fmt.Errorf("raise task ceiling: %w", err)
	}
	if err := appendTaskCeilingTx(tx, TaskCeiling{Root: root, Ceiling: current + amount, Origin: origin}); err != nil {
		return fmt.Errorf("raise task ceiling: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("raise task ceiling: %w", err)
	}
	return nil
}

// ClearTaskCeiling returns one task to ungoverned. A root with no ceiling to
// clear is reported rather than journaled: the journal records decisions that
// changed something, and an outer ceiling — if any — resumes governing the
// subtree the moment this one is gone.
func (s *Store) ClearTaskCeiling(root, origin string) error {
	root = strings.TrimSpace(root)
	origin = strings.TrimSpace(origin)
	if root == "" || origin == "" {
		return fmt.Errorf("clear task ceiling: %w: root and origin are required", ErrInvalid)
	}
	if _, set, err := s.TaskCeilingOf(root); err != nil {
		return err
	} else if !set {
		return fmt.Errorf("clear task ceiling: %w: %q has no ceiling", ErrNotFound, root)
	}
	return s.journalTaskCeiling(TaskCeiling{Root: root, Origin: origin, Cleared: true})
}

func (s *Store) journalTaskCeiling(ceiling TaskCeiling) error {
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return fmt.Errorf("journal task ceiling: %w", err)
	}
	defer tx.Rollback()
	if err := requireNode(tx, ceiling.Root); err != nil {
		return fmt.Errorf("journal task ceiling: %w", err)
	}
	if err := appendTaskCeilingTx(tx, ceiling); err != nil {
		return fmt.Errorf("journal task ceiling: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("journal task ceiling: %w", err)
	}
	return nil
}

func appendTaskCeilingTx(tx *sql.Tx, ceiling TaskCeiling) error {
	seq, at, err := appendEvent(tx, ceiling.Root, EventTaskCeilingSet, ceiling)
	if err != nil {
		return err
	}
	return applyTaskCeilingView(tx, ceiling, seq, at)
}

// TaskCeilingOf reports the ceiling set on exactly this root, which is not the
// same question as what governs it: an ancestor's ceiling still applies to a
// node that carries none of its own. TaskRailFor answers that one.
func (s *Store) TaskCeilingOf(root string) (float64, bool, error) {
	var ceiling float64
	err := s.db.QueryRow(`SELECT ceiling FROM task_budgets WHERE root = ?`, root).Scan(&ceiling)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read task ceiling %q: %w", root, err)
	}
	return ceiling, true, nil
}

// SubtreeSpend sums every usage row recorded against root or any node beneath
// it. A run is one row against one node, so a subtree total is a sum of
// distinct rows: what a child spent is never counted a second time for its
// parent, and a node that ran twice contributes both of its runs.
func (s *Store) SubtreeSpend(root string) (float64, error) {
	return subtreeSpend(s.db, root)
}

func subtreeSpend(query rowQuerier, root string) (float64, error) {
	var spend float64
	// One recursive CTE down the parent index, then one indexed lookup per node
	// in it. The CROSS JOIN is the load-bearing word: written as a plain join,
	// SQLite reads the whole usage table and builds a throwaway index over the
	// subtree instead, which turns a claim-time check into a scan of every run
	// this machine has ever recorded. CROSS fixes the order — subtree outside,
	// usage_node inside — so the cost is the subtree's own rows and no more.
	if err := query.QueryRow(`
		WITH RECURSIVE descendants(id) AS (
		    SELECT id FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT child.id FROM nodes AS child
		    JOIN descendants ON child.parent_id = descendants.id
		)
		SELECT COALESCE(SUM(usage.cost), 0)
		FROM descendants CROSS JOIN usage ON usage.node_id = descendants.id`, root).Scan(&spend); err != nil {
		return 0, fmt.Errorf("subtree spend %q: %w", root, err)
	}
	return spend, nil
}

// TaskRailFor resolves the ceiling governing one node — the nearest ancestor
// carrying one, itself included — and measures that root's subtree against it.
// An ungoverned node returns the zero rail, which is Set false and therefore
// never Reached.
func (s *Store) TaskRailFor(nodeID string) (TaskRail, error) {
	if strings.TrimSpace(nodeID) == "" {
		return TaskRail{}, fmt.Errorf("task rail: %w: empty node id", ErrInvalid)
	}
	governed, err := taskCeilingsExist(s.db)
	if err != nil || !governed {
		return TaskRail{}, err
	}
	return taskRailAt(s.db, nodeID)
}

// PauseTaskRail is the gate: it checks one node against the ceiling governing
// it immediately before the work starts, and at the ceiling it atomically posts
// at most one agent question per root per ceiling change. Callers do exactly
// what they do at the daily rail — stop, and try again on their next tick —
// except that the stop is scoped to this subtree.
//
// The first thing it does is ask whether any ceiling exists at all, because the
// answer is no on every machine that has not set one and the whole check must
// cost nothing there.
func (s *Store) PauseTaskRail(nodeID, sessionID string, additional float64) (TaskRail, bool, error) {
	if strings.TrimSpace(nodeID) == "" {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w: empty node id", ErrInvalid)
	}
	if additional < 0 || math.IsNaN(additional) || math.IsInf(additional, 0) {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w: invalid additional spend", ErrInvalid)
	}
	governed, err := taskCeilingsExist(s.db)
	if err != nil {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w", err)
	}
	if !governed {
		return TaskRail{}, false, nil
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w", err)
	}
	defer tx.Rollback()
	rail, err := taskRailAt(tx, nodeID)
	if err != nil {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w", err)
	}
	rail = rail.WithAdditionalSpend(additional)
	if !rail.Reached {
		return rail, false, nil
	}
	posted, err := pauseTaskRailTx(tx, rail, sessionID)
	if err != nil {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w", err)
	}
	if !posted {
		return rail, false, nil
	}
	if err := tx.Commit(); err != nil {
		return TaskRail{}, false, fmt.Errorf("pause task rail: %w", err)
	}
	return rail, true, nil
}

// taskCeilingsExist is the short-circuit that keeps the ungoverned path free.
// task_budgets holds one row per governed task and no rows at all until
// somebody sets a ceiling, so this is a single probe rather than a graph walk.
func taskCeilingsExist(query rowQuerier) (bool, error) {
	var any int
	if err := query.QueryRow(`SELECT EXISTS(SELECT 1 FROM task_budgets)`).Scan(&any); err != nil {
		return false, fmt.Errorf("read task ceilings: %w", err)
	}
	return any != 0, nil
}

// taskRailAt walks the node's ancestry to the nearest governed root and
// measures that root's subtree. The walk is one recursive CTE resolved by
// primary key at each hop, and a cleared root is simply not in task_budgets,
// so an outer ceiling takes over without any second pass.
func taskRailAt(query rowQuerier, nodeID string) (TaskRail, error) {
	var (
		root    string
		ceiling float64
	)
	err := query.QueryRow(`
		WITH RECURSIVE ancestry(id, parent_id, depth) AS (
		    SELECT id, parent_id, 0 FROM nodes WHERE id = ?
		    UNION ALL
		    SELECT parent.id, parent.parent_id, ancestry.depth + 1
		    FROM nodes AS parent
		    JOIN ancestry ON parent.id = ancestry.parent_id
		)
		SELECT task_budgets.root, task_budgets.ceiling
		FROM ancestry
		JOIN task_budgets ON task_budgets.root = ancestry.id
		ORDER BY ancestry.depth LIMIT 1`, nodeID).Scan(&root, &ceiling)
	if errors.Is(err, sql.ErrNoRows) {
		return TaskRail{}, nil
	}
	if err != nil {
		return TaskRail{}, fmt.Errorf("resolve task ceiling for %q: %w", nodeID, err)
	}
	spend, err := subtreeSpend(query, root)
	if err != nil {
		return TaskRail{}, err
	}
	rail := TaskRail{Root: root, Ceiling: ceiling, Spend: spend, Set: true}
	rail.Reached = rail.Spend >= rail.Ceiling
	return rail, nil
}

// pauseTaskRailTx posts the durable question once per ceiling. The suppression
// is per root rather than per session because a task has one owner thread,
// while the day the other rail governs is shared by all of them — so the
// question that stops a task belongs in the one place the task is being
// watched, and asking again only makes sense once the ceiling has moved.
func pauseTaskRailTx(tx *sql.Tx, rail TaskRail, sessionID string) (bool, error) {
	askSeq, ceilingSeq, err := latestTaskRailMarkers(tx, rail.Root)
	if err != nil {
		return false, err
	}
	if askSeq > ceilingSeq {
		return false, nil
	}
	payload := messagePayload{SessionID: sessionID, Role: RoleAgent, Body: rail.Question()}
	seq, at, err := appendEvent(tx, "", EventMessagePosted, payload)
	if err != nil {
		return false, err
	}
	if err := applyMessageView(tx, payload, seq, at); err != nil {
		return false, err
	}
	ask := TaskRailAsk{Root: rail.Root, SessionID: sessionID, Spend: rail.Spend, Ceiling: rail.Ceiling}
	if _, _, err := appendEvent(tx, rail.Root, EventTaskRailAsked, ask); err != nil {
		return false, err
	}
	return true, nil
}

// latestTaskRailMarkers pairs the newest ask for one root with the sequence of
// the ceiling currently in force. Both are indexed lookups: the ask by
// (node_id, seq), the ceiling by primary key.
func latestTaskRailMarkers(query rowQuerier, root string) (askSeq, ceilingSeq int64, err error) {
	if err = query.QueryRow(`
		SELECT COALESCE(MAX(seq), 0) FROM events
		WHERE node_id = ? AND kind = ?`, root, EventTaskRailAsked).Scan(&askSeq); err != nil {
		return 0, 0, err
	}
	err = query.QueryRow(`SELECT seq FROM task_budgets WHERE root = ?`, root).Scan(&ceilingSeq)
	if errors.Is(err, sql.ErrNoRows) {
		return askSeq, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	return askSeq, ceilingSeq, nil
}

func applyTaskCeilingView(tx *sql.Tx, ceiling TaskCeiling, seq int64, at time.Time) error {
	if ceiling.Cleared {
		_, err := tx.Exec(`DELETE FROM task_budgets WHERE root = ?`, ceiling.Root)
		return err
	}
	_, err := tx.Exec(`
		INSERT INTO task_budgets (root, ceiling, origin, seq, ts) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(root) DO UPDATE SET
		    ceiling = excluded.ceiling, origin = excluded.origin,
		    seq = excluded.seq, ts = excluded.ts`,
		ceiling.Root, ceiling.Ceiling, strings.TrimSpace(ceiling.Origin), seq, formatTime(at))
	return err
}
