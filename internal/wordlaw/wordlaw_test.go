// Package wordlaw is THE WORDS LAW, and it is a package of nothing but the law.
//
// Two spellings were retired on 2026-11-23, and a string a person reads may not
// bring either one back:
//
//   - "Continue here" was the label for carrying a chat from another computer to
//     this one. The act has one verb, "Move here", wherever a person meets it.
//   - "sync service" was the TUI's name for the server. People call it the relay,
//     and "relay" names only that server and its storage switch.
//
// The law reads every string literal in the program's own sources, because a
// person reads the string and not the constant it hangs from, and it fails
// naming the file, the line and the spelling. Comments are not strings and are
// not read here. Test files are not read either: a test that asserts a person-
// facing string asserts the NEW spelling, and the manual has its own gate for
// the manual (internal/manual TestChatCorpusKeepsTheRetiredWordsOutOfSight).
//
// IT LIVES IN ITS OWN PACKAGE FOR THE SAME REASON iconlaw DOES: a word is not one
// surface's law, and a list of roots here is how a new surface joins it.
package wordlaw

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// roots are the trees whose string literals a person can read: the commands and
// every package under internal. third_party is vendored code and never ours.
var roots = []string{"../../cmd", ".."}

// retired are the spellings no literal may carry, compared in lower case.
var retired = []string{"continue here", "sync service"}

// kept are the few literals that may still carry a retired spelling, each with
// the reason. A search alias is typed by a person and never drawn, so it is how
// somebody who remembers the old label still finds the row it names.
var kept = map[string]string{
	"internal/config/settings_presentation.go": "continue here",
}

func TestNoStringAPersonReadsCarriesARetiredSpelling(t *testing.T) {
	seen := 0
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				switch d.Name() {
				case "testdata", "third_party", "wordlaw":
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			seen++
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Errorf("%s: %v", path, err)
				return nil
			}
			rel := repoRelative(path)
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				lower := strings.ToLower(text)
				for _, word := range retired {
					if strings.Contains(lower, word) && kept[rel] != word {
						t.Errorf("%s:%d spells %q in a string a person can read; say %q instead",
							rel, fset.Position(lit.Pos()).Line, word, replacement(word))
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if seen < 500 {
		t.Fatalf("the law read only %d Go files; its roots no longer reach the program", seen)
	}
}

// replacement names the spelling that took each retired one's place.
func replacement(word string) string {
	if word == "sync service" {
		return "relay"
	}
	return "Move here"
}

// repoRelative turns a path found under ../.. or .. into the repository-relative
// spelling the kept table uses.
func repoRelative(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	clean = strings.TrimPrefix(clean, "../../")
	if strings.HasPrefix(clean, "../") {
		clean = "internal/" + strings.TrimPrefix(clean, "../")
	}
	return clean
}
