package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// ── THE RENAME'S OWN CONTRACT ───────────────────────────────────────────────
//
// `run` used to mean two unrelated commands, and `--budget` used to mean tokens
// in a product where *budget* means dollars everywhere else. Both moved. The
// three tests below are the whole of what "moved" is allowed to mean:
//
//  1. every old spelling still works, is absent from `--help`, and says on
//     STDERR what it is called now;
//  2. that notice never reaches stdout, so `--json` stays parseable;
//  3. one concept is spelled one way on every door, checked against the tree
//     itself rather than against a list somebody keeps by hand.

// captureNotice runs a door with the rename notice pointed at a buffer, and
// hands back what was said. The door's own error is deliberately dropped: most
// of these commands go on to want a provider key, and what is under test is the
// sentence that is printed BEFORE any of that.
func captureNotice(t *testing.T, run func()) string {
	t.Helper()
	var said bytes.Buffer
	previous := renameNotice
	renameNotice = &said
	t.Cleanup(func() { renameNotice = previous })
	run()
	return said.String()
}

// writeTestPlan puts a real, loadable plan file on disk, because two of the old
// spellings are told apart from the new ones by whether their first positional
// names a file (run.go's namesAPlanFile).
func writeTestPlan(t *testing.T) string {
	t.Helper()
	graph := &plan.Graph{Goal: "measure the old spellings"}
	encoded, err := graph.JSON()
	if err != nil {
		t.Fatalf("build a plan file: %v", err)
	}
	path := filepath.Join(t.TempDir(), "plan.json")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write a plan file: %v", err)
	}
	return path
}

// EVERY OLD SPELLING STILL WORKS, IS ABSENT FROM `--help`, AND SAYS WHAT IT IS
// CALLED NOW — once, in one line, on stderr.
//
// The three parts are one test because they are one promise, and dropping any
// of them turns a rename into one of the three failures it exists to avoid: a
// script that stops working overnight, a `--help` page teaching two spellings
// for one knob, or a person left to guess what happened to their command.
func TestAnOldSpellingStillWorksAndSaysWhatItIsCalledNow(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	planFile := writeTestPlan(t)

	for _, spelling := range []struct {
		name string
		old  string // the retired word or flag, as a person types it
		now  string // what the notice must name instead
		run  func()
	}{{
		name: "run subharness is run",
		old:  "run subharness", now: "aforge run",
		run: func() { _ = runExecute([]string{"subharness", "a-program", "--input", "-"}) },
	}, {
		name: "run of a plan file is plan run",
		old:  "run <plan.json>", now: "aforge plan run",
		run: func() { _ = runExecute([]string{planFile}) },
	}, {
		name: "bare plan is plan new",
		old:  `plan "<goal>"`, now: "aforge plan new",
		run: func() { _ = runPlanCommand([]string{"a goal"}) },
	}, {
		name: "show is plan show",
		old:  "show", now: "aforge plan show",
		run: func() {
			_ = renamedTo("show <plan.json>", "plan show <plan.json>", []string{planFile},
				func(args []string) error { return runShow("plan show", args) })
		},
	}, {
		name: "revise is plan revise",
		old:  "revise", now: "aforge plan revise",
		run: func() {
			_ = renamedTo("revise <plan.json>", "plan revise <plan.json>", []string{planFile, "it went badly"},
				func(args []string) error { return runRevise("plan revise", args) })
		},
	}, {
		name: "exec --budget is --token-budget",
		old:  "--budget", now: "--token-budget",
		run: func() { _ = runExec([]string{"a prompt", "--budget", "9000"}) },
	}, {
		name: "exec --turns is --max-turns",
		old:  "--turns", now: "--max-turns",
		run: func() { _ = runExec([]string{"a prompt", "--turns", "3"}) },
	}, {
		name: "plan run --budget is --token-budget",
		old:  "--budget", now: "--token-budget",
		run: func() { _ = runGraph("plan run", []string{planFile, "--budget", "9000"}) },
	}, {
		name: "plan run --run-budget is --total-token-budget",
		old:  "--run-budget", now: "--total-token-budget",
		run: func() { _ = runGraph("plan run", []string{planFile, "--run-budget", "9000"}) },
	}, {
		name: "plan run --contracts is --no-method",
		old:  "--contracts", now: "--no-method",
		run: func() { _ = runGraph("plan run", []string{planFile, "--contracts=false"}) },
	}, {
		name: "plan new --brief is --instructions",
		old:  "--brief", now: "--instructions",
		run: func() { _ = runPlanNew("plan new", []string{"a goal", "--brief"}) },
	}, {
		name: "plan new --ensemble is --passes",
		old:  "--ensemble", now: "--passes",
		run: func() { _ = runPlanNew("plan new", []string{"a goal", "--ensemble", "3"}) },
	}, {
		name: "wake --max-seconds is --timeout",
		old:  "--max-seconds", now: "--timeout",
		run: func() { _ = runWake([]string{"--max-seconds", "30"}) },
	}} {
		t.Run(spelling.name, func(t *testing.T) {
			said := captureNotice(t, spelling.run)
			if said == "" {
				t.Fatalf("`%s` printed no notice at all — nothing told anybody it is now `%s`",
					spelling.old, spelling.now)
			}
			if lines := strings.Count(strings.TrimSpace(said), "\n") + 1; lines != 1 {
				t.Fatalf("`%s` printed %d lines, want ONE:\n%s", spelling.old, lines, said)
			}
			if !strings.Contains(said, spelling.now) {
				t.Fatalf("`%s` said %q, which never names %q", spelling.old, strings.TrimSpace(said), spelling.now)
			}
			if !strings.Contains(said, "one more release") {
				t.Fatalf("`%s` said %q, which never says the old spelling is going away",
					spelling.old, strings.TrimSpace(said))
			}
		})
	}
}

// AND THE OLD SPELLINGS ARE NOWHERE ON THE HELP PAGE. A `--help` that printed
// both would teach a reader that `--budget` and `--token-budget` are two knobs,
// which is the confusion the rename exists to end.
func TestNoOldSpellingIsPrintedByHelp(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	// The whole front page, plus every per-command page, read as one body of
	// text — an old spelling hiding on any of them is the same defect.
	pages := []string{usageText, environmentText}
	for _, door := range []struct {
		name string
		run  func([]string) error
	}{
		{"do", runDo},
		{"exec", runExec},
		{"run", runExecute},
		{"plan new", func(args []string) error { return runPlanNew("plan new", args) }},
		{"plan run", func(args []string) error { return runGraph("plan run", args) }},
		{"plan revise", func(args []string) error { return runRevise("plan revise", args) }},
		{"wake", runWake},
	} {
		out, _ := captureUsage(t)
		if code := exitCodeOf(door.run([]string{"--help"})); code != 0 {
			t.Fatalf("`aforge %s --help` left with %d", door.name, code)
		}
		pages = append(pages, out.String())
	}
	printed := strings.Join(pages, "\n")
	for _, retired := range []string{
		"--budget ", "--turns ", "--run-budget", "--contracts", "--brief", "--ensemble",
		"--max-seconds", "run subharness", "--w ", "--o ", "--j ",
	} {
		if strings.Contains(printed, retired) {
			for _, line := range strings.Split(printed, "\n") {
				if strings.Contains(line, retired) {
					t.Errorf("a help page still offers the retired spelling %q: %q", retired, strings.TrimSpace(line))
					break
				}
			}
		}
	}
}

// A SHORTHAND IS NOT A DEPRECATION. `-w`, `-o` and `-j` are what fingers
// already know; they keep working forever and they say NOTHING, because there
// is nothing to say. Printing a going-away notice over them would be a lie
// about their lifetime, and it would fire on almost every headless invocation
// in the wild.
func TestASingleLetterShorthandKeepsWorkingAndSaysNothing(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	planFile := writeTestPlan(t)
	for _, letter := range []struct {
		name string
		run  func()
	}{
		{"-w on do", func() { _ = runDo([]string{"a task", "-w", "."}) }},
		{"-w on exec", func() { _ = runExec([]string{"a prompt", "-w", "."}) }},
		{"-o on plan new", func() { _ = runPlanNew("plan new", []string{"a goal", "-o", "out.json"}) }},
		{"-j on plan run", func() { _ = runGraph("plan run", []string{planFile, "-j", "4"}) }},
	} {
		t.Run(letter.name, func(t *testing.T) {
			if said := captureNotice(t, letter.run); said != "" {
				t.Fatalf("%s printed %q — a shorthand is not going away and must say nothing",
					letter.name, strings.TrimSpace(said))
			}
		})
	}
}

// THE NOTICE NEVER REACHES STDOUT, SO `--json` STAYS PARSEABLE.
//
// stdout carries the answer and nothing else: the deliverable, the rows, the
// one machine-readable object. One line of prose in front of that object breaks
// every caller piping into `jq` — which is the very population a hidden alias
// exists to protect, so a rename that printed there would do more damage than
// the rename it was softening.
func TestARenameNoticeNeverReachesTheJSONOnStdout(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	// The writer itself, first: nothing else in this test can be right if the
	// notice is aimed at the wrong stream to begin with.
	if renameNotice != os.Stderr {
		t.Fatalf("the rename notice is written to %T, want os.Stderr", renameNotice)
	}

	out, said := captureUsage(t)
	previous := renameNotice
	renameNotice = said
	t.Cleanup(func() { renameNotice = previous })
	// `--json` ACTUALLY ON, with an old flag spelling beside it. Whatever this
	// run manages — an envelope, or nothing at all because there is no key —
	// stdout must be machine-readable and must not hold one word of the notice.
	_ = runExec([]string{"say hello", "--budget", "9000", "--json"})

	if !strings.Contains(said.String(), "--token-budget") {
		t.Fatalf("the notice did not go to stderr:\n%s", said.String())
	}
	printed := strings.TrimSpace(out.String())
	if strings.Contains(printed, "one more release") || strings.Contains(printed, "--token-budget") {
		t.Fatalf("the rename notice landed on stdout, where the JSON is:\n%s", printed)
	}
	if printed != "" {
		var envelope map[string]any
		if err := json.Unmarshal([]byte(printed), &envelope); err != nil {
			t.Fatalf("--json stdout did not parse (%v):\n%s", err, printed)
		}
	}
}

// ── ONE CONCEPT, ONE SPELLING, ON EVERY DOOR ────────────────────────────────
//
// A developer who learned `--dir` on `do` must be able to type it on `exec`
// without checking, and a flag that means the same thing under two names is two
// things to learn for one idea.
//
// IT READS THE TREE with go/parser rather than a hand-kept list, for two
// reasons. The list would be the second source of truth this law exists to
// abolish, and scripts/laws.sh finds the laws by this import — so a structural
// test written this way is on the pull-request gate the day it lands.
func TestOneConceptIsSpelledOneWayOnEveryDoor(t *testing.T) {
	declared := printedFlags(t)
	if len(declared) < 30 {
		t.Fatalf("only %d flag declarations were read out of cmd/aforge; the reader has stopped working", len(declared))
	}

	// THE RETIRED SPELLINGS, and the one spelling each of them means. A door
	// that declares one of these as a PRINTED flag has reintroduced the split.
	retired := map[string]string{
		"budget":      "token-budget",
		"turns":       "max-turns",
		"run-budget":  "total-token-budget",
		"max-seconds": "timeout",
		"ensemble":    "passes",
		"brief":       "instructions",
		"contracts":   "no-method",
		"w":           "dir",
		"o":           "out",
		"j":           "parallel",
	}
	for _, found := range declared {
		if now, ok := retired[found.name]; ok {
			t.Errorf("%s declares --%s as a printed flag; the one spelling for that is --%s "+
				"(an old spelling belongs in renamedFlag or shorthandFlag, where --help does not print it)",
				found.door, found.name, now)
		}
	}

	// MACHINERY VOCABULARY DOES NOT REACH A FLAG'S HELP SENTENCE. These are the
	// words COMMANDS.md §3 rules out of anything a person reads, and `--turns`
	// and `--budget` on `aforge run` could not be reasoned about at all while
	// their sentences explained them in terms of a *leaf*.
	//
	// `lane` is the ONE exception and is not on this list: internal/manual/chat/
	// lanes.md is an established person-facing page using it to mean the
	// provider route that answered, and `aforge logs` prints exactly that.
	//
	// `node` is not on it either, and for a narrower reason: the only flag that
	// carries the word is `aforge logs --node`, whose whole contract is that it
	// filters the call log's own recorded field, printed back byte-for-byte
	// under `--json`. A filter named after the field it filters is not a leak.
	machinery := []string{
		"leaf", "leaves", "spine", "seat", "sheet", "rail", "brain",
		"charter", "verdict", "errand", "ensemble", "contract", "panel",
	}
	for _, found := range declared {
		lower := strings.ToLower(found.usage)
		for _, word := range machinery {
			if !strings.Contains(lower, word) {
				continue
			}
			t.Errorf("%s's --%s explains itself with the machinery word %q: %q",
				found.door, found.name, word, found.usage)
		}
	}

	// AND ONE CONCEPT SAYS ONE SENTENCE, SPELLED ONCE, IN A CONSTANT.
	//
	// `--yes-spend` was "approve a plan whose price crosses the consent
	// threshold" on `do` and "preauthorize raising today's dollar rail when
	// reached" on the plan runner — two decisions, as far as a reader could
	// tell, for one flag doing one thing. `--db` was "path to the durable graph
	// database" on six doors, "work in this durable store instead of a private
	// one" on a seventh, and a store on an eighth.
	//
	// The table names the doors rather than saying "every door that declares
	// this flag", because SOME FLAG NAMES GENUINELY MEAN TWO THINGS and pooling
	// them would demand one sentence for two ideas: `aforge logs --model` is a
	// FILTER over recorded calls, `aforge competence --model` names whose
	// measurements to read, and `aforge chat --model` is the model you talk to
	// — none of which is the work model the headless doors take. Likewise
	// `--json` is a result envelope on the three headless verbs, a document on
	// the two plan-writing ones, and a row stream on `logs` (COMMANDS.md §5).
	// The law is that the doors sharing a MEANING share a sentence.
	sentenceFor := map[string]map[string]string{}
	for _, found := range declared {
		if sentenceFor[found.name] == nil {
			sentenceFor[found.name] = map[string]string{}
		}
		sentenceFor[found.name][found.door] = found.usage
	}
	for _, concept := range []struct {
		flag     string
		constant string
		doors    []string
	}{
		{"model", "modelFlagHelp", []string{"runDo", "runExec", "runGraph", "runPlanNew", "runRevise", "runSubharnessCommand"}},
		{"json", "jsonFlagHelp", []string{"runDo", "runExec", "runSubharnessCommand"}},
		{"yes-spend", "yesSpendFlagHelp", []string{"runDo", "runGraph"}},
		{"db", "storeFlagHelp", []string{"runDoctorWith", "runWhyTo", "runNotebookTo", "runServices", "runCompetenceTo", "runRebuildWith", "runWakeWith"}},
	} {
		for _, door := range concept.doors {
			said, declared := sentenceFor[concept.flag][door]
			if !declared {
				t.Errorf("%s no longer declares --%s; the concept table in this test is out of date", door, concept.flag)
				continue
			}
			if said != concept.constant {
				t.Errorf("%s describes --%s as %s instead of reaching for %s — one concept is one sentence, spelled once",
					door, concept.flag, strconv.Quote(said), concept.constant)
			}
		}
	}
}

// declaredFlag is one printed flag: the door that declares it, its name, and the
// sentence `--help` prints under it.
type declaredFlag struct{ door, name, usage string }

// printedFlags reads every flag cmd/aforge declares AND PRINTS.
//
// A hidden alias is skipped, and it is recognised by the mark carried in its own
// usage string (rename.go): `renamedFlag`, `shorthandFlag` and the one inverted
// boolean all write [hiddenRenamed] or [hiddenShorthand] there, and nothing a
// person reads ever does.
func printedFlags(t *testing.T) []declaredFlag {
	t.Helper()
	var found []declaredFlag
	entries, err := os.ReadDir("./")
	if err != nil {
		t.Fatalf("read cmd/aforge: %v", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			function, ok := node.(*ast.FuncDecl)
			if !ok {
				return true
			}
			ast.Inspect(function, func(inner ast.Node) bool {
				call, ok := inner.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				nameIndex, usageIndex := 0, 2
				switch selector.Sel.Name {
				case "String", "Int", "Bool", "Float64", "Duration", "Int64":
					// flags.String(name, default, usage)
				case "StringVar", "IntVar", "BoolVar", "Float64Var":
					nameIndex, usageIndex = 1, 3
				case "Var":
					// flags.Var(value, name, usage)
					nameIndex, usageIndex = 1, 2
				default:
					return true
				}
				if len(call.Args) <= usageIndex {
					return true
				}
				flagName := stringLiteralOf(call.Args[nameIndex])
				if flagName == "" {
					return true
				}
				if mentionsHiddenMark(call.Args[usageIndex]) {
					return true
				}
				// A FLAG WITH NO SENTENCE IS NOT A PRINTED FLAG. The only ones
				// in this package are the union set `aforge run` builds to find
				// its first positional (run.go's namesAPlanFile), which is a
				// reader of somebody else's grammar and prints nothing at all.
				if usageTextOf(call.Args[usageIndex]) == "" {
					return true
				}
				found = append(found, declaredFlag{
					door:  function.Name.Name,
					name:  flagName,
					usage: usageTextOf(call.Args[usageIndex]),
				})
				return true
			})
			return false
		})
	}
	return found
}

// mentionsHiddenMark reports whether a usage expression is one of the two hidden
// marks — which is what makes a flag an alias rather than a printed knob.
func mentionsHiddenMark(expression ast.Expr) bool {
	marked := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, ok := node.(*ast.Ident)
		if ok && (identifier.Name == "hiddenRenamed" || identifier.Name == "hiddenShorthand") {
			marked = true
		}
		return !marked
	})
	return marked
}

// usageTextOf flattens a usage expression to the words in it: the string pieces
// of a concatenation, joined. A help string built from a constant contributes
// the constant's NAME, which is exactly right for the shared-sentence check —
// two doors reaching for `modelFlagHelp` are two doors saying one sentence.
func usageTextOf(expression ast.Expr) string {
	var pieces []string
	ast.Inspect(expression, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.BasicLit:
			if word := stringLiteralOf(value); word != "" {
				pieces = append(pieces, word)
			}
		case *ast.Ident:
			pieces = append(pieces, value.Name)
		}
		return true
	})
	return strings.Join(pieces, "")
}

// stringLiteralOf is one quoted word, or "" for anything that is not one.
func stringLiteralOf(expression ast.Expr) string {
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

// `--help` IS THE COMMANDS, GROUPED, THEN FIVE EXAMPLES, AND NOTHING ELSE.
//
// It was 127 lines and more than half of them were the environment table, so
// the last thing on a person's screen after asking what the commands are was
// AFORGE_CALL_LOG_BODIES and the commands themselves had scrolled off.
func TestTheHelpPageIsGroupedCommandsAndExamplesAndNotTheEnvironmentTable(t *testing.T) {
	for _, heading := range []string{
		"Talk to it", "Hand it work", "Look at what happened", "Housekeeping", "Plan work by hand",
	} {
		if !strings.Contains(usageText, "\n"+heading) {
			t.Errorf("`aforge --help` has no %q group", heading)
		}
	}
	if !strings.Contains(usageText, "\nExamples:\n") {
		t.Error("`aforge --help` has no worked examples, and the two lines a developer most wants to copy are nowhere")
	}
	if examples := strings.Count(usageText[strings.Index(usageText, "\nExamples:\n"):], "\n    aforge "); examples != 5 {
		t.Errorf("`aforge --help` shows %d examples, want the five that each teach a different thing", examples)
	}
	// AND AN EXAMPLE IS NOT A COMMAND ROW. The per-command usage is lifted out
	// of this same table by matching lines that begin `  aforge ` — so an
	// example written at that indent is read as part of a command's synopsis,
	// and `aforge do --help` printed two example lines under `do`'s shape.
	if shape := usageForCommand("do"); strings.Contains(shape, "jq -r .answer") {
		t.Errorf("`aforge do --help` swallowed an example out of the table:\n%s", shape)
	}
	if strings.Contains(usageText, "AFORGE_CALL_LOG_BODIES") {
		t.Error("the environment table is back on `aforge --help`; it belongs at `aforge help env`")
	}
	if lines := strings.Count(usageText, "\n") + 1; lines > 110 {
		t.Errorf("`aforge --help` is %d lines; it was cut down to fit a screen and a bit", lines)
	}
	if !strings.Contains(usageText, "an agent you talk to, and hand work to when you walk away") {
		t.Error("`aforge --help` no longer opens with what this product is")
	}
	if strings.Contains(usageText, "build and revise task graphs") {
		t.Error("`aforge --help` opens by describing a static pipeline that is four of twenty-three verbs")
	}
}

// AND THE TABLE IT LOST HAS A DOOR OF ITS OWN, which `--help` names.
func TestTheEnvironmentTableHasItsOwnDoor(t *testing.T) {
	out, errs := captureUsage(t)
	if err := usage([]string{"env"}); err != nil {
		t.Fatalf("`aforge help env` failed: %v", err)
	}
	printed := out.String()
	for _, variable := range []string{"OPENROUTER_API_KEY", "AFORGE_DAILY_BUDGET", "AFORGE_CALL_LOG_BODIES"} {
		if !strings.Contains(printed, variable) {
			t.Errorf("`aforge help env` never mentions %s:\n%s", variable, printed)
		}
	}
	if errs.Len() != 0 {
		t.Errorf("`aforge help env` wrote to stderr, where a caller reads failures:\n%s", errs.String())
	}
	out, _ = captureUsage(t)
	if err := usage(nil); err != nil {
		t.Fatalf("`aforge --help` failed: %v", err)
	}
	if strings.Contains(out.String(), "AFORGE_CALL_LOG_BODIES") {
		t.Error("`aforge --help` is printing the environment table again")
	}
}

// docs/HEADLESS.md IS A CONTRACT OTHER PEOPLE PROGRAM AGAINST, so it has to
// document the ladder and the envelope this binary actually has.
//
// It documented `exec`'s old 2/3/4/5/6 and its old `text`/`elapsed_ms`/`usage`
// object for an hour after both were replaced, which is the worst state for a
// contract to be in: confidently wrong, in the one file a harness author reads
// instead of the source.
func TestHeadlessDocumentsTheLadderAndTheEnvelopeItActuallyHas(t *testing.T) {
	raw, err := os.ReadFile("../../docs/HEADLESS.md")
	if err != nil {
		t.Fatal(err)
	}
	document := string(raw)

	// The one envelope, by every field name a caller reads off it.
	for _, field := range []string{
		`"ok"`, `"stop"`, `"answer"`, `"files"`, `"error"`, `"spend_usd"`,
		`"tokens"`, `"seconds"`, `"model"`, `"steps"`,
	} {
		if !strings.Contains(document, field) {
			t.Errorf("docs/HEADLESS.md never shows the %s field of the one result envelope", field)
		}
	}
	// The one ladder, and the hatch back to exec's old numbers.
	for _, promise := range []string{
		"AFORGE_EXIT_CODES=legacy",
		"same ladder as `do` and `run`",
		"`stop` is the field to move a script to",
	} {
		if !strings.Contains(document, promise) {
			t.Errorf("docs/HEADLESS.md never says %q", promise)
		}
	}
	// AND NOT THE OLD TABLE. Six exit codes for `exec` is the shape that was
	// replaced; a row for `6` is the tell.
	for _, stale := range []string{
		"| `6` | `done` | It finished cleanly with an empty `text`. |",
		"`5` is the catch-all",
		"| `2` | `budget` | The token budget ran out.",
	} {
		if strings.Contains(document, stale) {
			t.Errorf("docs/HEADLESS.md still documents exec's old exit table: %q", stale)
		}
	}
	// And the flags say what the binary answers to.
	for _, retired := range []string{"[--turns N]", "`--budget N`", "-w dir]", "aforge run <graph.json>", "--max-seconds"} {
		if strings.Contains(document, retired) {
			t.Errorf("docs/HEADLESS.md still offers the retired spelling %q", retired)
		}
	}
	for _, current := range []string{"--token-budget", "--max-turns", "aforge plan run", "aforge run <program>"} {
		if !strings.Contains(document, current) {
			t.Errorf("docs/HEADLESS.md never names %q", current)
		}
	}
}
