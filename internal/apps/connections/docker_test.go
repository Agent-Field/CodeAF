package connections_test

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/connections"
)

func TestPostgresCRMReadAndBrokeredWrite(t *testing.T) {
	testSQLCRM(t, connections.Postgres, "postgres:17-alpine", "pgx",
		5432, []string{"POSTGRES_PASSWORD=fixture-password", "POSTGRES_DB=crm"},
		func(address string) string {
			return "postgres://postgres:fixture-password@" + address + "/crm?sslmode=disable"
		})
}

func TestMySQLCRMReadAndBrokeredWrite(t *testing.T) {
	testSQLCRM(t, connections.MySQL, "mysql:8.4", "mysql",
		3306, []string{"MYSQL_ROOT_PASSWORD=fixture-root", "MYSQL_DATABASE=crm", "MYSQL_USER=crm", "MYSQL_PASSWORD=fixture-password"},
		func(address string) string { return "crm:fixture-password@tcp(" + address + ")/crm" })
}

func testSQLCRM(t *testing.T, kind connections.Kind, image, driver string, port int, env []string, dsnFor func(string) string) {
	t.Helper()
	endpoint := startDatabase(t, image, port, env)
	dsn := dsnFor(endpoint)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal("could not open the test database")
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for {
		probe, stop := context.WithTimeout(ctx, time.Second)
		err := db.PingContext(probe)
		stop()
		if err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("test database did not accept connections within 90 seconds")
		case <-time.After(250 * time.Millisecond):
		}
	}
	create := "CREATE TABLE contacts (id BIGINT PRIMARY KEY, name VARCHAR(200) NOT NULL, title VARCHAR(200), email VARCHAR(254), phone VARCHAR(40), version BIGINT NOT NULL)"
	if kind == connections.MySQL {
		create += " ENGINE=InnoDB"
	}
	for _, statement := range []string{
		create,
		`INSERT INTO contacts (id, name, title, email, phone, version) VALUES (41, 'Grace', 'Engineer', 'grace@example.test', '555-1234', 1)`,
		`INSERT INTO contacts (id, name, title, email, phone, version) VALUES (42, 'Ada', 'Analyst', 'ada@example.test', '555-2345', 1)`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}

	svc, _ := crm(t)
	cfg := connections.Config{ID: "directory", Kind: kind, App: "crm",
		Reads: map[string]connections.Read{"contacts": {
			Type: "contact", Table: "contacts", Key: "id", Version: "version", OrderBy: "name",
			Fields: map[string]string{"name": "name", "title": "title", "email": "email", "phone": "phone"},
			Match:  map[string]string{"name": "name"},
		}},
		Writes: map[string]connections.Write{"edit": {Read: "contacts", Fields: []string{"name"}}},
	}
	if err := svc.Register(cfg, dsn); err != nil {
		t.Fatal(err)
	}
	if err := svc.Test(ctx, "crm", "directory"); err != nil {
		t.Fatal(err)
	}
	query := contactsQuery()
	query.Limit = 1
	first, err := svc.Read(ctx, query)
	if err != nil || len(first.Rows) != 1 || !first.HasMore ||
		first.Rows[0]["name"] != "Ada" || first.Rows[0]["_source_version"] != "1" ||
		first.Rows[0]["phone"] != "••••" || first.Source.Connection != "directory" ||
		first.Source.ObservedAt.IsZero() || first.Source.Residency != "live" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	query.Args = map[string]any{"name": "O'Reilly"}
	empty, err := svc.Read(ctx, query)
	if err != nil || len(empty.Rows) != 0 || empty.Source.ObservedAt.IsZero() {
		t.Fatalf("empty page: %+v %v", empty, err)
	}
	query.Args["name"] = "Grace"
	filtered, err := svc.Read(ctx, query)
	if err != nil || len(filtered.Rows) != 1 || filtered.Rows[0]["name"] != "Grace" || filtered.HasMore {
		t.Fatalf("filtered page: %+v %v", filtered, err)
	}
	runCfg := crmRunConfig(t, svc)
	id := first.Rows[0].ID()
	run := startEdit(t, runCfg, id, "1", func() {
		query.Args["name"] = "Ada"
		page, err := svc.Read(ctx, query)
		if err != nil || len(page.Rows) != 1 || page.Rows[0]["_source_version"] != "1" {
			t.Fatalf("the contact changed before approval: %+v %v", page, err)
		}
	})
	checkReceipt(t, run, "committed")
	if run.Effects[0].Version != "2" {
		t.Fatalf("receipt version: %+v", run.Effects[0])
	}
	stale := startEdit(t, runCfg, id, "1", func() {})
	checkReceipt(t, stale, "conflict")
	query.Args["name"] = "Ada Lovelace"
	edited, err := svc.Read(ctx, query)
	if err != nil || len(edited.Rows) != 1 || edited.Rows[0].ID() != id ||
		edited.Rows[0]["name"] != "Ada Lovelace" || edited.Rows[0]["_source_version"] != "2" {
		t.Fatalf("edited page: %+v %v", edited, err)
	}
}

func startDatabase(t *testing.T, image string, port int, env []string) string {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker is absent")
	}
	ping, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ping, "docker", "info", "--format", "{{.ServerVersion}}").Run(); err != nil {
		t.Skip("Docker daemon is unavailable")
	}
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Minute)
	defer stop()
	exposed := fmt.Sprintf("%d/tcp", port)
	args := []string{"run", "--rm", "--detach", "--publish", "127.0.0.1::" + exposed}
	for _, setting := range env {
		args = append(args, "--env", setting)
	}
	out, err := exec.CommandContext(ctx, "docker", append(args, image)...).Output()
	if err != nil {
		t.Fatalf("could not start %s: %v", image, err)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = exec.CommandContext(cleanup, "docker", "rm", "-f", id).Run()
	})
	addr, err := exec.CommandContext(ctx, "docker", "port", id, exposed).Output()
	if err != nil {
		t.Fatalf("could not find the %s port: %v", image, err)
	}
	return strings.TrimSpace(string(addr))
}
