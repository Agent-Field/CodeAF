package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

func runNotebook(args []string) error {
	return runNotebookTo(args, os.Stdout, time.Now())
}

func runNotebookTo(args []string, output io.Writer, now time.Time) error {
	flags := flag.NewFlagSet("notebook", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	database := flags.String("db", defaultChatDB(), "path to the durable graph database")
	if err := flags.Parse(reorder(args, map[string]bool{"db": true})); err != nil {
		return err
	}
	path, err := expandHome(strings.TrimSpace(*database))
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("open notebook: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("open notebook: %s is not a regular database file", path)
	}
	graph, err := store.Open(path)
	if err != nil {
		return err
	}
	defer graph.Close()

	rest := flags.Args()
	if len(rest) == 0 {
		return writeNotebook(output, graph, now)
	}
	if len(rest) != 2 {
		return fmt.Errorf("usage: aforge notebook retract|restore <seq> [--db path]")
	}
	seq, err := parseFactSeq(rest[1])
	if err != nil {
		return err
	}
	fact, found, err := graph.FactBySeq(seq)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("notebook fact #%d not found", seq)
	}
	switch rest[0] {
	case "retract":
		if err := graph.QuarantineFact(seq, 0, store.FactOriginCLI); err != nil {
			return err
		}
		fmt.Fprintf(output, "quarantined #%d: %s\n", seq, oneLineFact(fact.Body))
	case "restore":
		if err := graph.RestoreFact(seq, store.FactOriginCLI); err != nil {
			return err
		}
		fmt.Fprintf(output, "restored #%d: %s\n", seq, oneLineFact(fact.Body))
	default:
		return fmt.Errorf("unknown notebook command %q", rest[0])
	}
	return nil
}

func writeNotebook(output io.Writer, graph *store.Store, now time.Time) error {
	facts, err := graph.Facts(0)
	if err != nil {
		return err
	}
	outcomes, err := graph.FactOutcomes()
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "SEQ\tSCOPE\tKIND\tAGE\tUSES\tRIDES\tBAD\tSTATUS\tBELIEF")
	for _, fact := range facts {
		outcome := outcomes[fact.Seq]
		fmt.Fprintf(table, "#%d\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			fact.Seq, fact.Scope, fact.Kind, store.AgeLabel(fact.Time, now), fact.Uses,
			outcome.Rides, outcome.Bad, fact.Status, oneLineFact(fact.Body))
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write notebook: %w", err)
	}
	return nil
}

func parseFactSeq(raw string) (int64, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "#"))
	seq, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || seq <= 0 {
		return 0, fmt.Errorf("invalid notebook fact sequence %q", raw)
	}
	return seq, nil
}

func oneLineFact(body string) string {
	return strings.Join(strings.Fields(body), " ")
}
