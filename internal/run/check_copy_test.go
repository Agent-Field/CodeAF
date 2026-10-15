package run_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/run"
)

// TestCheckWorkerIsToldTheWorkIsInItsOwnCopy is the fresh-install checker that
// began with `cd /tmp/fxfresh/repo`: nothing in its opening said where the work
// was, so it decoded the person's checkout out of the project folder's name,
// read that tree's unrelated diff, and was moved home only when a write there
// was refused. The opening a check really gets — built by the run's own worker
// seat, from a check the review round's store door seated — must say the work
// is in its own working directory and that the person's checkout is not it.
func TestCheckWorkerIsToldTheWorkIsInItsOwnCopy(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PLANDB_BIN", stubCLI(t))
	store := runOpenStore(t)
	copyDir := filepath.Join(t.TempDir(), "h", ".codeaf", "v3", "projects", "-tmp-x-repo", "abc", "trees", "2")
	if err := os.MkdirAll(copyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	check, err := store.AddReviewCheck(plandb.TaskSpec{
		ID:          "chk",
		Title:       "check: fix the bug",
		Description: "Acceptance: fix the bug that makes test_calc.py fail\n\nResult: one-line fix in calc.py",
		Role:        plandb.RoleCheck,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The opening is all this test reads, so the run is cut the moment the
	// first request carrying it has been recorded.
	ctx, cut := context.WithCancel(runContext(t))
	defer cut()
	seat := &seat{script: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		cut()
		return textReply("done"), nil
	}}}
	_, _ = run.NewBashWorker(store, copyDir, "test/model", seat).Run(run.WithStepsPerTask(ctx, 2), *check)

	opening := seat.opening(t)
	for _, want := range []string{
		"The work is in your working directory",
		"run every check and probe there",
		"The person's own checkout is not the work",
	} {
		if !strings.Contains(opening, want) {
			t.Errorf("the check's opening does not say %q:\n%s", want, opening)
		}
	}
	if strings.Contains(opening, "/tmp/x/repo") {
		t.Errorf("the check's opening names the person's checkout as a place:\n%s", opening)
	}
}
