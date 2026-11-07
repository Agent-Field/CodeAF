package serve

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// IntakePath is where outside work is posted: a ticket tracker's automation,
// a webhook, a script, `codeaf factory add`. It sits outside /v1 because it
// is the one address a person types into another system's settings, and its
// shape is [inbox.Entry], which is the inbox's to keep.
const IntakePath = "/api/intake"

func (h *Handler) intakeRoutes() {
	h.route(IntakePath, map[string]http.HandlerFunc{http.MethodPost: h.intake})
	h.triggerRoutes()
}

// intake is POST /api/intake: one [inbox.Entry] filed through [inbox.Add],
// answered {id, created}. THIS HANDLER ONLY ADAPTS — it reads the body,
// checks who is calling, and hands over; the checking of the entry and the
// filing are the inbox's, the same for every door.
//
// ONLY THE OPERATOR FILES WORK FROM OUTSIDE: the factory's access token (what
// an automation holds) or the operator's own session. A teammate's session is
// refused, because what is filed here starts with the owner's standing.
func (h *Handler) intake(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, inbox.MaxRequest))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.fail(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("an intake body is at most %d KiB", inbox.MaxRequest>>10))
		} else {
			h.fail(w, http.StatusUnprocessableEntity, "could not read JSON body")
		}
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(data))
	var e inbox.Entry
	if !h.decode(w, r, &e, false) {
		return
	}
	clean, err := inbox.Validate(e)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	// Work for a repository that is off is refused in the words every other
	// door uses, before anything is filed.
	if clean.Repo != "" && h.refuseOff(w, clean.Repo, "") {
		return
	}
	var opts []inbox.Option
	if h.seam.RecipeAt != nil {
		opts = append(opts, inbox.WithRecipe(func(repo string) (factory.Recipe, error) {
			recipe, _, _, err := h.seam.RecipeAt(repo)
			return recipe, err
		}))
	}
	out, err := inbox.Add(r.Context(), h.config.Store, e, h.clock(), opts...)
	if h.failed(w, err) {
		return
	}
	status := http.StatusOK
	if out.Created {
		status = http.StatusCreated
	}
	h.write(w, status, out)
}

// IntakeJobPath is one filed job's status (GET) and, under /messages, the
// door that says more about it (POST). Both are the operator's, like
// POST /api/intake, and both only adapt HTTP to internal/factory/trigger,
// whose [trigger.Message] and [trigger.Status] AgentField's reasoners call too.
const IntakeJobPath = IntakePath + "/{source}/{id}"

// WebhooksPath is the operator's list of webhooks (GET), where one is set
// (POST), and under /{source} where one is removed (DELETE).
const WebhooksPath = "/v1/webhooks"

// triggerState is the floor's webhooks while the handler runs.
type triggerState struct {
	hub  *trigger.Hub
	stop context.CancelFunc
	done chan struct{}
	once sync.Once
}

func (h *Handler) triggerRoutes() {
	h.route(IntakeJobPath, map[string]http.HandlerFunc{http.MethodGet: h.intakeStatus})
	h.route(IntakeJobPath+"/messages", map[string]http.HandlerFunc{http.MethodPost: h.intakeMessage})
	h.route(WebhooksPath, map[string]http.HandlerFunc{http.MethodGet: h.webhooks, http.MethodPost: h.addWebhook})
	h.route(WebhooksPath+"/{source}", map[string]http.HandlerFunc{http.MethodDelete: h.removeWebhook})
	if h.config.Store == nil {
		return
	}
	// The webhook sink goes on the bus, and the outbox's sender runs until
	// Close.
	hub := trigger.Open(h.config.Store.Root())
	ctx, cancel := context.WithCancel(context.Background())
	t := &triggerState{hub: hub, stop: cancel, done: make(chan struct{})}
	h.triggers = t
	guard.Go("factory webhook outbox", func() {
		defer close(t.done)
		hub.Out.Run(ctx)
	})
}

func (h *Handler) closeTriggers() {
	t := h.triggers
	if t == nil {
		return
	}
	t.once.Do(func() {
		t.stop()
		<-t.done
		t.hub.Close()
	})
}

// intakeStatus is GET /api/intake/{source}/{id} ([trigger.Status]).
func (h *Handler) intakeStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	out, err := trigger.Status(h.config.Store, r.PathValue("source"), r.PathValue("id"))
	if errors.Is(err, inbox.ErrNoJob) {
		h.fail(w, http.StatusNotFound, err.Error())
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, out)
}

// messageBody is POST /api/intake/{source}/{id}/messages.
type messageBody struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// intakeMessage is POST /api/intake/{source}/{id}/messages
// ([trigger.Message]): the words reach the job's manager as a comment on an
// issue does, held to the job's trust. It answers {job, accepted}.
func (h *Handler) intakeMessage(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) || !h.requireOperator(w, r) {
		return
	}
	var body messageBody
	if !h.decode(w, r, &body, false) {
		return
	}
	said := trigger.Said{Source: r.PathValue("source"), ID: r.PathValue("id"), Author: body.Author, Text: body.Text}
	out, err := trigger.Message(r.Context(), h.config.Store, said, h.clock())
	var refused inbox.Refused
	switch {
	case errors.Is(err, inbox.ErrNoJob):
		h.fail(w, http.StatusNotFound, err.Error())
		return
	case errors.As(err, &refused):
		h.fail(w, http.StatusUnprocessableEntity, refused.Words)
		return
	case h.failed(w, err):
		return
	}
	h.write(w, http.StatusOK, out)
}

// webhookBody is POST /v1/webhooks.
type webhookBody struct {
	Source string `json:"source"`
	URL    string `json:"url"`
}

// webhooks is GET /v1/webhooks: every webhook, never its secret.
func (h *Handler) webhooks(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.triggers != nil) || !h.requireOperator(w, r) {
		return
	}
	all, err := h.triggers.hub.Webhooks.Load()
	if h.failed(w, err) {
		return
	}
	out := make([]trigger.Public, 0, len(all))
	for _, hook := range all {
		out = append(out, hook.Public())
	}
	h.write(w, http.StatusOK, struct {
		Webhooks []trigger.Public `json:"webhooks"`
	}{out})
}

// addWebhook is POST /v1/webhooks {source, url}: it sets the source's webhook
// and answers its signing secret, THE ONLY TIME IT IS SHOWN. A source that
// had one is given the new address and a new secret.
func (h *Handler) addWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.triggers != nil) || !h.requireOperator(w, r) {
		return
	}
	var body webhookBody
	if !h.decode(w, r, &body, false) {
		return
	}
	added, err := h.triggers.hub.Webhooks.Add(body.Source, body.URL, h.clock())
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	h.write(w, http.StatusCreated, added)
}

// removeWebhook is DELETE /v1/webhooks/{source}.
func (h *Handler) removeWebhook(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.triggers != nil) || !h.requireOperator(w, r) {
		return
	}
	source := r.PathValue("source")
	err := h.triggers.hub.Webhooks.Remove(source)
	if errors.Is(err, trigger.ErrNotFound) {
		h.fail(w, http.StatusNotFound, err.Error())
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, struct {
		Removed string `json:"removed"`
	}{source})
}
