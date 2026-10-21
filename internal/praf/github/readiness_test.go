package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReadinessMaterialReadsDiffAndBothRequiredCheckPolicies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			if r.Header.Get("Accept") != "application/vnd.github.v3.diff" {
				t.Error("not reading actual diff")
			}
			fmt.Fprint(w, "diff --git a/a b/a\n+fixed")
		case strings.HasSuffix(r.URL.Path, "/required_status_checks"):
			fmt.Fprint(w, `{"contexts":["build"],"checks":[{"context":"test"}]}`)
		case strings.Contains(r.URL.Path, "/rules/branches/"):
			fmt.Fprint(w, `[{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"security"}]}}]`)
		default:
			t.Error(r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	diff, required, err := testClient(srv.URL, "tok").ReadinessMaterial(context.Background(), "o", "r", "main", 7)
	if err != nil || !strings.Contains(diff, "+fixed") || len(required) != 3 || required[0].Context != "build" || required[1].Context != "security" || required[2].Context != "test" {
		t.Fatalf("material incomplete: %v", err)
	}
}
func TestReadinessMarkIsSingleMutationAndRechecksHead(t *testing.T) {
	for _, mode := range []string{"ok", "moved", "already ready", "uncertain", "graphql refusal"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			posts := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method == "GET" {
					h := "head"
					if mode == "moved" {
						h = "new"
					}
					fmt.Fprintf(w, `{"node_id":"PR_node","draft":%t,"state":"open","head":{"sha":%q}}`, mode != "already ready", h)
					return
				}
				posts++
				var p struct {
					Query string `json:"query"`
				}
				json.NewDecoder(r.Body).Decode(&p)
				if !strings.Contains(p.Query, "markPullRequestReadyForReview") || strings.Contains(p.Query, "mergePullRequest") {
					t.Error("wrong mutation")
				}
				if mode == "uncertain" {
					w.WriteHeader(503)
					return
				}
				if mode == "graphql refusal" {
					fmt.Fprint(w, `{"errors":[{"message":"not permitted"}]}`)
					return
				}
				fmt.Fprint(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false,"headRefOid":"head"}}}}`)
			}))
			defer srv.Close()
			err := testClient(srv.URL, "tok").MarkPullReady(context.Background(), "o", "r", 7, "head")
			if (mode == "ok" || mode == "already ready") && err != nil {
				t.Fatal(err)
			}
			if mode != "ok" && mode != "already ready" && err == nil {
				t.Fatal("unsafe outcome accepted")
			}
			want := 1
			if mode == "moved" || mode == "already ready" {
				want = 0
			}
			if posts != want || calls > 2 {
				t.Fatal("mutation replayed or sent to moved head")
			}
		})
	}
}
func TestReadinessUnknownPolicyAndOversizeDiffAreBlocked(t *testing.T) {
	for _, mode := range []string{"forbidden", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "pulls") {
					if mode == "oversize" {
						fmt.Fprint(w, strings.Repeat("x", 192<<10+1))
					} else {
						fmt.Fprint(w, "diff")
					}
					return
				}
				w.WriteHeader(403)
			}))
			defer srv.Close()
			if _, _, e := testClient(srv.URL, "tok").ReadinessMaterial(context.Background(), "o", "r", "main", 7); e == nil {
				t.Fatal("unread evidence treated as complete")
			}
		})
	}
}
