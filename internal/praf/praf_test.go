package praf

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/agentsession"
	secappx "github.com/Agent-Field/codeaf/internal/agentsession/appx"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/praf/appx"
	"github.com/Agent-Field/codeaf/internal/praf/config"
	"github.com/Agent-Field/codeaf/internal/praf/orch"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// testHost is codeaf's side of a run as a test sees it: the folder, the
// ceilings and the model API it handed over, the record folder, and every
// record in order.
type testHost struct {
	workspace string
	records   string
	ceilings  delegate.Ceilings
	api       delegate.ModelAPI

	mu      sync.Mutex
	hello   []string
	stages  []delegate.StageRecord
	steps   []delegate.StepRecord
	endings []delegate.Ending
}

func newTestHost(t *testing.T) *testHost {
	return &testHost{workspace: t.TempDir(), records: t.TempDir(),
		api: delegate.ModelAPI{BaseURL: "http://127.0.0.1:1/v1", Token: "run-token"}}
}

func (h *testHost) Workspace() string           { return h.workspace }
func (h *testHost) Records() string             { return h.records }
func (h *testHost) Ceilings() delegate.Ceilings { return h.ceilings }
func (h *testHost) Models() delegate.ModelAPI   { return h.api }
func (h *testHost) Hello(stages []string)       { h.mu.Lock(); h.hello = stages; h.mu.Unlock() }
func (h *testHost) Stage(s delegate.StageRecord) {
	h.mu.Lock()
	h.stages = append(h.stages, s)
	h.mu.Unlock()
}
func (h *testHost) Step(s delegate.StepRecord) {
	h.mu.Lock()
	h.steps = append(h.steps, s)
	h.mu.Unlock()
}
func (h *testHost) Terminal(e delegate.Ending) {
	h.mu.Lock()
	h.endings = append(h.endings, e)
	h.mu.Unlock()
}

var _ delegate.Recorder = (*testHost)(nil)

// fakeSessions stands in for sec's agent sessions.
type fakeSessions struct {
	cost   float64
	failed string
	err    error
}

func (f *fakeSessions) Harness(context.Context, string, map[string]any, any, secappx.HarnessOptions) (*secappx.HarnessResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.failed != "" {
		return &secappx.HarnessResult{IsError: true, ErrorMessage: f.failed}, nil
	}
	return &secappx.HarnessResult{}, nil
}
func (f *fakeSessions) AI(context.Context, string, ...ai.Option) (*ai.Response, error) {
	return &ai.Response{}, nil
}
func (f *fakeSessions) Spent() (float64, int, int) { return f.cost, 3, 4 }

// fakeGitHub stands in for GitHub.
type fakeGitHub struct {
	posted []schemas.GitHubReview
	refuse error
}

func (g *fakeGitHub) ParsePRURL(string) (string, string, int, error) { return "", "", 0, nil }
func (g *fakeGitHub) FetchPR(context.Context, string, string, int) (schemas.GitHubPRData, error) {
	return schemas.GitHubPRData{}, nil
}
func (g *fakeGitHub) PostReview(_ context.Context, _, _ string, _ int, review schemas.GitHubReview, _ string) (map[string]any, error) {
	if g.refuse != nil {
		return nil, g.refuse
	}
	g.posted = append(g.posted, review)
	return map[string]any{"id": 1}, nil
}
func (g *fakeGitHub) OpenPullRequest(context.Context, string, string, string) (int, error) {
	return 0, nil
}

func sampleResult() schemas.ReviewResult {
	suggestion := "check the error before using the value"
	return schemas.ReviewResult{
		ReviewID: "rev_test",
		PrURL:    "https://github.com/o/r/pull/7",
		Review: schemas.GitHubReview{Body: "summary", Event: "REQUEST_CHANGES",
			Comments: []schemas.GitHubComment{{Path: "api/handler.go", Line: 40, Body: "nil"}}},
		Findings: []schemas.ScoredFinding{
			{Title: "Nitpick on naming", Severity: "nitpick", FilePath: "/tmp/co/util.go", LineStart: 3, Score: 0.9},
			{Title: "Nil dereference on error path, verified by the reviewer", Severity: "important", FilePath: "/tmp/co/api/handler.go",
				LineStart: 40, LineEnd: 44, Blocking: true, BlockingReason: "crashes on every failed lookup",
				Body: "The value is used before err is checked.", Suggestion: &suggestion, Evidence: "v := lookup()\nv.Name", Confidence: 0.8},
		},
		Summary: schemas.ReviewSummary{
			TotalFindings: 2, BlockingCount: 1, AdvisoryCount: 1,
			BySeverity: map[string]int{"important": 1, "nitpick": 1}, DimensionsRun: 3,
		},
		Metadata: schemas.ReviewMetadata{Intake: map[string]any{"pr_summary": "Adds a lookup endpoint."}},
	}
}

// captured is what a stand-in review saw.
type captured struct {
	in     schemas.ReviewInput
	cfg    config.ReviewConfig
	access orch.Access
	conf   agentsession.Config
}

// testReviewer wires every seam to a stand-in; review is the pipeline.
func testReviewer(sessions *fakeSessions, review func(ctx context.Context, deps orch.Deps) (schemas.ReviewResult, error)) (reviewer, *captured) {
	c := &captured{}
	return reviewer{
		token: func(context.Context) string { return "gh-token" },
		gh:    func(string) gitHub { return &fakeGitHub{} },
		resolveRepo: func(_ context.Context, access orch.Access, _ string) (string, error) {
			c.access = access
			return "/tmp/co", nil
		},
		sessions: func(_ delegate.ModelAPI, conf agentsession.Config) (sessionApp, error) {
			c.conf = conf
			return sessions, nil
		},
		review: func(ctx context.Context, deps orch.Deps, in schemas.ReviewInput, cfg config.ReviewConfig) (schemas.ReviewResult, *schemas.GitHubPRData, error) {
			c.in, c.cfg = in, cfg
			result, err := review(ctx, deps)
			return result, &schemas.GitHubPRData{Owner: "o", Repo: "r", Number: 7, Title: "Add lookup", HeadSHA: "abc123"}, err
		},
		branchPR: func(context.Context, string, gitHub) (Target, error) {
			return Target{Owner: "o", Repo: "r", Number: 7}, nil
		},
		workdir: func() (string, func(), error) { return "/tmp/work", func() {}, nil },
	}, c
}

func defaults() options {
	return options{sessions: 8, maxTurns: 50}
}

func TestProgramIsValid(t *testing.T) {
	if err := Program.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, stage := range Stages {
		if stageWords[stage] == "" || stepWords[stage] == "" {
			t.Errorf("stage %q has no words", stage)
		}
	}
	for _, ph := range phases {
		if stageWords[ph.stage] == "" {
			t.Errorf("a phase's stage %q is not one of Stages", ph.stage)
		}
	}
}

func TestCrewFlagsKeepTheCrewsSpelling(t *testing.T) {
	got := strings.Join(crewFlags(delegate.Crew{Hands: "moonshotai/kimi-k2", Light: "z-ai/glm-5"}), " ")
	if got != "--model moonshotai/kimi-k2 --light z-ai/glm-5" {
		t.Errorf("crew flags = %q", got)
	}
	got = strings.Join(crewFlags(delegate.Crew{Hands: "a/b", Asked: []string{"c/d", "e/f"}}), " ")
	if got != "--model c/d" {
		t.Errorf("asked crew flags = %q, want the first model asked for", got)
	}
	fs := flag.NewFlagSet("pr run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	runCommand.Bind(fs)
	if err := fs.Parse(crewFlags(delegate.Crew{Hands: "a/b", Light: "c/d"})); err != nil || fs.NArg() != 0 {
		t.Errorf("crew flags do not parse: %v", err)
	}
}

func TestTitles(t *testing.T) {
	for brief, want := range map[string]string{
		"https://github.com/o/r/pull/7 focus on retries": "Code review of o/r#7",
		"o/r#9":                  "Code review of o/r#9",
		"current branch":         "Code review of this branch's pull request",
		"post /x/pr-report.json": "Posting a code review to GitHub",
	} {
		if got := title(brief); got != want {
			t.Errorf("title(%q) = %q, want %q", brief, got, want)
		}
	}
}

func TestReviewHandsBackTheAccountAndTheReport(t *testing.T) {
	r, c := testReviewer(&fakeSessions{cost: 0.42}, func(ctx context.Context, deps orch.Deps) (schemas.ReviewResult, error) {
		return sampleResult(), nil
	})
	host := newTestHost(t)
	host.ceilings = delegate.Ceilings{CostUSD: 5, Hours: 2}
	request := ReadBrief("https://github.com/o/r/pull/7 focus on error handling", nil)
	end := runReview(context.Background(), host, request, defaults(), io.Discard, r)
	if end.Status != delegate.StatusPass {
		t.Fatalf("status = %q (%s)", end.Status, end.Message)
	}
	lines := strings.Split(end.Deliverable, "\n")
	if want := "Review of o/r#7 (Add lookup): 2 findings, 1 blocking the merge (1 important, 1 nitpick)."; lines[0] != want {
		t.Errorf("first line = %q\nwant         %q", lines[0], want)
	}
	for _, want := range []string{
		"- important · blocking · Nil dereference on error path, checked by the reviewer · api/handler.go:40-44",
		"  fix: check the error before using the value",
		"Nothing was posted to GitHub.",
		"Full report: " + filepath.Join(host.records, reportMarkdown),
	} {
		if !strings.Contains(end.Deliverable, want) {
			t.Errorf("account lacks %q:\n%s", want, end.Deliverable)
		}
	}
	if strings.Contains(strings.ToLower(end.Deliverable), "verified") || strings.Contains(end.Deliverable, "/tmp/co/") {
		t.Errorf("account says a banned word or the checkout path:\n%s", end.Deliverable)
	}
	if end.CostUSD != 0.42 || end.Extra["findings"] != 2 || end.Extra["blocking"] != 1 {
		t.Errorf("ending = $%v %v", end.CostUSD, end.Extra)
	}

	// The pipeline input: a dry run of the named pull request in the private
	// checkout, the focus as guidance, the gate under the ceilings.
	if c.in.PrURL == nil || *c.in.PrURL != "https://github.com/o/r/pull/7" || !c.in.DryRun || *c.in.RepoPath != "/tmp/co" {
		t.Errorf("input = %+v", c.in)
	}
	if strings.Join(c.cfg.Hints, "|") != "focus on error handling" || c.cfg.Budget.MaxCostUSD != 4 {
		t.Errorf("hints %q, gate $%v", c.cfg.Hints, c.cfg.Budget.MaxCostUSD)
	}
	if c.access != (orch.Access{Workdir: "/tmp/work", Token: "gh-token"}) || c.conf.Root != "/tmp/co" {
		t.Errorf("access %+v, sessions on %q", c.access, c.conf.Root)
	}
	if c.conf.Work != "a code review" {
		t.Errorf("sessions are told their work is %q, want a code review", c.conf.Work)
	}

	// The report: the review to read, and the review to post.
	md, err := os.ReadFile(filepath.Join(host.records, reportMarkdown))
	if err != nil || !strings.Contains(string(md), "### 1. [important, blocking]") || !strings.Contains(string(md), "Adds a lookup endpoint.") {
		t.Errorf("markdown report: %v\n%s", err, md)
	}
	saved, _, err := readSaved(host.records)
	if err != nil {
		t.Fatal(err)
	}
	if saved.PullRequest.HeadSHA != "abc123" || saved.Review.Review.Event != "REQUEST_CHANGES" || saved.Focus != "focus on error handling" {
		t.Errorf("saved = %+v", saved.PullRequest)
	}
}

func TestABareReviewFindsTheBranchsPullRequest(t *testing.T) {
	r, c := testReviewer(&fakeSessions{}, func(context.Context, orch.Deps) (schemas.ReviewResult, error) {
		return schemas.ReviewResult{Summary: schemas.ReviewSummary{BySeverity: map[string]int{}}}, nil
	})
	end := runReview(context.Background(), newTestHost(t), ReadBrief("current branch", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusPass || !strings.HasPrefix(end.Deliverable, "Review of o/r#7 (Add lookup): no findings.") {
		t.Errorf("ending = %q %q", end.Status, end.Deliverable)
	}
	if len(c.cfg.Hints) != 0 {
		t.Errorf("hints = %q, want none for a bare review", c.cfg.Hints)
	}
	r.branchPR = func(context.Context, string, gitHub) (Target, error) {
		return Target{}, errors.New("the branch fix-it has no open pull request on o/r")
	}
	end = runReview(context.Background(), newTestHost(t), ReadBrief("", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusFail || !strings.Contains(end.Message, "has no open pull request") || !strings.Contains(end.Message, "/pr owner/repo#123") {
		t.Errorf("ending = %q %q", end.Status, end.Message)
	}
}

func TestEveryFailedSessionIsNotAReview(t *testing.T) {
	r, _ := testReviewer(&fakeSessions{failed: "the model refused"}, func(ctx context.Context, deps orch.Deps) (schemas.ReviewResult, error) {
		for i := 0; i < 2; i++ {
			_, _ = deps.App.Harness(ctx, "review", nil, nil, appxOptions())
		}
		return schemas.ReviewResult{Summary: schemas.ReviewSummary{BySeverity: map[string]int{}}}, nil
	})
	end := runReview(context.Background(), newTestHost(t), ReadBrief("o/r#7", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusFail || !strings.Contains(end.Message, "every agent session failed (2 of 2)") {
		t.Errorf("ending = %q %q", end.Status, end.Message)
	}
}

func TestATimeCeilingCutSaysWhereItStopped(t *testing.T) {
	r, _ := testReviewer(&fakeSessions{}, func(ctx context.Context, deps orch.Deps) (schemas.ReviewResult, error) {
		_, _ = deps.Local.CallLocal(ctx, "review_dimension", map[string]any{"target_files": []any{"a.go"}})
		<-ctx.Done()
		return sampleResult(), nil
	})
	was := reportReserve
	reportReserve = 50 * time.Millisecond
	t.Cleanup(func() { reportReserve = was })
	host := newTestHost(t)
	host.ceilings = delegate.Ceilings{Hours: (200 * time.Millisecond).Hours()}
	r.review = wrapReview(r.review, host)
	end := runReview(context.Background(), host, ReadBrief("o/r#7", nil), defaults(), io.Discard, r)
	if end.Status != delegate.StatusPass || !strings.Contains(end.Message, "before its time ceiling cut it short while reviewing") {
		t.Errorf("message = %q", end.Message)
	}
	if !strings.Contains(end.Deliverable, "It reached its time ceiling") || end.Extra["cut_at"] != "reviewing" {
		t.Errorf("account does not say where it was cut:\n%s", end.Deliverable)
	}
}

// wrapReview runs the stand-in review with a tracker whose reasoners return
// at once, so the review reaches the stage the test names.
func wrapReview(review func(context.Context, orch.Deps, schemas.ReviewInput, config.ReviewConfig) (schemas.ReviewResult, *schemas.GitHubPRData, error), host delegate.Host) func(context.Context, orch.Deps, schemas.ReviewInput, config.ReviewConfig) (schemas.ReviewResult, *schemas.GitHubPRData, error) {
	return func(ctx context.Context, deps orch.Deps, in schemas.ReviewInput, cfg config.ReviewConfig) (schemas.ReviewResult, *schemas.GitHubPRData, error) {
		tracker := deps.Local.(*phaseTracker)
		tracker.handlers = map[string]handler{"review_dimension": func(context.Context, map[string]any) (any, error) {
			return map[string]any{"findings": []any{}}, nil
		}}
		return review(ctx, deps, in, cfg)
	}
}

func TestFailuresEndByTheirCause(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status string
		says   string
	}{
		{agentsession.ErrCeiling, delegate.StatusBudget, "dollar ceiling"},
		{orch.ErrBadInput, delegate.StatusFail, "could not review o/r#7"},
		{errors.New("boom"), delegate.StatusFail, "boom"},
	} {
		r, _ := testReviewer(&fakeSessions{}, func(context.Context, orch.Deps) (schemas.ReviewResult, error) {
			return schemas.ReviewResult{}, tc.err
		})
		end := runReview(context.Background(), newTestHost(t), ReadBrief("o/r#7", nil), defaults(), io.Discard, r)
		if end.Status != tc.status || !strings.Contains(end.Message, tc.says) {
			t.Errorf("%v: ending = %q %q", tc.err, end.Status, end.Message)
		}
	}
}

func TestPostSendsTheSavedReview(t *testing.T) {
	host := newTestHost(t)
	r, _ := testReviewer(&fakeSessions{}, func(context.Context, orch.Deps) (schemas.ReviewResult, error) { return sampleResult(), nil })
	runReview(context.Background(), host, ReadBrief("o/r#7", nil), defaults(), io.Discard, r)

	t.Setenv("GH_TOKEN", "gh-token")
	gh := &fakeGitHub{}
	end := runPost(context.Background(), newTestHost(t), filepath.Join(host.records, reportJSON), func(token string) gitHub { return gh })
	if end.Status != delegate.StatusPass || end.Message != "Posted the review to o/r#7 as a request for changes with 1 inline comment" {
		t.Errorf("ending = %q %q", end.Status, end.Message)
	}
	if len(gh.posted) != 1 || gh.posted[0].Body != "summary" {
		t.Errorf("posted = %+v", gh.posted)
	}

	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	was := ghLookPath
	ghLookPath = func(string) (string, error) { return "", errors.New("absent") }
	t.Cleanup(func() { ghLookPath = was })
	end = runPost(context.Background(), newTestHost(t), host.records, func(string) gitHub { return gh })
	if end.Status != delegate.StatusFail || !strings.Contains(end.Message, "needs a GitHub token") {
		t.Errorf("tokenless post = %q %q", end.Status, end.Message)
	}
	if end := runPost(context.Background(), newTestHost(t), t.TempDir(), func(string) gitHub { return gh }); end.Status != delegate.StatusFail {
		t.Errorf("a folder with no review posted: %q", end.Message)
	}
}

func TestThePageSaysWhatEachStepFound(t *testing.T) {
	host := newTestHost(t)
	tracker := newPhaseTracker(host, map[string]handler{
		"review_dimension": func(context.Context, map[string]any) (any, error) {
			return map[string]any{"findings": []map[string]any{{"title": "Race on the cache"}, {"title": "Missing verdict check"}}}, nil
		},
		"coverage_gate": func(context.Context, map[string]any) (any, error) { return nil, errors.New("gate down") },
	})
	ctx := context.Background()
	_, _ = tracker.CallLocal(ctx, "review_dimension", map[string]any{"target_files": []string{"a.go", "b.go", "c.go"}})
	_, _ = tracker.CallLocal(ctx, "review_dimension", map[string]any{"target_files": []string{"d.go"}})
	_, _ = tracker.CallLocal(ctx, "coverage_gate", nil)
	read := presentActions()
	var got []string
	for _, s := range host.stages {
		if shown, ok := read(delegate.Action{Kind: delegate.ActionStage, Stage: s.Stage, Status: s.Status, Data: s.Data}); ok {
			got = append(got, shown.Step+" | "+shown.Text)
		}
	}
	for _, s := range host.steps {
		shown, _ := read(delegate.Action{Kind: delegate.ActionStep, Tool: s.Tool, Step: s.Step, Command: s.Command, Observation: s.Observation})
		got = append(got, shown.Step+" | "+shown.Text+" | "+shown.Outcome+" | "+shown.Detail)
	}
	want := []string{
		"review | reviewing",
		"coverage | looking for gaps",
		"review | reviewed a.go, b.go and 1 more | 2 findings | 2 findings: Race on the cache; Missing decision check",
		"review | reviewed d.go | 2 findings | 2 findings: Race on the cache; Missing decision check",
		"coverage | looked for gaps | failed | error: gate down",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("page:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	raw, _ := json.Marshal(host.stages[len(host.stages)-1])
	if strings.Contains(string(raw), "verdict") {
		t.Errorf("a record says a banned word: %s", raw)
	}
}

func TestPlainWordsKeepsCodeSpans(t *testing.T) {
	if got := plainWords("Verdict: the `verdict` field was verified"); got != "Decision: the `verdict` field was checked" {
		t.Errorf("plainWords = %q", got)
	}
}

func appxOptions() appx.HarnessOptions { return appx.HarnessOptions{Label: "reviewer"} }

// TestASessionErrorIsOneFailedReviewer: an error the session loop answers for
// one session — a reply with no choices — is that reviewer failing, which the
// pipeline degrades and counts, not the end of the review; only the run's
// ceiling, a refused key or a stop ends every call.
func TestASessionErrorIsOneFailedReviewer(t *testing.T) {
	ctx := context.Background()
	review := &app{sessions: &fakeSessions{err: errors.New("the model's reply has no choices")}}
	result, err := review.Harness(ctx, "review", nil, nil, appxOptions())
	if err != nil || result == nil || !result.IsError || result.ErrorMessage != "the model's reply has no choices" {
		t.Errorf("result %+v, err %v: want a failed result and no error", result, err)
	}
	if stats := review.stats(); stats.SessionFailed != 1 || stats.SessionTotal != 1 {
		t.Errorf("stats = %+v", stats)
	}
	for _, ends := range []error{agentsession.ErrCeiling, &agentsession.RefusedError{Code: 401, Message: "bad key"}, context.Canceled} {
		review := &app{sessions: &fakeSessions{err: ends}}
		if _, err := review.Harness(ctx, "review", nil, nil, appxOptions()); err == nil {
			t.Errorf("%v did not end the run", ends)
		}
	}
}
