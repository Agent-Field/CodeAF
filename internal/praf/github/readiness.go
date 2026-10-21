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
	"time"
)

type RequiredCheck struct {
	Context string
	AppID   int
}

// ReadinessMaterial reads the complete diff and the enforced required-check
// names. Unreadable policy and truncated material never mean no requirements.
func (c *client) ReadinessMaterial(ctx context.Context, owner, repo, base string, number int) (string, []RequiredCheck, error) {
	headers, err := c.headersForRepo(ctx, owner, repo)
	if err != nil {
		return "", nil, err
	}
	headers["Accept"] = "application/vnd.github.v3.diff"
	raw, err := c.request(ctx, http.MethodGet, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(number)), headers, nil, 30*time.Second)
	if err != nil {
		return "", nil, err
	}
	if len(raw) == 0 || len(raw) > 192<<10 {
		return "", nil, fmt.Errorf("complete readiness diff unavailable or exceeds 192 KiB")
	}
	var required []RequiredCheck
	b, _, err := c.read(ctx, c.repoURL(owner, repo, "/branches/"+url.PathEscape(base)+"/protection/required_status_checks"), "")
	if err != nil {
		var ae *APIError
		if !errors.As(err, &ae) || ae.StatusCode != 404 {
			return "", nil, err
		}
	} else {
		var v struct {
			Contexts []string `json:"contexts"`
			Checks   []struct {
				Context string `json:"context"`
				AppID   int    `json:"app_id"`
			} `json:"checks"`
		}
		if err = json.Unmarshal(b, &v); err != nil {
			return "", nil, err
		}
		for _, name := range v.Contexts {
			required = append(required, RequiredCheck{Context: name})
		}
		for _, check := range v.Checks {
			required = append(required, RequiredCheck{Context: check.Context, AppID: check.AppID})
		}
	}
	// This endpoint covers enforced repository/organization rulesets, which
	// do not necessarily appear in classic branch protection.
	for page := 1; page <= 20; page++ {
		b, _, err = c.read(ctx, c.repoURL(owner, repo, "/rules/branches/"+url.PathEscape(base)+"?per_page=100&page="+strconv.Itoa(page)), "")
		if err != nil {
			return "", nil, err
		}
		var rules []struct {
			Type       string `json:"type"`
			Parameters struct {
				Checks []struct {
					Context string `json:"context"`
					AppID   int    `json:"integration_id"`
				} `json:"required_status_checks"`
			} `json:"parameters"`
		}
		if err = json.Unmarshal(b, &rules); err != nil {
			return "", nil, err
		}
		for _, rule := range rules {
			if rule.Type == "required_status_checks" {
				for _, check := range rule.Parameters.Checks {
					required = append(required, RequiredCheck{Context: check.Context, AppID: check.AppID})
				}
			}
		}
		if len(rules) < 100 {
			sort.Slice(required, func(i, j int) bool {
				if required[i].Context != required[j].Context {
					return required[i].Context < required[j].Context
				}
				return required[i].AppID < required[j].AppID
			})
			return string(raw), required, nil
		}
	}
	return "", nil, fmt.Errorf("required check rules exceed 2000; full policy could not be read")
}

// MarkPullReady is a single mutation, never an automatically replayed POST.
// The caller rechecks the complete readiness snapshot first and observes the
// result afterward. GitHub supplies no expected-head field for this mutation.
func (c *client) MarkPullReady(ctx context.Context, owner, repo string, n int, head string) error {
	raw, _, err := c.read(ctx, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(n)), "")
	if err != nil {
		return err
	}
	var p struct {
		NodeID string `json:"node_id"`
		Draft  *bool  `json:"draft"`
		State  string `json:"state"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err = json.Unmarshal(raw, &p); err != nil {
		return err
	}
	if p.State != "open" || p.Head.SHA != head || p.Draft == nil {
		return fmt.Errorf("PR closed, head moved or draft state is unknown before marking ready")
	}
	if !*p.Draft {
		return nil
	}
	if p.NodeID == "" {
		return fmt.Errorf("GitHub did not supply the PR node identity")
	}
	headers, err := c.headersForRepo(ctx, owner, repo)
	if err != nil {
		return err
	}
	payload := map[string]any{"query": "mutation($id:ID!){markPullRequestReadyForReview(input:{pullRequestId:$id}){pullRequest{isDraft headRefOid}}}", "variables": map[string]any{"id": p.NodeID}}
	raw, err = c.request(ctx, http.MethodPost, c.baseURL+"/graphql", headers, payload, 60*time.Second)
	if err != nil {
		return fmt.Errorf("ready-for-review outcome uncertain; re-read the PR before retrying: %w", err)
	}
	var v struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
		Data struct {
			Ready struct {
				PR *struct {
					Draft bool   `json:"isDraft"`
					Head  string `json:"headRefOid"`
				} `json:"pullRequest"`
			} `json:"markPullRequestReadyForReview"`
		} `json:"data"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if len(v.Errors) > 0 {
		return fmt.Errorf("GitHub refused ready for review: %s", v.Errors[0].Message)
	}
	if v.Data.Ready.PR == nil || v.Data.Ready.PR.Draft || v.Data.Ready.PR.Head != head {
		return fmt.Errorf("ready-for-review result did not confirm the reviewed head; re-read the PR")
	}
	return nil
}
