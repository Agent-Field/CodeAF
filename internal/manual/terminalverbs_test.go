package manual

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// THE TERMINAL HALF OF THE COMPLETENESS GATE.
//
// internal/tui3's manual_test.go checks every slash command and alias against
// this corpus, and internal/session's checks every tool on the belt. Neither of
// them could see a COMMAND-LINE VERB, and the hole was not theoretical: `why`,
// `notebook`, `competence`, `services`, `wake` and `rebuild` shipped for months
// with no mention anywhere in chat/. This corpus is the only authoritative
// source about aforge for the model — its training data does not contain this
// program — so somebody who asked the running chat "how do I see what that task
// actually did?" was answered by an improvisation, or by a flat denial of a
// command the binary has always had.
//
// The bar is the same low bar the other two keep: the corpus must MENTION the
// verb, not describe it well. A test that graded prose is a test nobody could
// keep green; this one only insists that whoever wired a verb into the dispatch
// also opened the manual.
//
// IT LIVES HERE, in the package that owns the corpus, because it is the one
// place that can see BOTH halves: chat/ is beside it, and cmd/aforge is read as
// source the way truth_test.go already reads it for the figures the pages quote.
// A test in cmd/aforge could see the dispatch more directly and could not be run
// at all while another lane has that package mid-edit.
//
// THE VERBS ARE READ OUT OF THE TREE WITH go/parser rather than off a list,
// for two reasons. The first is that `knownCommands` is hand-kept while the
// dispatch switch in run() is what is actually true, so reading only the list
// would let a verb be dispatched, typed, and undocumented all at once. The
// second is mechanical: scripts/laws.sh finds the laws by this import, so a
// structural test written this way is on the pull-request gate the day it lands.
func TestTheChatManualMentionsEveryVerbTheCommandLineAnswersTo(t *testing.T) {
	verbs := dispatchedVerbs(t)
	if len(verbs) < 20 {
		t.Fatalf("only %d verbs were read out of the dispatch; the reader has stopped working", len(verbs))
	}
	for _, verb := range verbs {
		if !chatManualNamesTheCommand(t, verb) {
			t.Errorf("no chat manual page mentions `aforge %s` — add it to internal/manual/chat/", verb)
		}
	}
}

// chatManualNamesTheCommand looks for `aforge <verb>` as a whole word.
//
// [Corpus.Mentions] is a plain substring test, which is right for a slash
// command and wrong here: "aforge shows their names" contains "aforge show", so
// a substring gate would have read the `show` verb as documented by a sentence
// about something else entirely. The word boundary is the difference between
// this gate checking the corpus and it checking the alphabet.
func chatManualNamesTheCommand(t *testing.T, verb string) bool {
	t.Helper()
	named := regexp.MustCompile(`(?i)\baforge ` + regexp.QuoteMeta(verb) + `\b`)
	for _, name := range Chat().Pages() {
		text, ok := Chat().Page(name)
		if !ok {
			t.Fatalf("page %s vanished between listing and reading", name)
		}
		if named.MatchString(text) {
			return true
		}
	}
	return false
}

// dispatchedVerbs is every word `aforge <word>` answers to, read from the two
// places that decide it: the switch in run() (cmd/aforge/main.go), which is the
// dispatch itself, and `knownCommands` (cmd/aforge/usage.go), which is what the
// typo suggester offers.  A word in either is a word a person can type.
//
// The flag spellings of a verb — `--version`, `-v`, `-h`, `--help` — are
// dropped: each is an alias of a verb already in the set, and no page would
// spell `aforge --version` as a command in its own right.
func dispatchedVerbs(t *testing.T) []string {
	t.Helper()
	found := map[string]bool{}
	for _, verb := range append(runSwitchCases(t), knownCommandsLiteral(t)...) {
		if verb == "" || strings.HasPrefix(verb, "-") {
			continue
		}
		found[verb] = true
	}
	verbs := make([]string, 0, len(found))
	for verb := range found {
		verbs = append(verbs, verb)
	}
	sort.Strings(verbs)
	return verbs
}

// runSwitchCases reads the case labels of the `switch os.Args[1]` inside run().
// That switch is the dispatch: every other fact about a verb — its help line,
// its place in the typo suggester — is a decision made after this one.
func runSwitchCases(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../../cmd/aforge/main.go", nil, 0)
	if err != nil {
		t.Fatalf("read cmd/aforge/main.go: %v", err)
	}
	var cases []string
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if !ok || function.Recv != nil || function.Name.Name != "run" {
			continue
		}
		ast.Inspect(function, func(node ast.Node) bool {
			clause, ok := node.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, expression := range clause.List {
				if word := stringLiteral(expression); word != "" {
					cases = append(cases, word)
				}
			}
			return true
		})
	}
	if len(cases) == 0 {
		t.Fatal("no case labels were read out of run()'s dispatch switch in cmd/aforge/main.go")
	}
	return cases
}

// knownCommandsLiteral reads the `knownCommands` slice out of usage.go, so that
// a verb left in the typo suggester after the dispatch dropped it is still
// checked — the suggester will still offer it, and a person will still type it.
func knownCommandsLiteral(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../../cmd/aforge/usage.go", nil, 0)
	if err != nil {
		t.Fatalf("read cmd/aforge/usage.go: %v", err)
	}
	var words []string
	ast.Inspect(file, func(node ast.Node) bool {
		spec, ok := node.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "knownCommands" {
			return true
		}
		for _, value := range spec.Values {
			composite, ok := value.(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, element := range composite.Elts {
				if word := stringLiteral(element); word != "" {
					words = append(words, word)
				}
			}
		}
		return false
	})
	if len(words) == 0 {
		t.Fatal("knownCommands was not found in cmd/aforge/usage.go")
	}
	return words
}

// stringLiteral is one quoted word, or "" for anything that is not one.
func stringLiteral(expression ast.Expr) string {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return ""
	}
	word, err := strconv.Unquote(literal.Value)
	if err != nil {
		return ""
	}
	return word
}
