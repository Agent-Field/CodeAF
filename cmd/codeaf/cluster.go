package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"io"
	"os"
)

const clusterHelp = `codeaf cluster status
  Show this joined computer, learners, voters and the current coordinator.
codeaf cluster lead
  Ask this joined computer to coordinate through a self-hosted relay.
  Persistent storage is required; hosted leadership is not available yet.
  Three voters tolerate one unavailable member.
`

type clusterDoor interface {
	ClusterStatus(context.Context) (clusterctl.Status, error)
	LeadCluster(context.Context, uint64) (cs.LeadershipReceipt, error)
}

func runCluster(args []string) error {
	if askedForHelp(args) {
		fmt.Fprint(usageOut, clusterHelp)
		return exitHelped
	}
	if len(args) != 1 || args[0] != "status" && args[0] != "lead" {
		return errors.New("usage: codeaf cluster status | lead")
	}
	if syncsetup.Quiet(home.Dir()) {
		return syncsetup.ErrQuiet
	}
	s, ok, err := syncsetup.Open(home.Dir())
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("sync is off on this computer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), clusterctl.ForwardTimeout)
	defer cancel()
	return clusterCommand(ctx, s, args[0], os.Stdout)
}
func clusterCommand(ctx context.Context, c clusterDoor, verb string, out io.Writer) error {
	if verb == "lead" {
		r, err := c.LeadCluster(ctx, 0)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "computer %d is coordinating · term %d · revision %d · read barrier %d\n", r.Node, r.Term, r.Revision, r.Barrier)
		return err
	}
	s, err := c.ClusterStatus(ctx)
	if err != nil {
		return err
	}
	if s.Leader != 0 {
		fmt.Fprintf(out, "coordinator %d\n", s.Leader)
	}
	if len(s.Configuration.Voters) > 0 {
		fmt.Fprintf(out, "voting computers %v\n", s.Configuration.Voters)
	}
	if s.Self != 0 {
		fmt.Fprintf(out, "this computer %d\n", s.Self)
	}
	if len(s.Configuration.Learners) > 0 {
		fmt.Fprintf(out, "learning computers %v\n", s.Configuration.Learners)
	}
	if len(s.Ready) > 0 {
		fmt.Fprintf(out, "ready computers %v\n", s.Ready)
	}
	if len(s.Configuration.Outgoing) > 0 {
		fmt.Fprintf(out, "finishing the change from %v\n", s.Configuration.Outgoing)
	}
	return nil
}
