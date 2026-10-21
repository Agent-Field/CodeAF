package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// PullFeedback includes inline review requests. Read all pages: a tail-only
// snapshot can silently miss an unresolved request at the top of a long review.
func (c *client) PullFeedback(ctx context.Context, owner, repo string, n int) (string, error) {
	var out strings.Builder
	for page := 1; page <= 20; page++ {
		raw, _, err := c.read(ctx, c.repoURL(owner, repo, "/pulls/"+strconv.Itoa(n)+"/comments?per_page=100&page="+strconv.Itoa(page)), "")
		if err != nil {
			return "", err
		}
		var rows []struct {
			ID           int64  `json:"id"`
			Body         string `json:"body"`
			Path         string `json:"path"`
			Line         *int   `json:"line"`
			OriginalLine *int   `json:"original_line"`
			Updated      string `json:"updated_at"`
			URL          string `json:"html_url"`
		}
		if err = json.Unmarshal(raw, &rows); err != nil {
			return "", err
		}
		for _, r := range rows {
			if r.Line == nil {
				continue
			}
			fmt.Fprintf(&out, "Review comment %d %s %s:%d (%s): %s\n", r.ID, r.URL, r.Path, *r.Line, r.Updated, r.Body)
		}
		if len(rows) < 100 {
			return out.String(), nil
		}
	}
	return "", fmt.Errorf("review comments exceed 2000; full feedback could not be read")
}
