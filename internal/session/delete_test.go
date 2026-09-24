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
			appendTaskIndex(filepath.Join(bucket, taskIndexName), TaskIndexEntry{ID: task, SessionID: id, Status: string(TaskDone), Title: task, TranscriptURI: taskURI(journal)})
		}
	}
	return filepath.Join(bucket, "one"), filepath.Join(bucket, taskIndexName)
}

func TestDeleteTaskRecordPermanentlyRemovesOnlyItsRecord(t *testing.T) {
	dir, index := deletionFixture(t)
	if err := DeleteTaskRecord(dir, "one", "1"); err != nil {
		t.Fatal(err)
	}
	appendTaskIndex(index, TaskIndexEntry{ID: "1", SessionID: "one", Status: string(TaskDone), Title: "late completion"})
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
	appendTaskIndex(index, TaskIndexEntry{ID: "1", SessionID: "one", Status: string(TaskDone), Title: "untrusted citation", TranscriptURI: taskURI(project)})
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
	appendTaskIndex(index, TaskIndexEntry{ID: "1", SessionID: "one", Status: string(TaskDone), Title: "retry", TranscriptURI: taskURI(newer)})
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
			appendTaskIndex(index, TaskIndexEntry{SessionID: "two", ID: "new-" + strconv.Itoa(i), Status: string(TaskDone), Title: "concurrent"})
			appendTaskIndex(index, TaskIndexEntry{SessionID: "one", ID: "1", Status: string(TaskDone), Title: "late completion"})
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

func TestDeleteTaskTreeRejectsActiveDescendantsAndDeletesOnlyItsSubtree(t *testing.T) {
	dir, index := deletionFixture(t)
	for _, state := range []string{string(TaskRunning), string(TaskQueued), "paused", "pending", ""} {
		appendTaskIndex(index, TaskIndexEntry{ID: "child", Parent: "1", SessionID: "one", Title: "Child", Status: state})
		if err := DeleteTaskRecord(dir, "one", "1"); err == nil {
			t.Fatalf("deleted subtree with %q child", state)
		}
		meta, _ := LoadMeta(dir)
		if meta.DeletedTasks["1"] {
			t.Fatal("refused deletion published a tombstone")
		}
	}
	appendTaskIndex(index, TaskIndexEntry{ID: "child", Parent: "1", SessionID: "one", Title: "Child", Status: string(TaskDone)})
	appendTaskIndex(index, TaskIndexEntry{ID: "grandchild", Parent: "child", SessionID: "one", Title: "Grandchild", Status: string(TaskFailed)})
	ids, err := DeleteTaskTree(dir, "one", "1", nil)
	if err != nil || len(ids) != 3 {
		t.Fatalf("subtree=%v err=%v", ids, err)
	}
	appendTaskIndex(index, TaskIndexEntry{ID: "late-child", Parent: "1", SessionID: "one", Title: "late", Status: string(TaskDone)})
	for _, row := range ReadTaskIndex(index) {
		if row.SessionID == "one" && row.ID != "2" {
			t.Fatalf("deleted descendant survived: %+v", row)
		}
	}
	meta, _ := LoadMeta(dir)
	for _, id := range []string{"1", "child", "grandchild"} {
		if !meta.DeletedTasks[id] {
			t.Fatalf("missing tombstone %s", id)
		}
	}
	if meta.DeletedTasks["2"] {
		t.Fatal("sibling deleted")
	}
	if _, err := os.Stat(filepath.Join(dir, "transcript.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteTaskTreeRechecksDiskAndLiveWork(t *testing.T) {
	for _, where := range []string{"disk", "live"} {
		t.Run(where, func(t *testing.T) {
			dir, index := deletionFixture(t)
			stale := TaskIndexEntry{ID: "1", SessionID: "one", Title: "Root", Status: string(TaskDone)}
			active := TaskIndexEntry{ID: "child", Parent: "1", SessionID: "one", Title: "Child", Status: string(TaskRunning)}
			rows := []TaskIndexEntry{stale}
			if where == "disk" {
				appendTaskIndex(index, active)
			} else {
				rows = append(rows, active)
			}
			if _, err := DeleteTaskTree(dir, "one", "1", rows); err == nil {
				t.Fatal("stale confirmation deleted active work")
			}
		})
	}
}

func TestDeleteTaskTreeHasNoSearchIndexLimit(t *testing.T) {
	dir, index := deletionFixture(t)
	for i := 0; i < taskIndexRows+1; i++ {
		appendTaskIndex(index, TaskIndexEntry{ID: strconv.Itoa(i + 10), Parent: "1", SessionID: "one", Title: "child", Status: string(TaskDone)})
	}
	ids, err := DeleteTaskTree(dir, "one", "1", nil)
	if err != nil || len(ids) != taskIndexRows+2 {
		t.Fatalf("deleted=%d err=%v", len(ids), err)
	}
	if len(ReadTaskIndex(index)) != 3 {
		t.Fatal("old descendants escaped deletion")
	}
}

func TestDeleteTaskTreeIncludesPlanIdentityAndDescendants(t *testing.T) {
	dir, index := deletionFixture(t)
	appendTaskIndex(index, TaskIndexEntry{ID: "1", PlanID: "t-root", SessionID: "one", Title: "Root", Status: string(TaskDone)})
	live := []TaskIndexEntry{
		{ID: "t-root", SessionID: "one", Title: "Renamed root", Status: "done"},
		{ID: "t-child", Parent: "t-root", SessionID: "one", Title: "Child", Status: "done"},
	}
	ids, err := DeleteTaskTree(dir, "one", "1", live)
	if err != nil || len(ids) != 3 {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	meta, _ := LoadMeta(dir)
	if !meta.DeletedTasks["t-child"] || !meta.DeletedTasks["t-root"] {
		t.Fatal("plan copy survived")
	}
}
