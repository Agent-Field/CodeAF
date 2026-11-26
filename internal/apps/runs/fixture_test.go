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

func TestCRMWonTriggerDraftsThenWaitsForSlackApproval(t *testing.T) {
	contents, err := os.ReadFile("testdata/crm/won-handoff.yaml")
	if err != nil {
		t.Fatal(err)
	}
	fake := runs.NewFakeConnector(map[string]runs.Reply{"slack.post": {Output: "posted"}})
	e := newEngine(t, string(contents), runs.Config{
		Connector: fake,
		Triggers: func() ([]runs.Trigger, error) {
			return []runs.Trigger{{App: "crm", ID: "won", On: "record.changed", Run: "won-handoff", Type: "deal",
				When: "changed('stage') && stage == 'Won'"}}, nil
		},
		Agent: func(_ context.Context, c runs.AgentCall) (runs.Reply, error) {
			if !strings.Contains(c.Prompt, "Northstar upgrade at Northstar") {
				t.Fatalf("prompt = %s", c.Prompt)
			}
			return runs.Reply{Output: "Handoff ready"}, nil
		},
	})
	err = e.RecordChanged(runs.Change{
		Type: "deal", ID: "d1", Before: map[string]any{"stage": "Proposal"},
		After: map[string]any{"stage": "Won", "name": "Northstar upgrade", "company": map[string]any{"name": "Northstar"}, "amount": 2000},
	})
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	list, err := e.List("crm", "won-handoff")
	if err != nil || len(list) != 1 || list[0].Approval == nil || list[0].Approval.Tool != "slack.post" {
		t.Fatalf("runs = %+v, %v", list, err)
	}
	if len(fake.Calls()) != 0 {
		t.Fatal("Slack was called before approval")
	}
}

func TestRunDefinitionRefusesShellAndRepositoryWorkspace(t *testing.T) {
	for _, step := range []string{
		"shell: pwd",
		"workspace: repo\n    agent: Example",
		"wait: {for: gmail.reply, thread: thread, timeout: 1h}",
	} {
		_, err := runs.Parse([]byte("run: example\nsteps:\n  - id: rejected\n    " + step))
		if err == nil {
			t.Errorf("accepted unsupported step %q", step)
		}
	}
}

func TestDraftBindsTheContactAndOwnerBeforeAskingForApproval(t *testing.T) {
	const flow = `run: follow-up
params:
  deal: {type: ref, to: deal}
steps:
  - id: draft
    agent: CRM
    prompt: "Hi {{params.deal.contact.name}}, following up on {{params.deal.name}}. From {{owner.name}}."
  - id: save
    gate: {approval: proof, who: $me}
    records.create: {type: note, set: {body: "$steps.draft.output"}}
`
	var written bool
	e := newEngine(t, flow, runs.Config{
		OwnerName: "Morgan Example",
		Agent: func(_ context.Context, c runs.AgentCall) (runs.Reply, error) {
			return runs.Reply{Output: c.Prompt}, nil
		},
		Records: func(_ context.Context, _ runs.RecordCall) (runs.Reply, error) {
			written = true
			return runs.Reply{}, nil
		},
	})
	r, err := e.Start("crm", "follow-up", map[string]any{"deal": map[string]any{
		"type": "deal", "id": "d1", "name": "Acme renew", "contact": map[string]any{"name": "Ada Example"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	waiting := state(t, e, r.ID)
	const note = "Hi Ada Example, following up on Acme renew. From Morgan Example."
	if written || waiting.Approval == nil || waiting.Approval.Args["body"] != note {
		t.Fatalf("before approving: written %v, approval %+v", written, waiting.Approval)
	}
	if err := e.Answer(r.ID, waiting.Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	if !written || state(t, e, r.ID).State != runs.Done {
		t.Fatal("the approved note was not saved")
	}
}

func TestStandaloneApprovalStaysDoneAfterTheRunFinishes(t *testing.T) {
	e := newEngine(t, "run: example\nsteps:\n  - id: approve\n    gate: {approval: proof}\n", runs.Config{})
	r := start(t, e)
	tick(t, e)
	waiting := state(t, e, r.ID)
	if waiting.Approval == nil || waiting.Steps[0].State != runs.YourCall {
		t.Fatalf("waiting for approval: %+v", waiting)
	}
	if err := e.Answer(r.ID, waiting.Approval.ID, true); err != nil {
		t.Fatal(err)
	}
	tick(t, e)
	done := state(t, e, r.ID)
	if done.State != runs.Done || done.Steps[0].State != runs.Done {
		t.Fatalf("approved run: state %q, step %q", done.State, done.Steps[0].State)
	}
}
