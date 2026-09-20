package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/plan"
	"github.com/Agent-Field/codeaf/internal/resident"
	"github.com/Agent-Field/codeaf/internal/store"
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

// TestTheFaultLogStaysUnderTheProfileTheTestChose is the incident, held down.
// reportFault appends the fault to config.ProfilePath(config.ProfileDir(),
// "chat.log"), and the harness that runs this suite exports CODEAF_PROFILE_DIR
// at a live profile — so a test that moved HOME and CODEAF_HOME but not the
// profile wrote its fixture into somebody's real chat.log, where it was read as
// a genuine crash.
//
// The assertion is on WHERE the file landed and never on a byte of its text, and
// it reads only directories this test owns. Every variable that decides where
// the write goes is named here, so no inherited CODEAF_PROFILE_DIR can reach it.
func TestTheFaultLogStaysUnderTheProfileTheTestChose(t *testing.T) {
	login := t.TempDir()
	profile := filepath.Join(t.TempDir(), "profile")
	t.Setenv("HOME", login)
	t.Setenv(home.EnvVar, filepath.Join(login, ".codeaf"))
	// The profile the test chose, so the log can only land in a directory it
	// owns — never the live one an inherited CODEAF_PROFILE_DIR would name.
	t.Setenv(config.ProfileDirEnv, profile)

	reportFault(&bytes.Buffer{}, "runtime error: slice bounds out of range [:-1]",
		[]byte("goroutine 1 [running]:\nmain.runDo(...)\n"))

	if _, err := os.Stat(filepath.Join(profile, "chat.log")); err != nil {
		t.Fatalf("the fault log is not under the profile the test chose: %v", err)
	}
	if _, err := os.Stat(filepath.Join(login, ".codeaf", "chat.log")); !os.IsNotExist(err) {
		t.Fatalf("the fault log also landed under the state root: %v", err)
	}
}

// TestReportFaultSaysOneCalmThingAndLogsTheStack holds the promise the user
// reads when nothing else worked.
func TestReportFaultSaysOneCalmThingAndLogsTheStack(t *testing.T) {
	login := t.TempDir()
	t.Setenv("HOME", login)
	// AND WHERE THE STATE ROOT IS, said rather than assumed. CODEAF_HOME moves
	// the whole of it (internal/home), so a test that pins a file's path by
	// moving HOME alone is reading whatever the environment happened to say —
	// which is a pass or a failure depending on whose machine it runs on.
	t.Setenv(home.EnvVar, filepath.Join(login, ".codeaf"))
	// AND WHERE THE PROFILE'S LOG IS, for the same reason and by the same road:
	// reportFault appends through config.ProfilePath(config.ProfileDir(),
	// "chat.log"), and an exported CODEAF_PROFILE_DIR — which the harness that
	// runs this suite sets at a live profile — wins over both roots above and
	// would send the fixture into somebody's real chat.log.
	t.Setenv(config.ProfileDirEnv, "")

	stderr := &bytes.Buffer{}
	code := reportFault(stderr, "runtime error: slice bounds out of range [:-1]",
		[]byte("goroutine 1 [running]:\nmain.runDo(...)\n"))

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	shown := stderr.String()
	if strings.Contains(shown, "goroutine") || strings.Contains(shown, "slice bounds") {
		t.Fatalf("the stack reached the user's screen: %q", shown)
	}
	for _, phrase := range []string{
		"codeaf hit an internal fault and had to stop",
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

	payload, err := os.ReadFile(filepath.Join(login, ".codeaf", "chat.log"))
	if err != nil {
		t.Fatalf("read the log: %v", err)
	}
	written := string(payload)
	if !strings.Contains(written, "slice bounds out of range") ||
		!strings.Contains(written, "goroutine 1 [running]") {
		t.Fatalf("the log did not get the detail: %q", written)
	}
}
