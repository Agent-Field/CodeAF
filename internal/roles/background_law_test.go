package roles

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestNoBackgroundSeatNamesItsOwnModel IS THE LAW THAT KEEPS THE OWNER'S RULE
// (background.go): a background seat answers on the model the person chose,
// through this package's one ladder, and never on a model id written into the
// build.
//
// It reads every non-test Go source under internal/ and cmd/ and fails on a
// string literal spelled like a model id — `vendor/model`, an optional `~` in
// front and an optional `:suffix` behind — in any file outside [modelLiteralHomes].
// Those homes are the places a model id is DATA rather than a choice: the
// catalog and its prices, the media slots (chosen by what a model can do, never
// by a tier), the conversation's own default, demo fixtures. Every one of the
// ids this law was written to remove — `google/gemini-2.5-flash` on the reflex
// row, `deepseek/deepseek-v4-flash-0731` on small work, the page AI's and the
// Library's `deepseek/deepseek-v4.1-flash`, the quorum verifier's
// `deepseek/deepseek-v4-flash` — lived in a file that is not on that list.
//
// THE LIST MAY ONLY SHRINK. A new background call that wants a model asks
// [Resolve] (or internal/session's roleReader, which wraps it); it does not get
// a line here.
func TestNoBackgroundSeatNamesItsOwnModel(t *testing.T) {
	root := filepath.Join("..", "..")
	var found []string
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)
			if _, home := modelLiteralHomes[rel]; home {
				return nil
			}
			if _, home := modelLiteralHomes[filepath.ToSlash(filepath.Dir(rel))+"/"]; home {
				return nil
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err == nil && modelID.MatchString(value) {
					pos := fset.Position(lit.Pos())
					found = append(found, rel+":"+strconv.Itoa(pos.Line)+": "+value)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(found)
	for _, hit := range found {
		t.Errorf("%s — a model id written into the build; a background seat resolves its model through roles.Resolve (internal/roles/background.go)", hit)
	}
}

// TestEveryModelLiteralHomeStillHasOne keeps [modelLiteralHomes] honest: a
// file that no longer spells a model id leaves the list, so the list only
// shrinks.
func TestEveryModelLiteralHomeStillHasOne(t *testing.T) {
	root := filepath.Join("..", "..")
	for home := range modelLiteralHomes {
		if strings.HasSuffix(home, "/") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(home)))
		if err != nil {
			t.Errorf("%s: %v — remove it from modelLiteralHomes", home, err)
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, home, body, 0)
		if err != nil {
			t.Fatal(err)
		}
		has := false
		ast.Inspect(file, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if value, err := strconv.Unquote(lit.Value); err == nil && modelID.MatchString(value) {
					has = true
				}
			}
			return !has
		})
		if !has {
			t.Errorf("%s spells no model id any more — remove it from modelLiteralHomes", home)
		}
	}
}

// modelID is a string spelled like a model id: a known vendor, a slash, a
// name, and optionally codeaf's `~` routing marker in front or a `:variant`
// behind. The vendor list is deliberately the ones this build has ever named;
// a literal from a new vendor is the one case this law can miss, and the
// behavioural tests in background_test.go are the second net.
var modelID = regexp.MustCompile(`^~?(openai|anthropic|google|deepseek|qwen|moonshotai|z-ai|x-ai|meta-llama|mistralai|minimax|nvidia|thinkingmachines|bytedance|bytedance-seed|fish-audio|hexgrad|openrouter|cohere|amazon|microsoft|perplexity|nousresearch|baidu|tencent|stepfun|arcee-ai|ai21)/[A-Za-z0-9._\-]+(:[a-z]+)?$`)

// modelLiteralHomes are the files (or, ending in a slash, directories) where a
// model id is data, each with why. NOT ONE OF THEM IS A BACKGROUND SEAT.
var modelLiteralHomes = map[string]string{
	"cmd/codeaf-demo-home/":            "the demo home's seeded conversations, spend and rooms",
	"internal/catalog/catalog.go":      "the model catalog",
	"internal/orchestrate/fuel.go":     "per-model prices",
	"internal/config/config.go":        "the conversation's own default, and the media slots, chosen by capability",
	"internal/config/models.go":        "media capability rows",
	"internal/config/freecrew.go":      "the conversation's free rung, under the person's free-routes opt-in",
	"internal/config/settings.go":      "the conversation's failover chain (DefaultFallbackModel)",
	"internal/crewroute/canonical.go":  "id aliases the crew router canonicalizes",
	"internal/selector/replay/main.go": "a replay tool's fixture",
	"internal/tui3/models.go":          "model-author names seeded for connection-name collision checks",
	"cmd/harness-design/main.go":       "a developer tool's default flag",
}
