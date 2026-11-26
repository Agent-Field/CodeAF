package data

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const policyApp = `{"id":"policy","records":{"account":{
	"fields":{"name":{"type":"text"},"owner":{"type":"text"},"salary":{"type":"number"},"customer":{"type":"text","class":"restricted"}},
	"policy":{"row":{"field":"owner","attribute":"id"},"fields":{"salary":"hide"}}
}},"capabilities":{"records":{"account":"rw"}}}`

func TestInheritedPolicyBeforeQuery(t *testing.T) {
	s, _ := openCRM(t)
	def := []byte(policyApp)
	if err := s.RegisterApp(def); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp([]byte(`{"id":"other","records":{"account":{"uses":true}},"capabilities":{"records":{"account":"r"}}}`)); err != nil {
		t.Fatal(err)
	}
	rows := mustCreate(t, s, Scope{Person: PersonPrincipal{ID: "first"}}, "account",
		map[string]any{"name": "Example", "owner": "first", "salary": 100, "customer": "private"})
	for _, tc := range []struct {
		name  string
		scope Scope
		want  int
	}{
		{"owner", Scope{App: "other", Person: PersonPrincipal{ID: "first"}}, 1},
		{"other person after owner's read", Scope{App: "other", Person: PersonPrincipal{ID: "second"}}, 0},
		{"agent inherits launcher", Scope{App: "other", Principal: AgentPrincipal{ID: "assistant"}, Person: PersonPrincipal{ID: "second"}, Actor: "first"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, err := s.Query(tc.scope, "account", "", "", 1)
			if err != nil || page.Total != tc.want || len(page.Rows) != tc.want {
				t.Fatalf("query = %+v, %v", page, err)
			}
			agg, err := s.Aggregate(tc.scope, &QueryIR{Type: "account", Aggregate: []Agg{{Fn: "count"}}})
			if err != nil || agg.Total != tc.want {
				t.Fatalf("aggregate = %+v, %v", agg, err)
			}
			row, err := s.Get(tc.scope, "account", rows[0].ID(), false)
			if tc.want == 0 {
				wantCode(t, err, CodeNotFound)
				return
			}
			if err != nil || row["customer"] != masked || row["salary"] != nil || page.Rows[0]["customer"] != masked {
				t.Fatalf("projection = %+v, %v", row, err)
			}
			if _, err := s.Query(tc.scope, "account", "sort:salary", "", 1); err == nil {
				t.Fatal("hidden salary influenced sorting")
			}
		})
	}
}

func TestPolicyRevocationClearsCachedRowsBeforeDelivery(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp([]byte(policyApp)); err != nil {
		t.Fatal(err)
	}
	first := Scope{App: "policy", Person: PersonPrincipal{ID: "first"}}
	second := Scope{App: "policy", Person: PersonPrincipal{ID: "second"}}
	id := mustCreate(t, s, first, "account", map[string]any{"name": "Private", "owner": "first"})[0].ID()
	events := make(chan Diff, 8)
	owner := &SubscriptionOwner{}
	snapshot, err := s.Watch(owner, first, SubSpec{ID: "accounts", Type: "account"}, func(d Diff) { events <- d })
	if err != nil || len(snapshot.Rows) != 1 || snapshot.Rows[0].ID() != id {
		t.Fatalf("first snapshot %+v, %v", snapshot, err)
	}
	other, err := s.Watch(&SubscriptionOwner{}, second, SubSpec{ID: "accounts", Type: "account"}, func(Diff) {})
	if err != nil || other.Total != 0 || len(other.Rows) != 0 {
		t.Fatalf("another person's snapshot included a cached result: %+v, %v", other, err)
	}
	if _, err := s.Update(first, "account", []string{id}, map[string]any{"owner": "second"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	tp, err := s.Type(first, "account")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidatePolicy(tp.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-events:
		if !update.Reset || len(update.Upserts) != 0 || len(update.Removes) != 0 {
			t.Fatalf("delivery after revocation: %+v", update)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("revocation did not reset the subscription")
	}
	now, err := s.Watch(owner, first, SubSpec{ID: "accounts", Type: "account"}, func(Diff) {})
	if err != nil || now.Total != 0 || len(now.Rows) != 0 {
		t.Fatalf("refetch after revocation %+v, %v", now, err)
	}
	if _, err := s.History(first, "account", id); err == nil {
		t.Fatal("history disclosed a revoked record")
	}
	audit, err := os.ReadFile(filepath.Join(filepath.Dir(s.path), "policy.jsonl"))
	if err != nil || !bytes.Contains(audit, []byte("row_refused")) || bytes.Contains(audit, []byte("Private")) || bytes.Contains(audit, []byte("first")) {
		t.Fatalf("policy log contains the wrong data: %s, %v", audit, err)
	}
}

func TestRowPolicyProjectsOnlyAuthorizedLinks(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp([]byte(policyApp)); err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp([]byte(`{"id":"links","records":{
		"account":{"uses":true},
		"project":{"fields":{"name":{"type":"text"},"accounts":{"type":"refs","to":"account"}}}
	},"capabilities":{"records":{"account":"r","project":"rw"}}}`)); err != nil {
		t.Fatal(err)
	}
	first := Scope{Person: PersonPrincipal{ID: "first"}}
	second := Scope{Person: PersonPrincipal{ID: "second"}}
	visible := mustCreate(t, s, first, "account", map[string]any{"name": "Mine", "owner": "first"})[0].ID()
	hidden := mustCreate(t, s, second, "account", map[string]any{"name": "Theirs", "owner": "second"})[0].ID()
	_ = mustCreate(t, s, first, "project", map[string]any{"name": "Project", "accounts": []any{visible, hidden}})
	first.App = "links"
	result, err := s.QueryRows(first, &QueryIR{Type: "project", Select: []string{"accounts", "accounts.name"}}, "", 1)
	if err != nil || len(result.Rows) != 1 {
		t.Fatalf("query: %+v, %v", result, err)
	}
	raw := canonicalJSON(result.Rows[0])
	if !strings.Contains(raw, visible) || !strings.Contains(raw, "Mine") || strings.Contains(raw, hidden) || strings.Contains(raw, "Theirs") {
		t.Fatalf("related projection %s", raw)
	}
}

func TestRecordWatchClearsOnPolicyInvalidation(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp([]byte(policyApp)); err != nil {
		t.Fatal(err)
	}
	sc := Scope{App: "policy", Person: PersonPrincipal{ID: "first"}}
	row := mustCreate(t, s, sc, "account", map[string]any{"name": "Mine", "owner": "first"})[0]
	var allowed atomic.Bool
	allowed.Store(true)
	updates := make(chan Diff, 8)
	_, err := s.Watch(&SubscriptionOwner{}, sc, SubSpec{ID: "record", Type: "account", Record: row.ID(), Authorize: allowed.Load}, func(diff Diff) { updates <- diff })
	if err != nil {
		t.Fatal(err)
	}
	allowed.Store(false)
	typ, err := s.Type(sc, "account")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.InvalidatePolicy(typ.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(sc, "account", []string{row.ID()}, map[string]any{"name": "After revocation"}, UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case diff := <-updates:
		if !diff.Reset || len(diff.Upserts) != 0 || len(diff.Removes) != 0 {
			t.Fatalf("revoked record watch delivered %+v", diff)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("record watch did not receive a reset")
	}
}

func TestGrantedAgentCannotCreateOutsideLaunchersRowPolicy(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp([]byte(policyApp)); err != nil {
		t.Fatal(err)
	}
	agent := AgentPrincipal{ID: "app-agent"}
	if err := s.SetAgentGrant("policy", agent, "actions", "create-account", true); err != nil {
		t.Fatal(err)
	}
	sc := Scope{App: "policy", Person: PersonPrincipal{ID: "launcher"}, Principal: agent, Action: "create-account"}
	_, err := s.Create(sc, "account", []map[string]any{{"name": "Wrong account", "owner": "other-person"}})
	wantCode(t, err, CodeNotAllowed)
	rows := mustCreate(t, s, sc, "account", map[string]any{"name": "My account", "owner": "launcher"})
	if len(rows) != 1 || rows[0]["name"] != "My account" {
		t.Fatalf("scoped create: %+v", rows)
	}
}

func TestPersonalAmountAndMaskedTotals(t *testing.T) {
	s, _ := openCRM(t)
	if err := s.RegisterApp([]byte(`{"id":"finance","records":{"transaction":{
		"fields":{"owner":{"type":"text"},"amount":{"type":"money","class":"confidential"},"secret":{"type":"number"},"name":{"type":"text","class":"pii"},"pin":{"type":"number"}},
		"policy":{"row":{"field":"owner","attribute":"id"},"fields":{"secret":"hide","pin":"mask"}}
	}},"capabilities":{"records":{"transaction":"rw"}}}`)); err != nil {
		t.Fatal(err)
	}
	owner := Scope{App: "finance", Person: PersonPrincipal{ID: s.Person()}}
	viewer := Scope{App: "finance", Person: PersonPrincipal{ID: "viewer"}}
	rows := mustCreate(t, s, owner, "transaction", map[string]any{"owner": s.Person(), "amount": 100, "secret": 90, "name": "My Name", "pin": 12})
	for _, amount := range []float64{2.50, 12.75} {
		mustCreate(t, s, viewer, "transaction", map[string]any{"owner": "viewer", "amount": amount, "secret": 90})
	}
	row, err := s.Get(owner, "transaction", rows[0].ID(), false)
	if err != nil || row["amount"] != float64(100) || row["name"] != masked || row["pin"] != masked {
		t.Errorf("the owner's data = %v, %v", row, err)
	}
	page, err := s.Query(viewer, "transaction", "", "", 10)
	if err != nil || len(page.Rows) != 2 || page.Rows[0]["amount"] != masked || page.Rows[1]["amount"] != masked {
		t.Fatalf("viewer rows: %+v, %v", page, err)
	}
	if _, err := s.Get(viewer, "transaction", rows[0].ID(), false); err == nil {
		t.Error("viewer read another person's row")
	}
	agg, err := s.Aggregate(viewer, &QueryIR{Type: "transaction", Aggregate: []Agg{{Fn: "sum", Field: "amount"}, {Fn: "avg", Field: "amount"}}})
	if err != nil || len(agg.Groups) != 1 || agg.Groups[0].Values["sum_amount"] != float64(15.25) || agg.Groups[0].Values["avg_amount"] != float64(7.63) {
		t.Errorf("viewer totals: %+v, %v", agg, err)
	}
	_, err = s.Aggregate(viewer, &QueryIR{Type: "transaction", Aggregate: []Agg{{Fn: "sum", Field: "secret"}}})
	if err == nil || !strings.Contains(err.Error(), "Choose a field you can access") {
		t.Errorf("denied total: %v", err)
	}
	_, err = s.Aggregate(viewer, &QueryIR{Type: "transaction", Group: []string{"amount"}, Aggregate: []Agg{{Fn: "count"}}})
	if err == nil {
		t.Error("viewer grouped by masked amounts")
	}
}
