package session

// Two aforge windows, one workspace: the job-log half of it.

import (
	"path/filepath"
	"testing"
)

// THE DEFECT: job ids come from an in-process counter that starts at zero in
// every window, and the log was opened with a create that truncates. Both
// windows wrote 1.log; the second one emptied the first one's file under it
// while the first kept writing at its old offset, and the person reading the log
// saw a hole followed by two runs mixed together.
func TestTwoSessionsDoNotTruncateEachOthersJobLogs(t *testing.T) {
	workspace := t.TempDir()
	first := newJobRegistry(workspace, Place{}, nil)
	second := newJobRegistry(workspace, Place{}, nil)

	one, err := first.newJob("the first window's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer one.sink.close()
	if _, err := one.sink.Write([]byte("the first window's output\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	two, err := second.newJob("the second window's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer two.sink.close()
	if two.logPath == one.logPath {
		t.Fatalf("both windows were given one log: %s", one.logPath)
	}
	if two.id == one.id {
		t.Fatalf("both windows were given job id %d", one.id)
	}
	if _, err := two.sink.Write([]byte("the second window's output\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	// The first window keeps writing, and its log still reads as one run.
	if _, err := one.sink.Write([]byte("and more of it\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := readFile(t, one.logPath); got != "the first window's output\nand more of it\n" {
		t.Fatalf("the first window's log reads %q", got)
	}
	if got := readFile(t, two.logPath); got != "the second window's output\n" {
		t.Fatalf("the second window's log reads %q", got)
	}
}

// A log left by a session that has closed is not a name this one may take
// either: the person can still open it, and `jobs output` is addressed by the
// number in it.
func TestAJobLogFromAnEarlierSessionIsNotReused(t *testing.T) {
	workspace := t.TempDir()
	directory := droppingsDir(Place{}, workspace, droppingJobs)
	writeFile(t, filepath.Join(directory, "1.log"), "yesterday's build\n")

	registry := newJobRegistry(workspace, Place{}, nil)
	fresh, err := registry.newJob("today's build", jobKindBash)
	if err != nil {
		t.Fatalf("newJob: %v", err)
	}
	defer fresh.sink.close()

	if fresh.id == 1 {
		t.Fatal("the new job took the id of a log that was already there")
	}
	if got := readFile(t, filepath.Join(directory, "1.log")); got != "yesterday's build\n" {
		t.Fatalf("yesterday's log reads %q", got)
	}
}
