package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// factory.rules says who last changed the factory's rules and when, as the
// factory reports it, and says neither for rules that were never written.
func TestFactoryRulesSaysWhoChangedThemLastAndWhen(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	if !strings.Contains(r.hello, appwire.FeatureFactoryRulesChangedBy) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryRulesChangedBy, r.hello)
	}
	lists := `"rules":["keep the diff small"],"ask_first":[],"never":[]`
	stub.other = serveRaw(`{` + lists + `}`)
	var fresh map[string]any
	r.must("factory.rules", appwire.FactoryRef{Factory: r.id}, &fresh)
	for _, k := range []string{"changed_by", "changed_at"} {
		if _, ok := fresh[k]; ok {
			t.Fatalf("rules never written carry %s: %v", k, fresh)
		}
	}
	stub.other = serveRaw(`{` + lists + `,"changed_by":"owner","changed_at":"2026-10-11T12:00:00Z"}`)
	var got map[string]json.RawMessage
	r.must("factory.rules", appwire.FactoryRef{Factory: r.id}, &got)
	if string(got["changed_by"]) != `"owner"` || string(got["changed_at"]) != `"2026-10-11T12:00:00Z"` {
		t.Fatalf("changed_by %s changed_at %s", got["changed_by"], got["changed_at"])
	}
}
