package crewroute

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// NO PROVIDER IS NAMED IN THE ROUTER. A route is a provider, a send id, a kind
// and a price; which provider is behind it is the caller's to know, and a
// branch on one provider's name here would be a rule the next provider does
// not get. The id-spelling tables (canonical.go) read how providers SPELL
// model ids, which is data about ids, and are the one file allowed to.
func TestTheRouterNamesNoProvider(t *testing.T) {
	names := []string{"openrouter", "fireworks", "together", "ollama", "codex", "chatgpt", "groq", "deepinfra", "anthropic", "cloudflare"}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "canonical.go" {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			for _, provider := range names {
				if strings.Contains(strings.ToLower(lit.Value), provider) {
					t.Errorf("%s names the provider %q in %s", name, provider, lit.Value)
				}
			}
			return true
		})
	}
}
