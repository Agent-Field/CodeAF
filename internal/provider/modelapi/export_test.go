package modelapi

import "time"

// ShortenReceiptWait lets a test outside the package see Close give up on a
// receipt that never comes without waiting the provider's whole schedule.
func ShortenReceiptWait(bound time.Duration) (restore func()) {
	was := receiptWait
	receiptWait = bound
	return func() { receiptWait = was }
}
