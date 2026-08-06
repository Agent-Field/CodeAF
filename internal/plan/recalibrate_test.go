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
