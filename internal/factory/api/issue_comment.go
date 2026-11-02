package api

import "time"

// IssueCommentRequest asks the factory to post ONE comment on ONE issue of a
// repository it watches, as the signed-in caller, over GitHub's REST API
// (POST /v1/repos/{owner}/{repo}/issues/{number}/comment). Only the operator
// may ask. Body is sent exactly as given.
type IssueCommentRequest struct {
	Body string `json:"body"`
}

// IssueCommentBodyMax bounds a comment in BYTES of UTF-8. GitHub's own limit
// is 65536 characters, so any body inside this bound is inside GitHub's.
const IssueCommentBodyMax = 65536

// Issue comment failure outcomes. "refused" says nothing was posted; "unknown"
// says the factory cannot say whether the comment exists, so the person looks
// at the issue on GitHub before trying again.
const (
	IssueCommentRefused = "refused"
	IssueCommentUnknown = "unknown"
)

// IssueCommentStages are the stages a failure is named at, in order.
var IssueCommentStages = []string{"operator", "request", "repository", "credential", "issue", "comment"}

// IssueCommentResult is the comment GitHub confirmed: a positive id, the
// canonical https://github.com/<owner>/<repo>/issues/<number>#issuecomment-<id>
// URL, and when GitHub says it was made.
type IssueCommentResult struct {
	ID        int64     `json:"id"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}

// IssueCommentResponse is the factory's receipt for a posted comment (201).
type IssueCommentResponse struct {
	Factory Factory            `json:"factory"`
	Repo    string             `json:"repo"`
	Number  int                `json:"number"`
	Comment IssueCommentResult `json:"comment"`
}

// IssueCommentFailure is the route's non-2xx body. Shared authentication,
// visibility and JSON validation may instead return the ordinary API error.
type IssueCommentFailure struct {
	Factory      Factory `json:"factory"`
	Error        string  `json:"error"`
	Reason       string  `json:"reason,omitempty"`
	Outcome      string  `json:"outcome"`
	Stage        string  `json:"stage"`
	GitHubStatus int     `json:"github_status,omitempty"`
	RequestID    string  `json:"request_id"`
}
