package session

import (
	"os"
	"path/filepath"
	"testing"
)

// Contract L1 accepts the chat's location while refusing redirects within its history.
func TestMovedTaskHistoryUnderAnAliasRejectsInternalRedirects(t *testing.T) {
	for _, redirect := range []string{"state", "history", "task", "worker", "trajectory"} {
		t.Run(redirect, func(t *testing.T) {
			physical := t.TempDir()
			alias := filepath.Join(t.TempDir(), "tmp")
			if err := os.Symlink(physical, alias); err != nil {
				t.Fatal(err)
			}
			place := Place{Dir: filepath.Join(alias, "copied-chat"), Workspace: t.TempDir()}
			dir := filepath.Join(place.Dir, ".cell", "tasks", "1")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			writeCheckpoint(t, place.Tasks(), taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 1, Runs: []runRecord{{ID: 1, Title: "Write a greeting", State: TaskDone, PlanTask: "t-1"}}})
			worker := filepath.Join(dir, "20261027-011448.092944_worker.jsonl")
			trajectory := filepath.Join(dir, "trajectory.jsonl")
			for path, body := range map[string]string{
				worker:     "{\"type\":\"session\",\"version\":1,\"id\":\"worker\",\"model\":\"saved/model\"}\n{\"type\":\"message\",\"role\":\"assistant\",\"content\":\"Greeting saved.\"}\n",
				trajectory: "{\"kind\":\"step\",\"step\":1,\"command\":\"printf greeting\",\"observation\":\"greeting\"}\n",
			} {
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			agent := journalAgent(t, place)
			page, found := agent.PlanTaskPage("t-1")
			if !found || len(page.Worker) != 1 || len(page.Steps) != 1 {
				t.Fatalf("ordinary history beneath an ancestor alias is unreadable: %+v", page)
			}
			if got := agent.TaskJournal(1); got != worker {
				t.Fatalf("worker path changed: %q, want %q", got, worker)
			}
			if rows := RebuiltTaskRows(place.Dir); len(rows) != 1 || rows[0].TranscriptURI != taskURI(worker) || rows[0].Model != "saved/model" {
				t.Fatalf("received activity row lost its worker: %+v", rows)
			}
			target := map[string]string{
				"state":      filepath.Join(place.Dir, ".cell"),
				"history":    place.NodeJournals(),
				"task":       dir,
				"worker":     worker,
				"trajectory": trajectory,
			}[redirect]
			outside := filepath.Join(t.TempDir(), "retained")
			if err := os.Rename(target, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			page, found = agent.PlanTaskPage("t-1")
			if !found || !page.Historical {
				t.Fatal("the finished receipt disappeared")
			}
			if redirect != "trajectory" {
				if len(page.Worker) != 0 || agent.TaskJournal(1) != "" {
					t.Error("redirected worker entered the task history")
				}
				if rows := RebuiltTaskRows(place.Dir); len(rows) != 1 || rows[0].TranscriptURI != "" {
					t.Errorf("redirected worker entered the activity index: %+v", rows)
				}
			}
			if redirect != "worker" && len(page.Steps) != 0 {
				t.Error("redirected trajectory entered the task history")
			}
		})
	}
}
