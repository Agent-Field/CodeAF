package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/data"
	"github.com/Agent-Field/codeaf/internal/appwire"
)

func TestAppRuleWireMatchesAgentWriter(t *testing.T) {
	root := resolvedTempDir(t)
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("CODEAF_PROFILE_DIR", filepath.Join(root, "profile"))
	place := data.NewPlace(filepath.Join(root, "state"), data.Options{NoRefresh: true})
	s, err := place.Main()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = place.Close() })
	raw, err := os.ReadFile(filepath.Join("testdata", "fluidapps", "crm-app.json"))
	if err != nil {
		t.Fatal(err)
	}
	var def map[string]json.RawMessage
	if err := json.Unmarshal(raw, &def); err != nil {
		t.Fatal(err)
	}
	def["process"] = json.RawMessage(`{
		"lifecycles":{"deal":{"field":"stage","initial":"Lead","transitions":{"qualify":{"from":["Lead"],"to":"Qualified","actors":["person","agent"]}}}},
		"work_items":{"follow_up":{"title":"Follow up","subject":{"type":"deal","id":"$record.id"},"assignee":"me","form":"follow_up"}}
	}`)
	def["roles"] = json.RawMessage(`{"finance":{"kind":"approver","label":"Finance"}}`)
	def["rules"] = json.RawMessage(`{
		"follow_up":{"kind":"assignment","record":"deal","strategy":"field","field":"owner","workItem":"follow_up","transition":"qualify"},
		"finance":{"kind":"approval-routing","record":"deal","order":"serial","roles":["finance"],"transition":"qualify","when":"amount >= 10000","makerCannotSign":true}
	}`)
	def["resources"] = json.RawMessage(`{"follow_up_slots":{"label":"Follow-up slots","booking":{"type":"deal","resource":"owner","day":"close_on","maxPerDay":1}}}`)
	raw, err = json.Marshal(def)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RegisterApp(raw); err != nil {
		t.Fatal(err)
	}

	agent := data.Scope{App: "crm", Agent: true}
	values := map[string]any{"name": "Agent's deal", "amount": 10000, "owner": "me", "close_on": "2099-01-01"}
	made, err := s.Create(agent, "deal", []map[string]any{values})
	if err != nil {
		t.Fatal(err)
	}
	agentRecord := made.Rows[0].ID()
	if _, err := s.Transition(agent, "deal", agentRecord, "qualify", data.TransitionOptions{Rev: 1}); err != nil {
		t.Fatal(err)
	}
	_, agentErr := s.Create(agent, "deal", []map[string]any{values})
	refusal, ok := data.AsError(agentErr)
	if !ok || refusal.Code != data.CodeBookingConflict {
		t.Fatalf("agent booking refusal = %v", agentErr)
	}
	if err := place.Close(); err != nil {
		t.Fatal(err)
	}

	p := startOneEngine(t, filepath.Join(root, "state"), "ordinary")
	p.hello(t)
	next := 0
	call := func(method string, params any, result any) *appwire.Error {
		t.Helper()
		next++
		frame, err := json.Marshal(map[string]any{"id": next, "method": method, "params": params})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fmt.Fprintln(p.stdin, string(frame)); err != nil {
			t.Fatal(err)
		}
		deadline := time.NewTimer(10 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case line := <-p.lines:
				var reply actionFrame
				if json.Unmarshal([]byte(line), &reply) != nil || reply.ID != next {
					continue
				}
				if reply.Error == nil && result != nil {
					if err := json.Unmarshal(reply.Result, result); err != nil {
						t.Fatal(err)
					}
				}
				return reply.Error
			case <-p.done:
				t.Fatalf("engine closed during %s: %s", method, p.stderr.String())
			case <-deadline.C:
				t.Fatalf("timed out waiting for %s", method)
			}
		}
	}
	scope := appwire.DataScope{App: "crm"}
	create := appwire.DataCreateParams{DataScope: scope, Type: "deal", Set: values}
	if e := call("data.create", create, nil); e == nil || e.Code != refusal.Code || e.Message != refusal.Message || !strings.Contains(e.Message, agentRecord) {
		t.Fatalf("wire refusal = %+v, agent refusal = %+v", e, refusal)
	}
	values["name"], values["close_on"] = "Desk's deal", "2099-01-02"
	var madeOnWire appwire.DataWriteResult
	if e := call("data.create", create, &madeOnWire); e != nil {
		t.Fatal(e)
	}
	deskRecord := madeOnWire.Rows[0]["id"].(string)
	var moved appwire.DataWriteResult
	if e := call("data.transition", appwire.DataTransitionParams{DataScope: scope, Type: "deal", ID: deskRecord, Transition: "qualify", Rev: 1}, &moved); e != nil {
		t.Fatal(e)
	}

	for _, id := range []string{deskRecord, agentRecord} {
		record := appwire.DataRecordRef{Type: "deal", ID: id}
		var view appwire.DataProcessResult
		if e := call("data.process", appwire.ProcessParams{DataScope: scope, Record: record}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Rev != 2 || view.State != "Qualified" || len(view.WorkItems) != 1 ||
			view.WorkItems[0]["title"] != "Follow up" || view.Approval == nil || view.Approval.Rule != "finance" ||
			len(view.Approval.Steps) != 1 || view.Approval.Steps[0].Ready ||
			!strings.Contains(view.Approval.Steps[0].Blocked, "second person") {
			t.Fatalf("process for %s = %+v", id, view)
		}
		var reasons []string
		for _, line := range view.Timeline {
			if line.Kind == "rule" {
				reasons = append(reasons, line.Why)
			}
		}
		if len(reasons) != 4 || !strings.Contains(strings.Join(reasons, " "), "field match") {
			t.Fatalf("rule reasons for %s = %v", id, reasons)
		}
		if e := call("data.signoffs.create", appwire.DataSignParams{DataScope: scope, Type: "deal", ID: id, Statement: "I approve this deal."}, nil); e == nil || e.Code != appwire.CodeNotAllowed || !strings.Contains(e.Message, "creator") {
			t.Fatalf("maker refusal for %s = %v", id, e)
		}
	}
	p.leave(t)
}
