package session

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"os"
	"path/filepath"
	"testing"
)

func taskDeleteFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root, profile, file := deletionFixture(t)
	doc := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 4}
	for n := uint64(1); n <= 4; n++ {
		parent := uint64(0)
		if n == 2 {
			parent = 1
		}
		if n == 3 {
			parent = 2
		}
		journal := filepath.Join(filepath.Dir(file), "tasks", taskIndexParent(n)+".jsonl")
		if err := os.MkdirAll(filepath.Dir(journal), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(journal, []byte("private task history"), 0600); err != nil {
			t.Fatal(err)
		}
		row := TaskIndexEntry{ID: taskIndexParent(n), Parent: taskIndexParent(parent), Title: "work", SessionID: "conversation", Status: string(TaskDone), TranscriptURI: taskURI(journal)}
		appendTaskIndex(TaskIndexPath(file), row)
		record := taskRecord{ID: n, Parent: parent, Title: "work", Brief: "do work", Acceptance: "done", State: TaskDone, Journal: journal}
		if n == 4 {
			record.DependsOn = []uint64{1}
		}
		doc.Nodes = append(doc.Nodes, record)
	}
	writeCheckpoint(t, taskCheckpointPath(file), doc)
	appendTaskIndex(TaskIndexPath(file), TaskIndexEntry{ID: "1", SessionID: "other", Title: "other conversation", Status: string(TaskDone)})
	return root, profile, file
}

func TestTaskDeleteSubtreePreservesSiblingsAndOtherOwnersAfterRestart(t *testing.T) {
	root, _, file := taskDeleteFixture(t)
	if err := DeleteTaskUnder(root, file, "1", nil); err != nil {
		t.Fatal(err)
	}
	rows := ReadTaskIndex(TaskIndexPath(file))
	if len(rows) != 2 {
		t.Fatalf("kept rows: %+v", rows)
	}
	for _, r := range rows {
		if r.SessionID == "conversation" && r.ID != "4" {
			t.Fatal("descendant returned")
		}
	}
	raw, err := os.ReadFile(taskCheckpointPath(file))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := decodeTasks(raw)
	if err != nil || len(doc.Nodes) != 1 || doc.Nodes[0].ID != 4 {
		t.Fatalf("surviving checkpoint invalid: %+v %v", doc, err)
	}
	if len(doc.Nodes[0].DependsOn) != 0 {
		t.Fatal("deleted dependency survived")
	}
	for _, id := range []string{"1", "2", "3"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(file), "tasks", id+".jsonl")); !os.IsNotExist(err) {
			t.Fatalf("journal %s retained: %v", id, err)
		}
		appendTaskIndex(TaskIndexPath(file), TaskIndexEntry{ID: id, SessionID: "conversation", Title: "late update"})
	}
	if len(ReadTaskIndex(TaskIndexPath(file))) != 2 {
		t.Fatal("late landing resurrected deleted work")
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal("task deletion removed conversation")
	}
}

func TestTaskDeleteOfflineBlocksUnfinishedDependentsAfterRestart(t *testing.T) {
	root, _, file := taskDeleteFixture(t)
	doc, ok := loadTaskCheckpoint(taskCheckpointPath(file))
	if !ok {
		t.Fatal("missing fixture checkpoint")
	}
	doc.Nodes[3].State = TaskQueued
	doc.Nodes = append(doc.Nodes,
		taskRecord{ID: 5, Title: "downstream", Brief: "do work", Acceptance: "done", State: TaskQueued, DependsOn: []uint64{4}},
		taskRecord{ID: 6, Title: "independent", Brief: "do work", Acceptance: "done", State: TaskQueued},
	)
	doc.Seq = 6
	writeCheckpoint(t, taskCheckpointPath(file), doc)
	for _, id := range []string{"4", "5", "6"} {
		appendTaskIndex(TaskIndexPath(file), TaskIndexEntry{ID: id, SessionID: "conversation", Status: string(TaskQueued)})
	}
	if err := DeleteTaskUnder(root, file, "1", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(taskCheckpointPath(file))
	if err != nil {
		t.Fatal(err)
	}
	restored, err := decodeTasks(raw)
	if err != nil || len(restored.Nodes) != 3 {
		t.Fatalf("restart lost siblings: %+v %v", restored, err)
	}
	for _, r := range restored.Nodes {
		if r.ID == 6 {
			if r.State != TaskQueued {
				t.Fatal("independent work was changed")
			}
		} else if r.State != TaskFailed || r.Ending != TaskEndingUpstream || r.Report != "dependency was deleted" {
			t.Fatalf("dependent can run without its prerequisite: %+v", r)
		}
	}
	for _, r := range ReadTaskIndex(TaskIndexPath(file)) {
		if r.SessionID == "conversation" && (r.ID == "4" || r.ID == "5") && r.Status != string(TaskFailed) {
			t.Fatalf("index disagrees with checkpoint: %+v", r)
		}
	}
}

func TestTaskDeleteLeafAndConversationHaveDifferentScopes(t *testing.T) {
	root, profile, file := taskDeleteFixture(t)
	if err := DeleteTaskUnder(root, file, "3", nil); err != nil {
		t.Fatal(err)
	}
	if len(ReadTaskIndex(TaskIndexPath(file))) != 4 {
		t.Fatal("leaf deletion removed parent or siblings")
	}
	if err := DeleteConversationUnder(root, profile, file, nil, nil); err != nil {
		t.Fatal(err)
	}
	rows := ReadTaskIndex(TaskIndexPath(file))
	if len(rows) != 1 || rows[0].SessionID != "other" {
		t.Fatalf("conversation tasks survived: %+v", rows)
	}
	if _, err := os.Stat(taskCheckpointPath(file)); !os.IsNotExist(err) {
		t.Fatal("conversation checkpoint retained")
	}
}

func TestTaskDeletePlanContainmentKeepsDependentSiblingAndCanDeleteRoot(t *testing.T) {
	root, _, file := deletionFixture(t)
	path := filepath.Join(filepath.Dir(file), planStoreFilename)
	store, err := plandb.Open(path, "project", "run", "run", "run brief", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AddMany([]plandb.TaskSpec{{ID: "alpha", Title: "alpha", Description: "a"}, {ID: "child", ParentID: "alpha", Title: "child", Description: "c"}, {ID: "beta", Title: "beta", Description: "b", Dependencies: []plandb.Dependency{{TaskID: "alpha", Kind: plandb.DepBlocks}}}})
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	if err = DeleteTaskUnder(root, file, "t-alpha", nil); err != nil {
		t.Fatal(err)
	}
	store, err = plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if store.Task("alpha") != nil || store.Task("child") != nil || store.Task("beta") == nil {
		t.Fatal("plan deletion did not preserve its sibling")
	}
	store.Close()
	if err = DeleteTaskUnder(root, file, "t-run", nil); err != nil {
		t.Fatal(err)
	}
	store, err = plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if len(store.Tasks()) != 0 {
		t.Fatal("deleted root returned after reopen")
	}
}

func TestTaskDeleteRefusesCorruptTombstonesBeforeChangingWork(t *testing.T) {
	root, _, file := taskDeleteFixture(t)
	marker := filepath.Join(filepath.Dir(file), taskDeletedFile)
	os.WriteFile(marker, []byte("broken"), 0600)
	if err := DeleteTaskUnder(root, file, "1", nil); err == nil {
		t.Fatal("corrupt earlier deletions overwritten")
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != "broken" {
		t.Fatal("marker changed")
	}
}

func TestTaskDeleteDocumentFiltersLateChildrenBeforeDecoding(t *testing.T) {
	doc := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 2, Nodes: []taskRecord{{ID: 1, Title: "parent", Brief: "b", Acceptance: "a", State: TaskDone}, {ID: 2, Parent: 1, Title: "late child", Brief: "b", Acceptance: "a", State: TaskDone}}}
	raw, _ := json.Marshal(filterDeletedDocument(doc, map[string]bool{"1": true}))
	got, err := decodeTasks(raw)
	if err != nil || len(got.Nodes) != 0 {
		t.Fatalf("late child returned: %+v %v", got, err)
	}
}

func TestTaskDeleteArchivedPlanRecordsAndAliasesAreIncluded(t *testing.T) {
	root, profile, file := deletionFixture(t)
	path := filepath.Join(filepath.Dir(file), planStoreFilename)
	store, err := plandb.Open(path, "project", "run", "run", "brief", "conversation")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.AddMany([]plandb.TaskSpec{{ID: "alpha", Title: "alpha", Description: "a"}, {ID: "child", ParentID: "alpha", Title: "child", Description: "c"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.Cancel("alpha", "finished"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.Archive(0); err != nil {
		t.Fatal(err)
	}
	archived, err := store.Archived()
	if err != nil || len(archived) != 2 {
		t.Fatalf("fixture archive: %+v %v", archived, err)
	}
	store.Close()
	for _, id := range []string{"alpha", "child"} {
		dir := plandb.TaskDir(filepath.Dir(file), id)
		os.MkdirAll(dir, 0700)
		os.WriteFile(filepath.Join(dir, "trajectory.jsonl"), []byte("history"), 0600)
	}
	if err = DeleteConversationUnder(root, profile, file, nil, nil); err != nil {
		t.Fatal(err)
	}
	store, err = plandb.Open(path, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	archived, err = store.Archived()
	if err != nil || len(archived) != 0 {
		t.Fatalf("deleted archive survived: %+v %v", archived, err)
	}
	for _, id := range []string{"alpha", "child"} {
		if _, err = os.Stat(plandb.TaskDir(filepath.Dir(file), id)); !os.IsNotExist(err) {
			t.Fatal("archived task record survived")
		}
	}
}

func TestTaskDeleteAliasesExpandToAFixedPoint(t *testing.T) {
	_, _, file := deletionFixture(t)
	doc := taskDocument{Type: taskDocumentType, Version: taskFileVersion, Seq: 3, Runs: []runRecord{{ID: 1, State: TaskDone, PlanTask: "t-run"}, {ID: 2, Parent: 1, State: TaskDone, PlanTask: "t-alpha"}, {ID: 3, Parent: 1, State: TaskDone, PlanTask: "t-child"}}}
	writeCheckpoint(t, taskCheckpointPath(file), doc)
	rows := []TaskIndexEntry{{ID: "2", SessionID: "conversation", Parent: "1"}, {ID: "t-child", SessionID: "conversation", Parent: "t-alpha"}, {ID: "3", SessionID: "conversation", Parent: "1"}}
	ids := map[string]bool{"2": true}
	expandTaskDeleteScope(file, "conversation", nil, ids, rows)
	if !ids["3"] || ids["1"] || ids["t-run"] {
		t.Fatalf("wrong alias scope: %+v", ids)
	}
}

func TestTaskDeletePlanTombstonesNeverBlockNewGraphRoots(t *testing.T) {
	_, _, file := deletionFixture(t)
	os.WriteFile(filepath.Join(filepath.Dir(file), taskDeletedFile), []byte(`{"t-alpha":true}`), 0600)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) { c.SessionFile = file })
	g := agent.graph()
	agent.markDeletingTasks(map[string]bool{"t-beta": true})
	if g.deleted[0] {
		t.Fatal("a plan id poisoned numeric root zero")
	}
}

func TestConversationDeleteCanRetryCleanupAfterJournalMoved(t *testing.T) {
	root, profile, file := taskDeleteFixture(t)
	rows, err := taskDeleteRows(file, "conversation", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = saveConversationTaskCleanup(file, rows); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(filepath.Dir(file), conversationDeletedFile), []byte("deleted"), 0600)
	if err = os.Rename(file, file+".delete-pending"); err != nil {
		t.Fatal(err)
	}
	if err = DeleteConversationUnder(root, profile, file, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(file + ".delete-pending"); !os.IsNotExist(err) {
		t.Fatal("pending transcript survived retry")
	}
	if len(ReadTaskIndex(TaskIndexPath(file))) != 1 {
		t.Fatal("task cleanup did not complete")
	}
}
