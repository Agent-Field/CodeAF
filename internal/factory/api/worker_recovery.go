package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// WorkerRecovery reconciles an immutable result request retained by the runner
// that owns this job's worktree. It grants no permission to rerun a step.
type WorkerRecovery struct {
	OriginalLease string          `json:"original_lease"`
	Body          json.RawMessage `json:"body"`
	Headers       http.Header     `json:"headers"`
	// AdditionalCost is cumulative usage recorded after this frozen report.
	// Repeated acknowledgments charge only a positive increase of this value.
	AdditionalCost float64 `json:"additional_cost,omitempty"`
}

// WorkerRecovered returns the authoritative baseline after results were saved.
// A repeated request returns the current job without reapplying cost or results.
type WorkerRecovered struct {
	Factory          Factory   `json:"factory"`
	Job              Job       `json:"job"`
	AlreadyApplied   bool      `json:"already_applied"`
	RetryRequestedAt time.Time `json:"retry_requested_at,omitzero"`
}
