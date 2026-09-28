package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/session"
)

func keptRunFolder(t *testing.T, stderr string) string {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if folder, ok := strings.CutPrefix(line, "record kept at "); ok {
			return folder
		}
	}
	t.Fatalf("no private run record named in stderr:\n%s", stderr)
	return ""
}

func TestDoRunRecordIsPrivateAndRemovedAfterSuccess(t *testing.T) {
	home := beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	workspace := beltRepoWorkspace(t)
	if err := os.WriteFile(filepath.Join(workspace, "already.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	err := doErrand(doRequest{
		task: "write out.txt", workspace: workspace, asJSON: true, timeout: 60 * time.Second,
		stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return finishingSeat(0) },
	})
	if err != nil {
		t.Fatalf("do errand: %v\n%s", err, stderr.String())
	}
	status := doGitIn(t, workspace, "status", "--porcelain", "--untracked-files=all")
	if strings.Contains(status, ".codeaf/") {
		t.Fatalf("run left a store in the repository:\n%s", status)
	}
	if status != "?? already.txt\n?? out.txt" {
		t.Fatalf("unexpected worktree status:\n%s", status)
	}
	stores, err := filepath.Glob(filepath.Join(home, "runs", "codeaf-do-*"))
	if err != nil || len(stores) != 0 {
		t.Fatalf("successful run left private stores: %v, %v", stores, err)
	}
	if strings.Contains(stderr.String(), "record kept at") {
		t.Fatalf("clean run named a kept record: %q", stderr.String())
	}
}

func TestDoRunRefusesFileDirectoryBeforeOpeningRecord(t *testing.T) {
	home := beltRunEnv(t)
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	seat := &beltSeat{}
	_, err := runErrand(doRequest{task: "work", workspace: file, timeout: time.Second,
		newBeltCompleter: func(string) session.Completer { return seat }}, stubErrandSeats())
	if err == nil || !strings.Contains(err.Error(), file) {
		t.Fatalf("bad --dir error = %v", err)
	}
	if seat.seen != 0 {
		t.Fatalf("bad --dir paid for %d model calls", seat.seen)
	}
	var stdout, stderr strings.Builder
	err = doErrand(doRequest{task: "work", workspace: file, asJSON: true, timeout: time.Second,
		stdout: &stdout, stderr: &stderr, newBeltCompleter: func(string) session.Completer { return seat }})
	var status exitStatus
	if !asExitStatus(err, &status) || status != exitCannotRun {
		t.Fatalf("bad --dir exited %v, want 1; stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	fields := doEnvelopeFields(t, stdout.String())
	errorText, _ := fields["error"].(string)
	if !strings.Contains(errorText, file) {
		t.Fatalf("bad --dir did not name path: %v", fields["error"])
	}
	stores, err := filepath.Glob(filepath.Join(home, "runs", "codeaf-do-*"))
	if err != nil || len(stores) != 0 {
		t.Fatalf("bad --dir opened records: %v, %v", stores, err)
	}
}

func TestDoRunCreatesMissingDirectoryWithoutRepositoryRecord(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	repo := beltRepoWorkspace(t)
	want := filepath.Join(repo, "new", "workspace")
	seat := finishingSeat(0)
	var stderr strings.Builder
	outcome, err := runErrand(doRequest{task: "work", workspace: want, timeout: 30 * time.Second,
		stderr: &stderr, newBeltCompleter: func(string) session.Completer { return seat }}, stubErrandSeats())
	if err != nil || outcome.Nodes == 0 {
		t.Fatalf("missing --dir did not run: nodes=%d err=%v", outcome.Nodes, err)
	}
	if info, err := os.Stat(want); err != nil || !info.IsDir() {
		t.Fatalf("missing --dir not created: %v, %v", info, err)
	}
	if status := doGitIn(t, repo, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, ".codeaf/") {
		t.Fatalf("run record landed in repository: %s", status)
	}
}

func TestDoRunKeepsFailedAndTimedOutRecordsOutsideRepository(t *testing.T) {
	for _, kind := range []string{"worker failure", "deadline"} {
		t.Run(kind, func(t *testing.T) {
			home := beltRunEnv(t)
			workspace := beltRepoWorkspace(t)
			seat := &beltSeat{}
			timeout := 2 * time.Second
			if kind == "worker failure" {
				seat.ever = func(context.Context, []ai.Message) (*ai.Response, error) {
					return nil, errors.New("scripted worker failure")
				}
			} else {
				timeout = 100 * time.Millisecond
				seat.ever = func(ctx context.Context, _ []ai.Message) (*ai.Response, error) { <-ctx.Done(); return nil, ctx.Err() }
			}
			var stdout, stderr strings.Builder
			_ = doErrand(doRequest{
				task: "try the work", workspace: workspace, asJSON: true, timeout: timeout,
				stdout: &stdout, stderr: &stderr,
				newBeltCompleter: func(string) session.Completer { return seat },
			})
			folder := keptRunFolder(t, stderr.String())
			if !strings.HasPrefix(folder, filepath.Join(home, "runs")+string(os.PathSeparator)) {
				t.Fatalf("record outside state root: %q", folder)
			}
			if !strings.HasSuffix(strings.TrimSpace(stderr.String()), "record kept at "+folder) {
				t.Fatalf("record is not stderr's last line:\n%s", stderr.String())
			}
			for _, name := range []string{"plandb.db", "tasks"} {
				if _, err := os.Stat(filepath.Join(folder, name)); err != nil {
					t.Fatalf("kept record lacks %s: %v", name, err)
				}
			}
			if status := doGitIn(t, workspace, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, ".codeaf/") {
				t.Fatalf("failed run polluted repository:\n%s", status)
			}
		})
	}
}
