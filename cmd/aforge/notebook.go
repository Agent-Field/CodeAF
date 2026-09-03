package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/charmbracelet/lipgloss"
)

var notebookAliasStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#686A78", Dark: "#6C7086"}).
	Faint(true)

func runNotebook(args []string) error {
	return runNotebookTo(args, os.Stdout, time.Now())
}

func runNotebookTo(args []string, output io.Writer, now time.Time) error {
	flags := commandFlags("notebook")
	database := flags.String("db", defaultChatDB(), storeFlagHelp)
	if err := parseCommandFlags(flags, reorder(flags, args)); err != nil {
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
	aliases, err := graph.ScopeAliases()
	if err != nil {
		return err
	}
	table := tabwriter.NewWriter(output, 0, 4, 2, ' ', 0)
	fmt.Fprintln(table, "SEQ\tSCOPE\tKIND\tAGE\tUSES\tRIDES\tBAD\tSTATUS\tBELIEF")
	for _, fact := range facts {
		canonical, err := graph.ResolveScope(fact.Scope)
		if err != nil {
			return err
		}
		outcome := outcomes[fact.Seq]
		fmt.Fprintf(table, "#%d\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			fact.Seq, canonical, fact.Kind, store.AgeLabel(fact.Time, now), fact.Uses,
			outcome.Rides, outcome.Bad, fact.Status, oneLineFact(fact.Body))
	}
	if err := table.Flush(); err != nil {
		return fmt.Errorf("write notebook: %w", err)
	}
	dailyBudget, err := config.DailyBudgetUSD()
	if err != nil {
		return err
	}
	rail, err := graph.DailyRailToday(dailyBudget)
	if err != nil {
		return err
	}
	// THE EMPTINESS LAW. A day that has cost nothing says nothing about what it
	// cost: the rail is a figure somebody chose and is printed, and `$0.00 of
	// $500.00` is a measurement nobody made ([config.SpentFigure]).
	fmt.Fprintln(output)
	spent := config.SpentFigure(rail.Spend)
	switch {
	case rail.Unlimited && spent == "":
		fmt.Fprintln(output, "daily rail: unlimited")
	case rail.Unlimited:
		fmt.Fprintf(output, "today's spend: %s; daily rail unlimited\n", spent)
	case spent == "":
		fmt.Fprintf(output, "daily rail: $%.2f\n", rail.Ceiling)
	default:
		fmt.Fprintf(output, "today's spend: %s of $%.2f daily rail\n", spent, rail.Ceiling)
	}
	if len(aliases) > 0 {
		fmt.Fprintln(output)
		fmt.Fprintln(output, notebookAliasStyle.Render("aliases"))
		for _, alias := range aliases {
			fmt.Fprintln(output, notebookAliasStyle.Render("  "+alias.From+" → "+alias.To))
		}
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
