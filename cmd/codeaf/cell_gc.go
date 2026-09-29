package main

import (
	"context"
	"fmt"
	"io"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellbudget"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/config"
)

// cellGC brings the cells' working files under the disk budget now.
func cellGC(_ cell.Cell, args []string, out io.Writer) error {
	flags := commandFlags("cell gc")
	dry := flags.Bool("dry-run", false, "say what would be removed and remove nothing")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: codeaf cell gc [--dry-run]")
	}
	rep, err := cellBudget().Collect(context.Background(), *dry)
	if err != nil {
		return err
	}
	writeGCReport(out, rep)
	return nil
}

func cellBudget() cellbudget.Manager {
	m := cellbudget.New(cellstore.Engine{}, cellBusy)
	m.Limit = int64(config.CellBudgetGBAt(config.ProfileDir())) << 30
	return m
}

func writeGCReport(out io.Writer, rep cellbudget.Report) {
	fmt.Fprintf(out, "cells hold %s, budget %s\n", gcSize(rep.Total), gcLimit(rep.Limit))
	for _, a := range rep.Actions {
		fmt.Fprintf(out, "  %s  %s  %s%s\n", a.ID, gcSize(a.Bytes), a.Outcome, gcWhy(a.Why))
	}
}

func gcWhy(why string) string {
	if why == "" {
		return ""
	}
	return " (" + why + ")"
}

func gcLimit(n int64) string {
	if n <= 0 {
		return "unlimited"
	}
	return gcSize(n)
}

func gcSize(n int64) string { return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20)) }
