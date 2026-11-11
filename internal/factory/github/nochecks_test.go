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

// A REPOSITORY WITH NO CHECKS CONFIGURED STILL LEAVES DRAFT. The fact is read,
// never guessed: no workflow files on the PR head and no statuses or check
// runs reported, held on the same head past the settle span. The readiness
// review then runs and the draft is marked ready, and the job says plainly that
// it was reviewed without CI. A repository whose checks are pending or failing
// is exactly as before: no review, no ready.
func TestARepoWithNoChecksConfiguredReachesReady(t *testing.T) {
	for _, mode := range []string{"no ci", "pending", "failing"} {
		t.Run(mode, func(t *testing.T) {
			head := strings.Repeat("a", 40)
			draft, writes := true, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/graphql":
					writes++
					draft = false
					fmt.Fprintf(w, `{"data":{"markPullRequestReadyForReview":{"pullRequest":{"isDraft":false,"headRefOid":%q}}}}`, head)
				case strings.HasSuffix(r.URL.Path, "/pulls/7"):
					if strings.Contains(r.Header.Get("Accept"), "diff") {
						fmt.Fprint(w, "diff --git a/a.go b/a.go\n+fixed\n")
						return
					}
					fmt.Fprintf(w, `{"node_id":"PR_node","number":7,"title":"Fix it","body":"Verified result","state":"open","draft":%t,"mergeable":true,"changed_files":1,"html_url":"https://github.com/acme/api/pull/7","head":{"sha":%q},"base":{"ref":"main","sha":"base"}}`, draft, head)
				case strings.HasSuffix(r.URL.Path, "/pulls/7/files"):
					fmt.Fprint(w, `[{"filename":"a.go","status":"modified","additions":1}]`)
				case strings.Contains(r.URL.Path, "/check-runs"):
					switch mode {
					case "pending":
						fmt.Fprint(w, `{"total_count":1,"check_runs":[{"name":"test","status":"in_progress"}]}`)
					case "failing":
						fmt.Fprint(w, `{"total_count":1,"check_runs":[{"name":"test","status":"completed","conclusion":"failure"}]}`)
					default:
						fmt.Fprint(w, `{"total_count":0,"check_runs":[]}`)
					}
				case strings.HasSuffix(r.URL.Path, "/status"):
					fmt.Fprint(w, `{"state":"pending","total_count":0,"statuses":[]}`)
				case strings.Contains(r.URL.Path, "/contents/.github/workflows"):
					if r.URL.Query().Get("ref") != head {
						t.Errorf("workflows read at %q, not the PR head", r.URL.Query().Get("ref"))
					}
					if mode == "no ci" {
						w.WriteHeader(404)
						return
					}
					fmt.Fprint(w, `[{"type":"file","name":"ci.yml","path":".github/workflows/ci.yml"}]`)
				case strings.Contains(r.URL.Path, "/protection"):
					w.WriteHeader(404)
				case strings.Contains(r.URL.Path, "/rules/branches/"):
					fmt.Fprint(w, "[]")
				case strings.Contains(r.URL.Path, "/reviews"), strings.Contains(r.URL.Path, "/comments"):
					fmt.Fprint(w, "[]")
				default:
					t.Error("unexpected request", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer srv.Close()
			clock := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
			api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
			s := New(api, []string{"acme/api"}, func() time.Time { return clock })
			it := factory.Item{Repo: "acme/api", Kind: factory.KindIssue, State: factory.StateShipped, Publish: "pr", Cap: 10, HeadCommit: head, Autonomy: factory.AutonomyAuto,
				PullRequest: &factory.PullRequest{Number: 7, HeadSHA: head, Draft: true},
				FollowUp:    &factory.PRFollowUp{Kind: "pr", State: "healthy", Limit: 3}, Stream: &factory.Stream{}}
			follow := func() {
				t.Helper()
				pr, err := s.Follow(context.Background(), it)
				if err != nil {
					t.Fatal(err)
				}
				it.PullRequest = &pr
				factory.QueueCIFollowUp(&it, clock)
			}
			follow()
			if it.FollowUp.Kind == "readiness" {
				t.Fatal("readiness started before the push settled")
			}
			clock = clock.Add(factory.NoChecksSettle)
			follow()
			queued := it.FollowUp.Kind == "readiness"
			if mode != "no ci" {
				if queued || factory.ReadinessGate(it) == nil {
					t.Fatalf("a %s repo was treated as having no checks: %+v", mode, it.FollowUp)
				}
				return
			}
			if !queued {
				t.Fatalf("a repo with no checks never started its readiness review: %+v (gate: %v)", it.FollowUp, factory.ReadinessGate(it))
			}
			if !strings.Contains(it.FollowUp.Reason, "No GitHub checks in this repo; reviewed without CI") {
				t.Fatalf("the job does not say it has no checks: %q", it.FollowUp.Reason)
			}
			it.FollowUp.Readiness.State = "reviewed"
			got, err := s.Ready(context.Background(), it)
			if err != nil || got.Draft || writes != 1 {
				t.Fatalf("a reviewed no-CI draft was not marked ready: %+v %v writes=%d", got, err, writes)
			}
		})
	}
}
