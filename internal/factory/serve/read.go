package serve

import (
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func (h *Handler) readRoutes() {
	h.route("/v1/read", map[string]http.HandlerFunc{http.MethodPost: h.readJobs})
}

// readJobs answers POST /v1/read {jobs}: a person's Read of exactly the jobs
// they chose. A job the caller cannot see is skipped, as a missing one is, so
// the answer never tells which repositories exist.
func (h *Handler) readJobs(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Read != nil) || !h.require(w, h.seam.Load != nil) {
		return
	}
	var body struct {
		Jobs []int `json:"jobs"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if len(body.Jobs) == 0 {
		h.fail(w, http.StatusUnprocessableEntity, "choose at least one job to read")
		return
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	seen := make(map[int]bool, len(body.Jobs))
	for _, id := range body.Jobs {
		seen[id] = false
	}
	for _, it := range s.Items {
		if _, asked := seen[it.ID]; asked {
			seen[it.ID] = factory.IsIssueJob(it) && h.repoVisible(r, itemRepo(it))
		}
	}
	if h.authFailed(w, r) {
		return
	}
	var chosen, hidden []int
	for _, id := range body.Jobs {
		if seen[id] {
			chosen = append(chosen, id)
		} else {
			hidden = append(hidden, id)
		}
	}
	out := api.ReadResult{Read: []int{}, Skipped: []int{}, Failed: []int{}}
	if len(chosen) > 0 {
		out = h.config.Read(r.Context(), chosen)
	}
	out.Skipped = append(out.Skipped, hidden...)
	h.write(w, http.StatusOK, out)
}
