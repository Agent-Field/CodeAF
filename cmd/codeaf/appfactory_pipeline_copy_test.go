package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

// USE ON THIS FACTORY, END TO END against the real factory handler: an edit
// previewed from main's (missing) file is saved as the factory's copy with no
// write to GitHub; the next source read IS the copy (state file, its text and
// parse), says main has no file, and moves the version; a stale version is
// refused as stale; a stop goes back to main's file. The solo host's person
// may, and the source says so.
func TestPipelineCopyWireAgainstTheRealFactoryHandler(t *testing.T) {
	const tip = "1111111111111111111111111111111111111111"
	var mu sync.Mutex
	var calls []string
	var file []byte // main has no file
	client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, r.Method+" "+r.URL.Path)
		status, body := 200, `{}`
		switch path := r.URL.Path; {
		case r.Method == "GET" && path == "/repos/acme/api":
			body = `{"default_branch":"main","permissions":{"push":true}}`
		case r.Method == "GET" && path == "/repos/acme/api/git/ref/heads/main":
			body = `{"object":{"type":"commit","sha":"` + tip + `"}}`
		case r.Method == "GET" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
			if file == nil {
				status = 404
				break
			}
			data, _ := json.Marshal(map[string]any{"type": "file", "encoding": "base64", "sha": tip, "content": base64.StdEncoding.EncodeToString(file)})
			body = string(data)
		default:
			t.Errorf("unexpected GitHub request: %s %s", r.Method, path)
			status = 500
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "alice"}, nil }}
	h := serve.New(seam, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice", Store: st,
		GitHubToken: func(context.Context) string { return "simulated-profile-credential" }, GitHubHTTPClient: client})
	srv := httptest.NewServer(h)
	t.Cleanup(func() { srv.Close(); h.Close() })
	w := newFactoryWire(t, testFactoryHub(t))
	if !strings.Contains(w.hello, appwire.FeatureFactoryPipelineCopy) {
		t.Fatalf("hello does not announce %s: %s", appwire.FeatureFactoryPipelineCopy, w.hello)
	}
	var conn appwire.FactoryConnection
	w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)

	var src appwire.FactoryPipelineSource
	w.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: conn.ID, Repo: "acme/api"}, &src)
	if src.State != "missing" || src.FactoryCopy != nil || src.CopyVersion != 1 || src.Can == nil || !src.Can.UseOnFactory {
		t.Fatalf("source %+v", src)
	}
	// Stopping a copy the factory does not keep writes nothing, and is done.
	var stopped appwire.FactoryPipelineCopyDone
	w.must("factory.pipeline_copy_clear", appwire.FactoryPipelineCopyClearParams{Factory: conn.ID, Repo: "acme/api",
		Expect: &appwire.FactoryPipelineCopyExpect{Version: src.CopyVersion}}, &stopped)
	if stopped.Version != 1 || stopped.FactoryCopy != nil {
		t.Fatalf("stopped nothing %+v", stopped)
	}
	var prev appwire.FactoryPipelinePreview
	w.must("factory.pipeline_preview", appwire.FactoryPipelinePreviewParams{Factory: conn.ID, Repo: "acme/api", BaseSHA: src.BaseSHA, BaseText: src.Text,
		Edits: []appwire.FactoryPipelineEdit{{Op: "set_step", Name: "write", Max: num(7)}}}, &prev)
	var done appwire.FactoryPipelineCopyDone
	w.must("factory.pipeline_copy_set", appwire.FactoryPipelineCopySetParams{Factory: conn.ID, Repo: "acme/api", Text: prev.Text,
		Expect: &appwire.FactoryPipelineCopyExpect{Version: src.CopyVersion}}, &done)
	if done.Version != 2 || done.FactoryCopy == nil || done.FactoryCopy.By != "alice" || done.FactoryCopy.At.IsZero() {
		t.Fatalf("saved %+v", done)
	}
	mu.Lock()
	for _, c := range calls {
		if !strings.HasPrefix(c, "GET ") {
			t.Fatalf("saving wrote to GitHub: %v", calls)
		}
	}
	mu.Unlock()

	w.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: conn.ID, Repo: "acme/api"}, &src)
	if src.State != "file" || !src.Editable || src.Text != prev.Text || src.BaseSHA != tip || src.CopyVersion != 2 ||
		src.FactoryCopy == nil || src.FactoryCopy.Main != appwire.PipelineCopyMainMissing || src.FactoryCopy.By != "alice" {
		t.Fatalf("the source is not the copy: %+v", src)
	}
	found := false
	for _, s := range src.Steps {
		if s.Name == "write" && s.Max == 7 {
			found = true
		}
	}
	if !found {
		t.Fatalf("the copy's steps: %+v", src.Steps)
	}
	// What a job starts under is the copy now.
	f, err := pipeline.WithFactoryCopy(pipeline.Defaults(), nil, st, "acme/api", "")
	if err != nil || f.From != factory.FromFactoryCopy || f.Steps["write"].Max != 7 {
		t.Fatalf("a job reads %q (%v)", f.From, err)
	}

	// A stale version writes nothing.
	w.mustFail("factory.pipeline_copy_set", appwire.FactoryPipelineCopySetParams{Factory: conn.ID, Repo: "acme/api", Text: prev.Text,
		Expect: &appwire.FactoryPipelineCopyExpect{Version: 1}}, appwire.CodeStale)
	// A file the pull request would refuse is refused here too.
	w.mustFail("factory.pipeline_copy_set", appwire.FactoryPipelineCopySetParams{Factory: conn.ID, Repo: "acme/api", Text: "pipeline: [\n",
		Expect: &appwire.FactoryPipelineCopyExpect{Version: 2}}, appwire.CodeBadParams)

	done = appwire.FactoryPipelineCopyDone{}
	w.must("factory.pipeline_copy_clear", appwire.FactoryPipelineCopyClearParams{Factory: conn.ID, Repo: "acme/api",
		Expect: &appwire.FactoryPipelineCopyExpect{Version: 2}}, &done)
	if done.Version != 3 || done.FactoryCopy != nil {
		t.Fatalf("stopped %+v", done)
	}
	src = appwire.FactoryPipelineSource{}
	w.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: conn.ID, Repo: "acme/api"}, &src)
	if src.State != "missing" || src.FactoryCopy != nil || src.CopyVersion != 3 {
		t.Fatalf("after a stop %+v", src)
	}

	// Main's file becomes the copy's (its pull request merged): it clears itself.
	w.must("factory.pipeline_copy_set", appwire.FactoryPipelineCopySetParams{Factory: conn.ID, Repo: "acme/api", Text: prev.Text,
		Expect: &appwire.FactoryPipelineCopyExpect{Version: 3}}, &done)
	mu.Lock()
	file = []byte(prev.Text)
	mu.Unlock()
	w.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: conn.ID, Repo: "acme/api"}, &src)
	if src.State != "file" || src.FactoryCopy == nil || src.FactoryCopy.Main != appwire.PipelineCopyMainSame || src.CopyVersion != 5 {
		t.Fatalf("main matches: %+v", src)
	}
	if c, _, _ := st.PipelineCopy("acme/api"); c != nil {
		t.Fatalf("the copy stayed: %+v", c)
	}
	w.neverSaid("simulated-profile-credential", testHostToken)
}

// ONLY THE OPERATOR WRITES, refused here before anything leaves: a member's
// save and stop send nothing to the factory.
func TestPipelineCopyAMembersWriteIsRefusedBeforeAnythingIsSent(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam, Profile: api.Profile{Handle: "alice"}})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	_, err := r.backend.FactoryPipelineCopySet(context.Background(), appwire.FactoryPipelineCopySetParams{Factory: r.id, Repo: prRepo, Text: editTestFile,
		Expect: &appwire.FactoryPipelineCopyExpect{Version: 1}})
	if wire, ok := err.(*appwire.Error); !ok || wire.Code != appwire.CodeForbidden {
		t.Fatalf("err %v", err)
	}
	_, err = r.backend.FactoryPipelineCopyClear(context.Background(), appwire.FactoryPipelineCopyClearParams{Factory: r.id, Repo: prRepo,
		Expect: &appwire.FactoryPipelineCopyExpect{Version: 1}})
	if wire, ok := err.(*appwire.Error); !ok || wire.Code != appwire.CodeForbidden {
		t.Fatalf("err %v", err)
	}
	for _, req := range stub.requests() {
		if !strings.HasPrefix(req, "GET /v1/me") {
			t.Fatalf("a member's write was sent: %v", stub.requests())
		}
	}
}

// THE COPY A FACTORY SAYS IT KEEPS IS READ BY THIS ENGINE, NOT BELIEVED: one
// that does not read cleanly, names no time, or contradicts main's state is
// refused whole.
func TestPipelineSourceBelievesOnlyACopyItCanRead(t *testing.T) {
	good := editSourceFor(prRepo, editTestFile)
	good.CopyVersion = 2
	good.Can = &api.PipelineCan{UseOnFactory: true}
	copied := "pipeline:\n  issue: [plan, write, proof]\n"
	for name, c := range map[string]*api.PipelineCopyState{
		"does not read":       {Text: "pipeline: [\n", By: "test-owner", At: prNow(), Main: api.PipelineCopyMainDiffers},
		"no time":             {Text: copied, By: "test-owner", Main: api.PipelineCopyMainDiffers},
		"missing beside file": {Text: copied, By: "test-owner", At: prNow(), Main: api.PipelineCopyMainMissing},
		"same but differs":    {Text: copied, By: "test-owner", At: prNow(), Main: api.PipelineCopyMainSame},
		"an unknown word":     {Text: copied, By: "test-owner", At: prNow(), Main: "older"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
			v := good
			v.FactoryCopy = c
			stub.source = editSourceReply(v)
			r := newEditWire(t, stub, false, api.FactorySolo, prToken)
			if _, err := r.source(); err == nil {
				t.Fatal("believed")
			}
		})
	}
	stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
	v := good
	v.FactoryCopy = &api.PipelineCopyState{Text: copied, By: "test-owner", At: prNow(), Main: api.PipelineCopyMainDiffers}
	stub.source = editSourceReply(v)
	r := newEditWire(t, stub, false, api.FactorySolo, prToken)
	got, err := r.source()
	if err != nil || got.Text != copied || got.State != "file" || got.FactoryCopy == nil || got.FactoryCopy.Main != "differs" || got.CopyVersion != 2 || got.Can == nil || !got.Can.UseOnFactory {
		t.Fatalf("%+v %v", got, err)
	}
}

func prNow() time.Time { return time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC) }
