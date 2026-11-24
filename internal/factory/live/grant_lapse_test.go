package live_test

import (
	"strconv"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/live"
)

func TestContractL1LargeLapseBatchStaysInReplay(t *testing.T) {
	root := t.TempDir()
	n := live.Limit + 1
	events := make([]live.Event, 0, n)
	for i := range n {
		events = append(events, live.New("resource.changed", 0, struct{ ID string }{strconv.Itoa(i)}))
	}
	if err := live.AppendGrantLapses(root, events...); err != nil {
		t.Fatal(err)
	}
	if err := live.Append(root, live.New("resource.changed", 0, struct{ ID string }{"later"})); err != nil {
		t.Fatal(err)
	}
	replay, _, err := live.Read(root)
	if err != nil || len(replay) != n+1 {
		t.Fatalf("read %d events, want %d: %v", len(replay), n+1, err)
	}
	for i, want := range events {
		if got := replay[i]; string(got.Data) != string(want.Data) {
			t.Fatalf("event %d = %s, want %s", i, got.Data, want.Data)
		}
	}
}
