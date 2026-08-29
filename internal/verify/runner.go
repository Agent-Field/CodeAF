package verify

// HOW a reading is taken, as opposed to WHAT is run.
//
// discovery.go answers "what command does this project say checks it". That
// question turned out to be one half of a reading, and the missing half cost a
// whole sweep. On 2026-08-29 five graded runs took five readings and named ZERO
// checks between them. ofetch's is the whole story in one line: the project
// declares `pnpm test`, whose body is a lint step and a typecheck step and THEN
// vitest, so a formatting complaint exits 1 before the suite runs at all — and
// the gate, reading that, said "the project's own verification exited 1 and
// named 0 checks", which is true of the script and says nothing whatever about
// the tests. The acceptance mapping then had an empty roster to map fifty-two
// stated behaviours onto, and raised nothing.
//
// The structural fact the old reading ignored is that A PROJECT'S TEST SCRIPT IS
// NOT ITS TEST RUNNER. The script is a lifecycle: it may lint, typecheck, build
// and then run checks, and only the last of those names identities. The runner
// underneath it is a program that can be ASKED for a machine-readable account of
// every check it ran — vitest and jest print JSON, go test prints JSON, pytest
// prints a named line per check when asked for one, mocha speaks TAP. So the
// reading is taken from the RUNNER's own output, and the runner is found in what
// the project itself declares: the body of its own test script, its manifest's
// dependencies, its runner's config file, its lockfile.
//
// Nothing here is a per-project list and nothing here knows what any benchmark
// contains. A runner is recognised the way a check declaration is in roster.go —
// by the shape of what the repository declares — and every strategy falls back
// to the shared PASS/FAIL vocabulary over the same bytes, so a runner nobody
// here has met is read exactly as well as it was before this file existed.

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Format is how one reading's bytes are turned into check identities.
//
// It is a property of the RUNNER and not of the language: vitest and jest print
// the same document because one copied the other's reporter, and mocha under
// `--reporter tap` prints what the shared vocabulary already reads. Three
// formats cover every runner this program has met, and a runner that matches
// none of them is read as plain text, which is what every runner was read as
// before this existed.
type Format string

const (
	// FormatPlain is the runners' shared human-readable vocabulary — the
	// PASS/FAIL lines failing.go and roster.go already know. It is the floor
	// every other format falls back to, never an absence of one.
	FormatPlain Format = "plain"
	// FormatNodeJSON is the document vitest's `--reporter=json` and jest's
	// `--json` both print: one entry per file, each holding one assertion
	// result per check, each naming the check and how it went.
	FormatNodeJSON Format = "node-json"
	// FormatGoJSON is `go test -json`: one JSON object per line, each an
	// action on a package or a named test.
	FormatGoJSON Format = "go-json"
)

// Strategy is how one reading is taken: the exact command, where it runs, how
// its output is read, and — the part an autopsy needs — WHY this command rather
// than the one the project declared.
//
// It is pinned on the first reading and re-used verbatim for the second, because
// two readings taken with two different commands subtract to noise. Re-deriving
// it would also hand a worker that edited its own test script the power to
// change what the after-reading measures, which is the tamper this whole
// photograph exists to catch.
type Strategy struct {
	// Command is what is actually run, whole.
	Command string `json:"command"`
	Workdir string `json:"workdir,omitempty"`
	// Runner names the program underneath, in the spelling the project uses.
	// Empty means none was found and the project's own script is being run as
	// it stands.
	Runner string `json:"runner,omitempty"`
	// Read is how the output is turned into names.
	Read Format `json:"read"`
	// Source is where the runner was found — the script whose body names it,
	// the manifest that depends on it, the config file that configures it. It
	// is the sentence an autopsy reads to see whether the reader looked in the
	// right place.
	Source string `json:"source,omitempty"`
	// Declared is the project's own entrypoint, kept beside the command that
	// was actually run so the two can be compared. Where a lifecycle script
	// wraps the runner these differ, and the difference is the news.
	Declared string `json:"declared,omitempty"`
	// Scope is HOW MUCH of the project this reading covers: ScopeWhole, or the
	// count of files a scoped reading selected. See scope.go for why a reading
	// is scoped before it is bounded, and Reading.comparable for why the scope
	// is part of this strategy's identity rather than a note beside it.
	Scope string `json:"scope,omitempty"`
}

// comparable says two readings are readings of the SAME thing, which is the
// only condition under which subtracting one from the other means anything.
//
// Command and workdir and scope, all three, because each of them alone has
// changed what a reading covers: a different rung of the ladder is a different
// command, a different package of a monorepo is a different workdir, and the
// same command scoped to three files is a different suite from the same command
// scoped to none. A before reading of a whole suite minus an after reading of
// three files is a hundred checks that "disappeared", and every one of them
// would be a finding.
func (s Strategy) comparable(other Strategy) bool {
	return s.Command == other.Command && s.Workdir == other.Workdir && s.Scope == other.Scope
}

// Empty reports a strategy that names no command, which is what an undiscovered
// entrypoint produces.
func (s Strategy) Empty() bool { return strings.TrimSpace(s.Command) == "" }

// runner is one program that can be asked for a reading, described by what the
// project would have to declare for it to be here at all.
//
// The table is a vocabulary of RUNNERS, not of frameworks or of projects: each
// row says how the runner spells its own executable, what flag makes it print a
// reading a machine can take, and how the project declares it. That is the same
// kind of table failingTestPatterns is, and it is safe the same way — a row that
// matches nothing costs a fallback to plain text, and a row that matches wrongly
// costs a command that exits without naming anything, which also falls back.
type runner struct {
	name string
	// binary is the head of the runner's own invocation, as it appears in a
	// script body. module is set for the runners that are spelled as an
	// interpreter's `-m` argument instead of as a program.
	binary string
	module string
	// machineArgs are the flags that make this runner name every check it ran.
	// They are appended to whatever invocation was found, so the project's own
	// flags survive.
	machineArgs []string
	// runArgs are inserted straight after the binary when the invocation found
	// does not already carry one of them. It exists for the runners whose bare
	// invocation does something other than run once — vitest watches — and it
	// is empty for every runner whose default is a single run.
	runArgs []string
	read    Format
	// declaredBy names the files whose presence is the project's own statement
	// that this runner is how it is checked. Any one of them is enough.
	declaredBy []string
	// dependency is the manifest key that declares the runner as a dependency.
	// It is read out of package.json's dependency maps, which is the JavaScript
	// ecosystem's own declaration of what a project is built out of.
	dependency string
	// invocation is how the runner is run when the project declares it but its
	// own script does not spell it out.
	invocation string
	// selects says this runner takes the checks to run as positional arguments
	// on its own command line — a path, a file, a pattern. It is what makes a
	// SCOPED reading possible at all: a runner that can only be told to run
	// everything has one honest reading, and that is the whole suite.
	selects bool
	// selectsDirectories says the positional arguments are directories rather
	// than files. Go is the case: `go test` is handed packages, and a package
	// is a directory, so a selection of three test files is a selection of the
	// one or two packages they live in.
	selectsDirectories bool
}

// runners is the table, in the order a project declaring two of them should be
// read. The order is from the most specific declaration to the least: a project
// with both a vitest config and a bare pytest tree is a JavaScript project with
// a Python script in it, and the runner its own test script names wins over all
// of this anyway.
var runners = []runner{{
	name: "vitest", binary: "vitest", read: FormatNodeJSON,
	machineArgs: []string{"--reporter=json"},
	// A bare `vitest` watches the filesystem and never exits. `run` is the
	// subcommand that makes it a measurement rather than a session, and a
	// reading taken without it is a reading that hits its ceiling every time.
	runArgs:    []string{"run", "related", "bench"},
	declaredBy: []string{"vitest.config.ts", "vitest.config.js", "vitest.config.mjs", "vitest.config.mts", "vitest.config.cjs"},
	dependency: "vitest", invocation: "vitest run", selects: true,
}, {
	name: "jest", binary: "jest", read: FormatNodeJSON,
	machineArgs: []string{"--json"},
	declaredBy:  []string{"jest.config.ts", "jest.config.js", "jest.config.mjs", "jest.config.cjs", "jest.config.json"},
	dependency:  "jest", invocation: "jest", selects: true,
}, {
	// Mocha's own JSON is a third document shape, and it does not need a third
	// parser: mocha speaks TAP, and TAP is already in the shared vocabulary.
	name: "mocha", binary: "mocha", read: FormatPlain,
	machineArgs: []string{"--reporter", "tap"},
	declaredBy:  []string{".mocharc.json", ".mocharc.yml", ".mocharc.yaml", ".mocharc.js", ".mocharc.cjs"},
	dependency:  "mocha", invocation: "mocha", selects: true,
}, {
	// ava speaks TAP for the same reason mocha is asked to: its own default
	// output names only what failed, and TAP is already in the shared
	// vocabulary. One row, no parser.
	name: "ava", binary: "ava", read: FormatPlain,
	machineArgs: []string{"--tap"},
	declaredBy:  []string{"ava.config.js", "ava.config.cjs", "ava.config.mjs"},
	dependency:  "ava", invocation: "ava", selects: true,
}, {
	// pytest's quiet default prints one dot per check and no names at all, and
	// its normal default names only the red ones. `-rA` asks for the short
	// summary over EVERY check, which is exactly the roster, spelled in the
	// `PASSED path::name` lines the shared vocabulary already reads. So pytest
	// needs a flag rather than a parser.
	name: "pytest", binary: "pytest", module: "pytest", read: FormatPlain,
	machineArgs: []string{"-rA"},
	declaredBy:  []string{"pytest.ini", "tox.ini", "pyproject.toml", "setup.cfg", "noxfile.py"},
	invocation:  "python3 -m pytest", selects: true,
}, {
	name: "unittest", binary: "", module: "unittest", read: FormatPlain,
	// unittest prints "ok" and nothing else without -v; with it, one named
	// line per check, which is what the vocabulary reads.
	machineArgs: []string{"-v"},
}, {
	name: "go test", binary: "go", read: FormatGoJSON,
	machineArgs: []string{}, runArgs: nil,
	declaredBy: []string{"go.mod"}, invocation: "go test -json ./...",
	selects: true, selectsDirectories: true,
}, {
	// Cargo's own machine-readable reporter is nightly-only, and a reading that
	// needs an unstable toolchain is a reading most repositories cannot take.
	// Its stable output already names every check — `test module::name ... ok` —
	// so the honest strategy here is the plain one, recorded as such.
	name: "cargo test", binary: "cargo", read: FormatPlain,
	declaredBy: []string{"Cargo.toml"}, invocation: "cargo test --workspace",
}}

// managerExec is how a locally-installed runner is reached, per package manager.
// A runner installed into node_modules/.bin is not on PATH, so a strategy that
// named the binary alone would be a command not found on every JavaScript
// project there is.
var managerExec = map[string]string{
	"pnpm": "pnpm exec", "yarn": "yarn exec", "bun": "bun x", "npm": "npx",
}

var (
	// A segment's leading environment assignments, which belong to the command
	// and must survive in front of whatever prefix is added.
	envAssignments = regexp.MustCompile(`^(?:[A-Z_][A-Z0-9_]*=(?:'[^']*'|"[^"]*"|[^[:space:]]*)[[:space:]]+)+`)
	// The runner-launcher prefixes a script body may already carry. A segment
	// that has one needs none added.
	//
	// A launcher is a program whose whole job is to run ANOTHER program inside
	// the project's own environment, and every ecosystem has one: npx and the
	// package managers' exec verbs in JavaScript, poetry/pdm/hatch/uv/pipenv
	// `run` in Python. They are stripped to FIND the runner and kept in the
	// command that is RUN, because the runner usually exists only inside the
	// environment the launcher opens. textual's own Makefile is `poetry run
	// pytest tests/ ...`, and a reader that could not see past `poetry` did not
	// find pytest at all — it fell through to a whole-repository invocation
	// that collected 3,422 tests and was killed at its ceiling.
	execPrefixes = regexp.MustCompile(`^(?:npx|pnpm[[:space:]]+exec|pnpm[[:space:]]+dlx|yarn[[:space:]]+exec|yarn[[:space:]]+dlx|npm[[:space:]]+exec|bun[[:space:]]+x|poetry[[:space:]]+run|pdm[[:space:]]+run|hatch[[:space:]]+run|uv[[:space:]]+run|pipenv[[:space:]]+run|rye[[:space:]]+run)[[:space:]]+(?:--[[:space:]]+)?`)
	// `<manager> run <script>` and `<manager> test`, which is how discovery.go
	// spells a package script and therefore what has to be expanded back into a
	// body before a runner can be found in it.
	managerScript = regexp.MustCompile(`^(npm|pnpm|yarn|bun)[[:space:]]+(?:run[[:space:]]+)?([A-Za-z0-9_:.-]+)[[:space:]]*$`)
	segmentBreak  = regexp.MustCompile(`&&|\|\||;`)
)

// scriptExpansions bounds how far a `<manager> run x` is followed into
// package.json before the reader gives up.
//
// Four is derived from the deepest chain a script can usefully be: a `test`
// that runs `test:unit` that runs `vitest`, plus one. Past that the project is
// not describing a runner, and a reader that followed a cycle would never
// return.
const scriptExpansions = 4

// ReadingStrategy is the first rung of [ReadingStrategies]: the most faithful
// way this project's checks can be read. It is what a caller taking exactly one
// reading uses.
func ReadingStrategy(workspace string, plan Plan, focus Focus) (Strategy, bool) {
	ladder, ok := ReadingStrategies(workspace, plan, focus)
	if !ok {
		return Strategy{}, false
	}
	return ladder[0], true
}

// expandScript follows a named target back to the commands it actually runs,
// and says which declaration it ended at.
//
// Discovery names a project's verification the way the project's own users
// invoke it — `pnpm test`, `make check` — and that name says nothing about what
// runs. The body is where the runner is, and it is also where the project's own
// flags for that runner are: its config path, its scope, its coverage settings.
// Following the name to the body is what keeps those.
func expandScript(root, command string) (body, source string) {
	body = strings.TrimSpace(command)
	for hop := 0; hop < scriptExpansions; hop++ {
		if match := managerScript.FindStringSubmatch(body); match != nil {
			scripts, ok := packageScripts(root)
			if !ok {
				return body, source
			}
			next, ok := scripts[match[2]]
			if !ok || strings.TrimSpace(next) == "" {
				return body, source
			}
			body, source = strings.TrimSpace(next), "package.json#scripts."+match[2]
			continue
		}
		if match := recipeInvocation.FindStringSubmatch(body); match != nil {
			next, file, ok := recipeBody(root, match[1], match[2])
			if !ok {
				return body, source
			}
			body, source = next, file+"#"+match[2]
			continue
		}
		return body, source
	}
	return body, source
}

// recipeInvocation is `make <target>` or `just <target>`, which is how
// discovery.go spells a Makefile or Justfile target.
var recipeInvocation = regexp.MustCompile(`^(make|just)[[:space:]]+([A-Za-z0-9_.-]+)[[:space:]]*$`)

// recipeBody is the commands a make or just target runs, joined into one body.
//
// It reads the recipe lines that follow the target's own header — indented in a
// Makefile, indented under the name in a Justfile — and stops at the first line
// that is not part of the recipe. A recipe line's leading @, - and + are the
// tool's own prefixes for "do not echo" and "ignore failure" and are not part of
// the command.
func recipeBody(root, tool, target string) (body, file string, ok bool) {
	files := []string{"Makefile", "makefile", "GNUmakefile"}
	if tool == "just" {
		files = []string{"Justfile", "justfile"}
	}
	for _, name := range files {
		text, found := readSmallFile(filepath.Join(root, name))
		if !found {
			continue
		}
		lines := strings.Split(text, "\n")
		for index, line := range lines {
			header := recipeHeader.FindStringSubmatch(line)
			if header == nil || header[1] != target {
				continue
			}
			var recipe []string
			for _, next := range lines[index+1:] {
				if strings.TrimSpace(next) == "" {
					continue
				}
				if next[0] != '\t' && !strings.HasPrefix(next, "    ") {
					break
				}
				recipe = append(recipe, strings.TrimLeft(strings.TrimSpace(next), "@-+"))
			}
			if len(recipe) == 0 {
				return "", "", false
			}
			return expandVariables(strings.Join(recipe, " ; "), lines), name, true
		}
	}
	return "", "", false
}

// recipeHeader is a target's own line: a name, a colon, and its prerequisites.
var recipeHeader = regexp.MustCompile(`^([A-Za-z0-9_.-]+)[[:space:]]*:(?:[^=]|$)`)

var (
	// A file-scope assignment: `run := poetry run`, `PYTEST = python -m pytest`.
	variableAssignment = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*[:?+]?=[[:space:]]*(.*)$`)
	// A reference to one, in either spelling a recipe may use.
	variableReference = regexp.MustCompile(`\$[({]([A-Za-z_][A-Za-z0-9_]*)[)}]`)
)

// expandVariables substitutes a recipe's own variables from the file it lives
// in, so the command a target actually runs is visible.
//
// It is the file's OWN table and nothing else — no environment, no defaults, no
// guesses. A reference with no assignment behind it is dropped rather than left
// standing, because a literal `$(ARGS)` in the middle of a command line is not
// something a shell can be handed, and an unset make variable expands to nothing
// there too. That is the same reading make itself gives it.
//
// This is what makes a recipe legible at all. textual declares
// `run := poetry run` and then `$(run) pytest tests/ -n 16 --dist=loadgroup
// $(ARGS)`, and a reader that took `$(run)` for the command name found no runner
// in the recipe, kept none of `tests/`, and ran the whole repository instead.
func expandVariables(body string, lines []string) string {
	table := map[string]string{}
	for _, line := range lines {
		if line == "" || line[0] == '\t' || line[0] == ' ' || line[0] == '#' {
			continue
		}
		if match := variableAssignment.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
			table[match[1]] = strings.TrimSpace(match[2])
		}
	}
	// Bounded for the reason scriptExpansions is: a variable that names itself
	// would otherwise never settle.
	for hop := 0; hop < scriptExpansions; hop++ {
		expanded := variableReference.ReplaceAllStringFunc(body, func(reference string) string {
			name := variableReference.FindStringSubmatch(reference)[1]
			return table[name]
		})
		if expanded == body {
			break
		}
		body = expanded
	}
	return strings.Join(strings.Fields(body), " ")
}

// ReadingStrategies is the ladder of ways this project's checks can be read,
// most specific first.
//
// There is a ladder because the most specific rung can fail in a way that says
// nothing about the suite. textual's own `make test` is `poetry run pytest
// tests/ -n 16 --dist=loadgroup` and the task image has no pytest-xdist, so that
// invocation exits in eight seconds on `unrecognized arguments: -n` — a reading
// that names nothing, of a suite that was never asked to run. A reader with one
// rung takes that for the whole answer; a reader with a ladder drops to the
// runner's own invocation and reads the suite.
//
// The ladder has two dimensions, and both of them are "most specific first".
//
// WHERE, from [Members]. A monorepo's root command is a fan-out: happy-dom's
// `npm test` is `turbo run test`, which at an uncompiled base commit dies inside
// turbo having named no check of any package, while the runner that names its
// 7,260 checks sits in packages/happy-dom's own manifest. So the packages the
// work touched are read first, most-touched first, and the root is the last
// place tried rather than the only one. A project that declares no workspace
// has exactly one place and this dimension collapses to nothing.
//
// HOW MUCH, from [Adjacent]. Inside each place the SCOPED rung comes first — the
// runner handed the checks that sit next to the change — and the whole-suite
// rungs come after it. textual's whole-repository reading collects 3,422 tests
// and takes 793 seconds against a budget of 5m30s, so the whole rung was the
// only rung and it never returned an answer at all.
//
// The rungs within one place, and why they are in this order:
//
//  1. The runner, handed the checks adjacent to the change. It is built from the
//     runner's OWN invocation rather than from the project's script, because
//     what is being replaced is the project's own scope — appending a selection
//     to a command that already names `tests/` selects both.
//  2. The runner as the PROJECT'S OWN script or recipe invokes it, with the
//     project's flags kept. This is the most faithful whole reading there is,
//     and it is the only rung that knows the project scoped its suite to
//     `tests/`.
//  3. The runner as it invokes itself, taken from what the project declares it
//     depends on and configures. It drops the project's flags, which is the
//     point: a flag that needs a plugin the environment lacks is what put us
//     here.
//  4. The project's declared entrypoint, run as it stands and read as plain
//     text. This is what every reading in this program was before strategies
//     existed, and it is the floor rather than an absence of one.
//
// Identical rungs are collapsed, so a project whose script already spells the
// runner plainly produces one strategy and one reading.
//
// ok is false exactly when there was nothing anywhere to run.
func ReadingStrategies(workspace string, plan Plan, focus Focus) ([]Strategy, bool) {
	var ladder []Strategy
	// The packages the work touched, most-touched first. Each is discovered in
	// its own right — a package declares its own runner, its own scripts and its
	// own config — which is the whole of what the root reader could not see.
	for _, member := range TouchedMembers(workspace, focus) {
		place := filepath.Join(workspace, filepath.FromSlash(member.Dir))
		ladder = append(ladder, placeStrategies(place, member.Dir, Discover(place), focus.Within(member.Dir))...)
	}
	ladder = append(ladder, placeStrategies(workspace, "", plan, focus)...)
	seen := map[string]bool{}
	distinct := make([]Strategy, 0, len(ladder))
	for _, rung := range ladder {
		key := rung.Workdir + "\x00" + rung.Command
		if rung.Empty() || seen[key] {
			continue
		}
		seen[key] = true
		distinct = append(distinct, rung)
	}
	return distinct, len(distinct) > 0
}

// placeStrategies is the ladder for ONE place — the workspace root, or one
// package of it — with the scoped rung in front of the whole ones.
//
// dir is that place relative to the workspace, and it is prefixed onto every
// rung's workdir rather than being a second field, because where a command runs
// is one fact and Strategy already holds it.
func placeStrategies(place, dir string, plan Plan, focus Focus) []Strategy {
	var entrypoint Entrypoint
	found := false
	for _, candidate := range plan.Entrypoints {
		if candidate.Kind == KindTest {
			entrypoint, found = candidate, true
			break
		}
	}
	if !found {
		return nil
	}
	plain := Strategy{
		Command: entrypoint.Command, Workdir: joinWorkdir(dir, entrypoint.Workdir),
		Read: FormatPlain, Source: entrypoint.Source, Declared: entrypoint.Command,
		Scope: ScopeWhole,
	}
	root := filepath.Join(place, entrypoint.Workdir)
	var whole []Strategy
	scriptRunner, scriptFound := runner{}, false
	body, source := expandScript(root, entrypoint.Command)
	if segment, chosen, ok := runnerSegment(body); ok {
		scriptRunner, scriptFound = chosen, true
		rung := plain
		rung.Runner, rung.Read = chosen.name, chosen.read
		rung.Command = machineReadable(root, segment, chosen)
		if source != "" {
			rung.Source = source
		}
		whole = append(whole, rung)
	}
	declared, declaredSource, declaredFound := declaredRunner(root)
	if declaredFound {
		rung := plain
		rung.Runner, rung.Read = declared.name, declared.read
		rung.Command = machineReadable(root, declared.invocation, declared)
		rung.Source = declaredSource
		whole = append(whole, rung)
	}
	whole = append(whole, plain)

	// The runner the scoped rung is built on is whichever one this place is
	// known to use, the script's own naming of it first.
	chosen, known := declared, declaredFound
	if scriptFound {
		chosen, known = scriptRunner, true
	}
	if !known {
		return whole
	}
	scoped, ok := scopedStrategy(root, plain, chosen, focus)
	if !ok {
		return whole
	}
	return append([]Strategy{scoped}, whole...)
}

// joinWorkdir puts a package's own directory in front of an entrypoint's, in the
// slash spelling every recorded path in this program uses.
func joinWorkdir(dir, workdir string) string {
	joined := strings.Trim(filepath.ToSlash(filepath.Join(dir, workdir)), "/")
	if joined == "." {
		return ""
	}
	return joined
}

// scopedStrategy is the reading of the checks that sit next to the change.
//
// It is built from the RUNNER'S OWN INVOCATION and never from the project's
// script, and that is the one decision in this function. A project's script
// carries the project's own scope — textual's says `tests/`, and pytest handed
// both `tests/` and `tests/test_rich_log.py` runs both — so a selection appended
// to it is not a selection at all. Dropping the script's flags is the same
// trade-off rung 3 already makes, for the same reason, and the config the runner
// needs is in the runner's own config file rather than on that command line.
//
// ok is false when this runner cannot be told what to run, or when nothing
// adjacent to the change was found. Both mean the ladder starts at the whole
// suite, which is where it started before scopes existed.
func scopedStrategy(root string, plain Strategy, chosen runner, focus Focus) (Strategy, bool) {
	if !chosen.selects || strings.TrimSpace(chosen.invocation) == "" {
		return Strategy{}, false
	}
	paths, ok := Adjacent(root, focus)
	if !ok {
		return Strategy{}, false
	}
	selectors := paths
	if chosen.selectsDirectories {
		selectors = selectedDirectories(paths)
	}
	rung := plain
	rung.Runner, rung.Read = chosen.name, chosen.read
	rung.Source = "the checks next to what this job touched"
	rung.Command = machineReadable(root, chosen.invocation, chosen) + " " + strings.Join(selectors, " ")
	rung.Scope = fmt.Sprintf("touched packages (%d %s)", len(paths), plural(len(paths), "file"))
	return rung, true
}

// selectedDirectories turns a selection of files into the packages holding them,
// in the spelling a toolchain that is handed packages expects. `go test` is the
// case: a package is a directory, and `./internal/verify/...` is how one is
// named on its own command line.
func selectedDirectories(paths []string) []string {
	seen := map[string]bool{}
	dirs := make([]string, 0, len(paths))
	for _, path := range paths {
		dir := pathDir(path)
		selector := "."
		if dir != "." && dir != "" {
			selector = "./" + dir + "/..."
		}
		if seen[selector] {
			continue
		}
		seen[selector] = true
		dirs = append(dirs, selector)
	}
	sort.Strings(dirs)
	return dirs
}

// plural is the one place this file spells the difference between one thing and
// several, so a scope sentence never reads "1 files".
func plural(count int, word string) string {
	if count == 1 {
		return word
	}
	return word + "s"
}

// packageScripts is package.json's own scripts map, or nothing.
func packageScripts(root string) (map[string]string, bool) {
	raw, ok := readSmallFile(filepath.Join(root, "package.json"))
	if !ok {
		return nil, false
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal([]byte(raw), &manifest) != nil || len(manifest.Scripts) == 0 {
		return nil, false
	}
	return manifest.Scripts, true
}

// runnerSegment finds the runner invocation inside a script body.
//
// A body is a sequence of commands and only one of them names checks. Reading
// the LAST matching segment rather than the first is deliberate: a lifecycle
// script lints and typechecks on its way to the suite, and the suite is what
// this is a reading of.
func runnerSegment(body string) (segment string, chosen runner, ok bool) {
	for _, raw := range segmentBreak.Split(body, -1) {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}
		bare := execPrefixes.ReplaceAllString(envAssignments.ReplaceAllString(candidate, ""), "")
		fields := strings.Fields(bare)
		if len(fields) == 0 {
			continue
		}
		head := filepath.Base(fields[0])
		for _, known := range runners {
			if !namesRunner(known, head, fields[1:]) {
				continue
			}
			segment, chosen, ok = candidate, known, true
		}
	}
	return segment, chosen, ok
}

// namesRunner reports that this invocation is this runner's. A runner spelled as
// an interpreter module — `python -m pytest` — is recognised through the `-m`
// rather than through the interpreter's name, because the interpreter is not the
// runner and several runners share it.
func namesRunner(known runner, head string, args []string) bool {
	if known.binary != "" && (head == known.binary || strings.HasPrefix(head, known.binary+".")) {
		// `go` and `cargo` are whole toolchains; only their test subcommand is
		// a reading. The subcommand is the first argument that is not a flag.
		if known.binary == "go" || known.binary == "cargo" {
			return firstWord(args) == "test"
		}
		return true
	}
	if known.module == "" {
		return false
	}
	for index, arg := range args {
		if arg == "-m" && index+1 < len(args) {
			return args[index+1] == known.module
		}
	}
	return false
}

// firstWord is the first argument that is not a flag.
func firstWord(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}
	return ""
}

// declaredRunner asks the project's own manifests and config files which runner
// checks it, in the table's order.
func declaredRunner(root string) (chosen runner, source string, ok bool) {
	dependencies := packageDependencies(root)
	for _, known := range runners {
		if known.dependency != "" {
			if version, declared := dependencies[known.dependency]; declared {
				return known, "package.json#" + known.dependency + "@" + version, true
			}
		}
		for _, file := range known.declaredBy {
			if fileExists(filepath.Join(root, file)) {
				return known, file, true
			}
		}
	}
	return runner{}, "", false
}

// packageDependencies is every package this project declares itself to depend
// on, whichever map it declared it in. A runner is a dependency like any other,
// and which map it sits in is a packaging decision that says nothing about
// whether the project is checked with it.
func packageDependencies(root string) map[string]string {
	raw, ok := readSmallFile(filepath.Join(root, "package.json"))
	if !ok {
		return nil
	}
	var manifest struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
		PeerText        map[string]string `json:"peerDependencies"`
	}
	if json.Unmarshal([]byte(raw), &manifest) != nil {
		return nil
	}
	all := map[string]string{}
	for _, group := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.PeerText} {
		for name, version := range group {
			all[name] = version
		}
	}
	return all
}

// machineReadable turns one runner invocation into the reading of it: the
// project's own flags kept, the run subcommand supplied where the runner's
// default is not a single run, the machine-readable flags appended, and a
// launcher prefixed where the runner lives in a directory that is not on PATH.
func machineReadable(root, invocation string, chosen runner) string {
	invocation = strings.TrimSpace(invocation)
	environment := envAssignments.FindString(invocation)
	command := strings.TrimPrefix(invocation, environment)
	if len(chosen.runArgs) > 0 && !carriesAny(command, chosen.runArgs) {
		// Inserted after the binary rather than appended, because a runner
		// reads its subcommand positionally and a trailing `run` would be read
		// as a file to test.
		fields := strings.Fields(command)
		fields = append(fields[:1], append([]string{chosen.runArgs[0]}, fields[1:]...)...)
		command = strings.Join(fields, " ")
	}
	if chosen.binary == "go" {
		// go test reads its flags before its packages, so -json is inserted at
		// the subcommand rather than appended after `./...`.
		command = insertAfter(command, "test", "-json")
	} else {
		for _, arg := range chosen.machineArgs {
			if !carriesAny(command, []string{arg}) {
				command += " " + arg
			}
		}
	}
	if needsLauncher(chosen) && execPrefixes.FindString(command) == "" {
		command = managerExec[packageManager(root)] + " " + command
	}
	return strings.TrimSpace(environment + command)
}

// needsLauncher says this runner is installed into a project directory rather
// than onto PATH. It is a property of the ecosystem's own packaging: a
// JavaScript runner lives in node_modules/.bin and is reached through the
// package manager, and everything else is a program.
func needsLauncher(chosen runner) bool { return chosen.dependency != "" }

func carriesAny(command string, words []string) bool {
	fields := strings.Fields(command)
	for _, field := range fields[min(1, len(fields)):] {
		for _, word := range words {
			if field == word || strings.HasPrefix(field, word+"=") {
				return true
			}
		}
	}
	return false
}

func insertAfter(command, after, flag string) string {
	fields := strings.Fields(command)
	for index, field := range fields {
		if field != after {
			continue
		}
		if carriesAny(command, []string{flag}) {
			return command
		}
		rest := append([]string{flag}, fields[index+1:]...)
		return strings.Join(append(fields[:index+1:index+1], rest...), " ")
	}
	return command + " " + flag
}

// Read turns one reading's bytes into the roster and the red half of it.
//
// ok is false when the format's own parser found nothing it recognised, and the
// caller then reads the same bytes as plain text. THAT FALLBACK IS THE WHOLE
// FAIL-SAFE DIRECTION OF THIS FILE: a runner that ignored the flag, a version
// whose reporter moved, a suite that died before printing its document — each of
// them lands exactly where every reading landed before strategies existed, and
// none of them can turn a red suite green.
func (f Format) Read(output string) (reported, failing []string, ok bool) {
	switch f {
	case FormatNodeJSON:
		return parseNodeJSON(output)
	case FormatGoJSON:
		return parseGoJSON(output)
	default:
		return ReportedTests(output), FailingTests(output), true
	}
}

// nodeReport is the document vitest's `--reporter=json` and jest's `--json`
// both print. Only the fields a roster needs are named.
type nodeReport struct {
	TestResults []struct {
		Name             string `json:"name"`
		AssertionResults []struct {
			FullName    string   `json:"fullName"`
			Title       string   `json:"title"`
			AncestorAll []string `json:"ancestorTitles"`
			Status      string   `json:"status"`
		} `json:"assertionResults"`
	} `json:"testResults"`
}

// parseNodeJSON reads that document out of whatever else the runner printed
// around it.
//
// The document is found rather than assumed to be the whole output: a runner
// prints warnings, a package manager prints a banner, and a reader that decoded
// only a pristine stdout would fall back to plain text on every real project.
func parseNodeJSON(output string) (reported, failing []string, ok bool) {
	report, found := decodeJSONObject[nodeReport](output)
	if !found || len(report.TestResults) == 0 {
		return nil, nil, false
	}
	seen, redSeen := map[string]bool{}, map[string]bool{}
	for _, file := range report.TestResults {
		for _, assertion := range file.AssertionResults {
			name := strings.TrimSpace(assertion.FullName)
			if name == "" {
				name = strings.TrimSpace(strings.Join(append(assertion.AncestorAll, assertion.Title), " > "))
			}
			if name = normalizeTestName(name); name == "" {
				continue
			}
			if !seen[name] {
				seen[name] = true
				reported = append(reported, name)
			}
			if assertion.Status == "failed" && !redSeen[name] {
				redSeen[name] = true
				failing = append(failing, name)
			}
		}
	}
	if len(reported) == 0 {
		return nil, nil, false
	}
	sortNames(reported)
	sortNames(failing)
	return reported, failing, true
}

// parseGoJSON reads `go test -json`: one object a line, each an action on a
// package or on a named test within it.
func parseGoJSON(output string) (reported, failing []string, ok bool) {
	seen, redSeen := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event struct {
			Action  string `json:"Action"`
			Package string `json:"Package"`
			Test    string `json:"Test"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Action != "pass" && event.Action != "fail" {
			continue
		}
		// A package-level action is a summary of the tests already named. It is
		// kept only when it names no test, so a package with no tests at all is
		// still an identity the roster holds.
		name := event.Test
		if name == "" {
			name = event.Package
		} else {
			name = event.Package + "." + name
		}
		if name = normalizeTestName(name); name == "" {
			continue
		}
		if !seen[name] {
			seen[name] = true
			reported = append(reported, name)
		}
		if event.Action == "fail" && !redSeen[name] {
			redSeen[name] = true
			failing = append(failing, name)
		}
	}
	if len(reported) == 0 {
		return nil, nil, false
	}
	sortNames(reported)
	sortNames(failing)
	return reported, failing, true
}

// decodeJSONObject finds the first JSON object in a body of text that decodes
// into T and says whether it found one.
func decodeJSONObject[T any](output string) (T, bool) {
	var decoded T
	for offset := strings.IndexByte(output, '{'); offset >= 0; {
		if json.Unmarshal([]byte(output[offset:]), &decoded) == nil {
			return decoded, true
		}
		// A decoder that stops at trailing bytes is the common case: a runner
		// prints its document and then a summary line. Decode the longest
		// prefix that is an object instead.
		if value, ok := decodePrefix[T](output[offset:]); ok {
			return value, true
		}
		next := strings.IndexByte(output[offset+1:], '{')
		if next < 0 {
			break
		}
		offset += next + 1
	}
	return decoded, false
}

// decodePrefix decodes the first complete JSON value in the text and ignores
// whatever follows it.
func decodePrefix[T any](text string) (T, bool) {
	var decoded T
	decoder := json.NewDecoder(strings.NewReader(text))
	if err := decoder.Decode(&decoded); err != nil {
		return decoded, false
	}
	return decoded, true
}

// sortNames orders a roster so two readings of one suite compare as sets rather
// than as transcripts. It is the same ordering FailingTests and ReportedTests
// apply, named once here so the structured readers and the plain one cannot
// disagree about it.
func sortNames(names []string) { sort.Strings(names) }
