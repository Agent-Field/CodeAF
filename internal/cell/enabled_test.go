package cell

import (
	"runtime"
	"testing"

	"github.com/Agent-Field/codeaf/internal/furrowbin"
)

// TestC27EnvironmentCannotDisableMovingChats proves C27.1 and C27.2 through
// the real availability door, including former overrides and invalid values.
func TestC27EnvironmentCannotDisableMovingChats(t *testing.T) {
	t.Cleanup(UseAvailabilityInTests(true))
	for _, value := range []string{"", "0", "1", "true", "2", " 1", "off"} {
		t.Run("env="+value, func(t *testing.T) {
			t.Setenv("CODEAF_CELLS", value)
			want := ""
			if runtime.GOOS == "windows" {
				want = WindowsReason
			}
			if got := UnavailableReason(); got != want {
				t.Fatalf("UnavailableReason() = %q, want %q", got, want)
			}
			if Available() != (want == "") {
				t.Fatal("availability disagrees with its reason")
			}
		})
	}
}

// TestC27MissingEngineStaysAbsent proves C27.3: removing the switch cannot
// advertise a capability when the runtime engine cannot provide it.
func TestC27MissingEngineStaysAbsent(t *testing.T) {
	t.Cleanup(UseAvailabilityInTests(false))
	want := furrowbin.CellsUnavailable
	if runtime.GOOS == "windows" {
		want = WindowsReason
	}
	if Available() || UnavailableReason() != want {
		t.Fatalf("missing engine: %q", UnavailableReason())
	}
}
