package serve

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

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
	h       *Handler
	st      *store.Store
	now     atomic.Int64
	calls   atomic.Int32
	who     atomic.Value
	token   atomic.Value
	refused atomic.Bool
	walk    func(context.Context) ([]factory.RepoInfo, error)
}

func newReposRig(t *testing.T) *reposRig {
	t.Helper()
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
	done := h.reposRead.done
	h.reposRead.mu.Unlock()
	if done != nil {
		select {
		case <-done:
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
	f.now.Add(int64(6 * time.Minute))
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := f.read(); w.Code != 200 {
				t.Errorf("%d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	<-entered
	if f.calls.Load() != 2 {
		t.Fatalf("walks=%d", f.calls.Load())
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

func TestReposFailedBackgroundWalkKeepsListAndKeepsFailing(t *testing.T) {
	f := newReposRig(t)
	if w := f.read(); w.Code != 200 {
		t.Fatal(w.Body)
	}
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, githubStatusError(401) }
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
}

func TestReposFirstFailedWalkDoesNotRepeatBeforeSlowTimer(t *testing.T) {
	f := newReposRig(t)
	f.walk = func(context.Context) ([]factory.RepoInfo, error) { return nil, githubStatusError(401) }
	for range 3 {
		if w := f.read(); w.Code != 500 {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	if f.calls.Load() != 1 {
		t.Fatalf("walks=%d, want 1", f.calls.Load())
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
