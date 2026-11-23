package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func mergeItem() factory.Item {
	return factory.Item{
		Repo: "acme/api", Num: 12, Kind: factory.KindIssue,
		PullRequest: &factory.PullRequest{Number: 40, URL: "https://github.com/acme/api/pull/40", State: factory.PROpen},
	}
}

func TestWriteMergeUsesTheFirstMethodTheRepositoryAllows(t *testing.T) {
	var sent map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api":
			_ = json.NewEncoder(w).Encode(map[string]any{"allow_merge_commit": false, "allow_squash_merge": true, "allow_rebase_merge": true})
		case r.Method == http.MethodPut && r.URL.Path == "/repos/acme/api/pulls/40/merge":
			_ = json.NewDecoder(r.Body).Decode(&sent)
			_ = json.NewEncoder(w).Encode(map[string]any{"sha": "m3rg3", "merged": true})
		default:
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	rcpt, err := New(api, []string{"acme/api"}, nil).Write(context.Background(), factory.Action{Verb: "merge", Item: mergeItem(), Commit: "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	if sent["merge_method"] != "squash" || sent["sha"] != "abc123" || rcpt.ID != "m3rg3" || rcpt.Number != 40 || rcpt.URL != "https://github.com/acme/api/pull/40" {
		t.Fatalf("sent %v receipt %+v", sent, rcpt)
	}
}

func TestWriteMergeRefusesWithoutACommitOrOutsideTheWatchedRepositories(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("called the forge: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)
	api := forge.NewClient("test-token").WithBaseURL(srv.URL)
	if _, err := New(api, []string{"acme/api"}, nil).Write(context.Background(), factory.Action{Verb: "merge", Item: mergeItem()}); err == nil {
		t.Fatal("merged with no commit")
	}
	if _, err := New(api, []string{"acme/web"}, nil).Write(context.Background(), factory.Action{Verb: "merge", Item: mergeItem(), Commit: "abc123"}); !errors.Is(err, factory.ErrReadOnly) {
		t.Fatalf("merged outside the watched repositories: %v", err)
	}
}
