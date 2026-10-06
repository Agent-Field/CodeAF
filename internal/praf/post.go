package praf

// Posting a review pr wrote: `/pr post <report>`, a run of its own that the
// conversation proposes only after the person says yes to posting
// (Program.FollowUp).
//
// IT POSTS WHAT WAS REVIEWED, AS IT STANDS. The saved report holds the GitHub
// review pr-af built — the summary, the event, the inline comments — and the
// commit it was built on, so the post lands on the lines that were read even
// when the pull request has moved since; GitHub shows such comments as
// outdated rather than placing them on code nobody reviewed.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf/orch"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// readSaved reads a saved review: the pr-report.json path, or the record
// folder that holds one.
func readSaved(path string) (Saved, string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Saved{}, "", errors.New("name the review to post: the pr-report.json path its account gave")
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, reportJSON)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Saved{}, path, fmt.Errorf("cannot read the review at %s: %w", path, err)
	}
	var saved Saved
	if err := json.Unmarshal(data, &saved); err != nil {
		return Saved{}, path, fmt.Errorf("%s is not a review pr wrote: %w", path, err)
	}
	switch pr := saved.PullRequest; {
	case saved.Program != Name:
		return Saved{}, path, fmt.Errorf("%s is not a review pr wrote", path)
	case pr.Owner == "" || pr.Repo == "" || pr.Number <= 0:
		return Saved{}, path, fmt.Errorf("the review at %s names no pull request", path)
	case pr.HeadSHA == "":
		return Saved{}, path, fmt.Errorf("the review at %s does not say which commit it reviewed", path)
	case strings.TrimSpace(saved.Review.Review.Body) == "" && len(saved.Review.Review.Comments) == 0:
		return Saved{}, path, fmt.Errorf("the review at %s has nothing to post", path)
	}
	return saved, path, nil
}

// eventWords is a GitHub review event as a person says it.
var eventWords = map[string]string{
	"REQUEST_CHANGES": "a request for changes",
	"APPROVE":         "an approval",
	"COMMENT":         "a comment",
}

// runPost posts a saved review.
func runPost(ctx context.Context, host delegate.Host, path string, newGitHub func(token string) gitHub) delegate.Ending {
	host.Stage(delegate.StageRecord{Stage: stagePost, Status: "running", Data: stageData(map[string]any{"doing": "reading the review"})})
	saved, path, err := readSaved(path)
	if err != nil {
		return delegate.Ending{Status: delegate.StatusFail, Message: "pr posted nothing: " + firstSentence(err.Error())}
	}
	t := saved.PullRequest.Target()
	token := githubToken(ctx)
	if token == "" {
		return delegate.Ending{Status: delegate.StatusFail,
			Message: "pr posted nothing: posting to " + t.String() + " needs a GitHub token; set GH_TOKEN or sign in with `gh auth login`"}
	}
	host.Stage(delegate.StageRecord{Stage: stagePost, Status: "running", Data: stageData(map[string]any{"doing": "posting the review to " + t.String()})})
	pr := schemas.GitHubPRData{Owner: t.Owner, Repo: t.Repo, Number: t.Number, HeadSHA: saved.PullRequest.HeadSHA}
	event, err := orch.PostReviewEvent(ctx, newGitHub(token), pr, saved.Review.Review)
	if err != nil {
		return delegate.Ending{Status: delegate.StatusFail, Reason: err.Error(),
			Message: "pr could not post the review to " + t.String() + ": " + firstSentence(err.Error())}
	}
	comments := len(saved.Review.Review.Comments)
	what := eventWords[event]
	if what == "" {
		what = "a review"
	}
	line := fmt.Sprintf("Posted the review to %s as %s with %d inline comment%s.", t, what, comments, plural(comments))
	if event != saved.Review.Review.Event {
		line += " GitHub does not take a request for changes on its author's own pull request, so it went as a comment."
	}
	host.Step(delegate.StepRecord{Tool: "post", Step: stagePost, Command: "posted the review to " + t.String(),
		Observation: fmt.Sprintf("%s · %d inline comment%s", what, comments, plural(comments))})
	return delegate.Ending{
		Status:      delegate.StatusPass,
		Message:     strings.TrimSuffix(line, "."),
		Deliverable: line + "\n" + saved.PullRequest.URL + "\nThe review posted is " + path + ".",
		Extra:       map[string]any{"pull_request": t.String(), "event": event, "comments": comments},
	}
}
