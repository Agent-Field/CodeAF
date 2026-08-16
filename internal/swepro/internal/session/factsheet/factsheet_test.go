package factsheet

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultFilesystem(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", `{
		"scripts":{"test":"go-like-test","build":"tsc"},
		"devDependencies":{"vitest":"1"},
		"engines":{"node":">=20"}
	}`)
	writeFile(t, root, "pnpm-lock.yaml", "lockfileVersion: 9\n")
	writeFile(t, root, "tsconfig.json", "{}\n")
	writeFile(t, root, "packages/a/package.json", `{"name":"@scope/a"}`)

	result := GenerateFactSheet(GenerateFactSheetOpts{RootDir: root})
	if result.Facts.PackageManager == nil || *result.Facts.PackageManager != PackageManagerPnpm {
		t.Fatalf("package manager = %#v", result.Facts.PackageManager)
	}
	if result.Facts.TestFramework == nil || *result.Facts.TestFramework != "vitest" {
		t.Fatalf("framework = %#v", result.Facts.TestFramework)
	}
	if !strings.Contains(result.Markdown, "- Language: TypeScript") {
		t.Fatalf("markdown = %q", result.Markdown)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
