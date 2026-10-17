package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

const editTestFile = `# our pipeline
default_budget: 4
pipeline:
  issue: [plan, approve, write, proof]
steps:
  plan:
    prompt: plan it
  write:
    prompt: write it
    max: 2
  proof:
    prompt: prove it
`

// ── a scripted factory: /v1/me and the one read route ───────────────────────

type editStub struct {
	t      *testing.T
	srv    *httptest.Server
	mu     sync.Mutex
	log    []string
	auths  []string
	me     api.Me
	source func(http.ResponseWriter, *http.Request)
	onRead func()
}

func newEditStub(t *testing.T, me api.Me) *editStub {
	t.Helper()
	s := &editStub{t: t, me: me}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.log = append(s.log, r.Method+" "+r.URL.RequestURI())
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		me, source, onRead := s.me, s.source, s.onRead
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/me":
			_ = json.NewEncoder(w).Encode(me)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pipeline/edit-source"):
			if onRead != nil {
				onRead()
			}
			if source != nil {
				source(w, r)
				return
			}
			editSourceReply(editSourceFor(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/repos/"), "/pipeline/edit-source"), editTestFile))(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *editStub) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.log...)
}

// editSourceFor is what the real route answers for a file that reads cleanly.
func editSourceFor(repo, text string) api.PipelineEditSource {
	src, problems, ok := pipeline.Inspect(text)
	out := api.PipelineEditSource{Factory: api.Factory{Name: "Acme"}, Repo: repo, Path: factory.PipelineFile, DefaultBranch: "main",
		BaseSHA: prBaseSHA, CanPush: true, State: api.PipelineEditInvalid, Text: text, Problems: problems}
	if ok {
		out.State, out.Editable, out.Problems = api.PipelineEditFile, true, nil
		out.SetParsed(src)
	}
	return out
}

func editSourceReply(v api.PipelineEditSource) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) { prWriteJSON(w, 200, v) }
}

type editWire struct {
	*factoryWire
	hub *appFactoryHub
	id  string
}

func newEditWire(t *testing.T, stub *editStub, local bool, mode, token string) *editWire {
	t.Helper()
	hub := testFactoryHub(t)
	w := newFactoryWire(t, hub)
	hub.resume = nil
	id := appFactoryID(stub.srv.URL)
	if local {
		id = appLocalID
	}
	hub.mu.Lock()
	hub.rows = []appFactoryRow{{ID: id, URL: stub.srv.URL, Name: "Acme", Mode: mode, Local: local}}
	hub.loaded = true
	hub.mu.Unlock()
	if token != "" {
		hub.hold(id, token, 0)
	}
	return &editWire{factoryWire: w, hub: hub, id: id}
}

func (r *editWire) source() (appwire.FactoryPipelineSource, error) {
	return r.backend.FactoryPipelineSource(context.Background(), appwire.FactoryPipelineSourceParams{Factory: r.id, Repo: prRepo})
}

func (r *editWire) preview(base string, edits ...appwire.FactoryPipelineEdit) (appwire.FactoryPipelinePreview, error) {
	return r.backend.FactoryPipelinePreview(context.Background(), appwire.FactoryPipelinePreviewParams{Factory: r.id, Repo: prRepo, BaseSHA: prBaseSHA, BaseText: base, Edits: edits})
}

func str(s string) *string   { return &s }
func num(n int) *int         { return &n }
func dol(v float64) *float64 { return &v }

// ── capability ─────────────────────────────────────────────────────────────

func TestPipelineEditIsAnnouncedAndDistinct(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	for _, want := range []string{appwire.FeatureFactoryPipelineEdit, appwire.FeatureFactoryPipelinePR} {
		if !strings.Contains(r.hello, want) {
			t.Fatalf("hello does not announce %s: %s", want, r.hello)
		}
	}
	var caps appwire.FactoryCapabilities
	r.must("factory.capabilities", struct{}{}, &caps)
	if !caps.PipelineEdit || !caps.PipelinePR || caps.Writes {
		t.Fatalf("%+v", caps)
	}
}

// ── source: the yes path ───────────────────────────────────────────────────

func TestPipelineSourceReadsOnceAsTheHeldCaller(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam, Profile: api.Profile{Handle: "alice"}}) // a member, not the operator
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	got, err := r.source()
	if err != nil {
		t.Fatal(err)
	}
	if got.Factory != r.id || got.Repo != prRepo || got.Path != ".codeaf/factory.yaml" || got.BaseSHA != prBaseSHA || got.DefaultBranch != "main" ||
		got.State != "file" || !got.Editable || got.Text != editTestFile || !got.CanPush || got.DefaultBudget != 4 || !got.BudgetInFile {
		t.Fatalf("%+v", got)
	}
	if len(got.Steps) == 0 || len(got.Pipelines) != 3 || got.Pipelines[0].Kind != "issue" {
		t.Fatalf("%+v", got)
	}
	if log := strings.Join(stub.requests(), "|"); log != "GET /v1/me|GET /v1/repos/octo/Hello.x_y/pipeline/edit-source" {
		t.Fatalf("requests %s", log)
	}
	for _, a := range stub.auths {
		if a != "Bearer "+prToken {
			t.Fatalf("sent %q", a)
		}
	}
}

func TestPipelineSourceServesSoloAndTheOwnedLocalFactoryWithAHeldCredential(t *testing.T) {
	for name, c := range map[string]struct {
		local bool
		mode  string
	}{"attached solo": {false, api.FactorySolo}, "owned local": {true, api.FactorySolo}, "mode not learned": {false, ""}} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactorySolo})
			r := newEditWire(t, stub, c.local, c.mode, prToken)
			if _, err := r.source(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPipelineSourceNeedsAHeldCredentialAndSendsNothingWithout(t *testing.T) {
	t.Setenv("GH_TOKEN", "ghp_ambient_host")
	t.Setenv("GITHUB_TOKEN", "ghp_ambient_host")
	t.Setenv("CODEAF_FACTORY_TOKEN", "tok_ambient_host_key")
	for name, c := range map[string]struct {
		local bool
		mode  string
		code  string
	}{"team": {false, api.FactoryTeam, appwire.CodeSignedOut}, "solo": {false, api.FactorySolo, appwire.CodeSignedOut}, "local": {true, api.FactorySolo, appwire.CodeUnreachable}} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: c.mode})
			r := newEditWire(t, stub, c.local, c.mode, "")
			_, err := r.source()
			prWantCode(t, err, c.code)
			_, err = r.preview(editTestFile, appwire.FactoryPipelineEdit{Op: "set_budget", Value: dol(5)})
			prWantCode(t, err, c.code)
			if len(stub.requests()) != 0 {
				t.Fatalf("requests %v", stub.requests())
			}
		})
	}
}

// ── source: states that refuse editing ─────────────────────────────────────

func TestPipelineSourceStatesAreCarriedAndRefuseEditing(t *testing.T) {
	for name, c := range map[string]struct {
		body  api.PipelineEditSource
		state string
	}{
		"missing":   {api.PipelineEditSource{State: api.PipelineEditMissing}, "missing"},
		"too large": {api.PipelineEditSource{State: api.PipelineEditTooLarge}, "too_large"},
		"syntax":    {editSourceFor(prRepo, "pipeline: ["), "invalid"},
		"unknown":   {editSourceFor(prRepo, "model: x\n"), "invalid"},
		"blank":     {editSourceFor(prRepo, " \n"), "invalid"},
	} {
		t.Run(name, func(t *testing.T) {
			body := c.body
			body.Repo, body.Path, body.DefaultBranch, body.BaseSHA = prRepo, factory.PipelineFile, "main", prBaseSHA
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			stub.source = editSourceReply(body)
			r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
			got, err := r.source()
			if err != nil || got.State != c.state || got.Editable || got.Steps != nil || got.Pipelines != nil || got.DefaultBudget != 0 {
				t.Fatalf("%+v %v", got, err)
			}
			if c.state == "invalid" && (len(got.Problems) == 0 || got.Text != body.Text) {
				t.Fatalf("%+v", got)
			}
			if c.state != "invalid" && (got.Text != "" || len(got.Problems) != 0) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

// ── source: a factory is not believed ──────────────────────────────────────

func TestPipelineSourceBelievesOnlyADocumentedShape(t *testing.T) {
	good := editSourceFor(prRepo, editTestFile)
	mutate := func(f func(*api.PipelineEditSource)) api.PipelineEditSource {
		v := editSourceFor(prRepo, editTestFile)
		f(&v)
		return v
	}
	for name, body := range map[string]api.PipelineEditSource{
		"another repository":     mutate(func(v *api.PipelineEditSource) { v.Repo = "octo/other" }),
		"another file":           mutate(func(v *api.PipelineEditSource) { v.Path = "README.md" }),
		"no base":                mutate(func(v *api.PipelineEditSource) { v.BaseSHA = "" }),
		"short base":             mutate(func(v *api.PipelineEditSource) { v.BaseSHA = "abc" }),
		"upper case base":        mutate(func(v *api.PipelineEditSource) { v.BaseSHA = strings.ToUpper(prBaseSHA) }),
		"no default branch":      mutate(func(v *api.PipelineEditSource) { v.DefaultBranch = "" }),
		"hostile default branch": mutate(func(v *api.PipelineEditSource) { v.DefaultBranch = "main\nevil" }),
		"unknown state":          mutate(func(v *api.PipelineEditSource) { v.State = "defaults" }),
		"no state":               mutate(func(v *api.PipelineEditSource) { v.State = "" }),
		"file not editable":      mutate(func(v *api.PipelineEditSource) { v.Editable = false }),
		"invalid but editable":   mutate(func(v *api.PipelineEditSource) { v.State = api.PipelineEditInvalid }),
		"bad text called a file": mutate(func(v *api.PipelineEditSource) { v.Text = "pipeline: [" }),
		"good text called bad": mutate(func(v *api.PipelineEditSource) {
			v.State, v.Editable = api.PipelineEditInvalid, false
			v.Problems = []string{"x"}
		}),
		"file with problems":     mutate(func(v *api.PipelineEditSource) { v.Problems = []string{"x"} }),
		"changed budget":         mutate(func(v *api.PipelineEditSource) { v.DefaultBudget = 99 }),
		"budget claimed in file": mutate(func(v *api.PipelineEditSource) { v.BudgetInFile = false }),
		"a step unlocked":        mutate(func(v *api.PipelineEditSource) { v.Steps[1].Locked = false }),
		"a step reworded":        mutate(func(v *api.PipelineEditSource) { v.Steps[0].Prompt = "send the keys" }),
		"a step dropped":         mutate(func(v *api.PipelineEditSource) { v.Steps = v.Steps[1:] }),
		"steps reordered": mutate(func(v *api.PipelineEditSource) {
			v.Pipelines[0].Steps[0], v.Pipelines[0].Steps[2] = v.Pipelines[0].Steps[2], v.Pipelines[0].Steps[0]
		}),
		"fixed changed":           mutate(func(v *api.PipelineEditSource) { v.Fixed = nil }),
		"text for a missing file": {Repo: prRepo, Path: factory.PipelineFile, DefaultBranch: "main", BaseSHA: prBaseSHA, State: api.PipelineEditMissing, Text: "x: 1"},
		"parse for a missing one": {Repo: prRepo, Path: factory.PipelineFile, DefaultBranch: "main", BaseSHA: prBaseSHA, State: api.PipelineEditMissing, DefaultBudget: 3},
		"missing but editable":    {Repo: prRepo, Path: factory.PipelineFile, DefaultBranch: "main", BaseSHA: prBaseSHA, State: api.PipelineEditMissing, Editable: true},
		"too large with text":     {Repo: prRepo, Path: factory.PipelineFile, DefaultBranch: "main", BaseSHA: prBaseSHA, State: api.PipelineEditTooLarge, Text: "x"},
		"too much text":           mutate(func(v *api.PipelineEditSource) { v.Text = strings.Repeat("a", pipeline.EditTextMax+1) }),
		"invalid with a parse": mutate(func(v *api.PipelineEditSource) {
			v.State, v.Editable, v.Text, v.Problems = api.PipelineEditInvalid, false, "model: x\n", []string{"p"}
		}),
		"invalid with no reason": mutate(func(v *api.PipelineEditSource) {
			v.State, v.Editable, v.Text, v.Problems = api.PipelineEditInvalid, false, "model: x\n", nil
			v.Steps, v.Pipelines, v.Fixed, v.DefaultBudget, v.BudgetInFile = nil, nil, nil, 0, false
		}),
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			stub.source = editSourceReply(body)
			r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
			got, err := r.source()
			if err == nil {
				t.Fatalf("believed %+v", got)
			}
			prWantCode(t, err, appwire.CodeEngine)
			prNoNames(t, err, "octo", "Hello", prBaseSHA, "send the keys")
		})
	}
	// The control: the untouched answer is believed.
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	stub.source = editSourceReply(good)
	if _, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source(); err != nil {
		t.Fatal(err)
	}
}

func TestPipelineSourceUnusableAnswersAreEngineErrorsWithNoFactoryProse(t *testing.T) {
	for name, h := range map[string]func(http.ResponseWriter, *http.Request){
		"not json": func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `<html>ghp_leaked`) },
		"null":     func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, `null`) },
		"empty":    func(w http.ResponseWriter, _ *http.Request) {},
		"too large": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, strings.Repeat("x", appFactoryPipelineSourceLimit+1))
		},
		"202": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(202); _, _ = io.WriteString(w, `{}`) },
		"redirect": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", "https://evil.example")
			w.WriteHeader(302)
		},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			stub.source = h
			r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
			_, err := r.source()
			if err == nil {
				t.Fatal("believed")
			}
			prNoNames(t, err, "evil.example", "html")
		})
	}
}

func TestPipelineSourceRefusalsUseFixedWords(t *testing.T) {
	typed := func(status int, stage string) func(http.ResponseWriter, *http.Request) {
		return func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, status, api.PipelineEditSourceFailure{Error: "ghp_leaked_secret at /v1/repos/octo/Hello.x_y", Stage: stage, RequestID: "9f8e7d6c", GitHubStatus: status})
		}
	}
	for name, c := range map[string]struct {
		h    func(http.ResponseWriter, *http.Request)
		code string
	}{
		"no github":       {typed(401, "credential"), appwire.CodeGitHubExpired},
		"github rejected": {typed(401, "repository"), appwire.CodeGitHubExpired},
		"repo hidden":     {typed(404, "repository"), appwire.CodeNotFound},
		"branch hidden":   {typed(403, "base"), appwire.CodeForbidden},
		"file refused":    {typed(403, "file"), appwire.CodeForbidden},
		"github down":     {typed(502, "file"), appwire.CodeEngine},
		"rate limited":    {typed(429, "repository"), appwire.CodeEngine},
		"request refused": {typed(422, "request"), appwire.CodeBadParams},
		"shared gate 401": {func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 401, map[string]string{"error": "ghp_leaked_secret"})
		}, appwire.CodeSignedOut},
		"shared gate 404": {func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 404, map[string]string{"error": "ghp_leaked_secret"})
		}, appwire.CodeNotFound},
		"proxy 500": {func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 500, map[string]string{"error": "ghp_leaked_secret"})
		}, appwire.CodeEngine},
		"unknown stage": {typed(403, "deploy"), appwire.CodeForbidden},
		"no request id": {func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 403, api.PipelineEditSourceFailure{Stage: "file", Error: "ghp_leaked_secret"})
		}, appwire.CodeForbidden},
		"hostile id": {func(w http.ResponseWriter, _ *http.Request) {
			prWriteJSON(w, 403, api.PipelineEditSourceFailure{Stage: "file", RequestID: "a b\nc", Error: "ghp_leaked_secret"})
		}, appwire.CodeForbidden},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			stub.source = c.h
			r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
			_, err := r.source()
			prWantCode(t, err, c.code)
			prNoNames(t, err, "octo", "Hello")
			if we := prWantCode(t, err, c.code); we.Detail != nil {
				t.Fatalf("detail %+v", we.Detail)
			}
		})
	}
}

func TestPipelineSourceConnectionTrouble(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	stub.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v1/me" {
			_ = json.NewEncoder(w).Encode(api.Me{Mode: api.FactoryTeam})
			return
		}
		hj, _ := w.(http.Hijacker)
		conn, _, _ := hj.Hijack()
		conn.Close()
	})
	_, err := r.source()
	prWantCode(t, err, appwire.CodeUnreachable)
	prNoNames(t, err, "127.0.0.1")
}

// ── the caller changes while the read is in flight ─────────────────────────

func TestPipelineSourceAnAnswerForAReplacedCallerRevealsNothing(t *testing.T) {
	for change, swap := range map[string]func(*editWire){
		"replaced by another sign-in": func(r *editWire) { r.hub.hold(r.id, "tok_other_person_0123456789", 0) },
		"signed out":                  func(r *editWire) { r.hub.dropSession(r.id, prToken) },
	} {
		for name, h := range map[string]func(http.ResponseWriter, *http.Request){
			"file": editSourceReply(editSourceFor(prRepo, editTestFile)),
			"refused": func(w http.ResponseWriter, _ *http.Request) {
				prWriteJSON(w, 403, api.PipelineEditSourceFailure{Stage: "file", RequestID: "9f8e7d6c"})
			},
		} {
			t.Run(change+" / "+name, func(t *testing.T) {
				stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
				stub.source = h
				r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
				stub.onRead = func() { swap(r) }
				got, err := r.source()
				if err == nil {
					t.Fatalf("revealed %+v", got)
				}
				if code := wireCodeOf(t, err); code != appwire.CodeConflict && code != appwire.CodeSignedOut {
					t.Fatalf("code %s", code)
				}
				prNoNames(t, err, "octo", "Hello", prBaseSHA, "plan it", "tok_other_person")
			})
		}
	}
}

func TestPipelineSourceACredentialReplacedBeforeTheReadSendsNothing(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	var once sync.Once
	stub.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v1/me" {
			once.Do(func() { r.hub.hold(r.id, "tok_other_person_0123456789", 0) })
			_ = json.NewEncoder(w).Encode(api.Me{Mode: api.FactoryTeam})
			return
		}
		t.Errorf("a request left after the sign-in changed: %s %s", req.Method, req.URL.Path)
	})
	if _, err := r.source(); err == nil {
		t.Fatal("read for a caller who was replaced")
	}
}

// ── preview ────────────────────────────────────────────────────────────────

func TestPipelinePreviewIsPureAndAsksOnlyWhoHoldsTheConnection(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	got, err := r.preview(editTestFile,
		appwire.FactoryPipelineEdit{Op: "set_step", Name: "write", Prompt: str("write it carefully"), Max: num(5)},
		appwire.FactoryPipelineEdit{Op: "set_budget", Value: dol(6)})
	if err != nil {
		t.Fatal(err)
	}
	if log := strings.Join(stub.requests(), "|"); log != "GET /v1/me" {
		t.Fatalf("a preview asks nobody but who holds the connection: %s", log)
	}
	if !got.Changed || !got.FormatRewritten || !got.CommentsDropped || got.Factory != r.id || got.Repo != prRepo || got.BaseSHA != prBaseSHA || got.Path != ".codeaf/factory.yaml" {
		t.Fatalf("%+v", got)
	}
	if len(got.Steps) != 1 || got.Steps[0].Name != "write" || strings.Join(got.Steps[0].Fields, ",") != "prompt,max" ||
		got.Steps[0].Before.Max != 2 || got.Steps[0].After.Max != 5 || got.Steps[0].After.Prompt != "write it carefully" ||
		got.Budget == nil || got.Budget.Before != 4 || got.Budget.After != 6 {
		t.Fatalf("%+v", got)
	}
	// The text is exactly what the route will commit: its parse-and-encode.
	file, err := pipeline.Parse(got.Text)
	if err != nil || len(file.Problems) > 0 {
		t.Fatal(err, file.Problems)
	}
	data, _ := pipeline.Encode(file)
	if string(data) != got.Text {
		t.Fatalf("not canonical:\n%s", got.Text)
	}
}

func TestPipelinePreviewOfNothingIsTheBaseAndChangedFalse(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	got, err := r.preview(editTestFile, appwire.FactoryPipelineEdit{Op: "set_step", Name: "write", Max: num(2)}, appwire.FactoryPipelineEdit{Op: "set_budget", Value: dol(4)})
	if err != nil || got.Changed || got.Text != editTestFile || got.FormatRewritten || got.CommentsDropped || got.Steps == nil || len(got.Steps) != 0 || got.Budget != nil {
		t.Fatalf("%+v %v", got, err)
	}
	blob, _ := json.Marshal(got)
	if !strings.Contains(string(blob), `"steps":[]`) {
		t.Fatalf("steps must be an empty list on the wire: %s", blob)
	}
}

func TestPipelinePreviewRefusalsAreTypedAndPlainlyWorded(t *testing.T) {
	for name, c := range map[string]struct {
		base string
		edit appwire.FactoryPipelineEdit
		code string
		op   bool
	}{
		"locked":         {editTestFile, appwire.FactoryPipelineEdit{Op: "set_step", Name: "approve", Prompt: str("x")}, "locked", true},
		"fixed":          {editTestFile, appwire.FactoryPipelineEdit{Op: "set_step", Name: "proof", Prompt: str("x")}, "locked", true},
		"unknown":        {editTestFile, appwire.FactoryPipelineEdit{Op: "set_step", Name: "deploy", Prompt: str("x")}, "unknown_step", true},
		"malformed base": {"pipeline: [", appwire.FactoryPipelineEdit{Op: "set_budget", Value: dol(2)}, "base_invalid", false},
		"cannot clear":   {editTestFile, appwire.FactoryPipelineEdit{Op: "set_step", Name: "plan", Until: str("")}, "not_representable", false},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
			_, err := r.preview(c.base, c.edit)
			we := prWantCode(t, err, appwire.CodeBadParams)
			d, ok := we.Detail.(appwire.FactoryPipelineEditRefusal)
			if !ok || d.Code != c.code || d.Factory != r.id || d.Repo != prRepo || (d.Op != nil) != c.op {
				t.Fatalf("detail %#v", we.Detail)
			}
			if !strings.HasSuffix(we.Message, "Nothing was written.") {
				t.Fatal(we.Message)
			}
			prNoNames(t, err, "deploy", "approve")
		})
	}
}

func TestPipelinePreviewForAReplacedCallerRevealsNothing(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	stub.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.hub.hold(r.id, "tok_other_person_0123456789", 0)
		_ = json.NewEncoder(w).Encode(api.Me{Mode: api.FactoryTeam})
	})
	got, err := r.preview(editTestFile, appwire.FactoryPipelineEdit{Op: "set_budget", Value: dol(9)})
	if err == nil || got.Text != "" {
		t.Fatalf("%+v %v", got, err)
	}
}

// ── the wire ───────────────────────────────────────────────────────────────

func TestPipelineEditOverTheWireRefusesWhatItDoesNotKnow(t *testing.T) {
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	r := newEditWire(t, stub, false, api.FactoryTeam, prToken)
	for _, c := range []struct {
		method string
		params string
	}{
		{"factory.pipeline_source", `{"factory":"` + r.id + `","repo":"` + prRepo + `","ref":"main"}`},
		{"factory.pipeline_source", `{"factory":"` + r.id + `","repo":"` + prRepo + `","path":"README.md"}`},
		{"factory.pipeline_source", `{"factory":"` + r.id + `","repo":"` + prRepo + `","url":"https://evil.example"}`},
		{"factory.pipeline_source", `{"factory":"` + r.id + `","repo":"a/b/c"}`},
		{"factory.pipeline_preview", `{"factory":"` + r.id + `","repo":"` + prRepo + `","base_sha":"` + prBaseSHA + `","base_text":"x: 1","edits":[{"op":"set_budget","value":3,"path":"x"}]}`},
		{"factory.pipeline_preview", `{"factory":"` + r.id + `","repo":"` + prRepo + `","base_sha":"` + prBaseSHA + `","base_text":"x: 1","edits":[{"op":"reorder","name":"write"}]}`},
		{"factory.pipeline_preview", `{"factory":"` + r.id + `","repo":"` + prRepo + `","base_sha":"` + prBaseSHA + `","base_text":"x: 1","edits":[],"token":"t"}`},
	} {
		_, code, _ := r.call(c.method, json.RawMessage(c.params))
		if code != appwire.CodeBadParams {
			t.Errorf("%s %s: %q", c.method, c.params, code)
		}
	}
	if len(stub.requests()) != 0 {
		t.Fatalf("a bad request reached the factory: %v", stub.requests())
	}
}

// ── the real handler, a simulated GitHub: read, preview, open ──────────────

func TestPipelineEditWireAgainstTheRealFactoryHandler(t *testing.T) {
	const tip = "1111111111111111111111111111111111111111"
	const blob = "2222222222222222222222222222222222222222"
	const head = "3333333333333333333333333333333333333333"
	const branch = "codeaf/edit-integration"
	for _, scenario := range []string{"edit_and_open", "missing", "invalid", "no_change"} {
		t.Run(scenario, func(t *testing.T) {
			var mu sync.Mutex
			var calls, writes []string
			var committed string
			file := []byte(editTestFile)
			switch scenario {
			case "missing":
				file = nil
			case "invalid":
				file = []byte("model: gpt\n")
			}
			client := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				if r.URL.Host != "api.github.com" || r.Header.Get("Authorization") != "Bearer simulated-profile-credential" {
					t.Error("GitHub request escaped its credential/origin scope")
				}
				calls = append(calls, r.Method+" "+r.URL.Path)
				status, body := 200, `{}`
				path := r.URL.Path
				switch {
				case r.Method == "GET" && path == "/repos/acme/api":
					body = `{"default_branch":"main","permissions":{"push":true}}`
				case r.Method == "GET" && path == "/repos/acme/api/git/ref/heads/main":
					body = `{"object":{"type":"commit","sha":"` + tip + `"}}`
				case r.Method == "GET" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
					if r.URL.Query().Get("ref") != tip {
						t.Error("file was not pinned to its base")
					}
					if file == nil {
						status = 404
						break
					}
					data, _ := json.Marshal(map[string]any{"type": "file", "encoding": "base64", "sha": blob, "content": base64.StdEncoding.EncodeToString(file)})
					body = string(data)
				case r.Method == "GET" && path == "/repos/acme/api/git/ref/heads/"+branch:
					status = 404
				case r.Method == "POST" && path == "/repos/acme/api/git/refs":
					writes = append(writes, "branch")
					status, body = 201, `{"ref":"refs/heads/`+branch+`","object":{"type":"commit","sha":"`+tip+`"}}`
				case r.Method == "PUT" && path == "/repos/acme/api/contents/"+factory.PipelineFile:
					writes = append(writes, "commit")
					var ask struct{ Content string }
					_ = json.NewDecoder(r.Body).Decode(&ask)
					raw, _ := base64.StdEncoding.DecodeString(ask.Content)
					committed = string(raw)
					status, body = 200, `{"commit":{"sha":"`+head+`"}}`
				case r.Method == "POST" && path == "/repos/acme/api/pulls":
					writes = append(writes, "pull_request")
					status, body = 201, `{"number":42,"html_url":"https://github.com/acme/api/pull/42"}`
				default:
					t.Errorf("unexpected simulated GitHub request: %s %s", r.Method, path)
					status = 500
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			h := serve.New(factory.Seam{}, serve.Config{Mode: api.FactorySolo, AccessToken: testHostToken, OperatorLogin: "alice",
				GitHubToken: func(context.Context) string { return "simulated-profile-credential" }, GitHubHTTPClient: client})
			srv := httptest.NewServer(h)
			t.Cleanup(func() { srv.Close(); h.Close() })
			w := newFactoryWire(t, testFactoryHub(t))
			var conn appwire.FactoryConnection
			w.must("factory.attach", map[string]any{"url": srv.URL, "token": testHostToken}, &conn)

			var src appwire.FactoryPipelineSource
			w.must("factory.pipeline_source", appwire.FactoryPipelineSourceParams{Factory: conn.ID, Repo: "acme/api"}, &src)
			if src.BaseSHA != tip || src.Factory != conn.ID || src.Repo != "acme/api" {
				t.Fatalf("source %+v", src)
			}
			switch scenario {
			case "missing":
				if src.State != "missing" || src.Editable || src.Text != "" {
					t.Fatalf("%+v", src)
				}
				return
			case "invalid":
				if src.State != "invalid" || src.Editable || len(src.Problems) == 0 {
					t.Fatalf("%+v", src)
				}
				// Preview of a file that is not editable is refused, whatever the edit.
				_, code, _ := w.call("factory.pipeline_preview", appwire.FactoryPipelinePreviewParams{Factory: conn.ID, Repo: "acme/api", BaseSHA: src.BaseSHA, BaseText: src.Text,
					Edits: []appwire.FactoryPipelineEdit{{Op: "set_budget", Value: dol(3)}}})
				if code != appwire.CodeBadParams {
					t.Fatalf("code %q", code)
				}
				return
			}
			if src.State != "file" || !src.Editable || src.Text != editTestFile {
				t.Fatalf("%+v", src)
			}
			edit := appwire.FactoryPipelineEdit{Op: "set_step", Name: "write", Max: num(7)}
			if scenario == "no_change" {
				edit.Max = num(2)
			}
			var prev appwire.FactoryPipelinePreview
			w.must("factory.pipeline_preview", appwire.FactoryPipelinePreviewParams{Factory: conn.ID, Repo: "acme/api", BaseSHA: src.BaseSHA, BaseText: src.Text,
				Edits: []appwire.FactoryPipelineEdit{edit}}, &prev)
			mu.Lock()
			if len(writes) != 0 {
				t.Fatalf("reading and previewing wrote to GitHub: %v", writes)
			}
			for _, c := range calls {
				if !strings.HasPrefix(c, "GET ") {
					t.Fatalf("a non-read: %v", calls)
				}
			}
			mu.Unlock()
			if scenario == "no_change" {
				if prev.Changed || prev.Text != src.Text {
					t.Fatalf("%+v", prev)
				}
				return
			}
			if !prev.Changed || len(prev.Steps) != 1 || prev.Steps[0].After.Max != 7 {
				t.Fatalf("%+v", prev)
			}
			var done appwire.FactoryPipelinePRDone
			w.must("factory.pipeline_pr", appwire.FactoryPipelinePRParams{Factory: conn.ID, Repo: "acme/api", Text: prev.Text, Branch: branch, ExpectedBaseSHA: prev.BaseSHA}, &done)
			mu.Lock()
			defer mu.Unlock()
			if done.Outcome != "opened" || done.PR == nil || done.PR.Number != 42 || done.BaseSHA != tip {
				t.Fatalf("%+v", done)
			}
			if committed != prev.Text {
				t.Fatalf("the committed file is not the previewed one:\n%s\n---\n%s", committed, prev.Text)
			}
			if strings.Join(writes, ",") != "branch,commit,pull_request" {
				t.Fatalf("writes %v", writes)
			}
			w.neverSaid("simulated-profile-credential", testHostToken)
		})
	}
}

// ── archived, a 4 MiB budget, and a runner that would drop a step ───────────

// serveRaw answers the source route with exactly these bytes.
func serveRaw(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestPipelineSourceCarriesArchivedBesideCanPushAndNeedsItSaid(t *testing.T) {
	for _, c := range []struct{ push, archived bool }{{true, true}, {true, false}, {false, true}, {false, false}} {
		stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
		v := editSourceFor(prRepo, editTestFile)
		v.CanPush, v.Archived = c.push, c.archived
		stub.source = editSourceReply(v)
		got, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source()
		if err != nil || got.CanPush != c.push || got.Archived != c.archived || !got.Editable {
			t.Fatalf("%+v: %+v %v", c, got, err)
		}
		raw, _ := json.Marshal(got)
		if !strings.Contains(string(raw), fmt.Sprintf(`"archived":%v`, c.archived)) {
			t.Fatalf("archived must be on the app wire, false included: %s", raw)
		}
	}
	whole, _ := json.Marshal(editSourceFor(prRepo, editTestFile))
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(whole, &fields)
	for name, drop := range map[string]func(){
		"absent":    func() { delete(fields, "archived") },
		"null":      func() { fields["archived"] = json.RawMessage("null") },
		"a string":  func() { fields["archived"] = json.RawMessage(`"false"`) },
		"a number":  func() { fields["archived"] = json.RawMessage("0") },
		"an object": func() { fields["archived"] = json.RawMessage("{}") },
	} {
		t.Run(name, func(t *testing.T) {
			whole, _ := json.Marshal(editSourceFor(prRepo, editTestFile))
			fields = map[string]json.RawMessage{}
			_ = json.Unmarshal(whole, &fields)
			drop()
			body, _ := json.Marshal(fields)
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			stub.source = serveRaw(string(body))
			_, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source()
			if e, ok := err.(*appwire.Error); !ok || e.Code != appwire.CodeEngine {
				t.Fatalf("an unsaid archived must not be assumed false: %v", err)
			}
		})
	}
}

func TestPipelineSourceOfTheLargestValidPromptIsBelievedAndARunawayIsNot(t *testing.T) {
	head := "pipeline:\n  issue: [plan]\nsteps:\n  plan:\n    prompt: \""
	file := head + strings.Repeat("<", pipeline.EditTextMax-len(head)-2) + "\"\n"
	stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
	v := editSourceFor(prRepo, file)
	stub.source = editSourceReply(v)
	raw, _ := json.Marshal(v)
	if len(raw) <= 2<<20 || len(raw) > appFactoryPipelineSourceLimit {
		t.Fatalf("fixture is %d bytes; it must beat the old 2 MiB and fit the limit %d", len(raw), appFactoryPipelineSourceLimit)
	}
	got, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source()
	planLen := -1
	for _, st := range got.Steps {
		if st.Name == "plan" {
			planLen = len(st.Prompt)
		}
	}
	if err != nil || !got.Editable || got.Text != file || planLen != pipeline.EditTextMax-len(head)-2 {
		t.Fatalf("%v editable=%v plan prompt %d", err, got.Editable, planLen)
	}
	// Past the budget is refused, never truncated into something that parses.
	stub = newEditStub(t, api.Me{Mode: api.FactoryTeam})
	stub.source = serveRaw(strings.Repeat(" ", appFactoryPipelineSourceLimit+1))
	if _, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source(); err == nil {
		t.Fatal("an answer over the budget was believed")
	}
}

func TestPipelineSourceOfABaseTheRunnerWouldDropIsInvalidAndAFactoryCannotSayOtherwise(t *testing.T) {
	for name, text := range map[string]string{
		"13 letters": "pipeline:\n  issue: [abcdefghijklm]\nsteps:\n  abcdefghijklm: {prompt: p}\n",
		"collision":  "pipeline:\n  issue: [lint-pass, lintpass]\nsteps:\n  lint-pass: {prompt: p}\n  lintpass: {prompt: q}\n",
	} {
		t.Run(name, func(t *testing.T) {
			stub := newEditStub(t, api.Me{Mode: api.FactoryTeam})
			stub.source = editSourceReply(editSourceFor(prRepo, text)) // this engine's Inspect: invalid with problems
			got, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source()
			if err != nil || got.State != api.PipelineEditInvalid || got.Editable || len(got.Problems) != 1 || len(got.Steps) != 0 {
				t.Fatalf("%+v %v", got, err)
			}
			// A factory that still calls it editable is not believed.
			lie := editSourceFor(prRepo, text)
			lie.State, lie.Editable, lie.Problems = api.PipelineEditFile, true, nil
			stub.source = editSourceReply(lie)
			if _, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).source(); err == nil {
				t.Fatal("a factory made an ignored step editable")
			}
			if _, err := newEditWire(t, stub, false, api.FactoryTeam, prToken).preview(text,
				appwire.FactoryPipelineEdit{Op: appwire.PipelineEditSetStep, Name: "lintpass", Max: num(2)}); err == nil {
				t.Fatal("a preview edited a step the runner drops")
			}
		})
	}
}
