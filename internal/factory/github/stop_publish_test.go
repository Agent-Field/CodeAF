package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestStopPublishAfterForgePreparation(t *testing.T) {
	f := newFixture()
	stopped := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api" {
			stopped = true
		}
		f.handle(w, r)
	}))
	defer srv.Close()
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	src := New(api, []string{"acme/api"}, nil)
	stopErr := errors.New("stopped during forge preparation")
	_, err := src.Write(context.Background(), factory.Action{Verb: "pr", Item: factory.Item{Repo: "acme/api", Num: 12, Title: "Stop publication"}, Branch: "feat", Draft: true, BeforeWrite: func(context.Context) error {
		if !stopped {
			t.Error("check ran before forge preparation")
		}
		return stopErr
	}})
	if !errors.Is(err, stopErr) {
		t.Errorf("Stop did not prevent PR request: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if got := f.hits["POST /repos/acme/api/pulls"]; got != 0 {
		t.Errorf("stopped job sent %d PR requests", got)
	}
}
