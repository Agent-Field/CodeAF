package pipeline_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

func TestLoadReadsTheWorkingTreeWhenThereIsNoBranch(t *testing.T) {
	dir := t.TempDir()
	if _, err := pipeline.Load(""); err == nil {
		t.Fatal("an empty path loaded")
	}
	f, err := pipeline.Load(dir)
	if err != nil || f.Source != pipeline.SourceDefaults || f.From != factory.FromDefault {
		t.Fatalf("empty = %s %s %v", f.Source, f.From, err)
	}
	writeFile(t, dir, pipeline.RepoFile, "start:\n  labels: [agent-ready]\n  max_cost: 1\n")
	writeFile(t, dir, factory.RecipeFile, "## issue\n1. plan · chat · from the markdown\n")
	f, err = pipeline.Load(dir)
	if err != nil || f.Source != pipeline.SourceFile || f.From != factory.FromWorkingTree {
		t.Fatalf("yaml wins = %s %s %v", f.Source, f.From, err)
	}
	if strings.Join(f.Start.Labels, ",") != "agent-ready" || f.Start.MaxCost != 1 {
		t.Fatalf("file = %+v", f.Start)
	}
	if !strings.Contains(f.Text, "agent-ready") {
		t.Fatal("the repository text was not kept")
	}
	if err := os.Remove(filepath.Join(dir, pipeline.RepoFile)); err != nil {
		t.Fatal(err)
	}
	f, err = pipeline.Load(dir)
	if err != nil || f.Source != pipeline.SourceMarkdown || f.From != factory.FromWorkingTree {
		t.Fatalf("markdown = %s %s %v", f.Source, f.From, err)
	}
	if f.Steps["plan"].Prompt != "from the markdown" {
		t.Fatalf("plan = %+v", f.Steps["plan"])
	}
}

func TestLoadReadsTheMainBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	git(t, dir, "config", "commit.gpgsign", "false")
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "start")
	writeFile(t, dir, pipeline.RepoFile, "start:\n  labels: [not-yet]\n")
	f, err := pipeline.Load(dir)
	if err != nil || f.Source != pipeline.SourceDefaults || f.From != factory.FromDefault {
		t.Fatalf("uncommitted = %s %s %v", f.Source, f.From, err)
	}
	git(t, dir, "add", pipeline.RepoFile)
	git(t, dir, "commit", "-q", "-m", "law")
	git(t, dir, "checkout", "-q", "-b", "pr")
	writeFile(t, dir, pipeline.RepoFile, "start:\n  labels: [from-the-pr]\n")
	git(t, dir, "commit", "-q", "-am", "a pull request lifts the law")
	f, err = pipeline.Load(dir)
	if err != nil || f.From != factory.FromMainBranch || f.Source != pipeline.SourceFile {
		t.Fatalf("branch = %s %s %v", f.Source, f.From, err)
	}
	if strings.Join(f.Start.Labels, ",") != "not-yet" {
		t.Fatalf("labels = %v", f.Start.Labels)
	}
	git(t, dir, "checkout", "-q", "main")
	writeFile(t, dir, pipeline.RepoFile, "start:\n  labels: [edited]\n")
	f, err = pipeline.Load(dir)
	if err != nil || strings.Join(f.Start.Labels, ",") != "not-yet" {
		t.Fatalf("uncommitted edit = %v %v", f.Start.Labels, err)
	}
}

func TestLoadMergedTightensWithThePersonalFile(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	writeFile(t, dir, pipeline.RepoFile, "start:\n  labels: [agent-ready]\n  max_cost: 2\n")
	path := pipeline.PersonalPath(home)
	writeFile(t, home, ".codeaf/factory.yaml", "start:\n  max_cost: 1\n")
	if path != filepath.Join(home, ".codeaf", "factory.yaml") {
		t.Fatalf("personal = %s", path)
	}
	f, err := pipeline.LoadMerged(dir, path)
	if err != nil {
		t.Fatal(err)
	}
	if f.Start.MaxCost != 1 || strings.Join(f.Start.Labels, ",") != "agent-ready" {
		t.Fatalf("merged = %+v", f.Start)
	}
	if !f.Personal || !strings.Contains(f.PersonalText, "max_cost: 1") || !strings.Contains(f.Text, "max_cost: 2") {
		t.Fatalf("personal text = %q repo text = %q personal %v", f.PersonalText, f.Text, f.Personal)
	}
	f, err = pipeline.LoadMerged(dir, filepath.Join(home, "missing.yaml"))
	if err != nil || f.Start.MaxCost != 2 || f.Personal {
		t.Fatalf("missing personal = %+v %v", f.Start, err)
	}
}

func writeFile(t *testing.T, dir, rel, text string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		"GIT_CONFIG_NOSYSTEM=1")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestConvertedMarkdownKeepsItsSectionConstraint(t *testing.T) {
	f := pipeline.FromMarkdown("## issue · fixed\n1. plan · chat · read the files\n")
	if mode := f.Recipe().Adapt[factory.KindIssue]; mode != factory.AdaptFixed {
		t.Fatalf("section mode: %q", mode)
	}
	data, warnings, err := pipeline.Convert("## issue · fixed\n1. plan · chat · read the files\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || !strings.Contains(joinProblems(warnings), "YAML fixes individual steps") {
		t.Fatalf("conversion warning: %+v", warnings)
	}
}
