package appwire

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// ── OPENING A PULL REQUEST FOR A REPOSITORY'S PIPELINE FILE ─────────────────
//
// factory.pipeline_pr asks a factory to open a pull request that changes ONLY
// .codeaf/factory.yaml in one repository, as the person who holds the
// connection now, over the factory's own caller-scoped route
// POST /v1/repos/{owner}/{repo}/pipeline/open-pr. It is a TRANSPORT SEAM and
// nothing more: the params are the route's own fields, spelled as the route
// spells them, and the factory parses and re-encodes the file itself. This
// method does not edit a pipeline, build or diff YAML, read the file, or
// change what a pipeline may say.
//
// THE PARAMS ARE EXACTLY {factory, repo, text, title, body, branch,
// expected_base_sha}. There is no method that names a route, a verb, a token
// or a reviewer; an unknown field is bad_params and never believed to have
// been honoured. Only factory, repo and text are required.
//
// THE GITHUB CREDENTIAL IS THE CALLER'S. The engine sends the factory the
// credential this app holds for that connection and nothing else: it never
// supplies a GitHub token, never reads the host's `gh` or git identity and
// never asks the factory without a held sign-in.
//
// A WRITE IS SENT ONCE. It is never retried, replayed, split or tried by a
// second route when the first said no. The factory's flow is not a
// transaction: it can stop between creating a branch, committing the file and
// opening the pull request, and it never deletes a branch.
//
// THE ANSWER IS ONE OF FIVE KNOWN OUTCOMES, or [CodeUncertain]:
//
//   - opened, unchanged: a [FactoryPipelinePRDone] result.
//   - refused, not_opened, unknown: an error whose Detail is a
//     [FactoryPipelinePRFailure], carrying the factory's own outcome, stage,
//     github_status, branch, branch_left and request_id in fixed words.
//     "refused" says nothing was written; "not_opened" says no pull request
//     was opened (a branch may remain: see branch_left); "unknown" says the
//     engine cannot say, so the person looks at GitHub before anything else.
//   - uncertain, with no Detail: a dropped connection, a timeout, a redirect,
//     a status or body the engine does not recognise, any 4xx or 5xx that is
//     not the flow's own typed failure, or ANY answer that arrives for a
//     caller who is no longer the connection's. The pull request may or may
//     not exist and the answer claims neither. A bare 4xx is never believed to
//     mean "not opened".
//
// ALL OF THIS IS OPTIONAL AND DISTINCT FROM every other factory feature. An
// engine that is not a [FactoryPipelineOpener] answers the method as
// unknown_method and does not announce [FeatureFactoryPipelinePR].
//
// ADDING A COMMIT TO AN OPEN PULL REQUEST is the same method with pr, the
// number of a pull request that is open, from a branch of the same repository
// into its default branch, and optionally expected_head_sha, the head the draft
// started from. The file is committed onto that pull request's head branch and
// no new pull request is opened. branch, body and expected_base_sha shape a
// new pull request only and are refused with pr. The answer is "updated" (pr
// carries the new head_sha) or "unchanged" (pr carries the head that already
// holds these bytes), and a failure never leaves a branch: the head branch was
// there before the call. A head that is not the expected one, a closed pull
// request or one from another repository is refused at stage "head" with
// nothing written. Only an engine that is a [FactoryPipelinePRUpdater]
// announces [FeatureFactoryPipelinePRUpdate]; any other refuses pr and
// expected_head_sha as unknown fields (bad_params), as before.

// FeatureFactoryPipelinePR is announced in hello when the engine serves
// factory.pipeline_pr.
const FeatureFactoryPipelinePR = "factory.pipeline_pr"

// FeatureFactoryPipelinePRUpdate is announced in hello when factory.pipeline_pr
// takes pr, adding a commit to an open pull request.
const FeatureFactoryPipelinePRUpdate = "factory.pipeline_pr_update"

// The bounds are the route's own (internal/factory/serve/pipeline_pr.go), and a
// test in that package fails when one drifts. A request that exceeds one is
// refused here, before anything is sent.
const (
	PipelinePRTextMax    = 256 << 10
	PipelinePRTitleMax   = 200 // characters
	PipelinePRBodyMax    = 16 << 10
	PipelinePRBranchMax  = 200 // bytes
	PipelinePRRequestMax = 1 << 20

	// factoryPipelinePRBudget is the most one factory.pipeline_pr call may take:
	// the route's own sixty seconds and a margin, because a shorter wait would
	// turn a slow success into an uncertain answer.
	factoryPipelinePRBudget = 75 * time.Second
)

// The outcomes the factory's route may answer.
const (
	PipelinePROutcomeOpened    = "opened"
	PipelinePROutcomeUnchanged = "unchanged"
	PipelinePROutcomeUpdated   = "updated"
	PipelinePROutcomeRefused   = "refused"
	PipelinePROutcomeNotOpened = "not_opened"
	PipelinePROutcomeUnknown   = "unknown"
)

// PipelinePRStages are the stages the route names a failure at, in the order of
// its sequences. "head" is the open pull request an update reads; "base",
// "branch" and "pull_request" belong to opening one. A stage outside this list
// is not believed.
var PipelinePRStages = []string{"request", "credential", "repository", "permission", "base", "head", "file", "branch", "commit", "pull_request"}

var pipelinePRBaseSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// FactoryPipelinePRParams is the whole request, with the route's own names.
type FactoryPipelinePRParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	// Text is the whole pipeline file the pull request should hold. The factory
	// parses it and re-encodes it, so the bytes committed are the factory's.
	Text string `json:"text"`
	// Title and Body default, in the factory's words, when empty.
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
	// Branch is the NEW branch to create; empty lets the factory suggest one.
	Branch string `json:"branch,omitempty"`
	// ExpectedBaseSHA is the 40-hex commit the draft started from; when it is not
	// the default branch's tip, the factory writes nothing.
	ExpectedBaseSHA string `json:"expected_base_sha,omitempty"`
	// PR is the number of an OPEN pull request to add the commit to instead of
	// opening a new one (feature factory.pipeline_pr_update).
	PR int `json:"pr,omitempty"`
	// ExpectedHeadSHA is, with PR, the 40-hex commit the pull request's head was
	// when the draft started; when it has moved, the factory writes nothing.
	ExpectedHeadSHA string `json:"expected_head_sha,omitempty"`
}

// FactoryPipelinePR is the pull request GitHub confirmed.
type FactoryPipelinePR struct {
	Number  int    `json:"number"`
	URL     string `json:"url"`
	Branch  string `json:"branch"`
	Base    string `json:"base"`
	BaseSHA string `json:"base_sha"`
	HeadSHA string `json:"head_sha"`
}

// FactoryPipelinePRDone is the answer to a pull request that was opened, or
// that did not need to be. It repeats the connection and repository that were
// asked for. For "unchanged" without pr, PR is absent: the default branch
// already holds these bytes and nothing was written. With pr, PR is always the
// pull request asked for: "updated" with HeadSHA the new commit, "unchanged"
// with HeadSHA the head that already holds these bytes.
type FactoryPipelinePRDone struct {
	Factory string             `json:"factory"`
	Repo    string             `json:"repo"`
	Outcome string             `json:"outcome" enum:"opened,updated,unchanged"`
	PR      *FactoryPipelinePR `json:"pr,omitempty"`
	BaseSHA string             `json:"base_sha"`
}

// FactoryPipelinePRFailure is the Detail of an error that carries one of the
// route's own failure outcomes. Every field is the route's, checked; none is
// prose from the factory. BranchLeft is true when the call created, or may have
// created, Branch on GitHub: codeaf never deletes a remote branch.
type FactoryPipelinePRFailure struct {
	Factory      string `json:"factory"`
	Repo         string `json:"repo"`
	Outcome      string `json:"outcome" enum:"refused,not_opened,unknown"`
	Stage        string `json:"stage"`
	GitHubStatus int    `json:"github_status,omitempty"`
	Branch       string `json:"branch,omitempty"`
	BranchLeft   bool   `json:"branch_left"`
	RequestID    string `json:"request_id,omitempty"`
}

// FactoryPipelineOpener is the engine's half of factory.pipeline_pr. It makes at
// most one POST to the factory and never sends it twice. An outcome it cannot
// confirm is [CodeUncertain]; a failure the route typed is an *Error whose
// Detail is a [FactoryPipelinePRFailure].
type FactoryPipelineOpener interface {
	FactoryPipelinePR(ctx context.Context, p FactoryPipelinePRParams) (FactoryPipelinePRDone, error)
}

// FactoryPipelinePRUpdater marks a [FactoryPipelineOpener] whose
// FactoryPipelinePR also takes PR and ExpectedHeadSHA.
type FactoryPipelinePRUpdater interface {
	PipelinePRUpdates()
}

func withFactoryPipelinePRUpdateFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(FactoryPipelineOpener); ok && servesPipelinePRUpdate(e) {
		hello = withFeature(hello, FeatureFactoryPipelinePRUpdate)
	}
	return hello
}

func servesPipelinePRUpdate(e Engine) bool {
	_, factory := e.(Factory)
	_, updater := e.(FactoryPipelinePRUpdater)
	return factory && updater
}

func withFactoryPipelinePRFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPipelineOpener); ok {
		hello = withFeature(hello, FeatureFactoryPipelinePR)
	}
	return hello
}

// ValidPipelinePRBranch is the route's own rule for a new branch name.
func ValidPipelinePRBranch(name string) bool {
	if name == "" || len(name) > PipelinePRBranchMax || name == "@" || name == "HEAD" ||
		strings.HasPrefix(name, "-") || strings.HasSuffix(name, ".") ||
		strings.Contains(name, "..") || strings.Contains(name, "@{") || strings.ContainsAny(name, " ~^:?*[\\") {
		return false
	}
	for _, ch := range name {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// ValidPipelinePRCommit is a lower-case 40-hex commit id.
func ValidPipelinePRCommit(sha string) bool { return pipelinePRBaseSHA.MatchString(sha) }

// CheckPipelinePR bounds the request without asking anyone. It answers the
// params with the connection and repository trimmed and the expected commit
// folded to lower case, as the route reads it. Text is forwarded exactly; title,
// body and branch are checked against the route's own rules and forwarded
// exactly too, so the factory's reading of them is the one the person sees.
func CheckPipelinePR(p FactoryPipelinePRParams) (FactoryPipelinePRParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	owner, name, ok := strings.Cut(strings.TrimSpace(p.Repo), "/")
	if !ok || !repoOwnerPattern.MatchString(owner) || !repoNamePattern.MatchString(name) ||
		owner == "." || owner == ".." || name == "." || name == ".." {
		return p, Errorf(CodeBadParams, "repo must be owner/name")
	}
	p.Repo = owner + "/" + name
	switch {
	case strings.TrimSpace(p.Text) == "":
		return p, Errorf(CodeBadParams, "text is required")
	case len(p.Text) > PipelinePRTextMax:
		return p, Errorf(CodeBadParams, "text is at most %d bytes", PipelinePRTextMax)
	case !utf8.ValidString(p.Text):
		return p, Errorf(CodeBadParams, "text must be valid UTF-8")
	}
	if title := strings.TrimSpace(p.Title); utf8.RuneCountInString(title) > PipelinePRTitleMax || strings.ContainsAny(title, "\r\n\x00") {
		return p, Errorf(CodeBadParams, "title must be one line of at most %d characters", PipelinePRTitleMax)
	}
	if body := strings.TrimSpace(p.Body); len(body) > PipelinePRBodyMax || strings.ContainsRune(body, 0) {
		return p, Errorf(CodeBadParams, "body is at most %d bytes", PipelinePRBodyMax)
	}
	if p.Branch != "" && !ValidPipelinePRBranch(p.Branch) {
		return p, Errorf(CodeBadParams, "branch must be a valid new Git branch name of at most %d bytes", PipelinePRBranchMax)
	}
	if p.ExpectedBaseSHA != "" {
		p.ExpectedBaseSHA = strings.ToLower(strings.TrimSpace(p.ExpectedBaseSHA))
		if !ValidPipelinePRCommit(p.ExpectedBaseSHA) {
			return p, Errorf(CodeBadParams, "expected_base_sha must be a 40-character commit SHA")
		}
	}
	return checkPipelinePRUpdate(p)
}

// checkPipelinePRUpdate bounds pr and expected_head_sha, the route's own rules.
func checkPipelinePRUpdate(p FactoryPipelinePRParams) (FactoryPipelinePRParams, error) {
	if p.PR == 0 && p.ExpectedHeadSHA == "" {
		return p, nil
	}
	p.ExpectedHeadSHA = strings.ToLower(strings.TrimSpace(p.ExpectedHeadSHA))
	switch {
	case p.PR <= 0:
		return p, Errorf(CodeBadParams, "pr must be the number of an open pull request")
	case p.Branch != "" || p.Body != "" || p.ExpectedBaseSHA != "":
		return p, Errorf(CodeBadParams, "branch, body and expected_base_sha are for a new pull request; leave them out with pr")
	case p.ExpectedHeadSHA != "" && !ValidPipelinePRCommit(p.ExpectedHeadSHA):
		return p, Errorf(CodeBadParams, "expected_head_sha must be a 40-character commit SHA")
	}
	return p, nil
}

func init() {
	const name = "factory.pipeline_pr"
	methods[name] = pipelinePRMethod(name)
	factoryPipelinePRMethods[name] = methods[name]
}

// pipelinePRMethod reads params STRICTLY, checks them, then asks the engine once.
func pipelinePRMethod(name string) method {
	return shaped[FactoryPipelinePRParams, FactoryPipelinePRDone](func(e Engine, raw json.RawMessage) (*FactoryPipelinePRDone, error) {
		o, ok := e.(FactoryPipelineOpener)
		if _, isFactory := e.(Factory); !ok || !isFactory {
			return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
		}
		// One JSON value, not a value and something after it.
		if !json.Valid(raw) {
			return nil, Errorf(CodeBadParams, "%s: the params are not one JSON value", name)
		}
		var p FactoryPipelinePRParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		// An engine without the update word keeps the fields it never had.
		if (p.PR != 0 || p.ExpectedHeadSHA != "") && !servesPipelinePRUpdate(e) {
			return nil, Errorf(CodeBadParams, "%s: unknown field \"pr\"", name)
		}
		p, err := CheckPipelinePR(p)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), factoryPipelinePRBudget)
		defer cancel()
		out, err := o.FactoryPipelinePR(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// factoryPipelinePRMethods is the table of factory.pipeline_pr, for the tests
// that walk it.
var factoryPipelinePRMethods = map[string]method{}
