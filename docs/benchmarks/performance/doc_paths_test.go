package performance_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

var localScriptExample = regexp.MustCompile(`(?:docs|scripts)/[A-Za-z0-9_./-]+\.sh`)

func TestDocumentedScriptPathsResolve(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not locate test source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))

	for _, document := range []string{"README.md", "measure-cli.sh"} {
		t.Run(document, func(t *testing.T) {
			path := filepath.Join(root, "docs", "benchmarks", "performance", document)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			matches := localScriptExample.FindAllString(string(contents), -1)
			if len(matches) == 0 {
				t.Fatal("no local script examples found")
			}

			for _, example := range matches {
				scriptPath := filepath.Join(root, filepath.FromSlash(example))
				if _, err := os.Stat(scriptPath); err != nil {
					t.Errorf("documented script %q does not resolve: %v", example, err)
				}
			}
		})
	}
}
