package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/factory/tokens"
)

type githubTransport func(*http.Request) (*http.Response, error)

func (f githubTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// fakeGitHub checks the real request shape at fixed GitHub URLs. A still clock
// lets tests exercise polling rules and expiry without waiting on the laptop.
type fakeGitHub struct {
	t          *testing.T
	login      string
	exchange   string
	userStatus int
	polls      int
	users      int
}

func (g *fakeGitHub) client() *http.Client {
	return &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		g.t.Helper()
		body, status := "", http.StatusOK
		if r.Header.Get("Accept") != "application/json" {
			g.t.Fatal("GitHub request needs JSON")
		}
		switch r.URL.String() {
		case "https://github.com/login/device/code":
			if err := r.ParseForm(); err != nil {
				g.t.Fatal(err)
			}
			if r.Method != "POST" || r.Form.Get("client_id") != "client" || r.Form.Get("scope") != "read:user repo" {
				g.t.Fatalf("start = %+v", r.Form)
			}
			body = `{"device_code":"github-secret","user_code":"ABCD-EFGH","verification_uri":"https://github.com/login/device","interval":5,"expires_in":900}`
		case "https://github.com/login/oauth/access_token":
			g.polls++
			if err := r.ParseForm(); err != nil {
				g.t.Fatal(err)
			}
			if r.Method != "POST" || r.Form.Get("client_id") != "client" || r.Form.Get("device_code") != "github-secret" || r.Form.Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" {
				g.t.Fatalf("poll = %+v", r.Form)
			}
			body = g.exchange
			if body == "" {
				body = `{"access_token":"github-access-secret","scope":"read:user,read:org","token_type":"bearer"}`
			}
		case "https://api.github.com/user":
			g.users++
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer github-access-secret" {
				g.t.Fatal("user must be checked with exchanged GitHub token")
			}
			body = fmt.Sprintf(`{"login":%q,"name":"Priya Nair","avatar_url":"https://avatars.githubusercontent.com/u/42"}`, g.login)
			if g.userStatus != 0 {
				status = g.userStatus
			}
		default:
			// Repository visibility checks use the exchanged token. Existing
			// sign-in tests treat every repository as readable.
			if r.Method == http.MethodGet && strings.HasPrefix(r.URL.String(), "https://api.github.com/repos/") && r.Header.Get("Authorization") == "Bearer github-access-secret" {
				body = `{}`
				break
			}
			g.t.Fatalf("unexpected GitHub URL %s", r.URL)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
}

func identityRequest(h *Handler, method, path, body, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "192.0.2.1:1234"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func identityHandler(t *testing.T, g *fakeGitHub) (*Handler, *time.Time, tokens.Token) {
	t.Helper()
	now := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "tokens.json")
	row, err := tokens.Mint()
	if err != nil {
		t.Fatal(err)
	}
	row.InvitedBy, row.Expires = "sam", now.Add(time.Hour)
	if err := tokens.Save(path, []tokens.Token{row}); err != nil {
		t.Fatal(err)
	}
	seam := factory.FixtureSeam(now)
	seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "factory-owner"}, nil
	}
	seam.Launch = func(int) error { return nil }
	seam.Dismiss = func(int) error { return nil }
	seam.Stop = func(int) error { return nil }
	seam.SignOff = func(int, bool) (bool, error) { return false, nil }
	seam.Answer = func(int, bool, string) error { return nil }
	seam.SendBack = func(int, string) error { return nil }
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	h := New(seam, Config{Address: "http://factory", Version: "test", AccessToken: "floor-secret", TokensPath: path, GitHubClientID: "client", GitHubHTTPClient: g.client(), Record: rec})
	h.sessions.now = func() time.Time { return now }
	t.Cleanup(h.Close)
	return h, &now, row
}

func startIdentity(t *testing.T, h *Handler, token string, cookie *http.Cookie) api.Device {
	t.Helper()
	w := identityRequest(h, "POST", "/v1/auth/device", "", token, cookie)
	var device api.Device
	if err := json.Unmarshal(w.Body.Bytes(), &device); err != nil || w.Code != 200 || device.DeviceCode == "" || device.DeviceCode == "github-secret" || device.UserCode != "ABCD-EFGH" || device.Interval != 5 || device.ExpiresIn != 900 {
		t.Fatalf("device = %d %s (%v)", w.Code, w.Body, err)
	}
	return device
}

func pollIdentity(h *Handler, device api.Device, token string, cookie *http.Cookie) *httptest.ResponseRecorder {
	body, _ := json.Marshal(api.DevicePoll{DeviceCode: device.DeviceCode})
	return identityRequest(h, "POST", "/v1/auth/device/poll", string(body), token, cookie)
}

func readIdentity(t *testing.T, h *Handler, token string, cookie *http.Cookie) api.Me {
	t.Helper()
	w := identityRequest(h, "GET", "/v1/me", "", token, cookie)
	var me api.Me
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &me) != nil {
		t.Fatalf("me = %d %s", w.Code, w.Body)
	}
	return me
}

func TestDeviceSignInBindsCallerCookieBearerInviteAndAudit(t *testing.T) {
	g := &fakeGitHub{t: t, login: "Priya"}
	h, now, row := identityHandler(t, g)
	before := readIdentity(t, h, row.Token, nil)
	if before.Handle != "invited:sam" || before.InvitedBy != "sam" {
		t.Fatalf("unsigned invite = %+v", before)
	}
	oldCookie := accessRequest(h, "POST", "/access", row.Token, nil, true).Result().Cookies()[0]
	device := startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, row.Token, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil {
		t.Fatalf("sign-in = %d %s", w.Code, w.Body)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || signed.AccessToken == "" || signed.AccessToken == "github-access-secret" || strings.Contains(w.Body.String(), "github-access-secret") || signed.AccessToken == signed.Me.SessionID {
		t.Fatalf("credentials = %s %+v", w.Body, cookies)
	}
	want := api.Profile{Handle: "priya", Name: "Priya Nair", AvatarURL: "https://avatars.githubusercontent.com/u/42", Scopes: []string{"read:user", "read:org"}}
	for _, me := range []api.Me{signed.Me, readIdentity(t, h, signed.AccessToken, nil), readIdentity(t, h, "", cookies[0]), readIdentity(t, h, row.Token, nil), readIdentity(t, h, "", oldCookie)} {
		if !reflect.DeepEqual(me.Profile, want) || me.InvitedBy != "sam" || me.Contract != 1 || me.Machines == nil || me.TakesUnassigned == nil || me.SessionID == "" {
			t.Fatalf("caller = %+v", me)
		}
	}
	for _, action := range []struct{ path, body string }{
		{"/v1/jobs/2/run", ""}, {"/v1/jobs/2/drop", ""},
		{"/v1/jobs/9/approve", ""}, {"/v1/jobs/1/answer", `{"yes":false,"words":"revise"}`},
		{"/v1/jobs/9/request_changes", `{"words":"show restart"}`},
	} {
		path := action.path
		response := identityRequest(h, "POST", path, action.body, signed.AccessToken, nil)
		if response.Code != 200 {
			t.Fatalf("%s = %d %s", path, response.Code, response.Body)
		}
	}
	events, err := h.config.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 5 {
		t.Fatalf("audit = %+v %v", events, err)
	}
	for _, event := range events {
		if event.Handle != "priya" {
			t.Fatalf("audit caller = %+v", event)
		}
	}
	q := identityRequest(h, "GET", "/v1/queue", "", signed.AccessToken, nil)
	if !strings.Contains(q.Body.String(), `"me":"priya"`) {
		t.Fatalf("queue = %s", q.Body)
	}
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 400 {
		t.Fatalf("replay = %d", w.Code)
	}
	restarted := New(h.seam, h.config)
	defer restarted.Close()
	restarted.sessions.now = func() time.Time { return *now }
	if me := readIdentity(t, restarted, row.Token, nil); me.Handle != "priya" {
		t.Fatalf("invite lost identity on restart = %+v", me)
	}
	if w := identityRequest(restarted, "GET", "/v1/me", "", signed.AccessToken, nil); w.Code != 401 {
		t.Fatalf("old session survived restart = %d", w.Code)
	}
	if err := tokens.Remove(h.config.TokensPath, row.ID); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{row.Token, signed.AccessToken} {
		if w := identityRequest(h, "GET", "/v1/me", "", token, nil); w.Code != 401 {
			t.Fatalf("revoked token = %d", w.Code)
		}
	}
	if w := identityRequest(h, "GET", "/v1/me", "", "", cookies[0]); w.Code != 401 {
		t.Fatalf("revoked cookie = %d", w.Code)
	}
}

func TestDevicePollingPendingSlowDownExpiryAndCallerBinding(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya", exchange: `{"error":"authorization_pending"}`}
	h, now, row := identityHandler(t, g)
	device := startIdentity(t, h, row.Token, nil)
	if w := pollIdentity(h, device, "floor-secret", nil); w.Code != 400 || g.polls != 0 {
		t.Fatalf("another caller polled = %d %d", w.Code, g.polls)
	}
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 202 || g.polls != 0 {
		t.Fatalf("early poll = %d %d", w.Code, g.polls)
	}
	*now = now.Add(5 * time.Second)
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 202 || g.polls != 1 {
		t.Fatalf("pending = %d %d", w.Code, g.polls)
	}
	g.exchange = `{"error":"slow_down"}`
	*now = now.Add(5 * time.Second)
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 202 || !strings.Contains(w.Body.String(), `"interval":10`) {
		t.Fatalf("slow_down = %d %s", w.Code, w.Body)
	}
	*now = now.Add(5 * time.Second)
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 202 || g.polls != 2 {
		t.Fatalf("slow interval ignored = %d %d", w.Code, g.polls)
	}
	*now = now.Add(900 * time.Second)
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 410 || g.polls != 2 {
		t.Fatalf("expired = %d %d", w.Code, g.polls)
	}
}

func TestDeviceSignInRefusesUnacceptedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, exchange, login string
		userStatus, status    int
		team                  []string
	}{
		{name: "denied", exchange: `{"error":"access_denied"}`, login: "priya", status: 403},
		{name: "expired", exchange: `{"error":"expired_token"}`, login: "priya", status: 410},
		{name: "invalid token", login: "priya", userStatus: 401, status: 502},
		{name: "empty login", status: 502},
		{name: "not on team", login: "stranger", team: []string{"@priya"}, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := &fakeGitHub{t: t, login: tc.login, exchange: tc.exchange, userStatus: tc.userStatus}
			h, now, row := identityHandler(t, g)
			h.config.Team = tc.team
			device := startIdentity(t, h, row.Token, nil)
			*now = now.Add(5 * time.Second)
			w := pollIdentity(h, device, row.Token, nil)
			rows, err := tokens.Load(h.config.TokensPath)
			if w.Code != tc.status || len(w.Result().Cookies()) != 0 || len(h.sessions.ids) != 0 || err != nil || rows[0].Profile != nil {
				t.Fatalf("refusal = %d %s rows %+v error %v", w.Code, w.Body, rows, err)
			}
		})
	}
}

func TestDeviceCookieSignInOwnerLoopbackAndAllowlist(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, _ := identityHandler(t, g)
	h.config.Team = []string{"@PRIYA"}
	cookie := accessRequest(h, "POST", "/access", "floor-secret", nil, false).Result().Cookies()[0]
	device := startIdentity(t, h, "", cookie)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, "", cookie)
	if w.Code != 200 {
		t.Fatalf("cookie sign-in = %d %s", w.Code, w.Body)
	}
	cookie = w.Result().Cookies()[0]
	if me := readIdentity(t, h, "", cookie); me.Handle != "priya" || me.InvitedBy != "" {
		t.Fatalf("cookie caller = %+v", me)
	}
	if me := readIdentity(t, h, "floor-secret", nil); me.Handle != "factory-owner" {
		t.Fatalf("host = %+v", me)
	}
	r := httptest.NewRequest("GET", "/v1/me", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	local := httptest.NewRecorder()
	h.ServeHTTP(local, r)
	if !strings.Contains(local.Body.String(), `"handle":"factory-owner"`) {
		t.Fatal(local.Body)
	}
	h.config.Team = []string{"someone-else"}
	if w := identityRequest(h, "GET", "/v1/me", "", "", cookie); w.Code != 403 {
		t.Fatalf("removed team member = %d", w.Code)
	}
}

func TestDeviceSignInNeedsConfiguredClientAndExistingAccess(t *testing.T) {
	h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "floor-secret"})
	defer h.Close()
	if w := identityRequest(h, "POST", "/v1/auth/device", "", "", nil); w.Code != 401 {
		t.Fatalf("unguarded start = %d", w.Code)
	}
	if w := identityRequest(h, "POST", "/v1/auth/device", "", "floor-secret", nil); w.Code != 501 {
		t.Fatalf("unconfigured start = %d", w.Code)
	}
}

// The signed-in fixture pins every old field as well as the additive profile.
// The credential is never the public session id, so normalize only that opaque id.
func TestDeviceIdentityContractMatchesFixture(t *testing.T) {
	g := &fakeGitHub{t: t, login: "Priya"}
	h, now, row := identityHandler(t, g)
	device := startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, row.Token, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil {
		t.Fatalf("sign-in = %d %s", w.Code, w.Body)
	}
	if !strings.HasPrefix(signed.Me.SessionID, "session_") {
		t.Fatalf("session id = %q", signed.Me.SessionID)
	}
	signed.Me.SessionID = "session_fixture"
	raw, err := os.ReadFile("testdata/me.json")
	if err != nil {
		t.Fatal(err)
	}
	var want, got map[string]any
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(signed.Me)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) || api.Contract != 1 {
		t.Fatalf("identity contract changed: got %+v want %+v", got, want)
	}
}

func TestSignedInCallerOwnsTakeAndStopReceipts(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, row := identityHandler(t, g)
	device := startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, row.Token, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil {
		t.Fatal(w.Body)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Title: "work", Repo: "owner/repo", State: factory.StateQueued})
	if err != nil {
		t.Fatal(err)
	}
	h.workers.st = st
	h.workers.desk, err = lease.Open(st, lease.Options{Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	h.seam.Load = func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err }
	h.seam.Stop = func(int) error { return nil }

	for _, verb := range []string{"take", "stop"} {
		w := identityRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", it.ID, verb), "", signed.AccessToken, nil)
		if w.Code != 200 {
			t.Fatalf("%s = %d %s", verb, w.Code, w.Body)
		}
	}
	took, err := h.config.Record.Since(context.Background(), time.Time{})
	if err != nil || len(took) != 1 || took[0].Handle != "priya" {
		t.Fatalf("take = %+v %v", took, err)
	}
	rec, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(stopped) != 1 || stopped[0].Handle != "priya" || stopped[0].Kind != record.Stopped {
		t.Fatalf("stop = %+v %v", stopped, err)
	}
}

func TestSignedInInviteCannotChangeHolderOrOutliveExpiry(t *testing.T) {
	g := &fakeGitHub{t: t, login: "priya"}
	h, now, row := identityHandler(t, g)
	device := startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, row.Token, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil {
		t.Fatal(w.Body)
	}
	g.login = "other"
	device = startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	if w := pollIdentity(h, device, row.Token, nil); w.Code != 403 {
		t.Fatalf("invite switched holder = %d %s", w.Code, w.Body)
	}
	if me := readIdentity(t, h, row.Token, nil); me.Handle != "priya" {
		t.Fatalf("holder changed = %+v", me)
	}
	*now = row.Expires
	if w := identityRequest(h, "GET", "/v1/me", "", signed.AccessToken, nil); w.Code != 401 {
		t.Fatalf("session outlived invite = %d", w.Code)
	}
}

func TestUnsignedInviteAuditsItsIdentityAndRequiresSignInToApprove(t *testing.T) {
	h, _, row := identityHandler(t, &fakeGitHub{t: t, login: "priya"})
	w := identityRequest(h, "POST", "/v1/jobs/2/run", "", row.Token, nil)
	if w.Code != 200 {
		t.Fatalf("unsigned run = %d %s", w.Code, w.Body)
	}
	events, err := h.config.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Handle != "invited:sam" {
		t.Fatalf("unsigned audit = %+v %v", events, err)
	}
	w = identityRequest(h, "POST", "/v1/jobs/9/approve", "", row.Token, nil)
	if w.Code != 403 || !strings.Contains(w.Body.String(), "GitHub sign-in is required to approve") {
		t.Fatalf("unsigned approval = %d %s", w.Code, w.Body)
	}
	events, err = h.config.Record.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 {
		t.Fatalf("refused approval was recorded: %+v %v", events, err)
	}
}

func TestSignedInWorkerCannotImpersonateAnotherHandle(t *testing.T) {
	h, now, row := identityHandler(t, &fakeGitHub{t: t, login: "priya"})
	device := startIdentity(t, h, row.Token, nil)
	*now = now.Add(5 * time.Second)
	w := pollIdentity(h, device, row.Token, nil)
	var signed api.SignedIn
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &signed) != nil {
		t.Fatal(w.Body)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h.workers.desk, err = lease.Open(st, lease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	w = identityRequest(h, "POST", "/v1/workers/attach", `{"name":"laptop","handle":"stranger"}`, signed.AccessToken, nil)
	if w.Code != 403 {
		t.Fatalf("impersonation = %d %s", w.Code, w.Body)
	}
	w = identityRequest(h, "POST", "/v1/workers/attach", `{"name":"laptop"}`, signed.AccessToken, nil)
	if w.Code != 201 {
		t.Fatalf("own worker = %d %s", w.Code, w.Body)
	}
	for _, registered := range h.workers.credentials {
		if registered.Worker.Handle != "priya" {
			t.Fatalf("worker = %+v", registered)
		}
	}
}

func TestLoopbackStopWithNoGitHubNeverBorrowsTheAssignee(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Title: "work", Repo: "owner/repo", State: factory.StateQueued, Assignee: "someone-else"})
	if err != nil {
		t.Fatal(err)
	}
	h := New(factory.Seam{
		Load: func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
		Stop: func(int) error { return nil },
	}, Config{Store: st})
	defer h.Close()
	r := httptest.NewRequest("POST", fmt.Sprintf("/v1/jobs/%d/stop", it.ID), nil)
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("stop = %d %s", w.Code, w.Body)
	}
	rec, err := record.Open(st, record.Options{})
	if err != nil {
		t.Fatal(err)
	}
	events, err := rec.Since(context.Background(), time.Time{})
	if err != nil || len(events) != 1 || events[0].Handle != "" {
		t.Fatalf("stop borrowed job identity = %+v %v", events, err)
	}
}
