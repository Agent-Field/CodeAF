package github

// forge_detail.go is what the factory reads about ONE item beyond its row in
// a list: the issue by its number, its last few comments, a pull request's
// changed files, and the check runs on its head by name. Each is one or two
// GETs under the same read discipline as the lists ([client.read]).

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"time"
)

// Comment is one issue comment.
type Comment struct {
	User    string
	Body    string
	Created time.Time
}

// FileChange is one file a pull request changes.
type FileChange struct {
	Path      string
	Additions int
	Deletions int
}

// CheckRun is one check run on a commit: its name, its status (`queued`,
// `in_progress`, `completed`), its conclusion once completed, and its page.
type CheckRun struct {
	Name       string
	Status     string
	Conclusion string
	URL        string
}

// FilesMost is how many of a pull request's files are kept, the most changed
// first. A pull request touching more is still read in one page of a hundred.
const FilesMost = 20

// Issue reads one issue (or the issue side of a pull request) by its number.
func (c *client) Issue(ctx context.Context, owner, repo string, number int) (Issue, error) {
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(number)), "")
	if err != nil {
		return Issue{}, err
	}
	var r issueJSON
	if err := json.Unmarshal(body, &r); err != nil {
		return Issue{}, err
	}
	return Issue{
		Number: r.Number, Title: r.Title, Body: deref(r.Body), User: r.User.Login,
		Labels: labelNames(r.Labels), Created: r.CreatedAt, Updated: r.UpdatedAt,
		URL: r.HTMLURL, Pull: r.PullRequest != nil, Comments: r.Comments,
	}, nil
}

// IssueComments reads the last `most` comments of an issue or pull request,
// oldest first. count is how many the issue says it has, which is what finds
// the last page without reading the first: GitHub lists comments oldest
// first, so the newest are on the page count/100 rounds up to, and the page
// before it when that page holds fewer than most. A count of 0 reads nothing.
func (c *client) IssueComments(ctx context.Context, owner, repo string, number, count, most int) ([]Comment, error) {
	if count <= 0 || most <= 0 {
		return nil, nil
	}
	const per = 100
	last := (count + per - 1) / per
	page := func(n int) ([]Comment, error) {
		q := url.Values{"per_page": {strconv.Itoa(per)}, "page": {strconv.Itoa(n)}}
		body, _, err := c.read(ctx, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(number)+"/comments?"+q.Encode()), "")
		if err != nil {
			return nil, err
		}
		var rows []struct {
			User      userJSON  `json:"user"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"created_at"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		out := make([]Comment, 0, len(rows))
		for _, r := range rows {
			out = append(out, Comment{User: r.User.Login, Body: r.Body, Created: r.CreatedAt})
		}
		return out, nil
	}
	got, err := page(last)
	if err != nil {
		return nil, err
	}
	if len(got) < most && last > 1 {
		before, err := page(last - 1)
		if err != nil {
			return nil, err
		}
		got = append(before, got...)
	}
	if len(got) > most {
		got = got[len(got)-most:]
	}
	return got, nil
}

// PullFiles reads a pull request's changed files, the most changed first, at
// most [FilesMost].
func (c *client) PullFiles(ctx context.Context, owner, repo string, number int) ([]FileChange, error) {
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(number)+"/files?per_page=100"), "")
	if err != nil {
		return nil, err
	}
	var rows []fileResp
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, err
	}
	out := make([]FileChange, 0, len(rows))
	for _, r := range rows {
		out = append(out, FileChange{Path: r.Filename, Additions: r.Additions, Deletions: r.Deletions})
	}
	sort.SliceStable(out, func(a, b int) bool {
		return out[a].Additions+out[a].Deletions > out[b].Additions+out[b].Deletions
	})
	if len(out) > FilesMost {
		out = out[:FilesMost]
	}
	return out, nil
}

// CheckRuns is what the commit statuses and check runs on sha add up to, and
// the check runs themselves: any failure is a failure, else anything
// unfinished is running, else anything that passed is passed, and a commit
// nothing checked is [ChecksNone]. [client.Checks] is this without the runs.
func (c *client) CheckRuns(ctx context.Context, owner, repo, sha string) (CheckState, []CheckRun, error) {
	if sha == "" {
		return ChecksNone, nil, nil
	}
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/commits/"+url.PathEscape(sha)+"/status"), "")
	if err != nil {
		return ChecksNone, nil, err
	}
	var combined struct {
		State    string `json:"state"`
		Statuses []struct {
			State string `json:"state"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(body, &combined); err != nil {
		return ChecksNone, nil, err
	}
	failed, running, passed := false, false, false
	for _, s := range combined.Statuses {
		switch s.State {
		case "failure", "error":
			failed = true
		case "pending":
			running = true
		case "success":
			passed = true
		}
	}
	body, _, err = c.read(ctx, c.repoURL(owner, repo, "/commits/"+url.PathEscape(sha)+"/check-runs?per_page=100"), "")
	if err != nil {
		return ChecksNone, nil, err
	}
	var runs struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"check_runs"`
	}
	if err := json.Unmarshal(body, &runs); err != nil {
		return ChecksNone, nil, err
	}
	var out []CheckRun
	for _, r := range runs.CheckRuns {
		out = append(out, CheckRun{Name: r.Name, Status: r.Status, Conclusion: r.Conclusion, URL: r.HTMLURL})
		if r.Status != "completed" {
			running = true
			continue
		}
		switch r.Conclusion {
		case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
			failed = true
		case "success":
			passed = true
		}
	}
	switch {
	case failed:
		return ChecksFailed, out, nil
	case running:
		return ChecksRunning, out, nil
	case passed:
		return ChecksPassed, out, nil
	}
	return ChecksNone, out, nil
}
