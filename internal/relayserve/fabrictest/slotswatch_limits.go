package fabrictest

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func testSlotWaitCancellation(t *testing.T, r *Rig) {
	_, client, path := pairClient(t, r)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan int, 3)
	for range 3 {
		go func() { result := pollSlots(ctx, r, client, path+"/slots?since=0&wait=25"); results <- result.status }()
	}
	select {
	case status := <-results:
		if status != 429 {
			t.Fatalf("waiter cap: %d", status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("third waiter was not refused")
	}
	// One client's full allowance must not block other clients or mutations.
	otherFull, otherClient, otherPath := pairClient(t, r)
	fabricCall(t, r, otherFull, "PUT", otherPath+"/slots/other", map[string]any{"sealed": []byte("independent")}, 204)
	fabricCall(t, r, otherClient, "GET", otherPath+"/slots?since=0&wait=25", nil, 200)
	cancel()
	for range 2 {
		select {
		case <-results:
		case <-time.After(time.Second):
			t.Fatal("cancelled request did not exit")
		}
	}
	// Retry until the server observes both disconnected sockets. A successful
	// timeout proves their capacity was returned, through the same HTTP seam.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", r.URL()+path+"/slots?since=0&wait=0.01", nil)
		reqsign.Sign(req, nil, client, r.Now())
		resp, err := r.Client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 204 {
			return
		}
		if resp.StatusCode != 429 {
			t.Fatalf("after cancel: %d", resp.StatusCode)
		}
	}
	t.Fatal("cancelled waiters retained capacity")
}
