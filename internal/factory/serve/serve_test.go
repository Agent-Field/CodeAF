package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

var now = time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
var config = serve.Config{Address: "http://127.0.0.1:7420", Version: "test", AccessToken: "floor-secret", PollingEvery: time.Minute}

func TestFixtureReadsOverHTTP(t *testing.T) {
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	var q api.Queue
	getJSON(t, s.URL+"/v1/queue", &q)
	want := api.FromSnapshot(factory.Fixture(now), "")
	want.Factory = api.Factory{Address: config.Address, Version: config.Version}
	// JSON normalizes time locations and nil maps, so compare against the
	// codec after the same wire round trip rather than its in-memory form.
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var expected api.Queue
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(q, expected) || len(q.Jobs) != 10 {
		t.Fatalf("queue did not carry the fixture: got %+v", q)
	}
	for _, job := range q.Jobs {
		var got struct {
			Factory api.Factory `json:"factory"`
			api.Job
		}
		getJSON(t, s.URL+"/v1/jobs/"+strconv.Itoa(job.ID), &got)
		if got.Factory != q.Factory || !reflect.DeepEqual(got.Job, job) {
			t.Fatalf("job %d differs from its queue row: %+v", job.ID, got)
		}
	}
}

func TestSettingsReadsUseSeam(t *testing.T) {
	seam := factory.FixtureSeam(now)
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "request")
	checkContext := func(ctx context.Context) {
		t.Helper()
		if ctx.Value(contextKey{}) != "request" {
			t.Fatal("seam did not receive the request context")
		}
	}
	seam.Repos = func(ctx context.Context) ([]string, []factory.RepoInfo, error) {
		checkContext(ctx)
		return []string{"owner/repo"}, []factory.RepoInfo{
			{Full: "owner/repo", Name: "repo", Owner: "owner", Private: true, Pushed: now, Open: -1, Dir: "/checkout"},
		}, nil
	}
	seam.GitHub = func(ctx context.Context) (factory.GitHubLink, error) {
		checkContext(ctx)
		return factory.GitHubLink{Login: "octocat", Via: factory.ViaGH}, nil
	}
	seam.RecipeAt = func(repo string) (factory.Recipe, []factory.Problem, string, error) {
		if repo != "owner/repo" {
			t.Fatalf("RecipeAt got %q", repo)
		}
		return factory.DefaultRecipe(), []factory.Problem{{Line: 7, Text: "bad", Why: "unknown step"}}, "", nil
	}
	h := serve.New(seam, config)
	read := func(path string, out any) {
		t.Helper()
		r := httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		checkJSON(t, w, http.StatusOK)
		if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
			t.Fatal(err)
		}
	}
	var repos struct {
		Watched   []string           `json:"watched"`
		Available []serve.Repository `json:"available"`
	}
	read("/v1/repos", &repos)
	if !reflect.DeepEqual(repos.Watched, []string{"owner/repo"}) || len(repos.Available) != 1 {
		t.Fatalf("repositories = %+v", repos)
	}
	if got := repos.Available[0]; got.Full != "owner/repo" || !got.Private || got.Open != -1 || got.Checkout != "/checkout" {
		t.Fatalf("repository facts = %+v", got)
	}
	var pipeline struct {
		api.Pipeline
		Problems []string `json:"problems"`
	}
	read("/v1/repos/owner/repo/pipeline", &pipeline)
	if pipeline.Source != "defaults" || len(pipeline.Steps) == 0 || len(pipeline.Problems) != 1 || !strings.Contains(pipeline.Problems[0], "line 7") {
		t.Fatalf("pipeline = %+v", pipeline)
	}
	var github struct {
		Login        string `json:"login"`
		Via          string `json:"via"`
		PollingEvery string `json:"polling_every"`
	}
	read("/v1/github", &github)
	if github.Login != "octocat" || github.Via != "gh" || github.PollingEvery != "1m0s" {
		t.Fatalf("github = %+v", github)
	}
	var me struct {
		CodeafVersion   string          `json:"codeaf_version"`
		Contract        int             `json:"contract"`
		Handle          string          `json:"handle"`
		Machines        []api.Worker    `json:"machines"`
		TakesUnassigned map[string]bool `json:"takes_unassigned"`
	}
	read("/v1/me", &me)
	if me.Contract != api.Contract || me.CodeafVersion == "" {
		t.Fatalf("me version = %q contract %d", me.CodeafVersion, me.Contract)
	}
	if me.Handle != "octocat" || me.Machines == nil || me.TakesUnassigned == nil {
		t.Fatalf("me = %+v", me)
	}
	var q api.Queue
	read("/v1/queue", &q)
	if q.Me != me.Handle {
		t.Fatalf("queue.me = %q, me.handle = %q", q.Me, me.Handle)
	}
}

func TestAccessUsesSocketPeer(t *testing.T) {
	for _, tc := range []struct {
		name, peer, auth string
		noToken, dup     bool
		status           int
	}{
		{"IPv4 loopback", "127.0.0.1:1234", "", false, false, 200},
		{"IPv6 loopback", "[::1]:1234", "", false, false, 200},
		{"mapped loopback", "[::ffff:127.0.0.1]:1234", "", false, false, 200},
		{"loopback without config token", "127.0.0.1:1234", "", true, false, 200},
		{"remote missing", "192.0.2.1:1234", "", false, false, 401},
		{"remote correct", "192.0.2.1:1234", "Bearer floor-secret", false, false, 200},
		{"remote scheme case", "192.0.2.1:1234", "bearer floor-secret", false, false, 200},
		{"remote wrong", "192.0.2.1:1234", "Bearer wrong", false, false, 401},
		{"remote wrong scheme", "192.0.2.1:1234", "Basic floor-secret", false, false, 401},
		{"remote extra words", "192.0.2.1:1234", "Bearer floor-secret extra", false, false, 401},
		{"no configured token", "192.0.2.1:1234", "Bearer floor-secret", true, false, 401},
		{"unknown peer", "", "", false, false, 401},
		{"hostname peer", "localhost:1234", "", false, false, 401},
		{"duplicate authorization", "192.0.2.1:1234", "Bearer floor-secret", false, true, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config
			if tc.noToken {
				cfg.AccessToken = ""
			}
			r := httptest.NewRequest(http.MethodGet, "/v1/queue", nil)
			r.RemoteAddr = tc.peer
			r.Host = "localhost"
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("Forwarded", "for=127.0.0.1")
			if tc.auth != "" {
				r.Header.Set("Authorization", tc.auth)
			}
			if tc.dup {
				r.Header.Add("Authorization", tc.auth)
			}
			w := httptest.NewRecorder()
			serve.New(factory.FixtureSeam(now), cfg).ServeHTTP(w, r)
			checkJSON(t, w, tc.status)
			if tc.status == 401 && w.Header().Get("WWW-Authenticate") == "" {
				t.Fatal("missing bearer challenge")
			}
		})
	}
}

func TestErrorsCarryFactory(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/v1/jobs/0", 400}, {"GET", "/v1/jobs/-1", 400},
		{"GET", "/v1/jobs/no", 400}, {"GET", "/v1/jobs/999999999999999999999", 400},
		{"GET", "/v1/jobs/999", 404}, {"GET", "/v1/missing", 404},
		{"GET", "/v1/jobs/0/thread", 400}, {"GET", "/v1/jobs/no/thread", 400},
		{"GET", "/v1/jobs/999/thread", 404},
		{"GET", "/v1/queue/", 404}, {"GET", "/v1//queue", 404},
		{"GET", "/v1/repos/a%2Fb/repo/pipeline", 400},
		{"GET", "/v1/repos/a%2Fb/repo/dryrun", 400},
		{"GET", "/v1/repos", 501}, {"GET", "/v1/repos/owner/repo/pipeline", 501},
		{"GET", "/v1/repos/owner/repo/dryrun", 501},
		{"POST", "/v1/queue", 405},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.RemoteAddr = "127.0.0.1:1234"
			w := httptest.NewRecorder()
			serve.New(factory.FixtureSeam(now), config).ServeHTTP(w, r)
			checkJSON(t, w, tc.status)
			var body map[string]any
			_ = json.Unmarshal(w.Body.Bytes(), &body)
			if body["error"] == nil {
				t.Fatal("missing error sentence")
			}
		})
	}
}

func TestSeamFailuresAndAbsentIdentity(t *testing.T) {
	seam := factory.FixtureSeam(now)
	seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{}, errors.New("store unavailable") }
	seam.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) {
		return nil, nil, errors.New("repos unavailable")
	}
	seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		return factory.Recipe{}, nil, "", errors.New("pipeline unavailable")
	}
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{}, errors.New("github unavailable")
	}
	for _, path := range []string{"/v1/queue", "/v1/jobs/1", "/v1/repos", "/v1/repos/o/r/pipeline", "/v1/repos/o/r/dryrun", "/v1/github", "/v1/me"} {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "127.0.0.1:1234"
		w := httptest.NewRecorder()
		serve.New(seam, config).ServeHTTP(w, r)
		checkJSON(t, w, 500)
	}
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	for _, path := range []string{"/v1/github", "/v1/me"} {
		var body map[string]any
		getJSON(t, s.URL+path, &body)
		if body["login"] != nil && body["login"] != "" || body["handle"] != nil && body["handle"] != "" {
			t.Fatalf("absent identity invented a login: %+v", body)
		}
	}
}

func TestFixtureQueueWithCurl(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("curl is not installed")
	}
	s := httptest.NewServer(serve.New(factory.FixtureSeam(now), config))
	defer s.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	data, err := exec.CommandContext(ctx, curl, "--fail", "--silent", "--show-error", "--noproxy", "*", s.URL+"/v1/queue").Output()
	if err != nil {
		t.Fatal(err)
	}
	var q api.Queue
	if err := json.Unmarshal(data, &q); err != nil || len(q.Jobs) != 10 {
		t.Fatalf("curl queue: jobs=%d, error=%v", len(q.Jobs), err)
	}
	t.Logf("curl %s/v1/queue returned %d jobs", s.URL, len(q.Jobs))
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("GET %s: %s %s", url, response.Status, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

func checkJSON(t *testing.T, w *httptest.ResponseRecorder, status int) {
	t.Helper()
	if w.Code != status || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response: status=%d headers=%v body=%s", w.Code, w.Header(), w.Body)
	}
	var body struct {
		Factory api.Factory `json:"factory"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Factory.Address != config.Address || body.Factory.Version != config.Version {
		t.Fatalf("factory identity = %+v", body.Factory)
	}
}
