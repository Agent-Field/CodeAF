package cell

import "testing"

// TestC11WindowsCannotEnableCells proves C27.3 with a nonexistent engine:
// Windows never attempts to launch it.
func TestC11WindowsCannotEnableCells(t *testing.T) {
	t.Setenv("CODEAF_FURROW", "does-not-exist.exe")
	if Enabled() || Available() || UnavailableReason() != WindowsReason {
		t.Fatal("Windows offered a Unix-only capability")
	}
}
