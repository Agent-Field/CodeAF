package serve

import (
	"errors"
	"github.com/Agent-Field/codeaf/internal/filelock"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/publish"
)

func (h *Handler) publishCodes(w http.ResponseWriter, r *http.Request) {
	h.write(w, http.StatusOK, struct {
		Factory api.Factory    `json:"factory"`
		Codes   []publish.Code `json:"codes"`
	}{h.factory, publish.Codes()})
}

// commitFresh keeps old, unreported jobs readable under Contract 1. Once a
// machine reports a head, every approval must name the head the person saw.
func (h *Handler) commitFresh(w http.ResponseWriter, it factory.Item, commit string) bool {
	if it.HeadCommit != "" && commit == "" {
		h.fail(w, http.StatusUnprocessableEntity, "approve requires the commit shown in head_commit")
		return false
	}
	if commit == it.HeadCommit {
		return true
	}
	h.changedSinceOpened(w, it.HeadCommit)
	return false
}

func (h *Handler) changedSinceOpened(w http.ResponseWriter, head string) {
	h.write(w, http.StatusConflict, struct {
		Factory    api.Factory `json:"factory"`
		Error      string      `json:"error"`
		Reason     string      `json:"reason"`
		HeadCommit string      `json:"head_commit"`
		RequestID  string      `json:"request_id"`
	}{h.factory, "This job changed since you opened it", "changed_since_opened", head, requestID()})
}

// retryPublish reuses a durable proof receipt, never another approval event.
func (h *Handler) retryPublish(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if !h.require(w, h.config.Record != nil) {
		return
	}
	if it.PublishStatus == nil || it.PublishStatus.State != "failed" {
		h.refuse(w, http.StatusConflict, "job is not waiting for Retry publishing", "conflict", "", nil, api.FromItem(it).State)
		return
	}
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	person := it.Person()
	if person == "" {
		person, _, _ = h.latestApproval(r.Context(), it.ID)
	}
	if person == "" || person != handle {
		h.fail(w, http.StatusForbidden, "only the job's person can Retry publishing")
		return
	}
	if _, err := h.config.Record.TrailerFor(r.Context(), it.ID, it.HeadCommit); err != nil {
		h.write(w, http.StatusConflict, struct {
			Factory    api.Factory `json:"factory"`
			Error      string      `json:"error"`
			Reason     string      `json:"reason"`
			HeadCommit string      `json:"head_commit"`
		}{h.factory, "The commit changed; approve again", "changed_since_opened", it.HeadCommit})
		return
	}
	_, err = h.seam.SignOff(it.ID, true)
	if h.changedApproval(w, r, err) {
		return
	}
	h.finish(w, err)
}

// lockConsent serializes double clicks even when two servers share the store.
// The job is read inside this lock, before a receipt or publish signal exists.
func (h *Handler) lockConsent() (func(), error) {
	if h.config.Store == nil {
		h.consentMu.Lock()
		return h.consentMu.Unlock, nil
	}
	f, err := os.OpenFile(filepath.Join(h.config.Store.Root(), "consent.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f, true, false); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); _ = f.Close() }, nil
}

// changedApproval catches a head that moved after the HTTP snapshot was read
// but before the lease door could queue the publishing signal.
func (h *Handler) changedApproval(w http.ResponseWriter, r *http.Request, err error) bool {
	if !errors.Is(err, publish.ErrCommitChanged) {
		return false
	}
	if latest, ok := h.findJob(w, r); ok {
		h.changedSinceOpened(w, latest.HeadCommit)
	}
	return true
}
