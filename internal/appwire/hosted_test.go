package appwire

import (
	"strings"
	"testing"
)

func TestHostedAgentIDShape(t *testing.T) {
	const session = "hs_0123456789abcdef01234567"
	for _, tc := range []struct {
		name, id, org string
	}{
		{"factory", session + "_factory_0123456789abcdef", "factory_0123456789abcdef"},
		{"connection punctuation and case", session + "__Org-1_", "_Org-1_"},
		{"one character", session + "_X", "X"},
		{"longest connection", session + "_" + strings.Repeat("x", 64), strings.Repeat("x", 64)},
		{"missing separator", session + "factory_0123456789abcdef", ""},
		{"session alone", session, ""},
		{"empty connection", session + "_", ""},
		{"short session", session[:len(session)-1] + "_factory", ""},
		{"long session", session + "a_factory", ""},
		{"uppercase session", "hs_0123456789ABCDEF01234567_factory", ""},
		{"nonhex session", "hs_0123456789abcdef0123456g_factory", ""},
		{"illegal connection", session + "_a/b", ""},
		{"nonascii connection", session + "_é", ""},
		{"oversize connection", session + "_" + strings.Repeat("x", 65), ""},
		{"ordinary id", "saved-local-chat", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, org, ok := ParseHostedAgentID(tc.id)
			if tc.org == "" {
				if ok || s != "" || org != "" {
					t.Fatalf("ParseHostedAgentID(%q) = %q, %q, %t", tc.id, s, org, ok)
				}
				return
			}
			if !ok || s != session || org != tc.org || HostedAgentID(s, org) != tc.id {
				t.Fatalf("ParseHostedAgentID(%q) = %q, %q, %t", tc.id, s, org, ok)
			}
		})
	}
}

func TestHostedV4OldEngineKeepsPersonalCreate(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	data := w.next()["data"].(map[string]any)
	if features, ok := data["features"].([]any); ok {
		for _, feature := range features {
			if feature == "agents.hosted" {
				t.Fatal("an engine without hosted chats advertised them")
			}
		}
	}

	w.send(`{"id":1,"method":"agents.create","params":{"text":"A personal chat."}}`)
	row := w.next()["result"].(map[string]any)["agent"].(map[string]any)
	if row["id"] != "new" || row["hosted"] != nil {
		t.Fatalf("personal create = %v", row)
	}
	for _, method := range []string{"agents.create", "agents.hosted_list"} {
		w.send(`{"id":2,"method":"` + method + `","params":{"org":"factory_0123456789abcdef","text":"Wrong window."}}`)
		got := w.next()["error"].(map[string]any)
		if got["code"] != CodeUnknownMethod {
			t.Fatalf("%s = %v", method, got)
		}
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.created.Text != "A personal chat." || engine.created.Org != "" {
		t.Fatalf("the old engine received a hosted create: %+v", engine.created)
	}
}
