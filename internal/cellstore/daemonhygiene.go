package cellstore

import (
	"os"
	"strconv"
	"sync"
	"syscall"
	"time"
)

// sealInheritedDescriptors marks every descriptor above stderr close-on-exec,
// so the daemon started next inherits none of them. Go opens its own files
// close-on-exec, but a descriptor this process was handed by its parent (a
// lock held by a wrapper script, a pipe from a harness) arrives without the
// flag, and os/exec passes it on to every child. A daemon that outlives its
// caller would then hold that lock for as long as it lives.
func sealInheritedDescriptors() {
	for _, dir := range []string{"/proc/self/fd", "/dev/fd"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if fd, err := strconv.Atoi(entry.Name()); err == nil && fd > 2 {
				syscall.CloseOnExec(fd)
			}
		}
		return
	}
}

// spawnedDaemon is a daemon this process started and has not seen exit.
type spawnedDaemon struct {
	process *os.Process
	exited  <-chan struct{}
}

var (
	spawnedMu sync.Mutex
	spawned   []spawnedDaemon
)

// remember records a daemon this process started, for [StopSpawned].
func remember(process *os.Process, exited <-chan struct{}) {
	spawnedMu.Lock()
	defer spawnedMu.Unlock()
	spawned = append(spawned, spawnedDaemon{process: process, exited: exited})
}

// stopGrace is how long a daemon gets to exit after SIGTERM before SIGKILL.
const stopGrace = 3 * time.Second

// StopSpawned stops every daemon this process started and waits for each to
// exit: SIGTERM first, SIGKILL when it has not gone within [stopGrace]. The
// product never calls it, because its daemon is meant to outlive the process
// that started it; a test binary calls it at the end of its run, so that no
// engine it caused to start is left behind.
func StopSpawned() {
	spawnedMu.Lock()
	all := spawned
	spawned = nil
	spawnedMu.Unlock()
	for _, d := range all {
		select {
		case <-d.exited:
			continue
		default:
		}
		_ = d.process.Signal(syscall.SIGTERM)
		select {
		case <-d.exited:
		case <-time.After(stopGrace):
			_ = d.process.Kill()
			<-d.exited
		}
	}
}
