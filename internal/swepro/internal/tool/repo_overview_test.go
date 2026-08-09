package tool

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRepoGit struct{}

func (fakeRepoGit) Branch(context.Context, string) string       { return "feature/test" }
func (fakeRepoGit) Head(context.Context, string) (string, bool) { return "deadbeef", true }

func TestBuildRepoOverview(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, root, "src/index.ts", "export {}")
	writeTestFile(t, root, "go.mod", "module example")
	writeTestFile(t, root, "bun.lock", "")
	writeTestFile(t, root, "package.json", `{
		"main":"dist/index.js",
		"bin":{"zed":"z.js","alpha":"a.js"},
		"exports":{"./z":"z","./a":"a"}
	}`)
	depth := 2.0
	result, err := BuildRepoOverview(context.Background(), RepoOverviewParams{
		Path: root, Depth: &depth,
	}, t.TempDir(), nil, fakeRepoGit{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata.PackageManager != "bun" ||
		strings.Join(result.Metadata.Ecosystems, ",") != "Node.js,Go" {
		t.Fatalf("metadata = %+v", result.Metadata)
	}
	if !strings.Contains(result.Output, "Branch: feature/test") ||
		!strings.Contains(result.Output, "- bin: zed") ||
		!strings.Contains(result.Output, "- exports: ./a") {
		t.Fatalf("output = %s", result.Output)
	}
}

func TestBuildRepoOverviewRepositoryResolverAndErrors(t *testing.T) {
	cache := t.TempDir()
	resolver := RepositoryResolverFunc(func(reference string) (string, string, bool) {
		if reference == "owner/repo" {
			return "github.com/owner/repo", cache, true
		}
		return "", "", false
	})
	result, err := BuildRepoOverview(context.Background(), RepoOverviewParams{
		Repository: "owner/repo",
	}, "", resolver, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Metadata.Repository != "github.com/owner/repo" {
		t.Fatalf("repository = %q", result.Metadata.Repository)
	}

	_, err = BuildRepoOverview(context.Background(), RepoOverviewParams{}, "", resolver, nil)
	if err == nil || err.Error() != "Either repository or path is required" {
		t.Fatalf("missing target error = %v", err)
	}
	_, err = BuildRepoOverview(context.Background(), RepoOverviewParams{Repository: "bad"}, "", resolver, nil)
	if err == nil || !strings.HasPrefix(err.Error(), "Repository must be") {
		t.Fatalf("bad repository error = %v", err)
	}
}

func TestBuildRepoStructureIgnoresHeavyDirectories(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{".git", "node_modules", "vendor", "target"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeTestFile(t, root, "visible.txt", "")
	structure := BuildRepoStructure(root, 3)
	if len(structure.Lines) != 1 || structure.Lines[0] != "visible.txt" {
		t.Fatalf("structure = %+v", structure)
	}
}
