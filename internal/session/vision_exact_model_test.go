package session

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// --one-model settles text roles; its separate vision slot must also be pinned
// for an experiment that requires one model across both text and image input.
func TestExplicitVisionPinKeepsActualImageRequestOnSelectedModel(t *testing.T) {
	const selected = "deepseek/deepseek-v4.1-flash"
	completer := &scriptedCompleter{steps: []step{func(context.Context, []ai.Message) (*ai.Response, error) {
		return textResponse("The mock chart has two amber columns."), nil
	}}}
	agent, workspace := newTestAgent(t, completer, func(c *Config) {
		c.OneModel = true
		c.Model = selected
		withSlot(map[string]string{"vision": selected})(c)
		c.RolesSource = func(key string) (string, bool) {
			if key == roles.PinKey(roles.RoleVision) {
				return "other/vision-model", true
			}
			return "", false
		}
	})
	writeImage(t, workspace, "mock-chart.png", "MOCK-IMAGE-BYTES")
	result, isErr := runViewImage(t, agent, map[string]any{"path": "mock-chart.png", "question": "How many amber columns are present?"})
	if isErr {
		t.Fatalf("pinned vision failed: %s", result)
	}
	if model := completer.model(0); model != selected {
		t.Fatalf("actual image request used %q, want selected %q", model, selected)
	}
	if !strings.HasPrefix(result, "seen by "+selected+":") {
		t.Fatalf("vision receipt misattributes the answering model: %q", result)
	}
	sent := completer.request(0)
	if len(sent) != 1 || len(imagePartURLs(sent[0])) != 1 {
		t.Fatal("regression did not exercise an actual image-content model request")
	}
}
