package syncsetup

import (
	"github.com/Agent-Field/codeaf/internal/cell"
	"testing"
)

// TestC12UnavailableSyncHasNoFirstRun proves C12: an unavailable move capability
// creates neither a sync client nor an announcement, even on a fresh home.
func TestC12UnavailableSyncHasNoFirstRun(t *testing.T) {
	t.Cleanup(cell.UseAvailabilityInTests(false))
	t.Setenv(cell.EnvVar, "1")
	s, ok, err := OpenFirst(t.TempDir())
	if s != nil || ok || err != nil {
		t.Fatalf("unavailable sync = %v, %v, %v", s, ok, err)
	}
	if line, said := s.FirstRun(); line != "" || said {
		t.Fatalf("unavailable sync announced %q", line)
	}
}
