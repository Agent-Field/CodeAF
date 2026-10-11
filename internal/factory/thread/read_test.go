package thread

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSpokenLinesKeepsClocksAndStableIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	const body = `{"type":"session","version":1,"id":"n","cwd":"/tmp","timestamp":"2026-10-11T12:00:00Z"}
{"type":"message","role":"user","content":"ship the proof","timestamp":"2026-10-11T12:00:01.5Z"}
{"type":"message","role":"assistant","content":"Proof is ready.","timestamp":"2026-10-11T12:01:00Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	lines := Read(path)
	if len(lines) != 2 || lines[0].From != "user" || lines[1].From != "manager" || lines[0].At.IsZero() || lines[1].ID == "" {
		t.Fatalf("spoken lines: %+v", lines)
	}
	again := Read(path)
	if lines[1].ID != again[1].ID {
		t.Fatal("reread changed the message ID")
	}
	if got := Read(filepath.Join(t.TempDir(), "absent")); len(got) != 0 {
		t.Fatalf("missing journal: %+v", got)
	}
}
