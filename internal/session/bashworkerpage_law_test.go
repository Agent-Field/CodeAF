package session

// THE LAW: THE BASH WORKER'S PAGE MAY ONLY NAME A COMMAND THAT EXISTS.
//
// A bash-belt worker has no tools but the shell, so the two pages it reads
// (prompts/bashtask.md, the loop policy, and prompts/bashworker.md, the belt's
// doctrine) teach it the plan by naming `plandb` verbs, and the belt's one
// non-shell road — the exact-match edit — by naming `codeaf patch`. A page that
// teaches a verb the CLI does not answer to, or a flag no door defines, is the
// prompt lying in the one place the model cannot check: it types the command,
// reads a usage refusal, and spends a step learning that the doctrine is wrong.
//
// SO THE LAW IS HELD ON THE SOURCES THEMSELVES, not on the handful of sentences
// a behavioural test happens to exercise. The verbs are the keys of the plan
// CLI's own usage table (internal/plandb/cli.go, cliVerbHelp) and the flags are
// the ones those usage lines spell; the doors are the registrations in
// cmd/codeaf, read with go/parser — a door is real when its own run function
// builds a flag set under its name, and its flags are the ones that function
// declares — and the dispatch case in main.go's run() names it too. A verb or a
// door renamed next year without the page moving is named the day it lands.

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

// bashVerbPageNames are the pages the law reads: the loop policy and the belt's
// doctrine, which between them are the whole page a bash-belt worker opens on.
var bashVerbPageNames = []string{"prompts/bashtask.md", "prompts/bashworker.md"}

// pageCommand is one command spelling read off a page: the page it stood on,
// the line number, the door or verb word, and the flags spelled beside it.
type pageCommand struct {
	page  string
	line  int
	word  string
	flags []string
}

// TestBashWorkerPageNamesOnlyRealPlanVerbs holds the plan half: every `plandb
// <verb>` and `plandb task <sub>` spelled in a fenced block or backticks on
// either page is a key of the CLI's usage table, and every `--flag` the page
// spells beside that verb appears in the verb's own usage line. The failure
// names the page, the line and the word that is not there.
func TestBashWorkerPageNamesOnlyRealPlanVerbs(t *testing.T) {
	table := planUsageTable(t)
	commands := pageCommandsOnAll(t, "plandb")
	if len(commands) == 0 {
		t.Fatal("neither page spells a `plandb` verb in a fenced block or backticks; the law stopped finding anything to judge")
	}
	for _, command := range commands {
		{
			line, ok := table[command.word]
			if !ok {
				t.Errorf("%s:%d: the page spells `plandb %s`, which is not a verb in the plan CLI's usage table",
					command.page, command.line, command.word)
				continue
			}
			spelled := map[string]bool{}
			for _, flag := range command.flags {
				spelled[flag] = true
			}
			for _, flag := range sortedKeys(spelled) {
				if !strings.Contains(line, flag) {
					t.Errorf("%s:%d: the page spells `%s` beside `plandb %s`, but that verb's usage line does not define it: %s",
						command.page, command.line, flag, command.word, strings.TrimSpace(line))
				}
			}
		}
	}
}

// TestBashWorkerPageNamesOnlyRealDoors holds the door half: every `codeaf
// <door>` the pages spell has a run function under cmd/codeaf that builds its
// flag set under that name and a `case` in main.go's dispatch, and every
// `--flag` the page spells beside it is declared in that function.
func TestBashWorkerPageNamesOnlyRealDoors(t *testing.T) {
	doors := codeafDoors(t)
	commands := pageCommandsOnAll(t, "codeaf")
	if len(commands) == 0 {
		t.Fatal("neither page spells a `codeaf` door in a fenced block or backticks; the law stopped finding anything to judge")
	}
	for _, command := range commands {
		{
			door, ok := doors[command.word]
			if !ok {
				t.Errorf("%s:%d: the page spells `codeaf %s`, which no file under cmd/codeaf registers and no dispatch case names",
					command.page, command.line, command.word)
				continue
			}
			for _, flag := range command.flags {
				if !door.flags[flag] {
					t.Errorf("%s:%d: the page spells `%s` beside `codeaf %s`, but %s declares no such flag",
						command.page, command.line, flag, command.word, door.file)
				}
			}
		}
	}
}

// planUsageTable reads the plan CLI's usage table out of internal/plandb/cli.go
// with go/parser: the map literal inside cliVerbHelp, whose keys are the verbs
// and whose values are the usage lines. Reading the source rather than calling
// the CLI keeps the law in this package and free of a store.
func planUsageTable(t *testing.T) map[string]string {
	t.Helper()
	path := filepath.Join("..", "plandb", "cli.go")
	file := parseGoFile(t, path)
	table := map[string]string{}
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "cliVerbHelp" || fn.Body == nil {
			return true
		}
		ast.Inspect(fn.Body, func(inner ast.Node) bool {
			literal, ok := inner.(*ast.CompositeLit)
			if !ok || literalType(literal) != "map[string]string" {
				return true
			}
			for _, element := range literal.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, keyOK := stringLiteral(pair.Key)
				value, valueOK := stringLiteral(pair.Value)
				if keyOK && valueOK {
					table[key] = value
				}
			}
			return false
		})
		return false
	})
	if len(table) == 0 {
		t.Fatal("the plan CLI's usage table was not found in internal/plandb/cli.go; if the map moved or was renamed, point this law at it")
	}
	return table
}

// codeafDoor is one door under cmd/codeaf: the file whose run function
// registers it, and the flags that function declares.
type codeafDoor struct {
	file  string
	flags map[string]bool
}

// codeafDoors parses every non-test source under cmd/codeaf with go/parser and
// answers the doors: a door is registered by a `commandFlags("<name>")` call,
// its flags are the names that same function declares, and the dispatch in
// main.go's run() must name the door too. A door with a sub-verb (`web search`)
// is filed under both the one word and the two, so either spelling on a page
// resolves.
func codeafDoors(t *testing.T) map[string]codeafDoor {
	t.Helper()
	dir := filepath.Join("..", "..", "cmd", "codeaf")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading cmd/codeaf: %v", err)
	}
	doors := map[string]codeafDoor{}
	dispatched := map[string]bool{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("cmd/codeaf/%s: %v", name, err)
		}
		if name == "main.go" {
			dispatched = dispatchedDoors(file)
		}
		for _, declared := range file.Decls {
			fn, ok := declared.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			for _, door := range commandFlagNames(fn) {
				doors[door] = codeafDoor{file: name, flags: declaredFlagNames(fn)}
			}
		}
	}
	if len(doors) == 0 {
		t.Fatal("no file under cmd/codeaf registers a door with commandFlags; if that seam moved, point this law at it")
	}
	for door := range doors {
		// A sub-verb (`web search`) answers to its own flag set but is
		// dispatched under the noun it hangs on, so the case named in main.go
		// is the door's first word.
		head, _, _ := strings.Cut(door, " ")
		if !dispatched[head] {
			t.Fatalf("cmd/codeaf registers the door %q but main.go's dispatch never names %q", door, head)
		}
	}
	return doors
}

// dispatchedDoors reads the door names main.go's run() answers to: the string
// cases of the switch on os.Args[1].
func dispatchedDoors(file *ast.File) map[string]bool {
	doors := map[string]bool{}
	for _, declared := range file.Decls {
		fn, ok := declared.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.SwitchStmt:
				for _, statement := range node.Body.List {
					clause, ok := statement.(*ast.CaseClause)
					if !ok {
						continue
					}
					for _, expression := range clause.List {
						if word, ok := stringLiteral(expression); ok {
							doors[word] = true
						}
					}
				}
			}
			return true
		})
	}
	return doors
}

// commandFlagNames is every door name one function registers by calling
// `commandFlags("<name>")`.
func commandFlagNames(fn *ast.FuncDecl) []string {
	var names []string
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || calledName(call.Fun) != "commandFlags" || len(call.Args) != 1 {
			return true
		}
		if word, ok := stringLiteral(call.Args[0]); ok {
			names = append(names, word)
		}
		return true
	})
	return names
}

// flagMethods are the flag package's declaration methods. A call to one of them
// on the door's flag set names a flag this door answers to.
var flagMethods = map[string]bool{
	"String": true, "Bool": true, "Int": true, "Int64": true, "Uint": true,
	"Uint64": true, "Float64": true, "Duration": true, "Var": true,
	"StringSlice": true, "IntSlice": true, "Func": true,
}

// declaredFlagNames is every flag one run function declares, read from the
// first string argument of its flag-set method calls.
func declaredFlagNames(fn *ast.FuncDecl) map[string]bool {
	flags := map[string]bool{}
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || !flagMethods[selector.Sel.Name] || len(call.Args) == 0 {
			return true
		}
		if _, ok := selector.X.(*ast.Ident); !ok {
			return true
		}
		if word, ok := stringLiteral(call.Args[0]); ok {
			flags["--"+word] = true
		}
		return true
	})
	return flags
}

// pageCommandsOnAll reads both pages and answers every `<word> <verb>`
// spelling either makes in a fenced block or in backticks. A fence is a whole
// line inside a ``` block; an inline span is the text between a pair of
// backticks. Prose beside them is not read: a sentence that names `plandb` as
// "the plan CLI" teaches no verb, and only a spelled command is a promise.
func pageCommandsOnAll(t *testing.T, word string) []pageCommand {
	t.Helper()
	var commands []pageCommand
	for _, page := range bashVerbPageNames {
		raw, err := os.ReadFile(page)
		if err != nil {
			t.Fatalf("read %s: %v", page, err)
		}
		for _, line := range pageCodeLines(string(raw)) {
			for _, found := range findCommands(line.text, word) {
				found.page = page
				found.line = line.number
				commands = append(commands, found)
			}
		}
	}
	return commands
}

// codeLine is one line of a page's code — inside a fence, or inside backticks —
// with the number of the page line it stood on.
type codeLine struct {
	number int
	text   string
}

// pageCodeLines masks a page down to the code it spells: every line inside a
// fenced block whole, and the text of every inline backtick span elsewhere. A
// span left open at the end of a line is read as code to the line's end, which
// is how the one command that wraps across two lines still reads whole. Line
// numbers survive, so a refusal can name where the word stood.
func pageCodeLines(page string) []codeLine {
	var out []codeLine
	inFence := false
	for index, line := range strings.Split(page, "\n") {
		number := index + 1
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		text := line
		if !inFence {
			text = inlineSpans(line)
		}
		if text = strings.TrimSpace(text); text != "" {
			out = append(out, codeLine{number: number, text: text})
		}
	}
	return out
}

// inlineSpans is the text of every backtick span on one line, joined by a
// space. A span left open at the line's end contributes what it holds, so a
// command whose closing backtick is on the next line is still read here, where
// it was written.
func inlineSpans(line string) string {
	var out []string
	for {
		open := strings.IndexByte(line, '`')
		if open < 0 {
			break
		}
		line = line[open+1:]
		close := strings.IndexByte(line, '`')
		if close < 0 {
			out = append(out, line)
			break
		}
		out = append(out, line[:close])
		line = line[close+1:]
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

// commandPattern matches `<word> <first>` at a word boundary, so a spelling is
// read from the word itself and never from the middle of another.
var commandPattern = regexp.MustCompile(`\b(plandb|codeaf)\s+([A-Za-z][A-Za-z0-9-]*)`)

// flagPattern is one long flag as a page spells it.
var flagPattern = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// findCommands reads every `<word> <verb>` spelling on one line of code and the
// flags spelled beside it. The verb is the two-word form when the CLI files one
// (`task note`, `what-if cancel`), and the one word otherwise; `task` and
// `what-if` alone fall back to the bare word, which is itself a table key.
func findCommands(text, word string) []pageCommand {
	var commands []pageCommand
	for _, match := range commandPattern.FindAllStringSubmatchIndex(text, -1) {
		if text[match[2]:match[3]] != word {
			continue
		}
		first := text[match[4]:match[5]]
		rest := text[match[5]:]
		verb := first
		if first == "task" || first == "what-if" {
			if next := strings.Fields(rest); len(next) > 0 {
				if sub := trimWord(next[0]); isVerbWord(sub) {
					verb = first + " " + sub
				}
			}
		}
		commands = append(commands, pageCommand{word: verb, flags: flagPattern.FindAllString(rest, -1)})
	}
	return commands
}

// trimWord drops the punctuation a page uses to end a word — a comma, a colon,
// a closing bracket — so `plandb critical-path,` reads as the verb.
func trimWord(word string) string {
	return strings.Trim(word, ",.;:)")
}

// isVerbWord reports whether a word could be a verb's second word rather than
// the argument that follows it: a bare word of letters and dashes.
func isVerbWord(word string) bool {
	if word == "" {
		return false
	}
	for index, r := range word {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '-' && index > 0:
		default:
			return false
		}
	}
	return true
}

// stringLiteral is the unquoted text of a string literal, or false when the
// node is not one.
func stringLiteral(node ast.Expr) (string, bool) {
	literal, ok := node.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	text, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return text, true
}

// literalType is a composite literal's written type — `map[string]string` and
// friends — as one string.
func literalType(literal *ast.CompositeLit) string {
	switch typed := literal.Type.(type) {
	case *ast.MapType:
		key := typeName(typed.Key)
		value := typeName(typed.Value)
		return "map[" + key + "]" + value
	case *ast.Ident:
		return typed.Name
	}
	return ""
}

// parseGoFile parses one source file with its comments and fails the test when
// it will not parse, so a moved file is a named refusal and not a silent miss.
func parseGoFile(t *testing.T, path string) *ast.File {
	t.Helper()
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return file
}
