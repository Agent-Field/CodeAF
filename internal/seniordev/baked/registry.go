//go:build !windows

// Package baked embeds the agent document the run executes. The roster is a
// single agent, coder: the pipeline reads its prompt body, its frontmatter
// metadata (model, steps, tier) and its permission rules.
package baked

import (
	"embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

var agentNames = []string{"coder"}

//go:embed agents/*.md
var agentFiles embed.FS

type agentDocument struct {
	raw      string
	prompt   string
	metadata map[string]any
}

var agentDocuments = loadAgentDocuments()

func loadAgentDocuments() map[string]agentDocument {
	out := make(map[string]agentDocument, len(agentNames))
	for _, name := range agentNames {
		data, err := agentFiles.ReadFile("agents/" + name + ".md")
		if err != nil {
			panic("baked agent asset missing: " + name)
		}
		raw := string(data)
		prompt, frontmatter, err := parseAgentMarkdown(raw)
		if err != nil {
			panic(fmt.Sprintf("baked agent %q frontmatter: %v", name, err))
		}
		metadata := map[string]any{}
		if err := yaml.Unmarshal([]byte(frontmatter), &metadata); err != nil {
			panic(fmt.Sprintf("baked agent %q frontmatter: %v", name, err))
		}
		out[name] = agentDocument{raw: raw, prompt: prompt, metadata: metadata}
	}
	return out
}

func parseAgentMarkdown(markdown string) (string, string, error) {
	normalized := strings.ReplaceAll(markdown, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return strings.TrimSpace(normalized), "", nil
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return "", "", fmt.Errorf("unterminated YAML frontmatter")
	}
	after := rest[end+len("\n---"):]
	if after != "" && !strings.HasPrefix(after, "\n") {
		return "", "", fmt.Errorf("closing YAML delimiter is not on its own line")
	}
	return strings.TrimSpace(strings.TrimPrefix(after, "\n")), rest[:end], nil
}

// PromptContent strips YAML frontmatter and trims the model-visible body.
func PromptContent(markdown string) string {
	prompt, _, err := parseAgentMarkdown(markdown)
	if err != nil {
		return ""
	}
	return prompt
}

// GetBakedAgent returns only the model-visible Markdown body for an agent.
func GetBakedAgent(name string) (string, bool) {
	document, ok := agentDocuments[name]
	return document.prompt, ok
}

// GetBakedAgentMarkdown returns the source document for frontmatter consumers.
func GetBakedAgentMarkdown(name string) (string, bool) {
	document, ok := agentDocuments[name]
	return document.raw, ok
}

// GetBakedAgentMetadata returns the parsed YAML fields used to configure an
// agent without exposing them to the model.
func GetBakedAgentMetadata(name string) (map[string]any, bool) {
	document, ok := agentDocuments[name]
	if !ok {
		return nil, false
	}
	metadata := make(map[string]any, len(document.metadata))
	for key, value := range document.metadata {
		metadata[key] = value
	}
	return metadata, true
}

// ListBakedAgents returns the baked agent names in registry order.
func ListBakedAgents() []string {
	return append([]string(nil), agentNames...)
}
