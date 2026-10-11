// Package pipeline reads a repository's .codeaf/factory.yaml: the steps a job
// runs, the sentences it obeys, and what may start on its own.
//
// A REPO WITH NO FILE GETS THE DEFAULTS AND WORKS. Every key is optional. The
// same keys in a personal file may only be stricter. The markdown recipe
// (.codeaf/factory.md) is read as this file when the yaml is not there yet.
//
// The shape is docs/design/factory/FACTORY-YAML.md. A step is a prompt. approve
// is a person and has no prompt. There is no condition language and no step kind.
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
	Start    Start
	Size     Size
	Pipeline map[factory.Kind][]string
	Steps    map[string]Step
	Fixed    []string
	Rules    []string
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

// Step is one prompt. Max of 0 is one round. Until is the sentence the step
// stops on; the four words the runner checks are done, clean, green and proven,
// and any other sentence is carried in the prompt.
type Step struct {
	Prompt string
	Until  string
	Max    int

	wrotePrompt bool
	wroteUntil  bool
	wroteMax    bool
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
	labels   bool
	maxSize  bool
	maxCost  bool
	never    bool
	askFirst bool
	prLines  bool
	issue    bool
	pipeline map[factory.Kind]bool
	fixed    bool
	rules    bool
}

// Defaults is the file a repository with nothing of its own runs.
func Defaults() File {
	return File{
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
		Pipeline: map[factory.Kind][]string{
			factory.KindIssue: {"plan", "approve", "write", "test", "review", "proof"},
			factory.KindPR:    {"read", "review", "proof"},
			factory.KindCI:    {"read", "write", "test", "proof"},
		},
		Steps: Builtins(),
		Fixed: []string{"approve", "proof"},
		Rules: []string{},
	}
}

// builtinOrder is the order [FormatSteps] prints. The map is the prompts.
var builtinOrder = []string{"plan", "approve", "write", "test", "review", "security", "proof", "read"}

var builtinTable = map[string]Step{
	"plan":     {Prompt: "read the issue and the code it names; write a plan and the list of files it will touch", Until: "the plan names its files"},
	"approve":  {Until: "you"},
	"write":    {Prompt: "carry out the plan in this worktree", Until: "the plan is done"},
	"test":     {Prompt: "find and run the repo's tests; fix what fails", Until: "green"},
	"review":   {Prompt: "review the diff as a careful colleague; fix what you find", Until: "clean"},
	"security": {Prompt: "review for injection, authz, secrets handling", Until: "no findings"},
	"proof":    {Prompt: "show each claim in its own medium", Until: "every claim has evidence"},
	"read":     {Prompt: "read the change at its head and the thread; say what it does and what it risks", Until: "summarised"},
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
// every other kind is ByKind. A free until sentence is written into the ask
// ("Stop when: …") and the stage's until is done, because the runner only checks
// done, clean, green and proven. approve is a gate.
//
// A kind whose list is empty is left out, so [factory.Recipe.For] falls back to
// the issue steps. The empty list stays on [File.Pipeline], which is the file.
func (f File) Recipe() factory.Recipe {
	r := factory.Recipe{
		Source: f.Source,

		Adapt:  copyAdapt(f.markdownAdapt),
		Policy: append([]string(nil), f.Rules...),
		ByKind: map[factory.Kind][]factory.Stage{},
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
		if !ok || strings.TrimSpace(step.Prompt) == "" {
			continue
		}
		name, err := factory.StageWord(strings.ReplaceAll(raw, "-", ""))
		if err != nil || used[name] {
			continue
		}
		used[name] = true
		word, free := mechanicalUntil(step.Until)
		ask := strings.TrimSpace(step.Prompt)
		if free {
			ask = withStop(ask, step.Until)
		}
		out = append(out, factory.Stage{
			Name: name, Kind: factory.StageChat, Ask: ask, Until: word,
			Max: step.Max, On: true, Fixed: listed(f.Fixed, raw),
		})
	}
	return out
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
