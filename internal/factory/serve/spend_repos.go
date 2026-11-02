package serve

import (
	"net/http"
	"sort"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// spendRepos answers GET /v1/spend/repos?from=&to=: each readable
// repository's job spend over the factory's own calendar days
// ([api.SpendByRepo] says how a job's spend is placed). It reads the job
// documents, which the factory keeps for every job it has run, so it sees the
// spend of every runner, not only this machine's. The caller's visibility is
// the queue's ([Handler.forViewer]); a repository that is off still counts,
// because what it spent was spent.
func (h *Handler) spendRepos(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	q := r.URL.Query()
	start, end, err := factory.SpendRange(q.Get("from"), q.Get("to"), h.clock().Location())
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	snap, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	snap, _, _ = h.forViewer(r, snap)
	if h.authFailed(w, r) {
		return
	}
	out := api.SpendByRepo{From: q.Get("from"), To: q.Get("to"), Repos: []api.RepoSpend{}}
	index := map[string]int{}
	for _, it := range snap.Items {
		repo, usd, at := factory.RepoOf(it), factory.JobSpent(it), factory.SpentAt(it)
		if repo == "" || usd <= 0 || at.Before(start) || !at.Before(end) {
			continue
		}
		i, ok := index[repo]
		if !ok {
			i = len(out.Repos)
			index[repo] = i
			out.Repos = append(out.Repos, api.RepoSpend{Repo: repo})
		}
		out.Repos[i].SpentUSD += usd
		out.Repos[i].Jobs++
		out.TotalUSD += usd
	}
	sort.Slice(out.Repos, func(i, j int) bool { return out.Repos[i].Repo < out.Repos[j].Repo })
	h.write(w, http.StatusOK, out)
}
