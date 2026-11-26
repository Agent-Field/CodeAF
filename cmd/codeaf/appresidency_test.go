package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppResidencyCLIAndWireParity(t *testing.T) {
	dsn := residencyFixturePostgres(t)
	b, events := dataBackend(t)
	b.profileDir = t.TempDir()
	store, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Publication("crm"); err != nil {
		t.Fatal(err)
	} else if !found {
		def, err := os.ReadFile(filepath.Join("testdata", "fluidapps", "crm-app.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := store.PublishApp(data.AppPublication{App: "crm", Version: 1, Definition: def, Metadata: []byte("{}")}, 0); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.BindCommonType(data.CommonPerson, "contact"); err != nil {
		t.Fatal(err)
	}
	svc := b.appConnections()
	err = svc.Register(connections.Config{App: "crm", ID: "directory", Kind: connections.Postgres, Reads: map[string]connections.Read{
		"contacts": {Type: "contact", Table: "contacts", Key: "id", Version: "version", OrderBy: "name",
			Fields: map[string]string{"name": "name"}, Residency: &connections.ResidencyConfig{
				UpdatedAt: "updated_at", Tombstone: "deleted", FreshForSeconds: 30,
			}},
	}}, dsn)
	if err != nil {
		t.Fatal(err)
	}
	wire := newActionWire(t, b)
	params := map[string]any{"app": "crm", "type": "contact"}
	checkParity := func(method string, args ...string) []byte {
		t.Helper()
		var out bytes.Buffer
		if err := appResidencyCommand(b, args, &out); err != nil {
			t.Fatal(err)
		}
		raw, err := wire.call(method, params)
		if err != nil || !bytes.Equal(bytes.TrimSpace(out.Bytes()), bytes.TrimSpace(raw)) {
			t.Fatalf("CLI %s and wire %s differ: %v", out.Bytes(), raw, err)
		}
		return raw
	}
	checkParity("residency.show", "show", "crm", "contact")

	query := func(want string) appwire.DataQueryResult {
		t.Helper()
		raw, err := wire.call("data.query", map[string]any{
			"app": "crm", "type": "contact", "query": "sort:name", "select": []string{"name"},
		})
		var page appwire.DataQueryResult
		if err != nil || json.Unmarshal(raw, &page) != nil || page.Total != 1 || len(page.Rows) != 1 ||
			page.Rows[0]["name"] != "Ada" || page.Source == nil || page.Source.Residency != want {
			t.Fatalf("query %s: %s %v", want, raw, err)
		}
		return page
	}
	live := query("live")
	watch := appwire.DataSubscribeParams{DataScope: appwire.DataScope{App: "crm"}, Sub: "contacts", Type: "contact", Query: "sort:name"}
	initial, err := b.DataSubscribe(watch)
	if err != nil || initial.Snapshot != nil {
		t.Fatalf("source watch leaked a snapshot: %+v %v", initial, err)
	}
	svc.Refresh(t.Context())
	events.waitFor(t, 5*time.Second, func(event string, payload any) bool {
		reset, ok := payload.(appwire.DataReset)
		return event == appwire.EventDataReset && ok && reset.Sub == watch.Sub && reset.Cursor > initial.Cursor
	})

	params["to"] = "mirror"
	var plan data.ResidencyPlan
	raw := checkParity("residency.preview", "preview", "crm", "contact", "mirror")
	if json.Unmarshal(raw, &plan) != nil || plan.Rows != 1 || plan.Confirmation == "" {
		t.Fatalf("preview: %s", raw)
	}
	var cli bytes.Buffer
	if err := appResidencyCommand(b, []string{"apply", "crm", "contact", "mirror"}, &cli); err == nil {
		t.Fatal("CLI applied without confirmation")
	}
	params["confirmation"] = "unrelated-preview"
	if _, err := wire.call("residency.cutover", params); err == nil {
		t.Fatal("wire applied with an unrelated confirmation")
	}
	if err := appResidencyCommand(b, []string{"apply", "crm", "contact", "mirror", "--confirm", plan.Confirmation}, &cli); err != nil {
		t.Fatal(err)
	}
	mirror := query("mirror")
	if mirror.Rows[0]["id"] != live.Rows[0]["id"] || mirror.Rows[0]["_source_version"] != "1" {
		t.Fatalf("mirror changed the Person or source revision: %+v", mirror.Rows[0])
	}
	params["to"] = "live"
	delete(params, "confirmation")
	raw = checkParity("residency.preview", "preview", "crm", "contact", "live")
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	params["confirmation"] = plan.Confirmation
	if _, err := wire.call("residency.cutover", params); err != nil {
		t.Fatal(err)
	}
	if restored := query("live"); restored.Rows[0]["id"] != live.Rows[0]["id"] {
		t.Fatal("wire rollback changed the Person")
	}
}

func residencyFixturePostgres(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is absent")
	}
	if err := exec.CommandContext(ctx, "docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		t.Skip("Docker daemon is unavailable")
	}
	container, err := exec.CommandContext(ctx, "docker", "run", "--rm", "-d", "-p", "127.0.0.1::5432",
		"-e", "POSTGRES_PASSWORD=residency-fixture", "postgres:17-alpine").Output()
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSpace(string(container))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })
	port, err := exec.CommandContext(ctx, "docker", "port", id, "5432/tcp").Output()
	if err != nil {
		t.Fatal(err)
	}
	dsn := "postgres://postgres:residency-fixture@" + strings.TrimSpace(string(port)) + "/postgres?sslmode=disable"
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for db.PingContext(ctx) != nil {
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, statement := range []string{
		`CREATE TABLE contacts(id BIGINT PRIMARY KEY, name TEXT NOT NULL, version BIGINT NOT NULL,
			updated_at TIMESTAMPTZ DEFAULT clock_timestamp(), deleted BOOLEAN NOT NULL DEFAULT false)`,
		`INSERT INTO contacts(id,name,version) VALUES(7,'Ada',1)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	return dsn
}
