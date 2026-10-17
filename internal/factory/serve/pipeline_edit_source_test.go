package serve

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

const editSourceFile = `# the team's pipeline
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

func sourceRequest(h *Handler, token, method, target string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "192.0.2.1:1234"
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	} else {
		r.RemoteAddr = "127.0.0.1:1234"
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const sourcePath = "/v1/repos/acme/api/pipeline/edit-source"

func readSource(t *testing.T, w *httptest.ResponseRecorder) api.PipelineEditSource {
	t.Helper()
	var out api.PipelineEditSource
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	return out
}

func decodeSourceFailure(t *testing.T, w *httptest.ResponseRecorder) api.PipelineEditSourceFailure {
	t.Helper()
	var f api.PipelineEditSourceFailure
	if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil || f.RequestID == "" {
		t.Fatalf("%v: %s", err, w.Body)
	}
	return f
}

func TestPipelineEditSourceIsTheRawFilePinnedAtTheDefaultBranchTip(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte(editSourceFile)
	// A checkout with a different file and a host recipe must never be consulted.
	asked := 0
	seam := factory.Seam{RecipeAt: func(string) (factory.Recipe, []factory.Problem, string, error) {
		asked++
		return factory.DefaultRecipe(), nil, t.TempDir(), nil
	}}
	h := soloPR(t, g, seam)
	h.config.Pipeline = func(context.Context, string, string) (pipeline.File, error) {
		asked++
		return pipeline.Defaults(), nil
	}
	w := sourceRequest(h, "", "GET", sourcePath)
	out := readSource(t, w)
	if out.State != api.PipelineEditFile || !out.Editable || out.Text != editSourceFile || out.Repo != "acme/api" ||
		out.Path != ".codeaf/factory.yaml" || out.BaseSHA != prTip || out.DefaultBranch != "main" || !out.CanPush {
		t.Fatalf("%+v", out)
	}
	if out.DefaultBudget != 4 || !out.BudgetInFile || strings.Join(out.Fixed, ",") != "approve,proof" {
		t.Fatalf("%+v", out)
	}
	if len(out.Pipelines) != 3 || out.Pipelines[0].Kind != "issue" || !out.Pipelines[0].InFile || out.Pipelines[1].InFile || out.Pipelines[2].InFile {
		t.Fatalf("a kind the file does not list is not the file's: %+v", out.Pipelines)
	}
	byName := map[string]api.PipelineEditStep{}
	for _, s := range out.Steps {
		byName[s.Name] = s
	}
	if !byName["approve"].Gate || !byName["approve"].Locked || !byName["proof"].Locked || byName["write"].Locked ||
		byName["write"].Max != 2 || byName["write"].Prompt != "write it" {
		t.Fatalf("%+v", byName)
	}
	want := "GET /repos/acme/api\nGET /repos/acme/api/git/ref/heads/main\nGET /repos/acme/api/contents/.codeaf/factory.yaml"
	if strings.Join(g.calls, "\n") != want {
		t.Fatalf("calls:\n%s", strings.Join(g.calls, "\n"))
	}
	for _, a := range g.auth {
		if a != "Bearer profile-token" {
			t.Fatalf("authorization %q", a)
		}
	}
	if len(g.writes()) != 0 || asked != 0 {
		t.Fatalf("writes %v, host reads %d", g.writes(), asked)
	}
	assertClean(t, w)
}

func TestPipelineEditSourceStatesThatRefuseEditing(t *testing.T) {
	big := "rules:\n  - " + strings.Repeat("a", pipeline.EditTextMax) + "\n"
	for name, c := range map[string]struct {
		file     []byte
		state    string
		problems bool
		text     bool
	}{
		"missing":     {nil, api.PipelineEditMissing, false, false},
		"empty":       {[]byte(" \n"), api.PipelineEditInvalid, true, true},
		"syntax":      {[]byte("pipeline: ["), api.PipelineEditInvalid, true, true},
		"unknown key": {[]byte("model: x\n"), api.PipelineEditInvalid, true, true},
		"not a step":  {[]byte("pipeline:\n  issue: [nothing]\n"), api.PipelineEditInvalid, true, true},
		"too large":   {[]byte(big), api.PipelineEditTooLarge, false, false},
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file = c.file
			out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath))
			if out.State != c.state || out.Editable || (len(out.Problems) > 0) != c.problems || (out.Text != "") != c.text {
				t.Fatalf("%+v", out)
			}
			if out.Steps != nil || out.Pipelines != nil || out.DefaultBudget != 0 || out.BaseSHA != prTip {
				t.Fatalf("nothing is parsed or defaulted for a file that cannot be edited: %+v", out)
			}
			if len(g.writes()) != 0 {
				t.Fatal(g.writes())
			}
		})
	}
}

func TestPipelineEditSourceReadsWithoutPushAccess(t *testing.T) {
	g := newPRGitHub(t)
	g.file, g.push = []byte(editSourceFile), false
	out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath))
	if out.CanPush || !out.Editable {
		t.Fatalf("%+v", out)
	}
}

func TestPipelineEditSourceUsesTheCallersOwnGitHubCredentialAndNeverTheHosts(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte(editSourceFile)
	signIn := &fakeGitHub{t: t}
	f := peopleV2Setup(t, api.FactoryTeam)
	hostReads := 0
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubToken = func(context.Context) string { hostReads++; return "host-read-grant" }
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}

	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	w := sourceRequest(f.h, priya, "GET", sourcePath)
	if out := readSource(t, w); !out.Editable {
		t.Fatalf("%+v", out)
	}
	for _, a := range g.auth {
		if a != "Bearer github-access-secret" {
			t.Fatalf("a team read must carry the member's own token, got %q", a)
		}
	}
	if hostReads != 0 {
		t.Fatal("a team must never read the host's grant")
	}

	g.calls, g.auth = nil, nil
	w = sourceRequest(f.h, "operator-secret", "GET", sourcePath)
	failure := decodeSourceFailure(t, w)
	if w.Code != 401 || failure.Stage != api.PipelineEditStageCredential || len(g.calls) != 0 || hostReads != 0 {
		t.Fatalf("floor: %d %s calls=%v host=%d", w.Code, w.Body, g.calls, hostReads)
	}
	assertClean(t, w, "host-read-grant")

	// No bearer at all is the shared authentication gate's, before any GitHub call.
	g.calls = nil
	if w = sourceRequest(f.h, "no-such-token", "GET", sourcePath); w.Code != 401 || len(g.calls) != 0 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
}

func TestPipelineEditSourceHiddenRepositoryStaysHidden(t *testing.T) {
	g := newPRGitHub(t)
	signIn := &fakeGitHub{t: t}
	f := peopleV2Setup(t, api.FactoryTeam)
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}
	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	signIn.repoStatus = 404
	if w := sourceRequest(f.h, priya, "GET", sourcePath); w.Code != 404 || len(g.calls) != 0 {
		t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
	}
}

func TestPipelineEditSourceTakesNoParameters(t *testing.T) {
	for _, q := range []string{"?ref=main", "?path=x", "?url=https://evil.example", "?"} {
		g := newPRGitHub(t)
		g.file = []byte(editSourceFile)
		w := sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath+q)
		if q == "?" {
			// An empty query string is no parameter.
			readSource(t, w)
			continue
		}
		if w.Code != 422 || len(g.calls) != 0 {
			t.Fatalf("%s: %d %s %v", q, w.Code, w.Body, g.calls)
		}
	}
	g := newPRGitHub(t)
	for _, method := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if w := sourceRequest(soloPR(t, g, factory.Seam{}), "", method, sourcePath); w.Code != 405 || len(g.calls) != 0 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
	}
}

func TestPipelineEditSourceGitHubFailuresAreHonestAndReadOnly(t *testing.T) {
	for name, c := range map[string]struct {
		stage  string
		status int
		drop   bool
		body   string
		code   int
		at     string
	}{
		"repo 404":        {stage: "repo", status: 404, code: 404, at: "repository"},
		"repo 401":        {stage: "repo", status: 401, code: 401, at: "repository"},
		"repo 500":        {stage: "repo", status: 500, code: 502, at: "repository"},
		"repo dropped":    {stage: "repo", drop: true, code: 502, at: "repository"},
		"repo redirect":   {stage: "repo", status: 302, code: 502, at: "repository"},
		"repo garbage":    {stage: "repo", body: `<html>`, code: 502, at: "repository"},
		"no default":      {stage: "repo", body: `{"default_branch":"","permissions":{"push":true}}`, code: 502, at: "repository"},
		"tip 403":         {stage: "tip", status: 403, code: 403, at: "base"},
		"tip dropped":     {stage: "tip", drop: true, code: 502, at: "base"},
		"tip not commit":  {stage: "tip", body: `{"object":{"type":"tag","sha":"` + prTip + `"}}`, code: 502, at: "base"},
		"tip bad sha":     {stage: "tip", body: `{"object":{"type":"commit","sha":"abc"}}`, code: 502, at: "base"},
		"file 500":        {stage: "file", status: 500, code: 502, at: "file"},
		"file dropped":    {stage: "file", drop: true, code: 502, at: "file"},
		"file redirect":   {stage: "file", status: 301, code: 502, at: "file"},
		"file a folder":   {stage: "file", body: `[{"type":"file"}]`, code: 502, at: "file"},
		"file wrong kind": {stage: "file", body: `{"type":"symlink","encoding":"base64","sha":"` + prBlob + `","content":""}`, code: 502, at: "file"},
		"file not base64": {stage: "file", body: `{"type":"file","encoding":"base64","sha":"` + prBlob + `","content":"@@@"}`, code: 502, at: "file"},
		"file no sha":     {stage: "file", body: `{"type":"file","encoding":"base64","sha":"x","content":""}`, code: 502, at: "file"},
		"file encoding":   {stage: "file", body: `{"type":"file","encoding":"none","sha":"` + prBlob + `","content":""}`, code: 502, at: "file"},
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file = []byte(editSourceFile)
			if c.status != 0 {
				g.status[c.stage] = c.status
			}
			if c.drop {
				g.drop[c.stage] = true
			}
			if c.body != "" {
				g.body[c.stage] = c.body
			}
			w := sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath)
			f := decodeSourceFailure(t, w)
			if w.Code != c.code || f.Stage != c.at {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if len(g.writes()) != 0 {
				t.Fatal(g.writes())
			}
			g.noDeleteEver()
			assertClean(t, w)
			if strings.Contains(w.Body.String(), "evil.example") || strings.Contains(w.Body.String(), "connection reset") {
				t.Fatalf("leak: %s", w.Body)
			}
		})
	}
}

func TestPipelineEditSourceGitHub401EndsTheSessionsGitHubAccess(t *testing.T) {
	g := newPRGitHub(t)
	g.status["repo"] = 401
	signIn := &fakeGitHub{t: t}
	f := peopleV2Setup(t, api.FactoryTeam)
	f.h.config.GitHubClientID = "client"
	f.h.config.GitHubHTTPClient = &http.Client{Transport: g.transport(signIn.client().Transport)}
	priya := signInHandle(t, f.h, signIn, &f.now, "priya", "operator-secret")
	if w := sourceRequest(f.h, priya, "GET", sourcePath); w.Code != 401 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	g.calls = nil
	delete(g.status, "repo")
	w := sourceRequest(f.h, priya, "GET", sourcePath)
	if w.Code != 401 || len(g.calls) != 0 {
		t.Fatalf("the rejected credential must not be offered to GitHub again: %d %s %v", w.Code, w.Body, g.calls)
	}
}

// TestPipelineEditSourceFeedsThePullRequestRouteEndToEnd is the whole read-edit-
// write loop through the real handler with a scripted GitHub: what the source
// route returns is what the preview edits, and the bytes the pull request
// commits are the bytes the preview showed, on the base commit the source named.
func TestPipelineEditSourceFeedsThePullRequestRouteEndToEnd(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte(editSourceFile)
	h := soloPR(t, g, factory.Seam{})
	src := readSource(t, sourceRequest(h, "", "GET", sourcePath))
	three := 3
	edited, err := pipeline.ApplyEdits(src.Text, []pipeline.EditOp{{Op: pipeline.OpSetStep, Name: "write", Max: &three}})
	if err != nil || !edited.Changed {
		t.Fatalf("%v %+v", err, edited)
	}
	g.calls, g.auth = nil, nil
	body, _ := json.Marshal(api.PipelinePRRequest{Text: edited.Text, Title: "Give write three rounds", ExpectedBaseSHA: src.BaseSHA})
	r := httptest.NewRequest("POST", "/v1/repos/acme/api/pipeline/open-pr", strings.NewReader(string(body)))
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 201 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(g.puts) != 1 || g.puts[0]["content"] != base64.StdEncoding.EncodeToString([]byte(edited.Text)) || g.puts[0]["sha"] != prBlob {
		t.Fatalf("the committed bytes are not the previewed bytes: %+v", g.puts)
	}

	// A preview that changes nothing is not something the route would open either.
	same, err := pipeline.ApplyEdits(src.Text, []pipeline.EditOp{{Op: pipeline.OpSetStep, Name: "write", Max: ptr(2)}})
	if err != nil || same.Changed || same.Text != src.Text {
		t.Fatalf("%v %+v", err, same)
	}
}

func ptr[T any](v T) *T { return &v }

// The read and the pull request route cannot drift: a file the source calls
// editable must be one the route can carry.
func TestPipelineEditSourceBoundsAreThePullRequestRoutes(t *testing.T) {
	if pipeline.EditTextMax != pipelinePRMaxText {
		t.Fatalf("source cap %d, pull request cap %d", pipeline.EditTextMax, pipelinePRMaxText)
	}
}

// An archived repository can say push true and still refuse a pull request, so
// the source states both facts and never folds one into the other.
func TestPipelineEditSourceStatesArchivedBesideCanPushAndWritesNothing(t *testing.T) {
	for name, c := range map[string]struct{ push, archived bool }{
		"archived and push": {true, true}, "plain": {true, false}, "no push": {false, false}, "archived no push": {false, true},
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file, g.push, g.archived = []byte(editSourceFile), c.push, c.archived
			h := soloPR(t, g, factory.Seam{})
			w := sourceRequest(h, "", "GET", sourcePath)
			out := readSource(t, w)
			if out.CanPush != c.push || out.Archived != c.archived || !out.Editable || out.State != api.PipelineEditFile {
				t.Fatalf("%+v", out)
			}
			// The field is on the wire even when false: absent must mean "not said".
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(w.Body.Bytes(), &raw)
			if string(raw["archived"]) != fmt.Sprint(c.archived) {
				t.Fatalf("archived on the wire: %q", raw["archived"])
			}
			if len(g.writes()) != 0 {
				t.Fatal(g.writes())
			}
			if c.archived {
				// The very repository the source called informative is refused by the PR route.
				g.calls = nil
				body, _ := json.Marshal(api.PipelinePRRequest{Text: out.Text, Title: "x", ExpectedBaseSHA: out.BaseSHA})
				r := httptest.NewRequest("POST", "/v1/repos/acme/api/pipeline/open-pr", strings.NewReader(string(body)))
				r.RemoteAddr = "127.0.0.1:1234"
				pw := httptest.NewRecorder()
				h.ServeHTTP(pw, r)
				if pw.Code != 403 || len(g.writes()) != 0 {
					t.Fatalf("%d %s %v", pw.Code, pw.Body, g.writes())
				}
			}
		})
	}
}

// The largest valid file is 256 KiB of '<'. encoding/json writes each as <
// (six bytes), so the raw text and the parsed prompt are about 1.5 MiB apiece.
func TestPipelineEditSourceOfTheLargestValidPromptFitsTheEnginesBudgetButAnOversizeFileStillRefuses(t *testing.T) {
	head := "pipeline:\n  issue: [plan]\nsteps:\n  plan:\n    prompt: \""
	tail := "\"\n"
	file := head + strings.Repeat("<", pipeline.EditTextMax-len(head)-len(tail)) + tail
	if len(file) != pipeline.EditTextMax {
		t.Fatalf("fixture is %d", len(file))
	}
	g := newPRGitHub(t)
	g.file = []byte(file)
	w := sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath)
	out := readSource(t, w)
	if out.State != api.PipelineEditFile || !out.Editable {
		t.Fatalf("%s %v", out.State, out.Problems)
	}
	if w.Body.Len() <= 2<<20 || w.Body.Len() > 4<<20 {
		t.Fatalf("the worst valid answer is %d bytes; the engine budget is 4 MiB and this fixture must exceed the old 2 MiB", w.Body.Len())
	}
	g = newPRGitHub(t)
	g.file = []byte(file + "#")
	if out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath)); out.State != api.PipelineEditTooLarge || out.Text != "" {
		t.Fatalf("%s", out.State)
	}
}
