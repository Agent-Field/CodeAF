package importgraph

import (
	"reflect"
	"testing"
)

// Translation of src/session/import-graph.test.ts. Subtest names are the TS
// describe/test strings verbatim; `expect(...).toEqual(array)` becomes a
// reflect.DeepEqual on []string (empty is `[]string{}`, never nil, matching
// the TS `[]`), and `expect(...).toContain(x)` becomes a membership check.

func eq(t *testing.T, got []string, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("toEqual failed\n got: %#v\nwant: %#v", got, want)
	}
}

func contains(t *testing.T, got []string, want string) {
	t.Helper()
	for _, v := range got {
		if v == want {
			return
		}
	}
	t.Fatalf("toContain failed: %#v does not contain %q", got, want)
}

func TestExtractTsJsImports(t *testing.T) {
	t.Run("captures import / export-from / require / dynamic import", func(t *testing.T) {
		content := `
      import { a } from "./foo"
      import type { B } from "../types"
      export { c } from "./c"
      const x = require("./req")
      const y = await import("./dyn")
      import "side-effect"
    `
		eq(t, ExtractTsJsImports("src/x.ts", content), []string{
			"./foo",
			"../types",
			"./c",
			"./req",
			"./dyn",
			"side-effect",
		})
	})
}

func TestExtractPythonImports(t *testing.T) {
	t.Run("import and from-import", func(t *testing.T) {
		content := `
import os
import foo.bar, baz
from pkg.sub import thing
from .local import x
from ..parent import y
`
		eq(t, ExtractPythonImports("app/main.py", content), []string{
			"pkg.sub",
			".local",
			"..parent",
			"os",
			"foo.bar",
			"baz",
		})
	})
}

func TestExtractGoImports(t *testing.T) {
	t.Run("single and block imports", func(t *testing.T) {
		content := `
package main
import "fmt"
import (
  "os"
  alias "github.com/x/y"
  _ "net/http/pprof"
)
`
		eq(t, ExtractGoImports("main.go", content), []string{
			"fmt",
			"os",
			"github.com/x/y",
			"net/http/pprof",
		})
	})
}

func TestExtractImportsRelativeResolution(t *testing.T) {
	known := []string{"src/foo.ts", "src/a/b/index.ts", "src/util.tsx", "src/x/y.ts"}

	t.Run("./foo → foo.ts against known file list", func(t *testing.T) {
		content := "import { z } from \"./foo\"\n"
		eq(t, ExtractImports("src/bar.ts", content, &ExtractImportsOptions{KnownFiles: known}),
			[]string{"src/foo.ts"})
	})

	t.Run("../a/b → a/b/index.ts", func(t *testing.T) {
		// from src/x/y.ts, ../a/b → src/a/b → index.ts
		content := "import { z } from \"../a/b\"\n"
		eq(t, ExtractImports("src/x/y.ts", content, &ExtractImportsOptions{KnownFiles: known}),
			[]string{"src/a/b/index.ts"})
	})

	t.Run("extensionless sibling with .tsx", func(t *testing.T) {
		eq(t, ExtractImports("src/x/y.ts", "import \"../util\"\n",
			&ExtractImportsOptions{KnownFiles: known}), []string{"src/util.tsx"})
	})
}

func TestBuildImportGraph(t *testing.T) {
	t.Run("internal vs external edges", func(t *testing.T) {
		files := []SourceFile{
			{
				Path:    "src/a.ts",
				Content: "import { b } from \"./b\"\nimport React from \"react\"\n",
			},
			{
				Path:    "src/b.ts",
				Content: "import { c } from \"./c\"\n",
			},
			{
				Path:    "src/c.ts",
				Content: "export const c = 1\n",
			},
		}
		g := BuildImportGraph(files, nil)
		eq(t, DependenciesOf(g, "src/a.ts"), []string{"src/b.ts"})
		ext, ok := g.External.Get("src/a.ts")
		if !ok {
			t.Fatalf("external missing src/a.ts")
		}
		eq(t, ext, []string{"react"})
		eq(t, DependenciesOf(g, "src/b.ts"), []string{"src/c.ts"})
		eq(t, DependenciesOf(g, "src/c.ts"), []string{})
	})

	t.Run("dependents/dependencies symmetry", func(t *testing.T) {
		files := []SourceFile{
			{Path: "src/a.ts", Content: "import \"./b\"\nimport \"./c\"\n"},
			{Path: "src/b.ts", Content: "import \"./c\"\n"},
			{Path: "src/c.ts", Content: ""},
		}
		g := BuildImportGraph(files, nil)
		eq(t, DependenciesOf(g, "src/a.ts"), []string{"src/b.ts", "src/c.ts"})
		eq(t, DependentsOf(g, "src/c.ts"), []string{"src/a.ts", "src/b.ts"})
		eq(t, DependentsOf(g, "src/b.ts"), []string{"src/a.ts"})
		eq(t, DependentsOf(g, "src/a.ts"), []string{})

		// Every dep edge has a matching reverse edge.
		for _, e := range g.Deps.Entries() {
			for _, to := range e.Val {
				contains(t, DependentsOf(g, to), e.Key)
			}
		}
	})

	t.Run("python module heuristic resolves internal packages", func(t *testing.T) {
		files := []SourceFile{
			{Path: "pkg/__init__.py", Content: ""},
			{Path: "pkg/util.py", Content: ""},
			{Path: "app/main.py", Content: "from pkg.util import f\nimport os\n"},
		}
		g := BuildImportGraph(files, nil)
		eq(t, DependenciesOf(g, "app/main.py"), []string{"pkg/util.py"})
		ext, ok := g.External.Get("app/main.py")
		if !ok {
			t.Fatalf("external missing app/main.py")
		}
		eq(t, ext, []string{"os"})
	})

	t.Run("go local file import vs external module", func(t *testing.T) {
		files := []SourceFile{
			{
				Path:    "cmd/main.go",
				Content: "package main\nimport (\n  \"fmt\"\n  \"lib/helper\"\n)\n",
			},
			{Path: "lib/helper.go", Content: "package helper\n"},
		}
		g := BuildImportGraph(files, nil)
		eq(t, DependenciesOf(g, "cmd/main.go"), []string{"lib/helper.go"})
		ext, ok := g.External.Get("cmd/main.go")
		if !ok {
			t.Fatalf("external missing cmd/main.go")
		}
		eq(t, ext, []string{"fmt"})
	})

	t.Run("injectable extractor map for other languages", func(t *testing.T) {
		files := []SourceFile{
			{Path: "a.rs", Content: "use crate::b;\n"},
			{Path: "b.rs", Content: ""},
		}
		g := BuildImportGraph(files, &BuildImportGraphOptions{
			Extractors: extractorSets("rs"),
		})
		eq(t, DependenciesOf(g, "a.rs"), []string{"b.rs"})
	})
}
