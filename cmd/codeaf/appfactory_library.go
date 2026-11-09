package main

// ── codeaf app: THE FACTORY'S PIPELINE LIBRARY ──────────────────────────────
//
// The engine's half of appwire.FactoryLibraryKeeper. The read is one GET of
// /v1/pipeline-library; the write is ONE PUT of the same route with a body of
// exactly the text and the version read. Only the operator may write, and the
// refusal is made here, in words, before anything leaves. The preview asks
// the factory nothing: it is the same parser and encoder the factory keeps
// the library with (pipeline.LibraryEdited).
//
// A WRITE IS SENT ONCE, never retried. Only a refusal carrying the phase
// marker (api.PhaseHeader) proves nothing was saved; any other failure is
// appwire.CodeUncertain, and factory.library says what the factory kept.
//
// THE ANSWER IS NOT TAKEN ON TRUST: a saved library is believed only when it
// is the text this engine would keep (pipeline.CheckLibrary), at a version
// past the one read, or the text already kept at the version read (writing
// the library it already holds writes nothing).

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var _ appwire.FactoryLibraryKeeper = (*appBackend)(nil)

const appFactoryLibraryRoute = "/v1/pipeline-library"

func (b *appBackend) FactoryLibrary(ctx context.Context, id string) (appwire.FactoryLibrary, error) {
	var got api.PipelineLibraryAnswer
	err := b.factoryHub().read(ctx, id, appFactoryLibraryRoute, appFactoryBody, &got)
	switch {
	case routeMissing(err):
		return appwire.FactoryLibrary{}, factoryTooOld()
	case err != nil:
		return appwire.FactoryLibrary{}, err
	}
	out, ok := libraryAnswer(id, got)
	if !ok {
		return appwire.FactoryLibrary{}, appwire.Errorf(appwire.CodeEngine, "the factory sent a pipeline library this app cannot read")
	}
	return out, nil
}

func (b *appBackend) FactoryLibrarySet(ctx context.Context, p appwire.FactoryLibrarySetParams) (appwire.FactoryLibrary, error) {
	none := appwire.FactoryLibrary{}
	p, err := appwire.CheckLibrarySet(p)
	if err != nil {
		return none, err
	}
	h := b.factoryHub()
	release, err := h.claimAdmin(p.Factory)
	if err != nil {
		return none, err
	}
	defer release()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return none, err
	}
	if !who.me.Operator {
		return none, appwire.Errorf(appwire.CodeForbidden, "only the factory's operator can change its pipeline library")
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	body := api.PipelineLibraryRequest{Text: p.Text, Expect: &api.PipelineCopyExpect{Version: p.Expect.Version}}
	r, err := h.linkDo(ctx, who, http.MethodPut, appFactoryLibraryRoute, body, appFactoryBody)
	switch {
	case err == errLinkNotSent:
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	case err != nil:
		return none, libraryUncertain()
	}
	if !h.callerIs(p.Factory, who.token) {
		return none, appwire.Errorf(appwire.CodeUncertain, "the sign-in changed while this was saving, and the result is not known; read the library before trying again")
	}
	// AN OLDER FACTORY HAS NO SUCH ROUTE: it answers 404 or 405 with no phase
	// marker, and a route it does not have wrote nothing.
	if (r.status == http.StatusNotFound || r.status == http.StatusMethodNotAllowed) && !r.provedNoWrite() {
		return none, factoryTooOld()
	}
	if r.provedNoWrite() {
		switch r.status {
		case http.StatusConflict:
			return none, appwire.Errorf(appwire.CodeStale, "the factory's pipeline library changed since it was read; nothing was changed")
		case http.StatusUnprocessableEntity:
			return none, appwire.Errorf(appwire.CodeBadParams, "the factory would not take this library; nothing was saved")
		}
		return none, linkRefusal(r, "the factory refused this library")
	}
	if r.status != http.StatusOK || r.bodyBad {
		return none, libraryUncertain()
	}
	var got api.PipelineLibraryAnswer
	if json.Unmarshal(r.body, &got) != nil {
		return none, libraryUncertain()
	}
	out, ok := libraryAnswer(p.Factory, got)
	if !ok || out.Text != *p.Text || out.Version < p.Expect.Version {
		return none, libraryUncertain()
	}
	h.touch(p.Factory)
	return out, nil
}

// FactoryLibraryPreview asks the factory nothing: it holds the caller to the
// connection and computes the library the edits make.
func (b *appBackend) FactoryLibraryPreview(ctx context.Context, p appwire.FactoryLibraryPreviewParams) (appwire.FactoryLibraryPreview, error) {
	h := b.factoryHub()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryLibraryPreview{}, err
	}
	out, err := appwire.LibraryPreview(p)
	if stale := h.staleCredential(p.Factory, who.token); stale != nil {
		return appwire.FactoryLibraryPreview{}, stale
	}
	if err != nil {
		return appwire.FactoryLibraryPreview{}, err
	}
	h.touch(p.Factory)
	return out, nil
}

// libraryAnswer is the factory's answer as the wire says it, and whether it
// reads: a version from 1 and every pipeline from the library.
func libraryAnswer(id string, got api.PipelineLibraryAnswer) (appwire.FactoryLibrary, bool) {
	if got.Version < 1 {
		return appwire.FactoryLibrary{}, false
	}
	out := appwire.FactoryLibrary{Factory: id, Text: got.Text, Version: got.Version, Pipelines: []appwire.FactoryLibraryChoice{},
		Can: appwire.FactoryLibraryCan{Change: got.Can.Change}, RulesChange: got.RulesChange}
	for _, c := range got.Pipelines {
		if c.From != "library" {
			return appwire.FactoryLibrary{}, false
		}
		out.Pipelines = append(out.Pipelines, c)
	}
	return out, true
}

func libraryUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the library before trying again")
}
