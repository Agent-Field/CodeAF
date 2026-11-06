package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// crewScopeContextKey keeps one run task's ceiling across calls and model changes.
type crewScopeContextKey struct{}

type seatScope struct {
	seat  crewroute.Seat
	scope string
}

// spendReservation keeps the task and ceiling holds separate from the day
// hold returned by before, since each tally can reserve a different estimate.
type spendReservation struct {
	task, ceiling float64
}

// ceilingTally separates a run task's ceiling from the persisted seat aggregate.
// Calls without a scope retain the single-task crew's existing seat tally.
func (g *SpendGuard) ceilingTally(ctx context.Context, seat crewroute.Seat) *SpendTask {
	scope, _ := ctx.Value(crewScopeContextKey{}).(string)
	if scope == "" {
		return g.seatTally(seat)
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.scopedSpent == nil {
		g.scopedSpent = make(map[seatScope]*SpendTask)
	}
	key := seatScope{seat: seat, scope: scope}
	if g.scopedSpent[key] == nil {
		g.scopedSpent[key] = &SpendTask{}
	}
	return g.scopedSpent[key]
}
