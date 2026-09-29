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
	"github.com/Agent-Field/codeaf/internal/cellbudget"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE PERSISTED-OBJECT TABLE ──────────────────────────────────────────────
//
// Every object the build writes to disk is one row here. A row is a name and a
// populated sample (every optional field set, so the checks see every key).
// A later lane that adds a persisted type adds ONE row; L1, L11 and the golden
// fixture (testdata/<name>.golden.json, made with -update) then hold for it with
// no other edit. Types this package cannot name (the WAL record, the vault
// envelope) have their row in their own package, through [Golden].
var _ = UpdateFlag()

var persisted = []row{
	{"cell.Meta", cell.Meta{
		V: 1, Class: cell.FilesOnly, CellKeyID: "00112233445566778899aabbccddeeff",
		Base: &cell.Base{Remote: "https://example.test/repo.git", SHA: "abc123"},
	}},
	{"session.SessionTruth", session.SessionTruth{
		V: 1, LaunchDir: "workspace:cmd", Owned: true, Effort: "high", Approval: "allow",
		Places:        []session.PlaceRef{{Path: "home:proj", Arrival: session.PlaceSaid}},
		Trees:         []session.StandingTree{{Folder: "home:proj", Dir: "session:trees/a", Root: "home:proj", Wrote: []string{"a.go"}}},
		Archived:      true,
		ArchivedTasks: map[string]bool{"t1": true},
	}},
	{"cellstore.Turn", cellstore.Turn{
		V: 1, ID: "9f2c", Parent: "41ab", MergeParents: []string{"77d0"}, SealedAtMs: 1759049990120,
		Quality: cellstore.Quiescent, Trigger: cellstore.Setup, Device: "c0de", Fence: 17,
		Receipt: "77d0", ExcludedPaths: []string{"node_modules"},
	}},
	{"cellstore.Receipt", cellstore.Receipt{
		V: 1, Transcript: cellstore.Range{Start: 40213, End: 41877},
		Calls: []cellstore.Call{{Tool: "bash", ArgsHash: "d1f0", Started: 1759049981200, Ended: 1759049981940,
			Exit: 0, StdoutHash: "0a1b", StderrHash: "e3b0", SideEffect: "local"}},
		Services: []cellstore.ServiceRec{{PID: 4711, Argv: []string{"postgres", "-D", "var/pg"}, Ports: []int{5432}}},
		ModelCalls: []cellstore.ModelCall{{Model: "deepseek/deepseek-v4.1-flash", ParamsHash: "7c7c",
			RequestHash: "5a5a", ResponseHash: "b2b2", TokensIn: 5210, TokensOut: 412, CostMicroUSD: 613}},
	}},
	{"cellstore.Receipt.minimal", cellstore.Receipt{V: 1, Calls: []cellstore.Call{}}},
	{"cellstore.Intent", cellstore.Intent{V: 1, Tool: "bash", ArgsHash: "d1f0", Started: 1759049981200, SideEffect: "external"}},
	{"inventory.Inventory", inventory.Inventory{
		V:        1,
		Tools:    []inventory.Tool{{Name: "node", BinaryHash: "3fa9c2", VersionString: "v22.4.0"}},
		Services: []inventory.Service{{Name: "postgres", Version: "16.3", Ports: []int{5432}, DataDir: "var/pg"}},
		Platform: inventory.Platform{OS: "linux", Arch: "amd64"}, Lockfiles: []string{"uv.lock"},
		EnvVarNames: []string{"DATABASE_URL"}, Hints: map[string]string{"devcontainer": ".devcontainer/devcontainer.json"},
		Annotations: map[string]map[string]any{"postgres": {"optional": true}},
	}},
}

type row struct {
	name   string
	sample any
}

// deviceLocal objects never enter a tree, so L1 does not apply to them (the
// budget ledger names the cell's own root); they are frozen all the same.
var deviceLocal = []row{
	{"cellbudget.Entry", cellbudget.Entry{
		V: 1, Root: "/cells/01J", OpenedMs: 1759049981200, Bytes: 4096, MeasuredMs: 1759049990000,
		Evicted: &cellbudget.Pointer{Snapshot: "9f2c", AtMs: 1759050000000, Paths: []string{"work"}},
	}},
	{"cellbudget.Entry.resident", cellbudget.Entry{V: 1, Root: "/cells/01J", OpenedMs: 1759049981200}},
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

func TestNoAbsolutePathsUnderBites(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("clean.json", `{"p":"a/b"}`)
	write("bad.json", `{"p":"/etc/x"}`)
	write("lines.jsonl", "{\"p\":\"a\"}\n\n{\"p\":\"/etc/y\"}\n")
	write("note.txt", "/etc/z")
	found, err := NoAbsolutePathsUnder(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(found["bad.json"]) != 1 || len(found["lines.jsonl"]) != 1 || len(found["clean.json"]) != 0 || len(found["note.txt"]) != 0 {
		t.Errorf("findings = %v", found)
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

func TestGoldenFixtures(t *testing.T) {
	for _, o := range append(append([]row{}, persisted...), deviceLocal...) {
		t.Run(o.name, func(t *testing.T) {
			Golden(t, filepath.Join("testdata", o.name+".golden.json"), o.sample)
		})
	}
}
