package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"strings"
	"testing"
)

type clusterFixture struct {
	receipt cs.LeadershipReceipt
	err     error
}

func (c clusterFixture) ClusterStatus(context.Context) (clusterctl.Status, error) {
	return clusterctl.Status{Leader: 4, Configuration: cs.Configuration{Voters: []uint64{1, 2, 4}, Learners: []uint64{3}}}, c.err
}
func (c clusterFixture) LeadCluster(context.Context, uint64) (cs.LeadershipReceipt, error) {
	return c.receipt, c.err
}

// R6/R8/R30 pin the actual CLI receipt and refusal. A pending transfer never
// prints a successful role, and help remains usable before pairing or setup.
func TestR6R8R30ClusterCommandReceiptAndRefusal(t *testing.T) {
	var out bytes.Buffer
	c := clusterFixture{receipt: cs.LeadershipReceipt{Node: 4, Term: 8, Revision: 3, Barrier: 19, Voters: []uint64{1, 2, 4}}}
	if e := clusterCommand(context.Background(), c, "lead", &out); e != nil {
		t.Fatal(e)
	}
	if got := out.String(); got != "computer 4 is coordinating · term 8 · revision 3 · read barrier 19\n" {
		t.Fatal(got)
	}
	out.Reset()
	c.err = clusterctl.ErrUnavailable
	if e := clusterCommand(context.Background(), c, "lead", &out); !errors.Is(e, clusterctl.ErrUnavailable) || out.Len() != 0 {
		t.Fatal(e, out.String())
	}
	c.err = nil
	if e := clusterCommand(context.Background(), c, "status", &out); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(out.String(), "voting computers [1 2 4]") {
		t.Fatal(out.String())
	}
	for _, arg := range []string{"--help", "-h", "-help"} {
		if code := exitCodeOf(runCluster([]string{arg})); code != 0 {
			t.Fatal(code)
		}
	}
}
