package manual

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// ── THE CHAT MAY NOT TEACH AN EXIT LADDER THE BINARY STOPPED USING ─────────
//
// There were THREE exit tables in this product and two of them meant opposite
// things by the same number. They were pulled onto one ladder in
// `cmd/aforge/envelope.go`, `docs/HEADLESS.md` was rewritten, and the terminal
// page in this corpus was written against the new one — and a FOURTH copy was
// left standing here, on `adaptive-runs`, still telling a person that `aforge
// exec` has "a six-rung ladder" and that "`5` is the one to watch for".
//
// That copy is the worst of the four, because this corpus is the ONLY
// authoritative source about aforge for the model: the running chat answers
// "what does exit 5 mean" out of these pages, and the answer it had was a
// number the binary no longer returns. A page that is complete, well-written
// and WRONG outranks the model's own uncertainty.
//
// SO THE RUNGS ARE READ OUT OF THE CODE. The page has to carry every number
// [exitLadder] actually publishes, spelled in the words that table spells it,
// and none of the sentences the retired six-code table was written in. Reading
// the composite literal with go/parser is also what puts this law on the
// pull-request gate — scripts/laws.sh finds the laws by that import.
func TestNoChatPageStillTeachesExecsRetiredExitCodes(t *testing.T) {
	rungs := exitRungsFromSource(t)
	if len(rungs) != 5 {
		t.Fatalf("cmd/aforge/envelope.go publishes %d rungs; the reader has stopped working", len(rungs))
	}
	pages := flatChatPages(t)

	// THE PAGE ABOUT `aforge exec` CARRIES THE LADDER THE BINARY HAS. This is
	// the half that discriminates: a page that simply deleted its stale
	// paragraph would satisfy any ban and leave the chat with nothing to say.
	const page = "adaptive-runs"
	text, ok := pages[page]
	if !ok {
		t.Fatalf("the chat corpus has no %s page", page)
	}
	for code, short := range rungs {
		if !strings.Contains(text, "`"+strconv.Itoa(code)+"`") {
			t.Errorf("%s never names exit `%d`, which is a rung `aforge exec` leaves on", page, code)
		}
		if !strings.Contains(text, short) {
			t.Errorf("%s does not say what exit %d means; envelope.go's own words for it are %q",
				page, code, short)
		}
	}
	// AND IT NAMES THE ESCAPE HATCH, because a person reading this page is
	// exactly the person whose script was written against the old numbers.
	if !strings.Contains(text, "AFORGE_EXIT_CODES=legacy") {
		t.Errorf("%s does not name AFORGE_EXIT_CODES=legacy, so a script pinned to exec's old "+
			"numbers is told they moved and not how to get them back", page)
	}

	// AND NO PAGE ANYWHERE STILL TEACHES THE OLD TABLE AS A LIVE FACT. These
	// are the sentences the retired six-code ladder was written in, quoted from
	// the page that carried them.
	for _, retired := range []string{
		"exit code is a six-rung ladder",
		"`5` is the one to watch for",
		"`6` it stopped with nothing to say",
		"`2` the token budget ran out · `3` the turn cap came first",
	} {
		for name, body := range pages {
			if strings.Contains(body, retired) {
				t.Errorf("%s still teaches exec's retired exit table: %q — the binary returns "+
					"0/1/2/3/4 now, and the old numbers only come back under AFORGE_EXIT_CODES=legacy",
					name, retired)
			}
		}
	}
}

// exitRungsFromSource is `exitLadder` read out of cmd/aforge/envelope.go: the
// number each rung publishes, against the clause that table spells it with.
//
// It reads the code names out of the const block first, because the ladder
// itself is written with them (`Code: exitDone`) and a rung's number is the one
// fact a page most needs to have right.
func exitRungsFromSource(t *testing.T) map[int]string {
	t.Helper()

	const path = "../../cmd/aforge/envelope.go"
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}

	codes := map[string]int{}
	rungs := map[int]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok {
			return true
		}
		// `exitDone exitStatus = 0`, and the four rungs beside it.
		if len(spec.Names) == 1 && len(spec.Values) == 1 {
			if number, err := strconv.Atoi(literalOf(spec.Values[0])); err == nil {
				codes[spec.Names[0].Name] = number
			}
		}
		if len(spec.Names) != 1 || spec.Names[0].Name != "exitLadder" || len(spec.Values) != 1 {
			return true
		}
		table, ok := spec.Values[0].(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, element := range table.Elts {
			row, ok := element.(*ast.CompositeLit)
			if !ok {
				continue
			}
			code, short, named := -1, "", false
			for _, field := range row.Elts {
				pair, ok := field.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := pair.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "Code":
					if name, ok := pair.Value.(*ast.Ident); ok {
						code, named = codes[name.Name], true
					}
				case "Short":
					short = literalOf(pair.Value)
				}
			}
			if named && short != "" {
				rungs[code] = short
			}
		}
		return true
	})
	return rungs
}

// literalOf is one unquoted literal, or "" for anything that is not one.
func literalOf(expression ast.Expr) string {
	literal, ok := expression.(*ast.BasicLit)
	if !ok {
		return ""
	}
	if literal.Kind != token.STRING {
		return literal.Value
	}
	word, err := strconv.Unquote(literal.Value)
	if err != nil {
		return ""
	}
	return word
}
