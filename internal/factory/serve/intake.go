package serve

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// IntakePath is where outside work is posted: a ticket tracker's automation,
// a webhook, a script, `codeaf factory add`. It sits outside /v1 because it
// is the one address a person types into another system's settings, and its
// shape is [inbox.Entry], which is the inbox's to keep.
const IntakePath = "/api/intake"

func (h *Handler) intakeRoutes() {
	h.route(IntakePath, map[string]http.HandlerFunc{http.MethodPost: h.intake})
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
