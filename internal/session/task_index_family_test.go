package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

func TestAdaptiveRunLandsAWholeFamilyInTheTaskIndex(t *testing.T) {
	var index string
	agent, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = filepath.Join(t.TempDir(), "session.jsonl")
		index = TaskIndexPath(config.SessionFile)
	})
	family := agent.newOrchestrateFamily("audit the pricing code", "", "run-7")
	family.upsert([]orchestrate.NodeStatus{
		{Node: orchestrate.Node{ID: "tariff", Goal: "read the tariff table"}, State: orchestrate.Done, Digest: "found regional prices"},
		{Node: orchestrate.Node{ID: "invoice", Goal: "read the invoice writer"}, State: orchestrate.Done, Digest: "found rounding"},
		{Node: orchestrate.Node{ID: "tests", Goal: "check the pricing tests"}, State: orchestrate.Failed, Err: "fixture missing"},
	})

	rows := ReadTaskIndex(index)
	if len(rows) != 4 {
		t.Fatalf("index has %d rows, want one run and three nodes: %+v (workspace %s)", len(rows), rows, workspace)
	}
	rootID := taskIndexParent(family.root)
	var roots, children int
	for _, row := range rows {
		if row.Parent == "" {
			roots++
			if row.ID != rootID || row.Title != "audit the pricing code" {
				t.Fatalf("root row is %+v", row)
			}
			continue
		}
		children++
		if row.Parent != rootID {
			t.Fatalf("node %s hangs under %q, want %q", row.ID, row.Parent, rootID)
		}
	}
	if roots != 1 || children != 3 {
		t.Fatalf("family has %d roots and %d children", roots, children)
	}
}

func TestTaskIndexRowsBeforeParentStillDecodeAsRoots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	old := `{"id":"4","name":"old-task","label":"Old task","title":"Old task","status":"done","endedAt":"2026-08-01T10:00:00Z","sessionId":"old"}` + "\n"
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := ReadTaskIndex(path)
	if len(rows) != 1 || rows[0].Parent != "" || rows[0].Title != "Old task" {
		t.Fatalf("old row did not decode as a root: %+v", rows)
	}
}

// THE FILE'S LAST LINE IS THE NODE'S PRESENT STATE, even when an earlier row
// has the clock a display sorter would otherwise trust. A live opening has no
// ending instant, so sorting before this choice would reopen work that the next
// line says is done; the converse pins the same rule for work still going.
func TestTheTaskIndexAnswersWithTheLastLineAboutANode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.jsonl")
	rows := strings.Join([]string{
		`{"id":"1","title":"settled run","status":"running","sessionId":"room-a","endedAt":"0001-01-01T00:00:00Z"}`,
		`{"id":"1","title":"settled run","status":"done","sessionId":"room-a","endedAt":"2026-08-25T12:00:00Z"}`,
		`{"id":"2","title":"live run","status":"done","sessionId":"room-a","endedAt":"2026-08-25T11:00:00Z"}`,
		`{"id":"2","title":"live run","status":"running","sessionId":"room-a","endedAt":"0001-01-01T00:00:00Z"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(rows), 0o600); err != nil {
		t.Fatal(err)
	}

	got := ReadTaskIndex(path)
	if len(got) != 2 {
		t.Fatalf("the index answered with %d rows, want one per node: %+v", len(got), got)
	}
	byID := map[string]TaskIndexEntry{}
	for _, row := range got {
		byID[row.ID] = row
	}
	if row := byID["1"]; row.Status != string(TaskDone) || row.EndedAt.IsZero() {
		t.Fatalf("the node whose last line landed answered as %+v", row)
	}
	if row := byID["2"]; row.Status != string(TaskRunning) || !row.EndedAt.IsZero() {
		t.Fatalf("the node whose last line is live answered as %+v", row)
	}
}

// A CLEANLY SETTLED RUN STAYS SETTLED WHEN ITS CONVERSATION REOPENS. The
// second agent runs the real reconciliation over the same journal and project
// index; if the undated opening wins the collapse, it appends an interrupted
// ending for work whose closing row already says done.
func TestASettledRunIsNotInterruptedWhenItsConversationReopens(t *testing.T) {
	dir := t.TempDir()
	journal := filepath.Join(dir, "session.jsonl")
	first, workspace := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.SessionFile = journal
	})
	family := first.newOrchestrateFamily("audit the pricing code", "", "run-7")
	family.settle(orchestrate.Snapshot{Done: true, Answer: "the prices are current"}, nil)
	if err := first.Close(); err != nil {
		t.Fatalf("close first agent: %v", err)
	}

	index := TaskIndexPath(journal)
	before := readTaskIndexFile(t, index)
	second, err := newAgent(Config{
		Workspace: workspace, Model: "test/model", System: "SYSTEM", SessionFile: journal,
	}, &scriptedCompleter{})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	after := readTaskIndexFile(t, index)
	if len(after) != len(before) {
		t.Fatalf("reopening appended %d rows to settled work: before %+v, after %+v", len(after)-len(before), before, after)
	}

	rows := ReadTaskIndex(index)
	if len(rows) != 1 || rows[0].Status != string(TaskDone) || rows[0].Outcome != "the prices are current" {
		t.Fatalf("the reopened index answers with %+v, want the clean closing row", rows)
	}
}

func TestTasksDrawsEveryFamilyCollapsedTheSameWay(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	rows := []TaskIndexEntry{
		{ID: "1", Title: "adaptive run", Name: "adaptive-run", Status: "running", SessionID: "s", TranscriptURI: "file:///runs/s/7", EndedAt: now},
		{ID: "2", Parent: "1", Title: "planner-cut node", Name: "planner-cut-node", Status: "done", SessionID: "s", EndedAt: now},
		{ID: "3", Parent: "1", Title: "another node", Name: "another-node", Status: "done", SessionID: "s", EndedAt: now},
		{ID: "4", Parent: "1", Title: "last node", Name: "last-node", Status: "done", SessionID: "s", EndedAt: now},
		{ID: "5", Title: "person task", Name: "person-task", Status: "done", SessionID: "s", TranscriptURI: "file:///tasks/5", EndedAt: now},
		{ID: "6", Parent: "5", Title: "person-cut piece", Name: "person-cut-piece", Status: "done", SessionID: "s", EndedAt: now},
	}
	text := taskRowsText(rows, "")
	for _, want := range []string{
		"… 3 nodes under it — tasks 1 for the family",
		"… 1 node under it — tasks 5 for the family",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("collapsed families are missing %q:\n%s", want, text)
		}
	}
	for _, hidden := range []string{"planner-cut node", "person-cut piece"} {
		if strings.Contains(text, hidden) {
			t.Fatalf("collapsed family exposed %q:\n%s", hidden, text)
		}
	}
}

func TestTasksQueryShowsMatchingChildUnderRootWithItsURI(t *testing.T) {
	now := time.Now().Add(-time.Minute)
	rows := []TaskIndexEntry{
		{ID: "10", Title: "audit pricing", Name: "audit-pricing", Status: "running", SessionID: "s", TranscriptURI: "file:///runs/s/7", EndedAt: now},
		{ID: "11", Parent: "10", Title: "read the tariff table", Name: "read-the-tariff-table", Status: "done", SessionID: "s", TranscriptURI: "file:///runs/s/7/tariff.jsonl", EndedAt: now},
		{ID: "12", Parent: "10", Title: "read invoices", Name: "read-invoices", Status: "done", SessionID: "s", EndedAt: now},
	}
	text := taskRowsText(rows, "tariff")
	for _, want := range []string{"audit pricing", "  read the tariff table · done", "transcript file:///runs/s/7/tariff.jsonl"} {
		if !strings.Contains(text, want) {
			t.Fatalf("queried family is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "read invoices") {
		t.Fatalf("query exposed an unmatched sibling:\n%s", text)
	}
}

func TestTaskMentionSearchIncludesAdaptiveChildren(t *testing.T) {
	rows := []TaskIndexEntry{
		{ID: "20", Title: "audit pricing", Name: "audit-pricing", SessionID: "s"},
		{ID: "21", Parent: "20", Title: "trace regional tariff fallback", Name: "trace-regional-tariff-fallback", SessionID: "s"},
	}
	hits := SearchTaskIndex(rows, "regional tariff", 10)
	if len(hits) != 1 || hits[0].ID != "21" {
		t.Fatalf("shared mention search did not complete the run node: %+v", hits)
	}
}
