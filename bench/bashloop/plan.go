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

	"github.com/Agent-Field/codeaf/internal/config"
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

// The belt switch, in the engine's own spelling. Arm A runs with it set to the
// word that turns the harness OFF; arm B runs with it set to "bash".
//
// BOTH ARMS SET IT, AND THAT IS THE POINT. The bash belt is the default now, so
// an arm that left the variable unset would ride the same belt as arm B and the
// driver would report a comparison it never ran — a difference of zero that
// looks like a measurement. Neither arm may rely on the default.
const beltEnvVar = "CODEAF_TASK_BELT"

// defaultCellWall is the per-invocation wall. It is a spend backstop, not a
// work limit — the same sentence bench/README.md writes about CELL_TIMEOUT.
const defaultCellWall = 30 * time.Minute

// Arm is one side of the comparison: which belt a run rides.
type Arm string

const (
	// ArmShipped is the older node belt: the belt env set to "node".
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
	return "node" // the older belt, named rather than defaulted to
}

// Seats names which seats an arm runs on: the one pinned model on every seat,
// or the profile's own crew rows.
type Seats string

const (
	// SeatsOne runs the one -model on every seat — the grid as it was before a
	// crew arm existed, and the default.
	SeatsOne Seats = "one"
	// SeatsCrew runs the five seat rows the machine's own profile holds, so the
	// grid can answer whether a person on the tuned crew is better off beside the
	// one-model comparison.
	SeatsCrew Seats = "crew"
)

// armSpec is one arm of the comparison, named by belt AND seats: A-one,
// B-one, A-crew, B-crew. The belt decides the environment variable; the seats
// decide which models the run resolves.
type armSpec struct {
	Belt  Arm
	Seats Seats
}

// name is the arm's own name, the word -arms selects it by and the word the
// summary table groups it under.
func (a armSpec) name() string { return string(a.Belt) + "-" + string(a.Seats) }

// armLabel names an arm in a run directory or a progress line. The one-model
// arms keep the bare belt letter, so today's grid composes byte for byte the
// way it did before seats existed; only a crew arm carries the extra word,
// which is what keeps its directories from colliding with its twin's.
func armLabel(belt Arm, seats Seats) string {
	if seats == "" || seats == SeatsOne {
		return string(belt)
	}
	return string(belt) + "-" + string(seats)
}

// parseArm reads an arm name back, for -arms.
func parseArm(name string) (armSpec, error) {
	parts := strings.SplitN(strings.TrimSpace(name), "-", 2)
	if len(parts) != 2 {
		return armSpec{}, fmt.Errorf("arm %q is belt-seats, one of %s", name, strings.Join(armNames(), ", "))
	}
	belt, seats := strings.ToUpper(parts[0]), Seats(strings.ToLower(parts[1]))
	if belt != string(ArmShipped) && belt != string(ArmBash) {
		return armSpec{}, fmt.Errorf("arm %q names belt %q; belts are %s or %s", name, parts[0], ArmShipped, ArmBash)
	}
	if seats != SeatsOne && seats != SeatsCrew {
		return armSpec{}, fmt.Errorf("arm %q names seats %q; seats are %s or %s", name, parts[1], SeatsOne, SeatsCrew)
	}
	return armSpec{Belt: Arm(belt), Seats: seats}, nil
}

// armNames lists every arm the grid knows, in the order a person reads them.
func armNames() []string {
	return []string{
		armSpec{ArmShipped, SeatsOne}.name(),
		armSpec{ArmBash, SeatsOne}.name(),
		armSpec{ArmShipped, SeatsCrew}.name(),
		armSpec{ArmBash, SeatsCrew}.name(),
	}
}

// chooseArms answers the arm list one run will make. An explicit -arms names
// them (each belt AND seats); otherwise the one -seats word fills both belts,
// which is how the default run stays A-one then B-one.
func chooseArms(names, seats string) ([]armSpec, error) {
	names = strings.TrimSpace(names)
	if names != "" {
		var chosen []armSpec
		seen := map[string]bool{}
		for _, name := range strings.Split(names, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			arm, err := parseArm(name)
			if err != nil {
				return nil, err
			}
			if seen[arm.name()] {
				continue
			}
			seen[arm.name()] = true
			chosen = append(chosen, arm)
		}
		if len(chosen) == 0 {
			return nil, fmt.Errorf("-arms named nothing")
		}
		return chosen, nil
	}
	word := Seats(strings.ToLower(strings.TrimSpace(seats)))
	if word != SeatsOne && word != SeatsCrew {
		return nil, fmt.Errorf("-seats is %s or %s, got %q", SeatsOne, SeatsCrew, seats)
	}
	return []armSpec{{ArmShipped, word}, {ArmBash, word}}, nil
}

// crewRow is one of the profile's five seat rows, resolved once when a crew
// arm is in the run: the tier, its model, and the rung that filled it.
type crewRow struct {
	Tier  string
	Model string
	Rung  string
}

// resolveCrew reads the five seat rows the machine's own profile holds, in the
// settings sheet's order. It is read before any throwaway home is put in front
// of the process, so the crew it names is the crew a person would find in
// their own settings.
func resolveCrew() []crewRow {
	rows := make([]crewRow, 0, len(config.ModelTiers))
	for _, tier := range config.ModelTiers {
		seat := config.TierSeatAt("", tier)
		rows = append(rows, crewRow{Tier: tier, Model: strings.TrimSpace(seat.Model), Rung: seat.Rung()})
	}
	return rows
}

// armsHaveCrew says whether any chosen arm runs on the crew, so the profile is
// read once for a run that needs it and never for one that does not.
func armsHaveCrew(arms []armSpec) bool {
	for _, arm := range arms {
		if arm.Seats == SeatsCrew {
			return true
		}
	}
	return false
}

// invocation is one task start: one door, one arm, one cell, one replicate.
type invocation struct {
	Door      door
	Arm       Arm
	Seats     Seats
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
	return fmt.Sprintf("%s-%s-r%d", armLabel(iv.Arm, iv.Seats), iv.Cell, iv.Replicate)
}

// envLine is the arm's belt env as the dry run prints it and as the test
// compares it. Every arm names a word, so there is no absent case to spell: an
// arm with a blank env would be an arm riding whatever the default is, which is
// the one thing this comparison may not do.
func (iv invocation) envLine() string {
	return beltEnvVar + "=" + beltEnvFor(iv.Arm)
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
	Arms        []armSpec
	Crew        []crewRow    // the five seat rows, when a crew arm is chosen
	Invocations []invocation // ordered: replicates outer, cells inner, arms innermost
	Cells       []cell
	Out         string // the run's output root
	Replicates  int
}

// composeGrid builds the grid plan: every cell crossed with every arm, n
// replicates, interleaved — replicate r takes every cell before replicate
// r+1 starts, and within a cell the arms run back to back so the pair shares
// the day, the machine and the provider's mood. The arms come in the order
// -arms named them; the default is still A-one then B-one.
func composeGrid(cells []cell, replicates int, out string, wall time.Duration, d door, arms []armSpec, crew []crewRow) plan {
	p := plan{Mode: "grid", Door: d, Arms: arms, Crew: crew, Cells: cells, Out: out, Replicates: replicates}
	for r := 1; r <= replicates; r++ {
		for _, c := range cells {
			for _, arm := range arms {
				p.Invocations = append(p.Invocations, newInvocation("grid", arm, c, r, out, wall, d))
			}
		}
	}
	return p
}

// composePair builds the same-question pair: one brief, both arms, n=1. It is
// the first smoke — the cheapest way to see the same question answered on
// both belts before paying for the grid.
func composePair(c cell, out string, wall time.Duration, d door, arms []armSpec, crew []crewRow) plan {
	p := plan{Mode: "pair", Door: d, Arms: arms, Crew: crew, Cells: []cell{c}, Out: out, Replicates: 1}
	for _, arm := range arms {
		p.Invocations = append(p.Invocations, newInvocation("pair", arm, c, 1, out, wall, d))
	}
	return p
}

// newInvocation fills in what every door of the plan shares. The brief is the
// cell's own, verbatim; the run directory is derived from the label so the
// dry run and the live runner name the same place.
func newInvocation(mode string, arm armSpec, c cell, replicate int, out string, wall time.Duration, d door) invocation {
	iv := invocation{
		Door: d, Arm: arm.Belt, Seats: arm.Seats, Cell: c.id, Replicate: replicate,
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
	if !defaultArms(p.Arms) {
		names := make([]string, 0, len(p.Arms))
		for _, arm := range p.Arms {
			names = append(names, arm.name())
		}
		fmt.Fprintf(w, "arms: %s\n", strings.Join(names, " "))
	}
	fmt.Fprintln(w)
	for i, iv := range p.Invocations {
		fmt.Fprintf(w, "[%02d/%02d] arm=%s cell=%s replicate=%d\n",
			i+1, len(p.Invocations), armLabel(iv.Arm, iv.Seats), iv.Cell, iv.Replicate)
		fmt.Fprintf(w, "  door: %s\n", iv.Door)
		fmt.Fprintf(w, "  model: %s\n", iv.Model)
		if iv.Seats == SeatsCrew {
			for _, row := range p.Crew {
				fmt.Fprintf(w, "  seat: %s %s (%s)\n", row.Tier, row.Model, row.Rung)
			}
		}
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

// defaultArms says whether the arms are exactly the two one-model belts, so
// the printed header stays silent for the grid as it was and only announces
// itself when a run has widened the arms.
func defaultArms(arms []armSpec) bool {
	if len(arms) != 2 {
		return false
	}
	return arms[0] == armSpec{ArmShipped, SeatsOne} && arms[1] == armSpec{ArmBash, SeatsOne}
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
