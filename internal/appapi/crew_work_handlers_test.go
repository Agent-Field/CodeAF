package appapi

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/teams"
)

type fakeCrewItems struct {
	done  bool
	reads int
}

func (f *fakeCrewItems) CrewItemDone(_ context.Context, _ CrewItem) (bool, error) {
	f.reads++
	return f.done, nil
}
func nativeCrewReport(t *testing.T, f *agentFixture, c Crew, left string) teams.Packet {
	t.Helper()
	p, err := teams.Raise(f.home, teams.Packet{Team: teams.Person, Origin: c.ID, Kind: teams.PacketClosing, RaisedBy: teams.FromManager, Question: "Finish crew?", Report: &teams.ClosingReport{Done: "v2 shipped", Left: left, Files: []string{"api.go"}}, Options: []teams.Option{{ID: teams.OptionClose, Label: "Finish crew", Consequence: "Members stop; history stays"}, {ID: teams.OptionKeepGoing, Label: "Keep going", Consequence: "Continue work"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func workTestCrew(t *testing.T, f *agentFixture) Crew {
	t.Helper()
	a := crewTestAgent(t, f, "Manager")
	b := crewTestAgent(t, f, "Helper")
	return crewTestMake(t, f, "Release", a, b)
}
func TestCrewWorkReportWrapKeepFinishArchiveReopen(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	path := "/app/v1/crews/" + c.ID
	crewTestRead(t, f.request(t, "GET", path+"/report", ""), 409, nil)
	crewTestRead(t, f.request(t, "POST", path+"/wrap-up", `{}`), 200, nil)
	first := crewTestGet(t, f, c.ID)
	if first.State != "wrapping_up" {
		t.Fatal(first.State)
	}
	file, err := teams.Snapshot(f.home)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := file.Team(c.ID)
	crewTestRead(t, f.request(t, "POST", path+"/wrap-up", `{}`), 200, nil)
	file, _ = teams.Snapshot(f.home)
	after, _ := file.Team(c.ID)
	if before.Wrap == nil || after.Wrap == nil || !before.Wrap.Started.Equal(after.Wrap.Started) {
		t.Fatal("second wrap restarted its clock")
	}
	p := nativeCrewReport(t, f, c, "Follow-up remains")
	var report CrewReport
	crewTestRead(t, f.request(t, "GET", path+"/report", ""), 200, &report)
	for _, part := range []string{"What shipped", "Your decisions", "Files", "Left open", "Cost"} {
		if !strings.Contains(report.Markdown, part) {
			t.Fatal(report)
		}
	}
	if report.Shipped != "v2 shipped" || len(report.Files) != 1 || len(report.LeftOpen) != 1 {
		t.Fatal(report)
	}
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 409, nil)
	crewTestRead(t, f.request(t, "POST", path+"/keep-going", `{}`), 200, nil)
	decided, err := teams.PacketByID(f.home, p.ID)
	if err != nil || decided.Decision != teams.OptionKeepGoing {
		t.Fatalf("packet=%+v err=%v", decided, err)
	}
	if crewTestGet(t, f, c.ID).State != "active" {
		t.Fatal("keep going did not resume")
	}
	crewTestRead(t, f.request(t, "GET", path+"/report", ""), 409, nil)
	nativeCrewReport(t, f, c, "")
	crewTestRead(t, f.request(t, "GET", path+"/report", ""), 200, &report)
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 200, nil)
	if got := crewTestGet(t, f, c.ID); got.State != "closed" || got.Report == nil {
		t.Fatal(got)
	}
	for _, engine := range f.engines {
		if engine.stop == 0 {
			t.Fatal("finish left a member running")
		}
	}
	crewTestRead(t, f.request(t, "POST", path+"/archive", `{"expect":{"state":"closed"}}`), 200, nil)
	if crewTestGet(t, f, c.ID).State != "archived" {
		t.Fatal("archive state lost")
	}
	crewTestRead(t, f.request(t, "POST", path+"/reopen", `{"expect":{"state":"archived"}}`), 200, nil)
	got := crewTestGet(t, f, c.ID)
	if got.State != "active" || got.Report == nil || len(got.Tree) != 2 {
		t.Fatal(got)
	}
	for _, member := range got.Tree {
		agent := readAgent(t, f.request(t, "GET", "/app/v1/agents/"+member.AgentID, ""), 200)
		if agent.CrewID != c.ID {
			t.Fatal(agent)
		}
	}
}
func TestCrewFinishWritesManagerReportAndChecksLinkedWorkFirst(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	path := "/app/v1/crews/" + c.ID
	var item CrewItem
	crewTestRead(t, f.request(t, "POST", path+"/items", `{"ref":"api#1502"}`), 201, &item)
	items := &fakeCrewItems{}
	f.server.options.CrewItems = items
	blocked := f.request(t, "POST", path+"/finish", `{}`)
	crewTestRead(t, blocked, 409, nil)
	if !strings.Contains(blocked.Body.String(), `"open_items":1`) || f.engines[0].text != "" {
		t.Fatal(blocked.Body.String())
	}
	items.done = true
	f.registry.mu.Lock()
	f.registry.live[c.ManagerID].conn.Engine = &crewProposalEngine{fakeAgentEngine: f.engines[0], reply: `{"shipped":"v2.4 shipped","decisions":["Ship Thursday"],"files":[{"path":"api.go","state":"changed"}],"left_open":[]}`}
	f.registry.mu.Unlock()
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 200, nil)
	got := crewTestGet(t, f, c.ID)
	if got.State != "closed" || got.Report == nil || got.Report.Shipped != "v2.4 shipped" || got.Report.Cost.USD != "0.00" || items.reads < 2 {
		t.Fatal(got)
	}
	if !strings.Contains(f.engines[0].text, "Do not start new work") {
		t.Fatal("report bypassed the manager's engine")
	}
	crewTestRead(t, f.request(t, "POST", path+"/items", `{"ref":"api#1503"}`), 409, nil)
}
func TestCrewItemsLogSummaryValidationAndStaleActions(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	path := "/app/v1/crews/" + c.ID
	for _, body := range []string{`{}`, `null`, `{"ref":null}`, `{"ref":"a","url":"https://x"}`, `{"url":"file:///etc/passwd"}`, `{"url":"https://user:password@example.com"}`, `{"unknown":1}`, `{} {}`} {
		crewTestRead(t, f.request(t, "POST", path+"/items", body), 422, nil)
	}
	var item CrewItem
	crewTestRead(t, f.request(t, "POST", path+"/items", `{"url":"https://github.com/acme/api/pull/1"}`), 201, &item)
	if item.Ref == "" || len(crewTestGet(t, f, c.ID).Items) != 1 {
		t.Fatal(item)
	}
	crewTestRead(t, f.request(t, "POST", path+"/items", `{"url":"https://github.com/acme/api/pull/1"}`), 409, nil)
	for i := 0; i < 105; i++ {
		err := teams.AppendTraffic(f.home, c.ID, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: fmt.Sprintf("message %03d", i), At: f.registry.now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
	}
	var log CrewLogResponse
	crewTestRead(t, f.request(t, "GET", path+"/log", ""), 200, &log)
	if len(log.Entries) != 100 || log.Entries[0].Text != "message 104" || log.Before == "" {
		t.Fatal(log)
	}
	var previous CrewLogResponse
	crewTestRead(t, f.request(t, "GET", path+"/log?before="+log.Before, ""), 200, &previous)
	if len(previous.Entries) < 5 || previous.Entries[0].Text != "message 004" {
		t.Fatal(previous)
	}
	crewTestRead(t, f.request(t, "GET", path+"/log?before=no", ""), 422, nil)
	var summary CrewSummaryResponse
	at := f.registry.now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	crewTestRead(t, f.request(t, "GET", path+"/summary-since?at="+url.QueryEscape(at), ""), 200, &summary)
	if !strings.Contains(summary.Text, "message 104") || len(summary.Items) != 1 {
		t.Fatal(summary)
	}
	recent := f.registry.now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	crewTestRead(t, f.request(t, "GET", path+"/summary-since?at="+url.QueryEscape(recent), ""), 200, &summary)
	if summary.Text != "" {
		t.Fatal("summary shown before one hour away")
	}
	crewTestRead(t, f.request(t, "GET", path+"/summary-since?at=no", ""), 422, nil)
	crewTestRead(t, f.request(t, "POST", path+"/wrap-up", `{"expect":{"version":0}}`), 409, nil)
	crewTestRead(t, f.request(t, "DELETE", path+"/items/"+item.Ref, `{}`), 200, nil)
	if len(crewTestGet(t, f, c.ID).Items) != 0 {
		t.Fatal("unlink did not remove reference")
	}
	crewTestRead(t, f.request(t, "DELETE", path+"/items/"+item.Ref, `{}`), 404, nil)
}
func TestCrewWorkRoutesRetainTransportGuardsAndPOSTReplay(t *testing.T) {
	f := newAgentFixture(t)
	for _, route := range crewWorkContractRoutes {
		path := strings.ReplaceAll(route.Path, "{cid}", "missing")
		path = strings.ReplaceAll(path, "{ref}", "missing")
		crewTestRead(t, request(t, f.server, route.Method, path, `{}`, nil, "127.0.0.1:4000"), 401, nil)
		body := `{}`
		if strings.HasSuffix(path, "/bring-in-helpers") {
			body = `{"member_id":"missing"}`
		}
		if strings.HasSuffix(path, "/items") {
			body = `{"ref":"api#1"}`
		}
		crewTestRead(t, f.request(t, route.Method, path, body), 404, nil)
	}
	c := workTestCrew(t, f)
	path := "/app/v1/crews/" + c.ID + "/items"
	headers := bearer()
	headers.Set("Idempotency-Key", "link")
	first := request(t, f.server, "POST", path, `{"ref":"api#1"}`, headers, "127.0.0.1:4000")
	second := request(t, f.server, "POST", path, `{"ref":"api#1"}`, headers, "127.0.0.1:4000")
	crewTestRead(t, first, 201, nil)
	crewTestRead(t, second, 201, nil)
	if first.Body.String() != second.Body.String() || len(crewTestGet(t, f, c.ID).Items) != 1 {
		t.Fatal("link replay duplicated reference")
	}
}
func TestCrewEndDateAsksOnceAndNeverCloses(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	now := time.Now().UTC()
	f.registry.mu.Lock()
	f.registry.now = func() time.Time { return now }
	f.registry.mu.Unlock()
	ends := now.Add(time.Hour)
	crewTestRead(t, f.request(t, "PATCH", "/app/v1/crews/"+c.ID, fmt.Sprintf(`{"ends_at":%q}`, ends.Format(time.RFC3339Nano))), 200, nil)
	f.registry.mu.Lock()
	now = now.Add(2 * time.Hour)
	f.registry.mu.Unlock()
	for i := 0; i < 2; i++ {
		if err := f.server.RefreshCrewWork(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	asks, err := f.registry.CrewWorkAsks()
	if err != nil || len(asks) != 1 {
		t.Fatalf("asks=%+v err=%v", asks, err)
	}
	if crewTestGet(t, f, c.ID).State != "active" {
		t.Fatal("end date silently closed crew")
	}
}
func TestCrewFinishRefusesOpenTasksAndInvalidManagerReport(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	path := "/app/v1/crews/" + c.ID
	f.engines[1].tasks = []session.PlanTaskRow{{ID: "running-task", Status: "running"}}
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 409, nil)
	f.engines[1].tasks = nil
	f.registry.mu.Lock()
	f.registry.live[c.ManagerID].conn.Engine = &crewProposalEngine{fakeAgentEngine: f.engines[0], reply: `{"shipped":""}`}
	f.registry.mu.Unlock()
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 502, nil)
	if crewTestGet(t, f, c.ID).State != "active" {
		t.Fatal("invalid report closed crew")
	}
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{"expect":{"version":`+strconv.Itoa(0)+`}}`), 409, nil)
}

func TestCrewSpendFreezesWhileClosedAndSurvivesMemberRemoval(t *testing.T) {
	f := newAgentFixture(t)
	f.server.Events().Close()
	c := workTestCrew(t, f)
	helper := crewTestGet(t, f, c.ID).Tree[1].AgentID
	f.registry.mu.Lock()
	engine := &crewCostEngine{fakeAgentEngine: f.engines[1], cost: 3}
	f.registry.live[helper].conn.Engine = engine
	f.registry.mu.Unlock()
	path := "/app/v1/crews/" + c.ID
	nativeCrewReport(t, f, c, "")
	crewTestRead(t, f.request(t, "POST", path+"/finish", `{}`), 200, nil)
	engine.mu.Lock()
	engine.cost = 8
	engine.mu.Unlock()
	if got := crewTestGet(t, f, c.ID); got.Spent == nil || got.Spent.USD != "3.00" {
		t.Fatal("closed crew counted independent work", got.Spent)
	}
	crewTestRead(t, f.request(t, "POST", path+"/reopen", `{}`), 200, nil)
	if got := crewTestGet(t, f, c.ID); got.Spent == nil || got.Spent.USD != "3.00" {
		t.Fatal("reopen counted the closed interval", got.Spent)
	}
	engine.mu.Lock()
	engine.cost = 9
	engine.mu.Unlock()
	crewTestRead(t, f.request(t, "POST", path+"/members/"+helper+"/remove", `{}`), 200, nil)
	if got := crewTestGet(t, f, c.ID); got.Spent == nil || got.Spent.USD != "4.00" {
		t.Fatal("removing member erased lifetime spend", got.Spent)
	}
	engine.mu.Lock()
	engine.cost = 15
	engine.mu.Unlock()
	if got := crewTestGet(t, f, c.ID); got.Spent == nil || got.Spent.USD != "4.00" {
		t.Fatal("removed member kept charging crew", got.Spent)
	}
}

func TestCrewNativeLogProducerPublishesEachNewLineOnce(t *testing.T) {
	f := newAgentFixture(t)
	c := workTestCrew(t, f)
	if err := f.server.RefreshCrewWork(context.Background()); err != nil {
		t.Fatal(err)
	}
	entry := teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: teams.ToRoom, Text: "Migration waits for review"}
	if err := teams.AppendTraffic(f.home, c.ID, entry); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.server.RefreshCrewWork(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	f.server.events.mu.Lock()
	defer f.server.events.mu.Unlock()
	count := 0
	for _, record := range f.server.events.ring {
		if record.kind == EventCrewLog && strings.Contains(string(record.data), entry.Text) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("native line published %d times", count)
	}
}
