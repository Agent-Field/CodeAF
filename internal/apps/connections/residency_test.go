package connections_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
)

func TestPostgresCRMResidencyRoundTrip(t *testing.T) {
	address := startDatabase(t, "postgres:17-alpine", 5432,
		[]string{"POSTGRES_PASSWORD=fixture-password", "POSTGRES_DB=crm"})
	dsn := "postgres://postgres:fixture-password@" + address + "/crm?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for db.PingContext(ctx) != nil {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
	execSQL := func(query string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(`CREATE TABLE contacts (id BIGINT PRIMARY KEY, name TEXT NOT NULL,
		version BIGINT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
		deleted BOOLEAN NOT NULL DEFAULT false, title TEXT, email TEXT, phone TEXT, company_id BIGINT)`)
	execSQL(`INSERT INTO contacts (id,name,version) VALUES (41,'Grace',1),(42,'Ada',1)`)
	execSQL(`UPDATE contacts SET title='Engineer', email='ada@example.test', phone='555-2345', company_id=17 WHERE id=42`)
	svc, store := crm(t)
	scope := data.Scope{App: "crm"}
	company, err := store.Create(scope, "company", []map[string]any{{"name": "Northwind"}})
	if err != nil {
		t.Fatal(err)
	}
	companyID := company.Rows[0].ID()
	rawScreen, err := os.ReadFile("testdata/contacts-screen.json")
	if err != nil {
		t.Fatal(err)
	}
	var screen struct {
		Body struct {
			Children []struct {
				Bind struct {
					Type  string `json:"records"`
					Query string `json:"query"`
				}
			}
		}
	}
	if err := json.Unmarshal(rawScreen, &screen); err != nil {
		t.Fatal(err)
	}
	contacts := screen.Body.Children[0].Bind
	cfg := connections.Config{ID: "directory", Kind: connections.Postgres, App: "crm",
		Reads: map[string]connections.Read{"contacts": {
			Type: "contact", Table: "contacts", Key: "id", Version: "version", OrderBy: "name",
			Fields: map[string]string{"name": "name", "title": "title", "email": "email", "phone": "phone", "company": "company_id"}, Match: map[string]string{"name": "name"},
			Residency: &connections.ResidencyConfig{UpdatedAt: "updated_at", Tombstone: "deleted", FreshForSeconds: 30, References: map[string]map[string]string{"company": {"17": companyID}}},
		}},
		Writes: map[string]connections.Write{"edit": {Read: "contacts", Fields: []string{"name"}}},
	}
	if err := svc.Register(cfg, dsn); err != nil {
		t.Fatal(err)
	}
	publication, _, err := store.Publication("crm")
	if err != nil {
		t.Fatal(err)
	}
	ids := make(map[string]bool)
	read := func(mode string, count int, first string) connections.Page {
		t.Helper()
		page, err := svc.Read(ctx, contactsQuery())
		if err != nil || len(page.Rows) != count || page.Rows[0]["name"] != first ||
			page.Source.Residency != mode || page.Source.ObservedAt.IsZero() || page.Rows[0]["company"] != companyID || page.Rows[0]["phone"] != "••••" {
			t.Fatalf("%s: %+v %v", mode, page, err)
		}
		for _, row := range page.Rows {
			if len(ids) > 0 && mode != "live" && !ids[row.ID()] {
				t.Fatalf("Person changed: %s", row.ID())
			}
		}
		after, _, err := store.Publication("crm")
		if err != nil || !bytes.Equal(after.Metadata, publication.Metadata) || after.Version != publication.Version {
			t.Fatalf("app hash changed: %v", err)
		}
		ir, err := data.ParseQuery(contacts.Query)
		if err != nil {
			t.Fatal(err)
		}
		ir.Type = contacts.Type
		plan, source, _, err := svc.PlanQuery(ctx, "crm", *ir)
		if err != nil || source.Residency != mode {
			t.Fatalf("query plan: %+v %v", source, err)
		}
		projected, err := store.QueryRows(scope, &plan, "", 0)
		if err != nil || len(projected.Rows) != count || projected.Rows[0].ID() != page.Rows[0].ID() ||
			projected.Rows[0]["name"] != first || projected.Rows[0]["company"] != companyID {
			t.Fatalf("Contacts screen: %+v %v", projected, err)
		}
		got, _, err := svc.GetRecord(ctx, "crm", "contact", page.Rows[0].ID(), false)
		if err != nil || got["company"] != companyID {
			t.Fatalf("Contacts detail: %+v %v", got, err)
		}
		return page
	}
	live := read("live", 2, "Ada")
	for _, row := range live.Rows {
		ids[row.ID()] = true
	}
	id := live.Rows[0].ID()
	var meta struct{ Digest string }
	if err := json.Unmarshal(publication.Metadata, &meta); err != nil || len(meta.Digest) != 64 {
		t.Fatal("missing kept app digest")
	}
	t.Logf("live rows=%d app=%s people=%s,%s", len(live.Rows), meta.Digest, id, live.Rows[1].ID())
	cut := func(mode data.ResidencyMode, quiesced bool) {
		t.Helper()
		request := connections.CutoverRequest{App: "crm", Type: "contact", To: mode, SourceQuiesced: quiesced}
		preview, err := svc.PreviewCutover(ctx, request)
		if err != nil || preview.Rows != 2 || preview.Conflicts != 0 {
			t.Fatalf("preview: %+v %v", preview, err)
		}
		result, err := svc.ApplyCutover(ctx, request, preview.Confirmation)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s → %s rows=%d conflicts=%d replay_rows=%d epochs=%+v", preview.From, mode, preview.Rows, preview.Conflicts, preview.ReplayRows, result.Epochs)
	}
	cut(data.ResidencyMirror, false)
	read("mirror", 2, "Ada")
	if _, err := store.Create(scope, "deal", []map[string]any{{"name": "Sale", "company": companyID, "contacts": []any{id}}}); err != nil {
		t.Fatal(err)
	}
	execSQL(`UPDATE contacts SET name='Ada Byron',version=2,updated_at=clock_timestamp() WHERE id=42`)
	if err := svc.SyncResidency(ctx, "crm", "contact"); err != nil {
		t.Fatal(err)
	}
	read("mirror", 2, "Ada Byron")
	execSQL(`UPDATE contacts SET deleted=true,version=2,updated_at=clock_timestamp() WHERE id=41`)
	if err := svc.SyncResidency(ctx, "crm", "contact"); err != nil {
		t.Fatal(err)
	}
	read("mirror", 1, "Ada Byron")
	execSQL(`UPDATE contacts SET deleted=false,version=3,updated_at=clock_timestamp() WHERE id=41`)
	if err := svc.SyncResidency(ctx, "crm", "contact"); err != nil {
		t.Fatal(err)
	}
	read("mirror", 2, "Ada Byron")
	run := startEdit(t, crmRunConfig(t, svc), id, "2", func() {})
	checkReceipt(t, run, "committed")
	if err := svc.SyncResidency(ctx, "crm", "contact"); err != nil {
		t.Fatal(err)
	}
	read("mirror", 2, "Ada Lovelace")
	stale := startEdit(t, crmRunConfig(t, svc), id, "2", func() {})
	checkReceipt(t, stale, "conflict")
	entry, err := store.History(scope, "contact", id)
	if err != nil || len(entry) == 0 || entry[0].ByKind != data.BySync {
		t.Fatalf("source edit history: %+v %v", entry, err)
	}
	t.Log("mirror rows=2, source edit and brokered write-back observed, stale revision conflicted, tombstone left and re-entered with the same ID")
	fenced := startEdit(t, crmRunConfig(t, svc), id, "3", func() {
		cut(data.ResidencyNative, true)
	})
	if fenced.State != "incomplete" || len(fenced.Effects) != 0 {
		t.Fatalf("approval survived the authority change: %+v", fenced)
	}
	if _, err := store.Update(scope, "contact", []string{id}, map[string]any{"name": "Ada King"}, data.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	read("native", 2, "Ada King")
	execSQL(`UPDATE contacts SET name='Grace Changed',version=4,updated_at=clock_timestamp() WHERE id=41`)
	toLive := connections.CutoverRequest{App: "crm", Type: "contact", To: data.ResidencyLive, SourceQuiesced: true}
	conflicted, err := svc.PreviewCutover(ctx, toLive)
	if err != nil || conflicted.Conflicts != 1 {
		t.Fatalf("upstream rollback conflict: %+v %v", conflicted, err)
	}
	if _, err := svc.ApplyCutover(ctx, toLive, conflicted.Confirmation); err == nil {
		t.Fatal("conflicted rollback changed authority")
	}
	read("native", 2, "Ada King")
	execSQL(`UPDATE contacts SET name='Grace',version=5,updated_at=clock_timestamp() WHERE id=41`)
	cut(data.ResidencyLive, true)
	final := read("live", 2, "Ada King")
	if final.Rows[0].ID() != id || final.Rows[1].ID() != live.Rows[1].ID() {
		t.Fatal("Person IDs changed on rollback")
	}
	linked := data.QueryIR{Type: "deal", Select: []string{"contacts.name"}, Where: []data.Cond{{Field: "contacts.name", Op: ":", Value: "Ada King"}}}
	planned, source, _, err := svc.PlanQuery(ctx, "crm", linked)
	if err != nil || len(source.Contributors) != 1 || source.Contributors[0].Residency != "live" {
		t.Fatalf("federated sources: %+v %v", source, err)
	}
	dealt, err := store.QueryRows(scope, &planned, "", 0)
	if err != nil || len(dealt.Rows) != 1 || !reflect.DeepEqual(dealt.Rows[0]["contacts.name"], []any{"Ada King"}) {
		t.Fatalf("native Deal's live Contact: %+v %v", dealt, err)
	}
	execSQL(`INSERT INTO contacts(id,name,version) SELECT id, 'Other '||id, 1 FROM generate_series(100,700) id`)
	narrow := data.QueryIR{Type: "contact", Where: []data.Cond{{Field: "name", Op: ":", Value: "ADA KING"}}}
	filtered, _, _, err := svc.PlanQuery(ctx, "crm", narrow)
	if err != nil {
		t.Fatalf("bounded pushdown: %v", err)
	}
	found, err := store.QueryRows(scope, &filtered, "", 0)
	if err != nil || found.Total != 1 || found.Rows[0].ID() != id {
		t.Fatalf("bounded pushdown: %+v %v", found, err)
	}
	t.Log("live still has the native edit and both original Person IDs")
}
