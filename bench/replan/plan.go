package main

// plan.go: what one run of the baseline will do, composed before anything is
// executed. The composition is pure: no clock, no key and no disk behind it, so
// the dry run and the test read exactly what the live run would do
// (bench/README.md's honest-wiring rule).

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The two seats every arm runs on. ONE OPEN-WEIGHT MODEL PER SEAT, THE SAME ON
// BOTH ARMS: the work seat is the bench's recorded cheap model, and the plan
// seat is a stronger open-weight model, so an arm that moves the root onto the
// plan seat actually changes the model that makes the first split decision.
const (
	defaultWorkModel = "deepseek/deepseek-v4-flash"
	defaultPlanModel = "z-ai/glm-5.3"
)

// defaultWall is the per-invocation wall, handed to the door as its own
// -timeout. It is a spend backstop, not a work limit.
const defaultWall = 20 * time.Minute

// The environment the arms differ by, and the ones every arm sets alike. They
// are spelled here once so the dry run, the live run and the test agree.
const (
	// rootPlanSeatVar is internal/run's experiment switch (RootPlanSeatEnv).
	rootPlanSeatVar = "CODEAF_EXPERIMENT_ROOT_PLAN_SEAT"
	// beltVar selects the bash belt, which is what routes `codeaf do` onto the
	// run engine (cmd/codeaf/do.go's runErrand). Both arms name it rather than
	// ride a default that could move under them.
	beltVar = "CODEAF_TASK_BELT"
	// homeVar is the throwaway home each invocation gets.
	homeVar = "CODEAF_HOME"
	// telemetryVar keeps the bench's own runs out of the product's usage
	// counts.
	telemetryVar = "CODEAF_TELEMETRY"
)

// arm is one side of the comparison: a name and the one switch value it sets.
type arm struct {
	Name     string
	RootPlan string // the value the arm gives rootPlanSeatVar
}

// allArms is today's engine and the experiment beside it. BOTH ARMS SET THE
// SWITCH, one to a word that leaves it off, so neither rides whatever the
// default happens to be.
func allArms() []arm {
	return []arm{
		{Name: "B-now", RootPlan: "off"},
		{Name: "B-rootplan", RootPlan: "1"},
	}
}

func armNames() []string {
	var names []string
	for _, a := range allArms() {
		names = append(names, a.Name)
	}
	return names
}

// chooseArms filters the arm list by name.
func chooseArms(names string) ([]arm, error) {
	names = strings.TrimSpace(names)
	if names == "" {
		return allArms(), nil
	}
	var chosen []arm
	seen := map[string]bool{}
	for _, name := range strings.Split(names, ",") {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		found := false
		for _, a := range allArms() {
			if strings.EqualFold(a.Name, name) {
				chosen = append(chosen, a)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no arm %q (have %s)", name, strings.Join(armNames(), ", "))
		}
		seen[name] = true
	}
	if len(chosen) == 0 {
		return nil, fmt.Errorf("-arms named nothing")
	}
	return chosen, nil
}

// invocation is one run of the door: one arm, one cell, one replicate.
type invocation struct {
	Arm       arm
	Cell      cell
	Replicate int
	Model     string
	PlanModel string
	Wall      time.Duration
	Bin       string
	RunDir    string
}

func (iv invocation) label() string {
	return fmt.Sprintf("%s-%s-r%d", iv.Arm.Name, iv.Cell.id, iv.Replicate)
}

func (iv invocation) homeDir() string    { return filepath.Join(iv.RunDir, "home") }
func (iv invocation) fixtureDir() string { return filepath.Join(iv.RunDir, "fixture") }
func (iv invocation) briefFile() string  { return filepath.Join(iv.RunDir, "brief.md") }

// argv is the door's own command line, the rig's shape (bench/bashloop's do
// door): the work seat and the plan seat named by flag, the wall, consent to
// spend, and one JSON envelope on stdout. The brief arrives on stdin.
func (iv invocation) argv() []string {
	return []string{
		"do",
		"-w", iv.fixtureDir(),
		"-model", iv.Model,
		"-plan-model", iv.PlanModel,
		"-timeout", fmt.Sprintf("%ds", int(iv.Wall.Seconds())),
		"-yes-spend",
		"-json",
	}
}

// armEnv is every variable this invocation sets on the door, in a fixed order.
// The provider key is NOT here: it is inherited from the driver's own
// environment and never written anywhere, least of all into the throwaway
// profile.
func (iv invocation) armEnv() []string {
	home := iv.homeDir()
	return []string{
		homeVar + "=" + home,
		"HOME=" + home,
		beltVar + "=bash",
		telemetryVar + "=off",
		rootPlanSeatVar + "=" + iv.Arm.RootPlan,
		"GIT_AUTHOR_NAME=replan bench",
		"GIT_AUTHOR_EMAIL=bench@localhost",
		"GIT_COMMITTER_NAME=replan bench",
		"GIT_COMMITTER_EMAIL=bench@localhost",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
	}
}

// specFingerprint is everything both arms of one cell and replicate must
// share. The switch and the run directory are the two things that may differ.
func (iv invocation) specFingerprint() string {
	return strings.Join([]string{
		"cell=" + iv.Cell.id, "model=" + iv.Model, "plan=" + iv.PlanModel,
		"wall=" + iv.Wall.String(), "brief=" + iv.Cell.brief,
	}, "\n")
}

// plan is the whole run, in execution order.
type plan struct {
	Cells       []cell
	Arms        []arm
	Replicates  int
	Out         string
	Invocations []invocation
}

// composePlan builds the grid: replicate outer, cell, then arm innermost, so
// the two arms of one cell run back to back and share the machine's mood.
func composePlan(cells []cell, arms []arm, o options, out string) plan {
	p := plan{Cells: cells, Arms: arms, Replicates: o.replicates, Out: out}
	for r := 1; r <= o.replicates; r++ {
		for _, c := range cells {
			for _, a := range arms {
				iv := invocation{
					Arm: a, Cell: c, Replicate: r,
					Model: o.model, PlanModel: o.planModel, Wall: o.wall, Bin: o.bin,
				}
				iv.RunDir = filepath.Join(out, iv.label())
				p.Invocations = append(p.Invocations, iv)
			}
		}
	}
	return p
}

// printPlan is the dry run's whole body.
func printPlan(p plan, w io.Writer) {
	names := make([]string, 0, len(p.Arms))
	for _, a := range p.Arms {
		names = append(names, a.Name)
	}
	ids := make([]string, 0, len(p.Cells))
	for _, c := range p.Cells {
		ids = append(ids, c.id)
	}
	fmt.Fprintf(w, "replan: %d invocations · cells %s · arms %s · replicates %d · out %s\n\n",
		len(p.Invocations), strings.Join(ids, " "), strings.Join(names, " "), p.Replicates, p.Out)
	for i, iv := range p.Invocations {
		fmt.Fprintf(w, "[%02d/%02d] %s\n", i+1, len(p.Invocations), iv.label())
		fmt.Fprintf(w, "  cell: %s (hidden: %s)\n", iv.Cell.name, iv.Cell.hidden)
		env := append([]string(nil), iv.armEnv()...)
		sort.Strings(env)
		for _, kv := range env {
			fmt.Fprintf(w, "  env: %s\n", kv)
		}
		fmt.Fprintf(w, "  run: %s %s < %s\n", iv.Bin, strings.Join(iv.argv(), " "), iv.briefFile())
		fmt.Fprintf(w, "  brief: |\n")
		for _, line := range strings.Split(strings.TrimRight(iv.Cell.brief, "\n"), "\n") {
			fmt.Fprintf(w, "    %s\n", line)
		}
		fmt.Fprintln(w)
	}
}
