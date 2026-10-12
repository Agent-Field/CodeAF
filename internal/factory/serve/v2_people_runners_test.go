package serve

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

type peopleV2Fixture struct {
	h    *Handler
	st   *store.Store
	now  time.Time
	cfg  Config
	seam factory.Seam
}

func peopleV2Setup(t *testing.T, mode string) *peopleV2Fixture {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &peopleV2Fixture{st: st, now: time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)}
	history, err := record.Open(st, record.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.seam = factory.Seam{Load: func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err }, SetRepos: st.SetRepos}
	f.cfg = Config{Name: "factory", Version: "test", Address: "https://factory.test", Mode: mode, OperatorLogin: "owner", AccessToken: "operator-secret", Store: st, Record: history, TokensPath: filepath.Join(st.Root(), "tokens.json"), PullWait: time.Millisecond, Now: func() time.Time { return f.now }, LeaseOptions: lease.Options{Now: func() time.Time { return f.now }}}
	f.h = New(f.seam, f.cfg)
	t.Cleanup(f.h.Close)
	return f
}

func (f *peopleV2Fixture) request(t *testing.T, method, path, token string, body any, status int, out any) string {
	t.Helper()
	data, _ := json.Marshal(body)
	text := ""
	if body != nil {
		text = string(data)
	}
	w := identityRequest(f.h, method, path, text, token, nil)
	if w.Code != status {
		t.Fatalf("%s %s: %d %s; want %d", method, path, w.Code, w.Body, status)
	}
	if out != nil {
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	return w.Body.String()
}

func (f *peopleV2Fixture) add(t *testing.T, handle string) factory.Item {
	t.Helper()
	it, err := f.st.Create(factory.Item{Title: "test work", Repo: "acme/api", State: factory.StateQueued, Assignee: handle, Stream: &factory.Stream{Cur: 0, Phases: []factory.Phase{{Name: "code", State: factory.PhaseRunning}}}})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

func TestV2RunnerCapacitySleepAndSixtySecondOffline(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	first, second := f.add(t, "sam"), f.add(t, "sam")
	f.add(t, "")
	f.add(t, "raj")
	var workers [3]api.RunnerAttached
	for i := range workers {
		f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "sam", Kind: api.RunnerMac, Checkouts: []api.Checkout{{Repo: "acme/api", Path: "/code/api"}}, TakesUnassigned: map[string]bool{"*": true}}, 201, &workers[i])
	}
	var list api.Runners
	raw := f.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
	compareJSONFixture(t, []byte(raw), "testdata/runners_v2.json")
	var pulls [2]api.RunnerPull
	for i := range pulls {
		f.request(t, "GET", "/v1/runners/pull", workers[i].Credential, nil, 200, &pulls[i])
	}
	f.request(t, "GET", "/v1/runners/pull", workers[2].Credential, nil, 204, nil)
	f.request(t, "POST", "/v1/runners/presence", workers[0].Credential, api.RunnerPresence{Asleep: true}, 200, nil)
	f.now = f.now.Add(2 * time.Hour)
	f.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
	if len(list.Runners) != 1 || list.Runners[0].State != api.RunnerAsleep || len(list.Runners[0].Jobs) != 2 {
		t.Fatalf("sleep: %+v", list)
	}
	f.request(t, "GET", "/v1/runners/pull", workers[2].Credential, nil, 204, nil)
	f.request(t, "POST", "/v1/runners/presence", workers[0].Credential, api.RunnerPresence{}, 200, nil)
	f.now = f.now.Add(lease.OfflineAfter - time.Nanosecond)
	f.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
	if list.Runners[0].State != api.RunnerBusy {
		t.Fatalf("before deadline: %+v", list)
	}
	f.now = f.now.Add(time.Nanosecond)
	f.request(t, "GET", "/v1/runners", "operator-secret", nil, 200, &list)
	if list.Runners[0].State != api.RunnerOffline {
		t.Fatalf("at deadline: %+v", list)
	}
	for _, id := range []int{first.ID, second.ID} {
		it, err := f.st.Get(id)
		if err != nil || it.State != factory.StateNeedsYou || it.Question != "Runner went offline mid-step" || factory.NeedsKind(it) != factory.NeedsRunnerOffline {
			t.Fatalf("offline job: %+v %v", it, err)
		}
	}
	f.request(t, "POST", "/v1/runners/leases/"+pulls[0].Lease.ID+"/report", workers[0].Credential, api.RunnerReport{Cost: 99}, 409, nil)
	f.request(t, "GET", "/v1/runners/pull", workers[2].Credential, nil, 204, nil)
	registry, err := os.ReadFile(filepath.Join(f.st.Root(), "runners.json"))
	if err != nil || strings.Contains(string(registry), workers[0].Credential) {
		t.Fatal("registry exposed credential")
	}
	reopened := New(f.seam, f.cfg)
	defer reopened.Close()
	if w := identityRequest(reopened, "GET", "/v1/runners", "", "operator-secret", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"owner":"sam"`) {
		t.Fatalf("restart: %d %s", w.Code, w.Body)
	}
}

func TestV2LinksGitHubPeopleAndOperatorRevocation(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "priya"}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	var created struct {
		Link api.AccessLink `json:"link"`
	}
	f.request(t, "POST", "/v1/access/links", "operator-secret", api.CreateAccessLink{Login: "priya", Name: "Priya Nair", ExpiresAt: f.now.Add(time.Hour)}, 201, &created)
	parsed, err := url.Parse(created.Link.URL)
	if err != nil {
		t.Fatal(err)
	}
	fragment, err := url.ParseQuery(parsed.Fragment)
	if err != nil {
		t.Fatal(err)
	}
	gate := fragment.Get("token")
	if gate == "" {
		t.Fatal("link did not contain its one-time returned secret")
	}
	var directory api.People
	raw := f.request(t, "GET", "/v1/people", "operator-secret", nil, 200, &directory)
	compareJSONFixture(t, []byte(raw), "testdata/people_v2.json")
	linksRaw := f.request(t, "GET", "/v1/access/links", "operator-secret", nil, 200, nil)
	compareJSONFixture(t, []byte(strings.ReplaceAll(linksRaw, created.Link.ID, "tok_fixture")), "testdata/access_links_v2.json")
	var activity struct {
		Factory api.Factory    `json:"factory"`
		Events  []record.Event `json:"events"`
	}
	f.request(t, "GET", "/v1/activity?kind=created+access+link", "operator-secret", nil, 200, &activity)
	if len(activity.Events) != 1 {
		t.Fatal("missing link Activity")
	}
	activity.Events[0].ID = "evt_fixture"
	activity.Events[0].Object = "link:tok_fixture"
	normalized, _ := json.Marshal(activity)
	compareJSONFixture(t, normalized, "testdata/activity_v2.json")
	f.request(t, "GET", "/v1/queue", gate, nil, 401, nil)
	f.request(t, "POST", "/v1/access/links", gate, api.CreateAccessLink{}, 401, nil)
	signed := signInHandle(t, f.h, g, &f.now, "priya", gate)
	f.request(t, "POST", "/v1/access/links", signed, api.CreateAccessLink{}, 403, nil)
	f.request(t, "PUT", "/v1/repos", signed, map[string]any{"watched": []string{"acme/api"}}, 403, nil)
	f.request(t, "GET", "/v1/people", "operator-secret", nil, 200, &directory)
	person := directory.People[0]
	for _, p := range directory.People {
		if p.Login == "priya" {
			person = p
		}
	}
	if person.Status != api.PersonActive || len(person.Sessions) != 1 {
		t.Fatalf("active person: %+v", person)
	}
	f.now = f.now.Add(api.PersonIdleAfter)
	f.request(t, "GET", "/v1/people", "operator-secret", nil, 200, &directory)
	for _, p := range directory.People {
		if p.Login == "priya" && p.Status != api.PersonAway {
			t.Fatalf("away deadline: %+v", p)
		}
	}
	it := f.add(t, "priya")
	var runner api.RunnerAttached
	f.request(t, "POST", "/v1/runners/attach", signed, api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "raj"}, 403, nil)
	f.request(t, "POST", "/v1/runners/attach", signed, api.RunnerAttach{Runner: "mbp", Name: "mbp", Owner: "priya", Kind: api.RunnerMac}, 201, &runner)
	var pull api.RunnerPull
	f.request(t, "GET", "/v1/runners/pull", runner.Credential, nil, 200, &pull)
	f.request(t, "DELETE", "/v1/people/priya/sessions", signed, nil, 403, nil)
	f.request(t, "DELETE", "/v1/people/priya/sessions", "operator-secret", nil, 200, nil)
	f.request(t, "GET", "/v1/me", signed, nil, 401, nil)
	f.request(t, "GET", "/v1/queue", gate, nil, 401, nil)
	f.request(t, "POST", "/v1/runners/leases/"+pull.Lease.ID+"/report", runner.Credential, api.RunnerReport{Cost: 9}, 401, nil)
	unassigned, err := f.st.Get(it.ID)
	if err != nil || unassigned.Person() != "" || unassigned.State != factory.StateNew {
		t.Fatalf("Unassigned: %+v %v", unassigned, err)
	}
	signed = signInHandle(t, f.h, g, &f.now, "priya", gate)
	f.request(t, "POST", "/v1/access/revoke-sessions", signed, nil, 401, nil)
	f.request(t, "POST", "/v1/access/revoke-sessions", "operator-secret", nil, 200, nil)
	f.request(t, "GET", "/v1/me", signed, nil, 401, nil)
	f.request(t, "DELETE", "/v1/access/links/"+created.Link.ID, "operator-secret", nil, 200, nil)
	f.request(t, "POST", "/v1/auth/device", gate, nil, 401, nil)
	events, err := f.cfg.Record.Since(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == record.LinkCreated || event.Kind == record.SessionsRevoked || event.Kind == record.LinkRevoked {
			if !event.Actor.Operator || !strings.Contains(event.Detail, "as operator") {
				t.Fatalf("operator missing: %+v", event)
			}
		}
	}
	exported := f.request(t, "GET", "/v1/activity?kind=created+access+link&format=csv", "operator-secret", nil, 200, nil)
	rows, err := csv.NewReader(strings.NewReader(exported)).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][9] != "created access link" || rows[1][12] != "true" {
		t.Fatalf("filtered CSV: %v %v", rows, err)
	}
	if strings.Contains(exported, gate) || strings.Contains(exported, signed) {
		t.Fatal("Activity exposed a credential")
	}
}

func TestV2SoloHasNoPeopleOrAssigneeAndRunsItsJobs(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	job := f.add(t, "someone")
	var people api.People
	f.request(t, "GET", "/v1/people", "operator-secret", nil, 200, &people)
	if people.Mode != api.FactorySolo || len(people.People) != 0 {
		t.Fatalf("solo people: %+v", people)
	}
	var q api.Queue
	f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &q)
	// A queued job has no approval to offer, even on a solo factory.
	if q.Jobs[0].AssignedTo != "" || q.Jobs[0].Assignee != "" || q.Jobs[0].Can.AssignToMe || q.Jobs[0].Can.Approve {
		t.Fatalf("solo assignee: %+v", q.Jobs[0])
	}
	var runner api.RunnerAttached
	f.request(t, "POST", "/v1/runners/attach", "operator-secret", api.RunnerAttach{Name: "this Mac", Owner: "owner", Kind: api.RunnerThisMac}, 201, &runner)
	var pull api.RunnerPull
	f.request(t, "GET", "/v1/runners/pull", runner.Credential, nil, 200, &pull)
	if pull.Job.ID != job.ID {
		t.Fatal("solo job did not run")
	}
}

func TestV2RepoOnOffIsRecordedAsOperator(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	f.request(t, "PUT", "/v1/repos/acme/api", "operator-secret", map[string]bool{"on": true}, 200, nil)
	repos, err := f.st.Repos()
	if err != nil || len(repos) != 1 || repos[0] != "acme/api" {
		t.Fatalf("repo on: %v %v", repos, err)
	}
	f.request(t, "PUT", "/v1/repos/acme/api", "operator-secret", map[string]bool{"on": false}, 200, nil)
	repos, err = f.st.Repos()
	if err != nil || len(repos) != 0 {
		t.Fatalf("repo off: %v %v", repos, err)
	}
	events, err := f.cfg.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 2 || events[0].Verb != "connected repo" || events[1].Verb != "disconnected repo" || !events[1].Actor.Operator {
		t.Fatalf("repo Activity: %+v %v", events, err)
	}
}
