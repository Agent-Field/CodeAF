package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Receipts are what a tree level costs.
//
// 13.11 filed this as a read gap and named it exactly: "per-worker MONEY:
// TopLevelJobUsage answers per JOB ROOT — what is needed is one read of the
// same shape keyed by node, for one subtree." The data was never missing. Every
// usage row already carries a node id, and RecordUsage has been writing one per
// executed leaf, per judge, per revision, since the journal existed. Nothing
// read them back at anything but a job's outermost edge, so a surface could say
// what a job cost and never what a part of it cost, and a collapsed parent
// could only ever say how many rows it was hiding.
//
// Three laws hold this file's shape.
//
//   - ONE QUERY, SHAPED LIKE [Store.SubtreeNodes]. A tree draws every level at
//     once; a read per row would be a read per row. The recursion down
//     parent_id is the same one SubtreeNodes walks, so a receipt exists for
//     exactly the nodes that read returns, in the same admission order.
//   - FOLDING HIDES NOTHING. 257800f's property, and for its reason: folding is
//     what happens to every job shortly after it settles, so a read that
//     honoured it would go blank on precisely the jobs a reader scrolls back to.
//   - ABSENT IS NOT ZERO (8.2.20). A node with no usage row against it has not
//     been measured, which is a different sentence from "it cost nothing" and
//     renders as a different glyph. Neither a receipt nor a rollup exposes its
//     money as a bare float: [NodeReceipt.Billed] and [SubtreeRollup.Billed]
//     answer the question the renderer has to ask first, exactly as
//     [RoomSpend.Recorded] does one level out.

// NodeReceipt is one node's own line: what it cost, when it ran, and what
// became of it.
//
// COST-SO-FAR IS PARTIAL, AND THIS IS WHERE THAT IS WRITTEN DOWN. A leaf's own
// tool loop is journaled ONCE, on the way out of the run — resident's
// recordSpend bills settled, refused and failed from one place, deliberately,
// because the three endings differ in what happens to the node and not at all
// in what was paid. So a leaf that is still running usually carries no usage row
// of its own and reports Billed false: not zero, not an estimate, absent. What a
// live node CAN carry is money spent against it by somebody else — a judge, a
// sentinel, a revision pass, all of which bill the node they are about through
// pool.WithSpendNode — and the runs of an earlier attempt, which stay on the
// node across a retry. Those are real and they are shown. A running node's
// figure is therefore a floor and never a forecast, and the way to make it a
// live total is to make the executor journal as it goes, not to make this read
// guess.
type NodeReceipt struct {
	NodeID string
	// Parent is the node's own parent, which may point outside the subtree when
	// the receipt is the root's. It is carried because a tree is drawn from it
	// and re-reading the node to learn it would defeat the single query.
	Parent string
	Status Status
	// Folded says this node is history in the active view. It is reported rather
	// than filtered: see the second law above.
	Folded bool

	// Runs is how many usage rows name this node. It is the presence bit —
	// see Billed — and not decoration.
	Runs             int
	Cost             float64
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	// LastBilled is the newest usage row against this node, or the zero time
	// when there is none. It is the journal's own stamp, so two surfaces asking
	// the same question agree about when the money moved.
	LastBilled time.Time

	StartedAt  time.Time
	FinishedAt time.Time
}

// Billed reports whether the journal has a single run to show for this node.
// False means nothing was measured here — drawn as — — while true with a zero
// Cost means a run that genuinely cost nothing, which is $0.00 and a different
// sentence. It is [RoomSpend.Recorded] for one node.
func (receipt NodeReceipt) Billed() bool { return receipt.Runs > 0 }

// Open reports work that started and has not stopped: the node holds a start
// stamp, no finish stamp, and no terminal status. A node that ended without a
// finish stamp — old history, a store written before the column meant anything —
// is NOT open, because a clock that would run forever is worse than no clock.
func (receipt NodeReceipt) Open() bool {
	return !receipt.StartedAt.IsZero() && receipt.FinishedAt.IsZero() && !terminal(receipt.Status)
}

// Elapsed is how long this node has been at it, and whether that can be said at
// all. Work that never started has no clock — absence, not a zero duration —
// and work still running is measured against now, which is why the caller hands
// its own clock in rather than this package reading one.
func (receipt NodeReceipt) Elapsed(now time.Time) (time.Duration, bool) {
	return span(receipt.StartedAt, receipt.FinishedAt, receipt.Open(), now)
}

// StateCounts is a subtree's census, the figure a collapsed parent draws in
// place of the rows it is hiding.
//
// Queued folds Claimed in with Pending on purpose. A claim is a worker picking
// the work up, and 13.11's second decision already settled that the state axis
// may only brighten on work that MOVES: a claimed node has not moved, so a
// reader counting what is running must not be told it has.
type StateCounts struct {
	Queued    int
	Running   int
	Done      int
	Failed    int
	Cancelled int
}

// Total is every node the census covers.
func (counts StateCounts) Total() int {
	return counts.Queued + counts.Running + counts.Done + counts.Failed + counts.Cancelled
}

// Settled is the work that has stopped, however it stopped. It is the
// numerator of the `3/7` a card draws.
func (counts StateCounts) Settled() int {
	return counts.Done + counts.Failed + counts.Cancelled
}

// SubtreeRollup is what one collapsed level says on one line: the money under
// it, the wall it has occupied, and the census of its members.
type SubtreeRollup struct {
	// Root is the node rolled up. It is a member of its own rollup: a parent's
	// own judge calls bill the parent, and a total that dropped them would be
	// smaller than the sum of the rows a reader can expand to see.
	Root  string
	Nodes int

	Runs             int
	Cost             float64
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	LastBilled       time.Time

	// Started is the earliest start anywhere under Root and Settled the latest
	// finish. They are the ENDS OF ONE WALL and not two sums: four workers that
	// each took ten minutes side by side took ten minutes, not forty, and a
	// rollup that added them would tell a reader a parallel job was four times
	// as slow as they watched it be.
	Started time.Time
	Settled time.Time
	// Live says something under Root started and has not stopped, which is what
	// makes the wall open-ended rather than measured.
	Live bool

	States StateCounts
}

// Billed reports whether anything under this root has been measured at all.
func (roll SubtreeRollup) Billed() bool { return roll.Runs > 0 }

// Elapsed is the wall this subtree has occupied: earliest start to latest
// settle while it is done, earliest start to now while anything under it still
// runs, and absent when nothing has started or when what started never recorded
// how it ended.
func (roll SubtreeRollup) Elapsed(now time.Time) (time.Duration, bool) {
	return span(roll.Started, roll.Settled, roll.Live, now)
}

// span is the one clock rule both Elapsed methods obey, written once so a node
// row and the parent above it can never disagree about what a missing stamp
// means. A negative result is clamped: a skewed clock is not a reason to draw a
// job that finished before it began.
func span(start, end time.Time, open bool, now time.Time) (time.Duration, bool) {
	if start.IsZero() {
		return 0, false
	}
	if open {
		if now.Before(start) {
			return 0, true
		}
		return now.Sub(start), true
	}
	if end.IsZero() {
		return 0, false
	}
	if end.Before(start) {
		return 0, true
	}
	return end.Sub(start), true
}

// SubtreeLedger is one subtree's receipts, read once and asked many times.
//
// It exists because a tree is not a list. The read is bounded by the subtree a
// reader ENTERED — the same gesture and the same bound as [Store.SubtreeNodes],
// for the reason 257800f gives — and every level inside it then rolls up from
// what is already in hand. Expanding and collapsing a parent is therefore a walk
// over a slice the window already holds, not a query per disclosure triangle.
type SubtreeLedger struct {
	// Root is the node the ledger was read from, and Receipts are it and every
	// descendant in the same stable admission order as [Store.SubtreeNodes].
	Root     string
	Receipts []NodeReceipt

	index    map[string]int
	children map[string][]string
}

// Receipt is one member's own line. The second return is presence: a node that
// is not in this subtree has no receipt here, which is not the same as a node
// that has spent nothing.
func (ledger SubtreeLedger) Receipt(id string) (NodeReceipt, bool) {
	position, found := ledger.index[id]
	if !found {
		return NodeReceipt{}, false
	}
	return ledger.Receipts[position], true
}

// Rollup aggregates any member and everything beneath it, using only what the
// single read already returned. Asked for the ledger's own root it is the whole
// job; asked for a step it is that step's branch; asked for a leaf it is the
// leaf, which is the same figures its own receipt carries and is deliberately
// not a special case — a row and the collapsed version of that row must agree.
//
// The second return is presence, for the same reason [Receipt]'s is.
func (ledger SubtreeLedger) Rollup(id string) (SubtreeRollup, bool) {
	if _, member := ledger.index[id]; !member {
		return SubtreeRollup{}, false
	}
	roll := SubtreeRollup{Root: id}
	// An explicit stack rather than recursion: the graph's depth is a plan's
	// business and not this reader's, and a materialized view with a bad parent
	// link must not take the window down with it. Every id is visited once
	// because each is pushed by exactly one parent.
	stack := []string{id}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		receipt, found := ledger.Receipt(current)
		if !found {
			continue
		}
		roll.absorb(receipt)
		stack = append(stack, ledger.children[current]...)
	}
	return roll, true
}

// absorb folds one member into the running rollup.
func (roll *SubtreeRollup) absorb(receipt NodeReceipt) {
	roll.Nodes++
	roll.Runs += receipt.Runs
	roll.Cost += receipt.Cost
	roll.PromptTokens += receipt.PromptTokens
	roll.CompletionTokens += receipt.CompletionTokens
	roll.CachedTokens += receipt.CachedTokens
	if receipt.LastBilled.After(roll.LastBilled) {
		roll.LastBilled = receipt.LastBilled
	}
	if !receipt.StartedAt.IsZero() && (roll.Started.IsZero() || receipt.StartedAt.Before(roll.Started)) {
		roll.Started = receipt.StartedAt
	}
	if receipt.FinishedAt.After(roll.Settled) {
		roll.Settled = receipt.FinishedAt
	}
	if receipt.Open() {
		roll.Live = true
	}
	switch receipt.Status {
	case Running:
		roll.States.Running++
	case Done:
		roll.States.Done++
	case Failed:
		roll.States.Failed++
	case Cancelled:
		roll.States.Cancelled++
	default:
		roll.States.Queued++
	}
}

// SubtreeReceipts reads one receipt per node in root's subtree — money, clock
// and state, per node, folded or not — in a single query.
//
// This is the read 13.11 filed as missing. Its shape is [Store.SubtreeNodes]'
// shape on purpose: same recursion, same order, same indifference to folding,
// so a surface can lay the two side by side and know the rows line up.
func (s *Store) SubtreeReceipts(root string) (SubtreeLedger, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return SubtreeLedger{}, fmt.Errorf("subtree receipts: %w: empty root", ErrInvalid)
	}
	rows, err := s.db.Query(subtreeReceiptsQuery, root)
	if err != nil {
		return SubtreeLedger{}, fmt.Errorf("subtree receipts %q: %w", root, err)
	}
	defer rows.Close()

	ledger := SubtreeLedger{
		Root:     root,
		Receipts: make([]NodeReceipt, 0, 8),
		index:    make(map[string]int, 8),
		children: make(map[string][]string, 8),
	}
	for rows.Next() {
		var receipt NodeReceipt
		var parent, started, finished, billed sql.NullString
		if err := rows.Scan(
			&receipt.NodeID, &parent, &receipt.Status, &receipt.Folded,
			&started, &finished,
			&receipt.Runs, &receipt.Cost, &receipt.PromptTokens,
			&receipt.CompletionTokens, &receipt.CachedTokens, &billed,
		); err != nil {
			return SubtreeLedger{}, fmt.Errorf("subtree receipts %q: %w", root, err)
		}
		if parent.Valid {
			receipt.Parent = parent.String
		}
		for _, stamp := range []struct {
			raw   sql.NullString
			field *time.Time
			what  string
		}{
			{started, &receipt.StartedAt, "start"},
			{finished, &receipt.FinishedAt, "finish"},
			{billed, &receipt.LastBilled, "billing"},
		} {
			if !stamp.raw.Valid || stamp.raw.String == "" {
				continue
			}
			at, err := parseTime(stamp.raw.String)
			if err != nil {
				return SubtreeLedger{}, fmt.Errorf("subtree receipts %q: parse node %q %s time: %w",
					root, receipt.NodeID, stamp.what, err)
			}
			*stamp.field = at
		}
		ledger.index[receipt.NodeID] = len(ledger.Receipts)
		ledger.Receipts = append(ledger.Receipts, receipt)
	}
	if err := rows.Err(); err != nil {
		return SubtreeLedger{}, fmt.Errorf("subtree receipts %q: %w", root, err)
	}
	// The child index is built from the rows rather than asked of the database,
	// because the parent link is already on every one of them. The root's own
	// parent is outside the subtree and is deliberately not recorded here: a
	// rollup must never climb out of the tree it was given.
	for _, receipt := range ledger.Receipts {
		if receipt.NodeID == root || receipt.Parent == "" {
			continue
		}
		if _, inside := ledger.index[receipt.Parent]; !inside {
			continue
		}
		ledger.children[receipt.Parent] = append(ledger.children[receipt.Parent], receipt.NodeID)
	}
	return ledger, nil
}

// SubtreeRollup is the one-figure form: what a whole subtree cost, how long it
// took, and what state its members are in, without the caller holding the
// members. It is the read behind a COLLAPSED card, and it is the ledger's own
// rollup of its own root rather than a second query with a second opinion.
//
// The second return is presence. A root the graph has never heard of has no
// rollup — absent — which a caller must not draw as an empty job.
func (s *Store) SubtreeRollup(root string) (SubtreeRollup, bool, error) {
	ledger, err := s.SubtreeReceipts(root)
	if err != nil {
		return SubtreeRollup{}, false, err
	}
	roll, found := ledger.Rollup(ledger.Root)
	return roll, found, nil
}

// subtreeReceiptsQuery is named rather than inlined so the plan test can explain
// the statement the code actually runs.
//
// Both CROSS JOINs are load-bearing, and subtreeSpend's comment is the reason:
// written as plain joins, SQLite reads the whole usage table and builds a
// throwaway index over the subtree instead, which turns a per-room read into a
// scan of every run this machine has ever recorded. CROSS fixes the order —
// subtree outside, usage_node and the node primary key inside — so the cost is
// the subtree's own rows and no more. The spend CTE groups before the join so a
// node with four runs stays one row and cannot multiply its own clock.
const subtreeReceiptsQuery = `
	WITH RECURSIVE descendants(id) AS (
	    SELECT id FROM nodes WHERE id = ?
	    UNION ALL
	    SELECT child.id FROM nodes AS child
	    JOIN descendants ON child.parent_id = descendants.id
	), spend(node_id, runs, cost, prompt_tokens, completion_tokens, cached_tokens, last_billed) AS (
	    SELECT usage.node_id, COUNT(*),
	           COALESCE(SUM(usage.cost), 0),
	           COALESCE(SUM(usage.prompt_tokens), 0),
	           COALESCE(SUM(usage.completion_tokens), 0),
	           COALESCE(SUM(usage.cached_tokens), 0),
	           MAX(usage.ts)
	    FROM descendants CROSS JOIN usage ON usage.node_id = descendants.id
	    GROUP BY usage.node_id
	)
	SELECT node.id, node.parent_id, node.status, node.folded,
	       node.started_at, node.finished_at,
	       COALESCE(spend.runs, 0), COALESCE(spend.cost, 0),
	       COALESCE(spend.prompt_tokens, 0), COALESCE(spend.completion_tokens, 0),
	       COALESCE(spend.cached_tokens, 0), COALESCE(spend.last_billed, '')
	FROM descendants
	CROSS JOIN nodes AS node ON node.id = descendants.id
	LEFT JOIN spend ON spend.node_id = descendants.id
	ORDER BY node.created_seq, node.created_order, node.id`
