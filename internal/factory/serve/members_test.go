package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestItemRepoJoinsOwnerAndName(t *testing.T) {
	if got := itemRepo(factory.Item{Repo: "API", Product: "Acme"}); got != "acme/api" {
		t.Fatalf("product join = %q", got)
	}
	if got := itemRepo(factory.Item{Repo: "acme/api"}); got != "acme/api" {
		t.Fatalf("full name = %q", got)
	}
	if got := itemRepo(factory.Item{Repo: "notes"}); got != "" {
		t.Fatalf("local name = %q", got)
	}
}

func TestMemberAndSessionFixturesMatchSignIn(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, row := identityHandler(t, g)
	device := startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, row.Token, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil || api.Contract != 1 {
		t.Fatalf("sign-in = %d %s", w.Code, w.Body)
	}
	members := identityRequest(h, "GET", "/v1/members", "", signed.AccessToken, nil)
	if members.Code != 200 {
		t.Fatal(members.Body)
	}
	compareJSONFixture(t, members.Body.Bytes(), "testdata/members.json")
	sessions := identityRequest(h, "GET", "/v1/me/sessions", "", signed.AccessToken, nil)
	if sessions.Code != 200 {
		t.Fatal(sessions.Body)
	}
	compareJSONFixture(t, normalizeSessionIDs(t, sessions.Body.Bytes()), "testdata/sessions.json")
	if strings.Contains(members.Body.String()+sessions.Body.String(), "github-access-secret") {
		t.Fatal("GitHub token was returned")
	}
}

func TestSessionsSignOutOneAndTheOthers(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, row := identityHandler(t, g)
	priya1 := signInHandle(t, h, g, now, "priya", row.Token)
	priya2 := signInHandle(t, h, g, now, "priya", row.Token)
	raj := signInHandle(t, h, g, now, "raj", "floor-secret")

	list := sessionList(t, h, priya2)
	if len(list.Sessions) != 2 || countCurrent(list) != 1 {
		t.Fatalf("priya sessions = %+v", list.Sessions)
	}
	var rajID string
	for _, session := range sessionList(t, h, raj).Sessions {
		rajID = session.ID
	}
	if w := identityRequest(h, "DELETE", "/v1/me/sessions/"+rajID, "", priya2, nil); w.Code != 404 || !strings.Contains(w.Body.String(), "session not found") {
		t.Fatalf("delete someone else = %d %s", w.Code, w.Body)
	}
	if got := sessionList(t, h, raj); len(got.Sessions) != 1 {
		t.Fatalf("raj lost a session: %+v", got.Sessions)
	}
	w := identityRequest(h, "POST", "/v1/me/sessions/signout-others", "", priya2, nil)
	var signedOut api.SignedOut
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signedOut) != nil || signedOut.SignedOut != 1 {
		t.Fatalf("sign out others = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/me/sessions", "", priya1, nil); w.Code != 401 {
		t.Fatalf("signed-out session still open: %d %s", w.Code, w.Body)
	}
	left := sessionList(t, h, priya2)
	if len(left.Sessions) != 1 || !left.Sessions[0].Current {
		t.Fatalf("remaining = %+v", left.Sessions)
	}
	if w := identityRequest(h, "DELETE", "/v1/me/sessions/"+left.Sessions[0].ID, "", priya2, nil); w.Code != 200 {
		t.Fatalf("delete current = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/me", "", priya2, nil); w.Code != 401 {
		t.Fatalf("deleted session still open: %d %s", w.Code, w.Body)
	}
	var members api.Members
	if w := identityRequest(h, "GET", "/v1/members", "", raj, nil); w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &members) != nil {
		t.Fatal(w.Body)
	}
	counts := map[string]int{}
	for _, member := range members.Members {
		counts[member.Handle] = member.Sessions
	}
	if counts["priya"] != 0 || counts["raj"] != 1 || members.Members[0].Name == "" {
		t.Fatalf("members = %+v", members.Members)
	}
}

func TestTwoViewersSeeDifferentQueuesAndCounts(t *testing.T) {
	h, clock, g := visibilityFloor(t, map[string]map[string]bool{
		"priya": {"acme/api": true},
		"raj":   {"acme/web": true},
		"mina":  {},
	})
	ctx := context.Background()
	if _, err := h.config.Record.Append(ctx, record.Event{Kind: record.Took, Job: 1, Handle: "priya", Detail: "api-secret-detail"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.config.Record.Append(ctx, record.Event{Kind: record.Took, Job: 2, Handle: "raj", Detail: "web-secret-detail"}); err != nil {
		t.Fatal(err)
	}
	owner := queueAs(t, h, "", true)
	if len(owner.Jobs) != 3 || owner.Capacity.Queued != 3 || owner.Capacity.Daily != 9 || owner.Since.Shipped != 4 || owner.Reason != "" || g.hits != 0 {
		t.Fatalf("owner queue = jobs %d queued %d daily %v shipped %d reason %q hits %d", len(owner.Jobs), owner.Capacity.Queued, owner.Capacity.Daily, owner.Since.Shipped, owner.Reason, g.hits)
	}

	priya := signViewer(t, h, g, clock, "priya")
	raj := signViewer(t, h, g, clock, "raj")
	mina := signViewer(t, h, g, clock, "mina")

	first := queueAs(t, h, priya, false)
	if g.hits != 2 {
		t.Fatalf("first view asked GitHub %d times", g.hits)
	}
	again := queueAs(t, h, priya, false)
	if g.hits != 2 {
		t.Fatalf("cached view asked GitHub again: %d", g.hits)
	}
	if !reflect.DeepEqual(jobIDs(first), jobIDs(again)) {
		t.Fatalf("cache changed the queue: %v %v", jobIDs(first), jobIDs(again))
	}
	*clock = clock.Add(repoVisibilityTTL)
	if queue := queueAs(t, h, priya, false); g.hits != 4 || len(queue.Jobs) != 2 {
		t.Fatalf("after 10 minutes hits=%d jobs=%d", g.hits, len(queue.Jobs))
	}
	if len(first.Jobs) != 2 || first.Capacity.Queued != 2 || first.Capacity.Daily != 2 || first.Since.Shipped != 0 || len(first.Repos) != 1 || first.Repos[0].Name != "acme/api" || first.Reason != "" {
		t.Fatalf("priya queue = %+v repos %+v", first.Capacity, first.Repos)
	}
	if ids := jobIDs(first); !reflect.DeepEqual(ids, []int{1, 3}) {
		t.Fatalf("priya jobs = %v", ids)
	}

	other := queueAs(t, h, raj, false)
	if len(other.Jobs) != 1 || other.Jobs[0].ID != 2 || other.Capacity.Queued != 1 || other.Capacity.Daily != 0.25 || len(other.Repos) != 1 || other.Repos[0].Name != "acme/web" {
		t.Fatalf("raj queue = jobs %+v queued %d daily %v repos %+v", other.Jobs, other.Capacity.Queued, other.Capacity.Daily, other.Repos)
	}
	priyaBody := identityRequest(h, "GET", "/v1/queue", "", priya, nil).Body.String()
	rajBody := identityRequest(h, "GET", "/v1/queue", "", raj, nil).Body.String()
	if strings.Contains(priyaBody, "acme/web") || strings.Contains(priyaBody, "web one") || strings.Contains(rajBody, "api one") || strings.Contains(priyaBody, "gh-priya") {
		t.Fatal("a queue leaked the other repository or a GitHub token")
	}

	none := queueAs(t, h, mina, false)
	if none.Reason != api.ReasonNoVisibleRepos || len(none.Jobs) != 0 || len(none.Repos) != 0 || none.Capacity.Queued != 0 || none.Since.Shipped != 0 {
		t.Fatalf("empty view = reason %q jobs %d repos %d queued %d shipped %d", none.Reason, len(none.Jobs), len(none.Repos), none.Capacity.Queued, none.Since.Shipped)
	}

	if w := identityRequest(h, "GET", "/v1/jobs/2", "", priya, nil); w.Code != 404 {
		t.Fatalf("hidden job = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/jobs/1", "", priya, nil); w.Code != 200 || strings.Contains(w.Body.String(), "acme/web") {
		t.Fatalf("visible job = %d %s", w.Code, w.Body)
	}
	var repos struct {
		Watched   []string
		Available []struct{ Full string }
	}
	w := identityRequest(h, "GET", "/v1/repos", "", priya, nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &repos) != nil || !reflect.DeepEqual(repos.Watched, []string{"acme/api"}) || len(repos.Available) != 1 {
		t.Fatalf("repos = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/repos/acme/web/pipeline", "", priya, nil); w.Code != 404 || !strings.Contains(w.Body.String(), "repository not found") {
		t.Fatalf("hidden pipeline = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/repos/acme/api/pipeline", "", priya, nil); w.Code != 200 {
		t.Fatalf("visible pipeline = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/repos/acme/web/dryrun", "", priya, nil); w.Code != 404 {
		t.Fatalf("hidden dry run = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "POST", "/v1/jobs", `{"repo":"acme/web","title":"nope"}`, priya, nil); w.Code != 404 {
		t.Fatalf("hidden new job = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "POST", "/v1/jobs", `{"repo":"acme/api","title":"yes"}`, priya, nil); w.Code != 201 {
		t.Fatalf("visible new job = %d %s", w.Code, w.Body)
	}

	audit := identityRequest(h, "GET", "/v1/audit", "", priya, nil)
	if audit.Code != 200 || strings.Contains(audit.Body.String(), "web-secret-detail") || !strings.Contains(audit.Body.String(), "api-secret-detail") {
		t.Fatalf("audit = %s", audit.Body)
	}
	csv := identityRequest(h, "GET", "/v1/audit?format=csv", "", priya, nil)
	if csv.Code != 200 || strings.Contains(csv.Body.String(), "web-secret-detail") || !strings.Contains(csv.Body.String(), "api-secret-detail") {
		t.Fatalf("csv = %s", csv.Body)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.RemoteAddr = "192.0.2.1:9"
	req.Header.Set("Authorization", "Bearer "+priya)
	if h.viewerWantsTick(req, "2") || !h.viewerWantsTick(req, "1") || !h.viewerWantsTick(req, "*") {
		t.Fatal("job ticks were not filtered")
	}
	loop := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	loop.RemoteAddr = "127.0.0.1:9"
	if !h.viewerWantsTick(loop, "2") {
		t.Fatal("the owner lost a tick")
	}
}

type scriptGitHub struct {
	t     *testing.T
	mu    chan struct{}
	login string
	allow map[string]map[string]bool
	hits  int
}

func (g *scriptGitHub) client() *http.Client {
	if g.mu == nil {
		g.mu = make(chan struct{}, 1)
		g.mu <- struct{}{}
	}
	return &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		g.t.Helper()
		<-g.mu
		defer func() { g.mu <- struct{}{} }()
		body, status := "", http.StatusOK
		if r.Header.Get("Accept") != "application/json" {
			g.t.Fatal("GitHub request needs JSON")
		}
		switch {
		case r.URL.String() == "https://github.com/login/device/code":
			if err := r.ParseForm(); err != nil {
				g.t.Fatal(err)
			}
			if r.Form.Get("scope") != githubSignInScope {
				g.t.Fatalf("scope = %q", r.Form.Get("scope"))
			}
			body = `{"device_code":"github-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","interval":5,"expires_in":900}`
		case r.URL.String() == "https://github.com/login/oauth/access_token":
			body = fmt.Sprintf(`{"access_token":"gh-%s","scope":"repo","token_type":"bearer"}`, g.login)
		case r.URL.String() == "https://api.github.com/user":
			login := strings.TrimPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "gh-")
			body = fmt.Sprintf(`{"login":%q,"name":%q,"avatar_url":"https://avatars.githubusercontent.com/u/7"}`, login, login)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/"):
			g.hits++
			login := strings.TrimPrefix(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), "gh-")
			full := strings.ToLower(strings.TrimPrefix(r.URL.Path, "/repos/"))
			if !g.allow[login][full] {
				status = http.StatusNotFound
			}
			body = `{}`
		default:
			g.t.Fatalf("unexpected GitHub URL %s", r.URL)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func visibilityFloor(t *testing.T, allow map[string]map[string]bool) (*Handler, *time.Time, *scriptGitHub) {
	t.Helper()
	g := &scriptGitHub{t: t, allow: allow}
	start := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	clock := start
	snap := func() factory.Snapshot {
		return factory.Snapshot{
			Items: []factory.Item{
				{ID: 1, Repo: "acme/api", Title: "api one", State: factory.StateQueued, Stream: &factory.Stream{Spent: 1.5}},
				{ID: 2, Repo: "acme/web", Title: "web one", State: factory.StateQueued, Stream: &factory.Stream{Spent: 0.25}},
				{ID: 3, Repo: "acme/api", Title: "api two", State: factory.StateQueued, Stream: &factory.Stream{Spent: 0.5}},
			},
			Repos: []factory.Repo{{Name: "acme/api"}, {Name: "acme/web"}},
			Daily: 9,
			Shift: factory.Shift{Since: start, Shipped: 4, Arrived: 2, Spent: 3},
		}
	}
	seam := factory.Seam{
		Load: func() (factory.Snapshot, error) { return snap(), nil },
		Repos: func(context.Context) ([]string, []factory.RepoInfo, error) {
			return []string{"acme/api", "acme/web"}, []factory.RepoInfo{
				{Full: "acme/api", Name: "api", Owner: "acme"},
				{Full: "acme/web", Name: "web", Owner: "acme"},
			}, nil
		},
		RecipeAt: func(string) (factory.Recipe, []factory.Problem, string, error) {
			return factory.Recipe{Source: "defaults"}, nil, "", nil
		},
		New: func(string, string) (int, error) { return 9, nil },
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return clock }})
	if err != nil {
		t.Fatal(err)
	}
	h := New(seam, Config{Address: "http://factory", Version: "test", AccessToken: "floor-secret", GitHubClientID: "client", GitHubHTTPClient: g.client(), Record: rec})
	h.sessions.now = func() time.Time { return clock }
	t.Cleanup(h.Close)
	return h, &clock, g
}

func signViewer(t *testing.T, h *Handler, g *scriptGitHub, clock *time.Time, login string) string {
	t.Helper()
	<-g.mu
	g.login = login
	g.mu <- struct{}{}
	return signInHandle(t, h, nil, clock, login, "floor-secret")
}

func signInHandle(t *testing.T, h *Handler, g *fakeGitHub, clock *time.Time, login, gate string) string {
	t.Helper()
	if g != nil {
		g.login = login
	}
	device := startIdentity(t, h, gate, nil)
	*clock = clock.Add(5 * time.Second)
	w := pollIdentity(h, device, gate, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil || signed.Me.Handle != login {
		t.Fatalf("sign in %s = %d %s", login, w.Code, w.Body)
	}
	return signed.AccessToken
}

func queueAs(t *testing.T, h *Handler, token string, loopback bool) api.Queue {
	t.Helper()
	var w *httptest.ResponseRecorder
	if token == "" {
		r := httptest.NewRequest(http.MethodGet, "/v1/queue", nil)
		r.RemoteAddr = "192.0.2.1:9"
		if loopback {
			r.RemoteAddr = "127.0.0.1:9"
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, r)
	} else {
		w = identityRequest(h, http.MethodGet, "/v1/queue", "", token, nil)
	}
	var q api.Queue
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil {
		t.Fatalf("queue = %d %s", w.Code, w.Body)
	}
	return q
}

func jobIDs(q api.Queue) []int {
	ids := make([]int, len(q.Jobs))
	for i, job := range q.Jobs {
		ids[i] = job.ID
	}
	return ids
}

func sessionList(t *testing.T, h *Handler, token string) api.Sessions {
	t.Helper()
	w := identityRequest(h, http.MethodGet, "/v1/me/sessions", "", token, nil)
	var list api.Sessions
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatalf("sessions = %d %s", w.Code, w.Body)
	}
	return list
}

func countCurrent(list api.Sessions) int {
	n := 0
	for _, session := range list.Sessions {
		if session.Current {
			n++
		}
	}
	return n
}

func compareJSONFixture(t *testing.T, raw []byte, path string) {
	t.Helper()
	wantRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want, got any
	if err := json.Unmarshal(wantRaw, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("got %s\nwant %s", raw, wantRaw)
	}
}

func normalizeSessionIDs(t *testing.T, raw []byte) []byte {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	sessions, _ := body["sessions"].([]any)
	for _, item := range sessions {
		session := item.(map[string]any)
		if id, _ := session["id"].(string); strings.HasPrefix(id, "session_") {
			session["id"] = "session_fixture"
		}
	}
	out, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
