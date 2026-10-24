package clusterctl_test

import (
	"context"
	ctl "github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"net/http"
	"net/http/httptest"
	"testing"
)

type retained struct{ nodes []uint64 }

func (b retained) Status(context.Context, string) (ctl.Status, error) {
	h := cs.Membership{Revision: 2, Index: 1, NextNode: b.nodes[len(b.nodes)-1] + 1, Anchors: b.nodes[:3], Nodes: b.nodes, Live: 1, Limit: cs.MaxMemberRecords}
	if err := h.Validate(); err != nil {
		return ctl.Status{}, err
	}
	return ctl.Status{Cluster: "large-node-ids", Self: b.nodes[3], Leader: b.nodes[0], Membership: h, Configuration: cs.Configuration{Voters: b.nodes[:3]}}, nil
}
func (b retained) Lead(context.Context, cs.Command) (cs.LeadershipReceipt, error) {
	return cs.LeadershipReceipt{}, ctl.ErrUnavailable
}

// R6/R27/R30 keep status readable when permanent node tombstones use the full
// uint64 range. The response envelope is distinct from the smaller log command
// budget; neither response truncation nor deleting retained records is valid.
func TestR6R27R30StatusRetainsFullWidthNodeHistory(t *testing.T) {
	nodes := make([]uint64, cs.MaxMemberRecords)
	for i := range nodes {
		nodes[i] = 1<<63 + uint64(i)
	}
	backend := retained{nodes}
	server := httptest.NewServer(ctl.Handler(func(*http.Request, []byte) (string, string, error) { return "identity", "device", nil }, func(string) (ctl.Backend, error) { return backend, nil }))
	defer server.Close()
	client := ctl.Client{Base: server.URL, Sign: func(*http.Request, []byte) {}, HTTP: server.Client()}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Membership.Nodes) != len(nodes) || status.Membership.Nodes[len(nodes)-1] != nodes[len(nodes)-1] {
		t.Fatal("retained IDs were truncated")
	}
}
