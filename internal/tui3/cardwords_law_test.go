package tui3

// cardwords_law_test.go is the law that a watch is said one way everywhere
// (wave 5 review of ruling R4).
//
// A FILE WATCH WITH A CONDITION RUNS ONLY WHEN THE CONDITION SAYS YES, and its
// `When.Words` — the model's reading of the person's cadence — says "whenever a
// file changes" all the same. [standing.When.CardWords] is the one sentence
// that names the condition, and a surface that reads the words instead tells
// the person a promise the item does not keep, while the terminal, reading the
// record, tells them the truth. So every read of `When.Words` outside the store
// is refused here, writes excepted, and the reads still owed are listed by
// file and function with who owes them. The list only shrinks: a line whose
// read is gone fails too, so it is deleted in the change that fixes it.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// cardWordsRoots are the packages that draw or say an item: every surface, the
// terminal, and the engine that answers the model.
var cardWordsRoots = []string{".", "../tui", "../head", "../resident", "../session", "../../cmd/aforge"}

// cardWordsOwed are the reads the law still forgives, keyed "package/file:func",
// each with who owes the switch. It only shrinks.
var cardWordsOwed = map[string]string{}

// cardWordsExempt are the reads that are not a surface saying what wakes an
// item, and never will be, each with why.
var cardWordsExempt = map[string]string{
	"session/tools_standing.go:standingNamed": "matching the words a person used for an item against the words it was made with — a search, and the words are what they would remember",
}

func TestEverySurfaceSaysAWatchFromItsCardWords(t *testing.T) {
	found := map[string]bool{}
	for _, root := range cardWordsRoots {
		files, err := filepath.Glob(filepath.Join(root, "*.go"))
		if err != nil {
			t.Fatal(err)
		}
		pkg := filepath.Base(filepath.Clean(root))
		if root == "." {
			pkg = "tui3"
		}
		for _, file := range files {
			if strings.HasSuffix(file, "_test.go") {
				continue
			}
			for _, read := range whenWordsReads(t, file) {
				key := pkg + "/" + filepath.Base(file) + ":" + read.fn
				found[key] = true
				_, owed := cardWordsOwed[key]
				if _, exempt := cardWordsExempt[key]; !owed && !exempt {
					t.Errorf("%s reads When.Words to say what wakes an item; say item.When.CardWords() so a condition is said as the terminal says it", read.at)
				}
			}
		}
	}
	var gone []string
	for _, ledger := range []map[string]string{cardWordsOwed, cardWordsExempt} {
		for key := range ledger {
			if !found[key] {
				gone = append(gone, key)
			}
		}
	}
	sort.Strings(gone)
	for _, key := range gone {
		t.Errorf("%s no longer reads When.Words; delete its line from the ledger", key)
	}
}

// whenWordsRead is one read of `….When.Words`: where, and in which function.
type whenWordsRead struct{ at, fn string }

// whenWordsReads finds every `….When.Words` in a file that is not the target
// of an assignment.
func whenWordsReads(t *testing.T, file string) []whenWordsRead {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, 0)
	if err != nil {
		t.Fatal(err)
	}
	var reads []whenWordsRead
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		written := map[ast.Node]bool{}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if assign, ok := node.(*ast.AssignStmt); ok {
				for _, target := range assign.Lhs {
					written[target] = true
				}
			}
			return true
		})
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Words" || written[sel] {
				return true
			}
			if inner, ok := sel.X.(*ast.SelectorExpr); ok && inner.Sel.Name == "When" {
				reads = append(reads, whenWordsRead{at: fset.Position(sel.Pos()).String(), fn: fn.Name.Name})
			}
			return true
		})
	}
	return reads
}

// A CONDITIONED WATCH READS THE SAME ON HOME AS AT THE TERMINAL: the row's
// tail and the roll-up of one never looked at are the card's words.
func TestHomeSaysAConditionedWatchAsTheTerminalDoes(t *testing.T) {
	item := standing.Item{When: standing.When{Kind: standing.WhenFile, Glob: "inbox/clients/**/*",
		Words: "whenever a file changes inside inbox/clients/", Hint: "a client is waiting on a quote"}}
	want := item.When.CardWords()
	if !strings.Contains(want, "only when: a client is waiting on a quote") {
		t.Fatalf("the card words do not name the condition: %q", want)
	}
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	if got := standWhenClause(item, now); got != want {
		t.Errorf("home's row tail reads %q, the terminal %q", got, want)
	}
	if got := standRollup(StandingItemView{Item: item}, now); got != want {
		t.Errorf("home's roll-up reads %q, the terminal %q", got, want)
	}
}

// A WAIT AFTER FAILED CHECKS IS NOT AN APPOINTMENT. Home said "in 38m" for a
// watch whose judge had no key, and sorted it ahead of real appointments; it
// says what is true of it instead, and sorts with the watches that have none.
func TestHomeDoesNotShowAFailingWatchAsAnAppointment(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	failing := standing.Item{ID: "failing", When: standing.When{Kind: standing.WhenFile, Glob: "inbox/*"},
		FailedChecks: 3, NextDue: now.Add(38 * time.Minute)}
	if got := standWhenClause(failing, now); got != standing.CouldNotCheck {
		t.Errorf("a failing watch's tail reads %q", got)
	}
	due := standing.Item{ID: "due", When: standing.When{Kind: standing.WhenEvery, Every: "0 11 * * *"}, NextDue: now.Add(2 * time.Hour)}
	views := []StandingItemView{{Item: failing}, {Item: due}}
	standByNextDue(views)
	if views[0].Item.ID != "due" {
		t.Errorf("a failing watch sorted ahead of an appointment: %s first", views[0].Item.ID)
	}
}
