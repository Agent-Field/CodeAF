package serve

import (
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/estimate"
)

// estimateNew answers GET /v1/repos/{owner}/{repo}/estimate?kind=K: what a
// job of kind K in a repository the caller can see will likely cost before
// it exists, measured from the finished jobs the caller can see.
func (h *Handler) estimateNew(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	kind := strings.TrimSpace(query.Get("kind"))
	if len(query) != 1 || len(query["kind"]) != 1 || kind == "" {
		h.fail(w, http.StatusUnprocessableEntity, "the estimate takes kind, once")
		return
	}
	if !h.require(w, h.seam.Load != nil) {
		return
	}
	s, err := h.seam.Load()
	if h.failed(w, err) {
		return
	}
	s, _, _ = h.forViewer(r, s)
	if h.authFailed(w, r) {
		return
	}
	out := api.NewEstimate{Factory: h.factory, Repo: owner + "/" + name, Kind: kind}
	if got, ok := estimate.Open(s.Items).ForNew(out.Repo, factory.Kind(kind)); ok {
		out.Estimate = api.FromRange(got)
	}
	h.write(w, http.StatusOK, out)
}
