package cellstore

import (
	"golang.org/x/sys/windows"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

// configureDaemonCommand keeps a native engine alive after its chat closes,
// without opening a console window for every background start (K2).
func configureDaemonCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
}

// sealInheritedDescriptors is unnecessary on Windows: os/exec passes only its
// explicitly listed handles, and the daemon has no extra handles (K2).
func sealInheritedDescriptors() {}

type spawnedDaemon struct {
	process *os.Process
	exited  <-chan struct{}
}

var (
	spawnedMu sync.Mutex
	spawned   []spawnedDaemon
)

func remember(process *os.Process, exited <-chan struct{}) {
	spawnedMu.Lock()
	defer spawnedMu.Unlock()
	spawned = append(spawned, spawnedDaemon{process: process, exited: exited})
}

// StopSpawned reaps only this process's children. Windows has no SIGTERM;
// ordinary idle shutdown and Daemon.Stop use the engine's protocol (K2).
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
		_ = d.process.Kill()
		<-d.exited
	}
}
