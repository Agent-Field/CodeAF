//go:build e2e

package e2e

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/store"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type privateMemoryRequestTrace struct {
	Time    string `json:"ts"`
	Path    string `json:"-"`
	Request struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
		Tools []json.RawMessage `json:"tools"`
	} `json:"request"`
}

// privateMemoryPromptGroundTruth uses the FIRST main-turn request, never a
// later request whose tool result contains the sought fact. Only its system
// memory block is evidence of anticipatory admission.
func privateMemoryPromptGroundTruth(home, prompt, source string) (*bool, string, string) {
	files, _ := filepath.Glob(filepath.Join(home, "logs", "trace", "*", "calls", "*.json"))
	traces := make([]privateMemoryRequestTrace, 0, len(files))
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var trace privateMemoryRequestTrace
		if json.Unmarshal(raw, &trace) != nil || len(trace.Request.Tools) == 0 {
			continue
		}
		trace.Path = path
		traces = append(traces, trace)
	}
	sort.SliceStable(traces, func(i, j int) bool { return traces[i].Time < traces[j].Time })
	for _, trace := range traces {
		if !privateMemoryTraceMatches(trace, prompt) {
			continue
		}
		block := privateMemoryTraceBlock(trace)
		present := source != "" && strings.Contains(block, store.MemoryQuote(source))
		return &present, block, trace.Path
	}
	return nil, "", ""
}
func privateMemoryTraceMatches(trace privateMemoryRequestTrace, prompt string) bool {
	for _, message := range trace.Request.Messages {
		if message.Role == "user" && canonicalPrivatePrompt(privateMemoryContent(message.Content)) == canonicalPrivatePrompt(prompt) {
			return true
		}
	}
	return false
}
func privateMemoryTraceBlock(trace privateMemoryRequestTrace) string {
	for _, message := range trace.Request.Messages {
		if message.Role != "system" {
			continue
		}
		content := privateMemoryContent(message.Content)
		_, tail, found := strings.Cut(content, "<memory>")
		if found {
			block, _, _ := strings.Cut(tail, "</memory>")
			return block
		}
	}
	return ""
}
func privateMemoryContent(raw json.RawMessage) string {
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var out strings.Builder
	for _, part := range parts {
		if part.Type == "text" {
			out.WriteString(part.Text)
		}
	}
	return out.String()
}
