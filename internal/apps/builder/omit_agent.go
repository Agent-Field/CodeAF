package builder

import (
	"bytes"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"gopkg.in/yaml.v3"
)

// The agent loses runs.start when the final run is left out, so Check can
// still enforce that every tool on its belt has an available capability.
func omitAgentRunTool(data []byte) ([]byte, error) {
	doc, err := spec.ParseAgent(data)
	if err != nil {
		return nil, err
	}
	keep := []string{}
	found := false
	for _, tool := range doc.Tools {
		if tool == "runs.start" {
			found = true
		} else {
			keep = append(keep, tool)
		}
	}
	if !found {
		return data, nil
	}
	normal := bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
	end := bytes.Index(normal[4:], []byte("\n---\n")) + 4
	var front map[string]any
	if err := yaml.Unmarshal(normal[4:end], &front); err != nil {
		return nil, err
	}
	front["tools"] = keep
	raw, err := yaml.Marshal(front)
	if err != nil {
		return nil, err
	}
	result := append([]byte("---\n"), raw...)
	result = append(result, []byte("---\n")...)
	return append(result, normal[end+5:]...), nil
}
