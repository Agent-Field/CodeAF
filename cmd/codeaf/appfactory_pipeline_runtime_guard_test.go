package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// guardRemote is a scripted factory with all three doors the guard touches:
// /v1/me, the edit-source read and the open-pr write. body is the edit-source
// answer and can be swapped between calls.
type guardRemote struct {
	srv   *httptest.Server
	mu    sync.Mutex
	log   []string
	posts []string
	body  string
	// operator is whether the caller is the factory's operator, who alone
	// saves its copy of the steps.
	operator bool
}

func newGuardRemote(t *testing.T) *guardRemote {
	t.Helper()
	g := &guardRemote{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.log = append(g.log, r.Method+" "+r.URL.EscapedPath())
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(b))
			g.posts = append(g.posts, string(b))
		}
		body, operator := g.body, g.operator
		g.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(api.Me{Mode: api.FactoryTeam, Operator: operator})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pipeline/edit-source"):
			serveRaw(body)(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/pipeline/open-pr"):
			prOpened(prDefaultBranch, "")(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *guardRemote) serve(t *testing.T, v api.PipelineEditSource, edit func(map[string]any)) {
	t.Helper()
	body := runtimeReply(t, v, edit)
	g.mu.Lock()
	g.body = body
	g.mu.Unlock()
}

func (g *guardRemote) count(prefix string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, l := range g.log {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (g *guardRemote) reset() { g.mu.Lock(); g.log, g.posts = nil, nil; g.mu.Unlock() }

// postContains is true when any POSTed body holds s.
func (g *guardRemote) postContains(s string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range g.posts {
		if strings.Contains(p, s) {
			return true
		}
	}
	return false
}

type guardWire struct {
	*editWire
	remote *guardRemote
	clock  *factoryClock
}

func newGuardWire(t *testing.T) *guardWire {
	t.Helper()
	g := newGuardRemote(t)
	stub := &editStub{t: t, srv: g.srv}
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	c := &factoryClock{at: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)}
	r.hub.now = c.now
	r.hub.hold(r.id, prToken, 0)
	return &guardWire{editWire: r, remote: g, clock: c}
}

// attest makes the remote answer text as an attesting factory and reads it.
func (w *guardWire) attest(t *testing.T, text string) appwire.FactoryPipelineSource {
	t.Helper()
	w.remote.serve(t, attested(editSourceFor(prRepo, text)), nil)
	got, err := w.source()
	if err != nil || !got.RuntimeFields {
		t.Fatalf("%+v %v", got, err)
	}
	return got
}

func (w *guardWire) open(text, sha string) (appwire.FactoryPipelinePRDone, error) {
	return w.backend.FactoryPipelinePR(context.Background(), appwire.FactoryPipelinePRParams{Factory: w.id, Repo: prRepo, Text: text, ExpectedBaseSHA: sha})
}

func (w *guardWire) previewAt(sha, base string, edits ...appwire.FactoryPipelineEdit) (appwire.FactoryPipelinePreview, error) {
	return w.backend.FactoryPipelinePreview(context.Background(), appwire.FactoryPipelinePreviewParams{Factory: w.id, Repo: prRepo, BaseSHA: sha, BaseText: base, Edits: edits})
}

func promptEdit(step, prompt string) appwire.FactoryPipelineEdit {
	return appwire.FactoryPipelineEdit{Op: "set_step", Name: step, Prompt: str(prompt)}
}

func wantRefusal(t *testing.T, err error, words string) {
	t.Helper()
	var we *appwire.Error
	if !errors.As(err, &we) || !strings.Contains(we.Message, words) {
		t.Fatalf("want %q, got %v", words, err)
	}
}

const rereadWords = "Read the file again"

func TestRuntimeAttestationIsRecordedOnlyForAnAcceptedAttestedFile(t *testing.T) {
	for name, c := range map[string]struct {
		v    api.PipelineEditSource
		edit func(map[string]any)
		want bool
	}{
		"attested file":     {attested(editSourceFor(prRepo, runtimeSourceFile)), nil, true},
		"attested legacy":   {attested(editSourceFor(prRepo, editTestFile)), nil, true},
		"no claim":          {editSourceFor(prRepo, editTestFile), oldShape, false},
		"claim false":       {editSourceFor(prRepo, editTestFile), func(m map[string]any) { m["runtime_fields"] = false }, false},
		"claim is a string": {attested(editSourceFor(prRepo, editTestFile)), func(m map[string]any) { m["runtime_fields"] = "true" }, false},
		"invalid file":      {attested(editSourceFor(prRepo, "pipeline: [")), nil, false},
		"lying steps":       {attested(editSourceFor(prRepo, runtimeSourceFile)), oldShape2, false},
		"runtime, no claim": {editSourceFor(prRepo, runtimeSourceFile), oldShape, false},
		// No file at all is edited from the shipped defaults, so the claim is
		// remembered for it too, as the text "".
		"missing file": {missingAttested(), nil, true},
		"missing, no claim": {func() api.PipelineEditSource {
			v := missingAttested()
			v.RuntimeFields = false
			return v
		}(), nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			w := newGuardWire(t)
			w.remote.serve(t, c.v, c.edit)
			_, _ = w.source()
			_, got := w.hub.runtimeRecord(w.id, w.hub.rows[0].URL, prToken, prRepo, prBaseSHA)
			if got != c.want {
				t.Fatalf("recorded %v, want %v", got, c.want)
			}
		})
	}
}

func missingAttested() api.PipelineEditSource {
	v := attested(editSourceFor(prRepo, ""))
	v.State, v.Problems = api.PipelineEditMissing, nil
	return v
}

// A RUNTIME FILE CREATED WHERE THERE WAS NONE takes the same seal as an edited
// one: the factory attested the missing read at this commit, and still serves
// no file there when the pull request is asked for.
func TestARuntimeFileCreatedFromNoFileIsSealedByTheMissingRead(t *testing.T) {
	w := newGuardWire(t)
	when := appwire.FactoryPipelineEdit{Op: "set_step", Name: "review", When: str("thin")}
	// Before any read, neither door takes it.
	_, err := w.previewAt(prBaseSHA, "", when)
	wantRefusal(t, err, rereadWords)
	w.remote.serve(t, missingAttested(), nil)
	if got, err := w.source(); err != nil || !got.Editable || got.State != "missing" {
		t.Fatalf("%+v %v", got, err)
	}
	got, err := w.previewAt(prBaseSHA, "", when)
	if err != nil || !got.Changed {
		t.Fatalf("%+v %v", got, err)
	}
	w.remote.reset()
	if _, err := w.open(got.Text, prBaseSHA); err != nil {
		t.Fatal(err)
	}
	if w.remote.count("POST") != 1 || w.remote.count("GET /v1/repos") != 1 {
		t.Fatalf("the seal must read the source again before the one write: %v", w.remote.log)
	}
	// A file appeared at that commit since: the missing read no longer matches.
	w.remote.serve(t, attested(editSourceFor(prRepo, editTestFile)), nil)
	w.remote.reset()
	_, err = w.open(got.Text, prBaseSHA)
	wantRefusal(t, err, "changed since it was read")
	if w.remote.count("POST") != 0 {
		t.Fatalf("%v", w.remote.log)
	}
}

// oldShape2 keeps the claim but drops the step words, the way a lying or
// half-built factory would.
func oldShape2(m map[string]any) {
	steps, _ := m["steps"].([]any)
	for _, s := range steps {
		for _, k := range []string{"kind", "when", "effort", "fanout"} {
			delete(s.(map[string]any), k)
		}
	}
}

func TestUnattestedRemoteCannotPreviewOrProposeRuntimeText(t *testing.T) {
	w := newGuardWire(t)
	// Nothing was ever read: a hand-built preview and a hand-built PR.
	_, err := w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "plan better"))
	wantRefusal(t, err, rereadWords)
	var we *appwire.Error
	_ = errors.As(err, &we)
	if d, ok := we.Detail.(appwire.FactoryPipelineEditRefusal); !ok || d.Code != "bad_op" || d.Op != nil {
		t.Fatalf("%+v", we.Detail)
	}
	// A no-op edit changes nothing, and still must not echo a runtime base back
	// as though this factory had vouched for it.
	_, err = w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "plan it"))
	wantRefusal(t, err, rereadWords)
	_, err = w.open(runtimeSourceFile, prBaseSHA)
	wantRefusal(t, err, "Nothing was sent")
	if w.remote.count("POST") != 0 || w.remote.count("GET /v1/repos") != 0 {
		t.Fatalf("remote saw %v", w.remote.log)
	}
}

func TestAnOldRemotesRuntimeFileIsNeverAttestedAndSendsNothing(t *testing.T) {
	w := newGuardWire(t)
	w.remote.serve(t, editSourceFor(prRepo, runtimeSourceFile), oldShape)
	if _, err := w.source(); err == nil {
		t.Fatal("accepted a runtime file from a factory that did not attest")
	}
	_, err := w.open(runtimeSourceFile, prBaseSHA)
	wantRefusal(t, err, rereadWords)
	if w.remote.count("POST") != 0 {
		t.Fatalf("remote saw %v", w.remote.log)
	}
}

func TestRuntimeAttestationDoesNotOutliveTheCallerOrTheOrigin(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	edit := promptEdit("plan", "plan better")
	if _, err := w.previewAt(prBaseSHA, runtimeSourceFile, edit); err != nil {
		t.Fatal(err)
	}
	// Another credential on the same connection inherits nothing.
	w.hub.hold(w.id, "tok_somebody_else_0123456789abcdef", 0)
	_, err := w.previewAt(prBaseSHA, runtimeSourceFile, edit)
	wantRefusal(t, err, rereadWords)
	// Holding the same bearer again is the same caller: only the same bytes
	// match the stored credential digest.
	w.hub.hold(w.id, prToken, 0)
	if _, err := w.previewAt(prBaseSHA, runtimeSourceFile, edit); err != nil {
		t.Fatalf("the same caller holding the same credential again should still match: %v", err)
	}
	// A row re-pointed at another server under the same id.
	other := newGuardRemote(t)
	w.hub.mu.Lock()
	w.hub.rows[0].URL = other.srv.URL
	w.hub.mu.Unlock()
	_, err = w.previewAt(prBaseSHA, runtimeSourceFile, edit)
	wantRefusal(t, err, rereadWords)
	_, err = w.open(runtimeSourceFile, prBaseSHA)
	wantRefusal(t, err, rereadWords)
	if other.count("POST") != 0 {
		t.Fatalf("%v", other.log)
	}
}

func TestRuntimeAttestationIsPerRepoCommitAndExactText(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	edit := promptEdit("plan", "plan better")
	other := strings.Repeat("a", 40)
	for name, call := range map[string]func() error{
		"another commit":   func() error { _, err := w.previewAt(other, runtimeSourceFile, edit); return err },
		"empty commit":     func() error { _, err := w.previewAt("", runtimeSourceFile, edit); return err },
		"edited base text": func() error { _, err := w.previewAt(prBaseSHA, runtimeSourceFile+"# note\n", edit); return err },
		"PR other commit":  func() error { _, err := w.open(runtimeSourceFile, other); return err },
		"PR empty commit":  func() error { _, err := w.open(runtimeSourceFile, ""); return err },
	} {
		t.Run(name, func(t *testing.T) { wantRefusal(t, call(), rereadWords) })
	}
	if w.remote.count("POST") != 0 {
		t.Fatalf("%v", w.remote.log)
	}
	// The owner's case does not matter, as on GitHub.
	if _, ok := w.hub.runtimeRecord(w.id, w.hub.rows[0].URL, prToken, strings.ToUpper(prRepo), prBaseSHA); !ok {
		t.Fatal("repository case split the record")
	}
}

func TestRuntimeAttestationExpiresAndIsBounded(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	w.clock.advance(appFactoryRuntimeAttestLife - time.Second)
	if _, ok := w.hub.runtimeRecord(w.id, w.hub.rows[0].URL, prToken, prRepo, prBaseSHA); !ok {
		t.Fatal("expired early")
	}
	w.clock.advance(time.Second)
	_, err := w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "x"))
	wantRefusal(t, err, rereadWords)

	h, origin := w.hub, w.hub.rows[0].URL
	for i := 0; i < appFactoryRuntimeAttestMax+1; i++ {
		w.clock.advance(time.Second)
		h.recordRuntime(w.id, origin, prToken, prRepo, strings.Repeat(string(rune('a'+i%6)), 39)+string(rune('0'+i/6)), "t")
	}
	h.mu.Lock()
	n := len(h.runtimeOK)
	h.mu.Unlock()
	if n != appFactoryRuntimeAttestMax {
		t.Fatalf("%d records", n)
	}
	if _, ok := h.runtimeRecord(w.id, origin, prToken, prRepo, strings.Repeat("a", 39)+"0"); ok {
		t.Fatal("the oldest survived")
	}
	last := appFactoryRuntimeAttestMax
	if _, ok := h.runtimeRecord(w.id, origin, prToken, prRepo, strings.Repeat(string(rune('a'+last%6)), 39)+string(rune('0'+last/6))); !ok {
		t.Fatal("the newest was evicted")
	}
	h.forgetAll()
	if _, ok := h.runtimeRecord(w.id, origin, prToken, prRepo, prBaseSHA); ok {
		t.Fatal("kept after close")
	}
}

func TestLegacyPreviewAndPRAreUnchangedWithoutAttestation(t *testing.T) {
	w := newGuardWire(t)
	got, err := w.previewAt(prBaseSHA, editTestFile, promptEdit("plan", "plan better"))
	if err != nil || !got.Changed || len(got.Steps) != 1 || strings.Join(got.Steps[0].Fields, ",") != "prompt" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := w.open(got.Text, ""); err != nil {
		t.Fatal(err)
	}
	if w.remote.count("POST") != 1 || w.remote.count("GET /v1/repos") != 0 {
		t.Fatalf("legacy text must take the old path: %v", w.remote.log)
	}
}

func TestAttestedRuntimeBaseKeepsItsWordsAndEditsACheckAsACommand(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	got, err := w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "plan better"))
	if err != nil || !got.Changed || strings.Join(got.Steps[0].Fields, ",") != "prompt" {
		t.Fatalf("%+v %v", got, err)
	}
	for _, word := range []string{"kind: check", "when: thin", "effort: cheap", "fanout: per-file"} {
		if !strings.Contains(got.Text, word) {
			t.Fatalf("lost %q:\n%s", word, got.Text)
		}
	}
	// A CHECK'S COMMAND IS EDITED LIKE A PROMPT, and it stays a check.
	got, err = w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("test", "go vet ./..."))
	if err != nil || !got.Changed || !strings.Contains(got.Text, "kind: check") || !strings.Contains(got.Text, "go vet ./...") {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestPRWithAttestedBaseIsReadAgainAndSentOnce(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	pv, err := w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "plan better"))
	if err != nil {
		t.Fatal(err)
	}
	w.remote.reset()
	if _, err := w.open(pv.Text, prBaseSHA); err != nil {
		t.Fatal(err)
	}
	if w.remote.count("POST") != 1 || w.remote.count("GET /v1/repos") != 1 {
		t.Fatalf("want one re-read and one POST: %v", w.remote.log)
	}
}

func TestPRRefusesWhenTheFactoryNoLongerServesWhatWasAttested(t *testing.T) {
	changed := runtimeSourceFile + "# changed after the read\n"
	moved := attested(editSourceFor(prRepo, runtimeSourceFile))
	moved.BaseSHA = strings.Repeat("b", 40)
	for name, c := range map[string]struct {
		v    api.PipelineEditSource
		edit func(map[string]any)
	}{
		"same commit, other bytes": {attested(editSourceFor(prRepo, changed)), nil},
		"default branch moved":     {moved, nil},
		"downgraded to no claim":   {editSourceFor(prRepo, runtimeSourceFile), oldShape},
	} {
		t.Run(name, func(t *testing.T) {
			w := newGuardWire(t)
			w.attest(t, runtimeSourceFile)
			w.remote.serve(t, c.v, c.edit)
			w.remote.reset()
			if _, err := w.open(runtimeSourceFile, prBaseSHA); err == nil {
				t.Fatal("sent")
			} else {
				var we *appwire.Error
				if !errors.As(err, &we) {
					t.Fatalf("untyped refusal: %v", err)
				}
				if name == "downgraded to no claim" {
					if we.Code != appwire.CodeEngine || !strings.Contains(err.Error(), errPipelineSourceRuntimeUnattested.Error()) {
						t.Fatalf("wrong downgrade refusal: %v", err)
					}
				} else if we.Code != appwire.CodeConflict || !strings.Contains(err.Error(), "The file or the factory changed since it was read.") {
					t.Fatalf("wrong source refusal: %v", err)
				}
			}
			if w.remote.count("POST") != 0 || w.remote.count("GET /v1/repos") != 1 {
				t.Fatalf("%v", w.remote.log)
			}
		})
	}
}

func TestPRRefusalDoesNotBecomeASilentFallback(t *testing.T) {
	w := newGuardWire(t)
	w.attest(t, runtimeSourceFile)
	// The refresh itself fails: the person gets an error, not a legacy send.
	w.remote.mu.Lock()
	w.remote.body = io.EOF.Error()
	w.remote.mu.Unlock()
	if _, err := w.open(runtimeSourceFile, prBaseSHA); err == nil {
		t.Fatal("sent after a failed re-read")
	}
	if w.remote.count("POST") != 0 {
		t.Fatalf("%v", w.remote.log)
	}
}

// A FACTORY COPY IS SEALED AS A PULL REQUEST IS: text that spells a runtime key
// is saved only when the factory attested the file it was edited from and, read
// again first, still serves and attests it. Nothing unattested is sent.
func TestPipelineCopyOfRuntimeTextIsSealedAsAPullRequestIs(t *testing.T) {
	save := func(w *guardWire, text string) error {
		_, err := w.backend.FactoryPipelineCopySet(context.Background(), appwire.FactoryPipelineCopySetParams{Factory: w.id, Repo: prRepo, Text: text,
			Expect: &appwire.FactoryPipelineCopyExpect{Version: 1}})
		return err
	}
	operator := func(t *testing.T) *guardWire {
		w := newGuardWire(t)
		w.remote.mu.Lock()
		w.remote.operator = true
		w.remote.mu.Unlock()
		return w
	}

	// Never read as attested: the factory serves the runtime file as an old
	// factory would, and nothing is saved.
	w := operator(t)
	w.remote.serve(t, editSourceFor(prRepo, runtimeSourceFile), oldShape)
	if err := save(w, runtimeSourceFile); err == nil {
		t.Fatal("saved")
	}
	if w.remote.count("PUT") != 0 {
		t.Fatalf("remote saw %v", w.remote.log)
	}

	// Attested, edited, saved: one read again, then the one write.
	w = operator(t)
	w.attest(t, runtimeSourceFile)
	pv, err := w.previewAt(prBaseSHA, runtimeSourceFile, promptEdit("plan", "plan better"))
	if err != nil {
		t.Fatal(err)
	}
	w.remote.reset()
	// The scripted factory has no copy route, so the write is refused as an
	// older factory's; what matters is that it was sent.
	_ = save(w, pv.Text)
	if w.remote.count("PUT") != 1 || w.remote.count("GET /v1/repos") != 1 {
		t.Fatalf("want one read again and one write: %v", w.remote.log)
	}

	// The factory serves other bytes than were attested: refused, not sent.
	w.remote.serve(t, attested(editSourceFor(prRepo, runtimeSourceFile+"# changed after the read\n")), nil)
	w.remote.reset()
	err = save(w, pv.Text)
	wantRefusal(t, err, rereadWords)
	if w.remote.count("PUT") != 0 {
		t.Fatalf("remote saw %v", w.remote.log)
	}

	// Legacy text is saved without a second read.
	w.remote.reset()
	_ = save(w, editTestFile)
	if w.remote.count("PUT") != 1 || w.remote.count("GET /v1/repos") != 0 {
		t.Fatalf("legacy text: %v", w.remote.log)
	}
}
