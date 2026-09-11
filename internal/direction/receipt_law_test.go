package direction

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// ── THE RECEIPT LAWS ────────────────────────────────────────────────────────
//
// Only a person makes a rule govern, and the store can only know that from a
// PersonReceipt. These laws are what keep that sentence true as callers are
// added: a receipt is minted at a runtime site a reviewer named, never in a
// tool a model calls, and never spelled field by field anywhere but receipt.go;
// the importer, which copies old authority, runs only from a reviewed site;
// and the collections store's raw transactions, which skip every check the
// write path makes, belong to this package and the store's own.
//
// THEY ASK THE TYPE CHECKER WHAT EACH NAME IS, NOT HOW IT IS SPELLED. A law
// that matches pkg.Name is walked around by a dot import or an import under
// another name, so every file that reaches a watched package's watched name is
// type-checked, against its dependencies' export data from `go list`, and each
// identifier is judged by the object it resolves to.
//
// AND EVERY WATCHED OBJECT IS A PACKAGE-LEVEL NAME. A method can be renamed
// by an interface or a type parameter's constraint — the mocking idiom — and
// then the call site resolves to the interface's method, not the watched one.
// A function cannot: calling it, or taking it as a value, names it. So the
// importer and the raw doors are functions (TestEveryWatchedObjectIsAPackage
// LevelName), and the only way to use one is to name it.

const directionImport = "github.com/Agent-Field/aforge-v2/internal/direction"
const workspaceImport = "github.com/Agent-Field/aforge-v2/internal/workspace"

// The watched objects, by their type checker's full names: "pkg.Name".
var (
	receiptConstructors = map[string]bool{
		directionImport + ".FromCardAnswer": true, directionImport + ".FromTerminal": true,
		directionImport + ".FromPage": true, directionImport + ".FromVerifiedStatement": true,
	}
	// personSymbols are everything a model's tool must not name at all.
	personSymbols = map[string]bool{directionImport + ".AsPerson": true, directionImport + ".PersonReceipt": true}
	importerCalls = map[string]bool{directionImport + ".Import": true, directionImport + ".FinishImportRun": true}
	rawDoors      = map[string]bool{workspaceImport + ".ReadSnapshot": true, workspaceImport + ".WriteImmediate": true}
)

// watched is every watched object's name, by the import path of the package
// that declares it. A file that imports none of these packages, or spells none
// of their watched names through its import, cannot refer to any of them.
var watched = func() map[string]map[string]bool {
	byPackage := map[string]map[string]bool{}
	for _, set := range []map[string]bool{receiptConstructors, personSymbols, importerCalls, rawDoors} {
		for full := range set {
			dot := strings.LastIndex(full, ".")
			if byPackage[full[:dot]] == nil {
				byPackage[full[:dot]] = map[string]bool{}
			}
			byPackage[full[:dot]][full[dot+1:]] = true
		}
	}
	return byPackage
}()

// receiptSites is THE ALLOW-LIST: "path/to/file.go" → the constructors it may
// call, by short name. It is empty until the lane that answers a card, runs a
// terminal verb, serves the direction page or captures a statement lands; that
// lane adds its one site here with the reason, and review reads the line.
var receiptSites = map[string][]string{}

// importSites is the importer's allow-list, the same way: empty until the
// migration tool (design lane 4) lands and names its one site here.
var importSites = map[string][]string{}

type lawFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
}

func findRoot() (string, error) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return "", fmt.Errorf("%s is not the repository root: %w", root, err)
	}
	return root, nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := findRoot()
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// parseTree parses every non-test Go file in the checkout. A file that does
// not parse is counted and skipped: several sessions share the tree, and
// another lane's half-written file is not this law's business — but a walk
// where nothing parsed cannot pass.
func parseTree(root string) ([]lawFile, error) {
	var files []lawFile
	unparsed := 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if entry.IsDir() {
			if name == ".git" || name == "bin" || name == "third_party" || name == "node_modules" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			unparsed++
			return nil
		}
		files = append(files, lawFile{rel: filepath.ToSlash(rel), fset: fset, file: file})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) < 100 {
		return nil, fmt.Errorf("only %d files parsed (%d did not); the law saw too little to pass", len(files), unparsed)
	}
	return files, nil
}

// ── type-checking what the laws read ────────────────────────────────────────

// typedFile is one file of a type-checked package.
type typedFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
	info *types.Info
	pkg  *types.Package
}

// checked is what typeCheck returns: the files of the packages asked for, and
// the importer they were checked against, which hands out the very package
// objects their types refer to.
type checked struct {
	files []typedFile
	imp   types.Importer
}

type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Export     string
	Error      *struct{ Err string }
}

// typeCheck type-checks the packages in dirs (relative to root) from source,
// with every dependency read from the export data `go list -export` names.
// The workspace package is always listed, so its export data — and the
// database handles it imports — can be looked up whatever dirs import.
func typeCheck(root string, dirs []string) (checked, error) {
	sort.Strings(dirs)
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,Export,Error", "./internal/workspace"}
	for _, d := range dirs {
		args = append(args, "./"+d)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return checked{}, fmt.Errorf("go list for the law's packages: %v\n%s", err, stderr.String())
	}
	exports := map[string]string{}
	wanted := map[string]bool{}
	for _, d := range dirs {
		wanted[filepath.Join(root, d)] = true
	}
	var targets []listedPackage
	for dec := json.NewDecoder(bytes.NewReader(out)); ; {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return checked{}, err
		}
		if p.Error != nil {
			return checked{}, fmt.Errorf("%s does not build, so the law cannot read it: %s", p.ImportPath, p.Error.Err)
		}
		exports[p.ImportPath] = p.Export
		if wanted[p.Dir] {
			targets = append(targets, p)
		}
	}
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok || file == "" {
			return nil, errors.New("no export data for " + path)
		}
		return os.Open(file)
	})
	result := checked{imp: imp}
	for _, p := range targets {
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				return checked{}, err
			}
			files = append(files, f)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
		pkg, err := (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, info)
		if err != nil {
			return checked{}, fmt.Errorf("%s does not type-check, so the law cannot read it: %v", p.ImportPath, err)
		}
		for _, f := range files {
			rel, err := filepath.Rel(root, fset.Position(f.Pos()).Filename)
			if err != nil {
				return checked{}, err
			}
			result.files = append(result.files, typedFile{rel: filepath.ToSlash(rel), fset: fset, file: f, info: info, pkg: pkg})
		}
	}
	return result, nil
}

func mustCheck(t *testing.T, dirs ...string) checked {
	t.Helper()
	c, err := typeCheck(repoRoot(t), dirs)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.files) == 0 {
		t.Fatalf("%v did not type-check", dirs)
	}
	return c
}

// exempt reports the two packages the raw doors and the receipt belong to.
// Their own testdata is not them.
func exempt(rel string) bool {
	dir := filepath.Dir(rel)
	return dir == "internal/direction" || dir == "internal/workspace"
}

// refersToWatched reports whether a file spells a watched name of a package
// it imports: pkg.Name under whatever name the import gives the package, or
// Name alone under a dot import. Every watched object is package-level, so
// this is the only way a file can refer to one.
func refersToWatched(f *ast.File) bool {
	local, dotted := map[string]map[string]bool{}, map[string]bool{}
	for _, spec := range f.Imports {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil || watched[p] == nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		switch name {
		case "_":
		case ".":
			for n := range watched[p] {
				dotted[n] = true
			}
		default:
			local[name] = watched[p]
		}
	}
	if len(local) == 0 && len(dotted) == 0 {
		return false
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			if x, ok := n.X.(*ast.Ident); ok && local[x.Name][n.Sel.Name] {
				found = true
			}
		case *ast.Ident:
			found = found || dotted[n.Name]
		}
		return !found
	})
	return found
}

// lawTree is every non-test file of the tree that refers to a watched name,
// type-checked; and, in loose, any such file the default build does not
// compile (a build tag), which the law therefore cannot vouch for.
type lawTree struct {
	typed []typedFile
	loose []string
}

// readTree is read once for every law. ITS ERROR IS KEPT WITH IT, so a tree
// the laws cannot read fails each of them, not only whichever ran first.
var readTree = sync.OnceValues(func() (lawTree, error) {
	root, err := findRoot()
	if err != nil {
		return lawTree{}, err
	}
	files, err := parseTree(root)
	if err != nil {
		return lawTree{}, err
	}
	dirs := map[string]bool{}
	referring := map[string]bool{}
	for _, f := range files {
		if exempt(f.rel) || !refersToWatched(f.file) {
			continue
		}
		dirs[filepath.Dir(f.rel)] = true
		referring[f.rel] = true
	}
	var tree lawTree
	if len(dirs) > 0 {
		list := make([]string, 0, len(dirs))
		for d := range dirs {
			list = append(list, d)
		}
		c, err := typeCheck(root, list)
		if err != nil {
			return lawTree{}, err
		}
		tree.typed = c.files
	}
	for _, f := range tree.typed {
		delete(referring, f.rel)
	}
	for rel := range referring {
		tree.loose = append(tree.loose, rel)
	}
	sort.Strings(tree.loose)
	return tree, nil
})

func typedTree(t *testing.T) lawTree {
	t.Helper()
	tree, err := readTree()
	if err != nil {
		t.Fatalf("the laws cannot read the tree, so none of them passes: %v", err)
	}
	return tree
}

// violation is one breach, named by the law it breaks.
type violation struct {
	law, pos, what string
}

// fullName is how the type checker names a package-level object: "pkg.Name".
// A method is "(pkg.Type).Name", which no watched object is.
func fullName(obj types.Object) string {
	if fn, ok := obj.(*types.Func); ok {
		return fn.FullName()
	}
	if obj.Pkg() == nil {
		return obj.Name()
	}
	return obj.Pkg().Path() + "." + obj.Name()
}

func allowed(sites map[string][]string, rel, full string) bool {
	short := full[strings.LastIndex(full, ".")+1:]
	for _, name := range sites[rel] {
		if name == short {
			return true
		}
	}
	return false
}

// breaches applies every law to type-checked files.
func breaches(files []typedFile) []violation {
	var out []violation
	for _, f := range files {
		if exempt(f.rel) {
			continue
		}
		isTool := strings.HasPrefix(f.rel, "internal/session/tools_")
		ast.Inspect(f.file, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok {
				return true
			}
			obj := f.info.Uses[id]
			if obj == nil {
				return true
			}
			pos := f.fset.Position(id.Pos()).String()
			full := fullName(obj)
			switch {
			case isTool && (receiptConstructors[full] || personSymbols[full]):
				out = append(out, violation{"tool", pos, "a model's tool names " + full + "; a tool proposes and the person accepts"})
			case receiptConstructors[full] && !allowed(receiptSites, f.rel, full):
				out = append(out, violation{"receipt", pos, full + " is called at a site not on receiptSites"})
			case importerCalls[full] && !allowed(importSites, f.rel, full):
				out = append(out, violation{"import", pos, full + " is called at a site not on importSites"})
			case rawDoors[full]:
				out = append(out, violation{"raw", pos, full + " is the direction package's door"})
			}
			return true
		})
	}
	return out
}

func report(t *testing.T, law string, found []violation) {
	t.Helper()
	for _, v := range found {
		if v.law == law || (law == "receipt" && v.law == "tool") {
			t.Errorf("%s: %s", v.pos, v.what)
		}
	}
}

// A RECEIPT IS MINTED ONLY AT AN ALLOW-LISTED RUNTIME SITE. No other file in
// the tree calls a receipt constructor, and no tool a model can call names
// the receipt, its constructors or AsPerson at all.
func TestPersonReceiptsAreMintedOnlyAtAllowListedSites(t *testing.T) {
	tree := typedTree(t)
	report(t, "receipt", breaches(tree.typed))
	for _, rel := range tree.loose {
		t.Errorf("%s refers to a watched name outside the default build; the law cannot resolve it", rel)
	}
}

// THE IMPORTER RUNS ONLY FROM A REVIEWED SITE. It is the one writer that
// copies authority an old store recorded, so where it is called from is
// reviewed like a receipt site.
func TestTheImporterIsCalledOnlyAtAllowListedSites(t *testing.T) {
	report(t, "import", breaches(typedTree(t).typed))
}

// THE RAW TRANSACTION DOORS ON THE COLLECTIONS STORE BELONG TO THIS PACKAGE.
// Anything else writing through them would skip validation, fences, the
// authority check and the live index — however it came to hold the handle.
func TestOnlyTheDirectionPackageUsesTheStoresRawTransactions(t *testing.T) {
	report(t, "raw", breaches(typedTree(t).typed))
}

// EVERY WATCHED OBJECT IS A PACKAGE-LEVEL NAME, which is what lets the laws
// above see every use of it (see the header). Should one become a method
// again, this fails before an interface can rename it.
func TestEveryWatchedObjectIsAPackageLevelName(t *testing.T) {
	c := mustCheck(t, "internal/direction/testdata/lawprobe/importer")
	for pkgPath, names := range watched {
		pkg, err := c.imp.Import(pkgPath)
		if err != nil {
			t.Fatal(err)
		}
		for name := range names {
			if pkg.Scope().Lookup(name) == nil {
				t.Errorf("%s.%s is not a package-level name", pkgPath, name)
			}
		}
	}
}

// NO INTERFACE CAN STAND IN FOR A WATCHED OBJECT. The mocking idiom — an
// interface of the importer's or a raw door's method, satisfied by the store
// — was the route around the laws while they were methods (re-review of
// 4e0c13013, R1). It no longer compiles.
func TestNoInterfaceCanStandInForTheImporterOrARawDoor(t *testing.T) {
	c := mustCheck(t, "internal/direction/testdata/lawprobe/importer")
	for want, src := range map[string]string{
		"missing method Import": `package p
import ("context"; "` + directionImport + `")
var _ interface {
	Import(context.Context, direction.ImportRun, direction.ImportItem) (direction.ImportResult, error)
} = (*direction.Store)(nil)`,
		"missing method FinishImportRun": `package p
import ("context"; "` + directionImport + `")
var _ interface{ FinishImportRun(context.Context, string, string, string) error } = (*direction.Store)(nil)`,
		"missing method WriteImmediate": `package p
import ("context"; "database/sql"; "` + workspaceImport + `")
var _ interface{ WriteImmediate(context.Context, func(*sql.Tx) error) error } = (*workspace.Store)(nil)`,
		"missing method ReadSnapshot": `package p
import ("context"; "database/sql"; "` + workspaceImport + `")
var _ interface{ ReadSnapshot(context.Context, func(*sql.Tx) error) error } = (*workspace.Store)(nil)`,
	} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "p.go", src, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (&types.Config{Importer: c.imp}).Check("p", fset, []*ast.File{f}, nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("an interface stood in for the store: want %q, got %v", want, err)
		}
	}
}

// THE LAWS CATCH EVERY ROUTE AROUND THEM. Each package under
// testdata/lawprobe is one way a spelling-matching law was walked around; the
// laws must name each.
func TestTheLawsCatchEveryRouteAroundThem(t *testing.T) {
	probes := map[string]string{
		"dotimport":   "receipt", // a constructor through a dot import
		"caller":      "raw",     // a raw door on a store a helper handed out, by call and by value
		"iface":       "raw",     // a raw door behind an interface of its own
		"importer":    "import",  // the importer from an unreviewed site
		"ifaceimport": "import",  // the importer behind an interface of its own
		"constraint":  "import",  // the importer behind a type parameter's constraint
		"handle":      "",        // only opens a store: nothing to report
		"namesake":    "",        // methods named like the raw doors on a type that holds no store
	}
	var dirs []string
	for name := range probes {
		dirs = append(dirs, "internal/direction/testdata/lawprobe/"+name)
	}
	got := map[string][]string{}
	for _, v := range breaches(mustCheck(t, dirs...).files) {
		name := filepath.Base(filepath.Dir(strings.SplitN(v.pos, ":", 2)[0]))
		got[name] = append(got[name], v.law)
	}
	for name, law := range probes {
		switch {
		case law == "" && len(got[name]) != 0:
			t.Errorf("%s reported %v and breaks no law", name, got[name])
		case law != "" && !contains(got[name], law):
			t.Errorf("%s walked around the %s law unreported (reported %v)", name, law, got[name])
		}
	}
	if n := len(got["caller"]); n != 2 {
		t.Errorf("caller writes the raw door by call and by function value; %d reported", n)
	}
}

// THE LOOSE-FILE RULE MATCHES THE WATCHED SYMBOL. A file the default build
// does not compile cannot be type-checked, so it is judged by what it spells:
// a watched package's watched name, through that package's import. A method
// that happens to be called Import in a file that imports neither package is
// not a watched symbol.
func TestTheLooseFileRuleMatchesTheWatchedSymbol(t *testing.T) {
	for src, want := range map[string]bool{
		"//go:build never\n\npackage p\n\ntype T struct{}\n\nfunc (T) Import() {}\n":                                           false,
		"package p\n\nimport \"go/importer\"\n\nvar _ = importer.Default\n\nfunc ReadSnapshot() {}\n":                          false,
		"package p\n\nimport \"" + directionImport + "\"\n\nvar _ = direction.Import\n":                                        true,
		"package p\n\nimport d \"" + directionImport + "\"\n\nvar _ = d.FinishImportRun\n":                                     true,
		"package p\n\nimport . \"" + directionImport + "\"\n\nvar _ = FromCardAnswer\n":                                        true,
		"package p\n\nimport \"" + workspaceImport + "\"\n\nvar _ = workspace.WriteImmediate\n":                                true,
		"package p\n\nimport \"" + directionImport + "\"\n\ntype T struct{}\n\nfunc (T) Import() {}\n\nvar _ direction.Kind\n": false,
	} {
		f, err := parser.ParseFile(token.NewFileSet(), "p.go", src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		if got := refersToWatched(f); got != want {
			t.Errorf("judged %v, want %v:\n%s", got, want, src)
		}
	}
}

// ── what the direction API hands out ────────────────────────────────────────

// THE DIRECTION API HANDS OUT NO RAW STORE. No exported function, method,
// variable, field or type of this package returns, takes or holds the
// workspace store, or a database handle or transaction — nor returns or holds
// an interface one of them satisfies: holding one is the way around every
// check here.
func TestTheDirectionAPIHandsOutNoRawStore(t *testing.T) {
	for _, e := range rawExposures(t, "internal/direction") {
		t.Errorf("direction.%s", e)
	}
}

// THE EXPOSURE CHECK SEES EVERY SHAPE A HANDLE CAN TAKE: a defined slice or
// function type, and an empty interface a store satisfies, as well as a field
// or a signature.
func TestTheExposureCheckSeesEveryShape(t *testing.T) {
	got := map[string]bool{}
	for _, e := range rawExposures(t, "internal/direction/testdata/lawprobe/exposes") {
		got[strings.SplitN(e, " ", 2)[0]] = true
	}
	for _, name := range []string{"Handles", "Opener", "Door"} {
		if !got[name] {
			t.Errorf("exposes.%s hands out a raw handle unreported (reported %v)", name, got)
		}
	}
	for _, name := range []string{"Names", "Count"} {
		if got[name] {
			t.Errorf("exposes.%s hands out nothing and was reported", name)
		}
	}
}

// rawExposures lists, as "Name exposes type", every exported object of the
// package in dir that hands out the workspace store or a database handle.
func rawExposures(t *testing.T, dir string) []string {
	t.Helper()
	c := mustCheck(t, dir)
	check := exposureCheck{module: strings.TrimSuffix(directionImport, "/internal/direction") + "/", seen: map[polarType]bool{}}
	for pkgPath, names := range map[string][]string{workspaceImport: {"Store"}, "database/sql": {"Tx", "DB", "Conn"}} {
		pkg, err := c.imp.Import(pkgPath)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range names {
			typ := pkg.Scope().Lookup(name).Type()
			check.forbidden = append(check.forbidden, typ, types.NewPointer(typ))
		}
	}
	scope := c.files[0].pkg.Scope()
	var out []string
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		for _, where := range exposedTypes(obj) {
			if check.reaches(where.typ, true) {
				out = append(out, where.name+" exposes "+types.TypeString(where.typ, nil))
			}
		}
	}
	return out
}

type exposed struct {
	name string
	typ  types.Type
}

// exposedTypes lists what an exported object shows a caller: a function's
// signature, a variable's type, and a type's exported fields, exported
// methods and — for a type that is not a struct — what it is defined as.
func exposedTypes(obj types.Object) []exposed {
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return []exposed{{obj.Name(), obj.Type()}}
	}
	named, ok := types.Unalias(tn.Type()).(*types.Named)
	if !ok {
		return []exposed{{tn.Name(), tn.Type()}}
	}
	var out []exposed
	if st, ok := named.Underlying().(*types.Struct); ok {
		for i := 0; i < st.NumFields(); i++ {
			if f := st.Field(i); f.Exported() {
				out = append(out, exposed{tn.Name() + "." + f.Name(), f.Type()})
			}
		}
	} else {
		out = append(out, exposed{tn.Name(), named.Underlying()})
	}
	set := types.NewMethodSet(types.NewPointer(named))
	for i := 0; i < set.Len(); i++ {
		if m := set.At(i).Obj(); m.Exported() {
			out = append(out, exposed{tn.Name() + "." + m.Name(), m.Type()})
		}
	}
	return out
}

// exposureCheck asks whether a type hands out a forbidden one.
type exposureCheck struct {
	forbidden []types.Type // each forbidden named type, and a pointer to it
	module    string       // this module's import-path prefix
	seen      map[polarType]bool
}

// polarType is a type and whether a caller receives it (out) or supplies it.
type polarType struct {
	typ types.Type
	out bool
}

// reaches reports whether t is, or is built from, a forbidden type — or, where
// a caller receives it, is an interface a forbidden type satisfies, as `any`
// is. A parameter flips the direction: a function that takes `any` hands
// nothing out, and a callback that is given `any` receives it. A named type of
// this module is read through: what it is defined as, and its exported
// methods. A named type from elsewhere is judged by what it is defined as
// only, since its methods are not this package's door (context.Context's
// Value returns `any` and hands out nothing of ours).
func (c *exposureCheck) reaches(t types.Type, out bool) bool {
	t = types.Unalias(t)
	key := polarType{t, out}
	if c.seen[key] {
		return false
	}
	c.seen[key] = true
	for _, f := range c.forbidden {
		if types.Identical(t, f) {
			return true
		}
	}
	switch x := t.(type) {
	case *types.Named:
		iface, isIface := x.Underlying().(*types.Interface)
		if obj := x.Obj(); obj.Pkg() == nil || !strings.HasPrefix(obj.Pkg().Path()+"/", c.module) {
			if isIface {
				return out && c.satisfied(iface)
			}
			return c.reaches(x.Underlying(), out)
		}
		for i := 0; i < x.NumMethods(); i++ {
			if m := x.Method(i); m.Exported() && c.reaches(m.Type(), out) {
				return true
			}
		}
		return c.reaches(x.Underlying(), out)
	case *types.Pointer:
		return c.reaches(x.Elem(), out)
	case *types.Slice:
		return c.reaches(x.Elem(), out)
	case *types.Array:
		return c.reaches(x.Elem(), out)
	case *types.Map:
		return c.reaches(x.Key(), out) || c.reaches(x.Elem(), out)
	case *types.Chan:
		return c.reaches(x.Elem(), out)
	case *types.Signature:
		for i := 0; i < x.Params().Len(); i++ {
			if c.reaches(x.Params().At(i).Type(), !out) {
				return true
			}
		}
		for i := 0; i < x.Results().Len(); i++ {
			if c.reaches(x.Results().At(i).Type(), out) {
				return true
			}
		}
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			if x.Field(i).Exported() && c.reaches(x.Field(i).Type(), out) {
				return true
			}
		}
	case *types.Interface:
		if out && c.satisfied(x) {
			return true
		}
		for i := 0; i < x.NumMethods(); i++ {
			if x.Method(i).Exported() && c.reaches(x.Method(i).Type(), out) {
				return true
			}
		}
	}
	return false
}

// satisfied reports whether any forbidden type satisfies iface.
func (c *exposureCheck) satisfied(iface *types.Interface) bool {
	for _, f := range c.forbidden {
		if types.Implements(f, iface) {
			return true
		}
	}
	return false
}

// Inside this package, a PersonReceipt is spelled with fields only in
// receipt.go, and exactly the four constructors return one.
func TestOnlyTheFourConstructorsSpellAPersonReceipt(t *testing.T) {
	files, err := parseTree(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	var constructors []string
	for _, f := range files {
		if filepath.Dir(f.rel) != "internal/direction" {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.CompositeLit:
				if id, ok := n.Type.(*ast.Ident); ok && id.Name == "PersonReceipt" && len(n.Elts) > 0 && filepath.Base(f.rel) != "receipt.go" {
					t.Errorf("%s: a PersonReceipt is spelled field by field outside receipt.go", f.fset.Position(n.Pos()))
				}
			case *ast.FuncDecl:
				if n.Recv != nil || !n.Name.IsExported() || n.Type.Results == nil {
					return true
				}
				for _, result := range n.Type.Results.List {
					if id, ok := result.Type.(*ast.Ident); ok && id.Name == "PersonReceipt" {
						constructors = append(constructors, n.Name.Name)
					}
				}
			}
			return true
		})
	}
	sort.Strings(constructors)
	want := []string{"FromCardAnswer", "FromPage", "FromTerminal", "FromVerifiedStatement"}
	if strings.Join(constructors, ",") != strings.Join(want, ",") {
		t.Fatalf("the functions that return a PersonReceipt are %v, want exactly %v", constructors, want)
	}
}
