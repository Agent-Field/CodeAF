package store

import (
	"fmt"
	"time"
)

// Every retrieval this store offers is either topical or sequential. Recall is
// BM25 over folds, SearchSurgeryTargets is BM25 over the active snapshot, and
// Events walks a journal by sequence. None of them can answer "what did you do
// yesterday", because that question carries no search terms and no sequence —
// it carries a window of wall-clock time, and until this read there was nowhere
// in the store to put one. Every time-bounded query here was hard-wired to
// today (localDayBounds) except SelfReceipts(since), which took an arbitrary
// window and covered only the resident's own upkeep. The user's own work
// deserved the same signature, so this is that signature.
//
// It reads the nodes table directly rather than through ActiveNodes, and that
// is the point rather than an oversight. ActiveNodes collapses a territory into
// its one representative row, so a job packed into last month's territory is
// absent from every snapshot-derived read in the product — the head could not
// see Tuesday's work at all once the retrospective had tidied it away. A job
// that finished is a fact about a moment, and the moment does not stop being
// true when the graph compacts around it.

// SettledHistoryCap is the most rows one history read returns. It is the
// board's own cap doubled: a window is allowed to be longer than a board
// because it is read once rather than resent every turn, and past this it is a
// log rather than an answer.
const SettledHistoryCap = 24

// SettledHistory returns the job roots that finished inside [since, until],
// newest first.
//
// A job root is what a person means by "a job": something spliced under the
// permanent root, or a job that a territory has since packed away — the second
// clause is what keeps packed history reachable. Territory furniture, the
// permanent root and the resident's own practice are not jobs and never appear.
//
// A zero since or until is unbounded on that side, so one call spells "since
// yesterday", "until Friday", and "everything" without a second entry point.
// limit <= 0 takes SettledHistoryCap; anything larger is clamped to it.
func (s *Store) SettledHistory(since, until time.Time, limit int) ([]Node, error) {
	if limit <= 0 || limit > SettledHistoryCap {
		limit = SettledHistoryCap
	}
	where := `
		WHERE finished_at IS NOT NULL AND finished_at != ''
		  AND status IN (?, ?, ?)
		  AND id != ?
		  AND grp NOT IN (?, ?)
		  AND (
		      parent_id = ?
		      OR EXISTS (
		          SELECT 1 FROM nodes AS packer
		          WHERE packer.id = nodes.parent_id AND packer.grp = ?
		      )
		  )`
	args := []any{Done, Failed, Cancelled, RootID, TerritoryGroup, PracticeGroup, RootID, TerritoryGroup}
	if !since.IsZero() {
		where += ` AND finished_at >= ?`
		args = append(args, formatTime(since))
	}
	if !until.IsZero() {
		where += ` AND finished_at <= ?`
		args = append(args, formatTime(until))
	}
	nodes, err := s.queryNodesLimitOrdered(where, args, limit, `finished_at DESC, created_seq DESC`)
	if err != nil {
		return nil, fmt.Errorf("settled history: %w", err)
	}
	return nodes, nil
}
