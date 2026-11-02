package serve

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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
//     (pipelinePRFlow.call and Handler.githubClient). No URL or path comes from
//     the caller. The one query is ?ref=, a pull request number or a 40-hex
//     commit, checked before anything is sent; any other query is refused.
//   - The sequence is repository, default-branch tip, file at that commit: three
//     GETs. With ref it is repository, the pull request (found from its head
//     commit by GET commits/{sha}/pulls when ref is one), its branch's tip, its
//     changed files, and the file at that tip: five or six GETs. There is no
//     write of any kind, to GitHub or to disk, and nothing is read from the
//     host's checkout, working tree or personal file.
//   - A ref read never touches the factory's own copy of the file: the copy is
//     compared with, and cleared by, main's file only.
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
	ref, ok := sourceRef(r.URL.RawQuery)
	if !ok {
		f.sourceFail(422, api.PipelineEditStageRequest, 0, "this read takes only ref, a pull request number or a 40-character commit SHA")
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
	out := api.PipelineEditSource{Factory: h.factory, Repo: owner + "/" + repo, Path: factory.PipelineFile,
		DefaultBranch: def, CanPush: meta.Permissions.Push, Archived: meta.Archived,
		// This binary's parser and runner both obey kind, when, effort and fanout.
		RuntimeFields: true}
	if ref == "" {
		out.BaseSHA, ok = f.sourceDefaultTip(def)
	} else {
		out.Ref = ref
		out.PR, out.BaseSHA, ok = f.sourcePull(ref, def)
	}
	if !ok {
		return
	}
	base := out.BaseSHA
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
	if out.Ref != "" {
		f.h.write(f.w, http.StatusOK, out)
		return
	}
	if err := f.h.withPipelineCopy(f.r, &out); err != nil {
		f.sourceFail(500, api.PipelineEditStageFile, 0, "the factory's own copy of these steps could not be read")
		return
	}
	f.h.write(f.w, http.StatusOK, out)
}

var sourcePRNumber = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)

// sourceRef is the read's one optional query, ref: a pull request number or a
// head commit, folded to lower case. ok is false for anything else.
func sourceRef(raw string) (string, bool) {
	if raw == "" {
		return "", true
	}
	q, err := url.ParseQuery(raw)
	if err != nil || len(q) != 1 || len(q["ref"]) != 1 {
		return "", false
	}
	ref := strings.ToLower(q["ref"][0])
	return ref, sourcePRNumber.MatchString(ref) || gitCommitSHA.MatchString(ref)
}

// sourceDefaultTip pins the default branch's tip, or ends the call.
func (f *pipelinePRFlow) sourceDefaultTip(def string) (string, bool) {
	var tip struct {
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if !f.sourceRead(api.PipelineEditStageBase, f.repoAt+"/git/ref/heads/"+escapeRef(def), &tip) {
		return "", false
	}
	base := strings.ToLower(tip.Object.SHA)
	if tip.Object.Type != "commit" || !gitCommitSHA.MatchString(base) {
		f.sourceFail(502, api.PipelineEditStageBase, 0, "GitHub did not return a commit for the default branch")
		return "", false
	}
	return base, true
}

// sourcePull answers the open pipeline pull request ref names and its head
// commit, or ends the call at stage "head". It is the pull request route's own
// check (openPull), so a pull request this read takes is one an update may
// write to, and then the changed files: only the pipeline file.
func (f *pipelinePRFlow) sourcePull(ref, def string) (int, string, bool) {
	f.source = true
	number, wanted := 0, ""
	if gitCommitSHA.MatchString(ref) {
		if number = f.pullAt(ref); number == 0 {
			return 0, "", false
		}
		wanted = ref
	} else {
		number, _ = strconv.Atoi(ref)
	}
	pr, ok := f.openPull(number, def, wanted)
	if !ok {
		return 0, "", false
	}
	var files []struct {
		Filename string `json:"filename"`
	}
	if !f.read("head", f.repoAt+"/pulls/"+strconv.Itoa(number)+"/files?per_page=100", &files) {
		return 0, "", false
	}
	for _, file := range files {
		if file.Filename != factory.PipelineFile || len(files) == 100 {
			f.refuse(409, "head", fmt.Sprintf("Pull request #%d changes more than %s", number, factory.PipelineFile))
			return 0, "", false
		}
	}
	return number, pr.HeadSHA, true
}

// pullAt is the one open pull request of this repository with commit sha on
// its branch, or 0 with the call ended.
func (f *pipelinePRFlow) pullAt(sha string) int {
	var pulls []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Base   struct {
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"base"`
	}
	if !f.read("head", f.repoAt+"/commits/"+sha+"/pulls", &pulls) {
		return 0
	}
	number := 0
	for _, p := range pulls {
		if p.State == "open" && p.Number > 0 && p.Base.Repo != nil && strings.EqualFold(p.Base.Repo.FullName, f.owner+"/"+f.repo) {
			if number != 0 {
				f.refuse(409, "head", "More than one open pull request has that commit; read it by number")
				return 0
			}
			number = p.Number
		}
	}
	if number == 0 {
		f.refuse(409, "head", "No open pull request of this repository has that commit")
	}
	return number
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
	out := api.PipelineEditSourceFailure{Factory: f.h.factory, Error: words, Reason: reasonFor(status),
		Stage: stage, GitHubStatus: github, RequestID: requestID()}
	f.h.keptPipelineCopy(f.r, f.owner+"/"+f.repo, &out)
	f.h.write(f.w, status, out)
}
