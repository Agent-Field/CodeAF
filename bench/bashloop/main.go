package main

// command bashloop — the two-arm bench for the bash-only task loop
// (docs/design/bash-task-loop/DESIGN.md, wave 3).
//
// One binary, two arms, six cells, n replicates, interleaved same-day. The
// dry run composes every invocation and executes none of them; the live grid
// starts one task per invocation through the engine's own task door against a
// throwaway home, waits on the landing, and reads the numbers the engine
// already keeps. No model judges anything: every cell is graded by its
// fixture's own checks, per bench/README.md.

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "bashloop: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, out, errOut io.Writer) error {
	fs := flag.NewFlagSet("bashloop", flag.ContinueOnError)
	mode := fs.String("mode", "grid", "grid (every cell, both arms, n replicates) or pair (one brief, both arms, n=1)")
	cells := fs.String("cells", "", "comma-separated cell ids to run (default: all six; pair mode takes one)")
	replicates := fs.Int("replicates", 3, "replicates per cell per arm (grid mode)")
	model := fs.String("model", pinnedModel, "the model both arms run on")
	outDir := fs.String("out", "", "output root (default bench-results/bashloop/<timestamp>)")
	wall := fs.Duration("timeout", defaultCellWall, "per-invocation wall — a spend backstop, not a work limit")
	fixtures := fs.String("fixtures", "", "where the cell fixtures live (default: bench/bashloop/fixtures, or ./fixtures when run from bench/bashloop)")
	var dry bool
	fs.BoolVar(&dry, "dry-run", false, "compose every invocation, print them, execute nothing")
	fs.BoolVar(&dry, "n", false, "shorthand for -dry-run")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Getenv("BENCH_DRY_RUN") == "1" {
		dry = true
	}
	setFixturesRoot(*fixtures)

	all := allCells()
	chosen, err := chooseCells(all, *cells, *mode)
	if err != nil {
		return err
	}
	outRoot := strings.TrimSpace(*outDir)
	if outRoot == "" {
		outRoot = filepath.Join("bench-results", "bashloop", time.Now().Format("20060102-150405"))
	}
	if *model != "" {
		setPinnedModel(*model)
	}

	// The plan is composed before anything is read from the machine — no key,
	// no home, no clock beyond the output directory's name. A dry run prints
	// it and stops; nothing below this line runs.
	var p plan
	switch *mode {
	case "grid":
		p = composeGrid(chosen, *replicates, outRoot, *wall)
	case "pair":
		p = composePair(chosen[0], outRoot, *wall)
	default:
		return fmt.Errorf("-mode is grid or pair, got %q", *mode)
	}
	if dry {
		printPlan(p, out)
		return nil
	}

	return live(p, out, errOut)
}

// chooseCells filters the cell list. Grid mode takes the named subset (all by
// default); pair mode takes exactly one.
func chooseCells(all []cell, names, mode string) ([]cell, error) {
	names = strings.TrimSpace(names)
	if mode == "pair" {
		if names == "" {
			return []cell{all[0]}, nil
		}
		one, err := cellByID(all, names)
		if err != nil {
			return nil, err
		}
		return []cell{one}, nil
	}
	if names == "" {
		return all, nil
	}
	var chosen []cell
	for _, name := range strings.Split(names, ",") {
		c, err := cellByID(all, strings.TrimSpace(name))
		if err != nil {
			return nil, err
		}
		chosen = append(chosen, c)
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("-cells named nothing")
	}
	return chosen, nil
}

// goAvailable says whether the fixture cells can be graded on this machine.
// A missing toolchain is said at the door, not discovered inside the first
// paid cell — the same discipline bench/e2e applies to its own prerequisites.
func goAvailable() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

// setFixturesRoot names where the fixtures live, for a run from outside the
// checkout. An empty answer keeps the resolver's own search.
func setFixturesRoot(dir string) {
	if dir != "" {
		fixturesFlag = dir
	}
}
