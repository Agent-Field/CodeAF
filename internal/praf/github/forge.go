package github

// forge.go is the part of the client the factory's GitHub source reads and
// writes through: open issues and pull requests of a watched repository, a
// person's standing on it, the checks on a pull request's head, the
// repositories a token can see, and the four outward moves (a comment, labels,
// a close, a pull request). Every method keeps the discipline the review
// pipeline's reads already keep ([client.FetchPR]): a transport failure, a 5xx,
// a 403 or a 429 is tried again after a growing pause, and any other 4xx is an
// answer, not an accident.
//
// A RATE LIMIT IS SLEPT, NOT GUESSED. When GitHub says how long to wait
// (Retry-After, or a spent X-RateLimit-Remaining with its reset), the pause is
// that long, capped so a poll never sleeps through a person's whole session.

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

// ErrNotModified is a conditional read whose answer has not changed since the
// ETag it was asked with. It is not a failure: the caller keeps what it had.
var ErrNotModified = errors.New("github: not modified")

// readAttempts and the pauses between them are FetchPR's: four tries, 2s, 4s,
// 6s apart.
const readAttempts = 4

// writeAttempts is PostReview's: three tries, and only a 5xx or a transport
// failure is tried again, so a refusal is never posted twice.
const writeAttempts = 3

// maxRateSleep is the longest a single pause for a rate limit may last. A
// limit that resets later than this is reported as the failure it is, and the
// factory's poll backs off on its own clock.
const maxRateSleep = 90 * time.Second

// listPages bounds a paginated list: a hundred per page, ten pages. A watched
// repository with more than a thousand open issues shows its thousand most
// recently changed.
const listPages = 10

// Issue is one issue as the factory reads it. Pull is true for the rows of
// the issues list that are pull requests, which the source skips there and
// reads from the pulls list instead.
type Issue struct {
	Number  int
	Title   string
	Body    string
	User    string
	Labels  []string
	Created time.Time
	Updated time.Time
	URL     string
	Pull    bool
	// Comments is how many comments the issue has, as the list says it; the
	// comments themselves are [client.IssueComments]'s.
	Comments int
}

// Pull is one open pull request. Additions, Deletions and Files are filled by
// [client.Pull] only: GitHub's list leaves them out.
type Pull struct {
	Number  int
	Title   string
	Body    string
	User    string
	Labels  []string
	Created time.Time
	Updated time.Time
	URL     string
	HeadSHA string
	// Base is the branch the pull request asks to merge into, Head the
	// branch it comes from, and HeadRepo the repository Head lives in
	// (`owner/name`), which differs from the base repository for a fork.
	Base      string
	Head      string
	HeadRepo  string
	Draft     bool
	Additions int
	Deletions int
	Files     int
	// Comments is how many issue comments the pull request has; like the line
	// counts, only [client.Pull] fills it, because the list leaves it out.
	Comments int
}

// RepoInfo is one repository a token can see, for the picker.
type RepoInfo struct {
	Full    string
	Name    string
	Owner   string
	Private bool
	Pushed  time.Time
	// Open is the API's open_issues_count, which counts open issues and open
	// pull requests together, and -1 when the API left it out.
	Open int
}

// CheckState is what the checks on one commit add up to.
type CheckState string

const (
	ChecksNone    CheckState = ""
	ChecksPassed  CheckState = "success"
	ChecksRunning CheckState = "pending"
	ChecksFailed  CheckState = "failure"
)

// Posted is what a write came back with: the thing's address and its id.
type Posted struct {
	URL    string
	ID     string
	Number int
}

type userJSON struct {
	Login string `json:"login"`
}

type labelJSON struct {
	Name string `json:"name"`
}

type issueJSON struct {
	Number      int         `json:"number"`
	Title       string      `json:"title"`
	Body        *string     `json:"body"`
	User        userJSON    `json:"user"`
	Labels      []labelJSON `json:"labels"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	HTMLURL     string      `json:"html_url"`
	PullRequest *struct{}   `json:"pull_request"`
	Comments    int         `json:"comments"`
}

type pullJSON struct {
	Number    int         `json:"number"`
	Title     string      `json:"title"`
	Body      *string     `json:"body"`
	User      userJSON    `json:"user"`
	Labels    []labelJSON `json:"labels"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	HTMLURL   string      `json:"html_url"`
	Draft     bool        `json:"draft"`
	Head      struct {
		SHA  string        `json:"sha"`
		Ref  string        `json:"ref"`
		Repo *pullRepoJSON `json:"repo"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Additions    int `json:"additions"`
	Deletions    int `json:"deletions"`
	ChangedFiles int `json:"changed_files"`
	Comments     int `json:"comments"`
}

func labelNames(in []labelJSON) []string {
	var out []string
	for _, l := range in {
		if l.Name != "" {
			out = append(out, l.Name)
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// pullRepoJSON is the repository a pull request's head lives in; nil when
// the fork was deleted.
type pullRepoJSON struct {
	FullName string `json:"full_name"`
}

func (p pullJSON) pull() Pull {
	headRepo := ""
	if p.Head.Repo != nil {
		headRepo = p.Head.Repo.FullName
	}
	return Pull{
		Base: p.Base.Ref, Head: p.Head.Ref, HeadRepo: headRepo,
		Number: p.Number, Title: p.Title, Body: deref(p.Body), User: p.User.Login,
		Labels: labelNames(p.Labels), Created: p.CreatedAt, Updated: p.UpdatedAt,
		URL: p.HTMLURL, HeadSHA: p.Head.SHA, Draft: p.Draft,
		Additions: p.Additions, Deletions: p.Deletions, Files: p.ChangedFiles,
		Comments: p.Comments,
	}
}

// retryAfter reads how long GitHub asked to be left alone: Retry-After in
// seconds, or, when the rate limit is spent, the time until its reset.
func retryAfter(h http.Header, now time.Time) time.Duration {
	if s := strings.TrimSpace(h.Get("Retry-After")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	if strings.TrimSpace(h.Get("X-RateLimit-Remaining")) == "0" {
		if n, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-RateLimit-Reset")), 10, 64); err == nil {
			if d := time.Unix(n, 0).Sub(now); d > 0 {
				return d
			}
		}
	}
	return 0
}

// readRetryable is FetchPR's rule with one narrowing: transport failures,
// 5xx and 429 are tried again, and a 403 only when it is a rate limit. A 403
// that is a plain refusal (a token that may not read a collaborator's
// standing) is an answer, and trying it four times would only spend twelve
// seconds learning it again.
func readRetryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		s := apiErr.StatusCode
		if s == 403 {
			return apiErr.RetryAfter > 0 || strings.Contains(strings.ToLower(apiErr.Body), "rate limit")
		}
		return s >= 500 || s == 429
	}
	var te *transportError
	return errors.As(err, &te)
}

// read is one GET under the read discipline, conditional when etag is set.
func (c *client) read(ctx context.Context, rawurl, etag string) ([]byte, string, error) {
	headers, err := c.headersForRepo(ctx, "", "")
	if err != nil {
		return nil, "", err
	}
	var lastErr error
	for attempt := 0; attempt < readAttempts; attempt++ {
		body, tag, err := c.requestTagged(ctx, http.MethodGet, rawurl, headers, nil, 30*time.Second, etag)
		if err == nil || errors.Is(err, ErrNotModified) {
			return body, tag, err
		}
		if !readRetryable(err) {
			return nil, "", err
		}
		lastErr = err
		if attempt == readAttempts-1 {
			break
		}
		if serr := c.sleep(ctx, c.pause(err, time.Duration(2*(attempt+1))*time.Second)); serr != nil {
			return nil, "", serr
		}
	}
	return nil, "", lastErr
}

// write is one POST/PATCH under the write discipline: only a 5xx or a
// transport failure is tried again.
func (c *client) write(ctx context.Context, method, rawurl string, payload any) ([]byte, error) {
	headers, err := c.headersForRepo(ctx, "", "")
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < writeAttempts; attempt++ {
		body, err := c.request(ctx, method, rawurl, headers, payload, 60*time.Second)
		if err == nil {
			return body, nil
		}
		if !isRetryablePostReviewError(err) {
			return nil, err
		}
		lastErr = err
		if attempt == writeAttempts-1 {
			break
		}
		if serr := c.sleep(ctx, time.Duration(1<<(attempt+1))*time.Second); serr != nil {
			return nil, serr
		}
	}
	return nil, lastErr
}

// pause is the wait before the next try: what GitHub asked for when it said,
// capped at [maxRateSleep], and the fallback otherwise.
func (c *client) pause(err error, fallback time.Duration) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return min(apiErr.RetryAfter, maxRateSleep)
	}
	return fallback
}

func (c *client) repoURL(owner, repo, rest string) string {
	return fmt.Sprintf("%s/repos/%s/%s%s", c.baseURL, url.PathEscape(owner), url.PathEscape(repo), rest)
}

// ListIssues lists the open issues of owner/repo changed at or after since
// (every open one when since is zero), most recently changed first, pull
// requests marked. etag is the first page's tag from last time; when GitHub
// says that page has not changed the answer is [ErrNotModified] and nothing
// else was read. The tag that comes back is for next time.
func (c *client) ListIssues(ctx context.Context, owner, repo string, since time.Time, etag string) ([]Issue, string, error) {
	q := url.Values{"state": {"open"}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {"100"}}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	var out []Issue
	tag := ""
	for page := 1; page <= listPages; page++ {
		q.Set("page", strconv.Itoa(page))
		ask := ""
		if page == 1 {
			ask = etag
		}
		body, got, err := c.read(ctx, c.repoURL(owner, repo, "/issues?"+q.Encode()), ask)
		if err != nil {
			return nil, etag, err
		}
		if page == 1 {
			tag = got
		}
		var rows []issueJSON
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, "", err
		}
		for _, r := range rows {
			out = append(out, Issue{
				Number: r.Number, Title: r.Title, Body: deref(r.Body), User: r.User.Login,
				Labels: labelNames(r.Labels), Created: r.CreatedAt, Updated: r.UpdatedAt,
				URL: r.HTMLURL, Pull: r.PullRequest != nil, Comments: r.Comments,
			})
		}
		if len(rows) < 100 {
			break
		}
	}
	return out, tag, nil
}

// ListPulls lists the open pull requests of owner/repo, most recently changed
// first, under the same ETag rule as [client.ListIssues]. GitHub's list has no
// since and no line counts; [client.Pull] reads those for the ones that moved.
func (c *client) ListPulls(ctx context.Context, owner, repo, etag string) ([]Pull, string, error) {
	q := url.Values{"state": {"open"}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {"100"}}
	var out []Pull
	tag := ""
	for page := 1; page <= listPages; page++ {
		q.Set("page", strconv.Itoa(page))
		ask := ""
		if page == 1 {
			ask = etag
		}
		body, got, err := c.read(ctx, c.repoURL(owner, repo, "/pulls?"+q.Encode()), ask)
		if err != nil {
			return nil, etag, err
		}
		if page == 1 {
			tag = got
		}
		var rows []pullJSON
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, "", err
		}
		for _, r := range rows {
			out = append(out, r.pull())
		}
		if len(rows) < 100 {
			break
		}
	}
	return out, tag, nil
}

// Pull reads one pull request whole, line counts included.
func (c *client) Pull(ctx context.Context, owner, repo string, number int) (Pull, error) {
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(number)), "")
	if err != nil {
		return Pull{}, err
	}
	var p pullJSON
	if err := json.Unmarshal(body, &p); err != nil {
		return Pull{}, err
	}
	return p.pull(), nil
}

// Permission is user's standing on owner/repo: admin, maintain, write,
// triage, read or none. A 404 (no such collaborator) or a 403 (a token that
// may not read the list) is "none" and no error, because it is an answer: the
// factory then trusts the author as a stranger, which is the safe direction.
func (c *client) Permission(ctx context.Context, owner, repo, user string) (string, error) {
	body, _, err := c.read(ctx, c.repoURL(owner, repo, "/collaborators/"+url.PathEscape(user)+"/permission"), "")
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && (apiErr.StatusCode == http.StatusNotFound || (apiErr.StatusCode == http.StatusForbidden && !readRetryable(err))) {
			return "none", nil
		}
		return "", err
	}
	var p struct {
		Permission string `json:"permission"`
		RoleName   string `json:"role_name"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", err
	}
	if p.RoleName == "maintain" {
		return "maintain", nil
	}
	return p.Permission, nil
}

// Me is the login the token belongs to, or "" with no token.
func (c *client) Me(ctx context.Context) (string, error) {
	if c.token == "" {
		return "", nil
	}
	body, _, err := c.read(ctx, c.baseURL+"/user", "")
	if err != nil {
		return "", err
	}
	var u userJSON
	if err := json.Unmarshal(body, &u); err != nil {
		return "", err
	}
	return u.Login, nil
}

// Checks is what the commit statuses and check runs on sha add up to: any
// failure is a failure, else anything unfinished is running, else anything
// that passed is passed, and a commit nothing checked is [ChecksNone].
func (c *client) Checks(ctx context.Context, owner, repo, sha string) (CheckState, error) {
	state, _, err := c.CheckRuns(ctx, owner, repo, sha)
	return state, err
}

// Repos is every repository the token can see as an owner, a collaborator or
// an organisation member, most recently pushed first.
func (c *client) Repos(ctx context.Context) ([]RepoInfo, error) {
	q := url.Values{"affiliation": {"owner,collaborator,organization_member"}, "per_page": {"100"}, "sort": {"pushed"}}
	var out []RepoInfo
	for page := 1; page <= listPages; page++ {
		q.Set("page", strconv.Itoa(page))
		body, _, err := c.read(ctx, c.baseURL+"/user/repos?"+q.Encode(), "")
		if err != nil {
			return nil, err
		}
		var rows []struct {
			FullName string    `json:"full_name"`
			Name     string    `json:"name"`
			Owner    userJSON  `json:"owner"`
			Private  bool      `json:"private"`
			PushedAt time.Time `json:"pushed_at"`
			Open     *int      `json:"open_issues_count"`
		}
		if err := json.Unmarshal(body, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			open := -1
			if r.Open != nil {
				open = *r.Open
			}
			out = append(out, RepoInfo{Full: r.FullName, Name: r.Name, Owner: r.Owner.Login, Private: r.Private, Pushed: r.PushedAt, Open: open})
		}
		if len(rows) < 100 {
			break
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Pushed.After(out[b].Pushed) })
	return out, nil
}

// Comment posts body as a comment on issue or pull request number.
func (c *client) Comment(ctx context.Context, owner, repo string, number int, body string) (Posted, error) {
	data, err := c.write(ctx, http.MethodPost, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(number)+"/comments"), map[string]any{"body": body})
	if err != nil {
		return Posted{}, err
	}
	var r struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Posted{}, err
	}
	return Posted{URL: r.HTMLURL, ID: strconv.FormatInt(r.ID, 10), Number: number}, nil
}

// AddLabels adds labels to issue or pull request number, keeping the ones it
// has.
func (c *client) AddLabels(ctx context.Context, owner, repo string, number int, labels []string) (Posted, error) {
	if _, err := c.write(ctx, http.MethodPost, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(number)+"/labels"), map[string]any{"labels": labels}); err != nil {
		return Posted{}, err
	}
	return Posted{URL: fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number), ID: strconv.Itoa(number), Number: number}, nil
}

// Close closes issue or pull request number.
func (c *client) Close(ctx context.Context, owner, repo string, number int) (Posted, error) {
	data, err := c.write(ctx, http.MethodPatch, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(number)), map[string]any{"state": "closed"})
	if err != nil {
		return Posted{}, err
	}
	var r struct {
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Posted{}, err
	}
	return Posted{URL: r.HTMLURL, ID: strconv.Itoa(r.Number), Number: r.Number}, nil
}

// DefaultBranch is the branch a new pull request into owner/repo targets.
func (c *client) DefaultBranch(ctx context.Context, owner, repo string) (string, error) {
	body, _, err := c.read(ctx, c.repoURL(owner, repo, ""), "")
	if err != nil {
		return "", err
	}
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

// CreatePullRequest opens a pull request from head into base, as a draft when
// draft is set.
func (c *client) CreatePullRequest(ctx context.Context, owner, repo, head, base, title, body string, draft bool) (Posted, error) {
	data, err := c.write(ctx, http.MethodPost, c.repoURL(owner, repo, "/pulls"), map[string]any{
		"head": head, "base": base, "title": title, "body": body, "draft": draft,
	})
	if err != nil {
		return Posted{}, err
	}
	var r struct {
		ID      int64  `json:"id"`
		Number  int    `json:"number"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Posted{}, err
	}
	return Posted{URL: r.HTMLURL, ID: strconv.Itoa(r.Number), Number: r.Number}, nil
}

// WithBaseURL points the client at another API root, which is how a test
// serves it from an httptest.Server; never the network.
func (c *client) WithBaseURL(base string) *client {
	c.baseURL = strings.TrimRight(base, "/")
	return c
}

// WithSleep replaces the pause between tries, so a test does not wait.
func (c *client) WithSleep(sleep func(context.Context, time.Duration) error) *client {
	c.sleepFn = sleep
	return c
}
