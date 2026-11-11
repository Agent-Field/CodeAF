package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestKeptJobFactsRoundTripAndOldRowsLoad(t *testing.T) {
	start := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	code := 17
	row := TaskNotice{
		ID: 8, Kind: TaskKindJob, State: TaskFailed, Title: "build",
		Report: jobRowLead(18, ""), Elapsed: 2 * time.Second,
		JobCommand: "printf build\nexit 17", JobCallID: "bash_28", JobExitCode: &code,
		StartedAt: start, EndedAt: start.Add(2 * time.Second),
	}
	raw, err := json.Marshal(runRowRecord(row))
	if err != nil {
		t.Fatal(err)
	}
	var saved runRecord
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	got, ok := jobNoticeFromRow(runRowNotice(saved))
	if !ok || got.Command != row.JobCommand || got.ToolCallID != row.JobCallID ||
		!got.Started.Equal(start) || !got.Ended.Equal(row.EndedAt) ||
		got.Elapsed != row.Elapsed || got.State != JobFailed || got.ExitCode != code || got.ExitCodeUnknown {
		t.Fatalf("round trip = %+v", got)
	}
	// A report without a log still names the original registry number.
	if got.ID != 18 {
		t.Fatalf("job = %d, want 18", got.ID)
	}
	var old runRecord
	if err := json.Unmarshal([]byte(`{"id":9,"kind":"job","state":"failed","report":"job 19 · log /tmp/19.log","elapsed_ms":20774}`), &old); err != nil {
		t.Fatal(err)
	}
	legacy, ok := jobNoticeFromRow(runRowNotice(old))
	if !ok || legacy.ID != 19 || legacy.State != JobFailed || !legacy.ExitCodeUnknown ||
		legacy.Command != "" || legacy.ToolCallID != "" || !legacy.Started.IsZero() || legacy.Elapsed != 20774*time.Millisecond {
		t.Fatalf("old row = %+v", legacy)
	}
}

func TestReadProcessNoticesIncludesKeptJobsWithoutShelf(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "transcript.jsonl")
	doc := `{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"kind":"job","state":"failed","report":"job 3 · log /tmp/3.log","elapsed_ms":2000,"job_exit_code":7,"job_command":"exit 7","job_call_id":"bash_3","startedAt":"2026-10-20T09:00:00Z","endedAt":"2026-10-20T09:00:02Z"}]}`
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadProcessNotices(file)
	if err != nil || len(rows) != 1 || rows[0].ID != 3 || rows[0].ExitCode != 7 || rows[0].ExitCodeUnknown {
		t.Fatalf("notices = %+v, %v", rows, err)
	}
}

func TestJobKeptRowRecordsLiveCallAndFailure(t *testing.T) {
	journal, _ := journalIn(t)
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = journal })
	lane := rosterLane(t, a)
	runBash(t, withCallID(context.Background(), "bash_failure"), a, map[string]any{"command": "exit 17", "background": true})
	started := awaitJob(t, lane, JobRunning)
	ended := awaitJob(t, lane, JobFailed)
	if started.ToolCallID != "bash_failure" || ended.ExitCode != 17 {
		t.Fatalf("live = %+v / %+v", started, ended)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	doc, ok := loadTaskCheckpoint(taskCheckpointPath(journal))
	if !ok {
		t.Fatal("no checkpoint")
	}
	var restored JobNotice
	for _, row := range doc.Runs {
		if n, isJob := jobNoticeFromRow(runRowNotice(row)); isJob {
			restored = n
		}
	}
	if restored.ID != started.ID || !restored.Started.Equal(started.Started) || restored.ToolCallID != "bash_failure" || restored.Command != "exit 17" || restored.State != JobFailed || restored.ExitCodeUnknown || restored.ExitCode != 17 || restored.Ended.IsZero() {
		t.Fatalf("restored = %+v; started = %+v", restored, started)
	}
}

func TestRestoredKeptJobsListMatchesReplayWithoutShelf(t *testing.T) {
	journal, _ := journalIn(t)
	doc := `{"type":"tasks","version":1,"seq":1,"runs":[{"id":1,"kind":"job","title":"build","state":"failed","report":"job 3 · log /tmp/3.log","elapsed_ms":2000,"job_exit_code":17,"job_command":"exit 17","job_call_id":"bash_3","startedAt":"2026-10-20T09:00:00Z","endedAt":"2026-10-20T09:00:02Z"}]}`
	if err := os.WriteFile(taskCheckpointPath(journal), []byte(doc), 0600); err != nil {
		t.Fatal(err)
	}
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = journal })
	replayed := awaitJob(t, rosterLane(t, a), JobFailed)
	rows, err := a.JobNotices()
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(replayed, rows[0]) {
		t.Fatalf("replay = %+v; list = %+v, %v", replayed, rows, err)
	}
}
