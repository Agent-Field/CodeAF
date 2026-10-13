package spec

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// AgentDoc is agent.md: YAML front matter plus the instructions body.
type AgentDoc struct {
	Name  string   `yaml:"name" json:"name"`
	Model string   `yaml:"model" json:"model,omitempty"`
	Tools []string `yaml:"tools" json:"tools,omitempty"`
	Body  string   `yaml:"-" json:"body"`
}

var frontMatterFence = []byte("---\n")

// ParseAgent reads agent.md. Front matter is required and must name the agent.
func ParseAgent(data []byte) (*AgentDoc, error) {
	data = bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	if !bytes.HasPrefix(data, frontMatterFence) {
		return nil, fmt.Errorf("agent.md must start with --- front matter")
	}
	rest := data[len(frontMatterFence):]
	end := bytes.Index(rest, []byte("\n---\n"))
	if end < 0 {
		return nil, fmt.Errorf("agent.md front matter is not closed with ---")
	}
	doc := &AgentDoc{}
	if err := yaml.Unmarshal(rest[:end+1], doc); err != nil {
		return nil, fmt.Errorf("agent.md front matter: %w", err)
	}
	if doc.Name == "" {
		return nil, fmt.Errorf("agent.md front matter needs name")
	}
	doc.Body = string(bytes.TrimSpace(rest[end+len("\n---\n"):]))
	return doc, nil
}
