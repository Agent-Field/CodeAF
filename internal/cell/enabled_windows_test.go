package cell

import "testing"

// TestK1WindowsMovingChatsNeedOnlyTheEngine proves K1: native Windows offers
// the same capability as Unix and keeps it absent when the engine is missing.
func TestK1WindowsMovingChatsNeedOnlyTheEngine(t *testing.T) {
	t.Cleanup(UseAvailabilityInTests(true))
	if !Enabled() || !Available() {
		t.Fatal("Windows omitted moving chats with an available engine")
	}
	engineAvailable = func() bool { return false }
	if Available() || UnavailableReason() == "" {
		t.Fatal("Windows offered moving chats without an engine")
	}
}
