package serve

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// inviteOrg is AgentField from the v2 design: Priya runs the server, the
// Engineering team, and three workstreams.
type inviteOrg struct {
	f                          *peopleV2Fixture
	g                          *fakeGitHub
	priya                      string
	team                       api.Team
	product, summit, marketing api.Workstream
}

func newInviteOrg(t *testing.T) *inviteOrg {
	t.Helper()
	f, g := linkFixture(t)
	o := &inviteOrg{f: f, g: g}
	// Priya claims the server, so she is its admin.
	if err := os.Remove(filepath.Join(f.st.Root(), "operator.json")); err != nil {
		t.Fatal(err)
	}
	link, err := f.h.NewOperatorLink(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	o.priya = signInHandle(t, f.h, g, &f.now, "priya", operatorSecret(t, link))
	f.request(t, "POST", "/v1/teams", o.priya, api.TeamName{Name: "Engineering team"}, 200, &o.team)
	for _, made := range []struct {
		name string
		out  *api.Workstream
	}{{"AgentField", &o.product}, {"DevTools Summit", &o.summit}, {"Marketing", &o.marketing}} {
		f.request(t, "POST", "/v1/workstreams", o.priya, api.WorkstreamNew{Name: made.name}, 201, made.out)
	}
	return o
}

func (o *inviteOrg) invite(t *testing.T, who string, body api.InviteNew, status int) (api.InviteMade, string) {
	t.Helper()
	var made api.InviteMade
	o.f.request(t, "POST", "/v1/invites", who, body, status, &made)
	if status != 201 {
		return made, ""
	}
	parsed, err := url.Parse(made.URL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, _ := url.ParseQuery(parsed.Fragment)
	return made, fragment.Get("token")
}

// patch sends a task change with the rev it read.
func (o *inviteOrg) patch(t *testing.T, who, path string, rev int, body any, status int) string {
	t.Helper()
	data, _ := json.Marshal(body)
	r := httptest.NewRequest("PATCH", path, strings.NewReader(string(data)))
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+who)
	r.Header.Set("If-Match", strconv.Itoa(rev))
	w := httptest.NewRecorder()
	o.f.h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("PATCH %s: %d %s; want %d", path, w.Code, w.Body, status)
	}
	return w.Body.String()
}

func (o *inviteOrg) tasks(t *testing.T, who, ws string) []factory.WorkstreamTask {
	t.Helper()
	var list api.WorkstreamTasks
	o.f.request(t, "GET", "/v1/workstreams/"+ws+"/tasks", who, nil, 200, &list)
	return list.Tasks
}

// THE J4/J7 JOURNEY ON THE SERVER: Priya invites Marco to two workstreams
// with the Engineering team and gives him a task before he joins; it waits.
// He joins and is in exactly those workstreams and that team, the task is
// his and reads as handed to him until he opens it, and his welcome line
// shows until Got it.
func TestAnInviteCarriesItsTeamWorkstreamsAndTasksIntoTheJoin(t *testing.T) {
	o := newInviteOrg(t)
	f := o.f

	// Only the operator invites, and a bad invite is refused before anything.
	f.request(t, "POST", "/v1/invites", "", api.InviteNew{Email: "marco@agentfield.example"}, 401, nil)
	o.invite(t, o.priya, api.InviteNew{Email: "not an email"}, 422)
	o.invite(t, o.priya, api.InviteNew{}, 422)
	o.invite(t, o.priya, api.InviteNew{Email: "marco@agentfield.example", Team: "tm_nope"}, 422)
	o.invite(t, o.priya, api.InviteNew{Email: "marco@agentfield.example", Workstreams: []string{"ws_nope"}}, 422)

	made, _ := o.invite(t, o.priya, api.InviteNew{Email: "Marco@AgentField.example", Name: "Marco", Team: o.team.ID,
		Workstreams: []string{o.product.ID}}, 201)
	inv := made.Invite
	if inv.State != api.InviteInvited || inv.Emailed || inv.Ref != "invite:"+inv.ID || inv.TeamName != "Engineering team" ||
		inv.Email != "marco@agentfield.example" || !inv.ExpiresAt.Equal(f.now.Add(7*24*time.Hour)) {
		t.Fatalf("invite = %+v", inv)
	}
	// Send invite again for the same person adds to the same invite.
	again, gate := o.invite(t, o.priya, api.InviteNew{Email: "marco@agentfield.example", Workstreams: []string{o.summit.ID}}, 201)
	if again.Invite.ID != inv.ID || strings.Join(again.Invite.WorkstreamNames, ",") != "AgentField,DevTools Summit" {
		t.Fatalf("second invite for the same email = %+v", again.Invite)
	}

	// He is already one of AgentField's people, by the name he was invited with.
	var ws api.Workstream
	f.request(t, "GET", "/v1/workstreams/"+o.product.ID, o.priya, nil, 200, &ws)
	at := slices.IndexFunc(ws.People, func(m api.WorkstreamMember) bool { return m.ID == inv.Ref })
	if at < 0 || !ws.People[at].Invited || ws.People[at].Name != "Marco" || ws.People[at].Role != factory.RoleEdit {
		t.Fatalf("people before joining = %+v", ws.People)
	}

	// A task given to him before he joins waits: nobody starts it for him.
	var task factory.WorkstreamTask
	f.request(t, "POST", "/v1/workstreams/"+o.product.ID+"/tasks", o.priya, factory.TaskCreate{Title: "Rehearse the demo",
		Owner:  &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: inv.Ref, Name: "Marco"},
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByPerson, ID: inv.Ref, Name: "Marco"}}, 201, &task)
	running := factory.TaskRunning
	body := o.patch(t, o.priya, "/v1/workstreams/"+o.product.ID+"/tasks/"+task.ID, task.Rev, factory.TaskPatch{State: &running}, 422)
	if !strings.Contains(body, "Marco has not joined yet") {
		t.Fatalf("refusal = %s", body)
	}

	// He joins through the link.
	marco := signInHandle(t, f.h, o.g, &f.now, "marco", gate)

	var list api.Invites
	f.request(t, "GET", "/v1/invites", o.priya, nil, 200, &list)
	if len(list.Invites) != 1 || list.Invites[0].State != api.InviteJoined || list.Invites[0].Handle != "marco" || list.Mailer {
		t.Fatalf("invites after joining = %+v", list)
	}
	f.request(t, "GET", "/v1/invites", marco, nil, 403, nil)
	// The invite is People's row, never a second bare row in Access links.
	var links api.AccessLinks
	f.request(t, "GET", "/v1/access/links", o.priya, nil, 200, &links)
	for _, l := range links.Links {
		if l.ID == inv.ID {
			t.Fatalf("access links list the invite again: %+v", l)
		}
	}

	var names api.WorkstreamList
	f.request(t, "GET", "/v1/workstreams", marco, nil, 200, &names)
	var his []string
	for _, w := range names.Workstreams {
		his = append(his, w.Name)
	}
	slices.Sort(his)
	if strings.Join(his, ",") != "AgentField,DevTools Summit" {
		t.Fatalf("marco's workstreams = %v", his)
	}
	if teams := f.h.Membership().TeamsOf("marco"); !slices.Equal(teams, []string{o.team.ID}) {
		t.Fatalf("marco's teams = %v", teams)
	}
	ws = api.Workstream{}
	f.request(t, "GET", "/v1/workstreams/"+o.product.ID, o.priya, nil, 200, &ws)
	if slices.ContainsFunc(ws.People, func(m api.WorkstreamMember) bool { return m.ID == inv.Ref }) ||
		!slices.ContainsFunc(ws.People, func(m api.WorkstreamMember) bool { return m.ID == "marco" && !m.Invited }) {
		t.Fatalf("people after joining = %+v", ws.People)
	}

	// The task is his now, handed to him by Priya, and shown once.
	mine := o.tasks(t, marco, o.product.ID)
	if len(mine) != 1 || mine[0].Owner.ID != "marco" || mine[0].DoneBy.ID != "marco" || !mine[0].Unopened ||
		mine[0].HandedBy == nil || mine[0].HandedBy.ID != "priya" {
		t.Fatalf("marco's task = %+v", mine)
	}
	if o.tasks(t, o.priya, o.product.ID)[0].Unopened {
		t.Fatal("the task reads as handed to Priya too")
	}
	f.request(t, "GET", "/v1/workstreams/"+o.product.ID+"/tasks/"+task.ID+"/events?seen=1", marco, nil, 200, nil)
	if o.tasks(t, marco, o.product.ID)[0].Unopened {
		t.Fatal("an opened task still reads as handed to him")
	}
	o.patch(t, marco, "/v1/workstreams/"+o.product.ID+"/tasks/"+task.ID, mine[0].Rev, factory.TaskPatch{State: &running}, 200)

	// His welcome line, until Got it; Priya, who came in another way, has none.
	var welcome api.Welcome
	f.request(t, "GET", "/v1/me/welcome", marco, nil, 200, &welcome)
	if !welcome.Show || welcome.InvitedBy != "priya" || welcome.TeamName != "Engineering team" ||
		strings.Join(welcome.WorkstreamNames, ",") != "AgentField,DevTools Summit" {
		t.Fatalf("welcome = %+v", welcome)
	}
	f.request(t, "POST", "/v1/me/welcome/seen", marco, nil, 200, nil)
	f.request(t, "GET", "/v1/me/welcome", marco, nil, 200, &welcome)
	if welcome.Show {
		t.Fatalf("welcome after Got it = %+v", welcome)
	}
	f.request(t, "GET", "/v1/me/welcome", o.priya, nil, 200, &welcome)
	if welcome.Show {
		t.Fatalf("priya's welcome = %+v", welcome)
	}

	// A joined invite is not sent again or withdrawn.
	f.request(t, "POST", "/v1/invites/"+inv.ID+"/resend", o.priya, nil, 422, nil)
	f.request(t, "DELETE", "/v1/invites/"+inv.ID, o.priya, nil, 422, nil)
}

// Send again makes a new link for seven more days and the old one stops
// working; withdrawing gives the invitee's tasks back and takes them off
// their workstreams.
func TestSendAgainRenewsTheLinkAndWithdrawGivesTheTasksBack(t *testing.T) {
	o := newInviteOrg(t)
	f := o.f
	made, oldGate := o.invite(t, o.priya, api.InviteNew{Name: "Lena", Workstreams: []string{o.marketing.ID}}, 201)
	if made.Invite.Emailed || made.Invite.Email != "" {
		t.Fatalf("a name-only invite = %+v", made.Invite)
	}
	f.now = f.now.Add(8 * 24 * time.Hour)
	var list api.Invites
	f.request(t, "GET", "/v1/invites", o.priya, nil, 200, &list)
	if list.Invites[0].State != api.InviteExpired {
		t.Fatalf("after 8 days = %+v", list.Invites[0])
	}
	var again api.InviteMade
	f.request(t, "POST", "/v1/invites/"+made.Invite.ID+"/resend", o.priya, nil, 200, &again)
	if again.Invite.State != api.InviteInvited || !again.Invite.ExpiresAt.Equal(f.now.Add(7*24*time.Hour)) || again.URL == made.URL {
		t.Fatalf("sent again = %+v", again)
	}
	f.request(t, "POST", "/v1/auth/device", oldGate, nil, 401, nil)

	var task factory.WorkstreamTask
	f.request(t, "POST", "/v1/workstreams/"+o.marketing.ID+"/tasks", o.priya, factory.TaskCreate{Title: "Draft the launch post",
		Owner:  &factory.TaskOwner{Kind: factory.TaskOwnerPerson, ID: made.Invite.Ref, Name: "Lena"},
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}}, 201, &task)
	f.request(t, "DELETE", "/v1/invites/"+made.Invite.ID, o.priya, nil, 200, nil)
	f.request(t, "DELETE", "/v1/invites/"+made.Invite.ID, o.priya, nil, 404, nil)
	tasks := o.tasks(t, o.priya, o.marketing.ID)
	if tasks[0].Owner.ID != "priya" {
		t.Fatalf("after withdrawing = %+v", tasks[0].Owner)
	}
	var ws api.Workstream
	f.request(t, "GET", "/v1/workstreams/"+o.marketing.ID, o.priya, nil, 200, &ws)
	if slices.ContainsFunc(ws.People, func(m api.WorkstreamMember) bool { return m.Invited }) {
		t.Fatalf("withdrawn invitee still listed: %+v", ws.People)
	}
	f.request(t, "GET", "/v1/invites", o.priya, nil, 200, &list)
	if len(list.Invites) != 0 {
		t.Fatalf("withdrawn invite still listed: %+v", list)
	}
}

// Removing a person offers the tasks they own to whoever removed them (v2
// C6): they arrive in that person's Handed to you, from the one who left.
func TestRemovingSomeoneOffersTheirTasksToTheAdmin(t *testing.T) {
	o := newInviteOrg(t)
	f := o.f
	_, gate := o.invite(t, o.priya, api.InviteNew{Email: "sam@agentfield.example", Workstreams: []string{o.product.ID}}, 201)
	sam := signInHandle(t, f.h, o.g, &f.now, "sam", gate)
	var task factory.WorkstreamTask
	f.request(t, "POST", "/v1/workstreams/"+o.product.ID+"/tasks", sam, factory.TaskCreate{Title: "Fix the two crash reports",
		DoneBy: factory.TaskDoneBy{Kind: factory.TaskByAfi}}, 201, &task)
	if task.Owner.ID != "sam" {
		t.Fatalf("sam's own task = %+v", task.Owner)
	}
	var list api.Invites
	f.request(t, "GET", "/v1/invites", o.priya, nil, 200, &list)
	if len(list.Invites) != 1 || list.Invites[0].State != api.InviteJoined {
		t.Fatalf("sam's invite before removing = %+v", list)
	}
	f.request(t, "DELETE", "/v1/people/sam/sessions", o.priya, nil, 200, nil)
	got := o.tasks(t, o.priya, o.product.ID)
	if len(got) != 1 || got[0].Owner.ID != "priya" || got[0].HandedBy == nil || got[0].HandedBy.ID != "sam" || !got[0].Unopened {
		t.Fatalf("after removing sam = %+v", got)
	}
	// A removed person is gone from People, not still listed as Joined (found live on spark, 2026-11-17).
	list = api.Invites{}
	f.request(t, "GET", "/v1/invites", o.priya, nil, 200, &list)
	if len(list.Invites) != 0 {
		t.Fatalf("removed person still listed: %+v", list.Invites)
	}
	var people api.People
	f.request(t, "GET", "/v1/people", o.priya, nil, 200, &people)
	for _, p := range people.People {
		if p.Handle == "sam" {
			t.Fatalf("removed person still in the people list: %+v", p)
		}
	}
}

// Joining moves who-added-whom too: a person the invite (the joiner, before
// they joined) added reads as added by the person who joined, even in a
// workstream the joiner was never shared on.
func TestJoiningRewritesWhoTheInviteAdded(t *testing.T) {
	o := newInviteOrg(t)
	f := o.f
	made, gate := o.invite(t, o.priya, api.InviteNew{Email: "marco@agentfield.example", Name: "Marco",
		Workstreams: []string{o.product.ID}}, 201)
	ref := made.Invite.Ref
	if _, err := f.h.config.Store.EditWorkstreams(func(all factory.OrgWorkstreams) ([]string, error) {
		var changed []string
		for _, id := range []string{o.product.ID, o.marketing.ID} {
			ws := all[id]
			ws.PeopleAddedBy = maps1(ws.PeopleAddedBy)
			ws.PeopleAddedBy[factory.ShareToPerson+":lena"] = ref
			all[id] = ws
			changed = append(changed, id)
		}
		return changed, nil
	}); err != nil {
		t.Fatal(err)
	}
	signInHandle(t, f.h, o.g, &f.now, "marco", gate)
	all, err := f.h.config.Store.Workstreams()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{o.product.ID, o.marketing.ID} {
		if got := all[id].PeopleAddedBy[factory.ShareToPerson+":lena"]; got != "marco" {
			t.Errorf("%s: lena reads as added by %q after the join, want marco", all[id].Name, got)
		}
		for k, v := range all[id].PeopleAddedBy {
			if v == ref {
				t.Errorf("%s: %s still reads as added by the invite", all[id].Name, k)
			}
		}
	}
}
