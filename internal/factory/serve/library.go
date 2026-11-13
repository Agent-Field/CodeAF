package serve

import (
	"errors"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// ── THE FACTORY'S PIPELINE LIBRARY ──────────────────────────────────────────
//
// GET and PUT /v1/pipeline-library answer and replace the factory's library
// (internal/factory/pipeline, library.go): named pipelines and the step
// definitions they use, kept in THIS FACTORY'S STORE for every repository it
// works on, so a Start and a routing rule can pick one on any of them. A
// repository's own pipeline of the same name overrides it ([pipeline.File.Resolve]).
//
// READING IS EVERYONE'S WHO MAY READ THE FACTORY; REPLACING IS THE OPERATOR'S,
// as the routing table and the factory's rules are, because a library
// pipeline runs on every repository whose jobs pick it. A reader learns
// whether it may through can.change.
//
// A PUT IS CHECKED WHOLE ([pipeline.CheckLibrary]): `pipeline:` and `steps:`
// only, read with no problem, no pipeline named pr or ci, every listed step
// one the runner runs. It is kept as the factory writes it, which writes only
// what the text wrote. text is required ("" is an empty library) and so is
// expect.version, the version the writer read: a library written since
// refuses the write with 409 and nothing is written. Every refusal before the
// write carries the phase marker (phase.go).
func (h *Handler) pipelineLibrary(w http.ResponseWriter, r *http.Request) {
	if !h.requireVerb(w, h.config.Store != nil) {
		return
	}
	if r.Method == http.MethodGet {
		kept, err := h.config.Store.PipelineLibrary()
		if h.failed(w, err) {
			return
		}
		h.write(w, http.StatusOK, h.libraryAnswer(r, kept))
		return
	}
	if !h.requireOperator(w, r) {
		return
	}
	var body api.PipelineLibraryRequest
	if !h.decode(w, r, &body, false) {
		return
	}
	if body.Text == nil {
		h.fail(w, http.StatusUnprocessableEntity, `text is required; send "" for an empty library`)
		return
	}
	if body.Expect == nil || body.Expect.Version < 1 {
		h.fail(w, http.StatusUnprocessableEntity, "expect.version is required: send the version of the library you read")
		return
	}
	data, refused := pipeline.CheckLibrary(*body.Text)
	if refused != "" {
		h.fail(w, http.StatusUnprocessableEntity, refused)
		return
	}
	clearPhase(w)
	kept, err := h.config.Store.SetPipelineLibrary(string(data), h.rulesChange(r), body.Expect.Version)
	if errors.Is(err, store.ErrLibraryMoved) {
		// Refused before anything was written: a proven no-write.
		w.Header().Set(api.PhaseHeader, api.PhasePreMutation)
		h.fail(w, http.StatusConflict, err.Error()+"; read it again, then make the change")
		return
	}
	if h.failed(w, err) {
		return
	}
	h.write(w, http.StatusOK, h.libraryAnswer(r, kept))
}

// libraryAnswer is the library as kept, with every pipeline it names.
func (h *Handler) libraryAnswer(r *http.Request, kept store.KeptLibrary) api.PipelineLibraryAnswer {
	out := api.PipelineLibraryAnswer{Factory: h.factory, Text: kept.Text, Version: kept.Version, Pipelines: []api.PipelineChoice{},
		Can: api.PipelineLibraryCan{Change: h.isOperator(r)}, RulesChange: kept.RulesChange}
	lib, err := pipeline.ParseLibrary(kept.Text)
	if err != nil {
		return out
	}
	for _, res := range pipeline.WithLibrary(pipeline.File{}, lib).Pipelines() {
		if res.From == pipeline.FromLibrary {
			out.Pipelines = append(out.Pipelines, api.PipelineChoice{Name: res.Name, From: res.From, Steps: res.Steps})
		}
	}
	return out
}

// pipelineChoices is every pipeline a job on f can run, as "Pick the
// pipeline" offers them ([pipeline.File.Pipelines]).
func pipelineChoices(f pipeline.File) []api.PipelineChoice {
	out := []api.PipelineChoice{}
	for _, res := range f.Pipelines() {
		out = append(out, api.PipelineChoice{Name: res.Name, From: res.From, Steps: res.Steps, Over: res.Over})
	}
	return out
}
