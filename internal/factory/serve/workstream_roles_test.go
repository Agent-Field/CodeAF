package serve

import (
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
)

func sharesPut(shares ...factory.Share) api.WorkstreamSharesPut {
	return api.WorkstreamSharesPut{Shares: &shares}
}

func (o *wsOrg) summary(t *testing.T, who, id string) (api.WorkstreamSummary, bool) {
	t.Helper()
	var list api.WorkstreamList
	o.do(t, who, "GET", "/v1/workstreams", -1, nil, 200, &list)
	for _, s := range list.Workstreams {
		if s.ID == id {
			return s, true
		}
	}
	return api.WorkstreamSummary{}, false
}

// A ROLE IS WHAT A SHARE LETS SOMEONE DO: view reads and is refused every
// change, edit writes the words but cannot archive, full archives. Each row
// says the caller's role.
func TestAShareGivesItsRoleAndNoMore(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Webhook reliability"})
	path := "/v1/workstreams/" + ws.ID
	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", path+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "view"}), 200, &shared)
	if !slices.Equal(shared.Workstream.Shares, []factory.Share{{Kind: "person", ID: "abir", Role: "view"}}) || shared.Workstream.MyRole != "full" {
		t.Fatalf("shared = %+v", shared.Workstream.WorkstreamSummary)
	}
	rev := shared.Workstream.Rev
	if row, ok := o.summary(t, "abir", ws.ID); !ok || row.MyRole != "view" {
		t.Fatalf("abir's row = %+v %v", row, ok)
	}
	o.do(t, "abir", "GET", path, -1, nil, 200, nil)
	o.do(t, "abir", "PATCH", path, rev, map[string]any{"purpose": "x"}, 403, nil)
	o.do(t, "abir", "DELETE", path, -1, nil, 403, nil)
	o.do(t, "abir", "PUT", path+"/shares", -1, sharesPut(), 403, nil)
	o.do(t, "mia", "GET", path, -1, nil, 404, nil)

	o.do(t, "santosh", "PUT", path+"/shares", -1, sharesPut(factory.Share{Kind: "team", ID: "tm_platform", Role: "edit"}, factory.Share{Kind: "person", ID: "abir", Role: "comment"}), 200, &shared)
	var got api.Workstream
	o.do(t, "abir", "PATCH", path, shared.Workstream.Rev, map[string]any{"purpose": "Stop dropping webhooks"}, 200, &got)
	if got.MyRole != "edit" {
		t.Fatalf("a team edit share and a person comment share should give edit: %q", got.MyRole)
	}
	o.do(t, "abir", "DELETE", path, -1, nil, 403, nil)
	o.do(t, "santosh", "PUT", path+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "full"}), 200, nil)
	o.do(t, "abir", "DELETE", path, -1, nil, 204, nil)

	o.do(t, "santosh", "PUT", path+"/shares", -1, sharesPut(factory.Share{Kind: "team", ID: "tm_nowhere", Role: "view"}), 422, nil)
	o.do(t, "santosh", "PUT", path+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "admin"}), 422, nil)
}

// A PRIVATE WORKSTREAM IS ITS OWNER'S ALONE: the operator's list has a row
// with its name and nothing it says, its content is 404 to them, and its
// events never reach them. Sharing it makes it not private.
func TestAPrivateWorkstreamIsANameToTheOperator(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Salary bands", Private: true, Purpose: "secret"})
	if !ws.Private || ws.MyRole != "full" {
		t.Fatalf("made = %+v", ws.WorkstreamSummary)
	}
	path := "/v1/workstreams/" + ws.ID
	row, ok := o.summary(t, "owner", ws.ID)
	if !ok || row.Name != "Salary bands" || !row.Private || row.MyRole != "" || row.Owner.ID != "santosh" {
		t.Fatalf("the operator's row = %+v %v", row, ok)
	}
	o.do(t, "owner", "GET", path, -1, nil, 404, nil)
	o.do(t, "owner", "PUT", path+"/owner", -1, api.WorkstreamOwner{Kind: "person", ID: "owner"}, 404, nil)
	o.do(t, "owner", "DELETE", path, -1, nil, 404, nil)
	if _, ok := o.summary(t, "abir", ws.ID); ok {
		t.Fatal("abir sees santosh's private workstream")
	}
	o.do(t, "santosh", "PATCH", path, ws.Rev, map[string]any{"purpose": "still secret"}, 200, nil)
	if o.operatorHeardLast(t) {
		t.Fatal("the operator heard a private workstream change")
	}

	team := api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}
	o.do(t, "santosh", "POST", "/v1/workstreams", -1, api.WorkstreamNew{Name: "y", Private: true, Owner: &team}, 422, nil)
	yes := true
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Private: &yes, Shares: &[]factory.Share{{Kind: "person", ID: "abir", Role: "view"}}}, 422, nil)

	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", path+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "view"}), 200, &shared)
	if shared.Workstream.Private {
		t.Fatal("a shared workstream stayed private")
	}
	o.do(t, "owner", "GET", path, -1, nil, 200, nil)
	o.do(t, "santosh", "PUT", path+"/shares", -1, api.WorkstreamSharesPut{Private: &yes, Shares: &[]factory.Share{}}, 200, &shared)
	if !shared.Workstream.Private {
		t.Fatal("a workstream made private again is not")
	}
	o.do(t, "abir", "GET", path, -1, nil, 404, nil)
}

// operatorHeardLast is whether the newest workstream event reaches the
// operator.
func (o *wsOrg) operatorHeardLast(t *testing.T) bool {
	t.Helper()
	events, _, err := live.Read(o.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	var last live.Event
	for _, e := range events {
		if e.Kind == api.EventWorkstreamChanged {
			last = e
		}
	}
	r := httptest.NewRequest("GET", "/v1/events", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("Authorization", "Bearer "+o.tokens["owner"])
	_, ok := o.h.liveForViewer(r, last)
	return ok
}

// THE SERVER SIGNS EVERY DECISION: a decision sent with someone else's name
// and an old time is kept as the caller's, now; one that did not change keeps
// its byline; an import is the importer's whatever created_by says.
func TestTheServerSignsDecisionsAndImports(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Webhook reliability",
		Decisions: []api.WorkstreamDecision{{Text: "Retry thrice", By: "abir", At: "2001-01-01T00:00:00Z"}}})
	if d := ws.Decisions[0]; d.By != "santosh" || d.At == "2001-01-01T00:00:00Z" {
		t.Fatalf("made = %+v", d)
	}
	kept := ws.Decisions[0]
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "abir", Role: "edit"}), 200, nil)
	cur := o.get(t, "abir", ws.ID)
	o.now = o.now.Add(5)
	var got api.Workstream
	o.do(t, "abir", "PATCH", "/v1/workstreams/"+ws.ID, cur.Rev, map[string]any{"decisions": []api.WorkstreamDecision{
		kept,
		{Text: "Page on-call", By: "santosh", At: "2001-01-01T00:00:00Z"},
	}}, 200, &got)
	if got.Decisions[0] != kept {
		t.Fatalf("an unchanged decision was re-signed: %+v", got.Decisions[0])
	}
	if d := got.Decisions[1]; d.By != "abir" || d.At == "2001-01-01T00:00:00Z" {
		t.Fatalf("a forged decision = %+v", d)
	}
	var moved api.Workstream
	o.do(t, "abir", "POST", "/v1/workstreams/import", -1, api.WorkstreamPackage{FromLocalID: "local-1", Name: "Moved", CreatedBy: "santosh"}, 201, &moved)
	if moved.CreatedBy != "abir" || moved.Owner.ID != "abir" {
		t.Fatalf("import = %+v", moved)
	}
}

func (o *wsOrg) get(t *testing.T, who, id string) api.Workstream {
	t.Helper()
	var ws api.Workstream
	o.do(t, who, "GET", "/v1/workstreams/"+id, -1, nil, 200, &ws)
	return ws
}

// MOVING LINKED WORKSTREAMS IN KEEPS THEIR LINKS, in either order: a link to
// one the same person already moved in names its copy, and one moved in later
// is linked from the copies that named it on the laptop.
func TestMovingLinkedWorkstreamsInKeepsTheLinks(t *testing.T) {
	o := newWSOrg(t)
	var b, a, c api.Workstream
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, api.WorkstreamPackage{FromLocalID: "local-b", Name: "B"}, 201, &b)
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, api.WorkstreamPackage{FromLocalID: "local-a", Name: "A", Links: []string{"local-b", "local-c", "local-gone"}}, 201, &a)
	if !slices.Equal(a.Links, []string{b.ID}) {
		t.Fatalf("A's links = %v; want B's copy %s", a.Links, b.ID)
	}
	o.do(t, "santosh", "POST", "/v1/workstreams/import", -1, api.WorkstreamPackage{FromLocalID: "local-c", Name: "C"}, 201, &c)
	if got := o.get(t, "santosh", a.ID).Links; !slices.Equal(got, []string{b.ID, c.ID}) {
		t.Fatalf("A's links after C moved in = %v", got)
	}
	// Another person's copy of a Personal id with the same name is not theirs.
	var other api.Workstream
	o.do(t, "abir", "POST", "/v1/workstreams/import", -1, api.WorkstreamPackage{FromLocalID: "local-z", Name: "Z", Links: []string{"local-b"}}, 201, &other)
	if len(other.Links) != 0 {
		t.Fatalf("abir's import linked to santosh's copy: %v", other.Links)
	}
}

// ONE SHARE SAVE CARRIES THE OWNER TOO, guarded by the rev the sheet read.
func TestOwnerAndSharesLandInOneSave(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch plan"})
	team := api.WorkstreamOwner{Kind: "team", ID: "tm_growth"}
	stale := ws.Rev - 1
	shares := []factory.Share{{Kind: "person", ID: "abir", Role: "comment"}, {Kind: "team", ID: "tm_growth", Role: "view"}}
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Owner: &team, Shares: &shares, Rev: &stale}, 412, nil)
	var shared api.WorkstreamShared
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, api.WorkstreamSharesPut{Owner: &team, Shares: &shares, Rev: &ws.Rev}, 200, &shared)
	got := shared.Workstream
	if got.Owner != team || got.Rev != ws.Rev+1 || !slices.Equal(got.Shares, []factory.Share{{Kind: "person", ID: "abir", Role: "comment"}}) {
		t.Fatalf("one save = %+v", got.WorkstreamSummary)
	}
}

// A TEAM'S GO-AHEADS ARE THE TEAM'S: the first teammate to approve does not
// lock out the next, and someone with only view cannot approve.
func TestATeamWorkstreamsGoAheadsAreNotTheFirstWritersAlone(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Webhook reliability", Owner: &api.WorkstreamOwner{Kind: "team", ID: "tm_platform"}})
	o.do(t, "santosh", "PUT", "/v1/policy", -1, api.PolicyPut{Group: ws.ID, Stopped: true}, 200, nil)
	o.do(t, "abir", "PUT", "/v1/policy", -1, api.PolicyPut{Group: ws.ID}, 200, nil)
	o.do(t, "santosh", "PUT", "/v1/workstreams/"+ws.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "mia", Role: "view"}), 200, nil)
	o.do(t, "mia", "PUT", "/v1/policy", -1, api.PolicyPut{Group: ws.ID}, 403, nil)
	o.do(t, "mia", "GET", "/v1/policy?group="+ws.ID, -1, nil, 200, nil)
}

// A PERSON WHO LEAVES LEAVES NOTHING ORPHANED: their workstreams go to the
// heir the operator names, private ones staying private, and they leave every
// share.
func TestARemovedPersonsWorkstreamsGoToAnHeir(t *testing.T) {
	o := newWSOrg(t)
	mine := o.create(t, "santosh", api.WorkstreamNew{Name: "Launch plan"})
	private := o.create(t, "santosh", api.WorkstreamNew{Name: "Salary bands", Private: true})
	theirs := o.create(t, "mia", api.WorkstreamNew{Name: "Pricing page"})
	o.do(t, "mia", "PUT", "/v1/workstreams/"+theirs.ID+"/shares", -1, sharesPut(factory.Share{Kind: "person", ID: "santosh", Role: "edit"}), 200, nil)
	o.do(t, "abir", "DELETE", "/v1/people/santosh/sessions?heir=abir", -1, nil, 403, nil)
	o.do(t, "owner", "DELETE", "/v1/people/santosh/sessions?heir=abir", -1, nil, 200, nil)
	for _, id := range []string{mine.ID, private.ID} {
		got := o.get(t, "abir", id)
		if got.Owner != (api.WorkstreamOwner{Kind: "person", ID: "abir"}) {
			t.Fatalf("%s owner = %+v", got.Name, got.Owner)
		}
	}
	if !o.get(t, "abir", private.ID).Private {
		t.Fatal("a private workstream stopped being private when it was handed on")
	}
	if got := o.get(t, "mia", theirs.ID); len(got.Shares) != 0 {
		t.Fatalf("a gone person kept a share: %+v", got.Shares)
	}
}
