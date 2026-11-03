package serve

import (
 "net/http"
 "github.com/Agent-Field/codeaf/internal/factory"
 "github.com/Agent-Field/codeaf/internal/factory/lease"
)

// resumeStopped is the person's Carry on. A recorded current step is retried
// through the revision-checked person door; a job with no step uses Start.
func (h *Handler) resumeStopped(w http.ResponseWriter, r *http.Request, it factory.Item) {
 if it.Stream == nil || it.Stream.Cur < 0 || it.Stream.Cur >= len(it.Stream.Phases) {
  h.startJob(w,r,it)
  return
 }
 revision, err := h.config.Store.Revision(it.ID)
 if h.failed(w,err) { return }
 if h.workers == nil || h.workers.desk == nil { h.fail(w,422,"This runner cannot resume stopped work. Press Start."); return }
 h.finish(w,lease.NewDoors(h.workers.desk).StepRerunExpected(it.ID,it.Stream.Phases[it.Stream.Cur].Name,"","",&revision))
}
