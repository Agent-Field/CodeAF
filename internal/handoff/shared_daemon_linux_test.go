package handoff

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// Linux permits renaming watched directories. Inspect the live handles too so
// W1 detects the Windows obstruction before a native Windows run is available.
func assertTakeStageReleased(t *testing.T, root string) {
	t.Helper()
	inodes := map[string]string{}
	if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			inodes[fmt.Sprintf("ino:%x", info.Sys().(*syscall.Stat_t).Ino)] = path
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	processes, err := filepath.Glob("/proc/[0-9]*/exe")
	if err != nil {
		t.Fatal(err)
	}
	for _, exe := range processes {
		binary, _ := os.Readlink(exe)
		if !strings.Contains(filepath.Base(binary), "furrow") {
			continue
		}
		proc := filepath.Dir(exe)
		fds, _ := filepath.Glob(filepath.Join(proc, "fd", "*"))
		fds = append(fds, filepath.Join(proc, "cwd"))
		for _, fd := range fds {
			link, _ := os.Readlink(fd)
			if link == root || strings.HasPrefix(link, root+string(filepath.Separator)) {
				t.Errorf("W1 engine retains %s -> %s", fd, link)
			}
			if link != "anon_inode:inotify" {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(proc, "fdinfo", filepath.Base(fd)))
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range strings.Fields(string(raw)) {
				if path := inodes[field]; path != "" {
					t.Errorf("W1 engine retains directory watch on %s (%s)", path, fd)
				}
			}
		}
	}
}
