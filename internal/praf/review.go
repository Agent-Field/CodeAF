package praf

// One review, from the brief to its ending.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/agentsession"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/praf/afx"
	"github.com/Agent-Field/codeaf/internal/praf/config"
	"github.com/Agent-Field/codeaf/internal/praf/github"
	"github.com/Agent-Field/codeaf/internal/praf/orch"
	"github.com/Agent-Field/codeaf/internal/praf/reasoners"
	"github.com/Agent-Field/codeaf/internal/praf/schemas"
)

// softFraction is where the pipeline's own budget gate is set, as a share of
// the run's dollar ceiling. The gate stops STARTING work — further reviewers,
// the challenge and coverage passes — and the review finishes with what it
// has; calls already in flight still land and still cost, so a gate at the
// ceiling itself would let them carry the run over it, where the model API
// refuses every call and the review has nothing to hand back.
const softFraction = 0.8

// sessionWork is what every agent session of a review is one agent of, in
// the loop's system prompt: "You are one agent of a code review of the
// repository at …".
const sessionWork = "a code review"

// reportReserve is the time kept back from the run's wall for writing the
// review down, so a review cut by its hours still says what it found. A
// variable only so a test can shorten it.
var reportReserve = 3 * time.Minute

// reviewer is the seams a review is built from. Production wires the real
// GitHub, clone, sessions and orchestrator; a test stands in for each.
type reviewer struct {
	token       func(ctx context.Context) string
	gh          func(token string) gitHub
	resolveRepo func(ctx context.Context, access orch.Access, prURL string) (string, error)
	sessions    func(api delegate.ModelAPI, config agentsession.Config) (sessionApp, error)
	review      func(ctx context.Context, deps orch.Deps, in schemas.ReviewInput, cfg config.ReviewConfig) (schemas.ReviewResult, *schemas.GitHubPRData, error)
	branchPR    func(ctx context.Context, dir string, gh gitHub) (Target, error)
	workdir     func() (string, func(), error)
}

// gitHub is the GitHub client a review uses: pr-af's own, and the one lookup
// a bare `/pr` adds.
type gitHub interface {
	github.Client
	OpenPullRequest(ctx context.Context, owner, repo, head string) (int, error)
}

func defaultReviewer() reviewer {
	return reviewer{
		token: githubToken,
		gh:    defaultGitHub(),
		resolveRepo: func(ctx context.Context, access orch.Access, prURL string) (string, error) {
			return orch.ResolveRepo(ctx, access, "", prURL)
		},
		sessions: func(api delegate.ModelAPI, config agentsession.Config) (sessionApp, error) {
			client, err := agentsession.NewClient(api)
			if err != nil {
				return nil, err
			}
			return agentsession.New(client, config), nil
		},
		review: func(ctx context.Context, deps orch.Deps, in schemas.ReviewInput, cfg config.ReviewConfig) (schemas.ReviewResult, *schemas.GitHubPRData, error) {
			o := orch.New(deps, in, cfg)
			result, err := o.Run(ctx)
			return result, o.PRData(), err
		},
		branchPR: func(ctx context.Context, dir string, gh gitHub) (Target, error) {
			return branchPullRequest(ctx, dir, gh.OpenPullRequest)
		},
		workdir: func() (string, func(), error) {
			// THE CHECKOUT IS THE RUN'S OWN AND GOES WHEN IT ENDS: never the
			// person's folder, which the review only reads git config from,
			// and never the record folder, which is kept.
			dir, err := os.MkdirTemp("", "codeaf-pr-")
			if err != nil {
				return "", func() {}, err
			}
			return dir, func() { _ = os.RemoveAll(dir) }, nil
		},
	}
}

func defaultGitHub() func(token string) gitHub {
	return func(token string) gitHub { return github.NewClient(token) }
}

// githubToken is the GitHub token for a private repository: GH_TOKEN, then
// GITHUB_TOKEN, then what `gh auth token` answers; "" for none, which reads a
// public repository. codeaf passes both variables through to a program
// (delegate.ChildEnv takes out only model keys).
func githubToken(ctx context.Context) string {
	for _, name := range []string{"GH_TOKEN", "GITHUB_TOKEN"} {
		if token := strings.TrimSpace(env.Value(name)); token != "" {
			return token
		}
	}
	gh, err := ghLookPath("gh")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, gh, "auth", "token").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// runReview is one review from its brief to its ending.
func runReview(ctx context.Context, host delegate.Host, request Request, o options, notes io.Writer, r reviewer) delegate.Ending {
	started := time.Now()
	host.Stage(delegate.StageRecord{Stage: stageStarting, Status: "running", Data: stageData(map[string]any{"doing": "finding the pull request"})})
	token := r.token(ctx)
	gh := r.gh(token)
	t := request.Target
	if t.Number == 0 {
		found, err := r.branchPR(ctx, host.Workspace(), gh)
		if err != nil {
			return delegate.Ending{Status: delegate.StatusFail,
				Message: "pr did not start: " + firstSentence(err.Error()) + "; name the pull request, such as /pr owner/repo#123",
				Reason:  fmt.Errorf("%w: %v", errNoBranchPR, err).Error()}
		}
		t = found
	}
	if !host.Models().Ready() {
		return delegate.Ending{Status: delegate.StatusCrashed, Message: "pr was started without a model API; codeaf serves one to every run it starts"}
	}
	workdir, cleanup, err := r.workdir()
	if err != nil {
		return delegate.Ending{Status: delegate.StatusFail, Message: "pr did not start: " + err.Error()}
	}
	defer cleanup()
	host.Stage(delegate.StageRecord{Stage: stageStarting, Status: "running", Data: stageData(map[string]any{"doing": "checking out " + t.String(), "pull_request": t.String()})})
	checkout, err := r.resolveRepo(ctx, orch.Access{Workdir: workdir, Token: token}, t.URL())
	if err != nil {
		if ctx.Err() != nil {
			return delegate.Ending{Status: delegate.StatusFail, Message: "pr was stopped before it finished"}
		}
		return delegate.Ending{Status: delegate.StatusFail, Message: "pr could not check out " + t.String() + ": " + firstSentence(err.Error()), Reason: err.Error()}
	}

	ceilings := host.Ceilings()
	in, err := reviewInput(t, checkout, request.Focus, ceilings, started)
	if err != nil {
		return delegate.Ending{Status: delegate.StatusCrashed, Message: "pr broke reading its own input: " + err.Error()}
	}
	cfg, err := config.ReviewConfig{}.FromInput(in)
	if err != nil {
		return delegate.Ending{Status: delegate.StatusCrashed, Message: "pr broke reading its own input: " + err.Error()}
	}
	light := o.light
	if light == "" {
		light = o.model
	}
	// THE SESSIONS ARE TOLD THEIR WORK IS A CODE REVIEW. The loop is sec's
	// too, and a session told nothing would read the checkout as no work in
	// particular.
	sessions, err := r.sessions(host.Models(), agentsession.Config{
		Root: checkout, Work: sessionWork, SessionModel: o.model, AIModel: light,
		Sessions: o.sessions, MaxTurns: o.maxTurns, SessionWall: o.sessionWall,
	})
	if err != nil {
		return delegate.Ending{Status: delegate.StatusFail, Message: "pr did not start: " + err.Error()}
	}
	review := &app{sessions: sessions}
	tracker := newPhaseTracker(host, routerHandlers(reasoners.Deps{Harness: review, AI: review}))
	spent := func() float64 { cost, _, _ := sessions.Spent(); return cost }

	// THE RUN'S HOURS ARE THE REVIEW'S, LESS THE TIME TO WRITE IT DOWN. codeaf
	// stops a program at its wall whatever it is doing; a review that stops
	// itself a little before has the minutes to say what it found.
	reviewCtx := ctx
	if wall := ceilings.Elapsed(); wall > 2*reportReserve {
		var cancel context.CancelFunc
		reviewCtx, cancel = context.WithTimeout(ctx, wall-reportReserve-time.Since(started))
		defer cancel()
	}
	result, pr, err := r.review(reviewCtx, orch.Deps{App: review, GH: gh, NodeID: Name, Local: tracker, SpentUSD: spent}, in, cfg)
	cost, sessionCount, callCount := sessions.Spent()
	if err != nil {
		_, _ = fmt.Fprintf(notes, "[pr] the review ended without a result: %v\n", err)
		return failedReview(ctx, reviewCtx, t, err, cost)
	}

	calls := review.stats()
	run := runFacts{target: t, focus: request.Focus, checkout: checkout, spent: cost, sessions: sessionCount, calls: callCount, wall: ceilings.TimeWord()}
	if pr != nil {
		run.title = pr.Title
	}
	if calls.SessionTotal > 0 && calls.SessionFailed == calls.SessionTotal {
		// Every reviewer degraded to "no findings" because none could run.
		// That is not a clean review; it is no review.
		return delegate.Ending{Status: delegate.StatusFail, CostUSD: cost, Reason: calls.FirstErr,
			Message: fmt.Sprintf("pr could not review %s: every agent session failed (%d of %d), the first with: %s",
				t, calls.SessionFailed, calls.SessionTotal, firstSentence(calls.FirstErr))}
	}
	// A REVIEW THE WALL CUT SAYS SO. The pipeline degrades a reviewer that was
	// stopped to "no findings" and hands back a whole-looking result, so the
	// account says where the run stopped rather than reading as a review that
	// ran to its end.
	if errors.Is(reviewCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		run.cutAt = stageWords[tracker.stage()]
	} else if result.Summary.BudgetExhausted {
		run.failNote = "It neared its dollar ceiling and stopped starting new work, so later checks were skipped; it covers part of the change."
	}
	if calls.Failed > 0 {
		run.failNote = strings.TrimSpace(run.failNote + fmt.Sprintf(" %d of %d model calls failed, so parts of the review may be missing; the first with: %s",
			calls.Failed, calls.Total, firstSentence(calls.FirstErr)))
	}

	host.Stage(delegate.StageRecord{Stage: stageReport, Status: "running", Data: stageData(map[string]any{"doing": "writing the review"})})
	saved := Saved{Program: Name, Focus: request.Focus, Review: result,
		PullRequest: SavedPullRequest{Owner: t.Owner, Repo: t.Repo, Number: t.Number, URL: t.URL()}}
	if pr != nil {
		saved.PullRequest.Title, saved.PullRequest.HeadSHA = pr.Title, pr.HeadSHA
	}
	records := ""
	if recorder, ok := host.(delegate.Recorder); ok {
		records = recorder.Records()
	}
	files, err := writeReport(records, saved, run)
	if err != nil {
		_, _ = fmt.Fprintf(notes, "[pr] could not write the full report: %v\n", err)
	}
	message := outcomeLine(result, run)
	if run.cutAt != "" {
		message = strings.TrimSuffix(message, ".") + ", before its time ceiling cut it short while " + run.cutAt
	}
	return delegate.Ending{
		Status:      delegate.StatusPass,
		Message:     message,
		CostUSD:     cost,
		Deliverable: account(result, run, files),
		Extra: map[string]any{
			"pull_request": t.String(), "reports": files, "findings": result.Summary.TotalFindings,
			"blocking": result.Summary.BlockingCount, "sessions": sessionCount, "calls": callCount, "cut_at": run.cutAt,
		},
	}
}

// failedReview is the ending of a review the pipeline gave up on, by why.
func failedReview(ctx, reviewCtx context.Context, t Target, err error, cost float64) delegate.Ending {
	ending := delegate.Ending{CostUSD: cost, Reason: err.Error(), Status: delegate.StatusFail}
	switch {
	case errors.Is(err, agentsession.ErrCeiling):
		ending.Status = delegate.StatusBudget
		ending.Message = "pr reached the run's dollar ceiling before it finished, so it has no review"
	case errors.Is(reviewCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil:
		ending.Status = delegate.StatusBudget
		ending.Message = "pr reached the run's time ceiling before it finished, so it has no review"
	case ctx.Err() != nil:
		ending.Message = "pr was stopped before it finished"
	case orch.IsBudgetExhausted(err):
		ending.Status = delegate.StatusBudget
		ending.Message = "pr " + firstSentence(err.Error()) + ", so it has no review"
	case errors.Is(err, orch.ErrBadInput):
		ending.Message = "pr could not review " + t.String() + ": " + firstSentence(err.Error())
	default:
		ending.Message = "pr did not finish: " + firstSentence(err.Error())
	}
	return ending
}

// reviewInput is the pipeline's input for this run, built through the same
// binding pr-af's `review` endpoint used so every default matches. It is
// always a dry run: posting is its own step, on the person's yes.
func reviewInput(t Target, checkout, focus string, ceilings delegate.Ceilings, started time.Time) (schemas.ReviewInput, error) {
	body := map[string]any{"pr_url": t.URL(), "repo_path": checkout, "dry_run": true}
	if focus != "" {
		body["hints"] = []any{focus}
	}
	in, err := afx.Bind[schemas.ReviewInput](body)
	if err != nil {
		return in, err
	}
	// THE BUDGET GATE SITS UNDER THE RUN'S CEILINGS, and is absent where a
	// ceiling is: codeaf meters and holds the run itself, and pr-af's own $2
	// and one hour were its served node's, not this run's.
	cost := math.MaxFloat64
	if ceilings.CostUSD > 0 {
		cost = ceilings.CostUSD * softFraction
	}
	in.MaxCostUSD = &cost
	seconds := math.MaxInt32
	if ceilings.Hours > 0 {
		left := ceilings.Elapsed().Seconds()*softFraction - time.Since(started).Seconds()
		seconds = int(math.Max(1, left))
	}
	in.MaxDurationSeconds = &seconds
	return in, nil
}
