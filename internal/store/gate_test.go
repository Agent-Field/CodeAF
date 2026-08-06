package store

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDeliveryGateIsAppendOnlyAndRebuildSafe(t *testing.T) {
	graph, err := Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{{
		ID: "job", Brief: "ship the complete answer", Stage: 1,
	}}}, Provenance{Origin: OriginUser, SessionID: "s1", Intent: "answer every part"}); err != nil {
		t.Fatal(err)
	}

	want := DeliveryGate{Pass: false, Gap: "the benchmark result is missing", PolishClosed: true}
	if err := graph.RecordDeliveryGate("job", want); err != nil {
		t.Fatal(err)
	}
	before, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	gateEvents := 0
	for _, event := range before {
		if event.Kind != EventDeliveryGate {
			continue
		}
		gateEvents++
		var payload DeliveryGate
		if err := json.Unmarshal(event.Payload, &payload); err != nil || !reflect.DeepEqual(payload, want) {
			t.Fatalf("gate payload = %+v, err %v; want %+v", payload, err, want)
		}
	}
	if gateEvents != 1 {
		t.Fatalf("gate events = %d, want one", gateEvents)
	}

	if err := graph.Rebuild(); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	after, err := graph.Events(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("Rebuild changed the append-only journal")
	}
	got, ok, err := graph.DeliveryGateFor("job")
	if err != nil || !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("rebuilt gate = %+v, ok %t, err %v; want %+v", got, ok, err, want)
	}
}
