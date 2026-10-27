package main

// ── codeaf app: READING AND PREVIEWING AN EDIT OF A PIPELINE FILE ───────────
//
// The engine's half of appwire.FactoryPipelineEditor. Neither method writes
// anything, to GitHub, to the factory or to disk.
//
// SOURCE makes ONE GET to ONE fixed route of the factory,
// /v1/repos/{owner}/{repo}/pipeline/edit-source, spelled here as a constant: the
// path and verb are never taken from the app, and the owner and name are
// validated by appwire and escaped here. The factory reads the default branch's
// file with the CALLER's GitHub credential and pins the commit. This engine
// supplies no GitHub token and reads no host checkout, `gh` or personal file.
//
// Whoever holds a credential for the connection now may read, asked again
// through GET /v1/me on every call. A connection with no held credential is
// refused before a request leaves (signed_out, or unreachable for the local
// row). After the wait the credential is checked again: if it was replaced or
// dropped, nothing the factory said is used.
//
// THE ANSWER IS NOT TAKEN ON TRUST. This engine parses the file text itself with
// the same parser the pull request route uses, and an answer whose state,
// editability or parsed steps disagree with that parse is refused: an edit is
// built on what the engine can read, not on what a factory claims it read. The
// factory's problems and prose are never passed on; the engine words its own.
//
// PREVIEW is pure. It asks only that the caller still holds the connection (the
// same GET /v1/me) and then applies the structured edits to the base text the
// caller was given by SOURCE. There is no route to call, so there is nothing to
// send, and the base a caller brings is never trusted beyond what the parser
// reads of it.
//
// A MISSING FILE IS DESCRIBED HERE, NOT BY THE FACTORY. The route says only
// that the default branch has no file, and that stays the only shape believed
// of it. The engine then describes the shipped defaults itself
// ([pipeline.InspectDefaults]), the same File its preview applies a base of ""
// to, so the source and the preview of a missing file can never disagree. The
// file the pull request then creates is this engine's defaults plus the edit;
// it is reviewed like any other, and the factory's own runner reads it as a
// file from then on.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

var _ appwire.FactoryPipelineEditor = (*appBackend)(nil)

const (
	appFactoryPipelineSourceSuffix = "/pipeline/edit-source"
	// appFactoryPipelineSourceTimeout covers the route's own three GitHub reads
	// (its limit is sixty seconds); the registry's shorter client timeout would
	// turn a slow answer into a failure.
	appFactoryPipelineSourceTimeout = 70 * time.Second
	// appFactoryPipelineSourceLimit is the most an answer may be. The worst valid
	// file is 256 KiB of '<', '>' or '&', which encoding/json writes as six bytes
	// each (\u003c): 1.5 MiB in text, and a parsed step repeating the same prompt
	// is another 1.5 MiB, about 3 MiB with the small fields around them. 4 MiB
	// holds that with a margin and still refuses a real runaway.
	appFactoryPipelineSourceLimit = 4 << 20
)

func (b *appBackend) FactoryPipelineSource(ctx context.Context, p appwire.FactoryPipelineSourceParams) (appwire.FactoryPipelineSource, error) {
	h := b.factoryHub()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryPipelineSource{}, err
	}
	out, err := b.readPipelineSource(ctx, who, p)
	if err != nil {
		return appwire.FactoryPipelineSource{}, err
	}
	// THE FACTORY'S CLAIM IS REMEMBERED, per caller, repository, commit and exact
	// text, and only for a file this engine accepted whole, or for no file at
	// all (text ""). The preview and the pull request look it up; the app's own
	// booleans are never consulted.
	if out.RuntimeFields && (out.State == appwire.PipelineSourceFile || out.State == appwire.PipelineSourceMissing) && out.Editable {
		h.recordRuntime(p.Factory, who.row.URL, who.token, p.Repo, out.BaseSHA, out.Text)
	}
	h.touch(p.Factory)
	return out, nil
}

// readPipelineSource is the one GET of the edit-source route, as the caller who
// is already known, with the reply classified and nothing remembered.
func (b *appBackend) readPipelineSource(ctx context.Context, who adminWho, p appwire.FactoryPipelineSourceParams) (appwire.FactoryPipelineSource, error) {
	h := b.factoryHub()
	// The wait for who is over; the read is made only for the caller who asked.
	if !h.callerIs(p.Factory, who.token) {
		return appwire.FactoryPipelineSource{}, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	owner, name, _ := strings.Cut(p.Repo, "/")
	path := appFactoryRepoRoute + url.PathEscape(owner) + "/" + url.PathEscape(name) + appFactoryPipelineSourceSuffix
	client := *h.client
	client.Timeout = appFactoryPipelineSourceTimeout
	r, err := h.linkDoWith(ctx, &client, who, http.MethodGet, path, nil, appFactoryPipelineSourceLimit)
	switch {
	case err == errLinkNotSent:
		return appwire.FactoryPipelineSource{}, appwire.Errorf(appwire.CodeUnreachable, "the factory is not answering")
	case err != nil:
		return appwire.FactoryPipelineSource{}, appwire.Errorf(appwire.CodeUnreachable, "the factory did not answer the read; nothing was written")
	}
	// Whatever the factory said describes the caller it was asked as. If that is
	// no longer the connection's, none of it is used, not even a refusal.
	if err := h.staleCredential(p.Factory, who.token); err != nil {
		return appwire.FactoryPipelineSource{}, err
	}
	return classifyPipelineSource(r, p)
}

// errPipelineSourceRuntimeUnattested is said when the file spells kind, when,
// effort or fanout and the factory did not say it runs them: a factory that
// predates those words would run the step as a plain chat that always runs.
var errPipelineSourceRuntimeUnattested = appwire.Errorf(appwire.CodeEngine, "this factory has not confirmed support for these pipeline settings. Update the factory, then read the file again. Nothing was written.")

var errPipelineSourceUnreadable = appwire.Errorf(appwire.CodeEngine, "the factory sent a pipeline file this app cannot use; nothing was written")

// classifyPipelineSource believes a reply only in its documented shape.
func classifyPipelineSource(r linkReply, p appwire.FactoryPipelineSourceParams) (appwire.FactoryPipelineSource, error) {
	if r.bodyBad {
		return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
	}
	if r.status != http.StatusOK {
		return appwire.FactoryPipelineSource{}, pipelineSourceFailure(r)
	}
	var got api.PipelineEditSource
	if json.Unmarshal(r.body, &got) != nil {
		return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
	}
	// archived must be SAID. An absent or non-boolean field would decode as false,
	// and unknown is never assumed to be "not archived".
	var seen map[string]json.RawMessage
	if json.Unmarshal(r.body, &seen) != nil || (string(seen["archived"]) != "true" && string(seen["archived"]) != "false") {
		return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
	}
	// runtime_fields is the REMOTE's own claim, read from this reply and from
	// nowhere else; it is absent from a factory that predates the words. Only the
	// literal true or false is a claim, anything else is not believed.
	runtimeClaim := false
	switch rf, said := seen["runtime_fields"]; {
	case !said:
	case string(rf) == "true":
		runtimeClaim = true
	case string(rf) != "false":
		return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
	}
	if got.Repo != p.Repo || got.Path != factory.PipelineFile || !appwire.ValidPipelinePRCommit(got.BaseSHA) || !plainBranchName(got.DefaultBranch) {
		return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
	}
	out := appwire.FactoryPipelineSource{Factory: p.Factory, Repo: p.Repo, Path: got.Path, DefaultBranch: got.DefaultBranch,
		BaseSHA: got.BaseSHA, CanPush: got.CanPush, Archived: got.Archived, RuntimeFields: runtimeClaim, State: got.State}

	// What a state may carry. A field of another state is a contradiction.
	parsed := func(s api.PipelineEditSource) []byte {
		s.Factory, s.Repo, s.Path, s.DefaultBranch, s.BaseSHA, s.CanPush, s.Archived, s.RuntimeFields, s.State, s.Editable, s.Text, s.Problems = api.Factory{}, "", "", "", "", false, false, false, "", false, "", nil
		raw, _ := json.Marshal(s)
		return raw
	}
	var bare api.PipelineEditSource
	switch got.State {
	case api.PipelineEditMissing, api.PipelineEditTooLarge:
		if got.Editable || got.Text != "" || len(got.Problems) > 0 || !bytes.Equal(parsed(got), parsed(bare)) {
			return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
		}
		if got.State == api.PipelineEditMissing {
			// No file: the shipped defaults are what the repository runs, and
			// what an edit starts from (a preview of base_text "").
			src := pipeline.InspectDefaults()
			var local api.PipelineEditSource
			local.SetParsed(src)
			out.Editable = true
			out.DefaultBudget, out.BudgetInFile = local.DefaultBudget, local.BudgetInFile
			out.Pipelines, out.Steps, out.Fixed = local.Pipelines, local.Steps, local.Fixed
			out.Rules, out.AskFirst, out.Never = &src.Rules, &src.AskFirst, &src.Never
			pipelineSettings(&out, src)
		}
	case api.PipelineEditInvalid, api.PipelineEditFile:
		if len(got.Text) > pipeline.EditTextMax || !utf8.ValidString(got.Text) {
			return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
		}
		// A FILE THAT SPELLS A RUNTIME KEY NEEDS THE FACTORY'S OWN SAY-SO, and this
		// is decided before the engine's own reading: the engine's parser knowing
		// the words proves nothing about the runner that will read the merged file.
		if !runtimeClaim && pipeline.RuntimeKeysIn(got.Text) {
			return appwire.FactoryPipelineSource{}, errPipelineSourceRuntimeUnattested
		}
		// THE ENGINE READS THE FILE ITSELF. A factory that calls a file editable
		// when this parser would refuse it, or the other way round, or that parsed
		// it into something else, is not believed.
		src, problems, editable := pipeline.Inspect(got.Text)
		if editable != (got.State == api.PipelineEditFile) || got.Editable != editable {
			return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
		}
		out.Text = got.Text
		if !editable {
			if !bytes.Equal(parsed(got), parsed(bare)) || len(got.Problems) == 0 {
				return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
			}
			out.Problems = cleanPipelineProblems(problems)
			break
		}
		var local api.PipelineEditSource
		local.SetParsed(src)
		if len(got.Problems) > 0 || !bytes.Equal(parsed(got), parsed(local)) {
			return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
		}
		out.Editable = true
		out.DefaultBudget, out.BudgetInFile = local.DefaultBudget, local.BudgetInFile
		out.Pipelines, out.Steps, out.Fixed = local.Pipelines, local.Steps, local.Fixed
		// The file's own lines, read by this engine's parser from Text alone:
		// the factory's house rules are never in the file.
		out.Rules, out.AskFirst, out.Never = &src.Rules, &src.AskFirst, &src.Never
		pipelineSettings(&out, src)
	default:
		return appwire.FactoryPipelineSource{}, errPipelineSourceUnreadable
	}
	return out, nil
}

// cleanPipelineProblems words the engine's own reading of a file that does not
// read cleanly: one line each, bounded.
func cleanPipelineProblems(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = cleanLinkText(s, 300); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{factory.PipelineFile + ": the file does not read"}
	}
	return out
}

func knownPipelineSourceStage(stage string) bool {
	switch stage {
	case api.PipelineEditStageCredential, api.PipelineEditStageRequest, api.PipelineEditStageRepository,
		api.PipelineEditStageBase, api.PipelineEditStageFile:
		return true
	}
	return false
}

// pipelineSourceFailure is the wire error for a read the factory refused. The
// words are fixed here: the factory's own may echo a path or a token. A reply
// that is not the route's typed failure (a proxy, the shared gate) goes through
// the same fixed-words mapper the other factory doors use.
func pipelineSourceFailure(r linkReply) error {
	var f api.PipelineEditSourceFailure
	typed := json.Unmarshal(r.body, &f) == nil && pipelinePRRequestID.MatchString(f.RequestID) && knownPipelineSourceStage(f.Stage) &&
		(f.GitHubStatus == 0 || (f.GitHubStatus >= 100 && f.GitHubStatus <= 599)) && r.status >= 400 && r.status < 600
	if !typed {
		return linkRefusal(r, "that repository is not one this factory can see")
	}
	switch {
	case r.status == http.StatusUnauthorized || f.Stage == api.PipelineEditStageCredential:
		return appwire.Errorf(appwire.CodeGitHubExpired, "GitHub access for this sign-in is missing or expired; reconnect GitHub.")
	case f.Stage == api.PipelineEditStageRequest:
		return appwire.Errorf(appwire.CodeBadParams, "the factory would not take that request as it is.")
	case r.status == http.StatusNotFound:
		return appwire.Errorf(appwire.CodeNotFound, "your GitHub access cannot see that repository or its default branch.")
	case r.status == http.StatusForbidden:
		return appwire.Errorf(appwire.CodeForbidden, "your GitHub access cannot read that file.")
	}
	return appwire.Errorf(appwire.CodeEngine, "GitHub did not answer the read; nothing was written, so you can try again.")
}

func (b *appBackend) FactoryPipelinePreview(ctx context.Context, p appwire.FactoryPipelinePreviewParams) (appwire.FactoryPipelinePreview, error) {
	h := b.factoryHub()
	who, err := b.heldWho(ctx, p.Factory)
	if err != nil {
		return appwire.FactoryPipelinePreview{}, err
	}
	if !h.callerIs(p.Factory, who.token) {
		return appwire.FactoryPipelinePreview{}, appwire.Errorf(appwire.CodeConflict, "the sign-in changed while this was loading; try again")
	}
	// A BASE THAT SPELLS A RUNTIME KEY IS EDITED ONLY WHEN THE FACTORY ATTESTED
	// THESE EXACT BYTES at this commit to this caller. The base comes from the
	// app, which may be stale or hand-built; the engine's parser reading the
	// words proves nothing about the runner that will read the merged file.
	baseAttested := h.runtimeAttestedText(p.Factory, who.row.URL, who.token, p.Repo, p.BaseSHA, p.BaseText)
	// An edit that names when, effort or fanout (even to clear it) is a runtime
	// edit and needs the same say-so, whatever the base spells.
	if i := firstRuntimeEdit(p.Edits); !baseAttested && i >= 0 {
		return appwire.FactoryPipelinePreview{}, pipelineRuntimeRefusal(p, &i)
	}
	if !baseAttested && pipeline.RuntimeKeysIn(p.BaseText) {
		return appwire.FactoryPipelinePreview{}, pipelineRuntimeRefusal(p, nil)
	}
	ed, err := pipeline.ApplyEdits(p.BaseText, appwire.PipelineEditOps(p.Edits))
	// The caller is the connection's to the end: a preview made for one who has
	// since gone is not shown to the next.
	if stale := h.staleCredential(p.Factory, who.token); stale != nil {
		return appwire.FactoryPipelinePreview{}, stale
	}
	if err != nil {
		var bad *pipeline.EditError
		if !errors.As(err, &bad) {
			return appwire.FactoryPipelinePreview{}, appwire.Errorf(appwire.CodeEngine, "the edit could not be previewed")
		}
		return appwire.FactoryPipelinePreview{}, pipelineEditRefusal(p, bad)
	}
	// The same bar for the RESULT: an edit that writes a runtime key into a
	// legacy file is not previewed for a factory that did not say
	// it runs them.
	if !baseAttested && ed.Changed && pipeline.RuntimeKeysIn(ed.Text) {
		return appwire.FactoryPipelinePreview{}, pipelineRuntimeRefusal(p, nil)
	}
	h.touch(p.Factory)
	out := appwire.FactoryPipelinePreview{Factory: p.Factory, Repo: p.Repo, BaseSHA: p.BaseSHA, Path: factory.PipelineFile,
		Text: ed.Text, Changed: ed.Changed, FormatRewritten: ed.FormatRewritten, CommentsDropped: ed.CommentsDropped,
		Steps: []appwire.FactoryPipelineStepChange{}}
	for _, c := range ed.Steps {
		// THE WIRE NEVER CARRIES A KIND EDIT. If a change ever lists one it is not
		// shown as a normal preview.
		for _, f := range c.Fields {
			if f == "kind" {
				return appwire.FactoryPipelinePreview{}, appwire.Errorf(appwire.CodeEngine, "the edit could not be previewed")
			}
		}
		out.Steps = append(out.Steps, appwire.FactoryPipelineStepChange{Name: c.Name, Fields: c.Fields,
			Before: pipelineStepValues(c.Before), After: pipelineStepValues(c.After)})
	}
	if ed.Budget != nil {
		out.Budget = &appwire.FactoryPipelineBudgetChange{Before: ed.Budget.Before, After: ed.Budget.After}
	}
	out.Structure = pipelineStructure(ed)
	out.Lists = pipelineLists(ed)
	out.Settings = pipelineSettingChanges(ed)
	return out, nil
}

// pipelineSettings puts the file's own settings, read by this engine's parser
// from Text alone, on a source.
func pipelineSettings(out *appwire.FactoryPipelineSource, src pipeline.Source) {
	adapt, size, cost := src.Adapt, src.MaxSize, src.MaxCost
	labels := append([]string{}, src.Labels...)
	out.Adapt, out.Labels, out.MaxSize, out.MaxCost = &adapt, &labels, &size, &cost
}

// pipelineSettingChanges is the settings half of a preview on the wire, nil for
// a preview with no settings edit.
func pipelineSettingChanges(ed pipeline.Edited) *[]appwire.FactoryPipelineSettingChange {
	if !ed.SettingsEdited {
		return nil
	}
	out := []appwire.FactoryPipelineSettingChange{}
	for _, c := range ed.Settings {
		out = append(out, appwire.FactoryPipelineSettingChange{Setting: c.Setting, Before: c.Before, After: c.After})
	}
	return &out
}

// pipelineLists is the list half of a preview on the wire, every list an array
// however empty, and nil for a preview with no set_list: such an answer is byte
// for byte what it always was.
func pipelineLists(ed pipeline.Edited) *[]appwire.FactoryPipelineListChange {
	if !ed.Listed {
		return nil
	}
	out := []appwire.FactoryPipelineListChange{}
	for _, l := range ed.Lists {
		out = append(out, appwire.FactoryPipelineListChange{List: l.List, Before: append([]string{}, l.Before...), After: append([]string{}, l.After...)})
	}
	return &out
}

// pipelineStructure is the structural half of a preview on the wire, every
// list an array however empty, and nil for a preview with no structural edit:
// a field edit's answer is byte for byte what it always was.
func pipelineStructure(ed pipeline.Edited) *appwire.FactoryPipelineStructure {
	if !ed.Structural {
		return nil
	}
	s := &appwire.FactoryPipelineStructure{Order: []appwire.FactoryPipelineOrderChange{}, Added: append([]string{}, ed.Added...),
		Counts: []appwire.FactoryPipelineCount{}, Gates: []appwire.FactoryPipelineGateChange{}}
	for _, o := range ed.Order {
		s.Order = append(s.Order, appwire.FactoryPipelineOrderChange{Pipeline: o.Pipeline, Before: append([]string{}, o.Before...), After: append([]string{}, o.After...)})
	}
	for _, c := range ed.Counts {
		s.Counts = append(s.Counts, appwire.FactoryPipelineCount{Pipeline: c.Pipeline, Steps: c.Steps})
	}
	for _, g := range ed.Gates {
		s.Gates = append(s.Gates, appwire.FactoryPipelineGateChange{Pipeline: g.Pipeline, Gate: g.Gate, Before: append([]string{}, g.Before...), After: append([]string{}, g.After...)})
	}
	return s
}

func pipelineStepValues(v pipeline.StepView) appwire.FactoryPipelineStepValues {
	return appwire.FactoryPipelineStepValues{Prompt: v.Prompt, Until: v.Until, Max: v.Max, When: v.When, Effort: v.Effort, Fanout: v.Fanout}
}

// firstRuntimeEdit is the position of the first edit that names when, effort or
// fanout, or -1. An explicit empty value counts: clearing a word is a runtime
// edit too, and so does an add_step that spells one for the step it adds.
func firstRuntimeEdit(edits []appwire.FactoryPipelineEdit) int {
	for i, e := range edits {
		if e.When != nil || e.Effort != nil || e.Fanout != nil {
			return i
		}
	}
	return -1
}

// pipelineEditRefusal is the wire error for an edit the file cannot take. The
// words are fixed from the code; the text of the edit or the file is not echoed.
func pipelineEditRefusal(p appwire.FactoryPipelinePreviewParams, bad *pipeline.EditError) error {
	words := map[string]string{
		pipeline.EditBaseInvalid:      "That file does not read cleanly, so it cannot be edited here.",
		pipeline.EditBadOp:            "That edit is not one this editor makes.",
		pipeline.EditLocked:           "That step is locked and cannot be edited.",
		pipeline.EditUnknownStep:      "That is not a step in this pipeline.",
		pipeline.EditBadValue:         "That value is out of bounds.",
		pipeline.EditTooLarge:         "The edited file would be too large for a pull request.",
		pipeline.EditNotRepresentable: "The pipeline file cannot hold that edit as asked.",
	}[bad.Code]
	if words == "" {
		bad = &pipeline.EditError{Code: pipeline.EditBadOp, Op: -1}
		words = "That edit is not one this editor makes."
	}
	detail := appwire.FactoryPipelineEditRefusal{Factory: p.Factory, Repo: p.Repo, Code: bad.Code}
	if bad.Op >= 0 {
		op := bad.Op
		detail.Op = &op
	}
	err := appwire.Errorf(appwire.CodeBadParams, "%s", words+" Nothing was written.")
	err.Detail = detail
	return err
}
