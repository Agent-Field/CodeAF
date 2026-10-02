package handoff

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAChatArrivesOnlyWhileATakeoverHoldsIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chat")
	if Arriving(root) {
		t.Fatal("a chat nobody is taking is arriving")
	}
	release := Arrive(root)
	if !Arriving(root) {
		t.Fatal("a chat being taken is not arriving")
	}
	release()
	if Arriving(root) {
		t.Fatal("a chat whose takeover ended is still arriving")
	}
}

// A takeover that died leaves its file but not its lock, so the chat opens.
func TestAFileLeftByADeadTakeoverFencesNothing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chat")
	if err := os.WriteFile(arrivalOf(root), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if Arriving(root) {
		t.Fatal("a file with no lock on it fences the chat")
	}
}
