package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// THE RULE THIS READS THE TREE FOR: stdout is the answer — the deliverable, the
// JSON, the rows, the table, the thing a script captures — and everything a
// person reads ABOUT the run is an aside and goes to stderr (streams.go).
//
// It is structural rather than a grep because the doors that broke it were not
// spelled alike: `plan run` printed a `goal:` preamble, `logs` printed a path
// header, and `cache clean` printed its QUESTION into the stream a script was
// capturing — which is the worst of the three, because a `tee` of it hands the
// person a blank terminal waiting for a word they cannot see, and puts the
// question in the data file. So the test looks at the SHAPE of what is written
// and not at its words, and it reads string literals out of the syntax tree,
// never the source text: a sentence quoted in a comment about the old defect
// must not be able to satisfy or fail it.
//
// Two shapes, and each is commentary by construction:
//
//  1. A PROMPT — a literal that ends on a colon, a question mark, or a
//     bracketed choice like `[y/N] `, with no newline after it. Nothing that is
//     an answer stops mid-line waiting.
//  2. A LABEL COLUMN — a line of a literal that opens with a lower-case word, a
//     colon, and two or more spaces of padding to align a value after it. That
//     is the preamble style every headless door writes its `goal:`,
//     `workspace:`, `models:` and `panel:` lines in, and a table of results is
//     not written that way.
//
// THE BRACKETED CHOICE WAS THE HOLE ROW 28 FELL THROUGH, and it is worth saying
// exactly how, because the report guessed a different mechanism. `aforge
// rebuild` asked its question through a writer that arrives as a PARAMETER —
// and that was never the problem: the parameter is named `output`, which is one
// of [answerWriters], so the call WAS scanned. What the scan could not see was
// the SHAPE. The question mark ended its own line, and the half that actually
// waits for a keystroke ends `[y/N] ` — no colon, no question mark, nothing the
// two rules above recognise. So a prompt written the way every yes/no prompt in
// the world is written was invisible, and the rule now names it.
func TestNoDoorPrintsItsCommentaryToStdout(t *testing.T) {
	fileSet := token.NewFileSet()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, name := range names {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := parser.ParseFile(fileSet, name, source, 0)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			text, endsTheLine, ok := stdoutLiteral(call)
			if !ok {
				return true
			}
			found++
			if reason := commentaryShape(text, endsTheLine); reason != "" {
				position := fileSet.Position(call.Pos())
				t.Errorf("%s:%d writes %s to STDOUT: %q\n"+
					"stdout is the answer a script captures; this is an aside and belongs on stderr — "+
					"write it to `aside` (streams.go) or to os.Stderr",
					filepath.Base(position.Filename), position.Line, reason, text)
			}
			return true
		})
	}
	// A scan that matched no writes at all would pass forever while saying
	// nothing, which is the failure mode of every structural test: if the
	// package stops spelling its stdout writes the way this reads them, the
	// test must say so rather than go quiet.
	if found < 40 {
		t.Fatalf("the scan found only %d writes to stdout in cmd/aforge, which is too few to be reading the package — "+
			"stdoutLiteral has stopped recognising how these doors write", found)
	}
}

// answerWriters are the names this package gives the stream a script captures.
// A write through one of them is a write to stdout as far as the rule is
// concerned, whatever the caller passed in a test.
var answerWriters = map[string]bool{"output": true, "out": true, "stdout": true, "answer": true}

// stdoutLiteral reports the first string literal a call writes to stdout,
// whether the call ENDS THE LINE by itself, and whether it writes to stdout at
// all. `fmt.Print`, `fmt.Printf` and `fmt.Println` are stdout by definition; the
// `Fprint` family is stdout when its writer is os.Stdout or one of
// [answerWriters].
//
// The line-ending answer is what tells a prompt from a heading: `Println("panel:")`
// is a section title in an answer, and a question is written with `Print` or
// `Printf` PRECISELY BECAUSE the cursor has to stay on the line for the person
// to type on.
func stdoutLiteral(call *ast.CallExpr) (text string, endsTheLine, writes bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false, false
	}
	if packageName, ok := selector.X.(*ast.Ident); !ok || packageName.Name != "fmt" {
		return "", false, false
	}
	args := call.Args
	name := selector.Sel.Name
	switch name {
	case "Print", "Printf", "Println":
	case "Fprint", "Fprintf", "Fprintln":
		if len(args) == 0 || !writesToStdout(args[0]) {
			return "", false, false
		}
		args = args[1:]
	default:
		return "", false, false
	}
	endsTheLine = strings.HasSuffix(name, "ln")
	for _, argument := range args {
		if text, ok := literalText(argument); ok {
			return text, endsTheLine, true
		}
	}
	return "", endsTheLine, true
}

// unreadable stands for a piece of a concatenated string that is not a literal
// — a constant, a variable, a call. It is one byte that can never be a colon, a
// question mark or a newline, so what surrounds it is still read correctly and
// nothing about it can be mistaken for the shape of a sentence.
const unreadable = "\x00"

// literalText reads a string argument that may have been BUILT rather than
// written: `\x60Type "\x60 + cacheCleanWord + \x60" to delete it: \x60` is one
// sentence spelled as three operands, and reading only the first of them was a
// hole this test fell straight through — the whole prompt sat on stdout and the
// scan went green.
func literalText(expr ast.Expr) (string, bool) {
	switch node := expr.(type) {
	case *ast.BasicLit:
		if node.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(node.Value)
		if err != nil {
			return "", false
		}
		return text, true
	case *ast.BinaryExpr:
		if node.Op != token.ADD {
			return "", false
		}
		left, leftOK := literalText(node.X)
		right, rightOK := literalText(node.Y)
		if !leftOK && !rightOK {
			return "", false
		}
		if !leftOK {
			left = unreadable
		}
		if !rightOK {
			right = unreadable
		}
		return left + right, true
	case *ast.ParenExpr:
		return literalText(node.X)
	}
	return "", false
}

// writesToStdout reads the writer a Fprint call was handed.
func writesToStdout(writer ast.Expr) bool {
	switch target := writer.(type) {
	case *ast.Ident:
		return answerWriters[target.Name]
	case *ast.SelectorExpr:
		packageName, ok := target.X.(*ast.Ident)
		return ok && packageName.Name == "os" && target.Sel.Name == "Stdout"
	}
	return false
}

// commentaryShape names which of the two shapes a literal is, and is empty when
// the literal is an answer.
func commentaryShape(text string, endsTheLine bool) string {
	if trimmed := strings.TrimRight(text, " \t"); trimmed != "" && !endsTheLine && !strings.HasSuffix(text, "\n") {
		if last := trimmed[len(trimmed)-1]; last == ':' || last == '?' {
			return "a question"
		}
		if bracketedChoice(trimmed) {
			return "a question"
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if labelColumn(line) {
			return "a labelled preamble line"
		}
	}
	return ""
}

// bracketedChoice reports the shape every yes/no prompt ends in: a short
// bracketed list of the answers, with the cursor left after it. `[y/N] `,
// `[y/n] `, `[Y/n/a] `. It is bounded and has to contain a separator, so a
// sentence that merely ends in a bracket — a citation, an index, a note in
// square brackets — is not mistaken for a question nobody can see.
func bracketedChoice(trimmed string) bool {
	if !strings.HasSuffix(trimmed, "]") {
		return false
	}
	open := strings.LastIndex(trimmed, "[")
	if open < 0 {
		return false
	}
	inside := trimmed[open+1 : len(trimmed)-1]
	if inside == "" || len(inside) > 8 || strings.ContainsAny(inside, " \t") {
		return false
	}
	return strings.Contains(inside, "/")
}

// labelColumn reports whether a line opens `word:` followed by the padding that
// aligns a value under the label above it — `goal:      `, `workspace: `,
// `models:    `. Two spaces or more is what makes it a column rather than
// ordinary prose with a colon in it.
func labelColumn(line string) bool {
	colon := strings.Index(line, ":")
	if colon <= 0 || colon+2 >= len(line) {
		return false
	}
	label := line[:colon]
	for _, letter := range label {
		if (letter < 'a' || letter > 'z') && letter != ' ' {
			return false
		}
	}
	return strings.HasPrefix(line[colon+1:], "  ")
}

// captureAside points the commentary stream at a buffer for one test, so what a
// door says ABOUT a run can be read back separately from its answer.
func captureAside(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := aside
	aside = &buffer
	t.Cleanup(func() { aside = previous })
	return &buffer
}

// Row 27. `aforge logs | grep -c .` counted one line too many for as long as
// the path was the first thing on stdout, and the flag that suppressed it under
// --json showed the author already knew: commentary does not belong in a stream
// another program reads. The path is still printed — it is genuinely useful —
// just not where the rows are.
func TestTheLogsPathHeaderIsAnAsideAndNotTheFirstRow(t *testing.T) {
	for _, mode := range [][]string{{"--tail", "2"}, {"--json", "--tail", "2"}} {
		t.Run(strings.Join(mode, " "), func(t *testing.T) {
			commentary := captureAside(t)
			path := fixtureLog(t)
			var answer strings.Builder
			if err := runLogsWith(mode, &answer, path, stoppedClock(t)); err != nil {
				t.Fatal(err)
			}
			first, _, _ := strings.Cut(answer.String(), "\n")
			if strings.Contains(first, path) {
				t.Fatalf("the first line of stdout is the log's path, not a call:\n%s", first)
			}
			if !strings.Contains(commentary.String(), path) {
				t.Fatalf("the log's path was not written to the aside either — it should be there, and only there:\n%q",
					commentary.String())
			}
		})
	}
}
