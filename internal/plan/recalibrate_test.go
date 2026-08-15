package plan

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/profile"
)

func TestCalibrationEvidenceOmitsUnknownSources(t *testing.T) {
	tests := []struct {
		name       string
		record     profile.Record
		wantClause string
	}{
		{
			name:   "unknown zero",
			record: profile.Record{Title: "direct", Turns: 3, Tokens: 2_000},
		},
		{
			name:       "known zero",
			record:     profile.Record{Title: "planned", Turns: 3, Tokens: 2_000, SourcesKnown: true},
			wantClause: "0 sources",
		},
		{
			name:       "legacy nonzero",
			record:     profile.Record{Title: "legacy", Turns: 3, Tokens: 2_000, Sources: 4},
			wantClause: "4 sources",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var evidence strings.Builder
			appendCalibrationEvidence(&evidence, "middle", test.record)
			got := evidence.String()
			if test.wantClause == "" && strings.Contains(got, "sources") {
				t.Fatalf("evidence %q claims a source count", got)
			}
			if test.wantClause != "" && !strings.Contains(got, test.wantClause) {
				t.Fatalf("evidence %q does not contain %q", got, test.wantClause)
			}
		})
	}
}

// The evidence block used to be a range over a map literal, so the same three
// bands came out in a different order on every run. That is a reproducibility
// bug first — two identical profiles produced two different documents — and a
// cache miss second.
func TestCalibrationEvidenceOrderIsFixed(t *testing.T) {
	small := []profile.Record{{Title: "quick one", Turns: 2, Tokens: 1_000}}
	middle := []profile.Record{{Title: "middling one", Turns: 4, Tokens: 8_000}}
	large := []profile.Record{{Title: "exhausted one", Turns: 9, Tokens: 40_000}}

	first := calibrationEvidence(small, middle, large)
	for attempt := 0; attempt < 32; attempt++ {
		if again := calibrationEvidence(small, middle, large); again != first {
			t.Fatalf("evidence render %d differs:\n%s\n\n%s", attempt, first, again)
		}
	}
	quick := strings.Index(first, "quick one")
	middling := strings.Index(first, "middling one")
	exhausted := strings.Index(first, "exhausted one")
	if quick < 0 || middling < quick || exhausted < middling {
		t.Fatalf("bands are not rendered small to large:\n%s", first)
	}
}
