package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

const copyPath = "/v1/repos/acme/api/pipeline/factory-copy"

// soloCopy is a solo factory with a store, reading GitHub through g, whose one
// person is @solo-owner.
func soloCopy(t *testing.T, g *prGitHub) (*Handler, *store.Store) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	seam := factory.Seam{GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "solo-owner"}, nil }}
	h := New(seam, Config{Name: "factory", Version: "test", Address: "http://factory", Mode: api.FactorySolo, Store: st,
		Now:              func() time.Time { return time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC) },
		GitHubToken:      func(context.Context) string { return "profile-token" },
		GitHubHTTPClient: &http.Client{Transport: g.transport(nil)}})
	t.Cleanup(h.Close)
	return h, st
}

func copyRequest(h *Handler, method, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, copyPath, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func copyBody(t *testing.T, text string, version int) string {
	t.Helper()
	raw, err := json.Marshal(api.PipelineCopyRequest{Text: text, Expect: &api.PipelineCopyExpect{Version: version}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func readCopyAnswer(t *testing.T, w *httptest.ResponseRecorder) api.PipelineCopyAnswer {
	t.Helper()
	var out api.PipelineCopyAnswer
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return out
}

// THE COPY IS THE FILE A PULL REQUEST WOULD WRITE: a save keeps the factory's
// own encoding, names who saved it and when, writes nothing to GitHub, and
// moves the version; the edit-source read then carries it beside main's file,
// with can.use_on_factory for the solo host's one person.
func TestPipelineCopySaveIsThePullRequestFileAndTheSourceCarriesIt(t *testing.T) {
	g := newPRGitHub(t)
	h, st := soloCopy(t, g)
	first := readCopyAnswer(t, copyRequest(h, "GET", ""))
	if first.Version != 1 || first.FactoryCopy != nil || !first.Can.UseOnFactory || first.Repo != "acme/api" {
		t.Fatalf("a factory that kept nothing answers %+v", first)
	}
	saved := readCopyAnswer(t, copyRequest(h, "PUT", copyBody(t, prText, 1)))
	want := string(encodedPipeline(t))
	if saved.Version != 2 || saved.FactoryCopy == nil || saved.FactoryCopy.Text != want || saved.FactoryCopy.By != "solo-owner" ||
		!saved.FactoryCopy.At.Equal(time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("saved %+v", saved)
	}
	if len(g.calls) != 0 {
		t.Fatalf("a save asked GitHub: %v", g.calls)
	}
	kept, version, err := st.PipelineCopy("ACME/api")
	if err != nil || kept == nil || kept.Text != want || version != 2 {
		t.Fatalf("the store keeps %+v at %d (%v)", kept, version, err)
	}

	// main has no file: the source says so beside the copy.
	src := readSource(t, sourceRequest(h, "", "GET", sourcePath))
	if src.State != api.PipelineEditMissing || src.FactoryCopy == nil || src.FactoryCopy.Main != api.PipelineCopyMainMissing ||
		src.FactoryCopy.Text != want || src.CopyVersion != 2 || src.Can == nil || !src.Can.UseOnFactory {
		t.Fatalf("source %+v", src)
	}
	// main has another file: differs, and main's fields stay main's.
	g.file = []byte(editSourceFile)
	src = readSource(t, sourceRequest(h, "", "GET", sourcePath))
	if src.State != api.PipelineEditFile || src.Text != editSourceFile || src.FactoryCopy == nil || src.FactoryCopy.Main != api.PipelineCopyMainDiffers {
		t.Fatalf("source %+v", src)
	}
}

// A SAVE IS CHECKED EXACTLY AS A PULL REQUEST IS: the same text is refused by
// both routes in the same words, before anything is written, with the phase
// marker; nothing is kept.
func TestPipelineCopyRefusesExactlyWhatThePullRequestRouteRefuses(t *testing.T) {
	g := newPRGitHub(t)
	h, st := soloCopy(t, g)
	for name, text := range map[string]string{
		"empty":     "   ",
		"too large": strings.Repeat("a", pipelinePRMaxText+1),
		"not yaml":  "pipeline: [\n",
		"a problem": "pipeline:\n  issue: [write, proof]\nsteps:\n  write:\n    max: lots\n",
	} {
		t.Run(name, func(t *testing.T) {
			w := copyRequest(h, "PUT", copyBody(t, text, 1))
			if w.Code != 422 || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
				t.Fatalf("%d %q %s", w.Code, w.Header().Get(api.PhaseHeader), w.Body)
			}
			raw, _ := json.Marshal(map[string]string{"text": text})
			pr := prRequest(h, t.Context(), "", string(raw))
			var copyFail struct {
				Error string `json:"error"`
			}
			var prFail api.PipelinePRFailure
			if json.Unmarshal(w.Body.Bytes(), &copyFail) != nil || json.Unmarshal(pr.Body.Bytes(), &prFail) != nil || pr.Code != 422 || copyFail.Error != prFail.Error {
				t.Fatalf("copy said %q, pull request %d %q", copyFail.Error, pr.Code, prFail.Error)
			}
		})
	}
	if c, _, _ := st.PipelineCopy("acme/api"); c != nil {
		t.Fatalf("a refused save kept %+v", c)
	}
}

// EVERY WRITE NAMES THE VERSION READ: none, or a stale one, is refused before
// anything is written (409 with the phase marker for a stale one), and a stop
// clears the copy and moves the version.
func TestPipelineCopyWritesNeedTheVersionReadAndAStopClears(t *testing.T) {
	g := newPRGitHub(t)
	h, st := soloCopy(t, g)
	if w := copyRequest(h, "PUT", `{"text":"pipeline: {}\n"}`); w.Code != 422 || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
		t.Fatalf("no expect: %d %s", w.Code, w.Body)
	}
	readCopyAnswer(t, copyRequest(h, "PUT", copyBody(t, prText, 1)))
	w := copyRequest(h, "PUT", copyBody(t, editSourceFile, 1))
	if w.Code != 409 || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
		t.Fatalf("stale save: %d %s", w.Code, w.Body)
	}
	if c, v, _ := st.PipelineCopy("acme/api"); c == nil || c.Text != string(encodedPipeline(t)) || v != 2 {
		t.Fatalf("a stale save changed the copy: %+v at %d", c, v)
	}
	if w := copyRequest(h, "DELETE", copyBody(t, "", 1)); w.Code != 409 {
		t.Fatalf("stale stop: %d %s", w.Code, w.Body)
	}
	if w := copyRequest(h, "DELETE", copyBody(t, prText, 2)); w.Code != 422 {
		t.Fatalf("a stop with text: %d %s", w.Code, w.Body)
	}
	stopped := readCopyAnswer(t, copyRequest(h, "DELETE", copyBody(t, "", 2)))
	if stopped.FactoryCopy != nil || stopped.Version != 3 {
		t.Fatalf("stopped %+v", stopped)
	}
	// Stopping nothing writes nothing.
	if again := readCopyAnswer(t, copyRequest(h, "DELETE", copyBody(t, "", 3))); again.Version != 3 {
		t.Fatalf("a second stop moved the version: %+v", again)
	}
}

// THE COPY CLEARS ITSELF when main's file becomes the same file: the read
// that finds it says main "same" once, and the factory keeps no copy after.
func TestPipelineCopyClearsItselfWhenMainSaysTheSame(t *testing.T) {
	g := newPRGitHub(t)
	h, st := soloCopy(t, g)
	readCopyAnswer(t, copyRequest(h, "PUT", copyBody(t, prText, 1)))
	// The merged file reads the same, with a comment and another layout.
	g.file = []byte("# merged\npipeline:\n  issue:\n    - write\n    - proof\n")
	src := readSource(t, sourceRequest(h, "", "GET", sourcePath))
	if src.FactoryCopy == nil || src.FactoryCopy.Main != api.PipelineCopyMainSame || src.CopyVersion != 3 {
		t.Fatalf("source %+v", src)
	}
	if c, v, err := st.PipelineCopy("acme/api"); c != nil || v != 3 || err != nil {
		t.Fatalf("the copy stayed: %+v at %d (%v)", c, v, err)
	}
	src = readSource(t, sourceRequest(h, "", "GET", sourcePath))
	if src.FactoryCopy != nil {
		t.Fatalf("the cleared copy is still said: %+v", src.FactoryCopy)
	}
}

// ONE ROUTE FOR SOLO AND TEAM; ON A TEAM ONLY THE OPERATOR SAVES OR STOPS:
// a signed-in member reads can.use_on_factory false and is refused a write
// with the phase marker, while the operator may.
func TestPipelineCopyOnATeamIsTheOperatorsAlone(t *testing.T) {
	f, g := linkFixture(t)
	_, gate := f.makeLink(t, api.CreateAccessLink{MaxUses: 2})
	member := signInHandle(t, f.h, g, &f.now, "priya", gate)
	var read api.PipelineCopyAnswer
	f.request(t, "GET", copyPath, member, nil, 200, &read)
	if read.Can.UseOnFactory || read.Version != 1 {
		t.Fatalf("a member reads %+v", read)
	}
	w := identityRequest(f.h, "PUT", copyPath, copyBody(t, prText, 1), member, nil)
	if w.Code != 403 || w.Header().Get(api.PhaseHeader) != api.PhasePreMutation {
		t.Fatalf("a member's save: %d %s", w.Code, w.Body)
	}
	f.request(t, "GET", copyPath, "operator-secret", nil, 200, &read)
	if !read.Can.UseOnFactory {
		t.Fatalf("the operator reads %+v", read)
	}
	f.request(t, "PUT", copyPath, "operator-secret", api.PipelineCopyRequest{Text: prText, Expect: &api.PipelineCopyExpect{Version: 1}}, 200, &read)
	if read.FactoryCopy == nil || read.Version != 2 {
		t.Fatalf("the operator saved %+v", read)
	}
	f.request(t, "PUT", copyPath, "", api.PipelineCopyRequest{Text: prText, Expect: &api.PipelineCopyExpect{Version: 2}}, 401, nil)
}

// A FACTORY WITH NO STORE keeps no copy: the route is not provided, and the
// edit-source read says nothing of one.
func TestPipelineCopyNeedsAStore(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte(editSourceFile)
	h := soloPR(t, g, factory.Seam{})
	if w := copyRequest(h, "GET", ""); w.Code != 501 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	src := readSource(t, sourceRequest(h, "", "GET", sourcePath))
	if src.FactoryCopy != nil || src.Can != nil || src.CopyVersion != 0 {
		t.Fatalf("%+v", src)
	}
}

// The factory's pipeline answer names a read from the copy.
func TestPipelineOriginNamesTheFactoryCopy(t *testing.T) {
	f, err := pipeline.CopyFile(factory.PipelineCopy{Repo: "acme/api", Text: prText, By: "solo-owner", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if p := wirePipeline(f); p.Origin != "factory copy" || p.Text != prText {
		t.Fatalf("%+v", p)
	}
}
