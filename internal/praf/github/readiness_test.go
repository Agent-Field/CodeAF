package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

func TestPolicyUnavailableRecognizesOnlyExplicitPlanRestriction(t *testing.T) {
	message := "Upgrade to GitHub Pro or make this repository public to enable this feature."
	docs := "https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"
	body := func(message, docs string) string {
		b, err := json.Marshal(map[string]string{"message": message, "documentation_url": docs})
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"rules plan restriction", &APIError{StatusCode: 403, Body: body(message, docs)}, true},
		{"classic protection plan restriction", &APIError{StatusCode: 403, Body: body(message, "https://docs.github.com/rest/branches/branch-protection#get-branch-protection")}, true},
		{"classic required checks plan restriction", &APIError{StatusCode: 403, Body: body(message, "https://docs.github.com/rest/branches/branch-protection#get-status-checks-protection")}, true},
		{"wrapped plan restriction", fmt.Errorf("policy: %w", &APIError{StatusCode: 403, Body: body(message, docs)}), true},
		{"missing permission", &APIError{StatusCode: 403, Body: body("Resource not accessible by integration", docs)}, false},
		{"generic forbidden", &APIError{StatusCode: 403, Body: body("Forbidden", docs)}, false},
		{"rate limit", &APIError{StatusCode: 403, Body: body("API rate limit exceeded", docs)}, false},
		{"rate limit header with plan body", &APIError{StatusCode: 403, Body: body(message, docs), RetryAfter: time.Second}, false},
		{"wrong endpoint documentation", &APIError{StatusCode: 403, Body: body(message, "https://docs.github.com/rest/pulls/pulls#get-a-pull-request")}, false},
		{"missing endpoint documentation", &APIError{StatusCode: 403, Body: body(message, "")}, false},
		{"similar message", &APIError{StatusCode: 403, Body: body(message+" Contact an administrator.", docs)}, false},
		{"non-json body", &APIError{StatusCode: 403, Body: message}, false},
		{"not found", &APIError{StatusCode: 404, Body: body(message, docs)}, false},
		{"too many requests", &APIError{StatusCode: 429, Body: body(message, docs)}, false},
		{"server failure", &APIError{StatusCode: 503, Body: body(message, docs)}, false},
		{"non-api failure", errors.New(message), false},
		{"nil", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PolicyUnavailable(tc.err); got != tc.want {
				t.Fatalf("PolicyUnavailable = %t; want %t", got, tc.want)
			}
		})
	}
}

func TestReadinessPlanUnavailableRetainsOtherApplicableRequirements(t *testing.T) {
	for _, unavailable := range []string{"rules", "classic"} {
		t.Run(unavailable, func(t *testing.T) {
			classicRead, rulesRead := false, false
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/pulls/7"):
					fmt.Fprint(w, "diff --git a/a b/a\n+fixed")
				case strings.HasSuffix(r.URL.Path, "/required_status_checks"):
					classicRead = true
					if unavailable == "classic" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/branches/branch-protection#get-status-checks-protection"}`)
					} else {
						fmt.Fprint(w, `{"checks":[{"context":"build","app_id":3}]}`)
					}
				case strings.Contains(r.URL.Path, "/rules/branches/"):
					rulesRead = true
					if unavailable == "rules" {
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"}`)
					} else {
						fmt.Fprint(w, `[{"type":"required_status_checks","parameters":{"required_status_checks":[{"context":"security","integration_id":11}]}}]`)
					}
				default:
					t.Error(r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer srv.Close()
			diff, required, err := testClient(srv.URL, "tok").ReadinessMaterial(context.Background(), "o", "r", "main", 7)
			if err != nil {
				t.Fatal(err)
			}
			want := RequiredCheck{Context: "build", AppID: 3}
			if unavailable == "classic" {
				want = RequiredCheck{Context: "security", AppID: 11}
			}
			if !strings.Contains(diff, "+fixed") || !classicRead || !rulesRead || len(required) != 1 || required[0] != want {
				t.Fatalf("applicable policy lost: diff=%q classic=%t rules=%t required=%v; want %v", diff, classicRead, rulesRead, required, want)
			}
		})
	}
}

func TestReadinessPermissionAndRateLimitFailuresRemainBlocked(t *testing.T) {
	for _, endpoint := range []string{"rules", "classic"} {
		for _, mode := range []string{"permission", "rate limit", "retry header"} {
			t.Run(endpoint+"/"+mode, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if strings.HasSuffix(r.URL.Path, "/pulls/7") {
						fmt.Fprint(w, "diff --git a/a b/a\n+fixed")
						return
					}
					classic := strings.HasSuffix(r.URL.Path, "/required_status_checks")
					if (endpoint == "classic") != classic {
						if classic {
							fmt.Fprint(w, `{"checks":[{"context":"build"}]}`)
						} else {
							fmt.Fprint(w, `[]`)
						}
						return
					}
					message := "Resource not accessible by integration"
					if mode == "rate limit" {
						message = "API rate limit exceeded"
					} else if mode == "retry header" {
						message = "Upgrade to GitHub Pro or make this repository public to enable this feature."
						w.Header().Set("Retry-After", "1")
					}
					w.WriteHeader(http.StatusForbidden)
					json.NewEncoder(w).Encode(map[string]string{"message": message, "documentation_url": "https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"})
				}))
				defer srv.Close()
				_, _, err := testClient(srv.URL, "tok").ReadinessMaterial(context.Background(), "o", "r", "main", 7)
				var apiErr *APIError
				if err == nil || !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden || PolicyUnavailable(err) {
					t.Fatalf("policy failure must block readiness: %v", err)
				}
			})
		}
	}
}

func TestReadinessChangedPolicyDuringPaginationRemainsBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/7"):
			fmt.Fprint(w, "diff --git a/a b/a\n+fixed")
		case strings.HasSuffix(r.URL.Path, "/required_status_checks"):
			w.WriteHeader(404)
		case r.URL.Query().Get("page") == "1":
			fmt.Fprint(w, "["+strings.Repeat(`{"type":"pull_request"},`, 99)+`{"type":"pull_request"}]`)
		default:
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"}`)
		}
	}))
	defer srv.Close()
	if _, _, e := testClient(srv.URL, "tok").ReadinessMaterial(context.Background(), "o", "r", "main", 7); e == nil {
		t.Fatal("partially read policy treated as complete")
	}
}
