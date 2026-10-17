//go:build e2e

package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// THE LIVE PIPELINE PULL REQUEST. The app's own local factory service answers
// POST /v1/repos/{owner}/{repo}/pipeline/open-pr against a real GitHub
// repository, and the pull request it opens is checked at GitHub afterwards.
//
// THIS LANE WRITES TO GITHUB, exactly three times and nowhere else: one new
// ref, one commit of .codeaf/factory.yaml on that ref, one new pull request.
// It is bounded by the transport, not by good manners: the factory's GitHub
// client is the real one behind a guard that lets every GET through and lets a
// write through ONLY when it is the next of those three, aimed at the chosen
// repository and carrying this run's unique branch. A PATCH, a DELETE, a second
// write of any kind, a write to the default or any existing branch, to an issue
// or to an existing pull request is refused AND counted, and the run fails.
//
// NOTHING IS CLEANED UP. The pull request stays open and its branch stays: the
// product never deletes a remote branch, and a test that closed or deleted
// would be a second kind of write. The person running it removes both.
//
// A WRITE IS NEVER REPEATED. The endpoint is called once. If its answer is not
// a confirmed "opened" (a timeout, an unreadable body, outcome "unknown"), the
// run only READS GitHub to learn what happened, says so in the summary, and
// fails; it never posts again.
//
// NO MODEL, NO RUNNER: the model endpoint is a trap that counts and refuses,
// and the run asserts it was never called and no runner was ever registered.
// (Were a model ever to be exercised here it would have to be OpenRouter
// deepseek/deepseek-v4.1-flash in every role; this lane makes none.)
//
// THE TOKEN IS NEVER READ HERE. The product takes it from the signed-in gh
// through its own connect door and keeps it in the app's credential memory.
// This test reads only public facts through read-only `gh api` calls and,
// afterwards, scans the app home, the profile, the person's home, the wire
// frames, the endpoint's answer and the summary for a token's shape.
//
// Opt-in: CODEAF_LIVE_FACTORY_PIPELINE_PR=1 and CODEAF_LIVE_FACTORY_REPO=
// <owner/name>, a repository the signed-in account can push to. Optional:
// CODEAF_LIVE_FACTORY_PIPELINE_PR_SUMMARY=<absolute path outside the
// repository>, a JSON file written the moment a pull request is confirmed and
// rewritten at the end, so an artifact survives a later failed assertion.
// The pull request's URL is printed as soon as it is known.

const (
	livePROptIn      = "CODEAF_LIVE_FACTORY_PIPELINE_PR"
	livePRSummaryEnv = "CODEAF_LIVE_FACTORY_PIPELINE_PR_SUMMARY"
	livePRPostWait   = 120 * time.Second
	livePRMaxAnswer  = 1 << 20
)

var livePRTokenShape = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}`)

// ── the bounded GitHub transport ───────────────────────────────────────────

// livePRGuard passes every GET and exactly the three planned writes.
type livePRGuard struct {
	mu      sync.Mutex
	repo    string // lower-case owner/name
	branch  string
	base    string // the pinned default-branch tip
	def     string // the default branch
	title   string
	content string // exact reviewed proposal bytes, base64 encoded
	reads   int
	step    int // writes allowed so far: 0 none, 1 ref, 2 commit, 3 pull request
	allowed []string
	refused []string
	// codes holds the status GitHub answered to each allowed write, in order.
	codes []int
}

func (g *livePRGuard) client() *http.Client {
	return &http.Client{Transport: roundTripper(g.roundTrip)}
}

func (g *livePRGuard) roundTrip(r *http.Request) (*http.Response, error) {
	if r.Method == http.MethodGet {
		g.mu.Lock()
		g.reads++
		g.mu.Unlock()
		return http.DefaultTransport.RoundTrip(r)
	}
	var raw []byte
	if r.Body != nil {
		raw, _ = io.ReadAll(io.LimitReader(r.Body, 1<<20))
		r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	label, ok := g.permit(r, raw)
	if !ok {
		return nil, errors.New("the live pipeline lane refuses this GitHub write")
	}
	resp, err := http.DefaultTransport.RoundTrip(r)
	g.mu.Lock()
	g.allowed = append(g.allowed, label)
	if err != nil {
		g.codes = append(g.codes, 0)
	} else {
		g.codes = append(g.codes, resp.StatusCode)
	}
	g.mu.Unlock()
	return resp, err
}

// permit decides one write. The next planned write is the only one that passes.
func (g *livePRGuard) permit(r *http.Request, raw []byte) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	deny := func() (string, bool) {
		g.refused = append(g.refused, r.Method+" "+r.URL.Path)
		return "", false
	}
	if r.URL.Host != "api.github.com" || r.URL.RawQuery != "" {
		return deny()
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		return deny()
	}
	str := func(k string) string { s, _ := body[k].(string); return s }
	path := strings.ToLower(r.URL.Path)
	repoAt := "/repos/" + g.repo
	switch {
	case g.step == 0 && r.Method == http.MethodPost && path == repoAt+"/git/refs":
		if len(body) != 2 || str("ref") != "refs/heads/"+g.branch || str("sha") != g.base {
			return deny()
		}
		g.step = 1
		return "POST git/refs", true
	case g.step == 1 && r.Method == http.MethodPut && path == repoAt+"/contents/"+strings.ToLower(factory.PipelineFile):
		for k := range body {
			if k != "message" && k != "content" && k != "branch" && k != "sha" {
				return deny()
			}
		}
		if str("branch") != g.branch || str("message") != g.title || str("content") != g.content {
			return deny()
		}
		g.step = 2
		return "PUT contents", true
	case g.step == 2 && r.Method == http.MethodPost && path == repoAt+"/pulls":
		if len(body) != 4 || str("head") != g.branch || str("base") != g.def || str("title") != g.title || str("body") == "" {
			return deny()
		}
		g.step = 3
		return "POST pulls", true
	}
	return deny()
}

func (g *livePRGuard) snapshot() (reads int, allowed, refused []string, codes []int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.reads, append([]string(nil), g.allowed...), append([]string(nil), g.refused...), append([]int(nil), g.codes...)
}

// ── read-only gh ───────────────────────────────────────────────────────────

// livePRGHTry runs the signed-in gh and reports a failure instead of ending the test.
func livePRGHTry(args ...string) ([]byte, string, error) {
	cmd := exec.Command(liveSetupGH, args...)
	cmd.Env = append(os.Environ(), "GH_CONFIG_DIR="+liveSetupGHConfig, "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	return out, stderr.String(), err
}

// livePRRead is `gh api <path>`: never a method, field or input flag, so it can
// only be a GET. The path must name this lane's own surface.
func livePRRead(t *testing.T, path string, out any) (notFound bool) {
	t.Helper()
	if path != "user" && !strings.HasPrefix(path, "repos/") {
		t.Fatalf("livePRRead refuses %q", path)
	}
	data, stderr, err := livePRGHTry("api", path)
	if err != nil {
		if strings.Contains(stderr, "HTTP 404") {
			return true
		}
		t.Fatalf("gh api %s: %v: %.160s", strings.SplitN(path, "?", 2)[0], err, stderr)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("gh api %s: unreadable answer: %v", strings.SplitN(path, "?", 2)[0], err)
	}
	return false
}

// livePRLines is a paginated GET reduced by a jq filter to one string per row.
func livePRLines(t *testing.T, path, jq string) []string {
	t.Helper()
	if !strings.HasPrefix(path, "repos/") {
		t.Fatalf("livePRLines refuses %q", path)
	}
	data, stderr, err := livePRGHTry("api", "--paginate", path, "--jq", jq)
	if err != nil {
		t.Fatalf("gh api %s: %v: %.160s", strings.SplitN(path, "?", 2)[0], err, stderr)
	}
	var rows []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line != "" {
			rows = append(rows, line)
		}
	}
	sort.Strings(rows)
	return rows
}

type liveItem struct {
	Number    int    `json:"number"`
	State     string `json:"state"`
	UpdatedAt string `json:"updatedAt"`
	Title     string `json:"title"`
}

// liveRepoState is every fact the lane promises not to change.
type liveRepoState struct {
	Tip      string
	Issues   []liveItem
	Pulls    []liveItem
	Branches []string // "name\tsha"
}

func livePRState(t *testing.T, repo, def string) liveRepoState {
	t.Helper()
	var st liveRepoState
	var ref struct {
		Object struct {
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"object"`
	}
	livePRRead(t, "repos/"+repo+"/git/ref/heads/"+def, &ref)
	st.Tip = strings.ToLower(ref.Object.SHA)
	for _, kind := range []string{"issue", "pr"} {
		out := liveGH(t, kind, "list", "-R", repo, "--state", "all", "--limit", "5000", "--json", "number,state,updatedAt,title")
		var items []liveItem
		if err := json.Unmarshal(out, &items); err != nil {
			t.Fatalf("gh %s list: %v", kind, err)
		}
		sort.Slice(items, func(i, j int) bool { return items[i].Number < items[j].Number })
		if kind == "issue" {
			st.Issues = items
		} else {
			st.Pulls = items
		}
	}
	st.Branches = livePRLines(t, "repos/"+repo+"/branches?per_page=100", `.[] | .name + "\t" + .commit.sha`)
	return st
}

func withoutNumber(items []liveItem, n int) []liveItem {
	out := make([]liveItem, 0, len(items))
	for _, it := range items {
		if it.Number != n {
			out = append(out, it)
		}
	}
	return out
}

// ── the summary artifact ───────────────────────────────────────────────────

// livePRSummary holds only facts that are safe to attach: no token, login,
// name, avatar, machine or path.
type livePRSummary struct {
	mu                 sync.Mutex
	path               string
	Outcome            string            `json:"outcome"`
	HTTPStatus         int               `json:"http_status,omitempty"`
	EndpointOutcome    string            `json:"endpoint_outcome,omitempty"`
	EndpointStage      string            `json:"endpoint_stage,omitempty"`
	GitHubStatus       int               `json:"endpoint_github_status,omitempty"`
	PRURL              string            `json:"pr_url,omitempty"`
	PRNumber           int               `json:"pr_number,omitempty"`
	Branch             string            `json:"branch,omitempty"`
	BranchLeft         bool              `json:"branch_left,omitempty"`
	Base               string            `json:"base_branch,omitempty"`
	BaseSHA            string            `json:"base_sha,omitempty"`
	HeadSHA            string            `json:"head_sha,omitempty"`
	BudgetBefore       float64           `json:"default_budget_before,omitempty"`
	BudgetProposed     float64           `json:"default_budget_proposed,omitempty"`
	FileExisted        bool              `json:"pipeline_file_existed"`
	GitHubReads        int               `json:"github_reads"`
	GitHubWrites       []string          `json:"github_writes_allowed"`
	GitHubWriteStatus  []int             `json:"github_write_statuses"`
	GitHubWritesDenied []string          `json:"github_writes_refused"`
	ModelCalls         int               `json:"model_calls"`
	RunnersRegistered  int               `json:"runners_registered"`
	Checks             map[string]bool   `json:"checks"`
	Failures           []string          `json:"failures,omitempty"`
	Extra              map[string]string `json:"extra,omitempty"`
}

func (s *livePRSummary) save() {
	if s.path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil || livePRTokenShape.Match(data) {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, append(data, '\n'), 0o600) == nil {
		_ = os.Rename(tmp, s.path)
	}
}

// check records one named assertion and fails the test without ending it, so
// every post-condition is reported even when an earlier one failed.
func (s *livePRSummary) check(t *testing.T, name string, ok bool, format string, args ...any) {
	t.Helper()
	s.mu.Lock()
	s.Checks[name] = ok
	if !ok {
		s.Failures = append(s.Failures, name)
	}
	s.mu.Unlock()
	if !ok {
		t.Errorf("%s: %s", name, fmt.Sprintf(format, args...))
	}
}

// livePRSummaryPath validates the optional artifact path before anything runs.
func livePRSummaryPath(t *testing.T) string {
	t.Helper()
	p := strings.TrimSpace(os.Getenv(livePRSummaryEnv))
	if p == "" {
		return ""
	}
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		t.Fatalf("BLOCKED: %s must be a clean absolute path", livePRSummaryEnv)
	}
	if st, err := os.Stat(filepath.Dir(p)); err != nil || !st.IsDir() {
		t.Fatalf("BLOCKED: the folder of %s does not exist", livePRSummaryEnv)
	}
	if st, err := os.Lstat(p); err == nil && !st.Mode().IsRegular() {
		t.Fatalf("BLOCKED: %s names something that is not a regular file", livePRSummaryEnv)
	}
	// Outside this repository: walk up from the working directory to go.mod.
	dir, _ := os.Getwd()
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			if rel, err := filepath.Rel(d, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
				t.Fatalf("BLOCKED: %s must be outside the repository", livePRSummaryEnv)
			}
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return p
}

// ── the proposal ───────────────────────────────────────────────────────────

// livePRProposal is the repository's current pipeline with ONE explicit budget
// moved, or the defaults with it moved when the repository has none.
func livePRProposal(t *testing.T, repo, tip string) (text string, before, proposed float64, existed bool) {
	t.Helper()
	var current struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	file := pipeline.Defaults()
	var oldBytes []byte
	if livePRRead(t, "repos/"+repo+"/contents/"+factory.PipelineFile+"?ref="+tip, &current) {
		existed = false
	} else {
		if current.Type != "file" || current.Encoding != "base64" {
			t.Fatalf("BLOCKED: the pipeline file is not a plain file at the default tip")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(current.Content))
		if err != nil {
			t.Fatalf("BLOCKED: the pipeline file is not readable base64")
		}
		parsed, err := pipeline.Parse(string(raw))
		if err != nil || len(parsed.Problems) > 0 {
			t.Fatalf("BLOCKED: the repository's pipeline does not load (err %v, problems %d); the endpoint would refuse any proposal built on it", err, len(parsed.Problems))
		}
		file, oldBytes, existed = parsed, raw, true
	}
	before = file.DefaultBudget
	proposed = before + 0.25
	if before > 0.5 {
		proposed = before - 0.25
	}
	proposed = math.Round(proposed*100) / 100
	file.DefaultBudget = proposed
	data, err := pipeline.Encode(file)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if existed && bytes.Equal(oldBytes, data) {
		t.Fatalf("BLOCKED: the proposal equals the current file; nothing to open")
	}
	return string(data), before, proposed, existed
}

func livePRRandom(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// ── the test ───────────────────────────────────────────────────────────────

func TestLiveLocalServiceOpensPipelinePullRequestAsSignedInAccount(t *testing.T) {
	if os.Getenv(livePROptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 and %s=<owner/name>; it opens ONE real, unmerged pull request on that repository (see the header of this file)", livePROptIn, liveSetupRepoEnv)
	}
	if strings.Count(liveSetupRepo, "/") != 1 || strings.HasPrefix(liveSetupRepo, "/") || strings.HasSuffix(liveSetupRepo, "/") {
		t.Fatalf("BLOCKED: set %s to the owner/name of a repository this account can push to", liveSetupRepoEnv)
	}
	if liveSetupGH == "" || liveSetupGHConfig == "" {
		t.Fatal("BLOCKED: no gh on PATH or no gh configuration; not faking a pass")
	}
	t.Setenv("GH_DEBUG", "")
	t.Setenv("GIT_TRACE", "0")
	t.Setenv("GIT_CURL_VERBOSE", "0")
	sum := &livePRSummary{path: livePRSummaryPath(t), Outcome: "not_started", Checks: map[string]bool{}, Extra: map[string]string{}}
	t.Cleanup(sum.save)
	repo := liveSetupRepo

	// ── what GitHub says before ──────────────────────────────────────────────
	var me struct {
		Login string `json:"login"`
	}
	livePRRead(t, "user", &me)
	if me.Login == "" {
		t.Fatal("gh api user named no login")
	}
	var info struct {
		DefaultBranch string `json:"default_branch"`
		Archived      bool   `json:"archived"`
		FullName      string `json:"full_name"`
		Permissions   struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	livePRRead(t, "repos/"+repo, &info)
	def := info.DefaultBranch
	switch {
	case def == "":
		t.Fatal("BLOCKED: the repository names no default branch")
	case info.Archived || !info.Permissions.Push:
		t.Fatal("BLOCKED: the repository is archived or this account cannot push to it")
	}
	before := livePRState(t, repo, def)
	if !gitCommitShaShape.MatchString(before.Tip) {
		t.Fatalf("BLOCKED: default tip %q is not a commit SHA", before.Tip)
	}
	text, budgetBefore, budgetProposed, existed := livePRProposal(t, repo, before.Tip)
	proposal, err := pipeline.Parse(text)
	if err != nil || len(proposal.Problems) > 0 {
		t.Fatal("the proposed pipeline does not pass validation")
	}
	writeBytes, err := pipeline.Encode(proposal)
	if err != nil {
		t.Fatal("the proposed pipeline cannot be encoded")
	}
	sum.Base, sum.BaseSHA, sum.BudgetBefore, sum.BudgetProposed, sum.FileExisted = def, before.Tip, budgetBefore, budgetProposed, existed

	id := livePRRandom(t)
	branch := "codeaf-acceptance/pipeline-pr-" + time.Now().UTC().Format("20060102-150405") + "-" + id
	title := "Factory pipeline validation proposal (do not merge)"
	prBody := fmt.Sprintf("This isolated proposal validates opening a pipeline change as the signed-in GitHub user.\n\n"+
		"**Do not merge.** It proposes changing `default_budget` from %.2f to %.2f in `%s`. If the repository has no pipeline file, the proposal adds one using the current defaults with that budget change. Jobs keep using the current pipeline until a proposal is merged.", budgetBefore, budgetProposed, factory.PipelineFile)
	for _, s := range []string{branch, title, prBody} {
		if livePRTokenShape.MatchString(s) {
			t.Fatal("the proposal text looks like a credential")
		}
	}
	sum.Branch = branch
	exists := false
	var probe struct{}
	if !livePRRead(t, "repos/"+repo+"/git/ref/heads/"+branch, &probe) {
		exists = true
	}
	if exists {
		t.Fatalf("BLOCKED: branch %q already exists", branch)
	}

	// ── isolation: the app home, HOME and profile are this test's own ────────
	appHome, userHome, decoy := useLocalHome(t)
	var dirs []string
	for _, tool := range []string{"git", "gh"} {
		if p, err := exec.LookPath(tool); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	dirs = append(dirs, filepath.Dir(liveSetupGH))
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
	t.Setenv("GH_CONFIG_DIR", liveSetupGHConfig)
	t.Setenv("GH_PROMPT_DISABLED", "1")
	t.Setenv(factoryPostureEnv, "")
	// NO MODEL: any request to a model endpoint is counted, and refused.
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	var modelMu sync.Mutex
	modelHits := 0
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelMu.Lock()
		modelHits++
		modelMu.Unlock()
		http.Error(w, "this lane makes no model call", http.StatusServiceUnavailable)
	}))
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)

	w := newFactoryWire(t, testFactoryHub(t))
	guard := &livePRGuard{repo: strings.ToLower(repo), branch: branch, base: before.Tip, def: def, title: title, content: base64.StdEncoding.EncodeToString(writeBytes)}
	w.backend.local.githubHTTP = guard.client()
	s := &localSetup{factoryWire: w, home: appHome, userHm: userHome}

	// ── 1. the app's own service, with the signed-in gh connected ────────────
	st := s.start()
	if st.State != appwire.LocalRunning || st.Authority != appwire.LocalAuthorityApp || st.Runner {
		t.Fatalf("start: %+v", st)
	}
	host := w.backend.local.host
	if host == nil || !host.quiet || host.address == "" || host.token == "" {
		t.Fatal("the local host is missing or not quiet")
	}
	v := s.setup("factory.setup_connect", map[string]any{"source": "gh"})
	if v.State != "repos" || !v.GitHub.Connected {
		t.Fatalf("connect: state %q connected %v error %q", v.State, v.GitHub.Connected, v.GitHub.Error)
	}
	if v.GitHub.Handle != factory.NormalizeHandle(me.Login) {
		t.Fatal("the product's connected account is not the account gh is signed in as")
	}
	// The repository is selected through the real setup door before proposing
	// its pipeline. Issue intake stays in Backlog; no checkout is needed here.
	s.setup("factory.setup_select", map[string]any{"repos": []string{repo}})
	deadline := time.Now().Add(liveSetupWait)
	for v = s.setup("factory.setup_read", map[string]any{}); v.State != "checkouts"; v = s.setup("factory.setup_read", map[string]any{}) {
		if time.Now().After(deadline) {
			t.Fatal("repository intake did not reach checkout setup")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if selected := repoOf(v, repo); !selected.Selected || selected.Read != "done" {
		t.Fatal("the chosen repository was not read through setup")
	}
	var queueBefore api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &queueBefore)
	for _, job := range queueBefore.Jobs {
		if job.State != api.StateBacklog {
			t.Fatal("setup started a job before the proposal")
		}
	}

	// ── 2. ONE call to the real endpoint. Never repeated. ────────────────────
	reqBody, _ := json.Marshal(api.PipelinePRRequest{Text: text, Title: title, Body: prBody, Branch: branch, ExpectedBaseSHA: before.Tip})
	req, err := http.NewRequest(http.MethodPost, host.address+"/v1/repos/"+repo+"/pipeline/open-pr", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+host.token)
	req.Header.Set("Content-Type", "application/json")
	sum.Outcome = "request_sent"
	sum.save()
	client := http.Client{Timeout: livePRPostWait, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, postErr := client.Do(req)
	var answer []byte
	if postErr == nil {
		sum.HTTPStatus = resp.StatusCode
		answer, _ = io.ReadAll(io.LimitReader(resp.Body, livePRMaxAnswer))
		resp.Body.Close()
	}
	if livePRTokenShape.Match(answer) {
		t.Error("the endpoint's answer carries something shaped like a GitHub token")
		answer = nil
	}

	// ── 3. the outcome, confirmed or learned by READING only ─────────────────
	var opened api.PipelinePRResponse
	var failure api.PipelinePRFailure
	confirmed := false
	if postErr == nil && sum.HTTPStatus == http.StatusCreated && json.Unmarshal(answer, &opened) == nil &&
		opened.Outcome == api.PipelinePROpened && opened.PR != nil && opened.PR.Number > 0 {
		confirmed = true
	} else if postErr == nil && json.Unmarshal(answer, &failure) == nil {
		sum.EndpointOutcome, sum.EndpointStage, sum.GitHubStatus = failure.Outcome, failure.Stage, failure.GitHubStatus
		sum.BranchLeft = failure.BranchLeft
	}
	prNumber, prURL := 0, ""
	if confirmed {
		prNumber, prURL = opened.PR.Number, opened.PR.URL
		sum.Outcome, sum.EndpointOutcome = "opened", opened.Outcome
		sum.HeadSHA = opened.PR.HeadSHA
	} else {
		// Unknown or failed: do not post again. Ask GitHub whether the pull request exists.
		sum.Outcome = "not_confirmed"
		if postErr != nil {
			sum.Extra["transport"] = "the call to the local service did not complete"
		}
		out, _, err := livePRGHTry("pr", "list", "-R", repo, "--head", branch, "--state", "all", "--json", "number,url")
		var found []struct {
			Number int    `json:"number"`
			URL    string `json:"url"`
		}
		if err == nil && json.Unmarshal(out, &found) == nil && len(found) == 1 {
			prNumber, prURL = found[0].Number, found[0].URL
			sum.Outcome = "found_after_unconfirmed_answer"
		}
		var ref struct{}
		sum.BranchLeft = sum.BranchLeft || !livePRRead(t, "repos/"+repo+"/git/ref/heads/"+branch, &ref)
		t.Errorf("the endpoint did not confirm a pull request: http %d, outcome %q, stage %q, github %d, branch_left %v, found_on_github %v",
			sum.HTTPStatus, sum.EndpointOutcome, sum.EndpointStage, sum.GitHubStatus, sum.BranchLeft, prNumber > 0)
	}
	if prNumber > 0 {
		sum.PRNumber, sum.PRURL = prNumber, prURL
		sum.save()
		// The URL is safe to share and is printed before any further assertion.
		t.Logf("PIPELINE_PR_URL=%s", prURL)
		fmt.Fprintf(os.Stdout, "PIPELINE_PR_URL=%s\n", prURL)
	}
	reads, allowed, refused, codes := guard.snapshot()
	sum.GitHubReads, sum.GitHubWrites, sum.GitHubWriteStatus, sum.GitHubWritesDenied = reads, allowed, codes, refused
	sum.save()

	// ── 4. what the lane promises, checked at GitHub and in the product ──────
	wantWrites := []string{"POST git/refs", "PUT contents", "POST pulls"}
	sum.check(t, "github_writes_exactly_the_three_planned", reflect.DeepEqual(allowed, wantWrites) && len(refused) == 0,
		"allowed %v, refused %v", allowed, refused)
	sum.check(t, "reads_went_through_the_real_transport", reads > 0, "no GitHub read was seen")

	modelMu.Lock()
	calls := modelHits
	modelMu.Unlock()
	var runners api.Runners
	s.must("factory.runners", map[string]any{"factory": st.Factory}, &runners)
	sum.ModelCalls, sum.RunnersRegistered = calls, len(runners.Runners)
	sum.check(t, "zero_model_calls", calls == 0, "%d model requests", calls)
	sum.check(t, "zero_runners", len(runners.Runners) == 0 && !w.backend.local.runner && w.backend.local.runnerDone == nil,
		"registered %d, flag %v", len(runners.Runners), w.backend.local.runner)
	var queueAfter api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &queueAfter)
	unchangedJobs := len(queueAfter.Jobs) == len(queueBefore.Jobs)
	prior := map[int]api.Job{}
	for _, job := range queueBefore.Jobs {
		prior[job.ID] = job
	}
	for _, job := range queueAfter.Jobs {
		old, exists := prior[job.ID]
		unchangedJobs = unchangedJobs && exists && job.State == api.StateBacklog && job.Budget == old.Budget
	}
	sum.check(t, "existing_jobs_remain_in_backlog_with_same_budgets", unchangedJobs, "the local jobs changed during a pipeline proposal")

	after := livePRState(t, repo, def)
	sum.check(t, "default_tip_unchanged", after.Tip == before.Tip, "default branch tip moved")
	if prNumber > 0 {
		var pr struct {
			Title   string `json:"title"`
			State   string `json:"state"`
			Merged  bool   `json:"merged"`
			Commits int    `json:"commits"`
			Changed int    `json:"changed_files"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			Base struct {
				Ref string `json:"ref"`
			} `json:"base"`
			Head struct {
				Ref  string `json:"ref"`
				SHA  string `json:"sha"`
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
		}
		livePRRead(t, fmt.Sprintf("repos/%s/pulls/%d", repo, prNumber), &pr)
		sum.HeadSHA = strings.ToLower(pr.Head.SHA)
		sum.check(t, "pr_author_is_signed_in_account", strings.EqualFold(pr.User.Login, me.Login), "the pull request was authored by a different account")
		sum.check(t, "pr_is_open_unmerged_on_new_branch", pr.State == "open" && !pr.Merged && pr.Head.Ref == branch && pr.Base.Ref == def && strings.EqualFold(pr.Head.Repo.FullName, repo),
			"state %q merged %v head %q base %q", pr.State, pr.Merged, pr.Head.Ref, pr.Base.Ref)
		sum.check(t, "pr_title_is_ours", pr.Title == title, "title differs")
		sum.check(t, "pr_is_one_commit_one_file", pr.Commits == 1 && pr.Changed == 1, "commits %d changed_files %d", pr.Commits, pr.Changed)
		files := livePRLines(t, fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100", repo, prNumber), ".[] | .filename")
		sum.check(t, "changed_files_only_pipeline_yaml", reflect.DeepEqual(files, []string{factory.PipelineFile}), "%d changed paths", len(files))

		// The committed file is exactly the proposal the product normalised, with
		// the one budget moved and nothing else about the budget in play.
		var landed struct {
			Content string `json:"content"`
		}
		livePRRead(t, "repos/"+repo+"/contents/"+factory.PipelineFile+"?ref="+pr.Head.SHA, &landed)
		raw, _ := base64.StdEncoding.DecodeString(strings.NewReplacer("\n", "", "\r", "").Replace(landed.Content))
		parsed, perr := pipeline.Parse(string(raw))
		sum.check(t, "committed_pipeline_has_the_proposed_budget", perr == nil && len(parsed.Problems) == 0 && parsed.DefaultBudget == budgetProposed,
			"parse error %v, budget %v want %v", perr, parsed.DefaultBudget, budgetProposed)
		// The endpoint parses and re-encodes what it is sent, so the expected bytes
		// are the same two steps applied here.
		normalised, _ := pipeline.Parse(text)
		wantBytes, _ := pipeline.Encode(normalised)
		sum.check(t, "committed_pipeline_is_the_proposal", bytes.Equal(raw, wantBytes), "committed bytes differ from the proposal")

		sum.check(t, "branches_only_gained_the_new_one", reflect.DeepEqual(after.Branches, livePRMerge(before.Branches, branch+"\t"+strings.ToLower(pr.Head.SHA))),
			"branch list differs from before plus the new branch")
		sum.check(t, "issues_unchanged", reflect.DeepEqual(before.Issues, withoutNumber(after.Issues, prNumber)), "issue snapshot differs")
		sum.check(t, "pull_requests_unchanged_excluding_new", reflect.DeepEqual(before.Pulls, withoutNumber(after.Pulls, prNumber)),
			"pull request snapshot differs (before %d, after %d)", len(before.Pulls), len(after.Pulls))
	}

	// ── 5. no credential reached a file, a frame, or the artifact ────────────
	for _, dir := range []string{appHome, userHome, decoy} {
		for path, data := range snapshotTree(t, dir) {
			if livePRTokenShape.MatchString(data) {
				t.Errorf("%s holds something shaped like a GitHub token", path)
			}
		}
	}
	for _, line := range w.lines {
		if livePRTokenShape.MatchString(line) {
			t.Error("a wire frame carries something shaped like a GitHub token")
			break
		}
	}
	if sum.Outcome == "opened" && len(sum.Failures) == 0 && !t.Failed() {
		sum.Outcome = "passed"
	}
	sum.save()
	t.Logf("live pipeline pr: outcome=%s pr_number=%d github_reads=%d writes_allowed=%d writes_refused=%d write_statuses=%v model_calls=%d runners=%d branch_left=%v",
		sum.Outcome, prNumber, reads, len(allowed), len(refused), codes, calls, len(runners.Runners), sum.BranchLeft)

	// Nothing is closed, merged or deleted. The engine alone is stopped.
	w.backend.Close()
}

// gitCommitShaShape is a full commit SHA.
var gitCommitShaShape = regexp.MustCompile(`^[0-9a-f]{40}$`)

// livePRMerge is rows plus one, sorted the way livePRLines sorts.
func livePRMerge(rows []string, extra string) []string {
	out := append(append([]string(nil), rows...), extra)
	sort.Strings(out)
	return out
}
