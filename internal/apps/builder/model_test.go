package builder

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestErrorsAreLabelledWithWhoAsked(t *testing.T) {
	tests := []struct{ role, want string }{{"", "builder"}, {"app agent", "app agent"}}
	for _, tc := range tests {
		if got := roleOf(svc.ModelCall{Role: tc.role}); got != tc.want {
			t.Errorf("roleOf(%q) = %q, want %q", tc.role, got, tc.want)
		}
	}
}
