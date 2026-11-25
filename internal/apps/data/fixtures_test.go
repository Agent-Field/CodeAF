package data

import (
	"encoding/json"
	"path/filepath"
	"testing"

	appschema "github.com/Agent-Field/codeaf/internal/apps/schema"
)

// THE APP SCHEMA'S OWN APPS LIVE ON THIS STORE: the CRM registers, its sample.json
// seeds a draft whole (refs between sample rows resolve), Hiring uses the
// CRM's contacts, and the TypeSet the store answers is one internal/appschema's
// validator accepts Hiring against.
func TestLaneAFixtures(t *testing.T) {
	read := func(name string) []byte { return fixture(t, filepath.Join("lane-a", name)) }
	p := NewPlace(t.TempDir(), Options{NoRefresh: true})
	defer p.Close()
	main, err := p.Main()
	if err != nil {
		t.Fatal(err)
	}
	if err := main.RegisterApp(read("crm-app.json")); err != nil {
		t.Fatal(err)
	}
	if err := main.RegisterApp(read("hiring-app.json")); err != nil {
		t.Fatal(err)
	}
	set := main.TypeSet()
	if set["contact"].Owner != "crm" || set["contact"].Fields["role_applied"].Type == "" && len(set["contact"].Fields) == 0 {
		t.Fatalf("type set: %+v", set["contact"])
	}
	hiring, err := appschema.ParseApp(read("hiring-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	for name, rt := range hiring.Records {
		if rt.Uses && set[name].Owner == "" {
			t.Fatalf("hiring uses %s, which the type set lacks", name)
		}
	}

	var sample appschema.Sample
	if err := json.Unmarshal(read("crm-sample.json"), &sample); err != nil {
		t.Fatal(err)
	}
	if err := p.SeedSample("crm-draft", read("crm-app.json"), sample.Records); err != nil {
		t.Fatal(err)
	}
	draft, _ := p.Store("crm-draft")
	for typ, rows := range sample.Records {
		page, err := draft.Query(Scope{App: "crm"}, typ, "", "", 0)
		if err != nil || page.Total != len(rows) {
			t.Fatalf("%s: %d of %d seeded (%v)", typ, page.Total, len(rows), err)
		}
	}
	groups, total, err := draft.Count(Scope{App: "crm"}, "deal", "", "stage")
	if err != nil || total != len(sample.Records["deal"]) || len(groups) == 0 {
		t.Fatalf("stage meter: %v %d %v", groups, total, err)
	}
	var hs appschema.Sample
	_ = json.Unmarshal(read("hiring-sample.json"), &hs)
	if err := p.SeedSample("hiring-draft", read("hiring-app.json"), hs.Records); err != nil {
		t.Fatal(err)
	}
}
