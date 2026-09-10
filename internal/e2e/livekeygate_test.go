package e2e

// livekeygate_test.go keeps every lane in this package asking for a provider key
// THE WAY THE PRODUCT ASKS FOR ONE.
//
// The product resolves a key three ways (internal/config's [config.APIKeyAt]):
// the OpenRouter variable, the OpenAI variable, then the `api_key` row in the
// profile. A lane that reads a variable itself sees only the first road, so on a
// machine whose key was pasted into the first-run setup the lane skips — and a
// skipped end-to-end suite is a suite that reports green without running, which
// is what #576 measured and what #184 cost a week.
//
// SO THE GATE IS STRUCTURAL AND UNTAGGED: it parses the suite's own sources, it
// needs no key, no tmux and no model, it runs in milliseconds on every pull
// request through `make test-laws`, and it names the file and line that went
// around [liveKey].

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// keyEnvNames is every variable the product's key resolution reads. A lane that
// names one of these to os.Getenv is a lane deciding for itself whether this
// machine can talk to a model.
var keyEnvNames = map[string]bool{
	"OPENROUTER_API_KEY": true,
	"OPENAI_API_KEY":     true,
}

// keyGateDoorFiles are the two files allowed to spell those names: the door
// itself, and this gate.
var keyGateDoorFiles = map[string]bool{
	"livekey_test.go":     true,
	"livekeygate_test.go": true,
}

// TestEveryLaneAsksForItsKeyTheWayTheProductDoes fails on a lane that reads a
// key variable out of the environment instead of going through [liveKey], and
// on a door that has stopped asking the product.
func TestEveryLaneAsksForItsKeyTheWayTheProductDoes(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), "internal", "e2e")
	theDoorStillAsksTheProduct(t, dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("the e2e suite is not where this gate expects it: %v", err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, "_test.go") || keyGateDoorFiles[name] {
			continue
		}
		checked++
		path := filepath.Join(dir, name)
		fset := token.NewFileSet()
		// THE BUILD TAG IS IRRELEVANT HERE. Every lane in this package is behind
		// `e2e`; go/parser reads the source whatever the tags say, which is why a
		// gate written this way can guard code it cannot itself build.
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if isAPIKeyAt(call) {
				t.Errorf("%s:%d calls config.APIKeyAt itself. That is the product's resolver, "+
					"but lanes here move AFORGE_HOME out from under themselves, so a resolution "+
					"at the gate answers for the fixture rather than for the machine. Ask liveKey(t) "+
					"instead, which reads those same three roads once at init (#576).",
					name, fset.Position(call.Pos()).Line)
				return true
			}
			if !readsTheEnvironment(call) {
				return true
			}
			for _, argument := range call.Args {
				text, ok := keyEnvArgument(argument)
				if !ok {
					continue
				}
				t.Errorf("%s:%d reads %s out of the environment. The product resolves a key "+
					"three ways (config.APIKeyAt: the two variables, then the profile's api_key "+
					"row), so a lane gated on one variable SKIPS on a machine that talks to a "+
					"model every day — and a skipped end-to-end suite reports green without "+
					"running (#576, #184). Ask liveKey(t) instead.",
					name, fset.Position(argument.Pos()).Line, text)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatalf("%s holds no lanes beside this gate, so it guards nothing", dir)
	}
}

// theDoorStillAsksTheProduct fails if livekey_test.go has stopped calling
// config.APIKeyAt. The other half of this gate excludes that file, so a door
// that went back to os.Getenv would go green on the exact skip #576 closed.
func theDoorStillAsksTheProduct(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "livekey_test.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing the liveKey door: %v", err)
	}
	found := false
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isAPIKeyAt(call) {
			found = true
		}
		return true
	})
	if !found {
		t.Errorf("internal/e2e/livekey_test.go is the one door this suite asks for a provider " +
			"key through, and it no longer calls config.APIKeyAt. That is the product's " +
			"three-road resolver; a door that reads one variable instead SKIPS on a machine " +
			"that talks to a model every day (#576).")
	}
}

// keyEnvArgument reports whether an argument names a key variable, either as a
// string literal or as config.APIKeyEnv — so a lane cannot hide the first road
// behind the constant the product spells.
func keyEnvArgument(argument ast.Expr) (string, bool) {
	switch arg := argument.(type) {
	case *ast.BasicLit:
		if arg.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(arg.Value)
		if err != nil || !keyEnvNames[text] {
			return "", false
		}
		return text, true
	case *ast.SelectorExpr:
		pkg, ok := arg.X.(*ast.Ident)
		if !ok || pkg.Name != "config" || arg.Sel.Name != "APIKeyEnv" {
			return "", false
		}
		return "config.APIKeyEnv", true
	default:
		return "", false
	}
}

// isAPIKeyAt reports whether a call is config.APIKeyAt. The product's
// resolver belongs in [liveKey]; a lane that calls it after AFORGE_HOME has
// moved is answering for the fixture.
func isAPIKeyAt(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "APIKeyAt" {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	return ok && pkg.Name == "config"
}

// readsTheEnvironment reports whether a call is os.Getenv or os.LookupEnv, which
// are the two doors a lane could read a key variable through.
func readsTheEnvironment(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "os" {
		return false
	}
	return selector.Sel.Name == "Getenv" || selector.Sel.Name == "LookupEnv"
}
