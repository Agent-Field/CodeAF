package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/delegate/builtin"
	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
)

// carryFake puts the fake program on the build's list for one test.
func carryFake(t *testing.T) {
	t.Helper()
	restore := builtin.Override([]delegate.Delegate{fakeCarriedProgram()})
	t.Cleanup(restore)
}

// EVERY CARRIED PROGRAM IS ON THE FRONT PAGE, inside the page's two laws: its
// own group after the work you hand codeaf, its line and its summary, eighty
// cells at most, and the whole page still inside its line cap.
func TestTheFrontPageListsEveryCarriedProgramInsideTheLaws(t *testing.T) {
	carryFake(t)
	out, errs := captureUsage(t)
	if err := usage(nil); err != nil {
		t.Fatal(err)
	}
	printed := out.String()
	for _, want := range []string{carriedHeading, "  codeaf " + fakeCarried + ` "<brief>"`, "a program the tests carry"} {
		if !strings.Contains(printed, want) {
			t.Fatalf("`codeaf --help` does not carry %q:\n%s", want, printed)
		}
	}
	group, work, look := strings.Index(printed, carriedHeading), strings.Index(printed, "Hand it work"), strings.Index(printed, "Look at what happened")
	if !(work < group && group < look) {
		t.Fatalf("the carried group is at %d, want it between the work group (%d) and what happened (%d)", group, work, look)
	}
	pageFits(t, printed, 1)
	if errs.Len() != 0 {
		t.Fatalf("`codeaf --help` wrote to stderr:\n%s", errs.String())
	}
	// A BUILD THAT CARRIES NOTHING DRAWS NO HEADING OVER NOTHING.
	restore := builtin.Override(nil)
	defer restore()
	if page := frontPage(); strings.Contains(page, carriedHeading) || page != usageText {
		t.Fatal("a build that carries no program still draws the carried group")
	}
}

// AND THE PAGE THIS BUILD REALLY PRINTS keeps the same laws with the programs
// it really carries — which is where a real program's long summary would show.
func TestTheFrontPageFitsWithTheProgramsThisBuildCarries(t *testing.T) {
	out, _ := captureUsage(t)
	if err := usage(nil); err != nil {
		t.Fatal(err)
	}
	pageFits(t, out.String(), len(builtin.All()))
	for _, program := range builtin.All() {
		if !strings.Contains(out.String(), "codeaf "+program.Name+" ") {
			t.Errorf("`codeaf --help` never names `codeaf %s`, a program this build carries", program.Name)
		}
	}
}

func TestSeniorDevShellFirstLineNamesItsEffectiveCeiling(t *testing.T) {
	program := fakeCarriedProgram()
	program.Name = "senior-dev"
	for _, tc := range []struct {
		line []string
		want string
	}{
		{[]string{"repair"}, (delegate.Ceilings{}).SeniorDevDefaults().Summary()},
		{[]string{"--max-cost", "2", "--max-hours", "0.5", "repair"}, "up to $2.00 and 30m"},
	} {
		inv, err := delegate.Parse(program, tc.line, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		newCarriedView(&out, inv, t.TempDir()).begin()
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("first line %q lacks %q", out.String(), tc.want)
		}
	}
}

// carriedPageLines is what the carried group may cost the front page on top
// of [helpLineCap]: its heading and the blank line under it, and TWO LINES FOR
// EACH PROGRAM — its synopsis and a summary that fits one line. The table
// itself sits at its cap, and the cap moves by exactly what a feature adds
// (helpLineCap's own rule); this is that move, fixed per program, so a program
// whose summary needs a second line fails here, and the answer is a shorter
// summary rather than a longer page.
func carriedPageLines(programs int) int {
	if programs == 0 {
		return 0
	}
	return 2 + 2*programs
}

// pageFits is the front page's two laws: eighty cells a line, and the cap with
// the carried programs' own fixed allowance.
func pageFits(t *testing.T, printed string, programs int) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(printed, "\n"), "\n")
	for at, line := range lines {
		if drawn := ansi.StringWidth(line); drawn > helpWidth {
			t.Errorf("`codeaf --help` line %d draws %d cells: %q", at+1, drawn, line)
		}
	}
	if cap := helpLineCap + carriedPageLines(programs); len(lines) > cap {
		t.Errorf("`codeaf --help` is %d lines with %d carried programs on it, past the %d-line cap", len(lines), programs, cap)
	}
}

// ASKING A PROGRAM FOR HELP IS NOT A FAILURE, through the one dispatch a
// person's line takes: its help on stdout, exit zero, nothing on stderr.
func TestACarriedProgramsHelpIsNotAFailure(t *testing.T) {
	carryFake(t)
	for _, line := range [][]string{{"-h"}, {"--help"}, {"help"}, {"run", "--help"}} {
		printed := &bytes.Buffer{}
		previous := carriedStdout
		carriedStdout = printed
		saved := os.Args
		os.Args = append([]string{"codeaf", fakeCarried}, line...)
		err := run()
		os.Args, carriedStdout = saved, previous
		if code := exitCodeOf(err); code != 0 {
			t.Fatalf("`codeaf %s %s` left with %d", fakeCarried, strings.Join(line, " "), code)
		}
		if !strings.Contains(printed.String(), "codeaf "+fakeCarried) {
			t.Fatalf("`codeaf %s %s` printed no help:\n%s", fakeCarried, strings.Join(line, " "), printed)
		}
	}
}

// A TYPO OF A PROGRAM'S NAME IS ANSWERED WITH THE PROGRAM, like a typo of any
// verb of codeaf's own.
func TestAMisspelledProgramNameIsAnsweredWithIt(t *testing.T) {
	carryFake(t)
	if said := unknownCommand("fake-carrid").Error(); !strings.Contains(said, "codeaf "+fakeCarried) {
		t.Fatalf("a typo of a carried program was answered %q", said)
	}
}

// NO PROGRAM MAY SHADOW A WORD OF CODEAF'S OWN. The dispatch asks the build's
// list last, after every verb, alias and hidden door it answers itself, so a
// program named after one of them would be a verb nobody could ever reach —
// and it fails the build here instead.
func TestNoCarriedProgramShadowsAWordOfCodeafsOwn(t *testing.T) {
	own := codeafsOwnWords(t)
	for _, word := range []string{"do", "doctor", "help", "--version", "engine", "plandb"} {
		if !own[word] {
			t.Fatalf("%q was not read as a word of codeaf's own; the reader of main.go has stopped working", word)
		}
	}
	for _, program := range append(builtin.All(), fakeCarriedProgram()) {
		if own[program.Name] {
			t.Errorf("the program %q shadows `codeaf %s`, a word codeaf answers itself; rename the program", program.Name, program.Name)
		}
	}
}

// codeafsOwnWords is every word the dispatch answers before it asks the
// build's list: every case of run()'s switch — the hidden doors and the flag
// spellings included — and every word the typo suggester offers.
func codeafsOwnWords(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	words := map[string]bool{}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Name.Name != "run" || function.Recv != nil {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				if literal, ok := expression.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					if word, err := strconv.Unquote(literal.Value); err == nil {
						words[word] = true
					}
				}
			}
			return true
		})
	}
	if len(words) < 20 {
		t.Fatalf("only %d words were read out of run()'s dispatch", len(words))
	}
	for _, word := range knownCommands {
		words[word] = true
	}
	return words
}

// A SHELL RUN'S CHILD IS HANDED THE PERSON'S OWN LINE: the command they named,
// its own flags and the brief as they typed it, with --json added — and the
// child's parser reads that line back to the same invocation.
func TestAShellRunHandsItsChildThePersonsOwnLine(t *testing.T) {
	program := fakeCarriedProgram()
	for _, row := range []struct {
		line  []string
		child []string
	}{
		{[]string{"--calls", "2", "fix", "it"}, []string{fakeCarried, "--json", "--calls", "2", "fix", "it"}},
		{[]string{"run", "--wait", "--", "--not-a-flag"}, []string{fakeCarried, "run", "--json", "--wait", "--", "--not-a-flag"}},
		{[]string{"check"}, []string{fakeCarried, "check", "--json"}},
	} {
		inv, err := delegate.Parse(program, row.line, &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		child := carriedChildLine(inv)
		if strings.Join(child, " ") != strings.Join(row.child, " ") {
			t.Fatalf("%q became the child line %q, want %q", row.line, child, row.child)
		}
		again, err := delegate.Parse(program, child[1:], &bytes.Buffer{})
		if err != nil {
			t.Fatal(err)
		}
		if again.Command.Name != inv.Command.Name || again.Brief() != inv.Brief() || again.Workspace != inv.Workspace || !again.JSON {
			t.Fatalf("the child reads %+v where the host read %+v", again, inv)
		}
	}
}

// A PREFIXED ID IS AN ACCOUNT, NOT PART OF THE MODEL: a shell run's adapter for
// `openrouter/deepseek/…` puts the router's own id on the wire, and a
// connection's own prefix is that connection's.
func TestAShellRunsAdapterPutsTheServicesOwnIDOnTheWire(t *testing.T) {
	router := modelsource.DefaultSource(config.DefaultBaseURL)
	proxy := modelsource.Source{ID: modelsource.CustomID, Written: "mybox", Name: "mybox", Address: "http://127.0.0.1:9000/v1"}
	settings := config.Config{
		APIKey: "sk-or-v1-routerkey0000000000", BaseURL: config.DefaultBaseURL,
		Sources: modelsource.NewSet(
			modelsource.Connected{Source: router, Key: "sk-or-v1-routerkey0000000000", Address: config.DefaultBaseURL},
			modelsource.Connected{Source: proxy, Key: "local", Address: proxy.Address},
		),
	}
	adapters := &carriedAdapters{settings: settings, built: map[string]modelapi.Completer{}}
	for model, wire := range map[string]string{
		"openrouter/deepseek/deepseek-v4-flash-0731": "deepseek/deepseek-v4-flash-0731",
		"deepseek/deepseek-v4-flash-0731":            "deepseek/deepseek-v4-flash-0731",
		"mybox/qwen3-coder":                          "qwen3-coder",
	} {
		built, ok := adapters.forModel(model).(wireCompleter)
		if !ok || built.wire != wire {
			t.Fatalf("the adapter for %q puts %q on the wire, want %q", model, built.wire, wire)
		}
	}
	if first, again := adapters.forModel("mybox/qwen3-coder"), adapters.forModel("mybox/qwen3-coder"); first != again {
		t.Fatal("an adapter was built twice for one model")
	}
}

// A PROGRAM THIS BUILD CARRIES HAS ITS PAGE IN THE CHAT'S MANUAL, and the page
// names both of its doors. The chat can say only what a page says, and a verb
// the manual does not know is one the chat will improvise about or deny.
func TestEveryCarriedProgramHasItsPageInTheChatManual(t *testing.T) {
	for _, program := range builtin.All() {
		page, ok := manual.Chat().Page(program.Page)
		if !ok {
			t.Errorf("%s names the manual page %q and the chat's manual has no such page", program.Name, program.Page)
			continue
		}
		shell := regexp.MustCompile(`\bcodeaf ` + regexp.QuoteMeta(program.Name) + `\b`)
		if !shell.MatchString(page) || !strings.Contains(page, "/"+program.Name) {
			t.Errorf("%s's page %q does not name `codeaf %s` and `/%s`", program.Name, program.Page, program.Name, program.Name)
		}
	}
}
