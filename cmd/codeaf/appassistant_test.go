package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/session"
)

func installAppAssistantCRM(t *testing.T) store.Meta {
	t.Helper()
	return installAppAssistantFixture(t, "../../internal/apps/store/testdata/crm")
}

func installAppAssistantFixture(t *testing.T, fixture string) store.Meta {
	t.Helper()
	definitions := store.New()
	draft, err := definitions.NewDraft("")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(draft.Dir(), os.DirFS(fixture)); err != nil {
		t.Fatal(err)
	}
	meta, err := definitions.Promote(draft, "Keep the app")
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

func TestAppAssistantAskRunsThroughEngineWithExactRecordAddress(t *testing.T) {
	root := t.TempDir()
	t.Setenv(home.EnvVar, root)
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = b.Close() })
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	var prompts []runs.AgentCall
	files := runs.Files(home.Join("apps"))
	engine, err := runs.Open(runs.Config{Root: filepath.Join(root, "test-runs"), Now: func() time.Time { return now },
		Load:     func(app, id string) (runs.Definition, error) { return b.loadAppAgentRun(files, app, id) },
		Triggers: func() ([]runs.Trigger, error) { return b.appAgentTriggers(files) },
		Agent: func(_ context.Context, c runs.AgentCall) (runs.Reply, error) {
			prompts = append(prompts, c)
			return runs.Reply{Output: "answer"}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	b.appRuns.Store(engine)
	wire := wireOver(t, b)
	record := appwire.DataRecordRef{Type: "deal", ID: "record-1"}
	var started appwire.RunStarted
	wire.must("agents.ask", appwire.AppAgentAsk{App: meta.ID, Screen: "deals", Record: &record, Text: "How is it?"}, &started)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0].Prompt != "How is it?" || prompts[0].Args["record"] != "record-1" {
		t.Fatalf("agent calls = %#v", prompts)
	}
	var rows []struct{ ID, Output string }
	wire.must("agents.query", appwire.AppAgentQuery{App: meta.ID, Kind: "runs", Record: &record}, &rows)
	if len(rows) != 1 || rows[0].ID != started.ID || rows[0].Output != "answer" {
		t.Fatalf("projection = %#v", rows)
	}
	var duty appwire.RunStarted
	wire.must("agents.duty_add", appwire.AppAgentDuty{AppAgentAsk: appwire.AppAgentAsk{App: meta.ID, Screen: "deals", Text: "Check the pipeline"}, Cron: "1 * * * *"}, &duty)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 || prompts[1].Run != duty.ID || prompts[1].Prompt != "Check the pipeline" {
		t.Fatalf("standing duty calls = %#v", prompts)
	}
	var listed []struct{ ID, Cron string }
	wire.must("agents.query", appwire.AppAgentQuery{App: meta.ID, Kind: "duties"}, &listed)
	if len(listed) != 1 || listed[0].ID != duty.ID || listed[0].Cron != "1 * * * *" {
		t.Fatalf("listed duties = %#v", listed)
	}
	wire.must("agents.duty_remove", appwire.AppAgentDutyRef{App: meta.ID, ID: duty.ID}, nil)
	now = now.Add(time.Hour)
	if err := engine.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("removed duty ran again: %#v", prompts)
	}
}

func TestAppAssistantReadsDeclaredViewOnlyWithGrant(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantFixture(t, "../../internal/apps/schema/testdata/support-inbox")
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = b.Close() })
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Create(data.Scope{}, "ticket", []map[string]any{{"subject": "Northwind support"}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"app": meta.ID, "params": map[string]string{"view": "inbox/ticket-list"}})
	var refusal *appwire.Error
	if _, err := b.runAppCLICommand(t.Context(), "query", raw); !errors.As(err, &refusal) || refusal.Code != appwire.CodeForbidden {
		t.Fatalf("ungranted query = %v", err)
	}
	if err := b.GrantAppAgent(appwire.AppAgentGrant{App: meta.ID, Agent: meta.ID, Group: "data", ID: "inbox/ticket-list", Allowed: true}); err != nil {
		t.Fatal(err)
	}
	reply, err := b.runAppCLICommand(t.Context(), "query", raw)
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Rows []map[string]any }
	if err := json.Unmarshal(reply.(json.RawMessage), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0]["subject"] != "Northwind support" || result.Rows[0]["_rev"] != float64(1) {
		t.Fatalf("declared view = %+v", result)
	}
}

func TestAppAssistantCLIAndToolBothEnforceActionGrant(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = b.Close() })
	const action = "not-declared"
	ctx := context.Background()
	input := json.RawMessage(`{"action":"not-declared"}`)
	scope := appwire.AppContentScope{App: meta.ID, Version: meta.Version, Profile: b.localProfile(), Conversation: "test", Actor: "CRM agent"}
	_, err := appwire.RunScopedAppCommand(ctx, b, scope, "invoke", input, "call", nil)
	var refused *appwire.Error
	if !errors.As(err, &refused) || refused.Code != appwire.CodeForbidden {
		t.Fatalf("invoke error = %v", err)
	}
	raw, _ := json.Marshal(map[string]any{"app": meta.ID, "params": input})
	_, err = b.runAppCLICommand(ctx, "invoke", raw)
	if !errors.As(err, &refused) || refused.Code != appwire.CodeForbidden {
		t.Fatalf("CLI invoke error = %v", err)
	}
	if err := b.GrantAppAgent(appwire.AppAgentGrant{App: meta.ID, Agent: "different-agent", Group: "actions", ID: action, Allowed: true}); err == nil {
		t.Fatal("accepted a grant for a different agent")
	}
}

func TestAppAssistantWritesWithProvenanceAndLeavesPersonValues(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	meta := installAppAssistantCRM(t)
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = b.Close() })
	db, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	person := data.Scope{}
	companies, err := db.Create(person, "company", []map[string]any{{"name": "Northwind"}})
	if err != nil {
		t.Fatal(err)
	}
	company := companies.Rows[0]["id"]
	result, err := db.Create(person, "deal", []map[string]any{
		{"name": "Unpinned", "company": company},
		{"name": "Pinned", "company": company, "stage": "Proposal"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.GrantAppAgent(appwire.AppAgentGrant{App: meta.ID, Agent: meta.ID, Group: "actions", ID: "mark-won", Allowed: true}); err != nil {
		t.Fatal(err)
	}
	scope := appwire.AppContentScope{App: meta.ID, Version: meta.Version, Profile: b.localProfile(), Conversation: "test", Actor: "CRM agent"}
	for i, row := range result.Rows {
		id := row["id"].(string)
		raw, _ := json.Marshal(map[string]any{"action": "mark-won", "expected_rev": 1, "args": map[string]string{"record": id}})
		if _, err := appwire.RunScopedAppCommand(context.Background(), b, scope, "invoke", raw, "mark-won-ask", nil); err != nil {
			t.Fatal(err)
		}
		got, err := db.Get(person, "deal", id, false)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"Won", "Proposal"}[i]
		if got["stage"] != want {
			t.Fatalf("stage = %v, want %s", got["stage"], want)
		}
		history, err := db.History(person, "deal", id)
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 && (len(history) != 2 || history[0].ByKind != data.ByAI || history[0].Actor != "agent:"+meta.ID || history[0].Cause != "mark-won-ask") {
			t.Fatalf("history = %#v", history)
		}
		if i == 0 {
			var displayed appwire.DataHistoryResult
			wireOver(t, b).must("data.history", map[string]string{"type": "deal", "id": id}, &displayed)
			if len(displayed.History) != 2 || displayed.History[0].Actor != meta.Name+" agent" || displayed.History[0].ByKind != data.ByAI {
				t.Fatalf("displayed history = %#v", displayed)
			}
		}
		if i == 1 && len(history) != 1 {
			t.Fatalf("pinned record history = %#v", history)
		}
	}
}
