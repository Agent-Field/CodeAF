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

func TestDraftExamplesLinkBothWays(t *testing.T) {
	const company = "00000000000000000000000001"
	const contact = "00000000000000000000000002"
	app := []byte(`{"id":"crm","name":"CRM","version":1,"kit":0,"records":{
		"company":{"title":"name","fields":{"name":{"type":"text"},"lead":{"type":"ref","to":"contact","required":true}}},
		"contact":{"title":"name","fields":{"name":{"type":"text"},"company":{"type":"ref","to":"company","required":true}}}
	},"capabilities":{"records":{"company":"rw","contact":"rw"}}}`)
	p := NewPlace(t.TempDir(), Options{NoRefresh: true})
	defer p.Close()
	examples := map[string][]map[string]any{
		"company": {{"id": company, "name": "Northwind", "lead": "Alex"}},
		"contact": {{"id": contact, "name": "Alex", "company": company}},
	}
	if err := p.SeedSample("crm-draft", app, examples); err != nil {
		t.Fatal(err)
	}
	draft, err := p.Store("crm-draft")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ typ, id, field, ref string }{
		{"company", company, "lead", contact},
		{"contact", contact, "company", company},
	} {
		row, err := draft.Get(Scope{App: "crm"}, want.typ, want.id, false)
		if err != nil || row[want.field] != want.ref {
			t.Fatalf("%s: got %v, want %s (%v)", want.typ, row, want.ref, err)
		}
	}
}

func TestMeetingRevisionExamplesSeedSharedCyclicReferences(t *testing.T) {
	p := NewPlace(t.TempDir(), Options{NoRefresh: true})
	defer p.Close()
	main, err := p.Main()
	if err != nil {
		t.Fatal(err)
	}
	const fixturePath = "meeting-revision/"
	if err := main.RegisterApp(fixture(t, fixturePath+"owner.json")); err != nil {
		t.Fatal(err)
	}
	var sample appschema.Sample
	if err := json.Unmarshal(fixture(t, fixturePath+"sample.json"), &sample); err != nil {
		t.Fatal(err)
	}
	app := fixture(t, fixturePath+"app.json")
	if err := p.SeedSample("meeting-draft", app, sample.Records); err != nil {
		t.Fatal(err)
	}
	draft, err := p.Store("meeting-draft")
	if err != nil {
		t.Fatal(err)
	}
	def, err := appschema.ParseApp(app)
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{App: def.ID}
	transcript, err := draft.Get(scope, "transcript", "00000000000000000000000019", false)
	if err != nil || transcript["meeting"] != "00000000000000000000000013" {
		t.Fatalf("transcript did not retain its shared meeting: %v (%v)", transcript, err)
	}
}
