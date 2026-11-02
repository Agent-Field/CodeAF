package serve

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// judgeProtected says which protected paths each Backlog job names
// ([api.JobProtected]): the paths intake would hand a start, against the
// rules the job would start under. jobs are the issue jobs of items, in
// order, as [api.FromSnapshot] makes them. Each
// repository's rules are read once, show-only, so a queue never waits on
// GitHub for them; a repository whose rules cannot be read leaves its jobs
// unjudged rather than safe.
func (h *Handler) judgeProtected(ctx context.Context, items []factory.Item, jobs []api.Job) {
	if h.config.Pipeline == nil {
		return
	}
	ctx = factorygithub.ShowOnly(ctx)
	rules := map[string]*pipeline.File{}
	for i, it := range factory.IssueJobs(items) {
		if it.State != factory.StateNew {
			continue
		}
		repo := factory.RepoOf(it)
		f, read := rules[repo]
		if !read {
			f = h.showRules(ctx, repo)
			rules[repo] = f
		}
		if f != nil {
			jobs[i].Protected = &api.JobProtected{Paths: f.ProtectedNamed(intake.Job(it).Paths)}
		}
	}
}

// showRules is repo's rules as the factory runs them, or nil when they
// cannot be read.
func (h *Handler) showRules(ctx context.Context, repo string) *pipeline.File {
	if repo == "" {
		return nil
	}
	dir := ""
	if h.seam.RecipeAt != nil {
		_, _, at, err := h.seam.RecipeAt(repo)
		if err != nil {
			return nil
		}
		dir = at
	}
	f, err := h.config.Pipeline(ctx, repo, dir)
	if err != nil {
		return nil
	}
	return &f
}
