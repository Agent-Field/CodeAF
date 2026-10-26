// Package pipeline reads a repository's .codeaf/factory.yaml: the steps a job
// runs, the sentences it obeys, and what may start on its own.
//
// A REPO WITH NO FILE GETS THE DEFAULTS AND WORKS. Every key is optional. The
// same keys in a personal file may only be stricter. The markdown recipe
// (.codeaf/factory.md) is read as this file when the yaml is not there yet.
//
// The shape is docs/design/factory/FACTORY-YAML.md. A step is a prompt. approve
// is a person and has no prompt. A step may also carry the four runtime words a
// stage already has: kind, when, effort and fanout, each from the runner's own
// vocabulary ([StepKinds], [factory.WhenWords], [factory.EffortWords],
// [StepFanouts]). An omitted word is the default, never a refusal.
//
// A step whose kind is gate is a person under whatever name it carries, the
// same as approve: [factory.IsApprove] reads the kind, not the name. A check
// runs its prompt as a shell command and a post reads its prompt as a verb, so
// neither ever borrows a shipped prompt or a shipped stop sentence, and a
// sentence the runner cannot check is never added to either.
//
// A STEP THAT CANNOT RUN AS WRITTEN HOLDS FOR A PERSON. An unreadable kind, a
// check or post with no prompt of its own or with a stop sentence only a model
// could read, two markdown sections that disagree about one step's kind or
// condition: each is a [Problem] and the step becomes a gate in [File.Recipe],
// never a chat and never a dropped stage. Such a file is never written either:
// [Encode] refuses it, because the word it could not read has nothing to be
// written as but the default.
package pipeline

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

const (
	// RepoFile is the pipeline file inside a repository, slash-separated.
	RepoFile = factory.PipelineFile

	// SourceDefaults is a repository with no pipeline file.
	SourceDefaults = "defaults"
	// SourceFile is a .codeaf/factory.yaml that parsed.
	SourceFile = "file"
	// SourceMarkdown is a .codeaf/factory.md read as this file.
	SourceMarkdown = "markdown"

	// DefaultMaxSize is the largest job that starts on its own.
	DefaultMaxSize = "M"
	// DefaultMaxCost is the dollars per job, over which the job asks first.
	DefaultMaxCost = 2
	// DefaultIssueMode is how an issue's size is read: the model's rubric.
	DefaultIssueMode = "model"
	// IssueModeLabel reads size/S, size/M and size/L off the issue's labels.
	IssueModeLabel = "label"

	// DefaultPRSmall is the changed-line bound for S, and DefaultPRMedium for M.
	// Past that a pull request is L. A pull request is measured, never guessed.
	// The same bounds are pullSize in internal/factory/github/source.go.
	DefaultPRSmall  = 50
	DefaultPRMedium = 400
)

// File is one pipeline file after its omitted keys have been filled in.
// Personal files are the exception: an omitted key there means no opinion,
// and [Merge] is what applies one.
type File struct {
	DefaultBudget float64
	Start         Start
	Size          Size
	Pipeline      map[factory.Kind][]string
	Steps         map[string]Step
	Fixed         []string
	Rules         []string
	// Adapt is the `adapt:` key: how much a job's plan may reshape the steps
	// it runs, for every pipeline in the file ([factory.AdaptMode]): adapt
	// (within the bounds, the default), ask (and a person ratifies before
	// anything after plan runs) or fixed (the steps as written). Empty when
	// the file says nothing, which is adapt.
	Adapt factory.AdaptMode
	// Merge is the `merge:` key. AUTO-MERGE IS OFF unless a file turns it on;
	// a personal file may only turn it off ([Merge]).
	Merge factory.MergePolicy
	// Source is defaults, file or markdown. From is where it was read
	// (factory.FromMainBranch and its siblings), empty until [Load].
	Source string
	From   string
	// Text is the repository file as it was read. PersonalText is the
	// personal file when [MergePersonal] tightened this read, and Personal
	// says that file was applied.
	Text         string
	PersonalText string
	Personal     bool
	// House is the factory's own rules when [WithHouseRules] added them to this
	// read, so a client can say which lines came from the factory rather than
	// the repository. The repository's Text never holds them.
	House factory.HouseRules
	// Problems are lines that did not load. The rest of the file did.
	Problems []Problem

	// set records which keys a personal file wrote. A repo file leaves it unused.
	set presence
	// markdownAdapt preserves the older section-wide constraint until conversion
	// is explicitly saved; YAML represents fixed steps rather than section modes.
	markdownAdapt map[factory.Kind]factory.AdaptMode
}

// Start is what may begin without a person. An empty Labels means nothing
// starts on its own: a label has to be named first.
type Start struct {
	Labels   []string
	MaxSize  string
	MaxCost  float64
	Never    []string
	AskFirst []string
}

// Size is how S, M and L are decided. PRLines is the changed-line bounds.
// Issue is [DefaultIssueMode] or [IssueModeLabel].
type Size struct {
	PRLines [2]int
	Issue   string
}

// Step is one prompt. Max of 0 is one round; a shipped step carries its own
// ([factory.DefaultRecipe]), which a file's max replaces. Until is the sentence the step
// stops on; the four words the runner checks are done, clean, green and proven,
// and any other sentence is carried in the prompt.
//
// Kind, When, Effort and Fanout are [factory.Stage]'s own fields, spelled as the
// runner spells them. "" is the default: a chat, always, the knee, one
// conversation. No other word is read, and a file never invents one. A file
// that says `kind: chat` reads as "", so the two spellings are one file.
type Step struct {
	Prompt string
	Kind   string
	When   string
	Effort string
	Fanout string
	Until  string
	Max    int

	wrotePrompt bool
	wroteUntil  bool
	wroteMax    bool
	// hold is why the step was read as something it cannot run as, already
	// named in a Problem where it was read. See [Step.holdWhy].
	hold string
}

// holdWhy is why this step cannot run as written, or "". A step with a reason
// is a gate in [File.Recipe] and is refused by [Encode].
func (s Step) holdWhy() string {
	if s.hold != "" {
		return s.hold
	}
	if !textRun(s.Kind) {
		return ""
	}
	if strings.TrimSpace(s.Prompt) == "" {
		return "a " + s.Kind + " runs its own prompt as written and this one has none; the run holds here for a person"
	}
	if _, free := mechanicalUntil(s.Until); free {
		return "a " + s.Kind + " stops on done, clean, green or proven, not on \"" + strings.TrimSpace(s.Until) + "\"; the run holds here for a person"
	}
	return ""
}

// textRun says whether a kind hands its prompt to something other than a
// model: a check's shell, a post's verb. Neither may be given extra words.
func textRun(kind string) bool {
	return kind == string(factory.StageCheck) || kind == string(factory.StagePost)
}

// isGate says whether a step is a person: a gate by kind, or a step that holds.
func (s Step) isGate() bool {
	return s.Kind == string(factory.StageGate) || s.holdWhy() != ""
}

// kindWord reads one kind. chat is the default and reads as "", so a file that
// says chat and a file that says nothing are the same file.
func kindWord(s string) (string, bool) {
	k, ok := stepWord(s, StepKinds)
	if k == string(factory.StageChat) {
		k = ""
	}
	return k, ok
}

// StepKinds are the kinds a step may name, the runner's own [factory.StageKinds].
// StepFanouts are the fanouts a step may name. They are the words the recipe
// vocabulary documents on [factory.Stage]; a count or a free "per x" is a
// markdown spelling the YAML does not take.
var (
	StepKinds   = []string{string(factory.StageChat), string(factory.StageCheck), string(factory.StageGate), string(factory.StagePost)}
	StepFanouts = []string{"one", "per-file", "per-finding", "per-claim"}
)

// stepWord normalizes one runtime word and says whether it is allowed. "" is
// always allowed because it is the default.
func stepWord(s string, words []string) (string, bool) {
	s = strings.Join(strings.Fields(strings.ToLower(s)), " ")
	if s == "" {
		return "", true
	}
	for _, w := range words {
		if s == w {
			return s, true
		}
	}
	return "", false
}

// Problem is one thing in a pipeline file that did not load, and why.
type Problem struct {
	Line  int
	Where string
	Why   string
}

func (p Problem) Error() string {
	where := p.Where
	if where == "" {
		where = RepoFile
	}
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", where, p.Line, p.Why)
	}
	return where + ": " + p.Why
}

// presence is which keys were in the file, so a personal file can omit one.
type presence struct {
	defaultBudget bool
	labels        bool
	maxSize       bool
	maxCost       bool
	never         bool
	askFirst      bool
	prLines       bool
	issue         bool
	pipeline      map[factory.Kind]bool
	fixed         bool
	rules         bool
	adapt         bool
	mergeAuto     bool
	mergeMethod   bool
}

// Defaults is the file a repository with nothing of its own runs.
//
// ITS STEPS ARE [factory.DefaultRecipe], read back as a file: each kind's list
// is that recipe's stages by name, every shipped stage is a built-in step, and
// fixed is the shipped stages that are fixed. So Defaults().Recipe() is the
// default recipe, stage for stage, and a job a start rule starts on a repository
// with no file runs exactly what one a person starts runs.
func Defaults() File {
	recipe := factory.DefaultRecipe()
	pipeline := map[factory.Kind][]string{}
	for _, kind := range []factory.Kind{factory.KindIssue} {
		names := []string{}
		for _, st := range recipe.For(kind) {
			names = append(names, st.Name)
		}
		pipeline[kind] = names
	}
	fixed := []string{}
	for _, st := range factory.ShippedStages() {
		if st.Fixed {
			fixed = append(fixed, st.Name)
		}
	}
	return File{
		DefaultBudget: recipe.DefaultBudget,
		Start: Start{
			Labels:   []string{},
			MaxSize:  DefaultMaxSize,
			MaxCost:  DefaultMaxCost,
			Never:    []string{},
			AskFirst: []string{},
		},
		Size: Size{
			PRLines: [2]int{DefaultPRSmall, DefaultPRMedium},
			Issue:   DefaultIssueMode,
		},
		Pipeline: pipeline,
		Steps:    Builtins(),
		Fixed:    fixed,
		Rules:    []string{},
	}
}

// builtinOrder is the order [FormatSteps] prints, the shipped stages' own.
// builtinTable is each one as a step.
var builtinOrder, builtinTable = builtinSteps()

// builtinSteps reads [factory.ShippedStages] as steps: the ask is the prompt,
// the until is the runner's own word, and the rounds and fanout travel as they
// are. Approve is a person, written the way a markdown approve step is read.
func builtinSteps() ([]string, map[string]Step) {
	stages := factory.ShippedStages()
	order := make([]string, 0, len(stages))
	table := make(map[string]Step, len(stages))
	for _, st := range stages {
		order = append(order, st.Name)
		if factory.IsApprove(st) {
			table[st.Name] = Step{Until: "you"}
			continue
		}
		kind := string(st.Kind)
		if st.Kind == factory.StageChat {
			kind = ""
		}
		table[st.Name] = Step{Prompt: st.Ask, Kind: kind, When: st.When, Effort: st.Effort, Fanout: st.Fanout, Until: st.Until, Max: st.Max}
	}
	return order, table
}

// Builtins are the prompts codeaf ships. A pipeline names them; a file overrides
// one by writing the same name under steps.
func Builtins() map[string]Step {
	out := make(map[string]Step, len(builtinTable))
	for name, step := range builtinTable {
		out[name] = step
	}
	return out
}

// FormatSteps prints every built-in prompt, the text `codeaf factory steps` shows.
func FormatSteps() string {
	var b strings.Builder
	for i, name := range builtinOrder {
		if i > 0 {
			b.WriteByte('\n')
		}
		step := builtinTable[name]
		b.WriteString(name)
		b.WriteString(":\n")
		if name == factory.ApproveName {
			b.WriteString("  a person\n")
			continue
		}
		if step.Until != "" {
			b.WriteString("  until: ")
			b.WriteString(step.Until)
			b.WriteByte('\n')
		}
		if step.Max > 0 {
			fmt.Fprintf(&b, "  max: %d\n", step.Max)
		}
		b.WriteString("  ")
		b.WriteString(step.Prompt)
		b.WriteByte('\n')
	}
	return b.String()
}

// Recipe is the file as the runner's recipe. Issue steps are [factory.Recipe.Stages];
// every other kind is ByKind. A free until sentence is written into a chat's
// ask ("Stop when: …") and the stage's until is done, because the runner only
// checks done, clean, green and proven. approve is a gate, and so is every step
// that cannot run as written ([Step.holdWhy]).
//
// A kind whose list is empty is left out, so [factory.Recipe.For] falls back to
// the issue steps. The empty list stays on [File.Pipeline], which is the file.
func (f File) Recipe() factory.Recipe {
	r := factory.Recipe{
		Source:        f.Source,
		DefaultBudget: f.DefaultBudget,

		Adapt:  f.adaptByKind(),
		Policy: append([]string(nil), f.Rules...),
		ByKind: map[factory.Kind][]factory.Stage{},
		Merge:  f.Merge,
	}
	for _, problem := range f.Problems {
		r.Problems = append(r.Problems, problem.Error())
	}
	for kind, names := range f.Pipeline {
		stages := f.stages(names)
		if kind == factory.KindIssue {
			r.Stages = stages
			continue
		}
		if len(stages) == 0 {
			continue
		}
		r.ByKind[kind] = stages
	}
	if r.Stages == nil {
		r.Stages = []factory.Stage{}
	}
	return r
}

func (f File) stages(names []string) []factory.Stage {
	var out []factory.Stage
	used := map[string]bool{}
	for _, raw := range names {
		if factory.ApproveWord(raw) {
			if used[raw] {
				continue
			}
			used[raw] = true
			out = append(out, factory.Stage{Name: raw, Kind: factory.StageGate, On: true, Fixed: listed(f.Fixed, raw)})
			continue
		}
		step, ok := f.Steps[raw]
		if !ok {
			continue
		}
		person := step.isGate()
		if !person && strings.TrimSpace(step.Prompt) == "" {
			continue
		}
		name, err := factory.StageWord(strings.ReplaceAll(raw, "-", ""))
		if err != nil || used[name] {
			if !person {
				continue
			}
			// A PERSON IS NEVER DROPPED FOR ITS NAME: the steps after it would
			// run without them. It holds under the next free approve name.
			name = freeApprove(used, names)
		}
		used[name] = true
		if step.holdWhy() != "" {
			// Nothing of a step that cannot run as written reaches the runner:
			// no prompt, no condition. A person decides whether the run goes on.
			out = append(out, factory.Stage{Name: name, Kind: factory.StageGate, On: true, Fixed: listed(f.Fixed, raw)})
			continue
		}
		word, free := mechanicalUntil(step.Until)
		ask := strings.TrimSpace(step.Prompt)
		kind := factory.StageKind(step.Kind)
		if kind == "" {
			kind = factory.StageChat
		}
		// Only a model reads a stop sentence. A check's ask is a shell command
		// and a post's is a verb, so the sentence is never added to either.
		if free && kind == factory.StageChat {
			ask = withStop(ask, step.Until)
		}
		out = append(out, factory.Stage{
			Name: name, Kind: kind, Ask: ask, When: step.When, Effort: step.Effort,
			Fanout: step.Fanout, Until: word, Max: step.Max, On: true, Fixed: listed(f.Fixed, raw),
		})
	}
	return out
}

// freeApprove is the first approve name neither taken nor listed in names.
func freeApprove(used map[string]bool, names []string) string {
	name := factory.ApproveName
	for k := 2; used[name] || listed(names, name); k++ {
		name = fmt.Sprintf("%s%d", factory.ApproveName, k)
	}
	return name
}

// mechanicalUntil is the runner's word for a sentence, and whether the sentence
// itself has to travel in the prompt because the runner cannot check it.
func mechanicalUntil(sentence string) (word string, free bool) {
	switch strings.ToLower(strings.TrimSpace(sentence)) {
	case "", "done", "the step says it is done":
		return factory.UntilDone, false
	case "clean", "no findings":
		return factory.UntilClean, false
	case "green":
		return factory.UntilGreen, false
	case "proven", "every claim has evidence":
		return factory.UntilProven, false
	default:
		return factory.UntilDone, true
	}
}

func withStop(prompt, until string) string {
	until = strings.TrimSpace(until)
	if until == "" {
		return prompt
	}
	if prompt == "" {
		return "Stop when: " + until
	}
	if strings.Contains(strings.ToLower(prompt), strings.ToLower(until)) {
		return prompt
	}
	return prompt + " Stop when: " + until
}

func listed(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func copyAdapt(modes map[factory.Kind]factory.AdaptMode) map[factory.Kind]factory.AdaptMode {
	if modes == nil {
		return nil
	}
	out := make(map[factory.Kind]factory.AdaptMode, len(modes))
	for kind, mode := range modes {
		out[kind] = mode
	}
	return out
}

// RecipeProblems preserves file locations when the read crosses the seam.
func (f File) RecipeProblems() []factory.Problem {
	out := make([]factory.Problem, len(f.Problems))
	for i, p := range f.Problems {
		out[i] = factory.Problem{Line: p.Line, Text: p.Where, Why: p.Why}
	}
	return out
}

// adaptByKind is the recipe's adapt word per kind: the file's `adapt:` for the
// issue pipeline and every pipeline the file lists, else the Markdown recipe's
// words carried over, else none (the default, adapt).
func (f File) adaptByKind() map[factory.Kind]factory.AdaptMode {
	if f.Adapt == "" {
		return copyAdapt(f.markdownAdapt)
	}
	out := map[factory.Kind]factory.AdaptMode{factory.KindIssue: f.Adapt}
	for kind := range f.Pipeline {
		out[kind] = f.Adapt
	}
	return out
}
