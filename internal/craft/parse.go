package craft

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// The layout of the craft repository. These names are law rather than
// convention: Script and Skill are the two fields in a craft file that become
// an exec call, so the directory they must live under is a constant the
// validator checks against, not a string the writer chooses.
const (
	WorkflowDir = "workflows"
	VerifierDir = "verifiers"
	SkillDir    = "skills"
	ExemplarDir = "exemplars"
)

// SuggestDistance is how far a misspelled reference may sit from a real step
// or param and still be offered as the intended one. Two edits catches typos
// and transpositions; three starts naming the wrong step confidently, and a
// confident wrong suggestion costs a model more than no suggestion at all.
const SuggestDistance = 2

// paramPattern is the substitution syntax declared in types.go. Whitespace
// inside the braces is tolerated because models write both {{topic}} and
// {{ topic }} and neither is a mistake worth an error.
var paramPattern = regexp.MustCompile(`\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// namePattern governs workflow names and step ids. A workflow name becomes a
// path segment and a step id becomes a node name, so both are held to a slug
// that cannot escape a directory or confuse a diff.
var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// paramNamePattern governs param names, which have to be writable inside
// {{...}} without ambiguity.
var paramNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// The file shape. These mirror the frozen types one for one in lowercase
// snake, and their field order is the field order in types.go — that is what
// makes a saved version's diff honest about what actually changed rather than
// about how a map happened to serialize.
type fileWorkflow struct {
	Name        string      `yaml:"name"`
	Description string      `yaml:"description,omitempty"`
	Params      []fileParam `yaml:"params,omitempty"`
	Steps       []fileStep  `yaml:"steps"`
	Limits      fileLimits  `yaml:"limits,omitempty"`
}

type fileParam struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	Default     string `yaml:"default,omitempty"`
	Required    bool   `yaml:"required,omitempty"`
}

type fileStep struct {
	ID      string       `yaml:"id"`
	Brief   string       `yaml:"brief,omitempty"`
	Needs   []string     `yaml:"needs,omitempty"`
	Model   string       `yaml:"model,omitempty"`
	Skill   string       `yaml:"skill,omitempty"`
	ForEach *fileForEach `yaml:"for_each,omitempty"`
	Verify  *fileVerify  `yaml:"verify,omitempty"`
}

type fileForEach struct {
	Source string `yaml:"source"`
	Fan    int    `yaml:"fan,omitempty"`
}

type fileVerify struct {
	Script    string         `yaml:"script"`
	UntilPass *fileUntilPass `yaml:"until_pass,omitempty"`
}

type fileUntilPass struct {
	Revise    []string `yaml:"revise,omitempty"`
	MaxRounds int      `yaml:"max_rounds,omitempty"`
}

// fileLimits spends wall clock in minutes because that is the unit the
// distiller thinks in; the Go side keeps a Duration.
type fileLimits struct {
	CostUSD float64 `yaml:"cost_usd,omitempty"`
	Minutes float64 `yaml:"minutes,omitempty"`
}

// Parse reads one craft file. It is strict about unknown fields on purpose: a
// brief written under `breif:` is a step that silently does nothing, and a
// step that silently does nothing is the most expensive kind of file to debug.
// Parse checks shape only — the structural law is Validate's.
func Parse(data []byte) (*Workflow, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, errors.New("craft: the file is empty — a workflow needs a name and at least one step")
	}
	var file fileWorkflow
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&file); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("craft: the file has no document — a workflow needs a name and at least one step")
		}
		return nil, readable(err)
	}
	return file.workflow(), nil
}

func (f fileWorkflow) workflow() *Workflow {
	w := &Workflow{
		Name:        strings.TrimSpace(f.Name),
		Description: strings.TrimSpace(f.Description),
		Limits: Limits{
			CostUSD:   f.Limits.CostUSD,
			WallClock: time.Duration(f.Limits.Minutes * float64(time.Minute)),
		},
	}
	for _, param := range f.Params {
		w.Params = append(w.Params, Param{
			Name:        strings.TrimSpace(param.Name),
			Description: strings.TrimSpace(param.Description),
			Default:     param.Default,
			Required:    param.Required,
		})
	}
	for _, step := range f.Steps {
		next := Step{
			ID:    strings.TrimSpace(step.ID),
			Brief: strings.TrimSpace(step.Brief),
			Model: strings.TrimSpace(step.Model),
			Skill: strings.TrimSpace(step.Skill),
		}
		for _, need := range step.Needs {
			next.Needs = append(next.Needs, strings.TrimSpace(need))
		}
		if step.ForEach != nil {
			next.ForEach = &ForEach{Source: strings.TrimSpace(step.ForEach.Source), Fan: step.ForEach.Fan}
		}
		if step.Verify != nil {
			next.Verify = &Verify{Script: strings.TrimSpace(step.Verify.Script)}
			if step.Verify.UntilPass != nil {
				until := &UntilPass{MaxRounds: step.Verify.UntilPass.MaxRounds}
				for _, revise := range step.Verify.UntilPass.Revise {
					until.Revise = append(until.Revise, strings.TrimSpace(revise))
				}
				next.Verify.UntilPass = until
			}
		}
		w.Steps = append(w.Steps, next)
	}
	return w
}

// Marshal writes the workflow back out in the file shape. Commit is
// deliberately absent: version identity belongs to the repository, and a file
// that names its own commit is a file that lies the moment it is edited.
func (w *Workflow) Marshal() ([]byte, error) {
	file := fileWorkflow{
		Name:        w.Name,
		Description: w.Description,
		Limits: fileLimits{
			CostUSD: w.Limits.CostUSD,
			Minutes: w.Limits.WallClock.Minutes(),
		},
	}
	for _, param := range w.Params {
		file.Params = append(file.Params, fileParam{
			Name:        param.Name,
			Description: param.Description,
			Default:     param.Default,
			Required:    param.Required,
		})
	}
	for _, step := range w.Steps {
		next := fileStep{
			ID:    step.ID,
			Brief: step.Brief,
			Needs: step.Needs,
			Model: step.Model,
			Skill: step.Skill,
		}
		if step.ForEach != nil {
			next.ForEach = &fileForEach{Source: step.ForEach.Source, Fan: step.ForEach.Fan}
		}
		if step.Verify != nil {
			next.Verify = &fileVerify{Script: step.Verify.Script}
			if step.Verify.UntilPass != nil {
				next.Verify.UntilPass = &fileUntilPass{
					Revise:    step.Verify.UntilPass.Revise,
					MaxRounds: step.Verify.UntilPass.MaxRounds,
				}
			}
		}
		file.Steps = append(file.Steps, next)
	}
	var buffer bytes.Buffer
	encoder := yaml.NewEncoder(&buffer)
	encoder.SetIndent(2)
	if err := encoder.Encode(file); err != nil {
		return nil, fmt.Errorf("craft %s: write the file: %w", w.Name, err)
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("craft %s: write the file: %w", w.Name, err)
	}
	return buffer.Bytes(), nil
}

// Validate is the whole structural law, and it reports every breach at once.
// One error per call would make a model repair a file in as many round trips
// as it has mistakes; the errors are written to be read together and fixed in
// one edit, each naming the step it belongs to first.
func (w *Workflow) Validate() []error {
	var found []error

	switch {
	case w.Name == "":
		found = append(found, errors.New("workflow: name is empty — a craft file is addressed by name"))
	case !namePattern.MatchString(w.Name):
		found = append(found, fmt.Errorf("workflow %q: name must be lowercase letters, digits, dash or underscore — it becomes a file path", w.Name))
	}

	declared := map[string]bool{}
	var paramNames []string
	for i, param := range w.Params {
		switch {
		case param.Name == "":
			found = append(found, fmt.Errorf("param %d: name is empty — a param is a hole the briefs address by name", i+1))
			continue
		case !paramNamePattern.MatchString(param.Name):
			found = append(found, fmt.Errorf("param %q: name must be letters, digits or underscore — it is written {{%s}} in a brief", param.Name, param.Name))
			continue
		case declared[param.Name]:
			found = append(found, fmt.Errorf("param %s: declared twice — a hole has one description", param.Name))
			continue
		}
		if param.Required && param.Default != "" {
			found = append(found, fmt.Errorf("param %s: required but carries a default — a default is what makes a param optional", param.Name))
		}
		declared[param.Name] = true
		paramNames = append(paramNames, param.Name)
	}

	if len(w.Steps) == 0 {
		found = append(found, errors.New("workflow: no steps — a craft file with no steps has nothing to compile"))
	}
	if len(w.Steps) > MaxSteps {
		found = append(found, fmt.Errorf("workflow: %d steps is past the ceiling of %d — a shape this big is a plan, and plans belong to the planner", len(w.Steps), MaxSteps))
	}

	ids := map[string]bool{}
	var stepIDs []string
	for i, step := range w.Steps {
		switch {
		case step.ID == "":
			found = append(found, fmt.Errorf("step %d: id is empty — every step is named by the steps that need it", i+1))
			continue
		case !namePattern.MatchString(step.ID):
			found = append(found, fmt.Errorf("step %q: id must be lowercase letters, digits, dash or underscore", step.ID))
			continue
		case ids[step.ID]:
			found = append(found, fmt.Errorf("step %s: duplicate id — a step id is how the others name it, so it has to be unique", step.ID))
			continue
		}
		ids[step.ID] = true
		stepIDs = append(stepIDs, step.ID)
	}

	for _, step := range w.Steps {
		if step.ID == "" || !ids[step.ID] {
			continue
		}
		found = append(found, w.validateStep(step, ids, stepIDs, declared, paramNames)...)
	}
	if cycle := w.cycle(ids); cycle != "" {
		found = append(found, fmt.Errorf("workflow: dependency cycle %s — needs must arrange a shape that finishes", cycle))
	}
	return found
}

func (w *Workflow) validateStep(step Step, ids map[string]bool, stepIDs []string, params map[string]bool, paramNames []string) []error {
	var found []error

	switch {
	case step.Brief == "" && step.Verify == nil:
		found = append(found, fmt.Errorf("step %s: has neither a brief nor a verify — a step is either agentic work or a check", step.ID))
	case step.Brief != "" && step.Verify != nil:
		found = append(found, fmt.Errorf("step %s: has both a brief and a verify — a step is either agentic work or a check, never both", step.ID))
	}

	seen := map[string]bool{}
	for _, need := range step.Needs {
		switch {
		case need == step.ID:
			found = append(found, fmt.Errorf("step %s: needs itself", step.ID))
		case seen[need]:
			found = append(found, fmt.Errorf("step %s: needs %s twice", step.ID, need))
		case !ids[need]:
			found = append(found, fmt.Errorf("step %s: needs unknown step %s%s", step.ID, need, didYouMean(need, stepIDs)))
		}
		seen[need] = true
	}

	if step.ForEach != nil {
		if step.Verify != nil {
			found = append(found, fmt.Errorf("step %s: is both a for_each and a verify — a check runs once over everything it checks, not once per item", step.ID))
		}
		source := step.ForEach.Source
		switch {
		case source == "":
			found = append(found, fmt.Errorf("step %s: for_each.source is empty — the unroll reads its items from a step's result", step.ID))
		case source == step.ID:
			found = append(found, fmt.Errorf("step %s: for_each.source names itself", step.ID))
		case !ids[source]:
			found = append(found, fmt.Errorf("step %s: for_each.source names unknown step %s%s", step.ID, source, didYouMean(source, stepIDs)))
		case !seen[source]:
			found = append(found, fmt.Errorf("step %s: for_each.source names %s but the step does not need it — add %s to needs so the items exist before the unroll", step.ID, source, source))
		}
		if step.ForEach.Fan < 0 {
			found = append(found, fmt.Errorf("step %s: for_each.fan is negative — leave it out for the default of %d", step.ID, DefaultFanCap))
		}
	}

	if step.Verify != nil {
		found = append(found, scriptLaw("step "+step.ID+": verify.script", step.Verify.Script, VerifierDir)...)
		if until := step.Verify.UntilPass; until != nil {
			ancestors := w.ancestors(step.ID)
			revised := map[string]bool{}
			for _, target := range until.Revise {
				switch {
				case target == step.ID:
					found = append(found, fmt.Errorf("step %s: until_pass.revise names the check itself — a round re-runs the work, not the checking", step.ID))
				case revised[target]:
					found = append(found, fmt.Errorf("step %s: until_pass.revise names %s twice", step.ID, target))
				case !ids[target]:
					found = append(found, fmt.Errorf("step %s: until_pass.revise names unknown step %s%s", step.ID, target, didYouMean(target, stepIDs)))
				case !ancestors[target]:
					found = append(found, fmt.Errorf("step %s: until_pass.revise names %s, which the check does not depend on — a round re-runs work the check actually read", step.ID, target))
				}
				revised[target] = true
			}
			if until.MaxRounds < 0 {
				found = append(found, fmt.Errorf("step %s: until_pass.max_rounds is negative — leave it out for the default of %d", step.ID, DefaultMaxRounds))
			}
		}
	}

	if step.Skill != "" && !plainName(step.Skill) {
		found = append(found, fmt.Errorf("step %s: skill %q must be a plain executable name from %s/, not a path", step.ID, step.Skill, SkillDir))
	}

	for _, reference := range paramPattern.FindAllStringSubmatch(step.Brief, -1) {
		if !params[reference[1]] {
			found = append(found, fmt.Errorf("step %s: brief references {{%s}}, which no param declares%s", step.ID, reference[1], didYouMean(reference[1], paramNames)))
		}
	}
	return found
}

// scriptLaw is the path law for anything a workflow can cause to be executed:
// repo-relative, under the one directory that holds it, and no traversal. The
// check is on the written path rather than on a resolved one so a file is
// refused before anything opens it.
func scriptLaw(where, script, dir string) []error {
	switch {
	case script == "":
		return []error{fmt.Errorf("%s is empty — a check needs an executable to run", where)}
	case strings.HasPrefix(script, "/") || strings.HasPrefix(script, "~"):
		return []error{fmt.Errorf("%s %q must be repo-relative, not absolute", where, script)}
	case strings.Contains(script, `\`):
		return []error{fmt.Errorf("%s %q must use forward slashes", where, script)}
	}
	parts := strings.Split(script, "/")
	for _, part := range parts {
		if part == ".." {
			return []error{fmt.Errorf("%s %q must not climb out of %s/ with ..", where, script, dir)}
		}
		if part == "" || part == "." {
			return []error{fmt.Errorf("%s %q is not a clean path", where, script)}
		}
	}
	if len(parts) < 2 || parts[0] != dir {
		return []error{fmt.Errorf("%s %q must live under %s/", where, script, dir)}
	}
	return nil
}

func plainName(name string) bool {
	return !strings.ContainsAny(name, `/\`) && name != "." && name != ".."
}

// ancestors is every step reachable upstream through needs. The walk carries
// its own seen set because a cyclic file still has to produce readable errors
// rather than a stack overflow.
func (w *Workflow) ancestors(id string) map[string]bool {
	needs := map[string][]string{}
	for _, step := range w.Steps {
		needs[step.ID] = step.Needs
	}
	found := map[string]bool{}
	var walk func(string)
	walk = func(at string) {
		for _, need := range needs[at] {
			if found[need] {
				continue
			}
			found[need] = true
			walk(need)
		}
	}
	walk(id)
	return found
}

// cycle returns the first dependency cycle as a readable path, or empty. Steps
// are walked in file order so the same file always names the same cycle.
func (w *Workflow) cycle(ids map[string]bool) string {
	needs := map[string][]string{}
	for _, step := range w.Steps {
		needs[step.ID] = step.Needs
	}
	const (
		open = 1
		shut = 2
	)
	state := map[string]int{}
	var path []string
	var walk func(string) string
	walk = func(at string) string {
		state[at] = open
		path = append(path, at)
		for _, need := range needs[at] {
			if !ids[need] || need == at {
				continue
			}
			switch state[need] {
			case open:
				from := 0
				for i, seen := range path {
					if seen == need {
						from = i
						break
					}
				}
				return strings.Join(append(append([]string{}, path[from:]...), need), " → ")
			case 0:
				if found := walk(need); found != "" {
					return found
				}
			}
		}
		path = path[:len(path)-1]
		state[at] = shut
		return ""
	}
	for _, step := range w.Steps {
		if state[step.ID] == 0 && ids[step.ID] {
			path = path[:0]
			if found := walk(step.ID); found != "" {
				return found
			}
		}
	}
	return ""
}

// Clamp applies the package ceilings and the defaults for everything the file
// left at zero, so a compiled run can trust every number it reads without
// re-deriving the rails. It is idempotent: clamping a clamped workflow is a
// no-op, which is what lets Save clamp whatever it is handed.
func (w *Workflow) Clamp() {
	if w.Limits.CostUSD <= 0 {
		w.Limits.CostUSD = DefaultRunBudgetUSD
	}
	if w.Limits.CostUSD > MaxRunBudgetUSD {
		w.Limits.CostUSD = MaxRunBudgetUSD
	}
	if w.Limits.WallClock <= 0 {
		w.Limits.WallClock = DefaultWallClock
	}
	if w.Limits.WallClock > MaxWallClock {
		w.Limits.WallClock = MaxWallClock
	}
	for i := range w.Steps {
		step := &w.Steps[i]
		if step.ForEach != nil {
			if step.ForEach.Fan <= 0 {
				step.ForEach.Fan = DefaultFanCap
			}
			if step.ForEach.Fan > MaxFanCap {
				step.ForEach.Fan = MaxFanCap
			}
		}
		if step.Verify == nil || step.Verify.UntilPass == nil {
			continue
		}
		until := step.Verify.UntilPass
		if until.MaxRounds <= 0 {
			until.MaxRounds = DefaultMaxRounds
		}
		if until.MaxRounds > MaxRounds {
			until.MaxRounds = MaxRounds
		}
		// types.go declares an empty Revise to mean the check's direct needs.
		// Writing it out here is what keeps that default in one place instead
		// of in every reader of a compiled workflow.
		if len(until.Revise) == 0 {
			until.Revise = append([]string(nil), step.Needs...)
		}
	}
}

// Fill resolves the invocation's params against the declarations: a given
// value wins, a default fills the silence, and everything still missing is
// reported in ONE error. A model that has to discover its missing arguments
// one run at a time will guess the rest, so the error names them all and, when
// a near-miss was passed instead, says which key it should have been.
func (w *Workflow) Fill(params map[string]string) (map[string]string, error) {
	values := make(map[string]string, len(w.Params))
	var missing []string
	declared := map[string]bool{}
	for _, param := range w.Params {
		declared[param.Name] = true
	}
	var extra []string
	for key := range params {
		if !declared[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)

	for _, param := range w.Params {
		value := strings.TrimSpace(params[param.Name])
		if value == "" {
			value = param.Default
		}
		if value == "" && param.Required {
			// A near-miss key is the likeliest cause, and naming it turns a
			// second failed run into a one-character fix.
			if near := nearest(param.Name, extra); near != "" {
				missing = append(missing, fmt.Sprintf("%s (you passed %s)", param.Name, near))
			} else {
				missing = append(missing, param.Name)
			}
			continue
		}
		values[param.Name] = value
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("craft %s: missing required param%s: %s",
			w.Name, plural(len(missing)), strings.Join(missing, ", "))
	}
	return values, nil
}

// Substitute fills {{name}} holes in a brief. An unresolved reference is left
// standing rather than blanked: Validate already refuses a brief that names an
// undeclared param, so anything still in braces at runtime is a bug worth
// seeing in the transcript.
func Substitute(text string, values map[string]string) string {
	return paramPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := paramPattern.FindStringSubmatch(match)[1]
		if value, ok := values[name]; ok {
			return value
		}
		return match
	})
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// didYouMean is the repair hint. It returns the empty string rather than a
// guess when nothing is close, because an unhelpful suggestion sends a model
// off to edit a step that was never wrong.
func didYouMean(word string, candidates []string) string {
	best := nearest(word, candidates)
	if best == "" {
		return ""
	}
	return " — did you mean " + best + "?"
}

func nearest(word string, candidates []string) string {
	best, distance := "", SuggestDistance+1
	for _, candidate := range candidates {
		if candidate == word {
			continue
		}
		if d := editDistance(word, candidate); d < distance {
			best, distance = candidate, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, min(current[j-1]+1, previous[j-1]+cost))
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// The decoder's own vocabulary, translated. yaml.v3 reports an unknown field
// against a Go type name, which means nothing to the writer of the file; these
// tables turn "field breif not found in type craft.fileStep" into a sentence
// about a step and a suggestion about what was meant.
var fileNouns = map[string]struct {
	noun   string
	fields []string
}{
	"craft.fileWorkflow":  {"workflow", []string{"name", "description", "params", "steps", "limits"}},
	"craft.fileParam":     {"param", []string{"name", "description", "default", "required"}},
	"craft.fileStep":      {"step", []string{"id", "brief", "needs", "model", "skill", "for_each", "verify"}},
	"craft.fileForEach":   {"for_each", []string{"source", "fan"}},
	"craft.fileVerify":    {"verify", []string{"script", "until_pass"}},
	"craft.fileUntilPass": {"until_pass", []string{"revise", "max_rounds"}},
	"craft.fileLimits":    {"limits", []string{"cost_usd", "minutes"}},
}

// shapeNouns names the Go types the decoder complains about in the words the
// file is written in. A writer told "into []craft.fileStep" learns nothing;
// told "a list of steps" they can see their own mistake.
var shapeNouns = map[string]string{
	"craft.fileWorkflow":  "a workflow",
	"[]craft.fileParam":   "a list of params",
	"craft.fileParam":     "a param",
	"[]craft.fileStep":    "a list of steps",
	"craft.fileStep":      "a step",
	"craft.fileForEach":   "a for_each block",
	"craft.fileVerify":    "a verify block",
	"craft.fileUntilPass": "an until_pass block",
	"craft.fileLimits":    "a limits block",
	"[]string":            "a list of names",
	"string":              "text",
	"int":                 "a whole number",
	"float64":             "a number",
	"bool":                "true or false",
}

var yamlKinds = map[string]string{
	"str": "text", "seq": "a list", "map": "a block",
	"int": "a whole number", "float": "a number",
	"bool": "true or false", "null": "nothing",
}

var unknownFieldPattern = regexp.MustCompile(`field ([A-Za-z0-9_]+) not found in type ([A-Za-z0-9_.]+)`)

var badShapePattern = regexp.MustCompile("cannot unmarshal !!([a-z]+)(?: `([^`]*)`)? into ([A-Za-z0-9_.\\[\\]]+)")

func readable(err error) error {
	var typeError *yaml.TypeError
	if !errors.As(err, &typeError) {
		return fmt.Errorf("craft: %s", strings.TrimSpace(err.Error()))
	}
	lines := make([]string, 0, len(typeError.Errors))
	for _, line := range typeError.Errors {
		lines = append(lines, rewriteYAMLError(line))
	}
	sort.Strings(lines)
	return fmt.Errorf("craft: %s", strings.Join(lines, "; "))
}

func rewriteYAMLError(line string) string {
	if match := unknownFieldPattern.FindStringSubmatch(line); match != nil {
		if known, ok := fileNouns[match[2]]; ok {
			return strings.TrimSpace(where(line, "field ") + " unknown " + known.noun + " field " + match[1] + didYouMean(match[1], known.fields))
		}
	}
	if match := badShapePattern.FindStringSubmatch(line); match != nil {
		if wanted, ok := shapeNouns[match[3]]; ok {
			found := yamlKinds[match[1]]
			if found == "" {
				found = match[1]
			}
			if match[2] != "" {
				found += " (" + match[2] + ")"
			}
			return strings.TrimSpace(where(line, "cannot unmarshal") + " expected " + wanted + " here, found " + found)
		}
	}
	return strings.TrimSpace(line)
}

// where keeps whatever the decoder said before its own vocabulary started —
// in practice the line number, which is the part worth keeping.
func where(line, marker string) string {
	if at := strings.Index(line, marker); at > 0 {
		return strings.TrimSpace(line[:at])
	}
	return ""
}
