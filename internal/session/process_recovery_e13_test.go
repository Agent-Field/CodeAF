package session

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The checkpoint knows this instant command ended, while its process record
// still carries the running snapshot left by the old engine. Deciding that
// needs no live conversation and writes nothing: the chat may be held by
// another computer, and its sealed files are not this check's to change.
func TestProcessRecoveryE13B1SettledCheckpointNeedsNoRecovery(t *testing.T) {
	for _, state := range []string{"done", "failed"} {
		t.Run(state, func(t *testing.T) {
			file := seedE13ProcessRecovery(t, state)
			record := filepath.Join(filepath.Dir(file), "processes", "1.json")
			before, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if NeedsProcessRecovery(file) {
					t.Fatal("B1 settled job demanded a live conversation")
				}
			}
			after, err := os.ReadFile(record)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("B1 the check changed the process record: %v", err)
			}
		})
	}
}

// The next conversation that holds the chat makes the known ending durable.
func TestProcessRecoveryE13B1HeldRecoveryRepairsRunningRecord(t *testing.T) {
	file := seedE13ProcessRecovery(t, "done")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = file
		c.Place = Place{Dir: filepath.Dir(file)}
	})
	n, found, err := ReadProcessRecord(file, 1)
	if err != nil || !found || n.State == JobRunning || n.Ended.IsZero() || n.Command != "say hi" {
		t.Fatalf("B1 ending was not durable: %+v, %v", n, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if NeedsProcessRecovery(file) {
		t.Fatal("B1 a repaired record demanded recovery again")
	}
}

func TestProcessRecoveryE13B2UnfinishedJobStillNeedsRecovery(t *testing.T) {
	file := seedE13ProcessRecovery(t, "running")
	if !NeedsProcessRecovery(file) {
		t.Fatal("B2 unfinished job skipped recovery")
	}
}

func TestProcessRecoveryE13B2RecoveryDurablySettlesBothStores(t *testing.T) {
	file := seedE13ProcessRecovery(t, "running")
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
		c.SessionFile = file
		c.Place = Place{Dir: filepath.Dir(file)}
	})
	n, found, err := ReadProcessRecord(file, 1)
	if err != nil || !found || n.State != JobStopped {
		t.Fatalf("B2 recovered ending was not durable: %+v, %v", n, err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if NeedsProcessRecovery(file) {
		t.Fatal("B1/B2 completed recovery demanded another live conversation")
	}
}

func TestProcessRecoveryE13B2AnotherRunsEndingIsNotBorrowed(t *testing.T) {
	file := seedE13ProcessRecovery(t, "done")
	n, _, _ := ReadProcessRecord(file, 1)
	n.Started = n.Started.AddDate(0, 0, 1)
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(file), "processes", "1.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if !NeedsProcessRecovery(file) {
		t.Fatal("B2 a reused job id was settled by another run's checkpoint")
	}
}

func seedE13ProcessRecovery(t *testing.T, state string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	if err := os.Mkdir(filepath.Join(dir, "processes"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"transcript.jsonl":    "",
		"processes/head.json": `{"epoch":"e13","high":1}`,
		"processes/1.json":    `{"ID":1,"Command":"say hi","Kind":"command","State":"running","Started":"2026-10-27T23:58:18Z","Elapsed":379625,"ToolCallID":"call_e13","StartedBy":"chat"}`,
		"tasks.json":          `{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"kind":"job","title":"say hi","state":"` + state + `","report":"job 1","job_command":"say hi","job_call_id":"call_e13","startedAt":"2026-10-27T23:58:18Z","endedAt":"2026-10-27T23:58:18.000379625Z"}]}`,
	}
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return file
}
