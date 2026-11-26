package connections_test

import (
	"context"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

func TestGoogleDraftUsesRunApprovalAndReceipt(t *testing.T) {
	ctx := context.Background()
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	defer place.Close()
	store, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	const manifest = `{"id":"brief","name":"Brief","kit":0,"version":1,
		"records":{"message":{"title":"subject","fields":{"subject":{"type":"text"},
		"recipient":{"type":"text"},"draft":{"type":"longtext"}}}},
		"capabilities":{"records":{"message":"rw"}}}`
	if err := store.PublishApp(data.AppPublication{App: "brief", Version: 1,
		Metadata: []byte(`{"digest":"draft-fixture"}`), Definition: []byte(manifest)}, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.BindCommonType(data.CommonMessage, "message"); err != nil {
		t.Fatal(err)
	}
	svc := connections.Service{Profile: t.TempDir(), Store: func() (*data.Store, error) { return store, nil }}
	if err := svc.Register(connections.Config{
		ID: "workspace", App: "brief", Kind: connections.Google, CredentialMode: connections.CredentialPerson, GoogleFixture: true,
		Reads: map[string]connections.Read{"messages": {Type: "message", Table: "gmail", Key: "id",
			Fields: map[string]string{"subject": "subject", "recipient": "to", "draft": "body"}}},
		Writes: map[string]connections.Write{"create-draft": {Read: "messages", Fields: []string{"subject", "recipient", "draft"}}},
	}, ""); err != nil {
		t.Fatal(err)
	}
	const tool = "connections.workspace.create-draft"
	tools := runs.ConnectedTools{Sources: svc, Apps: func(string) (runs.App, error) {
		return schema.App{Capabilities: schema.Capabilities{Tools: []string{tool}}}, nil
	}}
	const flow = `run: draft
steps:
  - id: draft
    tool: connections.workspace.create-draft
    gate: {approval: send}
    args:
      set: {subject: Follow-up, recipient: ari@example.com, draft: Thanks for the agenda.}
`
	cfg := runs.Config{Root: t.TempDir(), Connector: tools, CheckTool: tools.Check, Load: func(string, string) (runs.Definition, error) {
		return runs.Parse([]byte(flow))
	}}
	engine, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	run, err := engine.Start("brief", "draft", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	waiting, err := engine.Get(run.ID)
	if err != nil || waiting.State != runs.YourCall || waiting.Approval == nil || len(waiting.Effects) != 0 {
		t.Fatalf("before approval %+v %v", waiting, err)
	}
	if err := engine.Answer(run.ID, waiting.Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := engine.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := engine.Get(run.ID)
	if err != nil || done.State != runs.Done || len(done.Effects) != 1 || done.Effects[0].Outcome != "committed" ||
		done.Effects[0].Connection != "workspace" || done.Effects[0].Key == "" {
		t.Fatalf("receipt %+v %v", done, err)
	}
}
