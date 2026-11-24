package serve

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

type activityAnswer struct {
	ID     string         `json:"id"`
	Events []record.Event `json:"events"`
}

func (o *wsOrg) activity(t *testing.T, who, id string, status int) []record.Event {
	t.Helper()
	var out activityAnswer
	if status != 200 {
		o.do(t, who, "GET", "/v1/workstreams/"+id+"/activity", -1, nil, status, nil)
		return nil
	}
	o.do(t, who, "GET", "/v1/workstreams/"+id+"/activity", -1, nil, status, &out)
	return out.Events
}

func said(events []record.Event) string {
	var lines []string
	for _, e := range events {
		lines = append(lines, e.Handle+" "+e.Verb)
	}
	return strings.Join(lines, "; ")
}

// THE ACTIVITY LIST NAMES BOTH, IN ORDER, with the actor the server resolved
// from each sign-in.
func TestWorkstreamActivityNamesEveryoneInOrder(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	o.do(t, "santosh", "PATCH", "/v1/workstreams/"+webhook.ID, webhook.Rev, map[string]any{"purpose": "fewer lost webhooks"}, 200, &webhook)
	o.do(t, "abir", "PUT", "/v1/policy", -1, api.PolicyPut{Group: webhook.ID}, 200, nil)
	want := "abir created workstream; abir changed sharing; santosh changed workstream; abir changed go-aheads"
	if got := said(o.activity(t, "santosh", webhook.ID, 200)); got != want {
		t.Fatalf("activity:\n got %s\nwant %s", got, want)
	}
	events := o.activity(t, "abir", webhook.ID, 200)
	if e := events[2]; e.Detail != "purpose" || e.Object != "workstream:"+webhook.ID || e.Actor.Name != "@santosh" || e.Actor.Operator {
		t.Fatalf("santosh's change: %+v", e)
	}
}

// A WORKSTREAM YOU CANNOT SEE HAS NO ACTIVITY YOU CAN READ, in its own route,
// in the audit list or on the live stream.
func TestWorkstreamActivityIsPrivateToThoseWhoSee(t *testing.T) {
	o := newWSOrg(t)
	_, launch, _, brand := o.orgScene(t)
	o.activity(t, "abir", launch.ID, 404)
	o.activity(t, "abir", brand.ID, 404)
	if got := said(o.activity(t, "mia", brand.ID, 200)); got != "mia created workstream" {
		t.Fatalf("mia's own: %s", got)
	}
	var audit struct {
		Events []record.Event `json:"events"`
	}
	o.do(t, "abir", "GET", "/v1/activity", -1, nil, 200, &audit)
	for _, e := range audit.Events {
		if e.Object == "workstream:"+brand.ID || e.Object == "workstream:"+launch.ID {
			t.Fatalf("abir's activity names someone else's workstream: %+v", e)
		}
	}
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	heard := 0
	for _, e := range events {
		if e.Kind != "activity" || !strings.Contains(string(e.Data), brand.ID) {
			continue
		}
		r := httptest.NewRequest("GET", "/v1/events", nil)
		r.RemoteAddr = "192.0.2.1:1234"
		r.Header.Set("Authorization", "Bearer "+o.tokens["abir"])
		if _, ok := o.h.liveForViewer(r, e); ok {
			t.Fatal("abir heard Design's workstream change on the live stream")
		}
		r.Header.Set("Authorization", "Bearer "+o.tokens["mia"])
		if _, ok := o.h.liveForViewer(r, e); ok {
			heard++
		}
	}
	if heard != 1 {
		t.Fatalf("mia heard %d of her own changes", heard)
	}
}

// EVERY ORGANIZATION EVENT CARRIES `by`, and the organization's own changes
// (teams, the factory's rules) are recorded with the operator as actor.
func TestOrgEventsCarryWhoAndTeamsAreRecorded(t *testing.T) {
	o := newWSOrg(t)
	webhook, _, _, _ := o.orgScene(t)
	var team api.Team
	o.do(t, "owner", "POST", "/v1/teams", -1, map[string]string{"name": "Support"}, 200, &team)
	o.do(t, "owner", "PUT", "/v1/teams/"+team.ID+"/members/abir", -1, nil, 200, nil)
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var teamBy, wsBy []string
	for _, e := range events {
		var c api.OrgChanged
		_ = json.Unmarshal(e.Data, &c)
		switch e.Kind {
		case api.TeamChangedKind:
			teamBy = append(teamBy, c.By)
		case api.EventWorkstreamChanged:
			if c.ID == webhook.ID {
				wsBy = append(wsBy, c.By)
			}
		}
	}
	if strings.Join(teamBy, ",") != "owner,owner" || strings.Join(wsBy, ",") != "abir,abir" {
		t.Fatalf("team events by %v, webhook events by %v", teamBy, wsBy)
	}
	// What the stream sends keeps by.
	r := httptest.NewRequest("GET", "/v1/events", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens["santosh"])
	for _, e := range events {
		if e.Kind == api.EventWorkstreamChanged && strings.Contains(string(e.Data), webhook.ID) {
			if out, ok := o.h.liveForViewer(r, e); ok && !strings.Contains(string(out.Data), `"by":"abir"`) {
				t.Fatalf("sent without by: %s", out.Data)
			}
		}
	}
	var audit struct {
		Events []record.Event `json:"events"`
	}
	o.do(t, "santosh", "GET", "/v1/activity?on=team:"+team.ID, -1, nil, 200, &audit)
	if got := said(audit.Events); got != "owner created team; owner added to team" {
		t.Fatalf("team record: %s", got)
	}
	if !audit.Events[0].Actor.Operator {
		t.Fatalf("a team change is the operator's: %+v", audit.Events[0])
	}
}
