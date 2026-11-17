package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Merged is what GitHub answered a merge with: the merge commit's SHA.
type Merged struct {
	SHA string
}

// ErrHeadMoved is GitHub refusing a merge because the pull request's head is
// no longer the commit the merge named (409).
var ErrHeadMoved = errors.New("the pull request's head moved")

// ErrNotMergeable is GitHub refusing a merge it cannot make: a conflict, a
// required check or review not yet met, or a method the repository does not
// allow (405).
var ErrNotMergeable = errors.New("the forge would not merge the pull request")

// MergePullRequest merges number with method, but ONLY IF ITS HEAD IS STILL
// sha: GitHub refuses with 409 when the branch has moved, so a commit nobody
// approved is never merged even if it arrives after every check here.
//
// A MERGE IS SENT ONCE. Unlike other writes it is never retried on its own: a
// merge whose answer was lost may have happened, and a second try would read
// as a refusal. The caller re-reads the pull request to learn which.
func (c *client) MergePullRequest(ctx context.Context, owner, repo string, number int, sha, method string) (Merged, error) {
	sha = strings.TrimSpace(sha)
	if number <= 0 || sha == "" {
		return Merged{}, errors.New("a merge needs a pull request number and the exact head to merge")
	}
	payload := map[string]any{"sha": sha}
	if method = strings.TrimSpace(method); method != "" {
		payload["merge_method"] = method
	}
	headers, err := c.headersForRepo(ctx, owner, repo)
	if err != nil {
		return Merged{}, err
	}
	data, err := c.request(ctx, http.MethodPut, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(number)+"/merge"), headers, payload, 60*time.Second)
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) {
			switch apiErr.StatusCode {
			case http.StatusConflict:
				return Merged{}, fmt.Errorf("%w: %s", ErrHeadMoved, githubMessage(apiErr.Body))
			case http.StatusMethodNotAllowed, http.StatusUnprocessableEntity:
				return Merged{}, fmt.Errorf("%w: %s", ErrNotMergeable, githubMessage(apiErr.Body))
			}
		}
		return Merged{}, err
	}
	var r struct {
		SHA    string `json:"sha"`
		Merged bool   `json:"merged"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Merged{}, err
	}
	if !r.Merged {
		return Merged{}, ErrNotMergeable
	}
	return Merged{SHA: r.SHA}, nil
}

// MergeMethods are the merge methods the repository allows, in the order
// merge, squash, rebase.
func (c *client) MergeMethods(ctx context.Context, owner, repo string) ([]string, error) {
	body, _, err := c.read(ctx, c.repoURL(owner, repo, ""), "")
	if err != nil {
		return nil, err
	}
	var r struct {
		Merge  *bool `json:"allow_merge_commit"`
		Squash *bool `json:"allow_squash_merge"`
		Rebase *bool `json:"allow_rebase_merge"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	var out []string
	// A token that may not read the settings leaves them out; GitHub's own
	// default for each is allowed, so a missing field reads as allowed.
	for _, m := range []struct {
		name string
		on   *bool
	}{{"merge", r.Merge}, {"squash", r.Squash}, {"rebase", r.Rebase}} {
		if m.on == nil || *m.on {
			out = append(out, m.name)
		}
	}
	return out, nil
}

// githubMessage is the message GitHub put in an error body, or the body.
func githubMessage(body string) string {
	var r struct {
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &r) == nil && strings.TrimSpace(r.Message) != "" {
		return strings.TrimSpace(r.Message)
	}
	return strings.TrimSpace(body)
}
