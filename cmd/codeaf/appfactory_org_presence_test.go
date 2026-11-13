package main

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// THE HEARTBEAT AND THE ACTIVITY LIST ARE ONE REQUEST EACH to the
// organization, built from checked ids, and the engine says so in hello.
func TestOrgPresenceAndActivityForwardToTheServer(t *testing.T) {
	o, wire, id := attachOrg(t)
	if !strings.Contains(wire.hello, `"org_presence"`) {
		t.Fatalf("hello lacks org_presence: %s", wire.hello)
	}
	var here appwire.OrgPresence
	wire.must("factory.org_presence", map[string]any{"connection": id, "item": map[string]string{"kind": "workstream", "id": "ws_1"},
		"part": "pg_retry", "editing": true}, &here)
	if here.Item != (appwire.OrgPresenceItem{Kind: "workstream", ID: "ws_1"}) ||
		!slices.Equal(here.Here, []appwire.OrgPresenceHere{{Handle: "santosh", Part: "pg_retry", Editing: true}}) {
		t.Fatalf("presence answered %+v", here)
	}
	var act appwire.OrgActivity
	wire.must("factory.org_workstream_activity", map[string]any{"connection": id, "id": "ws_1", "limit": 20}, &act)
	if len(act.Events) != 1 || act.Events[0].Handle != "abir" || act.Events[0].Verb != "changed workstream" {
		t.Fatalf("activity answered %+v", act)
	}
	if got := o.since(); !slices.Equal(got, []string{"POST /v1/presence", "GET /v1/workstreams/ws_1/activity?limit=20"}) {
		t.Fatalf("the server was asked %v", got)
	}
	wire.mustFail("factory.org_presence", map[string]any{"connection": id, "item": map[string]string{"kind": "spaceship", "id": "x"}}, "bad_params")
	wire.mustFail("factory.org_presence", map[string]any{"connection": id, "item": map[string]string{"kind": "workstream", "id": "../me"}}, "bad_params")
	wire.mustFail("factory.org_workstream_activity", map[string]any{"connection": id, "id": "ws_1", "since": "yesterday"}, "bad_params")
	if got := o.since(); len(got) != 0 {
		t.Fatalf("a refused call reached the server: %v", got)
	}
}

// WHO IS HERE, AND WHO CHANGED IT, REACH THE DESK: a presence event is relayed
// as factory.org_presence with the connection, and an organization change
// carries the person who made it.
func TestOrgPresenceAndByAreRelayed(t *testing.T) {
	o, wire, id, got := orgRelay(t)
	present := make(chan appwire.FactoryOrgPresence, 4)
	h := wire.backend.factories
	h.mu.Lock()
	h.orgPresence = func(e appwire.FactoryOrgPresence) { present <- e }
	h.mu.Unlock()
	var teams appwire.OrgTeams
	wire.must("factory.teams", map[string]any{"connection": id}, &teams)
	nextOpening(t, o)
	o.frames <- "id: 3\nevent: presence\ndata: {\"item\":{\"kind\":\"workstream\",\"id\":\"ws_2\"},\"here\":[{\"handle\":\"santosh\",\"part\":\"pg_1\",\"editing\":true}]}\n\n"
	o.frames <- "id: 4\nevent: workstream.changed\ndata: {\"kind\":\"workstream.changed\",\"id\":\"ws_2\",\"by\":\"abir\"}\n\n"
	select {
	case e := <-present:
		if e.Connection != id || e.Item.ID != "ws_2" || !slices.Equal(e.Here, []appwire.OrgPresenceHere{{Handle: "santosh", Part: "pg_1", Editing: true}}) {
			t.Fatalf("relayed presence %+v", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no factory.org_presence arrived")
	}
	if e := nextOrgEvent(t, got); e != (appwire.FactoryOrgChanged{Connection: id, Kind: "workstream.changed", ID: "ws_2", By: "abir"}) {
		t.Fatalf("relayed %+v", e)
	}
}
