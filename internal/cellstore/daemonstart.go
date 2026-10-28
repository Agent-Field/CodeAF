package cellstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// idleMinutes is how long the daemon outlives its last client. The next verb
// after it exits starts it again, so the value costs latency, never correctness.
const idleMinutes = "10"

// testIdleMinutes is how long a daemon started by a test binary outlives its
// last client. A test that crashes before its teardown runs leaves the daemon
// behind, and a short idle exit is what lets that leak heal itself.
const testIdleMinutes = "1"

// idleFlag is the --idle-minutes value the daemon is started with.
func idleFlag() string {
	if testing.Testing() {
		return testIdleMinutes
	}
	return idleMinutes
}

// retryAfter is how long a daemon that failed to start is left alone: every
// verb in that window goes straight to the fallback.
const retryAfter = 30 * time.Second

// daemonExits lets an owned shutdown join the process rather than guessing
// how long its final group flush and catalog close will take (U1/C28).
var daemonExits sync.Map

var daemonOwners sync.Map
var startLocks sync.Map

var (
	startMu sync.Mutex
	failed  = map[string]failure{} // socket -> last failed start
)

type failure struct {
	at  time.Time
	err error
}

// startAndDial starts the daemon behind d.Socket and dials it, one start at a
// time per process. A start that finds the daemon already there (another
// process won the race for the socket) is a success: the daemon exits quietly
// when it loses.
func startAndDial(ctx context.Context, d Daemon) (net.Conn, error) {
	gate, _ := startLocks.LoadOrStore(d.Socket, make(chan struct{}, 1))
	lock := gate.(chan struct{})
	timer := time.NewTimer(startWait)
	defer timer.Stop()
	select {
	case lock <- struct{}{}:
		defer func() { <-lock }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		if heldWithoutSocket(d.Socket) {
			return nil, &SavingStuckError{Socket: d.Socket}
		}
		return nil, errors.New("saving is waiting for the storage process to start; try again shortly")
	}
	if err := validateSocket(d.Socket); err != nil {
		return nil, err
	}
	if conn, err := dial(ctx, d.Socket); err == nil {
		return conn, nil
	}
	startMu.Lock()
	f, cached := failed[d.Socket]
	startMu.Unlock()
	if cached && time.Since(f.at) < retryAfter {
		var stuck *SavingStuckError
		if !errors.As(f.err, &stuck) || heldWithoutSocket(d.Socket) {
			return nil, f.err
		}
	}
	if heldWithoutSocket(d.Socket) {
		timer := time.NewTimer(missingSocketWithin)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
		if conn, err := dial(ctx, d.Socket); err == nil {
			return conn, nil
		}
		if heldWithoutSocket(d.Socket) && !recoverOwnedDaemon(ctx, d.Socket) {
			err := &SavingStuckError{Socket: d.Socket}
			startMu.Lock()
			failed[d.Socket] = failure{at: time.Now(), err: err}
			startMu.Unlock()
			return nil, err
		}
	}

	pruneStaleSockets(ctx, filepath.Dir(d.Socket))
	conn, err := launchAndDial(ctx, d)
	if err != nil {
		startMu.Lock()
		failed[d.Socket] = failure{at: time.Now(), err: err}
		startMu.Unlock()
	}
	return conn, err
}

func launchAndDial(ctx context.Context, d Daemon) (net.Conn, error) {
	exited, err := launch(d)
	if err != nil {
		return nil, err
	}
	return awaitDial(ctx, d.Socket, exited)
}

// launch starts the daemon and answers a channel that closes when it exits.
func launch(d Daemon) (<-chan struct{}, error) {
	bin, err := Spawn{Binary: d.Binary}.program()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(d.Socket), 0o700); err != nil {
		return nil, err
	}
	sealInheritedDescriptors()
	cmd := exec.Command(bin, "serve", "--socket", d.Socket, "--idle-minutes", idleFlag()) //codeaf:plumbing starts the engine's long-lived daemon
	configureDaemonCommand(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start engine daemon: %w", err)
	}
	exited := make(chan struct{})
	remember(cmd.Process, exited)
	daemonExits.Store(d.Socket, (<-chan struct{})(exited))
	owner := &spawnedDaemon{process: cmd.Process, exited: exited}
	daemonOwners.Store(d.Socket, owner)
	go func() { // reap it; it outlives this call by design
		_ = cmd.Wait()
		close(exited)
		daemonOwners.CompareAndDelete(d.Socket, owner)
		daemonExits.CompareAndDelete(d.Socket, (<-chan struct{})(exited))
	}()
	return exited, nil
}

// startWait is how long a freshly started daemon has to begin listening.
const startWait = 5 * time.Second

// awaitDial dials until the freshly started daemon listens. A daemon that
// exits first gets one last dial: it exits quietly when another daemon already
// holds the socket, and that one is listening.
func awaitDial(ctx context.Context, socket string, exited <-chan struct{}) (net.Conn, error) {
	deadline := time.Now().Add(startWait)
	for {
		conn, err := dial(ctx, socket)
		if err == nil || time.Now().After(deadline) || ctx.Err() != nil {
			if err != nil && heldWithoutSocket(socket) {
				return nil, &SavingStuckError{Socket: socket}
			}
			return conn, err
		}
		select {
		case <-exited:
			if heldWithoutSocket(socket) {
				return nil, &SavingStuckError{Socket: socket}
			}
			return dial(ctx, socket)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// A lock can precede bind briefly during startup. One quiet wait distinguishes
// that race from a listener whose socket disappeared while it stayed alive.
const missingSocketWithin = 250 * time.Millisecond

func heldWithoutSocket(socket string) bool {
	if _, err := os.Lstat(socket); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	file, err := os.OpenFile(socket+".lock", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	err = filelock.Lock(file, true, true)
	if err == nil {
		_ = filelock.Unlock(file)
		return false
	}
	return filelock.IsBusy(err)
}
