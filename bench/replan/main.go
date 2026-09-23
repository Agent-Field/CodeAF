package main

// command replan — the baseline bench for the task engine's replanning
// question (docs/design/replan/DESIGN.md).
//
// Four cells with hidden structure, two arms, n replicates, run through the
// product's own `codeaf do` door on the bash belt, which drives the run
// engine (internal/run) over the fixture's own plan store. Every cell is
// graded by code: the fixture's own suite, the tests left byte-for-byte, and
// one mechanical check per cell. No model judges anything (bench/README.md).
//
// The driver measures before anybody builds: where today's engine repeats
// work, how wide it fans out, what its review round catches and what its
// wakes cost. It changes nothing in the engine; the one switch it flips,
// CODEAF_EXPERIMENT_ROOT_PLAN_SEAT, is an experiment the engine already
// carries (internal/run's RootPlanSeatEnv), off by default.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "replan: %v\n", err)
		os.Exit(1)
	}
}

// options is everything one invocation of the driver was asked for.
type options struct {
	cells      string
	arms       string
	replicates int
	model      string
	planModel  string
	out        string
	wall       time.Duration
	bin        string
	fixtures   string
	parallel   int
	cellCap    float64
	totalCap   float64
	dry        bool
}

func run(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("replan", flag.ContinueOnError)
	fs.SetOutput(errOut)
	var o options
	fs.StringVar(&o.cells, "cells", "", "comma-separated cell ids to run (default: all four)")
	fs.StringVar(&o.arms, "arms", "", "comma-separated arms to run (default: "+strings.Join(armNames(), ",")+")")
	fs.IntVar(&o.replicates, "replicates", 2, "replicates per cell per arm")
	fs.StringVar(&o.model, "model", defaultWorkModel, "the work seat's model, the same on every arm")
	fs.StringVar(&o.planModel, "plan-model", defaultPlanModel, "the plan seat's model, the same on every arm")
	fs.StringVar(&o.out, "out", "", "output root (default bench-results/replan/<timestamp>)")
	fs.DurationVar(&o.wall, "timeout", defaultWall, "per-invocation wall, handed to the door as its own -timeout")
	fs.StringVar(&o.bin, "bin", "bin/codeaf", "the product binary the do door runs (build it with make build)")
	fs.StringVar(&o.fixtures, "fixtures", "", "where the cell fixtures live (default: bench/replan/fixtures, or ./fixtures)")
	fs.IntVar(&o.parallel, "parallel", 1, "how many invocations run at once; keep it at 1 or 2 on a shared machine")
	fs.Float64Var(&o.cellCap, "cell-cap", 1.50, "dollars one invocation may spend before the driver interrupts it")
	fs.Float64Var(&o.totalCap, "total-cap", 9.00, "dollars the whole run may spend; nothing new starts past it")
	fs.BoolVar(&o.dry, "dry-run", false, "compose every invocation, print them, execute nothing")
	fs.BoolVar(&o.dry, "n", false, "shorthand for -dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Getenv("BENCH_DRY_RUN") == "1" {
		o.dry = true
	}
	if o.fixtures != "" {
		fixturesFlag = o.fixtures
	}
	if o.replicates < 1 {
		return fmt.Errorf("-replicates must be at least 1")
	}
	if o.parallel < 1 {
		o.parallel = 1
	}
	cells, err := chooseCells(o.cells)
	if err != nil {
		return err
	}
	arms, err := chooseArms(o.arms)
	if err != nil {
		return err
	}
	outRoot := strings.TrimSpace(o.out)
	if outRoot == "" {
		outRoot = filepath.Join("bench-results", "replan", time.Now().Format("20060102-150405"))
	}
	// THE ROOT IS ABSOLUTE, because the door runs in a working directory of its
	// own and every path it is handed has to mean the same place there.
	if abs, err := filepath.Abs(outRoot); err == nil {
		outRoot = abs
	}
	p := composePlan(cells, arms, o, outRoot)
	if o.dry {
		printPlan(p, out)
		return nil
	}
	return live(p, o, out)
}
