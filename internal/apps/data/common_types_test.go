package data

import "testing"

func TestCommonTypeDeclarationsReuseSpaceIDs(t *testing.T) {
	place := NewPlace(t.TempDir(), Options{NoRefresh: true})
	defer place.Close()
	s, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	crm := []byte(`{"id":"crm","records":{
		"company":{"common":"organization","fields":{"name":{"type":"text"}}},
		"contact":{"common":"person","fields":{"name":{"type":"text"},"company":{"type":"ref","to":"company"}}}
	},"capabilities":{"records":{"company":"rw","contact":"rw"}}}`)
	support := []byte(`{"id":"support","records":{
		"customer":{"common":"person","uses":true,"fields":{}},
		"account":{"common":"organization","uses":true,"fields":{}},
		"ticket":{"fields":{"contact":{"type":"ref","to":"customer"},"company":{"type":"ref","to":"account"}}}
	},"capabilities":{"records":{"customer":"r","account":"r","ticket":"rw"}}}`)
	for _, p := range []AppPublication{
		{App: "crm", Version: 1, Metadata: []byte("{}"), Definition: crm},
		{App: "support", Version: 1, Metadata: []byte("{}"), Definition: support},
	} {
		if err := s.PublishApp(p, 0); err != nil {
			t.Fatal(err)
		}
	}
	crmScope, supportScope := Scope{App: "crm"}, Scope{App: "support"}
	company := mustCreate(t, s, crmScope, "company", map[string]any{"name": "Acme"})[0]
	contact := mustCreate(t, s, crmScope, "contact", map[string]any{"name": "Ari", "company": company.ID()})[0]
	for _, names := range [][2]string{{"contact", "customer"}, {"company", "account"}} {
		first, err := s.Type(crmScope, names[0])
		if err != nil {
			t.Fatal(err)
		}
		second, err := s.Type(supportScope, names[1])
		if err != nil || first.ID != second.ID || first.Fields["name"].ID != second.Fields["name"].ID {
			t.Fatalf("common type and field IDs differ for %v: %v", names, err)
		}
	}
	for _, row := range []struct{ alias, id string }{{"customer", contact.ID()}, {"account", company.ID()}} {
		if got, err := s.Get(supportScope, row.alias, row.id, false); err != nil || got.ID() != row.id {
			t.Fatalf("support can't read CRM's %s: %v, %v", row.alias, got, err)
		}
	}
	mustCreate(t, s, supportScope, "ticket", map[string]any{"contact": contact.ID(), "company": company.ID()})

	if err := place.SeedSample("support-draft", support, map[string][]map[string]any{
		"account":  {{"name": "Sample Co"}},
		"customer": {{"name": "Sample Contact", "company": "Sample Co"}},
	}); err != nil {
		t.Fatal(err)
	}
	draft, err := place.Store("support-draft")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := draft.Get(supportScope, "customer", contact.ID(), false); err == nil {
		t.Fatal("the draft can see a live contact")
	}
}
