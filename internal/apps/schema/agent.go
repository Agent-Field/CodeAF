package schema

import (
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentInstructions reads an assistant's Markdown body and optional model hint.
func AgentInstructions(markdown string) (body, model string, err error) {
	body = strings.ReplaceAll(markdown, "\r\n", "\n")
	if front, ok := strings.CutPrefix(body, "---\n"); ok {
		metadata, rest, found := strings.Cut(front, "\n---\n")
		if !found {
			return "", "", errors.New("agent.md front matter needs a closing delimiter")
		}
		var header struct {
			Model string `yaml:"model"`
		}
		if err := yaml.Unmarshal([]byte(metadata), &header); err != nil {
			return "", "", err
		}
		body, model = rest, header.Model
	}
	return strings.TrimSpace(body), model, nil
}
