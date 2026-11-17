package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
)

func TestTaskChildrenKeepUnreadableProfileApproval(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("mode 000 requires nonroot")
	}
	profile := t.TempDir()
	file := filepath.Join(profile, "config.json")
	if err := os.WriteFile(file, []byte(`{"tools.approvalMode":"prompt","tools.approvals":"write:deny"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })
	_, readErr := config.ReadProfileApprovalConfig(profile)
	if readErr == nil {
		t.Fatal("mode 000 profile unexpectedly readable")
	}
	for _, kind := range []string{"worker", "repair", "auditor"} {
		for _, pushed := range []bool{false, true} {
			name := kind + "/configured"
			if pushed {
				name = kind + "/pushed"
			}
			t.Run(name, func(t *testing.T) {
				tool, args := "write", `{"path":"denied.txt","content":"must not be written"}`
				if kind == "auditor" {
					tool, args = "bash", `{"command":"pwd"}`
				}
				script := &scriptedCompleter{steps: []step{
					func(context.Context, []ai.Message) (*ai.Response, error) {
						return toolResponse("attempt", tool, args), nil
					},
					func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
				}}
				policy := &approval.Policy{ProfileReadError: readErr.Error()}
				parent, _ := newTestAgent(t, script, func(c *Config) {
					c.OneModel = true
					if !pushed {
						c.ApprovalPolicy = policy
					}
				})
				if pushed {
					parent.SetApprovalPolicy(policy)
				}
				g := parent.graph()
				g.run = func(*TaskNode) {}
				id := g.reserve()
				g.admit(id, taskSpec{title: "work", brief: "answer", acceptance: "done"})
				workspace := t.TempDir()
				var child *Agent
				var err error
				switch kind {
				case "auditor":
					child, err = parent.newAuditAgent(workspace, g.node(id), plainDoor(auditReadCommands), "")
				case "repair":
					child, err = parent.newTaskAgentOn(context.Background(), workspace, g.node(id), "-repair1", "", true)
				default:
					child, err = parent.newTaskAgent(context.Background(), workspace, g.node(id), "")
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = child.Close() })
				events := collect(t, mustSubmit(t, child, "attempt the call"))
				failed, ok := firstOfKind(events, EventToolFailed)
				t.Logf("child %s: failed=%v output=%q", tool, ok, failed.Output)
				if !ok || !strings.Contains(failed.Output, "saved approval rules could not be read") || !strings.Contains(failed.Output, readErr.Error()) {
					t.Errorf("child lost failed-read reason: events=%v refusal=%q", kinds(events), failed.Output)
				}
				if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); !os.IsNotExist(err) {
					t.Errorf("refused write created file: %v", err)
				}
			})
		}
	}
}
