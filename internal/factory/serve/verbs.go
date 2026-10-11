package serve

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func (h *Handler) verbs() {
	s := h.seam
	h.postJob("run", s.Launch != nil, h.recordedID(s.Launch, record.Started))
	h.postJob("stop", s.Stop != nil, h.stop)
	h.postJob("pause", s.Hold != nil, h.holdVerb(true))
	h.postJob("resume", s.Hold != nil, h.holdVerb(false))
	h.postJob("drop", s.Dismiss != nil, h.recordedID(s.Dismiss, record.Dropped))
	h.postJob("answer", s.Answer != nil, h.answer)
	h.postJob("steer", s.Steer != nil, h.wordsVerb(s.Steer))
	h.postJob("approve", true, h.approve)
	h.postJob("request_changes", s.SendBack != nil, h.recordedWords(s.SendBack, record.RequestChanges))
	h.postJob("recheck", s.Reverify != nil, h.idVerb(s.Reverify))
	// Shape is present even when the seam has no Shape door: a worker holding
	// the job is signaled, and only a job nobody holds needs the local turn.
	h.postJob("shape", true, h.shape)
	// Take is the lease desk's own door, not a seam field. The desk exists
	// only after the worker routes open it, so the handler checks then.
	h.postJob("take", true, h.take)
	h.postJob("prioritize", s.Mark != nil, h.prioritize)
	h.postJob("reply", h.config.Store != nil || s.Say != nil, h.reply)
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

// take is POST /v1/jobs/{id}/take. It names the connected person on the job
// and leaves the work queued. A job the person rule already gave to someone
// else is refused in a sentence, and the refusal is not recorded.
func (h *Handler) take(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if !h.workerReady(w) {
		return
	}
	me, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if me == "" {
		h.fail(w, http.StatusBadRequest, "take requires a GitHub login")
		return
	}
	if err := h.workers.desk.Assign(it.ID, me); h.takeFailed(w, err) {
		return
	}
	if h.config.Record != nil {
		if _, err := h.config.Record.Append(r.Context(), record.Event{Kind: record.Took, Job: it.ID, Handle: me, Cost: jobCost(it), Detail: h.machineFor(me)}); h.failed(w, err) {
			return
		}
	}
	updated, err := h.workers.st.Get(it.ID)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		api.Job
	}{h.factory, api.FromItem(updated)})
}

// takeFailed maps the person rule and a live worker hold onto 409. The
// sentence is what the dashboard shows; the lease package prefix stays off it.
func (h *Handler) takeFailed(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, lease.ErrPerson):
		h.fail(w, http.StatusConflict, "job belongs to another person")
		return true
	case errors.Is(err, lease.ErrLeased):
		h.fail(w, http.StatusConflict, "job is already taken")
		return true
	case errors.Is(err, store.ErrNotFound):
		h.fail(w, http.StatusNotFound, "job not found")
		return true
	default:
		return h.failed(w, err)
	}
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
	h.finish(w, h.recordAnswer(r, it, yes, body.Words))
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
	event, err := h.recordApproval(r, it, body.Edited)
	if h.failed(w, err) {
		return
	}
	if h.failed(w, door()) {
		return
	}
	if it.Question != "" {
		handle, err := h.callerHandle(r)
		if h.failed(w, err) {
			return
		}
		at := time.Now()
		if h.workers != nil {
			at = h.workers.now()
		}
		if h.failed(w, h.keepAnswered(it, factory.Answer{By: handle, Text: "yes", At: at, Event: event})) {
			return
		}
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

// shape is POST /v1/jobs/{id}/shape. A worker holding the job hears it as a
// lease signal and gives the manager the turn on its own copy. A job nobody
// holds is shaped here, which is where that job's worker lives.
func (h *Handler) shape(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if h.workers != nil && h.workers.desk != nil {
		held, err := h.workers.desk.SignalIfHeld(it.ID, lease.Signal{Verb: lease.SignalShape})
		if h.failed(w, err) {
			return
		}
		if held {
			h.finish(w, nil)
			return
		}
	}
	if !h.requireVerb(w, h.seam.Shape != nil) {
		return
	}
	line, err := h.seam.Shape(r.Context(), it.ID)
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Factory api.Factory `json:"factory"`
		Line    string      `json:"line,omitempty"`
	}{h.factory, line})
}

func (h *Handler) reply(w http.ResponseWriter, r *http.Request, it factory.Item) {
	if words, ok := h.words(w, r); ok {
		if h.config.Store != nil {
			if !h.workerReady(w) {
				return
			}
			from, err := h.callerHandle(r)
			if h.failed(w, err) {
				return
			}
			if from == "" {
				from = "user"
			}
			h.finish(w, h.workers.desk.Reply(it.ID, from, words))
			return
		}
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
	if !h.repoVisible(r, body.Repo) {
		h.fail(w, http.StatusNotFound, "repository not found")
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
	if body.Autostart && h.failed(w, h.recordAction(r, factory.Item{ID: id}, record.AutoStarted, "", func() error { return h.seam.Launch(id) })) {
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

// stop supplies the caller before the lease door writes its durable stop receipt.
func (h *Handler) stop(w http.ResponseWriter, r *http.Request, it factory.Item) {
	handle, err := h.callerHandle(r)
	if h.failed(w, err) {
		return
	}
	if h.workers.desk != nil {
		h.finish(w, lease.NewDoors(h.workers.desk).StopAs(it.ID, handle))
		return
	}
	h.finish(w, h.recordAction(r, it, record.Stopped, "", func() error { return h.seam.Stop(it.ID) }))
}
