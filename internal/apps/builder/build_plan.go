package builder

import (
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

// buildPlan carries approved run addresses and capability choices across edits.
type buildPlan struct {
	ask      string
	runs     []schema.PlanLine
	builtins []schema.PlanBuiltin
	removed  []string
	disabled []string
}

func plannedWork(ask string, plan schema.Plan) buildPlan {
	work := buildPlan{ask: ask, builtins: slices.Clone(plan.Builtins), disabled: removedCapabilities(ask)}
	for _, line := range plan.Automation {
		if removesRun(ask, line.ID) {
			work.removed = append(work.removed, line.ID)
		} else {
			work.runs = append(work.runs, line)
		}
	}
	for _, id := range explicitRunIDs(ask) {
		if !hasPlanLine(work.runs, id) && !slices.Contains(work.removed, id) {
			work.runs = append(work.runs, schema.PlanLine{ID: id, Line: displayName(id, "Run")})
		}
	}
	return work
}

const removalPrefix = `(?i)(?:^|[.!?;\n]\s*)(?:please\s+)?`
const removalVerb = `(?:remove|delete|drop|forget)\s+(?:the\s+)?`

func removesRun(ask, id string) bool {
	name := "[`'\"]?" + regexp.QuoteMeta(id) + "[`'\"]?"
	return regexp.MustCompile(removalPrefix + removalVerb + `(?:run\s+` + name + `|` + name + `\s+run)(?:[\s,.!?;]|$)`).MatchString(ask)
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
	original, err := schema.ParsePlan([]byte(files[schema.PlanFile]))
	if err != nil || original.Ask == "" {
		return nil
	}
	plan := original
	plan.Automation = slices.Clone(plan.Automation)
	plan.Actions = slices.Clone(plan.Actions)
	plan.Builtins = slices.Clone(plan.Builtins)
	for _, id := range w.removed {
		removeRunFiles(files, id)
		remove := func(line schema.PlanLine) bool { return line.ID == id }
		plan.Automation = slices.DeleteFunc(plan.Automation, remove)
		plan.Actions = slices.DeleteFunc(plan.Actions, remove)
		plan.Builtins = slices.DeleteFunc(plan.Builtins, func(b schema.PlanBuiltin) bool { return b.Run == id })
		w.builtins = slices.DeleteFunc(w.builtins, func(b schema.PlanBuiltin) bool { return b.Run == id })
	}
	for _, line := range w.runs {
		if !hasPlanLine(plan.Automation, line.ID) {
			plan.Automation = append(plan.Automation, line)
		}
	}
	for _, b := range append(slices.Clone(w.builtins), plan.Builtins...) {
		if slices.Contains(w.disabled, b.Kind) {
			plan.Actions = slices.DeleteFunc(plan.Actions, func(line schema.PlanLine) bool { return line.ID == b.ID })
		}
	}
	if err := w.compileCapabilities(&plan, files); err != nil {
		return err
	}
	w.builtins = slices.Clone(plan.Builtins)
	if reflect.DeepEqual(original, plan) {
		return nil
	}
	return saveJSON(files, schema.PlanFile, plan)
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
		return b.ID == input.ID && (b.Kind == schema.BuiltinCSV || b.Kind == schema.BuiltinLifecycle)
	}) {
		return errors.New("the plan already uses a built-in for this work. Supply its record settings instead of a run file")
	}
	if verb == "remove_run" && hasPlanLine(w.runs, input.ID) {
		return errors.New("this run is reserved by the plan. Keep it unless the person explicitly asks to remove it by its run ID")
	}
	if verb == "set_run" {
		if slices.Contains(w.removed, input.ID) {
			return errors.New("the person asked to remove this run")
		}
		if len(plan.Automation) == 0 && slices.ContainsFunc(plan.Builtins, func(b schema.PlanBuiltin) bool {
			return b.Kind == schema.BuiltinLifecycle || b.Kind == schema.BuiltinCSV
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

func saveJSON(files map[string]string, path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err == nil {
		files[path] = string(raw)
	}
	return err
}
