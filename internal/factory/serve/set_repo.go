package serve

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/run"
)

// SetRepoVerb is POST /v1/jobs/{id}/repo, the door factory.job_set_repo
// reaches.
const SetRepoVerb = "repo"

// SetRepoStarted is the refusal of a repository for a job that has already
// started: its run, its file and its steps were decided on the one it has.
const SetRepoStarted = "This job has already started, so it keeps its repository."

// SetRepoForge is the refusal of a repository for a GitHub issue's job.
const SetRepoForge = "A GitHub issue's job is worked on its issue's repository."

// setRepoBody is the POST /v1/jobs/{id}/repo body.
type setRepoBody struct {
	Repo   string      `json:"repo"`
	Expect *api.Expect `json:"expect,omitempty"`
}

// errSetRepoStarted is [SetRepoStarted] found under the job's write lock.
var errSetRepoStarted = errors.New(SetRepoStarted)

// setRepo gives a job that never ran its repository: THE ONE DOOR FOR OUTSIDE
// WORK THAT ARRIVED WITH NONE, which no rule sent anywhere. It sets the job's
// repository and places, stamps that repository's steps for its kind, keeps
// who chose it ([factory.Item.RepoSetBy], so a routing rule's repository
// never moves it again), clears the rule that routed it, and runs the
// routing again, so a rule that matches the job on its new repository
// decides its pipeline, priority and start.
//
// ONLY THE OPERATOR OR THE JOB'S PERSON may: a job nobody owns yet is the
// operator's to place. A job that has started keeps its repository; every
// refusal comes before the write.
func (h *Handler) setRepo(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body setRepoBody
	if !h.decode(w, r, &body, false) || !h.fresh(w, r, it, SetRepoVerb, body.Expect) {
		return
	}
	repo := strings.TrimSpace(body.Repo)
	if !factory.RepoName(repo) {
		h.fail(w, 422, "repo must be owner/name")
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	who = factory.NormalizeHandle(who)
	if !h.mayPlace(r, it, who) {
		if person := it.Person(); person != "" {
			h.notJobPerson(w, it, person)
		} else {
			h.fail(w, http.StatusForbidden, "only the operator can do this")
		}
		return
	}
	if !h.repoVisible(r, repo) {
		h.fail(w, 422, "this factory does not work on "+repo)
		return
	}
	if placeStarted(it) {
		h.refuse(w, 409, SetRepoStarted, "conflict", "", nil, factory.JobState(it))
		return
	}
	// A GitHub issue's job is its issue's: its pull request closes that
	// issue, on that repository.
	if it.Origin == factory.OriginForge && it.Num > 0 {
		h.refuse(w, 409, SetRepoForge, "conflict", "", nil, factory.JobState(it))
		return
	}
	stages := factory.CopyStages(factory.DefaultRecipe().For(it.Kind))
	if h.seam.RecipeAt != nil {
		if recipe, _, _, err := h.seam.RecipeAt(repo); err == nil {
			if steps := recipe.For(it.Kind); len(steps) > 0 {
				stages = factory.CopyStages(steps)
			}
		}
	}
	change := func(cur *factory.Item) error {
		if placeStarted(*cur) {
			return errSetRepoStarted
		}
		cur.Repo, cur.Places = repo, []string{repo}
		cur.Stages = factory.CopyStages(stages)
		cur.Route, cur.RouteRefused = "", ""
		cur.RepoSetBy = who
		if cur.RepoSetBy == "" {
			cur.RepoSetBy = "operator"
		}
		return nil
	}
	err = h.recordAction(r, it, record.RepoSet, repo, func() error {
		if body.Expect != nil && body.Expect.Version != nil {
			return h.config.Store.UpdateExpected(it.ID, *body.Expect.Version, change)
		}
		return h.config.Store.Update(it.ID, change)
	})
	if errors.Is(err, errSetRepoStarted) {
		h.refuse(w, 409, SetRepoStarted, "conflict", "", nil, factory.JobState(it))
		return
	}
	if err != nil {
		h.finish(w, err)
		return
	}
	h.routeNow(r.Context())
	h.finish(w, nil)
}

// mayPlace says the caller may give it a repository: the operator always,
// and the job's person when it has one.
func (h *Handler) mayPlace(r *http.Request, it factory.Item, who string) bool {
	if h.mode() == api.FactorySolo || h.isOperator(r) {
		return true
	}
	return it.Person() != "" && who == it.Person()
}

// placeStarted says it has started in a way that fixed its repository: it
// ran, its pipeline file was decided at a start, or it is not in Backlog.
func placeStarted(it factory.Item) bool {
	return run.Ran(it) || it.PipelineIsDecided() || it.State != factory.StateNew
}

// routeNow runs the routing over new jobs at once when this factory can, and
// nudges the poll either way, whose pass routes them too. A routing pass that
// fails is not the door's failure: the repository is kept, and the next pass
// routes the job.
func (h *Handler) routeNow(ctx context.Context) {
	if h.config.Route != nil {
		_ = h.config.Route(context.WithoutCancel(ctx))
	}
	_ = h.config.Store.NudgePoll()
}
