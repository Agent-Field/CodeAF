package serve

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// Contract S7: An operator can turn on a repository that discovery finds even
// when their own GitHub credential cannot see it.
func TestReposToggleRefreshFindsNewTeamRepo(t *testing.T) {
	f, calls := newTeamReposFixture(t)
	w := identityRequest(f.h, http.MethodPut, "/v1/repos/acme/new", `{"on":true}`, "owner-session", nil)
	watched, err := f.st.Repos()
	if w.Code != http.StatusOK || calls.Load() != 1 || err != nil || !slices.Equal(watched, []string{"acme/new", "acme/on"}) {
		t.Fatalf("toggle: %d %s, walks=%d watched=%v err=%v", w.Code, w.Body, calls.Load(), watched, err)
	}
}

// Contract S7: Turning off follows the same discovery and visibility rules.
func TestReposToggleRefreshFindsRepoWhenTurningOff(t *testing.T) {
	f, calls := newTeamReposFixture(t)
	w := identityRequest(f.h, http.MethodPut, "/v1/repos/acme/new", `{"on":false}`, "owner-session", nil)
	watched, err := f.st.Repos()
	if w.Code != http.StatusOK || calls.Load() != 1 || err != nil || !slices.Equal(watched, []string{"acme/on"}) {
		t.Fatalf("toggle off: %d %s, walks=%d watched=%v err=%v", w.Code, w.Body, calls.Load(), watched, err)
	}
}

// Contract S7: A member cannot toggle, and an absent repository keeps the
// same 404 while repeat switches share the cooldown.
func TestReposToggleRefreshKeepsOperatorAndVisibilityRules(t *testing.T) {
	f, calls := newTeamReposFixture(t)
	w := identityRequest(f.h, http.MethodPut, "/v1/repos/acme/new", `{"on":true}`, "member-session", nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "only the operator can do this") || calls.Load() != 0 {
		t.Fatalf("member toggle: %d %s, walks=%d", w.Code, w.Body, calls.Load())
	}
	for range 3 {
		w = identityRequest(f.h, http.MethodPut, "/v1/repos/acme/absent", `{"on":true}`, "owner-session", nil)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"error":"repository not found"`) || calls.Load() != 1 {
			t.Fatalf("absent toggle: %d %s, walks=%d", w.Code, w.Body, calls.Load())
		}
	}
}

// Contract S7: An unfiltered solo owner or team host can already toggle a
// repository outside the saved list, without a discovery.
func TestReposToggleOutsideSavedListAsUnfilteredOperator(t *testing.T) {
	t.Run("solo owner", func(t *testing.T) {
		f := newReposRig(t)
		if w := f.read(); w.Code != http.StatusOK {
			t.Fatal(w.Body)
		}
		w := identityRequest(f.h, http.MethodPut, "/v1/repos/acme/new", `{"on":true}`, "read-secret", nil)
		if w.Code != http.StatusOK || f.calls.Load() != 1 {
			t.Fatalf("solo switch: %d %s, walks=%d", w.Code, w.Body, f.calls.Load())
		}
	})
	t.Run("team host", func(t *testing.T) {
		f, calls := newTeamReposFixture(t)
		w := identityRequest(f.h, http.MethodPut, "/v1/repos/acme/new", `{"on":true}`, "operator-secret", nil)
		if w.Code != http.StatusOK || calls.Load() != 0 {
			t.Fatalf("host switch: %d %s, walks=%d", w.Code, w.Body, calls.Load())
		}
	})
}

// Contract S8: The refreshed reply has the ordinary list shape, and
// discovery never exposes unwatched repositories to a team member.
func TestReposRefreshKeepsPerCallerListShape(t *testing.T) {
	f, calls := newTeamReposFixture(t)
	w := identityRequest(f.h, http.MethodGet, "/v1/repos?refresh=1", "", "member-session", nil)
	assertReposList(t, w, f.now, []string{"acme/on"}, nil)
	w = identityRequest(f.h, http.MethodGet, "/v1/repos", "", "owner-session", nil)
	assertReposList(t, w, f.now, []string{"acme/on"}, []string{"acme/old", "acme/new"})
	if calls.Load() != 1 {
		t.Fatalf("walks=%d, want 1", calls.Load())
	}
}

func newTeamReposFixture(t *testing.T) (*peopleV2Fixture, *atomic.Int32) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	f := peopleV2Setup(t, api.FactoryTeam)
	const grant = "factory-grant"
	if err := factorygithub.SaveReadGrant(factorygithub.GrantPath(f.cfg.TokensPath), factorygithub.ReadGrant{Token: grant, Login: "owner", GrantedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(grant)))
	if err := f.st.SetAvailableRepos(store.AvailableRepos{
		Identity: "owner", Credential: key, ReadAt: f.now.Add(-time.Minute), Repos: []factory.RepoInfo{{Full: "acme/old"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetRepos([]string{"acme/on"}); err != nil {
		t.Fatal(err)
	}
	f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{Login: "owner", Via: factory.ViaToken}, nil
	}
	var calls atomic.Int32
	f.h.seam.Repos = func(context.Context) ([]string, []factory.RepoInfo, error) {
		calls.Add(1)
		return nil, []factory.RepoInfo{{Full: "acme/old"}, {Full: "acme/new"}}, nil
	}
	g := &fakeGitHub{t: t, repoStatus: http.StatusNotFound}
	f.h.config.GitHubHTTPClient = g.client()
	f.h.sessions.mu.Lock()
	f.h.sessions.ids[sessionHash("owner-session")] = accessSession{
		profile: &api.Profile{Handle: "owner"}, githubToken: "github-access-secret", expires: f.now.Add(time.Hour),
	}
	f.h.sessions.ids[sessionHash("member-session")] = accessSession{
		profile: &api.Profile{Handle: "member"}, expires: f.now.Add(time.Hour),
	}
	f.h.sessions.mu.Unlock()
	return f, &calls
}

func TestReposAnswersFromStoreWithoutGitHubRequest(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte("test-token")))
	if err := st.SetAvailableRepos(store.AvailableRepos{Identity: "alice", Credential: key, ReadAt: at, Repos: []factory.RepoInfo{{Full: "acme/api", Name: "api", Owner: "acme"}}}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	h := New(factory.Seam{
		GitHub: func(context.Context) (factory.GitHubLink, error) {
			return factory.GitHubLink{Login: "alice", Via: factory.ViaToken}, nil
		},
		Repos: func(context.Context) ([]string, []factory.RepoInfo, error) {
			calls.Add(1)
			return nil, nil, fmt.Errorf("unexpected GitHub request")
		},
	}, Config{Mode: "solo", AccessToken: "read-secret", Store: st, GitHubToken: func(context.Context) string { return "test-token" }})
	defer h.Close()
	w := identityRequest(h, "GET", "/v1/repos", "", "read-secret", nil)
	var got struct {
		Available []Repository `json:"available"`
		ReadAt    time.Time    `json:"available_read_at"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Available) != 1 || !got.ReadAt.Equal(at) || calls.Load() != 0 {
		t.Fatalf("read = %d %s, walks=%d", w.Code, w.Body, calls.Load())
	}
}

type reposRig struct {
	h            *Handler
	st           *store.Store
	now          atomic.Int64
	calls        atomic.Int32
	who          atomic.Value
	token        atomic.Value
	refused      atomic.Bool
	notConnected atomic.Bool
	walk         func(context.Context) ([]factory.RepoInfo, error)
}

func newReposRig(t *testing.T) *reposRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &reposRig{st: st}
	f.now.Store(time.Now().UnixNano())
	f.who.Store("alice")
	f.token.Store("token-" + t.Name())
	f.walk = func(context.Context) ([]factory.RepoInfo, error) {
		return []factory.RepoInfo{{Full: "acme/api", Name: "api", Owner: "acme"}}, nil
	}
	seam := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) {
		if f.refused.Load() {
			return factory.GitHubLink{}, githubStatusError(401)
		}
		if f.notConnected.Load() {
			return factory.GitHubLink{}, nil
		}
		return factory.GitHubLink{Login: f.who.Load().(string), Via: factory.ViaToken}, nil
	},
		Repos: func(ctx context.Context) ([]string, []factory.RepoInfo, error) {
			f.calls.Add(1)
			repos, err := f.walk(ctx)
			return nil, repos, err
		}, SetRepos: st.SetRepos}
	f.h = New(seam, Config{Mode: "solo", AccessToken: "read-secret", Store: st, Now: func() time.Time { return time.Unix(0, f.now.Load()) }, GitHubToken: func(context.Context) string { return f.token.Load().(string) }})
	t.Cleanup(func() { f.h.Close() })
	return f
}
func (f *reposRig) read() *httptest.ResponseRecorder {
	return identityRequest(f.h, "GET", "/v1/repos", "", "read-secret", nil)
}

func (f *reposRig) refresh(value string) *httptest.ResponseRecorder {
	return identityRequest(f.h, http.MethodGet, "/v1/repos?refresh="+value, "", "read-secret", nil)
}

func (f *reposRig) clock() time.Time { return time.Unix(0, f.now.Load()) }

func assertReposList(t *testing.T, w *httptest.ResponseRecorder, at time.Time, watched, available []string) {
	t.Helper()
	var got struct {
		Factory   api.Factory  `json:"factory"`
		Watched   []string     `json:"watched"`
		Available []Repository `json:"available"`
		ReadAt    time.Time    `json:"available_read_at"`
	}
	err := json.Unmarshal(w.Body.Bytes(), &got)
	names := make([]string, 0, len(got.Available))
	for _, repo := range got.Available {
		names = append(names, repo.Full)
	}
	if w.Code != http.StatusOK || err != nil || got.Factory.Name == "" || !got.ReadAt.Equal(at) ||
		got.Watched == nil || got.Available == nil || !slices.Equal(got.Watched, watched) || !slices.Equal(names, available) {
		t.Fatalf("read: %d %s, want timestamp %s, watched %v, available %v", w.Code, w.Body, at, watched, available)
	}
}

func (f *reposRig) saved(t *testing.T) store.AvailableRepos {
	t.Helper()
	out, err := f.st.AvailableRepos()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func awaitRepos(t *testing.T, h *Handler) {
	t.Helper()
	h.reposRead.mu.Lock()
	walk := h.reposRead.walk
	h.reposRead.mu.Unlock()
	if walk != nil {
		select {
		case <-walk.done:
		case <-time.After(5 * time.Second):
			t.Fatal("repository walk did not finish")
		}
	}
}

func TestReposFirstReadWalksAndWaits(t *testing.T) {
	f := newReposRig(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.walk = func(ctx context.Context) ([]factory.RepoInfo, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []factory.RepoInfo{{Full: "acme/api"}}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("GET", "/v1/repos", nil).WithContext(ctx)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer read-secret")
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { w := httptest.NewRecorder(); f.h.ServeHTTP(w, req); done <- w }()
	<-entered
	select {
	case w := <-done:
		t.Fatalf("first read returned before discovery: %d", w.Code)
	default:
	}
	close(release)
	w := <-done
	if w.Code != 200 || !strings.Contains(w.Body.String(), "acme/api") {
		t.Fatalf("completed first read: %d %s", w.Code, w.Body)
	}
}

// Contract S1: A fresh ordinary read stays on the saved list.
func TestReposFreshListStartsNoWalk(t *testing.T) {
	f := newReposRig(t)
	if w := f.read(); w.Code != 200 {
		t.Fatal(w.Body)
	}
	for range 5 {
		if w := f.read(); w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	if f.calls.Load() != 1 {
		t.Fatalf("walks=%d", f.calls.Load())
	}
}

// Contract S1: Ordinary reads return the saved list while one stale-list
// discovery runs in the background.
func TestReposStaleListStartsOneWalkWithConcurrentReads(t *testing.T) {
	f := newReposRig(t)
	if w := f.read(); w.Code != 200 {
		t.Fatal(w.Body)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	f.walk = func(ctx context.Context) ([]factory.RepoInfo, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []factory.RepoInfo{}, nil
	}
	old := f.saved(t).ReadAt
	f.now.Add(int64(5 * time.Minute))
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := f.read(); w.Code != 200 || !strings.Contains(w.Body.String(), "acme/api") {
				t.Errorf("%d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("stale-list discovery did not start")
	}
	if f.calls.Load() != 2 {
		t.Fatalf("walks=%d", f.calls.Load())
	}
	assertReposList(t, f.read(), old, nil, []string{"acme/api"})
}

// Contract S2: Either explicit value can replace a fresh saved list and its
// timestamp with a completed discovery.
func TestReposRefreshUpdatesFreshList(t *testing.T) {
	for _, value := range []string{"1", "true"} {
		t.Run(value, func(t *testing.T) {
			f := newReposRig(t)
			assertReposList(t, f.read(), f.clock(), nil, []string{"acme/api"})
			f.walk = func(context.Context) ([]factory.RepoInfo, error) {
				return []factory.RepoInfo{{Full: "acme/api"}, {Full: "acme/new"}}, nil
			}
			f.now.Add(int64(time.Minute))
			assertReposList(t, f.read(), f.clock().Add(-time.Minute), nil, []string{"acme/api"})
			assertReposList(t, f.refresh(value), f.clock(), nil, []string{"acme/api", "acme/new"})
			if f.calls.Load() != 2 {
				t.Fatalf("walks=%d, want 2", f.calls.Load())
			}
		})
	}
}

// Contract S3: The cooldown starts with either kind of discovery and elapses
// at 30 seconds on the handler clock.
func TestReposRefreshCooldown(t *testing.T) {
	f := newReposRig(t)
	start := f.clock()
	assertReposList(t, f.read(), start, nil, []string{"acme/api"})
	f.walk = func(context.Context) ([]factory.RepoInfo, error) {
		return []factory.RepoInfo{{Full: "acme/new"}}, nil
	}
	f.now.Add(int64(30*time.Second - time.Nanosecond))
	assertReposList(t, f.refresh("1"), start, nil, []string{"acme/api"})
	if f.calls.Load() != 1 {
		t.Fatalf("walks after timed read=%d, want 1", f.calls.Load())
	}
	f.now.Add(1)
	start = f.clock()
	assertReposList(t, f.refresh("1"), start, nil, []string{"acme/new"})
	f.walk = func(context.Context) ([]factory.RepoInfo, error) {
		return []factory.RepoInfo{{Full: "acme/later"}}, nil
	}
	for range 10 {
		f.now.Add(int64(time.Second))
		assertReposList(t, f.refresh("1"), start, nil, []string{"acme/new"})
	}
	if f.calls.Load() != 2 {
		t.Fatalf("walks after forced read=%d, want 2", f.calls.Load())
	}
	f.now.Add(int64(20 * time.Second))
	assertReposList(t, f.refresh("true"), f.clock(), nil, []string{"acme/later"})
	if f.calls.Load() != 3 {
		t.Fatalf("walks=%d, want 3", f.calls.Load())
	}
}

type reposWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *reposWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func startReposRefresh(h *Handler, ctx context.Context) (<-chan struct{}, <-chan *httptest.ResponseRecorder) {
	wait := &reposWaitContext{Context: ctx, waiting: make(chan struct{})}
	req := httptest.NewRequest(http.MethodGet, "/v1/repos?refresh=1", nil).WithContext(wait)
	req.Header.Set("Authorization", "Bearer read-secret")
	replied := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		replied <- w
	}()
	return wait.waiting, replied
}

// Contract S2 and S4: Forced requests join an active forced or timed
// discovery and each receives the completed list. A canceled waiter leaves
// the discovery available to the other callers.
func TestReposRefreshSharesActiveWalk(t *testing.T) {
	for _, mode := range []string{"forced", "timed"} {
		t.Run(mode, func(t *testing.T) {
			f := newReposRig(t)
			old := f.clock()
			assertReposList(t, f.read(), old, nil, []string{"acme/api"})
			f.now.Add(int64(5 * time.Minute))
			entered, release := make(chan struct{}, 1), make(chan struct{})
			var releaseWalk sync.Once
			finish := func() { releaseWalk.Do(func() { close(release) }) }
			t.Cleanup(finish)
			f.walk = func(ctx context.Context) ([]factory.RepoInfo, error) {
				select {
				case entered <- struct{}{}:
				default:
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return []factory.RepoInfo{{Full: "acme/new"}}, nil
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var replies []<-chan *httptest.ResponseRecorder
			if mode == "timed" {
				assertReposList(t, f.read(), old, nil, []string{"acme/api"})
			} else {
				_, reply := startReposRefresh(f.h, ctx)
				replies = append(replies, reply)
			}
			select {
			case <-entered:
			case <-ctx.Done():
				t.Fatal("discovery did not start")
			}
			for range 4 {
				waiting, reply := startReposRefresh(f.h, ctx)
				select {
				case <-waiting:
				case w := <-reply:
					t.Fatalf("refresh returned during discovery: %d %s", w.Code, w.Body)
				case <-ctx.Done():
					t.Fatal("refresh did not join discovery")
				}
				replies = append(replies, reply)
			}
			canceled, stop := context.WithCancel(ctx)
			waiting, reply := startReposRefresh(f.h, canceled)
			select {
			case <-waiting:
			case w := <-reply:
				stop()
				t.Fatalf("refresh returned before request cancellation: %d %s", w.Code, w.Body)
			case <-ctx.Done():
				stop()
				t.Fatal("refresh did not wait")
			}
			stop()
			select {
			case w := <-reply:
				if !strings.Contains(w.Body.String(), "context canceled") {
					t.Fatalf("canceled refresh: %d %s", w.Code, w.Body)
				}
			case <-ctx.Done():
				t.Fatal("canceled refresh did not return")
			}
			// The worker has been running past the cooldown when it publishes.
			f.now.Add(int64(31 * time.Second))
			finish()
			for _, reply := range replies {
				select {
				case w := <-reply:
					assertReposList(t, w, f.clock(), nil, []string{"acme/new"})
				case <-ctx.Done():
					t.Fatal("refresh did not return after discovery")
				}
			}
			if f.calls.Load() != 2 {
				t.Fatalf("walks=%d, want 2", f.calls.Load())
			}
			f.walk = func(context.Context) ([]factory.RepoInfo, error) {
				return []factory.RepoInfo{{Full: "acme/later"}}, nil
			}
			assertReposList(t, f.refresh("1"), f.clock(), nil, []string{"acme/later"})
			if f.calls.Load() != 3 {
				t.Fatalf("walks after a long discovery=%d, want 3", f.calls.Load())
			}
		})
	}
}

// Contract S5: Discovery failures use the ordinary read's saved-list and
// sign-in rules, and a first discovery failure reports its error.
func TestReposRefreshFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		saved bool
		keep  bool
	}{
		{"network", errors.New("connection reset"), true, true},
		{"refused", &forge.APIError{StatusCode: 401}, true, false},
		{"expired", errGitHubExpired, true, false},
		{"no saved list", errors.New("connection reset"), false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReposRig(t)
			old := f.clock()
			if tc.saved {
				assertReposList(t, f.read(), old, nil, []string{"acme/api"})
			}
			f.now.Add(int64(time.Minute))
			f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, tc.err }
			before := f.calls.Load()
			w := f.refresh("1")
			if f.calls.Load() != before+1 {
				t.Errorf("failure case started %d walks, want 1", f.calls.Load()-before)
			}
			if tc.keep {
				assertReposList(t, w, old, nil, []string{"acme/api"})
			} else if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "acme/api") {
				t.Fatalf("failed discovery: %d %s", w.Code, w.Body)
			}
			if ordinary := f.read(); ordinary.Code != w.Code || strings.Contains(ordinary.Body.String(), "acme/api") != tc.keep {
				t.Fatalf("ordinary read after failure: %d %s", ordinary.Code, ordinary.Body)
			}
		})
	}
}

// Contract S5: The identity and credential gate stays ahead of a forced walk
// and never falls back to a different connection's repositories.
func TestReposRefreshChecksIdentityBeforeDiscovery(t *testing.T) {
	for _, tc := range []struct {
		name, login, token string
		err                error
	}{
		{"identity", "bob", "", nil},
		{"credential", "alice", "different-token", nil},
		{"lost sign-in", "", "", errSessionEnded},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReposRig(t)
			assertReposList(t, f.read(), f.clock(), nil, []string{"acme/api"})
			f.now.Add(int64(time.Minute))
			if tc.token != "" {
				f.token.Store(tc.token)
			}
			f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
				return factory.GitHubLink{Login: tc.login, Via: factory.ViaToken}, tc.err
			}
			f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, errors.New("connection reset") }
			w := f.refresh("1")
			wantCalls := int32(2)
			if tc.err != nil {
				wantCalls = 1
			}
			if w.Code != http.StatusInternalServerError || strings.Contains(w.Body.String(), "acme/api") || f.calls.Load() != wantCalls {
				t.Fatalf("unsafe fallback: %d %s, walks=%d want %d", w.Code, w.Body, f.calls.Load(), wantCalls)
			}
		})
	}
}

// Contract S3: the cooldown protects GitHub from one connection's picker opening
// again and again. A DIFFERENT connection has no list of its own, so a forced
// read inside the cooldown still reads for it and never answers empty or with
// the previous account's list.
func TestReposRefreshForAChangedConnectionIgnoresTheCooldown(t *testing.T) {
	f := newReposRig(t)
	f.read()
	f.now.Add(int64(10 * time.Second))
	f.who.Store("bob")
	f.token.Store("different-token")
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { return []factory.RepoInfo{{Full: "bob/other"}}, nil }
	w := f.refresh("1")
	if w.Code != 200 || strings.Contains(w.Body.String(), "acme/api") || !strings.Contains(w.Body.String(), "bob/other") || f.calls.Load() != 2 {
		t.Fatalf("changed connection: %d %s, walks=%d want 2", w.Code, w.Body, f.calls.Load())
	}
}

// Contract S6: A closed handler serves a saved list without starting a walk.
func TestReposRefreshDoesNotStartAfterClose(t *testing.T) {
	f := newReposRig(t)
	old := f.clock()
	assertReposList(t, f.read(), old, nil, []string{"acme/api"})
	f.h.Close()
	f.now.Add(int64(6 * time.Minute))
	assertReposList(t, f.refresh("1"), old, nil, []string{"acme/api"})
	if f.calls.Load() != 1 {
		t.Fatalf("walks=%d, want 1", f.calls.Load())
	}
}

func TestReposSignInLostKeepsFailing(t *testing.T) {
	f := newReposRig(t)
	f.read()
	f.refused.Store(true)
	for range 2 {
		w := f.read()
		if w.Code != 500 || strings.Contains(w.Body.String(), "acme/api") {
			t.Fatalf("lost sign-in: %d %s", w.Code, w.Body)
		}
	}
}

func TestReposAnotherIdentityNeverGetsStoredList(t *testing.T) {
	f := newReposRig(t)
	f.read()
	f.who.Store("bob")
	f.token.Store("different-token")
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { return []factory.RepoInfo{{Full: "bob/other"}}, nil }
	w := f.read()
	if w.Code != 200 || strings.Contains(w.Body.String(), "acme/api") || !strings.Contains(w.Body.String(), "bob/other") {
		t.Fatalf("identity change: %d %s", w.Code, w.Body)
	}
}

func TestReposWatchedIsLive(t *testing.T) {
	f := newReposRig(t)
	f.read()
	for _, step := range []struct {
		path, body string
		watched    []string
	}{
		{"/v1/repos", `{"watched":["acme/api"]}`, []string{"acme/api"}},
		{"/v1/repos/acme/new", `{"on":true}`, []string{"acme/api", "acme/new"}},
		{"/v1/repos/acme/api", `{"on":false}`, []string{"acme/new"}},
		{"/v1/repos/acme/new", `{"on":false}`, []string{}},
	} {
		changed := identityRequest(f.h, "PUT", step.path, step.body, "read-secret", nil)
		if changed.Code != 200 {
			t.Fatalf("switch: %d %s", changed.Code, changed.Body)
		}
		w := f.read()
		var got struct {
			Watched []string `json:"watched"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || !slices.Equal(got.Watched, step.watched) {
			t.Fatalf("watched: %d %s", w.Code, w.Body)
		}
	}
	if f.calls.Load() != 1 {
		t.Fatalf("walks=%d", f.calls.Load())
	}
}

func TestReposRestartKeepsList(t *testing.T) {
	f := newReposRig(t)
	f.read()
	f.h.Close()
	st, err := store.Open(f.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	f.h = New(f.h.seam, Config{Mode: "solo", AccessToken: "read-secret", Store: st, Now: func() time.Time { return time.Unix(0, f.now.Load()) }, GitHubToken: func(context.Context) string { return f.token.Load().(string) }})
	w := f.read()
	if w.Code != 200 || !strings.Contains(w.Body.String(), "acme/api") || f.calls.Load() != 1 {
		t.Fatalf("restart: %d %s walks=%d", w.Code, w.Body, f.calls.Load())
	}
}

func TestReposFailedBackgroundWalkWithRefusedSignInKeepsFailing(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{"refused", githubStatusError(401)},
		{"wrapped GitHub refusal", fmt.Errorf("discovery: %w", &forge.APIError{StatusCode: 401})},
		{"expired", fmt.Errorf("discovery: %w", errGitHubExpired)},
	} {
		t.Run(failure.name, func(t *testing.T) {
			f := newReposRig(t)
			if w := f.read(); w.Code != 200 {
				t.Fatal(w.Body)
			}
			f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, failure.err }
			f.now.Add(int64(6 * time.Minute))
			f.read()
			awaitRepos(t, f.h)
			for range 3 {
				w := f.read()
				if w.Code != 500 || strings.Contains(w.Body.String(), "acme/api") {
					t.Fatalf("refused refresh: %d %s", w.Code, w.Body)
				}
			}
			if got := f.saved(t); len(got.Repos) != 1 || f.calls.Load() != 2 {
				t.Fatalf("lost list or repeated walk: %+v calls=%d", got, f.calls.Load())
			}
		})
	}
}

func TestReposFailedBackgroundWalkWithTransientFailureServesStoredList(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{"network", errors.New("connection reset")},
		{"server", &forge.APIError{StatusCode: 503}},
		{"rate limit", &forge.APIError{StatusCode: 429}},
		{"secondary rate limit", &forge.APIError{StatusCode: 403}},
		{"timeout", context.DeadlineExceeded},
	} {
		t.Run(failure.name, func(t *testing.T) {
			f := newReposRig(t)
			if w := f.read(); w.Code != 200 {
				t.Fatal(w.Body)
			}
			old := f.saved(t)
			f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, failure.err }
			f.now.Add(int64(6 * time.Minute))
			f.read()
			awaitRepos(t, f.h)
			for range 3 {
				w := f.read()
				var got struct {
					Available []Repository `json:"available"`
					ReadAt    time.Time    `json:"available_read_at"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Available) != 1 || !got.ReadAt.Equal(old.ReadAt) {
					t.Fatalf("stored read: %d %s", w.Code, w.Body)
				}
			}
			if f.calls.Load() != 2 {
				t.Fatalf("walks=%d, want 2", f.calls.Load())
			}
			f.now.Add(int64(30*time.Second - time.Nanosecond))
			f.read()
			awaitRepos(t, f.h)
			if f.calls.Load() != 2 {
				t.Fatalf("walk before retry: %d", f.calls.Load())
			}
			f.now.Add(1)
			if w := f.read(); w.Code != 200 {
				t.Fatalf("retry read: %d %s", w.Code, w.Body)
			}
			awaitRepos(t, f.h)
			if f.calls.Load() != 3 || !f.saved(t).ReadAt.Equal(old.ReadAt) {
				t.Fatalf("failed retry: walks=%d saved=%+v", f.calls.Load(), f.saved(t))
			}
		})
	}
}

func TestReposFirstFailedWalkRetriesAfterShortWait(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{"refused", githubStatusError(401)},
		{"network", errors.New("connection reset")},
	} {
		t.Run(failure.name, func(t *testing.T) {
			f := newReposRig(t)
			f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, failure.err }
			if w := f.read(); w.Code != 500 {
				t.Fatalf("first failure: %d %s", w.Code, w.Body)
			}
			f.now.Add(int64(30*time.Second - time.Nanosecond))
			if w := f.read(); w.Code != 500 || f.calls.Load() != 1 {
				t.Fatalf("before retry: %d %s walks=%d", w.Code, w.Body, f.calls.Load())
			}
			f.walk = func(context.Context) ([]factory.RepoInfo, error) { return []factory.RepoInfo{{Full: "acme/api"}}, nil }
			f.now.Add(1)
			if w := f.read(); w.Code != 200 || f.calls.Load() != 2 {
				t.Fatalf("retry: %d %s walks=%d", w.Code, w.Body, f.calls.Load())
			}
			f.now.Add(int64(5*time.Minute - time.Nanosecond))
			if w := f.read(); w.Code != 200 || f.calls.Load() != 2 {
				t.Fatalf("before successful refresh: %d %s walks=%d", w.Code, w.Body, f.calls.Load())
			}
			f.now.Add(1)
			f.read()
			awaitRepos(t, f.h)
			if f.calls.Load() != 3 {
				t.Fatalf("successful refresh walks=%d, want 3", f.calls.Load())
			}
		})
	}
}

func TestReposRemovedOnGitHubLeavesListAtNextWalk(t *testing.T) {
	f := newReposRig(t)
	f.read()
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { return []factory.RepoInfo{}, nil }
	f.now.Add(int64(6 * time.Minute))
	f.read()
	awaitRepos(t, f.h)
	w := f.read()
	if w.Code != 200 || strings.Contains(w.Body.String(), "acme/api") {
		t.Fatalf("removed repo: %d %s", w.Code, w.Body)
	}
}

func TestReposDiscoveryPanicFailsReadAndAllowsClose(t *testing.T) {
	f := newReposRig(t)
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { panic("scripted discovery fault") }
	done := make(chan int, 1)
	go func() { done <- f.read().Code }()
	select {
	case code := <-done:
		if code != 500 {
			t.Fatalf("status=%d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("panicked discovery blocked the read")
	}
}

func TestReposConcurrentFirstReadsShareOneWalk(t *testing.T) {
	f := newReposRig(t)
	entered, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	f.walk = func(ctx context.Context) ([]factory.RepoInfo, error) {
		close(entered)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return []factory.RepoInfo{{Full: "acme/api"}}, nil
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := f.read()
			if w.Code != 200 || !strings.Contains(w.Body.String(), "acme/api") {
				t.Errorf("%d %s", w.Code, w.Body)
			}
		}()
	}
	<-entered
	close(release)
	wg.Wait()
	if f.calls.Load() != 1 {
		t.Fatalf("walks=%d", f.calls.Load())
	}
}

func TestReposIdentityFailureUsesOnlySameCredentialStoredList(t *testing.T) {
	for _, tc := range []struct {
		name               string
		err                error
		token, login       string
		stored, wantStored bool
		notConnected       bool // no token: an answer without the stored list, not a failure
	}{
		{name: "network", err: errors.New("connection reset"), stored: true, wantStored: true},
		{name: "server", err: &forge.APIError{StatusCode: 503}, stored: true, wantStored: true},
		{name: "wrapped server", err: fmt.Errorf("identity: %w", githubStatusError(502)), stored: true, wantStored: true},
		{name: "refused", err: githubStatusError(401), stored: true},
		{name: "revoked", err: fmt.Errorf("identity: %w", &forge.APIError{StatusCode: 401}), stored: true},
		{name: "forbidden", err: &forge.APIError{StatusCode: 403}, stored: true},
		{name: "expired", err: fmt.Errorf("identity: %w", errGitHubExpired), stored: true},
		{name: "lost", stored: true, token: "", notConnected: true},
		{name: "changed token", err: errors.New("connection reset"), token: "other-token", stored: true},
		{name: "changed identity", err: errors.New("connection reset"), login: "bob", stored: true},
		{name: "nothing stored", err: errors.New("connection reset")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newReposRig(t)
			if tc.stored {
				if w := f.read(); w.Code != 200 {
					t.Fatal(w.Body)
				}
			}
			old := f.saved(t)
			if tc.token != "" || tc.name == "lost" {
				f.token.Store(tc.token)
			}
			f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: tc.login}, tc.err }
			w := f.read()
			var got struct {
				Available []Repository `json:"available"`
				ReadAt    time.Time    `json:"available_read_at"`
			}
			if tc.wantStored {
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || len(got.Available) != 1 || got.Available[0].Full != "acme/api" || !got.ReadAt.Equal(old.ReadAt) {
					t.Fatalf("stored read: %d %s", w.Code, w.Body)
				}
			} else if tc.notConnected {
				if w.Code != 200 || strings.Contains(w.Body.String(), "acme/api") {
					t.Fatalf("not connected: %d %s", w.Code, w.Body)
				}
			} else if w.Code != 500 || strings.Contains(w.Body.String(), "acme/api") {
				t.Fatalf("unsafe fallback: %d %s", w.Code, w.Body)
			}
		})
	}
}

func TestReposTransientIdentityFailureCannotOverrideRefusedDiscovery(t *testing.T) {
	f := newReposRig(t)
	if w := f.read(); w.Code != 200 {
		t.Fatal(w.Body)
	}
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, githubStatusError(401) }
	f.now.Add(int64(6 * time.Minute))
	f.read()
	awaitRepos(t, f.h)
	f.h.seam.GitHub = func(context.Context) (factory.GitHubLink, error) {
		return factory.GitHubLink{}, errors.New("connection reset")
	}
	w := f.read()
	if w.Code != 500 || strings.Contains(w.Body.String(), "acme/api") {
		t.Fatalf("refused sign-in: %d %s", w.Code, w.Body)
	}
}

// No token at all is not a lost sign-in: the page shows the repos that are on and
// offers Connect GitHub. No GitHub call, and nothing of an earlier account's list.
func TestReposNotConnectedAnswersWatchedOnly(t *testing.T) {
	f := newReposRig(t)
	if w := f.read(); w.Code != 200 {
		t.Fatalf("connected: %d %s", w.Code, w.Body)
	}
	awaitRepos(t, f.h)
	if err := f.st.SetRepos([]string{"acme/on"}); err != nil {
		t.Fatal(err)
	}
	calls := f.calls.Load()
	f.notConnected.Store(true)
	w := f.read()
	if w.Code != 200 {
		t.Fatalf("not connected: %d %s", w.Code, w.Body)
	}
	if !strings.Contains(w.Body.String(), "acme/on") || strings.Contains(w.Body.String(), "acme/api") {
		t.Fatalf("not connected answers the watched repos only: %s", w.Body)
	}
	if f.calls.Load() != calls {
		t.Fatal("not connected: no walk")
	}
}
