//go:build linux

package sessionhost

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

// VALIDATION CONTRACT: what a chat's Linux user leaves in the places every chat
// can write is gone after the chat is stopped, deleted or started afresh, so a
// later chat given the same user finds nothing of the earlier one's. Nothing
// owned by another user is removed, and a link is never followed.

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestSweepRemovesWhatTheChatsUserLeftInSharedAreas(t *testing.T) {
	shm, tmp, outside := t.TempDir(), t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(shm, "segment"), "old chat's secret")
	writeFile(t, filepath.Join(tmp, "deep", "er", "note"), "old chat's note")
	writeFile(t, filepath.Join(outside, "precious"), "not the chat's to lose")
	if err := os.Symlink(outside, filepath.Join(tmp, "link-to-outside")); err != nil {
		t.Fatal(err)
	}

	if err := sweepShared(os.Geteuid(), []string{shm, tmp, filepath.Join(tmp, "no-such-area")}, t.TempDir()); err != nil {
		t.Fatalf("sweep = %v", err)
	}
	for _, path := range []string{filepath.Join(shm, "segment"), filepath.Join(tmp, "deep"), filepath.Join(tmp, "link-to-outside")} {
		if exists(path) {
			t.Errorf("%s survived the sweep", path)
		}
	}
	if !exists(filepath.Join(outside, "precious")) {
		t.Error("the sweep followed a link and removed what it pointed at")
	}
}

func TestSweepLeavesAnotherUsersFilesAlone(t *testing.T) {
	area := t.TempDir()
	writeFile(t, filepath.Join(area, "theirs"), "owned by the test's user, so by someone else as far as the sweep is concerned")
	other := os.Geteuid() + 1
	if err := sweepShared(other, []string{area}, t.TempDir()); err != nil {
		t.Fatalf("sweep = %v", err)
	}
	if !exists(filepath.Join(area, "theirs")) {
		t.Error("a sweep for another user removed this user's file")
	}
}

func TestSysVTablesAreReadByColumnNameAndMatchOwnerOrCreator(t *testing.T) {
	dir := t.TempDir()
	// The columns of the kernel's own tables; a row for uid 200030 owned, one created by it, one for another user.
	writeFile(t, filepath.Join(dir, "shm"), "       key      shmid perms                  size  cpid  lpid nattch   uid   gid  cuid  cgid      atime      dtime      ctime                   rss                  swap\n"+
		"         0        101   600                  4096     7     7      0 200030 200030 200030 200030          0          0          1                     0                     0\n"+
		"         0        102   600                  4096     7     7      0     0     0 200030 200030          0          0          1                     0                     0\n"+
		"         0        103   600                  4096     7     7      0 200031 200031 200031 200031          0          0          1                     0                     0\n")
	ids, err := ownedSysV(filepath.Join(dir, "shm"), "shmid", 200030)
	if err != nil || len(ids) != 2 || ids[0] != 101 || ids[1] != 102 {
		t.Fatalf("owned = %v, %v; want 101 and 102 only", ids, err)
	}
	writeFile(t, filepath.Join(dir, "msg"), "just some other text\n")
	if _, err := ownedSysV(filepath.Join(dir, "msg"), "msqid", 200030); err == nil {
		t.Error("a table with unknown columns was read as if it had the expected ones")
	}
	if err := sweepSysV(200030, t.TempDir()); err != nil {
		t.Errorf("a missing table is not an error, got %v", err)
	}
}

func TestASystemVSegmentIsRemoved(t *testing.T) {
	id, _, errno := unix.Syscall(unix.SYS_SHMGET, 0, 4096, unix.IPC_CREAT|0o600)
	if errno != 0 {
		t.Skipf("this kernel does not allow a System V segment here: %v", errno)
	}
	segment := int(id)
	t.Cleanup(func() { _ = sysvTables[0].remove(segment) })
	listed := func() bool {
		ids, err := ownedSysV("/proc/sysvipc/shm", "shmid", os.Geteuid())
		if err != nil {
			t.Skipf("no table to read: %v", err)
		}
		for _, owned := range ids {
			if owned == segment {
				return true
			}
		}
		return false
	}
	if !listed() {
		t.Fatalf("segment %s is not listed before removal (control)", strconv.Itoa(segment))
	}
	if err := sysvTables[0].remove(segment); err != nil {
		t.Fatalf("remove = %v", err)
	}
	if listed() {
		t.Error("the segment is still listed after removal")
	}
}

// Every way the helper is run (clearing the whole folder when a chat is deleted, only the tmp folder when it stops,
// and the three working folders when one begins) clears the user's leftovers in the shared areas too.
func TestEveryModeOfTheReapHelperSweepsTheSharedAreas(t *testing.T) {
	if os.Geteuid() < minSessionUID {
		t.Skip("the helper refuses to run as a user below the session range, as it should")
	}
	for _, mode := range []reapMode{reapAll, reapTmp, reapInit} {
		t.Run("mode "+string(mode), func(t *testing.T) {
			area, folder := t.TempDir(), t.TempDir()
			writeFile(t, filepath.Join(area, "segment"), "an earlier chat's file")
			defer func(areas []string, sysv string) { sharedAreas, sysvDir = areas, sysv }(sharedAreas, sysvDir)
			sharedAreas, sysvDir = []string{area}, t.TempDir()

			args := []string{folder}
			if mode != reapAll {
				args = append(args, string(mode))
			}
			if err := reap(args); err != nil {
				t.Fatalf("reap(%v) = %v", args, err)
			}
			if exists(filepath.Join(area, "segment")) {
				t.Errorf("a file the chat's user left in a shared area survived reap(%v)", args)
			}
		})
	}
}
