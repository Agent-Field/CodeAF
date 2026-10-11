package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// PendingApproval is deliberately not a bound approval id. Until a record is
// supplied, callers can distinguish this placeholder from a durable receipt.
const PendingApproval = "evt_pending"

func (h *Handler) verbs() {
	s := h.seam
	h.postJob("run", s.Launch != nil, h.idVerb(s.Launch))
	h.postJob("stop", s.Stop != nil, h.idVerb(s.Stop))
	h.postJob("pause", s.Hold != nil, h.holdVerb(true))
	h.postJob("resume", s.Hold != nil, h.holdVerb(false))
	h.postJob("drop", s.Dismiss != nil, h.idVerb(s.Dismiss))
	h.postJob("answer", s.Answer != nil, h.answer)
	h.postJob("steer", s.Steer != nil, h.wordsVerb(s.Steer))
	h.postJob("approve", true, h.approve)
	h.postJob("request_changes", s.SendBack != nil, h.wordsVerb(s.SendBack))
	h.postJob("recheck", s.Reverify != nil, h.idVerb(s.Reverify))
	h.postJob("prioritize", s.Mark != nil, h.prioritize)
	h.postJob("reply", s.Say != nil, h.reply)
	h.route("/v1/jobs", map[string]http.HandlerFunc{http.MethodPost: h.newJob})
	h.route("/v1/github/connect", map[string]http.HandlerFunc{http.MethodPost: h.connectGitHub})
}

type jobAction func(http.ResponseWriter, *http.Request, factory.Item)

func (h *Handler) postJob(verb string, exists bool, action jobAction) {
	h.route("/v1/jobs/{id}/"+verb, map[string]http.HandlerFunc{
		http.MethodPost: func(w http.ResponseWriter, r *http.Request) {
			if !h.requireVerb(w, exists) {
				return
			}
			if it, ok := h.findJob(w, r); ok {
				action(w, r, it)
			}
		},
	})
}

func (h *Handler) idVerb(door func(int) error) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		h.finish(w, door(it.ID))
	}
}

func (h *Handler) holdVerb(on bool) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		h.finish(w, h.seam.Hold(it.ID, on))
	}
}

func (h *Handler) wordsVerb(door func(int, string) error) jobAction {
	return func(w http.ResponseWriter, r *http.Request, it factory.Item) {
		if words, ok := h.words(w, r); ok {
			h.finish(w, door(it.ID, words))
		}
	}
}

func (h *Handler) answer(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Yes   *bool  `json:"yes"`
		Words string `json:"words"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Yes == nil && strings.TrimSpace(body.Words) == "" {
		h.fail(w, http.StatusBadRequest, "answer requires yes or words")
		return
	}
	yes := body.Yes != nil && *body.Yes
	h.finish(w, h.seam.Answer(it.ID, yes, body.Words))
}

func (h *Handler) approve(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		Edited bool `json:"edited"`
	}
	if !h.decode(w, r, &body, true) {
		return
	}
	door := h.approvalDoor(w, it, body.Edited)
	if door == nil {
		return
	}
	event := PendingApproval
	if h.config.RecordApproval != nil {
		var err error
		event, err = h.config.RecordApproval(r.Context(), it.ID, body.Edited)
		if h.failed(w, err) {
			return
		}
	}
	if h.failed(w, door()) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Event   string      `json:"event"`
	}{h.factory, event})
}

// approvalDoor chooses from the stored gate, never from a caller's guess.
// The seam still enforces proof and policy before it ships anything.
func (h *Handler) approvalDoor(w http.ResponseWriter, it factory.Item, edited bool) func() error {
	if it.State == factory.StateLanded {
		if !h.requireVerb(w, h.seam.SignOff != nil) {
			return nil
		}
		return func() error {
			_, err := h.seam.SignOff(it.ID, edited)
			return err
		}
	}
	if it.State == factory.StateNeedsYou && it.QKind == factory.QKindApprove {
		if !h.requireVerb(w, h.seam.Answer != nil) {
			return nil
		}
		return func() error { return h.seam.Answer(it.ID, true, "") }
	}
	h.fail(w, http.StatusConflict, "job is not waiting for approval")
	return nil
}

func (h *Handler) prioritize(w http.ResponseWriter, r *http.Request, it factory.Item) {
	var body struct {
		To string `json:"to"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.To != "top" {
		h.fail(w, http.StatusBadRequest, "prioritize requires to: top")
		return
	}
	h.finish(w, h.seam.Mark([]int{it.ID}, true))
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if words, ok := h.words(w, r); ok {
		h.finish(w, h.seam.Say(r.Context(), it.ID, words))
	}
}

func (h *Handler) newJob(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.New != nil) {
		return
	}
	var body struct {
		Repo      string `json:"repo"`
		Title     string `json:"title"`
		Text      string `json:"text"`
		Autostart bool   `json:"autostart"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if strings.TrimSpace(body.Repo) == "" || strings.TrimSpace(body.Title) == "" {
		h.fail(w, http.StatusBadRequest, "new job requires repo and title")
		return
	}
	if body.Autostart && !h.requireVerb(w, h.seam.Launch != nil) {
		return
	}
	// New has one words argument today. Join the title and brief here so a
	// wire client does not need to know the seam's terminal-input shape.
	words := body.Title
	if body.Text != "" {
		words += "\n\n" + body.Text
	}
	id, err := h.seam.New(body.Repo, words)
	if h.failed(w, err) {
		return
	}
	if body.Autostart && h.failed(w, h.seam.Launch(id)) {
		return
	}
	h.write(w, http.StatusCreated, struct {
		Factory api.Factory `json:"factory"`
		ID      int         `json:"id"`
	}{h.factory, id})
}

func (h *Handler) setRepos(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.SetRepos != nil) {
		return
	}
	var body struct {
		Watched *[]string `json:"watched"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Watched == nil {
		h.fail(w, http.StatusBadRequest, "watched must be an array of repository names")
		return
	}
	h.finish(w, h.seam.SetRepos(*body.Watched))
}

func (h *Handler) connectGitHub(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.seam.ConnectGitHub != nil) {
		return
	}
	var body struct {
		Token *string `json:"token"`
	}
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Token == nil {
		h.fail(w, http.StatusBadRequest, "connect requires token; an empty token selects gh")
		return
	}
	h.finish(w, h.seam.ConnectGitHub(r.Context(), *body.Token))
}

func (h *Handler) words(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Words string `json:"words"`
	}
	if !h.decode(w, r, &body, false) {
		return "", false
	}
	if strings.TrimSpace(body.Words) == "" {
		h.fail(w, http.StatusBadRequest, "words must not be empty")
		return "", false
	}
	return body.Words, true
}

// decode bounds input before touching the seam and rejects extra objects or
// misspelled fields, since silently ignoring intent would change the job.
func (h *Handler) decode(w http.ResponseWriter, r *http.Request, out any, optional bool) bool {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(w, http.StatusRequestEntityTooLarge, "JSON body exceeds 1 MiB")
		} else {
			h.fail(w, http.StatusBadRequest, "could not read JSON body")
		}
		return false
	}
	data = bytes.TrimSpace(data)
	if optional && len(data) == 0 {
		return true
	}
	if len(data) == 0 || data[0] != '{' {
		h.fail(w, http.StatusBadRequest, "body must be a JSON object")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		h.fail(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	if d.Decode(new(any)) != io.EOF {
		h.fail(w, http.StatusBadRequest, "body must contain one JSON object")
		return false
	}
	return true
}

func (h *Handler) requireVerb(w http.ResponseWriter, exists bool) bool {
	if !exists {
		h.fail(w, http.StatusNotImplemented, "this factory does not provide that action")
	}
	return exists
}

func (h *Handler) finish(w http.ResponseWriter, err error) {
	if !h.failed(w, err) {
		h.write(w, http.StatusOK, struct {
			Factory api.Factory `json:"factory"`
		}{h.factory})
	}
}
