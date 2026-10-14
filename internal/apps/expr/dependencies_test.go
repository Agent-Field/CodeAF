package expr

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestExpressionsDoNotDependOnServices(t *testing.T) {
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range file.Imports {
			name, err := strconv.Unquote(item.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, pkg := range []string{"svc", "records", "check"} {
				if name == "github.com/Agent-Field/codeaf/internal/apps/"+pkg || strings.HasPrefix(name, "github.com/Agent-Field/codeaf/internal/apps/"+pkg+"/") {
					t.Errorf("%s imports %s; worker drafts need a service-independent expression grammar", path, name)
				}
			}
		}
	}
}
