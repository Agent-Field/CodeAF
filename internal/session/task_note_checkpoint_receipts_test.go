package session

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Brief: direction written, then the page-brief write fails. Must wrap ErrBriefNotKept,
// must not read as a refusal (errNotWrittenDown) or unknown (ErrSendUnanswered), and
// the keyed direction must stay on disk and in the worker's queue.
func TestADeliveredBriefReportsWhenOnlyItsPageMetadataCouldNotBeKept(t *testing.T) {
	agent, id, _ := nodeWithNobodyIn(t)
	node := agent.taskNode(id)
	worker := workerIn(t, node)
	path := steerStore(t, node, filepath.Join(t.TempDir(), "tasks.json"))
	writes := 0
	node.graph.mu.Lock()
	node.graph.store.duringWrite = func() {
		writes++
		if writes == 2 {
			_ = os.Mkdir(path+".tmp", 0o700)
		}
	}
	node.graph.mu.Unlock()
	err := agent.AppTaskAction(AppTaskIntent{ID: strconv.FormatUint(id, 10), Action: "brief", Text: "new brief", ClientID: "b-1"})
	if !errors.Is(err, ErrBriefNotKept) {
		t.Fatalf("err=%v want ErrBriefNotKept (writes=%d)", err, writes)
	}
	if errors.Is(err, errNotWrittenDown) || errors.Is(err, ErrSendUnanswered) {
		t.Fatalf("brief-not-kept reads as refusal/unknown: %v", err)
	}
	if q := workerQueue(worker); len(q) == 0 || !strings.Contains(q[0], "new brief") {
		t.Fatalf("not delivered: %q", q)
	}
	for _, line := range workerQueue(worker) {
		if strings.Contains(line, "b-1") {
			t.Fatalf("client id reached the model: %q", line)
		}
	}
	_ = os.Remove(path + ".tmp")
	if keys := diskKeys(t, path, id); len(keys) != 1 || keys[0] != "b-1" {
		t.Fatalf("disk keys %q", keys)
	}
	row, _, _ := agent.AppTask(strconv.FormatUint(id, 10))
	if row.Brief == "new brief" {
		t.Fatalf("brief kept in memory despite failed write")
	}
	if len(row.Notes) != 1 || row.Notes[0].ClientID != "b-1" {
		t.Fatalf("page %+v", row.Notes)
	}
}

// Malformed / oversized / absent on-disk keys restore as unknown.
func TestARestoredTaskDirectionOnlyKeepsAValidNoteClientID(t *testing.T) {
	rec := &assignmentRecord{Directions: []directionRecord{
		{ID: 1, Words: "a", ClientID: "ok-1"},
		{ID: 2, Words: "b", ClientID: "bad\x01"},
		{ID: 3, Words: "c", ClientID: strings.Repeat("x", AppNoteClientIDMax+1)},
		{ID: 4, Words: "d", ClientID: "\xff\xfe"},
		{ID: 5, Words: "e"},
		{ID: 6, Words: "f", ClientID: strings.Repeat("y", AppNoteClientIDMax)},
	}}
	got := restoredAssignment(rec)
	want := []string{"ok-1", "", "", "", "", strings.Repeat("y", AppNoteClientIDMax)}
	if len(got.directions) != len(want) {
		t.Fatalf("restored %d directions", len(got.directions))
	}
	for i, w := range want {
		if got.directions[i].clientID != w {
			t.Fatalf("dir %d key %q want %q", i, got.directions[i].clientID, w)
		}
	}
}
