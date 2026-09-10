package session

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// THE DELETED WORDS STAY DELETED, AND THIS IS THE GATE THAT KEEPS THEM SO.
//
// The three tiers gave every task state one word and one glyph, and the words
// that had been standing in for `your call` were struck out with them:
// `needs your look` was the landing report's lead, one wire sentence and the
// ratings file's outcome all at once, and `awaiting review` was a rail's own
// private ordering exception (docs/design/task-states/DESIGN.md). Each of those
// was correct on its own page, which is exactly why they survived three passes:
// nobody reading one file could see that four files were spelling one state four
// ways.
//
// SO THE LAW IS HELD ON THE SOURCE ITSELF rather than on the handful of rows a
// behavioural test happens to build. A row a test never constructs is a row this
// still catches, and a new sentence written next year in the old vocabulary is
// named the day it lands.
//
// IT READS STRING LITERALS AND NOTHING ELSE. `TaskUnverified` and the rest of
// the engine's own Go names are untouched by the design and are meant to stay:
// what was deleted is what a PERSON or the model reads, and in this package that
// is always a literal. Comments are left alone too — several of them say, on
// purpose, which word used to be there and why it is gone, and a law that
// stopped people writing that down would be a law against the record.
func TestNoStringInThisPackageSpellsADeletedTaskWord(t *testing.T) {
	// The words, exactly as they were spelled when they were person-facing. They
	// are matched case-insensitively because a sentence that opens with one is
	// the same sentence.
	deleted := []string{"needs your look", "awaiting review"}
	forEachPackageFile(t, func(path string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text := literal.Value
			if unquoted, err := strconv.Unquote(text); err == nil {
				text = unquoted
			}
			for _, word := range deleted {
				if strings.Contains(strings.ToLower(text), word) {
					t.Errorf("%s: %q still spells %q — the word for that state is %q (docs/design/task-states/DESIGN.md)",
						fset.Position(literal.Pos()), text, word, taskWordYourCall)
				}
			}
			return true
		})
	})
}
