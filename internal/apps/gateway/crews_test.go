package gateway

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/crewagent"
	"github.com/Agent-Field/codeaf/internal/apps/crewlink"
	"github.com/Agent-Field/codeaf/internal/apps/custom"
	"github.com/Agent-Field/codeaf/internal/apps/lifecycle"
	"github.com/Agent-Field/codeaf/internal/apps/prebuilt"
	"github.com/Agent-Field/codeaf/internal/apps/settings"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func crewsTestServices(t *testing.T) *svc.Services {
	t.Helper()
	all := testServices(t)
	cfg := svc.Config{Root: t.TempDir()}
	preferences := settings.New(cfg, all)
	all.Crews = svc.CrewsServices{
		Lifecycle: lifecycle.New(cfg, all), Settings: preferences,
		Links: crewlink.New(cfg, all), Agent: crewagent.New(cfg, all),
		Prebuilt: prebuilt.New(cfg, all), Custom: custom.New(cfg, all),
		TriggerGate: preferences, BudgetCap: preferences,
	}
	all.Caps = CrewsCaps(all.Caps, all)
	return all
}

func TestCrewsMethodsDispatchThroughWebSocket(t *testing.T) {
	tests := []struct {
		name   string
		global bool
		params any
	}{
		{bridge.MethodAppsArchive, false, bridge.Empty{}},
		{bridge.MethodAppsRestore, false, bridge.Empty{}},
		{bridge.MethodAppsDelete, false, bridge.DeleteAppParams{Confirm: "CRM"}},
		{bridge.MethodAppsCopy, false, bridge.CopyAppParams{Name: "My CRM"}},
		{bridge.MethodAppsLifecycle, true, bridge.Empty{}},
		{bridge.MethodTriggersList, false, bridge.Empty{}},
		{bridge.MethodTriggersPause, false, bridge.PauseTriggerParams{ID: "follow-up", Paused: true}},
		{bridge.MethodBudgetGet, false, bridge.Empty{}},
		{bridge.MethodBudgetSet, false, bridge.SetBudgetParams{Month: 20}},
		{bridge.MethodLinksAdd, false, bridge.AddLinkParams{Crew: "pricing", Type: "deal", ID: "dl_hooli"}},
		{bridge.MethodLinksRemove, false, bridge.RemoveLinkParams{Crew: "pricing", Type: "deal", ID: "dl_hooli"}},
		{bridge.MethodLinksForRecord, false, bridge.LinksForRecordParams{Type: "deal", ID: "dl_hooli"}},
		{bridge.MethodLinksForCrew, true, bridge.LinksForCrewParams{Crew: "pricing"}},
		{bridge.MethodCrewAgentBadge, false, bridge.Empty{}},
		{bridge.MethodCrewAgentCandidates, true, bridge.Empty{}},
		{bridge.MethodAppsPrebuilt, true, bridge.Empty{}},
		{bridge.MethodCustomBundle, false, bridge.CustomBundleParams{File: "custom/chart.tsx"}},
	}
	if len(tests) != len(crewsMethods) {
		t.Fatalf("coverage=%d methods=%d", len(tests), len(crewsMethods))
	}
	ctx, url := testServer(t, crewsTestServices(t))
	socket := dialSocket(t, ctx, url)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry, exists := methods[tt.name]
			if !exists || entry.global != tt.global {
				t.Fatalf("registration=%+v exists=%v", entry, exists)
			}
			app := "crm"
			if tt.global {
				app = ""
			}
			id := socket.send(bridge.TypeCall, app, tt.name, "", tt.params)
			reply := socket.read()
			code := bridge.CodeNotBuilt
			if tt.name == bridge.MethodCustomBundle {
				code = bridge.CodeNotFound
			}
			if reply.ID != id || reply.Type != bridge.TypeError || reply.Error == nil || reply.Error.Code != code {
				t.Fatalf("reply=%+v", reply)
			}
			id = socket.send(bridge.TypeCall, app, tt.name, "", []string{"invalid"})
			reply = socket.read()
			if reply.ID != id || reply.Error == nil || reply.Error.Code != bridge.CodeInvalid {
				t.Fatalf("invalid params=%+v", reply)
			}
			if !tt.global {
				id = socket.send(bridge.TypeCall, "", tt.name, "", tt.params)
				if reply := socket.read(); reply.ID != id || reply.Error == nil || reply.Error.Code != bridge.CodeInvalid {
					t.Fatalf("missing app=%+v", reply)
				}
			}
		})
	}
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
	ctx, url := testServer(t, all)
	socket := dialSocket(t, ctx, url)
	socket.send(bridge.TypeCall, "deleted", bridge.MethodAppsRestore, "", bridge.Empty{})
	if reply := socket.read(); reply.Error == nil || reply.Error.Code != bridge.CodeNotBuilt {
		t.Fatalf("restore=%+v", reply)
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
