//go:build windows

package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
	commandshell "github.com/Agent-Field/codeaf/internal/shell"
)

// The helper is a native CLI, so C6 observes the environment Windows programs
// really receive after Git Bash has executed the run's own shim.
func init() {
	if os.Getenv("GO_WANT_WINDOWS_PLAN_CLI") == "1" && len(os.Args) > 1 && (os.Args[1] == "plandb" || os.Args[1] == "marker") {
		fmt.Printf("RIGHT %s\nDB=%s\nRUN=%s\n", strings.Join(os.Args[1:], " "), os.Getenv("PLANDB_DB"), os.Getenv("PLANDB_RUN"))
		os.Exit(0)
	}
}

// N2 prove the armed worker's real bash belt reaches its own native
// plandb and codeaf from another cwd, ahead of stale programs on PATH.
func TestWindowsWorkerShims(t *testing.T) {
	if _, err := commandshell.Bash(); err != nil {
		t.Skipf("Git Bash is required for native worker-shim tests: %v", err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv(planCLIBinEnv, "")
	t.Setenv("GO_WANT_WINDOWS_PLAN_CLI", "1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	registered := filepath.Join(t.TempDir(), "running codeaf's (héllo).exe")
	if err := os.WriteFile(registered, body, 0o755); err != nil {
		t.Fatal(err)
	}
	previous := runningCLI
	SetRunningCLI(registered)
	t.Cleanup(func() { runningCLI = previous })
	stale := t.TempDir()
	for _, name := range []string{"codeaf", "plandb"} {
		writeScript(t, stale, name, "echo WRONG; exit 3")
	}
	t.Setenv("PATH", stale+string(os.PathListSeparator)+os.Getenv("PATH"))
	planDir := filepath.Join(t.TempDir(), "plan's state (héllo)")
	if err := os.MkdirAll(planDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(filepath.Join(planDir, planStoreFilename), "the work", planRootID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace := t.TempDir()
	writeScript(t, workspace, "check.sh", "printf 'SCRIPT RIGHT\\n'")
	agent, err := NewBeltWorker(Config{Workspace: workspace, Model: "test/model"}, &scriptedCompleter{}, store.Task(planRootID), store.Path(), store.RootID())
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	plan := agent.graph().planIfArmed()
	if plan == nil {
		t.Fatal("worker has no armed plan")
	}
	destination := t.TempDir()
	command := "cd " + quoteShWord(commandshell.ListPath(destination)) + "; x=inner; echo \"x=[$x]\"; plandb status; codeaf marker; " + checkCommand(workspace, "bash check.sh")
	args, err := json.Marshal(map[string]string{"command": command})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	text, failed, err := beltBash(t, agent).Execute(ctx, args)
	if err != nil || failed {
		t.Fatalf("worker bash: %q %v %v", text, failed, err)
	}
	for _, want := range []string{"x=[inner]", "RIGHT plandb status", "RIGHT marker", "SCRIPT RIGHT", "DB=" + plan.path, "RUN=" + plan.root} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "WRONG") {
		t.Fatalf("a stale CLI ran: %q", text)
	}
}

// N4 proves both rendered project-fact footers name the chosen Windows shell
// and drive spelling, rather than only testing a detached formatting helper.
func TestWindowsPromptFooter(t *testing.T) {
	bash, err := commandshell.Bash()
	if err != nil {
		t.Skipf("Git Bash is required for shell facts: %v", err)
	}
	config := Config{Workspace: t.TempDir()}
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	for _, page := range []string{workerFooter(config, now), chatPage(config, now)} {
		// R4 carries the route conversion remedies to both model-facing pages.
		if !strings.Contains(page, "- Shell: Git Bash ("+bash+")") || !strings.Contains(page, "/c/…, /d/…") || !strings.Contains(page, commandshell.PathConversionFact) {
			t.Fatalf("missing shell facts: %q", page)
		}
	}
}

// N1 omits bash from ordinary and task belts and stops before registering a job.
func TestWindowsMissingShellBeltAndJobs(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_BASH", filepath.Join(t.TempDir(), "missing.exe"))
	for _, task := range []bool{false, true} {
		agent, err := newAgent(Config{Workspace: t.TempDir(), Model: "test/model", InTask: task, bashBelt: task}, &scriptedCompleter{})
		if err != nil {
			t.Fatal(err)
		}
		for _, tool := range agent.belt() {
			if tool.Name == "bash" || tool.Name == "" || tool.Name == "watch" {
				t.Fatalf("unusable tool %q", tool.Name)
			}
		}
		if !task {
			if _, err := agent.SubmitBash(context.Background(), "! echo WRONG > marker"); err == nil || !strings.Contains(err.Error(), "CODEAF_BASH") {
				t.Fatalf("human shell did not explain missing bash: %v", err)
			}
		}
		if _, err := agent.jobs.start("echo WRONG > marker"); err == nil {
			t.Fatal("missing shell started a job")
		}
		page := renderSystemAt(agent.config, time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC))
		if !strings.Contains(page, "Shell commands are unavailable") || !strings.Contains(page, "CODEAF_BASH") {
			t.Fatalf("missing setup facts: %q", page)
		}
		agent.Close()
	}
}

// N1 and N2 run session jobs, automations, watches and task checks through the
// same native shell, preserving variables, cwd, exit status and the job log.
func TestWindowsSessionCommandDoors(t *testing.T) {
	if _, err := commandshell.Bash(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	// This fixture owns its spending ledger as well as its agent; Windows
	// cannot remove the temporary home while the ledger writer holds it open.
	t.Cleanup(func() {
		if !StopUsageWriter(UsageLedgerPath()) {
			t.Error("fixture ledger did not close")
		}
	})
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("right"), 0600); err != nil {
		t.Fatal(err)
	}
	agent, err := newAgent(Config{Workspace: dir, Model: "test/model"}, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	command := `export PATH='/c/Program Files (x86)/fixture':$PATH; x=inner; printf '%s:' "$x"; cat marker`
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	sight, err := automationLookCommand(ctx, dir, command+"; exit 23")
	if err != nil || sight.Text != "inner:right\n(exit status 23)" {
		t.Fatalf("automation: %+v %v", sight, err)
	}
	output, failure := agent.jobs.runTick(ctx, watchSpec{command: command, every: 20 * time.Second})
	if output != "inner:right" || failure != "" {
		t.Fatalf("watch: %q %q", output, failure)
	}
	reading := runOneCheck(ctx, dir, command)
	if !reading.Ran || !reading.Passed || reading.Tail != "inner:right" {
		t.Fatalf("task check: %+v", reading)
	}
	stream, err := agent.SubmitBash(ctx, "!"+command+"; exit 23")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range collect(t, stream) {
		if event.Kind == EventToolFailed && strings.Contains(event.Output, "inner:right") && strings.Contains(event.Output, "23") {
			found = true
		}
	}
	if !found {
		t.Fatal("the person's command lost its cwd, variables or exit status")
	}

	job, err := agent.jobs.start(command)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	case <-ctx.Done():
		t.Fatal("job did not finish")
	}
	body, err := os.ReadFile(job.logPath)
	if err != nil || !strings.Contains(string(body), "inner:right") {
		t.Fatalf("job log: %q %v", body, err)
	}
}
