package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/route"
)

// triggerPlane is a control plane with a catalog of three sources, two
// triggers (one aimed at another node), and one node with one reasoner. It
// keeps every trigger POST's body.
type triggerPlane struct {
	*httptest.Server
	mu    sync.Mutex
	posts []agentfield.NewTrigger
}

func newTriggerPlane(t *testing.T) *triggerPlane {
	p := &triggerPlane{}
	p.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/sources":
			_, _ = io.WriteString(w, `{"sources":[
				{"name":"linear","kind":"http","secret_required":true,"config_schema":{"type":"object","properties":{"tolerance_seconds":{"type":"integer"}}}},
				{"name":"generic_bearer","kind":"http","secret_required":true,"config_schema":{"type":"object","properties":{"idempotency_header":{"type":"string"},"api_token":{"type":"string"}}}},
				{"name":"cron","kind":"loop","secret_required":false,"config_schema":{"type":"object","properties":{"expression":{"type":"string","title":"When"}}}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/triggers":
			_, _ = io.WriteString(w, `{"triggers":[
				{"id":"t-lin","source_name":"linear","target_node_id":"codeaf-factory","target_reasoner":"intake","event_types":["Issue.create"],"enabled":true,"secret_env_var":"LINEAR_SECRET","created_at":"2026-11-01T10:00:00Z"},
				{"id":"t-cron","source_name":"cron","target_node_id":"codeaf-factory","target_reasoner":"intake","enabled":true},
				{"id":"t-other","source_name":"linear","target_node_id":"someone-else","target_reasoner":"intake","enabled":true}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/triggers":
			var nt agentfield.NewTrigger
			_ = json.NewDecoder(r.Body).Decode(&nt)
			p.mu.Lock()
			p.posts = append(p.posts, nt)
			p.mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(agentfield.Trigger{ID: "t-new", SourceName: nt.SourceName, TargetNodeID: nt.TargetNodeID, TargetReasoner: nt.TargetReasoner, EventTypes: nt.EventTypes, SecretEnvVar: nt.SecretEnvVar, Enabled: true})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/nodes":
			_, _ = io.WriteString(w, `{"nodes":[{"id":"support-bot","health_status":"active","reasoners":[{"id":"draft_reply"}]}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"not found"}`)
		}
	}))
	t.Cleanup(p.Close)
	return p
}

// agentFieldKept is this machine's factory with settings for plane kept, as
// a connect leaves it, and the node not started: the calls under test only
// talk to the control plane.
func agentFieldKept(t *testing.T, plane string) (*localSetup, string) {
	t.Helper()
	s := newLocalSetup(t)
	s.start()
	s.backend.local.mu.Lock()
	host := s.backend.local.host
	s.backend.local.mu.Unlock()
	if host == nil {
		t.Fatal("the factory on this machine did not start")
	}
	if plane != "" {
		settings, err := agentfield.Clean(agentfield.Settings{URL: plane})
		if err != nil {
			t.Fatal(err)
		}
		if err := host.agentFieldNode().Conn.Keep(settings, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	return s, s.backend.localFactoryID()
}

// THE TRIGGER PAGE READS ONE CALL: this node's triggers only, each with the
// source its jobs carry and where the outside system posts, beside the
// catalog in words with each source's form fields.
func TestTheTriggerListCarriesTheCatalogAndOnlyThisNodesTriggers(t *testing.T) {
	plane := newTriggerPlane(t)
	s, id := agentFieldKept(t, plane.URL)
	out, err := s.backend.FactoryAgentFieldTriggers(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Triggers) != 2 {
		t.Fatalf("listed %+v, want this node's two", out.Triggers)
	}
	lin, cron := out.Triggers[0], out.Triggers[1]
	if lin.EntrySource != "agentfield:linear" || lin.Label != "Linear" || lin.WebhookURL != plane.URL+"/sources/t-lin" || lin.CreatedAt == nil {
		t.Fatalf("the linear trigger %+v", lin)
	}
	if cron.WebhookURL != "" || cron.IngestURL == "" || cron.Label != "Schedule" {
		t.Fatalf("a cron trigger has no webhook to post to: %+v", cron)
	}
	if len(out.Sources) != 3 || out.Sources[1].Label != "Webhook with a bearer token" {
		t.Fatalf("sources %+v", out.Sources)
	}
	fields := out.Sources[1].ConfigFields
	if len(fields) != 2 || fields[0].Name != "api_token" || !fields[0].Secret || fields[1].Secret || fields[1].Label != "Idempotency header" {
		t.Fatalf("generic_bearer's fields %+v", fields)
	}
	if f := out.Sources[2].ConfigFields; len(f) != 1 || f[0].Label != "When" {
		t.Fatalf("a schema's title is the field's label: %+v", f)
	}
}

// AN ADD IS AIMED AT THIS NODE'S INTAKE, says where the outside system
// posts, and a source that is not in the catalog or that needs a secret it
// was not given is refused in words before anything is made.
func TestAddingATriggerAimsAtTheIntakeAndRefusesInWords(t *testing.T) {
	plane := newTriggerPlane(t)
	s, id := agentFieldKept(t, plane.URL)
	ctx := context.Background()
	for _, tc := range []struct {
		p   appwire.FactoryAgentFieldAddTriggerParams
		say string
	}{
		{appwire.FactoryAgentFieldAddTriggerParams{Factory: id, Source: "zendesk"}, `no source named "zendesk"`},
		{appwire.FactoryAgentFieldAddTriggerParams{Factory: id, Source: "linear", Config: json.RawMessage(`{"tolerance_seconds":"private-value"}`)}, "Linear needs a signing secret"},
	} {
		_, err := s.backend.FactoryAgentFieldAddTrigger(ctx, tc.p)
		if err == nil || !strings.Contains(err.Error(), tc.say) || strings.Contains(err.Error(), "private-value") {
			t.Errorf("%s: %v, want one saying %q", tc.p.Source, err, tc.say)
		}
	}
	if len(plane.posts) != 0 {
		t.Fatalf("a refused add reached the control plane: %+v", plane.posts)
	}
	out, err := s.backend.FactoryAgentFieldAddTrigger(ctx, appwire.FactoryAgentFieldAddTriggerParams{Factory: id, Source: "linear", EventTypes: []string{"Issue.create"}, SecretEnvVar: "LINEAR_SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	if len(plane.posts) != 1 || plane.posts[0].TargetNodeID != "codeaf-factory" || plane.posts[0].TargetReasoner != agentfield.ReasonerIntake {
		t.Fatalf("posted %+v", plane.posts)
	}
	if out.Trigger.ID != "t-new" || out.Trigger.EntrySource != "agentfield:linear" || out.Setup == nil || out.Setup.WebhookURL != plane.URL+"/sources/t-new" || out.Setup.SecretEnvVar != "LINEAR_SECRET" || out.Setup.Secret != "" {
		t.Fatalf("answered %+v setup %+v", out.Trigger, out.Setup)
	}
	cron, err := s.backend.FactoryAgentFieldAddTrigger(ctx, appwire.FactoryAgentFieldAddTriggerParams{Factory: id, Source: "cron", Config: json.RawMessage(`{"expression":"0 9 * * 1"}`)})
	if err != nil || cron.Setup != nil || cron.Trigger.WebhookURL != "" {
		t.Fatalf("a cron add has nothing to set up: %+v %v", cron, err)
	}
}

// NOT CONNECTED IS A REFUSAL IN WORDS, never a failure of the engine.
func TestTriggerCallsRefuseAnOrganizationNotConnected(t *testing.T) {
	s, id := agentFieldKept(t, "")
	_, err := s.backend.FactoryAgentFieldTriggers(context.Background(), id)
	var wireErr *appwire.Error
	if !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeConflict || !strings.Contains(err.Error(), "not connected to AgentField") {
		t.Fatalf("listed with no connection: %v", err)
	}
	if _, err := s.backend.FactoryAgentFieldAddTrigger(context.Background(), appwire.FactoryAgentFieldAddTriggerParams{Factory: id, Source: "linear"}); !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeConflict {
		t.Fatalf("added with no connection: %v", err)
	}
}

// A RULE THAT RUNS ON A REASONER THE CONTROL PLANE DOES NOT HAVE IS REFUSED
// WHEN IT IS SAVED, naming the rule and the reasoner; one it has is kept,
// and a table with no run_on asks nothing.
func TestSavingARuleRefusesAReasonerAgentFieldDoesNotHave(t *testing.T) {
	plane := newTriggerPlane(t)
	s, id := agentFieldKept(t, plane.URL)
	ctx := context.Background()
	rule := func(target string) []route.Rule {
		return []route.Rule{{Name: "support", When: route.Match{Sources: []string{"agentfield:*"}}, Then: route.Action{RunOn: target, Start: route.StartAuto}}}
	}
	if err := s.backend.routeReasonersKnown(ctx, id, rule("support-bot.draft_reply")); err != nil {
		t.Fatalf("a reasoner the control plane has: %v", err)
	}
	err := s.backend.routeReasonersKnown(ctx, id, rule("support-bot.nope"))
	var wireErr *appwire.Error
	if !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeBadParams || !strings.Contains(err.Error(), `routing rule "support": AgentField has no reasoner support-bot.nope`) {
		t.Fatalf("an unknown reasoner: %v", err)
	}
	if err := s.backend.routeReasonersKnown(ctx, "someone-elses-factory", rule("x.y")); err != nil {
		t.Fatalf("another factory's table is held to its spelling only: %v", err)
	}
	bare, bareID := agentFieldKept(t, "")
	if err := bare.backend.routeReasonersKnown(ctx, bareID, []route.Rule{{Name: "plain", Then: route.Action{Start: route.StartAuto}}}); err != nil {
		t.Fatalf("a table with no run_on: %v", err)
	}
	if err := bare.backend.routeReasonersKnown(ctx, bareID, rule("support-bot.draft_reply")); !errors.As(err, &wireErr) || wireErr.Code != appwire.CodeConflict || !strings.Contains(err.Error(), "not connected to AgentField") {
		t.Fatalf("run_on with no connection: %v", err)
	}
}
