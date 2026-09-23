package remote

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"
)

var helloFields = map[string]bool{
	"Headless": true, "Version": true, "Workspace": true, "Session": true, "Model": true,
	"Level": true, "Launch": true, "Encodings": true, "Resume": true,
	"Surface": true, "Back": true, "Join": true, "New": true, "Watch": true,
}

// TestNoServiceKeyOrAddressCrossesTheWire refuses model-service credential
// fields throughout the remote stack. Generic pairing keys and relay addresses
// are different protocol facts and deliberately remain legal.
func TestNoServiceKeyOrAddressCrossesTheWire(t *testing.T) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate remote wire law")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
	forbidden := map[string]bool{
		"APIKey": true, "BaseURL": true, "SourceKey": true, "SourceAddress": true,
		"ServiceKey": true, "ServiceAddress": true, "ModelKey": true, "ModelAddress": true,
	}
	set := token.NewFileSet()
	for _, directory := range []string{"internal/remote", "internal/enginehost", "internal/pair", "internal/relay"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(set, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(node ast.Node) bool {
				field, ok := node.(*ast.Field)
				if !ok {
					return true
				}
				for _, name := range field.Names {
					if forbidden[name.Name] {
						position := set.Position(name.Pos())
						t.Errorf("%s:%d carries forbidden service credential field %s", filepath.ToSlash(path), position.Line, name.Name)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	file, err := parser.ParseFile(set, filepath.Join(root, "internal", "remote", "wire.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if unexpected := unexpectedHelloFields(file); len(unexpected) > 0 {
		t.Fatalf("remote.Hello gained fields outside its credential-reviewed allowlist: %s", strings.Join(unexpected, ", "))
	}
}

func unexpectedHelloFields(file *ast.File) []string {
	seen := make(map[string]bool, len(helloFields))
	var unexpected []string
	ast.Inspect(file, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok || typeSpec.Name.Name != "Hello" {
			return true
		}
		structure, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			unexpected = append(unexpected, "Hello is not a struct")
			return false
		}
		for _, field := range structure.Fields.List {
			for _, name := range field.Names {
				seen[name.Name] = true
				if !helloFields[name.Name] {
					unexpected = append(unexpected, name.Name)
				}
			}
		}
		return false
	})
	for name := range helloFields {
		if !seen[name] {
			unexpected = append(unexpected, fmt.Sprintf("missing %s", name))
		}
	}
	sort.Strings(unexpected)
	return unexpected
}

func TestTheHelloAllowlistRejectsAnUnknownCredentialSpelling(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "scratch.go", "package remote; type Hello struct { Version int; Credential string }", 0)
	if err != nil {
		t.Fatal(err)
	}
	unexpected := unexpectedHelloFields(file)
	if !slices.Contains(unexpected, "Credential") {
		t.Fatalf("unknown field escaped the allowlist: %v", unexpected)
	}
}
