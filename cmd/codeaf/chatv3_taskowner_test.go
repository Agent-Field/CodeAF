package main

import (
	"net"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// Opening another conversation is a second connection from a surface whose host
// can be restarting independently. A held stale socket must not turn that brief
// replacement window into a visible refusal.
func TestTaskOwnerRetriesAHostInTheStaleSocketWindow(t *testing.T) {
	shortStateRoot(t)
	workspace := t.TempDir()
	if _, err := enginehost.Dir(workspace); err != nil {
		t.Fatal(err)
	}
	socket, err := enginehost.SocketPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	unixStale, ok := stale.(*net.UnixListener)
	if !ok {
		t.Fatal("unix listener had an unexpected type")
	}
	unixStale.SetUnlinkOnClose(false)
	if err := stale.Close(); err != nil {
		t.Fatal(err)
	}

	accepted := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		if err := os.Remove(socket); err != nil {
			accepted <- err
			return
		}
		listener, err := net.Listen("unix", socket)
		if err != nil {
			accepted <- err
			return
		}
		defer listener.Close()
		if unix, ok := listener.(*net.UnixListener); ok {
			_ = unix.SetDeadline(time.Now().Add(time.Second))
		}
		conn, err := listener.Accept()
		if err == nil {
			err = conn.Close()
		}
		accepted <- err
	}()

	_, _ = openTaskOwnerView(workspace, tui3.TaskOwnerAsk{Session: "already-open.jsonl"})
	if err := <-accepted; err != nil {
		t.Fatalf("task owner missed a host replacing a stale socket: %v", err)
	}
}
