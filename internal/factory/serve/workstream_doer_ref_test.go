package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestTheDoerWordNamesAfiAPersonOrAnAgentAndNothingElse(t *testing.T) {
	for word, want := range map[string]doerRef{
		"": {Kind: "afi"}, "afi": {Kind: "afi"}, "this_chat": {Kind: "afi"}, "person": {Kind: "owner"},
		"person:marco": {Kind: "person", ID: "marco"}, "person:@marco": {Kind: "person", ID: "marco"},
		"agent:ag-copy": {Kind: "agent", ID: "ag-copy"}, "factory": {Kind: "factory"},
	} {
		got, err := parseDoerRef(word)
		if err != nil || got != want {
			t.Errorf("%q: %+v, %v; want %+v", word, got, err, want)
		}
	}
	for _, bad := range []string{"person:", "crew:x", "marco", "everyone"} {
		if _, err := parseDoerRef(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
}

// THE REVIEWER DOES THE REVIEW (org-space DESIGN §10.1, L7 acceptance 3). Live
// use made "Marco reviews the store text" with its owner as the doer, because
// the only person word was "person". Afi now names the reviewer: abir's review
// is abir's, owned by santosh; someone outside the workstream and an agent it
// has not added are refused, and one it has added does the task by its list name.
func TestAfiNamesWhoDoesATaskAndTheReviewLandsOnTheReviewer(t *testing.T) {
	o, path := taskOrg(t, "edit")
	ctx := context.Background()
	door := &agentDoor{h: o.h, id: strings.TrimPrefix(path, "/v1/workstreams/"), asker: "santosh"}
	create := func(doneBy string) (factory.TaskDoneBy, error) {
		line, err := door.Work(ctx, "ws_task_create", json.RawMessage(`{"title":"Review the store text","request":"Read it for tone.","goal":"The store text reads in our voice","deliverable":"Notes on the task","acceptance":["Names each off-tone line","Suggests a fix for each"],"done_by":"`+doneBy+`"}`))
		if err != nil {
			return factory.TaskDoneBy{}, err
		}
		var out struct{ Task struct{ ID string } }
		if err := json.Unmarshal([]byte(line), &out); err != nil {
			t.Fatal(err)
		}
		tk, err := door.task(ctx, out.Task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if tk.Owner.ID != "santosh" {
			t.Fatalf("owner %+v: naming the doer must not move the owner", tk.Owner)
		}
		return tk.DoneBy, nil
	}
	if d, err := create("person:abir"); err != nil || d.Kind != factory.TaskByPerson || d.ID != "abir" {
		t.Fatalf("person:abir made %+v, %v", d, err)
	}
	if _, err := create("person:mallory"); err == nil || !strings.Contains(err.Error(), "not someone in this workstream") {
		t.Fatalf("a stranger: %v", err)
	}
	if _, err := create("agent:ag-copy"); err == nil || !strings.Contains(err.Error(), "not one of this workstream's agents") {
		t.Fatalf("an agent not on the list: %v", err)
	}
	o.h.config.WorkstreamAgents = func(factory.OrgWorkstream) []WorkstreamAgent {
		return []WorkstreamAgent{{ID: "ag-copy", Name: "Copywriter"}}
	}
	if d, err := create("agent:ag-copy"); err != nil || d.Kind != factory.TaskByAgent || d.Name != "Copywriter" {
		t.Fatalf("agent:ag-copy made %+v, %v", d, err)
	}
	if d, err := create("this_chat"); err != nil || d.Kind != factory.TaskByAfi {
		t.Fatalf("the old spelling made %+v, %v", d, err)
	}
}
