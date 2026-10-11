package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
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
	seam := factory.LocalSeam(st, time.Now(), factory.WithRepoDirs(dirs), factoryPipelineOption())
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
	if err := os.WriteFile(path, []byte("## issue\n1. plan · chat · read the files · fanout per-file\n2. approve · gate\n"), 0600); err != nil {
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
