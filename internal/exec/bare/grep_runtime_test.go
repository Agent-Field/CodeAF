package bare

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
)

func runtimeSearchWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
func runtimeSearchTool(t *testing.T, engine, root string) Tool {
	t.Helper()
	path, err := exec.LookPath("rg")
	if engine == "rg" && err != nil {
		t.Skip("ripgrep required for the native-engine parity case")
	}
	return newGrepToolUsing(root, DefaultCaps(), path, engine == "rg")
}
func runtimeSearchCall(t *testing.T, tool Tool, path, glob string) string {
	t.Helper()
	args, _ := json.Marshal(map[string]any{"pattern": "needle", "path": path, "glob": glob})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	text, bad, err := tool.Execute(ctx, args)
	if err != nil || bad {
		t.Fatalf("grep(%s,%s): %s %v", path, glob, text, err)
	}
	return text
}

func TestRecursiveSearchExcludesForegroundSpillsBothEngines(t *testing.T) {
	for _, engine := range []string{"walk", "rg"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("TMPDIR", filepath.Join(root, "tmp"))
			t.Setenv(home.EnvVar, filepath.Join(root, "state"))
			for _, file := range []string{"source.go", "tmp/pi-bash-legacy.log", "state/logs/bash/spill-current.log", "source/pi-bash-not-runtime.log"} {
				runtimeSearchWrite(t, filepath.Join(root, file), "needle\n")
			}
			tool := runtimeSearchTool(t, engine, root)
			text := runtimeSearchCall(t, tool, root, "**/*")
			if strings.Contains(text, "pi-bash-legacy.log:") || strings.Contains(text, "spill-current.log:") {
				t.Fatalf("foreground runtime leaked: %s", text)
			}
			if !strings.Contains(text, "source.go:") || !strings.Contains(text, "pi-bash-not-runtime.log:") {
				t.Fatalf("source excluded: %s", text)
			}
			explicit := runtimeSearchCall(t, tool, filepath.Join(root, "tmp/pi-bash-legacy.log"), "")
			if !strings.Contains(explicit, "needle") {
				t.Fatal("explicit bounded log inspection refused")
			}
		})
	}
}

func TestRuntimeSearchExclusionsBothEngines(t *testing.T) {
	for _, engine := range []string{"walk", "rg"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "state[custom]")
			t.Setenv(home.EnvVar, state)
			source := []string{
				"ordinary/logs/source-generic.go",
				".codeaf/work/source-legacy-work.go",
				"state[custom]/work/source-root-work.go",
				"state[custom]/trees/source-root-tree.go",
				"state[custom]/v3/projects/p/s/work/source-session-work.go",
				"state[custom]/v3/projects/p/s/trees/1/logs/source-tree-log.go",
				"state[custom]/runs/codeaf-do-test/work/source-do-work.go",
				"state[custom]/v3/standing/task/runs/run/trees/1/source-standing.go",
				"state[custom]/v3/standing/exchanges/chat/work/source-exchange.go",
			}
			runtime := []string{
				".codeaf/jobs/runtime-legacy.log", ".codeaf/stubs/runtime-legacy.txt",
				"state[custom]/logs/runtime-daemon.log",
				"state[custom]/v3/projects/p/s/logs/jobs/runtime-job.log",
				"state[custom]/v3/projects/p/s/tasks/runtime-task.jsonl",
				"state[custom]/v3/projects/p/s/transcript.jsonl",
				"state[custom]/runs/codeaf-do-test/tasks/root/runtime-do.jsonl",
				"state[custom]/v3/standing/task/runs/run/logs/runtime-standing.log",
				"state[custom]/v3/standing/exchanges/chat/tasks/runtime-exchange.jsonl",
			}
			for _, name := range append(append([]string{}, source...), runtime...) {
				runtimeSearchWrite(t, filepath.Join(root, name), "needle\n")
			}
			tool := runtimeSearchTool(t, engine, root)
			for _, glob := range []string{"", "**/*"} {
				text := runtimeSearchCall(t, tool, root, glob)
				for _, name := range source {
					if !strings.Contains(text, filepath.Base(name)) {
						t.Fatalf("source omitted: %s\n%s", name, text)
					}
				}
				for _, name := range runtime {
					if strings.Contains(text, filepath.Base(name)+":") {
						t.Fatalf("runtime output re-included: %s\n%s", name, text)
					}
				}
			}
			// Starting at or below the state root must not switch off exclusions.
			for _, path := range []string{state, filepath.Join(state, "v3"), filepath.Join(state, "v3/projects/p/s")} {
				text := runtimeSearchCall(t, tool, path, "**/*")
				if strings.Contains(text, "runtime-") || strings.Contains(text, "transcript.jsonl:") {
					t.Fatalf("state-relative search leaked logs: %s", text)
				}
				if !strings.Contains(text, "source-session-work.go") || !strings.Contains(text, "source-tree-log.go") {
					t.Fatalf("state source omitted: %s", text)
				}
			}
			for _, path := range []string{filepath.Join(state, "logs"), filepath.Join(state, "v3/projects/p/s/logs/jobs"), filepath.Join(root, ".codeaf/jobs")} {
				text := runtimeSearchCall(t, tool, path, "**/*")
				if !strings.Contains(text, "excluded from recursive search") {
					t.Fatalf("runtime-root search was not excluded: %s", text)
				}
			}
			legacyRoot := filepath.Join(root, ".codeaf")
			text := runtimeSearchCall(t, tool, legacyRoot, "**/*")
			if strings.Contains(text, "runtime-legacy") || !strings.Contains(text, "source-legacy-work.go:") {
				t.Fatalf("search rooted at legacy .codeaf leaked logs or hid source: %s", text)
			}
			file := filepath.Join(state, "v3/projects/p/s/logs/jobs/runtime-job.log")
			if text := runtimeSearchCall(t, tool, file, ""); !strings.Contains(text, "needle") {
				t.Fatalf("explicit inspection denied: %s", text)
			}
		})
	}
}
func TestRuntimeSearchHomeAndAliases(t *testing.T) {
	for _, engine := range []string{"walk", "rg"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("HOME", root)
			t.Setenv(home.EnvVar, filepath.Join(root, ".codeaf"))
			state := home.Dir()
			runtime := filepath.Join(state, "v3/projects/p/s/logs/jobs/runtime.log")
			source := filepath.Join(state, "v3/projects/p/s/trees/1/source.go")
			runtimeSearchWrite(t, runtime, "needle\n")
			runtimeSearchWrite(t, source, "needle\n")
			alias := filepath.Join(root, "alias")
			if err := os.Symlink(state, alias); err != nil {
				t.Skip(err)
			}
			tool := runtimeSearchTool(t, engine, root)
			if text := runtimeSearchCall(t, tool, alias, "**/*"); strings.Contains(text, "runtime.log:") || !strings.Contains(text, "source.go:") {
				t.Fatalf("state alias escaped policy: %s", text)
			}
			explicitAlias := filepath.Join(alias, "v3/projects/p/s/logs/jobs")
			if text := runtimeSearchCall(t, tool, explicitAlias, "**/*"); !strings.Contains(text, "excluded from recursive search") {
				t.Fatalf("nested alias escaped policy: %s", text)
			}
			nested := filepath.Join(root, "ordinary", "linked-runtime")
			if err := os.MkdirAll(filepath.Dir(nested), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Dir(runtime), nested); err != nil {
				t.Skip(err)
			}
			if text := runtimeSearchCall(t, tool, filepath.Dir(nested), "**/*"); strings.Contains(text, "runtime.log:") {
				t.Fatalf("nested symlink followed: %s", text)
			}
			if text := runtimeSearchCall(t, tool, filepath.Join(".codeaf", "v3", "projects", "p", "s", "logs"), "**/*"); !strings.Contains(text, "excluded from recursive search") {
				t.Fatalf("relative root escaped: %s", text)
			}
		})
	}
}
func TestRuntimeSearchIgnoresRipgrepConfig(t *testing.T) {
	root := t.TempDir()
	state := filepath.Join(root, "state")
	t.Setenv(home.EnvVar, state)
	runtimeSearchWrite(t, filepath.Join(state, "v3/projects/p/s/logs/runtime.log"), "needle\n")
	runtimeSearchWrite(t, filepath.Join(root, "source.go"), "needle\n")
	config := filepath.Join(root, "rg-config")
	runtimeSearchWrite(t, config, "--follow\n--glob=**/*\n")
	t.Setenv("RIPGREP_CONFIG_PATH", config)
	text := runtimeSearchCall(t, runtimeSearchTool(t, "rg", root), root, "**/*")
	if strings.Contains(text, "runtime.log:") || !strings.Contains(text, "source.go:") {
		t.Fatalf("config changed search policy: %s", text)
	}
}
func TestBoundedGrepSnapshotDoesNotChaseGrowth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "growing.log")
	runtimeSearchWrite(t, path, "first\n")
	var lines []string
	limited, err := scanGrepFile(context.Background(), path, func(_ int, line string) error {
		lines = append(lines, line)
		file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = file.WriteString("second\n")
		return err
	})
	if err != nil || limited || len(lines) != 1 || lines[0] != "first\n" {
		t.Fatalf("snapshot chased growth: %q %v %v", lines, limited, err)
	}
}
func TestBoundedGrepSkipsLongLineAndKeepsLineNumbers(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	path := filepath.Join(root, "source.go")
	runtimeSearchWrite(t, path, strings.Repeat("x", grepLineBytes+100)+"\nneedle\n")
	matches, hit, bounded, err := grepByWalkingBounded(context.Background(), "needle", root, "", false, false, 100)
	if err != nil || hit || !bounded || len(matches) != 1 || matches[0].lineNumber != 2 {
		t.Fatalf("long line hid later match: %+v %v %v %v", matches, hit, bounded, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := scanGrepFile(ctx, path, func(int, string) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel ignored: %v", err)
	}
}
func TestBoundedGrepExplicitLargeFileAndContext(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	path := filepath.Join(root, "state/logs/large.log")
	runtimeSearchWrite(t, path, "before\nneedle\nafter\n"+strings.Repeat("x", 8192)+"\n")
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(grepFileBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	for _, engine := range []string{"walk", "rg"} {
		tool := newGrepToolUsing(root, DefaultCaps(), "must-not-execute-for-a-file", engine == "rg")
		text := runtimeSearchCall(t, tool, path, "")
		if !strings.Contains(text, "needle") {
			t.Fatalf("specific large log not inspectable: %s", text)
		}
		args, _ := json.Marshal(map[string]any{"pattern": "needle", "path": path, "context": 2})
		text, bad, err := tool.Execute(context.Background(), args)
		if err != nil || bad || !strings.Contains(text, "before") || !strings.Contains(text, "after") {
			t.Fatalf("bounded context failed: %s %v", text, err)
		}
		args, _ = json.Marshal(map[string]any{"pattern": "[unclosed", "path": path})
		_, bad, err = tool.Execute(context.Background(), args)
		if err != nil || !bad {
			t.Fatalf("explicit-file invalid regex not surfaced: %v %v", bad, err)
		}
	}
}
func TestRipgrepLongJSONLineIsKilledAndReaped(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, filepath.Join(root, "state"))
	runtimeSearchWrite(t, filepath.Join(root, "long.txt"), "needle"+strings.Repeat("x", 2<<20)+"\n")
	tool := runtimeSearchTool(t, "rg", root)
	args, _ := json.Marshal(map[string]any{"pattern": "needle", "path": root})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	text, bad, err := tool.Execute(ctx, args)
	if ctx.Err() != nil {
		t.Fatalf("scanner stopped without reaping rg: %v", ctx.Err())
	}
	if err != nil || !bad || !strings.Contains(text, "bounded reader") {
		t.Fatalf("oversized JSON was not reported: %s %v %v", text, bad, err)
	}
}
func TestRuntimeGlobEscaping(t *testing.T) {
	policy := grepRuntimePolicy{state: "/home/user/state[one]"}
	globs := policy.rgGlobs("/home/user")
	found := false
	for _, glob := range globs {
		if glob == "!/state\\[one\\]/v3/projects/*/*/logs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom home treated as glob metacharacters: %v", globs)
	}
	if policy.excludes("/home/user/state[one]/v3/projects/p/s/work/logs/source.go") {
		t.Fatal("source logs directory hidden")
	}
}

func TestRecursiveSearchSkipsOversizedFiles(t *testing.T) {
	for _, engine := range []string{"walk", "rg"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv(home.EnvVar, filepath.Join(root, "state"))
			runtimeSearchWrite(t, filepath.Join(root, "small.go"), "needle\n")
			large := filepath.Join(root, "oversized.go")
			runtimeSearchWrite(t, large, "needle\n"+strings.Repeat("x", 8192))
			file, err := os.OpenFile(large, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = file.Truncate(grepFileBytes + 1); err != nil {
				file.Close()
				t.Fatal(err)
			}
			file.Close()
			text := runtimeSearchCall(t, runtimeSearchTool(t, engine, root), root, "**/*")
			if !strings.Contains(text, "small.go:") || strings.Contains(text, "oversized.go:") {
				t.Fatalf("recursive size cap failed: %s", text)
			}
		})
	}
}

func TestGrepOversizedLineWithContextBothEngines(t *testing.T) {
	for _, engine := range []string{"walk", "rg"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv(home.EnvVar, filepath.Join(root, "state"))
			runtimeSearchWrite(t, filepath.Join(root, "long.txt"), "before\nneedle-long"+strings.Repeat("x", 80<<10)+"\nneedle-short\nafter\n")
			tool := runtimeSearchTool(t, engine, root)
			args, _ := json.Marshal(map[string]any{"pattern": "needle", "path": root, "context": 1})
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			text, bad, err := tool.Execute(ctx, args)
			if err != nil || bad || !strings.Contains(text, "long.txt:3: needle-short") || !strings.Contains(text, "long.txt-4- after") || !strings.Contains(text, "Context incomplete") {
				t.Fatalf("bounded context lost its match or notice: %q %v %v", text, bad, err)
			}
			if engine == "rg" && (!strings.Contains(text, "long.txt:2: needle-long") || !strings.Contains(text, "long.txt-1- before")) {
				t.Fatalf("native long-line match disappeared during context rendering: %q", text)
			}
		})
	}
}

func TestGrepContextPreservesMatchAfterFileDisappears(t *testing.T) {
	root := t.TempDir()
	match := grepMatch{filePath: filepath.Join(root, "gone.txt"), lineNumber: 2, lineText: "needle"}
	text, bad, err := grepRender(DefaultCaps(), []grepMatch{match}, 1, false, false, 1, root, true, 100)
	if err != nil || bad || !strings.Contains(text, "gone.txt:2: needle") || !strings.Contains(text, "Context incomplete") {
		t.Fatalf("unavailable context erased original match: %q %v %v", text, bad, err)
	}
}
