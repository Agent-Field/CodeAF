// Package stucksilence ports src/session/stuck-silence.ts:1-20 from swe-pro
// commit 3b25a1a.
package stucksilence

const (
	StuckSilenceMS          = 90_000
	StuckIterationThreshold = 2
	StuckHardCap            = StuckIterationThreshold * 5
)

// StuckSilenceInput contains the pure dispatch-loop liveness counters.
type StuckSilenceInput struct {
	NowMS               float64  `json:"nowMs"`
	StartedAtMS         float64  `json:"startedAtMs"`
	LastToolEventMS     *float64 `json:"lastToolEventMs"`
	UnchangedIterations float64  `json:"unchangedIterations"`
}

// IsLeafStuck reports whether silence or the unchanged-iteration cap fired.
func IsLeafStuck(input StuckSilenceInput) bool {
	lastActivity := input.StartedAtMS
	if input.LastToolEventMS != nil {
		lastActivity = *input.LastToolEventMS
	}
	silent := input.NowMS-lastActivity >= StuckSilenceMS
	hardCap := input.UnchangedIterations >= StuckHardCap
	return silent || hardCap
}
