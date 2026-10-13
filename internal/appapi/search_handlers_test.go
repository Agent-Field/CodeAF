package appapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/session"
)

func searchDecode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	var value T
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
func fakeSearchCrews(f *agentFixture, crews ...Crew) {
	f.server.Handle("GET /app/v1/crews", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { write(w, 200, CrewsResponse{Crews: crews}) }))
}
func createSearchAgent(t *testing.T, f *agentFixture, name string) Agent {
	t.Helper()
	a := readAgent(t, f.request(t, "POST", "/app/v1/agents", `{"start":"empty"}`), 201)
	body, _ := json.Marshal(map[string]string{"name": name})
	return readAgent(t, f.request(t, "PATCH", "/app/v1/agents/"+a.ID, string(body)), 200)
}
func TestSearchGroupsRankingAndClosedScope(t *testing.T) {
	f := newAgentFixture(t)
	fakeSearchCrews(f, Crew{ID: "crew", Name: "Migrate crew", State: "active"})
	titles := []string{"Premigration fix", "Plan migration", "Migration alpha", "Migration beta", "Migration gamma", "Migration closed"}
	ids := map[string]string{}
	for i, title := range titles {
		f.registry.now = func() time.Time { return time.Date(2026, 10, 12, 12, i, 0, 0, time.UTC) }
		ids[title] = createSearchAgent(t, f, title).ID
	}
	readAgent(t, f.request(t, "POST", "/app/v1/agents/"+ids["Migration closed"]+"/close", `{}`), 200)
	f.server.Handle("GET /v1/queue", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != bearer().Get("Authorization") {
			t.Error("factory lost bearer")
		}
		write(w, 200, factoryapi.Queue{Factory: factoryapi.Factory{Address: "solo"}, Jobs: []factoryapi.Job{{JobID: "job1", Title: "Migration job", State: "backlog"}, {JobID: "job2", Title: "Migration done", State: "done"}}})
	}))
	got := searchDecode[SearchResponse](t, f.request(t, "GET", "/app/v1/search?q=migra&kinds=agents_crews,jobs", ""))
	if len(got.Groups) != 2 || len(got.Groups[0].Rows) != 4 || len(got.Groups[1].Rows) != 2 {
		t.Fatalf("groups: %+v", got)
	}
	want := []string{"Migration gamma", "Migration beta", "Migration alpha", "Migrate crew"}
	for i, title := range want {
		if got.Groups[0].Rows[i].Title != title {
			t.Fatalf("row %d: %+v", i, got.Groups[0])
		}
	}
	if got.Groups[1].Rows[0].Source.Factory != "solo" {
		t.Fatalf("missing source: %+v", got)
	}
	closed := searchDecode[SearchResponse](t, f.request(t, "GET", "/app/v1/search?q=migra&kinds=agents_crews&include_closed=true", ""))
	if closed.Groups[0].Rows[0].ID != ids["Migration closed"] {
		t.Fatalf("closed missing: %+v", closed)
	}
	for _, test := range []struct {
		title string
		rank  int
	}{{"Migration alpha", 0}, {"Plan migration", 1}, {"Premigration fix", 2}} {
		if rank := searchRank(test.title, "migra"); rank != test.rank {
			t.Fatalf("%s rank %d", test.title, rank)
		}
	}
}
func TestSearchFilesUseContextAndFileInterface(t *testing.T) {
	f := newAgentFixture(t)
	fakeSearchCrews(f)
	a := createSearchAgent(t, f, "Unrelated title")
	path := filepath.Join(f.home, "webhook.txt")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := f.registry.disk(func(d *agentDisk) error {
		rec := d.Records[a.ID]
		rec.Agent.Context = []AgentContext{{Path: path, Kind: "file"}}
		d.Records[a.ID] = rec
		return nil
	}, true); err != nil {
		t.Fatal(err)
	}
	f.server.Handle("GET /app/v1/agents/{id}/files/find", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "webhook" {
			t.Fatalf("file query: %s", r.URL)
		}
		write(w, 200, FileFindResponse{Files: []FileStat{{Path: path, Exists: true}, {Path: filepath.Join(f.home, "webhook2.txt"), Exists: true}}})
	}))
	got := searchDecode[SearchResponse](t, f.request(t, "GET", "/app/v1/search?q=webhook&kinds=files", ""))
	if len(got.Groups) != 1 || len(got.Groups[0].Rows) != 2 || got.Groups[0].Rows[0].Source.ID != a.ID {
		t.Fatalf("files: %+v", got)
	}
	got = searchDecode[SearchResponse](t, f.request(t, "GET", "/app/v1/search?q=kind:file%20webhook&kinds=files", ""))
	if len(got.Groups[0].Rows) != 2 {
		t.Fatalf("file kind filtered agent inventory: %+v", got)
	}
}
func TestHistoryTranscriptFiltersAndAgentListExpansion(t *testing.T) {
	f := newAgentFixture(t)
	fakeSearchCrews(f, Crew{ID: "crew", Name: "Release", ManagerID: "not-a-manager", State: "closed", Report: &CrewReport{Markdown: "webhook report"}})
	a := createSearchAgent(t, f, "Saved discussion")
	readAgent(t, f.request(t, "POST", "/app/v1/agents/"+a.ID+"/close", `{}`), 200)
	if err := f.registry.disk(func(d *agentDisk) error {
		rec := d.Records[a.ID]
		// Search the saved transcript without attaching a model.
		raw := `{"type":"message","role":"user","content":"billing timeout webhook","timestamp":"2026-10-12T12:00:00Z"}` + "\n" + `{"type":"usage","usage":{"model":"m","input":10,"output":5,"costUsd":4.20,"calls":1},"timestamp":"2026-10-12T12:00:00Z"}` + "\n"
		if err := os.WriteFile(rec.SessionFile, []byte(raw), 0600); err != nil {
			return err
		}
		meta, err := session.LoadMeta(filepath.Dir(rec.SessionFile))
		if err != nil {
			return err
		}
		meta.SpentUSD = 4.20
		if err := session.SaveMeta(filepath.Dir(rec.SessionFile), meta); err != nil {
			return err
		}
		rec.Agent.CrewID = "crew"
		rec.Agent.Pinned = true
		d.Records[a.ID] = rec
		return nil
	}, true); err != nil {
		t.Fatal(err)
	}

	all := searchDecode[HistoryResponse](t, f.request(t, "GET", "/app/v1/history", ""))
	if len(all.Agents) != 1 || all.Agents[0].Spent == nil || all.Agents[0].Spent.USD != "4.20" {
		t.Fatalf("saved spend missing from unfiltered history: %+v", all)
	}
	q := `state:closed said:"billing timeout" kind:agent crew:Release model:test cost:>=4.20 since:2026-10-01 pinned:true`
	path := "/app/v1/history?q=" + url.QueryEscape(q)
	got := searchDecode[HistoryResponse](t, f.request(t, "GET", path, ""))
	if len(got.Agents) != 1 || got.Agents[0].ID != a.ID || len(got.Crews) != 0 {
		t.Fatalf("history: %+v", got)
	}
	expanded := searchDecode[AgentsResponse](t, f.request(t, "GET", "/app/v1/agents?q="+url.QueryEscape(q), ""))
	if len(expanded.Agents) != 1 {
		t.Fatalf("agent list: %+v", expanded)
	}
	crews := searchDecode[HistoryResponse](t, f.request(t, "GET", "/app/v1/history?q="+url.QueryEscape("kind:crew said:webhook"), ""))
	if len(crews.Crews) != 1 {
		t.Fatalf("crew report search: %+v", crews)
	}
	if len(f.opens) != 1 {
		t.Fatalf("search opened engines: %d", len(f.opens))
	}
}
func TestSearchAndHistoryValidateEmptyHomes(t *testing.T) {
	f := newAgentFixture(t)
	fakeSearchCrews(f)
	for _, path := range []string{"/app/v1/search", "/app/v1/history", "/app/v1/activity"} {
		w := f.request(t, "GET", path, "")
		if w.Code != 200 || strings.Contains(w.Body.String(), "null") {
			t.Fatalf("empty %s: %s", path, w.Body)
		}
		if w := request(t, f.server, "GET", path, "", nil, "127.0.0.1:4000"); w.Code != 401 {
			t.Fatalf("unguarded %s: %d", path, w.Code)
		}
		if w := f.request(t, "POST", path, `{}`); w.Code != 405 {
			t.Fatalf("method %s: %d", path, w.Code)
		}
	}
	for _, q := range []string{"bad:value", "since:no", "since:0d", "cost:-1", "cost:1.001", "kind:unknown", "state:unknown", "pinned:yes", `said:"open`, "said:"} {
		for _, path := range []string{"/app/v1/history", "/app/v1/agents", "/app/v1/search"} {
			if w := f.request(t, "GET", path+"?q="+url.QueryEscape(q), ""); w.Code != 422 {
				t.Fatalf("%s %q: %d %s", path, q, w.Code, w.Body)
			}
		}
	}
	for _, suffix := range []string{"kinds=unknown", "include_closed=yes"} {
		if w := f.request(t, "GET", "/app/v1/search?"+suffix, ""); w.Code != 422 {
			t.Fatalf("%s: %d", suffix, w.Code)
		}
	}
	if w := f.request(t, "GET", "/app/v1/activity?since=no", ""); w.Code != 422 {
		t.Fatalf("activity date: %d", w.Code)
	}
}
func TestHistoryQueryComparisonAndRelativeDates(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		q    string
		item historyItem
		want bool
	}{
		{"cost:>4.20", historyItem{spent: &Money{USD: "4.21"}}, true},
		{"cost:<=4.20", historyItem{spent: &Money{USD: "4.20"}}, true},
		{"cost:4.20", historyItem{spent: &Money{USD: "4.19"}}, false},
		{"cost:0", historyItem{}, false},
		{"since:7d", historyItem{at: now.AddDate(0, 0, -7)}, true},
		{"since:7d", historyItem{at: now.AddDate(0, 0, -8)}, false},
		{"state:open", historyItem{state: "archived"}, false},
		{"state:pinned", historyItem{pinned: true}, true},
	} {
		t.Run(fmt.Sprintf("%s-%v", test.q, test.want), func(t *testing.T) {
			q, err := parseHistoryQuery(test.q, now)
			if err != nil {
				t.Fatal(err)
			}
			if got := q.match(test.item); got != test.want {
				t.Fatalf("match=%v", got)
			}
		})
	}
}

func TestSearchCrewRanksByManagerUseEvenWhenManagerNameDoesNotMatch(t *testing.T) {
	f := newAgentFixture(t)
	first := createSearchAgent(t, f, "Different manager name")
	f.registry.now = func() time.Time { return time.Date(2026, 10, 12, 13, 0, 0, 0, time.UTC) }
	second := createSearchAgent(t, f, "Another manager name")
	fakeSearchCrews(f, Crew{ID: "a", Name: "Migration older", ManagerID: first.ID, State: "active"}, Crew{ID: "z", Name: "Migration newer", ManagerID: second.ID, State: "active"})
	got := searchDecode[SearchResponse](t, f.request(t, "GET", "/app/v1/search?q=migra&kinds=crews", ""))
	if len(got.Groups) != 1 || len(got.Groups[0].Rows) != 2 || got.Groups[0].Rows[0].ID != "z" {
		t.Fatalf("crew usage rank: %+v", got)
	}
}
