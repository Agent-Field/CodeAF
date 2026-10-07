package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestDeletionPreflightRefusesSiblingTaskFolderSymlinksBeforeMutation(t *testing.T) {
	root, profile, file := deletionFixture(t)
	path := filepath.Join(filepath.Dir(file), planStoreFilename)
	db, err := plandb.Open(path, "project", "run", "run", "brief", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AddMany([]plandb.TaskSpec{{ID: "alpha", Title: "alpha", Description: "a"}, {ID: "beta", Title: "beta", Description: "b"}})
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	beta := plandb.TaskDir(filepath.Dir(file), "beta")
	if err = os.MkdirAll(beta, 0700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(beta, "trajectory.jsonl")
	if err = os.WriteFile(record, []byte("sibling history"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(beta, plandb.TaskDir(filepath.Dir(file), "alpha")); err != nil {
		t.Fatal(err)
	}
	for _, delete := range []func() error{func() error { return DeleteTaskUnder(root, file, "t-alpha", nil) }, func() error { return DeleteConversationUnder(root, profile, file, nil, nil) }} {
		if err = delete(); err == nil {
			t.Fatal("symlinked deletion accepted")
		}
		if raw, e := os.ReadFile(record); e != nil || string(raw) != "sibling history" {
			t.Fatal("sibling record changed")
		}
		if _, e := os.Stat(file); e != nil {
			t.Fatal("refusal hid transcript")
		}
		if _, e := os.Stat(filepath.Join(filepath.Dir(file), conversationDeletedFile)); !os.IsNotExist(e) {
			t.Fatal("refusal committed tombstone")
		}
		db, e := plandb.Open(path, "", "", "", "", "")
		if e != nil {
			t.Fatal(e)
		}
		if db.Task("alpha") == nil || db.Task("beta") == nil {
			t.Fatal("preflight already deleted store rows")
		}
		db.Close()
	}
	f, _ := teams.Load(profile)
	if !f.Teams[0].Holds(file) {
		t.Fatal("refusal removed membership")
	}
}

func TestTaskDeletionRefusesSiblingJournalSymlinksBeforeTombstones(t *testing.T) {
	root, _, file := taskDeleteFixture(t)
	one := filepath.Join(filepath.Dir(file), "tasks", "1.jsonl")
	four := filepath.Join(filepath.Dir(file), "tasks", "4.jsonl")
	if err := os.Remove(one); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(four, one); err != nil {
		t.Fatal(err)
	}
	if err := DeleteTaskUnder(root, file, "1", nil); err == nil {
		t.Fatal("sibling journal deletion accepted")
	}
	if _, err := os.Stat(four); err != nil {
		t.Fatal("sibling journal removed")
	}
	deleted, err := readTaskDeletions(file)
	if err != nil || len(deleted) != 0 {
		t.Fatalf("refusal changed tombstones: %+v %v", deleted, err)
	}
}

func TestConversationDeletionRecoveryKeepsFailedCleanupReachable(t *testing.T) {
	root, profile, file := deletionFixture(t)
	if err := saveConversationTaskCleanup(file, nil); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(file)
	if err := os.WriteFile(filepath.Join(dir, conversationDeletedFile), []byte("deleted"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file, file+".delete-pending"); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "keep"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	tasks := filepath.Join(dir, "tasks")
	if err := os.Symlink(outside, tasks); err != nil {
		t.Fatal(err)
	}
	if err := RecoverConversationDeletions(root, profile); err == nil {
		t.Fatal("unsafe pending cleanup accepted")
	}
	row, ok := readSessionRow(dir, "conversation", time.Time{})
	if !ok || !row.DeletionPending || row.Transcript != file || !strings.Contains(row.Title, "deletion incomplete") {
		t.Fatalf("retry disappeared: %+v %v", row, ok)
	}
	if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
		t.Fatal("recovery followed outside symlink")
	}
	if err := os.Remove(file + ".delete-pending"); err != nil {
		t.Fatal(err)
	}
	if row, ok := readSessionRow(dir, "conversation", time.Time{}); !ok || !row.DeletionPending {
		t.Fatal("late cleanup failure hid retry after journal removal")
	}

	if err := os.Remove(tasks); err != nil {
		t.Fatal(err)
	}
	if err := RecoverConversationDeletions(root, profile); err != nil {
		t.Fatal(err)
	}
	if _, ok := readSessionRow(dir, "conversation", time.Time{}); ok {
		t.Fatal("completed deletion remains visible")
	}
	if _, err := os.Stat(file + ".delete-pending"); !os.IsNotExist(err) {
		t.Fatal("pending journal survived recovery")
	}
	if _, err := os.Stat(filepath.Join(dir, conversationTaskCleanupFile)); !os.IsNotExist(err) {
		t.Fatal("cleanup receipt survived")
	}
	f, _ := teams.Load(profile)
	if f.Teams[0].Holds(file) {
		t.Fatal("recovery retained membership")
	}
}

func TestConversationDeletionJoinRetainsLockAndCompletionAcrossTimeoutRetry(t *testing.T) {
	_, _, file := deletionFixture(t)
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file })
	done := make(chan struct{})
	a.beltRun = &beltRun{over: done}
	if err := a.closeForDeletion(0); err == nil {
		t.Fatal("unfinished run reported stopped")
	}
	if !InUse(file) {
		t.Fatal("timeout released journal while run may write")
	}
	a.beltMu.Lock()
	a.beltRun = nil
	a.beltMu.Unlock()
	if err := a.closeForDeletion(0); err == nil {
		t.Fatal("lost run pointer bypassed completion join")
	}
	close(done)
	if err := a.CloseForDeletion(); err != nil {
		t.Fatal(err)
	}
	if InUse(file) {
		t.Fatal("settled deletion retained journal lock")
	}
}

func TestTaskDeletionRejectsOutsideSymlinkBackIntoSiblingJournal(t *testing.T) {
	root, _, file := taskDeleteFixture(t)
	outside := filepath.Join(t.TempDir(), "alias.jsonl")
	sibling := filepath.Join(filepath.Dir(file), "tasks", "4.jsonl")
	if err := os.Symlink(sibling, outside); err != nil {
		t.Fatal(err)
	}
	appendTaskIndex(TaskIndexPath(file), TaskIndexEntry{ID: "1", SessionID: "conversation", Title: "work", Status: string(TaskDone), TranscriptURI: taskURI(outside)})
	if err := DeleteTaskUnder(root, file, "1", nil); err == nil {
		t.Fatal("outside alias bypassed sibling protection")
	}
	if _, err := os.Stat(sibling); err != nil {
		t.Fatal("sibling journal was removed")
	}
}

func TestNormalCloseRetainsJournalUntilRunStopsWithoutWaitingForIt(t *testing.T) {
	root, profile, file := deletionFixture(t)
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file })
	done := make(chan struct{})
	a.beltRun = &beltRun{over: done}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if !InUse(file) {
		t.Fatal("normal close released journal before workers left")
	}
	if err := DeleteConversationUnder(root, profile, file, nil, func(string) error { return a.closeForDeletion(0) }); err == nil {
		t.Fatal("overlapping close bypassed worker join")
	}
	f, _ := teams.Load(profile)
	if !f.Teams[0].Holds(file) {
		t.Fatal("failed join removed membership")
	}
	close(done)
	if err := DeleteConversationUnder(root, profile, file, nil, func(string) error { return a.CloseForDeletion() }); err != nil {
		t.Fatal(err)
	}
}

func TestConversationDeletionPreservesSavedMemoriesAndTheirSourceIdentity(t *testing.T) {
	root, profile, file := deletionFixture(t)
	brain, err := store.Open(filepath.Join(profile, "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer brain.Close()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file; c.Memory = brain })
	var memories []store.Memory
	for _, owner := range []string{store.OwnerUser, store.OwnerMachine, store.OwnerProject("project")} {
		m, err := brain.AddMemory(store.Memory{Type: store.MemoryFact, Owner: owner, Title: "Saved separately", Text: "The project uses an amber release key.", SourceSession: "conversation"})
		if err != nil {
			t.Fatal(err)
		}
		memories = append(memories, m)
	}
	if err := DeleteConversationUnder(root, profile, file, nil, func(string) error { return a.CloseForDeletion() }); err != nil {
		t.Fatal(err)
	}
	for _, m := range memories {
		rows, err := brain.GetMemories([]string{m.Owner}, []string{m.ID})
		if err != nil || len(rows) != 1 || rows[0].SourceSession != "conversation" {
			t.Fatalf("saved memory changed: %+v %v", rows, err)
		}
	}
}
