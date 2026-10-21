package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestReadinessSourceRevalidatesEverythingAndConfirmsTheMutation(t *testing.T) {
	for _, mode := range []string{"ok", "body moved", "comment arrived", "head moved", "checks pending", "already ready", "uncertain", "wrong check app"} {
		t.Run(mode, func(t *testing.T) {
			policyPinned := false
			draft := true
			head := strings.Repeat("a", 40)
			body := "Verified result"
			comments := "[]"
			pending := false
			writes := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					writes++
					draft = false
					if mode == "uncertain" {
						w.WriteHeader(503)
						return
					}
					fmt.Fprintf(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false,"headRefOid":%q}}}}`, head)
				case strings.HasSuffix(r.URL.Path, "/pulls/7"):
					if strings.Contains(r.Header.Get("Accept"), "diff") {
						fmt.Fprint(w, "diff --git a/a.go b/a.go\n+fixed\n")
						return
					}
					fmt.Fprintf(w, `{"node_id":"PR_node","number":7,"title":"Fix it","body":%q,"state":"open","draft":%t,"mergeable":true,"changed_files":1,"html_url":"https://github.com/acme/api/pull/7","head":{"sha":%q},"base":{"ref":"main","sha":"base"}}`, body, draft, head)
				case strings.Contains(r.URL.Path, "/check-runs"):
					if pending {
						fmt.Fprint(w, `{"total_count":1,"check_runs":[{"name":"test","status":"in_progress"}]}`)
					} else {
						fmt.Fprint(w, `{"total_count":1,"check_runs":[{"name":"test","status":"completed","conclusion":"success"}]}`)
					}
				case strings.HasSuffix(r.URL.Path, "/status"):
					fmt.Fprint(w, `{"state":"success","statuses":[]}`)
				case strings.Contains(r.URL.Path, "/protection"):
					if mode == "wrong check app" && policyPinned && strings.HasSuffix(r.URL.Path, "required_status_checks") {
						fmt.Fprint(w, `{"checks":[{"context":"test","app_id":123}]}`)
					} else {
						w.WriteHeader(404)
					}
				case strings.Contains(r.URL.Path, "/rules/branches/"):
					fmt.Fprint(w, "[]")
				case strings.Contains(r.URL.Path, "/issues/7/comments"):
					fmt.Fprint(w, comments)
				case strings.Contains(r.URL.Path, "/reviews"), strings.Contains(r.URL.Path, "/comments"):
					fmt.Fprint(w, "[]")
				default:
					t.Error("unexpected request", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
			s := New(api, []string{"acme/api"}, nil)
			pr, err := s.FollowPR(context.Background(), "acme", "api", 7)
			if err != nil {
				t.Fatal(err)
			}
			if pr.ReadinessKey == "" || !strings.Contains(pr.ReadinessContext, body) {
				t.Fatal("incomplete review material")
			}
			it := factory.Item{Repo: "acme/api", Kind: factory.KindIssue, Cap: 10, HeadCommit: head, PullRequest: &pr, FollowUp: &factory.PRFollowUp{Kind: "readiness", Readiness: &factory.PRReadiness{Key: pr.ReadinessKey, Head: head, State: "reviewed"}}}
			switch mode {
			case "body moved":
				body = "Changed description"
			case "comment arrived":
				comments = `[{"id":1,"body":"Please fix the race","updated_at":"2026-10-21T20:00:00Z","user":{"login":"reviewer"}}]`
			case "head moved":
				head = strings.Repeat("b", 40)
			case "checks pending":
				pending = true
			case "wrong check app":
				policyPinned = true
			case "already ready":
				draft = false
			}
			got, err := s.Ready(context.Background(), it)
			if mode == "ok" || mode == "already ready" {
				if err != nil || got.Draft {
					t.Fatalf("ready not confirmed: %v", err)
				}
			} else if err == nil {
				t.Fatal("changed/uncertain result accepted")
			}
			want := 0
			if mode == "ok" || mode == "uncertain" {
				want = 1
			}
			if writes != want {
				t.Fatalf("ready writes=%d want %d", writes, want)
			}
			if mode == "uncertain" {
				got, err = s.Ready(context.Background(), it)
				if err != nil || got.Draft || writes != 1 {
					t.Fatal("uncertain success replayed mutation")
				}
			}
		})
	}
}

func TestReadinessCommentReviewDoesNotClearAnOutstandingRequest(t *testing.T) {
	now := time.Now()
	reviews := []forge.Review{{User: "reviewer", State: "CHANGES_REQUESTED", At: now}, {User: "reviewer", State: "COMMENTED", At: now.Add(time.Second)}}
	if reviewDecision(factoryReviews(reviews), 0) != "changes_requested" {
		t.Fatal("an ordinary comment cleared a requested-changes review")
	}
	reviews = append(reviews, forge.Review{User: "reviewer", State: "APPROVED", At: now.Add(2 * time.Second)})
	if reviewDecision(factoryReviews(reviews), 0) != "approved" {
		t.Fatal("a real later approval was ignored")
	}
}
