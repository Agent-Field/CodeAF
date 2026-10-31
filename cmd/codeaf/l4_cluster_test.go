package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

type l4Cluster struct{}

func (l4Cluster) ClusterStatus(context.Context) (clusterctl.Status, error) {
	return clusterctl.Status{Self: 1, Leader: 1, Members: []cs.MemberLabel{{Node: 5, Device: "gone"}}, Configuration: cs.Configuration{Voters: []uint64{1, 2, 3}, Learners: []uint64{4, 5}}}, nil
}
func (l4Cluster) LeadCluster(context.Context, uint64) (cs.LeadershipReceipt, error) {
	return cs.LeadershipReceipt{Node: 1}, nil
}
func (l4Cluster) ClusterNames(context.Context, clusterctl.Status) map[uint64]string {
	return map[uint64]string{1: "laptop-a", 2: "desk-b", 3: "the sync service", 4: "box-c", 5: ""}
}

// L4-M14a, L4-M14b, L4-M14d: ordinary status and leadership use names; verbose keeps debugging details.
func TestL4ClusterNames(t *testing.T) {
	var out bytes.Buffer
	if err := clusterCommand(context.Background(), l4Cluster{}, "status", &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"coordinating    laptop-a (this computer)", "deciding        laptop-a", "2 of 3 needed", "keeping copies  box-c", "a computer no longer in your devices"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("%q in %s", want, out.String())
		}
	}
	for _, bad := range []string{"[1 2 3]", "term", "revision", "read barrier", "voting", "learning"} {
		if strings.Contains(out.String(), bad) {
			t.Fatal(out.String())
		}
	}
	out.Reset()
	if err := clusterCommand(context.Background(), l4Cluster{}, "lead", &out); err != nil {
		t.Fatal(err)
	}
	if out.String() != "laptop-a now coordinates your devices\n" {
		t.Fatal(out.String())
	}
	out.Reset()
	if err := clusterCommandMode(context.Background(), l4Cluster{}, "status", &out, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[1 2 3]") {
		t.Fatal(out.String())
	}
}
