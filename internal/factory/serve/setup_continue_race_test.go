package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

// midReadSetup is a team factory whose setup has read acme/api whole and is
// reading acme/big, held on its second page until the read is stopped. When a
// read of that page is stopped, cancelled is closed and the reader returns
// only once release is closed.
func midReadSetup(t *testing.T) (f *peopleV2Fixture, cancelled, release chan struct{}) {
	t.Helper()
	f = peopleV2Setup(t, api.FactoryTeam)
	cancelled, release = make(chan struct{}), make(chan struct{})
	var once sync.Once
	if err := factorygithub.SaveReadGrant(factorygithub.GrantPath(f.cfg.TokensPath), factorygithub.ReadGrant{Token: "read-only", Login: "owner", GrantedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	full := []map[string]any{}
	for i := 1; i <= 100; i++ {
		full = append(full, map[string]any{"number": i, "title": fmt.Sprintf("big %d", i)})
	}
	page, _ := json.Marshal(full)
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		body := `[]`
		switch {
		case r.URL.Path == "/repos/acme/api/issues":
			body = `[{"number":1,"title":"small"}]`
		case r.URL.Path == "/repos/acme/big/issues" && r.URL.Query().Get("page") == "1":
			body = string(page)
		case r.URL.Path == "/repos/acme/big/issues":
			<-r.Context().Done()
			once.Do(func() { close(cancelled) })
			<-release
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	out := setupState{View: api.FactorySetup{ID: "local", Mode: "team", State: "reading", Repos: []api.SetupRepo{
		{ID: "acme/api", Selected: true, Read: "next", Clone: "missing"},
		{ID: "acme/big", Selected: true, Read: "next", Clone: "missing"},
	}}}
	if err := f.h.saveSetup(out); err != nil {
		t.Fatal(err)
	}
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, nil)
	untilSetup(t, f.h, "the big repo mid-read", func(s setupState) bool {
		return setupRead(s, "acme/api") == "done" && setupRead(s, "acme/big") == "reading" && s.Pages["acme/big"] == 2
	})
	return f, cancelled, release
}

// assertContinued checks that reading ended for good: no reader runs, setup
// is at checkouts, and only the repository read whole is watched.
func assertContinued(t *testing.T, f *peopleV2Fixture) {
	t.Helper()
	f.h.setupMu.Lock()
	busy := f.h.setupBusy
	f.h.setupMu.Unlock()
	saved, _ := f.h.loadSetup("local")
	if busy || saved.View.State != "checkouts" || setupRead(saved, "acme/big") != setupReadSkipped {
		t.Fatalf("reading goes on after Continue: busy %v, %+v", busy, saved.View)
	}
	if watched, _ := f.st.Repos(); len(watched) != 1 || watched[0] != "acme/api" {
		t.Fatalf("watched %v", watched)
	}
}

// A window reading setup while Continue stops the reader cannot start the
// reader again: Continue answers 200 and reading ends. The read is made to
// arrive in the gap between the stopped reader letting go and Continue
// taking its step: the reader and then the read queue on the setup lock
// while the test holds it, and a lock waited on this long is handed to its
// waiters in order, ahead of Continue.
func TestFactorySetupContinueHoldsAgainstAConcurrentRead(t *testing.T) {
	f, cancelled, release := midReadSetup(t)
	answer := make(chan int, 1)
	go func() {
		answer <- identityRequest(f.h, "POST", "/v1/factory-setup/local", `{"verb":"continue"}`, "operator-secret", nil).Code
	}()
	<-cancelled
	f.h.setupMu.Lock()
	close(release)
	time.Sleep(20 * time.Millisecond)
	read := make(chan struct{})
	go func() {
		defer close(read)
		identityRequest(f.h, "GET", "/v1/factory-setup/local", "", "operator-secret", nil)
	}()
	time.Sleep(20 * time.Millisecond)
	f.h.setupMu.Unlock()
	<-read
	if code := <-answer; code != 200 {
		t.Fatalf("Continue with a window reading setup answered %d", code)
	}
	assertContinued(t, f)
}

// Two Continues at once end reading once: one is taken, the other is told
// setup is no longer reading, and no reader is left running.
func TestFactorySetupDoubleContinueEndsReadingOnce(t *testing.T) {
	f, _, release := midReadSetup(t)
	close(release)
	codes := make(chan int, 2)
	for range 2 {
		go func() {
			codes <- identityRequest(f.h, "POST", "/v1/factory-setup/local", `{"verb":"continue"}`, "operator-secret", nil).Code
		}()
	}
	a, b := <-codes, <-codes
	if a+b != 200+409 {
		t.Fatalf("two Continues answered %d and %d, want one 200 and one 409", a, b)
	}
	assertContinued(t, f)
}
