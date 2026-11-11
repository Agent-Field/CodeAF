package github

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const PublicationMarker = "<!-- codeaf:publication -->"

// FeedbackItem has a stable identity independent of its diff position. Resolved
// is a thread fact, never inferred from an outdated or missing current line.
type FeedbackItem struct {
	ID       string
	Revision string
	Resolved bool
	External bool
}
type ReviewFeedback struct {
	Text  string
	Items []FeedbackItem
}
type feedbackComment struct {
	ID           int64  `json:"id"`
	Body         string `json:"body"`
	Path         string `json:"path"`
	Line         *int   `json:"line"`
	OriginalLine *int   `json:"original_line"`
	Updated      string `json:"updated_at"`
	URL          string `json:"html_url"`
	User         struct {
		Login string `json:"login"`
	} `json:"user"`
	inline bool
}

// PullFeedback is the legacy read door. It includes outdated inline findings;
// callers needing resolution and durable cursors use PullFeedbackState.
func (c *client) PullFeedback(ctx context.Context, owner, repo string, n int) (string, error) {
	rows, err := c.feedbackComments(ctx, owner, repo, n)
	if err != nil {
		return "", err
	}
	return renderFeedback(rows, nil).Text, nil
}
func (c *client) PullFeedbackState(ctx context.Context, owner, repo string, n int) (ReviewFeedback, error) {
	rows, err := c.feedbackComments(ctx, owner, repo, n)
	if err != nil {
		return ReviewFeedback{}, err
	}
	var resolved map[int64]bool
	for _, r := range rows {
		if r.inline {
			resolved, err = c.feedbackResolutions(ctx, owner, repo, n)
			break
		}
	}
	if err != nil {
		return renderFeedback(rows, nil), err
	}
	for _, r := range rows {
		if r.inline {
			if _, ok := resolved[r.ID]; !ok {
				return renderFeedback(rows, nil), fmt.Errorf("review thread membership incomplete; feedback will be read again")
			}
		}
	}
	return renderFeedback(rows, resolved), nil
}
func (c *client) feedbackComments(ctx context.Context, owner, repo string, n int) ([]feedbackComment, error) {
	var out []feedbackComment
	size := 0
	for _, kind := range []string{"pulls", "issues"} {
		complete := false
		for page := 1; page <= 20; page++ {
			raw, _, err := c.read(ctx, c.repoURL(owner, repo, "/"+kind+"/"+strconv.Itoa(n)+"/comments?per_page=100&page="+strconv.Itoa(page)), "")
			if err != nil {
				return nil, err
			}
			var rows []feedbackComment
			if err = json.Unmarshal(raw, &rows); err != nil {
				return nil, err
			}
			for _, r := range rows {
				r.inline = kind == "pulls"
				if !conversationFeedback(r.Body) {
					continue
				}
				size += len(r.Body)
				if size > 192<<10 {
					return nil, fmt.Errorf("review feedback exceeds 192 KiB; full feedback could not be read")
				}
				out = append(out, r)
			}
			if len(rows) < 100 {
				complete = true
				break
			}
		}
		if !complete {
			return nil, fmt.Errorf("PR comments exceed 2000; full feedback could not be read")
		}
	}
	return out, nil
}
func renderFeedback(rows []feedbackComment, resolved map[int64]bool) ReviewFeedback {
	var b strings.Builder
	var out ReviewFeedback
	for _, r := range rows {
		kind := "conversation"
		if r.inline {
			kind = "inline"
		}
		revision := fmt.Sprintf("%x", sha256.Sum256([]byte(r.Updated+"\n"+r.Body)))
		external := strings.EqualFold(r.User.Login, "CLAassistant") && strings.Contains(r.Body, "https://cla-assistant.io/")
		item := FeedbackItem{ID: kind + ":" + strconv.FormatInt(r.ID, 10), Revision: revision, Resolved: resolved[r.ID], External: external}
		out.Items = append(out.Items, item)
		if item.Resolved {
			continue
		}
		if r.inline {
			line := r.OriginalLine
			if line == nil {
				line = r.Line
			}
			location := r.Path
			if line != nil {
				location += ":" + strconv.Itoa(*line)
			}
			fmt.Fprintf(&b, "Review comment %d %s %s (%s): %s\n", r.ID, r.URL, location, r.Updated, r.Body)
		} else {
			fmt.Fprintf(&b, "PR conversation comment %d %s by @%s (updated %s): %s\n", r.ID, r.URL, r.User.Login, r.Updated, r.Body)
		}
	}
	out.Text = b.String()
	return out
}

// GraphQL supplies resolution, while REST supplies complete comment bodies.
// Refuse partial thread/comment pages rather than silently omitting a concern.
func (c *client) feedbackResolutions(ctx context.Context, owner, repo string, n int) (map[int64]bool, error) {
	headers, err := c.headersForRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}
	result := map[int64]bool{}
	cursor := ""
	const query = `query($owner:String!,$repo:String!,$number:Int!,$cursor:String){repository(owner:$owner,name:$repo){pullRequest(number:$number){reviewThreads(first:100,after:$cursor){nodes{id isResolved comments(first:100){nodes{databaseId} pageInfo{hasNextPage endCursor}}} pageInfo{hasNextPage endCursor}}}}}`
	for page := 0; page < 20; page++ {
		vars := map[string]any{"owner": owner, "repo": repo, "number": n, "cursor": nil}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		raw, e := c.request(ctx, http.MethodPost, c.baseURL+"/graphql", headers, map[string]any{"query": query, "variables": vars}, 30*time.Second)
		if e != nil {
			return nil, e
		}
		var reply struct {
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
			Data struct {
				Repository *struct {
					PullRequest *struct {
						Threads struct {
							Nodes []struct {
								ID       string `json:"id"`
								Resolved bool   `json:"isResolved"`
								Comments struct {
									Nodes []struct {
										ID int64 `json:"databaseId"`
									} `json:"nodes"`
									Page struct {
										More   bool   `json:"hasNextPage"`
										Cursor string `json:"endCursor"`
									} `json:"pageInfo"`
								} `json:"comments"`
							} `json:"nodes"`
							Page struct {
								More   bool   `json:"hasNextPage"`
								Cursor string `json:"endCursor"`
							} `json:"pageInfo"`
						} `json:"reviewThreads"`
					} `json:"pullRequest"`
				} `json:"repository"`
			} `json:"data"`
		}
		if e = json.Unmarshal(raw, &reply); e != nil {
			return nil, e
		}
		if len(reply.Errors) > 0 || reply.Data.Repository == nil || reply.Data.Repository.PullRequest == nil {
			return nil, fmt.Errorf("GitHub review thread resolution could not be read")
		}
		threads := reply.Data.Repository.PullRequest.Threads
		for _, t := range threads.Nodes {
			if t.Comments.Page.More {
				members, e := c.feedbackThreadMembers(ctx, headers, t.ID)
				if e != nil {
					return nil, e
				}
				for _, id := range members {
					result[id] = t.Resolved
				}
			}
			for _, r := range t.Comments.Nodes {
				result[r.ID] = t.Resolved
			}
		}
		if !threads.Page.More {
			return result, nil
		}
		if threads.Page.Cursor == "" || threads.Page.Cursor == cursor {
			return nil, fmt.Errorf("review thread pagination did not advance")
		}
		cursor = threads.Page.Cursor
	}
	return nil, fmt.Errorf("review threads exceed 2000; complete resolution membership required")
}
func conversationFeedback(body string) bool {
	b := strings.TrimSpace(body)
	if b == "" {
		return false
	}
	if strings.HasPrefix(b, PublicationMarker) || (strings.HasPrefix(b, "## Changes") && strings.HasSuffix(b, "by codeaf factory")) {
		return false
	}
	if b == "+1" {
		return false
	}
	switch strings.TrimFunc(strings.ToLower(b), func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) }) {
	case "", "thanks", "thank you", "lgtm", "looks good", "+1", "👍", "approved":
		return false
	}
	return true
}

// Long discussion threads are paged independently of the outer thread list.
func (c *client) feedbackThreadMembers(ctx context.Context, headers map[string]string, id string) ([]int64, error) {
	const query = `query($id:ID!,$cursor:String){node(id:$id){... on PullRequestReviewThread{comments(first:100,after:$cursor){nodes{databaseId} pageInfo{hasNextPage endCursor}}}}}`
	cursor := ""
	var ids []int64
	for page := 0; page < 20; page++ {
		vars := map[string]any{"id": id, "cursor": nil}
		if cursor != "" {
			vars["cursor"] = cursor
		}
		raw, err := c.request(ctx, http.MethodPost, c.baseURL+"/graphql", headers, map[string]any{"query": query, "variables": vars}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		var v struct {
			Errors []json.RawMessage `json:"errors"`
			Data   struct {
				Node *struct {
					Comments struct {
						Nodes []struct {
							ID int64 `json:"databaseId"`
						} `json:"nodes"`
						Page struct {
							More   bool   `json:"hasNextPage"`
							Cursor string `json:"endCursor"`
						} `json:"pageInfo"`
					} `json:"comments"`
				} `json:"node"`
			} `json:"data"`
		}
		if err = json.Unmarshal(raw, &v); err != nil {
			return nil, err
		}
		if len(v.Errors) > 0 || v.Data.Node == nil {
			return nil, fmt.Errorf("review comment membership could not be read")
		}
		for _, n := range v.Data.Node.Comments.Nodes {
			ids = append(ids, n.ID)
		}
		p := v.Data.Node.Comments.Page
		if !p.More {
			return ids, nil
		}
		if p.Cursor == "" || p.Cursor == cursor {
			return nil, fmt.Errorf("review comment pagination did not advance")
		}
		cursor = p.Cursor
	}
	return nil, fmt.Errorf("review thread exceeds 2000 comments; complete membership required")
}
