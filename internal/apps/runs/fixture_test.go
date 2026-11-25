package runs_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/runs"
)

func TestCRMFollowUpOnSampleData(t *testing.T) {
	contents, err := os.ReadFile("testdata/crm/follow-up.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var writes []runs.RecordCall
	connectors := runs.NewFakeConnector(map[string]runs.Reply{
		"gmail.search": {Output: "latest thread"},
		"gmail.send":   {Output: "sent"},
	})
	e := newEngine(t, string(contents), runs.Config{
		Connector: connectors,
		Agent: func(_ context.Context, c runs.AgentCall) (runs.Reply, error) {
			if !strings.Contains(c.Prompt, "Northstar upgrade") {
				t.Fatalf("prompt = %q", c.Prompt)
			}
			return runs.Reply{Output: "sample draft"}, nil
		},
		Records: func(_ context.Context, c runs.RecordCall) (runs.Reply, error) {
			writes = append(writes, c)
			return runs.Reply{}, nil
		},
	})
	r, err := e.Start("crm", "follow-up", map[string]any{
		"deals": []any{map[string]any{
			"type": "deal", "id": "d1", "name": "Northstar upgrade",
			"contacts": []any{map[string]any{"email": "buyer@example.test"}},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	pending := state(t, e, r.ID)
	if pending.Approval == nil || pending.Step != "send" || pending.Approval.Args["draft"] != "sample draft" {
		t.Fatalf("approval = %+v", pending)
	}
	if err := e.Answer(r.ID, pending.Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if calls := connectors.Calls(); len(calls) != 2 || calls[0].Args["q"] != "to:buyer@example.test newer_than:30d" {
		t.Fatalf("connector calls = %+v", calls)
	}
	if len(writes) != 1 || writes[0].Type != "deal" || writes[0].IDs[0] != "d1" || state(t, e, r.ID).State != runs.Done {
		t.Fatalf("record writes = %+v", writes)
	}
}
