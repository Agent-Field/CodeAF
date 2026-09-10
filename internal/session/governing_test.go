package session

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/workspace"
)

// Constructor coverage uses real child assembly, while the model remains inert.
func TestGoverningWorkersAndCheckersInheritReadsOnly(t *testing.T) {
	parent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	store, err := standing.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	parent.config.Standing = &Standing{Store: store}
	parent.config.Organization = &Organization{Path: filepath.Join(t.TempDir(), "collections.db")}
	graph := parent.graph()
	graph.run = func(*TaskNode) {}
	id := graph.reserve()
	graph.admit(id, taskSpec{title: "read", brief: "read the result", acceptance: "source named"})
	worker, err := parent.newTaskAgent(context.Background(), t.TempDir(), graph.node(id), "")
	if err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	checker, err := parent.newAuditAgent(t.TempDir(), graph.node(id), plainDoor(auditReadCommands), "")
	if err != nil {
		t.Fatal(err)
	}
	defer checker.Close()
	seed, system := worker.forkSeed()
	hand, err := worker.newHandAgent(forkPart{Role: "inspect", Scope: []string{}}, seed, system, &handLeash{limit: forkRounds})
	if err != nil {
		t.Fatal(err)
	}
	defer hand.Close()
	for _, child := range []*Agent{worker, checker, hand} {
		if child.config.Standing != nil || child.config.Governing == nil || child.config.Governing.Reader != store {
			t.Fatal("child gained mutation door or lost governing reader")
		}
		ref := child.organizationSource()
		if ref.Kind != workspace.TaskKind || ref.ID != fmt.Sprint(id) || ref.SessionID != parent.sessionID() {
			t.Fatalf("generated execution leaked into owning scope: %+v", ref)
		}
		if !historyCarriesTool(child, "shared_context") || !historyCarriesTool(child, "collections") {
			t.Fatal("child lost organization inspection")
		}
		_, failed, err := child.sharedContextTool(context.Background(), json.RawMessage(`{"action":"create","title":"no","text":"no","targets":[]}`))
		if err != nil || !failed {
			t.Fatal("child could mutate shared context")
		}
	}
}

func TestGoverningScheduledRunUsesDutyNotSetupFolder(t *testing.T) {
	setup := workspace.Ref{Kind: workspace.ConversationKind, ID: "setup"}
	parent := Config{Organization: &Organization{Path: filepath.Join(t.TempDir(), "collections.db")}, Governing: &Governing{Owners: []workspace.Ref{setup}}}
	item := standing.Item{ID: "duty", Workspace: t.TempDir(), Origin: standing.Origin{SessionID: "setup"}}
	cfg, err := standingRunConfig(parent, item, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := &Agent{config: cfg}
	targets := run.organizationTargets()
	if len(targets) != 1 || targets[0] != (workspace.Ref{Kind: workspace.StandingKind, ID: "duty"}) {
		t.Fatalf("setup folder leaked into duty: %+v", targets)
	}
	inherited := run.governingLocked()
	inherited.Owners = append(inherited.Owners, workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: "run"})
	if len(parent.Governing.Owners) != 1 || len(cfg.Governing.Owners) != 1 {
		t.Fatal("child mutated parent owner ancestry")
	}
	child := &Agent{config: Config{Organization: cfg.Organization, Governing: inherited, OrganizationRef: workspace.Ref{Kind: workspace.TaskKind, ID: "1", SessionID: "run"}}}
	foundDuty := false
	for _, ref := range child.organizationTargets() {
		if ref.Kind == workspace.StandingKind && ref.ID == "duty" {
			foundDuty = true
		}
		if ref == setup {
			t.Fatal("child borrowed setup scope")
		}
	}
	if !foundDuty {
		t.Fatal("delegated duty lost originating work scope")
	}
}

func TestGoverningNeverClipsMandatoryHolds(t *testing.T) {
	var items []standing.Item
	for i := 0; i < standingWorldMost+3; i++ {
		items = append(items, standing.Item{ID: fmt.Sprint(i), Words: fmt.Sprintf("required-condition-%d", i), When: standing.When{Kind: standing.WhenHold}})
	}
	block := renderStandingWorld(items, "")
	for _, item := range items {
		if !strings.Contains(block, item.Words) {
			t.Fatalf("mandatory direction omitted: %s", item.Words)
		}
	}
	if strings.Contains(block, "more") {
		t.Fatal("full mandatory set described as truncated")
	}
}
