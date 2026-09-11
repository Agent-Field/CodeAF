package session

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// ── THE LAW: THE WRAPPER HIDES NO DOOR THE ADAPTER OFFERS ───────────────────
//
// [sessionCompleter] is the one thing every request an agent makes goes
// through, and it is a WRAPPER: [Agent.installSessionClient] writes it on every
// construction path, so `a.client` is never the adapter itself. Everything this
// package asks the adapter through an optional interface — `client.(modelChain)`,
// `client.(laneProber)` — is therefore asking the WRAPPER, and a door the
// wrapper does not forward is a door the conversation does not have, however
// completely the adapter implements it.
//
// THAT IS NOT A HYPOTHETICAL. `ProbeLanes` was swallowed exactly this way: the
// probe bought by a person starting to type — the one that exists so a turn's
// first token is not also paying for a TLS handshake — had never fired in a
// built agent, on any road, since the wrapper landed. It looked covered, because
// the test that covered it constructed `&Agent{client: adapter}` directly and so
// never met the wrapper at all. A green test over a door nobody could reach is
// the same shape internal/remote's surface-door law was written for, one layer
// down.
//
// So: every optional interface this package type-asserts on a completer, that
// the real adapter satisfies, must be forwarded by [sessionCompleter]. The
// comparison is by name and arity for internal/remote's stated reason — the same
// method is spelled `Answer` in one package and `session.Answer` in another —
// and a door that is absent or the wrong shape is what this catches.

// TestTheCompleterWrapperForwardsEveryDoorTheAdapterOffers is the law above.
func TestTheCompleterWrapperForwardsEveryDoorTheAdapterOffers(t *testing.T) {
	root := completerLawRoot(t)
	here := filepath.Join(root, "internal", "session")

	declared := interfacesDeclaredIn(t, here)
	asserted := completerAssertionsIn(t, here, declared)
	if len(asserted) == 0 {
		t.Fatal("this law found no optional door asserted on a completer: the walk has stopped reaching them")
	}
	adapter := methodsOfType(t, filepath.Join(root, "internal", "provider"), "Client")
	wrapper := methodsOfType(t, here, "sessionCompleter")

	for _, door := range asserted {
		if !completerHolds(adapter, door.methods) {
			// A DOOR THE ADAPTER DOES NOT HAVE EITHER IS NOT AN ASYMMETRY. The
			// package is reaching for something only a test double or a future
			// completer implements, and the wrapper owes it nothing.
			continue
		}
		if !door.onCompleter {
			// AN ASSERTION THIS LAW CANNOT PLACE IS NOT ONE IT MAY SKIP, which
			// is internal/remote's surface-door law's rule said on this seam.
			// The name list below ([completerNamed]) is the weak point of the
			// whole thing: rename the field, or put the completer in a local
			// first, and every door this law holds would quietly leave it. So a
			// door the ADAPTER answers, asserted on something this law does not
			// recognise as the completer, is a red — add the spelling, or say
			// in the message why that value is not one.
			t.Errorf("internal/session asserts %s on %q, *provider.Client answers it, and this law does not recognise that as the completer.\n"+
				"Either the completer is now spelled something else — teach completerNamed — or this is a different value that happens to share a door's shape.",
				door.name, door.receiver)
			continue
		}
		if completerHolds(wrapper, door.methods) {
			continue
		}
		t.Errorf("internal/session asserts %s on a completer, *provider.Client answers it, and sessionCompleter does not forward it (missing %s).\n"+
			"Every request goes through the wrapper, so a door it swallows is a door the conversation does not have.",
			door.name, strings.Join(completerMissing(wrapper, door.methods), ", "))
	}
}

// TestAnAgentBuiltTheOrdinaryWayCanProbe is the behaviour under that law,
// driven through the REAL constructor rather than an Agent literal.
//
// THE LITERAL IS WHY THIS WAS NOT CAUGHT. [TestTypingReachesTheTransportAndOnlyWhereThereIsOne]
// builds `&Agent{client: …}`, which puts the adapter straight into the field so
// the wrapper never exists — and the assertion that failed in every shipped
// binary succeeded there. This one goes through [newAgent], the constructor
// every road opens, and asks whether the probing seam survived it.
func TestAnAgentBuiltTheOrdinaryWayCanProbe(t *testing.T) {
	adapter := &probingCompleter{}
	agent, err := newAgent(Config{Workspace: t.TempDir(), Model: "sim/model"}, adapter)
	if err != nil {
		t.Fatalf("build an agent the ordinary way: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close() })

	if _, wrapped := agent.client.(sessionCompleter); !wrapped {
		t.Fatal("this test no longer goes through the wrapper, which is the only thing it is about")
	}
	if !agent.probeClientLanes(context.Background(), "sim/model") {
		t.Fatal("AN AGENT BUILT THE ORDINARY WAY HAS NO PROBING DOOR: sessionCompleter is swallowing it again")
	}
	if len(adapter.probed) != 1 || adapter.probed[0] != "sim/model" {
		t.Fatalf("the door answered and the adapter underneath was asked for %v", adapter.probed)
	}
}

// A compile-time statement of what this file is about: the adapter this build
// ships has the door, so the wrapper is the only thing that can lose it.
var _ laneProber = (*provider.Client)(nil)

// ── the tree readers ────────────────────────────────────────────────────────

func completerLawRoot(t *testing.T) string {
	t.Helper()
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the completer-door law")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(here), "..", ".."))
}

func walkPackage(t *testing.T, dir string, visit func(string, *ast.File)) {
	t.Helper()
	set := token.NewFileSet()
	listing, readErr := filepath.Glob(filepath.Join(dir, "*.go"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, path := range listing {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		visit(path, file)
	}
}

type completerDoor struct {
	name    string
	methods map[string]bool
	// receiver is what the assertion was written on, and onCompleter is whether
	// [completerNamed] recognised it. They are carried rather than filtered on
	// so that an assertion this law cannot place becomes a MESSAGE instead of a
	// silence — see the loop in the law above.
	receiver    string
	onCompleter bool
}

// interfacesDeclaredIn is every interface a package declares, as a method set
// keyed by name and arity.
func interfacesDeclaredIn(t *testing.T, dir string) map[string]map[string]bool {
	found := map[string]map[string]bool{}
	walkPackage(t, dir, func(_ string, file *ast.File) {
		for _, decl := range file.Decls {
			general, ok := decl.(*ast.GenDecl)
			if !ok || general.Tok != token.TYPE {
				continue
			}
			for _, spec := range general.Specs {
				typed, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				shape, ok := typed.Type.(*ast.InterfaceType)
				if !ok {
					continue
				}
				found[typed.Name.Name] = completerMethodSet(shape)
			}
		}
	})
	return found
}

func completerMethodSet(shape *ast.InterfaceType) map[string]bool {
	methods := map[string]bool{}
	if shape.Methods == nil {
		return methods
	}
	for _, field := range shape.Methods.List {
		function, ok := field.Type.(*ast.FuncType)
		if !ok {
			continue
		}
		for _, name := range field.Names {
			methods[completerSignature(name.Name, function)] = true
		}
	}
	return methods
}

// completerSignature is name and arity, for internal/remote's stated reason: the
// same method is spelled `Answer` in one package and `session.Answer` in another,
// and a comparison that read the spelling would report every door as missing.
func completerSignature(name string, function *ast.FuncType) string {
	in, out := 0, 0
	if function.Params != nil {
		for _, field := range function.Params.List {
			in += max(1, len(field.Names))
		}
	}
	if function.Results != nil {
		for _, field := range function.Results.List {
			out += max(1, len(field.Names))
		}
	}
	return name + "/" + itoaSmall(in) + "/" + itoaSmall(out)
}

func itoaSmall(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for ; n > 0; n /= 10 {
		digits = string(rune('0'+n%10)) + digits
	}
	return digits
}

// completerAssertionsIn is every `x.(T)` in the package where T is an interface
// the package declares, carrying what x was spelled and whether that spelling is
// one this law recognises as the completer.
//
// IT DOES NOT FILTER BY RECEIVER, and that is the change internal/remote's
// surface-door law asked for. Filtering here would mean a renamed field or a
// local alias took a door off the law with the gate green throughout; carrying
// the spelling instead lets the law say so out loud, for the doors the adapter
// actually answers.
func completerAssertionsIn(t *testing.T, dir string, declared map[string]map[string]bool) []completerDoor {
	best := map[string]completerDoor{}
	walkPackage(t, dir, func(_ string, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			asserted, ok := node.(*ast.TypeAssertExpr)
			if !ok || asserted.Type == nil {
				return true
			}
			name, ok := asserted.Type.(*ast.Ident)
			if !ok {
				return true
			}
			methods, known := declared[name.Name]
			if !known || len(methods) == 0 {
				return true
			}
			on, named := completerReceiver(asserted.X)
			// A RECOGNISED SPELLING WINS wherever one door is asserted on both.
			// The door IS on this law when any site asks it of the completer,
			// and one local alias elsewhere must not turn that into a complaint
			// that it is not.
			if prior, already := best[name.Name]; already && (prior.onCompleter || !named) {
				return true
			}
			best[name.Name] = completerDoor{name: name.Name, methods: methods, receiver: on, onCompleter: named}
			return true
		})
	})
	found := make([]completerDoor, 0, len(best))
	for _, door := range best {
		found = append(found, door)
	}
	sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })
	return found
}

// completerReceiver is what an assertion was written on, and whether that is one
// of the two names this package gives the completer underneath.
func completerReceiver(expr ast.Expr) (string, bool) {
	switch shape := expr.(type) {
	case *ast.Ident:
		return shape.Name, shape.Name == "client" || shape.Name == "inner"
	case *ast.SelectorExpr:
		return shape.Sel.Name, shape.Sel.Name == "client" || shape.Sel.Name == "inner"
	}
	return "an expression with no name", false
}

// methodsOfType is every method on a named type in one package, by either
// receiver form.
func methodsOfType(t *testing.T, dir, typeName string) map[string]bool {
	doors := map[string]bool{}
	walkPackage(t, dir, func(_ string, file *ast.File) {
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Recv == nil || len(function.Recv.List) != 1 {
				continue
			}
			receiver := function.Recv.List[0].Type
			if pointer, isPointer := receiver.(*ast.StarExpr); isPointer {
				receiver = pointer.X
			}
			name, ok := receiver.(*ast.Ident)
			if !ok || name.Name != typeName {
				continue
			}
			doors[completerSignature(function.Name.Name, function.Type)] = true
		}
	})
	return doors
}

func completerHolds(doors, wanted map[string]bool) bool {
	for method := range wanted {
		if !doors[method] {
			return false
		}
	}
	return true
}

func completerMissing(doors, wanted map[string]bool) []string {
	var absent []string
	for method := range wanted {
		if !doors[method] {
			absent = append(absent, method)
		}
	}
	sort.Strings(absent)
	return absent
}
