package session

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/manual"
	"testing"
)

// The durable status capability belongs only to its manager, remains read-only,
// and is routine in Auto without overriding any explicit permission refusal.
func TestFactoryStatusManagerBeltManualAndAutoPermissions(t *testing.T) {
	for _, tc := range []struct {
		name string
		door RunDoor
		want bool
	}{
		{"durable manager", &savedManagerDoor{}, true},
		{"older run door", &fakeRunDoor{}, false},
		{"ordinary chat", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Agent{config: Config{Workspace: t.TempDir(), FactoryRun: tc.door}}
			a.tools = a.belt()
			found := false
			for _, tool := range a.offeredTools() {
				if tool.Name == "factory_status" {
					found = true
				}
			}
			if found != tc.want {
				t.Fatalf("factory_status present=%v, want=%v", found, tc.want)
			}
		})
	}
	if ActionCategoryForTool("factory_status") != ActionRead || !manual.Chat().Mentions("factory_status") {
		t.Fatal("factory_status must be a documented read")
	}
	base := &approval.Policy{Default: approval.ActionPrompt}
	if got := automaticJobPolicy(base).Check("factory_status", json.RawMessage(`{}`)); got.Action != approval.ActionAllow {
		t.Fatalf("routine Auto status asks for approval: %s", got.Action)
	}
	denied := &approval.Policy{Default: approval.ActionPrompt, Tools: map[string]approval.Action{"factory_status": approval.ActionDeny}}
	if got := automaticJobPolicy(denied).Check("factory_status", nil); got.Action != approval.ActionDeny {
		t.Fatal("Auto overrode an explicit status denial")
	}
	if base.Tools != nil {
		t.Fatal("Auto changed the saved policy")
	}
}
