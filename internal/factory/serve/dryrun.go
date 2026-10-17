package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/intake"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// dryrun answers what this repository's start rules would do to the jobs
// still on its backlog. It starts nothing: the page says so beside the lists.
func (h *Handler) dryrun(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	// Both doors are the read. A floor that cannot load the backlog, or
	// cannot say where the pipeline file is, does not guess a dry run.
	if !h.require(w, h.seam.Load != nil && h.seam.RecipeAt != nil) {
		return
	}
	full := owner + "/" + repo
	_, _, dir, err := h.seam.RecipeAt(full)
	if h.failed(w, err) {
		return
	}
	var file pipeline.File
	if h.config.Pipeline != nil {
		file, err = h.config.Pipeline(r.Context(), full, dir)
	} else {
		file, err = pipelineFile(dir)
	}
	if h.failed(w, err) {
		return
	}
	snap, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.DryRun
	}{h.factory, wireDry(file.DryItems(backlog(snap.Items, owner, repo), func(it factory.Item) []string { return intake.Job(it).Paths }))})
}

// pipelineFile is the checkout's pipeline, or the defaults when this machine
// has no checkout. The defaults name no start label, so nothing auto-starts.
func pipelineFile(dir string) (pipeline.File, error) {
	if strings.TrimSpace(dir) == "" {
		file := pipeline.Defaults()
		file.Source = pipeline.SourceDefaults
		return file, nil
	}
	return pipeline.Load(dir)
}

// backlog is the jobs still new on repo, in the floor's order. Queued and
// running work has already left the backlog, so a dry run does not offer it.
// A GitHub item stores the short name in Repo and the owner in Product;
// a floor item stores owner/name in Repo. Both match.
func backlog(items []factory.Item, owner, name string) []factory.Item {
	var jobs []factory.Item
	for _, it := range items {
		if it.State != factory.StateNew || !sameRepo(it, owner, name) {
			continue
		}
		jobs = append(jobs, it)
	}
	return jobs
}

// sameRepo matches owner/name, and a short Repo when Product is that owner
// or was never set. A different product's short name does not match.
func sameRepo(it factory.Item, owner, name string) bool {
	if strings.EqualFold(it.Repo, owner+"/"+name) {
		return true
	}
	if !strings.EqualFold(strings.TrimSpace(it.Repo), name) {
		return false
	}
	product := strings.TrimSpace(it.Product)
	return product == "" || strings.EqualFold(product, owner)
}

// wireDry is the pipeline's lists in the API's words. One reason string is
// every reason Decide gave, in its order, so the page does not pick one.
func wireDry(d pipeline.DryRun) api.DryRun {
	out := api.DryRun{
		WouldStart: append([]int{}, d.WouldStart...),
		WouldAsk:   make([]api.Ask, 0, len(d.WouldAsk)),
		WouldWait:  make([]api.Ask, 0, len(d.WouldWait)),
	}
	for _, id := range d.WouldAsk {
		out.WouldAsk = append(out.WouldAsk, api.Ask{Job: id, Reason: strings.Join(d.Why[id], " · ")})
	}
	for _, wait := range d.WouldWait {
		out.WouldWait = append(out.WouldWait, api.Ask{Job: wait.ID, Reason: wait.Reason})
	}
	return out
}
