package tia

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Agent-Field/swe-pro-go/internal/session/importgraph"
)

// Translation of src/session/tia.test.ts. Subtest names are verbatim; the
// describe() blocks become the outer t.Run names.
//
// The TS helper builds its graph from a `Record<string, string[]>`, whose key
// iteration order is JS object insertion order — reproduced here with an
// ordered slice rather than a Go map, which would randomize it. (None of the
// keys are integer-like, so the "integer keys hoist to the front" rule never
// fires.)

type edgeSpec struct {
	path string
	deps []string
}

var lastExtRE = regexp.MustCompile(`\.[^.]+$`)

// graphFor is the test file's graph() helper.
func graphFor(edges []edgeSpec) importgraph.ImportGraph {
	files := newOrderedSet()
	for _, e := range edges {
		files.add(e.path)
	}
	for _, e := range edges {
		for _, dep := range e.deps {
			files.add(dep)
		}
	}
	byPath := map[string][]string{}
	for _, e := range edges {
		byPath[e.path] = e.deps
	}
	contents := []importgraph.SourceFile{}
	for _, path := range files.values() {
		deps := byPath[path]
		lines := []string{}
		for _, dep := range deps {
			base := dep[strings.LastIndex(dep, "/")+1:]
			lines = append(lines, `import "./`+lastExtRE.ReplaceAllString(base, "")+`"`)
		}
		contents = append(contents, importgraph.SourceFile{
			Path:    path,
			Content: strings.Join(lines, "\n"),
		})
	}
	return importgraph.BuildImportGraph(contents, nil)
}

func TestSelectImpactedTests(t *testing.T) {
	t.Run("finds a direct importer", func(t *testing.T) {
		g := graphFor([]edgeSpec{{"a.test.ts", []string{"b.ts"}}, {"b.ts", nil}})
		got := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{"b.ts"}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		})
		// toMatchObject: only the named members are asserted.
		if !reflect.DeepEqual(got.Impacted, []string{"a.test.ts"}) {
			t.Fatalf("impacted = %v", got.Impacted)
		}
		if got.Confidence != ConfidenceExact {
			t.Fatalf("confidence = %q", got.Confidence)
		}
	})

	t.Run("finds a transitive importer", func(t *testing.T) {
		g := graphFor([]edgeSpec{
			{"a.test.ts", []string{"b.ts"}},
			{"b.ts", []string{"c.ts"}},
			{"c.ts", nil},
		})
		got := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{"c.ts"}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		}).Impacted
		if !reflect.DeepEqual(got, []string{"a.test.ts"}) {
			t.Fatalf("impacted = %v", got)
		}
	})

	t.Run("handles cycles without looping", func(t *testing.T) {
		g := graphFor([]edgeSpec{
			{"a.test.ts", []string{"b.ts"}},
			{"b.ts", []string{"a.test.ts"}},
		})
		got := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{"a.test.ts"}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		}).Impacted
		if !reflect.DeepEqual(got, []string{"a.test.ts"}) {
			t.Fatalf("impacted = %v", got)
		}
	})

	t.Run("marks config changes partial", func(t *testing.T) {
		g := graphFor([]edgeSpec{{"a.test.ts", []string{"b.ts"}}, {"b.ts", nil}})
		result := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{"config.json"}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		})
		if result.Confidence != ConfidencePartial {
			t.Fatalf("confidence = %q", result.Confidence)
		}
		if !strings.Contains(result.Reason, "non-code") {
			t.Fatalf("reason = %q", result.Reason)
		}
	})

	t.Run("marks unknown files partial", func(t *testing.T) {
		g := graphFor([]edgeSpec{{"a.test.ts", []string{"b.ts"}}, {"b.ts", nil}})
		result := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{"missing.ts"}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		})
		if result.Confidence != ConfidencePartial {
			t.Fatalf("confidence = %q", result.Confidence)
		}
		if !strings.Contains(result.Reason, "unknown") {
			t.Fatalf("reason = %q", result.Reason)
		}
	})

	t.Run("includes a changed test even without a dependent edge", func(t *testing.T) {
		g := graphFor([]edgeSpec{{"a.test.ts", nil}})
		got := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{"a.test.ts"}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		}).Impacted
		if !reflect.DeepEqual(got, []string{"a.test.ts"}) {
			t.Fatalf("impacted = %v", got)
		}
	})

	t.Run("empty changes are partial and select no tests", func(t *testing.T) {
		g := graphFor([]edgeSpec{{"a.test.ts", nil}})
		result := SelectImpactedTests(SelectImpactedTestsOptions{
			ChangedFiles: []string{}, Edges: g, AllTestFiles: []string{"a.test.ts"},
		})
		if !reflect.DeepEqual(result.Impacted, []string{}) {
			t.Fatalf("impacted = %v", result.Impacted)
		}
		if result.Confidence != ConfidencePartial {
			t.Fatalf("confidence = %q", result.Confidence)
		}
	})
}

func TestBuildTestCommand(t *testing.T) {
	t.Run("builds runner commands and handles no impacted tests", func(t *testing.T) {
		if got := BuildTestCommand([]string{}, RunnerBun); got != nil {
			t.Fatalf("expected null, got %q", *got)
		}
		if got := BuildTestCommand([]string{"a.test.ts", "b.test.ts"}, RunnerBun); got == nil ||
			*got != "bun test a.test.ts b.test.ts" {
			t.Fatalf("got %v", got)
		}
		if got := BuildTestCommand([]string{"a.test.ts"}, RunnerVitest); got == nil ||
			*got != "npx vitest run a.test.ts" {
			t.Fatalf("got %v", got)
		}
	})
}

func TestComputeImpactedTestsForWorkspace(t *testing.T) {
	makeRepo := func(t *testing.T) string {
		t.Helper()
		dir, err := os.MkdirTemp("", "tia-ws-")
		if err != nil {
			t.Fatalf("mkdtemp: %v", err)
		}
		mkdir := func(p string) {
			if err := os.MkdirAll(filepath.Join(dir, p), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
		}
		write := func(p string, content string) {
			if err := os.WriteFile(filepath.Join(dir, p), []byte(content), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
		}
		mkdir("src")
		mkdir(filepath.Join("node_modules", "junk"))
		write(filepath.Join("src", "c.ts"), "export const c = 1\n")
		write(filepath.Join("src", "b.ts"), "import { c } from \"./c\"\nexport const b = c\n")
		write(filepath.Join("src", "a.test.ts"), "import { b } from \"./b\"\ntest(\"x\", () => b)\n")
		write(filepath.Join("src", "other.test.ts"), "test(\"y\", () => 1)\n")
		write(filepath.Join("node_modules", "junk", "index.ts"), "import \"everything\"\n")
		return dir
	}

	t.Run("finds transitively impacted tests through the real filesystem", func(t *testing.T) {
		dir := makeRepo(t)
		defer os.RemoveAll(dir)
		result := ComputeImpactedTestsForWorkspace(ComputeImpactedTestsForWorkspaceOptions{
			Workspace: dir, ChangedFiles: []string{"src/c.ts"},
		})
		if result == nil {
			t.Fatal("expected a result, got null")
		}
		if !reflect.DeepEqual(result.Impacted, []string{"src/a.test.ts"}) {
			t.Fatalf("impacted = %v", result.Impacted)
		}
		if result.Confidence != ConfidenceExact {
			t.Fatalf("confidence = %q", result.Confidence)
		}
		// node_modules junk was skipped by the walk.
		if result.ScannedFiles != 4 {
			t.Fatalf("scannedFiles = %d", result.ScannedFiles)
		}
	})

	t.Run("returns null instead of truncating when the repo exceeds maxFiles", func(t *testing.T) {
		dir := makeRepo(t)
		defer os.RemoveAll(dir)
		maxFiles := 2.0
		if got := ComputeImpactedTestsForWorkspace(ComputeImpactedTestsForWorkspaceOptions{
			Workspace: dir, ChangedFiles: []string{"src/c.ts"}, MaxFiles: &maxFiles,
		}); got != nil {
			t.Fatalf("expected null, got %+v", *got)
		}
	})
}
