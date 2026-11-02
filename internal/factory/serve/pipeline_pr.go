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
// WITH "pr" IT ADDS ONE COMMIT TO AN OPEN PULL REQUEST instead, so a person
// never has a second pull request for the same file while the first is open.
// The sequence is read, read, read, then one write:
//
//	GET  /repos/{o}/{r}                       default branch and push permission
//	GET  /repos/{o}/{r}/pulls/{n}             open, into the default branch, from a branch of THIS repository
//	GET  /repos/{o}/{r}/contents/{file}?ref=<head sha>   the file at the pull request's head
//	PUT  /repos/{o}/{r}/contents/{file}       commit the file on the head branch, naming the blob read
//
// The pull request's head branch was there before the call, so a failure never
// leaves a branch. GitHub's contents API guards the FILE (the blob sha), not
// the branch: expected_head_sha refuses a head that moved before the read, and
// a push between the read and the write that left the file alone is built on.
//
// DEFERRED, and refused as unknown request fields rather than ignored:
// requesting reviewers. Monorepo sub-roots are not supported: the one file is
// factory.PipelineFile.
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
	// update is true when the call adds a commit to an existing pull request:
	// its branch was never this call's to leave.
	update bool
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
	if !ok || !f.validateUpdate(body) {
		return
	}
	data, refused := pipelineFileFor(body.Text)
	if refused != "" {
		f.refuse(422, "request", refused)
		return
	}

	// The person's own GitHub credential. A team never falls back to the
	// host's read grant or any host token; only a solo host may use its own
	// profile token, because there the host is the person.
	f.token, f.viaSes = h.personGitHubToken(r)
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
	if f.update {
		f.runUpdate(body.PR, strings.ToLower(strings.TrimSpace(body.ExpectedHeadSHA)), title, data)
		return
	}
	f.run(title, text, branchWanted, wanted, data)
}

// pipelineFileFor is a person's pipeline text as the factory writes it, or the
// words that refuse it. IT IS THE ONE CHECK OF A WHOLE FILE A PERSON SENDS:
// the pull request route and the factory's own copy (pipeline_copy.go) both
// take a file only through it, so a file one of them takes the other takes
// too, byte for byte.
func pipelineFileFor(text string) ([]byte, string) {
	switch {
	case strings.TrimSpace(text) == "":
		return nil, "pipeline text is required"
	case len(text) > pipelinePRMaxText:
		return nil, "pipeline text is too large"
	}
	file, err := pipeline.Parse(text)
	switch {
	case err != nil:
		return nil, err.Error()
	case len(file.Problems) > 0:
		return nil, file.Problems[0].Error()
	}
	data, err := pipeline.Encode(file)
	if err != nil || len(data) > pipelinePRMaxText {
		return nil, "the pipeline could not be written as a small YAML file"
	}
	return data, ""
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

// validateUpdate bounds the fields of a commit to an existing pull request.
// Those that only shape a NEW pull request are refused with pr, never ignored.
func (f *pipelinePRFlow) validateUpdate(body api.PipelinePRRequest) bool {
	head := strings.ToLower(strings.TrimSpace(body.ExpectedHeadSHA))
	switch {
	case body.PR == 0 && head == "":
		return true
	case body.PR <= 0:
		f.refuse(422, "request", "pr must be the number of an open pull request")
	case body.Branch != "" || body.Body != "" || body.ExpectedBaseSHA != "":
		f.refuse(422, "request", "branch, body and expected_base_sha are for a new pull request; leave them out with pr")
	case head != "" && !gitCommitSHA.MatchString(head):
		f.refuse(422, "request", "expected_head_sha must be a 40-character commit SHA")
	default:
		f.update = true
		return true
	}
	return false
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

// repository reads the default branch and whether this token can push, or
// ends the call.
func (f *pipelinePRFlow) repository() (string, bool) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if !f.read("repository", f.repoAt, &repo) {
		return "", false
	}
	def := repo.DefaultBranch
	if def == "" || len(def) > 255 || strings.HasPrefix(def, "-") || strings.ContainsAny(def, "\x00\r\n\\") {
		f.fail(502, api.PipelinePRNotOpened, "repository", 0, "GitHub did not name a usable default branch")
		return "", false
	}
	if repo.Archived || !repo.Permissions.Push {
		f.fail(403, api.PipelinePRRefused, "permission", 0, "Your GitHub access cannot push to "+f.owner+"/"+f.repo+"; nothing was written")
		return "", false
	}
	return def, true
}

func (f *pipelinePRFlow) run(title, text, branch, wantedBase string, data []byte) {
	// 1. The repository: its default branch and whether this token can push.
	def, ok := f.repository()
	if !ok {
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
	blob, same, ok := f.fileAt(base, data)
	if !ok {
		return
	}
	if same {
		f.h.write(f.w, 200, api.PipelinePRResponse{Factory: f.h.factory, Outcome: api.PipelinePRUnchanged, BaseSHA: base,
			Note: "The default branch already has this pipeline; no pull request was opened."})
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
	status, err := f.get(f.repoAt+"/git/ref/heads/"+escapeRef(branch), &struct{}{})
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
	head, ok := f.commit(title, blob, data)
	if !ok {
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

// runUpdate adds one commit of the file to the open pull request number, on its
// own head branch, or ends the call. Nothing is written until the pull request
// is known to be open, into the default branch, from a branch of this same
// repository, and (when the caller pinned one) still at the expected head.
func (f *pipelinePRFlow) runUpdate(number int, wantedHead, title string, data []byte) {
	def, ok := f.repository()
	if !ok {
		return
	}
	pr, ok := f.openPull(number, def, wantedHead)
	if !ok {
		return
	}
	f.branch = pr.Branch
	blob, same, ok := f.fileAt(pr.HeadSHA, data)
	if !ok {
		return
	}
	if same {
		f.h.write(f.w, 200, api.PipelinePRResponse{Factory: f.h.factory, Outcome: api.PipelinePRUnchanged, PR: &pr, BaseSHA: pr.BaseSHA,
			Note: fmt.Sprintf("Pull request #%d already has this pipeline; nothing was written.", number)})
		return
	}
	if pr.HeadSHA, ok = f.commit(title, blob, data); !ok {
		return
	}
	f.h.write(f.w, 200, api.PipelinePRResponse{Factory: f.h.factory, Outcome: api.PipelinePRUpdated, PR: &pr, BaseSHA: pr.BaseSHA,
		Note: fmt.Sprintf("Pull request #%d updated with your GitHub access.", number)})
}

// openPull reads pull request number and answers it only when this flow may
// write to it; every other answer ends the call at stage "head".
func (f *pipelinePRFlow) openPull(number int, def, wantedHead string) (api.PipelinePRResult, bool) {
	type side struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	}
	var pull struct {
		Number int    `json:"number"`
		State  string `json:"state"`
		Head   side   `json:"head"`
		Base   side   `json:"base"`
	}
	if !f.read("head", f.repoAt+"/pulls/"+strconv.Itoa(number), &pull) {
		return api.PipelinePRResult{}, false
	}
	here := func(s side) bool { return s.Repo != nil && strings.EqualFold(s.Repo.FullName, f.owner+"/"+f.repo) }
	head, base := strings.ToLower(pull.Head.SHA), strings.ToLower(pull.Base.SHA)
	switch {
	case pull.Number != number || !gitCommitSHA.MatchString(head) || !gitCommitSHA.MatchString(base):
		f.fail(502, api.PipelinePRNotOpened, "head", 0, "GitHub returned an unreadable pull request; nothing was written")
	case pull.State != "open":
		f.refuse(409, "head", fmt.Sprintf("Pull request #%d is not open; nothing was written", number))
	case !here(pull.Base) || pull.Base.Ref != def:
		f.refuse(409, "head", fmt.Sprintf("Pull request #%d does not merge into this repository's default branch; nothing was written", number))
	case !here(pull.Head) || pull.Head.Ref == def || !validPipelineBranch(pull.Head.Ref):
		f.refuse(409, "head", fmt.Sprintf("Pull request #%d is not from a branch of this repository; nothing was written", number))
	case wantedHead != "" && wantedHead != head:
		f.refuse(409, "head", fmt.Sprintf("Pull request #%d has new commits since this draft started; nothing was written", number))
	default:
		return api.PipelinePRResult{Number: number, URL: "https://github.com/" + f.owner + "/" + f.repo + "/pull/" + strconv.Itoa(number),
			Branch: pull.Head.Ref, Base: def, BaseSHA: base, HeadSHA: head}, true
	}
	return api.PipelinePRResult{}, false
}

// fileAt reads the pipeline file at commit ref. It answers the blob to name
// when replacing it ("" when it is missing) and whether it already holds data,
// or ends the call.
func (f *pipelinePRFlow) fileAt(ref string, data []byte) (blob string, same, ok bool) {
	var current struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
		SHA      string `json:"sha"`
	}
	status, err := f.get(f.repoAt+"/contents/"+factory.PipelineFile+"?ref="+ref, &current)
	switch {
	case err != nil:
		f.readFailed("file", status, err)
		return "", false, false
	case status == 404:
		return "", false, true
	case status != 200:
		f.readFailed("file", status, nil)
		return "", false, false
	}
	old, decoded := decodeBase64(current.Content)
	if current.Type != "file" || current.Encoding != "base64" || !gitCommitSHA.MatchString(current.SHA) || !decoded {
		f.fail(502, api.PipelinePRNotOpened, "file", 0, "GitHub returned an unreadable "+factory.PipelineFile)
		return "", false, false
	}
	return current.SHA, bytes.Equal(old, data), true
}

// commit puts the file on f.branch, naming blob when it replaces one, and
// answers the new commit, or ends the call. It is sent once.
func (f *pipelinePRFlow) commit(title, blob string, data []byte) (string, bool) {
	put := map[string]string{"message": title, "content": base64.StdEncoding.EncodeToString(data), "branch": f.branch}
	if blob != "" {
		put["sha"] = blob
	}
	var commit struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	status, err := f.call(http.MethodPut, f.repoAt+"/contents/"+factory.PipelineFile, put, &commit)
	if (status != 200 && status != 201) || err != nil {
		f.writeFailed("commit", status, err)
		return "", false
	}
	head := strings.ToLower(commit.Commit.SHA)
	if !gitCommitSHA.MatchString(head) {
		f.fail(502, api.PipelinePRUnknown, "commit", status, "GitHub did not confirm the pipeline commit; check GitHub before trying again"+f.branchWords())
		return "", false
	}
	return head, true
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

// personGitHubToken is the person's own GitHub credential, and whether it came
// from their factory sign-in. A team never falls back to the host's read
// grant or any host token; only a solo host may use its own profile token,
// because there the host is the person.
func (h *Handler) personGitHubToken(r *http.Request) (string, bool) {
	session, _ := h.credential(r)
	if session.githubToken != "" {
		return session.githubToken, true
	}
	if h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		return h.config.GitHubToken(r.Context()), false
	}
	return "", false
}

func (f *pipelinePRFlow) call(method, path string, in, out any) (int, error) {
	return f.h.githubREST(f.ctx, f.token, method, path, in, out, pipelinePRReadLimit)
}

// githubREST sends one request to GitHub's REST API with the person's token
// in the Authorization header only, and decodes at most limit bytes of a 2xx
// answer into out. An error means there is no usable answer; a non-2xx status
// is an answer, and its body is never read.
func (h *Handler) githubREST(ctx context.Context, token, method, path string, in, out any, limit int) (int, error) {
	var raw []byte
	if in != nil {
		var err error
		if raw, err = json.Marshal(in); err != nil {
			return 0, errGitHubUnreadable
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, githubAPI+path, ioBody(raw))
	if err != nil {
		return 0, errGitHubUnreachable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "codeaf-factory")
	client := h.githubClient()
	response, err := client.Do(req)
	if err != nil {
		return 0, errGitHubUnreachable
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 || out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<16))
		return response.StatusCode, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(limit)+1))
	if err != nil || len(data) > limit || json.Unmarshal(data, out) != nil {
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
		f.left = !f.update
		f.fail(502, api.PipelinePRUnknown, stage, status, "GitHub's answer to "+stageWords(stage)+" is unknown; check "+f.owner+"/"+f.repo+" on GitHub before trying again"+f.branchWords())
		return
	}
	if stage == "branch" && (status == 409 || status == 422) {
		f.fail(409, api.PipelinePRRefused, stage, status, fmt.Sprintf("GitHub would not create branch %s (HTTP %d); nothing was written", f.branch, status))
		return
	}
	httpStatus, outcome := upstreamStatus(status, false)
	if (f.left || f.update) && outcome == api.PipelinePRRefused {
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
