package factory

import "time"

// WorkerReportReceipt records only an opaque request digest, never credentials,
// payloads or machine-local paths. It is not writable through worker reports.
type WorkerReportReceipt struct {
	Digest         string
	At             time.Time
	AdditionalCost float64 `json:",omitempty"`
}

// WorkerReportReceiptsMost bounds reconciliation history. Older reports also
// have to match the current step attempt before they can be accepted anew.
const WorkerReportReceiptsMost = 64
