package triage

import (
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// A genuine read's size lands and says so; a body, a missing size and an
// invalid one are never turned into a size.
func TestApplyLandsTheReadSizeOverAGuess(t *testing.T) {
	read := factory.Triage{Read: "r", Size: "L"}
	for _, tc := range []struct {
		name       string
		have       factory.Triage
		size, from string
	}{
		{"nothing before", factory.Triage{}, "L", factory.SizeFromRead},
		{"a legacy size with no provenance", factory.Triage{Size: "S"}, "L", factory.SizeFromRead},
		{"a chat guess", factory.Triage{Size: "S", SizeFrom: factory.SizeFromChat}, "L", factory.SizeFromRead},
		{"a measured diff stays", factory.Triage{Size: "M", SizeFrom: factory.SizeFromDiff}, "M", factory.SizeFromDiff},
		{"a size already read stays", factory.Triage{Size: "S", SizeFrom: factory.SizeFromRead}, "S", factory.SizeFromRead},
	} {
		it := factory.Item{Triage: tc.have}
		Apply(&it, read)
		if it.Triage.Size != tc.size || it.Triage.SizeFrom != tc.from {
			t.Errorf("%s: %q from %q, want %q from %q", tc.name, it.Triage.Size, it.Triage.SizeFrom, tc.size, tc.from)
		}
	}
}

// A read with no valid size leaves what was there alone and invents nothing.
func TestApplyNeverInventsASize(t *testing.T) {
	parsed, err := Parse(`{"read":"r","type":"bug","size":"XXL","est":1}`)
	if err != nil || parsed.Size != "" {
		t.Fatalf("Parse kept an invalid size: %+v %v", parsed, err)
	}
	for _, have := range []factory.Triage{{}, {Size: "S", SizeFrom: factory.SizeFromChat}, {Size: "M"}} {
		it := factory.Item{Triage: have}
		Apply(&it, parsed)
		if it.Triage.Size != have.Size || it.Triage.SizeFrom != have.SizeFrom {
			t.Errorf("%+v became %q from %q", have, it.Triage.Size, it.Triage.SizeFrom)
		}
	}
}

// A completed read, once stamped, shows as the read; the same item before the
// stamp shows nothing.
func TestAReadIsShownOnlyOnceItIsStamped(t *testing.T) {
	it := factory.Item{Kind: factory.KindIssue, Origin: factory.OriginForge}
	Apply(&it, factory.Triage{Read: "r", Size: "M"})
	if size, _ := factory.ShownSize(it); size != "" {
		t.Fatalf("a size showed before TriagedAt: %q", size)
	}
	it.Triage.TriagedAt = time.Now()
	if size, from := factory.ShownSize(it); size != "M" || from != factory.ShownRead {
		t.Fatalf("shown %q from %q", size, from)
	}
}
