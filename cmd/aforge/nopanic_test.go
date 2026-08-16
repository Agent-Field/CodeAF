package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// TestPlanProgressPosterSurvivesAZeroConstruction is the second bug from the
// incident, held down: every bookkeeping map on the poster is filled on use,
// so a caller that assembles the struct directly cannot write to a nil one.
func TestPlanProgressPosterSurvivesAZeroConstruction(t *testing.T) {
	history, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer history.Close()
	if err := history.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{
		ID: "job", Brief: "do the work", Stage: 1,
	}}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "do the work"}); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(100, 0)
	poster := &planProgressPoster{
		history:  history,
		anchor:   resident.PlanAnchor{NodeID: "job", SessionID: "s1"},
		interval: planCountThrottle,
		now:      func() time.Time { return now },
	}
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 1, Total: 5})
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 2, Total: 5})
	now = now.Add(planCountThrottle)
	poster.report(plan.ProgressUpdate{Phase: "writing the plan", Done: 5, Total: 5})
	poster.flushCount("writing the plan")

	messages, err := history.NodeMessages("job", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) == 0 {
		t.Fatal("a zero-constructed poster posted nothing")
	}
}

// TestReportFaultSaysOneCalmThingAndLogsTheStack holds the promise the user
// reads when nothing else worked.
func TestReportFaultSaysOneCalmThingAndLogsTheStack(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	stderr := &bytes.Buffer{}
	code := reportFault(stderr, "runtime error: slice bounds out of range [:-1]",
		[]byte("goroutine 1 [running]:\nmain.runChat(...)\n"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	shown := stderr.String()
	if strings.Contains(shown, "goroutine") || strings.Contains(shown, "slice bounds") {
		t.Fatalf("the stack reached the user's screen: %q", shown)
	}
	for _, phrase := range []string{
		"aforge hit an internal fault and had to stop",
		"Nothing is lost",
		"restarting resumes where it left off",
		"chat.log",
	} {
		if !strings.Contains(shown, phrase) {
			t.Fatalf("the calm block is missing %q: %q", phrase, shown)
		}
	}
	if lines := strings.Count(strings.TrimSpace(shown), "\n"); lines != 0 {
		t.Fatalf("the calm block is more than one block: %q", shown)
	}

	payload, err := os.ReadFile(filepath.Join(home, ".aforge", "chat.log"))
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	written := string(payload)
	if !strings.Contains(written, "slice bounds out of range") ||
		!strings.Contains(written, "goroutine 1 [running]") {
		t.Fatalf("the log did not get the detail: %q", written)
	}
}
