package main

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
)

// B5 gives an unreachable relay the same words at both command doors.
func TestClusterStatusUsesTheDevicesUnreachableSentence(t *testing.T) {
	var out bytes.Buffer
	failure := &url.Error{Op: "Get", URL: "http://relay.invalid/v2/cluster/control", Err: errors.New("dial tcp: connect: connection refused")}
	err := clusterCommand(context.Background(), clusterFixture{err: failure}, "status", &out)
	want := "the sync service at http://relay.invalid cannot be reached from here — check this computer's network"
	if err == nil || err.Error() != want || out.Len() != 0 {
		t.Fatalf("status: %v output=%q", err, out.String())
	}
	for _, raw := range []string{"Get ", "dial tcp", "connection refused", "/v2/cluster/control"} {
		if strings.Contains(err.Error(), raw) {
			t.Fatalf("status exposed %q: %v", raw, err)
		}
	}
}
