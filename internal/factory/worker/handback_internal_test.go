package worker

import (
	"net/http"
	"sync/atomic"
	"testing"
)

// A QUIT HANDS ITS HOLD BACK, unless its results are not yet confirmed: their
// recovery is keyed to this hold, so it is left to run out as before.
func TestAQuitHandsTheHoldBackUnlessResultsAreUnconfirmed(t *testing.T) {
	var asked atomic.Int64
	var path atomic.Value
	j, _ := deliveryJob(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		path.Store(r.Method + " " + r.URL.Path)
		_, _ = w.Write([]byte(`{"factory":{"name":"test"}}`))
	}))
	j.pending = &pendingDelivery{}
	j.handBack()
	if asked.Load() != 0 {
		t.Fatal("a hold with unconfirmed results was handed back")
	}
	j.pending, j.blocked = nil, "results not saved"
	j.handBack()
	if asked.Load() != 0 {
		t.Fatal("a hold whose delivery is blocked was handed back")
	}
	j.blocked = ""
	before := handBacks.Load()
	j.handBack()
	if asked.Load() != 1 || path.Load() != "POST /v1/workers/leases/lease_test/handback" {
		t.Fatalf("the quit asked %d times, last %v; want one POST of the hand-back", asked.Load(), path.Load())
	}
	if handBacks.Load()-before != 1 {
		t.Fatal("the hand-back was not counted")
	}
}
