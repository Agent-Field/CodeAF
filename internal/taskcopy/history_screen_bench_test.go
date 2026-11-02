package taskcopy

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
)

// The public composition seam includes bundling, screening and ordinary overlay
// capture. Setup is excluded; every file has distinct bytes and its own blob.
func BenchmarkFullHistoryComposition(b *testing.B) {
	for _, cached := range []bool{false, true} {
		name := "first"
		if cached {
			name = "unchanged"
		}
		b.Run(name, func(b *testing.B) {
			root := b.TempDir()
			project := filepath.Join(root, "plain")
			tree := filepath.Join(root, "chat", cell.TreesDir, "1")
			if err := os.MkdirAll(project, 0700); err != nil {
				b.Fatal(err)
			}
			if err := os.MkdirAll(tree, 0700); err != nil {
				b.Fatal(err)
			}
			for n := range 1000 {
				if err := os.WriteFile(filepath.Join(tree, fmt.Sprintf("file-%04d.txt", n)), []byte(fmt.Sprintf("unique mock content %04d\n", n)), 0600); err != nil {
					b.Fatal(err)
				}
			}
			gitRun := func(args ...string) {
				cmd := exec.Command("git", args...)
				cmd.Dir = tree
				cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Mock", "GIT_AUTHOR_EMAIL=mock@example.invalid", "GIT_COMMITTER_NAME=Mock", "GIT_COMMITTER_EMAIL=mock@example.invalid", "GIT_CONFIG_GLOBAL=/dev/null")
				if out, err := cmd.CombinedOutput(); err != nil {
					b.Fatalf("git: %v %s", err, out)
				}
			}
			gitRun("init", "-q")
			gitRun("add", ".")
			gitRun("commit", "-qm", "mock baseline")
			c := cell.Cell{ID: "benchmark", Root: filepath.Join(root, "chat")}
			if cached {
				if err := (Carry{}).ComposeWorkspace(c, project); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if !cached {
					b.StopTimer()
					if err := os.RemoveAll(carriedRoot(c)); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
				if err := (Carry{}).ComposeWorkspace(c, project); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestFullHistoryCompositionHasBoundedGitProcessFanout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable counter")
	}
	source := unsealedPlainMirrorFixture(t)
	tree := filepath.Join(liveRoot(source.cell), "1")
	for n := range 1000 {
		write(t, filepath.Join(tree, fmt.Sprintf("mock-%04d.txt", n)), fmt.Sprintf("unique mock content %04d\n", n))
	}
	run(t, tree, "git", "add", ".")
	run(t, tree, "git", "commit", "-qm", "large mock baseline")
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	log := filepath.Join(directory, "calls")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nprintf '.\\n' >> " + quote(log) + "\nexec " + quote(realGit) + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(directory, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, phase := range []string{"first", "unchanged"} {
		if err := os.WriteFile(log, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := (Carry{}).ComposeWorkspace(source.cell, source.project); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		calls := strings.Count(string(raw), "\n")
		// This is a process budget, not a machine-dependent latency threshold.
		// One process per file would exceed it by two orders of magnitude.
		if calls > 20 {
			t.Fatalf("%s composition spawned %d Git processes for 1000 files, budget 20", phase, calls)
		}
		t.Logf("%s composition: %d Git processes", phase, calls)
	}
}
