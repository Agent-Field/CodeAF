package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
)

// untilSetup reads the saved setup until ok, for the reader finishing a step.
func untilSetup(t *testing.T, h *Handler, what string, ok func(setupState) bool) setupState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.setupMu.Lock()
		saved, err := h.loadSetup("local")
		h.setupMu.Unlock()
		if err == nil && ok(saved) {
			return saved
		}
		if time.Now().After(deadline) {
			t.Fatalf("setup never reached %s: %+v %v", what, saved.View, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func setupRead(s setupState, id string) string {
	for _, r := range s.View.Repos {
		if r.ID == id {
			return r.Read
		}
	}
	return ""
}

// Continue ends reading while a large repository is still being read: the
// repository already read is watched, the one still reading stops, keeps the
// issues it already brought held in Backlog, and is not watched, so a poll
// cannot import the rest of its issues as jobs that start by themselves.
func TestFactorySetupContinueEndsReadingWithoutWatchingUnreadRepos(t *testing.T) {
	f := peopleV2Setup(t, api.FactoryTeam)
	if err := factorygithub.SaveReadGrant(factorygithub.GrantPath(f.cfg.TokensPath), factorygithub.ReadGrant{Token: "read-only", Login: "owner", GrantedAt: f.now}); err != nil {
		t.Fatal(err)
	}
	full := []map[string]any{}
	for i := 1; i <= 100; i++ {
		full = append(full, map[string]any{"number": i, "title": fmt.Sprintf("big %d", i), "labels": []map[string]string{{"name": "ready"}}})
	}
	page, _ := json.Marshal(full)
	stopped := make(chan struct{})
	f.h.config.GitHubHTTPClient = &http.Client{Transport: githubTransport(func(r *http.Request) (*http.Response, error) {
		body := `[]`
		switch {
		case r.URL.Path == "/repos/acme/api/issues":
			body = `[{"number":1,"title":"small"}]`
		case r.URL.Path == "/repos/acme/big/issues" && r.URL.Query().Get("page") == "1":
			body = string(page)
		case r.URL.Path == "/repos/acme/big/issues":
			<-r.Context().Done()
			close(stopped)
			return nil, r.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	out := setupState{View: api.FactorySetup{ID: "local", Mode: "team", State: "reading", Repos: []api.SetupRepo{
		{ID: "acme/api", Selected: true, Read: "next", Clone: "missing"},
		{ID: "acme/big", Selected: true, Read: "next", Clone: "missing"},
		{ID: "acme/other", Read: "next", Clone: "missing"},
	}}}
	if err := f.h.saveSetup(out); err != nil {
		t.Fatal(err)
	}
	// Nothing is read yet, so there is nothing to continue with.
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "continue"}, 409, nil)
	f.request(t, "GET", "/v1/factory-setup/local", "operator-secret", nil, 200, nil)
	untilSetup(t, f.h, "the big repo mid-read", func(s setupState) bool {
		return setupRead(s, "acme/api") == "done" && setupRead(s, "acme/big") == "reading" && s.Pages["acme/big"] == 2
	})

	var setup api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "continue"}, 200, &setup)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the reader was not stopped")
	}
	if setup.State != "checkouts" || setup.Repos[0].Read != "done" || setup.Repos[1].Read != "skipped" || setup.Repos[2].Read != "next" {
		t.Fatalf("continue: %+v", setup)
	}
	f.h.setupMu.Lock()
	busy := f.h.setupBusy
	f.h.setupMu.Unlock()
	if busy {
		t.Fatal("a reader still runs after continue")
	}
	if watched, _ := f.st.Repos(); len(watched) != 1 || watched[0] != "acme/api" {
		t.Fatalf("watched %v; only the repository read whole may be polled", watched)
	}
	items, _ := f.st.List()
	if len(items) != 101 {
		t.Fatalf("imported %d issues, want the 101 already read", len(items))
	}
	for _, it := range items {
		if !it.SetupHeld {
			t.Fatalf("an imported issue is not held: %+v", it)
		}
	}
	// Continue belongs to reading only.
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "continue"}, 409, nil)
}

// Discovery quotes each repository's size from the list GitHub already sent,
// in whole megabytes rounded up, so a person sees what a clone will take.
func TestFactorySetupDiscoveryQuotesGitHubSizeInMegabytes(t *testing.T) {
	f := peopleV2Setup(t, api.FactorySolo)
	fail := false
	setupGitHub(t, f, &fail)
	answer := `[{"full_name":"acme/api","size":2048},{"full_name":"acme/tiny","size":1},{"full_name":"acme/empty","size":0}]`
	reposAnswer(f, &answer)
	var out api.FactorySetup
	f.request(t, "POST", "/v1/factory-setup/local", "operator-secret", api.FactorySetupRequest{Verb: "connect", Source: "token", Token: "setup-secret"}, 200, &out)
	got := map[string]int{}
	for _, r := range out.Repos {
		got[r.ID] = r.CloneMB
	}
	if got["acme/api"] != 2 || got["acme/tiny"] != 1 || got["acme/empty"] != 0 {
		t.Fatalf("clone_mb: %v", got)
	}
}
