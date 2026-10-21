package cellstore

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
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
	startMu.Lock()
	defer startMu.Unlock()
	if conn, err := dial(ctx, d.Socket); err == nil {
		return conn, nil // an earlier caller started it while this one waited
	}
	if f, ok := failed[d.Socket]; ok && time.Since(f.at) < retryAfter {
		return nil, f.err
	}
	conn, err := launchAndDial(ctx, d)
	if err != nil {
		failed[d.Socket] = failure{at: time.Now(), err: err}
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start engine daemon: %w", err)
	}
	exited := make(chan struct{})
	remember(cmd.Process, exited)
	go func() { // reap it; it outlives this call by design
		_ = cmd.Wait()
		close(exited)
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
			return conn, err
		}
		select {
		case <-exited:
			return dial(ctx, socket)
		case <-time.After(10 * time.Millisecond):
		}
	}
}
