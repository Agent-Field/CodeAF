package main

import (
	"net/url"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// L4-H3a: an unreachable service cannot trap a tool behind an endless startup retry loop.
func TestL4SyncOutageGateFallsBackLocally(t *testing.T) {
	h := t.TempDir()
	t.Setenv("CODEAF_HOME", h)
	t.Setenv("CODEAF_SYNC_URL", "http://127.0.0.1:8901")
	if _, err := identity.Ensure(h); err != nil {
		t.Fatal(err)
	}
	var attempts atomic.Int32
	drive := newLiveDrive(func(func()) (*syncsetup.Drive, error) {
		attempts.Add(1)
		return nil, &url.Error{Op: "Put", URL: "http://127.0.0.1:8901/device", Err: syscall.ECONNREFUSED}
	})
	if ready := drive.pending(); ready != nil {
		<-ready
	}
	done := make(chan error, 1)
	go func() { done <- drive.Gate() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("local tools were trapped by sync startup retries")
	}
	if n := attempts.Load(); n != 2 {
		t.Fatalf("startup attempts=%d, want one initial and one for this call", n)
	}
}
