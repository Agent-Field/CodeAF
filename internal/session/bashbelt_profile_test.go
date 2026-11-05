package session

import (
	"context"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBeltWorkerKeepsProfileReadFailure(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, filepath.Join(t.TempDir(), "stub-codeaf"))
	profile := t.TempDir()
	if err := os.Mkdir(filepath.Join(profile, "config.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, readErr := config.ReadProfileApprovalConfig(profile)
	if readErr == nil {
		t.Fatal("expected read failure")
	}
	dir := t.TempDir()
	store, err := plandb.Open(filepath.Join(dir, planStoreFilename), "the work", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	script := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("attempt", "bash", `{"command":"printf WRITTEN > denied.txt"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
	}}
	cfg := Config{Workspace: workspace, Model: "test/model", System: "SYSTEM", ApprovalPolicy: &approval.Policy{ProfileReadError: readErr.Error()}}
	child, err := NewBeltWorker(cfg, script, store.Task(planRootID), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close() })
	events := collect(t, mustSubmit(t, child, "write"))
	failed, ok := firstOfKind(events, EventToolFailed)
	_, statErr := os.Stat(filepath.Join(workspace, "denied.txt"))
	t.Logf("belt refusal=%v reason=%q file_exists=%v", ok, failed.Output, statErr == nil)
	if !ok || !strings.Contains(failed.Output, readErr.Error()) || !os.IsNotExist(statErr) {
		t.Fatal("belt worker lost unreadable signal")
	}
}
