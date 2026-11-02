package serve

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// GET /v1/repos/{owner}/{repo}/pipeline/prs lists the repository's OPEN pull
// requests into its default branch that change .codeaf/factory.yaml, so a
// client can say "pull request #N is open" across windows and restarts, not
// only for one it opened itself.
//
// It keeps the pull request route's rules (pipeline_pr.go): the caller's GitHub
// credential only (a solo host may use its own profile token; a team never
// falls back), the same fixed origin, headers, redirect refusal, size bound and
// timeout, no query from the caller, and GETs only.
//
// THE READ IS BOUNDED, at most 2 + pipelinePRsMaxChecks GitHub calls:
//
//	GET /repos/{o}/{r}                         the default branch
//	GET /repos/{o}/{r}/pulls?state=open&base=  one page of 100, most recently updated first
//	GET /repos/{o}/{r}/pulls/{n}/files         first page of 100, for at most 20 of them
//
// GitHub cannot filter pull requests by path, so the changed files are read
// per pull request. The factory's own branches (api.PipelinePRBranchPrefix) are
// read first, then the rest by recency. When a bound may have hidden a pull
// request (a full page, more candidates than reads, or a files page that is
// full without the file) the answer says Complete false rather than guessing.
const (
	pipelinePRsMaxChecks = 20
	pipelinePRsPage      = 100
	pipelinePRsParallel  = 4
)

type githubPull struct {
	Number    int       `json:"number"`
	HTMLURL   string    `json:"html_url"`
	Title     string    `json:"title"`
	Draft     bool      `json:"draft"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		Ref  string `json:"ref"`
		SHA  string `json:"sha"`
		Repo *struct {
			FullName string `json:"full_name"`
		} `json:"repo"`
	} `json:"head"`
}

// pullChecked is one pull request's files read.
type pullChecked struct {
	changes, full bool
	status        int
	err           error
}

func (h *Handler) pipelinePRs(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	f := &pipelinePRFlow{h: h, w: w, r: r, owner: owner, repo: repo,
		repoAt: "/repos/" + url.PathEscape(owner) + "/" + url.PathEscape(repo)}
	if r.URL.RawQuery != "" {
		f.prsFail(422, api.PipelinePRsStageRequest, 0, "this read takes no parameters")
		return
	}
	session, _ := h.credential(r)
	f.token, f.viaSes = session.githubToken, session.githubToken != ""
	if f.token == "" && h.mode() == api.FactorySolo && h.config.GitHubToken != nil {
		f.token = h.config.GitHubToken(r.Context())
	}
	if f.token == "" {
		f.prsFail(401, api.PipelinePRsStageCredential, 0, "Reconnect GitHub to see open pipeline pull requests")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), pipelinePRTimeout)
	defer cancel()
	f.ctx = ctx

	var meta struct {
		DefaultBranch string `json:"default_branch"`
	}
	if !f.prsRead(api.PipelinePRsStageRepository, f.repoAt, &meta) {
		return
	}
	def := meta.DefaultBranch
	if def == "" || len(def) > 255 || strings.HasPrefix(def, "-") || strings.ContainsAny(def, "\x00\r\n\\") {
		f.prsFail(502, api.PipelinePRsStageRepository, 0, "GitHub did not name a usable default branch")
		return
	}
	query := url.Values{"state": {"open"}, "base": {def}, "sort": {"updated"}, "direction": {"desc"}, "per_page": {strconv.Itoa(pipelinePRsPage)}}
	var pulls []githubPull
	if !f.prsRead(api.PipelinePRsStagePulls, f.repoAt+"/pulls?"+query.Encode(), &pulls) {
		return
	}
	open, ok := f.openPRs(pulls)
	if !ok {
		f.prsFail(502, api.PipelinePRsStagePulls, 0, "GitHub answered with a pull request this factory cannot read")
		return
	}
	order := checkOrder(open)
	complete := len(pulls) < pipelinePRsPage && len(order) <= pipelinePRsMaxChecks
	if len(order) > pipelinePRsMaxChecks {
		order = order[:pipelinePRsMaxChecks]
	}
	checked := f.checkFiles(open, order)
	out := api.PipelinePRs{Factory: h.factory, Repo: owner + "/" + repo, PRs: []api.PipelineOpenPR{}, CheckedAt: time.Now().UTC()}
	for _, i := range order {
		if c := checked[i]; c.err != nil || c.status != 200 {
			f.prsReadFailed(api.PipelinePRsStageFiles, c.status, c.err)
			return
		}
	}
	for i, pr := range open {
		complete = complete && !(checked[i].full && !checked[i].changes)
		if checked[i].changes {
			out.PRs = append(out.PRs, pr)
		}
	}
	out.Complete = complete
	h.write(w, http.StatusOK, out)
}

// openPRs is GitHub's list in the wire's shape, every pull request checked.
func (f *pipelinePRFlow) openPRs(pulls []githubPull) ([]api.PipelineOpenPR, bool) {
	out := make([]api.PipelineOpenPR, 0, len(pulls))
	self := f.owner + "/" + f.repo
	for _, p := range pulls {
		canonical := "https://github.com/" + self + "/pull/" + strconv.Itoa(p.Number)
		sha := strings.ToLower(p.Head.SHA)
		if p.Number <= 0 || !strings.EqualFold(p.HTMLURL, canonical) || !gitCommitSHA.MatchString(sha) || p.Head.Ref == "" {
			return nil, false
		}
		inRepo := p.Head.Repo != nil && strings.EqualFold(p.Head.Repo.FullName, self)
		out = append(out, api.PipelineOpenPR{Number: p.Number, URL: canonical, Title: p.Title, Branch: p.Head.Ref, HeadSHA: sha,
			Author: p.User.Login, Draft: p.Draft, CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.UpdatedAt.UTC(),
			FactoryBranch: inRepo && strings.HasPrefix(p.Head.Ref, api.PipelinePRBranchPrefix)})
	}
	return out, true
}

// checkOrder is the positions of open in the order their files are read: the
// factory's own branches first, then the rest, each by GitHub's recency.
func checkOrder(open []api.PipelineOpenPR) []int {
	order := make([]int, 0, len(open))
	for _, ours := range []bool{true, false} {
		for i, pr := range open {
			if pr.FactoryBranch == ours {
				order = append(order, i)
			}
		}
	}
	return order
}

// checkFiles reads the first page of changed files of each pull request at the
// given positions, a few at a time. Positions not read stay zero.
func (f *pipelinePRFlow) checkFiles(open []api.PipelineOpenPR, order []int) []pullChecked {
	out := make([]pullChecked, len(open))
	slots := make(chan struct{}, pipelinePRsParallel)
	var wg sync.WaitGroup
	for _, i := range order {
		wg.Add(1)
		slots <- struct{}{}
		go func(i int) {
			defer func() { <-slots; wg.Done() }()
			var files []struct {
				Filename         string `json:"filename"`
				PreviousFilename string `json:"previous_filename"`
			}
			path := f.repoAt + "/pulls/" + strconv.Itoa(open[i].Number) + "/files?per_page=" + strconv.Itoa(pipelinePRsPage)
			c := &out[i]
			if c.status, c.err = f.get(path, &files); c.err != nil || c.status != 200 {
				return
			}
			c.full = len(files) >= pipelinePRsPage
			for _, file := range files {
				c.changes = c.changes || file.Filename == factory.PipelineFile || file.PreviousFilename == factory.PipelineFile
			}
		}(i)
	}
	wg.Wait()
	return out
}

// prsRead is a GET that must answer 200; anything else ends the call.
func (f *pipelinePRFlow) prsRead(stage, path string, out any) bool {
	status, err := f.get(path, out)
	if err == nil && status == 200 {
		return true
	}
	f.prsReadFailed(stage, status, err)
	return false
}

// prsReadFailed uses the pull request route's status mapping and never reads
// GitHub's text.
func (f *pipelinePRFlow) prsReadFailed(stage string, status int, err error) {
	if err != nil {
		f.prsFail(502, stage, status, "GitHub isn't answering; nothing was written, so you can retry")
		return
	}
	httpStatus, _ := upstreamStatus(status, stage == api.PipelinePRsStageRepository)
	f.prsFail(httpStatus, stage, status, "GitHub refused the "+stage+" step")
}

func (f *pipelinePRFlow) prsFail(status int, stage string, github int, words string) {
	if status == 401 && f.viaSes {
		f.h.endGitHub(f.r, f.token)
	}
	f.h.write(f.w, status, api.PipelinePRsFailure{Factory: f.h.factory, Error: words, Reason: reasonFor(status),
		Stage: stage, GitHubStatus: github, RequestID: requestID()})
}
