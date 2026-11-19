package session

import (
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
)

// ── what a joined hand-off cost is on its own row ───────────────────────────
//
// THE MEASURED FAILURE (connected-canvas qloop run23, vector-db t1). The
// manager proposed two tasks in one message: "Comparison charts" and "Deep
// research". The second joined the run the first had opened, which makes it a
// child of the run's root. The run's whole meter went onto the root's row, so
// "Comparison charts" read $1.43 against $1.61 for the whole scenario. The
// charts' own worker had spent $0.13. The rest was the research: five research
// leaves on the worker seat (about $0.10) and the review round's one check per
// leaf on the careful seat (five checks, $0.82). The research row showed none
// of it. A person reading the card was told the cheap job was the dear one.
//
// THE STORE ALREADY KNOWS WHO SPENT WHAT. Every charge a run's worker or check
// writes names its task ([plandb.Store.SpendBy] on the "task" axis), and the
// review checks sit under the coordinator whose leaves they read
// ([run.Supervisor.addReviewCheck]). So a joined hand-off's row is labelled
// with the charges of its own subtree, and the run's row with what is left.
// These are labels and never a second charge: the conversation's total comes
// from the calls themselves (task_run_money.go), and the parts still add up to
// the run's own figure.

// runSubtreeSpend is every dollar the run's store holds against one task and
// all of its descendants, checks included.
func runSubtreeSpend(store *plandb.Store, id string) float64 {
	if store == nil || id == "" {
		return 0
	}
	byTask := map[string]float64{}
	for _, line := range store.SpendBy("task", time.Time{}) {
		byTask[line.Key] += line.USD
	}
	children := map[string][]string{}
	for _, task := range store.Tasks() {
		if task.ParentID != "" {
			children[task.ParentID] = append(children[task.ParentID], task.ID)
		}
	}
	total := 0.0
	seen := map[string]bool{}
	pending := []string{id}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if seen[current] {
			continue
		}
		seen[current] = true
		total += byTask[current]
		pending = append(pending, children[current]...)
	}
	return total
}

// joinedRowSpend is what one joined hand-off's own subtree spent, read off the
// run's store by the id the store gave that hand-off.
func joinedRowSpend(store *plandb.Store, row uint64) float64 {
	if store == nil {
		return 0
	}
	task := store.Task(strconv.FormatUint(row, 10))
	if task == nil {
		return 0
	}
	return runSubtreeSpend(store, task.ID)
}

// rootRowShare is the run's own row's part of what the run spent: the run's
// figure less what every hand-off that joined it spent under its own row.
//
// IT NEVER READS BELOW WHAT THE ROOT'S OWN WORK CHARGED. The run's figure also
// counts money no task row names (a helper, a charge reconciled late), and a
// joined subtree whose rows outran that figure would otherwise leave the run's
// own row at nothing, which the settled-row fallback would then refill with the
// whole run again ([Agent.beltRunSpent]).
func rootRowShare(store *plandb.Store, total float64, joined []uint64) float64 {
	if store == nil || len(joined) == 0 {
		return total
	}
	away := 0.0
	for _, row := range joined {
		away += joinedRowSpend(store, row)
	}
	if away <= 0 {
		return total
	}
	share := total - away
	if own := rootOwnSpend(store, joined); share < own {
		share = own
	}
	return share
}

// rootOwnSpend is what the store charged to tasks outside every joined
// hand-off's subtree: the run's own root and the work it split into.
func rootOwnSpend(store *plandb.Store, joined []uint64) float64 {
	all := 0.0
	for _, line := range store.SpendBy("task", time.Time{}) {
		all += line.USD
	}
	for _, row := range joined {
		all -= joinedRowSpend(store, row)
	}
	return max(all, 0)
}
