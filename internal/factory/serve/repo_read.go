package serve

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/reporead"
)

// repoReadParams are the only query keys the read route takes; any other is
// refused, so a caller never believes an unknown key was honoured.
var repoReadParams = map[string]bool{"op": true, "path": true, "pattern": true, "from": true, "to": true, "job": true}

// repoRead answers GET /v1/repos/{owner}/{repo}/read: one bounded,
// read-only look at THIS factory's checkout of a repository the caller can
// see (reporead). A job's head is read only for a job of the same
// repository the caller can see, and only when its commit is in this
// checkout; otherwise the look is at the default branch. The text is the
// repository's, so the answer is marked untrusted.
func (h *Handler) repoRead(w http.ResponseWriter, r *http.Request) {
	owner, name, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	repo := owner + "/" + name
	query := r.URL.Query()
	for key, values := range query {
		if !repoReadParams[key] || len(values) != 1 {
			h.fail(w, http.StatusUnprocessableEntity, "the read takes op, path, pattern, from, to and job, once each")
			return
		}
	}
	req := reporead.Request{Op: query.Get("op"), Path: query.Get("path"), Pattern: query.Get("pattern")}
	var job int
	for key, n := range map[string]*int{"from": &req.From, "to": &req.To, "job": &job} {
		if raw := query.Get(key); raw != "" {
			v, err := strconv.Atoi(raw)
			if err != nil || v <= 0 {
				h.fail(w, http.StatusUnprocessableEntity, key+" must be a positive number")
				return
			}
			*n = v
		}
	}
	if job > 0 {
		it, found := h.repoJob(r, repo, job)
		if !found {
			h.fail(w, http.StatusNotFound, "job not found")
			return
		}
		if req.Head = strings.TrimSpace(it.HeadCommit); req.Head == "" {
			h.fail(w, http.StatusNotFound, "the job has no head commit yet")
			return
		}
	}
	dir := ""
	if h.config.Store != nil {
		dir = h.config.Store.CheckoutDir(repo)
	}
	out, err := reporead.Read(r.Context(), dir, req)
	if err != nil {
		h.repoReadFail(w, repo, err)
		return
	}
	out.Factory, out.Repo, out.Job = h.factory, repo, job
	h.write(w, http.StatusOK, out)
}

// repoJob is job id of repo when the caller may read it.
func (h *Handler) repoJob(r *http.Request, repo string, id int) (factory.Item, bool) {
	if h.seam.Load == nil {
		return factory.Item{}, false
	}
	s, err := h.seam.Load()
	if err != nil {
		return factory.Item{}, false
	}
	for _, it := range s.Items {
		if it.ID == id && itemRepo(it) == strings.ToLower(repo) {
			return it, h.repoVisible(r, repo)
		}
	}
	return factory.Item{}, false
}

func (h *Handler) repoReadFail(w http.ResponseWriter, repo string, err error) {
	var missing reporead.NotFound
	var bad reporead.BadInput
	switch {
	case errors.Is(err, reporead.ErrNotCloned):
		h.refuse(w, http.StatusConflict, repo+" has no git checkout on the factory's machine", api.ReasonNotCloned, "", nil, "")
	case errors.Is(err, reporead.ErrDenied):
		h.refuse(w, http.StatusForbidden, "denied", api.ReasonDenied, "", nil, "")
	case errors.Is(err, reporead.ErrBinary):
		h.refuse(w, http.StatusUnprocessableEntity, "binary", api.ReasonBinary, "", nil, "")
	case errors.As(err, &missing):
		h.fail(w, http.StatusNotFound, missing.Error())
	case errors.As(err, &bad):
		h.fail(w, http.StatusUnprocessableEntity, bad.Error())
	default:
		h.fail(w, http.StatusInternalServerError, "the checkout could not be read")
	}
}
