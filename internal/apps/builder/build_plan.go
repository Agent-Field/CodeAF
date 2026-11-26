package builder

import (
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// A run address belongs to the plan that was approved for writing.
type buildPlan struct {
	ask     string
	runs    []schema.PlanLine
	builtin []schema.PlanBuiltin
}

func plannedWork(ask string, plan schema.Plan, planning bool) buildPlan {
	work := buildPlan{ask: ask, builtin: slices.Clone(plan.Builtins)}
	if !planning {
		work.runs = slices.Clone(plan.Automation)
	}
	for _, id := range explicitRunIDs(ask) {
		if !hasPlanLine(work.runs, id) {
			work.runs = append(work.runs, schema.PlanLine{ID: id, Line: displayName(id, "Run")})
		}
	}
	return work
}

const runWord = "[`'\"]?[a-z][a-z0-9_-]*[`'\"]?"
const runWords = runWord + "(?:(?:\\s*,\\s*(?:and\\s+)?|\\s+and\\s+)" + runWord + ")*"

var runIDLists = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[,.;\n])\s*(?:use\s+|and\s+)?(` + runWords + `)\s+as\s+(?:the\s+)?run\s+ids?\b`),
	regexp.MustCompile(`(?i)\b(?:use|reserve)\s+(?:the\s+)?run\s+ids?\s*:?\s*(` + runWords + `)(?:\s*[.;\n]|$)`),
}

var runIDTokens = regexp.MustCompile(runWord)

func explicitRunIDs(ask string) []string {
	var ids []string
	for _, pattern := range runIDLists {
		for _, match := range pattern.FindAllStringSubmatch(ask, -1) {
			for _, token := range runIDTokens.FindAllString(strings.ToLower(match[1]), -1) {
				id := strings.Trim(token, "`'\"")
				if id != "and" && validTypeWord(id) && !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func hasPlanLine(lines []schema.PlanLine, id string) bool {
	return slices.ContainsFunc(lines, func(line schema.PlanLine) bool { return line.ID == id })
}

func (w *buildPlan) apply(files map[string]string) error {
	plan, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if err != nil || plan.Ask == "" {
		return nil
	}
	for _, line := range w.runs {
		if !hasPlanLine(plan.Automation, line.ID) {
			plan.Automation = append(plan.Automation, line)
		}
	}
	plan.Builtins = slices.Clone(w.builtin)
	compileCapabilities(w.ask, &plan, files)
	w.builtin = slices.Clone(plan.Builtins)
	raw, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return err
	}
	files[schema.PlanFile] = string(raw)
	return nil
}

func (w buildPlan) checkEdit(verb string, args json.RawMessage, files map[string]string) error {
	if verb != "set_run" && verb != "remove_run" {
		return nil
	}
	var input fileCommand
	if json.Unmarshal(args, &input) != nil {
		return nil
	}
	plan, _ := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if verb == "set_run" && slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool {
		return b.ID == input.ID && (b.Kind == builtinCSV || b.Kind == builtinLifecycle)
	}) {
		return errors.New("the plan already uses a built-in for this work. Supply its record settings instead of a run file")
	}
	if verb == "remove_run" && hasPlanLine(w.runs, input.ID) {
		return errors.New("this run is reserved by the plan. Revise the plan before removing it")
	}
	if verb == "set_run" {
		if len(plan.Automation) == 0 && slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool {
			return b.Kind == builtinLifecycle || b.Kind == builtinCSV
		}) {
			return errors.New("the plan uses built-in record capabilities. Declare any separate automation in the plan before writing its run")
		}
		run, err := schema.ParseRun([]byte(input.Text))
		if err == nil && run.Run != input.ID {
			return errors.New("the run declaration must use the same ID as the requested run file")
		}
	}
	return nil
}
