package leafbriefing

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/cochange"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/importgraph"
)

type repoMapStub struct {
	mu       sync.Mutex
	calls    int
	lastOpts RepoMapOptions
	result   string
	panic    bool
}

func (s *repoMapStub) BuildRepoMap(_ []importgraph.SourceFile, opts RepoMapOptions) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastOpts = opts
	if s.panic {
		panic("repo map failed")
	}
	return s.result
}

type graphLoaderStub struct {
	mu         sync.Mutex
	head       string
	buildCalls int
	graphs     WorkspaceGraphs
	err        error
}

func (s *graphLoaderStub) HeadSHA(string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.head
}

func (s *graphLoaderStub) BuildWorkspaceGraphs(string) (WorkspaceGraphs, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buildCalls++
	return s.graphs, s.err
}

func sampleGraphs() WorkspaceGraphs {
	files := []importgraph.SourceFile{
		{Path: "src/a.ts", Content: `import "./b"`},
		{Path: "src/b.ts", Content: "export const b = 1"},
		{Path: "src/c.ts", Content: "export const c = 1"},
	}
	return WorkspaceGraphs{
		RepoMapFiles: files,
		CoChange: cochange.BuildCoChangeGraph([]cochange.CoChangeCommit{
			{Hash: "abcdef1", Files: []string{"src/a.ts", "src/c.ts"}},
		}, cochange.BuildCoChangeOptions{}),
		Imports: importgraph.BuildImportGraph(files, nil),
	}
}

func TestBuilderCachesGraphsAndRendersSections(t *testing.T) {
	repo := &repoMapStub{result: "repo map"}
	loader := &graphLoaderStub{head: "abc", graphs: sampleGraphs()}
	builder := NewBuilder(repo, loader)
	args := BuildLeafBriefingSectionsArgs{
		Workspace:  "/repo",
		RunKey:     "run",
		FocusPaths: []string{"src/a.ts"},
	}

	first := builder.BuildLeafBriefingSections(args)
	second := builder.BuildLeafBriefingSections(args)
	if first == nil || second == nil {
		t.Fatal("expected sections")
	}
	if loader.buildCalls != 1 {
		t.Fatalf("graph loads = %d, want 1", loader.buildCalls)
	}
	if first.RepoMap != "repo map" {
		t.Fatalf("repo map = %q", first.RepoMap)
	}
	if !strings.Contains(first.CoChange, "src/c.ts (1.000)") {
		t.Fatalf("co-change section = %q", first.CoChange)
	}
	if first.Imports != "src/a.ts:\n- src/b.ts" {
		t.Fatalf("imports = %q", first.Imports)
	}
	if first.Exemplars == "" {
		t.Fatal("empty exemplars")
	}
	if repo.lastOpts.BudgetChars < 300 {
		t.Fatalf("repo budget = %v, want >= 300", repo.lastOpts.BudgetChars)
	}
}

func TestBuilderCacheKeyIncludesHeadAndRunKey(t *testing.T) {
	repo := &repoMapStub{}
	loader := &graphLoaderStub{head: "one", graphs: sampleGraphs()}
	builder := NewBuilder(repo, loader)
	args := BuildLeafBriefingSectionsArgs{Workspace: "/repo", RunKey: "a"}
	if builder.BuildLeafBriefingSections(args) == nil {
		t.Fatal("first build failed")
	}
	args.RunKey = "b"
	if builder.BuildLeafBriefingSections(args) == nil {
		t.Fatal("second build failed")
	}
	loader.mu.Lock()
	loader.head = "two"
	loader.mu.Unlock()
	if builder.BuildLeafBriefingSections(args) == nil {
		t.Fatal("third build failed")
	}
	if loader.buildCalls != 3 {
		t.Fatalf("graph loads = %d, want 3", loader.buildCalls)
	}
}

func TestBuilderConvertsFailuresToUndefined(t *testing.T) {
	repo := &repoMapStub{}
	loader := &graphLoaderStub{head: "abc", err: errors.New("boom")}
	if got := NewBuilder(repo, loader).BuildLeafBriefingSections(BuildLeafBriefingSectionsArgs{}); got != nil {
		t.Fatalf("loader error returned %#v", got)
	}

	repo.panic = true
	loader = &graphLoaderStub{head: "def", graphs: sampleGraphs()}
	if got := NewBuilder(repo, loader).BuildLeafBriefingSections(BuildLeafBriefingSectionsArgs{}); got != nil {
		t.Fatalf("repo-map panic returned %#v", got)
	}
}

func TestDefaultWorkspaceGraphLoader(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	workspace := t.TempDir()
	runTestGit(t, workspace, "init", "-q")
	runTestGit(t, workspace, "config", "user.name", "Parity Test")
	runTestGit(t, workspace, "config", "user.email", "parity@example.invalid")
	writeTestFile(t, workspace, "src/a.ts", `import "./b"`+"\n")
	writeTestFile(t, workspace, "src/b.ts", "export const b = 1\n")
	runTestGit(t, workspace, "add", ".")
	runTestGit(t, workspace, "commit", "-qm", "initial")
	writeTestFile(t, workspace, "src/c.ts", "export const c = 1\n")

	repo := &repoMapStub{result: ""}
	builder := NewBuilder(repo, nil)
	sections := builder.BuildLeafBriefingSections(BuildLeafBriefingSectionsArgs{
		Workspace:  workspace,
		RunKey:     "run",
		FocusPaths: []string{"src/a.ts"},
	})
	if sections == nil {
		t.Fatal("default loader returned nil")
	}
	if sections.RepoMap != "(no symbol map entries for the focused paths)" {
		t.Fatalf("repo-map fallback = %q", sections.RepoMap)
	}
	if sections.Imports != "src/a.ts:\n- src/b.ts" {
		t.Fatalf("imports = %q", sections.Imports)
	}
	if repo.calls != 1 {
		t.Fatalf("repo-map calls = %d, want 1", repo.calls)
	}
}

func TestDefaultBuilderRendersPortedRepoMap(t *testing.T) {
	workspace := t.TempDir()
	runTestGit(t, workspace, "init", "-q")
	runTestGit(t, workspace, "config", "user.name", "Parity Test")
	runTestGit(t, workspace, "config", "user.email", "parity@example.invalid")
	writeTestFile(t, workspace, "src/marker.go", "package marker\n\nfunc RepositoryMarker() {}\n")
	runTestGit(t, workspace, "add", ".")
	runTestGit(t, workspace, "commit", "-qm", "initial")

	sections := NewDefaultBuilder().BuildLeafBriefingSections(BuildLeafBriefingSectionsArgs{
		Workspace: workspace, RunKey: "run", FocusPaths: []string{"src/marker.go"},
	})
	if sections == nil || !strings.Contains(sections.RepoMap, "function RepositoryMarker (line 3)") {
		t.Fatalf("default repo-map sections = %#v", sections)
	}
}

func runTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
