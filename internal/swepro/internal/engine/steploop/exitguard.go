package steploop

import (
	"context"
	"sync"

	"github.com/Agent-Field/swe-pro-go/internal/plandb"
)

// LeafFailure is run-failure-ledger.ts's stored value.
type LeafFailure struct {
	TaskID      string
	Title       string
	Reason      string
	Bugs        []FailureBug
	RepairHints []string
	Confidence  *string
	Attempt     *float64
	Timestamp   float64
}

// FailureLedger is the run-local evidence/nudge slice used by the exit guard.
type FailureLedger interface {
	RecentFailures(rootTaskID string, limit int) []LeafFailure
	WasFailureNudged(rootTaskID, taskID string) bool
	MarkFailureNudged(rootTaskID, taskID string)
}

// MemoryFailureLedger is run-failure-ledger.ts's bounded process-local store.
type MemoryFailureLedger struct {
	mu      sync.Mutex
	byRoot  map[string][]LeafFailure
	nudged  map[string]map[string]bool
	maxRoot int
}

func NewMemoryFailureLedger() *MemoryFailureLedger {
	return &MemoryFailureLedger{
		byRoot:  map[string][]LeafFailure{},
		nudged:  map[string]map[string]bool{},
		maxRoot: 50,
	}
}

func (l *MemoryFailureLedger) RecordLeafFailure(rootTaskID string, failure LeafFailure) {
	if l == nil || rootTaskID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.byRoot == nil {
		l.byRoot = map[string][]LeafFailure{}
	}
	maxRoot := l.maxRoot
	if maxRoot == 0 {
		maxRoot = 50
	}
	all := append(l.byRoot[rootTaskID], failure)
	if len(all) > maxRoot {
		all = all[len(all)-maxRoot:]
	}
	l.byRoot[rootTaskID] = all
}

func (l *MemoryFailureLedger) RecentFailures(rootTaskID string, limit int) []LeafFailure {
	if l == nil {
		return []LeafFailure{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	all := l.byRoot[rootTaskID]
	if limit < 0 {
		limit = 0
	}
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return append([]LeafFailure(nil), all...)
}

func (l *MemoryFailureLedger) WasFailureNudged(rootTaskID, taskID string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.nudged[rootTaskID][taskID]
}

func (l *MemoryFailureLedger) MarkFailureNudged(rootTaskID, taskID string) {
	if l == nil || rootTaskID == "" || taskID == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.nudged == nil {
		l.nudged = map[string]map[string]bool{}
	}
	if l.nudged[rootTaskID] == nil {
		l.nudged[rootTaskID] = map[string]bool{}
	}
	l.nudged[rootTaskID][taskID] = true
}

func (l *MemoryFailureLedger) ClearLedger(rootTaskID string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	delete(l.byRoot, rootTaskID)
	l.mu.Unlock()
}

// PlanDBExitGuard is findOpenCapExhaustFailures over the native in-memory
// PlanDB snapshot, avoiding the TS subprocess/JSON round trip.
type PlanDBExitGuard struct {
	DB     *plandb.PlanDB
	Ledger FailureLedger
}

func (g PlanDBExitGuard) FindOpenCapExhaustFailures(ctx context.Context, query ExitGuardQuery) ([]OpenFailure, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if g.DB == nil || g.Ledger == nil {
		return []OpenFailure{}, nil
	}
	ledger := g.Ledger.RecentFailures(query.RootTaskID, 50)
	if len(ledger) == 0 {
		return []OpenFailure{}, nil
	}
	snapshot := g.DB.SnapshotState()
	statusByID := make(map[string]plandb.TaskStatus, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		if task != nil {
			statusByID[task.ID] = task.Status
		}
	}
	downstream := map[string][]string{}
	for _, dep := range snapshot.Dependencies {
		if dep.FromTask == "" || dep.ToTask == "" {
			continue
		}
		downstream[dep.FromTask] = append(downstream[dep.FromTask], dep.ToTask)
	}

	// JS Map.set keeps the first insertion position while replacing the value.
	order := []string{}
	latest := map[string]LeafFailure{}
	for _, failure := range ledger {
		if _, exists := latest[failure.TaskID]; !exists {
			order = append(order, failure.TaskID)
		}
		latest[failure.TaskID] = failure
	}

	open := []OpenFailure{}
	for _, taskID := range order {
		failure := latest[taskID]
		if statusByID[taskID] != plandb.StatusFailed || g.Ledger.WasFailureNudged(query.RootTaskID, taskID) {
			continue
		}
		blocked := pendingDescendantCount(taskID, downstream, statusByID)
		if blocked == 0 {
			continue
		}
		open = append(open, OpenFailure{
			TaskID:             failure.TaskID,
			Title:              failure.Title,
			Reason:             failure.Reason,
			Bugs:               append([]FailureBug(nil), failure.Bugs...),
			RepairHints:        append([]string(nil), failure.RepairHints...),
			BlockedDescendants: float64(blocked),
		})
	}
	return open, nil
}

func (g PlanDBExitGuard) MarkFailureNudged(rootTaskID, taskID string) {
	if g.Ledger != nil {
		g.Ledger.MarkFailureNudged(rootTaskID, taskID)
	}
}

func pendingDescendantCount(taskID string, downstream map[string][]string, statuses map[string]plandb.TaskStatus) int {
	stack := append([]string(nil), downstream[taskID]...)
	seen := map[string]bool{}
	count := 0
	for len(stack) > 0 {
		last := len(stack) - 1
		id := stack[last]
		stack = stack[:last]
		if seen[id] {
			continue
		}
		seen[id] = true
		switch statuses[id] {
		case plandb.StatusDone, plandb.StatusCancelled, plandb.StatusFailed:
		default:
			count++
		}
		stack = append(stack, downstream[id]...)
	}
	return count
}
