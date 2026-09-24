package session

import (
	"github.com/Agent-Field/codeaf/internal/filelock"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func deletionFixture(t *testing.T) (string, string) {
	t.Helper()
	bucket := t.TempDir()
	for _, id := range []string{"one", "two"} {
		dir := filepath.Join(bucket, id)
		if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := SaveMeta(dir, Meta{ID: id, Title: id, LastUserAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "transcript.jsonl"), []byte("conversation"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, task := range []string{"1", "2"} {
			journal := filepath.Join(dir, "tasks", task+".jsonl")
			if err := os.WriteFile(journal, []byte("task report"), 0o600); err != nil {
				t.Fatal(err)
			}
			appendTaskIndex(filepath.Join(bucket, taskIndexName), TaskIndexEntry{ID: task, SessionID: id, Title: task, TranscriptURI: taskURI(journal)})
		}
	}
	return filepath.Join(bucket, "one"), filepath.Join(bucket, taskIndexName)
}

func TestDeleteTaskRecordPermanentlyRemovesOnlyItsRecord(t *testing.T) {
	dir, index := deletionFixture(t)
	if err := DeleteTaskRecord(dir, "one", "1"); err != nil {
		t.Fatal(err)
	}
	appendTaskIndex(index, TaskIndexEntry{ID: "1", SessionID: "one", Title: "late completion"})
	rows := ReadTaskIndex(index)
	if len(rows) != 3 {
		t.Fatalf("rows=%+v", rows)
	}
	for _, row := range rows {
		if row.SessionID == "one" && row.ID == "1" {
			t.Fatal("deleted record was republished")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks", "1.jsonl")); !os.IsNotExist(err) {
		t.Fatal("deleted journal remains")
	}
	for _, path := range []string{filepath.Join(dir, "transcript.jsonl"), filepath.Join(dir, "tasks", "2.jsonl")} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("sibling or parent lost: %v", err)
		}
	}
}

func TestDeleteConversationRemovesAllTasksAndPreservesOtherConversations(t *testing.T) {
	dir, index := deletionFixture(t)
	if err := DeleteConversation(dir, "one"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("conversation remains")
	}
	rows := ReadTaskIndex(index)
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	for _, row := range rows {
		if row.SessionID != "two" {
			t.Fatal("deleted conversation retained task records")
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "two", "tasks", "2.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteConversationRefusesHeldAndMismatchedOwners(t *testing.T) {
	dir, index := deletionFixture(t)
	if err := DeleteConversation(dir, "two"); err == nil {
		t.Fatal("mismatched owner deleted")
	}
	file, err := os.OpenFile(filepath.Join(dir, "transcript.jsonl"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	defer filelock.Unlock(file)
	if err := DeleteConversation(dir, "one"); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("held conversation deletion: %v", err)
	}
	if len(ReadTaskIndex(index)) != 4 {
		t.Fatal("failed delete changed task records")
	}
}

func TestDeleteTaskRecordNeverFollowsACitationIntoProjectFiles(t *testing.T) {
	dir, index := deletionFixture(t)
	project := filepath.Join(t.TempDir(), "important.txt")
	if err := os.WriteFile(project, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendTaskIndex(index, TaskIndexEntry{ID: "1", SessionID: "one", Title: "untrusted citation", TranscriptURI: taskURI(project)})
	if err := DeleteTaskRecord(dir, "one", "1"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(project); err != nil || string(raw) != "keep" {
		t.Fatal("deletion followed an external citation")
	}
}

func TestDeleteConversationKeepsItsBorrowedWorkspace(t *testing.T) {
	dir, _ := deletionFixture(t)
	workspace := t.TempDir()
	file := filepath.Join(workspace, "keep.txt")
	if err := os.WriteFile(file, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta, _ := LoadMeta(dir)
	meta.Workspace = workspace
	if err := SaveMeta(dir, meta); err != nil {
		t.Fatal(err)
	}
	if err := DeleteConversation(dir, "one"); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(file); err != nil || string(raw) != "keep" {
		t.Fatal("borrowed workspace was deleted")
	}
}

func TestDeleteTaskRecordRemovesEarlierJournalsForTheSameTask(t *testing.T) {
	dir, index := deletionFixture(t)
	newer := filepath.Join(dir, "tasks", "1-retried.jsonl")
	if err := os.WriteFile(newer, []byte("retry"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendTaskIndex(index, TaskIndexEntry{ID: "1", SessionID: "one", Title: "retry", TranscriptURI: taskURI(newer)})
	if err := DeleteTaskRecord(dir, "one", "1"); err != nil {
		t.Fatal(err)
	}
	for _, file := range []string{newer, filepath.Join(dir, "tasks", "1.jsonl")} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("old task journal remains: %s", file)
		}
	}
}

func TestDeleteTaskRecordKeepsConcurrentAppends(t *testing.T) {
	dir, index := deletionFixture(t)
	var writers sync.WaitGroup
	for i := 0; i < 24; i++ {
		writers.Add(1)
		go func(i int) {
			defer writers.Done()
			appendTaskIndex(index, TaskIndexEntry{SessionID: "two", ID: "new-" + strconv.Itoa(i), Title: "concurrent"})
			appendTaskIndex(index, TaskIndexEntry{SessionID: "one", ID: "1", Title: "late completion"})
		}(i)
	}
	if err := DeleteTaskRecord(dir, "one", "1"); err != nil {
		t.Fatal(err)
	}
	writers.Wait()
	rows := ReadTaskIndex(index)
	if len(rows) != 27 {
		t.Fatalf("concurrent deletion lost or restored records: %d", len(rows))
	}
	for _, row := range rows {
		if row.SessionID == "one" && row.ID == "1" {
			t.Fatal("deleted task returned")
		}
	}
}
