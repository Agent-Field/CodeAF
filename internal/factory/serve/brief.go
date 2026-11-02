package serve

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

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

// beginBrief marks a write of the job's brief on its way and answers the
// writer's prompt and input, or says why no write begins. The input is read
// from the item inside the same update that marks it, so the hash names
// exactly what the writer is given.
func (h *Handler) beginBrief(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.BriefBeginRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if !slices.Contains(factory.BriefTriggers, body.Trigger) {
		h.fail(w, http.StatusUnprocessableEntity, "trigger must be one of "+strings.Join(factory.BriefTriggers, ", "))
		return
	}
	out := api.BriefBegin{Factory: h.factory, Job: it.ID}
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		in := factory.BriefInputOf(*cur)
		out.Hash = factory.BriefInputHash(in)
		if out.Skip = cur.BeginBrief(body.Trigger, out.Hash, h.clock()); out.Skip == "" {
			out.System, out.Input = factory.BriefSystem(), factory.BriefInputText(in)
		}
		return nil
	})
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, out)
}

// keepBrief ends a write: the factory checks the writer's draft against the
// job as it is now, keeps what passes, books the call's cost to the job, and
// answers the brief as it then reads.
func (h *Handler) keepBrief(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body api.BriefKeepRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.CostUSD < 0 || (body.Draft == "") == (body.Failure == "") {
		h.fail(w, http.StatusUnprocessableEntity, "a keep carries a draft or a failure, and a cost of zero or more")
		return
	}
	var out api.BriefKept
	err := h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		_, drops, kept := cur.EndBrief(factory.BriefWritten{
			Hash: body.Hash, Model: body.Model, CostUSD: body.CostUSD, Draft: []byte(body.Draft), Failure: body.Failure,
		}, h.clock())
		out = api.BriefKept{JobBrief: jobBrief(h.factory, *cur, 0), Kept: kept, Drops: drops}
		return nil
	})
	if h.failed(w, err) {
		return
	}
	if out.Drops == nil {
		out.Drops = []factory.BriefDrop{}
	}
	h.write(w, http.StatusOK, out)
}
