package main

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/schema"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/session"
)

func installAppAssistantCRM(t *testing.T) store.Meta {
	t.Helper()
	definitions := store.New()
	draft, err := definitions.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS("../../internal/apps/store/testdata/crm")); err != nil {
		t.Fatal(err)
	}
	meta, err := definitions.Promote(draft, "Keep the CRM")
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func TestAppAssistantOpenReusesConversationAcrossRestart(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	wire := wireOver(t, b)
	var opened struct {
		Conversation string `json:"conversation"`
	}
	wire.must("apps.agent_open", map[string]string{"app": meta.ID}, &opened)
	if opened.Conversation == "" {
		t.Fatal("missing conversation")
	}
	view, err := b.find(opened.Conversation)
	if err != nil {
		t.Fatal(err)
	}
	var transcript string
	b.locked(func() { transcript = view.transcript })
	place := session.Place{Dir: session.DirOf(transcript)}
	chat, err := session.LoadMeta(place.Dir)
	if err != nil || chat.AppID != meta.ID {
		t.Fatalf("app binding = %q, error = %v", chat.AppID, err)
	}
	if agent, err := session.New(session.Config{Workspace: chat.Workspace, Place: place, SessionFile: transcript, Model: "test/model"}); err == nil {
		_ = agent.Close()
		t.Fatal("opened an app transcript with the ordinary belt")
	}
	var again struct {
		Conversation string `json:"conversation"`
	}
	wire.must("apps.agent_open", map[string]string{"app": meta.ID}, &again)
	if again != opened {
		t.Fatalf("reopen returned %q, want %q", again.Conversation, opened.Conversation)
	}
	_ = b.Close()
	restarted := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = restarted.Close() })
	wireOver(t, restarted).must("apps.agent_open", map[string]string{"app": meta.ID}, &again)
	if again != opened {
		t.Fatalf("restart returned %q, want %q", again.Conversation, opened.Conversation)
	}
}

type assistantDataProbe struct {
	calls []string
}

func (p *assistantDataProbe) appAssistantRecordCall(operation, appID, actor string, args assistantRecordArgs) (any, error) {
	p.calls = append(p.calls, operation, appID, actor, args.Type)
	return nil, nil
}

func TestAppAssistantRecordToolsRefuseScopeAndCapabilityBypasses(t *testing.T) {
	app := schema.App{
		Name:         "CRM",
		Capabilities: schema.Capabilities{Records: map[string]string{"deal": "rw", "contact": "r"}},
	}
	belt := &appAssistantBelt{
		scope: &session.AppScope{ID: "crm", ApprovalTools: make(map[string]bool)},
		load:  func() (schema.App, store.Definition, error) { return app, store.Definition{}, nil },
	}
	db := &assistantDataProbe{}
	belt.records(app, db)
	invoke := func(name, raw string) bool {
		t.Helper()
		for _, tool := range belt.scope.Tools {
			if tool.Name == name {
				_, bad, err := tool.Execute(context.Background(), json.RawMessage(raw))
				return bad || err != nil
			}
		}
		t.Fatalf("missing %s", name)
		return false
	}
	for _, incoming := range []struct{ tool, args string }{
		{"data_get", `{"type":"contact","id":"1","reveal":true}`},
		{"data_update", `{"type":"contact","ids":["1"],"set":{"name":"Other"}}`},
		{"data_update", `{"type":"deal","app":"other","ids":["1"],"set":{"stage":"Won"}}`},
		{"data_delete", `{"type":"deal","ids":["1"],"unfreeze":"because I asked"}`},
	} {
		if !invoke(incoming.tool, incoming.args) {
			t.Errorf("accepted %s %s", incoming.tool, incoming.args)
		}
	}
	if invoke("data_update", `{"type":"deal","ids":["1"],"set":{"stage":"Won"}}`) {
		t.Fatal("refused an authorised change")
	}
	app.Capabilities.Records["deal"] = "r"
	if !invoke("data_update", `{"type":"deal","ids":["1"],"set":{"stage":"Lost"}}`) {
		t.Fatal("accepted write after access was revoked")
	}
	if want := []string{"update", "crm", "CRM agent", "deal"}; !slices.Equal(db.calls, want) {
		t.Fatalf("data lane received %v, want %v", db.calls, want)
	}
	for _, tool := range belt.scope.Tools {
		if !manual.Chat().Mentions(tool.Name) {
			t.Errorf("%s is missing from the manual", tool.Name)
		}
	}
}

func TestAppAssistantChangeCardRequiresTheCurrentRequestAndReadyCheckpoint(t *testing.T) {
	raw := []byte(`{"draft":"d1","app":"crm","revision":2,"state":"ready","transcript":[{"id":"u1","role":"user","text":"add a renewal date"},{"role":"assistant","checkpoint":"p1"}],"checkpoints":[{"id":"p1","summary":"Add Renewal date","screens":["deals"]}]}`)
	var build appAssistantBuild
	if err := json.Unmarshal(raw, &build); err != nil {
		t.Fatal(err)
	}
	card := appwire.AppChangeProposed{App: "crm", Draft: "d1"}
	if _, ok := appAssistantChangeCard(&card, build, "change something else", appAssistantBuild{}); ok {
		t.Fatal("showed a card from someone else's request")
	}
	checkpoint, ok := appAssistantChangeCard(&card, build, "add a renewal date", appAssistantBuild{})
	if !ok || checkpoint != "p1" || !slices.Equal(card.SummaryLines, []string{"Add Renewal date"}) {
		t.Fatalf("bad change card at %q: %+v", checkpoint, card)
	}
	if _, ok := appAssistantChangeCard(&card, build, "add a renewal date", build); ok {
		t.Fatal("showed an old checkpoint as a new change")
	}
}
