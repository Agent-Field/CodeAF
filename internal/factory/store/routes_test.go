package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/route"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// The routing table is kept beside the house rules: empty until written,
// cleaned on the way in, kept with who changed it, and refused whole.
func TestRoutesAreKeptCheckedAndGuarded(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	kept, err := st.KeptRoutes()
	if err != nil || kept.Rules == nil || len(kept.Rules) != 0 || kept.ChangedBy != "" {
		t.Fatalf("a factory never given rules: %+v, %v", kept, err)
	}
	at := time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)
	by := factory.RulesChange{ChangedBy: "octocat", ChangedAt: &at}
	table := []route.Rule{{Name: " jira ", When: route.Match{Sources: []string{"Jira"}}, Then: route.Action{Repo: "acme/api", Start: "auto"}}}
	saved, err := st.SetRoutes(table, by, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []route.Rule{{Name: "jira", When: route.Match{Sources: []string{"jira"}}, Then: route.Action{Repo: "acme/api", Start: route.StartAuto}}}
	if !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved %+v, want %+v", saved, want)
	}
	kept, err = st.KeptRoutes()
	if err != nil || !reflect.DeepEqual(kept.Rules, want) || kept.ChangedBy != "octocat" {
		t.Fatalf("read back %+v, %v", kept, err)
	}

	// A stale expect writes nothing; a current one writes.
	stale := []route.Rule{}
	if _, err := st.SetRoutes(nil, by, &stale); !errors.Is(err, store.ErrRoutesMoved) {
		t.Fatalf("a stale expect: %v, want ErrRoutesMoved", err)
	}
	if got, _ := st.Routes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("a stale write changed the table to %+v", got)
	}
	if _, err := st.SetRoutes([]route.Rule{{Name: "x"}}, by, nil); err == nil {
		t.Fatal("a rule with no start was kept")
	}
	if _, err := st.SetRoutes(nil, by, &want); err != nil {
		t.Fatalf("a current expect: %v", err)
	}
	if got, _ := st.Routes(); len(got) != 0 {
		t.Fatalf("emptied table reads %+v", got)
	}

	// AN UNREADABLE FILE IS AN ERROR, never an empty table.
	if err := os.WriteFile(filepath.Join(st.Root(), "factory-routes.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Routes(); err == nil {
		t.Fatal("an unreadable routing file read as no rules")
	}
}
