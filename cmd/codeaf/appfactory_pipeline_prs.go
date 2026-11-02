package main

// ── codeaf app: THE OPEN PULL REQUESTS THAT CHANGE A PIPELINE FILE ──────────
//
// The engine's half of appwire.FactoryPipelinePRLister. It makes ONE GET to ONE
// fixed route of the factory, /v1/repos/{owner}/{repo}/pipeline/prs, as the
// caller who holds the connection now, exactly as the edit-source read does
// (appfactory_pipeline_edit.go): no GitHub token of this engine's, no host
// checkout or `gh`, and nothing the factory said is used if the sign-in changed
// meanwhile. It writes nothing anywhere.
//
// THE ANSWER IS CHECKED, NOT TRUSTED: every pull request must name this
// repository's canonical URL, a valid branch and a 40-hex head, and a reply
// that does not is refused whole. Titles and authors are cleaned to one
// bounded line; the factory's own prose is never passed on.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryPipelinePRLister = (*appBackend)(nil)

const (
	appFactoryPipelinePRsSuffix = "/pipeline/prs"
	// appFactoryPipelinePRsTimeout covers the route's own sixty seconds.
	appFactoryPipelinePRsTimeout = 70 * time.Second
	// appFactoryPipelinePRsLimit bounds the answer: at most 100 pull requests
	// of a few hundred bytes each, with a wide margin.
	appFactoryPipelinePRsLimit = 1 << 20
)

var errPipelinePRsUnreadable = appwire.Errorf(appwire.CodeEngine, "the factory sent a pull request list this app cannot use")

func (b *appBackend) FactoryPipelinePRs(ctx context.Context, p appwire.FactoryPipelinePRsParams) (appwire.FactoryPipelinePRs, error) {
	h := b.factoryHub()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryPipelinePRs{}, err
	}
	if !h.callerIs(p.Factory, who.token) {
		return appwire.FactoryPipelinePRs{}, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + appFactoryPipelinePRsSuffix
	client := *h.client
	client.Timeout = appFactoryPipelinePRsTimeout
	r, err := h.linkDoWith(ctx, &client, who, http.MethodGet, path, nil, appFactoryPipelinePRsLimit)
	switch {
	case err == errLinkNotSent:
		return appwire.FactoryPipelinePRs{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	case err != nil:
		return appwire.FactoryPipelinePRs{}, appwire.Errorf(appwire.CodeUnreachable, "the factory did not answer the read; nothing was written")
	}
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryPipelinePRs{}, err
	}
	out, err := classifyPipelinePRs(r, p)
	if err == nil {
		h.touch(p.Factory)
	}
	return out, err
}

// classifyPipelinePRs believes a reply only in its documented shape.
func classifyPipelinePRs(r linkReply, p appwire.FactoryPipelinePRsParams) (appwire.FactoryPipelinePRs, error) {
	if r.bodyBad {
		return appwire.FactoryPipelinePRs{}, errPipelinePRsUnreadable
	}
	if r.status != http.StatusOK {
		return appwire.FactoryPipelinePRs{}, pipelinePRsFailure(r)
	}
	var got api.PipelinePRs
	if json.Unmarshal(r.body, &got) != nil || got.Repo != p.Repo || got.PRs == nil || got.CheckedAt.IsZero() {
		return appwire.FactoryPipelinePRs{}, errPipelinePRsUnreadable
	}
	out := appwire.FactoryPipelinePRs{Factory: p.Factory, Repo: p.Repo, PRs: make([]appwire.FactoryPipelineOpenPR, 0, len(got.PRs)),
		Complete: got.Complete, CheckedAt: got.CheckedAt.UTC()}
	for _, pr := range got.PRs {
		if pr.Number <= 0 || pr.URL != "https://github.com/"+p.Repo+"/pull/"+strconv.Itoa(pr.Number) ||
			!plainBranchName(pr.Branch) || !appwire.ValidPipelinePRCommit(pr.HeadSHA) {
			return appwire.FactoryPipelinePRs{}, errPipelinePRsUnreadable
		}
		out.PRs = append(out.PRs, appwire.FactoryPipelineOpenPR{Number: pr.Number, URL: pr.URL, Title: cleanLinkText(pr.Title, 300),
			Branch: pr.Branch, HeadSHA: pr.HeadSHA, Author: cleanLinkText(pr.Author, 64), Draft: pr.Draft, FactoryBranch: pr.FactoryBranch,
			CreatedAt: pr.CreatedAt.UTC(), UpdatedAt: pr.UpdatedAt.UTC()})
	}
	return out, nil
}

// pipelinePRsFailure words a refused read here: the factory's own words may
// echo a path or a token. A reply that is not the route's typed failure goes
// through the fixed-words mapper the other factory doors use.
func pipelinePRsFailure(r linkReply) error {
	var f api.PipelinePRsFailure
	if json.Unmarshal(r.body, &f) != nil || !pipelinePRRequestID.MatchString(f.RequestID) || f.Stage == "" || r.status < 400 || r.status >= 600 {
		return linkRefusal(r, "that repository is not one this factory can see")
	}
	switch {
	case r.status == http.StatusUnauthorized || f.Stage == api.PipelinePRsStageCredential:
		return appwire.Errorf(appwire.CodeGitHubExpired, "GitHub access for this sign-in is missing or expired; reconnect GitHub.")
	case f.Stage == api.PipelinePRsStageRequest:
		return appwire.Errorf(appwire.CodeBadParams, "the factory would not take that request as it is.")
	case r.status == http.StatusNotFound:
		return appwire.Errorf(appwire.CodeNotFound, "your GitHub access cannot see that repository.")
	case r.status == http.StatusForbidden:
		return appwire.Errorf(appwire.CodeForbidden, "your GitHub access cannot read that repository's pull requests.")
	}
	return appwire.Errorf(appwire.CodeEngine, "GitHub did not answer the read; nothing was written, so you can try again.")
}
