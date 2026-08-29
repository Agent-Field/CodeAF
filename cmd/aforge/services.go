package main

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

func runServices(args []string) error {
	flags := flag.NewFlagSet("services", flag.ContinueOnError)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	if err := flags.Parse(reorder(flags, args)); err != nil {
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
		now := time.Now()
		for _, service := range services {
			fmt.Printf("%s\t%s\t%s\t%s\t%s\n", service.Name, service.Status,
				serviceAge(service.StartedAt, now), service.Health.String(), service.LogPath)
		}
		return nil
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
	fmt.Printf("%s\tstopped\n", service.Name)
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
