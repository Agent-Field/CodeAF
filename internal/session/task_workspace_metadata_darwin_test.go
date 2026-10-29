//go:build darwin

package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func darwinWorkspaceMetadataFixture(t *testing.T, repository bool) (*Agent, uint64, string, string) {
	t.Helper()
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "1")
	t.Setenv("CODEAF_TASK_BELT", "bash")
	t.Setenv("CODEAF_PROTOTYPE_TASK_STORAGE", "1")
	source := t.TempDir()
	if repository {
		source = newTestRepo(t)
	} else {
		writeFile(t, filepath.Join(source, "shared.txt"), "the original line\n")
	}
	place := Place{Dir: t.TempDir(), Workspace: source}
	engine := newBeltRunDouble("done")
	engine.real = true
	registerBeltRunEngine(t, engine)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(c *Config) {
		c.Workspace, c.Place, c.SessionFile = source, place, place.Transcript()
	})
	id, _, _, err := agent.StartTaskInWorkspaceSet(context.Background(), "write private output", WorkspaceSetRequest{Primary: "work", Roots: []WorkspaceRootRequest{{Source: source, Slot: "work", Access: WorkspaceWrite}}})
	if err != nil {
		t.Fatal(err)
	}
	<-engine.entered
	engine.mu.Lock()
	copyDir := engine.spec.Workspace
	engine.mu.Unlock()
	writeFile(t, filepath.Join(copyDir, "shared.txt"), "reviewed private result\r\nsecond line\n")
	endBeltRun(t, agent, engine)
	return agent, id, source, copyDir
}

func TestTaskWorkspaceApplyWithDarwinProvenance(t *testing.T) {
	// Canonical temporary paths avoid the unrelated symlinked TMPDIR workspace rejection.
	real, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", real)
	for _, repository := range []bool{false, true} {
		t.Run(map[bool]string{false: "folder", true: "repository"}[repository], func(t *testing.T) {
			agent, id, source, copyDir := darwinWorkspaceMetadataFixture(t, repository)
			private := filepath.Join(copyDir, "shared.txt")
			var value [12]byte
			n, err := unix.Lgetxattr(private, "com.apple.provenance", value[:])
			if errors.Is(err, unix.ENOATTR) || errors.Is(err, unix.ENOTSUP) {
				// A tracked test binary tags what it writes, so only an untracked one may skip.
				self, selfErr := os.Executable()
				var mark [12]byte
				if _, markErr := unix.Lgetxattr(self, "com.apple.provenance", mark[:]); selfErr == nil && markErr == nil {
					t.Fatalf("this test binary carries com.apple.provenance, but the private output has none: %v", err)
				}
				t.Skip("private output has no system provenance; the test binary is untracked")
			}
			if err != nil || n != 11 {
				t.Fatalf("private output provenance size = %d, error = %v; want 11 bytes", n, err)
			}
			review, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(review.Changed, "shared.txt") || !strings.Contains(review.Diff, "shared.txt") {
				t.Fatal("review did not name the changed shared.txt")
			}
			writeFile(t, private, "later private edit\n")
			if _, err := agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
				t.Fatal(err)
			}
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != "reviewed private result\r\nsecond line\n" {
				t.Fatalf("applied bytes differ from reviewed bytes: %q", got)
			}
			if got := readFile(t, private); got != "later private edit\n" {
				t.Fatalf("private copy was not retained: %q", got)
			}
		})
	}
}

func TestTaskWorkspaceApplyRefusesDarwinQuarantine(t *testing.T) {
	// Canonical temporary paths avoid the unrelated symlinked TMPDIR workspace rejection.
	real, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", real)
	const quarantine = "0081;00000000;Safari;"
	for _, location := range []string{"private-output", "destination-after-review"} {
		t.Run(location, func(t *testing.T) {
			agent, id, source, copyDir := darwinWorkspaceMetadataFixture(t, false)
			path := filepath.Join(copyDir, "shared.txt")
			if location == "destination-after-review" {
				if _, err := agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work"); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(source, "shared.txt")
			}
			if err := unix.Lsetxattr(path, "com.apple.quarantine", []byte(quarantine), 0); err != nil {
				t.Skipf("kernel rejected quarantine fixture: %v", err)
			}
			var before [64]byte
			n, err := unix.Lgetxattr(path, "com.apple.quarantine", before[:])
			if err != nil || string(before[:n]) != quarantine {
				t.Fatalf("quarantine fixture did not persist (size %d, error %v)", n, err)
			}
			if location == "private-output" {
				_, err = agent.ReviewTaskWorkspaceRoot(context.Background(), id, "work")
			} else {
				_, err = agent.ApplyTaskWorkspaceRoot(context.Background(), id, "work")
			}
			if err == nil || !strings.Contains(err.Error(), "portable apply cannot preserve extended attributes or access-control metadata") {
				t.Fatalf("want extended-attributes refusal, got %v", err)
			}
			if got := readFile(t, filepath.Join(source, "shared.txt")); got != "the original line\n" {
				t.Fatalf("refused operation changed original bytes: %q", got)
			}
			var after [64]byte
			n, err = unix.Lgetxattr(path, "com.apple.quarantine", after[:])
			if err != nil || string(after[:n]) != quarantine {
				t.Fatalf("refused operation changed quarantine (size %d, error %v)", n, err)
			}
		})
	}
}
