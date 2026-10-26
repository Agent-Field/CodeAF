//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/workspaceidentity"
)

type ownershipBuildUsage struct {
	Calls  int64   `json:"calls"`
	Input  int64   `json:"input_tokens"`
	Output int64   `json:"output_tokens"`
	USD    float64 `json:"usd"`
}

type ownershipBuildStage struct {
	Stage                   int                 `json:"stage"`
	WorkspaceURI            string              `json:"workspace_uri"`
	ChatURI                 string              `json:"chat_uri"`
	ProjectURI              string              `json:"project_uri"`
	WorkflowSeconds         float64             `json:"workflow_seconds"`
	IndependentCheckSeconds float64             `json:"independent_check_seconds"`
	TemporaryDirectory      string              `json:"temporary_directory"`
	CacheDirectory          string              `json:"cache_directory"`
	Usage                   ownershipBuildUsage `json:"cumulative_usage"`
}

// This acceptance asks a real model to build and extend a small project. The
// primary checkpoints, registered working copies, delivered files and independent
// Python checks decide success; model prose is never treated as evidence.
func TestWorkspaceOwnershipLiveBuildAndExtendProject(t *testing.T) {
	requireTmuxAndKey(t)
	const model = "deepseek/deepseek-v4.1-flash"
	if e2eModel != model {
		t.Fatalf("requires CODEAF_E2E_MODEL=%s", model)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 required")
	}
	home := lifecycleHome(t, model)
	project := workspaceAt(t, filepath.Join(shortRoot(t), "model-build"), false)
	// Tasks intentionally keep branches when the checkout is protected main.
	// This build-and-deliver fixture starts on the existing ordinary work branch.
	statesWorkBranch(t, project)
	env := []string{config.APIKeyEnv + "=" + liveKey(t), "CODEAF_BASE_URL=https://openrouter.ai/api/v1", "CODEAF_PROFILE_DIR=", "CODEAF_SYNC_URL=off", "CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP=1", "CODEAF_PROTOTYPE_TASK_STORAGE=1"}
	t.Cleanup(func() {
		binaries, _ := filepath.Glob(filepath.Join(home, "bin", "furrow-*"))
		for _, binary := range binaries {
			lifecycleDaemonCleanup(t, home, binary)
		}
	})
	stopRecoveryEngineOnCleanup(t, env, home, project)
	rig := startWithEnv(t, env, "ownership_build", home, project, 180, 52, "chat", "--model", model, "--one-model", "--yolo")
	rig.skipSetup(t)
	evidence := os.Getenv("CODEAF_E2E_EVIDENCE_DIR")
	if evidence != "" {
		if err := os.MkdirAll(evidence, 0700); err != nil {
			t.Fatal(err)
		}
		// pipe-pane records the actual application stream, including terminal colors.
		capturePath := filepath.Join(evidence, "build-live-stream.ansi")
		command := "cat > " + shellQuote(capturePath)
		if raw, err := exec.Command("tmux", "pipe-pane", "-o", "-t", rig.name, command).CombinedOutput(); err != nil {
			t.Fatalf("record live terminal: %v %s", err, raw)
		}
	}
	prompts := []string{
		"/task solo Build a small fictional Python standard-library project in your task working copy. Create mini_tasks.py with class TaskList: add(title) returns monotonically increasing integer IDs starting at 1; an empty title raises ValueError; complete(id) returns None and unknown IDs raise KeyError; list_open() returns a list of (id,title) tuples in insertion order for uncompleted tasks. Do not reset IDs when tasks finish. Create test_mini_tasks.py using unittest with at least four meaningful test methods, and update README.md with usage and the exact test command. Run PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v successfully. Also produce the runtime receipt described below. Do not delegate further, install dependencies, make commits yourself, or alter files outside this project and the assigned runtime scratch/cache. Finish normally with plandb done using your actual assigned task id and claimed agent only after checking the project.",
		"/task solo Extend the existing fictional mini_tasks.py project from the previous completed task: add TaskList.pending_count() returning the number of uncompleted tasks. Preserve all existing add/complete/list_open behavior, keep existing tests and add meaningful pending_count tests, and update README.md. Read the existing project first; do not replace it with an unrelated rewrite. Before writing your runtime receipt, verify that $XDG_CACHE_HOME/build-cache-marker contains exactly MOCK_BUILD_CACHE from the first task, and preserve that cache marker. Run PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v successfully. Also produce a fresh runtime receipt described below. Do not delegate further, install dependencies or make commits yourself. Finish normally with plandb done using your actual assigned task id and claimed agent only after checking the extension.",
	}
	runtimeReceipt := " Runtime receipt: write build-workspace-uri.txt containing the actual CODEAF_WORKSPACE_URI environment value followed by a newline. Write build-runtime.txt containing actual CODEAF_WORKSPACE_URI, TMPDIR and XDG_CACHE_HOME values, one per line. Use mktemp, retain the resulting temporary file, and put its absolute path in build-temp-path.txt. In the first task only, write exactly MOCK_BUILD_CACHE without a newline into $XDG_CACHE_HOME/build-cache-marker. Keep these receipt files as deliverables."
	registry, err := workspaceidentity.Open(filepath.Join(home, "workspace-identities"))
	if err != nil {
		t.Fatal(err)
	}
	var stages []ownershipBuildStage
	var previous string
	for index, prompt := range prompts {
		stage := index + 1
		began := time.Now()
		rig.lit(prompt + runtimeReceipt)
		rig.keys("Enter")
		copy, run := ownershipAwaitBuiltStage(t, home, project, registry, previous, 6*time.Minute)
		elapsed := time.Since(began).Seconds()
		checked := time.Now()
		output := ownershipCheckPythonProject(t, project, stage)
		checkSeconds := time.Since(checked).Seconds()
		runtime, err := os.ReadFile(filepath.Join(project, "build-runtime.txt"))
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(runtime)), "\n")
		if len(lines) != 3 || lines[0] != run.URI {
			t.Fatalf("stage %d runtime receipt does not name actual owned run", stage)
		}
		if raw, err := os.ReadFile(filepath.Join(run.Path, "build-workspace-uri.txt")); err != nil || strings.TrimSpace(string(raw)) != run.URI {
			t.Fatalf("stage %d owned artifact lost its actual run identity", stage)
		}
		for _, item := range []struct {
			role        workspaceidentity.Role
			path, owner string
		}{{workspaceidentity.Temp, lines[1], run.URI}, {workspaceidentity.Cache, lines[2], run.ProjectURI}} {
			record, err := registry.Lookup(item.role, item.path)
			if err != nil || record.OwnerURI != item.owner {
				t.Fatalf("stage %d actual runtime path is not registered to the expected owner: %+v %v", stage, record, err)
			}
			relative, err := filepath.Rel(home, item.path)
			if err != nil || !filepath.IsLocal(relative) || strings.HasPrefix(item.path, run.Path+string(filepath.Separator)) {
				t.Fatalf("stage %d runtime scratch/cache escaped assigned profile", stage)
			}
		}
		temporary, err := os.ReadFile(filepath.Join(project, "build-temp-path.txt"))
		if err != nil {
			t.Fatal(err)
		}
		tempPath := strings.TrimSpace(string(temporary))
		if filepath.Dir(tempPath) != lines[1] {
			t.Fatal("mktemp escaped the actual worker temporary directory")
		}
		if info, err := os.Stat(tempPath); err != nil || !info.Mode().IsRegular() {
			t.Fatal("actual worker temporary file is unavailable")
		}
		if marker, err := os.ReadFile(filepath.Join(lines[2], "build-cache-marker")); err != nil || string(marker) != "MOCK_BUILD_CACHE" {
			t.Fatal("shared project cache marker was not preserved")
		}
		if copy.ChatURI != run.OwnerURI || copy.ProjectURI != run.ProjectURI {
			t.Fatal("primary checkpoint and registered run ownership disagree")
		}
		if index > 0 {
			first := stages[0]
			if run.URI == first.WorkspaceURI || run.OwnerURI != first.ChatURI || run.ProjectURI != first.ProjectURI {
				t.Fatal("extension did not get a new run in the original chat and project")
			}
			if lines[1] == first.TemporaryDirectory || lines[2] != first.CacheDirectory {
				t.Fatal("new run must have separate temporary files and reuse the project cache")
			}
		}
		previous = run.URI
		usage := ownershipBuildUsageAt(t, home, model)
		stages = append(stages, ownershipBuildStage{Stage: stage, WorkspaceURI: run.URI, ChatURI: run.OwnerURI, ProjectURI: run.ProjectURI, WorkflowSeconds: elapsed, IndependentCheckSeconds: checkSeconds, TemporaryDirectory: lines[1], CacheDirectory: lines[2], Usage: usage})
		if evidence != "" {
			ownershipBuildEvidence(t, filepath.Join(evidence, fmt.Sprintf("build-stage-%d.ansi", stage)), []byte(captureStyled(rig)))
			ownershipBuildEvidence(t, filepath.Join(evidence, fmt.Sprintf("build-stage-%d-python.txt", stage)), []byte(output))
		}
		t.Logf("stage %d passed: actual completed primary checkpoint, owned run, delivered project and independent Python checks; workflow %.3fs independent checks %.3fs", stage, elapsed, checkSeconds)
	}
	models := callModels(t, home)
	if len(models) == 0 {
		t.Fatal("project workflow has no real model call receipts")
	}
	for _, got := range models {
		if got != model {
			t.Fatalf("unexpected actual project model %q", got)
		}
	}
	lifecycleCompletedCalls(t, home, model)
	summary := struct {
		Model                 string                `json:"model"`
		RecordedModelReceipts int                   `json:"recorded_model_receipts"`
		Stages                []ownershipBuildStage `json:"stages"`
		Usage                 ownershipBuildUsage   `json:"usage"`
		Scope                 string                `json:"scope"`
	}{model, len(models), stages, ownershipBuildUsageAt(t, home, model), "Two real model tasks build and extend a mock project. Separate run scratch and shared project cache are verified. This test does not claim fork, relay, shell containment or long-term disk reclamation."}
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if evidence != "" {
		ownershipBuildEvidence(t, filepath.Join(evidence, "build-project-summary.json"), append(raw, '\n'))
	}
	t.Logf("PASS actual model project build and extension:\n%s", raw)
}

func ownershipAwaitBuiltStage(t *testing.T, home, project string, registry *workspaceidentity.Registry, previous string, within time.Duration) (*session.TaskCopyRecord, workspaceidentity.Record) {
	t.Helper()
	for until := time.Now().Add(within); time.Now().Before(until); {
		raw, err := os.ReadFile(filepath.Join(project, "build-workspace-uri.txt"))
		uri := strings.TrimSpace(string(raw))
		if err == nil && uri != "" && uri != previous {
			run, err := registry.Resolve(uri)
			if err == nil && run.Role == workspaceidentity.Run && run.OwnerURI != "" && run.ProjectURI != "" && run.Path != project {
				owner, e := registry.Resolve(run.OwnerURI)
				origin, p := registry.Resolve(run.ProjectURI)
				if e != nil || owner.Role != workspaceidentity.Chat || p != nil || origin.Role != workspaceidentity.Project || origin.Path != project {
					t.Fatal("actual project task has invalid chat/project identity")
				}
				var completed *session.TaskCopyRecord
				_ = filepath.WalkDir(home, func(path string, d os.DirEntry, e error) error {
					if e != nil || d.IsDir() || d.Name() != "tasks.json" {
						return nil
					}
					data, e := os.ReadFile(path)
					if e != nil {
						return nil
					}
					var checkpoint struct {
						Runs []struct {
							Parent uint64                  `json:"parent"`
							Node   string                  `json:"node"`
							State  string                  `json:"state"`
							Copy   *session.TaskCopyRecord `json:"copy"`
						} `json:"runs"`
					}
					if json.Unmarshal(data, &checkpoint) != nil {
						return nil
					}
					for _, row := range checkpoint.Runs {
						if row.Parent == 0 && row.Node == "" && row.State == "done" && row.Copy != nil && row.Copy.URI == uri {
							completed = row.Copy
							return filepath.SkipAll
						}
					}
					return nil
				})
				ready := completed != nil
				for _, name := range []string{"mini_tasks.py", "test_mini_tasks.py", "README.md", "build-runtime.txt", "build-temp-path.txt"} {
					if !fileExists(filepath.Join(project, name)) || !fileExists(filepath.Join(run.Path, name)) {
						ready = false
					}
				}
				if ready {
					return completed, run
				}
			}
		}
		time.Sleep(pollEvery)
	}
	t.Fatal("actual model task did not finish with both a primary checkpoint and delivered project files")
	return nil, workspaceidentity.Record{}
}

func ownershipCheckPythonProject(t *testing.T, project string, stage int) string {
	t.Helper()
	run := func(args ...string) string {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, "python3", args...)
		command.Dir = project
		command.Env = append(os.Environ(), "PYTHONDONTWRITEBYTECODE=1")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("independent Python project check failed: %v\n%s", err, output)
		}
		return string(output)
	}
	output := run("-m", "unittest", "-v")
	checks := `import pathlib,unittest
from mini_tasks import TaskList
suite=unittest.defaultTestLoader.discover(".",pattern="test_mini_tasks.py")
n=suite.countTestCases()
assert n>=4, n
x=TaskList()
assert x.list_open()==[]
a=x.add("copper"); b=x.add("heron")
assert (a,b)==(1,2)
assert x.list_open()==[(1,"copper"),(2,"heron")]
assert x.complete(a) is None
assert x.list_open()==[(2,"heron")]
assert x.add("third")==3
try:
 x.complete(987654)
except KeyError:
 pass
else:
 raise AssertionError("unknown task ID accepted")
try:
 x.add("")
except ValueError:
 pass
else:
 raise AssertionError("empty title accepted")
assert "TaskList" in pathlib.Path("README.md").read_text()
`
	if stage == 2 {
		checks += `y=TaskList()
assert y.pending_count()==0
first=y.add("one"); second=y.add("two")
assert y.pending_count()==2
y.complete(first)
assert y.pending_count()==1
y.complete(second)
assert y.pending_count()==0
assert "pending_count" in pathlib.Path("README.md").read_text()
`
	}
	output += run("-c", checks)
	return output
}

func ownershipBuildUsageAt(t *testing.T, home, model string) ownershipBuildUsage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, "v3", "usage.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var total ownershipBuildUsage
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var row struct {
			Model  string  `json:"model"`
			Calls  int64   `json:"calls"`
			Input  int64   `json:"in"`
			Output int64   `json:"out"`
			USD    float64 `json:"usd"`
		}
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row.Model != "" && row.Model != model {
			t.Fatalf("usage includes unexpected model %q", row.Model)
		}
		total.Calls += row.Calls
		total.Input += row.Input
		total.Output += row.Output
		total.USD += row.USD
	}
	return total
}

func ownershipBuildEvidence(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
