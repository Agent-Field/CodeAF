package draft

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
)

// MaxPlanLine is how long the worker may write one card line. The plan schema
// allows 200; a shorter line reads better on the card.
const MaxPlanLine = 120

// MaxNameLen is the longest app name the plan card has room for.
const MaxNameLen = 40

// planGroup is one list on the plan card that takes id-and-line entries.
type planGroup struct {
	word  string // what the worker types: data, run, screen
	lines func(*State) *[]spec.PlanLine
}

var planGroups = []planGroup{
	{"data", func(st *State) *[]spec.PlanLine { return &st.Plan.Data }},
	{"run", func(st *State) *[]spec.PlanLine { return &st.Plan.Automation }},
	{"screen", func(st *State) *[]spec.PlanLine { return &st.Plan.Screens }},
}

func groupByWord(word string) (planGroup, bool) {
	for _, g := range planGroups {
		if g.word == word {
			return g, true
		}
	}
	return planGroup{}, false
}

func init() {
	for _, g := range planGroups {
		register("plan "+g.word, planLineDef(g))
	}
	register("name", nameDef())
	register("tint", tintDef())
	register("plan agent", planAgentDef())
	register("plan drop", planDropDef())
	register("plan ready", planReadyDef())
	register("phase", phaseDef())
}

type nameArgs struct{ name, about, tint string }

func nameDef() def[nameArgs] {
	return def[nameArgs]{
		Usage:  `codeaf app name "<Name>" [--about "<one sentence>"] [--tint <name>]`,
		Values: []string{"about", "tint"},
		Decode: func(a argv, _ string) (nameArgs, error) {
			if err := a.exactly(1); err != nil {
				return nameArgs{}, err
			}
			return nameArgs{strings.TrimSpace(a.pos[0]), strings.TrimSpace(a.str("about")), a.str("tint")}, nil
		},
		Piece: func(nameArgs) Piece { return Piece{Kind: "name"} },
		Check: func(_ State, _ Entry, n nameArgs) error {
			if n.name == "" || runeLen(n.name) > MaxNameLen {
				return refuse("the app name must be 1 to %d characters, got %d — give a short name such as \"CRM\"", MaxNameLen, runeLen(n.name))
			}
			return checkTint(n.tint)
		},
		Apply: func(st *State, n nameArgs) {
			st.Tint = orElse(n.tint, st.Tint)
			st.Name = n.name
			if n.about != "" {
				st.About = n.about
			}
		},
		Line: func(_ State, n nameArgs) string { return "Name · " + n.name },
	}
}

type planLineArgs struct{ id, line string }

func planLineDef(g planGroup) def[planLineArgs] {
	return def[planLineArgs]{
		Usage: "codeaf app plan " + g.word + ` <id> "<line>"`,
		Decode: func(a argv, _ string) (planLineArgs, error) {
			if err := a.exactly(2); err != nil {
				return planLineArgs{}, err
			}
			return planLineArgs{a.pos[0], strings.TrimSpace(a.pos[1])}, nil
		},
		Piece: func(p planLineArgs) Piece { return Piece{Kind: "plan." + g.word, ID: p.id} },
		Check: func(st State, e Entry, p planLineArgs) error {
			if err := checkSlug("plan "+g.word, p.id); err != nil {
				return err
			}
			if err := checkPlanLine(p.line); err != nil {
				return err
			}
			// The owner edits a line in place; the worker may not silently
			// overwrite a line it or the owner already wrote.
			if e.Author != AuthorOwner && planHas(*g.lines(&st), p.id) {
				return refuse("plan %s %q already exists — drop it first with: codeaf app plan drop %s %s", g.word, p.id, g.word, p.id)
			}
			return nil
		},
		Apply: func(st *State, p planLineArgs) { setPlanLine(g.lines(st), spec.PlanLine{ID: p.id, Line: p.line}) },
		Line:  func(_ State, p planLineArgs) string { return p.line },
	}
}

func checkPlanLine(line string) error {
	if line == "" {
		return refuse("a plan line must not be empty — write one plain sentence the owner can read")
	}
	if runeLen(line) > MaxPlanLine {
		return refuse("a plan line is at most %d characters, got %d — shorten it", MaxPlanLine, runeLen(line))
	}
	return nil
}

func planHas(lines []spec.PlanLine, id string) bool {
	for _, l := range lines {
		if l.ID == id {
			return true
		}
	}
	return false
}

func setPlanLine(lines *[]spec.PlanLine, line spec.PlanLine) {
	for i, l := range *lines {
		if l.ID == line.ID {
			(*lines)[i] = line
			return
		}
	}
	*lines = append(*lines, line)
}

type planAgentArgs struct{ line string }

func planAgentDef() def[planAgentArgs] {
	return def[planAgentArgs]{
		Usage: `codeaf app plan agent "<line>"`,
		Decode: func(a argv, _ string) (planAgentArgs, error) {
			if err := a.exactly(1); err != nil {
				return planAgentArgs{}, err
			}
			return planAgentArgs{strings.TrimSpace(a.pos[0])}, nil
		},
		Piece: func(planAgentArgs) Piece { return Piece{Kind: "plan.agent", ID: "agent"} },
		Check: func(_ State, _ Entry, p planAgentArgs) error { return checkPlanLine(p.line) },
		Apply: func(st *State, p planAgentArgs) { st.Plan.Agent = spec.PlanLine{ID: "agent", Line: p.line} },
		Line:  func(_ State, p planAgentArgs) string { return p.line },
	}
}

type planDropArgs struct{ group, id string }

func planDropDef() def[planDropArgs] {
	return def[planDropArgs]{
		Usage: "codeaf app plan drop <data|run|screen> <id> | codeaf app plan drop agent",
		Decode: func(a argv, _ string) (planDropArgs, error) {
			if len(a.pos) == 1 && a.pos[0] == "agent" {
				return planDropArgs{group: "agent", id: "agent"}, nil
			}
			if err := a.exactly(2); err != nil {
				return planDropArgs{}, err
			}
			return planDropArgs{a.pos[0], a.pos[1]}, nil
		},
		Piece: func(p planDropArgs) Piece { return Piece{Kind: "drop", ID: "plan." + p.group + ":" + p.id} },
		Check: func(st State, _ Entry, p planDropArgs) error {
			if p.group == "agent" {
				if st.Plan.Agent.Line == "" {
					return refuse("there is no plan agent line to drop")
				}
				return nil
			}
			g, ok := groupByWord(p.group)
			if !ok {
				return usage("plan drop group must be data, run, screen or agent, got %q", p.group)
			}
			if !planHas(*g.lines(&st), p.id) {
				return refuse("plan %s %q does not exist (have: %s)", p.group, p.id, ids(planIDs(*g.lines(&st))))
			}
			return nil
		},
		Apply: func(st *State, p planDropArgs) {
			if p.group == "agent" {
				st.Plan.Agent = spec.PlanLine{}
				return
			}
			g, _ := groupByWord(p.group)
			lines := g.lines(st)
			kept := (*lines)[:0:0]
			for _, l := range *lines {
				if l.ID != p.id {
					kept = append(kept, l)
				}
			}
			*lines = kept
		},
		Line: func(_ State, p planDropArgs) string { return "Removed plan " + p.group + " " + p.id },
	}
}

func planIDs(lines []spec.PlanLine) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.ID
	}
	return out
}

type noArgs struct{}

func planReadyDef() def[noArgs] {
	return def[noArgs]{
		Usage:  "codeaf app plan ready",
		Decode: func(a argv, _ string) (noArgs, error) { return noArgs{}, a.exactly(0) },
		Piece:  func(noArgs) Piece { return Piece{Kind: "plan.ready"} },
		Check: func(st State, _ Entry, _ noArgs) error {
			var missing []string
			if st.Name == "" {
				missing = append(missing, `a name (codeaf app name "<Name>")`)
			}
			if len(st.Plan.Data) == 0 {
				missing = append(missing, `at least one data line (plan data <id> "<line>")`)
			}
			if len(st.Plan.Screens) == 0 {
				missing = append(missing, `at least one screen line (plan screen <id> "<line>")`)
			}
			if st.Plan.Agent.Line == "" {
				missing = append(missing, `an agent line (plan agent "<line>")`)
			}
			if len(missing) > 0 {
				return refuse("the plan is not ready: it needs %s", strings.Join(missing, ", "))
			}
			return nil
		},
		Apply: func(st *State, _ noArgs) { st.Phase = PhasePlanned },
		Line:  func(State, noArgs) string { return "Plan ready" },
	}
}

type phaseArgs struct {
	phase Phase
	note  string
}

// phaseDef is written only by codeaf, after Check passed. It is internal so a
// worker cannot declare itself done.
func phaseDef() def[phaseArgs] {
	return def[phaseArgs]{
		Usage:    "codeaf app phase done",
		Body:     true,
		Internal: true,
		Decode: func(a argv, body string) (phaseArgs, error) {
			if err := a.exactly(1); err != nil {
				return phaseArgs{}, err
			}
			return phaseArgs{Phase(a.pos[0]), strings.TrimSpace(body)}, nil
		},
		Piece: func(phaseArgs) Piece { return Piece{Kind: "phase", ID: "done"} },
		Check: func(_ State, _ Entry, p phaseArgs) error {
			if p.phase != PhaseDone {
				return usage("phase must be done, got %q", p.phase)
			}
			return nil
		},
		Apply: func(st *State, p phaseArgs) { st.Phase, st.Done = p.phase, p.note },
		Line: func(_ State, p phaseArgs) string {
			if p.note == "" {
				return "Done"
			}
			return "Done · " + p.note
		},
	}
}

type tintArgs struct{ tint string }

func tintDef() def[tintArgs] {
	return def[tintArgs]{
		Usage: "codeaf app tint <" + strings.Join(spec.Tints, "|") + ">",
		Decode: func(a argv, _ string) (tintArgs, error) {
			if err := a.exactly(1); err != nil {
				return tintArgs{}, err
			}
			return tintArgs{a.pos[0]}, nil
		},
		Piece: func(tintArgs) Piece { return Piece{Kind: "tint"} },
		Check: func(_ State, _ Entry, t tintArgs) error { return checkTint(t.tint) },
		Apply: func(st *State, t tintArgs) { st.Tint = t.tint },
		Line:  func(_ State, t tintArgs) string { return "Tint · " + t.tint },
	}
}

// checkTint accepts "" (no tint chosen) or one of spec.Tints.
func checkTint(name string) error {
	if name == "" || contains(spec.Tints, name) {
		return nil
	}
	return refuse("tint %q is not one of %s", name, strings.Join(spec.Tints, ", "))
}
