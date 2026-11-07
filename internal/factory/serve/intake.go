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
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/trigger"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// IntakePath is where outside work is posted: a ticket tracker's automation,
// a webhook, a script, `codeaf factory add`. It sits outside /v1 because it
// is the one address a person types into another system's settings, and its
// shape is [inbox.Entry], which is the inbox's to keep.
const IntakePath = "/api/intake"

// IntakeJobPath is one filed job's status (GET) and, under /messages, the
// door a source says more about it through (POST).
const IntakeJobPath = IntakePath + "/{source}/{id}"

// SourcesPath is the operator's list of registered sources (GET), where one
// is registered (POST), and under /{name} where one is revoked (DELETE).
const SourcesPath = "/v1/sources"

// triggerState is the floor's Trigger API while the handler runs.
type triggerState struct {
	hub  *trigger.Hub
	stop context.CancelFunc
	done chan struct{}
	once sync.Once
}

// sourceKey carries the registered source a request's token belongs to.
type sourceKey struct{}

// isIntakePath is every path a source's token opens.
func isIntakePath(p string) bool {
	return p == IntakePath || (len(p) > len(IntakePath) && p[:len(IntakePath)+1] == IntakePath+"/")
}

// sourceRequest is r marked with the registered source whose token it
// carries, ON AN INTAKE PATH ONLY; anywhere else a source token is not
// looked up at all, so it opens nothing.
func (h *Handler) sourceRequest(r *http.Request) (*http.Request, bool) {
	if h.triggers == nil || !isIntakePath(r.URL.Path) {
		return r, false
	}
	token := bearer(r)
	if token == "" || h.floorToken(token) {
		return r, false
	}
	src, ok := h.triggers.hub.Registry.Match(token)
	if !ok {
		return r, false
	}
	return r.WithContext(context.WithValue(r.Context(), sourceKey{}, src)), true
}

// callerSource is the registered source r was authorized as, when it was.
func callerSource(r *http.Request) (trigger.Source, bool) {
	src, ok := r.Context().Value(sourceKey{}).(trigger.Source)
	return src, ok
}

func (h *Handler) intakeRoutes() {
	h.route(IntakePath, map[string]http.HandlerFunc{http.MethodPost: h.intake})
	h.route(IntakeJobPath, map[string]http.HandlerFunc{http.MethodGet: h.intakeStatus})
	h.route(IntakeJobPath+"/messages", map[string]http.HandlerFunc{http.MethodPost: h.intakeMessage})
	h.route(SourcesPath, map[string]http.HandlerFunc{http.MethodGet: h.sources, http.MethodPost: h.addSource})
	h.route(SourcesPath+"/{name}", map[string]http.HandlerFunc{http.MethodDelete: h.revokeSource})
	h.startTriggers()
}

// startTriggers opens the floor's Trigger API: the answerers for every source
// with a callback, and the outbox's sender, which runs until Close.
func (h *Handler) startTriggers() {
	if h.config.Store == nil {
		return
	}
	hub := trigger.Open(h.config.Store.Root())
	ctx, cancel := context.WithCancel(context.Background())
	t := &triggerState{hub: hub, stop: cancel, done: make(chan struct{})}
	h.triggers = t
	_ = hub.Sync()
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

// intakeCaller is who may file, read and message work from source: the
// operator for any source, a registered source only for its own. It answers
// the source when the caller is one, and refuses everyone else.
func (h *Handler) intakeCaller(w http.ResponseWriter, r *http.Request, source string) (trigger.Source, bool, bool) {
	if src, ok := callerSource(r); ok {
		if source != src.Name {
			h.fail(w, http.StatusForbidden, fmt.Sprintf("this token files and reads only work from %s", src.Name))
			return trigger.Source{}, false, false
		}
		return src, true, true
	}
	if !h.requireOperator(w, r) {
		return trigger.Source{}, false, false
	}
	return trigger.Source{}, false, true
}

// intake is POST /api/intake: one [inbox.Entry] filed through [inbox.Add],
// answered {id, created}. THIS HANDLER ONLY ADAPTS — it reads the body,
// checks who is calling, and hands over; the checking of the entry and the
// filing are the inbox's, the same for every door.
//
// THE OPERATOR FILES ANY WORK: the factory's access token (what an
// automation holds) or the operator's own session; a teammate's session is
// refused, because what is filed here starts with the owner's standing. A
// REGISTERED SOURCE FILES ONLY ITS OWN: the entry's source must be its name
// (an empty one is taken as it), and its trust is capped at the source's.
func (h *Handler) intake(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	src, viaSource := callerSource(r)
	if !viaSource && !h.requireOperator(w, r) {
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
	if viaSource {
		if e.Source == "" {
			e.Source = src.Name
		}
		if _, ok, _ := h.intakeCaller(w, r, e.Source); !ok {
			return
		}
	}
	clean, err := inbox.Validate(e)
	if err != nil {
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if viaSource {
		e.Trust = src.Cap(clean.Trust)
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

// intakeStatus is GET /api/intake/{source}/{id}: how far the job is
// ([api.TriggerStatus]).
func (h *Handler) intakeStatus(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	source, id := r.PathValue("source"), r.PathValue("id")
	if _, _, ok := h.intakeCaller(w, r, source); !ok {
		return
	}
	it, err := inbox.Find(h.config.Store, source, id)
	if errors.Is(err, inbox.ErrNoJob) {
		h.fail(w, http.StatusNotFound, err.Error())
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, api.TriggerStatus(it))
}

// messageBody is POST /api/intake/{source}/{id}/messages.
type messageBody struct {
	Author string `json:"author"`
	Text   string `json:"text"`
}

// intakeMessage is POST /api/intake/{source}/{id}/messages: the words reach
// the job's manager as a comment on an issue does ([inbox.Message]), at the
// lower of the job's trust and the source's. It answers {job, accepted}.
func (h *Handler) intakeMessage(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	source, id := r.PathValue("source"), r.PathValue("id")
	src, viaSource, ok := h.intakeCaller(w, r, source)
	if !ok {
		return
	}
	var body messageBody
	if !h.decode(w, r, &body, false) {
		return
	}
	said := inbox.Said{Source: source, ID: id, Author: body.Author, Text: body.Text}
	if viaSource {
		said.Trust = src.Trust
	}
	heard, err := inbox.Message(r.Context(), h.config.Store, said, h.clock())
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
	h.write(w, http.StatusOK, struct {
		Job      int  `json:"job"`
		Accepted bool `json:"accepted"`
	}{heard.Job, true})
}

// sourceBody is POST /v1/sources.
type sourceBody struct {
	Name     string       `json:"name"`
	Callback string       `json:"callback"`
	Trust    factory.Tier `json:"trust"`
}

// sources is GET /v1/sources: every registered source, never its secrets.
func (h *Handler) sources(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.triggers != nil) || !h.requireOperator(w, r) {
		return
	}
	all, err := h.triggers.hub.Registry.Load()
	if h.failed(w, err) {
		return
	}
	out := make([]trigger.Public, 0, len(all))
	for _, s := range all {
		out = append(out, s.Public())
	}
	h.write(w, http.StatusOK, struct {
		Sources []trigger.Public `json:"sources"`
	}{out})
}

// addSource is POST /v1/sources {name, callback?, trust?}: it registers the
// source and answers its token and signing secret, THE ONLY TIME EITHER IS
// SHOWN.
func (h *Handler) addSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.triggers != nil) || !h.requireOperator(w, r) {
		return
	}
	var body sourceBody
	if !h.decode(w, r, &body, false) {
		return
	}
	minted, err := h.triggers.hub.Registry.Add(body.Name, body.Callback, body.Trust, h.clock())
	switch {
	case errors.Is(err, trigger.ErrExists):
		h.fail(w, http.StatusConflict, err.Error())
		return
	case err != nil:
		h.fail(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	_ = h.triggers.hub.Sync()
	h.write(w, http.StatusCreated, minted)
}

// revokeSource is DELETE /v1/sources/{name}.
func (h *Handler) revokeSource(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.triggers != nil) || !h.requireOperator(w, r) {
		return
	}
	name := r.PathValue("name")
	err := h.triggers.hub.Registry.Revoke(name)
	if errors.Is(err, trigger.ErrNotFound) {
		h.fail(w, http.StatusNotFound, err.Error())
		return
	}
	if h.failed(w, err) {
		return
	}
	_ = h.triggers.hub.Sync()
	h.write(w, http.StatusOK, struct {
		Revoked string `json:"revoked"`
	}{name})
}
