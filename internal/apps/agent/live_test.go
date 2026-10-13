//go:build live

package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/records"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/config"
)

// sampleRecords serves the golden CRM's sample rows, deals largest first.
type sampleRecords struct {
	svc.Records
	rows map[string][]bridge.Record
}

func newSampleRecords(t *testing.T, sample []byte) *sampleRecords {
	t.Helper()
	var raw map[string][]bridge.Record
	if err := json.Unmarshal(sample, &raw); err != nil {
		t.Fatal(err)
	}
	for typ, rows := range raw {
		for i := range rows {
			rows[i].Type = typ
		}
	}
	return &sampleRecords{rows: raw}
}

func (s *sampleRecords) Query(_ context.Context, _ svc.Scope, p bridge.QueryParams) (bridge.QueryResult, error) {
	rows := append([]bridge.Record(nil), s.rows[p.Type]...)
	sort.SliceStable(rows, func(i, j int) bool { return amount(rows[i]) > amount(rows[j]) })
	return bridge.QueryResult{Rows: rows, Total: len(rows)}, nil
}

func (s *sampleRecords) Get(_ context.Context, _ svc.Scope, p bridge.GetRecordParams) (bridge.Record, error) {
	for _, r := range s.rows[p.Type] {
		if r.ID == p.ID {
			return r, nil
		}
	}
	return bridge.Record{}, bridge.Errorf(bridge.CodeNotFound, "no such record")
}

func amount(r bridge.Record) float64 {
	v, _ := r.Fields["amount"].(float64)
	return v
}

// TestLiveCRMQuestion: go test -tags live -run TestLiveCRMQuestion -v ./internal/apps/agent/
func TestLiveCRMQuestion(t *testing.T) {
	files, sample := spec.GoldenCRM()
	bundle, err := spec.Decode(files)
	if err != nil {
		t.Fatal(err)
	}
	r := newRig(t)
	r.svc.all.Records = newSampleRecords(t, sample)
	r.svc.all.Store = fakeStore{bundle: bundle}
	r.svc.all.Model = builder.NewModel(svc.Config{}, r.svc.all)

	events := r.askWithin(t, "Which open deals are worth the most, and which company is each with?", 2*time.Minute)
	var answer strings.Builder
	for _, e := range events {
		t.Logf("%s tool=%q summary=%q", e.Kind, e.Tool, e.Summary)
		if e.Kind == bridge.AgentError {
			t.Fatalf("agent failed: %s", e.Text)
		}
		answer.WriteString(e.Text)
	}
	t.Logf("ANSWER:\n%s", answer.String())
	for _, id := range []string{"co_", "dl_"} {
		if strings.Contains(answer.String(), id) {
			t.Errorf("answer shows a record id (%s)", id)
		}
	}
	if !strings.Contains(answer.String(), "Northwind") {
		t.Error("answer does not name the companies")
	}
}

// TestLiveCRMContactChanges uses the real model and record store to prove that
// an old persona tool list cannot prevent content creation or deletion.
func TestLiveCRMContactChanges(t *testing.T) {
	if config.APIKeyAt(config.ProfilePath(config.ProfileDir(), "")) == "" {
		t.Skip("no configured provider key")
	}
	r := newRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	r.svc.ctx = ctx
	r.svc.all.Store.(fakeStore).bundle.Agent.Tools = []string{"records.query", "records.update"}
	r.svc.all.Records = records.New(svc.Config{Root: r.root}, r.svc.all)
	r.svc.all.Model = builder.NewModel(svc.Config{}, r.svc.all)
	if err := r.svc.all.Records.Seed(ctx, "crm", map[string][]bridge.Record{}); err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct {
		name, ask, tool string
		wantRows        int
	}{
		{"add-contact", "Add a contact named Morgan Vale with email morgan.vale@example.com.", "records.create", 1},
		{"delete-contact", "Delete the contact named Morgan Vale.", "records.delete", 0},
	} {
		t.Run(task.name, func(t *testing.T) {
			events := r.askWithin(t, task.ask, 2*time.Minute)
			used := false
			for _, e := range events {
				t.Logf("%s tool=%q summary=%q", e.Kind, e.Tool, e.Summary)
				if e.Kind == bridge.AgentError || strings.ContainsAny(e.Text+e.Summary, "{}") {
					t.Fatalf("bad stream event: %+v", e)
				}
				used = used || e.Tool == task.tool
			}
			rows, err := r.svc.all.Records.Query(ctx, scope, bridge.QueryParams{Type: "contact", Query: `name:"Morgan Vale"`})
			if err != nil || !used || len(rows.Rows) != task.wantRows {
				t.Fatalf("used %s = %v, rows = %+v, error = %v", task.tool, used, rows, err)
			}
		})
	}
}
