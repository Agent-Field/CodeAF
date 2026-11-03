package serve

import (
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"net/http"
 "errors"
 "github.com/Agent-Field/codeaf/internal/factory/store"
)

func (h *Handler) publishAnyway(w http.ResponseWriter, r *http.Request, it factory.Item) {
	session, authenticated := h.credential(r)
 if !authenticated || (session.profile == nil && !(h.config.ServedBy == "app" && h.floorToken(bearer(r)))) {
 h.fail(w,403,"Sign in before pressing Publish anyway");return
 }
 var body struct {
		Expect *api.Expect `json:"expect"`
	}
	if !h.decode(w, r, &body, true) || !h.fresh(w, r, it, "publish_anyway", body.Expect) || !h.approves(w, r, it) {
		return
	}
	who, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if who == "" {
		h.fail(w, 403, "Sign in before pressing Publish anyway")
		return
	}
	if body.Expect == nil || body.Expect.Version == nil {
		h.fail(w, 422, "expect.version is required")
		return
	}
	err = h.config.Store.UpdateExpected(it.ID, *body.Expect.Version, func(cur *factory.Item) error {
		return factory.DecidePublishAnyway(cur, who, h.clock())
	})
	if err != nil {
 var stale *store.StaleError
 if errors.As(err,&stale) {h.refuse(w,409,err.Error(),"stale","",nil,factory.JobState(it));return}
		h.refuse(w, 409, err.Error(), "conflict", "", nil, factory.JobState(it))
		return
	}
	if h.config.Record != nil {
		_, err = h.config.Record.Append(r.Context(), record.Event{Kind: record.Approved, Job: it.ID, Handle: who, Commit: it.HeadCommit, Detail: "Publish anyway; skipped checks remain unmeasured"})
		if h.failed(w, err) {
			return
		}
	}
	h.finish(w, h.seam.Answer(it.ID, true, factory.ChoiceTryOnceMore))
}
