package serve

import (
	"bytes"
	"encoding/csv"
	"errors"
	"io"
	"net/http"
	"strconv"
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
			h.fail(w, http.StatusUnprocessableEntity, "since must be an RFC3339 timestamp")
			return
		}
	}
	format := r.URL.Query().Get("format")
	if format != "" && format != "json" && format != "csv" {
		h.fail(w, http.StatusUnprocessableEntity, "format must be json or csv")
		return
	}
	_, viewer := h.repoAccess(r, nil)
	if format == "csv" && !viewer {
		// Finish reading before sending headers so disk failures retain JSON errors.
		var data bytes.Buffer
		if h.failed(w, h.config.Record.CSV(r.Context(), &data, since)) {
			return
		}
		h.sendAuditCSV(w, data.Bytes())
		return
	}
	events, err := h.config.Record.Since(r.Context(), since)
	if h.failed(w, err) {
		return
	}
	if viewer {
		events = h.filterAudit(r, events)
	}
	if format == "csv" {
		var data bytes.Buffer
		if h.failed(w, writeAuditCSV(&data, events)) {
			return
		}
		h.sendAuditCSV(w, data.Bytes())
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory    `json:"factory"`
		Events  []record.Event `json:"events"`
	}{h.factory, events})
}

// writeAuditCSV matches record.CSV's columns so a filtered export is the
// same file with rows removed, not a second dialect.
func writeAuditCSV(out io.Writer, events []record.Event) error {
	w := csv.NewWriter(out)
	if err := w.Write([]string{"id", "at", "kind", "job", "handle", "cost", "detail", "commit"}); err != nil {
		return err
	}
	for _, e := range events {
		if err := w.Write([]string{e.ID, e.At.Format(time.RFC3339Nano), e.Kind, strconv.Itoa(e.Job), e.Handle, strconv.FormatFloat(e.Cost, 'f', -1, 64), e.Detail, e.Commit}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func (h *Handler) sendAuditCSV(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="factory-record.csv"`)
	w.Header().Set("X-Factory-Address", h.factory.Address)
	w.Header().Set("X-Factory-Version", h.factory.Version)
	_, _ = w.Write(data)
}

func (h *Handler) recordApproval(r *http.Request, it factory.Item, edited bool) (string, error) {
	if h.config.Record == nil {
		if h.config.RecordApproval != nil {
			return h.config.RecordApproval(r.Context(), it.ID, edited)
		}
		return "", errors.New("approval requires a factory record")
	}
	handle, err := h.callerHandle(r)
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
	event, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Approved, Job: it.ID, Handle: handle, Cost: jobCost(it), Detail: detail, Commit: it.HeadCommit})
	return event.ID, err
}

func jobCost(it factory.Item) float64 {
	if it.Stream != nil {
		return it.Stream.Spent
	}
	return 0
}

// The identity is resolved before touching the job, and only a verb the door
// accepted enters the history. An approve-step yes uses the same receipt as
// approve, written after the answer door accepts.
func (h *Handler) recordAction(r *http.Request, it factory.Item, kind, detail string, door func() error) error {
	if h.config.Record == nil {
		return door()
	}
	handle, err := h.callerHandle(r)
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
		if words, ok := h.words(w, r, it, "request_changes"); ok {
			h.finish(w, h.recordAction(r, it, kind, words, func() error { return door(it.ID, words) }))
		}
	}
}

// A yes through Answer is still consent at an approve step. The receipt is
// written only after that door accepts, so a refused yes leaves no approved event.
func (h *Handler) recordAnswer(r *http.Request, it factory.Item, yes bool, words string) error {
	handle, err := h.callerHandle(r)
	if err != nil {
		return err
	}
	if err := h.seam.Answer(it.ID, yes, words); err != nil {
		return err
	}
	eventID := ""
	at := time.Now()
	if h.workers != nil {
		at = h.workers.now()
	}
	if yes && it.QKind == factory.QKindApprove {
		eventID, err = h.recordApproval(r, it, false)
		if err != nil {
			return err
		}
	} else if h.config.Record != nil {
		event, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Answered, Job: it.ID, Handle: handle, Cost: jobCost(it), Detail: words})
		if err != nil {
			return err
		}
		eventID, at = event.ID, event.At
	}
	text := words
	if text == "" {
		if yes {
			text = "yes"
		} else {
			text = "no"
		}
	}
	return h.keepAnswered(it, factory.Answer{By: handle, Text: text, At: at, Event: eventID})
}

func (h *Handler) keepAnswered(it factory.Item, answer factory.Answer) error {
	if h.config.Store == nil {
		return nil
	}
	// Keep the ask as it stood before the answer cleared the open gate.
	qs := api.FromItem(it).Questions
	return h.config.Store.Update(it.ID, func(cur *factory.Item) error {
		for _, q := range qs {
			if q.Answer != nil || (q.With != "" && q.With != factory.AskedYou) {
				continue
			}
			q.State = "answered"
			kept := answer
			q.Answer = &kept
			cur.AnsweredQuestions = append(cur.AnsweredQuestions, q)
		}
		if len(cur.AnsweredQuestions) > factory.AnswersMost {
			cur.AnsweredQuestions = cur.AnsweredQuestions[len(cur.AnsweredQuestions)-factory.AnswersMost:]
		}
		return nil
	})
}
