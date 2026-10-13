//go:build live

package agent

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/builder"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
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

	events := r.ask(t, "Which open deals are worth the most, and which company is each with?")
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
