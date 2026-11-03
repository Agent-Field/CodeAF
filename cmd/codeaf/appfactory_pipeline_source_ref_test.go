package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

var refTime = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// refSourceFor is the route's answer for text at pull request 820's head,
// with the factory's runtime claim.
func refSourceFor(text string) api.PipelineEditSource {
	v := editSourceFor(prRepo, text)
	v.BaseSHA, v.Ref, v.PR, v.RuntimeFields = prHeadSHA, "820", 820, true
	return v
}

// refSourceReply is the route's answer to a read of pull request 820 at head.
func refSourceReply(ref string, edit func(*api.PipelineEditSource)) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		v := editSourceFor(prRepo, editTestFile)
		v.BaseSHA, v.Ref, v.PR = prHeadSHA, ref, 820
		if edit != nil {
			edit(&v)
		}
		prWriteJSON(w, 200, v)
	}
}

func (r *editWire) sourceAt(ref string) (appwire.FactoryPipelineSource, error) {
	return r.backend.FactoryPipelineSource(context.Background(), appwire.FactoryPipelineSourceParams{Factory: r.id, Repo: prRepo, Ref: ref})
}

func TestPipelineSourceByNumberSendsRefAndReadsThePullRequestsHead(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
	stub.source = refSourceReply("820", nil)
	r := newEditWire(t, stub, false, api.FactorySolo, prToken)
	got, err := r.sourceAt("820")
	if err != nil || got.BaseSHA != prHeadSHA || got.Ref != "820" || got.PR != 820 || got.Text != editTestFile || !got.Editable {
		t.Fatalf("%+v %v", got, err)
	}
	if log := strings.Join(stub.requests(), "|"); log != "GET /v1/me|GET /v1/repos/octo/Hello.x_y/pipeline/edit-source?ref=820" {
		t.Fatalf("requests %s", log)
	}
}

func TestPipelineSourceByHeadCommitReadsThatHead(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
	stub.source = refSourceReply(prHeadSHA, nil)
	r := newEditWire(t, stub, false, api.FactorySolo, prToken)
	got, err := r.sourceAt(prHeadSHA)
	if err != nil || got.BaseSHA != prHeadSHA || got.Ref != prHeadSHA || got.PR != 820 {
		t.Fatalf("%+v %v", got, err)
	}
}

// AN ANSWER THAT IS NOT THE READ ASKED FOR is not believed: a default-branch
// read for a ref, a ref read for none, another ref, another head.
func TestPipelineSourceRefAnswersMustBeTheReadAskedFor(t *testing.T) {
	for name, c := range map[string]struct {
		ref  string
		post func(http.ResponseWriter, *http.Request)
	}{
		"default read for a ref": {"820", editSourceReply(editSourceFor(prRepo, editTestFile))},
		"ref read for none":      {"", refSourceReply("820", nil)},
		"other ref":              {"820", refSourceReply("821", nil)},
		"other number":           {"820", refSourceReply("820", func(v *api.PipelineEditSource) { v.PR = 821 })},
		"no number":              {"820", refSourceReply("820", func(v *api.PipelineEditSource) { v.PR = 0 })},
		"other head":             {prHeadSHA, refSourceReply(prHeadSHA, func(v *api.PipelineEditSource) { v.BaseSHA = prBaseSHA })},
		"carries the copy": {"820", refSourceReply("820", func(v *api.PipelineEditSource) {
			v.FactoryCopy, v.CopyVersion = &api.PipelineCopyState{Text: editTestFile, By: "a", At: refTime}, 1
		})},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
			stub.source = c.post
			r := newEditWire(t, stub, false, api.FactorySolo, prToken)
			if _, err := r.sourceAt(c.ref); wireCodeOf(t, err) != appwire.CodeEngine {
				t.Fatalf("%v", err)
			}
		})
	}
}

// A REF THAT IS NOT AN OPEN PIPELINE PULL REQUEST is conflict; an older
// factory, which refuses the query, says it cannot read a pull request.
func TestPipelineSourceRefRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		stage  string
		code   string
		words  string
	}{
		"not an open pipeline pull request": {409, api.PipelineEditStageHead, appwire.CodeConflict, "pull request"},
		"missing pull request":              {403, api.PipelineEditStageHead, appwire.CodeConflict, "pull request"},
		"older factory":                     {422, api.PipelineEditStageRequest, appwire.CodeBadParams, "update the factory"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
			stub.source = func(w http.ResponseWriter, _ *http.Request) {
				prWriteJSON(w, c.status, api.PipelineEditSourceFailure{Error: "ghp_leaked", Stage: c.stage, RequestID: "9f8e7d6c"})
			}
			r := newEditWire(t, stub, false, api.FactorySolo, prToken)
			_, err := r.sourceAt("820")
			if wireCodeOf(t, err) != c.code || !strings.Contains(err.Error(), c.words) || strings.Contains(err.Error(), "leaked") {
				t.Fatalf("%v", err)
			}
		})
	}
}

// The preview takes a pull request's head as base_sha: the runtime words the
// factory attested at that head carry, as they do at the default branch's.
func TestPipelinePreviewTakesThePullRequestsHeadAsBase(t *testing.T) {
	const text = runtimeSourceFile
	stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
	stub.source = refSourceReply("820", func(v *api.PipelineEditSource) {
		*v = refSourceFor(text)
	})
	r := newEditWire(t, stub, false, api.FactorySolo, prToken)
	got, err := r.sourceAt("820")
	if err != nil || got.BaseSHA != prHeadSHA {
		t.Fatalf("%+v %v", got, err)
	}
	p := appwire.FactoryPipelinePreviewParams{Factory: r.id, Repo: prRepo, BaseSHA: prHeadSHA, BaseText: text,
		Edits: []appwire.FactoryPipelineEdit{{Op: "set_step", Name: "plan", Effort: str("cheap")}}}
	if _, err := r.backend.FactoryPipelinePreview(context.Background(), p); err != nil {
		t.Fatalf("preview at the head: %v", err)
	}
}

func TestPipelineSourceRefIsAdvertised(t *testing.T) {
	stub := newEditStub(t, api.Me{})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	caps, err := r.backend.FactoryCapabilities(context.Background())
	if err != nil || !caps.PipelineSourceRef {
		t.Fatalf("%+v %v", caps, err)
	}
	if !strings.Contains(r.hello, `"`+appwire.FeatureFactoryPipelineSourceRef+`"`) {
		t.Fatal("hello does not announce the feature")
	}
}

// AN UPDATE THAT SPELLS A RUNTIME KEY is sealed at the pull request's head: the
// head read by ref attests it, and the seal reads it again by number.
func TestPipelinePRUpdateRuntimeTextIsSealedAtThePullRequestsHead(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
	stub.source = func(w http.ResponseWriter, _ *http.Request) { prWriteJSON(w, 200, refSourceFor(runtimeSourceFile)) }
	stub.other = prUpdated(api.PipelinePRUpdated, prOldHeadSHA, nil)
	r := newEditWire(t, stub, false, api.FactorySolo, prToken)
	if _, err := r.sourceAt("820"); err != nil {
		t.Fatal(err)
	}
	done, err := r.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: r.id, Repo: prRepo,
		Text: strings.Replace(runtimeSourceFile, "plan it", "plan it well", 1), PR: 820, ExpectedHeadSHA: prHeadSHA})
	if err != nil || done.Outcome != "updated" {
		t.Fatalf("%+v %v", done, err)
	}
	reads := 0
	for _, l := range stub.requests() {
		if strings.HasSuffix(l, "/pipeline/edit-source?ref=820") {
			reads++
		}
	}
	if reads != 2 {
		t.Fatalf("requests %v", stub.requests())
	}
}
