package factory

import "time"

// WorkerReportReceipt records only an opaque request digest, never credentials,
// payloads or machine-local paths. Machine and Handle bind late usage to its
// original worker after a new run clears or replaces the worktree owner.
// It is not writable through worker reports.
type WorkerReportReceipt struct {
	// Archived means this receipt belongs to work cleared from the job.
	Archived bool `json:",omitempty"`
	// Cleared means a Bring back made the job fresh after this report: usage
	// that settles late for it is the factory's spending ([Item.SpentCleared])
	// and never the fresh job's.
	Cleared        bool `json:",omitempty"`
	Digest         string
	Lease          string `json:",omitempty"`
	Machine        string `json:",omitempty"`
	Handle         string `json:",omitempty"`
	At             time.Time
	AdditionalCost float64 `json:",omitempty"`
}

// WorkerReportReceiptsMost bounds reconciliation history. Older reports also
// have to match the current step attempt before they can be accepted anew.
const WorkerReportReceiptsMost = 64
