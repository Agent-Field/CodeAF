package main

// plan.go — what one run of the bash-task-loop bench will do, composed before
// anything is executed.
//
// The composition is pure: the same plan in, the same invocations out, with no
// clock, no key and no disk behind it. The dry run prints the plan and exits;
// the live runner executes it one invocation at a time. That split is
// bench/README.md's own honest-wiring rule — "compose every invocation and
// execute none of them" — carried over to a Go driver: a flag that moved or a
// brief that drifted is caught by the dry run and by the test, not by the
// first paid cell.

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The pinned model. One model for both arms is what makes the comparison a
// comparison (DESIGN.md, "The comparison"); it is the bench's recorded model
// and the same slug the e2e lanes run. A run may name another model with
// -model; both arms of THAT run ride it, and its rows live in their own CSV.
const pinnedModel = "deepseek/deepseek-v4-flash"

// pinModel is the model the current run pins, in one variable so the plan,
// the dry run and the live runner quote the same word.
var pinModel = pinnedModel

// setPinnedModel moves the pin for this run. An empty answer keeps the
// recorded model.
func setPinnedModel(m string) {
	if strings.TrimSpace(m) != "" {
		pinModel = strings.TrimSpace(m)
	}
}

// The belt switch, in the engine's own spelling. Arm A runs with it unset —
// the belt as shipped; arm B runs with it set to "bash".
const beltEnvVar = "CODEAF_TASK_BELT"

// defaultCellWall is the per-invocation wall. It is a spend backstop, not a
// work limit — the same sentence bench/README.md writes about CELL_TIMEOUT.
const defaultCellWall = 30 * time.Minute

// Arm is one side of the comparison.
type Arm string

const (
	// ArmShipped is the belt as shipped: the belt env unset.
	ArmShipped Arm = "A"
	// ArmBash is the bash belt: the belt env set to bash.
	ArmBash Arm = "B"
)

// beltEnvFor answers the env this arm sets, in one place so the dry run and
// the live runner cannot disagree about what the arm is.
func beltEnvFor(arm Arm) string {
	if arm == ArmBash {
		return "bash"
	}
	return "" // unset: the belt as shipped
}

// invocation is one task start: one door, one arm, one cell, one replicate.
type invocation struct {
	Door      door
	Arm       Arm
	Cell      string // the cell id, c1..c6
	Replicate int    // 1-based
	Model     string
	Brief     string // the cell's brief, verbatim, as the task gets it
	Wall      time.Duration
	// RunDir is where this invocation keeps its home, its seeded fixture and
	// its readings. It is derived, not composed: the runner needs no other
	// knowledge of the layout.
	RunDir string
}

// label names the invocation the way the CSV and the progress lines do.
func (iv invocation) label() string {
	return fmt.Sprintf("%s-%s-r%d", iv.Arm, iv.Cell, iv.Replicate)
}

// envLine is the arm's belt env as the dry run prints it and as the test
// compares it. Unset is spelled, not blank, so a reader can see the arm A
// case is a deliberate absence and not a missing line.
func (iv invocation) envLine() string {
	if value := beltEnvFor(iv.Arm); value != "" {
		return beltEnvVar + "=" + value
	}
	return beltEnvVar + "=(unset)"
}

// spec is everything about one invocation that BOTH arms must share: the
// model, the wall, the brief. The belt env and the per-invocation directory
// are deliberately outside it — they are the two things that may differ.
func (iv invocation) specFingerprint() string {
	return strings.Join([]string{"model=" + iv.Model, "wall=" + iv.Wall.String(), "brief=" + iv.Brief}, "\n")
}

// plan is the whole run, in execution order.
type plan struct {
	Mode        string // "grid" or "pair"
	Door        door
	Invocations []invocation // ordered: replicates outer, cells inner, arms innermost
	Cells       []cell
	Out         string // the run's output root
	Replicates  int
}

// composeGrid builds the grid plan: every cell crossed with every arm, n
// replicates, interleaved — replicate r takes every cell before replicate
// r+1 starts, and within a cell the two arms run back to back so the pair
// shares the day, the machine and the provider's mood.
func composeGrid(cells []cell, replicates int, out string, wall time.Duration, d door) plan {
	p := plan{Mode: "grid", Door: d, Cells: cells, Out: out, Replicates: replicates}
	for r := 1; r <= replicates; r++ {
		for _, c := range cells {
			for _, arm := range []Arm{ArmShipped, ArmBash} {
				p.Invocations = append(p.Invocations, newInvocation("grid", arm, c, r, out, wall, d))
			}
		}
	}
	return p
}

// composePair builds the same-question pair: one brief, both arms, n=1. It is
// the first smoke — the cheapest way to see the same question answered on
// both belts before paying for the grid.
func composePair(c cell, out string, wall time.Duration, d door) plan {
	p := plan{Mode: "pair", Door: d, Cells: []cell{c}, Out: out, Replicates: 1}
	for _, arm := range []Arm{ArmShipped, ArmBash} {
		p.Invocations = append(p.Invocations, newInvocation("pair", arm, c, 1, out, wall, d))
	}
	return p
}

// newInvocation fills in what every door of the plan shares. The brief is the
// cell's own, verbatim; the run directory is derived from the label so the
// dry run and the live runner name the same place.
func newInvocation(mode string, arm Arm, c cell, replicate int, out string, wall time.Duration, d door) invocation {
	iv := invocation{
		Door: d, Arm: arm, Cell: c.id, Replicate: replicate,
		Model: pinModel, Brief: c.brief, Wall: wall,
	}
	iv.RunDir = filepath.Join(out, iv.label())
	return iv
}

// printPlan writes every invocation the run will make — arm, cell,
// replicate, env, model, wall, brief and where its record lands — and
// nothing else. It is the dry run's whole body: composed, printed, executed
// never.
func printPlan(p plan, w io.Writer) {
	fmt.Fprintf(w, "bashloop %s: %d invocations · door %s · model %s · out %s\n",
		p.Mode, len(p.Invocations), p.Door, pinModel, p.Out)
	if p.Mode == "grid" {
		fmt.Fprintf(w, "cells: %s · replicates: %d · order: replicate, then cell, then arm (A before B)\n",
			strings.Join(cellIDs(p.Cells), " "), p.Replicates)
	} else {
		fmt.Fprintf(w, "cell: %s · one brief, both arms, n=1\n", cellIDs(p.Cells)[0])
	}
	fmt.Fprintln(w)
	for i, iv := range p.Invocations {
		fmt.Fprintf(w, "[%02d/%02d] arm=%s cell=%s replicate=%d\n",
			i+1, len(p.Invocations), iv.Arm, iv.Cell, iv.Replicate)
		fmt.Fprintf(w, "  door: %s\n", iv.Door)
		fmt.Fprintf(w, "  model: %s\n", iv.Model)
		fmt.Fprintf(w, "  env: %s\n", iv.envLine())
		fmt.Fprintf(w, "  wall: %s\n", iv.Wall)
		fmt.Fprintf(w, "  home: %s\n", filepath.Join(iv.RunDir, "home"))
		fmt.Fprintf(w, "  fixture: %s\n", filepath.Join(iv.RunDir, "fixture"))
		if iv.Door == doorDo {
			fmt.Fprintf(w, "  invocation: %s\n", doDoorLine(iv, filepath.Join(iv.RunDir, "fixture"), filepath.Join(iv.RunDir, "home")))
		}
		fmt.Fprintf(w, "  brief: |\n")
		for _, line := range strings.Split(strings.TrimRight(iv.Brief, "\n"), "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
		fmt.Fprintln(w)
	}
}

func cellIDs(cells []cell) []string {
	ids := make([]string, 0, len(cells))
	for _, c := range cells {
		ids = append(ids, c.id)
	}
	return ids
}

// cellByID finds one cell, or says which ones exist.
func cellByID(cells []cell, id string) (cell, error) {
	for _, c := range cells {
		if c.id == id {
			return c, nil
		}
	}
	sort.Slice(cells, func(i, j int) bool { return cells[i].id < cells[j].id })
	return cell{}, fmt.Errorf("no cell %q (have %s)", id, strings.Join(cellIDs(cells), " "))
}
