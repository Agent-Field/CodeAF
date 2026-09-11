package direction

import (
	"bytes"
	"encoding/json"
	"errors"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
// that matches pkg.Name is walked around by a dot import, by a helper that
// hands out a handle whose package the caller never imports, or by an
// interface that renames the method. So every file that spells a watched name
// anywhere is type-checked, against its dependencies' export data from `go
// list`, and each identifier is judged by the object it resolves to.

const directionImport = "github.com/Agent-Field/aforge-v2/internal/direction"
const workspaceImport = "github.com/Agent-Field/aforge-v2/internal/workspace"

// The watched objects, by their type checker's full names.
var (
	receiptConstructors = map[string]bool{
		directionImport + ".FromCardAnswer": true, directionImport + ".FromTerminal": true,
		directionImport + ".FromPage": true, directionImport + ".FromVerifiedStatement": true,
	}
	// personSymbols are everything a model's tool must not name at all.
	personSymbols = map[string]bool{directionImport + ".AsPerson": true, directionImport + ".PersonReceipt": true}
	importerCalls = map[string]bool{
		"(*" + directionImport + ".Store).Import": true, "(*" + directionImport + ".Store).FinishImportRun": true,
	}
	rawDoors = map[string]bool{
		"(*" + workspaceImport + ".Store).ReadSnapshot": true, "(*" + workspaceImport + ".Store).WriteImmediate": true,
	}
	rawNames = map[string]bool{"ReadSnapshot": true, "WriteImmediate": true}
)

// watchedNames are the identifiers that send a file to the type checker: the
// last element of every watched object's name. A file that spells none of
// them cannot refer to any of them.
var watchedNames = func() map[string]bool {
	names := map[string]bool{}
	for _, set := range []map[string]bool{receiptConstructors, personSymbols, importerCalls, rawDoors} {
		for full := range set {
			names[full[strings.LastIndex(full, ".")+1:]] = true
		}
	}
	return names
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

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("%s is not the repository root: %v", root, err)
	}
	return root
}

// sources parses every non-test Go file in the checkout. A file that does not
// parse is counted and skipped: several sessions share the tree, and another
// lane's half-written file is not this law's business — but a walk where
// nothing parsed cannot pass.
func sources(t *testing.T) []lawFile {
	t.Helper()
	root := repoRoot(t)
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
		t.Fatal(err)
	}
	if len(files) < 100 {
		t.Fatalf("only %d files parsed (%d did not); the law saw too little to pass", len(files), unparsed)
	}
	return files
}

// ── type-checking what the laws read ────────────────────────────────────────

// typedFile is one file of a type-checked package.
type typedFile struct {
	rel  string
	fset *token.FileSet
	file *ast.File
	info *types.Info
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
func typeCheck(t *testing.T, root string, dirs []string) []typedFile {
	t.Helper()
	if len(dirs) == 0 {
		return nil
	}
	sort.Strings(dirs)
	args := []string{"list", "-export", "-deps", "-json=ImportPath,Dir,GoFiles,Export,Error"}
	for _, d := range dirs {
		args = append(args, "./"+d)
	}
	cmd := exec.Command("go", args...)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list for the law's packages: %v\n%s", err, stderr.String())
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
			t.Fatal(err)
		}
		if p.Error != nil {
			t.Fatalf("%s does not build, so the law cannot read it: %s", p.ImportPath, p.Error.Err)
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
	var typed []typedFile
	for _, p := range targets {
		var files []*ast.File
		for _, name := range p.GoFiles {
			f, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			files = append(files, f)
		}
		info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
		if _, err := (&types.Config{Importer: imp}).Check(p.ImportPath, fset, files, info); err != nil {
			t.Fatalf("%s does not type-check, so the law cannot read it: %v", p.ImportPath, err)
		}
		for _, f := range files {
			rel, err := filepath.Rel(root, fset.Position(f.Pos()).Filename)
			if err != nil {
				t.Fatal(err)
			}
			typed = append(typed, typedFile{rel: filepath.ToSlash(rel), fset: fset, file: f, info: info})
		}
	}
	return typed
}

// exempt reports the two packages the raw doors and the receipt belong to.
// Their own testdata is not them.
func exempt(rel string) bool {
	dir := filepath.Dir(rel)
	return dir == "internal/direction" || dir == "internal/workspace"
}

func spellsWatchedName(f *ast.File) bool {
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok && watchedNames[id.Name] {
			found = true
		}
		return !found
	})
	return found
}

var (
	treeOnce  sync.Once
	treeTyped []typedFile
	treeLoose []string
)

// typedTree is every non-test file of the tree that spells a watched name,
// type-checked; and, in loose, any such file the default build does not
// compile (a build tag), which the law therefore cannot vouch for.
func typedTree(t *testing.T) ([]typedFile, []string) {
	t.Helper()
	treeOnce.Do(func() {
		root := repoRoot(t)
		dirs := map[string]bool{}
		spelling := map[string]bool{}
		for _, f := range sources(t) {
			if exempt(f.rel) || !spellsWatchedName(f.file) {
				continue
			}
			dirs[filepath.Dir(f.rel)] = true
			spelling[f.rel] = true
		}
		var list []string
		for d := range dirs {
			list = append(list, d)
		}
		treeTyped = typeCheck(t, root, list)
		for _, f := range treeTyped {
			delete(spelling, f.rel)
		}
		for rel := range spelling {
			treeLoose = append(treeLoose, rel)
		}
		sort.Strings(treeLoose)
	})
	return treeTyped, treeLoose
}

// violation is one breach, named by the law it breaks.
type violation struct {
	law, pos, what string
}

// fullName is how the type checker names an object: "pkg.Name", or
// "(*pkg.Type).Method" for a method.
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
			pos := f.fset.Position(id.Pos()).String()
			if obj := f.info.Defs[id]; obj != nil {
				if _, isFunc := obj.(*types.Func); isFunc && rawNames[id.Name] {
					out = append(out, violation{"raw", pos, "declares a method named " + id.Name + ", a second door to the store's raw transaction"})
				}
			}
			obj := f.info.Uses[id]
			if obj == nil {
				return true
			}
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
	typed, loose := typedTree(t)
	report(t, "receipt", breaches(typed))
	for _, rel := range loose {
		t.Errorf("%s spells a watched name outside the default build; the law cannot resolve it", rel)
	}
}

// THE IMPORTER RUNS ONLY FROM A REVIEWED SITE. It is the one writer that
// copies authority an old store recorded, so where it is called from is
// reviewed like a receipt site.
func TestTheImporterIsCalledOnlyAtAllowListedSites(t *testing.T) {
	typed, _ := typedTree(t)
	report(t, "import", breaches(typed))
}

// THE RAW TRANSACTION DOORS ON THE COLLECTIONS STORE BELONG TO THIS PACKAGE.
// Anything else writing through them would skip validation, fences, the
// authority check and the live index — however it came to hold the handle.
func TestOnlyTheDirectionPackageUsesTheStoresRawTransactions(t *testing.T) {
	typed, _ := typedTree(t)
	report(t, "raw", breaches(typed))
}

// THE LAWS CATCH EVERY ROUTE AROUND THEM. Each package under
// testdata/lawprobe is one way a spelling-matching law was walked around; the
// laws must name each.
func TestTheLawsCatchEveryRouteAroundThem(t *testing.T) {
	root := repoRoot(t)
	probes := map[string]string{
		"dotimport": "receipt", // a constructor through a dot import
		"caller":    "raw",     // a raw door through a handle whose package is never imported
		"iface":     "raw",     // a raw door renamed by an interface
		"importer":  "import",  // the importer from an unreviewed site
		"handle":    "",        // only opens a store: nothing to report
	}
	var dirs []string
	for name := range probes {
		dirs = append(dirs, "internal/direction/testdata/lawprobe/"+name)
	}
	got := map[string][]string{}
	for _, v := range breaches(typeCheck(t, root, dirs)) {
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
		t.Errorf("caller writes the raw door by call and by method value; %d reported", n)
	}
}

// THE DIRECTION API HANDS OUT NO RAW STORE. No exported function, method,
// variable or field of this package returns, takes or holds the workspace
// store, or a database handle or transaction: holding one is the way around
// every check here.
func TestTheDirectionAPIHandsOutNoRawStore(t *testing.T) {
	root := repoRoot(t)
	typed := typeCheck(t, root, []string{"internal/direction"})
	if len(typed) == 0 {
		t.Fatal("the direction package did not type-check")
	}
	forbidden := map[string]bool{workspaceImport + ".Store": true, "database/sql.Tx": true, "database/sql.DB": true, "database/sql.Conn": true}
	pkg := typed[0].info.Defs
	var scope *types.Scope
	for _, obj := range pkg {
		if obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == directionImport {
			scope = obj.Pkg().Scope()
			break
		}
	}
	if scope == nil {
		t.Fatal("no package scope for direction")
	}
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		if !obj.Exported() {
			continue
		}
		for _, where := range exposedTypes(obj) {
			if reaches(where.typ, forbidden, map[types.Type]bool{}) {
				t.Errorf("direction.%s exposes %s", where.name, types.TypeString(where.typ, nil))
			}
		}
	}
}

type exposed struct {
	name string
	typ  types.Type
}

// exposedTypes lists what an exported object shows a caller: a function's
// signature, a variable's type, a type's exported fields and exported methods.
func exposedTypes(obj types.Object) []exposed {
	switch o := obj.(type) {
	case *types.Func, *types.Var, *types.Const:
		return []exposed{{o.Name(), o.Type()}}
	case *types.TypeName:
		var out []exposed
		named, ok := o.Type().(*types.Named)
		if !ok {
			return []exposed{{o.Name(), o.Type()}}
		}
		if st, ok := named.Underlying().(*types.Struct); ok {
			for i := 0; i < st.NumFields(); i++ {
				if f := st.Field(i); f.Exported() {
					out = append(out, exposed{o.Name() + "." + f.Name(), f.Type()})
				}
			}
		}
		for _, recv := range []types.Type{named, types.NewPointer(named)} {
			set := types.NewMethodSet(recv)
			for i := 0; i < set.Len(); i++ {
				if m := set.At(i).Obj(); m.Exported() {
					out = append(out, exposed{o.Name() + "." + m.Name(), m.Type()})
				}
			}
		}
		return out
	}
	return nil
}

// reaches reports whether a type is, points at, or is built from a forbidden
// named type. This package's own named types are judged at their own
// declaration, not through every use.
func reaches(t types.Type, forbidden map[string]bool, seen map[types.Type]bool) bool {
	if seen[t] {
		return false
	}
	seen[t] = true
	switch x := t.(type) {
	case *types.Named:
		obj := x.Obj()
		if obj.Pkg() != nil && forbidden[obj.Pkg().Path()+"."+obj.Name()] {
			return true
		}
		return false
	case *types.Pointer:
		return reaches(x.Elem(), forbidden, seen)
	case *types.Slice:
		return reaches(x.Elem(), forbidden, seen)
	case *types.Array:
		return reaches(x.Elem(), forbidden, seen)
	case *types.Map:
		return reaches(x.Key(), forbidden, seen) || reaches(x.Elem(), forbidden, seen)
	case *types.Chan:
		return reaches(x.Elem(), forbidden, seen)
	case *types.Signature:
		for _, tuple := range []*types.Tuple{x.Params(), x.Results()} {
			for i := 0; i < tuple.Len(); i++ {
				if reaches(tuple.At(i).Type(), forbidden, seen) {
					return true
				}
			}
		}
	case *types.Struct:
		for i := 0; i < x.NumFields(); i++ {
			if x.Field(i).Exported() && reaches(x.Field(i).Type(), forbidden, seen) {
				return true
			}
		}
	case *types.Interface:
		for i := 0; i < x.NumMethods(); i++ {
			if reaches(x.Method(i).Type(), forbidden, seen) {
				return true
			}
		}
	}
	return false
}

// Inside this package, a PersonReceipt is spelled with fields only in
// receipt.go, and exactly the four constructors return one.
func TestOnlyTheFourConstructorsSpellAPersonReceipt(t *testing.T) {
	var constructors []string
	for _, f := range sources(t) {
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
