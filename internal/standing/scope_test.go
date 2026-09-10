package standing

import (
	"reflect"
	"testing"
	"time"
)

func TestFolderScopeNeverFallsBackToLegacyAltitude(t *testing.T) {
	item := holding("Use the agreed release process")
	item.Scope = &Scope{CollectionIDs: []string{"product"}}
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	if item.Reaches(item.Workspace, "chat") || item.AppliesTo(item.Workspace, "chat") || item.AppliesToScope(item.Workspace, "chat", nil) {
		t.Fatal("a folder rule leaked through legacy project reach")
	}
	for _, shape := range []struct {
		name     string
		scope    Scope
		altitude Altitude
	}{
		{"empty", Scope{}, ""},
		{"duplicate", Scope{CollectionIDs: []string{"product", "product"}}, ""},
		{"altitude collision", Scope{CollectionIDs: []string{"product"}}, AltitudeMachine},
	} {
		t.Run(shape.name, func(t *testing.T) {
			bad := item
			bad.Scope = &shape.scope
			bad.Altitude = shape.altitude
			if bad.Validate() == nil {
				t.Fatal("invalid scope was admitted")
			}
			if bad.Reaches(item.Workspace, "chat") || bad.AppliesToScope(item.Workspace, "chat", map[string]int{"product": 0}) {
				t.Fatal("invalid stored scope acquired reach")
			}
		})
	}
}

func TestFolderScopeRequiresExplicitDescendantsAndKeepsIndependentRules(t *testing.T) {
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	s := openStore(t, now)
	direct := holding("Use the Product release process")
	direct.Scope = &Scope{CollectionIDs: []string{"product"}}
	first, err := s.Create(direct)
	if err != nil {
		t.Fatal(err)
	}
	inherited := holding("Do not include secrets in reports")
	inherited.Scope = &Scope{CollectionIDs: []string{"company"}, Descendants: true}
	second, err := s.Create(inherited)
	if err != nil {
		t.Fatal(err)
	}
	legacy := holding("Keep output concise")
	third, err := s.Create(legacy)
	if err != nil {
		t.Fatal(err)
	}
	depths := map[string]int{"product": 0, "company": 2}
	assertIDs := func(session string, want map[string]bool) {
		t.Helper()
		got, err := s.ApplicableScope(direct.Workspace, session, depths)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, item := range got {
			ids[item.ID] = true
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("scope %v, want %v", ids, want)
		}
	}
	assertIDs("one", map[string]bool{first.ID: true, second.ID: true, third.ID: true})
	if err := s.AddException(first.ID, Exception{SessionID: "one", At: now}); err != nil {
		t.Fatal(err)
	}
	assertIDs("one", map[string]bool{second.ID: true, third.ID: true})
	assertIDs("two", map[string]bool{first.ID: true, second.ID: true, third.ID: true})
	depths["product"] = 1
	assertIDs("two", map[string]bool{second.ID: true, third.ID: true})
	if _, err := s.SetStatus(second.ID, StatusPaused, ""); err != nil {
		t.Fatal(err)
	}
	assertIDs("two", map[string]bool{third.ID: true})
}
