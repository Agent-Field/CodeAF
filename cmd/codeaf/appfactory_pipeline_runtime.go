package main

// ── codeaf app: WHAT THE ENGINE REMEMBERS OF A FACTORY'S RUNTIME CLAIM ───────
//
// A factory's edit-source reply may say runtime_fields:true, meaning its parser
// reads and its runner obeys kind, when, effort and fanout. A factory that
// predates those words cannot say so, and it would commit a file spelling them
// and then run every such step as a plain chat that always runs. The claim is
// therefore the only evidence there is, and it is worth nothing once the read
// that carried it is forgotten: the app is not trusted to remember it, to be
// current, or to be honest.
//
// So an ACCEPTED read records it here, and the doors that could put those words
// in front of a runner (the preview and the pull request) look it up. A record
// binds the factory, the origin it was read from, the digest of the credential
// that read it, the repository, the commit AND a hash of the exact file text the
// claim described. Nothing is inferred from a version, the hello, or a boolean
// the app sends. A record is memory only: a restart, an expiry or an eviction
// fails closed, and the person is told to read the file again.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const (
	// appFactoryRuntimeAttestMax bounds the memory spent on records, and
	// appFactoryRuntimeAttestLife bounds how long a claim is believed: a remote
	// downgraded after the read is caught within one life, and a pull request
	// re-reads the file anyway.
	appFactoryRuntimeAttestMax  = 64
	appFactoryRuntimeAttestLife = 30 * time.Minute
)

type appFactoryRuntimeRecord struct {
	at  time.Time
	raw [32]byte // sha256 of the exact base text the factory attested
}

// runtimeKey names one claim. Keying on the DIGEST of the credential means a
// replaced, expired or dropped credential can never match, and binding the
// origin covers a row re-pointed at another server under the same id.
func runtimeKey(id, origin, token, repo, base string) string {
	d := bearerDigest(token)
	return id + "\x00" + origin + "\x00" + hex.EncodeToString(d[:]) + "\x00" + strings.ToLower(repo) + "\x00" + base
}

func (h *appFactoryHub) recordRuntime(id, origin, token, repo, base, text string) {
	if token == "" || base == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.tokenLocked(id) != token {
		return
	}
	if h.runtimeOK == nil {
		h.runtimeOK = map[string]appFactoryRuntimeRecord{}
	}
	key := runtimeKey(id, origin, token, repo, base)
	if _, again := h.runtimeOK[key]; !again && len(h.runtimeOK) >= appFactoryRuntimeAttestMax {
		oldest, first := "", true
		for k, r := range h.runtimeOK {
			if first || r.at.Before(h.runtimeOK[oldest].at) {
				oldest, first = k, false
			}
		}
		delete(h.runtimeOK, oldest)
	}
	h.runtimeOK[key] = appFactoryRuntimeRecord{at: h.now(), raw: sha256.Sum256([]byte(text))}
}

// runtimeRecord is the claim for this caller, repository and commit while it is
// in date and the credential is still the connection's.
func (h *appFactoryHub) runtimeRecord(id, origin, token, repo, base string) (appFactoryRuntimeRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || token == "" || base == "" || h.tokenLocked(id) != token {
		return appFactoryRuntimeRecord{}, false
	}
	key := runtimeKey(id, origin, token, repo, base)
	r, ok := h.runtimeOK[key]
	if !ok {
		return appFactoryRuntimeRecord{}, false
	}
	if h.now().Sub(r.at) >= appFactoryRuntimeAttestLife {
		delete(h.runtimeOK, key)
		return appFactoryRuntimeRecord{}, false
	}
	return r, true
}

// runtimeAttestedText is true when the factory attested EXACTLY this text at
// this commit for this caller.
func (h *appFactoryHub) runtimeAttestedText(id, origin, token, repo, base, text string) bool {
	r, ok := h.runtimeRecord(id, origin, token, repo, base)
	return ok && r.raw == sha256.Sum256([]byte(text))
}

const pipelineRuntimeReread = "This factory has not confirmed that it runs kind, when, effort or fanout for this exact file. Read the file again; if this repeats, update the factory."

// pipelineRuntimeRefusal names the edit that needed the claim by op, or no edit
// (nil) when the base itself spells the runtime words.
func pipelineRuntimeRefusal(p appwire.FactoryPipelinePreviewParams, op *int) error {
	err := appwire.Errorf(appwire.CodeBadParams, "%s Nothing was written.", pipelineRuntimeReread)
	err.Detail = appwire.FactoryPipelineEditRefusal{Factory: p.Factory, Repo: p.Repo, Code: pipeline.EditBadOp, Op: op}
	return err
}

// checkPipelineRuntimeText is the pull request's seal. The request carries the
// NEW text and a commit, never the base text, so the claim cannot be matched
// against the file from the request alone. For text that spells a runtime key
// it requires a record for exactly this commit, then reads the file again as the
// same caller and requires that the factory still attests it, still serves the
// same commit, and still serves the bytes that were attested. Legacy text costs
// nothing and takes the path it always took.
func (b *appBackend) checkPipelineRuntimeText(ctx context.Context, who adminWho, p appwire.FactoryPipelinePRParams) error {
	if !pipeline.RuntimeKeysIn(p.Text) {
		return nil
	}
	// An update of an open pull request is sealed at its head, read again by
	// its number; anything else at the default branch's commit.
	pin, read := p.ExpectedBaseSHA, appwire.FactoryPipelineSourceParams{Factory: p.Factory, Repo: p.Repo}
	if p.PR != 0 {
		pin, read.Ref = p.ExpectedHeadSHA, strconv.Itoa(p.PR)
	}
	h := b.factoryHub()
	rec, ok := h.runtimeRecord(p.Factory, who.row.URL, who.token, p.Repo, pin)
	if pin == "" || !ok {
		return appwire.Errorf(appwire.CodeBadParams, "%s Nothing was sent.", pipelineRuntimeReread)
	}
	fresh, err := b.readPipelineSource(ctx, who, read)
	if err != nil {
		return err
	}
	// A file read as missing was attested as text "", so a later file at the
	// same commit can never match it, nor a missing read a file's record.
	if !fresh.RuntimeFields || (fresh.State != api.PipelineEditFile && fresh.State != api.PipelineEditMissing) || !fresh.Editable ||
		fresh.BaseSHA != pin || sha256.Sum256([]byte(fresh.Text)) != rec.raw {
		return appwire.Errorf(appwire.CodeConflict, "The file or the factory changed since it was read. Read the file again and redo the edit. Nothing was sent.")
	}
	return nil
}

// checkPipelineCopyRuntimeText is the factory copy's seal, as
// checkPipelineRuntimeText is the pull request's: a copy is a file the
// factory's runner reads at once, so text that spells a runtime key is saved
// only when the file the edit was made from was attested to this caller and
// the factory still attests it. The save names no commit, so the file is read
// again as the same caller and a record is required for the commit and the
// exact bytes the factory serves now: a main branch that moved, other bytes, or
// a factory that no longer claims the words refuses, and the person reads the
// file again. Legacy text costs nothing.
func (b *appBackend) checkPipelineCopyRuntimeText(ctx context.Context, who adminWho, id, repo, text string) error {
	if !pipeline.RuntimeKeysIn(text) {
		return nil
	}
	fresh, err := b.readPipelineSource(ctx, who, appwire.FactoryPipelineSourceParams{Factory: id, Repo: repo})
	if err != nil {
		return err
	}
	if !fresh.RuntimeFields || (fresh.State != api.PipelineEditFile && fresh.State != api.PipelineEditMissing) || !fresh.Editable ||
		!b.factoryHub().runtimeAttestedText(id, who.row.URL, who.token, repo, fresh.BaseSHA, fresh.Text) {
		return appwire.Errorf(appwire.CodeBadParams, "%s Nothing was saved.", pipelineRuntimeReread)
	}
	return nil
}
