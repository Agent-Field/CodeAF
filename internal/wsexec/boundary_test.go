package wsexec

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestAdmitRequestHasNoParentID(t *testing.T) {
	assertNoParentID(t, reflect.TypeOf(AdmitRequest{}))
	assertNoParentID(t, reflect.TypeOf(LaunchRequest{}))
	assertNoParentID(t, reflect.TypeOf(ExecutionBinding{}))
}

func assertNoParentID(t *testing.T, rt reflect.Type) {
	t.Helper()
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).Name == "ParentID" {
			t.Errorf("%s carries ParentID; folder membership is not a plan parent", rt.Name())
		}
	}
}

func TestLaunchDoesNotPassFolderAsParent(t *testing.T) {
	ctx := context.Background()
	store, rt, grant := harness(t)
	req := launchReq("rk-folder", "eq-folder", grant.ID)
	req.OwnerChatID = "folder-billing"
	req.CoordinatorID = "coord-billing"
	if _, err := Open(store, rt).LaunchOrJoin(ctx, req); err != nil {
		t.Fatal(err)
	}
	if len(rt.admits) != 1 {
		t.Fatalf("admits=%d", len(rt.admits))
	}
	got := rt.admits[0]
	if got.OwnerChatID != "folder-billing" {
		t.Fatalf("owner chat lost: %+v", got)
	}
	assertNoParentID(t, reflect.TypeOf(got))
}

func TestProductionFilesNeverWriteParentID(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	set := token.NewFileSet()
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := parser.ParseFile(set, file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(source, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				if typed.Name == "ParentID" {
					t.Errorf("%s names ParentID; folder membership is not a plan parent", file.Name())
				}
			}
			return true
		})
	}
}

func TestFakeRuntimeLivesOnlyInTests(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range source.Decls {
			named, ok := typeName(declaration)
			if !ok {
				continue
			}
			switch named {
			case "fakeRuntime", "memStore", "FakeRuntime", "MemoryStore":
				t.Errorf("%s declares %s; fake runtime belongs in tests", file.Name(), named)
			}
		}
	}
}

func TestPackageDoesNotImportSessionRunOrPlandb(t *testing.T) {
	files, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".go") || strings.HasSuffix(file.Name(), "_test.go") {
			continue
		}
		source, err := parser.ParseFile(token.NewFileSet(), file.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range source.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, owner := range []string{"session", "tui3", "wsapi", "provider", "run", "plandb", "wscollab", "wsdiscover", "enginehost"} {
				forbidden := "github.com/Agent-Field/codeaf/internal/" + owner
				if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
					t.Errorf("%s imports %s; the host binds this package", file.Name(), path)
				}
			}
			if path == "github.com/Agent-Field/codeaf/cmd" || strings.HasPrefix(path, "github.com/Agent-Field/codeaf/cmd/") {
				t.Errorf("%s imports %s", file.Name(), path)
			}
		}
	}
}
