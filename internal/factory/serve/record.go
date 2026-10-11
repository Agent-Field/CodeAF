package serve

import (
	"bytes"
	"errors"
	"net/http"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func (h *Handler) audit(w http.ResponseWriter, r *http.Request) {
	if !h.require(w, h.config.Record != nil) {
		return
	}
	var since time.Time
	if words := r.URL.Query().Get("since"); words != "" {
		var err error
		since, err = time.Parse(time.RFC3339Nano, words)
		if err != nil {
			h.fail(w, http.StatusBadRequest, "since must be an RFC3339 timestamp")
			return
		}
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "csv" {
		h.fail(w, http.StatusBadRequest, "format must be json or csv")
		return
	}
	if format == "csv" {
		// Finish reading before sending headers so disk failures retain JSON errors.
		var data bytes.Buffer
		if h.failed(w, h.config.Record.CSV(r.Context(), &data, since)) {
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="factory-record.csv"`)
		w.Header().Set("X-Factory-Address", h.factory.Address)
		w.Header().Set("X-Factory-Version", h.factory.Version)
		_, _ = w.Write(data.Bytes())
		return
	}
	events, err := h.config.Record.Since(r.Context(), since)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory    `json:"factory"`
		Events  []record.Event `json:"events"`
	}{h.factory, events})
}

func (h *Handler) recordApproval(r *http.Request, it factory.Item, edited bool) (string, error) {
	if h.config.Record == nil {
		if h.config.RecordApproval != nil {
			return h.config.RecordApproval(r.Context(), it.ID, edited)
		}
		return "", errors.New("approval requires a factory record")
	}
	handle, err := h.seam.PersonHandle(r.Context())
	if err != nil {
		return "", err
	}
	detail := "proof"
	if it.State != factory.StateLanded {
		detail = "plan"
	}
	if edited {
		detail += " · edited"
	}
	event, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Approved, Job: it.ID, Handle: handle, Cost: jobCost(it), Detail: detail})
	return event.ID, err
}

func jobCost(it factory.Item) float64 {
	if it.Stream != nil {
		return it.Stream.Spent
	}
	return 0
}

// The identity is resolved before touching the job, and only successful verbs
// enter the history. Approvals have their stricter write-before-consent door.
func (h *Handler) recordAction(r *http.Request, it factory.Item, kind, detail string, door func() error) error {
	if h.config.Record == nil {
		return door()
	}
	handle, err := h.seam.PersonHandle(r.Context())
	if err != nil {
		return err
	}
	if err := door(); err != nil {
		return err
	}
	_, err = h.config.Record.Append(r.Context(), record.Event{Kind: kind, Job: it.ID, Handle: handle, Cost: jobCost(it), Detail: detail})
	return err
}

func (h *Handler) recordedID(door func(int) error, kind string) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		h.finish(w, h.recordAction(r, it, kind, "", func() error { return door(it.ID) }))
	}
}

func (h *Handler) recordedWords(door func(int, string) error, kind string) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if words, ok := h.words(w, r); ok {
			h.finish(w, h.recordAction(r, it, kind, words, func() error { return door(it.ID, words) }))
		}
	}
}

// A yes through Answer is still consent at an approve step. Both public doors
// must leave the same durable receipt before waking the runner.
func (h *Handler) recordAnswer(r *http.Request, it factory.Item, yes bool, words string) error {
	door := func() error { return h.seam.Answer(it.ID, yes, words) }
	if yes && it.QKind == factory.QKindApprove {
		if _, err := h.recordApproval(r, it, false); err != nil {
			return err
		}
		return door()
	}
	return h.recordAction(r, it, record.Answered, words, door)
}
