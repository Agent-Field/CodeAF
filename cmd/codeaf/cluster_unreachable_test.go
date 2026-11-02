package main

import (
	"bytes"
	"context"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// B5 gives an unreachable relay the same words at both command doors.
func TestClusterStatusUsesTheDevicesUnreachableSentence(t *testing.T) {
	var out bytes.Buffer
	failure := &url.Error{Op: "Get", URL: "http://relay.invalid/v2/cluster/control", Err: syscall.ECONNREFUSED}
	err := clusterCommand(context.Background(), clusterFixture{err: failure}, "status", &out)
	want := syncwords.Unreachable("http://relay.invalid")
	if err == nil || err.Error() != want || out.Len() != 0 {
		t.Fatalf("status: %v output=%q", err, out.String())
	}
	for _, raw := range []string{"Get ", "dial tcp", "connection refused", "/v2/cluster/control"} {
		if strings.Contains(err.Error(), raw) {
			t.Fatalf("status exposed %q: %v", raw, err)
		}
	}
}
