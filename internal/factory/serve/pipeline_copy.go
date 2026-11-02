package serve

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// ── THE FACTORY'S OWN COPY OF A REPOSITORY'S STEPS ──────────────────────────
//
// GET, PUT and DELETE /v1/repos/{owner}/{repo}/pipeline/factory-copy answer,
// save and stop the factory's own copy of one repository's
// .codeaf/factory.yaml ([factory.PipelineCopy]): the file a person edited in
// the app, kept in THIS FACTORY'S STORE so the jobs it starts afterwards run it
// at once, with no push and no pull request. The owner decided this on
// 2026-10-28, knowing it departs from "team law lives in the repository" (see
// factory.RecipeSource for who reads what, and the precedence).
//
// ONE PATH FOR EVERY FACTORY. A solo factory and a team server answer the same
// route the same way; what differs is only who its operator is. SAVING AND
// STOPPING ARE THE OPERATOR'S, as the factory's own rules are (rules.go),
// because the copy changes what every job on the repository runs, whoever
// starts it. On a solo factory the one person is its operator. A reader learns
// whether it may through can.use_on_factory, on this answer and on the
// edit-source read (pipeline_edit_source.go), and a client offers the action
// only then.
//
// A SAVE IS CHECKED EXACTLY AS A PULL REQUEST IS ([pipelineFileFor]) and kept
// as the factory writes it, so the copy and a pull request of the same steps
// are the same bytes, and the copy can clear itself when main's file becomes
// that file. Every save names who saved it and when. Both writes require
// expect.version, the copy's version as the writer read it: a copy saved or
// stopped since refuses the write with 409, and nothing is written. Every
// refusal before the write carries the phase marker (phase.go).
func (h *Handler) pipelineCopy(w http.ResponseWriter, r *http.Request) {
	owner, repo, ok := h.splitRepo(w, r)
	if !ok {
		return
	}
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	full := owner + "/" + repo
	if r.Method == http.MethodGet {
		cur, version, err := h.config.Store.PipelineCopy(full)
		if h.failed(w, err) {
			return
		}
		h.write(w, http.StatusOK, h.pipelineCopyAnswer(r, full, cur, version))
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body api.PipelineCopyRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Expect == nil || body.Expect.Version < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "expect.version is required: send the version of the copy you read")
		return
	}
	var next *factory.PipelineCopy
	if r.Method == http.MethodPut {
		data, refused := pipelineFileFor(body.Text)
		if refused != "" {
			h.fail(w, http.StatusUnprocessableEntity, refused)
			return
		}
		who, err := h.callerHandle(r)
		if h.failed(w, err) {
			return
		}
		next = &factory.PipelineCopy{Repo: full, Text: string(data), By: factory.NormalizeHandle(who), At: h.clock().UTC()}
	} else if body.Text != "" {
		h.fail(w, http.StatusUnprocessableEntity, "stopping the factory's copy takes no text")
		return
	}
	clearPhase(w)
	saved, version, err := h.config.Store.ChangePipelineCopy(full, func(cur *factory.PipelineCopy, version int) (*factory.PipelineCopy, bool, error) {
		if version != body.Expect.Version {
			return cur, false, errCopyMoved
		}
		// Stopping a copy that is not kept changes nothing and writes nothing.
		return next, next != nil || cur != nil, nil
	})
	if errors.Is(err, errCopyMoved) {
		// Refused before anything was written: a proven no-write.
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		h.fail(w, http.StatusConflict, errCopyMoved.Error()+"; read it again, then make the change")
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, h.pipelineCopyAnswer(r, full, saved, version))
}

// errCopyMoved refuses a write whose reader's version of the copy is no longer
// the factory's.
var errCopyMoved = errors.New("the factory's copy of these steps changed since it was read")

func (h *Handler) pipelineCopyAnswer(r *http.Request, repo string, c *factory.PipelineCopy, version int) api.PipelineCopyAnswer {
	out := api.PipelineCopyAnswer{Factory: h.factory, Repo: repo, Version: version, Can: h.pipelineCan(r)}
	if c != nil {
		out.FactoryCopy = &api.PipelineCopyState{Text: c.Text, By: c.By, At: c.At}
	}
	return out
}

// pipelineCan is what the caller may do with a repository's pipeline here.
func (h *Handler) pipelineCan(r *http.Request) api.PipelineCan {
	return api.PipelineCan{UseOnFactory: h.config.Store != nil && h.isOperator(r)}
}

// withPipelineCopy puts the factory's copy, its version and the caller's Can
// on an edit-source answer whose main's file is already read, and CLEARS THE
// COPY when that file is now the same file (its pull request merged): the
// answer then says main "same" once, and its text is main's, which is what
// runs. A factory without a store answers none of the three.
func (h *Handler) withPipelineCopy(r *http.Request, out *api.PipelineEditSource) error {
	if h.config.Store == nil {
		return nil
	}
	can := h.pipelineCan(r)
	out.Can = &can
	cur, version, err := h.config.Store.PipelineCopy(out.Repo)
	if err != nil {
		return err
	}
	out.CopyVersion = version
	if cur == nil {
		return nil
	}
	state := &api.PipelineCopyState{Text: cur.Text, By: cur.By, At: cur.At, Main: api.PipelineCopyMainDiffers}
	switch {
	case out.State == api.PipelineEditMissing:
		state.Main = api.PipelineCopyMainMissing
	case out.State == api.PipelineEditFile && pipeline.SameFile(out.Text, cur.Text):
		kept, v, err := h.config.Store.ChangePipelineCopy(out.Repo, func(now *factory.PipelineCopy, _ int) (*factory.PipelineCopy, bool, error) {
			if !now.Same(cur) {
				return now, false, nil
			}
			return nil, true, nil
		})
		if err != nil {
			return err
		}
		out.CopyVersion = v
		if kept == nil {
			factory.NoteRecipeSource(out.Repo, factory.FromMainBranch)
			state.Main = api.PipelineCopyMainSame
		} else {
			// Saved again while this read ran: that save stands.
			state = &api.PipelineCopyState{Text: kept.Text, By: kept.By, At: kept.At, Main: api.PipelineCopyMainDiffers}
		}
	}
	out.FactoryCopy = state
	return nil
}

// keptPipelineCopy puts on a refused edit-source read the copy the factory
// keeps for repo, if any: who saved it and when, its version and the caller's
// Can, so the reader can still stop using it although the file was not read.
// Main is not said (main's file was not read), the copy is never cleared here,
// and a copy that cannot be read is left out.
func (h *Handler) keptPipelineCopy(r *http.Request, repo string, out *api.PipelineEditSourceFailure) {
	if h.config.Store == nil {
		return
	}
	cur, version, err := h.config.Store.PipelineCopy(repo)
	if err != nil || cur == nil {
		return
	}
	can := h.pipelineCan(r)
	out.FactoryCopy = &api.PipelineCopyState{By: cur.By, At: cur.At}
	out.CopyVersion, out.Can = version, &can
}
