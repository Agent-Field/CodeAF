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

func TestStoppedReviewKeepsANonPostableFailureRecord(t *testing.T) {
	r, _ := testReviewer(&fakeSessions{failed: "the reply holds no JSON object"}, func(ctx context.Context, deps orch.Deps) (schemas.ReviewResult, error) {
		_, _ = deps.App.Harness(ctx, "review", nil, nil, appxOptions())
		return schemas.ReviewResult{}, context.Canceled
	})
	host := newTestHost(t)
	end := runReview(t.Context(), host, ReadBrief("o/r#7", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusFail || !strings.Contains(end.Deliverable, "No assembled review") {
		t.Fatalf("failure changed into a review: %+v", end)
	}
	path := filepath.Join(host.records, "review-failure.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record failureRecord
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	if record.Kind != "review-failure" || record.PullRequest != "https://github.com/o/r/pull/7" || record.Failure != "context canceled" || record.Sessions != 1 || record.FailedSession != 1 || record.Elapsed <= 0 {
		t.Fatalf("lost failure evidence: %+v", record)
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatal("failure record is not owner-readable only")
	}
	if _, _, err := readSaved(path); err == nil {
		t.Fatal("a failure record could be posted as a review")
	}
	if _, err := os.Stat(filepath.Join(host.records, reportJSON)); !os.IsNotExist(err) {
		t.Fatal("a failed pipeline wrote a completed-review payload")
	}
}

func TestUnwritableFailureRecordDoesNotChangeTheFailedEnding(t *testing.T) {
	r, _ := testReviewer(&fakeSessions{}, func(context.Context, orch.Deps) (schemas.ReviewResult, error) {
		return schemas.ReviewResult{}, context.Canceled
	})
	host := newTestHost(t)
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	host.records = blocked
	var notes strings.Builder
	end := runReview(t.Context(), host, ReadBrief("o/r#7", nil), defaults(), &notes, r)
	if end.Status != delegate.StatusFail || end.Extra != nil || !strings.Contains(notes.String(), "could not save the failure record") {
		t.Fatalf("saving error hid the failure: %+v, %s", end, notes.String())
	}
}
