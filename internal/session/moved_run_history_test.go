package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAMovedRunKeepsItsRosterAndWorkerTranscript(t *testing.T) {
	for _, name := range []string{"worker.jsonl", "20261026-120000.000000_worker.jsonl"} {
		t.Run(name, func(t *testing.T) {
			place := newPlace(t, false)
			if err := os.MkdirAll(filepath.Join(place.Dir, ".cell"), 0700); err != nil {
				t.Fatal(err)
			}
			writeCheckpoint(t, place.Tasks(), taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 1, Runs: []runRecord{{ID: 1, Title: "Create hello-task.txt", State: TaskDone, PlanTask: "t-1", Model: "z-ai/glm-5.3-flash", Merge: "merged", Changed: []string{"hello-task.txt"}}}})
			dir := filepath.Join(place.NodeJournals(), "1")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			journal := filepath.Join(dir, name)
			body := `{"type":"session","version":1,"id":"worker","model":"z-ai/glm-5.3-flash"}` + "\n" + `{"type":"message","role":"assistant","content":"created hello-task.txt"}` + "\n"
			if err := os.WriteFile(journal, []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			trajectory := []byte(`{"kind":"step","step":1,"command":"printf hello","observation":"hello"}` + "\n")
			if err := os.WriteFile(filepath.Join(dir, "trajectory.jsonl"), trajectory, 0600); err != nil {
				t.Fatal(err)
			}
			agent := journalAgent(t, place)
			if len(agent.PlanTasks()) != 0 {
				t.Fatal("fixture unexpectedly contains a plan database")
			}
			if _, found := agent.PlanTaskPage("t-1"); found {
				t.Fatal("a trajectory file unexpectedly recreated the missing plan page")
			}
			t.Log("trajectory is present; a plan page still requires the plan database")
			events, stop := agent.WatchTaskUpdates()
			defer stop()
			select {
			case event := <-events:
				if event.Task == nil || event.Task.ID != 1 || event.Task.State != TaskDone || event.Task.Model != "z-ai/glm-5.3-flash" {
					t.Fatalf("finished run lost from roster: %+v", event.Task)
				}
				t.Logf("roster: id=%d state=%s model=%s; plan rows=0", event.Task.ID, event.Task.State, event.Task.Model)
			case <-time.After(2 * time.Second):
				t.Fatal("finished run missing from roster replay")
			}
			if got := agent.TaskJournal(1); got != journal {
				t.Errorf("TaskJournal(1)=%q, want transferred local worker", got)
			} else if record := ReadTranscript(got); len(record.Entries) != 1 || record.Entries[0].Text != "created hello-task.txt" {
				t.Errorf("worker room cannot replay its transcript: %+v", record)
			}
			rows := RebuiltTaskRows(place.Dir)
			if len(rows) != 1 || rows[0].TranscriptURI != taskURI(journal) || rows[0].Status != "done" {
				t.Errorf("rebuilt activity row lacks worker transcript: %+v", rows)
			}
			if got := agent.TaskJournal(99); got != "" {
				t.Errorf("unknown run exposed another task's journal: %q", got)
			}
		})
	}
}

func TestWorkerHistoryPrefersTheNewestAndRefusesRedirects(t *testing.T) {
	place := newPlace(t, false)
	writeCheckpoint(t, place.Tasks(), taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 1, Runs: []runRecord{{ID: 1, Title: "Create hello-task.txt", State: TaskDone, PlanTask: "t-1"}}})
	dir := filepath.Join(place.NodeJournals(), "1")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"session","version":1,"id":"worker","model":"recorded/model"}` + "\n")
	stable := filepath.Join(dir, "worker.jsonl")
	newest := filepath.Join(dir, "20261026-120000.000000_worker.jsonl")
	for _, path := range []string{stable, newest} {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	agent := journalAgent(t, place)
	if got := agent.TaskJournal(1); got != newest {
		t.Fatalf("newest worker journal=%q", got)
	}
	if err := os.Remove(newest); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stable); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "worker.jsonl")
	if err := os.WriteFile(outside, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, stable); err != nil {
		t.Fatal(err)
	}
	if got := agent.TaskJournal(1); got != "" {
		t.Fatalf("redirected worker journal=%q", got)
	}
	if rows := RebuiltTaskRows(place.Dir); len(rows) != 1 || rows[0].TranscriptURI != "" {
		t.Fatalf("redirected worker indexed: %+v", rows)
	}
	if err := os.Remove(stable); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(outside), dir); err != nil {
		t.Fatal(err)
	}
	if got := agent.TaskJournal(1); got != "" {
		t.Fatalf("redirected task directory=%q", got)
	}
}
