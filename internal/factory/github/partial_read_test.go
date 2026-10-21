package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// Each fixture is an isolated forge. Every unsupported path is a test failure,
// including any write: these regressions only observe and queue local state.
type partialPRForge struct {
	mu               sync.Mutex
	rulesError       string
	checksFail       bool
	reviewPolicyFail bool
	feedbackFail     bool
	terminal         string
	comments         string
	calls            map[string]int
}

const partialPRHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const partialPRComment = "Please preserve the original token failure in source_runs."
const partialPRDenied = `{"message":"Resource not accessible by integration","documentation_url":"https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"}`
const partialPRPlan = `{"message":"Upgrade to GitHub Pro or make this repository public to enable this feature.","documentation_url":"https://docs.github.com/rest/repos/rules#get-rules-for-a-branch"}`

func newPartialPRForge(t *testing.T) (*partialPRForge, *Source) {
	t.Helper()
	f := &partialPRForge{comments: `[{"id":42,"body":"` + partialPRComment + `","updated_at":"2026-10-21T21:00:00Z","user":{"login":"reviewer"}}]`, calls: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls[r.URL.Path]++
		rulesError, checksFail, terminal, comments := f.rulesError, f.checksFail, f.terminal, f.comments
		reviewPolicyFail, feedbackFail := f.reviewPolicyFail, f.feedbackFail
		f.mu.Unlock()
		if r.Method != http.MethodGet {
			t.Errorf("unexpected mutation: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/repos/acme/api/pulls/7":
			if strings.Contains(r.Header.Get("Accept"), "diff") {
				fmt.Fprint(w, "diff --git a/a.go b/a.go\n+fixed\n")
				return
			}
			state, merged := "open", false
			if terminal != "" {
				state, merged = "closed", terminal == factory.PRMerged
			}
			fmt.Fprintf(w, `{"number":7,"title":"Surface token failures","body":"Preserves the original failure with verified tests.","state":%q,"merged":%t,"draft":true,"mergeable":true,"changed_files":1,"html_url":"https://github.com/acme/api/pull/7","head":{"sha":%q},"base":{"ref":"main","sha":"base"}}`, state, merged, partialPRHead)
		case "/repos/acme/api/commits/" + partialPRHead + "/check-runs":
			if checksFail {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, `{"message":"Resource not accessible by integration"}`)
				return
			}
			fmt.Fprint(w, `{"total_count":1,"check_runs":[{"name":"test","status":"completed","conclusion":"success"}]}`)
		case "/repos/acme/api/commits/" + partialPRHead + "/status":
			fmt.Fprint(w, `{"state":"success","statuses":[]}`)
		case "/repos/acme/api/pulls/7/reviews":
			fmt.Fprint(w, `[{"id":11,"state":"APPROVED","body":"The fix preserves the failure.","submitted_at":"2026-10-21T20:00:00Z","user":{"login":"reviewer"}}]`)
		case "/repos/acme/api/branches/main/protection":
			if reviewPolicyFail {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, partialPRDenied)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case "/repos/acme/api/branches/main/protection/required_status_checks":
			w.WriteHeader(http.StatusNotFound)
		case "/repos/acme/api/rules/branches/main":
			if rulesError != "" {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, rulesError)
			} else {
				fmt.Fprint(w, "[]")
			}
		case "/repos/acme/api/pulls/7/comments":
			fmt.Fprint(w, "[]")
		case "/repos/acme/api/issues/7/comments":
			if feedbackFail {
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprint(w, partialPRDenied)
			} else {
				fmt.Fprint(w, comments)
			}
		default:
			t.Errorf("unexpected forge read: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	api := forge.NewClient("test-token").WithBaseURL(srv.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
	return f, New(api, []string{"acme/api"}, nil)
}

func partialPRRead(t *testing.T, s *Source) factory.PullRequest {
	t.Helper()
	pr, err := s.FollowPR(context.Background(), "acme", "api", 7)
	if err != nil {
		t.Fatalf("optional section failure discarded the PR snapshot: %v", err)
	}
	return pr
}

func assertPartialPRFeedback(t *testing.T, pr factory.PullRequest) {
	t.Helper()
	if !pr.ReviewsRead || len(pr.Reviews) != 1 || pr.Reviews[0].By != "reviewer" || pr.ReviewDecision != "approved" || !strings.Contains(pr.Feedback, partialPRComment) {
		t.Fatalf("fresh reviews or comments were lost: reviews_read=%t reviews=%v decision=%q feedback=%q", pr.ReviewsRead, pr.Reviews, pr.ReviewDecision, pr.Feedback)
	}
}

func TestPartialPRReadinessDeniedPreservesFreshFacts(t *testing.T) {
	f, src := newPartialPRForge(t)
	f.rulesError = partialPRDenied
	pr := partialPRRead(t, src)
	if !pr.ChecksRead || pr.CI != "ci ✓" || pr.Checks != (factory.PRChecks{Passed: 1}) || pr.HeadSHA != partialPRHead || len(pr.CheckRuns) != 1 {
		t.Fatalf("current green CI was lost: checks_read=%t ci=%q checks=%+v head=%q", pr.ChecksRead, pr.CI, pr.Checks, pr.HeadSHA)
	}
	assertPartialPRFeedback(t, pr)
	if pr.ReadinessKey != "" || pr.ReadinessContext != "" || len(pr.Problems) != 1 || pr.Problems[0].Area != "readiness" || pr.Problems[0].Code != "access_denied" || pr.Problems[0].Retryable {
		t.Fatalf("generic 403 became an all-clear: key=%q problems=%+v", pr.ReadinessKey, pr.Problems)
	}
}

func TestPartialPRExplicitPlanRestrictionAllowsCompleteReadiness(t *testing.T) {
	f, src := newPartialPRForge(t)
	f.rulesError = partialPRPlan
	pr := partialPRRead(t, src)
	assertPartialPRFeedback(t, pr)
	if len(pr.Problems) != 0 || pr.ReadinessKey == "" || !strings.Contains(pr.ReadinessContext, "Complete PR diff:") || !strings.Contains(pr.ReadinessContext, partialPRComment) {
		t.Fatalf("explicit unavailable capability blocked complete review: key=%q problems=%+v", pr.ReadinessKey, pr.Problems)
	}
}

func TestPartialPRChecksDeniedStillReadsFreshReviewsAndComments(t *testing.T) {
	f, src := newPartialPRForge(t)
	f.checksFail = true
	pr := partialPRRead(t, src)
	assertPartialPRFeedback(t, pr)
	if pr.ChecksRead || pr.CI != "" || pr.ReadinessKey != "" || len(pr.Problems) != 1 || pr.Problems[0].Area != "checks" || pr.Problems[0].Code != "access_denied" {
		t.Fatalf("incomplete checks were mistaken for green: checks_read=%t ci=%q problems=%+v", pr.ChecksRead, pr.CI, pr.Problems)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls["/repos/acme/api/rules/branches/main"] != 0 {
		t.Fatal("requested readiness policy before CI could be verified")
	}
}

func TestPartialPRTerminalStateDoesNotDependOnChecksOrPolicy(t *testing.T) {
	for _, terminal := range []string{factory.PRMerged, factory.PRClosed} {
		t.Run(terminal, func(t *testing.T) {
			f, src := newPartialPRForge(t)
			f.terminal, f.checksFail, f.rulesError = terminal, true, partialPRDenied
			pr := partialPRRead(t, src)
			if pr.State != terminal || len(pr.Problems) != 0 {
				t.Fatalf("terminal state was blocked: state=%q problems=%+v", pr.State, pr.Problems)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.calls) != 1 || f.calls["/repos/acme/api/pulls/7"] != 1 {
				t.Fatalf("terminal PR queried unnecessary APIs: %v", f.calls)
			}
		})
	}
}

func TestPartialPRRecoveredPollResumesExistingWatchingIssueReadiness(t *testing.T) {
	f, src := newPartialPRForge(t)
	f.rulesError, f.comments = partialPRDenied, "[]"
	blocked := partialPRRead(t, src)
	now := time.Date(2026, 10, 21, 21, 0, 0, 0, time.UTC)
	it := factory.Item{Kind: factory.KindIssue, Repo: "acme/api", Num: 58, State: factory.StateShipped, Publish: "pr", HeadCommit: partialPRHead, Cap: 10, Autonomy: factory.AutonomyAuto,
		PullRequest: &blocked, FollowUp: &factory.PRFollowUp{Kind: "pr", State: "watching", Limit: 3},
		Proof:  []factory.Claim{{Text: "original issue verified", OK: true, Evidence: "test receipt"}},
		Stream: &factory.Stream{Phases: []factory.Phase{{Name: "proof", State: factory.PhaseDone}}}}
	if !factory.QueueCIFollowUp(&it, now) || it.State != factory.StateShipped || it.FollowUp.State != "watching" || it.FollowUp.Attempt != 0 || !strings.Contains(it.FollowUp.Reason, "Ready for review blocked:") {
		t.Fatalf("blocked readiness was not explicit and recoverable: state=%q follow=%+v", it.State, it.FollowUp)
	}
	// A later poll on the same client/server gains access. No Start, user
	// message, reset, or reconstruction of the existing job is required.
	f.mu.Lock()
	f.rulesError = ""
	f.mu.Unlock()
	recovered := partialPRRead(t, src)
	if len(recovered.Problems) != 0 || recovered.ReadinessKey == "" {
		t.Fatalf("successful poll retained old failure: key=%q problems=%+v", recovered.ReadinessKey, recovered.Problems)
	}
	it.PullRequest = &recovered
	if !factory.QueueCIFollowUp(&it, now.Add(time.Minute)) || it.State != factory.StateQueued || it.FollowUp.State != "queued" || it.FollowUp.Kind != "readiness" || it.FollowUp.Readiness == nil || it.FollowUp.Readiness.Key != recovered.ReadinessKey || it.FollowUp.Attempt != 1 {
		t.Fatalf("existing shipped issue did not resume final review: state=%q follow=%+v", it.State, it.FollowUp)
	}
	if len(it.Proof) != 1 || it.Proof[0].Text != "original issue verified" {
		t.Fatal("recovering readiness discarded the original issue proof")
	}
	if factory.QueueCIFollowUp(&it, now.Add(2*time.Minute)) || it.FollowUp.Attempt != 1 {
		t.Fatal("the unchanged queued review was scheduled twice")
	}
}

func TestPartialPRReadProblemClassification(t *testing.T) {
	var decoded any
	malformed := json.Unmarshal([]byte("{"), &decoded)
	for _, tc := range []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{"authentication", &forge.APIError{StatusCode: 401}, "authentication", false},
		{"generic forbidden", &forge.APIError{StatusCode: 403, Body: partialPRDenied}, "access_denied", false},
		{"wrapped forbidden", fmt.Errorf("rules read: %w", &forge.APIError{StatusCode: 403}), "access_denied", false},
		{"rate limit forbidden", &forge.APIError{StatusCode: 403, Body: `{"message":"API rate limit exceeded"}`}, "transient", true},
		{"retry after forbidden", &forge.APIError{StatusCode: 403, RetryAfter: time.Second}, "transient", true},
		{"rate limit", &forge.APIError{StatusCode: 429}, "transient", true},
		{"server unavailable", &forge.APIError{StatusCode: 503}, "transient", true},
		{"not found", &forge.APIError{StatusCode: 404}, "unavailable", false},
		{"network timeout", &net.DNSError{Err: "request timed out", IsTimeout: true}, "transient", true},
		{"deadline", context.DeadlineExceeded, "transient", true},
		{"malformed JSON", malformed, "incomplete", false},
		{"ordinary error", errors.New("diff unavailable"), "incomplete", false},
		{"bounded diagnostic", errors.New(strings.Repeat("diagnostic ", 400)), "incomplete", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := readProblem("readiness", tc.err)
			if p.Area != "readiness" || p.Code != tc.code || p.Retryable != tc.retryable || !strings.Contains(p.Summary, "PR readiness") {
				t.Fatalf("wrong problem classification: %+v", p)
			}
			want := tc.err.Error()
			if len(want) > 2000 {
				want = want[:2000]
			}
			if len(p.Detail) > 2000 || p.Detail != want {
				t.Fatalf("diagnostic was lost or unbounded: length=%d want=%d", len(p.Detail), len(want))
			}
		})
	}
}

func TestPartialPRReviewSectionFailurePreservesOtherCurrentFacts(t *testing.T) {
	for _, area := range []string{"review policy", "feedback"} {
		t.Run(area, func(t *testing.T) {
			f, src := newPartialPRForge(t)
			f.reviewPolicyFail = area == "review policy"
			f.feedbackFail = area == "feedback"
			pr := partialPRRead(t, src)
			if !pr.ChecksRead || pr.CI != "ci ✓" || pr.Checks != (factory.PRChecks{Passed: 1}) {
				t.Fatalf("review read error discarded fresh CI: checks_read=%t ci=%q checks=%+v", pr.ChecksRead, pr.CI, pr.Checks)
			}
			if pr.ReviewsRead || len(pr.Reviews) != 1 || pr.Reviews[0].By != "reviewer" || pr.Reviews[0].State != "approved" || pr.ReadinessKey != "" {
				t.Fatalf("formal reviews were lost or incomplete review treated as ready: reviews_read=%t reviews=%v key=%q", pr.ReviewsRead, pr.Reviews, pr.ReadinessKey)
			}
			if len(pr.Problems) != 1 || pr.Problems[0].Area != area || pr.Problems[0].Code != "access_denied" {
				t.Fatalf("missing section-specific failure: %+v", pr.Problems)
			}
			if area == "review policy" && !strings.Contains(pr.Feedback, partialPRComment) {
				t.Fatal("review policy failure prevented fresh conversation feedback")
			}
		})
	}
}
