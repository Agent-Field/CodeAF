package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/orchestrate"
)

func newProfileTestChild(t *testing.T, parent *Agent, kind, workspace string) *Agent {
	t.Helper()
	g := parent.graph()
	g.run = func(*TaskNode) {}
	id := g.reserve()
	g.admit(id, taskSpec{title: "work", brief: "answer", acceptance: "done"})
	var child *Agent
	var err error
	switch kind {
	case "worker":
		child, err = parent.newTaskAgent(context.Background(), workspace, g.node(id), "")
	case "repair":
		child, err = parent.newTaskAgentOn(context.Background(), workspace, g.node(id), "-repair1", "", true)
	case "auditor":
		child, err = parent.newAuditAgent(workspace, g.node(id), plainDoor(auditReadCommands), "")
	case "fleet":
		executor := &orchestrateExec{agent: parent, id: "work"}
		child, err = executor.newChild(workspace, orchestrate.Node{ID: "n1", Goal: "answer"})
	default:
		t.Fatalf("unknown child kind %q", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Close() })
	return child
}

func profileTestReadFailure(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	if err := os.Mkdir(filepath.Join(profile, "config.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	_, err := config.ReadProfileApprovalConfig(profile)
	if err == nil {
		t.Fatal("expected filesystem read failure")
	}
	return err.Error()
}

func TestChildrenObserveCurrentProfileReadFailure(t *testing.T) {
	for _, kind := range []string{"worker", "repair", "auditor", "fleet"} {
		for _, after := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failed_after_creation=%v", kind, after), func(t *testing.T) {
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
				parent, _ := newTestAgent(t, script, func(c *Config) { c.OneModel = true; c.ApprovalPolicy = &approval.Policy{Default: approval.ActionAllow} })
				readError := profileTestReadFailure(t)
				failed := &approval.Policy{ProfileReadError: readError}
				if !after {
					parent.SetApprovalPolicy(failed)
				}
				workspace := t.TempDir()
				child := newProfileTestChild(t, parent, kind, workspace)
				if after {
					parent.SetApprovalPolicy(failed)
				}
				events := collect(t, mustSubmit(t, child, "attempt the call"))
				refusal, ok := firstOfKind(events, EventToolFailed)
				if !ok || !strings.Contains(refusal.Output, readError) {
					t.Fatalf("lost current profile failure: events=%v refusal=%q", kinds(events), refusal.Output)
				}
				if _, err := os.Stat(filepath.Join(workspace, "denied.txt")); !os.IsNotExist(err) {
					t.Fatalf("refused write created file: %v", err)
				}
			})
		}
	}
}

func TestChildObservesProfileFailureDuringTurn(t *testing.T) {
	for _, kind := range []string{"worker", "repair", "auditor", "fleet"} {
		for _, stage := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stage=%v", kind, stage), func(t *testing.T) {
				readError := profileTestReadFailure(t)
				// The child must keep its own allowance even when the parent's rules deny calls.
				normal := &approval.Policy{Default: approval.ActionDeny}
				var parent *Agent
				call := func(id string) *ai.Response {
					if kind == "auditor" {
						return toolResponse(id, "bash", `{"command":"pwd"}`)
					}
					return toolResponse(id, "write", fmt.Sprintf(`{"path":%q,"content":"written"}`, id+".txt"))
				}
				script := &scriptedCompleter{steps: []step{
					func(context.Context, []ai.Message) (*ai.Response, error) { return call("before"), nil },
					func(context.Context, []ai.Message) (*ai.Response, error) {
						parent.SetApprovalPolicy(&approval.Policy{ProfileReadError: readError})
						return call("after"), nil
					},
					func(context.Context, []ai.Message) (*ai.Response, error) {
						parent.SetApprovalPolicy(normal)
						return call("recovered"), nil
					},
					func(context.Context, []ai.Message) (*ai.Response, error) { return textResponse("done"), nil },
				}}
				parent, _ = newTestAgent(t, script, func(c *Config) {
					c.OneModel = true
					c.ApprovalPolicy = normal
					if stage {
						c.Stage = &fakeStageDoor{}
						c.Unattended = true
					}
				})
				workspace := t.TempDir()
				child := newProfileTestChild(t, parent, kind, workspace)
				events := collect(t, mustSubmit(t, child, "attempt three calls"))
				succeeded := make(map[string]bool)
				var refusal Event
				for _, event := range events {
					if event.Kind == EventToolEnd {
						succeeded[event.CallID] = true
					}
					if event.Kind == EventToolFailed && event.CallID == "after" {
						refusal = event
					}
				}
				if !succeeded["before"] || !succeeded["recovered"] || succeeded["after"] || !strings.Contains(refusal.Output, readError) {
					t.Fatalf("child must allow, refuse, then allow on its own rules: succeeded=%v refusal=%q", succeeded, refusal.Output)
				}
				if _, err := os.Stat(filepath.Join(workspace, "after.txt")); !os.IsNotExist(err) {
					t.Fatalf("refused write created file: %v", err)
				}
			})
		}
	}
}
