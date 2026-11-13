//go:build linux || darwin

package cellstore

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Recovery only stops a child this client started. Process identity, user,
// socket argument and the broken listener state must all still agree.
func recoverOwnedDaemon(ctx context.Context, socket string) bool {
	value, ok := daemonOwners.Load(socket)
	if !ok {
		return false
	}
	owned := value.(*spawnedDaemon)
	select {
	case <-owned.exited:
		return !heldWithoutSocket(socket)
	default:
	}
	uid, argv := storageProcess(ctx, owned.process.Pid)
	if !daemonServesSocket(uid, strconv.Itoa(os.Getuid()), argv, socket) || !heldWithoutSocket(socket) {
		return false
	}
	if err := owned.process.Signal(syscall.SIGTERM); err != nil {
		return false
	}
	timer := time.NewTimer(missingSocketWithin)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return false
	case <-owned.exited:
		return !heldWithoutSocket(socket)
	}
}

func storageProcess(ctx context.Context, pid int) (string, []string) {
	if runtime.GOOS == "linux" {
		root := filepath.Join("/proc", strconv.Itoa(pid))
		info, err := os.Stat(root)
		if err != nil {
			return "", nil
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return "", nil
		}
		raw, err := os.ReadFile(filepath.Join(root, "cmdline"))
		if err != nil {
			return "", nil
		}
		return strconv.FormatUint(uint64(stat.Uid), 10), strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00")
	}
	bounded, cancel := context.WithTimeout(ctx, missingSocketWithin)
	defer cancel()
	raw, err := exec.CommandContext(bounded, "ps", "-p", strconv.Itoa(pid), "-o", "uid=", "-o", "command=").Output()
	if err != nil {
		return "", nil
	}
	fields := strings.Fields(string(raw))
	if len(fields) < 2 {
		return "", nil
	}
	return fields[0], fields[1:]
}
