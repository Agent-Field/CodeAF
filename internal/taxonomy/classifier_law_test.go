package taxonomy

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// ── THE CLASSIFIER LAWS ─────────────────────────────────────────────────────
//
// These are structural tests in the repository's own convention: they open the
// tree with go/ast and refuse a SHAPE, so they are on the pull-request gate the
// day they land (scripts/laws.sh finds them by the import).
//
// They exist because the defect docs/design/recovery/DESIGN.md was written about
// is not a wrong line anywhere. It is that four waves in two weeks each added a
// correct special case to a DIFFERENT rule about what a failed call meant, and
// the shape that allows that is what these two refuse:
//
//	a 4xx turned into an action anywhere but here, and
//	the word `Verdict` naming two things.
//
// A person who reads only the code cannot see either. A build can.

// moduleRoot is the repository root, two directories up from this package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the module root is not where this law thinks it is: %v", err)
	}
	return root
}

// walkSources hands every non-test Go file in the module to `visit`, with its
// path relative to the module root and spelled with forward slashes so a law can
// name a file the same way on every machine.
func walkSources(t *testing.T, visit func(rel string, fset *token.FileSet, file *ast.File)) {
	t.Helper()
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			// Vendored and generated trees are nobody's law to keep.
			switch entry.Name() {
			case ".git", "vendor", "node_modules", "testdata", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		parsed, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		visit(filepath.ToSlash(rel), fset, parsed)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestOnlyOneVerdictTypeExists holds the word `Verdict` to ONE meaning in this
// module.
//
// It named two: this package's control answer — what the harness DOES about a
// failed call — and internal/provider's learning record, which is what a rating
// is trained on, in a package this one is read from. A reader holding either
// could not tell from the name which question it answered, and the census found
// three separate rules deciding what a 404 meant partly because the vocabulary
// let every answer look like the same kind of answer. The learning record is
// [provider.Reading] now, and this is what stops the next one.
func TestOnlyOneVerdictTypeExists(t *testing.T) {
	var declared []string
	walkSources(t, func(rel string, fset *token.FileSet, file *ast.File) {
		for _, decl := range file.Decls {
			generic, ok := decl.(*ast.GenDecl)
			if !ok || generic.Tok != token.TYPE {
				continue
			}
			for _, spec := range generic.Specs {
				typed, ok := spec.(*ast.TypeSpec)
				if !ok || typed.Name.Name != "Verdict" {
					continue
				}
				declared = append(declared,
					fmt.Sprintf("%s:%d", rel, fset.Position(typed.Pos()).Line))
			}
		}
	})
	if len(declared) != 1 {
		t.Fatalf("the module declares %d types named Verdict (%s), want exactly one — "+
			"a verdict is what the harness DOES about a failed call, and a second "+
			"meaning of the word is how three rules come to answer one question "+
			"(docs/design/recovery/DESIGN.md §2.7)", len(declared), strings.Join(declared, ", "))
	}
	if !strings.HasPrefix(declared[0], "internal/taxonomy/") {
		t.Fatalf("Verdict is declared in %s, want internal/taxonomy — the one place "+
			"that turns evidence into a move", declared[0])
	}
}

// factPackages are the two packages allowed to read an HTTP status at all.
//
//   - internal/taxonomy turns evidence into a MOVE, which is the whole of what
//     this law protects.
//   - internal/provider turns a wire response into a FACT: `Routing`, `Account`,
//     `Withdrawn`, `Overflow`, `Unserved`, `OurBytes` — each decided once, at the
//     refusal door, and carried on the error as a typed field. That is the half
//     nobody else can do, and it is deliberately not the half that decides
//     anything (refusalobject.go's [provider.Evidence]).
var factPackages = []string{"internal/taxonomy/", "internal/provider/"}

// seamsOwed are the sites that still read a status outside those two packages,
// each with the seam that closes it. THE LIST ONLY SHRINKS: a new entry is a
// fourth rule about what a 4xx means, which is the defect, and the test fails
// on one whether or not somebody adds it here.
//
// IT IS EMPTY, AND THAT IS THE POINT OF IT. The one entry was
// internal/session/auxiliary.go's `errandWalksOn`, which spelled `Status >= 400
// && Status < 500 && Upstream == ""` by hand; it reads `!evidence.OurBytes &&
// !evidence.Overflow` since the dispatcher wave (docs/design/recovery/DESIGN.md
// §4), so the carve-out came off with it. An empty list means the law below
// covers every file in the module outside the two fact packages — leave it that
// way.
var seamsOwed = map[string]string{}

// TestOnlyTheTaxonomyTurnsAStatusIntoAMove refuses the shape that produced four
// waves of the same defect: a caller reading a provider error's status and
// deciding for itself what to do about it.
//
// ── WHAT IT LOOKS FOR AND WHY THAT IS THE RIGHT SHAPE ───────────────────────
//
// `something.Status` compared against 400 or above — a literal, or one of the
// `http.Status*` names for them. That one expression is how all three of the
// rules the census counted were spelled: [classOf]'s switch, the refusal
// object's, and internal/session's `providerCouldNotServe`, whose list read
// `>= 500 || 404 || 429 || 401 || 403` and had drifted furthest from the others.
// Each was right about the half it had been told; none of them could be right
// about all of it, because only the transport knows whether a list emptied the
// set and only the session knows whether there is another model.
//
// So the shape is banned rather than the mistake. A caller that wants to know
// what a status MEANS asks [Classify] and does what the verdict says; a caller
// that wants to know what the wire SAID reads the facts internal/provider
// stamped on it.
func TestOnlyTheTaxonomyTurnsAStatusIntoAMove(t *testing.T) {
	var stray []string
	walkSources(t, func(rel string, fset *token.FileSet, file *ast.File) {
		for _, allowed := range factPackages {
			if strings.HasPrefix(rel, allowed) {
				return
			}
		}
		if _, owed := seamsOwed[rel]; owed {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			binary, ok := node.(*ast.BinaryExpr)
			if !ok || !comparison(binary.Op) {
				return true
			}
			if !readsAStatus(binary.X) && !readsAStatus(binary.Y) {
				return true
			}
			if !namesAFailureStatus(binary.X) && !namesAFailureStatus(binary.Y) {
				return true
			}
			stray = append(stray, fmt.Sprintf("%s:%d", rel, fset.Position(binary.Pos()).Line))
			return true
		})
	})
	if len(stray) > 0 {
		t.Fatalf("a provider status is read as a decision outside internal/taxonomy at %s — "+
			"ask taxonomy.Classify and do what the verdict says, or read the fact "+
			"internal/provider stamped on the error (provider.Evidence). "+
			"docs/design/recovery/DESIGN.md §2.6 counts what happens otherwise",
			strings.Join(stray, ", "))
	}
	// AND THE DEBT ONLY SHRINKS. An entry that no longer names a real site is a
	// carve-out nobody removed, which is how a law stops being one.
	for file, seam := range seamsOwed {
		if _, err := os.Stat(filepath.Join(moduleRoot(t), file)); err != nil {
			t.Fatalf("%s is on the owed-seams list and does not exist — delete the "+
				"entry (the seam was %q)", file, seam)
		}
	}
}

func comparison(op token.Token) bool {
	switch op {
	case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
		return true
	}
	return false
}

// readsAStatus reports that an expression is somebody's `.Status` field — the
// spelling every provider error in this build uses.
func readsAStatus(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "Status"
}

// namesAFailureStatus reports that an expression is 400 or above, written as a
// number or as one of net/http's names for one.
func namesAFailureStatus(expr ast.Expr) bool {
	switch named := expr.(type) {
	case *ast.BasicLit:
		if named.Kind != token.INT {
			return false
		}
		value, err := strconv.Atoi(named.Value)
		return err == nil && value >= 400
	case *ast.SelectorExpr:
		pkg, ok := named.X.(*ast.Ident)
		return ok && pkg.Name == "http" && strings.HasPrefix(named.Sel.Name, "Status")
	}
	return false
}

// TestTheAdapterNeverChangesTheModel holds internal/provider to the half of the
// recovery contract that makes ONE model hop possible.
//
// Two mechanisms drew from one `FallbackModels` list — the endpoint ladder's own
// walk and the pacing door's, both inside the adapter, plus internal/session's
// turn loop — and none of them knew the others had already tried a model
// (docs/design/recovery/DESIGN.md §2.2). Live evidence from 2026-09-10 22:32 says
// what that cost beyond the double spend: the adapter's hop carried the ORIGINAL
// model's `provider.only` across to the new model and was answered `404 No
// allowed providers are available for the selected model`, because a lane pin is
// per model and nothing re-derived it.
//
// The adapter owns the request's SHAPE and the session owns the model. So the
// chain — the ONLY source of another model in this package — may be read by
// exactly one function, the accessor that hands it to the layer above.
//
// IT NAMES THE READER RATHER THAN THE SHAPE OF A HOP, in
// refusaldoor_law_test.go's way and for its reason: a hop can be written a dozen
// ways and every one of them has to get the list from somewhere.
func TestTheAdapterNeverChangesTheModel(t *testing.T) {
	const chain, accessor = "fallbackChain", "FallbackModels"
	var callers []string
	walkSources(t, func(rel string, fset *token.FileSet, file *ast.File) {
		if !strings.HasPrefix(rel, "internal/provider/") {
			return
		}
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != chain {
					return true
				}
				callers = append(callers,
					fmt.Sprintf("%s (%s:%d)", function.Name.Name, rel, fset.Position(call.Pos()).Line))
				return true
			})
		}
	})
	if len(callers) != 1 || !strings.HasPrefix(callers[0], accessor+" ") {
		t.Fatalf("%s is read by %v, want only %s — the adapter relaxes a request's "+
			"SHAPE and never its model, because the layer that owns the turn is the "+
			"only one that knows what has already been tried (internal/session's "+
			"nextFallback, provider.ModelsTried)", chain, callers, accessor)
	}
}
