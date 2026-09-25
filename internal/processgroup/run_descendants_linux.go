//go:build linux

package processgroup

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const RunMarkerEnv = "CODEAF_DELEGATE_RUN"

// EnableSubreaper keeps an orphaned descendant with codeaf if its engine
// exits first, so this run can kill and reap it before the folder is released.
func EnableSubreaper() { _ = unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) }

// CleanupRun kills processes that inherited the launch's private marker.
// Unlike a process-group signal, this also reaches a setsid grandchild after
// the engine has exited or crashed.
func CleanupRun(marker string) {
	// A setsid wrapper can fork just as the engine exits. A short settle
	// window lets its final exec inherit the marker before declaring it gone.
	empty := 0
	seen := make(map[int]struct{})
	for pass := 0; pass < 20; pass++ {
		pids := runProcesses(marker)
		if len(pids) == 0 {
			empty++
		} else {
			empty = 0
		}
		for _, pid := range pids {
			seen[pid] = struct{}{}
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		for pid := range seen {
			var status syscall.WaitStatus
			waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
			if waited == pid || err == syscall.ECHILD {
				delete(seen, pid)
			}
		}
		if empty >= 5 && len(seen) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runProcesses(marker string) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	want := []byte(RunMarkerEnv + "=" + marker)
	pids := make([]int, 0)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == os.Getpid() {
			continue
		}
		environ, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "environ"))
		if err != nil {
			continue
		}
		for _, variable := range bytes.Split(environ, []byte{0}) {
			if bytes.Equal(variable, want) {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}
