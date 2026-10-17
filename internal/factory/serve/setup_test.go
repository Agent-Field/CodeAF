package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

func waitSetup(t *testing.T, h *Handler) {
	t.Helper()
	h.setupMu.Lock()
	done := h.setupDone
	busy := h.setupBusy
	h.setupMu.Unlock()
	if !busy {
		return
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("setup worker did not finish")
	}
}
func setupGitHub(t *testing.T, f *peopleV2Fixture, fail *bool) {
	t.Helper()
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	f.h.config.ProfileDir = t.TempDir()
	f.h.seam.ConnectGitHub = func(_ context.Context, token string) error {
		if token != "setup-secret" {
			return fmt.Errorf("bad token")
		}
		return config.WriteGitHubToken(f.h.config.ProfileDir, token)
	}
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "Bearer setup-secret" {
			t.Errorf("unexpected credential")
		}
		body, status := "", 200
		switch r.URL.Path {
		case "/user":
			body = `{"login":"owner"}`
		case "/user/repos":
			body = `[{"full_name":"acme/api","open_issues_count":3},{"full_name":"acme/old","archived":true}]`
		case "/repos/acme/api/issues":
			if *fail {
				status, body = 503, `{}`
			} else {
				body = `[{"number":1,"title":"First","body":"body","html_url":"https://github.com/acme/api/issues/1","assignee":{"login":"sam"},"labels":[{"name":"ready"}]},{"number":2,"title":"Second"},{"number":3,"title":"Pull","pull_request":{"url":"pr"}}]`
			}
		default:
			t.Errorf("unexpected GitHub path %s", r.URL.Path)
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
}
func TestFactorySetupIngestRetryCheckoutAndRestart(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := true
	setupGitHub(t, f, &fail)
	var setup api.FactorySetup
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, &setup)
	if setup.State != "github" || setup.GitHub.Connected {
		t.Fatalf("initial: %+v", setup)
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &setup)
	if setup.State != "repos" || !setup.GitHub.Connected || len(setup.Repos) != 2 || !setup.Repos[1].Archived {
		t.Fatalf("discovery: %+v", setup)
	}
	f.request(t, "POST", "/v1/factory-setup", "operator-secret", api.FactorySetupRequest{ID: "local", Verb: "select_repos", RepoIDs: []string{"acme/old"}}, 409, nil)
	f.request(t, "POST", "/v1/factory-setup", "operator-secret", api.FactorySetupRequest{ID: "local", Verb: "select_repos", RepoIDs: []string{"acme/api"}}, 200, &setup)
	waitSetup(t, f.h)
	if watched, _ := f.st.Repos(); len(watched) != 0 {
		t.Fatal("poll could see first-run repos before imports were held")
	}
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, &setup)
	if setup.Repos[0].Read != "waiting" || setup.State != "reading" {
		t.Fatalf("waiting: %+v", setup)
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "retry_read", RepoID: "acme/api"}, 409, nil)
	f.now = f.now.Add(setupWait)
	fail = false
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "retry_read", RepoID: "acme/api"}, 200, nil)
	waitSetup(t, f.h)
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, &setup)
	if setup.State != "checkouts" || setup.Repos[0].ReadCount != 2 || setup.Repos[0].OpenIssues != 2 {
		t.Fatalf("ingestion: %+v", setup)
	}
	items, err := f.st.List()
	if err != nil || len(items) != 2 || !items[0].SetupHeld || items[0].State != factory.StateNew {
		t.Fatalf("jobs: %+v %v", items, err)
	}
	for _, item := range items {
		if item.Num == 1 && item.Assignee != "sam" {
			t.Fatal("assignee lost")
		}
	}
	// A repeat read uses the same merge identity and cannot create another copy of either issue.
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "back"}, 200, nil)
	waitSetup(t, f.h)
	items, _ = f.st.List()
	if len(items) != 2 {
		t.Fatal("duplicate imports")
	}
	dir := t.TempDir()
	for _, args := range [][]string{{"init", dir}, {"-C", dir, "remote", "add", "origin", "https://github.com/Acme/API.git"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "checkout", RepoID: "acme/api", Path: dir}, 200, &setup)
	if setup.Repos[0].Clone != "found" || f.st.CheckoutDir("acme/api") != dir {
		t.Fatal("checkout not kept")
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "done"}, 200, &setup)
	if setup.State != "done" {
		t.Fatal(setup.State)
	}
	reopened := New(f.h.seam, f.h.config)
	defer reopened.Close()
	w := identityRequest(reopened, "GET", "/v1/factory-setup/local", "", "operator-secret", nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &setup) != nil || setup.State != "done" {
		t.Fatalf("restart: %d %s", w.Code, w.Body)
	}
	raw, _ := os.ReadFile(f.h.setupPath())
	if strings.Contains(string(raw), "setup-secret") {
		t.Fatal("credential persisted in progress")
	}
}

func TestFactorySetupRateLimitResumesAtBinaryDeadline(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	if err := factorygithub.SaveReadGrant(factorygithub.GrantPath(f.cfg.TokensPath), factorygithub.ReadGrant{Token: "read-only", Login: "owner", GrantedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	resumed := make(chan struct{})
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 2 {
			close(resumed)
		}
		status, body := 200, `[]`
		header := http.Header{}
		if calls == 1 {
			status = 429
			header.Set("Retry-After", "60")
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	out := setupState{View: api.FactorySetup{ID: "local", State: "reading", Mode: "team", Repos: []api.SetupRepo{{ID: "acme/api", Selected: true, Read: "next", Clone: "missing"}}}, Waiting: map[string]time.Time{}}
	if err := f.h.saveSetup(out); err != nil {
		t.Fatal(err)
	}
	var setup api.FactorySetup
	f.h.Close()
	f.h = New(f.seam, f.h.config)
	t.Cleanup(f.h.Close)
	waitSetup(t, f.h)
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, &setup)
	if setup.Repos[0].Read != "rate_limited" || setup.Repos[0].ResumesAt == nil || calls != 1 {
		t.Fatalf("rate limit: %+v calls %d", setup, calls)
	}
	f.now = f.now.Add(time.Minute)
	f.h.setupMu.Lock()
	if f.h.setupTimer == nil {
		f.h.setupMu.Unlock()
		t.Fatal("engine did not schedule the rate-limit deadline")
	}
	f.h.setupTimer.Reset(0)
	f.h.setupMu.Unlock()
	select {
	case <-resumed:
	case <-time.After(5 * time.Second):
		t.Fatal("reading did not resume without a setup poll")
	}
	waitSetup(t, f.h)
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, &setup)
	if setup.State != "checkouts" || setup.Repos[0].Read != "empty" || calls != 2 {
		t.Fatalf("resumed: %+v calls %d", setup, calls)
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "write-secret"}, 409, nil)
}

func joinLinkRequest(t *testing.T, h *Handler, method, link, verb string, cookies []*http.Cookie) (*httptest.ResponseRecorder, api.FactoryAccess) {
	t.Helper()
	path := "/v1/factory-access?link=" + url.QueryEscape(link)
	body := ""
	if method == "POST" {
		data, _ := json.Marshal(api.FactoryAccessRequest{Link: link, Verb: verb})
		body = string(data)
	}
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "203.0.113.12:4567"
	for _, cookie := range cookies {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out api.FactoryAccess
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("access: %d %s", w.Code, w.Body)
	}
	return w, out
}
func TestFactoryAccessValidityCancelJoinAndCallerIsolation(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "sam"}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	token, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	token.AccessLink = true
	token.Created = f.now
	token.Expires = f.now.Add(time.Hour)
	if err = tokens.Add(f.cfg.TokensPath, token); err != nil {
		t.Fatal(err)
	}
	link := accessLinkURL(f.cfg.Address, token)
	_, out := joinLinkRequest(t, f.h, "GET", link, "", nil)
	if out.State != "ready" {
		t.Fatal(out)
	}
	w, out := joinLinkRequest(t, f.h, "POST", link, "sign_in", nil)
	cookies := w.Result().Cookies()
	if out.State != "waiting" || out.SignInURL == "" || out.UserCode == "" {
		t.Fatal(out)
	}
	_, other := joinLinkRequest(t, f.h, "GET", link, "", nil)
	if other.State != "ready" {
		t.Fatal("another caller inherited pending flow")
	}
	_, out = joinLinkRequest(t, f.h, "POST", link, "cancel", cookies)
	if out.State != "ready" {
		t.Fatal(out)
	}
	w, out = joinLinkRequest(t, f.h, "POST", link, "sign_in", nil)
	cookies = w.Result().Cookies()
	f.now = f.now.Add(time.Second)
	_, out = joinLinkRequest(t, f.h, "GET", link, "", cookies)
	if out.State != "waiting" {
		t.Fatal(out)
	}
	f.now = f.now.Add(10 * time.Second)
	_, out = joinLinkRequest(t, f.h, "GET", link, "", cookies)
	if out.State != "joined" || out.AccessToken == "" {
		t.Fatal(out)
	}
	_, other = joinLinkRequest(t, f.h, "GET", link, "", nil)
	if other.State != "used_up" || other.AccessToken != "" {
		t.Fatal("completed session exposed to another opener")
	}
	if err = tokens.Remove(f.cfg.TokensPath, token.ID); err != nil {
		t.Fatal(err)
	}
	_, out = joinLinkRequest(t, f.h, "GET", link, "", cookies)
	if out.State != "revoked" || out.AccessToken != "" {
		t.Fatal(out)
	}
	token2, _ := tokens.Mint()
	token2.AccessLink = true
	token2.Expires = f.now
	_ = tokens.Add(f.cfg.TokensPath, token2)
	_, out = joinLinkRequest(t, f.h, "GET", accessLinkURL(f.cfg.Address, token2), "", nil)
	if out.State != "expired" {
		t.Fatal(out)
	}
	bad := identityRequest(f.h, "POST", "/v1/factory-access", `{"link":"oops","verb":"other"}`, "", nil)
	if bad.Code != 422 {
		t.Fatal(bad.Code)
	}
}

func TestFactoryDayAndRunningRetentionUseHostClock(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	mergedAt := f.now.Add(-23 * time.Hour)
	it, err := f.st.Create(factory.Item{Repo: "acme/api", Title: "merged", State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PRMerged, MergedAt: mergedAt}, Stream: &factory.Stream{Started: f.now.Add(-time.Hour)}, PublishStatus: &factory.Publishing{State: "opened", At: f.now.Add(-time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	var queue api.Queue
	f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &queue)
	if len(queue.Jobs) != 1 || !queue.Jobs[0].Display.RunningVisible {
		t.Fatalf("retained: %+v", queue.Jobs)
	}
	var day api.FactoryDay
	f.request(t, "GET", "/v1/factory-days/today", "operator-secret", nil, 200, &day)
	if day.ID != "2026-10-11" || day.Started != 1 || day.Posted != 1 || day.Merged != 0 || day.WaitingMedianSeconds != nil {
		t.Fatalf("day: %+v", day)
	}
	f.now = mergedAt.Add(api.RunningRetention)
	var job api.Job
	f.request(t, "GET", fmt.Sprintf("/v1/jobs/%d", it.ID), "operator-secret", nil, 200, &job)
	if job.Display.RunningVisible {
		t.Fatal("merged job stayed visible at 24 hours")
	}
	f.request(t, "GET", "/v1/queue", "operator-secret", nil, 200, &queue)
	if queue.Jobs[0].Display.RunningVisible {
		t.Fatal("queue disagrees with job read")
	}
	f.now = f.now.AddDate(0, 0, 1)
	f.request(t, "GET", "/v1/factory-days/today", "operator-secret", nil, 200, &day)
	if day.Started != 0 || day.Posted != 0 || day.Merged != 0 {
		t.Fatalf("new day: %+v", day)
	}
}

func TestFactorySetupAppScopeValidationKeepsCredentialsOffDisk(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := false
	setupGitHub(t, f, &fail)
	credentials := config.NewAppCredentials()
	credentials.Set("github", "")
	restore := config.UseAppCredentials(credentials)
	defer restore()
	var out api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "scope", Source: "token", Token: "setup-secret"}, 200, &out)
	if !out.GitHub.Connected || out.State != "repos" {
		t.Fatal(out)
	}
	if token, _ := credentials.Key("github"); token != "setup-secret" {
		t.Fatal("app handoff not connected")
	}
	if data, _ := os.ReadFile(config.ProfilePath(f.h.config.ProfileDir, "config.json")); strings.Contains(string(data), "setup-secret") {
		t.Fatal("app credential written to profile")
	}
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"X-Oauth-Scopes": []string{"read:user"}}, Body: io.NopCloser(strings.NewReader(`{"login":"owner"}`))}, nil
	})}
	if err := f.h.setupScope(context.Background(), "narrow-secret"); err == nil {
		t.Fatal("classic token without repo scope accepted")
	}
	f.request(t, "GET", "/v1/factory-setup/local", "bad-token", nil, 401, nil)
}

func TestFactorySetupPagedReadResumesFromSavedPage(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	if err := factorygithub.SaveReadGrant(factorygithub.GrantPath(f.cfg.TokensPath), factorygithub.ReadGrant{Token: "read-only", Login: "owner", GrantedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	first := []map[string]any{}
	for i := 1; i <= 100; i++ {
		first = append(first, map[string]any{"number": i, "title": fmt.Sprintf("issue %d", i)})
	}
	payload, _ := json.Marshal(first)
	firstCalls, secondCalls := 0, 0
	resumedCall := make(chan struct{})
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		header := http.Header{}
		status := 200
		body := `[{"number":101,"title":"last issue"}]`
		if r.URL.Query().Get("page") == "1" {
			firstCalls++
			body = string(payload)
		} else {
			secondCalls++
			if secondCalls == 1 {
				status = 429
				header.Set("Retry-After", "60")
				body = `{}`
			} else {
				close(resumedCall)
			}
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	out := setupState{View: api.FactorySetup{ID: "local", Mode: "team", State: "reading", Repos: []api.SetupRepo{{ID: "acme/api", Selected: true, Read: "next", Clone: "missing"}}}}
	if err := f.h.saveSetup(out); err != nil {
		t.Fatal(err)
	}
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, nil)
	waitSetup(t, f.h)
	saved, err := f.h.loadSetup("local")
	if err != nil || saved.Pages["acme/api"] != 2 || saved.View.Repos[0].ReadCount != 100 {
		t.Fatalf("cursor: %+v %v", saved, err)
	}
	f.h.Close()
	f.now = f.now.Add(time.Minute)
	resumed := New(f.seam, f.h.config)
	defer resumed.Close()
	select {
	case <-resumedCall:
	case <-time.After(5 * time.Second):
		t.Fatal("restart did not resume the saved page")
	}
	waitSetup(t, resumed)
	saved, err = resumed.loadSetup("local")
	items, _ := f.st.List()
	if err != nil || firstCalls != 1 || secondCalls != 2 || len(items) != 101 || saved.View.Repos[0].ReadCount != 101 || saved.View.State != "checkouts" {
		t.Fatalf("resumed: page1=%d page2=%d jobs=%d saved=%+v err=%v", firstCalls, secondCalls, len(items), saved, err)
	}
}

func TestFactoryAccessSignInCookiesAreNotReplayedAcrossOpeners(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	g := &fakeGitHub{t: t, login: "sam"}
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = g.client()
	token, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	token.AccessLink = true
	if err = tokens.Add(f.cfg.TokensPath, token); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(api.FactoryAccessRequest{Link: accessLinkURL(f.cfg.Address, token), Verb: "sign_in"})
	cookies := []string{}
	for range 2 {
		r := httptest.NewRequest("POST", "/v1/factory-access", strings.NewReader(string(data)))
		r.RemoteAddr = "203.0.113.12:4567"
		r.Header.Set("Idempotency-Key", "open-link")
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		if w.Code != 200 || len(w.Result().Cookies()) != 1 {
			t.Fatalf("sign-in: %d %s", w.Code, w.Body)
		}
		cookies = append(cookies, w.Result().Cookies()[0].Value)
	}
	if cookies[0] == cookies[1] {
		t.Fatal("an independent opener inherited the first sign-in cookie")
	}
}

func TestFactorySetupBackContinuesTheVerifiedConnectionWithoutAnotherToken(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := false
	setupGitHub(t, f, &fail)
	f.h.seam.GHLogin = func(context.Context) (string, error) { return "cli-other", nil }
	original := f.h.config.GitHubHTTPClient.Transport
	f.h.config.GitHubHTTPClient.Transport = githubTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/user" {
			return original.RoundTrip(r)
		}
		if r.Header.Get("Authorization") != "Bearer setup-secret" {
			t.Fatal("wrong connection credential")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"login":"owner","name":"Verified Owner","avatar_url":"https://avatars.githubusercontent.com/u/77"}`))}, nil
	})
	var out api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &out)
	if out.State != "repos" || !out.GitHub.Connected {
		t.Fatalf("initial connection: %+v", out)
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "back"}, 200, &out)
	if out.State != "github" || !out.GitHub.Connected {
		t.Fatalf("Back lost the verified connection: %+v", out)
	}
	f.h.seam.ConnectGitHub = func(context.Context, string) error {
		t.Fatal("Continue must not reconnect or require a token")
		return nil
	}
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "discover"}, 200, &out)
	if out.State != "repos" || !out.GitHub.Connected || out.GitHub.GH == nil || out.GitHub.GH.Login != "cli-other" || out.GitHub.Handle != "owner" || out.GitHub.Name != "Verified Owner" || out.GitHub.AvatarURL != "https://avatars.githubusercontent.com/u/77" || len(out.Repos) == 0 {
		t.Fatalf("Continue did not reuse verified identity: %+v", out)
	}
}
