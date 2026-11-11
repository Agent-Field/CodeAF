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

// job.read_size is the shown size when no label sets one, and only then. Each
// row names the Item and the exact wire bytes a client reads.
func TestJobReadSizeIsPresentOnlyWithoutALabel(t *testing.T) {
	tri := func(size, from string, at time.Time) factory.Triage {
		return factory.Triage{Size: size, SizeFrom: from, TriagedAt: at}
	}
	read, none := time.Unix(5, 0).UTC(), time.Time{}
	rows := []struct {
		name     string
		it       factory.Item
		wantSize string // job.size
		want     string // job.read_size as JSON, "" when absent
	}{
		{"read", factory.Item{Triage: tri("M", factory.SizeFromRead, read)}, "", `{"value":"M","from":"read"}`},
		{"diff", factory.Item{Kind: factory.KindPR, Origin: factory.OriginForge, Triage: tri("L", factory.SizeFromDiff, none)}, "", `{"value":"L","from":"diff"}`},
		{"label wins over a read", factory.Item{Labels: []string{"size:s"}, Triage: tri("L", factory.SizeFromRead, read)}, "S", ""},
		{"label alone", factory.Item{Labels: []string{"size/m"}}, "M", ""},
		{"legacy guess", factory.Item{Triage: tri("S", "", read)}, "", ""},
		{"chat guess", factory.Item{Triage: tri("S", factory.SizeFromChat, read)}, "", ""},
		{"read not completed", factory.Item{Triage: tri("S", factory.SizeFromRead, none)}, "", ""},
		{"read outside S M L", factory.Item{Triage: tri("XL", factory.SizeFromRead, read)}, "", ""},
		{"nothing", factory.Item{}, "", ""},
	}
	for _, r := range rows {
		raw, err := json.Marshal(FromItem(r.it))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		got := ""
		if v, ok := m["read_size"]; ok {
			got = string(v)
		}
		if got != r.want {
			t.Errorf("%s: read_size = %q, want %q", r.name, got, r.want)
		}
		if size := FromItem(r.it).Size; size != r.wantSize {
			t.Errorf("%s: size = %q, want %q", r.name, size, r.wantSize)
		}
		if _, both := m["size"]; both && got != "" {
			t.Errorf("%s: size and read_size are both on the wire: %s", r.name, raw)
		}
	}
}

// The shown size survives Job -> JSON -> Job -> Item, so a worker or peer that
// rebuilds the item shows what the engine showed; a payload from before the
// field decodes to no read_size and the item keeps its own answer.
func TestReadSizeRoundTripsToTheSameShownSize(t *testing.T) {
	it := factory.Item{Triage: factory.Triage{Size: "M", SizeFrom: factory.SizeFromRead, TriagedAt: time.Unix(5, 0).UTC()}}
	raw, err := json.Marshal(FromItem(it))
	if err != nil {
		t.Fatal(err)
	}
	var j Job
	if err := json.Unmarshal(raw, &j); err != nil {
		t.Fatal(err)
	}
	if j.ReadSize == nil || j.ReadSize.Value != "M" || j.ReadSize.From != factory.ShownRead {
		t.Fatalf("decoded read_size = %+v", j.ReadSize)
	}
	if size, from := factory.ShownSize(ToItem(j)); size != "M" || from != factory.ShownRead {
		t.Fatalf("rebuilt item shows %q from %q", size, from)
	}
	var old Job
	if err := json.Unmarshal([]byte(`{"id":"a#1","size":"S"}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.ReadSize != nil {
		t.Fatalf("an old payload gained a read_size: %+v", old.ReadSize)
	}
}
