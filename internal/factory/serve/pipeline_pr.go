package serve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// POST /v1/repos/{owner}/{repo}/pipeline/open-pr opens a pull request that
// changes ONLY .codeaf/factory.yaml, AS THE SIGNED-IN CALLER, over GitHub's
// REST API. It needs no checkout and never touches one: no git, no gh, no
// exec of any kind, so the host's ambient identity can never be used.
//
// The sequence is read, read, read, then three writes:
//
//	GET  /repos/{o}/{r}                       default branch and push permission
//	GET  /repos/{o}/{r}/git/ref/heads/{def}   the default branch tip, pinned as a commit SHA
//	GET  /repos/{o}/{r}/contents/{file}?ref=<sha>   the file at that exact commit
//	GET  /repos/{o}/{r}/git/ref/heads/{new}   the new branch must not exist yet
//	POST /repos/{o}/{r}/git/refs              create the NEW branch at the pinned SHA
//	PUT  /repos/{o}/{r}/contents/{file}       commit the file on the new branch
//	POST /repos/{o}/{r}/pulls                 open the pull request
//
// THIS IS NOT A TRANSACTION. Three separate writes can stop part way, and the
// first slice deliberately has NO cleanup: it never issues a DELETE against any
// ref. A branch it created stays on GitHub and is reported (branch_left,
// branch), because an unknown pull request result or a concurrent push to the
// branch must never cost anyone work. The caller removes a stray branch on
// GitHub. A server restart mid-flight can likewise leave a branch behind.
// A write whose effect is unknown is never retried and never repeated.
//
// DEFERRED, and refused as unknown request fields rather than ignored:
// requesting reviewers, and adding a commit to an existing pull request.
// Monorepo sub-roots are not supported: the one file is factory.PipelineFile.
const (
	pipelinePRTimeout   = 60 * time.Second
	pipelinePRMaxText   = 256 << 10
	pipelinePRMaxTitle  = 200
	pipelinePRMaxBody   = 16 << 10
	pipelinePRMaxBranch = 200
	pipelinePRReadLimit = 2 << 20
	githubAPI           = "https://api.github.com"
	githubAPIVersion    = "2022-11-28"
)

var gitCommitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// pipelinePRFlights holds the repositories with a pull request in flight on
// this host, so a double click cannot open two.
var pipelinePRFlights sync.Map

type pipelinePRFlow struct {
	h      *Handler
	w      http.ResponseWriter
	r      *http.Request
	ctx    context.Context
	token  string
	viaSes bool
	owner  string
	repo   string
	repoAt string
	branch string
	// left is true once the new branch exists or may exist on GitHub.
	left bool
}

func (h *Handler) pipelinePR(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	f := &pipelinePRFlow{h: h, w: w, r: r, owner: owner, repo: repo,
		repoAt: "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)}
	var body api.PipelinePRRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	title, text, branchWanted, wanted, ok := f.validate(body)
	if !ok {
		return
	}
	file, err := pipeline.Parse(body.Text)
	switch {
	case err != nil:
		f.refuse(422, "request", err.Error())
		return
	case len(file.Problems) > 0:
		f.refuse(422, "request", file.Problems[0].Error())
		return
	}
	data, err := pipeline.Encode(file)
	if err != nil || len(data) > pipelinePRMaxText {
		f.refuse(422, "request", "the pipeline could not be written as a small YAML file")
		return
	}

	// The person's own GitHub credential. A team never falls back to the
	// host's read grant or any host token; only a solo host may use its own
	// profile token, because there the host is the person.
	session, _ := h.credential(r)
	f.token, f.viaSes = session.githubToken, session.githubToken != ""
	if f.token == "" && h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		f.token = h.config.GitHubToken(r.Context())
	}
	if f.token == "" {
		f.refuse(401, "credential", "Reconnect GitHub to open a pull request")
		return
	}
	key := strings.ToLower(owner + "/" + repo)
	if _, busy := pipelinePRFlights.LoadOrStore(key, struct{}{}); busy {
		f.refuse(409, "request", "a pull request for this repository is already being opened")
		return
	}
	defer pipelinePRFlights.Delete(key)
	ctx, cancel := context.WithTimeout(r.Context(), pipelinePRTimeout)
	defer cancel()
	f.ctx = ctx
	f.run(title, text, branchWanted, wanted, data)
}

// validate bounds every caller-supplied field before anything is sent.
func (f *pipelinePRFlow) validate(body api.PipelinePRRequest) (title, text, branch, base string, ok bool) {
	if strings.TrimSpace(body.Text) == "" {
		f.refuse(422, "request", "pipeline text is required")
		return
	}
	if len(body.Text) > pipelinePRMaxText {
		f.refuse(422, "request", "pipeline text is too large")
		return
	}
	title = strings.TrimSpace(body.Title)
	if title == "" {
		title = "Update factory pipeline"
	}
	if len([]rune(title)) > pipelinePRMaxTitle || strings.ContainsAny(title, "\r\n\x00") {
		f.refuse(422, "request", fmt.Sprintf("title must be one line of at most %d characters", pipelinePRMaxTitle))
		return
	}
	text = strings.TrimSpace(body.Body)
	if text == "" {
		text = "Changes to " + factory.PipelineFile + ", made in codeaf."
	}
	if len(text) > pipelinePRMaxBody || strings.ContainsRune(text, 0) {
		f.refuse(422, "request", "body is too large")
		return
	}
	branch = body.Branch
	if branch != "" && !validPipelineBranch(branch) {
		f.refuse(422, "request", fmt.Sprintf("branch must be a valid new Git branch name of at most %d bytes", pipelinePRMaxBranch))
		return
	}
	base = strings.ToLower(strings.TrimSpace(body.ExpectedBaseSHA))
	if base != "" && !gitCommitSHA.MatchString(base) {
		f.refuse(422, "request", "expected_base_sha must be a 40-character commit SHA")
		return
	}
	return title, text, branch, base, true
}

func validPipelineBranch(name string) bool {
	if name == "" || len(name) > pipelinePRMaxBranch || name == "@" || name == "HEAD" ||
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

func (f *pipelinePRFlow) run(title, text, branch, wantedBase string, data []byte) {
	// 1. The repository: its default branch and whether this token can push.
	var repo struct {
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if !f.read("repository", f.repoAt, &repo) {
		return
	}
	def := repo.DefaultBranch
	if def == "" || len(def) > 255 || strings.HasPrefix(def, "-") || strings.ContainsAny(def, "\x00\r\n\\") {
		f.fail(502, api.PipelinePRNotOpened, "repository", 0, "GitHub did not name a usable default branch")
		return
	}
	if repo.Archived || !repo.Permissions.Push {
		f.fail(403, api.PipelinePRRefused, "permission", 0, "Your GitHub access cannot push to "+f.owner+"/"+f.repo+"; nothing was written")
		return
	}

	// 2. Pin the default branch tip as a commit SHA. Everything after reads and
	// branches from this SHA, never from the moving branch name.
	var tip struct {
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	if !f.read("base", f.repoAt+"/git/ref/heads/"+escapeRef(def), &tip) {
		return
	}
	base := strings.ToLower(tip.Object.SHA)
	if tip.Object.Type != "commit" || !gitCommitSHA.MatchString(base) {
		f.fail(502, api.PipelinePRNotOpened, "base", 0, "GitHub did not return a commit for the default branch")
		return
	}
	if wantedBase != "" && wantedBase != base {
		f.fail(409, api.PipelinePRRefused, "base", 0, "The default branch has moved since this draft started; nothing was written")
		return
	}

	// 3. The file at the pinned commit. Missing means this PR creates it.
	var current struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
	}
	blob := ""
	status, err := f.get(f.repoAt+"/contents/"+factory.PipelineFile+"?ref="+base, &current)
	switch {
	case err != nil:
		f.readFailed("file", status, err)
		return
	case status == 404:
	case status == 200:
		old, ok := decodeBase64(current.Content)
		if current.Type != "file" || current.Encoding != "base64" || !gitCommitSHA.MatchString(current.SHA) || !ok {
			f.fail(502, api.PipelinePRNotOpened, "file", 0, "GitHub returned an unreadable "+factory.PipelineFile)
			return
		}
		if bytes.Equal(old, data) {
			f.h.write(f.w, 200, api.PipelinePRResponse{Factory: f.h.factory, Outcome: api.PipelinePRUnchanged, BaseSHA: base,
				Note: "The default branch already has this pipeline; no pull request was opened."})
			return
		}
		blob = current.SHA
	default:
		f.readFailed("file", status, nil)
		return
	}

	// 4. A NEW branch. An existing one is refused, never reused or renamed.
	if branch == "" {
		sum := sha256.Sum256(append([]byte(base+"\n"), data...))
		branch = "codeaf/pipeline-" + hex.EncodeToString(sum[:6])
	}
	f.branch = branch
	// A repository may use any valid branch name as its default, including
	// our suggested prefix. Protect it even if its ref disappears after read.
	if branch == def {
		f.refuse(409, "branch", "Choose a new branch; the default branch cannot be a pipeline proposal")
		return
	}
	status, err = f.get(f.repoAt+"/git/ref/heads/"+escapeRef(branch), &struct{}{})
	switch {
	case err != nil:
		f.readFailed("branch", status, err)
		return
	case status == 200:
		f.fail(409, api.PipelinePRRefused, "branch", 0, "Branch "+branch+" already exists; choose another branch name. Nothing was written")
		return
	case status != 404:
		f.readFailed("branch", status, nil)
		return
	}
	var created struct {
		Ref    string `json:"ref"`
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	status, err = f.call(http.MethodPost, f.repoAt+"/git/refs", map[string]string{"ref": "refs/heads/" + branch, "sha": base}, &created)
	if status != 201 || err != nil {
		f.writeFailed("branch", status, err)
		return
	}
	f.left = true
	if created.Ref != "refs/heads/"+branch || created.Object.Type != "commit" || strings.ToLower(created.Object.SHA) != base {
		f.fail(502, api.PipelinePRUnknown, "branch", status, "GitHub did not confirm the proposed branch; check GitHub before trying again"+f.branchWords())
		return
	}

	// 5. Commit the file on the new branch (never the default branch).
	put := map[string]string{"message": title, "content": base64.StdEncoding.EncodeToString(data), "branch": branch}
	if blob != "" {
		put["sha"] = blob
	}
	var commit struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	status, err = f.call(http.MethodPut, f.repoAt+"/contents/"+factory.PipelineFile, put, &commit)
	if (status != 200 && status != 201) || err != nil {
		f.writeFailed("commit", status, err)
		return
	}
	head := strings.ToLower(commit.Commit.SHA)
	if !gitCommitSHA.MatchString(head) {
		f.fail(502, api.PipelinePRUnknown, "commit", status, "GitHub did not confirm the pipeline commit; check GitHub before trying again"+f.branchWords())
		return
	}

	// 6. The pull request. Success is a positive number and the canonical URL.
	var pr struct {
		Number int    `json:"number"`
		URL    string `json:"html_url"`
	}
	status, err = f.call(http.MethodPost, f.repoAt+"/pulls", map[string]string{"title": title, "head": branch, "base": def, "body": text}, &pr)
	if status != 201 || err != nil {
		f.writeFailed("pull_request", status, err)
		return
	}
	canonical := "https://github.com/" + f.owner + "/" + f.repo + "/pull/" + strconv.Itoa(pr.Number)
	if pr.Number <= 0 || !strings.EqualFold(pr.URL, canonical) {
		f.fail(502, api.PipelinePRUnknown, "pull_request", status, "GitHub answered without a usable pull request; check "+f.owner+"/"+f.repo+" on GitHub. Branch "+branch+" was left there")
		return
	}
	f.h.write(f.w, 201, api.PipelinePRResponse{Factory: f.h.factory, Outcome: api.PipelinePROpened,
		PR:      &api.PipelinePRResult{Number: pr.Number, URL: canonical, Branch: branch, Base: def, BaseSHA: base, HeadSHA: head},
		BaseSHA: base, Note: fmt.Sprintf("Pull request #%d opened with your GitHub access.", pr.Number)})
}

// read is a GET that must answer 200; anything else ends the call.
func (f *pipelinePRFlow) read(stage, path string, out any) bool {
	status, err := f.get(path, out)
	if err == nil && status == 200 {
		return true
	}
	f.readFailed(stage, status, err)
	return false
}

// get decodes only a 200 body; a failed status never reads GitHub's text.
func (f *pipelinePRFlow) get(path string, out any) (int, error) {
	return f.call(http.MethodGet, path, nil, out)
}

var (
	errGitHubUnreachable = errors.New("GitHub did not answer")
	errGitHubUnreadable  = errors.New("GitHub answered unreadably")
)

// call sends one request with the caller's token in the Authorization header
// only. An error means there is no usable answer; a non-2xx status is an answer.
func (f *pipelinePRFlow) call(method, path string, in, out any) (int, error) {
	var raw []byte
	if in != nil {
		var err error
		if raw, err = json.Marshal(in); err != nil {
			return 0, errGitHubUnreadable
		}
	}
	req, err := http.NewRequestWithContext(f.ctx, method, githubAPI+path, ioBody(raw))
	if err != nil {
		return 0, errGitHubUnreachable
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := f.h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		return 0, errGitHubUnreachable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		return response.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, pipelinePRReadLimit+1))
	if err != nil || len(data) > pipelinePRReadLimit || json.Unmarshal(data, out) != nil {
		return response.StatusCode, errGitHubUnreadable
	}
	return response.StatusCode, nil
}

// readFailed ends the call at a read. Nothing has been written to GitHub by a
// read stage, so a transport failure there is a plain not_opened.
func (f *pipelinePRFlow) readFailed(stage string, status int, err error) {
	if err != nil {
		f.fail(502, api.PipelinePRNotOpened, stage, status, "GitHub isn't answering; nothing was written, so you can retry")
		return
	}
	httpStatus, outcome := upstreamStatus(status, stage == "repository")
	f.fail(httpStatus, outcome, stage, status, fmt.Sprintf("GitHub refused the %s step (HTTP %d); nothing was written", stage, status))
}

// writeFailed ends the call at a write. A transport failure, an unreadable or
// unexpected 2xx answer, a redirect or a 5xx leaves the effect UNKNOWN: the
// write is not repeated and nothing is deleted, and the branch is reported as
// left because it may exist. Only a definitive 4xx says the write did not happen.
func (f *pipelinePRFlow) writeFailed(stage string, status int, err error) {
	if err != nil || status < 400 || status >= 500 {
		f.left = true
		f.fail(502, api.PipelinePRUnknown, stage, status, "GitHub's answer to "+stageWords(stage)+" is unknown; check "+f.owner+"/"+f.repo+" on GitHub before trying again"+f.branchWords())
		return
	}
	if stage == "branch" && (status == 409 || status == 422) {
		f.fail(409, api.PipelinePRRefused, stage, status, fmt.Sprintf("GitHub would not create branch %s (HTTP %d); nothing was written", f.branch, status))
		return
	}
	httpStatus, outcome := upstreamStatus(status, false)
	if f.left && outcome == api.PipelinePRRefused {
		outcome = api.PipelinePRNotOpened
	}
	f.fail(httpStatus, outcome, stage, status, fmt.Sprintf("GitHub refused to %s (HTTP %d)%s", stageWords(stage), status, f.branchWords()))
}

func stageWords(stage string) string {
	switch stage {
	case "branch":
		return "create the branch"
	case "commit":
		return "commit the pipeline"
	}
	return "open the pull request"
}

// branchWords is the honest cleanup statement: the first slice never deletes.
func (f *pipelinePRFlow) branchWords() string {
	if !f.left || f.branch == "" {
		return ""
	}
	return ". Branch " + f.branch + " was left on " + f.owner + "/" + f.repo + "; codeaf does not delete branches, so remove it on GitHub if you don't need it"
}

// upstreamStatus maps a definitive GitHub status to this API's status. 401 is
// the caller's sign-in; 403 and 404 on a write are one answer, "not allowed".
func upstreamStatus(status int, repoStage bool) (int, string) {
	switch {
	case status == 401:
		return 401, api.PipelinePRRefused
	case status == 404 && repoStage:
		return 404, api.PipelinePRRefused
	case status == 403 || status == 404:
		return 403, api.PipelinePRRefused
	case status == 409:
		return 409, api.PipelinePRNotOpened
	case status == 422:
		return 422, api.PipelinePRNotOpened
	case status == 429:
		return 429, api.PipelinePRNotOpened
	}
	return 502, api.PipelinePRNotOpened
}

func (f *pipelinePRFlow) refuse(status int, stage, words string) {
	f.fail(status, api.PipelinePRRefused, stage, 0, words)
}

func (f *pipelinePRFlow) fail(status int, outcome, stage string, github int, words string) {
	if status == 401 && f.viaSes {
		f.h.endGitHub(f.r, f.token)
	}
	f.h.write(f.w, status, api.PipelinePRFailure{Factory: f.h.factory, Error: words, Reason: reasonFor(status), Outcome: outcome,
		Stage: stage, GitHubStatus: github, Branch: f.branch, BranchLeft: f.left, RequestID: requestID()})
}

func escapeRef(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

func decodeBase64(s string) ([]byte, bool) {
	out, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(s))
	return out, err == nil
}
