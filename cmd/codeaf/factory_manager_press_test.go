package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// THE MANAGER'S MERGE AND MARK READY ARE THE JOB PAGE'S PRESSES: refused in
// words unless the page offers them now, sent as the page's own POST, and
// absent (said so, never pretended) where no factory connection is.
func TestTheManagersPRDecisionIsTheJobPagesPress(t *testing.T) {
	it := factory.Item{ID: 4, Kind: factory.KindIssue, Publish: "pr", HeadCommit: "abc", State: factory.StateShipped,
		PullRequest: &factory.PullRequest{Number: 161, State: factory.PROpen, Draft: true, HeadSHA: "abc", Mergeable: factory.MergeableYes, ChecksRead: true, ReviewsRead: true}}
	var verbs []string
	press := localJobPress(func(_ context.Context, job int, verb string, _ any) error {
		if job != 4 {
			t.Fatalf("pressed job %d", job)
		}
		verbs = append(verbs, verb)
		return nil
	})
	if _, err := pressPRDecision(context.Background(), &press, it, session.ActionMerge, nil); err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: merge is not offered: the pull request is a draft") {
		t.Fatalf("a draft was merged: %v", err)
	}
	// THE REPLY SAYS WHAT THE PAGE OFFERS NOW. The live walk's manager marked
	// #177 ready, then said merge was not offered while the page offered it:
	// it repeated the status it read while the pull request was a draft.
	reread := func() (factory.Item, error) {
		after := it
		pr := *it.PullRequest
		pr.Draft = false
		after.PullRequest = &pr
		return after, nil
	}
	line, err := pressPRDecision(context.Background(), &press, it, session.ActionMarkReady, reread)
	if err != nil || !strings.Contains(line, "#161 is marked ready") || !strings.Contains(line, "Now on the job page: Waiting on the person: Merge pull request #161?") || strings.Contains(line, "not offered") {
		t.Fatalf("mark ready = %q (%v)", line, err)
	}
	it.PullRequest.Draft = false
	if _, err := pressPRDecision(context.Background(), &press, it, session.ActionMarkReady, nil); err == nil {
		t.Fatal("a ready pull request was marked ready again")
	}
	if line, err := pressPRDecision(context.Background(), &press, it, session.ActionMerge, nil); err != nil || !strings.Contains(line, "Merge pressed for pull request #161") {
		t.Fatalf("merge = %q (%v)", line, err)
	}
	if strings.Join(verbs, ",") != "pr-ready,merge" {
		t.Fatalf("pressed %v", verbs)
	}
	if _, err := pressPRDecision(context.Background(), nil, it, session.ActionMerge, nil); !errors.Is(err, errNoPress) {
		t.Fatalf("no connection = %v", err)
	}
	refused := localJobPress(func(context.Context, int, string, any) error {
		return errors.New("Sign in to GitHub before merging this job")
	})
	if _, err := pressPRDecision(context.Background(), &refused, it, session.ActionMerge, nil); err == nil || !strings.HasPrefix(err.Error(), "Nothing changed: Sign in") {
		t.Fatalf("a refusal = %v", err)
	}
}

// THE MANAGER'S BRIEF SAYS WHERE JOB FACTS COME FROM. The audit's manager said
// a gate held an Auto run and counted steps its own way; the brief now sends
// it to factory_status for every job fact and forbids the Auto gate claim.
func TestTheManagersBriefAnswersJobFactsOnlyFromStatus(t *testing.T) {
	for _, want := range []string{"only from factory_status", "never claim a gate holds the run when it says Autonomy: Auto"} {
		if !strings.Contains(talkBlockSay, want) {
			t.Errorf("the brief lacks %q", want)
		}
	}
	if !strings.Contains(talkBlockShape, "unless factory_status says the job is in Auto") {
		t.Error("the brief still says nothing posts before an approve step in Auto")
	}
}
