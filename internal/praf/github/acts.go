package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ── PUBLIC ACTS ON A PULL REQUEST ───────────────────────────────────────────
//
// These are the writes a factory job makes on its own pull request with its
// person's sign-in, beyond opening and merging it: an edit of the title, the
// body or the base, a reply in a review thread, a comment, resolving a
// thread, asking for a review again and running failed checks again.
//
// A WRITE THAT READS AS THE PERSON SPEAKING IS SENT ONCE. A reply or a comment
// whose answer was lost may have been posted, and a second try would say it
// twice under their name. Those two go through [client.request] directly, as
// a merge does; the caller reads the pull request before it tries again. An
// edit, a resolve and a review request say the same thing however often they
// are sent, so they keep the ordinary write discipline.

// PullEdit is what to change on a pull request. A nil field is left as it is.
type PullEdit struct {
	Title *string
	Body  *string
	Base  *string
}

// EditPullRequest changes only the fields edit names and answers the pull
// request as GitHub now has it. GitHub refuses a base that does not exist, or
// one the head already merged into, with 422.
func (c *client) EditPullRequest(ctx context.Context, owner, repo string, number int, edit PullEdit) (Pull, error) {
	payload := map[string]any{}
	if edit.Title != nil {
		payload["title"] = *edit.Title
	}
	if edit.Body != nil {
		payload["body"] = *edit.Body
	}
	if edit.Base != nil {
		payload["base"] = *edit.Base
	}
	if number <= 0 || len(payload) == 0 {
		return Pull{}, errors.New("an edit needs a pull request number and something to change")
	}
	data, err := c.write(ctx, http.MethodPatch, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(number)), payload)
	if err != nil {
		return Pull{}, err
	}
	var p pullJSON
	if err := json.Unmarshal(data, &p); err != nil {
		return Pull{}, err
	}
	return p.pull(), nil
}

// once is one POST that is never tried again by itself.
func (c *client) once(ctx context.Context, rawurl string, payload any) ([]byte, error) {
	headers, err := c.headersForRepo(ctx, "", "")
	if err != nil {
		return nil, err
	}
	return c.request(ctx, http.MethodPost, rawurl, headers, payload, 60*time.Second)
}

func posted(data []byte, number int) (Posted, error) {
	var r struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Posted{}, err
	}
	return Posted{URL: r.HTMLURL, ID: strconv.FormatInt(r.ID, 10), Number: number}, nil
}

// ReplyToReviewComment posts body as a reply in the review thread that begins
// with comment, the thread's first comment: GitHub takes no reply to a reply.
// It is sent once.
func (c *client) ReplyToReviewComment(ctx context.Context, owner, repo string, number int, comment int64, body string) (Posted, error) {
	if number <= 0 || comment <= 0 || strings.TrimSpace(body) == "" {
		return Posted{}, errors.New("a reply needs a pull request, the comment it answers and some words")
	}
	data, err := c.once(ctx, c.repoURL(owner, repo, fmt.Sprintf("/pulls/%d/comments/%d/replies", number, comment)), map[string]any{"body": body})
	if err != nil {
		return Posted{}, err
	}
	return posted(data, number)
}

// CommentOnce is [client.Comment] sent once: a comment in the person's name.
func (c *client) CommentOnce(ctx context.Context, owner, repo string, number int, body string) (Posted, error) {
	if number <= 0 || strings.TrimSpace(body) == "" {
		return Posted{}, errors.New("a comment needs an item with a number and some words")
	}
	data, err := c.once(ctx, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(number)+"/comments"), map[string]any{"body": body})
	if err != nil {
		return Posted{}, err
	}
	return posted(data, number)
}

// ThreadRefusal is GitHub's definite no to a change of a review thread.
// Permission is a sign-in that may not make it, Gone a thread that is no
// longer there.
type ThreadRefusal struct {
	Permission bool
	Gone       bool
	Message    string
}

func (e *ThreadRefusal) Error() string { return e.Message }

// ResolveReviewThread marks the review thread with GraphQL node id thread as
// resolved. Resolving one already resolved is a yes.
func (c *client) ResolveReviewThread(ctx context.Context, owner, repo, thread string) error {
	if strings.TrimSpace(thread) == "" {
		return errors.New("resolving a review thread needs its id")
	}
	headers, err := c.headersForRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	payload := map[string]any{
		"query":     "mutation($id:ID!){resolveReviewThread(input:{threadId:$id}){thread{id isResolved}}}",
		"variables": map[string]any{"id": thread},
	}
	raw, err := c.request(ctx, http.MethodPost, c.baseURL+"/graphql", headers, payload, 60*time.Second)
	if err != nil {
		return err
	}
	var v struct {
		Errors []struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"errors"`
		Data struct {
			Resolve struct {
				Thread *struct {
					Resolved bool `json:"isResolved"`
				} `json:"thread"`
			} `json:"resolveReviewThread"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if len(v.Errors) > 0 {
		refusal := &ThreadRefusal{Message: "GitHub refused to resolve the thread: " + v.Errors[0].Message}
		for _, failure := range v.Errors {
			refusal.Permission = refusal.Permission || failure.Type == "FORBIDDEN" || failure.Type == "UNAUTHORIZED"
			refusal.Gone = refusal.Gone || failure.Type == "NOT_FOUND"
		}
		return refusal
	}
	if v.Data.Resolve.Thread == nil || !v.Data.Resolve.Thread.Resolved {
		return errors.New("GitHub did not confirm the thread as resolved")
	}
	return nil
}

// RequestReviewers asks reviewers, by login, to review the pull request
// again. GitHub refuses a login that may not review it with 422.
func (c *client) RequestReviewers(ctx context.Context, owner, repo string, number int, reviewers []string) error {
	if number <= 0 || len(reviewers) == 0 {
		return errors.New("a review request needs a pull request and someone to ask")
	}
	_, err := c.write(ctx, http.MethodPost, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(number)+"/requested_reviewers"), map[string]any{"reviewers": reviewers})
	return err
}

// Rerun is what running a head's failed checks again came to: the checks
// asked to run again by name, and those that could not be, each with why.
type Rerun struct {
	Ran     []string
	Skipped []RerunSkipped
}

// RerunSkipped is one failed check GitHub's API would not run again.
type RerunSkipped struct {
	Name string
	Why  string
}

// failedConclusions are the ways a finished check reads as failed.
var failedConclusions = map[string]bool{"failure": true, "timed_out": true, "cancelled": true, "startup_failure": true}

// RerunFailedChecks asks GitHub to run again every failed check on sha: each
// failed GitHub Actions workflow run has its failed jobs run again, and each
// other app's failed check suite is requested again. A CHECK THAT CANNOT BE
// RUN AGAIN IS SAID, NOT FAILED ON: a commit status belongs to whatever set
// it, and an app may refuse a sign-in that is not its own. An error means
// nothing could be learned (GitHub did not answer), so nothing was asked.
func (c *client) RerunFailedChecks(ctx context.Context, owner, repo, sha string) (Rerun, error) {
	var out Rerun
	if strings.TrimSpace(sha) == "" {
		return out, errors.New("running checks again needs the commit they ran on")
	}
	// The workflow runs on the head. A repository with Actions off, or a
	// sign-in that may not read them, answers 403 or 404: there are then no
	// workflow runs to run again, which the check runs below say by name.
	type workflowRun struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Suite      int64  `json:"check_suite_id"`
	}
	var runs []workflowRun
	actionsRead := true
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/actions/runs?per_page=100&head_sha="+url.QueryEscape(sha)), "")
	if err != nil {
		if !definiteRefusal(err) {
			return out, err
		}
		actionsRead = false
	} else {
		var list struct {
			Runs []workflowRun `json:"workflow_runs"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return out, err
		}
		runs = list.Runs
	}
	type checkRun struct {
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		App        struct {
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"app"`
		Suite struct {
			ID int64 `json:"id"`
		} `json:"check_suite"`
	}
	var checks []checkRun
	for page := 1; page <= checkPagesMost; page++ {
		body, _, err := c.read(ctx, c.repoURL(owner, repo, "/commits/"+url.PathEscape(sha)+"/check-runs?filter=latest&per_page=100&page="+strconv.Itoa(page)), "")
		if err != nil {
			return out, err
		}
		var list struct {
			Total  int        `json:"total_count"`
			Checks []checkRun `json:"check_runs"`
		}
		if err := json.Unmarshal(body, &list); err != nil {
			return out, err
		}
		checks = append(checks, list.Checks...)
		if len(list.Checks) < 100 || len(checks) >= list.Total {
			break
		}
	}
	var statuses struct {
		Statuses []struct {
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	body, _, err = c.read(ctx, c.repoURL(owner, repo, "/commits/"+url.PathEscape(sha)+"/status?per_page=100"), "")
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &statuses); err != nil {
		return out, err
	}

	actionSuites := map[int64]bool{}
	for _, r := range runs {
		actionSuites[r.Suite] = true
		if r.Status != "completed" || !failedConclusions[r.Conclusion] {
			continue
		}
		name := firstWords(r.Name, "workflow run "+strconv.FormatInt(r.ID, 10))
		if _, err := c.once(ctx, c.repoURL(owner, repo, "/actions/runs/"+strconv.FormatInt(r.ID, 10)+"/rerun-failed-jobs"), map[string]any{}); err != nil {
			out.Skipped = append(out.Skipped, RerunSkipped{Name: name, Why: rerunRefusal(err)})
			continue
		}
		out.Ran = append(out.Ran, name)
	}
	suites := map[int64][]string{}
	var order []int64
	for _, r := range checks {
		if r.Status != "completed" || !failedConclusions[r.Conclusion] {
			continue
		}
		if r.App.Slug == "github-actions" {
			// Its workflow run was run again above, or could not be read.
			if !actionsRead || !actionSuites[r.Suite.ID] {
				out.Skipped = append(out.Skipped, RerunSkipped{Name: r.Name, Why: "its workflow run could not be read with this GitHub sign-in"})
			}
			continue
		}
		if _, seen := suites[r.Suite.ID]; !seen {
			order = append(order, r.Suite.ID)
		}
		suites[r.Suite.ID] = append(suites[r.Suite.ID], r.Name)
	}
	for _, id := range order {
		names := strings.Join(suites[id], ", ")
		if id <= 0 {
			out.Skipped = append(out.Skipped, RerunSkipped{Name: names, Why: "GitHub named no check suite for it"})
			continue
		}
		if _, err := c.once(ctx, c.repoURL(owner, repo, "/check-suites/"+strconv.FormatInt(id, 10)+"/rerequest"), map[string]any{}); err != nil {
			out.Skipped = append(out.Skipped, RerunSkipped{Name: names, Why: rerunRefusal(err)})
			continue
		}
		out.Ran = append(out.Ran, names)
	}
	var contexts []string
	for _, s := range statuses.Statuses {
		if s.State == "failure" || s.State == "error" {
			contexts = append(contexts, s.Context)
		}
	}
	sort.Strings(contexts)
	for _, name := range contexts {
		out.Skipped = append(out.Skipped, RerunSkipped{Name: name, Why: "it is a commit status, which only the service that set it can run again"})
	}
	return out, nil
}

// definiteRefusal is GitHub answering a read with a no that another try will
// not change: not signed in, not allowed, not there.
func definiteRefusal(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.StatusCode {
	case http.StatusUnauthorized, http.StatusNotFound:
		return true
	case http.StatusForbidden:
		return apiErr.RetryAfter == 0 && !strings.Contains(strings.ToLower(apiErr.Body), "rate limit")
	}
	return false
}

// rerunRefusal says in a few words why one check was not run again.
func rerunRefusal(err error) string {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return "this GitHub sign-in may not run it again"
		}
		if msg := githubMessage(apiErr.Body); msg != "" && apiErr.StatusCode < 500 {
			return "GitHub refused: " + msg
		}
	}
	return "GitHub did not confirm it was run again"
}

func firstWords(words ...string) string {
	for _, w := range words {
		if w = strings.TrimSpace(w); w != "" {
			return w
		}
	}
	return ""
}
