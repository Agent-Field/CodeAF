package github

import (
	"context"
	"errors"
	"strings"
	"testing"

	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// MARK READY WORKS ON A PULL REQUEST THE JOB HAS NOT READ YET. A job offers
// Mark ready as soon as its pull request is open, before the first read of
// GitHub has told it the pull request's head. The head it asks GitHub to mark
// is then the commit the job itself published. Someone else's commit on top
// is still refused: the yes was not to it.
func TestMarkReadyOnAPullRequestNotReadSinceItOpened(t *testing.T) {
	f, src, it := actsFixture(t)
	unread := *it.PullRequest
	unread.HeadSHA = ""
	it.PullRequest, it.HeadCommit = &unread, actsHead
	ready, err := src.MarkReady(context.Background(), it)
	if err != nil {
		t.Fatalf("mark ready before the first read: %v", err)
	}
	if ready.Draft || len(f.Writes()) != 1 || !strings.Contains(f.Writes()[0], "markPullRequestReadyForReview") {
		t.Fatalf("answered %+v, writes %v", ready, f.Writes())
	}
	f.mu.Lock()
	f.draft, f.head = true, strings.Repeat("2", 40)
	f.mu.Unlock()
	_, err = src.MarkReady(context.Background(), it)
	var refusal *forge.ReadyRefusal
	if !errors.As(err, &refusal) || len(f.Writes()) != 1 {
		t.Fatalf("a head the job did not publish: %v, writes %v", err, f.Writes())
	}
}
