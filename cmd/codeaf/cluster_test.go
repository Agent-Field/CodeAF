package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
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

// R6/R8/R30 retain the dormant client's receipt and refusal checks for #2559.
// A pending transfer never prints success, and help needs no pairing or setup.
func TestR6R8R30ClusterCommandReceiptAndRefusal(t *testing.T) {
	var out bytes.Buffer
	c := clusterFixture{receipt: cs.LeadershipReceipt{Node: 4, Term: 8, Revision: 3, Barrier: 19, Voters: []uint64{1, 2, 4}}}
	if e := clusterCommandMode(context.Background(), c, "lead", &out, true); e != nil {
		t.Fatal(e)
	}
	if got := out.String(); got != "computer 4 is coordinating · term 8 · revision 3 · read barrier 19\n" {
		t.Fatal(got)
	}
	out.Reset()
	c.err = clusterctl.ErrUnavailable
	if e := clusterCommandMode(context.Background(), c, "lead", &out, true); !errors.Is(e, clusterctl.ErrUnavailable) || out.Len() != 0 {
		t.Fatal(e, out.String())
	}
	c.err = nil
	if e := clusterCommandMode(context.Background(), c, "status", &out, true); e != nil {
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

// The design law makes an unusable capability absent for this release, per #2559.
func TestR3bClusterHelpOffersOnlyStatus(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	out, _ := captureUsage(t)
	if code := exitCodeOf(runCluster([]string{"--help"})); code != 0 {
		t.Fatalf("cluster help exited %d", code)
	}
	if !strings.Contains(out.String(), "codeaf cluster status [--verbose]") {
		t.Fatalf("cluster help omits status: %s", out.String())
	}
	for _, help := range []string{out.String(), usageText} {
		if strings.Contains(help, "lead") {
			t.Errorf("help offers removed leadership: %s", help)
		}
	}
}

func TestR3bClusterLeadIsUnknown(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_SYNC_URL", "off")
	for _, args := range [][]string{{"lead"}, {"lead", "--verbose"}} {
		err := runCluster(args)
		const want = "there is no `codeaf cluster lead`.\nrun `codeaf --help` for every command"
		if err == nil || err.Error() != want || exitCodeOf(err) != 1 {
			t.Errorf("cluster %v = %v, want unknown-command refusal %q", args, err, want)
		}
	}
}

func TestR3bClusterStatusStillDispatches(t *testing.T) {
	t.Setenv("CODEAF_HOME", t.TempDir())
	t.Setenv("CODEAF_SYNC_URL", "off")
	for _, args := range [][]string{{"status"}, {"status", "--verbose"}} {
		if err := runCluster(args); !errors.Is(err, syncsetup.ErrQuiet) {
			t.Errorf("cluster %v did not reach sync setup: %v", args, err)
		}
	}
}
