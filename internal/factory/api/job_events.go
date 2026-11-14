package api

import "github.com/Agent-Field/codeaf/internal/factory/live"

// JobEvent is one durable finished, merged, failed or needs-you job outcome.
type JobEvent = live.Outcome

// JobEvents answers a bounded read from the factory's outcome cursor.
type JobEvents struct {
	// Events contains visible outcomes in sequence order, and is never null.
	Events []JobEvent `json:"events"`
	// Cursor is the position to send as after on the next read.
	Cursor int64 `json:"cursor"`
	// Reset asks the caller to read the current factory state once, then continue.
	Reset bool `json:"reset"`
	// More says another bounded read can return further retained outcomes.
	More bool `json:"more"`
}
