package pipeline

import (
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"gopkg.in/yaml.v3"
)

// Parse reads a repository's pipeline file. A syntax error is the error, and
// the file answered is [Defaults]: a file that cannot be read does not take
// the floor down with it. Everything else loads, and each key that does not
// is a [Problem].
func Parse(text string) (File, error) {
	f, err := parse(text, true)
	if err != nil {
		d := Defaults()
		d.Text = text
		d.Problems = []Problem{{Where: RepoFile, Why: err.Error()}}
		return d, err
	}
	f.Text = text
	return f, nil
}

// ParsePersonal reads ~/.codeaf/factory.yaml. Omitted keys are no opinion,
// not the defaults, so [Merge] can tell a tighter cap from a file that never
// mentioned one.
func ParsePersonal(text string) (File, error) {
	f, err := parse(text, false)
	if err != nil {
		return f, err
	}
	f.Text = text
	return f, nil
}

func parse(text string, defaults bool) (File, error) {
	root, err := decodeRoot(text)
	if err != nil {
		return File{}, err
	}
	d := readDoc(root)
	return d.file(defaults), nil
}

func decodeRoot(text string) (*yaml.Node, error) {
	text = strings.TrimPrefix(text, "\uFEFF")
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	dec := yaml.NewDecoder(strings.NewReader(text))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("factory.yaml is one document")
		}
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil, nil
		}
		doc = *doc.Content[0]
	}
	if doc.Tag == "!!null" || doc.Kind == 0 {
		return nil, nil
	}
	if doc.Kind != yaml.MappingNode {
		return nil, errors.New("factory.yaml is a map of start, size, pipeline, steps, fixed, rules and merge")
	}
	return &doc, nil
}

type draft struct {
	defaultBudget float64
	problems      []Problem
	set           presence

	labels   []string
	maxSize  string
	maxCost  float64
	never    []string
	askFirst []string
	prLines  [2]int
	issue    string
	pipeline map[factory.Kind][]string
	steps    map[string]Step
	fixed    []string
	rules    []string
	adapt    factory.AdaptMode
	merge    factory.MergePolicy
}

func readDoc(root *yaml.Node) draft {
	var d draft
	d.pipeline = map[factory.Kind][]string{}
	d.steps = map[string]Step{}
	d.set.pipeline = map[factory.Kind]bool{}
	if root == nil {
		return d
	}
	pairs, probs := mapping(root)
	d.problems = append(d.problems, probs...)
	for _, p := range pairs {
		switch p.name {
		case "default_budget":
			v, err := floatOf(p.val)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
				d.problems = append(d.problems, Problem{Line: p.line, Where: p.name, Why: "budget must be a positive number of dollars"})
			} else {
				d.defaultBudget = v
				d.set.defaultBudget = true
			}
		case "start":
			d.readStart(p.val)
		case "size":
			d.readSize(p.val)
		case "pipeline":
			d.readPipeline(p.val)
		case "steps":
			d.readSteps(p.val)
		case "fixed":
			names, ps, ok := nameList(p.val)
			d.problems = append(d.problems, tag("fixed", ps)...)
			if ok {
				d.fixed = names
				d.set.fixed = true
			}
		case "merge":
			d.readMerge(p.val)
		case "adapt":
			s, ok := scalar(p.val)
			word, good := stepWord(s, factory.AdaptWords)
			if !ok || !good {
				d.problems = append(d.problems, Problem{Line: p.line, Where: "adapt", Why: "adapt is adapt, ask or fixed"})
				continue
			}
			d.adapt = factory.AdaptMode(word)
			d.set.adapt = true
		case "rules":
			rules, ps, ok := stringList(p.val)
			d.problems = append(d.problems, tag("rules", ps)...)
			if ok {
				d.rules = dedupeFold(rules)
				d.set.rules = true
			}
		default:
			d.problems = append(d.problems, Problem{Line: p.line, Where: p.name, Why: "not a key factory.yaml reads"})
		}
	}
	return d
}

func (d *draft) readStart(n *yaml.Node) {
	pairs, probs := mapping(n)
	d.problems = append(d.problems, tag("start", probs)...)
	if n.Kind != yaml.MappingNode {
		return
	}
	for _, p := range pairs {
		switch p.name {
		case "labels":
			names, ps, ok := stringList(p.val)
			d.problems = append(d.problems, tag("start.labels", ps)...)
			if ok {
				d.labels = dedupeFold(names)
				d.set.labels = true
			}
		case "max_size":
			s, ok := scalar(p.val)
			size, good := normSize(s)
			if !ok || !good {
				d.problems = append(d.problems, Problem{Line: p.line, Where: "start.max_size", Why: "size is S, M or L"})
				continue
			}
			d.maxSize = size
			d.set.maxSize = true
		case "max_cost":
			v, err := floatOf(p.val)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				d.problems = append(d.problems, Problem{Line: p.line, Where: "start.max_cost", Why: "cost is a number of dollars, zero or more"})
				continue
			}
			d.maxCost = v
			d.set.maxCost = true
		case "never":
			globs, ps, ok := stringList(p.val)
			d.problems = append(d.problems, tag("start.never", ps)...)
			if ok {
				d.never = dedupe(globs)
				d.set.never = true
			}
		case "ask_first":
			lines, ps, ok := stringList(p.val)
			d.problems = append(d.problems, tag("start.ask_first", ps)...)
			if ok {
				d.askFirst = dedupeFold(lines)
				d.set.askFirst = true
			}
		default:
			d.problems = append(d.problems, Problem{Line: p.line, Where: "start." + p.name, Why: "not a start key"})
		}
	}
}

// readMerge reads `merge: {auto, method}`. auto is on or off (true and false
// read the same); method is merge, squash or rebase, and omitted lets the
// forge's repository settings choose.
func (d *draft) readMerge(n *yaml.Node) {
	pairs, probs := mapping(n)
	d.problems = append(d.problems, tag("merge", probs)...)
	if n.Kind != yaml.MappingNode {
		return
	}
	for _, p := range pairs {
		switch p.name {
		case "auto":
			s, ok := scalar(p.val)
			on, good := onOff(s)
			if !ok || !good {
				d.problems = append(d.problems, Problem{Line: p.line, Where: "merge.auto", Why: "auto-merge is on or off"})
				continue
			}
			d.merge.Auto = on
			d.set.mergeAuto = true
		case "method":
			s, ok := scalar(p.val)
			method, good := stepWord(s, factory.MergeMethods)
			if !ok || !good {
				d.problems = append(d.problems, Problem{Line: p.line, Where: "merge.method", Why: "method is merge, squash or rebase"})
				continue
			}
			d.merge.Method = method
			d.set.mergeMethod = true
		default:
			d.problems = append(d.problems, Problem{Line: p.line, Where: "merge." + p.name, Why: "not a merge key"})
		}
	}
}

// onOff reads a yes-or-no word. YAML 1.1 spells on and off as booleans; the
// decoder keeps them as the words, so both spellings arrive here as text.
func onOff(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "on", "true", "yes":
		return true, true
	case "off", "false", "no":
		return false, true
	}
	return false, false
}

func (d *draft) readSize(n *yaml.Node) {
	pairs, probs := mapping(n)
	d.problems = append(d.problems, tag("size", probs)...)
	if n.Kind != yaml.MappingNode {
		return
	}
	for _, p := range pairs {
		switch p.name {
		case "pr_lines":
			bounds, ps, ok := lineBounds(p.val)
			d.problems = append(d.problems, tag("size.pr_lines", ps)...)
			if ok {
				d.prLines = bounds
				d.set.prLines = true
			}
		case "issue":
			s, ok := scalar(p.val)
			mode, good := normIssue(s)
			if !ok || !good {
				d.problems = append(d.problems, Problem{Line: p.line, Where: "size.issue", Why: "issue size is model or label"})
				continue
			}
			d.issue = mode
			d.set.issue = true
		default:
			d.problems = append(d.problems, Problem{Line: p.line, Where: "size." + p.name, Why: "not a size key"})
		}
	}
}

func (d *draft) readPipeline(n *yaml.Node) {
	pairs, probs := mapping(n)
	d.problems = append(d.problems, tag("pipeline", probs)...)
	if n.Kind != yaml.MappingNode {
		return
	}
	for _, p := range pairs {
		kind := factory.Kind(stepToken(p.name))
		if !validStepName(string(kind)) {
			d.problems = append(d.problems, Problem{Line: p.line, Where: "pipeline." + p.name, Why: "not a kind"})
			continue
		}
		names, ps, ok := nameList(p.val)
		d.problems = append(d.problems, tag("pipeline."+string(kind), ps)...)
		if !ok {
			continue
		}
		d.pipeline[kind] = names
		d.set.pipeline[kind] = true
	}
}

func (d *draft) readSteps(n *yaml.Node) {
	pairs, probs := mapping(n)
	d.problems = append(d.problems, tag("steps", probs)...)
	if n.Kind != yaml.MappingNode {
		return
	}
	for _, p := range pairs {
		name := stepToken(p.name)
		if !validStepName(name) {
			d.problems = append(d.problems, Problem{Line: p.line, Where: "steps." + p.name, Why: "a step name is lowercase letters, digits and hyphens, 2 to 32"})
			continue
		}
		if _, ok := d.steps[name]; ok {
			d.problems = append(d.problems, Problem{Line: p.line, Where: "steps." + name, Why: "named twice; the first is kept"})
			continue
		}
		step, ps := readStep(p.val)
		d.problems = append(d.problems, tag("steps."+name, ps)...)
		if step.broken {
			continue
		}
		d.steps[name] = step.step
	}
}

type stepRead struct {
	step   Step
	broken bool
}

func readStep(n *yaml.Node) (stepRead, []Problem) {
	if n.Kind == yaml.ScalarNode && n.Tag != "!!null" {
		prompt := strings.TrimSpace(n.Value)
		return stepRead{step: Step{Prompt: prompt, wrotePrompt: true}}, nil
	}
	if n.Tag == "!!null" || n.Kind == 0 {
		return stepRead{step: Step{wrotePrompt: true}}, nil
	}
	pairs, probs := mapping(n)
	if n.Kind != yaml.MappingNode {
		return stepRead{broken: true}, probs
	}
	var step Step
	badWhen := 0
	for _, p := range pairs {
		switch p.name {
		case "prompt":
			s, ok := scalar(p.val)
			if !ok {
				probs = append(probs, Problem{Line: p.line, Where: "prompt", Why: "a prompt is a sentence"})
				continue
			}
			step.Prompt = strings.TrimSpace(s)
			step.wrotePrompt = true
		case "until":
			s, ok := scalar(p.val)
			if !ok {
				probs = append(probs, Problem{Line: p.line, Where: "until", Why: "until is a sentence"})
				continue
			}
			step.Until = strings.TrimSpace(s)
			step.wroteUntil = true
		case "max":
			m, err := intOf(p.val)
			if err != nil || m < 1 {
				probs = append(probs, Problem{Line: p.line, Where: "max", Why: "max is a number of rounds, 1 or more"})
				continue
			}
			step.Max = m
			step.wroteMax = true
		case "kind":
			// AN UNREADABLE KIND IS NEVER A CHAT: a misspelt gate would let a
			// model run where a person was meant to sit. The step holds.
			s, ok := scalar(p.val)
			k, good := kindWord(s)
			if !ok || !good {
				why := "kind is chat, check, gate or post; until it is one, the run holds at this step for a person"
				probs = append(probs, Problem{Line: p.line, Where: p.name, Why: why})
				step.hold = why
				continue
			}
			step.Kind = k
		case "when":
			n := len(probs)
			step.When = enumWord(p, factory.WhenWords, "when is one of "+strings.Join(factory.WhenWords, ", "), &probs)
			if len(probs) > n {
				badWhen = p.line
			}
		case "effort":
			step.Effort = enumWord(p, factory.EffortWords, "effort is cheap or strong; leave it out for the default", &probs)
		case "fanout":
			step.Fanout = enumWord(p, StepFanouts, "fanout is one, per-file, per-finding or per-claim", &probs)
		default:
			probs = append(probs, Problem{Line: p.line, Where: p.name, Why: "a step is a prompt, a kind, when, effort, fanout, an until and a max"})
		}
	}
	// An unreadable condition costs a chat or a check money when it falls back
	// to always. A post acts on the forge, so it holds rather than act on
	// every item its author meant to spare.
	if badWhen > 0 && step.Kind == string(factory.StagePost) && step.hold == "" {
		why := "a post with a condition the runner cannot read holds here for a person"
		probs = append(probs, Problem{Line: badWhen, Where: "when", Why: why})
		step.hold = why
	}
	return stepRead{step: step}, probs
}

// enumWord reads one runtime word. A word outside the vocabulary is a problem
// and reads as the default, so a typo never becomes a stage setting.
func enumWord(p pair, words []string, why string, probs *[]Problem) string {
	if s, ok := scalar(p.val); ok {
		if w, good := stepWord(s, words); good {
			return w
		}
	}
	*probs = append(*probs, Problem{Line: p.line, Where: p.name, Why: why})
	return ""
}

func (d draft) file(defaults bool) File {
	var f File
	if defaults {
		f = Defaults()
	} else {
		f.Start.Labels = []string{}
		f.Start.Never = []string{}
		f.Start.AskFirst = []string{}
		f.Pipeline = map[factory.Kind][]string{}
		f.Steps = map[string]Step{}
		f.Fixed = []string{}
		f.Rules = []string{}
	}
	f.Problems = append([]Problem{}, d.problems...)
	f.set = d.set
	if d.set.defaultBudget {
		f.DefaultBudget = d.defaultBudget
	}
	if d.set.labels {
		f.Start.Labels = d.labels
	}
	if d.set.maxSize {
		f.Start.MaxSize = d.maxSize
	}
	if d.set.maxCost {
		f.Start.MaxCost = d.maxCost
	}
	if d.set.never {
		f.Start.Never = d.never
	}
	if d.set.askFirst {
		f.Start.AskFirst = d.askFirst
	}
	if d.set.prLines {
		f.Size.PRLines = d.prLines
	}
	if d.set.issue {
		f.Size.Issue = d.issue
	}
	if len(d.set.pipeline) > 0 {
		if f.Pipeline == nil {
			f.Pipeline = map[factory.Kind][]string{}
		}
		for kind, names := range d.pipeline {
			f.Pipeline[kind] = append([]string(nil), names...)
		}
	}
	for name, step := range d.steps {
		if defaults {
			base := f.Steps[name]
			if step.wrotePrompt {
				base.Prompt = step.Prompt
				base.wrotePrompt = true
			}
			if step.wroteUntil {
				base.Until = step.Until
				base.wroteUntil = true
			}
			if step.wroteMax {
				base.Max = step.Max
				base.wroteMax = true
			}
			base.Kind, base.When, base.Effort, base.Fanout, base.hold = step.Kind, step.When, step.Effort, step.Fanout, step.hold
			if base.Kind != "" {
				// A shipped prompt and stop sentence are words for a model. A
				// check would run the prompt in a shell and a post would read it
				// as a verb, so a step of another kind keeps only its own words.
				if !step.wrotePrompt {
					base.Prompt = ""
				}
				if !step.wroteUntil {
					base.Until = ""
				}
				f.Steps[name] = base
				continue
			}
			if step.wrotePrompt && strings.TrimSpace(step.Prompt) == "" && !factory.ApproveWord(name) {
				f.Problems = append(f.Problems, Problem{Where: "steps." + name, Why: "a step needs a prompt"})
				if b, ok := builtinTable[name]; ok {
					base.Prompt = b.Prompt
					base.wrotePrompt = false
				}
			}
			f.Steps[name] = base
			continue
		}
		f.Steps[name] = step
	}
	if d.set.fixed {
		f.Fixed = append([]string(nil), d.fixed...)
	}
	if d.set.rules {
		f.Rules = append([]string(nil), d.rules...)
	}
	if d.set.adapt {
		f.Adapt = d.adapt
	}
	if d.set.mergeAuto {
		f.Merge.Auto = d.merge.Auto
	}
	if d.set.mergeMethod {
		f.Merge.Method = d.merge.Method
	}
	if defaults {
		f.resolve()
	}
	f.clean(defaults)
	return f
}

// resolve fills a built-in prompt and until wherever the file left them out.
// AN EMPTY PROMPT DOES NOT DELETE A BUILT-IN: the shipped sentence stays, and
// the empty write is a problem from the reader.
func (f *File) resolve() {
	if f.Steps == nil {
		f.Steps = map[string]Step{}
	}
	for name, builtin := range builtinTable {
		got, ok := f.Steps[name]
		if !ok {
			f.Steps[name] = builtin
			continue
		}
		if got.Kind != "" {
			continue // a shipped prompt is a model's; see draft.file
		}
		if strings.TrimSpace(got.Prompt) == "" {
			got.Prompt = builtin.Prompt
		}
		if strings.TrimSpace(got.Until) == "" && !got.wroteUntil {
			got.Until = builtin.Until
		}
		f.Steps[name] = got
	}
}

func (f *File) clean(requireStep bool) {
	for name, step := range f.Steps {
		if factory.ApproveWord(name) && step.wrotePrompt && strings.TrimSpace(step.Prompt) != "" {
			f.Problems = append(f.Problems, Problem{Where: "steps." + name, Why: "approve is a person and has no prompt"})
			step.Prompt = ""
			step.wrotePrompt = false
			f.Steps[name] = step
		}
		// A person step is only ever a gate: it is not written to the file, so a
		// runtime word kept on it would be dropped without a word.
		if factory.ApproveWord(name) {
			step := f.Steps[name]
			if step.When != "" || step.Effort != "" || step.Fanout != "" || (step.Kind != "" && step.Kind != string(factory.StageGate)) {
				f.Problems = append(f.Problems, Problem{Where: "steps." + name, Why: "approve is a person; it has no kind, condition, effort or fanout"})
				step.Kind, step.When, step.Effort, step.Fanout = "", "", "", ""
				f.Steps[name] = step
			}
			continue
		}
		// A hold found where the step was read is named there already. A
		// personal step (requireStep false) only tightens the repository's, so
		// a prompt it leaves out is no opinion, not a missing command.
		if step.hold == "" && requireStep {
			if why := step.holdWhy(); why != "" {
				f.Problems = append(f.Problems, Problem{Where: "steps." + name, Why: why})
			}
		}
		if _, free := mechanicalUntil(step.Until); free && step.Kind == string(factory.StageGate) {
			f.Problems = append(f.Problems, Problem{Where: "steps." + name + ".until", Why: "a gate is a person and stops when they answer; until is not read"})
		}
	}
	for kind, names := range f.Pipeline {
		f.Pipeline[kind] = f.cleanNames(kind, names, requireStep)
	}
	if f.set.fixed {
		var fixed []string
		seen := map[string]bool{}
		for _, name := range f.Fixed {
			if seen[name] {
				continue
			}
			if !validStepName(name) {
				f.Problems = append(f.Problems, Problem{Where: "fixed", Why: name + " is not a step name"})
				continue
			}
			seen[name] = true
			fixed = append(fixed, name)
		}
		if fixed == nil {
			fixed = []string{}
		}
		f.Fixed = fixed
	}
}

func (f *File) cleanNames(kind factory.Kind, names []string, requireStep bool) []string {
	var out []string
	seen := map[string]bool{}
	where := "pipeline." + string(kind)
	for _, name := range names {
		if seen[name] {
			f.Problems = append(f.Problems, Problem{Where: where, Why: name + " is named twice"})
			continue
		}
		if !validStepName(name) {
			f.Problems = append(f.Problems, Problem{Where: where, Why: name + " is not a step name"})
			continue
		}
		if requireStep && !factory.ApproveWord(name) {
			step, ok := f.Steps[name]
			// A person needs no prompt, and a step that holds must stay where it
			// is: dropping either would let the steps after it run unwatched.
			if !ok || strings.TrimSpace(step.Prompt) == "" && !step.isGate() {
				f.Problems = append(f.Problems, Problem{Where: where, Why: name + " is not a step; write it under steps"})
				continue
			}
			if _, err := factory.StageWord(strings.ReplaceAll(name, "-", "")); err != nil && step.isGate() {
				f.Problems = append(f.Problems, Problem{Where: where, Why: name + " is not a name the runner can run; it holds there as an approve step"})
			}
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) > factory.StageMost {
		f.Problems = append(f.Problems, Problem{Where: where, Why: factory.ErrNineStages.Error()})
		out = out[:factory.StageMost]
	}
	if out == nil {
		out = []string{}
	}
	return out
}

type pair struct {
	name string
	line int
	val  *yaml.Node
}

func mapping(n *yaml.Node) ([]pair, []Problem) {
	if n == nil || n.Tag == "!!null" {
		return nil, nil
	}
	if n.Kind != yaml.MappingNode {
		return nil, []Problem{{Line: n.Line, Why: "expected a map"}}
	}
	var out []pair
	seen := map[string]int{}
	var probs []Problem
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, val := n.Content[i], n.Content[i+1]
		if key.Kind != yaml.ScalarNode {
			probs = append(probs, Problem{Line: key.Line, Why: "a key is a word"})
			continue
		}
		name := strings.TrimSpace(key.Value)
		if prev, ok := seen[name]; ok {
			probs = append(probs, Problem{Line: key.Line, Where: name, Why: "named twice; line " + strconv.Itoa(prev) + " is kept"})
			continue
		}
		seen[name] = key.Line
		if val.Kind == yaml.AliasNode {
			probs = append(probs, Problem{Line: val.Line, Where: name, Why: "an alias is not read"})
			continue
		}
		out = append(out, pair{name: name, line: key.Line, val: val})
	}
	return out, probs
}

func stringList(n *yaml.Node) ([]string, []Problem, bool) {
	if n == nil || n.Tag == "!!null" {
		return []string{}, nil, true
	}
	if n.Kind != yaml.SequenceNode {
		return nil, []Problem{{Line: n.Line, Why: "expected a list"}}, false
	}
	var out []string
	var probs []Problem
	for _, c := range n.Content {
		if c.Kind != yaml.ScalarNode || c.Tag == "!!null" {
			probs = append(probs, Problem{Line: c.Line, Why: "expected a sentence"})
			continue
		}
		s := strings.TrimSpace(c.Value)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	if out == nil {
		out = []string{}
	}
	return out, probs, true
}

func nameList(n *yaml.Node) ([]string, []Problem, bool) {
	names, probs, ok := stringList(n)
	if !ok {
		return nil, probs, false
	}
	for i, name := range names {
		names[i] = stepToken(name)
	}
	return names, probs, true
}

func lineBounds(n *yaml.Node) ([2]int, []Problem, bool) {
	why := Problem{Line: n.Line, Why: "pr_lines is two numbers, the S bound then the M bound"}
	if n == nil || n.Kind != yaml.SequenceNode || len(n.Content) != 2 {
		return [2]int{}, []Problem{why}, false
	}
	lo, errLo := intOf(n.Content[0])
	hi, errHi := intOf(n.Content[1])
	if errLo != nil || errHi != nil || lo < 0 || hi < 0 || lo > hi {
		return [2]int{}, []Problem{why}, false
	}
	return [2]int{lo, hi}, nil, true
}

func scalar(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return "", false
	}
	return n.Value, true
}

func intOf(n *yaml.Node) (int, error) {
	v, err := floatOf(n)
	if err != nil || v != math.Trunc(v) || v < 0 || v > 1e9 {
		return 0, errors.New("not a whole number")
	}
	return int(v), nil
}

func floatOf(n *yaml.Node) (float64, error) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return 0, errors.New("not a number")
	}
	switch n.Tag {
	case "!!int", "!!float", "!!str":
		return strconv.ParseFloat(strings.TrimSpace(n.Value), 64)
	default:
		return 0, errors.New("not a number")
	}
}

func tag(where string, ps []Problem) []Problem {
	if ps == nil {
		return nil
	}
	out := make([]Problem, len(ps))
	for i, p := range ps {
		if p.Where == "" {
			p.Where = where
		} else if !strings.Contains(p.Where, ".") && where != "" {
			p.Where = where + "." + p.Where
		}
		out[i] = p
	}
	return out
}

func normSize(s string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "XS", "S", "M", "L", "XL":
		return strings.ToUpper(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

func normIssue(s string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case DefaultIssueMode, IssueModeLabel:
		return strings.ToLower(strings.TrimSpace(s)), true
	default:
		return "", false
	}
}

func stepToken(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func validStepName(name string) bool {
	if len(name) < 2 || len(name) > 32 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-' && i > 0 && i < len(name)-1:
		default:
			return false
		}
	}
	return true
}

func dedupe(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func dedupeFold(in []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range in {
		key := strings.ToLower(strings.TrimSpace(s))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, strings.TrimSpace(s))
	}
	if out == nil {
		return []string{}
	}
	return out
}
