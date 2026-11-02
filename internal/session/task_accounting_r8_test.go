package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// R7-8 and contract 5 include a complete ledger object whose final newline was lost.
func TestARemoteChargeWithoutItsNewlineIsNotChargedTwice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	line := UsageLine{RemoteEvent: "att_0123456789abcdef:3", USD: .25}
	raw, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = appendRemoteUsage(path, line); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(raw) {
		t.Fatalf("replaying a complete EOF charge appended bytes: %s", got)
	}
	if strings.Count(string(got), line.RemoteEvent) != 1 {
		t.Fatalf("charge repeated: %s", got)
	}
	if err = appendRemoteUsage(path, line); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(path)
	if string(got) != string(again) {
		t.Fatal("second replay changed the ledger")
	}
}
