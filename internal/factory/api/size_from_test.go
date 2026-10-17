package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// The size's provenance crosses the wire and back, so a worker or a peer never
// strips it; a payload from a build that predates it reads as unknown.
func TestTriageSizeFromRoundTrips(t *testing.T) {
	for _, from := range []string{factory.SizeFromRead, factory.SizeFromDiff, factory.SizeFromChat} {
		tr := factory.Triage{Size: "M", SizeFrom: from, TriagedAt: time.Unix(5, 0).UTC()}
		raw, err := json.Marshal(fromTriage(tr))
		if err != nil {
			t.Fatal(err)
		}
		var wire Triage
		if err := json.Unmarshal(raw, &wire); err != nil {
			t.Fatal(err)
		}
		if got := toTriage(wire); got.Size != "M" || got.SizeFrom != from {
			t.Errorf("%s came back %q from %q", from, got.Size, got.SizeFrom)
		}
	}
	var m map[string]any
	raw, _ := json.Marshal(fromTriage(factory.Triage{Size: "S"}))
	_ = json.Unmarshal(raw, &m)
	if _, ok := m["size_from"]; ok {
		t.Fatalf("an unset provenance reached the wire: %s", raw)
	}
	var old Triage
	if err := json.Unmarshal([]byte(`{"size":"S","triaged_at":"2026-10-01T00:00:00Z"}`), &old); err != nil {
		t.Fatal(err)
	}
	it := factory.Item{Triage: toTriage(old)}
	if it.Triage.SizeFrom != "" {
		t.Fatalf("an old payload gained a provenance: %q", it.Triage.SizeFrom)
	}
	if size, _ := factory.ShownSize(it); size != "" {
		t.Fatalf("an old payload's size is shown: %q", size)
	}
}

// job.size stays the label alone: the wire carries no read's size as a start size.
func TestJobSizeOnTheWireIsTheLabelAlone(t *testing.T) {
	read := factory.Item{Triage: factory.Triage{Size: "S", SizeFrom: factory.SizeFromRead, TriagedAt: time.Unix(5, 0)}}
	if got := FromItem(read).Size; got != "" {
		t.Fatalf("job.size = %q from a read", got)
	}
	labelled := factory.Item{Labels: []string{"size:m"}, Triage: read.Triage}
	if got := FromItem(labelled).Size; got != "M" {
		t.Fatalf("job.size = %q, want the label", got)
	}
}
