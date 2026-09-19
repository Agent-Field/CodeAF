package enginehost

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
)

// DialStartingHost dials a workspace's host, retrying a refused or briefly
// missing connection ONLY while the host lock is held. A host takes that lock
// before it removes the old socket and listens, so a failure with the lock held
// is the small remove-to-listen window and is worth a retry; a failure with the
// lock free, or no lock file at all, means no host is coming up and it answers at
// once. So the ordinary state after a host crashed and left a stale socket costs
// a headless launch nothing rather than the whole retry budget on every start.
//
// The gate is the lock FILE, read with a non-blocking flock released at once, so
// it is the same answer across pid namespaces. On Windows errors.Is(err,
// syscall.ECONNREFUSED) never matches, so the retry never engages there and the
// first Dial result stands.
func DialStartingHost(workspace string) (net.Conn, error) {
	deadline := time.Now().Add(startingHostWait)
	for {
		conn, err := Dial(workspace)
		if err == nil {
			return conn, nil
		}
		transient := errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, os.ErrNotExist)
		if !transient || !time.Now().Before(deadline) || !hostLockHeld(workspace) {
			return conn, err
		}
		time.Sleep(startingHostPause)
	}
}

const (
	// startingHostWait bounds how long a launch retries while a host comes up in
	// its remove-to-listen window; startingHostPause is the gap between tries.
	// Each Dial keeps its own dialTimeout ceiling.
	startingHostWait  = 250 * time.Millisecond
	startingHostPause = 10 * time.Millisecond
)

// hostLockHeld reports whether some process holds this workspace's host lock.
// The probe is a non-blocking flock released immediately, so it is a question
// and never a wait, and because it reads the file rather than a pid it is correct
// across pid namespaces. A missing lock file means no host has taken it.
func hostLockHeld(workspace string) bool {
	// where, not Dir: probing whether a host is coming up must not create the
	// host directory (Dir would MkdirAll it), the same rule Dial and SocketPath
	// keep so a mere question leaves no litter behind.
	dir, err := where(workspace)
	if err != nil {
		return false
	}
	file, err := os.OpenFile(filepath.Join(dir, lockName), os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return filelock.IsBusy(err)
	}
	_ = filelock.Unlock(file)
	return false
}

// LockPath is the file a host flocks for its lifetime, beside its socket. It is
// exported so a test can hold the lock a host would, to model the window in which
// a host has taken its lock and is replacing a stale socket.
func LockPath(workspace string) (string, error) {
	dir, err := where(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, lockName), nil
}
