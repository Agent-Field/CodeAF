package lane

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/home"
)

// TestNoPathHereResolvesInsideTheHomeThisBinaryInherited is the law of
// undertest.go, asked of the ambient environment: this test sets nothing, moves
// nothing and pins nothing, so what it reads is exactly what a test binary is
// handed when it starts.
//
// It is the witness for #475. Against the tree before the gate, on a machine
// where a real run has written a sheet, both of these resolved into
// `~/.aforge/v3` and `TestAnEmptyLedgerIsAnEmptyChoice` was answered out of a
// person's own router state.
func TestNoPathHereResolvesInsideTheHomeThisBinaryInherited(t *testing.T) {
	if got := StorePath(); got != "" {
		t.Errorf("the belief store of whoever ran this resolved to %q; a test binary is entitled to no store at all", got)
	}
	if got := cachePathIn("", testModel); got != "" {
		t.Errorf("the sheet cache of whoever ran this resolved to %q; a test binary is entitled to no cache at all", got)
	}
	// And the reach-through that made this a defect rather than a curiosity,
	// asked the way #475 was replicated: nothing here names a sheet, a home or
	// a file, and a chooser with an empty ledger still used to be answered out
	// of the cached lanes of whoever ran the suite.
	if choice := (&chooser{ledger: &fakeLedger{}}).Choose(talk()); !choice.Empty() {
		t.Errorf("an empty ledger produced an opinion out of the state of whoever ran this: %+v", choice)
	}
}

// TestAHomeATestChoseIsNotTheHomeItInherited is the other half, and it is why
// the gate can be this blunt: what is refused is a root NOBODY CHOSE. Most
// tests in this directory point AFORGE_HOME at a directory of their own and
// then read the store back, and every one of them must keep working.
func TestAHomeATestChoseIsNotTheHomeItInherited(t *testing.T) {
	chosen := t.TempDir()
	t.Setenv(home.EnvVar, chosen)
	if got, want := StorePath(), filepath.Join(chosen, "v3", "lanes.json"); got != want {
		t.Errorf("a home the test chose gave %q, want %q", got, want)
	}
	if got := cachePathIn("", testModel); !strings.HasPrefix(got, filepath.Join(chosen, "v3", "lanes")) {
		t.Errorf("a home the test chose gave the cache %q, want it under %q", got, chosen)
	}
	// A directory handed straight to the sheet is a choice too, and it never
	// touches the environment at all.
	own := t.TempDir()
	if got, want := cachePathIn(own, "vendor/model"), filepath.Join(own, "vendor%2Fmodel.json"); got != want {
		t.Errorf("a directory the test named gave %q, want %q", got, want)
	}
}

// TestBothInheritedRootsAreRefused covers the second root a test binary is
// handed without asking. AFORGE_PROFILE_DIR moves the profile out from under
// the state root, and a gate that watched only one of them would refuse the
// common case and read the person's file in the rarer one.
func TestBothInheritedRootsAreRefused(t *testing.T) {
	restore := inheritedRoots
	t.Cleanup(func() { inheritedRoots = restore })
	for _, root := range []string{"state", "profile"} {
		pretend := filepath.Join(t.TempDir(), root)
		inheritedRoots = []string{pretend}
		t.Setenv(home.EnvVar, pretend)
		if got := StorePath(); got != "" {
			t.Errorf("an inherited %s root resolved to %q", root, got)
		}
		if got := stateFile("v3", "somewhere", "deeper.json"); got != "" {
			t.Errorf("a file deep inside an inherited %s root resolved to %q", root, got)
		}
		// A sibling named like the root is not inside it, which is the whole
		// reason [home.Contains] compares path elements and not prefixes.
		t.Setenv(home.EnvVar, pretend+"-2")
		if stateFile("v3", "lanes.json") == "" {
			t.Errorf("a sibling of the inherited %s root was mistaken for a child of it", root)
		}
	}
}

// TestOutsideATestBinaryNothingChanges pins the half that cannot be observed
// from inside one: the product resolves its files exactly as it always has, and
// the gate is one bool read on a path that is taken once per process.
func TestOutsideATestBinaryNothingChanges(t *testing.T) {
	restore := underTest
	underTest = false
	t.Cleanup(func() { underTest = restore })
	t.Setenv(home.EnvVar, filepath.Join(t.TempDir(), "inherited"))
	inheritedRoots = []string{os.Getenv(home.EnvVar)}
	t.Cleanup(func() { inheritedRoots = []string{home.Dir(), os.Getenv(profileDirEnv)} })
	if got, want := StorePath(), home.Join("v3", "lanes.json"); got != want {
		t.Errorf("the product's belief file resolved to %q, want %q", got, want)
	}
}

// TestEveryPathInThisPackageGoesThroughTheGate is the law that makes the two
// tests above hold for code nobody has written yet.
//
// A gate is only worth what its coverage is: a new file that reaches the state
// root with [home.Join] of its own would be #475 again under another file name,
// and it would pass every behavioural test in this directory because nothing
// would be asking about it. So the state root is named in exactly one file
// here, and this reads the sources to say so — the same shape as the laws in
// structure_test.go and law_test.go.
func TestEveryPathInThisPackageGoesThroughTheGate(t *testing.T) {
	fset, files := sources(t)
	for name, file := range files {
		// This law's own file names the state root on purpose: it is where the
		// gate is written, and its test is where the gate is proved.
		if name == "undertest.go" || name == "undertest_test.go" {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok || ident.Name != "home" {
				return true
			}
			switch selector.Sel.Name {
			case "Join", "Dir", "DefaultUnder":
				t.Errorf("%s: %s resolves the state root itself; every path in this package goes through stateFile (undertest.go)",
					fset.Position(selector.Pos()), name)
			}
			return true
		})
	}
}
