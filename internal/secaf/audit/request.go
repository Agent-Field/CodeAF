package audit

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/secaf/afx"
	"github.com/Agent-Field/codeaf/internal/secaf/reasoners"
	"github.com/Agent-Field/codeaf/internal/secaf/schemas"
)

// AuditRequest transcribes the `audit(...)` signature (app.py:124-144) — all
// twenty parameters, with their exact names, types and defaults.
//
// Nil pointers stand for Python's `None`; nil slices stand for the four
// `list[str] | None = None` parameters, whose `or` fallbacks live in
// ToAuditInput.
//
// RepoURL keeps its wire name, but inside codeaf it must name a local
// directory: ResolveRepo refuses a URL rather than cloning it.
type AuditRequest struct {
	RepoURL              string   `json:"repo_url"`
	Depth                string   `json:"depth"`
	Branch               string   `json:"branch"`
	CommitSha            *string  `json:"commit_sha"`
	BaseCommitSha        *string  `json:"base_commit_sha"`
	SeverityThreshold    string   `json:"severity_threshold"`
	ScanTypes            []string `json:"scan_types"`
	OutputFormats        []string `json:"output_formats"`
	ComplianceFrameworks []string `json:"compliance_frameworks"`
	MaxCostUsd           *float64 `json:"max_cost_usd"`
	MaxProvers           *int     `json:"max_provers"`
	MaxDurationSeconds   *int     `json:"max_duration_seconds"`
	IncludePaths         []string `json:"include_paths"`
	ExcludePaths         []string `json:"exclude_paths"`
	IsPr                 bool     `json:"is_pr"`
	PrID                 *string  `json:"pr_id"`
	PostPrComments       bool     `json:"post_pr_comments"`
	FailOnFindings       bool     `json:"fail_on_findings"`
	// EnableDast is accepted and then DISCARDED — see ToAuditInput.
	EnableDast           bool    `json:"enable_dast"`
	ResumeFromCheckpoint *string `json:"resume_from_checkpoint"`
	// CheckpointDir is where the phases' checkpoints are written, in place of
	// the `.sec-af` folder inside the repository. It is codeaf's, never on the
	// wire: an audit run by codeaf promises the person their folder back as it
	// found it, so its checkpoints go to the run's record folder. Empty keeps
	// sec-af's own place.
	CheckpointDir string `json:"-"`
}

// NewAuditRequest returns the three non-empty keyword defaults of audit():
// depth="standard", branch="main", severity_threshold="low". Every other
// parameter's default is None / [] / False, which is the Go zero value.
func NewAuditRequest() AuditRequest {
	return AuditRequest{
		Depth:             "standard",
		Branch:            "main",
		SeverityThreshold: "low",
	}
}

// UnmarshalJSON seeds audit()'s keyword defaults before decoding.
func (a *AuditRequest) UnmarshalJSON(b []byte) error {
	*a = NewAuditRequest()
	type alias AuditRequest
	return json.Unmarshal(b, (*alias)(a))
}

// ToAuditInput ports the `AuditInput(...)` construction at app.py:146.
//
// Python parity, three points:
//
//   - the four list fallbacks are `x or [default]`, i.e. PYTHON TRUTHINESS, not
//     `is None`. An EXPLICIT EMPTY LIST therefore also falls back to the
//     default: `scan_types=[]` yields ["sast","sca","secrets","config"], and
//     `exclude_paths=[]` yields the four-entry default. There is no way to ask
//     for "no exclusions" through this entry. Reproduced.
//   - `include_paths` has NO fallback: it is forwarded as-is, so None stays
//     None (scan everything) and [] stays [] (an empty include filter).
//   - `enable_dast=enable_dast` is passed to a model that has NO `enable_dast`
//     FIELD — AuditInput declares `dast_enabled`. pydantic's default
//     `extra="ignore"` DROPS it silently, so `dast_enabled` is False for every
//     request no matter what the caller sends. This is a live Python bug — the
//     `enable_dast` parameter is inert — and it is reproduced rather than
//     fixed: DastEnabled is left at the pydantic default.
//
// The five fields audit() does not pass at all (dast_enabled, repo_urls,
// monitoring_mode, baseline_path, custom_policies) keep their pydantic
// defaults, which is what starting from schemas.NewAuditInput() gives.
func (a AuditRequest) ToAuditInput() schemas.AuditInput {
	in := schemas.NewAuditInput()
	in.RepoURL = a.RepoURL
	in.Depth = a.Depth
	in.Branch = a.Branch
	in.CommitSha = a.CommitSha
	in.BaseCommitSha = a.BaseCommitSha
	in.SeverityThreshold = a.SeverityThreshold
	in.ScanTypes = orDefault(a.ScanTypes, []string{"sast", "sca", "secrets", "config"})
	in.OutputFormats = orDefault(a.OutputFormats, []string{"json"})
	in.ComplianceFrameworks = orDefault(a.ComplianceFrameworks, []string{})
	in.MaxCostUsd = a.MaxCostUsd
	in.MaxProvers = a.MaxProvers
	in.MaxDurationSeconds = a.MaxDurationSeconds
	in.IncludePaths = a.IncludePaths
	in.ExcludePaths = orDefault(a.ExcludePaths, []string{"tests/", "vendor/", "node_modules/", ".git/"})
	in.IsPr = a.IsPr
	in.PrID = a.PrID
	in.PostPrComments = a.PostPrComments
	in.FailOnFindings = a.FailOnFindings
	// in.DastEnabled deliberately untouched — see the doc comment.
	return in
}

// BindRequest turns an untyped keyword map into an AuditRequest the way the
// node's `audit` reasoner did, in Python's order:
//
//	validated = self._validate_handler_input(body, input_fields)   # SDK
//	audit_input = AuditInput(**validated)                          # pydantic
//
// The SDK-level validation is what turns `{"is_pr": "yes"}` into true and
// `{"repo_url": 5}` into "5" instead of a bind error, and what rejects an
// explicit null on a required parameter. Its refusal is an *Error with 422,
// the status Python's endpoint answered with; a failure of the typed bind that
// follows is an *Error with 400.
func BindRequest(input map[string]any) (AuditRequest, error) {
	validated, err := reasoners.ValidateHandlerInput(reasoners.NameAudit, input)
	if err != nil {
		var handlerInput *reasoners.HandlerInputError
		if errors.As(err, &handlerInput) {
			return AuditRequest{}, &Error{StatusCode: http.StatusUnprocessableEntity, Message: err.Error(), Err: err}
		}
		return AuditRequest{}, err
	}
	req, err := afx.Bind[AuditRequest](validated)
	if err != nil {
		// The pydantic half — `AuditInput(**validated)` at app.py:146 — was
		// raised outside audit()'s try and reached FastAPI as a generic 500;
		// the Go node answered 400, the bad-input status, because a 500 tells
		// the caller nothing about a malformed body. Kept.
		return AuditRequest{}, &Error{StatusCode: http.StatusBadRequest, Message: err.Error(), Err: err}
	}
	return req, nil
}

// orDefault is Python's `value or default` for a list: an empty (or nil) slice
// is falsy and yields the default.
func orDefault(value, def []string) []string {
	if len(value) == 0 {
		return def
	}
	return value
}
