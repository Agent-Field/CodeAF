package appapi

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestCrewRecordLinksAddListRemoveFinish(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	links := &fakeCrewLinks{}
	f.server.SetAppsServices(&svc.Services{
		Crews:   svc.CrewsServices{Links: links},
		Records: &fakeLinkRecords{row: bridge.Record{ID: "north", Type: "deal", Fields: map[string]any{"name": "Northwind · team plan", "stage": "Proposal", "amount": float64(48000)}}},
	})
	path := "/app/v1/crews/" + c.ID
	body := `{"kind":"record","app":"crm","type":"deal","id":"north"}`
	var added recordLinkView
	crewTestRead(t, f.request(t, "POST", path+"/items", body), 201, &added)
	if added.Kind != "record" || added.Ref != "app:crm/deal/north" || added.Label != "Northwind · team plan" || added.Detail != "Proposal · $48,000" || added.Access != "ok" || added.Href != "codeaf://apps/crm/deal/north" || added.AppName != "CRM" || added.State != "open" {
		t.Fatalf("added=%+v", added)
	}
	if got := recordLinks(t, f, c.ID); len(got) != 1 || got[0].Ref != added.Ref {
		t.Fatalf("list=%+v", got)
	}
	crewTestRead(t, f.request(t, "POST", path+"/items", body), 409, nil)
	crewTestRead(t, f.request(t, "DELETE", path+"/items/app:crm/deal/north", `{}`), 200, nil)
	if got := recordLinks(t, f, c.ID); len(got) != 0 {
		t.Fatalf("unlinked=%+v", got)
	}
	crewTestRead(t, f.request(t, "DELETE", path+"/items/app:crm/deal/north", `{}`), 404, nil)
	crewTestRead(t, f.request(t, "POST", path+"/items", body), 201, &added)
	crewTestRead(t, f.request(t, "POST", path+"/wrap-up", `{}`), 200, nil)
	nativeCrewReport(t, f, c, "")
	var report CrewReport
	crewTestRead(t, f.request(t, "GET", path+"/report", ""), 200, &report)
	if !strings.Contains(report.Markdown, "## Everything it touched") || !strings.Contains(report.Markdown, "Proposal · $48,000") || !strings.Contains(report.Markdown, "CRM · View") {
		t.Fatal(report.Markdown)
	}
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 200, nil)
	if len(links.finished) != 1 || links.finished[0] != c.ID {
		t.Fatalf("finish=%v", links.finished)
	}
	if got := recordLinks(t, f, c.ID); len(got) != 1 || got[0].State != "finished" {
		t.Fatalf("finished=%+v", got)
	}
	crewTestRead(t, f.request(t, "POST", path+"/items", body), 409, nil)
	crewTestRead(t, f.request(t, "POST", path+"/reopen", `{"expect":{"state":"closed"}}`), 200, nil)
	if len(links.reopened) != 1 || links.reopened[0] != c.ID {
		t.Fatalf("reopen=%v", links.reopened)
	}
	if got := recordLinks(t, f, c.ID); len(got) != 1 || got[0].State != "open" {
		t.Fatalf("reopened=%+v", got)
	}
}

type recordLinkView struct {
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Label   string `json:"label"`
	Detail  string `json:"detail"`
	State   string `json:"state"`
	Access  string `json:"access"`
	Href    string `json:"href"`
	AppName string `json:"app_name"`
}

func recordLinks(t *testing.T, f *agentFixture, cid string) []recordLinkView {
	t.Helper()
	var body struct {
		Items []recordLinkView `json:"items"`
	}
	crewTestRead(t, f.request(t, "GET", "/app/v1/crews/"+cid, ""), 200, &body)
	var records []recordLinkView
	for _, item := range body.Items {
		if item.Kind == "record" {
			records = append(records, item)
		}
	}
	return records
}

type fakeCrewLinks struct {
	mu       sync.Mutex
	links    []bridge.CrewLink
	finished []string
	reopened []string
}

func (f *fakeCrewLinks) Add(_ context.Context, scope svc.Scope, p bridge.AddLinkParams) (bridge.CrewLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.links {
		if existing.Crew != p.Crew || existing.App != scope.App || existing.Type != p.Type || existing.ID != p.ID {
			continue
		}
		if existing.State == bridge.CrewLinkOpen {
			return bridge.CrewLink{}, bridge.Errorf(bridge.CodeConflict, "already linked to this crew")
		}
		existing.State = bridge.CrewLinkOpen
		f.links[i] = existing
		return existing, nil
	}
	link := bridge.CrewLink{Crew: p.Crew, App: scope.App, AppName: "CRM", Type: p.Type, ID: p.ID, Label: "stored", State: bridge.CrewLinkOpen, Access: bridge.CrewLinkAccessOK}
	f.links = append(f.links, link)
	return link, nil
}
func (f *fakeCrewLinks) Remove(_ context.Context, scope svc.Scope, p bridge.RemoveLinkParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, existing := range f.links {
		if existing.Crew == p.Crew && existing.App == scope.App && existing.Type == p.Type && existing.ID == p.ID && existing.State != bridge.CrewLinkUnlinked {
			f.links[i].State = bridge.CrewLinkUnlinked
			return nil
		}
	}
	return bridge.Errorf(bridge.CodeNotFound, "no such link")
}
func (f *fakeCrewLinks) ForRecord(context.Context, svc.Scope, bridge.LinksForRecordParams) (bridge.ListLinksResult, error) {
	return bridge.ListLinksResult{Links: []bridge.CrewLink{}}, nil
}
func (f *fakeCrewLinks) ForCrew(_ context.Context, _ svc.Caller, p bridge.LinksForCrewParams) (bridge.ListLinksResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var links []bridge.CrewLink
	for _, link := range f.links {
		if link.Crew == p.Crew {
			links = append(links, link)
		}
	}
	if links == nil {
		links = []bridge.CrewLink{}
	}
	return bridge.ListLinksResult{Links: links}, nil
}
func (f *fakeCrewLinks) Finish(_ context.Context, crew string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished = append(f.finished, crew)
	for i, link := range f.links {
		if link.Crew == crew && link.State == bridge.CrewLinkOpen {
			f.links[i].State = bridge.CrewLinkFinished
		}
	}
	return nil
}
func (f *fakeCrewLinks) Reopen(_ context.Context, crew string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reopened = append(f.reopened, crew)
	for i, link := range f.links {
		if link.Crew == crew && link.State == bridge.CrewLinkFinished {
			f.links[i].State = bridge.CrewLinkOpen
		}
	}
	return nil
}

type fakeLinkRecords struct{ row bridge.Record }

func (f *fakeLinkRecords) Get(context.Context, svc.Scope, bridge.GetRecordParams) (bridge.Record, error) {
	return f.row, nil
}
func (f *fakeLinkRecords) Query(context.Context, svc.Scope, bridge.QueryParams) (bridge.QueryResult, error) {
	return bridge.QueryResult{}, bridge.NotBuilt("records")
}
func (f *fakeLinkRecords) Create(context.Context, svc.Scope, bridge.CreateRecordParams) (bridge.Record, error) {
	return bridge.Record{}, bridge.NotBuilt("records")
}
func (f *fakeLinkRecords) Update(context.Context, svc.Scope, bridge.UpdateRecordParams) (bridge.UpdateRecordResult, error) {
	return bridge.UpdateRecordResult{}, bridge.NotBuilt("records")
}
func (f *fakeLinkRecords) Delete(context.Context, svc.Scope, bridge.DeleteRecordParams) (bridge.DeleteRecordResult, error) {
	return bridge.DeleteRecordResult{}, bridge.NotBuilt("records")
}
func (f *fakeLinkRecords) Seed(context.Context, string, map[string][]bridge.Record) error {
	return bridge.NotBuilt("records")
}
