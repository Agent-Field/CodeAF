package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ── EVERY PAGE OF HELP FITS THE TERMINAL IT IS READ IN ─────────────────────
//
// `aforge --help` was a hundred and eight lines that drew a hundred and
// sixty-seven ROWS on an eighty-column terminal, because its longest line was a
// hundred and sixty-four cells. Every second line was therefore folded by the
// terminal, at a break nobody chose, INSIDE A WORD — `resuming you|r last
// conversation` — and the twenty-five-column hanging indent stopped aligning
// the moment it happened. The one page that has to be readable was the least
// readable thing this binary printed, and `aforge help env` was worse: a
// reference table whose widest row was a hundred and sixteen cells.
//
// IT IS MEASURED IN DISPLAY CELLS AND NOT IN BYTES. That is the whole reason
// [wrapAt] exists and the whole reason [ansi.StringWidth] is what measures a
// row everywhere else in this binary: the em dashes and the `·` these pages are
// full of weigh three bytes each and draw one cell, so `len` would call a
// perfectly good line over-wide and — on a combining accent — a bad one fine.
//
// AND IT READS THE DOORS, NOT THE VARIABLES. `usageText` is a concatenation:
// the exit ladder is folded into it from envelope.go, the cache word from
// cache.go, the dollar figures from internal/config. A test that measured the
// literal would measure a page nobody sees. So every page here is captured from
// the door that prints it, exactly as a person gets it.
func TestEveryHelpPageFitsAnEightyColumnTerminal(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())

	for _, page := range helpPages(t) {
		t.Run(page.name, func(t *testing.T) {
			printed := strings.Split(strings.TrimRight(page.text, "\n"), "\n")
			if len(printed) < 3 {
				t.Fatalf("`%s` printed %d lines; the door is not printing its help at all:\n%s",
					page.name, len(printed), page.text)
			}
			for at, line := range printed {
				drawn := ansi.StringWidth(line)
				if drawn <= helpWidth {
					continue
				}
				t.Errorf("`%s` line %d draws %d cells in a %d-column terminal, "+
					"so the terminal folds it inside a word%s:\n  %s",
					page.name, at+1, drawn, helpWidth, sourceOfHelpLine(t, line), line)
			}
		})
	}
}

// ── AND THE PAGE IS STILL SHORTER THAN WHAT IT REPLACED ────────────────────
//
// The obvious way to make a hundred-and-sixty-four-cell page fit eighty is to
// fold every line in half and print twice as many, which trades one unreadable
// page for a longer one. It has to fit AND cost the reader fewer rows than the
// page it replaced — a hundred and sixty-seven, measured on the binary this
// change started from.
func TestTheHelpPageCostsFewerRowsThanTheOneItReplaced(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())

	// What `aforge --help` cost on an eighty-column terminal before this
	// change: a hundred and eight lines, a hundred and sixty-seven rows.
	const rowsBefore = 167

	out, _ := captureUsage(t)
	if err := usage(nil); err != nil {
		t.Fatalf("`aforge --help` failed: %v", err)
	}
	rows := 0
	for _, line := range strings.Split(strings.TrimRight(out.String(), "\n"), "\n") {
		drawn := ansi.StringWidth(line)
		// A line that fits is one row; the emptiness of a blank line is a row
		// too, which is how a terminal counts it.
		rows += max(1, (drawn+helpWidth-1)/helpWidth)
	}
	if rows >= rowsBefore {
		t.Errorf("`aforge --help` draws %d rows on an %d-column terminal; the page it replaced "+
			"drew %d, so folding it has bought the reader nothing",
			rows, helpWidth, rowsBefore)
	}
}

// helpPage is one page of help and the name a person reaches it by.
type helpPage struct {
	name string
	text string
}

// helpPages captures every page of help this binary prints: the front page, the
// environment table, and one per-command page for each of the shapes
// [writeCommandUsage] can produce — a door with a long flag list, a door with a
// folded synopsis, a door with subcommands, and a door with no flags at all.
func helpPages(t *testing.T) []helpPage {
	t.Helper()

	pages := []helpPage{}
	capture := func(name string, print func() error) {
		out, errs := captureUsage(t)
		if err := print(); err != nil && exitCodeOf(err) != 0 {
			t.Fatalf("`aforge %s` left with %d: %s", name, exitCodeOf(err), errs.String())
		}
		pages = append(pages, helpPage{name: name, text: out.String()})
	}

	capture("--help", func() error { return usage(nil) })
	capture("help env", func() error { return usage([]string{"env"}) })
	for _, door := range []struct {
		name string
		run  func([]string) error
	}{
		{"do", runDo},
		{"exec", runExec},
		{"run", runExecute},
		{"chat", runChatV3},
		{"logs", runLogs},
		{"doctor", runDoctor},
		{"wake", runWake},
		{"why", runWhy},
		{"cache", runCache},
		{"devices", runDevices},
		{"plan new", func(args []string) error { return runPlanNew("plan new", args) }},
		{"plan run", func(args []string) error { return runGraph("plan run", args) }},
	} {
		door := door
		capture(door.name+" --help", func() error { return door.run([]string{"--help"}) })
	}
	return pages
}

// sourceOfHelpLine names the file and line a printed help line was written at,
// so the failure above is something an author can open rather than something
// they have to grep for.
//
// It reads cmd/aforge with go/parser because the two big pages are RAW STRING
// LITERALS spanning a hundred lines each: the position of the literal plus the
// number of newlines before the offending text inside it is the line in the
// file. That import is also what puts this law on the pull-request gate —
// scripts/laws.sh finds the structural tests by it.
func sourceOfHelpLine(t *testing.T, line string) string {
	t.Helper()

	// The text as it was typed, with the leading indent the literal carries.
	wanted := strings.TrimRight(line, " ")
	if wanted == "" {
		return ""
	}
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseDir(fileSet, ".", func(entry fs.FileInfo) bool {
		return !strings.HasSuffix(entry.Name(), "_test.go")
	}, 0)
	if err != nil {
		return ""
	}
	for _, pkg := range parsed {
		for path, file := range pkg.Files {
			for _, literal := range stringLiterals(file) {
				text, err := strconv.Unquote(literal.Value)
				if err != nil {
					continue
				}
				at := strings.Index(text, wanted)
				if at < 0 {
					continue
				}
				start := fileSet.Position(literal.Pos()).Line
				return " (" + path + ":" +
					strconv.Itoa(start+strings.Count(text[:at], "\n")) + ")"
			}
		}
	}
	return ""
}

// stringLiterals is every quoted string in one file, in source order.
func stringLiterals(file *ast.File) []*ast.BasicLit {
	var found []*ast.BasicLit
	ast.Inspect(file, func(node ast.Node) bool {
		if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
			found = append(found, literal)
		}
		return true
	})
	return found
}
