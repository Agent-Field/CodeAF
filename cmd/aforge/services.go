package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func runServices(args []string) error {
	flags := commandFlags("services")
	database := flags.String("db", defaultChatDB(), storeFlagHelp)
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
		return err
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()

	remaining := flags.Args()
	if len(remaining) == 0 {
		services, err := graph.ActiveServices()
		if err != nil {
			return err
		}
		// A LISTING WITH NOTHING IN IT SAYS SO. Silence and exit 0 is what this
		// door used to answer on a healthy machine, and silence is
		// indistinguishable from a command that broke — which is the one
		// reading the emptiness law exists to prevent. `aforge cache` has had
		// the sentence all along and is the model for it.
		if len(services) == 0 {
			_, err := fmt.Println("nothing is being kept running.")
			return err
		}
		// AND WHEN THERE IS SOMETHING, IT IS READABLE.
		//
		// These rows used to be five raw tab-separated fields with no header
		// and no alignment — `dev-server\trunning\t2h\tport:5173\t/tmp/dev.log`
		// — which is a machine's shape printed at a person, and the only
		// listing in the binary that had it. `notebook` and `why` have both
		// drawn a headed, aligned table all along; this one now draws the same
		// one, from the same tabwriter, so a person reading two listings reads
		// one shape. There is a header here because there are rows: the rule is
		// that a header is never drawn WITHOUT one.
		now := time.Now()
		table := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
		fmt.Fprintln(table, "NAME\tSTATUS\tAGE\tHEALTH\tLOG")
		for _, service := range services {
			fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\n", service.Name, service.Status,
				serviceAge(service.StartedAt, now), service.Health.String(), service.LogPath)
		}
		return table.Flush()
	}
	if len(remaining) != 2 || remaining[0] != "stop" {
		return fmt.Errorf("usage: aforge services [--db path] | aforge services stop <name> [--db path]")
	}
	service, found, err := graph.ServiceByName(remaining[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("service %q is not running", remaining[1])
	}
	if err := resident.NewServiceSupervisor(graph).Stop(service.ID, "stopped from aforge services"); err != nil {
		return err
	}
	// The receipt for one service, in the register every other one-line answer
	// in this binary is written in — a name, a middle dot, what happened —
	// rather than two fields with a tab between them for a reader that is not
	// there.
	fmt.Printf("%s · stopped\n", service.Name)
	return nil
}

func serviceAge(started, now time.Time) string {
	if started.IsZero() || now.Before(started) {
		return "0s"
	}
	age := now.Sub(started)
	switch {
	case age < time.Minute:
		return fmt.Sprintf("%ds", int(age/time.Second))
	case age < time.Hour:
		return fmt.Sprintf("%dm", int(age/time.Minute))
	case age < 24*time.Hour:
		return fmt.Sprintf("%dh", int(age/time.Hour))
	default:
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
}
