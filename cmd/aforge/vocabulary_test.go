package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
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
// hands back what was said AND WHAT THE DOOR RETURNED.
//
// THE ERROR USED TO BE DROPPED, and dropping it is what made this whole table
// unable to fail for its own reason. An old spelling that printed its notice
// and then died as an unknown flag, or that was refused its old value, or that
// walked into a different command altogether, satisfied every assertion here:
// the notice is written before any of that happens. A compatibility window
// nobody can observe is not one, so the error comes back with the sentence.
func captureNotice(t *testing.T, run func() error) (string, error) {
	t.Helper()
	var said bytes.Buffer
	previous := renameNotice
	renameNotice = &said
	t.Cleanup(func() { renameNotice = previous })
	failed := run()
	return said.String(), failed
}

// doorParse is what one door's parse actually produced: which door it was, the
// positionals it was left holding, and every flag's value as the door will read
// it. It is the fact a rename is a claim about — "the old spelling reaches the
// same place with the same value" — and it is unreadable from outside, because
// every door builds its flag set privately and then goes looking for a provider
// key. [parseWatcher] is the seam that hands it over.
type doorParse struct {
	door       string
	positional []string
	values     map[string]string
}

// watchParses runs a door and records every clean parse it completed, along
// with the rename notice and the door's own error.
func watchParses(t *testing.T, run func() error) ([]doorParse, string, error) {
	t.Helper()
	var seen []doorParse
	previous := parseWatcher
	parseWatcher = func(flags *flag.FlagSet) {
		snapshot := doorParse{
			door:       flags.Name(),
			positional: append([]string{}, flags.Args()...),
			values:     map[string]string{},
		}
		// EVERY flag, hidden aliases included. An alias writes through to the
		// printed flag's own value (rename.go), so the two spellings of one
		// invocation must produce identical maps down to the last key.
		flags.VisitAll(func(f *flag.Flag) { snapshot.values[f.Name] = f.Value.String() })
		seen = append(seen, snapshot)
	}
	t.Cleanup(func() { parseWatcher = previous })
	said, failed := captureNotice(t, run)
	parseWatcher = previous
	return seen, said, failed
}

// firstDifference says, in one clause, where two parses stopped agreeing — the
// door, a positional, or a flag's value — so a failure names the fact that
// moved instead of printing two structs and leaving the reading to a person.
func firstDifference(old, now []doorParse) string {
	if len(old) != len(now) {
		return fmt.Sprintf("the old spelling parsed %d door(s) and the new one parsed %d", len(old), len(now))
	}
	for at := range old {
		if old[at].door != now[at].door {
			return fmt.Sprintf("it entered the door %q where the new spelling enters %q", old[at].door, now[at].door)
		}
		if !reflect.DeepEqual(old[at].positional, now[at].positional) {
			return fmt.Sprintf("%s was left holding %v where the new spelling leaves %v",
				old[at].door, old[at].positional, now[at].positional)
		}
		names := make([]string, 0, len(old[at].values))
		for name := range old[at].values {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			was, still := old[at].values[name], now[at].values[name]
			if was != still {
				return fmt.Sprintf("%s read --%s as %q where the new spelling reads %q",
					old[at].door, name, was, still)
			}
		}
		for name := range now[at].values {
			if _, ok := old[at].values[name]; !ok {
				return fmt.Sprintf("%s never declared --%s under the old spelling", old[at].door, name)
			}
		}
	}
	return ""
}

// wordsOf is an error as a string, and "" for none, so two doors' endings can be
// compared as one value.
func wordsOf(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// typed is a command line, run THROUGH THE BINARY'S OWN DISPATCH — the words
// after `aforge`, read by the same switch on os.Args a person's shell fills in.
//
// It matters that the rename rows go through the switch rather than calling the
// door they believe the old word reaches. Half of what a command rename can get
// wrong is which door the old word lands in, and a row that calls the door
// itself has assumed the answer to the question it is asking.
func typed(words ...string) func() error {
	return func() error {
		previous := os.Args
		os.Args = append([]string{"aforge"}, words...)
		defer func() { os.Args = previous }()
		return run()
	}
}

// writeTestPlan puts a real, loadable plan file on disk, because two of the old
// spellings are told apart from the new ones by whether their first positional
// is spelled as a path (run.go's namesAPlanPath) — and a temp-directory path
// always is.
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

// EVERY OLD SPELLING STILL WORKS, REACHES THE SAME PLACE WITH THE SAME VALUE,
// IS ABSENT FROM `--help`, AND SAYS WHAT IT IS CALLED NOW — once, in one line,
// on stderr.
//
// The parts are one test because they are one promise, and dropping any of them
// turns a rename into one of the failures it exists to avoid: a script that
// stops working overnight, a `--help` page teaching two spellings for one knob,
// or a person left to guess what happened to their command.
//
// EACH ROW IS THE SAME INVOCATION SPELLED TWICE, and the assertion is that the
// two are the same run. It used to be the notice and nothing else, which could
// not fail for its own reason: the notice is written BEFORE the parse can
// refuse the old flag, before the old value can be rejected, and before the
// door the old word dispatches into is known. So the row runs both spellings,
// compares the door each entered, the positionals it was left holding and every
// flag value it read ([watchParses]), and then compares what each door returned
// — which is the same sentence, at the same pre-provider wall, or the test says
// where the two parted. No row spends money: every one of them stops at a
// missing key, a plan with no nodes, an input that was never named, or a store
// that is not there.
func TestAnOldSpellingReachesTheSamePlaceAndSaysWhatItIsCalledNow(t *testing.T) {
	t.Setenv("AFORGE_HOME", t.TempDir())
	planFile := writeTestPlan(t)

	for _, spelling := range []struct {
		name string
		old  string // the retired word or flag, as a person types it
		now  string // what the notice must name instead
		// The two spellings of ONE invocation. Same door, same values, same
		// ending — or this row is not a rename, it is a second command.
		wasTyped func() error
		nowTyped func() error
	}{{
		name: "run subharness is run",
		old:  "run subharness", now: "aforge run",
		wasTyped: typed("run", "subharness", "a-program", "--input", "in.json"),
		nowTyped: typed("run", "a-program", "--input", "in.json"),
	}, {
		name: "run of a plan file is plan run",
		old:  "run <plan.json>", now: "aforge plan run",
		wasTyped: typed("run", planFile),
		nowTyped: typed("plan", "run", planFile),
	}, {
		name: "bare plan is plan new",
		old:  `plan "<goal>"`, now: "aforge plan new",
		wasTyped: typed("plan", "a goal"),
		nowTyped: typed("plan", "new", "a goal"),
	}, {
		name: "show is plan show",
		old:  "show", now: "aforge plan show",
		wasTyped: typed("show", planFile),
		nowTyped: typed("plan", "show", planFile),
	}, {
		name: "revise is plan revise",
		old:  "revise", now: "aforge plan revise",
		wasTyped: typed("revise", planFile, "it went badly"),
		nowTyped: typed("plan", "revise", planFile, "it went badly"),
	}, {
		name: "exec --budget is --token-budget",
		old:  "--budget", now: "--token-budget",
		wasTyped: typed("exec", "a prompt", "--budget", "9000"),
		nowTyped: typed("exec", "a prompt", "--token-budget", "9000"),
	}, {
		name: "exec --turns is --max-turns",
		old:  "--turns", now: "--max-turns",
		wasTyped: typed("exec", "a prompt", "--turns", "3"),
		nowTyped: typed("exec", "a prompt", "--max-turns", "3"),
	}, {
		name: "plan run --budget is --token-budget",
		old:  "--budget", now: "--token-budget",
		wasTyped: typed("plan", "run", planFile, "--budget", "9000"),
		nowTyped: typed("plan", "run", planFile, "--token-budget", "9000"),
	}, {
		name: "plan run --run-budget is --total-token-budget",
		old:  "--run-budget", now: "--total-token-budget",
		wasTyped: typed("plan", "run", planFile, "--run-budget", "9000"),
		nowTyped: typed("plan", "run", planFile, "--total-token-budget", "9000"),
	}, {
		name: "plan run --contracts is --no-method",
		old:  "--contracts", now: "--no-method",
		wasTyped: typed("plan", "run", planFile, "--contracts=false"),
		nowTyped: typed("plan", "run", planFile, "--no-method"),
	}, {
		name: "plan new --brief is --instructions",
		old:  "--brief", now: "--instructions",
		wasTyped: typed("plan", "new", "a goal", "--brief"),
		nowTyped: typed("plan", "new", "a goal", "--instructions"),
	}, {
		name: "plan new --ensemble is --passes",
		old:  "--ensemble", now: "--passes",
		wasTyped: typed("plan", "new", "a goal", "--ensemble", "3"),
		nowTyped: typed("plan", "new", "a goal", "--passes", "3"),
	}, {
		name: "wake --max-seconds is --timeout",
		old:  "--max-seconds", now: "--timeout",
		wasTyped: typed("wake", "--max-seconds", "30"),
		nowTyped: typed("wake", "--timeout", "30"),
	}} {
		t.Run(spelling.name, func(t *testing.T) {
			wasParsed, said, wasEnding := watchParses(t, spelling.wasTyped)
			nowParsed, quiet, nowEnding := watchParses(t, spelling.nowTyped)

			// THE NOTICE: one line, on stderr, naming what to type instead and
			// saying the grace ends.
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
			// AND THE NEW SPELLING SAYS NOTHING. A notice on the spelling a
			// person is being sent to would be the rename shouting at the
			// people who already did what it asked.
			if quiet != "" {
				t.Fatalf("the current spelling of `%s` printed a rename notice of its own: %q",
					spelling.old, strings.TrimSpace(quiet))
			}

			// IT GOT PAST THE COMMAND LINE. A spelling whose door parses flags
			// and yet completed no parse died there — the exact failure the
			// notice hides. `show` and `revise` declare no flags at all and so
			// record nothing; the ending below is what carries those two.
			if len(nowParsed) > 0 && len(wasParsed) == 0 {
				t.Fatalf("`%s` printed its notice and then never got past the command line: %v",
					spelling.old, wasEnding)
			}
			// THE SAME DOOR, THE SAME POSITIONALS, THE SAME VALUES.
			if where := firstDifference(wasParsed, nowParsed); where != "" {
				t.Fatalf("`%s` is not the same run as `%s`:\n  %s",
					spelling.old, spelling.now, where)
			}
			// AND THE SAME ENDING. Both spellings walk into the same wall — a
			// missing key, a plan with no nodes — and say the same sentence
			// there. A different one is the old spelling landing somewhere else.
			if was, still := wordsOf(wasEnding), wordsOf(nowEnding); was != still {
				t.Fatalf("`%s` ended with %q; the current spelling ends with %q",
					spelling.old, was, still)
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
		run  func() error
	}{
		{"-w on do", func() error { return runDo([]string{"a task", "-w", "."}) }},
		{"-w on exec", func() error { return runExec([]string{"a prompt", "-w", "."}) }},
		{"-o on plan new", func() error { return runPlanNew("plan new", []string{"a goal", "-o", "out.json"}) }},
		{"-j on plan run", func() error { return runGraph("plan run", []string{planFile, "-j", "4"}) }},
	} {
		t.Run(letter.name, func(t *testing.T) {
			if said, _ := captureNotice(t, letter.run); said != "" {
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
				// its first positional (run.go's namesAPlanPath), which is a
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
	// `unjudged` is a field and not a stop word, so the sweep at the bottom of
	// this test does not reach it. It is named here because it is the other
	// half of the ending #593 added, and a caller reads it instead of parsing
	// a sentence.
	if !strings.Contains(document, "`unjudged`") {
		t.Error("docs/HEADLESS.md never names the `unjudged` field of `do --json`")
	}
	// `checklist` is another do-only extra field, so the envelope-field sweep
	// above cannot name it from the shared contract.
	if !strings.Contains(document, "`checklist`") {
		t.Error("docs/HEADLESS.md never names the `checklist` field of `do --json`")
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

	// AND THE LADDER IS READ OFF THE TABLE RATHER THAN REMEMBERED. Everything
	// above this line is a string somebody typed here after noticing a drift,
	// which is a gate that catches the drift it was written for and no other:
	// the exit-3 row named `price` and `deadline` for as long as the rung
	// produced four reasons, and every check above passed the whole time. So
	// the rows are compared against [exitLadder] itself.
	ladder := headlessLadderSection(t, document)
	for _, rung := range exitLadder {
		row := headlessLadderRow(ladder, int(rung.Code))
		if row == "" {
			t.Errorf("docs/HEADLESS.md's exit table has no row for exit %d, which exitLadder has", int(rung.Code))
			continue
		}
		for _, stop := range rung.Stops {
			if !strings.Contains(row, "`"+string(stop)+"`") {
				t.Errorf("docs/HEADLESS.md's exit-%d row does not name `%s`, which exitLadder says produces that rung:\n  %s",
					int(rung.Code), stop, row)
			}
		}
	}

	// AND EVERY WORD THE BINARY CAN PUT IN `stop` IS ON THE PAGE SOMEWHERE.
	// A caller branches on this field, so an ending the page never names is an
	// ending they meet as an unhandled default. The words come off the two
	// const blocks that declare them, so one added tomorrow is on this gate
	// tomorrow — `no-progress` was emitted for months and named nowhere.
	for _, source := range []struct{ file, kind string }{
		{"envelope.go", "stopReason"},
		{"../../internal/exec/executor.go", "StopReason"},
		{"../../internal/exec/noprogress.go", "StopReason"},
	} {
		for _, word := range declaredStopWords(t, source.file, source.kind) {
			if !strings.Contains(document, "`"+word+"`") {
				t.Errorf("docs/HEADLESS.md never names the `%s` ending, which %s declares", word, source.file)
			}
		}
	}
}

// headlessLadderSection returns the body of the one section of docs/HEADLESS.md
// that carries the shared ladder, so the rows read out of it are that table's
// and not `exec`'s narrower restatement further down the page.
func headlessLadderSection(t *testing.T, document string) string {
	t.Helper()
	const heading = "### Exit codes — one ladder, and it is the same one on all three commands"
	start := strings.Index(document, heading)
	if start < 0 {
		t.Fatalf("docs/HEADLESS.md no longer carries the section %q, so the ladder cannot be read off it", heading)
	}
	body := document[start+len(heading):]
	if end := strings.Index(body, "\n### "); end >= 0 {
		body = body[:end]
	}
	return body
}

// headlessLadderRow is the table row for one exit code, or "" when the table
// has none.
func headlessLadderRow(section string, code int) string {
	prefix := fmt.Sprintf("| `%d` |", code)
	for _, line := range strings.Split(section, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// declaredStopWords is every string constant of type `kind` declared in `file`,
// which is how the gate above learns a new ending without being told.
func declaredStopWords(t *testing.T, file, kind string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	words := []string{}
	for _, declaration := range parsed.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			named, ok := value.Type.(*ast.Ident)
			if !ok || named.Name != kind {
				continue
			}
			for _, expression := range value.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				word, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatal(err)
				}
				words = append(words, word)
			}
		}
	}
	if len(words) == 0 {
		t.Fatalf("no %s constant was found in %s, so this gate would pass on nothing", kind, file)
	}
	return words
}
