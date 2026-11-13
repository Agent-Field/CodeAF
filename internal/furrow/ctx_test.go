//go:build unix

package furrow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// installParkedFurrow is a furrow whose `status` announces itself on the ready
// pipe and then parks on the gate pipe, so a test can hold one question open for
// as long as it likes and prove a second asker did not wait for it.
func installParkedFurrow(t *testing.T) (pid func() int, release func()) {
	t.Helper()
	carryNothing(t)
	dir := t.TempDir()
	ready, gate, pidfile := filepath.Join(dir, "ready"), filepath.Join(dir, "gate"), filepath.Join(dir, "pid")
	for _, fifo := range []string{ready, gate} {
		if err := syscall.Mkfifo(fifo, 0o600); err != nil {
			t.Fatalf("mkfifo: %v", err)
		}
	}
	script := filepath.Join(dir, "furrow")
	body := `#!/bin/sh
if [ "$1" = "--version" ]; then echo "furrow 0.1.0"; exit 0; fi
echo $$ > "` + pidfile + `"
echo started > "` + ready + `"
cat "` + gate + `" > /dev/null
echo '{"head":"aaaabbbbcccc0001","watcher_running":true}'
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv(BinaryEnvVar, "")
	Forget()
	t.Cleanup(Forget)
	t.Cleanup(func() {
		if f, err := os.OpenFile(gate, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = f.Close()
		}
	})
	pid = func() int {
		done := make(chan struct{})
		go func() { _, _ = os.ReadFile(ready); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Minute):
			t.Fatal("furrow's status call never started")
		}
		raw, _ := os.ReadFile(pidfile)
		var n int
		_, _ = fmt.Sscan(string(raw), &n)
		return n
	}
	release = func() {
		done := make(chan struct{})
		go func() { _ = os.WriteFile(gate, nil, 0o600); close(done) }()
		select {
		case <-done:
		case <-time.After(time.Minute):
			t.Fatal("furrow was not waiting at the gate")
		}
	}
	return pid, release
}

// A caller whose context is cancelled stops waiting behind another caller's
// question. The other caller's furrow is untouched and still answers.
func TestAWaiterCanAbandonAnotherAskersQuestion(t *testing.T) {
	pid, release := installParkedFurrow(t)
	root := t.TempDir()
	leader := make(chan Presence, 1)
	go func() { leader <- Detect(context.Background(), root) }()
	leaderPid := pid()

	ctx, cancel := context.WithCancel(context.Background())
	waiter := make(chan Presence, 1)
	go func() { waiter <- Detect(ctx, root) }()
	cancel()
	select {
	case got := <-waiter:
		if got.Available() {
			t.Fatalf("an abandoned wait answered %+v", got)
		}
	case <-time.After(time.Minute):
		t.Fatal("a cancelled caller kept waiting behind another caller's furrow")
	}
	if syscall.Kill(leaderPid, 0) != nil {
		t.Fatal("abandoning the wait cut the other caller's furrow")
	}
	release()
	if got := <-leader; !got.Available() {
		t.Fatalf("the question that was waited behind answered %+v; want available", got)
	}
}
