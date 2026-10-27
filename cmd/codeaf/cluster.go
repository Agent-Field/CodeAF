package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"io"
	"os"
	"time"
)

const clusterHelp = `codeaf cluster status
  Show this joined computer, learners, voters and the current coordinator.
codeaf cluster lead
  Ask this joined computer to coordinate through a hosted or self-hosted relay.
  Persistent storage is required; keep codeaf open while this computer leads.
  Three voters tolerate one unavailable member.
`

type clusterDoor interface {
	ClusterStatus(context.Context) (clusterctl.Status, error)
	LeadCluster(context.Context, uint64) (cs.LeadershipReceipt, error)
}

const clusterStatusTimeout = 5 * time.Second

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
	return explainSyncUnreachable(clusterCommand(ctx, s, args[0], os.Stdout), s.Relay)
}

// clusterCommand answers `codeaf cluster status` and `codeaf cluster lead`. The
// whole answer is written into a buffer by work bounded by one ceiling, so a
// store open that ignores its context cannot hold the terminal past it, and an
// answer that arrives late never reaches the terminal at all.
func clusterCommand(ctx context.Context, c clusterDoor, verb string, out io.Writer) error {
	if verb == "status" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, clusterStatusTimeout)
		defer cancel()
	}
	return boundedClusterOutput(ctx, out, func(ctx context.Context, out io.Writer) error {
		if verb == "lead" {
			r, err := c.LeadCluster(ctx, 0)
			if err != nil {
				return explainSyncUnreachable(explainRemovedDevice(err), "")
			}
			_, err = fmt.Fprintf(out, "computer %d is coordinating · term %d · revision %d · read barrier %d\n", r.Node, r.Term, r.Revision, r.Barrier)
			return err
		}
		s, err := c.ClusterStatus(ctx)
		if err != nil {
			return explainSyncUnreachable(explainRemovedDevice(err), "")
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
	})
}

// boundedClusterOutput runs one cluster answer under the forwarding ceiling and
// copies its text to out only when it finished in time. A deadline is reported
// as the sync service not answering, never as an unreachable network.
func boundedClusterOutput(ctx context.Context, out io.Writer, work func(context.Context, io.Writer) error) error {
	ctx, cancel := context.WithTimeout(ctx, clusterctl.ForwardTimeout)
	defer cancel()
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	guard.Go("cluster/answer", func() {
		var output bytes.Buffer
		err := work(ctx, &output)
		done <- result{output.String(), err}
	})
	select {
	case got := <-done:
		if got.err != nil {
			return got.err
		}
		_, err := io.WriteString(out, got.text)
		return err
	case <-ctx.Done():
		return explainSyncUnreachable(ctx.Err(), "")
	}
}
