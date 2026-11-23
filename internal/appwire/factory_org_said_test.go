package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// LIVE TEST 2026-11-23: the bridge's own types dropped added_by and rules in
// words, so "Ann added you" and a new workstream's rules never reached the
// app. An empty rules list must still go to the server, or the last rule
// taken out stays.
func TestTheBridgeCarriesAddedByAndRulesInWords(t *testing.T) {
	var m OrgMember
	if err := json.Unmarshal([]byte(`{"kind":"person","id":"bob","name":"Bob","role":"edit","added_at":"2026-11-23T15:13:43Z","added_by":"ann"}`), &m); err != nil {
		t.Fatal(err)
	}
	if out, _ := json.Marshal(m); !strings.Contains(string(out), `"added_by":"ann"`) {
		t.Fatalf("added_by dropped: %s", out)
	}
	var r OrgWorkstreamRules
	if err := json.Unmarshal([]byte(`{"default_owner":{"kind":"everyone"},"start_without_asking_usd":0,"keeps_running":{"factory":false,"orders":[]},"said":["Keep it short"]}`), &r); err != nil {
		t.Fatal(err)
	}
	if out, _ := json.Marshal(r); !strings.Contains(string(out), `"said":["Keep it short"]`) {
		t.Fatalf("said dropped: %s", out)
	}
	empty := []string{}
	r.Said = &empty
	if out, _ := json.Marshal(r); !strings.Contains(string(out), `"said":[]`) {
		t.Fatalf("an empty list must clear the server's rules in words: %s", out)
	}
	r.Said = nil
	if out, _ := json.Marshal(r); strings.Contains(string(out), `"said"`) {
		t.Fatalf("absent must stay absent, so the server keeps its rules in words: %s", out)
	}
}
