// Package verify discovers the project-wide build and test entrypoints that
// form the session-end machine verification floor, runs one of them, and reads
// the failing test names out of what it printed.
//
// It lived under internal/swepro/internal/session/fullverification until
// 2026-08-29, where Go's own visibility rule made it reachable by exactly one
// worker. That is the WHY of the move, and it is a law about laws: A LAW ABOUT
// A PROJECT'S OWN VERIFICATION THAT ONLY ONE WORKER CAN REACH IS A LAW THE
// OTHER WORKERS SILENTLY DO WITHOUT. The bare and linear workers — the only
// workers `aforge do` uses on the plain path — could not import this, so they
// never photographed the repository, never compared two readings of it, and
// shipped patches that deleted attributes the repository already had while
// their own narrow tests stayed green (docs/design/gate/SETTLEMENT.md §4).
//
// Nothing here knows what language the workspace is in, and nothing here is a
// gate. It discovers, it runs, it reads, and it subtracts; what a caller does
// with a new red name is the caller's business.
package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EntrypointKind string

const (
	KindBuild EntrypointKind = "build"
	KindTest  EntrypointKind = "test"
)

type Entrypoint struct {
	Kind    EntrypointKind `json:"kind"`
	Command string         `json:"command"`
	Workdir string         `json:"workdir,omitempty"`
	Source  string         `json:"source"`
}

type Plan struct {
	Entrypoints []Entrypoint `json:"entrypoints"`
	// BuildExpected reports whether this project is required to have a
	// build/typecheck step. True for any accountable workspace — including one
	// whose ecosystem we do not recognize — so the gate stays fail-closed; a
	// plain Python package is the one case we can positively identify as
	// having nothing to compile.
	BuildExpected bool `json:"buildExpected"`
	// TestExpected reports whether this project is required to have a test
	// step. True for any accountable workspace: unlike a build, no ecosystem
	// is exempt from having tests. Both flags are false only for a workspace
	// that is not accountable at all (see accountableWorkspace).
	TestExpected bool `json:"testExpected"`
}

var (
	testCommandPattern       = regexp.MustCompile(`(?i)(?:^|(?:&&|\|\||[;&|])[[:space:]]*)(?:env[[:space:]]+)?(?:[A-Z_][A-Z0-9_]*=[^[:space:]]+[[:space:]]+)*(?:go[[:space:]]+test|cargo[[:space:]]+test|bun[[:space:]]+(?:run[[:space:]]+)?(?:test(?::unit)?|unit|verify|check)|npm[[:space:]]+(?:run[[:space:]]+)?(?:test(?::unit)?|unit|verify|check)|pnpm[[:space:]]+(?:run[[:space:]]+)?(?:test(?::unit)?|unit|verify|check)|yarn[[:space:]]+(?:run[[:space:]]+)?(?:test(?::unit)?|unit|verify|check)|python(?:3)?[[:space:]]+-m[[:space:]]+(?:pytest|unittest)|pytest|tox|nox|make[[:space:]]+(?:test|check|verify)|just[[:space:]]+(?:test|check|verify)|(?:\./)?mvnw?[[:space:]].*(?:test|verify)|\./gradlew[[:space:]].*(?:test|check)|dotnet[[:space:]]+test|ctest(?:[[:space:]]|$))`)
	buildCommandPattern      = regexp.MustCompile(`(?i)(?:^|(?:&&|\|\||[;&|])[[:space:]]*)(?:env[[:space:]]+)?(?:[A-Z_][A-Z0-9_]*=[^[:space:]]+[[:space:]]+)*(?:go[[:space:]]+build|cargo[[:space:]]+build|npm[[:space:]]+run[[:space:]]+(?:build|compile|typecheck)|pnpm[[:space:]]+(?:run[[:space:]]+)?(?:build|compile|typecheck)|yarn[[:space:]]+(?:run[[:space:]]+)?(?:build|compile|typecheck)|bun[[:space:]]+run[[:space:]]+(?:build|compile|typecheck)|make[[:space:]]+(?:build|all)|just[[:space:]]+(?:build|all)|(?:\./)?mvnw?[[:space:]].*(?:package|compile)|\./gradlew[[:space:]].*(?:build|assemble)|dotnet[[:space:]]+build|cmake[[:space:]]+--build|python(?:3)?[[:space:]]+-m[[:space:]]+(?:build|compileall|mypy|pyright|ruff[[:space:]]+check)|(?:\./)?(?:mypy|pyright)(?:[[:space:]]|$)|(?:\./)?ruff[[:space:]]+check|tsc(?:[[:space:]]|$))`)
	makeTargetPattern        = regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+)[[:space:]]*:(?:[^=]|$)`)
	inlineCodePattern        = regexp.MustCompile("`([^`\n]+)`")
	leadingCDPattern         = regexp.MustCompile(`^cd[[:space:]]+((?:'[^']*'|"[^"]*"|[^;&|[:space:]]+))[[:space:]]*&&[[:space:]]*(.+)$`)
	standaloneCDPattern      = regexp.MustCompile(`^cd[[:space:]]+((?:'[^']*'|"[^"]*"|[^;&|[:space:]]+))[[:space:]]*$`)
	interactiveRunnerPattern = regexp.MustCompile(`(?i)(?:^|(?:&&|\|\||[;&|])[[:space:]]*)cypress[[:space:]]+open(?:[[:space:]]|$)`)
	heredocPattern           = regexp.MustCompile(`<<-?[[:space:]]*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	numericFlagPattern       = regexp.MustCompile(`^[0-9]+$`)

	// Any package-manager invocation disqualifies a script from being chosen
	// as the build entrypoint. `npm run x` hides another script's effects, and
	// `npm publish` / `npm version` are outright destructive — a body like
	// "tsc -p tsconfig.json && npm publish" reads as a compile right up to the
	// point where satisfying the verification gate ships a release. A false
	// negative here costs a discovered entrypoint; a false positive publishes a
	// package. npx is excluded from the ban: it is a runner, not a lifecycle
	// manager, so `npx tsc` stays selectable.
	packageScriptDelegationPattern = regexp.MustCompile(
		`(?i)(?:^|[^[:alnum:]_./-])(?:npm|pnpm|yarn|bun)(?:[[:space:]]|$)`)
)

// Discover follows the auditor convention in src/baked/agents/auditor.md:
// prefer CI, then repository instructions, declared scripts/manifests, and
// finally ecosystem defaults. Each role gets one project-wide entrypoint.
func Discover(workspace string) Plan {
	selected := map[EntrypointKind]Entrypoint{}
	add := func(kind EntrypointKind, command, workdir, source string) {
		command = normalizeCommand(command)
		if command == "" {
			return
		}
		if _, exists := selected[kind]; !exists {
			selected[kind] = Entrypoint{
				Kind: kind, Command: command, Workdir: workdir, Source: source,
			}
		}
	}
	addCandidates := func(candidates []commandCandidate) {
		for _, candidate := range candidates {
			if candidate.kind != "" {
				add(candidate.kind, candidate.command, candidate.workdir, candidate.source)
				continue
			}
			if isBuildCommand(candidate.command) {
				add(KindBuild, candidate.command, candidate.workdir, candidate.source)
			}
			if isTestCommand(candidate.command) && !isInteractiveTestCommand(candidate.command) {
				add(KindTest, candidate.command, candidate.workdir, candidate.source)
			}
		}
	}

	addCandidates(ciCandidates(workspace))
	addCandidates(documentCandidates(workspace, []string{"AGENTS.md"}))
	addCandidates(scriptCandidates(workspace))
	addCandidates(documentCandidates(workspace, []string{
		"README.md", "README", "CONTRIBUTING.md", "CONTRIBUTING",
	}))

	defaults := ecosystemDefaults(workspace)
	for _, entrypoint := range defaults {
		add(entrypoint.Kind, entrypoint.Command, entrypoint.Workdir, entrypoint.Source)
	}

	_, hasBuild := selected[KindBuild]
	_, hasTest := selected[KindTest]
	// Both demands hang off one question: is there a project here to hold to a
	// standard? Discovering any command answers it outright — someone wrote
	// that command down — and otherwise the ecosystem markers decide.
	accountable := hasBuild || hasTest || accountableWorkspace(workspace)
	plan := Plan{
		Entrypoints:   []Entrypoint{},
		BuildExpected: hasBuild || (accountable && !ecosystemLacksBuild(workspace)),
		TestExpected:  accountable,
	}
	for _, kind := range []EntrypointKind{KindBuild, KindTest} {
		if entrypoint, ok := selected[kind]; ok {
			plan.Entrypoints = append(plan.Entrypoints, entrypoint)
		}
	}
	return plan
}

type commandCandidate struct {
	command string
	workdir string
	source  string
	// kind pins the classification when the caller already established it from
	// something other than the command text. `npm run check:type:js` is a
	// typecheck entrypoint, but only its script BODY says so — the invocation
	// itself is indistinguishable from any other named script.
	kind EntrypointKind
}

func ciCandidates(workspace string) []commandCandidate {
	paths := []string{
		".gitlab-ci.yml", "azure-pipelines.yml", "bitbucket-pipelines.yml",
		filepath.Join(".circleci", "config.yml"),
	}
	workflows, _ := filepath.Glob(filepath.Join(workspace, ".github", "workflows", "*.y*ml"))
	for _, path := range workflows {
		relative, err := filepath.Rel(workspace, path)
		if err == nil {
			paths = append(paths, relative)
		}
	}
	sort.Strings(paths)
	var candidates []commandCandidate
	for _, relative := range paths {
		body, ok := readSmallFile(filepath.Join(workspace, relative))
		if !ok {
			continue
		}
		var document yaml.Node
		if yaml.Unmarshal([]byte(body), &document) != nil {
			continue
		}
		candidates = appendYAMLCommandCandidates(candidates, &document, "", relative)
	}
	return candidates
}

func appendYAMLCommandCandidates(out []commandCandidate, node *yaml.Node, inheritedWorkdir, source string) []commandCandidate {
	workdir := inheritedWorkdir
	if node.Kind == yaml.MappingNode {
		if defaults := yamlMappingValue(node, "defaults"); defaults != nil {
			if run := yamlMappingValue(defaults, "run"); run != nil {
				if value := yamlMappingValue(run, "working-directory"); value != nil && value.Kind == yaml.ScalarNode {
					workdir = strings.TrimSpace(value.Value)
				}
			}
		}
		if value := yamlMappingValue(node, "working-directory"); value != nil && value.Kind == yaml.ScalarNode {
			workdir = strings.TrimSpace(value.Value)
		}
		for index := 0; index+1 < len(node.Content); index += 2 {
			key, value := node.Content[index].Value, node.Content[index+1]
			if key == "run" || key == "script" {
				out = appendYAMLCommandValue(out, value, workdir, source)
			}
		}
	}
	for _, child := range node.Content {
		out = appendYAMLCommandCandidates(out, child, workdir, source)
	}
	return out
}

func yamlMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		if node.Content[index].Value == key {
			return node.Content[index+1]
		}
	}
	return nil
}

func appendYAMLCommandValue(out []commandCandidate, node *yaml.Node, workdir, source string) []commandCandidate {
	switch node.Kind {
	case yaml.ScalarNode:
		return appendCIShellCandidates(out, node.Value, workdir, source)
	case yaml.SequenceNode:
		for _, child := range node.Content {
			if child.Kind == yaml.ScalarNode {
				out = appendCIShellCandidates(out, child.Value, workdir, source)
			}
		}
	}
	return out
}

func appendCIShellCandidates(out []commandCandidate, raw, workdir, source string) []commandCandidate {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	if !strings.Contains(raw, "\n") {
		return appendShellCandidatesFrom(out, raw, workdir, source)
	}
	activeWorkdir := workdir
	pending := ""
	heredocEnd := ""
	for _, line := range strings.Split(raw, "\n") {
		if heredocEnd != "" {
			if strings.TrimSpace(line) == heredocEnd {
				heredocEnd = ""
			}
			continue
		}
		logical := line
		if pending != "" {
			logical = pending + strings.TrimSpace(line)
		}
		if shellLineContinues(logical) {
			pending = strings.TrimSpace(strings.TrimSuffix(strings.TrimRight(logical, " \t"), "\\")) + " "
			continue
		}
		pending = ""
		command := normalizeCommand(logical)
		if command == "" {
			continue
		}
		if match := standaloneCDPattern.FindStringSubmatch(command); match != nil {
			activeWorkdir = combineWorkingDirectories(activeWorkdir, strings.Trim(match[1], "\"'"))
			continue
		}
		out = appendShellCandidatesFrom(out, command, activeWorkdir, source)
		if match := heredocPattern.FindStringSubmatch(logical); match != nil {
			heredocEnd = match[1]
		}
	}
	return out
}

func shellLineContinues(line string) bool {
	line = strings.TrimRight(line, " \t")
	backslashes := 0
	for index := len(line) - 1; index >= 0 && line[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func documentCandidates(workspace string, names []string) []commandCandidate {
	var candidates []commandCandidate
	for _, relative := range names {
		body, ok := readSmallFile(filepath.Join(workspace, relative))
		if !ok {
			continue
		}
		inFence := false
		for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				inFence = !inFence
				continue
			}
			for _, match := range inlineCodePattern.FindAllStringSubmatch(line, -1) {
				candidates = appendShellCandidates(candidates, match[1], relative)
			}
			if inFence || strings.HasPrefix(trimmed, "$") {
				candidates = appendShellCandidates(candidates, strings.TrimSpace(strings.TrimPrefix(trimmed, "$")), relative)
			}
		}
	}
	return candidates
}

func scriptCandidates(workspace string) []commandCandidate {
	var candidates []commandCandidate
	if body, ok := readSmallFile(filepath.Join(workspace, "package.json")); ok {
		var manifest struct {
			Scripts map[string]string `json:"scripts"`
		}
		if json.Unmarshal([]byte(body), &manifest) == nil {
			manager := packageManager(workspace)
			foundBuild := false
			for _, script := range []string{"build", "compile", "typecheck"} {
				if _, ok := manifest.Scripts[script]; !ok {
					continue
				}
				candidates = append(candidates, commandCandidate{
					command: managerRun(manager, script), source: "package.json#scripts." + script,
				})
				foundBuild = true
				break
			}
			if !foundBuild {
				// A project may name its compile/typecheck script anything.
				// commander.js declares `check:type:ts` -> `tsd && tsc -p
				// tsconfig.ts.json`; matching only on the three blessed NAMES
				// missed it, the gate reported "no standard build/typecheck
				// entrypoint was discoverable", and the agent's rational way out
				// was to invent one — its delivered patch added
				//   "build": "echo \"commander is shipped as source ...\""
				// which upstream would never merge. isBuildCommand already knows
				// what a build/typecheck command looks like, so apply it to the
				// script BODY rather than requiring a blessed name.
				//
				// Leaf scripts only. A body that shells out to other package
				// scripts can carry a publish or deploy step alongside the
				// compile, and running that to satisfy a verification gate would
				// be far worse than failing the gate. Names are sorted so the
				// choice is deterministic across runs.
				for _, name := range sortedScriptNames(manifest.Scripts) {
					script := manifest.Scripts[name]
					if scriptRunsOtherScripts(script) || !isBuildCommand(script) {
						continue
					}
					if isTestCommand(script) || isInteractiveTestCommand(script) {
						continue
					}
					candidates = append(candidates, commandCandidate{
						command: managerRun(manager, name),
						source:  "package.json#scripts." + name,
						kind:    KindBuild,
					})
					break
				}
			}
			for _, script := range []string{"test", "unit", "test:unit", "verify", "check"} {
				body, ok := manifest.Scripts[script]
				if !ok || isInteractiveTestCommand(body) {
					continue
				}
				command := managerRun(manager, script)
				if script == "test" {
					command = managerTest(manager)
				}
				candidates = append(candidates, commandCandidate{command: command, source: "package.json#scripts." + script})
				break
			}
		}
	}
	for _, file := range []string{"Makefile", "makefile", "GNUmakefile", "Justfile", "justfile"} {
		body, ok := readSmallFile(filepath.Join(workspace, file))
		if !ok {
			continue
		}
		targets := map[string]bool{}
		for _, match := range makeTargetPattern.FindAllStringSubmatch(body, -1) {
			targets[strings.ToLower(match[1])] = true
		}
		command := "make "
		if strings.EqualFold(file, "Justfile") {
			command = "just "
		}
		for _, target := range []string{"build", "all"} {
			if targets[target] {
				candidates = append(candidates, commandCandidate{command: command + target, source: file + "#" + target})
				break
			}
		}
		for _, target := range []string{"test", "check", "verify"} {
			if targets[target] {
				candidates = append(candidates, commandCandidate{command: command + target, source: file + "#" + target})
				break
			}
		}
	}
	return candidates
}

// accountableWorkspace reports whether the workspace looks like a software
// project at all: a language manifest, a build system, or a test suite. It is
// the precondition for BOTH verification demands.
//
// A workspace with none of these — a fresh `git init` carrying a README, a
// directory of loose data files — cannot satisfy either demand no matter what
// an agent does to it. There is nothing to compile and nothing to test, so
// "no build entrypoint was discoverable" and "no test entrypoint was
// discoverable" are not defects to repair; they are descriptions of an empty
// room. Reporting them as verification failures sends the audit-fix loop after
// a target that does not exist, and it runs until the cost ceiling stops it
// with the requested deliverable already sitting on disk.
//
// Recognizing a project is deliberately generous: anything here means the
// full fail-closed floor applies, so a real repository whose test command is
// merely undiscoverable still fails, which is the point of the floor.
func accountableWorkspace(workspace string) bool {
	for _, marker := range []string{
		// Language and dependency manifests.
		"go.mod", "Cargo.toml", "package.json", "deno.json", "deno.jsonc",
		"tsconfig.json", "pom.xml", "build.gradle", "build.gradle.kts",
		"gradlew", "Gemfile", "composer.json", "mix.exs", "pubspec.yaml",
		// Build systems that stand in for a manifest.
		"Makefile", "makefile", "GNUmakefile", "justfile", "Justfile",
		"CMakeLists.txt", "meson.build", "BUILD", "BUILD.bazel",
		// Test configuration implies a suite even with no manifest at all.
		"pytest.ini", "tox.ini", "noxfile.py", "conftest.py",
		"phpunit.xml", "phpunit.xml.dist", ".rspec",
	} {
		if fileExists(filepath.Join(workspace, marker)) {
			return true
		}
	}
	for _, runner := range []string{"jest", "vitest", "playwright", "karma", "cypress"} {
		for _, ext := range []string{".js", ".ts", ".mjs", ".cjs", ".json"} {
			if fileExists(filepath.Join(workspace, runner+".config"+ext)) {
				return true
			}
		}
	}
	if hasSuffixFile(workspace, ".sln") || hasSuffixFile(workspace, ".csproj") {
		return true
	}
	// Covers packaging metadata, Python test config, and test_*.py layouts.
	if isPythonProject(workspace) {
		return true
	}
	return hasTestDirectory(workspace)
}

// hasTestDirectory reports a conventional test directory at the project root —
// the last signal that a suite is expected when no manifest names one.
func hasTestDirectory(workspace string) bool {
	for _, dir := range []string{"tests", "test", "spec", "specs", "__tests__"} {
		if info, err := os.Stat(filepath.Join(workspace, dir)); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// ecosystemLacksBuild reports the one ecosystem we can positively identify as
// having no build or typecheck step: a plain Python package, which has tests to
// run but nothing to compile. Every other project — including one whose
// ecosystem we do not recognize — is still required to produce a build
// entrypoint, so the gate stays fail-closed by default.
func ecosystemLacksBuild(workspace string) bool {
	if isPythonProject(workspace) {
		lacks := true
		for _, marker := range []string{
			"go.mod", "Cargo.toml", "pom.xml", "gradlew", "package.json", "tsconfig.json",
		} {
			if fileExists(filepath.Join(workspace, marker)) {
				lacks = false
				break
			}
		}
		if lacks {
			return true
		}
	}
	// The same carve-out the plain-Python case gets. A package.json project
	// with no TypeScript config compiles nothing, so demanding a build
	// entrypoint from it is unsatisfiable by construction — and an agent facing
	// an unsatisfiable gate fabricates a no-op `build` script to get past it.
	// Discover still overrides this the moment any build/typecheck step is
	// found, from a script, CI, or the repository instructions.
	if fileExists(filepath.Join(workspace, "package.json")) &&
		!fileExists(filepath.Join(workspace, "tsconfig.json")) &&
		!hasTypeScriptSources(workspace) {
		for _, marker := range []string{"go.mod", "Cargo.toml", "pom.xml", "gradlew"} {
			if fileExists(filepath.Join(workspace, marker)) {
				return false
			}
		}
		return true
	}
	return false
}

// hasTypeScriptSources reports whether the project ships TypeScript that a
// typecheck step would be expected to cover, without walking the whole tree.
func hasTypeScriptSources(workspace string) bool {
	for _, dir := range []string{".", "src", "lib", "types"} {
		for _, pattern := range []string{"*.ts", "*.tsx", "*.mts", "*.cts"} {
			matches, _ := filepath.Glob(filepath.Join(workspace, dir, pattern))
			if len(matches) > 0 {
				return true
			}
		}
	}
	return false
}

// sortedScriptNames gives package-script iteration a stable order; Go map
// ranging is randomized and the chosen entrypoint must not vary between runs.
func sortedScriptNames(scripts map[string]string) []string {
	names := make([]string, 0, len(scripts))
	for name := range scripts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// scriptRunsOtherScripts reports whether a package script delegates to other
// package scripts, which makes its full effect unknowable from its own body.
func scriptRunsOtherScripts(body string) bool {
	return packageScriptDelegationPattern.MatchString(body)
}

func ecosystemDefaults(workspace string) []Entrypoint {
	entries := []Entrypoint{}
	add := func(kind EntrypointKind, command, source string) {
		entries = append(entries, Entrypoint{Kind: kind, Command: command, Source: source})
	}
	switch {
	case fileExists(filepath.Join(workspace, "go.mod")):
		add(KindBuild, "go build ./...", "go.mod")
		add(KindTest, "go test ./...", "go.mod")
	case fileExists(filepath.Join(workspace, "Cargo.toml")):
		add(KindBuild, "cargo build --workspace", "Cargo.toml")
		add(KindTest, "cargo test --workspace", "Cargo.toml")
	case fileExists(filepath.Join(workspace, "pom.xml")):
		binary := "mvn"
		if fileExists(filepath.Join(workspace, "mvnw")) {
			binary = "./mvnw"
		}
		add(KindBuild, binary+" -DskipTests package", "pom.xml")
		add(KindTest, binary+" test", "pom.xml")
	case fileExists(filepath.Join(workspace, "gradlew")):
		add(KindBuild, "./gradlew assemble", "gradlew")
		add(KindTest, "./gradlew test", "gradlew")
	case hasSuffixFile(workspace, ".sln") || hasSuffixFile(workspace, ".csproj"):
		add(KindBuild, "dotnet build", "dotnet project")
		add(KindTest, "dotnet test", "dotnet project")
	case isPythonProject(workspace):
		if hasPythonTests(workspace) {
			add(KindTest, "python3 -m pytest", "Python test files")
		}
	}
	return entries
}

func appendShellCandidates(out []commandCandidate, raw, source string) []commandCandidate {
	return appendShellCandidatesFrom(out, raw, "", source)
}

func appendShellCandidatesFrom(out []commandCandidate, raw, baseWorkdir, source string) []commandCandidate {
	raw = strings.TrimSpace(strings.Trim(raw, "`"))
	if raw == "" || strings.Contains(raw, "${{") {
		return out
	}
	command, inlineWorkdir := preserveLeadingWorkingDirectory(raw)
	workdir := combineWorkingDirectories(baseWorkdir, inlineWorkdir)
	if isBuildCommand(command) || isTestCommand(command) {
		out = append(out, commandCandidate{
			command: command, workdir: workdir, source: source,
		})
	}
	return out
}

func preserveLeadingWorkingDirectory(raw string) (string, string) {
	command := normalizeCommand(raw)
	match := leadingCDPattern.FindStringSubmatch(command)
	if match == nil {
		return command, ""
	}
	workdir := strings.Trim(match[1], "\"'")
	return normalizeCommand(match[2]), workdir
}

func combineWorkingDirectories(base, nested string) string {
	if nested == "" {
		return base
	}
	if base == "" || filepath.IsAbs(nested) {
		return filepath.Clean(nested)
	}
	// A CI step starts in working-directory before its shell runs, so a relative
	// inline cd is appended to that directory; an absolute cd replaces it. This
	// preserves the clean-process semantics in src/baked/agents/auditor.md:53-55.
	return filepath.Clean(filepath.Join(base, nested))
}

func normalizeCommand(command string) string {
	command = executableShellText(strings.TrimSpace(strings.Trim(command, "`")))
	return strings.Join(strings.Fields(command), " ")
}

// executableShellText removes shell comments before command-name matching.
// The TS auditor requires independently executed commands, not command-shaped
// prose (src/baked/agents/auditor.md:55,59), so quoted hashes remain data while
// an unquoted # at a shell word boundary hides everything through the newline.
func executableShellText(command string) string {
	var out strings.Builder
	singleQuoted := false
	doubleQuoted := false
	escaped := false
	for index := 0; index < len(command); index++ {
		character := command[index]
		if escaped {
			out.WriteByte(character)
			escaped = false
			continue
		}
		if character == '\\' && !singleQuoted {
			out.WriteByte(character)
			escaped = true
			continue
		}
		if character == '\'' && !doubleQuoted {
			singleQuoted = !singleQuoted
			out.WriteByte(character)
			continue
		}
		if character == '"' && !singleQuoted {
			doubleQuoted = !doubleQuoted
			out.WriteByte(character)
			continue
		}
		if character == '#' && !singleQuoted && !doubleQuoted && shellCommentBoundary(command, index) {
			for index < len(command) && command[index] != '\n' {
				index++
			}
			if index < len(command) {
				out.WriteString(" ; ")
			}
			continue
		}
		if character == '\n' && !singleQuoted && !doubleQuoted {
			out.WriteString(" ; ")
			continue
		}
		out.WriteByte(character)
	}
	return out.String()
}

func shellCommentBoundary(command string, index int) bool {
	if index == 0 {
		return true
	}
	previous := command[index-1]
	return previous == ' ' || previous == '\t' || previous == '\r' || previous == '\n' ||
		strings.ContainsRune(";&|()", rune(previous))
}

func isBuildCommand(command string) bool {
	return safeShellControlFlow(command) && buildCommandPattern.MatchString(classifiableShellText(command))
}

func isTestCommand(command string) bool {
	return safeShellControlFlow(command) && testCommandPattern.MatchString(classifiableShellText(command))
}

// safeShellControlFlow admits only structures whose status the verification
// harness can make authoritative: simple commands, && chains, and output
// capture through tee. Alternative/sequence/background clauses can skip a
// classified tool or replace its status, so discovery fails closed on them.
func safeShellControlFlow(command string) bool {
	text := classifiableShellText(command)
	if strings.Contains(text, "||") || strings.Contains(text, ";") {
		return false
	}
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '&':
			if index+1 < len(text) && text[index+1] == '&' {
				index++
				continue
			}
			if index > 0 && (text[index-1] == '>' || text[index-1] == '<') {
				continue
			}
			return false
		case '|':
			if index+1 < len(text) && text[index+1] == '|' {
				return false
			}
			remainder := strings.TrimSpace(text[index+1:])
			if remainder != "tee" && !strings.HasPrefix(remainder, "tee ") {
				return false
			}
		}
	}
	return true
}

// classifiableShellText keeps shell structure and unquoted command words but
// masks quoted arguments. Without this, `echo "x; go build"` looks like a
// second command even though the semicolon and build words are only echo data.
func classifiableShellText(command string) string {
	command = normalizeCommand(command)
	var out strings.Builder
	singleQuoted := false
	doubleQuoted := false
	escaped := false
	for index := 0; index < len(command); index++ {
		character := command[index]
		if escaped {
			if !singleQuoted && !doubleQuoted {
				out.WriteByte(character)
			}
			escaped = false
			continue
		}
		if character == '\\' && !singleQuoted {
			escaped = true
			continue
		}
		if character == '\'' && !doubleQuoted {
			if !singleQuoted {
				out.WriteByte('Q')
			}
			singleQuoted = !singleQuoted
			continue
		}
		if character == '"' && !singleQuoted {
			if !doubleQuoted {
				out.WriteByte('Q')
			}
			doubleQuoted = !doubleQuoted
			continue
		}
		if !singleQuoted && !doubleQuoted {
			out.WriteByte(character)
		}
	}
	return out.String()
}

func isInteractiveTestCommand(command string) bool {
	command = normalizeCommand(command)
	if interactiveRunnerPattern.MatchString(classifiableShellText(command)) {
		return true
	}
	words := strings.Fields(command)
	for index := 0; index < len(words); index++ {
		word := strings.ToLower(strings.Trim(words[index], ";&|"))
		name, value, assigned := strings.Cut(word, "=")
		switch name {
		case "-w":
			if assigned {
				if !numericFlagPattern.MatchString(value) {
					return true
				}
				continue
			}
			if index+1 < len(words) && numericFlagPattern.MatchString(strings.Trim(words[index+1], ";&|")) {
				index++
				continue
			}
			return true
		case "--watch", "--watch-all", "--watchall", "--ui", "--interactive":
			if assigned {
				if !falseFlagValue(value) {
					return true
				}
				continue
			}
			if index+1 < len(words) && falseFlagValue(strings.Trim(words[index+1], ";&|")) {
				index++
				continue
			}
			return true
		}
	}
	return false
}

func falseFlagValue(value string) bool {
	switch strings.ToLower(value) {
	case "false", "0", "no", "off":
		return true
	default:
		return false
	}
}

func packageManager(workspace string) string {
	for _, candidate := range []struct {
		file    string
		manager string
	}{
		{"bun.lock", "bun"}, {"bun.lockb", "bun"},
		{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"},
	} {
		if fileExists(filepath.Join(workspace, candidate.file)) {
			return candidate.manager
		}
	}
	return "npm"
}

func managerRun(manager, script string) string {
	if manager == "npm" || manager == "bun" {
		return manager + " run " + script
	}
	return manager + " " + script
}

func managerTest(manager string) string {
	if manager == "bun" {
		return "bun run test"
	}
	return manager + " test"
}

func readSmallFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1_000_000 {
		return "", false
	}
	body, err := os.ReadFile(path)
	return string(body), err == nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func isPythonProject(workspace string) bool {
	for _, marker := range []string{
		"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt", "pytest.ini", "tox.ini", "noxfile.py",
	} {
		if fileExists(filepath.Join(workspace, marker)) {
			return true
		}
	}
	return hasPythonTests(workspace)
}

func leadingSpaces(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func hasSuffixFile(workspace, suffix string) bool {
	entries, err := os.ReadDir(workspace)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), suffix) {
			return true
		}
	}
	return false
}

func hasPythonTests(workspace string) bool {
	for _, config := range []string{"pytest.ini", "tox.ini", "noxfile.py"} {
		if fileExists(filepath.Join(workspace, config)) {
			return true
		}
	}
	found := false
	_ = filepath.WalkDir(workspace, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != workspace && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(entry.Name())
		if strings.HasPrefix(name, "test_") && strings.HasSuffix(name, ".py") ||
			strings.HasSuffix(name, "_test.py") {
			found = true
		}
		return nil
	})
	return found
}
