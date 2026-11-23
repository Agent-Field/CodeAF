package api

import "time"

// GET /v1/repos/{owner}/{repo}/pipeline/prs answers the repository's OPEN pull
// requests into its default branch that change the pipeline file
// (factory.PipelineFile), from any author, read with the caller's own GitHub
// credential. It writes nothing and takes no query.
//
// The read is bounded: one repository read, one page of at most 100 open pull
// requests into the default branch (most recently updated first), and the
// changed files (first page of 100) of at most 20 of them, the factory's own
// branches first. Complete is false when that bound may have hidden one.

// PipelinePRs is the answer. PRs is always a list, empty for none.
type PipelinePRs struct {
	Factory   Factory          `json:"factory"`
	Repo      string           `json:"repo"`
	PRs       []PipelineOpenPR `json:"prs"`
	Complete  bool             `json:"complete"`
	CheckedAt time.Time        `json:"checked_at"`
}

// PipelineOpenPR is one open pull request that changes the pipeline file.
// FactoryBranch is true when its head is a branch in the repository itself
// named the way the pull request route names the branches it suggests
// (PipelinePRBranchPrefix); a person may also have named theirs that way.
type PipelineOpenPR struct {
	Number        int       `json:"number"`
	URL           string    `json:"url"`
	Title         string    `json:"title"`
	Branch        string    `json:"branch"`
	HeadSHA       string    `json:"head_sha"`
	Author        string    `json:"author"`
	Draft         bool      `json:"draft"`
	FactoryBranch bool      `json:"factory_branch"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// PipelinePRBranchPrefix starts every branch name the pull request route
// suggests itself.
const PipelinePRBranchPrefix = "codeaf/pipeline-"

// Open pipeline pull requests failure stages, in the order of the read.
const (
	PipelinePRsStageCredential = "credential"
	PipelinePRsStageRequest    = "request"
	PipelinePRsStageRepository = "repository"
	PipelinePRsStagePulls      = "pulls"
	PipelinePRsStageFiles      = "files"
)

// PipelinePRsFailure is the route's non-2xx body. The read wrote nothing.
type PipelinePRsFailure struct {
	Factory      Factory `json:"factory"`
	Error        string  `json:"error"`
	Reason       string  `json:"reason,omitempty"`
	Stage        string  `json:"stage"`
	GitHubStatus int     `json:"github_status,omitempty"`
	RequestID    string  `json:"request_id"`
}
