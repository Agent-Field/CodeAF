package bare

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBashSpillBoundsAndPreservesPrefix(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newOutputAccumulator(Caps{MaxBytes: 64, MaxLines: 10})
	a.Write([]byte("FIRST-PREFIX\n"))
	a.Write([]byte(strings.Repeat("x", bashSpillBytes+1024)))
	a.Write([]byte("\nLAST-TAIL\n"))
	snap := a.sealed()
	data, err := os.ReadFile(snap.fullOutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != bashSpillBytes || !strings.HasPrefix(string(data), "FIRST-PREFIX\n") {
		t.Fatalf("snapshot size=%d or initial prefix lost", len(data))
	}
	footer := formatBashTruncationFooter(snap)
	if strings.Contains(footer, "Full output") || !strings.Contains(footer, "first 8 MiB only") || !strings.Contains(snap.content, "LAST-TAIL") {
		t.Fatalf("dishonest or lost tail: %s %s", footer, snap.content)
	}
}

func TestBashSpillSmallSnapshotIsActuallyFull(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newOutputAccumulator(Caps{MaxBytes: 10, MaxLines: 2})
	for _, piece := range []string{"first\n", "second\n", "third\n"} {
		a.Write([]byte(piece))
	}
	snap := a.sealed()
	data, err := os.ReadFile(snap.fullOutputPath)
	if err != nil || string(data) != "first\nsecond\nthird\n" {
		t.Fatalf("full snapshot=%q error=%v", data, err)
	}
	if !strings.Contains(formatBashTruncationFooter(snap), "Full output:") {
		t.Fatal("complete snapshot not named")
	}
}

func TestBashSpillWriteFailureStillDrains(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newOutputAccumulator(Caps{MaxBytes: 10, MaxLines: 2})
	a.Write([]byte("initial output exceeding cap\n"))
	if a.spill.file == nil {
		t.Fatalf("spill missing: %v", a.spill.problem)
	}
	a.spill.file.Close()
	text := []byte("FINAL-TAIL\n")
	if n, err := a.Write(text); err != nil || n != len(text) {
		t.Fatalf("logging failure broke drain: %d %v", n, err)
	}
	snap := a.sealed()
	footer := formatBashTruncationFooter(snap)
	if strings.Contains(footer, "Full output") || !strings.Contains(footer, "incomplete") {
		t.Fatalf("failure hidden: %s", footer)
	}
}

func TestBashSpillPromotionClosesSnapshot(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	a := newOutputAccumulator(Caps{MaxBytes: 16, MaxLines: 2})
	a.Write([]byte("before promotion\nmore\n"))
	info, err := os.Stat(a.spill.path)
	if err != nil {
		t.Fatal(err)
	}
	var job strings.Builder
	a.mirrorTo(&job)
	a.Write([]byte("after promotion\n"))
	after, err := os.Stat(a.spill.path)
	if err != nil || after.Size() != info.Size() || !a.spill.closed || !strings.Contains(job.String(), "after promotion") {
		t.Fatalf("promotion kept duplicate spool: %v", err)
	}
}

func TestBashSpillActualForegroundCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell pipeline fixture")
	}
	t.Setenv("CODEAF_HOME", t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	args, _ := json.Marshal(map[string]any{"command": "head -c 12582912 /dev/zero | tr '\\0' x; printf '\\nFINAL-FOREGROUND\\n'"})
	text, failed, err := newBashTool(t.TempDir(), Caps{MaxBytes: 1024, MaxLines: 10}).Execute(ctx, args)
	if err != nil || failed || !strings.Contains(text, "FINAL-FOREGROUND") || !strings.Contains(text, "first 8 MiB only") || strings.Contains(text, "Full output:") {
		t.Fatalf("foreground: failed=%v error=%v output=%s", failed, err, text)
	}
}

func TestBashSpillRetentionProcessHelper(t *testing.T) {
	path := os.Getenv("CODEAF_TEST_BASH_SPILL_HELPER")
	if path == "" {
		return
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	lock, err := bashSpillLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := sweepBashSpills(root, 0, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestBashSpillRetentionLeasesAcrossProcesses(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	live := newBashSpill()
	if live.problem != nil {
		t.Fatal(live.problem)
	}
	defer live.close()
	live.write([]byte("still running"))
	finished := newBashSpill()
	if finished.problem != nil {
		t.Fatal(finished.problem)
	}
	finished.write([]byte("finished"))
	finished.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestBashSpillRetentionProcessHelper$")
	cmd.Env = append(os.Environ(), "CODEAF_TEST_BASH_SPILL_HELPER="+filepath.Dir(live.path))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child sweep: %v %s", err, out)
	}
	if _, err := os.Stat(live.path); err != nil {
		t.Fatalf("active writer deleted: %v", err)
	}
	if _, err := os.Stat(finished.path); !os.IsNotExist(err) {
		t.Fatalf("completed log not evicted: %v", err)
	}
	live.write([]byte("after sweep"))
}

func TestBashSpillRetentionBudgetExpiryAndUnsafeFiles(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	var spills []*bashSpill
	for i := 0; i < 4; i++ {
		s := newBashSpill()
		if s.problem != nil {
			t.Fatal(s.problem)
		}
		s.write([]byte("12345"))
		s.close()
		spills = append(spills, s)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(spills[0].path, old, old); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(filepath.Dir(spills[0].path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	unknown := filepath.Join(root.Name(), "user.log")
	if err := os.WriteFile(unknown, []byte("preserved"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root.Name(), "spill-"+strings.Repeat("a", 32)+".log")
	if err := os.Symlink(unknown, link); err != nil {
		t.Skip(err)
	}
	hardlink := filepath.Join(root.Name(), "spill-"+strings.Repeat("b", 32)+".log")
	if err := os.Link(unknown, hardlink); err != nil {
		t.Skip(err)
	}
	lock, err := bashSpillLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := sweepBashSpills(root, 10, 2, time.Now()); err != nil {
		t.Fatal(err)
	}
	count, total := 0, int64(0)
	for _, s := range spills {
		if info, err := os.Stat(s.path); err == nil {
			count++
			total += info.Size()
		}
	}
	if count != 2 || total != 10 {
		t.Fatalf("budget count=%d bytes=%d", count, total)
	}
	if _, err := os.Stat(spills[0].path); !os.IsNotExist(err) {
		t.Fatal("expired log retained")
	}
	if data, err := os.ReadFile(unknown); err != nil || string(data) != "preserved" {
		t.Fatal("unknown target changed")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("unsafe link deleted")
	}
	if _, err := os.Lstat(hardlink); err != nil {
		t.Fatal("unsafe hardlink deleted")
	}
}

func TestBashSpillSelectedHomeAlias(t *testing.T) {
	real := t.TempDir()
	alias := filepath.Join(t.TempDir(), "state")
	if err := os.Symlink(real, alias); err != nil {
		t.Skip(err)
	}
	t.Setenv("CODEAF_HOME", alias)
	s := newBashSpill()
	defer s.close()
	if s.problem != nil {
		t.Fatal(s.problem)
	}
	s.write([]byte("alias output"))
	if !strings.Contains(s.description(), "Full output:") {
		t.Fatalf("selected home alias failed: %s", s.description())
	}
}

func TestBashSpillRefusesLinkedOwnedDirectory(t *testing.T) {
	state, other := t.TempDir(), t.TempDir()
	t.Setenv("CODEAF_HOME", state)
	if err := os.Symlink(other, filepath.Join(state, "logs")); err != nil {
		t.Skip(err)
	}
	s := newBashSpill()
	defer s.close()
	if s.problem == nil || s.file != nil {
		t.Fatal("linked owned storage accepted")
	}
	entries, err := os.ReadDir(other)
	if err != nil || len(entries) > 0 {
		t.Fatal("linked target modified")
	}
}
