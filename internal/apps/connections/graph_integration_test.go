package connections_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/apppackage"
	"github.com/Agent-Field/codeaf/internal/apps/check"
	"github.com/Agent-Field/codeaf/internal/apps/connections"
	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/apps/runs"
	"github.com/Agent-Field/codeaf/internal/apps/schema"
)

const briefApp = "daily-brief"

func graphBrief(t *testing.T) (connections.Service, *data.Store, schema.App) {
	t.Helper()
	place := data.NewPlace(t.TempDir(), data.Options{NoRefresh: true})
	t.Cleanup(func() { _ = place.Close() })
	store, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/daily-brief/app.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishApp(data.AppPublication{App: briefApp, Version: 1, Metadata: fmt.Appendf(nil, `{"digest":"%x"}`, sha256.Sum256(raw)), Definition: raw}, 0); err != nil {
		t.Fatal(err)
	}
	for alias, key := range map[string]data.CommonTypeKey{"message": data.CommonMessage, "meeting": data.CommonEvent,
		"document": data.CommonDocument, "person": data.CommonPerson, "company": data.CommonOrganization} {
		if err := store.BindCommonType(key, alias); err != nil {
			t.Fatal(err)
		}
	}
	var app schema.App
	if err := json.Unmarshal(raw, &app); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("testdata/daily-brief/connection.json")
	if err != nil {
		t.Fatal(err)
	}
	var config connections.Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	svc := connections.Service{Profile: t.TempDir(), Store: func() (*data.Store, error) { return store, nil }}
	if err := svc.Register(config, ""); err != nil {
		t.Fatal(err)
	}
	return svc, store, app
}

func graphQuery(read string) connections.Query {
	return connections.Query{App: briefApp, Connection: "m365", Read: read}
}

func TestGraphBriefPublicationAndPolicy(t *testing.T) {
	svc, store, app := graphBrief(t)
	ctx := context.Background()
	records := map[string][]data.Row{}
	for _, typ := range []string{"message", "meeting", "document"} {
		ir, source, _, err := svc.PlanQuery(ctx, briefApp, data.QueryIR{Type: typ})
		if err != nil || source.Residency != "live" {
			t.Fatalf("%s source planning: %+v %v", typ, source, err)
		}
		page, err := store.QueryRows(data.Scope{App: briefApp}, &ir, "", 100)
		if err != nil || len(page.Rows) != 1 {
			t.Fatalf("%s source query: %+v %v", typ, page, err)
		}
		records[typ] = page.Rows
	}
	message, meeting, file := records["message"][0], records["meeting"][0], records["document"][0]
	if message["subject"] != "Review the launch brief" || message["body"] != nil || meeting["title"] != "Daily launch review" || file["title"] != "Launch decision log.txt" || file["text"] != nil {
		t.Fatalf("misprojected rows %v %v %v", message, meeting, file)
	}
	p, err := svc.Read(ctx, graphQuery("mail"))
	if err != nil || p.Next == "" || p.Checkpoint != "" {
		t.Fatalf("mail next: %+v %v", p, err)
	}
	other := graphQuery("files")
	other.Cursor = p.Next
	if _, err := svc.Read(ctx, other); !errors.Is(err, connections.ErrGraphCursor) {
		t.Fatalf("cursor crossed sources: %v", err)
	}
	q := graphQuery("mail")
	q.Cursor = p.Next
	p, err = svc.Read(ctx, q)
	if err != nil || p.Checkpoint == "" || p.Next != "" {
		t.Fatalf("mail checkpoint: %+v %v", p, err)
	}
	q.Cursor = p.Checkpoint
	p, err = svc.Read(ctx, q)
	if err != nil || len(p.Rows) != 1 || len(p.Removed) != 1 || p.Removed[0] != message.ID() {
		t.Fatalf("mail changes: %+v %v", p, err)
	}
	content, err := svc.ReadGraphContent(ctx, graphQuery("mail"), message.ID(), connections.ContentPerson)
	if err != nil || content.Class != schema.ClassPII || !strings.Contains(content.Text, "ready for review") {
		t.Fatalf("explicit mail content: %+v %v", content, err)
	}
	if _, err := svc.ReadGraphContent(ctx, graphQuery("mail"), message.ID(), 0); !errors.Is(err, connections.ErrSourcePermission) {
		t.Fatalf("outside harness could obtain a body: %v", err)
	}
	classes, err := svc.PersonalSourceClasses(briefApp)
	if err != nil || !slices.Equal(classes, []schema.DataClass{schema.ClassPII}) {
		t.Fatalf("model route classification: %v %v", classes, err)
	}
	content, err = svc.ReadGraphContent(ctx, graphQuery("files"), file.ID(), connections.ContentPerson)
	if err != nil || !strings.Contains(content.Text, "staged launch cohort") {
		t.Fatalf("file text: %+v %v", content, err)
	}
	q = graphQuery("transcripts")
	q.Args = connections.Arguments{"meeting": meeting.ID()}
	p, err = svc.Read(ctx, q)
	if err != nil || len(p.Rows) != 1 || p.Rows[0]["meeting"] != meeting.ID() {
		t.Fatalf("transcript link: %+v %v", p, err)
	}
	records["document"] = append(records["document"], p.Rows[0])
	content, err = svc.ReadGraphContent(ctx, q, p.Rows[0].ID(), connections.ContentPerson)
	if err != nil || !strings.HasPrefix(content.Text, "WEBVTT") {
		t.Fatalf("transcript text: %+v %v", content, err)
	}
	for _, tc := range []struct{ read, arg, value, want string }{
		{"person", "email", "MIRA@ASTERLOOM.EXAMPLE", "Mira Vennett"}, {"company", "domain", "asterloom.example", "asterloom.example"},
	} {
		q := graphQuery(tc.read)
		q.Args = connections.Arguments{tc.arg: tc.value}
		p, err := svc.Read(ctx, q)
		if err != nil || len(p.Rows) != 1 || p.Rows[0]["name"] != tc.want {
			t.Fatalf("%s identity: %+v %v", tc.read, p, err)
		}
	}
	if path := os.Getenv("CM_DESK_FIXTURE"); path != "" {
		raw, err := os.ReadFile("testdata/daily-brief/brief.json")
		if err != nil {
			t.Fatal(err)
		}
		fixture, err := json.Marshal(map[string]any{"app": app, "screens": map[string]json.RawMessage{"brief": raw}, "records": records})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, fixture, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGraphSourceTriggersAndBroker(t *testing.T) {
	svc, _, app := graphBrief(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC)
	since := now.Add(-time.Minute)
	triggers := []runs.Trigger{
		{App: briefApp, ID: "mail", On: "connector", Connector: schema.GraphSourceConnector, Event: schema.SourceMessageReceived, Match: map[string]any{"connection": "m365", "read": "mail", "query": "Launch numbers"}, Run: "brief", Since: since},
		{App: briefApp, ID: "meeting", On: "connector", Connector: schema.GraphSourceConnector, Event: schema.SourceMeetingStarting, Match: map[string]any{"connection": "m365", "read": "calendar", "minutes": float64(30)}, Run: "brief", Since: since},
		{App: briefApp, ID: "transcript", On: "connector", Connector: schema.GraphSourceConnector, Event: schema.SourceTranscriptCreated, Match: map[string]any{"connection": "m365", "read": "transcripts"}, Run: "brief", Since: since},
	}
	raw := []byte(`run: brief
title: Prepare the brief
steps:
  - id: collect
    finish: "$input.id"
`)
	brief, err := schema.ParseRun(raw)
	if err != nil {
		t.Fatal(err)
	}
	draft, err := schema.ParseRun([]byte(`run: draft
title: Draft a reply
steps:
  - id: draft
    tool: connections.m365.create-draft
    gate: {approval: send}
    args:
      set:
        to: mira@asterloom.example
        subject: "Re: Review the launch brief"
        body: "I'll review it today."
`))
	if err != nil {
		t.Fatal(err)
	}
	actions := map[string]runs.Definition{"draft": draft}
	for _, id := range []string{"create-event", "update-event"} {
		raw, err := os.ReadFile("testdata/daily-brief/runs/" + id + ".yaml")
		if err != nil {
			t.Fatal(err)
		}
		def, err := schema.ParseRun(raw)
		if err != nil {
			t.Fatal(err)
		}
		actions[id] = def
	}
	tool := runs.ConnectedTools{Sources: svc, Apps: func(string) (runs.App, error) { return app, nil }}
	cfg := runs.Config{Root: t.TempDir(), Now: func() time.Time { return now }, SourceTriggers: svc.PollGraphTrigger,
		Triggers: func() ([]runs.Trigger, error) { return triggers, nil },
		Load: func(_, id string) (runs.Definition, error) {
			if def, ok := actions[id]; ok {
				return def, nil
			}
			return brief, nil
		},
		Connector: tool, CheckTool: tool.Check}
	engine, err := runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := range 4 {
		if err := engine.PollSources(ctx); err != nil {
			t.Fatal(err)
		}
		if err := engine.Tick(ctx); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		now = now.Add(time.Minute)
	}
	if err := engine.Close(); err != nil {
		t.Fatal(err)
	}
	engine, err = runs.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close()
	now = time.Date(2026, 11, 26, 11, 2, 0, 0, time.UTC)
	for range 2 {
		if err := engine.PollSources(ctx); err != nil {
			t.Fatal(err)
		}
		if err := engine.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
	}
	list, err := engine.List(briefApp, "brief")
	if err != nil || len(list) != 3 {
		t.Fatalf("expected one run per source trigger, including after restart, got %d %v", len(list), err)
	}

	calendar, err := svc.Read(ctx, graphQuery("calendar"))
	if err != nil || len(calendar.Rows) != 1 {
		t.Fatalf("calendar: %+v %v", calendar, err)
	}
	for _, test := range []struct {
		run, version string
		params       map[string]any
	}{
		{"draft", `W/"draft-1"`, nil},
		{"create-event", `W/"event-created"`, map[string]any{"title": "Daily launch review", "start": "2026-11-26T12:30:00Z", "end": "2026-11-26T13:00:00Z"}},
		{"update-event", `W/"event-version-2"`, map[string]any{"id": calendar.Rows[0].ID(), "version": calendar.Rows[0]["_source_version"], "title": "Daily launch review moved"}},
	} {
		t.Run(test.run, func(t *testing.T) {
			run, err := engine.Start(briefApp, test.run, test.params)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			pending, err := engine.Get(run.ID)
			if err != nil || pending.State != runs.YourCall || pending.Approval == nil || len(pending.Effects) != 0 {
				t.Fatalf("before approval: %+v %v", pending, err)
			}
			if err := engine.Answer(run.ID, pending.Approval.ID, true); err != nil {
				t.Fatal(err)
			}
			if err := engine.Tick(ctx); err != nil {
				t.Fatal(err)
			}
			finished, err := engine.Get(run.ID)
			if err != nil || finished.State != runs.Done || len(finished.Effects) != 1 ||
				finished.Effects[0].Outcome != "committed" || finished.Effects[0].Version != test.version {
				t.Fatalf("Graph effect receipt: %+v %v", finished, err)
			}
		})
	}
}

func TestGraphBriefPackage(t *testing.T) {
	files := map[string]string{}
	for _, name := range []string{"app.json", "plan.json", "screens/brief.json", "runs/brief.yaml", "runs/draft.yaml", "runs/create-event.yaml", "runs/update-event.yaml"} {
		src := name
		if name == "screens/brief.json" {
			src = "brief.json"
		}
		raw, err := os.ReadFile(filepath.Join("testdata/daily-brief", src))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	definition := schema.Definition{Files: map[string][]byte{}}
	for name, raw := range files {
		definition.Files[name] = []byte(raw)
	}
	if report := check.Definition(definition, nil); !report.OK() {
		t.Fatal(report.String())
	}
	pkg, err := apppackage.Pack(files, nil, apppackage.PackOptions{Publisher: "Daily brief fixture"})
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("CM_BRIEF_PACKAGE"); path != "" {
		if err := os.WriteFile(path, pkg, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
