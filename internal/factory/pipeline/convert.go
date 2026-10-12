package pipeline

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"gopkg.in/yaml.v3"
)

// Encode is the one YAML representation used by conversion and every writer.
// Runtime source and read problems are omitted because they are not file keys.
func Encode(f File) ([]byte, error) {
	type step struct {
		Prompt string `yaml:"prompt"`
		Until  string `yaml:"until,omitempty"`
		Max    int    `yaml:"max,omitempty"`
	}
	steps := make(map[string]step)
	for name, s := range f.Steps {
		if !factory.ApproveWord(name) {
			steps[name] = step{s.Prompt, s.Until, s.Max}
		}
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
