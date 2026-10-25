package fabrictest

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"io"
	"net/http"
	"testing"
	"time"
)

func testSlotChanges(t *testing.T, r *Rig) {
	full, client, path := pairClient(t, r)
	changes := path + "/slots?since=0&wait=0"
	fabricCall(t, r, client, "GET", changes, nil, 204)
	fabricCall(t, r, full, "PUT", path+"/slots/chat", map[string]any{"sealed": []byte("one")}, 204)
	var got struct {
		Version uint64
		Slots   []string
	}
	if err := json.Unmarshal(fabricCall(t, r, client, "GET", changes, nil, 200), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != 1 || len(got.Slots) != 1 || got.Slots[0] != "chat" {
		t.Fatalf("changes: %+v", got)
	}
	if r.Restart != nil {
		r.Restart()
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan slotPollResult, 3)
	for range 3 {
		go func() { done <- pollSlots(ctx, r, client, path+"/slots?since=1&wait=25") }()
	}
	// A 429 proves two requests have been admitted and are blocked, so this
	// exercises notification rather than the immediate newer-version path.
	select {
	case result := <-done:
		if result.status != 429 {
			t.Fatalf("waiter admission: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("waiters never admitted")
	}
	fabricCall(t, r, full, "PUT", path+"/slots/chat", map[string]any{"sealed": []byte("two")}, 204)
	for range 2 {
		select {
		case result := <-done:
			if result.err != nil || result.status != 200 {
				t.Fatalf("notification: %+v", result)
			}
			if err := json.Unmarshal(result.raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.Version != 2 || len(got.Slots) != 1 || got.Slots[0] != "chat" {
				t.Fatalf("replacement: %+v", got)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("PUT did not wake the 25-second wait")
		}
	}
	fabricCall(t, r, client, "GET", path+"/slots?since=2&wait=0.01", nil, 204)
	fabricCall(t, r, full, "GET", changes, nil, 401)
	for _, query := range []string{"since=-1&wait=0", "since=x&wait=0", "since=0&wait=26", "since=0&wait=-1", "since=0&wait=NaN"} {
		t.Run(fmt.Sprint(query), func(t *testing.T) { fabricCall(t, r, client, "GET", path+"/slots?"+query, nil, 400) })
	}
}

// pollSlots reports errors to the test goroutine instead of calling Fatal
// from a worker which would leave its caller waiting for a missing result.
type slotPollResult struct {
	status int
	raw    []byte
	err    error
}

func pollSlots(ctx context.Context, r *Rig, client device, path string) slotPollResult {
	req, err := http.NewRequestWithContext(ctx, "GET", r.URL()+path, nil)
	if err != nil {
		return slotPollResult{err: err}
	}
	reqsign.Sign(req, nil, client, r.Now())
	resp, err := r.Client.Do(req)
	if err != nil {
		return slotPollResult{err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	return slotPollResult{resp.StatusCode, raw, err}
}
