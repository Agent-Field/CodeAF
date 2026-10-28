package serve

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// GET /v1/repos/{owner}/{repo}/pipeline/edit-source is the READ HALF of editing
// a repository's pipeline: the raw .codeaf/factory.yaml as the DEFAULT BRANCH
// holds it, pinned to the commit it was read at, so the pull request route can
// be told exactly which base the edit started from.
//
// It is built from the pull request route's own parts and keeps its rules:
//
//   - The caller's GitHub credential only. A team never falls back to the host's
//     read grant or any host token; only a solo host may use its own profile
//     token, because there the host is the person.
//   - The same fixed origin, headers, redirect refusal, size bound and timeout
//     (pipelinePRFlow.call and Handler.githubClient). No URL, path, ref or
//     query comes from the caller, and a query string is refused.
//   - The sequence is repository, default-branch tip, file at that commit: three
//     GETs. There is no write of any kind, to GitHub or to disk, and nothing is
//     read from the host's checkout, working tree or personal file.
//
// A file that is not there, does not read cleanly or is too large for a pull
// request is an explicit state that refuses editing. Defaults are never offered
// as though they were the file.
func (h *Handler) pipelineEditSource(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	f := &pipelinePRFlow{h: h, w: w, r: r, owner: owner, repo: repo,
		repoAt: "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)}
	if r.URL.RawQuery != "" {
		f.sourceFail(422, api.PipelineEditStageRequest, 0, "this read takes no parameters")
		return
	}
	session, _ := h.credential(r)
	f.token, f.viaSes = session.githubToken, session.githubToken != ""
	if f.token == "" && h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		f.token = h.config.GitHubToken(r.Context())
	}
	if f.token == "" {
		f.sourceFail(401, api.PipelineEditStageCredential, 0, "Reconnect GitHub to edit the pipeline")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pipelinePRTimeout)
	defer cancel()
	f.ctx = ctx

	var meta struct {
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if !f.sourceRead(api.PipelineEditStageRepository, f.repoAt, &meta) {
		return
	}
	def := meta.DefaultBranch
	if def == "" || len(def) > 255 || strings.HasPrefix(def, "-") || strings.ContainsAny(def, "\x00\r\n\\") {
		f.sourceFail(502, api.PipelineEditStageRepository, 0, "GitHub did not name a usable default branch")
		return
	}
	var tip struct {
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if !f.sourceRead(api.PipelineEditStageBase, f.repoAt+"/git/ref/heads/"+escapeRef(def), &tip) {
		return
	}
	base := strings.ToLower(tip.Object.SHA)
	if tip.Object.Type != "commit" || !gitCommitSHA.MatchString(base) {
		f.sourceFail(502, api.PipelineEditStageBase, 0, "GitHub did not return a commit for the default branch")
		return
	}

	out := api.PipelineEditSource{Factory: h.factory, Repo: owner + "/" + repo, Path: factory.PipelineFile,
		DefaultBranch: def, BaseSHA: base, CanPush: meta.Permissions.Push, Archived: meta.Archived,
		// This binary's parser and runner both obey kind, when, effort and fanout.
		RuntimeFields: true}
	var file struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
	}
	status, err := f.get(f.repoAt+"/contents/"+factory.PipelineFile+"?ref="+base, &file)
	switch {
	case err != nil:
		f.sourceReadFailed(api.PipelineEditStageFile, status, err)
		return
	case status == 404:
		out.State = api.PipelineEditMissing
		f.answerSource(out)
		return
	case status != 200:
		f.sourceReadFailed(api.PipelineEditStageFile, status, nil)
		return
	}
	raw, decoded := decodeBase64(file.Content)
	if file.Type != "file" || file.Encoding != "base64" || !gitCommitSHA.MatchString(file.SHA) || !decoded {
		f.sourceFail(502, api.PipelineEditStageFile, 0, "GitHub returned an unreadable "+factory.PipelineFile)
		return
	}
	if len(raw) > pipeline.EditTextMax {
		out.State = api.PipelineEditTooLarge
		f.answerSource(out)
		return
	}
	out.Text = string(raw)
	src, problems, editable := pipeline.Inspect(out.Text)
	if !editable {
		out.State, out.Problems = api.PipelineEditInvalid, problems
		f.answerSource(out)
		return
	}
	out.State, out.Editable = api.PipelineEditFile, true
	out.SetParsed(src)
	f.answerSource(out)
}

// answerSource writes a read that found main's file, with THE FACTORY'S OWN
// COPY of it beside it (pipeline_copy.go) when the factory keeps one: its text,
// who saved it and when, how main's file compares, the version a save names,
// and whether the caller may save or stop it. Main's fields stay main's: the
// pull request is still pinned to main's commit and text.
func (f *pipelinePRFlow) answerSource(out api.PipelineEditSource) {
	if err := f.h.withPipelineCopy(f.r, &out); err != nil {
		f.sourceFail(500, api.PipelineEditStageFile, 0, "the factory's own copy of these steps could not be read")
		return
	}
	f.h.write(f.w, http.StatusOK, out)
}

// sourceRead is a GET that must answer 200; anything else ends the call.
func (f *pipelinePRFlow) sourceRead(stage, path string, out any) bool {
	status, err := f.get(path, out)
	if err == nil && status == 200 {
		return true
	}
	f.sourceReadFailed(stage, status, err)
	return false
}

// sourceReadFailed uses the pull request route's own status mapping, and never
// reads GitHub's text.
func (f *pipelinePRFlow) sourceReadFailed(stage string, status int, err error) {
	if err != nil {
		f.sourceFail(502, stage, status, "GitHub isn't answering; nothing was written, so you can retry")
		return
	}
	httpStatus, _ := upstreamStatus(status, stage == api.PipelineEditStageRepository)
	f.sourceFail(httpStatus, stage, status, "GitHub refused the "+stage+" step")
}

func (f *pipelinePRFlow) sourceFail(status int, stage string, github int, words string) {
	if status == 401 && f.viaSes {
		f.h.endGitHub(f.r, f.token)
	}
	f.h.write(f.w, status, api.PipelineEditSourceFailure{Factory: f.h.factory, Error: words, Reason: reasonFor(status),
		Stage: stage, GitHubStatus: github, RequestID: requestID()})
}
