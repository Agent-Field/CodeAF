//go:build !windows

package app

// Budget queries for the solo loop. The run's ceilings are --max-cost and
// --max-hours.

// budgetIsExhausted is the boolean-only form of budgetExhausted, for callers
// that do not need the reason string.
func (runner *pipeline) budgetIsExhausted() bool {
	exhausted, _ := runner.budgetExhausted()
	return exhausted
}
