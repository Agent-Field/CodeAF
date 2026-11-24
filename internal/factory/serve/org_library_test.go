package serve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func (o *wsOrg) library(t *testing.T, who string) []api.OrgLibraryPage {
	t.Helper()
	var list api.OrgLibraryList
	o.do(t, who, "GET", "/v1/library", -1, nil, 200, &list)
	return list.Pages
}

// THE ORGANIZATION'S LIBRARY IS EVERYONE'S TO READ, AND NOTHING REACHES IT BUT
// BY A PERSON (org-space DESIGN §2.1, OWNER-DECISIONS §5). A page santosh adds
// from a file is read by abir and mia alike; only santosh and the operator may
// change or remove it.
func TestAPageAddedToTheLibraryIsReadByEveryoneAndChangedByItsPersonOrTheOperator(t *testing.T) {
	o := newWSOrg(t)
	if pages := o.library(t, "mia"); len(pages) != 0 {
		t.Fatalf("a new organization's Library = %+v", pages)
	}
	ws := o.create(t, "santosh", api.WorkstreamNew{Name: "Pantry"})
	var added api.OrgLibraryPage
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "  Launch   checklist ", Body: "# Launch\n\nEvery release ships on a Tuesday.", FromWorkstream: ws.ID}, 201, &added)
	if !strings.HasPrefix(added.ID, "lb_") || added.Title != "Launch checklist" || !added.Mine || !added.Changeable || added.FromName != "Pantry" {
		t.Fatalf("added = %+v", added)
	}
	for _, who := range []string{"abir", "mia", "owner"} {
		pages := o.library(t, who)
		if len(pages) != 1 || pages[0].Title != "Launch checklist" || pages[0].Body != "" || pages[0].By != "santosh" {
			t.Fatalf("%s's Library = %+v", who, pages)
		}
		var page api.OrgLibraryPage
		o.do(t, who, "GET", "/v1/library/"+added.ID, -1, nil, 200, &page)
		if !strings.Contains(page.Body, "Tuesday") {
			t.Fatalf("%s reads %+v", who, page)
		}
	}
	// mia is not in Pantry: the page is hers to read, the place it came from
	// is not hers to be told.
	if mia := o.library(t, "mia")[0]; mia.FromWorkstream != "" || mia.FromName != "" || mia.Changeable {
		t.Fatalf("to mia the page says %+v", mia)
	}

	o.do(t, "abir", "PATCH", "/v1/library/"+added.ID, -1, api.OrgLibraryPatch{Title: ptr("Abir's now")}, 403, nil)
	o.do(t, "abir", "DELETE", "/v1/library/"+added.ID, -1, nil, 403, nil)
	var changed api.OrgLibraryPage
	o.do(t, "santosh", "PATCH", "/v1/library/"+added.ID, -1, api.OrgLibraryPatch{Body: ptr("Releases ship on Wednesdays now.")}, 200, &changed)
	if changed.Title != "Launch checklist" || changed.Body != "Releases ship on Wednesdays now." {
		t.Fatalf("changed = %+v", changed)
	}
	o.do(t, "owner", "DELETE", "/v1/library/"+added.ID, -1, nil, 204, nil)
	if pages := o.library(t, "santosh"); len(pages) != 0 {
		t.Fatalf("removed, the Library still holds %+v", pages)
	}

	// The refusals say what is wrong.
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "", Body: "x"}, 422, nil)
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "Empty", Body: "  "}, 422, nil)
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "Huge", Body: strings.Repeat("x", api.OrgLibraryBodyMost+1)}, 422, nil)
	// A page cannot claim to come from a workstream its person cannot see.
	private := o.create(t, "abir", api.WorkstreamNew{Name: "Layoff plan", Private: true})
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "Leak", Body: "x", FromWorkstream: private.ID}, 404, nil)
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "Nowhere", Body: "x", FromWorkstream: ws.ID, FromFile: "fl_nothing"}, 404, nil)
}

// A CHAT ON THE SERVER READS THE LIBRARY WITH ONE TOOL, AND ONLY READS. The
// door a turn is handed carries org_library on an organization's server; the
// search finds a page by its words and a long page reads on in parts.
func TestAChatOnTheServerSearchesAndReadsTheLibrary(t *testing.T) {
	o := newWSOrg(t)
	ws := o.create(t, "abir", api.WorkstreamNew{Name: "Pantry"})
	long := "Refunds are approved by the finance lead. " + strings.Repeat("More policy words. ", 1200)
	var refunds api.OrgLibraryPage
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "Refund policy", Body: long}, 201, &refunds)
	o.do(t, "santosh", "POST", "/v1/library", -1, api.OrgLibraryNew{Title: "Brand voice", Body: "Plain, warm, short."}, 201, nil)

	door, ok := o.h.withLibrary(&agentDoor{h: o.h, id: ws.ID, asker: "abir"}).(session.OrgLibraryDoor)
	if !ok {
		t.Fatal("an organization's server hands its agents no Library")
	}
	found, err := door.Library(context.Background(), "who approves refunds", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var hits struct {
		Pages []struct{ ID, Title, Snippet string }
	}
	if err := json.Unmarshal([]byte(found), &hits); err != nil || len(hits.Pages) == 0 || hits.Pages[0].ID != refunds.ID || !strings.Contains(hits.Pages[0].Snippet, "finance lead") {
		t.Fatalf("search = %s", found)
	}
	first, err := door.Library(context.Background(), "", refunds.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var part struct {
		Words    string `json:"words"`
		MoreFrom int    `json:"more_from"`
	}
	if err := json.Unmarshal([]byte(first), &part); err != nil || part.MoreFrom == 0 || !strings.HasPrefix(part.Words, "Refunds are approved") {
		t.Fatalf("first part = %.200s", first)
	}
	rest, _ := door.Library(context.Background(), "", refunds.ID, part.MoreFrom)
	if strings.Contains(rest, "more_from") || strings.Contains(rest, "Refunds are approved") {
		t.Fatalf("the second part = %.200s", rest)
	}
	if _, err := door.Library(context.Background(), "", "lb_gone", 0); err == nil {
		t.Fatal("a page that is not there read as one")
	}
}
