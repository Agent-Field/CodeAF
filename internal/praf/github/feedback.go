package github

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// PublicationMarker identifies writes made by the factory, not external bot reviews.
const PublicationMarker = "<!-- codeaf:publication -->"

// PullFeedback includes inline reviews and top-level PR conversation comments. Read all pages: a tail-only
// snapshot can silently miss an unresolved request at the top of a long review.
func (c *client) PullFeedback(ctx context.Context, owner, repo string, n int) (string, error) {
	const feedbackBytesMost = 192 << 10
	var out strings.Builder
	inlineComplete := false
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
			if out.Len() > feedbackBytesMost {
				return "", fmt.Errorf("review feedback exceeds 192 KiB; full feedback could not be read")
			}
		}
		if len(rows) < 100 {
			inlineComplete = true
			break
		}
	}
	if !inlineComplete {
		return "", fmt.Errorf("review comments exceed 2000; full feedback could not be read")
	}
	for page := 1; page <= 20; page++ {
		raw, _, err := c.read(ctx, c.repoURL(owner, repo, "/issues/"+strconv.Itoa(n)+"/comments?per_page=100&page="+strconv.Itoa(page)), "")
		if err != nil {
			return "", err
		}
		var rows []struct {
			ID      int64  `json:"id"`
			Body    string `json:"body"`
			Updated string `json:"updated_at"`
			URL     string `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
		}
		if err = json.Unmarshal(raw, &rows); err != nil {
			return "", err
		}
		for _, row := range rows {
			if !conversationFeedback(row.Body) {
				continue
			}
			fmt.Fprintf(&out, "PR conversation comment %d %s by @%s (updated %s): %s\n", row.ID, row.URL, row.User.Login, row.Updated, row.Body)
			if out.Len() > feedbackBytesMost {
				return "", fmt.Errorf("review feedback exceeds 192 KiB; full feedback could not be read")
			}
		}
		if len(rows) < 100 {
			return out.String(), nil
		}
	}
	return "", fmt.Errorf("PR conversation comments exceed 2000; full feedback could not be read")
}

// Ignore only empty/known generated receipts and whole-message acknowledgements.
// Substantive human and external bot comments remain untrusted review evidence;
// keywords inside a longer review must never hide its findings.
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
