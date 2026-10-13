package agent

import (
	"encoding/json"
	"strings"
)

// ShapeThisText is the exact answer to a request that changes the app (rule 02).
const ShapeThisText = "That changes the app"

// action is one model step: speak, call a tool, or hand the request to Shape.
type action struct {
	Say       string         `json:"say,omitempty"`
	Tool      string         `json:"tool,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	ShapeThis bool           `json:"shape_this,omitempty"`
}

// actionSchema is sent with every call; providers without schema support
// still get the same contract through the system prompt.
var actionSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "say": {"type": "string"},
    "tool": {"type": "string"},
    "args": {"type": "object"},
    "shape_this": {"type": "boolean"}
  }
}`)

// parseAction reads a model reply. A reply that is not JSON is plain speech,
// so a model that ignores the format still answers.
func parseAction(text string) action {
	body := strings.TrimSpace(text)
	body = strings.TrimSuffix(strings.TrimPrefix(body, "```json"), "```")
	body = strings.TrimSpace(strings.TrimPrefix(body, "```"))
	var a action
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return action{Say: strings.TrimSpace(text)}
	}
	return a
}
