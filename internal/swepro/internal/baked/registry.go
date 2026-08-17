// Package baked ports the coder-only baked agent registry from
// swe-pro/src/baked/registry.ts:1-134 at commit 3b25a1a. Specialist review and
// architecture agent definitions are intentionally outside this port's seam.
package baked

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const (
	// EntryAgent is the default coder-pipeline entry point.
	EntryAgent = "root-orchestrator"
)

var agentNames = []string{
	"root-orchestrator",
	"subtask-executor",
	"deep-worker",
	"orchestrator",
	"designer",
	"fixer",
	"explorer",
	"planner",
	"planner-translate",
	"issue-writer",
	"pr-ready-planner",
	"pr-formatter",
	"superpowers-code-reviewer",
	"auditor",
	"auditor-light",
	"adjudicator",
	"coder",
	"issue-advisor",
	"replanner",
	"retry-advisor",
	"input-classifier",
	"product-manager",
	"architect",
	"tech-lead",
	"fix-generator",
	"gate-synthesizer",
	"merger",
	"observer",
	"validity-judge",
	"convention-scout",
	"contract-reviewer",
	"plan-arbiter",
	"plan-sketch",
	"root-cause",
}

var loadBearingAgentNames = []string{
	"root-orchestrator",
	"subtask-executor",
	"deep-worker",
	"planner",
	"fixer",
	"merger",
	"auditor",
	"auditor-light",
	"coder",
}

//go:embed agents/*.md
var agentFiles embed.FS

type agentDocument struct {
	raw      string
	prompt   string
	metadata map[string]any
}

// agentDocuments parses the embedded roster on first use, not at init.
//
// The parse is not cheap — thirty-four Markdown files whose YAML frontmatter
// each becomes a map[string]any — and at package-init it was 12 ms, 4 MB and
// 46k allocations on every single `aforge` invocation, a third of the whole
// binary's init budget. Every caller of it is inside the swe pipeline
// (codeaf, tool, reviewgate, agentjson), so a chat, a plan or a --help paid
// all of it and read none of it. Deferring to first use is what removes it
// from those runs; a swe run pays exactly what it paid before, once.
//
// The only observable shift is when a corrupt embed panics: at first agent
// lookup rather than at process start. The assets are compiled in and the
// package's own tests read every one of them, so that panic is a build-time
// impossibility, not a runtime mode.
var agentDocuments = sync.OnceValue(loadAgentDocuments)

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

// GetBakedAgent returns only the model-visible Markdown body for a core agent.
func GetBakedAgent(name string) (string, bool) {
	document, ok := agentDocuments()[name]
	return document.prompt, ok
}

// GetBakedAgentMarkdown returns the source document for frontmatter consumers.
func GetBakedAgentMarkdown(name string) (string, bool) {
	document, ok := agentDocuments()[name]
	return document.raw, ok
}

// GetBakedAgentMetadata returns the parsed YAML fields used to configure an
// agent without exposing them to the model.
func GetBakedAgentMetadata(name string) (map[string]any, bool) {
	document, ok := agentDocuments()[name]
	if !ok {
		return nil, false
	}
	metadata := make(map[string]any, len(document.metadata))
	for key, value := range document.metadata {
		metadata[key] = value
	}
	return metadata, true
}

// ListBakedAgents returns core agent names in JavaScript object insertion
// order.
func ListBakedAgents() []string {
	return append([]string(nil), agentNames...)
}

// LoadBearingAgents returns the dispatch targets whose absence must prevent
// startup, in Set insertion order.
func LoadBearingAgents() []string {
	return append([]string(nil), loadBearingAgentNames...)
}

// RosterEntry is the value-level agent definition used by the parity fixture.
type RosterEntry struct {
	Name     string `json:"name"`
	Markdown string `json:"markdown"`
	Tier     Tier   `json:"tier"`
}

// Roster returns every coder-only baked definition in registry insertion
// order.
func Roster() []RosterEntry {
	documents := agentDocuments()
	out := make([]RosterEntry, 0, len(agentNames))
	for _, name := range agentNames {
		document := documents[name]
		out = append(out, RosterEntry{
			Name:     name,
			Markdown: document.raw,
			Tier:     TierFor(name),
		})
	}
	return out
}
