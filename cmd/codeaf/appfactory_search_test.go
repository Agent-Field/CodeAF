package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// Search uses the same wire and queue as the jobs page, without opening it first.
func TestAppSearchJobs(t *testing.T) {
	search := func(t *testing.T, w *factoryWire, query string, limit int) []map[string]any {
		t.Helper()
		var reply struct {
			Groups []struct {
				Kind string
				Hits []map[string]any
			}
		}
		w.must("search.all", map[string]any{"query": query, "limit_per_group": limit}, &reply)
		for _, g := range reply.Groups {
			if g.Kind == "job" {
				if g.Hits == nil {
					t.Fatal("job hits must be an array")
				}
				return g.Hits
			}
		}
		t.Fatal("search.all omitted the job group")
		return nil
	}
	t.Run("local queue before any desk read", func(t *testing.T) {
		s := newLocalSetup(t)
		st := s.start()
		var resumes atomic.Int32
		s.backend.factories.resume = func(context.Context) { resumes.Add(1) }
		var jobs []factory.Item
		for _, it := range []factory.Item{
			{Tier: factory.TierOwner, Repo: "acme/api", Num: 12, Kind: factory.KindIssue, Title: "Repair Webhook delivery", Ask: &factory.JobAsk{Text: "Handle the kumquat header"}, State: factory.StateNew},
			{Tier: factory.TierOwner, Repo: "acme/api", Num: 13, Kind: factory.KindIssue, Title: "Webhook retries", State: factory.StateNew},
			{Tier: factory.TierOwner, Repo: "acme/api", Num: 14, Kind: factory.KindIssue, Title: "Webhook abandoned", State: factory.StateShipped, DoneHow: "dropped", DoneAt: time.Now()},
		} {
			kept, err := s.backend.local.host.floor.Create(it)
			if err != nil {
				t.Fatal(err)
			}
			jobs = append(jobs, kept)
		}
		for _, query := range []string{"DELIVERY", "repair delivery", "12", "#12", "api#12", "ACME/API#12"} {
			hits := search(t, s.factoryWire, query, 8)
			if len(hits) != 1 {
				t.Fatalf("%q: got %v, want one job", query, hits)
			}
			h := hits[0]
			if h["id"] != fmt.Sprintf("%s#%d", st.Factory, jobs[0].ID) || h["factory"] != st.Factory || h["document_id"] != float64(jobs[0].ID) || h["repo"] != "acme/api" || h["number"] != float64(12) || h["title"] != "Repair Webhook delivery" || h["state"] != "backlog" || h["kind"] != "job" {
				t.Fatalf("job wire: %v", h)
			}
			if _, ok := h["agent"]; ok {
				t.Fatalf("job has agent: %v", h)
			}
		}
		if hits := search(t, s.factoryWire, "KUMQUAT", 8); len(hits) != 1 {
			t.Fatalf("detail: %v", hits)
		}
		if hits := search(t, s.factoryWire, "webhook", 1); len(hits) != 1 {
			t.Fatalf("limit: %v", hits)
		}
		if hits := search(t, s.factoryWire, "abandoned", 8); len(hits) != 0 {
			t.Fatalf("dropped job: %v", hits)
		}
		if resumes.Load() != 0 {
			t.Fatal("search tried to resume the running factory")
		}
		if !strings.Contains(s.hello, `"search.jobs"`) {
			t.Fatal("hello omitted search.jobs")
		}
	})
	t.Run("parallel factories share one deadline and one limit", func(t *testing.T) {
		useLocalHome(t)
		w := newFactoryWire(t, testFactoryHub(t))
		var mu sync.Mutex
		var opens, releases int
		w.backend.open = fakeOpener(&fakeConversation{}, &opens, &releases, &mu)
		var created struct{ Agent appwire.Agent }
		w.must("agents.create", map[string]any{"workspace": t.TempDir(), "text": "Needle conversation"}, &created)
		w.must("agents.rename", map[string]any{"id": created.Agent.ID, "title": "Needle conversation"}, nil)
		var listed struct{ Agents []appwire.Agent }
		w.must("agents.list", map[string]any{}, &listed)
		if len(listed.Agents) != 1 || listed.Agents[0].ID != created.Agent.ID {
			t.Fatalf("agent fixture must be listed before testing search: %+v", listed)
		}

		var reads atomic.Int32
		attach := func(jobs []api.Job, hang bool) appwire.FactoryConnection {
			srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/about":
					json.NewEncoder(rw).Encode(map[string]any{"contract": api.Contract})
				case "/v1/me":
					json.NewEncoder(rw).Encode(map[string]any{"mode": "solo"})
				case "/v1/queue":
					reads.Add(1)
					if hang {
						<-r.Context().Done()
						return
					}
					if jobs == nil {
						rw.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					json.NewEncoder(rw).Encode(api.Queue{Jobs: jobs})
				default:
					http.NotFound(rw, r)
				}
			}))
			t.Cleanup(srv.Close)
			var c appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": srv.URL}, &c)
			return c
		}
		attach(nil, true)
		attach(nil, true)
		attach(nil, false)
		old := time.Now().Add(-time.Hour)
		newer := time.Now()
		attach([]api.Job{{ID: 1, Title: "Needle oldest", CreatedAt: old, State: "backlog"}, {ID: 2, Title: "Other", Ask: &api.JobAsk{Text: "needle"}, CreatedAt: newer, State: "backlog"}, {ID: 3, Title: "Needle dismissed", Dismissed: true, State: "backlog"}}, false)
		c := attach([]api.Job{{ID: 4, Title: "Needle newest", CreatedAt: newer, State: "queued"}}, false)
		started := time.Now()
		var reply struct {
			Groups []struct {
				Kind string
				Hits []map[string]any
			}
		}
		w.must("search.all", map[string]any{"query": "needle", "limit_per_group": 2}, &reply)
		var hits []map[string]any
		var foundAgent bool
		for _, group := range reply.Groups {
			if group.Kind == "job" {
				hits = group.Hits
			}
			if group.Kind == "agent" && len(group.Hits) == 1 && group.Hits[0]["id"] == created.Agent.ID {
				foundAgent = true
			}
		}
		if !foundAgent {
			t.Fatalf("agent results lost while factories timed out: %+v", reply)
		}
		if elapsed := time.Since(started); elapsed > 2*time.Second {
			t.Fatalf("search exceeded total deadline: %s", elapsed)
		}
		if reads.Load() != 5 {
			t.Fatalf("read %d queues, want all five", reads.Load())
		}
		if len(hits) != 2 || hits[0]["id"] != c.ID+"#4" || hits[1]["title"] != "Needle oldest" {
			t.Fatalf("global rank and limit: %v", hits)
		}
	})
	t.Run("search never resumes local factory", func(t *testing.T) {
		s := newLocalSetup(t)
		s.start()
		s.backend.closeLocalFactory()
		var resumes atomic.Int32
		s.backend.factories.resume = func(_ context.Context) { resumes.Add(1) }
		if hits := search(t, s.factoryWire, "needle", 8); len(hits) != 0 {
			t.Fatalf("stopped factory: %v", hits)
		}
		if resumes.Load() != 0 {
			t.Fatal("search tried to resume a stopped factory")
		}
	})
	t.Run("team visibility", func(t *testing.T) {
		useLocalHome(t)
		f := startActionFactory(t)
		f.github.rest = func(r *http.Request) (int, string, bool) {
			if r.URL.Path == "/repos/secret/private" {
				return http.StatusNotFound, `{}`, true
			}
			return 0, "", false
		}
		visible := f.create(t, factory.Item{Repo: "acme/api", Title: "Visible kumquat", State: factory.StateNew})
		f.create(t, factory.Item{Repo: "secret/private", Title: "Invisible kumquat", State: factory.StateNew})
		p := signedIn(t, f, 1, "alice")
		if hits := search(t, p.w, "kumquat", 8); len(hits) != 1 || hits[0]["document_id"] != float64(visible.ID) {
			t.Fatalf("want only the visible team job: %v", hits)
		}
	})
	t.Run("schema names job fields and keeps agent optional", func(t *testing.T) {
		raw, err := appwire.SchemaJSON()
		if err != nil {
			t.Fatal(err)
		}
		var schema struct {
			Defs map[string]struct {
				Properties map[string]struct {
					Type string
					Enum []string
				}
				Required []string
			} `json:"$defs"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"Hit", "HitGroup"} {
			kinds := schema.Defs[name].Properties["kind"].Enum
			if strings.Join(kinds, ",") != "agent,crew,task,message,job" {
				t.Fatalf("%s kind enum: %v", name, kinds)
			}
		}
		hit := schema.Defs["Hit"]
		if strings.Join(hit.Required, ",") != "kind,id,title" {
			t.Fatalf("required hit fields: %v", hit.Required)
		}
		for name, typ := range map[string]string{"factory": "string", "document_id": "integer", "repo": "string", "number": "integer", "state": "string"} {
			if hit.Properties[name].Type != typ {
				t.Fatalf("%s schema: %+v", name, hit.Properties[name])
			}
		}
	})
	t.Run("older hit JSON stays identical", func(t *testing.T) {
		for _, kind := range []string{"agent", "crew", "task", "message"} {
			raw, err := json.Marshal(appwire.Hit{Kind: kind, ID: "a", Agent: "a", Title: "Title"})
			want := fmt.Sprintf(`{"kind":%q,"id":"a","agent":"a","title":"Title"}`, kind)
			if err != nil || string(raw) != want {
				t.Fatalf("legacy hit: %s, %v; want %s", raw, err, want)
			}
		}
	})
}
