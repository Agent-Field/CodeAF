package lawcheck

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
)

// ── THE PERSISTED-OBJECT TABLE ──────────────────────────────────────────────
//
// Every object the build writes to disk is one row here. A row is a name and a
// populated sample (every optional field set, so the checks see every key).
// A later lane that adds a persisted type adds ONE row; L1 and L11 then hold
// for it with no other edit.
var persisted = []struct {
	name   string
	sample any
}{
	{"cell.Meta", cell.Meta{
		V: 1, Class: cell.FilesOnly, CellKeyID: "00112233445566778899aabbccddeeff",
		Base: &cell.Base{Remote: "https://example.test/repo.git", SHA: "abc123"},
	}},
	{"inventory.Inventory", inventory.Inventory{
		V:        1,
		Tools:    []inventory.Tool{{Name: "node", BinaryHash: "3fa9c2", VersionString: "v22.4.0"}},
		Services: []inventory.Service{{Name: "postgres", Version: "16.3", Ports: []int{5432}, DataDir: "var/pg"}},
		Platform: inventory.Platform{OS: "linux", Arch: "amd64"}, Lockfiles: []string{"uv.lock"},
		EnvVarNames: []string{"DATABASE_URL"}, Hints: map[string]string{"devcontainer": ".devcontainer/devcontainer.json"},
		Annotations: map[string]map[string]any{"postgres": {"optional": true}},
	}},
}

// The sealing package: the one place a Turn may be built (L2).
const (
	sealingPackage = "cellstore"
	sealedType     = "Turn"
)

// Directories (relative to the module root) that may not build a Turn outside
// the sealing package.
var turnScope = []string{"internal/cell", "internal/cellstore"}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestLawL1NoAbsolutePathsInPersistedObjects(t *testing.T) {
	for _, o := range persisted {
		if bad := NoAbsolutePaths(encode(t, o.sample)); len(bad) > 0 {
			t.Errorf("%s persists absolute paths: %q", o.name, bad)
		}
	}
}

func TestLawL11VersionFieldFirst(t *testing.T) {
	for _, o := range persisted {
		if b := encode(t, o.sample); !VersionFirst(b) {
			t.Errorf("%s does not start with {\"V\":: %s", o.name, b)
		}
	}
}

func TestCheckersBite(t *testing.T) {
	home, _ := os.UserHomeDir()
	for _, doc := range []string{
		`{"V":1,"p":"/etc/x"}`, `{"a":["C:\\Users\\x"]}`, `{"a":{"b":"` + home + `/x"}}`,
	} {
		if len(NoAbsolutePaths([]byte(doc))) == 0 {
			t.Errorf("NoAbsolutePaths missed %s", doc)
		}
	}
	if NoAbsolutePaths([]byte(`{"V":1,"r":"https://h/x","p":"a/b"}`)) != nil {
		t.Error("NoAbsolutePaths flagged relative values")
	}
	if VersionFirst([]byte(`{"class":"x","V":1}`)) {
		t.Error("VersionFirst accepted V that is not first")
	}
}

func TestConstructsTypeBites(t *testing.T) {
	for _, src := range []string{
		"package p\nvar _ = Turn{}", "package p\nvar _ = &x.Turn{}",
		"package p\nvar _ = new(Turn)", "package p\nvar t Turn",
	} {
		hits, err := ConstructsType("f.go", src, "Turn")
		if err != nil || len(hits) != 1 {
			t.Errorf("want one hit for %q, got %v %v", src, hits, err)
		}
	}
	hits, _ := ConstructsType("f.go", "package p\ntype Turn struct{}\nfunc f(t Turn) {}", "Turn")
	if len(hits) != 0 {
		t.Errorf("declaration or use counted as construction: %v", hits)
	}
}

func TestLawL2OneConstructorOfTurn(t *testing.T) {
	root := moduleRoot(t)
	seen := 0
	for _, dir := range turnScope {
		for _, file := range goFiles(t, filepath.Join(root, dir)) {
			hits, pkg := constructions(t, file)
			seen += len(hits)
			if len(hits) > 0 && pkg != sealingPackage {
				t.Errorf("%s (package %s) builds %s outside %s: %v", file, pkg, sealedType, sealingPackage, hits)
			}
		}
	}
	if seen == 0 {
		t.Logf("no %s is built in %v yet: the law passes vacuously and bites once one exists", sealedType, turnScope)
	}
}

func constructions(t *testing.T, file string) (hits []string, pkg string) {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	hits, err = ConstructsType(file, src, sealedType)
	if err != nil {
		t.Fatal(err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), file, src, parser.PackageClauseOnly)
	if err != nil {
		t.Fatal(err)
	}
	return hits, packageName(f)
}

func packageName(f *ast.File) string { return f.Name.Name }

// goFiles lists non-test Go sources under dir; a missing dir is empty.
func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for ; dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
	}
	t.Fatal("go.mod not found")
	return ""
}
