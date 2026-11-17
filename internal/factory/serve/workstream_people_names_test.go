package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// LIVE L1 F4/F17, TWO PEOPLE: santosh and abir each sign up with a password
// and the name they gave. santosh makes a workstream with abir in it and a
// child inside it; abir reads both. Every person the workstream names (its
// people, its steward, its rules' default owner and fallback) carries the
// name, so a desk offers "Hand to Abir Das", never the handle "abir" beside
// "Abir Das" on a task row. A person the organization knows no name for keeps
// their handle.
func TestAWorkstreamNamesItsPeopleByTheNamesTheyJoinedWith(t *testing.T) {
	o := newPWOrg(t)
	santosh := o.signUpNamed(o.operatorLink(), "santosh", pwOperator, "Santosh Kumar")
	abir := o.signUpNamed(o.invite(santosh), "abir", "copper-meadow-47-violin", "Abir Das")

	call := func(token, method, path string, body any, out any) {
		t.Helper()
		r := o.do(method, path, token, body, "")
		if r.Code >= 300 {
			t.Fatalf("%s %s = %d %s", method, path, r.Code, r.Body)
		}
		if out != nil {
			if err := json.Unmarshal(r.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}

	var ws api.Workstream
	call(santosh, "POST", "/v1/workstreams", api.WorkstreamNew{Name: "Pantry"}, &ws)
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	var shared api.WorkstreamShared
	call(santosh, "PUT", "/v1/workstreams/"+ws.ID+"/shares", api.WorkstreamSharesPut{Shares: &shares, Rev: &ws.Rev}, &shared)
	var child api.Workstream
	call(santosh, "POST", "/v1/workstreams", map[string]any{"name": "Meal planner", "parent": ws.ID}, &child)

	for _, id := range []string{ws.ID, child.ID} {
		var seen api.Workstream
		call(abir, "GET", "/v1/workstreams/"+id, nil, &seen)
		names := map[string]string{}
		for _, m := range seen.People {
			names[m.ID] = m.Name
		}
		if names["santosh"] != "Santosh Kumar" || names["abir"] != "Abir Das" {
			t.Fatalf("%s's people as abir reads them = %+v", seen.Name, seen.People)
		}
		if seen.Steward == nil || seen.Steward.Name != "Santosh Kumar" {
			t.Fatalf("%s's steward = %+v", seen.Name, seen.Steward)
		}
		if seen.Rules.DefaultOwner.Kind == "person" && seen.Rules.DefaultOwner.Name != "Santosh Kumar" {
			t.Fatalf("%s's default owner = %+v", seen.Name, seen.Rules.DefaultOwner)
		}
	}
}

// signUpNamed is signUp with the name a person types beside their username.
func (o *pwOrg) signUpNamed(link, username, password, name string) string {
	o.t.Helper()
	out, w := o.access(api.FactoryAccessRequest{Link: link, Verb: "sign_up", Username: username, Password: password, Name: name})
	if w.Code != 200 || out.State != "joined" || out.AccessToken == "" {
		o.t.Fatalf("sign up %s = %d %s", username, w.Code, w.Body)
	}
	return out.AccessToken
}

// LIVE L1 F17 / L3 F-14, TWO PEOPLE: santosh makes a task for abir from a desk,
// which names the owner and the doer by handle alone. Read back by abir (the
// list, the one task) and in the answer to the create itself, every person on
// it carries the name they joined with, so a desk never draws "Owner undefined
// (you)" or the handle beside a name.
func TestATaskNamesItsPeopleByTheNamesTheyJoinedWith(t *testing.T) {
	o := newPWOrg(t)
	santosh := o.signUpNamed(o.operatorLink(), "santosh", pwOperator, "Santosh Kumar")
	abir := o.signUpNamed(o.invite(santosh), "abir", "copper-meadow-47-violin", "Abir Das")
	call := func(token, method, path string, body any, out any) {
		t.Helper()
		r := o.do(method, path, token, body, "")
		if r.Code >= 300 {
			t.Fatalf("%s %s = %d %s", method, path, r.Code, r.Body)
		}
		if out != nil {
			if err := json.Unmarshal(r.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	var ws api.Workstream
	call(santosh, "POST", "/v1/workstreams", api.WorkstreamNew{Name: "Pantry"}, &ws)
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	call(santosh, "PUT", "/v1/workstreams/"+ws.ID+"/shares", api.WorkstreamSharesPut{Shares: &shares, Rev: &ws.Rev}, nil)

	var made factory.WorkstreamTask
	call(santosh, "POST", "/v1/workstreams/"+ws.ID+"/tasks", map[string]any{
		"title": "Sign off the pantry redlines", "request": "Sign off if the substitutions look right.",
		"owner": map[string]string{"kind": "person", "id": "abir"}, "done_by": map[string]string{"kind": "person", "id": "abir"},
	}, &made)
	var list api.WorkstreamTasks
	call(abir, "GET", "/v1/workstreams/"+ws.ID+"/tasks", nil, &list)
	var one factory.WorkstreamTask
	call(abir, "GET", "/v1/workstreams/"+ws.ID+"/tasks/"+made.ID, nil, &one)
	if len(list.Tasks) != 1 {
		t.Fatalf("abir's list = %+v", list.Tasks)
	}
	for where, got := range map[string]factory.WorkstreamTask{"made": made, "list": list.Tasks[0], "one": one} {
		if got.Owner.Name != "Abir Das" || got.DoneBy.Name != "Abir Das" {
			t.Fatalf("%s: owner %+v, done by %+v", where, got.Owner, got.DoneBy)
		}
		if got.CameFrom.Name != "Santosh Kumar" || got.CreatedBy.Name != "Santosh Kumar" {
			t.Fatalf("%s: came from %+v, created by %+v", where, got.CameFrom, got.CreatedBy)
		}
	}
}

// E1 F10: the thread, the Inbox and Decisions read who did something from the
// event as it was written, and the server wrote the handle — "Stopped by abir",
// "Sent back by priya", comment authors "lena". Every line a person's act
// stamps now carries the name they joined with, and the task they made says it
// too.
func TestATasksThreadNamesPeopleByTheNamesTheyJoinedWith(t *testing.T) {
	o := newPWOrg(t)
	santosh := o.signUpNamed(o.operatorLink(), "santosh", pwOperator, "Santosh Kumar")
	abir := o.signUpNamed(o.invite(santosh), "abir", "copper-meadow-47-violin", "Abir Das")
	call := func(token, method, path string, body any, out any) {
		t.Helper()
		r := o.do(method, path, token, body, "")
		if r.Code >= 300 {
			t.Fatalf("%s %s = %d %s", method, path, r.Code, r.Body)
		}
		if out != nil {
			if err := json.Unmarshal(r.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}
	var ws api.Workstream
	call(santosh, "POST", "/v1/workstreams", api.WorkstreamNew{Name: "Pantry"}, &ws)
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: "edit"}}
	call(santosh, "PUT", "/v1/workstreams/"+ws.ID+"/shares", api.WorkstreamSharesPut{Shares: &shares, Rev: &ws.Rev}, nil)
	path := "/v1/workstreams/" + ws.ID
	var made api.WorkstreamTask
	call(abir, "POST", path+"/tasks", api.TaskCreate{Title: "Run five user interviews", DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: "abir"}}, &made)
	if made.CreatedBy.Name != "Abir Das" {
		t.Fatalf("created by %+v, want Abir Das", made.CreatedBy)
	}
	call(abir, "POST", path+"/tasks/"+made.ID+"/comment", map[string]any{"text": "Booked three so far."}, nil)
	var thread api.WorkstreamTaskEvents
	call(santosh, "GET", path+"/tasks/"+made.ID+"/events", nil, &thread)
	if len(thread.Events) == 0 {
		t.Fatal("no thread")
	}
	for _, e := range thread.Events {
		if e.By.Kind == factory.TaskByPerson && e.By.Name == "abir" {
			t.Fatalf("a line names the handle: %+v", e)
		}
	}
	last := thread.Events[len(thread.Events)-1]
	if last.By.Name != "Abir Das" {
		t.Fatalf("the comment's author is %+v, want Abir Das", last.By)
	}
}

// E1 F10: a rule saved with only a handle recorded "marco answers what waits
// unanswered for a day" in Decisions. The decision names the person.
func TestARuleDecisionNamesThePersonNotTheHandle(t *testing.T) {
	o := newPWOrg(t)
	santosh := o.signUpNamed(o.operatorLink(), "santosh", pwOperator, "Santosh Kumar")
	o.signUpNamed(o.invite(santosh), "abir", "copper-meadow-47-violin", "Abir Das")
	before := factory.WorkstreamRules{}
	after := factory.WorkstreamRules{
		DefaultOwner: factory.RulesOwner{Kind: factory.RulesOwnerPerson, ID: "abir"},
		Fallback:     &factory.RulesPerson{ID: "abir", Name: "abir"},
	}
	got := o.h.ruleDecisions(before, after, Viewer{Handle: "santosh"})
	if len(got) < 2 {
		t.Fatalf("decisions %+v", got)
	}
	for _, d := range got {
		if !strings.HasPrefix(d.Text, "Abir Das ") {
			t.Errorf("decision %q does not name Abir Das", d.Text)
		}
	}
	if after.Fallback.Name != "abir" {
		t.Error("the caller's rules were changed")
	}
}
