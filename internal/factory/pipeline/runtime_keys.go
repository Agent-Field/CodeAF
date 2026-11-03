package pipeline

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// RuntimeKeysIn is true when text decodes as one factory.yaml whose top-level
// adapt is a map (one word per pipeline), or whose top-level steps mapping
// holds a step that is itself a mapping spelling the KEY kind, when, effort,
// fanout or who, whatever its value: valid, invalid, empty, or `kind:
// chat`. A parser that predates those words discards them and runs the step as a
// plain chat that always runs, so the mere spelling of a key is what a reader of
// this file cannot take on trust from a factory that never said it runs them.
//
// It walks the parsed YAML and never matches text, so a word in a comment or
// inside a quoted prompt is not a key. Text that does not decode, a scalar step,
// a one-word adapt and any other key outside steps are all false.
func RuntimeKeysIn(text string) bool {
	root, err := decodeRoot(text)
	if err != nil || root == nil {
		return false
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := ""
		if root.Content[i].Kind == yaml.ScalarNode {
			key = strings.TrimSpace(root.Content[i].Value)
		}
		// AN ADAPT MAP IS ONE TOO: a reader older than it finds a problem
		// where the word was and lets every plan reshape every pipeline.
		if key == "adapt" && resolveAlias(root.Content[i+1]).Kind == yaml.MappingNode {
			return true
		}
		if key != "steps" {
			continue
		}
		steps := resolveAlias(root.Content[i+1])
		if steps.Kind != yaml.MappingNode {
			continue
		}
		for j := 1; j < len(steps.Content); j += 2 {
			if stepSpellsRuntimeKey(resolveAlias(steps.Content[j])) {
				return true
			}
		}
	}
	return false
}

func resolveAlias(n *yaml.Node) *yaml.Node {
	for depth := 0; n.Kind == yaml.AliasNode && n.Alias != nil && depth < 8; depth++ {
		n = n.Alias
	}
	return n
}

func stepSpellsRuntimeKey(n *yaml.Node) bool {
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Kind != yaml.ScalarNode {
			continue
		}
		switch strings.TrimSpace(n.Content[i].Value) {
		case "kind", "when", "effort", "fanout", "who":
			return true
		}
	}
	return false
}
