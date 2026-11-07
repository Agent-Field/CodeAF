package pipeline

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"gopkg.in/yaml.v3"
)

// Encode is the one YAML representation used by conversion and every writer.
// Runtime source and read problems are omitted because they are not file keys.
func Encode(f File) ([]byte, error) {
	type step struct {
		Label  string  `yaml:"label,omitempty"`
		Prompt *string `yaml:"prompt,omitempty"`
		Kind   string  `yaml:"kind,omitempty"`
		When   string  `yaml:"when,omitempty"`
		Effort string  `yaml:"effort,omitempty"`
		Fanout string  `yaml:"fanout,omitempty"`
		Until  string  `yaml:"until,omitempty"`
		Max    int     `yaml:"max,omitempty"`
		Who    string  `yaml:"who,omitempty"`
	}
	names := make([]string, 0, len(f.Steps))
	for name := range f.Steps {
		names = append(names, name)
	}
	sort.Strings(names)
	steps := make(map[string]step)
	builtin := Builtins()
	for _, name := range names {
		s := f.Steps[name]
		// A SHIPPED STEP THE FILE LEAVES AS SHIPPED IS NOT WRITTEN: a reader
		// fills it back in, and writing all of them buried the few steps a
		// person wrote under every one they did not.
		if b, ok := builtin[name]; ok && sameStep(s, b) {
			continue
		}
		// An approval carries its label and who it waits on, but no prompt.
		if factory.ApproveWord(name) {
			if s.Who != "" || strings.TrimSpace(s.Label) != "" {
				steps[name] = step{Label: strings.TrimSpace(s.Label), Who: s.Who}
			}
			continue
		}
		// A STEP THAT HOLDS IS NEVER WRITTEN. The word that made it hold
		// could only be written as the default, which is a chat that runs.
		if why := s.holdWhy(); why != "" {
			return nil, fmt.Errorf("steps.%s: %s", name, why)
		}
		prompt := &s.Prompt
		if s.Kind == string(factory.StageGate) && s.Prompt == "" {
			prompt = nil // a person needs no prompt, and "" would read as a missing one
		}
		until, rounds := s.Until, s.Max
		// A SHIPPED STEP A FILE CHANGED IN PART WRITES ONLY THAT PART: the
		// words it did not write are the shipped ones, which a reader fills
		// back in, so a new prompt for write is not saved with write's
		// shipped stop and rounds echoed beside it. Only a chat inherits
		// (a step of another kind keeps none of the shipped words).
		if b, ok := builtin[name]; ok && s.Kind == b.Kind {
			if !s.wrotePrompt && s.Prompt == b.Prompt {
				prompt = nil
			}
			if !s.wroteUntil && s.Until == b.Until {
				until = ""
			}
			if !s.wroteMax && s.Max == b.Max {
				rounds = 0
			}
		}
		steps[name] = step{strings.TrimSpace(s.Label), prompt, s.Kind, s.When, s.Effort, s.Fanout, until, rounds, s.Who}
	}
	// A KEY THE FILE NEVER SAID IS NEVER WRITTEN, the same law as merge
	// below. Writing a default spells it out, and a spelled-out key READS
	// DIFFERENTLY: an explicit max_cost asks first on every job with no cost
	// history, an explicit size.issue asks for size labels, and an explicit
	// default_budget or max_cost is no longer the factory's spending to set.
	// A factory copy saved through Encode sat every routed job at "Approval
	// needed: start" that way. A key is written when it was in the file or
	// its value is not the default (a File built in code carries no
	// presence).
	var start any
	var size any
	if f.Source != SourceMarkdown {
		type startKey struct {
			Labels   *[]string `yaml:"labels,omitempty"`
			MaxSize  string    `yaml:"max_size,omitempty"`
			MaxCost  *float64  `yaml:"max_cost,omitempty"`
			Never    *[]string `yaml:"never,omitempty"`
			AskFirst *[]string `yaml:"ask_first,omitempty"`
		}
		var k startKey
		if f.set.labels || len(f.Start.Labels) > 0 {
			k.Labels = listKey(f.Start.Labels)
		}
		if f.set.maxSize || normOr(f.Start.MaxSize, DefaultMaxSize) != DefaultMaxSize {
			k.MaxSize = f.Start.MaxSize
		}
		if f.set.maxCost || f.Start.MaxCost != DefaultMaxCost {
			v := f.Start.MaxCost
			k.MaxCost = &v
		}
		if f.set.never || len(f.Start.Never) > 0 {
			k.Never = listKey(f.Start.Never)
		}
		if f.set.askFirst || len(f.Start.AskFirst) > 0 {
			k.AskFirst = listKey(f.Start.AskFirst)
		}
		if k.Labels != nil || k.MaxSize != "" || k.MaxCost != nil || k.Never != nil || k.AskFirst != nil {
			start = k
		}
		type sizeKey struct {
			PRLines *[2]int `yaml:"pr_lines,omitempty"`
			Issue   string  `yaml:"issue,omitempty"`
		}
		var z sizeKey
		if f.set.prLines || f.Size.PRLines != [2]int{DefaultPRSmall, DefaultPRMedium} {
			v := f.Size.PRLines
			z.PRLines = &v
		}
		if f.set.issue || !strings.EqualFold(strings.TrimSpace(f.Size.Issue), DefaultIssueMode) {
			z.Issue = f.Size.Issue
		}
		if z.PRLines != nil || z.Issue != "" {
			size = z
		}
	}
	def := Defaults()
	pipelines := map[factory.Kind][]string{}
	for kind, names := range f.Pipeline {
		if f.set.pipeline[kind] || !slices.Equal(names, def.Pipeline[kind]) {
			pipelines[kind] = names
		}
	}
	var fixed, rules *[]string
	if f.set.fixed || !slices.Equal(f.Fixed, def.Fixed) {
		fixed = listKey(f.Fixed)
	}
	if f.set.rules || len(f.Rules) > 0 {
		rules = listKey(f.Rules)
	}
	budget := 0.0
	if f.set.defaultBudget || f.DefaultBudget != factory.DefaultJobBudget {
		budget = f.DefaultBudget
	}
	// A FILE THAT SAYS NOTHING ABOUT MERGING IS WRITTEN WITHOUT THE KEY, so
	// saving a pipeline never adds a line its author did not write.
	type mergeKey struct {
		Auto   string `yaml:"auto,omitempty"`
		Method string `yaml:"method,omitempty"`
	}
	var merge *mergeKey
	if f.Merge.Auto || f.Merge.Method != "" || f.set.mergeAuto {
		merge = &mergeKey{Method: f.Merge.Method}
		if f.Merge.Auto {
			merge.Auto = "on"
		} else if f.set.mergeAuto {
			merge.Auto = "off"
		}
	}
	// A file that defines no step of its own writes no `steps:` key at all,
	// never an empty `steps: {}` its author did not write.
	var stepsKey any
	if len(steps) > 0 {
		stepsKey = steps
	}
	var pipelinesKey any
	if len(pipelines) > 0 {
		pipelinesKey = pipelines
	}
	return yaml.Marshal(struct {
		DefaultBudget float64   `yaml:"default_budget,omitempty"`
		Start         any       `yaml:"start,omitempty"`
		Size          any       `yaml:"size,omitempty"`
		Pipeline      any       `yaml:"pipeline,omitempty"`
		Steps         any       `yaml:"steps,omitempty"`
		Fixed         *[]string `yaml:"fixed,omitempty"`
		Rules         *[]string `yaml:"rules,omitempty"`
		Adapt         any       `yaml:"adapt,omitempty"`
		Merge         *mergeKey `yaml:"merge,omitempty"`
	}{budget, start, size, pipelinesKey, stepsKey, fixed, rules, adaptKey(f), merge})
}

// sameStep says a and b say the same thing in a file, reading only what a
// file can say.
func sameStep(a, b Step) bool {
	return a.Label == b.Label && a.Prompt == b.Prompt && a.Kind == b.Kind && a.When == b.When && a.Effort == b.Effort &&
		a.Fanout == b.Fanout && a.Until == b.Until && a.Max == b.Max && a.Who == b.Who
}

// listKey is a list key to write: a pointer, because omitempty drops an
// empty slice and a file that said `never: []` must still say it.
func listKey(s []string) *[]string {
	if s == nil {
		s = []string{}
	}
	return &s
}

// adaptKey is `adapt:` as the file writes it: one word, a map of one word per
// pipeline, or nothing (adapt everywhere).
func adaptKey(f File) any {
	if len(f.AdaptKinds) > 0 {
		out := make(map[string]string, len(f.AdaptKinds))
		for kind, mode := range f.AdaptKinds {
			out[string(kind)] = string(mode)
		}
		return out
	}
	if f.Adapt != "" {
		return string(f.Adapt)
	}
	return nil
}

// Convert renders a Markdown recipe as YAML without writing either file.
// Warnings accompany the result because older stage knobs may lose meaning.
func Convert(text string) ([]byte, []Problem, error) {
	f := FromMarkdown(text)
	data, err := Encode(f)
	return data, f.Problems, err
}
