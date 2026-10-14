package praf

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf/orch"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

func invalidConsistencyResult() schemas.ReviewResult {
	r := sampleResult()
	r.Findings = []schemas.ScoredFinding{{DimensionID: "consistency-verify", Title: "draft note: the property holds; holds=true", Severity: "important"}}
	return r
}

func TestInvalidConsistencyOutputCannotBecomeASavedOrPostedReview(t *testing.T) {
	r, _ := testReviewer(&fakeSessions{}, func(context.Context, orch.Deps) (schemas.ReviewResult, error) { return invalidConsistencyResult(), nil })
	host := newTestHost(t)
	end := runReview(context.Background(), host, ReadBrief("o/r#7", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusFail {
		t.Fatalf("invalid output ended %s: %s", end.Status, end.Deliverable)
	}
	if _, err := os.Stat(filepath.Join(host.records, reportJSON)); !os.IsNotExist(err) {
		t.Fatalf("invalid review was saved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(host.records, "review-failure.json")); err != nil {
		t.Fatalf("missing failure record: %v", err)
	}
}

func TestLegacyInvalidConsistencyReviewIsRefusedBeforePosting(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-fixture")
	// A legacy file bypasses the current writer, as the retained trial does.
	saved := Saved{Program: Name, PullRequest: SavedPullRequest{Owner: "o", Repo: "r", Number: 7, HeadSHA: "abc"}, Review: invalidConsistencyResult()}
	b, _ := json.Marshal(saved)
	path := filepath.Join(t.TempDir(), reportJSON)
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	called := false
	end := runPost(context.Background(), newTestHost(t), path, func(string) gitHub { called = true; return &fakeGitHub{} })
	if end.Status != delegate.StatusFail || called || !strings.Contains(end.Message, "invalid consistency finding") {
		t.Fatalf("legacy invalid post: %+v, provider constructed=%v", end, called)
	}
}

func TestPartialPostingRequiresHonestCoverageAndCannotApprove(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*Saved)
		refused bool
	}{
		{"warned comment", func(*Saved) {}, false},
		{"missing warning", func(s *Saved) { s.Review.Review.Body = "No findings." }, true},
		{"approval", func(s *Saved) { s.Review.Review.Event = "APPROVE" }, true},
		{"contradictory complete", func(s *Saved) { s.Coverage.Complete = true }, true},
		{"negative count", func(s *Saved) { s.Coverage.FailedSessions = -1 }, true},
		{"metadata budget mismatch", func(s *Saved) { s.Review.Summary.BudgetExhausted = true }, true},
		{"legacy complete report", func(s *Saved) {
			s.Coverage = nil
			s.Review.Review = schemas.GitHubReview{Body: "summary", Event: "APPROVE"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Saved{Program: Name, PullRequest: SavedPullRequest{Owner: "o", Repo: "r", Number: 7, HeadSHA: "abc"}, Review: schemas.ReviewResult{Review: schemas.GitHubReview{Body: partialWarning + " One session failed.\n\nNo findings.", Event: "COMMENT"}}, Coverage: &ReportCoverage{FailedSessions: 1, Note: "One session failed."}}
			tc.mutate(&s)
			b, _ := json.Marshal(s)
			path := filepath.Join(t.TempDir(), reportJSON)
			if err := os.WriteFile(path, b, 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := readSaved(path)
			if (err != nil) != tc.refused {
				t.Fatalf("readSaved error=%v; refused want %v", err, tc.refused)
			}
		})
	}
}

func TestPartialReviewKeepsPassButPersistsCoverageAndCannotApprove(t *testing.T) {
	sessions := &fakeSessions{failed: "session deadline: private provider detail"}
	r, _ := testReviewer(sessions, func(ctx context.Context, deps orch.Deps) (schemas.ReviewResult, error) {
		_, _ = deps.App.Harness(ctx, "review", nil, nil, appxOptions())
		sessions.failed = ""
		_, _ = deps.App.Harness(ctx, "review", nil, nil, appxOptions())
		x := sampleResult()
		x.Findings = nil
		x.Summary = schemas.ReviewSummary{BySeverity: map[string]int{}}
		x.Review = schemas.GitHubReview{Body: "> **Safe to merge.** No findings from automated review.", Event: "APPROVE"}
		return x, nil
	})
	host := newTestHost(t)
	end := runReview(context.Background(), host, ReadBrief("o/r#7", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusPass {
		t.Fatalf("partial execution status changed: %+v", end)
	}
	saved, _, err := readSaved(host.records)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(saved)
	var data map[string]any
	_ = json.Unmarshal(b, &data)
	coverage, _ := data["coverage"].(map[string]any)
	if coverage == nil || coverage["complete"] != false || saved.Review.Review.Event != "COMMENT" || !strings.Contains(saved.Review.Review.Body, "Partial review") || end.Extra["partial"] != true {
		t.Fatalf("partial report lost its limits: %s; ending=%+v", b, end)
	}
	if !strings.HasPrefix(end.Message, "Partial review of ") || coverage["failed_sessions"] != float64(1) {
		t.Fatalf("partial first line or count missing: %+v; %s", end, b)
	}
	if strings.Contains(saved.Review.Review.Body, "Safe to merge") || strings.Contains(saved.Review.Review.Body, "private provider detail") {
		t.Fatalf("posting payload kept a clean approval or private exception: %s", saved.Review.Review.Body)
	}
	t.Setenv("GH_TOKEN", "gh-fixture")
	gh := &fakeGitHub{}
	posted := runPost(context.Background(), newTestHost(t), host.records, func(string) gitHub { return gh })
	if posted.Status != delegate.StatusPass || len(gh.posted) != 1 || gh.posted[0].Event != "COMMENT" || gh.posted[0].Body != saved.Review.Review.Body {
		t.Fatalf("partial warning changed on posting: %+v; %+v", posted, gh.posted)
	}
}
