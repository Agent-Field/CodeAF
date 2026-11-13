package serve

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// GitHub can return fifty full issue bodies before the public body cut.
const repoIssuesGitHubMax = 32 << 20

type githubRepoIssue struct {
	ID          int64           `json:"id"`
	Number      int             `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	URL         string          `json:"html_url"`
	State       string          `json:"state"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	PullRequest json.RawMessage `json:"pull_request"`
}

// repoIssues reads GitHub as this caller, after the usual repository check.
func (h *Handler) repoIssues(w http.ResponseWriter, r *http.Request) {
	query, err := repoIssuesQuery(r.URL.RawQuery)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	owner, name, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	token := h.repoIssuesToken(w, r)
	if token == "" {
		return
	}
	path := "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(name) + "/issues?" + query.Encode()
	rows, ok := h.githubRepoIssues(w, r, path, token)
	if ok {
		h.write(w, http.StatusOK, repoIssuesOf(owner+"/"+name, rows))
	}
}

func repoIssuesQuery(raw string) (url.Values, error) {
	query, err := url.ParseQuery(raw)
	if err != nil {
		return nil, fmt.Errorf("issues takes since and page, once each")
	}
	for key, values := range query {
		if (key != "since" && key != "page") || len(values) != 1 {
			return nil, fmt.Errorf("issues takes since and page, once each")
		}
	}
	n := 1
	if query.Has("page") {
		if n, err = strconv.Atoi(query.Get("page")); err != nil {
			return nil, fmt.Errorf("page is a whole number from 1 to %d", api.RepoIssuesPageMax)
		}
	}
	if err := api.CheckRepoIssues(query.Get("since"), n); err != nil {
		return nil, err
	}
	out := url.Values{"state": {"all"}, "sort": {"updated"}, "direction": {"asc"},
		"per_page": {strconv.Itoa(api.RepoIssuesPerPage)}, "page": {strconv.Itoa(n)}}
	if since := query.Get("since"); since != "" {
		out.Set("since", since)
	}
	return out, nil
}

// The credential rule matches searchIssues: a person's sign-in, or only a
// solo Factory's own profile. A team's host credential cannot stand in for it.
//
// A sign-in with no GitHub credential (a pasted access token, or a solo
// Factory whose host has no GitHub profile) is still a sign-in this Factory
// accepts. Its refusal names github_expired and carries no bearer challenge,
// so an app asks for GitHub again rather than forgetting the sign-in.
func (h *Handler) repoIssuesToken(w http.ResponseWriter, r *http.Request) string {
	session, _ := h.credential(r)
	token := session.githubToken
	if token == "" && h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		token = h.config.GitHubToken(r.Context())
	}
	if h.authFailed(w, r) {
		return ""
	}
	if token == "" {
		h.refuse(w, http.StatusUnauthorized, "Reconnect GitHub to read repository issues", api.ReasonGitHubExpired, "", nil, "")
	}
	return token
}

func (h *Handler) githubRepoIssues(w http.ResponseWriter, r *http.Request, path, token string) ([]githubRepoIssue, bool) {
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com"+path, nil)
	if h.failed(w, err) {
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		h.fail(w, http.StatusBadGateway, "GitHub isn't answering; retry this issue read")
		return nil, false
	}
	defer response.Body.Close()
	if !h.repoIssuesStatus(w, r, response, token) {
		return nil, false
	}
	var rows []githubRepoIssue
	raw, err := io.ReadAll(io.LimitReader(response.Body, repoIssuesGitHubMax+1))
	if err != nil || len(raw) > repoIssuesGitHubMax || json.Unmarshal(raw, &rows) != nil || len(rows) > api.RepoIssuesPerPage {
		h.fail(w, http.StatusBadGateway, "GitHub returned an invalid repository issues response")
		return nil, false
	}
	return rows, true
}

func (h *Handler) repoIssuesStatus(w http.ResponseWriter, r *http.Request, response *http.Response, token string) bool {
	status, header := response.StatusCode, response.Header
	switch {
	case status == http.StatusOK:
		return true
	case githubLimitSpent(status, header):
		h.write(w, http.StatusTooManyRequests, struct {
			Factory api.Factory `json:"factory"`
			Error   string      `json:"error"`
			api.RepoIssuesRetry
		}{h.factory, "GitHub's rate limit is spent; retry after the reset", api.RepoIssuesRetry{RetryAt: h.repoIssuesRetryAt(header)}})
	case status == http.StatusNotFound:
		h.fail(w, http.StatusNotFound, "repository not found")
	case status == http.StatusUnauthorized:
		h.endGitHub(r, token)
		h.authFailed(w, r)
	default:
		h.fail(w, http.StatusBadGateway, fmt.Sprintf("GitHub refused the repository issues read (HTTP %d)", status))
	}
	return false
}

// repoIssuesRetryAt is when GitHub says a spent limit ends. Retry-After names
// it for a secondary limit. X-RateLimit-Reset comes on every response but ends
// the primary window, so it counts only when that window is spent or GitHub
// does not say what remains. When both count, the later one wins: a request
// before either time would be refused again.
func (h *Handler) repoIssuesRetryAt(header http.Header) time.Time {
	var at time.Time
	if seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 32); err == nil && seconds >= 0 {
		at = h.clock().Add(time.Duration(seconds) * time.Second).UTC()
	} else if retry, err := http.ParseTime(header.Get("Retry-After")); err == nil {
		at = retry.UTC()
	}
	if remaining, err := strconv.ParseInt(header.Get("X-RateLimit-Remaining"), 10, 64); err != nil || remaining <= 0 {
		if reset, err := strconv.ParseInt(header.Get("X-RateLimit-Reset"), 10, 64); err == nil && reset > 0 {
			if primary := time.Unix(reset, 0).UTC(); primary.After(at) {
				at = primary
			}
		}
	}
	if at.IsZero() {
		// A spent limit without either clock hint still needs a usable retry time.
		return h.clock().Add(time.Minute).UTC()
	}
	return at
}

func repoIssuesOf(repo string, rows []githubRepoIssue) api.RepoIssues {
	out := api.RepoIssues{Repo: repo, Issues: []api.RepoIssue{}, Rows: len(rows)}
	for _, row := range rows {
		if out.Newest == nil || row.UpdatedAt.After(*out.Newest) {
			stamp := row.UpdatedAt
			out.Newest = &stamp
		}
		if len(row.PullRequest) != 0 && string(row.PullRequest) != "null" {
			continue
		}
		body, cut := repoIssueBody(row.Body)
		out.Issues = append(out.Issues, api.RepoIssue{ID: row.ID, Number: row.Number, Title: row.Title, Body: body, URL: row.URL,
			State: row.State, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt, BodyCut: cut})
	}
	return out
}

func repoIssueBody(body string) (string, bool) {
	if len(body) <= api.RepoIssueBodyMax {
		return body, false
	}
	end := api.RepoIssueBodyMax
	for !utf8.RuneStart(body[end]) {
		end--
	}
	return body[:end], true
}
