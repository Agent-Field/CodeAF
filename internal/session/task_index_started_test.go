package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A ROW CARRIES THE NODE'S OWN START ACROSS THE FILE. A node restored from its
// checkpoint keeps the instant it began (task_store.go), so the row written for
// it says when the work started rather than when some window first met it —
// the defect docs/design/polish/audit-jobs.md row 2 describes.
func TestAnIndexRowCarriesTheNodesRecordedStart(t *testing.T) {
	started := time.Now().Add(-32 * time.Minute).Truncate(time.Millisecond)
	landed := started.Add(12 * time.Minute)
	node := restoreNode(newTaskGraph(), taskRecord{
		ID: 1, Title: "Rotate the staging certificate", Brief: "rotate it", Acceptance: "the certificate is current",
		State: TaskDone, ElapsedMS: (12 * time.Minute).Milliseconds(), StartedAt: started, EndedAt: landed,
	})
	node.graph.mu.Lock()
	entry := node.indexEntryLocked("aaaa1111aaaa1111")
	node.graph.mu.Unlock()
	if !entry.StartedAt.Equal(started) {
		t.Fatalf("the row started at %s, want the recorded %s", entry.StartedAt, started)
	}

	path := filepath.Join(t.TempDir(), taskIndexName)
	appendTaskIndex(path, entry)
	appendTaskIndex(path, TaskIndexEntry{ID: "2", Title: "Write the change entry", Status: string(TaskQueued), SessionID: "aaaa1111aaaa1111"})
	var read TaskIndexEntry
	for _, row := range ReadTaskIndex(path) {
		if row.ID == "1" {
			read = row
		}
	}
	if !read.StartedAt.Equal(started) {
		t.Fatalf("the start did not survive the file: %s", read.StartedAt)
	}
	// A QUEUED ROW HAS NO START, AND TAKES NO SPACE SAYING SO.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(string(raw)), "\n"); strings.Contains(lines[1], `"startedAt"`) {
		t.Fatalf("a queued row wrote a start it never had: %s", lines[1])
	}
}

// A LIVE ROW COUNTS UP FROM ITS START; A LANDED ONE KEEPS ITS FROZEN FIGURE. The
// age baked into a live row goes stale the moment it is cached, and the start
// does not — but a landed row with no landing instant must not count up from a
// start hours old as though it were still going.
func TestDurationCountsALiveRowFromItsStartAndKeepsALandedOnesFigure(t *testing.T) {
	started := time.Now().Add(-12 * time.Minute)
	live := TaskIndexEntry{Status: string(TaskRunning), StartedAt: started, DurationMS: 1000}
	if got := live.Duration(); got < 12*time.Minute || got > 13*time.Minute {
		t.Fatalf("a live row twelve minutes in says it has run %s", got)
	}
	landed := TaskIndexEntry{Status: string(TaskDone), StartedAt: started.Add(-2 * time.Hour), DurationMS: 720000}
	if got := landed.Duration(); got != 12*time.Minute {
		t.Fatalf("a landed row with no landing instant says it ran %s, want its frozen 12m", got)
	}
	older := TaskIndexEntry{Status: string(TaskRunning), DurationMS: 90000}
	if got := older.Duration(); got != 90*time.Second {
		t.Fatalf("a live row from a build with no start says %s, want its own 1m30s", got)
	}
}
