package bridge

import (
	"encoding/json"
	"testing"
)

func TestCrewsWireJSON(t *testing.T) {
	for _, tt := range []struct {
		name  string
		value any
		want  string
	}{
		{"delete", DeleteAppParams{Confirm: "CRM"}, `{"confirm":"CRM"}`},
		{"default copy", CopyAppParams{}, `{}`},
		{"pause", PauseTriggerParams{ID: "quiet", Paused: false}, `{"id":"quiet","paused":false}`},
		{"budget", BudgetInfo{Month: 20, Spent: 6.4, Currency: "USD", ResetsAt: "2026-11-01", AtCap: "ask", Presets: []float64{5, 10, 20, 50}}, `{"month":20,"spent":6.4,"currency":"USD","resets_at":"2026-11-01","at_cap":"ask","paused":false,"can_edit":false,"presets":[5,10,20,50]}`},
		{"trigger", TriggerInfo{ID: "quiet", On: "record.changed", Run: "follow-up", Source: TriggerSourceRecord, Label: "Draft a follow-up"}, `{"id":"quiet","on":"record.changed","run":"follow-up","source":"record","label":"Draft a follow-up","ai":false,"paused":false}`},
		{"link", CrewLink{Crew: "pricing", CrewName: "Pricing", App: "crm", AppName: "CRM", Type: "deal", ID: "row", Label: "Renewal", By: "mona", LinkedAt: "now", State: CrewLinkFinished, Access: CrewLinkNoAccess}, `{"crew":"pricing","crew_name":"Pricing","app":"crm","app_name":"CRM","type":"deal","id":"row","label":"Renewal","by":"mona","linked_at":"now","state":"finished","access":"no_access"}`},
		{"badge", Badge{App: "crm", AppName: "CRM", AgentName: "CRM agent", Can: "can read deals", Role: "viewer"}, `{"app":"crm","app_name":"CRM","agent_name":"CRM agent","can":"can read deals","role":"viewer"}`},
		{"prebuilt", PrebuiltResult{Apps: []PrebuiltApp{{ID: "factory", Name: "Factory", Kind: "factory", Version: 1, Space: "personal"}}}, `{"apps":[{"id":"factory","name":"Factory","kind":"factory","version":1,"space":"personal"}]}`},
		{"custom", CustomBundleResult{JS: "export {}", Hash: "hash"}, `{"js":"export {}","hash":"hash"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.value)
			if err != nil || string(got) != tt.want {
				t.Fatalf("JSON=%s error=%v want=%s", got, err, tt.want)
			}
		})
	}
	if got := TopicLinks("crm"); got != "app/crm/links" {
		t.Fatalf("topic=%q", got)
	}
}
