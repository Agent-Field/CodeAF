package tui3

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ── A PLACE THAT DOES NOT SAY ITS OWN KEYS IS LYING WITH SOMEBODY ELSE'S ─────
//
// The search place had no `hint` of its own, so the router's default answered
// for it and its foot read `enter talk about it · alt+enter send it off as a
// task · alt+. for the map · tab next place` — six rows under the place's own
// body saying `enter opens the conversation at the matching turn.`, which is
// also what the manual says (places.md's search section). The spend place had
// the identical hole and hid `enter`, `→ b the limits` and its shift-arrow
// window behind the same wrong sentence.
//
// The seam was the default, not the two symptoms. [placeBase] no longer carries
// a `hint`, so a place without one does not compile — and this test says the
// same thing a second time, by name, so that a default quietly restored is
// caught by a failure message rather than by somebody eventually reading a
// frame.

// placeHandleNames is every type registered as a place, read out of the package
// source: each `place_<word>.go` calls `registerPlace(placeWord{})` in its
// `init`, and that call is the one list of what a place IS.
func placeHandleNames(t *testing.T) map[string]string {
	t.Helper()
	names := map[string]string{}
	fset := token.NewFileSet()
	for _, name := range placeSourceFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("could not parse %s: %v", name, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.Ident)
			if !ok || fn.Name != "registerPlace" || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			if id, ok := lit.Type.(*ast.Ident); ok {
				names[id.Name] = name
			}
			return true
		})
	}
	if len(names) != len(placeOrder) {
		t.Fatalf("found %d registered place handles in the source but %d places in placeOrder — %v",
			len(names), len(placeOrder), names)
	}
	return names
}

// placeMethodOwners is every type in this package that declares a method of the
// given name, by receiver type name.
func placeMethodOwners(t *testing.T, method string) map[string]string {
	t.Helper()
	owners := map[string]string{}
	fset := token.NewFileSet()
	for _, name := range placeSourceFiles(t) {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("could not parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || len(fn.Recv.List) != 1 || fn.Name.Name != method {
				continue
			}
			owners[receiverTypeName(fn.Recv.List[0].Type)] = name
		}
	}
	return owners
}

// receiverTypeName is the type a method hangs off, with any pointer star taken
// off it — `*tasksPlace` and `tasksPlace` are one type as far as a contract is
// concerned.
func receiverTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func TestEveryPlaceSaysItsOwnKeys(t *testing.T) {
	owners := placeMethodOwners(t, "hint")
	for handle, file := range placeHandleNames(t) {
		if _, ok := owners[handle]; !ok {
			t.Errorf("%s (registered in %s) has no hint method of its own, so its foot would be another place's sentence — "+
				"give it one beside the other five, the way placeSearch.hint and placeSpend.hint are written",
				handle, file)
		}
	}
	// AND THE DEFAULT MAY NOT COME BACK. A `hint` on placeBase makes every one of
	// the checks above pass while the frame still draws the router's line over a
	// room that never wrote one — which is exactly the state this wave found.
	if file, ok := owners["placeBase"]; ok {
		t.Errorf("placeBase declares a hint again (%s) — a place with no foot of its own must not compile, "+
			"let alone silently draw placeHintWords", file)
	}
}

// The search place's own foot, in both of the states `enter` means something
// different in. The router's default said `enter talk about it` over both, and
// with an empty box `enter` does nothing at all.
func TestTheSearchFootNamesTheKeyThatOpensAHit(t *testing.T) {
	a := newTestApp(nil)
	a.width, a.height = 120, 40

	// Nothing typed: the teaching page, where there is no row to stand on.
	resting := (placeSearch{}).hint(a)
	if resting != searchAskHint {
		t.Fatalf("the resting search foot drew %q, want %q", resting, searchAskHint)
	}
	if strings.Contains(resting, "talk about it") {
		t.Errorf("the search foot still carries the router's default clause: %q", resting)
	}

	// A hit under the cursor: enter opens it, and the foot says so in the words
	// the place's own body and the manual both use.
	a.search.reading = searchReading{query: "docker", hits: []searchHit{{title: "a chat", transcript: "/tmp/t.jsonl"}}}
	a.search.cursor = 0
	line := (placeSearch{}).hint(a)
	if line != searchHitHint {
		t.Fatalf("the search foot over a result drew %q, want %q", line, searchHitHint)
	}
	if !strings.Contains(line, "enter opens it") {
		t.Errorf("the search foot does not say what enter opens: %q", line)
	}
}

// The spend place names the two keys a person standing on a row would press,
// and the one it used to name — `enter talk about it` — meant something else.
func TestTheSpendFootNamesTheKeysAPersonWouldPress(t *testing.T) {
	a := newTestApp(nil)
	a.width, a.height = 160, 50

	// An empty ledger: no row, no verb, and the foot promises neither.
	bare := placeTailed((placeSpend{}).hint(a))
	if strings.Contains(bare, spendEnterWord) {
		t.Errorf("the spend foot promises %q over a ledger with no rows: %q", spendEnterWord, bare)
	}
	if bare != placeHintTail+" · esc" {
		t.Errorf("the spend foot over an empty ledger drew %q, want %q — the way out is said last and said once",
			bare, placeHintTail+" · esc")
	}

	// A row under the cursor: enter, the one verb, and the window.
	a.spend.stops = []spendStop{{ok: true}}
	a.spend.cursor = 0
	line := (placeSpend{}).hint(a)
	for _, want := range []string{spendEnterWord, spendVerbLead + "the limits"} {
		if !strings.Contains(line, want) {
			t.Errorf("the spend foot does not name %q — it drew %q", want, line)
		}
	}
	if strings.Contains(line, "talk about it") || strings.Contains(line, "send it off as a task") {
		t.Errorf("the spend foot still carries the router's default clauses: %q", line)
	}
}

// ── HINTS DROP WHOLE HINTS, NEVER SLICE ONE ─────────────────────────────────
//
// The composer's own line is ninety-two cells, so an eighty-column terminal drew
// `… · alt+. for the map · t…` and lost `tab next place` — the clause that tells
// a person how to leave — to a character ruler that has no idea what a clause
// is.
func TestAHintDropsWholeClausesAndKeepsTheWayOut(t *testing.T) {
	for _, room := range []int{78, 70, 60, 40, 30, 20} {
		line := hintFit(placeHintWords, room)
		if width := ansi.StringWidth(line); width > room {
			t.Errorf("at %d cells the foot drew %d: %q", room, width, line)
		}
		if !strings.Contains(line, placeHintTail) {
			t.Errorf("at %d cells the foot lost %q, which is the way out: %q", room, placeHintTail, line)
		}
		// EVERY CLAUSE STILL STANDING IS A WHOLE CLAUSE. What is shown at a narrow
		// width is a subset of what is shown at a wide one, never a prefix of a
		// word (rowfit.go's law 3).
		for _, clause := range strings.Split(line, railSep) {
			if !strings.Contains(placeHintWords, clause) {
				t.Errorf("at %d cells the foot drew a clause that is not one: %q (whole line %q)", room, clause, line)
			}
		}
	}

	// THE LADDER'S ORDER, stated. The clause nearest the way out goes first, the
	// clause about the row under the cursor goes last.
	at78 := hintFit(placeHintWords, 78)
	if strings.Contains(at78, "alt+. for the map") {
		t.Errorf("the map clause should be the first one dropped, and it is still there: %q", at78)
	}
	if !strings.Contains(at78, "enter talk about it") || !strings.Contains(at78, "alt+enter") {
		t.Errorf("at 78 cells only the map clause should be gone: %q", at78)
	}
	at40 := hintFit(placeHintWords, 40)
	if strings.Contains(at40, "alt+enter") {
		t.Errorf("at 40 cells the chord clause should be gone too: %q", at40)
	}
	if !strings.Contains(at40, "enter talk about it") {
		t.Errorf("at 40 cells the clause about the row under the cursor should still stand: %q", at40)
	}

	// A LINE THAT ALREADY FITS IS UNTOUCHED, byte for byte.
	if line := hintFit(placeHintWords, 200); line != placeHintWords {
		t.Errorf("a foot with room to spare was rewritten: %q", line)
	}

	// AND A PLACE'S OWN FOOT DEGRADES THE SAME WAY, with `tab next place` kept
	// ahead of the `esc` that follows it.
	tailed := placeTailed(searchHitHint)
	for _, room := range []int{70, 50, 34} {
		line := hintFit(tailed, room)
		if !strings.Contains(line, placeHintTail) {
			t.Errorf("at %d cells the search foot lost %q: %q", room, placeHintTail, line)
		}
		if ansi.StringWidth(line) > room {
			t.Errorf("at %d cells the search foot drew %d cells: %q", room, ansi.StringWidth(line), line)
		}
	}
}
