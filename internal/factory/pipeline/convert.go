package pipeline

import (
	"fmt"
	"sort"

	"github.com/Agent-Field/codeaf/internal/factory"
	"gopkg.in/yaml.v3"
)

// Encode is the one YAML representation used by conversion and every writer.
// Runtime source and read problems are omitted because they are not file keys.
func Encode(f File) ([]byte, error) {
	type step struct {
		Prompt *string `yaml:"prompt,omitempty"`
		Kind   string  `yaml:"kind,omitempty"`
		When   string  `yaml:"when,omitempty"`
		Effort string  `yaml:"effort,omitempty"`
		Fanout string  `yaml:"fanout,omitempty"`
		Until  string  `yaml:"until,omitempty"`
		Max    int     `yaml:"max,omitempty"`
	}
	names := make([]string, 0, len(f.Steps))
	for name := range f.Steps {
		names = append(names, name)
	}
	sort.Strings(names)
	steps := make(map[string]step)
	for _, name := range names {
		s := f.Steps[name]
		if factory.ApproveWord(name) {
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
		steps[name] = step{prompt, s.Kind, s.When, s.Effort, s.Fanout, s.Until, s.Max}
	}
	var start any
	var size any
	if f.Source != SourceMarkdown {
		start = struct {
			Labels   []string `yaml:"labels"`
			MaxSize  string   `yaml:"max_size"`
			MaxCost  float64  `yaml:"max_cost"`
			Never    []string `yaml:"never"`
			AskFirst []string `yaml:"ask_first"`
		}{f.Start.Labels, f.Start.MaxSize, f.Start.MaxCost, f.Start.Never, f.Start.AskFirst}
		size = struct {
			PRLines [2]int `yaml:"pr_lines"`
			Issue   string `yaml:"issue"`
		}{f.Size.PRLines, f.Size.Issue}
	}
	return yaml.Marshal(struct {
		DefaultBudget float64  `yaml:"default_budget,omitempty"`
		Start         any      `yaml:"start,omitempty"`
		Size          any      `yaml:"size,omitempty"`
		Pipeline      any      `yaml:"pipeline"`
		Steps         any      `yaml:"steps"`
		Fixed         []string `yaml:"fixed"`
		Rules         []string `yaml:"rules"`
	}{f.DefaultBudget, start, size, f.Pipeline, steps, f.Fixed, f.Rules})
}

// Convert renders a Markdown recipe as YAML without writing either file.
// Warnings accompany the result because older stage knobs may lose meaning.
func Convert(text string) ([]byte, []Problem, error) {
	f := FromMarkdown(text)
	data, err := Encode(f)
	return data, f.Problems, err
}
