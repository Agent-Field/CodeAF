package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/caps"
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/crewlink"
	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/lifecycle"
	"github.com/Agent-Field/codeaf/internal/apps/prebuilt"
	"github.com/Agent-Field/codeaf/internal/apps/realtime"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/sample"
	"github.com/Agent-Field/codeaf/internal/apps/settings"
	"github.com/Agent-Field/codeaf/internal/apps/store"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func crewsTestServices(t *testing.T) *svc.Services {
	t.Helper()
	cfg := svc.Config{Root: t.TempDir()}
	all := &svc.Services{}
	all.Store = store.New(cfg, all)
	all.Caps = caps.New(cfg, all)
	all.Records = records.New(cfg, all)
	all.Sample = sample.New(cfg, all)
	if err := all.Store.(*store.Service).SeedGoldenCRM(context.Background()); err != nil {
		t.Fatal(err)
	}
	all.Hub = realtime.New(cfg, all)
	preferences := settings.New(cfg, all)
	all.Crews = svc.CrewsServices{
		Lifecycle: lifecycle.New(cfg, all), Settings: preferences,
		Links: crewlink.New(cfg, all), Agent: crewagent.New(cfg, all),
		Prebuilt: prebuilt.New(cfg, all), Custom: custom.New(cfg, all),
		TriggerGate: preferences, BudgetCap: preferences,
	}
	all.Caps = CrewsCaps(all.Caps, all)
	engine := runs.New(cfg, all)
	all.Runs = engine
	t.Cleanup(engine.Close)
	return all
}

func linksBuilt(method string) bool {
	switch method {
	case bridge.MethodLinksAdd, bridge.MethodLinksRemove, bridge.MethodLinksForRecord, bridge.MethodLinksForCrew:
		return true
	default:
		return false
	}
}

func TestCrewsMethodsThroughWebSocket(t *testing.T) {
	tests := []struct {
		name     string
		global   bool
		params   any
		notBuilt bool
	}{
		{bridge.MethodAppsArchive, false, bridge.Empty{}, false},
		{bridge.MethodAppsRestore, false, bridge.Empty{}, false},
		{bridge.MethodAppsDelete, false, bridge.DeleteAppParams{Confirm: "CRM"}, false},
		{bridge.MethodAppsCopy, false, bridge.CopyAppParams{Name: "My CRM"}, false},
		{bridge.MethodAppsLifecycle, true, bridge.Empty{}, false},
		{bridge.MethodTriggersList, false, bridge.Empty{}, false},
		{bridge.MethodTriggersPause, false, bridge.PauseTriggerParams{ID: "proposal-sent", Paused: true}, false},
		{bridge.MethodBudgetGet, false, bridge.Empty{}, false},
		{bridge.MethodBudgetSet, false, bridge.SetBudgetParams{Month: 20}, false},
		{bridge.MethodLinksAdd, false, bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "dl_hooli"}, false},
		{bridge.MethodLinksRemove, false, bridge.RemoveLinkParams{Crew: "pricing", Type: "deal", ID: "dl_hooli"}, false},
		{bridge.MethodLinksForRecord, false, bridge.LinksForRecordParams{Type: "deal", ID: "dl_hooli"}, false},
		{bridge.MethodLinksForCrew, true, bridge.LinksForCrewParams{Crew: "pricing"}, false},
		{bridge.MethodCrewAgentBadge, false, bridge.Empty{}, false},
		{bridge.MethodCrewAgentCandidates, true, bridge.Empty{}, false},
		{bridge.MethodAppsPrebuilt, true, bridge.Empty{}, false},
		{bridge.MethodCustomBundle, false, bridge.CustomBundleParams{File: "custom/chart.tsx"}, false},
	}
	if len(tests) != len(crewsMethods) {
		t.Fatalf("coverage=%d methods=%d", len(tests), len(crewsMethods))
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			all := crewsTestServices(t)
			if tt.name == bridge.MethodLinksRemove {
				if _, err := all.Crews.Links.Add(context.Background(), ownerScope("crm"), bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "dl_hooli"}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, url := testServer(t, all)
			socket := dialSocket(t, ctx, url)
			entry, exists := methods[tt.name]
			if !exists || entry.global != tt.global {
				t.Fatalf("registration=%+v exists=%v", entry, exists)
			}
			app := "crm"
			if tt.global {
				app = ""
			}
			// Validate malformed calls before a successful delete can hide this fixture.
			id := socket.send(bridge.TypeCall, app, tt.name, "", []string{"invalid"})
			reply := socket.read()
			if reply.ID != id || reply.Error == nil || reply.Error.Code != bridge.CodeInvalid {
				t.Fatalf("invalid params=%+v", reply)
			}
			if !tt.global {
				id = socket.send(bridge.TypeCall, "", tt.name, "", tt.params)
				if reply := socket.read(); reply.ID != id || reply.Error == nil || reply.Error.Code != bridge.CodeInvalid {
					t.Fatalf("missing app=%+v", reply)
				}
			}
			id = socket.send(bridge.TypeCall, app, tt.name, "", tt.params)
			reply = socket.read()
			if reply.ID != id {
				t.Fatalf("reply=%+v", reply)
			}
			if (reply.Type != bridge.TypeResult && reply.Type != bridge.TypeError) || (reply.Type == bridge.TypeError) != (reply.Error != nil) {
				t.Fatalf("reply=%+v", reply)
			}
			notBuilt := reply.Error != nil && reply.Error.Code == bridge.CodeNotBuilt
			if notBuilt != tt.notBuilt {
				t.Fatalf("reply=%+v want not_built=%v", reply, tt.notBuilt)
			}
			switch {
			case tt.name == bridge.MethodCrewAgentBadge || tt.name == bridge.MethodCrewAgentCandidates:
				checkCrewAgentResult(t, tt.name, reply)
			case tt.name == bridge.MethodAppsPrebuilt:
				if !factoryResult(t, id, reply) {
					t.Fatalf("reply=%+v", reply)
				}
			case linksBuilt(tt.name):
				if reply.Type != bridge.TypeResult || reply.Error != nil {
					t.Fatalf("reply=%+v", reply)
				}
			case tt.name == bridge.MethodCustomBundle:
				if reply.Error == nil || reply.Error.Code != bridge.CodeNotFound {
					t.Fatalf("reply=%+v", reply)
				}
			}
		})
	}
}

func checkCrewAgentResult(t *testing.T, method string, reply bridge.Envelope) {
	t.Helper()
	if reply.Type != bridge.TypeResult || reply.Error != nil {
		t.Fatalf("reply=%+v", reply)
	}
	var badge bridge.Badge
	if method == bridge.MethodCrewAgentCandidates {
		var got bridge.CrewAgentCandidatesResult
		if err := json.Unmarshal(reply.Result, &got); err != nil {
			t.Fatal(err)
		}
		if len(got.Agents) != 1 {
			t.Fatalf("candidates=%+v", got)
		}
		badge = got.Agents[0]
	} else if err := json.Unmarshal(reply.Result, &badge); err != nil {
		t.Fatal(err)
	}
	want := bridge.Badge{
		App: "crm", AppName: "CRM", AgentName: "CRM agent", Role: string(svc.RoleEditor),
		Can: "can read companies, contacts, deals, create and change companies, contacts, deals, draft emails, read emails, send emails",
	}
	if badge != want {
		t.Fatalf("badge=%+v want=%+v", badge, want)
	}
}

func factoryResult(t *testing.T, id string, reply bridge.Envelope) bool {
	t.Helper()
	var got bridge.PrebuiltResult
	if reply.ID != id || reply.Type != bridge.TypeResult || reply.Error != nil {
		return false
	}
	if err := json.Unmarshal(reply.Result, &got); err != nil || len(got.Apps) != 1 {
		return false
	}
	want := bridge.PrebuiltApp{ID: "factory", Name: "Factory", Kind: "factory", Version: 3, Space: "acme"}
	return got.Apps[0] == want
}

func TestCrewsCapabilityChecks(t *testing.T) {
	all := crewsTestServices(t)
	for _, tt := range []struct {
		method string
		role   svc.Role
		agent  bool
		params string
		denied bool
	}{
		{bridge.MethodAppsArchive, svc.RoleEditor, false, `{}`, false},
		{bridge.MethodAppsArchive, svc.RoleUser, false, `{}`, true},
		{bridge.MethodTriggersPause, svc.RoleViewer, false, `{}`, true},
		{bridge.MethodAppsCopy, svc.RoleViewer, false, `{}`, false},
		{bridge.MethodBudgetGet, svc.RoleViewer, false, `{}`, false},
		{bridge.MethodBudgetSet, svc.RoleUser, false, `{"month":20}`, true},
		{bridge.MethodBudgetSet, svc.RoleEditor, false, `{"month":20}`, false},
		{bridge.MethodLinksAdd, svc.RoleViewer, false, `{"type":"deal","id":"row"}`, false},
		{bridge.MethodLinksAdd, svc.RoleEditor, false, `{"type":"secret","id":"row"}`, true},
		{bridge.MethodAppsCopy, svc.RoleEditor, true, `{}`, true},
	} {
		t.Run(tt.method+"/"+string(tt.role)+"/"+tt.params, func(t *testing.T) {
			scope := svc.Scope{App: "crm", Caller: svc.Caller{Member: "person", Role: tt.role, Agent: tt.agent}}
			err := all.Caps.Allow(context.Background(), scope, tt.method, json.RawMessage(tt.params))
			if (err != nil) != tt.denied {
				t.Fatalf("error=%v denied=%v", err, tt.denied)
			}
			if err != nil {
				typed, ok := err.(*bridge.Error)
				if !ok || typed.Code != bridge.CodeDenied {
					t.Fatalf("error=%v", err)
				}
			}
		})
	}
}

func TestCrewsUseExistingCapabilityDoor(t *testing.T) {
	for name := range crewsMethods {
		t.Run(name, func(t *testing.T) {
			base := &rejectingCaps{}
			all := &svc.Services{}
			checker := CrewsCaps(base, all)
			// No store is installed: a denied call must stop before reaching services.
			err := checker.Allow(context.Background(), ownerScope("crm"), name, json.RawMessage(`{}`))
			if len(base.seen) != 1 || err == nil {
				t.Fatalf("calls=%v error=%v", base.seen, err)
			}
		})
	}
}

func TestRestoreReachesLifecycleWhenStoreHidesDeletedApp(t *testing.T) {
	all := crewsTestServices(t)
	if err := all.Crews.Lifecycle.Delete(context.Background(), ownerScope("crm"), bridge.DeleteAppParams{Confirm: "CRM"}); err != nil {
		t.Fatal(err)
	}
	if _, err := all.Store.Get(context.Background(), "crm"); err == nil {
		t.Fatal("deleted app remains visible")
	} else if typed, ok := err.(*bridge.Error); !ok || typed.Code != bridge.CodeNotFound {
		t.Fatalf("deleted app error=%v", err)
	}
	ctx, url := testServer(t, all)
	socket := dialSocket(t, ctx, url)
	id := socket.send(bridge.TypeCall, "crm", bridge.MethodAppsRestore, "", bridge.Empty{})
	reply := socket.read()
	if reply.ID != id || reply.Type != bridge.TypeResult || reply.Error != nil {
		t.Fatalf("restore=%+v", reply)
	}
	var restored bridge.AppSummary
	if err := json.Unmarshal(reply.Result, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.ID != "crm" || restored.State != bridge.StateKept {
		t.Fatalf("restored=%+v", restored)
	}
	if _, err := all.Store.Get(context.Background(), "crm"); err != nil {
		t.Fatalf("restored app error=%v", err)
	}
}

func TestCrewsLinkTopicUsesGuardedWebSocket(t *testing.T) {
	all := crewsTestServices(t)
	ctx, url := testServer(t, all)
	socket := dialSocket(t, ctx, url)
	topic := bridge.TopicLinks("crm")
	id := socket.send(bridge.TypeSub, "crm", "", topic, nil)
	if reply := socket.read(); reply.ID != id || reply.Type != bridge.TypeResult {
		t.Fatalf("subscribe=%+v", reply)
	}
	link := bridge.CrewLink{Crew: "pricing", App: "crm", Type: "deal", ID: "row", State: bridge.CrewLinkOpen, Access: bridge.CrewLinkAccessOK}
	all.Hub.Publish(topic, link)
	reply := socket.read()
	var got bridge.CrewLink
	if err := json.Unmarshal(reply.Event, &got); err != nil {
		t.Fatal(err)
	}
	if reply.Type != bridge.TypeEvent || reply.Topic != topic || reply.Seq != 1 || got != link {
		t.Fatalf("event=%+v link=%+v", reply, got)
	}
	id = socket.send(bridge.TypeUnsub, "crm", "", topic, nil)
	if reply := socket.read(); reply.ID != id || reply.Type != bridge.TypeResult {
		t.Fatalf("unsubscribe=%+v", reply)
	}
}
