package directory

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// R22's explicit receipt is distinct from pong/version frames and preserves
// full-width indexes/fences; a maximum hold set fits the client's read budget.
func TestR22CommittedHoldReceiptWire(t *testing.T) {
	receipts := make([]dirwatch.HoldReceipt, MaxHolds)
	for i := range receipts {
		receipts[i] = dirwatch.HoldReceipt{Cell: strings.Repeat("x", MaxHoldCellBytes), Fence: ^uint64(0), Index: ^uint64(0), At: 1000, Until: 91000}
	}
	raw, e := json.Marshal(struct {
		Holds []dirwatch.HoldReceipt `json:"holds"`
	}{receipts})
	if e != nil || len(raw) > watchLimit {
		t.Fatal("R22 receipt read budget", len(raw), e)
	}
	frame, e := decodeFrame(raw)
	if e != nil || len(frame.Holds) != MaxHolds || frame.Holds[0].Fence != ^uint64(0) || frame.Holds[0].Index != ^uint64(0) {
		t.Fatal(frame, e)
	}
}
