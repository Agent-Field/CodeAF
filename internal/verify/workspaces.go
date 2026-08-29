package verify

// WHERE a reading is taken, as opposed to what is run and how it is read.
//
// discovery.go answers "what command checks this project" and runner.go answers
// "which program underneath it names the checks". Both of them assume there is
// ONE project at the root, and a monorepo is the case where that assumption is
// simply false. happy-dom's root `package.json` declares `turbo run test`, and
// turbo's whole job is to fan a task out over the packages the root says it is
// made of — so at a base commit whose workspace is uncompiled, the root command
// dies inside turbo having named no check of any package. The runner that names
// happy-dom's 7,260 checks is `vitest run`, and it is declared in
// `packages/happy-dom/package.json`, which the root reader never opened.
//
// The structural fact is that A WORKSPACE'S PACKAGES ARE DECLARED, and every
// ecosystem declares them in a file this program can read: npm and bun in
// `workspaces`, pnpm in pnpm-workspace.yaml, lerna in lerna.json, turbo by
// being a turbo repo at all, cargo in `[workspace] members`, Go in go.work, and
// Python by putting a pyproject.toml in each package. That is the same kind of
// declaration `declaredRunner` already reads, and it is read the same way: by
// what the repository says about itself, never by a list of project names.
//
// Which package to READ is then answered by the work itself. A job that changed
// files under packages/happy-dom is a job about happy-dom, and the package a
// path belongs to is the nearest directory at or above it that carries a
// manifest — the same nearest-declaration walk a package manager does. Nothing
// here knows what any benchmark contains.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Member is one package of a workspace: where it lives, and what declared it.
//
// Dir is workspace-relative and slash-spelled, so it drops straight into
// Strategy.Workdir; the empty string is the root itself, which is what a
// project that declares no workspace has exactly one of.
type Member struct {
	Dir string `json:"dir,omitempty"`
	// Source is the declaration this member was read out of — the manifest key,
	// the workspace file, the go.work. It is the sentence an autopsy reads to
	// see whether the reader looked in the right place, and it is the same
	// field Strategy.Source is for the same reason.
	Source string `json:"source,omitempty"`
}

// memberScanLimit bounds the search for packages a monorepo declares only by
// scattering manifests, which is the Python and turbo-without-workspaces shape.
//
// Three levels is derived from the deepest layout anybody declares: happy-dom's
// own `packages/@happy-dom/global-registrator` is three, and past it a directory
// holding a manifest is a fixture, a vendored copy or an example rather than a
// member of the workspace. It is a bound on a walk that runs once per reading.
const memberScanLimit = 3

// memberLimit bounds how many members one reading will consider.
//
// A reading is ONE command in ONE place — that is what makes two readings
// subtractable — so the members past the first are only ever the rungs below it
// on the ladder, and a ladder with a rung per package of a forty-package
// monorepo is a ladder nothing walks to the bottom of inside one budget. Four is
// the ladder's own shape: the most specific package, two more, and the root.
const memberLimit = 4

// manifests are the files whose presence says "a package begins here", in every
// ecosystem this program has met. It is the same kind of table `runner.declaredBy`
// is, and it is safe the same way: a file that is not really a package root
// costs a reading taken one directory too deep, which names what it names.
var manifests = []string{
	"package.json", "pyproject.toml", "Cargo.toml", "go.mod",
	"setup.py", "setup.cfg", "pom.xml", "build.gradle", "build.gradle.kts",
	"composer.json", "Gemfile",
}

// Members is every package this workspace declares itself to be made of, in the
// order the declaration gives them, with the root last.
//
// The root is ALWAYS a member and always last. A monorepo's root usually holds a
// runnable command of its own — happy-dom's is the turbo fan-out — and it is the
// honest last rung of a ladder whose earlier rungs are packages: if reading the
// package the work touched names nothing, reading the whole thing is what is
// left to try.
//
// A project that declares no workspace returns the root alone, which is what
// every reader here did before this file existed.
func Members(root string) []Member {
	seen := map[string]bool{}
	var members []Member
	add := func(dir, source string) {
		dir = strings.Trim(filepath.ToSlash(strings.TrimSpace(dir)), "/")
		if dir == "" || dir == "." || strings.Contains(dir, "..") || seen[dir] {
			return
		}
		if !holdsManifest(filepath.Join(root, filepath.FromSlash(dir))) {
			return
		}
		seen[dir] = true
		members = append(members, Member{Dir: dir, Source: source})
	}
	for _, pattern := range declaredMemberPatterns(root) {
		for _, dir := range expandMemberPattern(root, pattern.glob) {
			add(dir, pattern.source)
		}
	}
	if len(members) == 0 {
		for _, found := range scatteredMembers(root) {
			add(found.Dir, found.Source)
		}
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Dir < members[j].Dir })
	return append(members, Member{Dir: "", Source: "the workspace root"})
}

// memberPattern is one declared glob and the declaration it came from.
type memberPattern struct {
	glob   string
	source string
}

// declaredMemberPatterns reads every way a repository states what it is made
// out of. Each reader is the ecosystem's own declaration and nothing else — no
// convention, no guess about a directory called "packages".
func declaredMemberPatterns(root string) []memberPattern {
	var patterns []memberPattern
	// npm, yarn and bun: `workspaces` is either a list of globs or an object
	// whose `packages` is one. Which spelling a project used is a packaging
	// decision that says nothing about what it declares.
	if raw, ok := readSmallFile(filepath.Join(root, "package.json")); ok {
		var flat struct {
			Workspaces []string `json:"workspaces"`
		}
		if json.Unmarshal([]byte(raw), &flat) == nil {
			for _, glob := range flat.Workspaces {
				patterns = append(patterns, memberPattern{glob, "package.json#workspaces"})
			}
		}
		var nested struct {
			Workspaces struct {
				Packages []string `json:"packages"`
			} `json:"workspaces"`
		}
		if json.Unmarshal([]byte(raw), &nested) == nil {
			for _, glob := range nested.Workspaces.Packages {
				patterns = append(patterns, memberPattern{glob, "package.json#workspaces.packages"})
			}
		}
	}
	// pnpm keeps the same list in its own file, because npm's `workspaces` key
	// is not pnpm's.
	if raw, ok := readSmallFile(filepath.Join(root, "pnpm-workspace.yaml")); ok {
		var document struct {
			Packages []string `yaml:"packages"`
		}
		if yaml.Unmarshal([]byte(raw), &document) == nil {
			for _, glob := range document.Packages {
				patterns = append(patterns, memberPattern{glob, "pnpm-workspace.yaml#packages"})
			}
		}
	}
	if raw, ok := readSmallFile(filepath.Join(root, "lerna.json")); ok {
		var document struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal([]byte(raw), &document) == nil {
			for _, glob := range document.Packages {
				patterns = append(patterns, memberPattern{glob, "lerna.json#packages"})
			}
		}
	}
	for _, glob := range cargoWorkspaceMembers(root) {
		patterns = append(patterns, memberPattern{glob, "Cargo.toml#workspace.members"})
	}
	for _, glob := range goWorkUses(root) {
		patterns = append(patterns, memberPattern{glob, "go.work#use"})
	}
	return patterns
}

// expandMemberPattern turns one declared glob into the directories it names.
// A glob with no magic in it is a directory named outright, and Glob answers
// that identically, so there is one path through here rather than two.
func expandMemberPattern(root, glob string) []string {
	glob = strings.Trim(filepath.ToSlash(strings.TrimSpace(glob)), "/")
	if glob == "" || strings.HasPrefix(glob, "!") || strings.Contains(glob, "..") {
		return nil
	}
	// `packages/**` is npm's spelling of "every directory below packages", and
	// filepath.Glob has no `**`. One level is what a workspace declaration ever
	// means by it in practice, and reading it as one level finds the packages
	// rather than none of them.
	glob = strings.ReplaceAll(glob, "**", "*")
	matches, err := filepath.Glob(filepath.Join(root, filepath.FromSlash(glob)))
	if err != nil {
		return nil
	}
	dirs := make([]string, 0, len(matches))
	for _, match := range matches {
		if info, err := os.Stat(match); err != nil || !info.IsDir() {
			continue
		}
		if relative, err := filepath.Rel(root, match); err == nil {
			dirs = append(dirs, filepath.ToSlash(relative))
		}
	}
	return dirs
}

// cargoMembers is `members = [...]` under a `[workspace]` table. Cargo.toml is
// read by shape rather than by a TOML decoder for the reason recipeBody reads a
// Makefile by shape: what is wanted is one assignment table, the file is the
// only place it can come from, and a dependency on a parser for the whole
// grammar buys nothing this reader would use.
var (
	cargoTableHeader   = regexp.MustCompile(`^\[([^\]]+)\]`)
	cargoMembersKey    = regexp.MustCompile(`^members[[:space:]]*=`)
	quotedListElements = regexp.MustCompile(`"([^"]*)"`)
)

func cargoWorkspaceMembers(root string) []string {
	raw, ok := readSmallFile(filepath.Join(root, "Cargo.toml"))
	if !ok {
		return nil
	}
	var globs []string
	table, collecting := "", false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if header := cargoTableHeader.FindStringSubmatch(trimmed); header != nil {
			table, collecting = strings.TrimSpace(header[1]), false
			continue
		}
		if table == "workspace" && cargoMembersKey.MatchString(trimmed) {
			collecting = true
		}
		if !collecting {
			continue
		}
		for _, match := range quotedListElements.FindAllStringSubmatch(trimmed, -1) {
			globs = append(globs, match[1])
		}
		if strings.Contains(trimmed, "]") {
			collecting = false
		}
	}
	return globs
}

// goWorkUses is go.work's `use` directive, in both the one-line and the
// parenthesised spellings the toolchain writes.
func goWorkUses(root string) []string {
	raw, ok := readSmallFile(filepath.Join(root, "go.work"))
	if !ok {
		return nil
	}
	var dirs []string
	block := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "use (":
			block = true
		case block && trimmed == ")":
			block = false
		case block:
			if trimmed != "" && !strings.HasPrefix(trimmed, "//") {
				dirs = append(dirs, trimmed)
			}
		case strings.HasPrefix(trimmed, "use "):
			dirs = append(dirs, strings.TrimSpace(strings.TrimPrefix(trimmed, "use ")))
		}
	}
	return dirs
}

// scatteredMembers finds the packages of a workspace that declares its members
// only by having them: a Python monorepo with a pyproject.toml per package, or a
// turbo repository whose root manifest lists no `workspaces` key.
//
// It is asked ONLY when nothing above declared anything, so a repository that
// says what it is made of is never second-guessed by a walk. The walk is bounded
// at memberScanLimit levels and skips the trees producedSkipDir's siblings skip,
// because a manifest inside node_modules is somebody else's package and there
// are forty thousand of them.
func scatteredMembers(root string) []Member {
	if !monorepoShaped(root) {
		return nil
	}
	var found []Member
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			return nil
		}
		if path == root {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return filepath.SkipDir
		}
		slashed := filepath.ToSlash(relative)
		if skipBuilt(entry.Name()) {
			return filepath.SkipDir
		}
		if strings.Count(slashed, "/")+1 > memberScanLimit {
			return filepath.SkipDir
		}
		if holdsManifest(path) {
			found = append(found, Member{Dir: slashed, Source: "a manifest of its own"})
			// A package inside a package is that package's own business, and
			// reading it as a member of the workspace would make a fixture
			// directory a rung on the ladder.
			return filepath.SkipDir
		}
		return nil
	})
	return found
}

// monorepoShaped says this root is a workspace whose members are elsewhere, on
// the evidence of a build tool that exists to fan a task out over packages.
// Without one, a nested manifest is a fixture or an example rather than a member,
// and reading it as a member would send every reading one directory sideways.
func monorepoShaped(root string) bool {
	for _, marker := range []string{"turbo.json", "nx.json", "rush.json", "pnpm-workspace.yaml", "lerna.json"} {
		if fileExists(filepath.Join(root, marker)) {
			return true
		}
	}
	return false
}

// SkipTree names the directories that are somebody else's files sitting in this
// workspace: the harness's own dot-directories, the tooling's, and the
// dependency installs. A leaf that ran `pip install` produced thousands of files
// and delivered none of them.
//
// IT IS THE ONE LIST. Three walks in this program ask the same question — the
// workspace's own record of what a run left behind (internal/exec), the delivery
// gate settling a named file against the disk (internal/revision), and the two
// walks in this package — and three lists of "what is not a deliverable" is
// three answers to one question, which is exactly how a number in this
// repository drifts. It lives here because this package depends on nothing and
// the other two already depend on it.
func SkipTree(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "site-packages", "__pycache__", "bower_components", "venv":
		return true
	}
	return false
}

// skipBuilt is SkipTree plus the trees a build writes.
//
// It is the stricter rule and only the two walks in THIS package use it, because
// only they are looking for source: a compiled `lib/` or `dist/` holds a copy of
// every test file the package declares, and a reader hunting for the checks next
// to a change would find each of them twice — once where a person wrote it, once
// where a compiler put it. The record of what a run LEFT BEHIND must not use
// this: a built artifact is exactly the kind of thing a person asks for.
func skipBuilt(name string) bool {
	if SkipTree(name) {
		return true
	}
	switch name {
	case "target", "dist", "build", "lib", "coverage", "out":
		return true
	}
	return false
}

// holdsManifest says a directory is where a package begins.
func holdsManifest(dir string) bool {
	for _, name := range manifests {
		if fileExists(filepath.Join(dir, name)) {
			return true
		}
	}
	return false
}

// MemberFor is the package one path belongs to: the nearest directory at or
// above it that carries a manifest, bounded by the workspace root.
//
// It is the nearest-declaration walk a package manager itself does, and it is
// the whole of how the record of what a job touched becomes the place a reading
// is taken. A path outside the workspace, or one with no manifest above it,
// belongs to the root — which is the honest answer and the one every reading
// gave before members existed.
func MemberFor(root, touched string) Member {
	clean := strings.TrimSpace(touched)
	if clean == "" {
		return Member{Source: "the workspace root"}
	}
	if filepath.IsAbs(clean) {
		relative, err := filepath.Rel(root, clean)
		if err != nil || strings.HasPrefix(relative, "..") {
			return Member{Source: "the workspace root"}
		}
		clean = relative
	}
	clean = filepath.ToSlash(filepath.Clean(clean))
	if clean == "." || strings.HasPrefix(clean, "..") {
		return Member{Source: "the workspace root"}
	}
	// The walk starts at the path itself when the path IS a directory and at its
	// parent otherwise, so a record naming a package and a record naming a file
	// inside it answer with the same package. A path a later round deleted is
	// not on disk to be asked, and its parent is the honest starting point.
	dir := pathDir(clean)
	if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(clean))); err == nil && info.IsDir() {
		dir = clean
	}
	for dir != "." && dir != "/" && dir != "" {
		if holdsManifest(filepath.Join(root, filepath.FromSlash(dir))) {
			return Member{Dir: dir, Source: "the nearest manifest above " + clean}
		}
		dir = pathDir(dir)
	}
	return Member{Source: "the workspace root"}
}

// TouchedMembers is the packages a job's own record of what it touched falls
// in, most-touched first, bounded at memberLimit.
//
// Most-touched first is the ladder's order and it is the only ordering that is
// a fact about the work: a job that changed nine files in one package and one in
// another is a job about the first. Ties break on the path so two readings of
// one record are one ladder.
func TouchedMembers(root string, paths []string) []Member {
	counted := map[string]int{}
	sources := map[string]string{}
	for _, path := range paths {
		member := MemberFor(root, path)
		if member.Dir == "" {
			continue
		}
		counted[member.Dir]++
		if _, held := sources[member.Dir]; !held {
			sources[member.Dir] = member.Source
		}
	}
	dirs := make([]string, 0, len(counted))
	for dir := range counted {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if counted[dirs[i]] != counted[dirs[j]] {
			return counted[dirs[i]] > counted[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	if len(dirs) > memberLimit {
		dirs = dirs[:memberLimit]
	}
	members := make([]Member, 0, len(dirs))
	for _, dir := range dirs {
		members = append(members, Member{Dir: dir, Source: sources[dir]})
	}
	return members
}

// pathDir is filepath.Dir for a slash-spelled relative path, which is what
// every path in this file is. It exists so this file never has to decide
// whether it is holding a native path or a recorded one.
func pathDir(slashed string) string {
	index := strings.LastIndexByte(slashed, '/')
	if index < 0 {
		return "."
	}
	return slashed[:index]
}
