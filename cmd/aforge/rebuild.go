package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/lease"
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// The whole architecture rests on one claim: every table in this database is a
// projection of the journal, and can be thrown away and rebuilt from it. Store.
// Rebuild is the routine that makes the claim good — and it had no caller
// outside the test suite, which meant the claim was only ever exercised where
// it was already believed. A guarantee nobody can invoke is an aspiration.
//
// So there is a command. It is the operator's recovery path from a corrupted
// view, and it is also the only way a future event kind whose view write was
// forgotten will ever be noticed: replay writes what the live path wrote, and
// the difference shows up here or nowhere.
func runRebuild(args []string) error {
	return runRebuildWith(args, os.Stdin, os.Stdout)
}

func runRebuildWith(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("rebuild", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	yes := flags.Bool("yes", false, "skip the confirmation prompt")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true})); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge rebuild [--db path] [--yes]")
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open store: %s is not a regular database file", path)
	}
	// A live resident is mid-write in every table this is about to replace. The
	// rebuild is one transaction and would not corrupt anything, but the
	// reconciler's in-memory carries would be reasoning about a graph that
	// moved underneath them, so the honest answer is to ask the operator to
	// close it first.
	holder, err := lease.ProbeResident(path)
	if err != nil {
		return err
	}
	if holder != nil {
		return fmt.Errorf("a resident is running (pid %d) — close it before rebuilding", holder.PID)
	}
	if !*yes {
		fmt.Fprintf(output, "Rebuild every materialized view in %s from the event journal?\n", path)
		fmt.Fprint(output, "The journal itself is untouched; everything derived from it is discarded and replayed. [y/N] ")
		reader := bufio.NewReader(input)
		answer, readErr := reader.ReadString('\n')
		if readErr != nil && strings.TrimSpace(answer) == "" {
			return fmt.Errorf("rebuild cancelled")
		}
		if reply := strings.ToLower(strings.TrimSpace(answer)); reply != "y" && reply != "yes" {
			_, err = fmt.Fprintln(output, "cancelled")
			return err
		}
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()
	if err := graph.Rebuild(); err != nil {
		return fmt.Errorf("rebuild: %w", err)
	}
	nodes, err := graph.Nodes()
	if err != nil {
		return err
	}
	events, err := graph.LatestEventSeq()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "rebuilt %d nodes from %d journaled events\n", len(nodes), events)
	return err
}
