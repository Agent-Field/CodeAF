package serve

import (
	"slices"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE OPERATOR CHOOSES WHERE AN ORGANIZATION WORKSTREAM'S AGENT RUNS, AND
// EVERYONE MAY READ THE CHOICE. A choice this server cannot run is not
// offered, and choosing it anyway is refused rather than saved.
func TestAgentRunnerIsTheOperatorsChoiceEveryoneReads(t *testing.T) {
	o := newWSOrg(t)
	var got api.FactorySettingsView
	o.do(t, "abir", "GET", "/v1/settings", -1, nil, 200, &got)
	if got.AgentRunner != "driver" || !slices.Equal(got.AgentRunners, []string{"driver"}) {
		t.Fatalf("abir reads %+v, want driver of [driver]", got)
	}
	driver, server := "driver", "server"
	o.do(t, "abir", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &driver}, 403, nil)
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &server}, 422, nil)
	bogus := "cloud"
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &bogus}, 422, nil)
	o.do(t, "owner", "PATCH", "/v1/settings", -1, api.PatchFactorySettings{AgentRunner: &driver}, 200, &got)
	if got.AgentRunner != "driver" || got.RunnerTimeoutSeconds == 0 {
		t.Fatalf("owner saved %+v", got)
	}
}

// ONE PERSON DRIVES A WORKSTREAM'S AGENT AT A TIME. The second person is told
// who drives it; the holder keeps it by renewing and lets go by DELETE; a hand
// nobody renews lapses; and only the holder, the owner or the operator can let
// go of it.
func TestOneDriverAtATime(t *testing.T) {
	o := newWSOrg(t)
	webhook, launch, _, _ := o.orgScene(t)
	path := "/v1/workstreams/" + webhook.ID + "/driver"

	var hand api.WorkstreamDriver
	o.do(t, "santosh", "PUT", path, -1, nil, 200, &hand)
	if hand.Handle != "santosh" || hand.TTL != api.DriverHandSeconds {
		t.Fatalf("santosh took %+v", hand)
	}
	var held api.WorkstreamDriverHeld
	o.do(t, "abir", "PUT", path, -1, nil, 409, &held)
	if held.Driver.Handle != "santosh" || held.Error != "santosh is driving this workstream's agent" {
		t.Fatalf("abir was told %+v", held)
	}
	var ws api.Workstream
	o.do(t, "abir", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &ws)
	if ws.Driver == nil || ws.Driver.Handle != "santosh" {
		t.Fatalf("abir reads driver %+v", ws.Driver)
	}
	// Someone who cannot see it learns nothing; someone who can see but not
	// write it may not drive it.
	o.do(t, "abir", "PUT", "/v1/workstreams/"+launch.ID+"/driver", -1, nil, 404, nil)
	o.do(t, "owner", "PUT", "/v1/workstreams/"+launch.ID+"/driver", -1, nil, 403, nil)

	// A renewal keeps since and moves until.
	o.now = o.now.Add(60 * time.Second)
	var renewed api.WorkstreamDriver
	o.do(t, "santosh", "PUT", path, -1, nil, 200, &renewed)
	if renewed.Since != hand.Since || renewed.Until == hand.Until {
		t.Fatalf("renewed %+v from %+v", renewed, hand)
	}
	// abir may not take it from santosh; the operator may.
	o.do(t, "mia", "DELETE", path, -1, nil, 403, nil)
	o.do(t, "owner", "DELETE", path, -1, nil, 204, nil)
	o.do(t, "abir", "PUT", path, -1, nil, 200, &hand)
	o.do(t, "abir", "DELETE", path, -1, nil, 204, nil)
	var after api.Workstream
	o.do(t, "santosh", "GET", "/v1/workstreams/"+webhook.ID, -1, nil, 200, &after)
	if after.Driver != nil {
		t.Fatalf("let go, still driven by %+v", after.Driver)
	}

	// A hand nobody renews lapses, and the next person takes it.
	o.do(t, "santosh", "PUT", path, -1, nil, 200, nil)
	o.now = o.now.Add((api.DriverHandSeconds + 1) * time.Second)
	o.do(t, "abir", "PUT", path, -1, nil, 200, &hand)
	if hand.Handle != "abir" {
		t.Fatalf("after the lapse abir got %+v", hand)
	}
}
