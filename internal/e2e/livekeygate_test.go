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
// key variable out of the environment instead of going through [liveKey].
func TestEveryLaneAsksForItsKeyTheWayTheProductDoes(t *testing.T) {
	dir := filepath.Join(moduleRoot(t), "internal", "e2e")
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
			if !readsTheEnvironment(call) {
				return true
			}
			for _, argument := range call.Args {
				lit, ok := argument.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil || !keyEnvNames[text] {
					continue
				}
				t.Errorf("%s:%d reads %s out of the environment. The product resolves a key "+
					"three ways (config.APIKeyAt: the two variables, then the profile's api_key "+
					"row), so a lane gated on one variable SKIPS on a machine that talks to a "+
					"model every day — and a skipped end-to-end suite reports green without "+
					"running (#576, #184). Ask liveKey(t) instead.",
					name, fset.Position(lit.Pos()).Line, text)
			}
			return true
		})
	}
	if checked == 0 {
		t.Fatalf("%s holds no lanes beside this gate, so it guards nothing", dir)
	}
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
