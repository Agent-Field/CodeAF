package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/devname"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
	"io"
	"os"
	"slices"
	"strings"
	"time"
)

const clusterVerboseFlag = "verbose"

const clusterHelp = `codeaf cluster status [--` + clusterVerboseFlag + `]
  Show the computers coordinating, deciding changes and keeping copies.
codeaf cluster lead [--` + clusterVerboseFlag + `]
  Ask this joined computer to coordinate through the sync service.
  Persistent storage is required; keep codeaf open while this computer leads.
  Three deciding computers tolerate one unavailable computer.
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
	flags := commandFlags("cluster")
	verbose := flags.Bool(clusterVerboseFlag, false, "show coordination details for debugging")
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	args = flags.Args()
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
	defer terminalSyncProgress(s.Relay)()
	return clusterCommandMode(ctx, s, args[0], os.Stdout, *verbose, s.Relay)
}

// clusterCommandOptions carries the terminal's explicit mode and selected
// service. Calls without options keep the existing detailed output.
type clusterCommandOptions struct {
	verbose bool
	service string
}

func clusterCommandMode(ctx context.Context, c clusterDoor, verb string, out io.Writer, verbose bool, service ...string) error {
	options := clusterCommandOptions{verbose: verbose}
	if len(service) > 0 {
		options.service = service[0]
	}
	return clusterCommand(ctx, c, verb, out, options)
}

// clusterCommand answers `codeaf cluster status` and `codeaf cluster lead`. The
// whole answer is written into a buffer by work bounded by one ceiling, so a
// store open that ignores its context cannot hold the terminal past it, and an
// answer that arrives late never reaches the terminal at all. Errors are explained
// once, with the service this command used, after either work or its ceiling ends.
func clusterCommand(ctx context.Context, c clusterDoor, verb string, out io.Writer, options ...clusterCommandOptions) error {
	selected := clusterCommandOptions{verbose: true}
	if len(options) > 0 {
		selected = options[0]
	}
	verbose, address := selected.verbose, selected.service
	if verb == "status" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, clusterStatusTimeout)
		defer cancel()
	}
	err := boundedClusterOutput(ctx, out, func(ctx context.Context, out io.Writer) error {
		if verb == "lead" {
			r, err := c.LeadCluster(ctx, 0)
			if err != nil {
				return explainRemovedDevice(err)
			}
			if !verbose {
				names := clusterDeviceNames(ctx, c, clusterctl.Status{Self: r.Node})
				_, err = fmt.Fprintln(out, names[r.Node]+" now coordinates your devices")
				return err
			}
			_, err = fmt.Fprintf(out, "computer %d is coordinating · term %d · revision %d · read barrier %d\n", r.Node, r.Term, r.Revision, r.Barrier)
			return err
		}
		s, err := c.ClusterStatus(ctx)
		if err != nil {
			return explainRemovedDevice(err)
		}
		if !verbose {
			return printClusterStatus(out, s, clusterDeviceNames(ctx, c, s))
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
	return explainSyncUnreachable(err, address)
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
		return ctx.Err()
	}
}

// clusterNamer resolves protocol membership through the encrypted device roster.
type clusterNamer interface {
	ClusterNames(context.Context, clusterctl.Status) map[uint64]string
}

func clusterDeviceNames(ctx context.Context, c clusterDoor, status clusterctl.Status) map[uint64]string {
	names := map[uint64]string{}
	if n, ok := c.(clusterNamer); ok {
		names = n.ClusterNames(ctx, status)
	}
	if names == nil {
		names = map[uint64]string{}
	}
	if names[status.Self] == "" && status.Self != 0 {
		names[status.Self] = devname.Name(home.Dir())
	}
	return names
}

const clusterOtherComputer = "another computer"

func clusterName(names map[uint64]string, node uint64, status clusterctl.Status) string {
	if name := names[node]; name != "" {
		return name
	}
	if _, absent := names[node]; absent && slices.ContainsFunc(status.Members, func(member cs.MemberLabel) bool {
		return member.Node == node && member.Device != "" && !member.Anchor
	}) {
		return "a computer no longer in your devices"
	}
	return clusterOtherComputer
}

func printClusterStatus(out io.Writer, s clusterctl.Status, names map[uint64]string) error {
	if s.Leader != 0 {
		leader := clusterName(names, s.Leader, s)
		if s.Leader == s.Self {
			leader += " (this computer)"
		}
		fmt.Fprintf(out, "coordinating    %s\n", leader)
	}
	join := func(nodes []uint64) string {
		var shown []string
		unknown := 0
		for _, node := range nodes {
			name := clusterName(names, node, s)
			if name == clusterOtherComputer && names[node] == "" {
				unknown++
				continue
			}
			if !slices.Contains(shown, name) {
				shown = append(shown, name)
			}
		}
		switch unknown {
		case 0:
		case 1:
			shown = append(shown, clusterOtherComputer)
		default:
			shown = append(shown, fmt.Sprintf("%d other computers", unknown))
		}
		return strings.Join(shown, " "+tokens.Plain.Glyph(tokens.GSeparator)+" ")
	}
	if len(s.Configuration.Voters) > 0 {
		fmt.Fprintf(out, "deciding        %s    %d of %d needed\n", join(s.Configuration.Voters), len(s.Configuration.Voters)/2+1, len(s.Configuration.Voters))
	}
	if len(s.Configuration.Learners) > 0 {
		fmt.Fprintf(out, "keeping copies  %s\n", join(s.Configuration.Learners))
	}
	return nil
}
