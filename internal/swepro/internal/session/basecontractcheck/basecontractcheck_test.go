package basecontractcheck

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyContractCopies(t *testing.T) {
	root := t.TempDir()
	live := filepath.Join(root, "live")
	base := filepath.Join(root, "base")
	if err := os.MkdirAll(filepath.Join(live, "tests"), 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(live, "tests", "contract_test.go")
	if err := os.WriteFile(source, []byte("new contract"), 0o640); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(base, "existing.txt")
	if err := os.MkdirAll(base, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(live, "replacement.txt")
	if err := os.WriteFile(replacement, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := []CopyPlanEntry{
		{Src: filepath.Join(live, "missing"), Dest: filepath.Join(base, "missing")},
		{Src: source, Dest: filepath.Join(base, "nested", "contract_test.go")},
		{Src: replacement, Dest: existing},
	}
	if got := ApplyContractCopies(entries); got != 2 {
		t.Fatalf("copied=%d, want 2", got)
	}
	assertFileText(t, filepath.Join(base, "nested", "contract_test.go"), "new contract")
	assertFileText(t, existing, "new")
	if _, err := os.Stat(filepath.Join(base, "missing")); !os.IsNotExist(err) {
		t.Fatalf("missing source unexpectedly created destination: %v", err)
	}
}

func TestApplyContractCopiesContinuesAfterFailures(t *testing.T) {
	root := t.TempDir()
	sourceDir := filepath.Join(root, "source-dir")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(root, "good.txt")
	if err := os.WriteFile(source, []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "dest", "good.txt")
	entries := []CopyPlanEntry{
		{Src: sourceDir, Dest: filepath.Join(root, "bad", "dir-copy")},
		{Src: source, Dest: source},
		{Src: source, Dest: dest},
	}
	if got := ApplyContractCopies(entries); got != 1 {
		t.Fatalf("copied=%d, want 1", got)
	}
	assertFileText(t, source, "good")
	assertFileText(t, dest, "good")
}

func assertFileText(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s=%q, want %q", path, got, want)
	}
}

type scriptedFS struct {
	exists    map[string]bool
	mkdirFail map[string]bool
	copyFail  map[string]bool
	copied    []string
}

func (f *scriptedFS) Exists(path string) bool { return f.exists[path] }
func (f *scriptedFS) MkdirAll(path string) error {
	if f.mkdirFail[path] {
		return os.ErrPermission
	}
	return nil
}
func (f *scriptedFS) CopyFile(src, dest string) error {
	if f.copyFail[src] {
		return os.ErrPermission
	}
	f.copied = append(f.copied, src+"->"+dest)
	return nil
}

func TestApplyContractCopiesBestEffortTranscript(t *testing.T) {
	entries := []CopyPlanEntry{
		{Src: "missing", Dest: "out/a"},
		{Src: "mkdir-fail", Dest: "blocked/b"},
		{Src: "copy-fail", Dest: "out/c"},
		{Src: "ok", Dest: "out/d"},
	}
	fs := &scriptedFS{
		exists:    map[string]bool{"mkdir-fail": true, "copy-fail": true, "ok": true},
		mkdirFail: map[string]bool{"blocked": true},
		copyFail:  map[string]bool{"copy-fail": true},
	}
	if got := applyContractCopies(entries, fs); got != 1 {
		t.Fatalf("copied=%d, want 1", got)
	}
	if len(fs.copied) != 1 || fs.copied[0] != "ok->out/d" {
		t.Fatalf("successful copy transcript=%v", fs.copied)
	}
}

// TestPlanContractCopiesExcludesAssertedPaths: files the contract asserts
// ABOUT are never copied into the base worktree. Copying one there makes the
// base check pass by construction, which the staleness judge then reads as
// "the bug never reproduced" (BUGS-KEPT.md, base contract check).
func TestPlanContractCopiesExcludesAssertedPaths(t *testing.T) {
	entries, excluded := PlanContractCopiesWithExclusions(PlanContractCopiesInput{
		Workspace:     "/ws",
		Worktree:      "/wt",
		Paths:         []string{"test-hello.sh"},
		AssertedPaths: []string{"hello.txt"},
	})
	if len(entries) != 1 || entries[0].Rel != "test-hello.sh" {
		t.Fatalf("entries = %#v, want the scaffolding only", entries)
	}
	if len(excluded) != 0 {
		t.Fatalf("excluded = %#v, want none (hello.txt was never in paths)", excluded)
	}
}

// TestPlanContractCopiesExcludesOverlapBetweenPathsAndAsserted: the real
// failure from issue #22 — the agent listed its deliverable in BOTH arrays.
// The overlap must resolve toward NOT copying, and must be reported so the
// contamination attempt is visible in the run log.
func TestPlanContractCopiesExcludesOverlapBetweenPathsAndAsserted(t *testing.T) {
	entries, excluded := PlanContractCopiesWithExclusions(PlanContractCopiesInput{
		Workspace:     "/ws",
		Worktree:      "/wt",
		Paths:         []string{"test-hello.sh", "hello.txt"},
		AssertedPaths: []string{"hello.txt"},
	})
	if len(entries) != 1 || entries[0].Rel != "test-hello.sh" {
		t.Fatalf("entries = %#v, want the deliverable withheld", entries)
	}
	if len(excluded) != 1 || excluded[0] != "hello.txt" {
		t.Fatalf("excluded = %#v, want [hello.txt] reported", excluded)
	}
}

// TestPlanContractCopiesNormalizesAssertedPaths: exclusion keys on the same
// normalized form the copy plan does, so an agent writing "./hello.txt" or
// "a/../hello.txt" cannot slip a deliverable through on spelling.
func TestPlanContractCopiesNormalizesAssertedPaths(t *testing.T) {
	for _, asserted := range []string{"./hello.txt", "a/../hello.txt", "hello.txt"} {
		entries, excluded := PlanContractCopiesWithExclusions(PlanContractCopiesInput{
			Workspace:     "/ws",
			Worktree:      "/wt",
			Paths:         []string{"hello.txt"},
			AssertedPaths: []string{asserted},
		})
		if len(entries) != 0 || len(excluded) != 1 {
			t.Errorf("asserted %q: entries=%#v excluded=%#v, want withheld", asserted, entries, excluded)
		}
	}
}

// TestPlanContractCopiesWithoutAssertedPathsIsUnchanged: with no asserted_paths
// the plan is exactly what it was before the field existed.
func TestPlanContractCopiesWithoutAssertedPathsIsUnchanged(t *testing.T) {
	input := PlanContractCopiesInput{
		Workspace: "/ws", Worktree: "/wt",
		Paths: []string{"test-hello.sh", "hello.txt"},
	}
	entries, excluded := PlanContractCopiesWithExclusions(input)
	if len(entries) != 2 || len(excluded) != 0 {
		t.Fatalf("entries=%#v excluded=%#v, want both copied", entries, excluded)
	}
	if len(PlanContractCopies(input)) != 2 {
		t.Fatal("PlanContractCopies must agree with the exclusion-aware form")
	}
}
