package api

import (
	"fmt"
	"time"
)

// Repository issue reads have the same bounds on both sides of the wire.
const (
	RepoIssuesPerPage = 50
	RepoIssuesPageMax = 10
	RepoIssueBodyMax  = 8192 // The limit counts bytes, preserving whole runes.
)

// RepoIssue is a GitHub issue, excluding pull requests. ID is GitHub's
// permanent numeric identity; Number is its number within the repository.
type RepoIssue struct {
	ID        int64     `json:"id"`
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	URL       string    `json:"url"`
	State     string    `json:"state" enum:"open,closed"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	BodyCut   bool      `json:"body_cut,omitempty"`
}

// RepoIssues counts all GitHub rows, including omitted pull requests, so a
// caller can advance its cursor even when there are no issues in the answer.
type RepoIssues struct {
	Repo   string      `json:"repo"`
	Issues []RepoIssue `json:"issues"`
	Rows   int         `json:"rows"`
	Newest *time.Time  `json:"newest,omitempty"`
}

// RepoIssuesRetry is the retry detail of a rate-limited repository read.
type RepoIssuesRetry struct {
	RetryAt time.Time `json:"retry_at"`
}

// CheckRepoIssues validates the cursor without rewriting its timestamp.
func CheckRepoIssues(since string, n int) error {
	if n < 1 || n > RepoIssuesPageMax {
		return fmt.Errorf("page is from 1 to %d", RepoIssuesPageMax)
	}
	if since != "" {
		if _, err := time.Parse(time.RFC3339, since); err != nil {
			return fmt.Errorf("since must be an RFC 3339 timestamp")
		}
	}
	return nil
}
