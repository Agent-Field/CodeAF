package bridge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestConnectWireRoundTrip(t *testing.T) {
	tests := []struct {
		value any
		wire  string
	}{
		{Connection{}, `{"id":"gmail-me","service":"gmail","account":"me@example.com","level":"personal","status":"connected","since":"2026-10-13","detail":"read and push in 6 repos"}`},
		{Grant{}, `{"member":"me","app":"crm","app_name":"CRM","space":"acme","service":"gmail","can":["send"],"at":"2026-10-13"}`},
		{AccountsResult{}, `{"connections":[],"grants":[]}`},
		{AllowParams{}, `{"service":"gmail","answer":"not_now"}`},
		{AllowParams{}, `{"service":"gmail","answer":"refresh","credential":{"kind":"refresh","token":"fixture-token","expires_at":"2026-10-20T00:00:00Z"}}`},
		{GrantCredential{}, `{"kind":"grant","token":"fixture-token"}`},
		{CredentialExportParams{}, `{"service":"gmail","space":"acme","app":"crm"}`},
		{CredentialExportResult{}, `{"credential":{"kind":"refresh","token":"fixture-token"}}`},
		{AllowSheetParams{}, `{"service":"gmail"}`},
		{AllowSheetParams{}, `{"service":"gmail","connected":false}`},
		{AllowSheet{}, `{"missing":false,"app":"crm","app_name":"CRM","member":"me","member_name":"Mona","service":"gmail","account":"me@example.com","title":"CRM may send email as Mona","can":["send email"],"cant":["read your other email"]}`},
		{RevokeParams{}, `{"app":"crm","space":"acme","service":"gmail"}`},
		{WorkspaceConnectionUse{}, `{"app":"crm","app_name":"CRM","for":"read invoices"}`},
		{WorkspaceConnection{}, `{"id":"stripe-acme","service":"stripe","account":"acme live","added_by":"mona","who":"chosen","apps":["crm"],"used_by":[{"app":"crm","app_name":"CRM","for":"read invoices"}]}`},
		{WorkspaceConnection{}, `{"auth_url":"http://localhost:8765/","id":"auth","service":"gmail","account":"","added_by":"","who":"","apps":[],"used_by":[]}`},
		{WorkspaceConnectionsResult{}, `{"connections":[],"can_edit":false}`},
		{AddWorkspaceConnectionParams{}, `{"service":"stripe","who":"chosen","apps":["crm"]}`},
		{SetWhoParams{}, `{"id":"stripe-acme","who":"chosen","apps":["crm"]}`},
		{WorkspaceConnectionParams{}, `{"id":"stripe-acme"}`},
		{RemovalImpactApp{}, `{"app":"crm","app_name":"CRM","for":"read invoices","runs_this_week":3}`},
		{RemovalImpact{}, `{"id":"stripe-acme","service":"stripe","apps":[{"app":"crm","app_name":"CRM","for":"read invoices","runs_this_week":3}]}`},
		{ConnectionAllowed{}, `{"n":3,"of":4}`},
		{ConnectionNeed{}, `{"service":"gmail","level":"personal","label":"as each person","for":"send follow-ups","status":"allowed","allowed":{"n":3,"of":4}}`},
		{ConnectionNeeds{}, `{"connections":[]}`},
		{ConnectorWait{}, `{"app":"crm","run_id":"run-1","step":"send","service":"gmail","member":"me","reason":"expired","since":"2026-10-13","stops_at":"2026-10-20"}`},
		{ConnectWaitsResult{}, `{"waits":[]}`},
		{FakeCall{}, `{"app":"crm","run_id":"run-1","tool":"gmail.send","verb_label":"Would send","summary":"Re: Team plan","at":"2026-10-13","fake":true}`},
		{ConnectFakesParams{}, `{"run_id":"run-1"}`},
		{ConnectFakesResult{}, `{"calls":[]}`},
		{ImportSource{}, `{"kind":"csv","label":"CSV file","connected":false,"detail":"choose a file"}`},
		{ImportSourcesParams{}, `{"type":"deal"}`},
		{ImportSourcesResult{}, `{"sources":[{"kind":"csv","label":"CSV file","connected":false,"detail":"choose a file"}]}`},
		{ImportSourceSelection{}, `{"kind":"hubspot","file":"bmFtZQ==","base":"base-1","table":"deals","object":"deals"}`},
		{ImportPreviewParams{}, `{"type":"deal","source":{"kind":"csv","file":"bmFtZQ=="},"mapping":[{"column":"Deal Stage","field":"stage","as":"field","values":{"appointmentscheduled":"Lead","other":""},"people":{"mona@example.com":"mona","unknown":""}}]}`},
		{Mapping{}, `{"column":"Deal Stage","field":"stage","as":"field","values":{"other":"","won":"Won"},"people":{"unknown":""}}`},
		{ImportPreview{}, `{"import_id":"import-1","type":"deal","total":312,"rows_shown":[{"id":"row-1","type":"deal","version":0,"fields":{"name":"Annual renewal","amount":48000},"created_at":"","updated_at":"","created_by":"","updated_by":""}],"columns":[],"need_choice":4,"duplicates":12,"new_refs":{"company":9},"can_import":false,"count":300}`},
		{ColumnMap{}, `{"column":"Amount","first":48000,"field":"value","match":"name","note":"matched by name","needs_choice":false,"options":["value","note","skip"]}`},
		{ImportRunParams{}, `{"import_id":"import-1","mapping":[{"column":"Probability","as":"skip"}]}`},
		{ImportResult{}, `{"import_id":"import-1","created":300,"skipped":12,"new_refs":{"company":9},"activity":"imported 300 deals"}`},
		{ImportUndoParams{}, `{"import_id":"import-1"}`},
		{ImportUndoResult{}, `{"removed":299,"kept_changed":["row-1"]}`},
	}
	for _, tt := range tests {
		t.Run(reflect.TypeOf(tt.value).Name(), func(t *testing.T) {
			decoded := reflect.New(reflect.TypeOf(tt.value)).Interface()
			if err := json.Unmarshal([]byte(tt.wire), decoded); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			var want, got any
			if err := json.Unmarshal([]byte(tt.wire), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(encoded, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("round trip = %s, want %s", encoded, tt.wire)
			}
		})
	}
}

func TestConnectOptionalFields(t *testing.T) {
	tests := []struct {
		value any
		wire  string
	}{
		{Connection{}, `{"id":"","service":"","account":"","level":"","status":"","since":""}`},
		{AddWorkspaceConnectionParams{}, `{"service":"","who":""}`},
		{SetWhoParams{}, `{"id":"","who":""}`},
		{ConnectionNeed{}, `{"service":"","level":"","label":"","for":"","status":""}`},
		{FakeCall{}, `{"app":"","tool":"","verb_label":"","summary":"","at":"","fake":false}`},
		{ConnectFakesParams{}, `{}`},
		{ImportSourceSelection{}, `{"kind":""}`},
		{ImportPreviewParams{}, `{"type":"","source":{"kind":""}}`},
		{Mapping{}, `{"column":""}`},
		{ColumnMap{}, `{"column":"","first":null,"match":"","note":"","needs_choice":false}`},
	}
	for _, tt := range tests {
		t.Run(reflect.TypeOf(tt.value).Name(), func(t *testing.T) {
			encoded, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != tt.wire {
				t.Fatalf("zero value = %s, want %s", encoded, tt.wire)
			}
		})
	}
}

func TestConnectTopics(t *testing.T) {
	for _, tt := range []struct{ got, want string }{
		{TopicConnectAllow("mona"), "connect/mona/allow"},
		{TopicConnectWaits("mona"), "connect/mona/waits"},
		{TopicConnectWorkspace(), "connect/workspace"},
		{TopicFakes("crm"), "app/crm/fakes"},
	} {
		if tt.got != tt.want {
			t.Errorf("topic = %q, want %q", tt.got, tt.want)
		}
	}
}
