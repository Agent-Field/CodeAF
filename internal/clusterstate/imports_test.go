package clusterstate

import (
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// R14 needs the same application package in the native and wasm builds. Keep
// its import boundary explicit so a storage/auth/HTTP helper cannot silently
// enter the pure graph when another packet adds a record or rule.
func TestR14PureImportBoundary(t *testing.T) {
	allowed := map[string]bool{
		"bytes": true, "encoding/json": true, "errors": true, "fmt": true,
		// Signature verification, hashing, key encoding and ordering are pure.
		"crypto/ed25519": true, "crypto/sha256": true, "encoding/hex": true, "sort": true,
		"reflect": true, "strconv": true, "strings": true, "unicode/utf8": true,
	}
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range parsed.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil || !allowed[path] {
				t.Errorf("%s imports %q outside the pure dependency boundary", file.Name(), path)
			}
		}
	}
}
