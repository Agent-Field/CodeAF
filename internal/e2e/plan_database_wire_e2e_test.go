//go:build e2e

package e2e

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/sqliteuri"
	_ "modernc.org/sqlite"
)

func planCLI(t *testing.T, ctx context.Context, dir, path string, args ...string) (string, error) {
	t.Helper()
	argv := append([]string{"plandb"}, args...)
	argv = append(argv, "--db", path, "--json")
	cmd := exec.CommandContext(ctx, binary(t), argv...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PLANDB_DB=", "PLANDB_RUN=", "PLANDB_AGENT=")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func mustPlanCLI(t *testing.T, dir, path string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := planCLI(t, ctx, dir, path, args...)
	if err != nil {
		t.Fatalf("actual plan CLI %v failed: %v %s", args, err, out)
	}
	return out
}

func TestPlanCLIReadsWhileWriterHeldAndFinishesAfterRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "contended.db")
	mustPlanCLI(t, dir, path, "init", "fictional-lock-plan", "--description", "Verify concurrent plan inspection")
	claimPlanProof(t, dir, path, "fixture-writer")
	uri := sqliteuri.File(path)
	uri.RawQuery = "_txlock=immediate&_pragma=busy_timeout(1000)"
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Hold beyond the old five-second busy timeout before opening a fresh CLI.
	time.Sleep(6 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	began := time.Now()
	out, err := planCLI(t, ctx, dir, path, "show", "t-proof")
	cancel()
	if err != nil || !strings.Contains(out, "fictional-proof-task") {
		t.Fatalf("read CLI failed while writer remained held: %v %s", err, out)
	}
	readElapsed := time.Since(began)
	type result struct {
		out string
		err error
	}
	finished := make(chan result, 1)
	ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	go func() {
		out, err := planCLI(t, ctx, dir, path, "done", "t-proof", "--agent", "fixture-writer", "--result", "LOCK_RELEASE_PROOF")
		finished <- result{out, err}
	}()
	select {
	case got := <-finished:
		t.Fatalf("write ended before held transaction released: %v %s", got.err, got.out)
	case <-time.After(time.Second):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got := <-finished
	if got.err != nil || !strings.Contains(got.out, "LOCK_RELEASE_PROOF") {
		t.Fatalf("write did not recover after release: %v %s", got.err, got.out)
	}
	reopened := mustPlanCLI(t, dir, path, "show", "t-proof")
	if !strings.Contains(reopened, "LOCK_RELEASE_PROOF") || !strings.Contains(reopened, `"done"`) {
		t.Fatalf("reopen lost completion: %s", reopened)
	}
	t.Logf("PASS actual CLI: read returned in %s with writer still held after6s; done waited for release and reopened with exact completion proof.", readElapsed)
}

func TestPlanCLIPreservesLiteralDatabasePaths(t *testing.T) {
	for _, name := range []string{"plan%23.db", "plan#&%.db", "plan?mode=ro.db", "nested #%?&/ledger%23#&?.db"} {
		t.Run(name, func(t *testing.T) {
			for _, relative := range []bool{false, true} {
				t.Run(fmt.Sprintf("relative=%t", relative), func(t *testing.T) {
					dir := t.TempDir()
					literal := filepath.Join(dir, name)
					if err := os.MkdirAll(filepath.Dir(literal), 0o700); err != nil {
						t.Fatal(err)
					}
					path := literal
					if relative {
						path = name
					}
					mustPlanCLI(t, dir, path, "init", "literal-path-proof", "--description", "The filename is literal")
					if info, err := os.Stat(literal); err != nil || info.Size() == 0 {
						t.Fatalf("literal requested database missing: %v", err)
					}
					claimPlanProof(t, dir, path, "path-owner")
					mustPlanCLI(t, dir, path, "done", "t-proof", "--agent", "path-owner", "--result", "LITERAL_PATH_OK")
					out := mustPlanCLI(t, dir, path, "show", "t-proof")
					if !strings.Contains(out, "LITERAL_PATH_OK") {
						t.Fatalf("literal reopen lost update: %s", out)
					}
					// Inspect the entire isolated tree: a decoded/truncated sibling must not
					// substitute for the exact filename or accompany a seemingly successful CLI.
					_ = filepath.WalkDir(dir, func(actual string, d os.DirEntry, err error) error {
						if err != nil {
							t.Error(err)
							return nil
						}
						if !d.IsDir() && actual != literal && actual != literal+"-wal" && actual != literal+"-shm" {
							t.Errorf("unexpected decoded/truncated database sibling: %s", actual)
						}
						return nil
					})
					t.Logf("PASS actual CLI literal path (relative=%t): create, claim, finish, reopen exact proof; no decoded sibling.", relative)
				})
			}
		})
	}
}

func TestSpecialPathProjectLivePlanWorkflow(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	home := recoveryShortHome(t, newHome(t, map[string]any{"memory.enabled": false}))
	ws := workspaceAt(t, filepath.Join(t.TempDir(), "parcel #%23?& +"), false)
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=https://openrouter.ai/api/v1", "CODEAF_PROFILE_DIR="}
	stopRecoveryEngineOnCleanup(t, env, home, ws)
	rig := startWithEnv(t, env, "special_path_plan", home, ws, 170, 48, "chat", "--model", model, "--one-model")
	rig.skipSetup(t)
	prompt := "We are preparing a fictional parcel counter. Inspect this repository and report the current files; do not edit yet."
	rig.lit(prompt)
	rig.keys("Enter")
	if answer, _ := privateMemoryAwaitAnswer(home, prompt, 90*time.Second); answer == "" {
		t.Fatal("initial inspection did not complete")
	}
	rig.lit("/task create count_parcels.py with a function count_lines(text) returning the number of nonempty lines, add test_count_parcels.py using the Python standard-library unittest module with at least three tests including empty input, run the tests, and finish only when they pass. Keep all work in your assigned project copy.")
	rig.keys("Enter")
	for deadline := time.Now().Add(4 * time.Minute); !freeConsentDoneNote(home); {
		if time.Now().After(deadline) {
			t.Fatalf("special-path actual task did not finish: %s", rig.capture())
		}
		time.Sleep(pollEvery)
	}
	branch := runBranch(t, ws)
	if branch == "" {
		t.Fatal("actual task has no saved branch")
	}
	proofDir := t.TempDir()
	for _, file := range []string{"count_parcels.py", "test_count_parcels.py"} {
		raw, err := exec.Command("git", "-C", ws, "show", branch+":"+file).CombinedOutput()
		if err != nil || len(raw) == 0 {
			t.Fatalf("task artifact %s missing: %v %s", file, err, raw)
		}
		if err := os.WriteFile(filepath.Join(proofDir, file), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	check := exec.CommandContext(ctx, "python3", "-m", "unittest", "-v")
	check.Dir = proofDir
	output, checkErr := check.CombinedOutput()
	cancel()
	count := regexp.MustCompile(`Ran (\d+) tests?`).FindStringSubmatch(string(output))
	n := 0
	if len(count) > 1 {
		n, _ = strconv.Atoi(count[1])
	}
	if checkErr != nil || n < 3 {
		t.Fatalf("actual task tests failed or omitted requested cases: %v %s", checkErr, output)
	}
	t.Logf("Actual branch artifacts independently checked with %d passing tests.", n)
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("no live receipts")
	}
	for _, got := range models {
		if got != model {
			t.Fatalf("unexpected model %s", got)
		}
	}
	t.Logf("PASS live special-path project: actual inspected repository, two-file tested plan completed, both artifacts on task branch; all %d receipts exact %s.\n%s", len(models), model, rig.capture())
}

func claimPlanProof(t *testing.T, dir, path, agent string) {
	t.Helper()
	mustPlanCLI(t, dir, path, "add", "fictional-proof-task", "--as", "proof", "--description", "Inspect and complete this fictional proof task")
	out := mustPlanCLI(t, dir, path, "go", "--agent", agent)
	var claimed struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &claimed); err != nil || claimed.ID != "t-proof" {
		t.Fatalf("actual CLI did not claim the proof child: %v %s", err, out)
	}
}
