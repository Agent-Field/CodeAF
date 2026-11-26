package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestMicrosoftBriefInstallsAndReadsFixtures(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	b := newAppBackend(newAppRecorder(), refuseAppOpen)
	t.Cleanup(func() { _ = b.Close() })
	jobs, err := b.AppBuilds()
	if err != nil {
		t.Fatal(err)
	}
	fixture := "../../internal/apps/connections/testdata/daily-brief"
	files := map[string]string{}
	for _, name := range []string{"app.json", "plan.json", "screens/brief.json", "runs/brief.yaml", "runs/create-event.yaml", "runs/update-event.yaml", "runs/draft.yaml"} {
		file := name
		if file == "screens/brief.json" {
			file = filepath.Base(file)
		}
		raw, err := os.ReadFile(filepath.Join(fixture, file))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	packed, err := apppackage.Pack(files, nil, apppackage.PackOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := apppackage.Read(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := jobs.Install(t.Context(), archive)
	if err != nil {
		t.Fatal(err)
	}
	app, err := jobs.Apply(proposal.Draft)
	if err != nil {
		t.Fatal(err)
	}
	store, err := b.dataStore("")
	if err != nil {
		t.Fatal(err)
	}
	for alias, key := range map[string]data.CommonTypeKey{"message": data.CommonMessage, "meeting": data.CommonEvent,
		"document": data.CommonDocument, "person": data.CommonPerson, "company": data.CommonOrganization} {
		typ, err := store.Type(data.Scope{App: app.ID}, alias)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.BindCommonType(key, typ.ID); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(filepath.Join(fixture, "connection.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var c connections.Config
	if err := decodeAppConnectionJSON(f, &c); err != nil {
		t.Fatal(err)
	}
	c.App = app.ID
	sources := b.appConnections()
	if err := sources.Register(c, ""); err != nil {
		t.Fatal(err)
	}
	p, err := sources.Read(context.Background(), connections.Query{App: app.ID, Connection: c.ID, Read: "mail"})
	if err != nil || len(p.Rows) != 1 || p.Rows[0]["subject"] != "Review the launch brief" || p.Next == "" {
		t.Fatalf("kept Brief source: %+v %v", p, err)
	}
}
