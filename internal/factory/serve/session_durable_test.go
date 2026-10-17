package serve

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func durableSignIn(t *testing.T, h *Handler, now *time.Time, bearer string) (api.SignedIn, *http.Cookie) {
	t.Helper()
	device := startIdentity(t, h, bearer, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, bearer, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil {
		t.Fatalf("sign-in = %d %s", w.Code, w.Body)
	}
	return signed, w.Result().Cookies()[0]
}

func restartIdentity(t *testing.T, h *Handler) *Handler {
	t.Helper()
	h.Close()
	restarted := New(h.seam, h.config)
	t.Cleanup(restarted.Close)
	return restarted
}

func TestGitHubSessionsSurviveRestartWithPrivateHashedCredentials(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, _ := identityHandler(t, g)
	signed, cookie := durableSignIn(t, h, now, "floor-secret")
	if _, ok := h.sessions.ids[signed.AccessToken]; ok {
		t.Fatal("session map keeps the credential")
	}
	if _, ok := h.sessions.ids[sessionHash(signed.AccessToken)]; !ok {
		t.Fatal("missing credential hash")
	}
	for _, file := range []string{"sessions.json", "sessions.key"} {
		path := filepath.Join(filepath.Dir(h.config.TokensPath), file)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{signed.AccessToken, "github-access-secret", "github-refresh-secret"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("%s contains a credential", file)
			}
		}
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %v, %v", file, info, err)
		}
	}
	h = restartIdentity(t, h)
	for _, me := range []api.Me{readIdentity(t, h, signed.AccessToken, nil), readIdentity(t, h, "", cookie)} {
		if me.Handle != "priya" || me.SessionID != signed.Me.SessionID {
			t.Fatalf("restarted me = %+v", me)
		}
	}
	if q := queueAs(t, h, signed.AccessToken, false); len(q.Jobs) == 0 || q.Reason != "" {
		t.Fatalf("restarted queue = %+v", q)
	}
	w := identityRequest(h, "GET", "/v1/me/sessions", "", signed.AccessToken, nil)
	var sessions api.Sessions
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &sessions) != nil || len(sessions.Sessions) != 1 || !sessions.Sessions[0].Current {
		t.Fatalf("restarted sessions = %d %s", w.Code, w.Body)
	}
	*now = now.Add(accessSessionTTL)
	h = restartIdentity(t, h)
	if len(h.sessions.ids) != 0 {
		t.Fatal("startup retained expired sessions")
	}
	if w := identityRequest(h, "GET", "/v1/me", "", signed.AccessToken, nil); w.Code != 401 {
		t.Fatalf("expired session = %d", w.Code)
	}
}

func TestGitHubRefreshRotatesOnceAndSurvivesRestart(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya", exchange: `{"access_token":"github-access-secret","refresh_token":"github-refresh-secret","expires_in":28800,"refresh_token_expires_in":15897600,"scope":"repo read:user"}`,
		refresh: `{"access_token":"github-new-secret","refresh_token":"github-new-refresh","expires_in":28800,"refresh_token_expires_in":15897600}`, refreshedToken: "github-new-secret"}
	h, now, _ := identityHandler(t, g)
	signed, _ := durableSignIn(t, h, now, "floor-secret")
	readIdentity(t, h, signed.AccessToken, nil)
	if g.refreshes != 0 {
		t.Fatal("refreshed before deadline")
	}
	*now = now.Add(8*time.Hour - 4*time.Minute)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() { defer wg.Done(); readIdentity(t, h, signed.AccessToken, nil) }()
	}
	wg.Wait()
	if g.refreshes != 1 {
		t.Fatalf("refreshes = %d", g.refreshes)
	}
	h = restartIdentity(t, h)
	session := h.sessions.ids[sessionHash(signed.AccessToken)]
	if session.githubToken != "github-new-secret" || session.githubRefresh != "github-new-refresh" || session.githubExpires != now.Add(8*time.Hour) {
		t.Fatalf("rotation was not persisted: expiry %s", session.githubExpires)
	}
	if q := queueAs(t, h, signed.AccessToken, false); len(q.Jobs) == 0 || q.Reason != "" {
		t.Fatal("refreshed token cannot read repositories")
	}
	if g.refreshes != 1 {
		t.Fatalf("restart repeated refresh: %d", g.refreshes)
	}
}

func TestGitHubRefreshFailureEndsSessionWithClear401(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"bad refresh token", `{"error":"bad_refresh_token"}`, 200},
		{"bad refresh token HTTP 400", `{"error":"bad_refresh_token"}`, 400},
		{"unauthorized", `{}`, 401},
		{"expired refresh", `{"error":"expired_token"}`, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGitHub{t: t, login: "priya", exchange: `{"access_token":"github-access-secret","refresh_token":"github-refresh-secret","expires_in":28800}`, refresh: tc.body, refreshStatus: tc.status}
			h, now, _ := identityHandler(t, g)
			signed, cookie := durableSignIn(t, h, now, "floor-secret")
			*now = now.Add(8 * time.Hour)
			assertGitHubExpired(t, identityRequest(h, "GET", "/v1/queue", "", signed.AccessToken, nil))
			assertGitHubExpired(t, identityRequest(h, "GET", "/v1/me", "", "", cookie))
			h = restartIdentity(t, h)
			assertGitHubExpired(t, identityRequest(h, "GET", "/v1/me", "", signed.AccessToken, nil))
			if g.refreshes != 1 || h.viewer.token("priya") != "" {
				t.Fatal("ended sign-in retained or retried its GitHub credential")
			}
		})
	}
}

func TestGitHubRefreshOutageRetainsSessionForRetry(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya", exchange: `{"access_token":"github-access-secret","refresh_token":"github-refresh-secret","expires_in":28800}`, refreshStatus: 503}
	h, now, _ := identityHandler(t, g)
	signed, _ := durableSignIn(t, h, now, "floor-secret")
	*now = now.Add(8 * time.Hour)
	if w := identityRequest(h, "GET", "/v1/queue", "", signed.AccessToken, nil); w.Code != 502 {
		t.Fatalf("outage = %d %s", w.Code, w.Body)
	}
	if h.sessions.ids[sessionHash(signed.AccessToken)].githubExpired {
		t.Fatal("outage ended session")
	}
	g.refreshStatus = 200
	g.refresh = `{"access_token":"github-new-secret","refresh_token":"github-new-refresh","expires_in":28800}`
	g.refreshedToken = "github-new-secret"
	if q := queueAs(t, h, signed.AccessToken, false); len(q.Jobs) == 0 {
		t.Fatal("retry did not restore queue")
	}
}

func TestGitHubUnauthorizedRepositoryEndsNonExpiringSession(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya", repoStatus: 401}
	h, now, _ := identityHandler(t, g)
	signed, _ := durableSignIn(t, h, now, "floor-secret")
	assertGitHubExpired(t, identityRequest(h, "GET", "/v1/queue", "", signed.AccessToken, nil))
	h = restartIdentity(t, h)
	assertGitHubExpired(t, identityRequest(h, "GET", "/v1/me", "", signed.AccessToken, nil))
	if g.refreshes != 0 {
		t.Fatal("non-expiring token was refreshed")
	}
}

func TestQueueListsVisibleWatchedRepositoriesWithoutJobs(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, _ := identityHandler(t, g)
	h.seam.Load = func() (factory.Snapshot, error) { return factory.Snapshot{Now: *now}, nil }
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"acme/new"}); err != nil {
		t.Fatal(err)
	}
	h.config.Store = st
	signed, _ := durableSignIn(t, h, now, "floor-secret")
	for _, token := range []string{"floor-secret", signed.AccessToken} {
		q := queueAs(t, h, token, false)
		if len(q.Jobs) != 0 || len(q.Repos) != 1 || q.Repos[0].Name != "acme/new" || q.Reason != "" {
			t.Fatalf("watched empty repo = %+v", q)
		}
	}
	g.repoStatus = 404
	*now = now.Add(repoVisibilityTTL)
	if q := queueAs(t, h, signed.AccessToken, false); len(q.Repos) != 0 || q.Reason != api.ReasonNoVisibleRepos {
		t.Fatalf("hidden watched repo = %+v", q)
	}
}

func assertGitHubExpired(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var failure api.AuthFailure
	if w.Code != 401 || json.Unmarshal(w.Body.Bytes(), &failure) != nil || failure.Reason != api.ReasonGitHubExpired || failure.RequestID == "" {
		t.Fatalf("expired GitHub = %d %s", w.Code, w.Body)
	}
	failure.RequestID = "request_fixture"
	raw, err := json.Marshal(failure)
	if err != nil {
		t.Fatal(err)
	}
	compareJSONFixture(t, raw, "testdata/github_expired.json")
}

func TestSessionSignOutPersistsAcrossRestart(t *testing.T) {
	for _, action := range []string{"one", "others", "all"} {
		t.Run(action, func(t *testing.T) {
			h, now, _ := identityHandler(t, &fakeGitHub{t: t, login: "priya"})
			first, _ := durableSignIn(t, h, now, "floor-secret")
			second, _ := durableSignIn(t, h, now, "floor-secret")
			method, path, token := "DELETE", "/v1/me/sessions/"+first.Me.SessionID, second.AccessToken
			if action == "others" {
				method, path = "POST", "/v1/me/sessions/signout-others"
			}
			if action == "all" {
				method, path, token = "POST", "/v1/access/revoke-sessions", "floor-secret"
			}
			if w := identityRequest(h, method, path, "", token, nil); w.Code != 200 {
				t.Fatalf("sign-out = %d %s", w.Code, w.Body)
			}
			h = restartIdentity(t, h)
			if w := identityRequest(h, "GET", "/v1/me", "", first.AccessToken, nil); w.Code != 401 {
				t.Fatalf("signed-out session revived: %d", w.Code)
			}
			if action == "all" {
				if w := identityRequest(h, "GET", "/v1/me", "", second.AccessToken, nil); w.Code != 401 {
					t.Fatalf("revoked session revived: %d", w.Code)
				}
			} else if me := readIdentity(t, h, second.AccessToken, nil); me.Handle != "priya" {
				t.Fatal("sign-out removed the other session")
			}
		})
	}
}

func TestEncryptedSessionTamperingFailsClosed(t *testing.T) {
	h, now, _ := identityHandler(t, &fakeGitHub{t: t, login: "priya"})
	signed, _ := durableSignIn(t, h, now, "floor-secret")
	body, err := os.ReadFile(h.sessions.path)
	if err != nil {
		t.Fatal(err)
	}
	var doc sessionDocument
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatal(err)
	}
	key := sessionHash(signed.AccessToken)
	row := doc.Sessions[key]
	row.GitHub[len(row.GitHub)-1] ^= 1
	doc.Sessions[key] = row
	body, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.sessions.path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	h = restartIdentity(t, h)
	if w := identityRequest(h, "GET", "/v1/me", "", signed.AccessToken, nil); w.Code != 502 {
		t.Fatalf("tampered session = %d %s", w.Code, w.Body)
	}
	if w := identityRequest(h, "GET", "/v1/me", "", "floor-secret", nil); w.Code != 200 {
		t.Fatalf("host access = %d", w.Code)
	}
}

func TestOriginalInviteUsesRefreshAndCanStartSignInAfterGitHubExpiry(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya", exchange: `{"access_token":"github-access-secret","refresh_token":"github-refresh-secret","expires_in":1800}`, refresh: `{"error":"bad_refresh_token"}`}
	h, now, row := identityHandler(t, g)
	durableSignIn(t, h, now, row.Token)
	*now = now.Add(26 * time.Minute)
	assertGitHubExpired(t, identityRequest(h, "GET", "/v1/queue", "", row.Token, nil))
	h = restartIdentity(t, h)
	assertGitHubExpired(t, identityRequest(h, "GET", "/v1/me", "", row.Token, nil))
	g.exchange = `{"access_token":"github-access-secret","scope":"repo read:user"}`
	signed, _ := durableSignIn(t, h, now, row.Token)
	if me := readIdentity(t, h, signed.AccessToken, nil); me.Handle != "priya" {
		t.Fatal("new sign-in failed")
	}
	if q := queueAs(t, h, row.Token, false); q.Reason != "" || len(q.Jobs) == 0 {
		t.Fatal("invite did not recover after sign-in")
	}
}
