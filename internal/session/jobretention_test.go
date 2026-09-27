package session

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

func retentionFixture(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	d, err := lockJobRetention(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	if err := d.persist(100); err != nil {
		t.Fatal(err)
	}
	return directory
}
func writeManagedLog(t *testing.T, directory string, id int64, baseBytes, backupBytes int) {
	t.Helper()
	base := fmt.Sprintf("%d.log", id)
	for name, data := range map[string][]byte{base: make([]byte, baseBytes), base + jobRetentionMarkerSuffix: []byte(jobRetentionMarker(base))} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if backupBytes > 0 {
		if err := os.WriteFile(filepath.Join(directory, base+".1"), make([]byte, backupBytes), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
func retentionExists(t *testing.T, path string, want bool) {
	t.Helper()
	_, err := os.Lstat(path)
	if want && err != nil {
		t.Fatalf("missing %s: %v", path, err)
	}
	if !want && !os.IsNotExist(err) {
		t.Fatalf("unexpected %s: %v", path, err)
	}
}
func TestJobRetentionByteAndCountBudgets(t *testing.T) {
	for _, budget := range []jobRetentionBudget{{25, 100}, {1 << 20, 2}} {
		directory := retentionFixture(t)
		writeManagedLog(t, directory, 1, 10, 0)
		writeManagedLog(t, directory, 2, 10, 5)
		writeManagedLog(t, directory, 3, 10, 0)
		writeManagedLog(t, directory, 4, 10, 0)
		if err := jobRetentionSweep(directory, budget); err != nil {
			t.Fatal(err)
		}
		for _, id := range []int{1, 2, 3, 4} {
			base := filepath.Join(directory, fmt.Sprintf("%d.log", id))
			retentionExists(t, base, id > 2)
			retentionExists(t, base+jobRetentionMarkerSuffix, id > 2)
		}
		retentionExists(t, filepath.Join(directory, "2.log.1"), false)
	}
}

// The actual OS lease in a second process must protect an active spool.
func TestJobRetentionLeaseHelper(t *testing.T) {
	path := os.Getenv("CODEAF_RETENTION_TEST_LEASE")
	if path == "" {
		return
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err = filelock.Lock(file, true, true); err != nil {
		t.Fatal(err)
	}
	fmt.Println("locked")
	_, _ = bufio.NewReader(os.Stdin).ReadByte()
}
func retentionSubprocessLease(t *testing.T, path string) func() {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestJobRetentionLeaseHelper$", "-test.timeout=30s")
	cmd.Env = append(os.Environ(), "CODEAF_RETENTION_TEST_LEASE="+path)
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	stop := func() { once.Do(func() { input.Close(); cmd.Wait() }) }
	t.Cleanup(stop)
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(output).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if line != "locked\n" {
			t.Fatalf("lease helper: %q", line)
		}
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		t.Fatal("lease helper timed out")
	}
	return stop
}
func TestJobRetentionActiveLeaseAcrossProcesses(t *testing.T) {
	directory := retentionFixture(t)
	writeManagedLog(t, directory, 1, 5, 0)
	writeManagedLog(t, directory, 2, 500, 0)
	stop := retentionSubprocessLease(t, filepath.Join(directory, "2.log"))
	if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
		t.Fatal(err)
	}
	retentionExists(t, filepath.Join(directory, "1.log"), false)
	retentionExists(t, filepath.Join(directory, "2.log"), true)
	stop()
	if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
		t.Fatal(err)
	}
	retentionExists(t, filepath.Join(directory, "2.log"), false)
}
func TestJobRetentionClaimLeaseAndRestartIDs(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "41.log.1"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	registry := newJobRegistry(t.TempDir(), Place{}, nil)
	first, path, file, err := registry.claimJobLog(directory)
	if err != nil {
		t.Fatal(err)
	}
	if first != 42 {
		t.Fatalf("rotated-only id not seeded: %d", first)
	}
	probe, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := filelock.Lock(probe, true, true); !filelock.IsBusy(err) {
		t.Fatalf("claim lacks live lease: %v", err)
	}
	probe.Close()
	if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
		t.Fatal(err)
	}
	retentionExists(t, path, true)
	file.Close()
	if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
		t.Fatal(err)
	}
	retentionExists(t, path, false)
	restarted := newJobRegistry(t.TempDir(), Place{}, nil)
	second, _, next, err := restarted.claimJobLog(directory)
	if err != nil {
		t.Fatal(err)
	}
	next.Close()
	if second <= first {
		t.Fatalf("reused id %d after %d", second, first)
	}
}
func TestJobRetentionDamagedCounterRefusesWithoutDeleting(t *testing.T) {
	for _, damage := range []string{"missing", "corrupt", "oversized", "overflow"} {
		t.Run(damage, func(t *testing.T) {
			directory := retentionFixture(t)
			writeManagedLog(t, directory, 1, 10, 0)
			counter := filepath.Join(directory, jobRetentionCounterName)
			switch damage {
			case "missing":
				if err := os.Remove(counter); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(counter, []byte("bad\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "oversized":
				if err := os.WriteFile(counter, []byte(strings.Repeat("1", 1024)), 0o600); err != nil {
					t.Fatal(err)
				}
			case "overflow":
				if err := os.WriteFile(counter, []byte("9223372036854775807\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			registry := newJobRegistry(t.TempDir(), Place{}, nil)
			if _, _, file, err := registry.claimJobLog(directory); err == nil {
				file.Close()
				t.Fatal("unsafe counter allocated an id")
			}
			if damage != "overflow" {
				if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err == nil {
					t.Fatal("damaged counter pruned")
				}
			}
			retentionExists(t, filepath.Join(directory, "1.log"), true)
		})
	}
	// Even with all completed payloads gone, losing the counter is not fresh.
	directory := retentionFixture(t)
	if err := os.Remove(filepath.Join(directory, jobRetentionCounterName)); err != nil {
		t.Fatal(err)
	}
	if _, _, file, err := jobRetentionClaim(directory); err == nil {
		file.Close()
		t.Fatal("missing counter reset after empty-directory eviction")
	}
}
func TestJobRetentionLegacyAndUnknownPreserved(t *testing.T) {
	directory := retentionFixture(t)
	for _, name := range []string{"9.log", "9.log.1", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), make([]byte, 4096), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeManagedLog(t, directory, 1, 5, 0)
	if err := jobRetentionSweep(directory, jobRetentionBudget{5, 1}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"9.log", "9.log.1", "notes.txt", "1.log"} {
		retentionExists(t, filepath.Join(directory, name), true)
	}
}
func TestJobRetentionUnsafePathsPreserved(t *testing.T) {
	for _, kind := range []string{"symlink-base", "hardlink-base", "symlink-counter", "hardlink-counter", "symlink-lock", "parent", "leaf"} {
		t.Run(kind, func(t *testing.T) {
			directory := retentionFixture(t)
			target := filepath.Join(t.TempDir(), "sentinel")
			if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
				t.Fatal(err)
			}
			writeManagedLog(t, directory, 1, 5, 0)
			selected := "1.log"
			if strings.Contains(kind, "counter") {
				selected = jobRetentionCounterName
			}
			if strings.Contains(kind, "lock") {
				selected = jobRetentionLockName
			}
			if kind == "parent" || kind == "leaf" {
				holder := t.TempDir()
				alias := filepath.Join(holder, "alias")
				if kind == "parent" {
					if err := os.Symlink(filepath.Dir(directory), alias); err != nil {
						t.Skip(err)
					}
					directory = filepath.Join(alias, filepath.Base(directory))
				} else {
					if err := os.Symlink(directory, alias); err != nil {
						t.Skip(err)
					}
					directory = alias
				}
			} else {
				path := filepath.Join(directory, selected)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				var err error
				if strings.HasPrefix(kind, "hardlink") {
					err = os.Link(target, path)
				} else {
					err = os.Symlink(target, path)
				}
				if err != nil {
					t.Skip(err)
				}
			}
			_ = jobRetentionSweep(directory, jobRetentionBudget{0, 0})
			if kind != "symlink-base" && kind != "hardlink-base" {
				if _, _, f, err := jobRetentionClaim(directory); err == nil {
					f.Close()
					t.Fatal("unsafe directory allocated")
				}
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "untouched" {
				t.Fatalf("external sentinel changed: %q %v", data, err)
			}
			retentionExists(t, filepath.Join(directory, "1.log"), true)
		})
	}
}
func TestJobRetentionOldFixedTempSymlinkCannotClobber(t *testing.T) {
	directory := retentionFixture(t)
	target := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(directory, jobRetentionCounterName+".tmp")); err != nil {
		t.Skip(err)
	}
	_, _, file, err := jobRetentionClaim(directory)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("temp followed: %q %v", data, err)
	}
}
func TestJobRetentionFailureKeepsOwnership(t *testing.T) {
	directory := retentionFixture(t)
	writeManagedLog(t, directory, 1, 5, 0)
	d, err := lockJobRetention(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	// A rotation path that became a directory makes removal fail before ownership
	// is lost. This is deterministic, including under a privileged test account.
	if err := d.root.Mkdir("1.log.1", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := d.remove("1.log"); err == nil {
		t.Fatal("unsafe removal reported success")
	}
	retentionExists(t, filepath.Join(directory, "1.log"), true)
	retentionExists(t, jobRetentionMarkerPath(directory, 1), true)
}
func TestJobRetentionPersistFailureNeverPrunes(t *testing.T) {
	directory := retentionFixture(t)
	writeManagedLog(t, directory, 1, 5, 0)
	if err := os.Remove(filepath.Join(directory, jobRetentionCounterName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(directory, jobRetentionCounterName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, file, err := jobRetentionClaim(directory); err == nil {
		file.Close()
		t.Fatal("counter failure allocated")
	}
	retentionExists(t, filepath.Join(directory, "1.log"), true)
}
func TestJobRetentionConcurrentClaimsRemainLeased(t *testing.T) {
	directory := t.TempDir()
	var workers sync.WaitGroup
	ids := make(chan int, 40)
	errs := make(chan error, 120)
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for n := 0; n < 10; n++ {
				id, path, file, err := jobRetentionClaim(directory)
				if err != nil {
					errs <- err
					return
				}
				if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
					errs <- err
				}
				if _, err := os.Stat(path); err != nil {
					errs <- fmt.Errorf("active claim removed: %w", err)
				}
				file.Close()
				ids <- id
			}
		}()
	}
	workers.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	seen := map[int]bool{}
	for id := range ids {
		if seen[id] {
			t.Errorf("duplicate id %d", id)
		}
		seen[id] = true
	}
	if len(seen) != 40 {
		t.Fatalf("claimed %d jobs, want 40", len(seen))
	}
}
func TestJobRetentionLaterEvictionAndCloseOnce(t *testing.T) {
	directory := t.TempDir()
	_, path, file, err := jobRetentionClaim(directory)
	if err != nil {
		t.Fatal(err)
	}
	sink := newJobSink(file, path)
	callbacks := 0
	sink.finishRetention = func() { callbacks++; jobRetentionFinish(sink) }
	sink.Write([]byte("kept in memory\n"))
	sink.close()
	if sink.retentionLost() {
		t.Fatal("premature eviction")
	}
	if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
		t.Fatal(err)
	}
	if !sink.retentionLost() {
		t.Fatal("cached presence hid later eviction")
	}
	sink.close()
	if callbacks != 1 {
		t.Fatalf("close ran %d callbacks", callbacks)
	}
	if !strings.Contains(sink.tail(10), "kept in memory") {
		t.Fatal("eviction lost in-memory tail")
	}
}

func TestJobRetentionPartialRemovalKeepsMarker(t *testing.T) {
	directory := retentionFixture(t)
	writeManagedLog(t, directory, 1, 5, 5)
	target := filepath.Join(t.TempDir(), "sentinel")
	if err := os.WriteFile(target, []byte("untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(directory, "1.log")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, filepath.Join(directory, "1.log")); err != nil {
		t.Skip(err)
	}
	d, err := lockJobRetention(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer d.close()
	if err := d.remove("1.log"); err == nil {
		t.Fatal("unsafe base removal succeeded")
	}
	retentionExists(t, filepath.Join(directory, "1.log.1"), false)
	retentionExists(t, jobRetentionMarkerPath(directory, 1), true)
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "untouched" {
		t.Fatalf("sentinel changed: %q %v", data, err)
	}
}
func TestJobRetentionInterruptedRemovalMarkerReclaimed(t *testing.T) {
	directory := retentionFixture(t)
	writeManagedLog(t, directory, 1, 5, 0)
	if err := os.Remove(filepath.Join(directory, "1.log")); err != nil {
		t.Fatal(err)
	}
	if err := jobRetentionSweep(directory, jobRetentionBudget{0, 0}); err != nil {
		t.Fatal(err)
	}
	retentionExists(t, jobRetentionMarkerPath(directory, 1), false)
}

func TestStartupSweepPreservesJobIDHistoryAndLegacyLogs(t *testing.T) {
	session := t.TempDir()
	directory := filepath.Join(session, placeLogs, droppingJobs)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	id, path, file, err := jobRetentionClaim(directory)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	legacy := filepath.Join(directory, "999.log")
	if err := os.WriteFile(legacy, []byte("older writer without a lease"), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * sweepTTL)
	for _, entry := range entries {
		if err := os.Chtimes(filepath.Join(directory, entry.Name()), old, old); err != nil {
			t.Fatal(err)
		}
	}
	var notes []string
	sweepLogs(context.Background(), session, time.Now(), func(note string) { notes = append(notes, note) })
	if len(notes) != 0 {
		t.Fatalf("startup maintenance failed: %v", notes)
	}
	for _, name := range []string{path, legacy, filepath.Join(directory, jobRetentionLockName), filepath.Join(directory, jobRetentionCounterName), jobRetentionMarkerPath(directory, int64(id))} {
		retentionExists(t, name, true)
	}
	next, _, file, err := jobRetentionClaim(directory)
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if next <= 999 {
		t.Fatalf("startup lost seeded history: %d", next)
	}
}
