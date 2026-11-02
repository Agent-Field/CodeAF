package main

// ── codeaf app: OPENING A PULL REQUEST FOR A PIPELINE FILE ──────────────────
//
// The engine's half of appwire.FactoryPipelineOpener. It makes ONE POST to ONE
// route of the factory's /v1 API, /v1/repos/{owner}/{repo}/pipeline/open-pr,
// spelled here as a constant: the path and the verb are never taken from the
// app, and the owner and name are validated by appwire and escaped here. It
// forwards the route's own fields and nothing else; the factory parses and
// re-encodes the file, so this file never reads, builds or diffs YAML.
//
// WHO MAY: whoever holds a credential for the connection right now, asked
// again through GET /v1/me on every call. It is NOT an operator action: the
// pull request is opened as the person, with the GitHub access the factory
// holds for that person's sign-in. A connection with no held credential is
// refused before a request leaves (signed_out, or unreachable for the local
// row), and nothing here supplies a GitHub token, reads the host's `gh` or git
// identity, or asks the factory with no bearer.
//
// A WRITE IS SENT ONCE. There is no retry and no second route. The route's
// flow is not a transaction (it can leave a branch behind and never deletes
// one), so the answer is believed only in its exact documented shapes:
//
//	201 opened     a positive number and the canonical URL of THIS repository
//	200 unchanged  the default branch already holds these bytes
//	typed failure  outcome refused, not_opened or unknown, a known stage, a
//	               request id, and a status and branch_left that agree with it
//
// With pr (adding a commit to that open pull request) the shapes are:
//
//	200 updated    that same pull request, its canonical URL and a NEW head
//	200 unchanged  that same pull request, at the expected head when one was named
//	typed failure  as above, but never a branch left and never a stage that
//	               only opening a pull request has
//
// Anything else is appwire.CodeUncertain with no repository, branch or number
// in it: a dropped connection, a timeout, a redirect, an unexpected 2xx, a 4xx
// or 5xx that is not the flow's own typed failure (a proxy, the shared
// authentication gate, the JSON gate), an unreadable or contradictory body, and
// EVERY answer that arrives for a caller who is no longer the connection's.
// The factory's prose is never passed on: it may echo a branch, a path or a
// token. Only the fields the route defines are carried, in words fixed here.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var (
	_ appwire.FactoryPipelineOpener    = (*appBackend)(nil)
	_ appwire.FactoryPipelinePRUpdater = (*appBackend)(nil)
)

// PipelinePRUpdates marks this backend as one whose FactoryPipelinePR adds a
// commit to an open pull request, which lets hello announce
// factory.pipeline_pr_update.
func (b *appBackend) PipelinePRUpdates() {}

const (
	appFactoryPipelinePRSuffix = "/pipeline/open-pr"
	// appFactoryPipelinePRTimeout covers the route's own sixty-second flow; the
	// registry's shorter client timeout would turn a slow success into an
	// uncertain answer.
	appFactoryPipelinePRTimeout = 70 * time.Second
)

var pipelinePRRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// claimPipelinePR marks one repository on one connection as having a pull
// request in flight in this engine. It is released when the call returns.
func (h *appFactoryHub) claimPipelinePR(id, repo string) (release func(), err error) {
	key := id + "\x00" + strings.ToLower(repo)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.pipelineBusy == nil {
		h.pipelineBusy = map[string]bool{}
	}
	if h.pipelineBusy[key] {
		return nil, appwire.Errorf(appwire.CodeBusy, "a pull request for that repository is already being opened; wait for it to finish")
	}
	h.pipelineBusy[key] = true
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.pipelineBusy, key)
	}, nil
}

func (b *appBackend) FactoryPipelinePR(ctx context.Context, p appwire.FactoryPipelinePRParams) (appwire.FactoryPipelinePRDone, error) {
	// The body is the route's own request type, so a field it does not know can
	// never be sent from here. Its encoded size is checked before anything leaves.
	body := api.PipelinePRRequest{Text: p.Text, Title: p.Title, Body: p.Body, Branch: p.Branch, ExpectedBaseSHA: p.ExpectedBaseSHA,
		PR: p.PR, ExpectedHeadSHA: p.ExpectedHeadSHA}
	raw, err := json.Marshal(body)
	if err != nil || len(raw) > appwire.PipelinePRRequestMax {
		return appwire.FactoryPipelinePRDone{}, appwire.Errorf(appwire.CodeBadParams, "that request is too large to send (at most %d bytes encoded)", appwire.PipelinePRRequestMax)
	}
	h := b.factoryHub()
	release, err := h.claimPipelinePR(p.Factory, p.Repo)
	if err != nil {
		return appwire.FactoryPipelinePRDone{}, err
	}
	defer release()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryPipelinePRDone{}, err
	}
	// The wait for who is over; the write is made only for the caller who asked.
	if !h.callerIs(p.Factory, who.token) {
		return appwire.FactoryPipelinePRDone{}, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	// TEXT THAT SPELLS A RUNTIME KEY goes only to a commit the factory attested,
	// and only after the file is read again (appfactory_pipeline_runtime.go).
	// An update names no base commit: the file is read again and must still be
	// the one attested, as the factory copy's seal does.
	if p.PR != 0 {
		err = b.checkPipelineCopyRuntimeText(ctx, who, p.Factory, p.Repo, p.Text)
	} else {
		err = b.checkPipelineRuntimeText(ctx, who, p)
	}
	if err != nil {
		return appwire.FactoryPipelinePRDone{}, err
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + appFactoryPipelinePRSuffix
	client := *h.client
	client.Timeout = appFactoryPipelinePRTimeout
	r, err := h.linkDoWith(ctx, &client, who, http.MethodPost, path, json.RawMessage(raw), appFactorySmall)
	switch {
	case err == errLinkNotSent:
		return appwire.FactoryPipelinePRDone{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was sent")
	case err != nil:
		return appwire.FactoryPipelinePRDone{}, pipelinePRUncertain(p)
	}
	// Whatever the factory said describes the caller it was asked as. If that
	// is no longer the connection's, none of it is used, not even a refusal.
	if !h.callerIs(p.Factory, who.token) {
		return appwire.FactoryPipelinePRDone{}, pipelinePRUncertain(p)
	}
	done, failure := classifyPipelinePR(r, p)
	if done == nil && failure == nil {
		return appwire.FactoryPipelinePRDone{}, pipelinePRUncertain(p)
	}
	h.touch(p.Factory)
	if failure != nil {
		return appwire.FactoryPipelinePRDone{}, pipelinePRFailureError(*failure, p.PR != 0)
	}
	return *done, nil
}

func pipelinePRUncertain(p appwire.FactoryPipelinePRParams) error {
	if p.PR != 0 {
		return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm whether the pull request was updated; it may have been, so check it on GitHub before trying again")
	}
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm whether a pull request was opened; it may have been, and a branch may remain, so check the repository on GitHub before trying again")
}

// classifyPipelinePR believes a reply only in its documented shapes. It answers
// neither value for anything else, which the caller reports as uncertain.
func classifyPipelinePR(r linkReply, p appwire.FactoryPipelinePRParams) (*appwire.FactoryPipelinePRDone, *appwire.FactoryPipelinePRFailure) {
	if r.bodyBad {
		return nil, nil
	}
	switch {
	case r.status == http.StatusCreated || r.status == http.StatusOK:
		return classifyPipelinePRSuccess(r, p), nil
	case r.status >= 400 && r.status < 600:
		return nil, classifyPipelinePRFailure(r, p)
	}
	// 202, 204, other 2xx, 3xx and anything else: not a documented answer.
	return nil, nil
}

func classifyPipelinePRSuccess(r linkReply, p appwire.FactoryPipelinePRParams) *appwire.FactoryPipelinePRDone {
	var got api.PipelinePRResponse
	if json.Unmarshal(r.body, &got) != nil {
		return nil
	}
	if !appwire.ValidPipelinePRCommit(got.BaseSHA) || (p.ExpectedBaseSHA != "" && got.BaseSHA != p.ExpectedBaseSHA) {
		return nil
	}
	out := appwire.FactoryPipelinePRDone{Factory: p.Factory, Repo: p.Repo, Outcome: got.Outcome, BaseSHA: got.BaseSHA}
	if p.PR != 0 {
		return classifyPipelinePRUpdate(r, p, got, out)
	}
	switch {
	case r.status == http.StatusOK && got.Outcome == api.PipelinePRUnchanged && got.PR == nil:
		return &out
	case r.status == http.StatusCreated && got.Outcome == api.PipelinePROpened && got.PR != nil:
		if !pipelinePRConfirmed(got, p.Repo, p.Branch) {
			return nil
		}
		out.PR = pipelinePRReceipt(got.PR)
		return &out
	}
	return nil
}

// classifyPipelinePRUpdate believes an answer to pr only when it is a 200 about
// that very pull request: "updated" at a head other than the expected one, or
// "unchanged" at the expected one.
func classifyPipelinePRUpdate(r linkReply, p appwire.FactoryPipelinePRParams, got api.PipelinePRResponse, out appwire.FactoryPipelinePRDone) *appwire.FactoryPipelinePRDone {
	if r.status != http.StatusOK || !pipelinePRConfirmed(got, p.Repo, "") || got.PR.Number != p.PR {
		return nil
	}
	moved := got.PR.HeadSHA != p.ExpectedHeadSHA
	switch {
	case got.Outcome == api.PipelinePRUpdated && (moved || p.ExpectedHeadSHA == ""):
	case got.Outcome == api.PipelinePRUnchanged && (!moved || p.ExpectedHeadSHA == ""):
	default:
		return nil
	}
	out.PR = pipelinePRReceipt(got.PR)
	return &out
}

// pipelinePRConfirmed is a reply's pull request in its documented shape: a
// positive number, the canonical URL of repo, a valid branch (the requested
// one, when one was), a plain base, and commits that agree.
func pipelinePRConfirmed(got api.PipelinePRResponse, repo, branch string) bool {
	pr := got.PR
	return pr != nil && pr.Number > 0 && pr.URL == "https://github.com/"+repo+"/pull/"+strconv.Itoa(pr.Number) &&
		appwire.ValidPipelinePRBranch(pr.Branch) && (branch == "" || pr.Branch == branch) && plainBranchName(pr.Base) &&
		pr.BaseSHA == got.BaseSHA && appwire.ValidPipelinePRCommit(pr.HeadSHA)
}

func pipelinePRReceipt(pr *api.PipelinePRResult) *appwire.FactoryPipelinePR {
	return &appwire.FactoryPipelinePR{Number: pr.Number, URL: pr.URL, Branch: pr.Branch, Base: pr.Base, BaseSHA: pr.BaseSHA, HeadSHA: pr.HeadSHA}
}

// plainBranchName is a non-empty one-line name of a bounded size.
func plainBranchName(s string) bool {
	if s == "" || len(s) > 255 {
		return false
	}
	for _, ch := range s {
		if ch < 0x20 || ch == 0x7f {
			return false
		}
	}
	return true
}

func classifyPipelinePRFailure(r linkReply, p appwire.FactoryPipelinePRParams) *appwire.FactoryPipelinePRFailure {
	var got api.PipelinePRFailure
	if json.Unmarshal(r.body, &got) != nil || !pipelinePRRequestID.MatchString(got.RequestID) || !knownPipelinePRStage(got.Stage) {
		return nil
	}
	if got.GitHubStatus != 0 && (got.GitHubStatus < 100 || got.GitHubStatus > 599) {
		return nil
	}
	left := got.BranchLeft
	if got.Branch != "" && (!appwire.ValidPipelinePRBranch(got.Branch) || (p.Branch != "" && got.Branch != p.Branch)) {
		return nil
	}
	if left && got.Branch == "" {
		return nil
	}
	if !pipelinePRStageFits(p, got.Stage, left) {
		return nil
	}
	switch got.Outcome {
	case api.PipelinePRRefused:
		// Nothing was written: a refusal that left a branch, or that names a
		// write stage, contradicts itself.
		if r.status >= 500 || left || got.Stage == "commit" || got.Stage == "pull_request" {
			return nil
		}
	case api.PipelinePRNotOpened:
		// When opening, a write stage cannot have been reached without a
		// branch to leave.
		if (got.Stage == "commit" || got.Stage == "pull_request") && !left && p.PR == 0 {
			return nil
		}
		if r.status != http.StatusBadGateway && r.status >= 500 {
			return nil
		}
	case api.PipelinePRUnknown:
		if r.status != http.StatusBadGateway || left == (p.PR != 0) {
			return nil
		}
		if got.Stage != "branch" && got.Stage != "commit" && got.Stage != "pull_request" {
			return nil
		}
	default:
		return nil
	}
	return &appwire.FactoryPipelinePRFailure{Factory: p.Factory, Repo: p.Repo, Outcome: got.Outcome, Stage: got.Stage,
		GitHubStatus: got.GitHubStatus, Branch: got.Branch, BranchLeft: left, RequestID: got.RequestID}
}

// pipelinePRStageFits is false for a failure that cannot belong to the request
// that was sent: an update never leaves a branch and never reaches a stage
// only opening has, and opening never reads an existing pull request.
func pipelinePRStageFits(p appwire.FactoryPipelinePRParams, stage string, left bool) bool {
	opening := stage == "base" || stage == "branch" || stage == "pull_request"
	if p.PR != 0 {
		return !left && !opening
	}
	return stage != "head"
}

func knownPipelinePRStage(stage string) bool {
	for _, s := range appwire.PipelinePRStages {
		if s == stage {
			return true
		}
	}
	return false
}

// pipelinePRFailureError is the wire error for a typed failure. The words are
// fixed here from the outcome and stage; the factory's own are never passed on.
// update says the call was adding a commit to an open pull request.
func pipelinePRFailureError(f appwire.FactoryPipelinePRFailure, update bool) error {
	left, verb := "", "opened"
	if f.BranchLeft {
		left = " A branch was left on GitHub; codeaf does not delete branches."
	}
	if update {
		verb = "updated"
	}
	var code, words string
	switch f.Outcome {
	case api.PipelinePRRefused:
		code, words = pipelinePRRefusedCode(f), pipelinePRRefusedWords(f.Stage)+" Nothing was written."
	case api.PipelinePRNotOpened:
		code, words = appwire.CodeEngine, "The pull request was not "+verb+"."+left
	default:
		code, words = appwire.CodeUncertain, "The factory cannot say whether the pull request was "+verb+"; check the repository on GitHub before trying again."+left
	}
	err := appwire.Errorf(code, "%s", words)
	err.Detail = f
	return err
}

// pipelinePRRefusedCode reads the code from the stage and GitHub's own status,
// both of which were validated. A refusal the stage does not explain is a plain
// engine refusal.
func pipelinePRRefusedCode(f appwire.FactoryPipelinePRFailure) string {
	switch f.Stage {
	case "credential":
		return appwire.CodeGitHubExpired
	case "permission":
		return appwire.CodeForbidden
	case "base", "head", "branch":
		return appwire.CodeConflict
	case "request":
		return appwire.CodeBadParams
	}
	switch f.GitHubStatus {
	case http.StatusUnauthorized:
		return appwire.CodeGitHubExpired
	case http.StatusForbidden:
		return appwire.CodeForbidden
	case http.StatusNotFound:
		return appwire.CodeNotFound
	}
	return appwire.CodeEngine
}

func pipelinePRRefusedWords(stage string) string {
	switch stage {
	case "credential":
		return "GitHub access for this sign-in is missing or expired; reconnect GitHub."
	case "permission":
		return "Your GitHub access cannot push to that repository."
	case "base":
		return "The default branch has moved since this draft started."
	case "head":
		return "That pull request is not open, has new commits since this draft started, or is not from a branch of this repository."
	case "branch":
		return "That branch name already exists or was refused; choose another."
	case "request":
		return "The factory would not take that request as it is."
	}
	return "GitHub refused a step before anything was written."
}
