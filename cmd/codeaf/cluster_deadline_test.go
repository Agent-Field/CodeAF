package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"syscall"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
)

// Contract E6 distinguishes an unanswered request from a network that has no route.
func TestClusterStatusDistinguishesDeadlinesFromUnreachableNetworks(t *testing.T) {
	for _, item := range []struct {
		name  string
		cause error
		want  string
	}{
		{"deadline", context.DeadlineExceeded, "the sync service at http://relay.invalid did not answer in time — try again"},
		{"refused", syscall.ECONNREFUSED, "the sync service at http://relay.invalid cannot be reached from here — check this computer's network"},
		{"route", syscall.ENETUNREACH, "the sync service at http://relay.invalid cannot be reached from here — check this computer's network"},
		{"dns", &net.DNSError{Err: "no such host", Name: "relay.invalid", IsNotFound: true}, "the sync service at http://relay.invalid cannot be reached from here — check this computer's network"},
	} {
		t.Run(item.name, func(t *testing.T) {
			failure := &url.Error{Op: "Get", URL: "http://relay.invalid/v2/cluster/control", Err: item.cause}
			var output bytes.Buffer
			err := clusterCommand(t.Context(), clusterFixture{err: failure}, "status", &output)
			if err == nil || err.Error() != item.want || !errors.Is(err, item.cause) || output.Len() != 0 {
				t.Fatalf("status=%v output=%q", err, output.String())
			}
		})
	}
}

type slowClusterStatus struct {
	clusterFixture
	release, finished chan struct{}
}

func (s slowClusterStatus) ClusterStatus(context.Context) (clusterctl.Status, error) {
	defer close(s.finished)
	<-s.release
	return clusterctl.Status{Self: 42}, nil
}

// Contract E6 bounds the command even when local store work ignores cancellation.
func TestClusterStatusBoundsLocalStoreWorkWithoutLateOutput(t *testing.T) {
	s := slowClusterStatus{release: make(chan struct{}), finished: make(chan struct{})}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	var output bytes.Buffer
	returned := make(chan error, 1)
	go func() { returned <- clusterCommand(ctx, s, "status", &output) }()
	select {
	case err := <-returned:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("status=%v", err)
		}
	case <-time.After(time.Second):
		close(s.release)
		<-s.finished
		<-returned
		t.Fatal("local store work exceeded the command deadline")
	}
	close(s.release)
	<-s.finished
	if output.Len() != 0 {
		t.Fatalf("late store work printed %q", output.String())
	}
}
