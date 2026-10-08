package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// factoryStripOf is the peek's strip for the item under the cursor, whole.
func factoryStripOf(t *testing.T, a *app) string {
	t.Helper()
	it, ok := a.factoryCursorItem()
	if !ok {
		t.Fatal("no item under the cursor")
	}
	return a.factoryActionWords(it)
}

// factoryVerbsShown is the verbs a person sees for the item under the
// cursor: the peek's strip on the floor, the pane's action line on the page.
func factoryVerbsShown(t *testing.T, a *app) string {
	t.Helper()
	it, ok := a.factoryCursorItem()
	if !ok {
		t.Fatal("no item under the cursor")
	}
	if a.fp.open {
		return ansi.Strip(a.factoryPageAction(it, 400))
	}
	return a.factoryActionWords(it)
}

// factorySheetText is the `?` sheet's rows as `key word` clauses, in order.
func factorySheetText(a *app) string {
	var out []string
	for _, g := range a.factorySheet() {
		for _, r := range g.rows {
			out = append(out, factoryHintClause(r.key, r.word))
		}
	}
	return strings.Join(out, " · ")
}

// factoryOldVerbWords are the words the vocabulary retired (owner decision,
// 2026-10-08). A hint clause spelling one of them, `<key> <word>` or the word
// alone, is the vocabulary drifting back.
var factoryOldVerbWords = []string{"plan first", "talk", "mark", "sign off", "send back", "check again", "cap", "effort", "hide", "read again", "launch marked"}

// THE VOCABULARY CANNOT DRIFT BACK. Every string literal in the factory's own
// sources (factory_*.go, place_factory.go and factoryitemcard.go, not the
// words file and not the tests) is read as a key line's clauses, and a clause
// that is an old verb word, alone or after its key, fails the build naming the
// file and the line. The words live in factory_words.go and nowhere else.
func TestFactoryWordsAreTheVocabulary(t *testing.T) {
	files, _ := filepath.Glob("factory_*.go")
	files = append(files, "place_factory.go", "factoryitemcard.go")
	fset := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || path == "factory_words.go" {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			// A SEAM'S DOOR IS NAMED BY THE SEAM (`Has("talk")`), which is
			// the engine's word and never drawn.
			if call, ok := n.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Has" {
					return false
				}
			}
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, clause := range strings.Split(s, "·") {
				clause = strings.TrimSpace(clause)
				for _, old := range factoryOldVerbWords {
					if factoryClauseIs(clause, old) {
						t.Errorf("%s: %q spells the retired verb word %q; read it from factory_words.go", fset.Position(lit.Pos()), s, old)
					}
				}
			}
			return true
		})
	}
}

// factoryClauseIs says whether a clause is the word alone, or one key (a
// letter, `space`, `enter`, `1-9`) and the word.
func factoryClauseIs(clause, word string) bool {
	if clause == word {
		return true
	}
	k, rest, ok := strings.Cut(clause, " ")
	if !ok || rest != word {
		return false
	}
	switch k {
	case "space", "enter", "1-9":
		return true
	}
	return len([]rune(k)) == 1
}

// THE HINTS ARE SHORT (owner decision, 2026-10-08): at every state the peek's
// strip and the item page's action line name at most five clauses, and the
// floor's bottom bar is its navigation set and nothing of the row's.
func TestFactoryHintsAreShort(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.width = 400
	nav := map[string]bool{
		factoryHintClause(keyNew, wordNew): true, factoryHintClause(keyRepos, wordRepos): true,
		foremanHintWord: true, factoryHintClause(keyFilter, wordFilter): true,
		placeHintTail: true, factoryHintClause(keyBack, wordBack): true, factoryHintClause(keyBack, wordClear): true,
		factoryHintClause(keySheet, wordSheet): true,
	}
	seen := map[factory.State]bool{}
	for _, it := range a.fp.snap.Items {
		if !factoryOnFloor(it) {
			continue
		}
		factoryOn(t, a, it.ID)
		seen[it.State] = true
		if strip := a.factoryVerbRail(it); len(strip) > factoryStripMost {
			t.Errorf("%s (%s): the strip names %d clauses: %q", it.Ref(), it.State, len(strip), strip)
		}
		for _, clause := range strings.Split(placeTailed((placeFactory{}).hint(a)), " · ") {
			if !nav[clause] {
				t.Errorf("%s (%s): the bottom bar names %q, which is not navigation", it.Ref(), it.State, clause)
			}
		}
		drive(t, a, key("enter"))
		if !a.fp.open {
			t.Fatalf("enter on %s did not open its page", it.Ref())
		}
		if words := a.factoryPageAction(it, 400); strings.Count(words, " · ")+1 > factoryStripMost {
			t.Errorf("%s (%s): the item page's action line names more than five: %q", it.Ref(), it.State, words)
		}
		hint := (placeFactory{}).hint(a)
		if !strings.HasPrefix(hint, factoryHintClause(keyWalk, wordWalkStages)) || !strings.HasSuffix(hint, factoryHintClause(keyBack, wordFloorName)+" · "+factoryHintClause(keySheet, wordSheet)) || strings.Count(hint, " · ") > 3 {
			t.Errorf("%s (%s): the item page's bottom line is %q", it.Ref(), it.State, hint)
		}
		drive(t, a, key("esc"))
	}
	for _, st := range []factory.State{factory.StateNew, factory.StateRunning, factory.StateNeedsYou, factory.StateLanded} {
		if !seen[st] {
			t.Errorf("the fixture has no %s item to read the strip of", st)
		}
	}
}

// `?` IS THE WHOLE KEYBOARD, on the floor and on the item page: it opens the
// sheet, grouped do / set / also / move with each key beside its word, the
// hint says how to close it, and `esc` closes it without leaving the place.
func TestFactoryQuestionMarkOpensTheKeySheet(t *testing.T) {
	f := &factoryFake{}
	a := factoryVerbLab(t, f)
	a.width, a.height = 160, 40
	factoryOn(t, a, 8)
	for _, page := range []bool{false, true} {
		if page {
			drive(t, a, key("enter"))
		}
		drive(t, a, key("?"))
		if !a.fp.keys {
			t.Fatalf("? (item page %v) did not open the sheet", page)
		}
		text := factoryFrameText(a)
		for _, want := range []string{wordGroupDo, wordGroupSet, wordGroupMove, wordRun, wordAskAt, wordBudget, wordThinking, wordStages} {
			if !strings.Contains(text, want) {
				t.Errorf("the sheet (item page %v) does not say %q:\n%s", page, want, text)
			}
		}
		if hint := (placeFactory{}).hint(a); hint != "esc close" {
			t.Errorf("the sheet's hint is %q", hint)
		}
		drive(t, a, key("r"))
		if got := f.said(); len(got) != 0 {
			t.Fatalf("a key on the sheet reached the row underneath: %v", got)
		}
		drive(t, a, key("esc"))
		if a.fp.keys || !a.at(pageFactory) || a.fp.open != page {
			t.Fatalf("esc did not put the sheet away and stay where it was (item page %v)", page)
		}
	}
}

// `t` CYCLES WHERE THE RUN ASKS: plan, pull request, never, and round; the
// stored gate keeps the recipe file's words.
func TestFactoryAskMeAtCycles(t *testing.T) {
	for _, c := range []struct {
		from factory.Gate
		next string
	}{
		{factory.GatePlan, "SetGate(8,ship)"},
		{factory.GateShip, "SetGate(8,none)"},
		{factory.GateNone, "SetGate(8,plan)"},
	} {
		if got := "SetGate(8," + string(factoryNextGate(c.from)) + ")"; got != c.next {
			t.Errorf("t on %s asks %s, want %s", c.from, got, c.next)
		}
	}
	for g, want := range map[factory.Gate]string{factory.GatePlan: "ask me at plan", factory.GateShip: "ask me at pull request", factory.GateNone: "ask me at never"} {
		if got := factoryAskAtWords(g); got != want {
			t.Errorf("gate %s reads %q, want %q", g, got, want)
		}
	}
}
