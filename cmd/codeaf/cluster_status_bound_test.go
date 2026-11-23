package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
)

type deadlineStatusDoor struct {
	clusterFixture
	remaining time.Duration
}

func (s *deadlineStatusDoor) ClusterStatus(ctx context.Context) (clusterctl.Status, error) {
	if deadline, ok := ctx.Deadline(); ok {
		s.remaining = time.Until(deadline)
	}
	return clusterctl.Status{}, context.DeadlineExceeded
}

// Contract J22 bounds a status read and reports its deadline without blaming the network.
func TestClusterStatusUsesOneShortReadDeadline(t *testing.T) {
	door := &deadlineStatusDoor{}
	var output bytes.Buffer
	err := clusterCommand(t.Context(), door, "status", &output)
	if door.remaining <= 0 || door.remaining > 5*time.Second {
		t.Errorf("status read deadline = %s, want at most five seconds", door.remaining)
	}
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "did not answer in time") || strings.Contains(err.Error(), "cannot be reached") || output.Len() != 0 {
		t.Fatalf("deadline response = %v, output = %q", err, output.String())
	}
}
