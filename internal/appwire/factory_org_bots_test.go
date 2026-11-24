package appwire

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestContractC10OrgBotWire(t *testing.T) {
	const conn = `"connection":"c1"`
	for _, tc := range []struct {
		method, fields, verb, path, body string
	}{
		{"factory.org_bots", "", "GET", "/v1/bots", "null"},
		{"factory.org_bot_new", `,"name":"Build helper","scope":"view"`, "POST", "/v1/bots", `{"name":"Build helper","scope":"view"}`},
		{"factory.org_bot_token_new", `,"bot":"bt_1","label":"rotation","expires_at":"2027-01-02T03:04:05Z"`, "POST", "/v1/bots/bt_1/tokens",
			`{"label":"rotation","expires_at":"2027-01-02T03:04:05Z"}`},
		{"factory.org_bot_token_new", `,"bot":"bt_1"`, "POST", "/v1/bots/bt_1/tokens", "{}"},
		{"factory.org_bot_token_revoke", `,"bot":"bt_1","token":"btk_1"`, "DELETE", "/v1/bots/bt_1/tokens/btk_1", "null"},
		{"factory.org_bot_remove", `,"bot":"bt_1"`, "DELETE", "/v1/bots/bt_1", "null"},
	} {
		t.Run(tc.method+" "+tc.fields, func(t *testing.T) {
			e := &orgAccessEngine{}
			if _, err := methods[tc.method].call(e, json.RawMessage(`{`+conn+tc.fields+`}`)); err != nil {
				t.Fatal(err)
			}
			if len(e.got) != 1 {
				t.Fatalf("got %d requests", len(e.got))
			}
			got := e.got[0]
			body, err := json.Marshal(got.Body)
			if err != nil || got.Method != tc.verb || got.Path != tc.path || string(body) != tc.body {
				t.Fatalf("request was %s %s %s, want %s %s %s", got.Method, got.Path, body, tc.verb, tc.path, tc.body)
			}
		})
	}
	for _, tc := range []struct{ method, fields string }{
		{"factory.org_bot_remove", `,"bot":"../policy"`},
		{"factory.org_bot_token_new", `,"bot":"b?c"`},
		{"factory.org_bot_token_revoke", `,"bot":"bt_1","token":"t/1"`},
		{"factory.org_bot_token_revoke", `,"bot":"bt_1","token":""`},
	} {
		e := &orgAccessEngine{}
		_, err := methods[tc.method].call(e, json.RawMessage(`{`+conn+tc.fields+`}`))
		if wireCode(t, err) != CodeBadParams || len(e.got) != 0 {
			t.Fatalf("%s accepted an invalid path segment: %v", tc.method, err)
		}
	}
	if hello := withFactoryOrgBotsFeature(Hello{}, &orgAccessEngine{}); !slices.Contains(hello.Features, "org_bots") {
		t.Fatalf("organization engine features = %v", hello.Features)
	}
	if hello := withFactoryOrgBotsFeature(Hello{}, &factoryEngine{}); slices.Contains(hello.Features, "org_bots") {
		t.Fatalf("factory-only engine features = %v", hello.Features)
	}
}
