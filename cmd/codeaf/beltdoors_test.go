// The shell doors codeaf grew for the bash belt — patch, doc, web fetch, web
// search and image — and the plandb subcommand beside them answer the same
// vocabulary laws the rest of the binary does: `-h` is not a failure, the page
// names the door it is about and every flag that door takes, and none of it
// explains itself in the machinery words COMMANDS.md §3 rules out.
//
// They are read through the BUILT BINARY, because three of the six things under
// test — the exit code, what is on stdout, what is on stderr — only exist for a
// real process, and because plandb settles it: its page is written by
// internal/plandb's own runner onto its own writer, which no seam in this
// package can reach. The build is the slow part and `go test -short` skips it,
// the way the exec smoke tests already do.
package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"
)

// helpLineCap is the most lines `codeaf --help` may run to: the page was cut
// down to the commands, grouped, and five examples, and every line past this is
// a command that scrolled off a person's screen.
const helpLineCap = 110

// doorsOffThePage are the command words main.go dispatches that `codeaf --help`
// deliberately does NOT name, so the check below does not demand a line for
// them:
//
//   - `engine` and `tick` are machinery a surface dials, not things a person
//     runs by hand; main.go says so where it dispatches them and
//     internal/manual's running-from-the-terminal page says so again.
//   - `show` and `revise` are the old top-level spellings of `plan show` and
//     `plan revise`; the page names the commands they became.
//   - `help` is the alias for `--help` itself.
var doorsOffThePage = map[string]bool{
	"engine": true, "tick": true, "show": true, "revise": true, "help": true,
}

// THE FIRST GESTURE ON AN UNFAMILIAR DOOR IS `-h`, and it must not be a
// failure: exit 0, the door's own name on the first line, every flag that door
// defines written down, and not one machinery word in the sentences.
func TestDoorsAnswerHelpWithTheirNameFlagsAndVocabulary(t *testing.T) {
	binary := buildCodeafStamped(t, "")
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"CODEAF_HOME=" + home,
		"CODEAF_PROFILE_DIR=" + home,
	}
	// Which flags each door function declares, read from the tree rather than
	// typed here, so a knob added to a door turns this red until it is written
	// down on the page.
	declared := map[string][]string{}
	for _, flag := range printedFlags(t) {
		declared[flag.door] = append(declared[flag.door], flag.name)
	}
	for _, door := range []struct {
		name  string
		args  []string
		funcs []string
	}{
		{"patch", []string{"patch", "-h"}, []string{"runPatch"}},
		{"doc", []string{"doc", "-h"}, []string{"runDoc"}},
		{"web fetch", []string{"web", "fetch", "-h"}, []string{"runWebFetch"}},
		{"web search", []string{"web", "search", "-h"}, []string{"runWebSearch"}},
		{"image", []string{"image", "-h"}, []string{"runImage"}},
		// plandb's flags belong to internal/plandb's runner, which is the code
		// this page IS — cmd/codeaf declares none for the door and its scanner
		// alone owns the grammar, so there is nothing here to read against it.
		{"plandb", []string{"plandb", "-h"}, nil},
	} {
		t.Run(door.name, func(t *testing.T) {
			stdout, stderr, code := runSmoke(t, binary, env, "", door.args...)
			if code != 0 {
				t.Fatalf("`codeaf %s` left with %d, want 0 — asking for help is not a failure\n%s",
					strings.Join(door.args, " "), code, stderr)
			}
			page := stdout
			first, _, _ := strings.Cut(page, "\n")
			if !strings.Contains(first, door.name) {
				t.Fatalf("the first line of `codeaf %s` never names the door:\n%s",
					strings.Join(door.args, " "), first)
			}
			for _, fn := range door.funcs {
				for _, name := range declared[fn] {
					// One dash for a single letter, two for a word — the spelling
					// [flagRows] writes and the surface teaches.
					spelling := "--" + name
					if len(name) == 1 {
						spelling = "-" + name
					}
					if !strings.Contains(page, spelling) {
						t.Errorf("`codeaf %s` never writes %s, which %s defines:\n%s",
							strings.Join(door.args, " "), spelling, fn, page)
					}
				}
			}
			lower := strings.ToLower(page)
			for _, word := range machineryVocabulary {
				if strings.Contains(lower, word) {
					t.Errorf("`codeaf %s` explains itself with the machinery word %q:\n%s",
						strings.Join(door.args, " "), word, page)
				}
			}
		})
	}
}

// A DOOR TYPED WITH A REQUIRED WORD MISSING REFUSES IN ONE SENTENCE AND LEAVES
// WITH 2 — the same shape `codeaf update` gives a call it cannot carry out, and
// the exit a script branches on for "you typed it short". Nothing reaches
// stdout, and the sentence names what was missing, because the whole of what the
// person gets is that one line.
func TestDoorsRefuseAWrongCallWithOneSentenceAndExitTwo(t *testing.T) {
	for _, door := range []struct {
		name    string
		run     func([]string) error
		missing string
	}{
		{"patch", runPatch, "file"},
		{"doc", runDoc, "document"},
		{"web", runWeb, "search"},
		{"image", runImage, "draw"},
	} {
		t.Run(door.name, func(t *testing.T) {
			out, errs := captureUsage(t)
			if code := exitCodeOf(door.run(nil)); code != 2 {
				t.Fatalf("`codeaf %s` with nothing left with %d, want 2", door.name, code)
			}
			if out.Len() != 0 {
				t.Fatalf("`codeaf %s` with nothing wrote to stdout, where the answer goes:\n%s",
					door.name, out.String())
			}
			said := strings.TrimRight(errs.String(), "\n")
			if lines := strings.Split(said, "\n"); len(lines) != 1 {
				t.Fatalf("`codeaf %s` with nothing said %d lines, want one sentence:\n%s",
					door.name, len(lines), errs.String())
			}
			if !strings.Contains(said, door.missing) {
				t.Fatalf("`codeaf %s` with nothing never names what was missing (%q):\n%s",
					door.name, door.missing, said)
			}
		})
	}
}

// THE PAGE NAMES EVERY DOOR, and stays inside its line cap doing it. The door
// list is read out of main.go's own dispatch rather than kept here, so a door
// added to the binary and forgotten on the page turns this red and is named in
// the failure — the plandb line underneath was added for exactly this.
func TestUsageNamesEveryDispatchedDoorAndKeepsTheLineCap(t *testing.T) {
	for _, door := range dispatchedDoors(t) {
		if !strings.Contains(usageText, "codeaf "+door) {
			t.Errorf("main.go dispatches `codeaf %s` and `codeaf --help` never names it", door)
		}
	}
	if lines := strings.Count(usageText, "\n") + 1; lines > helpLineCap {
		t.Errorf("`codeaf --help` is %d lines after naming every door, past the %d-line cap", lines, helpLineCap)
	}
}

// dispatchedDoors reads main.go's `switch os.Args[1]` and answers every command
// word it dispatches, minus the two things that are not doors: a flag spelling
// (a word opening with a dash) and the words [doorsOffThePage] exempts.
func dispatchedDoors(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	seen := map[string]bool{}
	var doors []string
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" {
			return true
		}
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			switched, ok := inner.(*ast.SwitchStmt)
			if !ok || switched.Tag == nil || !isCommandSwitch(switched.Tag) {
				return true
			}
			for _, stmt := range switched.Body.List {
				clause, ok := stmt.(*ast.CaseClause)
				if !ok {
					continue
				}
				for _, expr := range clause.List {
					literal, ok := expr.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING {
						continue
					}
					word, unquoteErr := strconv.Unquote(literal.Value)
					if unquoteErr != nil || seen[word] || doorsOffThePage[word] || strings.HasPrefix(word, "-") {
						continue
					}
					seen[word] = true
					doors = append(doors, word)
				}
			}
			return true
		})
		return true
	})
	if len(doors) == 0 {
		t.Fatal("found no dispatch switch in main.go, so this check read nothing")
	}
	// A parse that latched onto the wrong switch would pass vacuously, so demand
	// the shape: the conversation, a belt door and plandb are on it.
	for _, must := range []string{"chat", "plandb", "patch", "web"} {
		if !seen[must] {
			t.Fatalf("the dispatch read from main.go has no %q door, so it read the wrong switch", must)
		}
	}
	return doors
}

// isCommandSwitch reports whether a switch's tag is `os.Args[1]`, the one
// switch in [run] that decides which door opens.
func isCommandSwitch(tag ast.Expr) bool {
	index, ok := tag.(*ast.IndexExpr)
	if !ok {
		return false
	}
	selector, ok := index.X.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Args" {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == "os"
}
