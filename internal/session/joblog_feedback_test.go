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

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// Search output is written back into the same live spool it could encounter
// on its next pass. A stable answer proves the real tool cannot amplify it.
func TestJobLogSearchCannotFeedItsOwnSpool(t *testing.T) {
	for _, layout := range []string{"legacy", "state-from-home", "state-from-inside"} {
		t.Run(layout, func(t *testing.T) {
			root := t.TempDir()
			state := filepath.Join(root, "runtime")
			t.Setenv("CODEAF_HOME", state)
			place := Place{}
			searchRoot := root
			sourceDir := root
			if layout != "legacy" {
				place = Place{Dir: filepath.Join(state, "v3", "projects", "project", "session")}
				if layout == "state-from-inside" {
					searchRoot = state
					sourceDir = filepath.Join(place.Dir, "work")
				}
			}
			if err := os.MkdirAll(sourceDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sourceDir, "needle.go"), []byte("feedback_1599_needle\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			registry := newJobRegistry(root, place, nil)
			job, err := registry.newJob("search feedback fixture", jobKindBash)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(job.sink.close)
			job.sink.chunkBytes = 512
			var grep bare.Tool
			for _, tool := range bare.AllTools(searchRoot) {
				if tool.Name == "grep" {
					grep = tool
				}
			}
			if grep.Execute == nil {
				t.Fatal("grep missing from real tool belt")
			}
			args, err := json.Marshal(map[string]any{"pattern": "feedback_1599_needle", "path": searchRoot, "limit": 10})
			if err != nil {
				t.Fatal(err)
			}
			var first string
			for iteration := 0; iteration < 40; iteration++ {
				// Bound each search, not the sum of forty process launches on
				// a shared build host; this checks amplification, not throughput.
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				answer, failed, err := grep.Execute(ctx, args)
				cancel()
				if failed || err != nil {
					t.Fatalf("grep: %s, %v", answer, err)
				}
				if !strings.Contains(answer, "needle.go") || strings.Contains(answer, "jobs/") {
					t.Fatalf("search included runtime output or missed source: %s", answer)
				}
				if iteration == 0 {
					first = answer
				} else if answer != first {
					t.Fatalf("search output amplified on iteration %d: %s", iteration, answer)
				}
				if _, err := fmt.Fprintln(job.sink, answer); err != nil {
					t.Fatal(err)
				}
			}
			for _, path := range []string{job.logPath, job.logPath + ".1"} {
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Size() > 512 {
					t.Fatalf("feedback spool escaped its chunk limit: %s has %d bytes", path, info.Size())
				}
			}
			if !strings.Contains(job.sink.notice(), "truncat") {
				t.Fatal("feedback fixture must cross the spool window and report truncation")
			}
		})
	}
}

// The actual background-command door drains sustained stdout and stderr to
// completion while a deliberately tiny disk window rotates many times.
func TestJobSpoolSustainedProcessExitsWithinDiskBound(t *testing.T) {
	root := t.TempDir()
	registry := newJobRegistry(root, Place{}, nil)
	t.Cleanup(func() { registry.shutdown(25 * time.Millisecond) })
	job, err := registry.start("while [ ! -e spool-start ]; do sleep 0.01; done; i=0; while [ \"$i\" -lt 200 ]; do printf 'stdout %s\\n' \"$i\"; printf 'stderr %s\\n' \"$i\" >&2; i=$((i+1)); done; printf 'LAST-OUTPUT\\n'")
	if err != nil {
		t.Fatal(err)
	}
	job.sink.mu.Lock()
	job.sink.chunkBytes = 128
	job.sink.mu.Unlock()
	if err := os.WriteFile(filepath.Join(root, "spool-start"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	case <-time.After(10 * time.Second):
		t.Fatal("bounded spool stopped draining the child")
	}
	if info := job.info(); info.state == jobRunning || info.code != 0 {
		t.Fatalf("child did not exit successfully: %+v", info)
	}
	for _, path := range []string{job.logPath, job.logPath + ".1"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 128 {
			t.Fatalf("sustained output escaped bound: %s has %d bytes", path, info.Size())
		}
	}
	answer, failed := registry.output(job.id, 10)
	if failed || !strings.Contains(answer, "LAST-OUTPUT") || !strings.Contains(answer, "truncat") {
		t.Fatalf("job output lost its latest bytes or truncation notice: %s", answer)
	}
}

// Arbitrary shell commands do not inherit structured grep's exclusions. Even
// when a recursive grep reaches its own output file, rotation must stop disk
// growth and let the reader reach EOF rather than replacing its live inode.
func TestJobShellSearchOfOwnLogStaysBounded(t *testing.T) {
	root := t.TempDir()
	registry := newJobRegistry(root, Place{}, nil)
	t.Cleanup(func() { registry.shutdown(25 * time.Millisecond) })
	job, err := registry.start("while [ ! -e search-start ]; do sleep 0.01; done; grep -r -a shell_feedback_1599 .; printf 'SHELL-SEARCH-DONE\\n'")
	if err != nil {
		t.Fatal(err)
	}
	job.sink.mu.Lock()
	job.sink.chunkBytes = 4096
	job.sink.mu.Unlock()
	// Seed an existing live chunk before releasing the child, so an empty
	// spool cannot make this pass without exercising a self-read.
	if _, err := job.sink.Write([]byte(strings.Repeat("shell_feedback_1599\n", 300))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "search-start"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-job.done:
	case <-time.After(10 * time.Second):
		t.Fatal("shell recursive search chased its own log without settling")
	}
	if info := job.info(); info.code != 0 {
		t.Fatalf("shell search failed: %+v", info)
	}
	for _, path := range []string{job.logPath, job.logPath + ".1"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > 4096 {
			t.Fatalf("shell feedback escaped disk bound: %d", info.Size())
		}
	}
	if !strings.Contains(job.sink.text(), "SHELL-SEARCH-DONE") {
		t.Fatal("shell completion missing")
	}
	if !strings.Contains(job.sink.text(), ".codeaf/jobs/") {
		t.Fatal("shell did not read its own runtime output")
	}
}
