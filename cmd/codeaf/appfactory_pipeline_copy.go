package main

// ── codeaf app: THE FACTORY'S OWN COPY OF A REPOSITORY'S STEPS ──────────────
//
// The engine's half of appwire.FactoryPipelineCopier, and of what
// factory.pipeline_source says of the copy. A save is ONE PUT and a stop ONE
// DELETE of one fixed route, /v1/repos/{owner}/{repo}/pipeline/factory-copy,
// spelled here as a constant, with a body of exactly the text and the version
// read. Only the operator may write, and the refusal is made here, in words,
// before anything leaves (the factory would answer 403, which can read as
// "signed out").
//
// A WRITE IS SENT ONCE, never retried. Only a refusal carrying the phase marker
// (api.PhaseHeader) proves nothing was saved; any other failure is
// appwire.CodeUncertain, and factory.pipeline_source says what the factory
// keeps.
//
// THE ANSWER IS NOT TAKEN ON TRUST. The engine writes the file the way the
// factory does ([pipeline.Encode]) and believes a saved copy only when it is
// that file, for that repository, at a version past the one read. A stop is
// believed at a version past the one read with no copy, or at the version
// read with no copy: stopping a copy the factory does not keep writes nothing.
//
// A COPY THAT SPELLS A RUNTIME KEY is sealed as a pull request is: saved only
// when the factory attested the file it was edited from and still does
// ([checkPipelineCopyRuntimeText]).

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

var _ appwire.FactoryPipelineCopier = (*appBackend)(nil)

const appFactoryPipelineCopySuffix = "/pipeline/factory-copy"

func (b *appBackend) FactoryPipelineCopySet(ctx context.Context, p appwire.FactoryPipelineCopySetParams) (appwire.FactoryPipelineCopyDone, error) {
	p, err := appwire.CheckPipelineCopySet(p)
	if err != nil {
		return appwire.FactoryPipelineCopyDone{}, err
	}
	// The file as the factory will keep it, so its answer can be checked.
	want, ok := pipelineCopyText(p.Text)
	if !ok {
		return appwire.FactoryPipelineCopyDone{}, appwire.Errorf(appwire.CodeBadParams, "these steps do not read as a pipeline file; nothing was saved")
	}
	body := api.PipelineCopyRequest{Text: p.Text, Expect: &api.PipelineCopyExpect{Version: p.Expect.Version}}
	return b.pipelineCopyWrite(ctx, p.Factory, p.Repo, http.MethodPut, body, p.Expect.Version, &want)
}

func (b *appBackend) FactoryPipelineCopyClear(ctx context.Context, p appwire.FactoryPipelineCopyClearParams) (appwire.FactoryPipelineCopyDone, error) {
	p, err := appwire.CheckPipelineCopyClear(p)
	if err != nil {
		return appwire.FactoryPipelineCopyDone{}, err
	}
	body := api.PipelineCopyRequest{Expect: &api.PipelineCopyExpect{Version: p.Expect.Version}}
	return b.pipelineCopyWrite(ctx, p.Factory, p.Repo, http.MethodDelete, body, p.Expect.Version, nil)
}

// pipelineCopyWrite is the one request. want is the text a save must come back
// with; nil is a stop, which must come back with no copy.
func (b *appBackend) pipelineCopyWrite(ctx context.Context, id, repo, method string, body api.PipelineCopyRequest, read int, want *string) (appwire.FactoryPipelineCopyDone, error) {
	none := appwire.FactoryPipelineCopyDone{}
	h := b.factoryHub()
	release, err := h.claimAdmin(id)
	if err != nil {
		return none, err
	}
	defer release()
	who, err := b.heldWho(ctx, id)
	if err != nil {
		return none, err
	}
	if !who.me.Operator {
		return none, appwire.Errorf(appwire.CodeForbidden, "only the factory's operator can change the steps this factory runs")
	}
	if !h.callerIs(id, who.token) {
		return none, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	// TEXT THAT SPELLS A RUNTIME KEY is saved only on a factory that attested
	// the file it was edited from, read again here, as a pull request of the
	// same text would be (appfactory_pipeline_runtime.go).
	if want != nil {
		if err := b.checkPipelineCopyRuntimeText(ctx, who, id, repo, body.Text); err != nil {
			return none, err
		}
	}
	owner, name, _ := strings.Cut(repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + appFactoryPipelineCopySuffix
	r, err := h.linkDo(ctx, who, method, path, body, appFactoryPipelineSourceLimit)
	switch {
	case err == errLinkNotSent:
		return none, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering; nothing was changed")
	case err != nil:
		return none, pipelineCopyUncertain()
	}
	if !h.callerIs(id, who.token) {
		return none, appwire.Errorf(appwire.CodeUncertain, "the sign-in changed while this was saving, and the result is not known; read the pipeline before trying again")
	}
	// AN OLDER FACTORY HAS NO SUCH ROUTE: it answers 404 or 405 with no phase
	// marker, which a factory serving the route puts on every refusal of it,
	// and a route it does not have wrote nothing.
	if (r.status == http.StatusNotFound || r.status == http.StatusMethodNotAllowed) && !r.provedNoWrite() {
		return none, factoryTooOld()
	}
	if r.provedNoWrite() {
		switch r.status {
		case http.StatusConflict:
			return none, appwire.Errorf(appwire.CodeStale, "this factory's copy of these steps changed since they were read; nothing was changed")
		case http.StatusUnprocessableEntity:
			return none, appwire.Errorf(appwire.CodeBadParams, "the factory would not take these steps; nothing was saved")
		}
		return none, linkRefusal(r, "that repository is not one this factory can see")
	}
	if r.status != http.StatusOK || r.bodyBad {
		return none, pipelineCopyUncertain()
	}
	var got api.PipelineCopyAnswer
	if json.Unmarshal(r.body, &got) != nil || !strings.EqualFold(got.Repo, repo) {
		return none, pipelineCopyUncertain()
	}
	// A WRITE MOVES THE VERSION, but STOPPING NOTHING WRITES NOTHING: a stop
	// of a copy the factory does not keep comes back at the version read, with
	// no copy, and that is the stop done.
	stoppedNothing := want == nil && got.FactoryCopy == nil && got.Version == read
	if got.Version <= read && !stoppedNothing {
		return none, pipelineCopyUncertain()
	}
	out := appwire.FactoryPipelineCopyDone{Factory: id, Repo: repo, Version: got.Version}
	switch {
	case want == nil && got.FactoryCopy == nil:
	case want != nil && got.FactoryCopy != nil && got.FactoryCopy.Text == *want && !got.FactoryCopy.At.IsZero():
		out.FactoryCopy = &appwire.FactoryPipelineCopy{By: cleanLinkText(got.FactoryCopy.By, 64), At: got.FactoryCopy.At}
	default:
		return none, pipelineCopyUncertain()
	}
	h.touch(id)
	return out, nil
}

func pipelineCopyUncertain() error {
	return appwire.Errorf(appwire.CodeUncertain, "the factory did not confirm the change; it may have happened, so read the pipeline before trying again")
}

// pipelineCopyText is text as the factory keeps it ([pipeline.Encode]), and
// whether it reads cleanly at all.
func pipelineCopyText(text string) (string, bool) {
	f, err := pipeline.Parse(text)
	if err != nil || len(f.Problems) > 0 {
		return "", false
	}
	data, err := pipeline.Encode(f)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// pipelineSourceCopy reads what the factory's edit-source answer says of its
// own copy onto out, which already holds main's file as this engine read it.
// A copy in effect BECOMES THE SOURCE: this engine reads its text itself, and
// an answer whose copy does not read cleanly, names no time, or spells a
// runtime key the factory did not vouch for is not believed. Without a copy,
// out is main's file as it was.
func pipelineSourceCopy(out *appwire.FactoryPipelineSource, got api.PipelineEditSource, runtimeClaim bool) error {
	if got.CopyVersion < 0 || (got.FactoryCopy != nil && got.CopyVersion < 1) {
		return errPipelineSourceUnreadable
	}
	out.CopyVersion = got.CopyVersion
	if got.Can != nil {
		out.Can = &appwire.FactoryPipelineCan{UseOnFactory: got.Can.UseOnFactory}
	}
	c := got.FactoryCopy
	if c == nil {
		return nil
	}
	if c.At.IsZero() || len(c.Text) > pipeline.EditTextMax || !utf8.ValidString(c.Text) {
		return errPipelineSourceUnreadable
	}
	copied := &appwire.FactoryPipelineCopy{By: cleanLinkText(c.By, 64), At: c.At.UTC(), Main: c.Main}
	switch c.Main {
	case api.PipelineCopyMainSame:
		// Cleared on this read: main's file is what runs, and it is the source.
		if got.State != api.PipelineEditFile || !pipeline.SameFile(got.Text, c.Text) {
			return errPipelineSourceUnreadable
		}
		out.FactoryCopy = copied
		return nil
	case api.PipelineCopyMainMissing:
		if got.State != api.PipelineEditMissing {
			return errPipelineSourceUnreadable
		}
	case api.PipelineCopyMainDiffers:
		if got.State == api.PipelineEditMissing {
			return errPipelineSourceUnreadable
		}
	default:
		return errPipelineSourceUnreadable
	}
	if !runtimeClaim && pipeline.RuntimeKeysIn(c.Text) {
		return errPipelineSourceRuntimeUnattested
	}
	src, _, editable := pipeline.Inspect(c.Text)
	if !editable {
		return errPipelineSourceUnreadable
	}
	var local api.PipelineEditSource
	local.SetParsed(src)
	out.State, out.Editable, out.Text, out.Problems = appwire.PipelineSourceFile, true, c.Text, nil
	out.DefaultBudget, out.BudgetInFile = local.DefaultBudget, local.BudgetInFile
	out.Pipelines, out.Steps, out.Fixed = local.Pipelines, local.Steps, local.Fixed
	out.Rules, out.AskFirst, out.Never = &src.Rules, &src.AskFirst, &src.Never
	pipelineSettings(out, src)
	out.FactoryCopy = copied
	return nil
}
