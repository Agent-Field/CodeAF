package serve

import (
	"net/http"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// readJobBrief answers GET /v1/jobs/{id}/brief: the job's newest brief, or
// its stub before any is kept, and what changed since the rev in `?since=`.
// It reads only what the job already holds, so anyone who may read the job
// may read its brief.
func (h *Handler) readJobBrief(w http.ResponseWriter, r *http.Request) {
	since := 0
	if raw := r.URL.Query().Get("since"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			h.fail(w, http.StatusUnprocessableEntity, "since must be a brief's rev")
			return
		}
		since = n
	}
	it, ok := h.findJob(w, r)
	if !ok {
		return
	}
	h.write(w, http.StatusOK, jobBrief(h.factory, it, since))
}

// jobBrief is the answer for one job, read whole from the item.
func jobBrief(f api.Factory, it factory.Item, since int) api.JobBrief {
	out := api.JobBrief{
		Factory: f, Job: it.ID, Status: factory.BriefStatusOf(it), Since: since,
		Brief: factory.LatestBrief(it), ChangedSinceSeen: factory.BriefChangedSince(it.Briefs, since),
	}
	if w := it.BriefWriting; out.Status == factory.BriefStatusFailed && w != nil {
		out.Failure, out.FailedAfter = w.Failure, w.AfterStep
	}
	return out
}
