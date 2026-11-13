package serve

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

// teamsFixture is a team organization whose operator holds operator-secret,
// with priya and raj signed in as members.
func teamsFixture(t *testing.T) (f *peopleV2Fixture, priya, raj string) {
	t.Helper()
	f = peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "priya"}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	priya = signInHandle(t, f.h, g, &f.now, "priya", "operator-secret")
	raj = signInHandle(t, f.h, g, &f.now, "raj", "operator-secret")
	return f, priya, raj
}

// ownsStub answers a fixed ownership for every team.
type ownsStub map[string]bool

func (o ownsStub) TeamOwnsWorkstream(team string) (bool, error) { return o[team], nil }

func TestOnlyTheOperatorShapesTeamsAndEveryMemberReadsThem(t *testing.T) {
	f, priya, raj := teamsFixture(t)
	const op = "operator-secret"

	var platform api.Team
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "  Platform  "}, 200, &platform)
	if !strings.HasPrefix(platform.ID, "tm_") || len(platform.ID) != len("tm_")+26 || platform.Name != "Platform" ||
		platform.Members == nil || len(platform.Members) != 0 || platform.CreatedAt.IsZero() {
		t.Fatalf("created: %+v", platform)
	}
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "platform"}, 409, nil)
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "  "}, 422, nil)
	f.request(t, "POST", "/v1/teams", priya, api.TeamName{Name: "Growth"}, 403, nil)

	var growth api.Team
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "Growth"}, 200, &growth)
	f.request(t, "PATCH", "/v1/teams/"+growth.ID, op, api.TeamName{Name: "PLATFORM"}, 409, nil)
	f.request(t, "PATCH", "/v1/teams/"+growth.ID, priya, api.TeamName{Name: "Sales"}, 403, nil)
	f.request(t, "PATCH", "/v1/teams/tm_nope", op, api.TeamName{Name: "Sales"}, 404, nil)

	// Membership only for people the organization already has.
	var got api.Team
	f.request(t, "PUT", "/v1/teams/"+platform.ID+"/members/Priya", op, nil, 200, &got)
	f.request(t, "PUT", "/v1/teams/"+platform.ID+"/members/raj", op, nil, 200, &got)
	if strings.Join(got.Members, ",") != "priya,raj" {
		t.Fatalf("members: %+v", got)
	}
	f.request(t, "PUT", "/v1/teams/"+platform.ID+"/members/stranger", op, nil, 422, nil)
	f.request(t, "PUT", "/v1/teams/"+platform.ID+"/members/raj", priya, nil, 403, nil)
	f.request(t, "PUT", "/v1/teams/"+growth.ID+"/members/priya", op, nil, 200, nil)
	f.request(t, "DELETE", "/v1/teams/"+platform.ID+"/members/raj", raj, nil, 403, nil)
	f.request(t, "DELETE", "/v1/teams/"+platform.ID+"/members/raj", op, nil, 200, &got)
	if strings.Join(got.Members, ",") != "priya" {
		t.Fatalf("after removing raj: %+v", got)
	}

	var me api.Me
	f.request(t, "GET", "/v1/me", priya, nil, 200, &me)
	if me.Operator || len(me.Teams) != 2 {
		t.Fatalf("priya's me: operator=%v teams=%v", me.Operator, me.Teams)
	}
	raw := f.request(t, "GET", "/v1/me", raj, nil, 200, nil)
	if !strings.Contains(raw, `"teams":[]`) {
		t.Fatalf("a person in no team must say an empty list: %s", raw)
	}

	// Teams survive a restart, in name order, and a member reads them all.
	f.h.Close()
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	var list api.Teams
	f.request(t, "GET", "/v1/teams", raj, nil, 200, &list)
	if len(list.Teams) != 2 || list.Teams[0].Name != "Growth" || list.Teams[1].Name != "Platform" ||
		strings.Join(list.Teams[1].Members, ",") != "priya" {
		t.Fatalf("after restart: %+v", list)
	}
	if teams := f.h.Membership().TeamsOf("@Priya"); len(teams) != 2 {
		t.Fatalf("TeamsOf: %v", teams)
	}
	if members := f.h.Membership().Members(platform.ID); strings.Join(members, ",") != "priya" {
		t.Fatalf("Members: %v", members)
	}
}

func TestATeamThatOwnsAWorkstreamCannotBeDeleted(t *testing.T) {
	f, priya, _ := teamsFixture(t)
	const op = "operator-secret"
	var team api.Team
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "Platform"}, 200, &team)

	owns := ownsStub{team.ID: true}
	f.h.SetTeamOwnership(owns)
	body := f.request(t, "DELETE", "/v1/teams/"+team.ID, op, nil, 409, nil)
	if !strings.Contains(body, "owns a workstream") {
		t.Fatalf("refusal: %s", body)
	}
	f.request(t, "DELETE", "/v1/teams/"+team.ID, priya, nil, 403, nil)

	owns[team.ID] = false
	f.request(t, "DELETE", "/v1/teams/"+team.ID, op, nil, 204, nil)
	f.request(t, "DELETE", "/v1/teams/"+team.ID, op, nil, 404, nil)
	var list api.Teams
	f.request(t, "GET", "/v1/teams", op, nil, 200, &list)
	if len(list.Teams) != 0 {
		t.Fatalf("deleted team still listed: %+v", list)
	}
}

func TestRemovingAPersonTakesThemOutOfEveryTeamAndSaysSo(t *testing.T) {
	f, _, _ := teamsFixture(t)
	const op = "operator-secret"
	var a, b api.Team
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "Platform"}, 200, &a)
	f.request(t, "POST", "/v1/teams", op, api.TeamName{Name: "Growth"}, 200, &b)
	for _, id := range []string{a.ID, b.ID} {
		f.request(t, "PUT", "/v1/teams/"+id+"/members/priya", op, nil, 200, nil)
		f.request(t, "PUT", "/v1/teams/"+id+"/members/raj", op, nil, 200, nil)
	}

	f.request(t, "DELETE", "/v1/people/priya/sessions", op, nil, 200, nil)
	var list api.Teams
	f.request(t, "GET", "/v1/teams", op, nil, 200, &list)
	for _, team := range list.Teams {
		if strings.Join(team.Members, ",") != "raj" {
			t.Fatalf("%s still names priya: %+v", team.Name, team)
		}
	}
	if teams := f.h.Membership().TeamsOf("priya"); len(teams) != 0 {
		t.Fatalf("priya is still in %v", teams)
	}

	// Every change above went out on the event stream as
	// {"kind":"team.changed","id"}, the last two for priya leaving each team.
	events, _, err := live.Read(f.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var changed []api.OrgChanged
	var lastEvent live.Event
	for _, e := range events {
		if e.Kind != api.TeamChangedKind {
			continue
		}
		lastEvent = e
		var c api.OrgChanged
		if err := json.Unmarshal(e.Data, &c); err != nil || c.Kind != api.TeamChangedKind {
			t.Fatalf("event data: %s %v", e.Data, err)
		}
		changed = append(changed, c)
	}
	if len(changed) != 8 {
		t.Fatalf("want 8 team.changed events (2 made, 4 joins, 2 leaves), got %d: %+v", len(changed), changed)
	}
	last := map[string]bool{changed[6].ID: true, changed[7].ID: true}
	if !last[a.ID] || !last[b.ID] {
		t.Fatalf("leaving events: %+v", changed[6:])
	}
	if out, ok := f.h.liveForViewer(nil, lastEvent); !ok || out.Kind != api.TeamChangedKind {
		t.Fatalf("team.changed is for every member: %+v %v", out, ok)
	}
}
