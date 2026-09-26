//go:build linux

package processgroup

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const RunMarkerEnv = "CODEAF_DELEGATE_RUN"

// EnableSubreaper keeps an orphaned shell descendant with the engine after
// the shell exits, so this run can kill and reap it before releasing its folder.
func EnableSubreaper() error {
	if _, err := os.ReadFile("/proc/self/stat"); err != nil {
		return fmt.Errorf("read process tree: %w", err)
	}
	return unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0)
}

// CleanupDescendants runs only inside the senior-dev engine, which is this
// run's child subreaper. Its process tree includes detached and environment-
// sanitized shell children, but no process started by the chat or a sibling run.
func CleanupDescendants() {
	self := os.Getpid()
	empty := 0
	for pass := 0; pass < 40; pass++ {
		pids := descendantProcesses(self)
		if len(pids) == 0 {
			empty++
		} else {
			empty = 0
		}
		for _, pid := range pids {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
		for _, pid := range pids {
			var status syscall.WaitStatus
			_, _ = syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		}
		if empty >= 5 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// CleanupRun is only the host's best-effort sweep after the engine itself was
// killed with SIGKILL. Normal endings use the engine's parent-link walk above;
// after SIGKILL the engine cannot do that walk, so the private launch marker
// can find only descendants that kept it in their environment.
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

func descendantProcesses(root int) []int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	parents := make(map[int]int, len(entries))
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == root {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// The command name is parenthesized and may itself contain spaces or
		// parentheses; the parent pid is the second field after its last ')'.
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			continue
		}
		fields := bytes.Fields(stat[end+1:])
		if len(fields) < 2 {
			continue
		}
		if parent, err := strconv.Atoi(string(fields[1])); err == nil {
			parents[pid] = parent
		}
	}
	owned := map[int]bool{root: true}
	for changed := true; changed; {
		changed = false
		for pid, parent := range parents {
			if owned[parent] && !owned[pid] {
				owned[pid] = true
				changed = true
			}
		}
	}
	var pids []int
	for pid := range owned {
		if pid != root {
			pids = append(pids, pid)
		}
	}
	return pids
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
