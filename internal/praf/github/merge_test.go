package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func mergeServer(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
}

// The merge names the exact head, so GitHub can refuse one that moved.
func TestMergePullRequestSendsTheExactHeadAndMethod(t *testing.T) {
	var got map[string]any
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/repos/acme/api/pulls/7/merge" {
			http.Error(w, r.Method+" "+r.URL.Path, http.StatusNotFound)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"sha": "m3rg3", "merged": true, "message": "Pull Request successfully merged"})
	})
	m, err := c.MergePullRequest(context.Background(), "acme", "api", 7, "abc123", "squash")
	if err != nil {
		t.Fatal(err)
	}
	if m.SHA != "m3rg3" || got["sha"] != "abc123" || got["merge_method"] != "squash" {
		t.Fatalf("merged %+v, sent %v", m, got)
	}
}

func TestMergePullRequestNamesTheForgeRefusal(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   error
	}{
		{http.StatusConflict, ErrHeadMoved},
		{http.StatusMethodNotAllowed, ErrNotMergeable},
	} {
		c := mergeServer(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(`{"message":"Head branch was modified. Review and try the merge again."}`))
		})
		_, err := c.MergePullRequest(context.Background(), "acme", "api", 7, "abc123", "")
		if !errors.Is(err, tc.want) {
			t.Fatalf("%d: err = %v, want %v", tc.status, err, tc.want)
		}
	}
}

// A MERGE IS SENT ONCE: a lost answer may hide a merge that happened, so a
// server error is returned rather than tried again.
func TestMergePullRequestIsNeverRetried(t *testing.T) {
	var calls atomic.Int32
	c := mergeServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	if _, err := c.MergePullRequest(context.Background(), "acme", "api", 7, "abc123", ""); err == nil {
		t.Fatal("a 502 merged")
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("merge sent %d times", n)
	}
}

func TestMergePullRequestRefusesWithoutAnExactHead(t *testing.T) {
	c := mergeServer(t, func(w http.ResponseWriter, _ *http.Request) { t.Fatal("sent a merge with no head") })
	if _, err := c.MergePullRequest(context.Background(), "acme", "api", 7, " ", "merge"); err == nil {
		t.Fatal("merged with no head")
	}
}

func TestMergeMethodsReadsTheRepositorySettings(t *testing.T) {
	c := mergeServer(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"allow_merge_commit": false, "allow_squash_merge": true})
	})
	got, err := c.MergeMethods(context.Background(), "acme", "api")
	if err != nil {
		t.Fatal(err)
	}
	// rebase is absent from the answer, which is GitHub's default: allowed.
	if len(got) != 2 || got[0] != "squash" || got[1] != "rebase" {
		t.Fatalf("methods %v", got)
	}
}
