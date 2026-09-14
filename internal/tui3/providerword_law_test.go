package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// THE PERSON-FACING WORD FOR THE THING BEHIND A MODEL IS `provider`.
//
// One model id is served by many machines, and until 2026-09-14 this surface
// called one of them a **lane**: the settings row was labelled `lane`, the
// picker's hint said `→ lanes`, the fold's empty line said `no machine has been
// measured`, and the manual's page was titled Lanes. The wire, OpenRouter's own
// refusals (`provider.only`) and the owner all said **provider**, so a person
// met two words for one thing and only one of them was ever written down
// anywhere they could check. The owner ruled on the word (issue #1023): it is
// `provider`, everywhere a person reads.
//
// THE RULING IS ABOUT VOCABULARY AND NOT ABOUT ARCHITECTURE, which is why this
// law reads STRING LITERALS and nothing else. `internal/lane`, `LanePin`,
// `laneAutoSaid`, `lanes.go` and the `lane.talk` settings key are all still
// spelled the old way on purpose — a name a person never reads costs nothing to
// leave alone, and renaming a key on disk would move somebody's pin. What a
// person READS is what moved, and a literal is the only place this surface can
// say a word to them.
//
// It is a law rather than a review note for the reason the icon sweep is one: a
// word reintroduced in one new hint is invisible in a diff and permanent on the
// screen, and the two spellings are exactly what the ruling exists to end.

// laneWordLaw is the standalone word in either number. It is a word boundary on
// both sides so that `lane.talk`, `lanes.json` and `internal/lane` — the three
// machinery spellings that are deliberately unchanged — are not swept up by a
// substring match, and it is case-insensitive because a sentence that opens
// with the word says it just as loudly.
var laneWordLaw = regexp.MustCompile(`(?i)\blanes?\b`)

// laneWordAllowed is every string literal in this package that may still carry
// the old word, each one with the reason it is not a person's word.
//
// `lane` in statusdeck.go is the KEY `/status --json` prints, beside `model`
// and `served`. It is a machinery name like `lane.talk` — a script that reads it
// was promised a stable spelling — so it stays, and the manual's own account of
// `/status` says the row keeps the old word and why. Everything else here is an
// import path, which is a package name and not a sentence.
var laneWordAllowed = map[string]string{
	"lane": "statusdeck.go",
}

// TestNoPersonFacingStringInThisSurfaceSaysLane walks every string literal this
// package declares and refuses the retired word.
//
// IT READS THE TREE AND NOT A RENDERED FRAME, which is what makes it a law: a
// hint that is only drawn at one width, a settings `about` nobody opened in a
// test, and a note posted from a branch no fixture reaches are all equally
// visible here, and all three are places the word lived.
func TestNoPersonFacingStringInThisSurfaceSaysLane(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("where am I: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read %s: %v", root, err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(root, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		// THE IMPORT BLOCK IS NOT A SENTENCE. `internal/lane` is the package
		// this surface asks its questions of, and its path is a literal like any
		// other, so it is cut out by shape rather than by an exception nobody
		// would remember to keep true.
		imports := map[ast.Node]bool{}
		for _, spec := range file.Imports {
			imports[spec.Path] = true
		}
		ast.Inspect(file, func(node ast.Node) bool {
			lit, ok := node.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || imports[lit] {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil || !laneWordLaw.MatchString(text) {
				return true
			}
			if where, allowed := laneWordAllowed[text]; allowed && where == name {
				return true
			}
			t.Errorf("%s:%d says %q — the person-facing word for the machine behind a model is `provider` (issue #1023)",
				name, fset.Position(lit.Pos()).Line, text)
			return true
		})
	}
}
