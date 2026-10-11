package pipeline

import "gopkg.in/yaml.v3"

// Convert renders a Markdown recipe as YAML without writing either file.
// Warnings accompany the result because older stage knobs may lose meaning.
func Convert(text string) ([]byte, []Problem, error) {
	f := FromMarkdown(text)
	type step struct {
		Prompt string `yaml:"prompt"`
		Until  string `yaml:"until,omitempty"`
		Max    int    `yaml:"max,omitempty"`
	}
	steps := make(map[string]step)
	for name, s := range f.Steps {
		if name != "approve" {
			steps[name] = step{s.Prompt, s.Until, s.Max}
		}
	}
	// Conversion cannot opt a repository into unattended work: start is absent.
	data, err := yaml.Marshal(struct {
		Pipeline any      `yaml:"pipeline"`
		Steps    any      `yaml:"steps"`
		Fixed    []string `yaml:"fixed"`
		Rules    []string `yaml:"rules"`
	}{f.Pipeline, steps, f.Fixed, f.Rules})
	return data, f.Problems, err
}
