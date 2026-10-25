package fabrictest

import (
	"encoding/json"
	"fmt"
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
	done := make(chan []byte, 1)
	go func() { done <- fabricCall(t, r, client, "GET", path+"/slots?since=1&wait=2", nil, 200) }()
	fabricCall(t, r, full, "PUT", path+"/slots/chat", map[string]any{"sealed": []byte("two")}, 204)
	select {
	case raw := <-done:
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Version != 2 || len(got.Slots) != 1 || got.Slots[0] != "chat" {
			t.Fatalf("replacement: %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("slot PUT did not wake long-poll")
	}
	fabricCall(t, r, client, "GET", path+"/slots?since=2&wait=0.01", nil, 204)
	fabricCall(t, r, full, "GET", changes, nil, 401)
	for _, query := range []string{"since=-1&wait=0", "since=x&wait=0", "since=0&wait=26", "since=0&wait=-1", "since=0&wait=NaN"} {
		t.Run(fmt.Sprint(query), func(t *testing.T) { fabricCall(t, r, client, "GET", path+"/slots?"+query, nil, 400) })
	}
}
