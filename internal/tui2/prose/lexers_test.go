package prose

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// TestMain is the pin on the set's laziness, and it runs before any test can
// build the set for its own reasons.
//
// THE REGISTRY MUST BE EMPTY HERE. The whole point of the curated set is that
// nothing is parsed until a block or a filename needs it, and the way that is
// lost is somebody adding an eager parse — a `var _ = curatedGet("go")`, or a
// registry built in a package var — which reintroduces the cold-start cost the
// set exists to remove. curatedParses counts every XML file parsed and the
// registry is nil until first use, so an eager parse makes one of these non-zero
// and this exits non-zero before a single test reports.
func TestMain(m *testing.M) {
	if n := curatedParses.Load(); n != 0 {
		fmt.Fprintf(os.Stderr, "the curated lexer set parsed %d definition(s) from XML before any use; the registry is eager\n", n)
		os.Exit(1)
	}
	if curatedRegistry.reg != nil {
		fmt.Fprintln(os.Stderr, "the curated lexer registry was built before any use; it is eager")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// TestCuratedSetBuildsOnceAndAnswersNames pins the other half: first use builds
// the set, and no later use rebuilds it. Each definition is parsed exactly once
// for the life of the process, which is what makes the cost a one-off.
func TestCuratedSetBuildsOnceAndAnswersNames(t *testing.T) {
	if curated() == nil {
		t.Fatal("the curated registry is nil after first use")
	}
	parsed := curatedParses.Load()
	if parsed == 0 {
		t.Fatal("first use parsed no definition; the set is empty")
	}
	// A later use must find what is already built, not re-read every file.
	_ = curated().Get("python")
	if again := curatedParses.Load(); again != parsed {
		t.Errorf("a later use re-parsed definitions: %d -> %d", parsed, again)
	}

	// Names, aliases and bare extensions all reach a curated lexer, because the
	// names live in the definitions and the lookup is the dependency's own.
	for _, lang := range []string{"go", "golang", "python", "py", "rust", "ts", "yaml", "yml", "bash", "shell", "hcl", "terraform", "markdown", "md", "text"} {
		if curatedGet(lang) == nil {
			t.Errorf("curatedGet(%q) = nil, want a lexer", lang)
		}
	}
	for _, filename := range []string{"main.go", "script.py", "styles.css", "Dockerfile", "x.yml", "notes.md"} {
		if curatedMatch(filename) == nil {
			t.Errorf("curatedMatch(%q) = nil, want a lexer", filename)
		}
	}

	// A miss is nil — plain text, no error — for a language and for a filename.
	for _, miss := range []string{"mermaid", "not-a-language-at-all", "", "zzz"} {
		if got := curatedGet(miss); got != nil {
			t.Errorf("curatedGet(%q) = %q, want nil (a miss is plain text)", miss, got.Config().Name)
		}
	}
	if curatedMatch("README.unknownext") != nil {
		t.Error("an uncurated filename matched a lexer; a miss must be plain text")
	}
}

// TestCuratedLanguagesHighlight is the no-regression half: the set carries real
// lexers, not just non-nil placeholders, so a curated language a person actually
// writes is still coloured.
func TestCuratedLanguagesHighlight(t *testing.T) {
	cases := map[string]string{
		"python":     "```python\ndef f(x):\n    return 1\n```\n",
		"rust":       "```rust\nfn main() { let x = 1; }\n```\n",
		"typescript": "```typescript\nconst n: number = 1;\n```\n",
		"markdown":   "```markdown\n# heading\nbody\n```\n",
	}
	for lang, src := range cases {
		rows := render(t, src, Options{Width: 72, Styler: styler(tokens.TrueColor)})
		all := strings.Join(rows, "\n")
		highlighted := false
		for _, slot := range tokens.CodeSlots() {
			if slot == tokens.CodeText {
				continue
			}
			if seq := slot.Fg(tokens.TrueColor, tokens.FocusNormal); seq != "" && strings.Contains(all, seq) {
				highlighted = true
				break
			}
		}
		if !highlighted {
			t.Errorf("%s: nothing was highlighted; the curated lexer is not doing its job", lang)
		}
	}
}
