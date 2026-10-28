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
	r = factoryItemRecipe(dirs, factory.Item{Repo: "owner/repo"})
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
