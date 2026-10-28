package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func TestFactoryRecipeAtAndRunnerReadYAMLFirst(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte("pipeline:\n  issue: [test]\nsteps:\n  test: YAML prompt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, factory.RecipeFile), []byte("## issue\n1. plan · chat · Markdown prompt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dirs := func(string) string { return dir }
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(dirs), factoryPipelineOption(st))
	r, problems, _, err := seam.RecipeAt("owner/repo")
	if err != nil || len(problems) != 0 || len(r.Stages) != 1 || r.Stages[0].Ask != "YAML prompt" {
		t.Fatalf("recipe %+v, problems %+v, err %v", r, problems, err)
	}
	r = factoryItemRecipe(nil, dirs, factory.Item{Repo: "owner/repo"})
	if len(r.Stages) != 1 || r.Stages[0].Ask != "YAML prompt" {
		t.Fatalf("runner: %+v", r)
	}
}

func TestFactoryConvertPrintsYAMLAndWarnsAboutLostKnobs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "factory.md")
	if err := os.WriteFile(path, []byte("## issue\n1. plan · chat · read the files · fanout 3\n2. approve · gate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, warnings bytes.Buffer
	if err := runFactoryConvert([]string{path}, &out, &warnings); err != nil {
		t.Fatal(err)
	}
	f, err := pipeline.Parse(out.String())
	if err != nil || f.Steps["plan"].Prompt != "read the files" || len(f.Start.Labels) != 0 {
		t.Fatalf("converted %s, err %v", out.String(), err)
	}
	if !strings.Contains(warnings.String(), "fanout") {
		t.Fatalf("warnings: %s", warnings.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "factory.yaml")); !os.IsNotExist(err) {
		t.Fatal("convert wrote a file")
	}
}

// This drives the host's intake and HTTP doors with no checkout or model.
func TestFactoryRulesFromForgeReachIntakeAndPipeline(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	// The intake's refresh also reads CODEOWNERS; sealed, it never reaches api.github.com
	// (whose retry backoff held this test for minutes).
	sealFactoryPeople(t)
	text := "default_budget: 1\nstart:\n  labels: [agent-ready]\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/repo/contents/"+pipeline.RepoFile {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("ref") != "" {
			t.Error("rules supplied a branch instead of reading the default")
		}
		json.NewEncoder(w).Encode(map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text)), "sha": fmt.Sprintf("blob-%s", text)})
	}))
	defer srv.Close()
	loader := factorygithub.NewPipelineLoader(func(context.Context) factorygithub.PipelineAPI { return forge.NewClient("stub").WithBaseURL(srv.URL) })
	factoryRuleCaches.Lock()
	factoryRuleCaches.profiles[factoryProfile("")] = loader
	factoryRuleCaches.Unlock()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetRepos([]string{"owner/repo"}); err != nil {
		t.Fatal(err)
	}
	it, err := st.Create(factory.Item{Repo: "owner/repo", Kind: factory.KindIssue, Tier: factory.TierOwner,
		Title: "Fix documentation", State: factory.StateNew, Labels: []string{"agent-ready", "size:S"},
		Triage: factory.Triage{Size: "S", Est: 1, TriagedAt: time.Unix(100, 0)}})
	if err != nil {
		t.Fatal(err)
	}
	in := factoryIntake(st)
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	items, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != it.ID || items[0].State != factory.StateQueued {
		t.Fatalf("intake did not queue checkout-free job: %+v", items)
	}
	seam := factory.LocalSeam(st, time.Now(), factoryPipelineOption(st), factory.WithRepoDirs(st.CheckoutDir))
	handler := serve.New(seam, serve.Config{})
	read := func() struct {
		Source   string
		Problems []string
	} {
		t.Helper()
		req := httptest.NewRequest("GET", "/v1/repos/owner/repo/pipeline", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		var payload struct {
			Source   string
			Problems []string
		}
		if out.Code != 200 {
			t.Fatalf("pipeline HTTP %d: %s", out.Code, out.Body.String())
		}
		if err := json.Unmarshal(out.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return payload
	}
	if got := read(); got.Source != "file" || len(got.Problems) != 0 {
		t.Fatalf("pipeline %+v", got)
	}
	text = "start: ["
	if err := in.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := read(); got.Source != "file" || len(got.Problems) == 0 || !strings.Contains(got.Problems[0], pipeline.RepoFile) {
		t.Fatalf("parse error disappeared: %+v", got)
	}
	_, problems, dir, err := seam.RecipeAt("owner/repo")
	if err == nil || len(problems) == 0 || dir != "" {
		t.Fatalf("recipe page read: %+v, %q, %v", problems, dir, err)
	}
}

// ONE RESOLVER BOUNDS A JOB ([factoryJobFile]): the factory's copy the job
// carries, else the factory's read of its repository now, the store's copy
// before main's file. The manager's brief before a job starts reads the
// store's copy; a job carrying a copy that fixes docs refuses the manager's
// removal of docs through the manager's door and the seam's, while a job
// carrying none is bounded by the store's copy, which does not fix it.
func TestEveryDoorBoundsAJobByTheCopyItCarries(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte("pipeline:\n  issue: [write, docs, proof]\nsteps:\n  docs: say it\nrules:\n  - main's rule\n"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)
	if _, _, err := st.ChangePipelineCopy("owner/repo", func(*factory.PipelineCopy, int) (*factory.PipelineCopy, bool, error) {
		return &factory.PipelineCopy{Repo: "owner/repo", Text: "pipeline:\n  issue: [write, docs, proof]\nsteps:\n  docs: say it\nrules:\n  - the factory copy's rule\n", By: "test-owner", At: at}, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	dirs := func(string) string { return dir }
	stages := []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "write it", On: true},
		{Name: "docs", Kind: factory.StageChat, Ask: "say it", On: true},
		{Name: "proof", Kind: factory.StageChat, Ask: "prove it", Until: factory.UntilProven, On: true},
	}
	plain, err := st.Create(factory.Item{Title: "plain", Repo: "owner/repo", Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(stages)})
	if err != nil {
		t.Fatal(err)
	}
	if brief := talkBrief(plain, factoryItemRecipe(st, dirs, plain)); !strings.Contains(brief, "the factory copy's rule") || strings.Contains(brief, "main's rule") {
		t.Fatalf("the brief before the start is not the factory's copy:\n%s", brief)
	}
	carried, err := st.Create(factory.Item{Title: "carried", Repo: "owner/repo", Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(stages),
		PipelineCopy: &factory.PipelineCopy{Repo: "owner/repo", Text: "pipeline:\n  issue: [write, docs, proof]\nsteps:\n  docs: say it\nfixed: [docs]\nrules:\n  - the carried rule\n", By: "test-owner", At: at.Add(-time.Hour)}})
	if err != nil {
		t.Fatal(err)
	}
	if r := factoryItemRecipe(st, dirs, carried); strings.Join(r.Policy, "|") != "the carried rule" {
		t.Fatalf("the carried job's recipe says %q", r.Policy)
	}
	if r := factoryItemRecipe(nil, dirs, carried); strings.Join(r.Policy, "|") != "the carried rule" {
		t.Fatalf("a runner's recipe for the carried job says %q", r.Policy)
	}
	remove := factory.RunEdit{By: factory.ByManager, Remove: []string{"docs"}}
	seam := factory.LocalSeam(st, time.Now(), factoryPipelineOption(st), factory.WithRepoDirs(dirs))
	for name, edit := range map[string]func(context.Context, int, factory.RunEdit) (factory.Item, []string, error){"manager": managerEdit(st, dirs), "seam": seam.Edit} {
		if _, _, err := edit(context.Background(), carried.ID, remove); err == nil || !strings.Contains(err.Error(), "docs is fixed") {
			t.Fatalf("%s: the carried copy's fixed step was removed (%v)", name, err)
		}
	}
	if _, _, err := managerEdit(st, dirs)(context.Background(), plain.ID, remove); err != nil {
		t.Fatalf("the plain job's removal was refused: %v", err)
	}

	// A JOB DECIDED ON MAIN'S FILE is bounded by main's file alone, though the
	// factory keeps a copy now: the doors (which read the store) and the runner
	// (which reads none) agree.
	onMain, err := st.Create(factory.Item{Title: "on main", Repo: "owner/repo", Kind: factory.KindIssue, State: factory.StateNew, Stages: factory.CopyStages(stages), PipelineDecided: true})
	if err != nil {
		t.Fatal(err)
	}
	for name, keep := range map[string]*store.Store{"a door": st, "the runner": nil} {
		if r := factoryItemRecipe(keep, dirs, onMain); strings.Join(r.Policy, "|") != "main's rule" {
			t.Fatalf("%s bounds the job decided on main by %q", name, r.Policy)
		}
	}
	// A JOB HANDED OVER BY ANOTHER FACTORY (a worker's runner, a remote job) is
	// briefed with no store: never this machine's copy.
	if brief := talkBrief(plain, factoryItemRecipe(nil, dirs, plain)); !strings.Contains(brief, "main's rule") || strings.Contains(brief, "the factory copy's rule") {
		t.Fatalf("a handed-over job's brief reads this machine's copy:\n%s", brief)
	}
}

// A RUNNER'S READ NEVER TOUCHES THIS MACHINE'S COPY: with no store, a read of
// a job whose main's file now says what this machine's copy says leaves the
// copy standing, where the factory's own read would clear it.
func TestARunnersReadNeverClearsThisMachinesCopy(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	dir := t.TempDir()
	text := "pipeline:\n  issue: [write, proof]\nrules:\n  - the merged rule\n"
	if err := os.MkdirAll(filepath.Join(dir, ".codeaf"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, pipeline.RepoFile), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ChangePipelineCopy("owner/repo", func(*factory.PipelineCopy, int) (*factory.PipelineCopy, bool, error) {
		return &factory.PipelineCopy{Repo: "owner/repo", Text: text, By: "test-owner", At: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	dirs := func(string) string { return dir }
	factoryItemRecipe(nil, dirs, factory.Item{Repo: "owner/repo"})
	if c, _, err := st.PipelineCopy("owner/repo"); err != nil || c == nil {
		t.Fatalf("a runner's read cleared this machine's copy: %+v (%v)", c, err)
	}
}
