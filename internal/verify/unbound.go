package verify

// The fourth reading of the same tree: the names this run's own sources READ
// that nothing anywhere in the tree binds.
//
// The three readings beside this one all answer a question about a name that
// used to exist. The check-level reading says what a suite exercises; the
// presence photograph says which public names went; the changed-definition
// reading says which stayed while their shape moved. None of them can see a
// name that was NEVER there — an attribute a class reads and no code assigns, a
// binding an import asks a module for and the module does not define. That is a
// run referring to something it imagined, and the tree says so with no model,
// no toolchain and no type checker: the reference is in one file and the
// definition is in none.
//
// igel s14 is the measured shape. The run rewrote `igel/configs.py` so that
// `temp_post_req_data_path` became a local inside `_get_configs`, and
// `igel/servers/fastapi_server.py` still opens with
// `from igel.configs import temp_post_req_data_path`. All twenty-four hidden
// tests failed on `ImportError: cannot import name 'temp_post_req_data_path'`,
// and the only thing this harness could say about it was that the run's own
// checks were red — never WHICH name was unbound, which is the one word a
// repair round needs.
//
// IT IS DELIBERATELY NARROW SO THAT IT NEVER LIES. Python is a language where a
// name can be bound by a decorator, a metaclass, a `setattr` in a base class
// four files away, or a module's own `__getattr__`; TypeScript is a language
// with declaration merging and index signatures. Every one of those is a way for
// a reference this reader cannot see the binding for to be perfectly correct, so
// each of them SILENCES the scope it appears in rather than being reasoned
// about. What is left is the residue: a name read in one place and bound in no
// place, under a tree-wide index that counts a binding wherever in the
// repository it is spelled.
//
// Go and Rust are absent on purpose. `go build` and `cargo build` are the
// reading for those two, they are already run by the check-level half, and a
// second reader that guessed at the same question in fewer lines could only
// disagree with a compiler.
//
// FAILSAFE.md clause 1 for the reading — it is shape and never vocabulary — and
// clause 2 for where it comes from: the files on disk, and nothing the component
// being checked says about itself.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	// unboundScanLimit bounds the walk that builds the tree-wide binding index.
	// It is surfaceFileLimit's sibling and carries the same figure for the same
	// reason: two thousand files is a large single-language repository, and past
	// it the index is PARTIAL — which is the one state this reading may not
	// report from, because a binding the walk never reached is a binding that
	// reads as absent. A partial index silences every check that depends on it.
	unboundScanLimit = 2000
	// unboundReadBudget bounds the bytes that walk reads. It is
	// surfaceReadBudget's figure, spent once per settlement rather than once per
	// job, because the index answers a question about the whole tree and there
	// is no smaller tree to ask.
	unboundReadBudget = 8 << 20
	// unboundModuleBudget bounds what resolving imports reads on top of it. An
	// import names one module, a changed file holds a handful of imports, and a
	// module already read is not read twice — so this is small on purpose, and
	// past it an import resolves to nothing and says nothing.
	unboundModuleBudget = 1 << 20
	// unboundSitesKept bounds how many unbound references one settlement
	// carries. It is consumerNamesWeighed's sibling one order up: a run that
	// reads twenty-four names nothing defines has one shape of problem and says
	// it in the first few.
	unboundSitesKept = 24
	// unboundNamesReported bounds what one finding spells out loud. It is
	// surfaceNamesReported's sibling and the same size for the same reason.
	unboundNamesReported = 8
)

// UnboundName is one name a source this run changed reads, that the tree binds
// nowhere.
//
// Ground is the evidence of ABSENCE in the words a reader can check — "assigned
// nowhere in class RichLog, tree-wide" — because a finding about something that
// is not there is only as good as its account of where it looked.
type UnboundName struct {
	// File and Line are where the reference is, in the record's own spelling.
	File string
	Line int
	// Name is the reference as a person writes it: `RichLog._size_known` for an
	// attribute, the bare binding for an import.
	Name string
	// Scope is the class or module the name should have lived in.
	Scope string
	// Ground is where this reader looked and did not find it.
	Ground string
	// Text is the reference's own line, trimmed, so a reader who disagrees with
	// the finding can see exactly what it was read off.
	Text string
}

// Where is the site as everything downstream spells it.
func (u UnboundName) Where() string { return u.File + ":" + strconv.Itoa(u.Line) }

// Words is one unbound reference as a person reads it.
func (u UnboundName) Words() string {
	line := u.Name + " (" + u.Where() + ""
	if u.Ground != "" {
		line += ", " + u.Ground
	}
	return line + ")"
}

// UnboundWords is one reading as the lines a finding, a journal and a brief all
// spell it: the name, where it is read, and where this reader looked for a
// binding.
//
// ONE SPELLING, because the leaf takes this reading on its own seam and the gate
// re-takes it on the tree it judges, and two wordings of one measurement read
// downstream as two measurements.
func UnboundWords(found []UnboundName) []string {
	words := make([]string, 0, len(found))
	for _, one := range found {
		words = append(words, one.Words())
	}
	return words
}

// UnboundNamed is what a finding says out loud: the first few references, and
// the rest left to the journal.
func UnboundNamed(found []UnboundName) []UnboundName {
	if len(found) > unboundNamesReported {
		return found[:unboundNamesReported]
	}
	return found
}

// UnboundReferences is every name the run's CHANGED SOURCES read that the tree
// defines nowhere.
//
// The record is the run's own account of what it left behind, settled against
// the filesystem, so this is a reading of the WORK and never of the repository:
// a dangling reference in a file nobody touched was dangling before the run
// started and is somebody else's finding.
//
// Every silence favours the work. No record, no changed source in a language
// with a reader, an index the budget could not finish, a file that cannot be
// read, a scope holding any construct that binds names dynamically — each of
// those adds nothing at all.
func UnboundReferences(root string, record []string) []UnboundName {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	files := readableSources(ChangedSources(root, record))
	if len(files) == 0 {
		return nil
	}
	// The index is walked only for the languages the changed sources are
	// actually in, because a python-only run has no use for a typescript member
	// index and the walk is what this costs.
	var python, script bool
	for _, file := range files {
		switch surfaceLanguage(lastSegment(file)) {
		case "python":
			python = true
		case "script":
			script = true
		}
	}
	if !python && !script {
		return nil
	}
	index := newBindings(root, python, script)
	var found []UnboundName
	for _, file := range files {
		body, read := readSurfaceFile(filepath.Join(root, filepath.FromSlash(file)))
		if !read {
			continue
		}
		switch surfaceLanguage(lastSegment(file)) {
		case "python":
			found = append(found, pythonUnbound(file, body, index)...)
		case "script":
			found = append(found, scriptUnbound(file, body, index)...)
		}
	}
	// Ordered by file and then by line, so one tree answers the same way twice
	// and a reader meets the references in the order the files spell them.
	sort.SliceStable(found, func(i, j int) bool {
		if found[i].File != found[j].File {
			return found[i].File < found[j].File
		}
		if found[i].Line != found[j].Line {
			return found[i].Line < found[j].Line
		}
		return found[i].Name < found[j].Name
	})
	if len(found) > unboundSitesKept {
		found = found[:unboundSitesKept]
	}
	return found
}

// readableSources drops every path that lives under a directory the binding
// index does not walk.
//
// A CHECK MAY ONLY BE ASKED WHERE THE INDEX LOOKED. `skipBuilt` keeps the walk
// out of `node_modules`, `dist`, `target` and their siblings, so a reference in
// one of those has its declarations in files the index never read — and every
// one of them reads as a name nothing binds. Run over happy-dom's vendored tree
// with this missing, the typescript half reported twenty-four members of
// `EntityDecoder`, `ChangeBuffer` and `HTMLReporter`, every one of them declared
// eighty lines below the line that used it (2026-08-29).
func readableSources(files Focus) Focus {
	kept := make(Focus, 0, len(files))
	for _, file := range files {
		vendored := false
		for _, segment := range strings.Split(file, "/") {
			if skipBuilt(segment) {
				vendored = true
				break
			}
		}
		if !vendored {
			kept = append(kept, file)
		}
	}
	return kept
}

// ── the tree-wide binding index ──────────────────────────────────────────────

// bindings is every name this tree binds, read once for the whole settlement.
//
// IT IS TREE-WIDE AND NOT PER-CLASS, and that is the whole of what makes it
// safe. An attribute a base class in another file sets, a mixin, a name a
// sibling module assigns onto an instance it was handed — every one of those is
// a real binding this reader cannot follow, and counting a binding WHEREVER the
// repository spells it turns all of them into silence instead of into a wrong
// finding. What survives is a name the repository does not spell as a binding
// anywhere at all, which is a much smaller claim and a true one.
type bindings struct {
	// attributes is every name python binds as an attribute, a method, a class
	// or a class-body field, anywhere in the tree.
	attributes map[string]bool
	// members is the same for typescript and javascript: class fields, methods,
	// accessors, interface members and every `x.name =` target.
	members map[string]bool
	// classes is every class name the tree declares, which is what decides
	// whether a class's BASES are readable — see pythonUnboundAttributes.
	classes map[string]bool
	// partial says the walk stopped at a bound, so the index is incomplete and
	// every check that reads it must stay silent.
	partial bool
	// root is where import paths are resolved from, and modules memoises what
	// each already-read module binds at its top level.
	root    string
	modules map[string]*moduleBindings
	// budget is what module resolution has left to spend reading files the walk
	// above did not keep the bodies of.
	budget int
	// sourceRoots is where a dotted import's first segment is looked for: the
	// tree root, plus every directory one level down that holds a package.
	sourceRoots []string
}

// moduleBindings is one python module's top-level surface as an importer sees
// it: what it binds, and whether anything in it makes that question
// unanswerable.
type moduleBindings struct {
	names map[string]bool
	// opaque says this module binds names in a way this reader cannot enumerate
	// — a star import, a module `__getattr__`, a write into `globals()` — so
	// nothing may be reported missing from it.
	opaque bool
	// known says the module was found and read at all.
	known bool
}

// newBindings walks the tree once and records every name it binds.
func newBindings(root string, python, script bool) *bindings {
	index := &bindings{
		attributes: map[string]bool{}, members: map[string]bool{},
		classes: map[string]bool{}, root: root, modules: map[string]*moduleBindings{}, budget: unboundModuleBudget,
	}
	files, budget := 0, unboundReadBudget
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && skipBuilt(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		language := surfaceLanguage(entry.Name())
		if language != "python" && language != "script" {
			return nil
		}
		if files++; files > unboundScanLimit || budget <= 0 {
			index.partial = true
			return filepath.SkipAll
		}
		body, read := readSurfaceFile(path)
		budget -= len(body)
		if !read {
			// A file too large to read is a file whose bindings are unknown,
			// and unknown bindings are exactly what a partial index means.
			index.partial = true
			return nil
		}
		switch {
		case language == "python" && python:
			pythonBindings(body, index.attributes, index.classes)
		case language == "script" && script:
			scriptBindings(body, index.members)
		}
		return nil
	})
	index.sourceRoots = sourceRootsUnder(root)
	return index
}

// sourceRootsUnder is where a dotted import's first segment may live: the tree
// root itself, and every directory one level down. `src/textual/...` is reached
// by `import textual` and `igel/configs.py` by `import igel`, and which of the
// two a repository uses is a layout convention rather than a fact this reader
// gets to assume.
func sourceRootsUnder(root string) []string {
	roots := []string{""}
	entries, err := os.ReadDir(root)
	if err != nil {
		return roots
	}
	for _, entry := range entries {
		if entry.IsDir() && !skipBuilt(entry.Name()) {
			roots = append(roots, entry.Name())
		}
	}
	return roots
}

// ── python ───────────────────────────────────────────────────────────────────

var (
	// unboundPythonSelf finds a `self.name` reference. The leading class keeps
	// `myself.name` and `not_self.name` out of it; Go has no lookbehind, so the
	// character before is captured and thrown away.
	unboundPythonSelf = regexp.MustCompile(`(^|[^A-Za-z0-9_.])self\.([A-Za-z_][A-Za-z0-9_]*)`)
	// unboundPythonAttrTarget finds every attribute an assignment writes into,
	// which is how `self.a, self.b = x` and `obj.name = 1` both read as
	// bindings without this reader parsing a target list.
	unboundPythonAttrTarget = regexp.MustCompile(`\.([A-Za-z_][A-Za-z0-9_]*)`)
	// unboundPythonSelfAnnotation is an annotated instance attribute with no
	// value beside it, which is a declaration and not a read.
	unboundPythonSelfAnnotation = regexp.MustCompile(`^\s*self\.([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\S`)
	// unboundPythonAnnotation is a bare annotated declaration — `count: int` —
	// which is how a dataclass, an attrs class and a plain annotated field all
	// spell a binding with no value beside it.
	unboundPythonAnnotation = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\S`)
	// unboundPythonQuoted is one quoted identifier, which is how `__slots__`
	// spells the names it binds.
	unboundPythonQuoted = regexp.MustCompile(`["']([A-Za-z_][A-Za-z0-9_]*)["']`)
	// unboundPythonFrom is a `from module import ...` statement's two halves.
	unboundPythonFrom = regexp.MustCompile(`^\s*from\s+(\.*)([A-Za-z_][A-Za-z0-9_.]*)?\s+import\s+(.*)$`)
	// unboundPythonImport is a plain `import a.b.c` or `import a as b`.
	unboundPythonImport = regexp.MustCompile(`^\s*import\s+(.*)$`)
	// unboundPythonBare is an identifier used as a call or as the base of an
	// attribute reach — the only two shapes check three reads.
	unboundPythonBare = regexp.MustCompile(`(^|[^A-Za-z0-9_.'"])([A-Za-z_][A-Za-z0-9_]*)\s*[.(]`)
)

// pythonDynamic is the set of constructs that bind names this reader cannot
// enumerate. Any of them in scope SILENCES the scope, which is the fail-safe
// direction: a class that hands its own attributes to `setattr` is a class where
// every reference is potentially bound and none of them is checkable.
var pythonDynamic = []string{
	"setattr(", "getattr(", "hasattr(", "delattr(", "__getattr__", "__setattr__",
	"__getattribute__", "globals()", "locals()", "vars(", "exec(", "eval(",
	"__dict__", "import *",
}

// pythonBindings records every name a python file binds, with no privacy rule
// applied: a leading underscore says a name is INTERNAL, never that it is
// absent, and this index answers absence.
//
// It reuses the surface reader's own grammar — pythonDef, pythonClass,
// pythonAssign, pythonSelfAssign are the same four regexes that decide what a
// module publishes — because two readers with two grammars would eventually
// disagree about whether a line is a definition, and this one is only ever
// consulted to say that the other one's answer was not the whole of it.
func pythonBindings(body string, into, classes map[string]bool) {
	lines := pythonSource(body)
	// `__slots__` names its attributes as STRING LITERALS, which is the one
	// thing pythonSource blanks out — so the quoted names are read off the file
	// as it is actually written, beside the blanked line the rest of this reads.
	raw := strings.Split(body, "\n")
	slots := 0
	for at, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if match := pythonDef.FindStringSubmatch(line); match != nil {
			into[match[2]] = true
			continue
		}
		if match := pythonClass.FindStringSubmatch(line); match != nil {
			into[match[2]] = true
			classes[match[2]] = true
			continue
		}
		// An annotated attribute with no value on its own line — `self.held:
		// WeakKeyDictionary[` opening a type that runs over four more — is a
		// DECLARATION, and reading it as a use of something undeclared is the
		// one shape that got past the first sweep of this reader.
		if match := unboundPythonSelfAnnotation.FindStringSubmatch(line); match != nil {
			into[match[1]] = true
			continue
		}
		// `__slots__` names its class's attributes as string literals, and it
		// may run over several lines; the brackets say where it ends.
		if strings.Contains(line, "__slots__") || slots > 0 {
			slots = openBrackets(line, slots)
			for _, quoted := range unboundPythonQuoted.FindAllStringSubmatch(lineText(raw, at), -1) {
				into[quoted[1]] = true
			}
			continue
		}
		if left, assigns := pythonAssignedTo(line); assigns {
			if match := pythonAssign.FindStringSubmatch(line); match != nil {
				into[match[2]] = true
			}
			for _, attribute := range unboundPythonAttrTarget.FindAllStringSubmatch(left, -1) {
				into[attribute[1]] = true
			}
			continue
		}
		// A bare annotation is a declaration with no value — the shape a
		// dataclass field, an attrs field and a class-level type hint all take.
		if match := unboundPythonAnnotation.FindStringSubmatch(line); match != nil &&
			!strings.HasSuffix(strings.TrimSpace(line), ":") {
			into[match[1]] = true
		}
	}
}

// pythonAssignedTo splits a line at its assignment, returning the target text
// and whether there was one.
//
// It is a scan for the first `=` that is not part of a comparison and not
// inside brackets, because a keyword argument, a default and a dictionary all
// spell `=` inside brackets and none of them binds a name in this scope.
func pythonAssignedTo(line string) (string, bool) {
	depth := 0
	for index := 0; index < len(line); index++ {
		switch line[index] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case '=':
			if depth != 0 {
				continue
			}
			if index+1 < len(line) && line[index+1] == '=' {
				index++
				continue
			}
			if index > 0 && strings.ContainsRune("=!<>", rune(line[index-1])) {
				continue
			}
			// An augmented assignment (`x += 1`) reads and writes at once, and
			// this counts it as a binding: over-binding is the direction that
			// costs a silence, and under-binding is the direction that lies.
			left := line[:index]
			if index > 0 && strings.ContainsAny(string(line[index-1]), "+-*/%&|^@") {
				left = line[:index-1]
			}
			return left, true
		}
	}
	return "", false
}

// pythonSource is the file's lines with every string literal and comment blanked
// out, so a docstring that mentions `self.foo` is not read as code that touches
// it. The lines keep their numbers, because a finding names one.
func pythonSource(body string) []string {
	lines := strings.Split(body, "\n")
	fence := ""
	for index, line := range lines {
		lines[index], fence = pythonCodeOf(line, fence)
	}
	return lines
}

// pythonCodeOf blanks one line's literals and comment, carrying the triple-quote
// state across lines.
func pythonCodeOf(line, fence string) (string, string) {
	var out strings.Builder
	quote := ""
	for index := 0; index < len(line); index++ {
		rest := line[index:]
		switch {
		case fence != "":
			if strings.HasPrefix(rest, fence) {
				out.WriteString(strings.Repeat(" ", len(fence)))
				index += len(fence) - 1
				fence = ""
				continue
			}
			out.WriteByte(' ')
		case quote != "":
			if line[index] == '\\' {
				out.WriteString("  ")
				index++
				continue
			}
			if strings.HasPrefix(rest, quote) {
				quote = ""
			}
			out.WriteByte(' ')
		case strings.HasPrefix(rest, `"""`), strings.HasPrefix(rest, "'''"):
			fence = rest[:3]
			out.WriteString("   ")
			index += 2
		case line[index] == '"' || line[index] == '\'':
			quote = string(line[index])
			out.WriteByte(' ')
		case line[index] == '#':
			out.WriteString(strings.Repeat(" ", len(line)-index))
			return out.String(), fence
		default:
			out.WriteByte(line[index])
		}
	}
	return out.String(), fence
}

// pythonUnbound is one changed python file's unbound references: attributes its
// classes read and nothing binds, and imported names the module they come from
// does not define.
func pythonUnbound(file, body string, index *bindings) []UnboundName {
	lines := pythonSource(body)
	raw := strings.Split(body, "\n")
	found := pythonUnboundImports(file, lines, raw, index)
	if index.partial {
		// A binding index that did not finish cannot answer absence, and an
		// import is answered by one module rather than by the index — so the
		// import half above stands and the two halves below do not.
		return found
	}
	found = append(found, pythonUnboundAttributes(file, lines, raw, index)...)
	return append(found, pythonUnboundBare(file, lines, raw, index)...)
}

// pythonUnboundAttributes is the `self.name` half: an attribute a class reads
// that the tree binds nowhere.
func pythonUnboundAttributes(file string, lines, raw []string, index *bindings) []UnboundName {
	var found []UnboundName
	for _, class := range pythonClassBodies(lines) {
		if pythonScopeIsDynamic(lines[class.from:class.to+1]) || index.inherits(class) {
			continue
		}
		seen := map[string]bool{}
		for number := class.from; number <= class.to && number < len(lines); number++ {
			line := lines[number]
			read := line
			if left, assigns := pythonAssignedTo(line); assigns {
				read = strings.Repeat(" ", len(left)) + line[len(left):]
			}
			for _, match := range unboundPythonSelf.FindAllStringSubmatch(read, -1) {
				name := match[2]
				if seen[name] || index.attributes[name] || dunder(name) {
					continue
				}
				seen[name] = true
				found = append(found, UnboundName{
					File: file, Line: number + 1, Name: class.name + "." + name,
					Scope:  "class " + class.name,
					Ground: "assigned nowhere in class " + class.name + ", tree-wide",
					Text:   lineText(raw, number)})
			}
		}
	}
	return found
}

// pythonScope is one top-level class, the lines its body covers, and the names
// it inherits from.
type pythonScope struct {
	name  string
	from  int
	to    int
	bases []string
}

// pythonClassBodies is every class the file declares at its top level, with the
// span of its body.
//
// Top-level only, which is the same restraint the surface reader keeps: a class
// defined inside a function or inside another class is a scope whose bindings
// this reader would have to reason about rather than read.
func pythonClassBodies(lines []string) []pythonScope {
	var scopes []pythonScope
	for index, line := range lines {
		match := pythonClass.FindStringSubmatch(line)
		if match == nil || len(match[1]) != 0 {
			continue
		}
		scopes = append(scopes, pythonScope{name: match[2], from: index,
			to: spanEnd(lines, index, 0), bases: pythonBases(lines, index)})
	}
	return scopes
}

// pythonScopeIsDynamic says this scope binds names in a way nothing here can
// enumerate, so nothing in it may be reported.
func pythonScopeIsDynamic(lines []string) bool {
	for _, line := range lines {
		for _, construct := range pythonDynamic {
			if strings.Contains(line, construct) {
				return true
			}
		}
	}
	return false
}

// dunder says this is one of the names the language itself owns on every object.
// `self.__class__` is bound by python and by nothing in any repository.
func dunder(name string) bool {
	return strings.HasPrefix(name, "__") && strings.HasSuffix(name, "__")
}

// lineText is one line of the file as it is actually written, which is the
// evidence a finding is read off.
func lineText(raw []string, index int) string {
	if index < 0 || index >= len(raw) {
		return ""
	}
	return strings.TrimSpace(raw[index])
}

// pythonUnboundImports is the `from module import name` half: a binding an
// import asks an IN-TREE module for that the module does not define.
//
// A module outside the tree is silent, always: this reader has no site-packages
// and no standard library, and the whole of its claim is that it read the file
// the name was supposed to be in.
func pythonUnboundImports(file string, lines, raw []string, index *bindings) []UnboundName {
	var found []UnboundName
	for number, line := range lines {
		match := unboundPythonFrom.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		imported := pythonImportedNames(lines, number)
		if len(imported) == 0 {
			continue
		}
		module := index.pythonModule(file, match[1], match[2])
		if module == nil || !module.known || module.opaque {
			continue
		}
		where := index.pythonModulePath(file, match[1], match[2])
		for _, name := range imported {
			if module.names[name] || index.pythonSubmodule(where, name) {
				continue
			}
			found = append(found, UnboundName{
				File: file, Line: number + 1, Name: name, Scope: where,
				Ground: where + " binds no top-level " + name,
				Text:   lineText(raw, number)})
		}
	}
	return found
}

// pythonImportedNames is what one `from ... import ...` statement asks for,
// following a parenthesised list over as many lines as it runs.
//
// A star import asks for everything and names nothing, so it contributes no
// reference — which is the same silence a module with a star import gets when it
// is on the other end.
func pythonImportedNames(lines []string, at int) []string {
	match := unboundPythonFrom.FindStringSubmatch(lines[at])
	if match == nil {
		return nil
	}
	text := match[3]
	if depth := openBrackets(match[3], 0); depth > 0 {
		for number := at + 1; number < len(lines) && depth > 0; number++ {
			text += " " + lines[number]
			depth = openBrackets(lines[number], depth)
		}
	}
	text = strings.NewReplacer("(", " ", ")", " ", "\\", " ").Replace(text)
	var names []string
	for _, part := range strings.Split(text, ",") {
		fields := strings.Fields(part)
		if len(fields) == 0 || fields[0] == "*" {
			continue
		}
		// `x as y` asks the module for x; y is what this file calls it.
		if name := fields[0]; unboundPythonAnnotation.MatchString(name + ": x") {
			names = append(names, name)
		}
	}
	return names
}

// pythonModule reads what one imported module binds at its top level, memoised
// for the settlement.
func (b *bindings) pythonModule(from, dots, module string) *moduleBindings {
	path := b.pythonModulePath(from, dots, module)
	if path == "" {
		return nil
	}
	if held, read := b.modules[path]; read {
		return held
	}
	held := &moduleBindings{names: map[string]bool{}}
	b.modules[path] = held
	if b.budget <= 0 {
		return held
	}
	body, read := readSurfaceFile(filepath.Join(b.root, filepath.FromSlash(path)))
	b.budget -= len(body)
	if !read {
		return held
	}
	held.known = true
	pythonModuleNames(body, held)
	return held
}

// pythonModulePath resolves an import to the ONE file in the tree it names, or
// to nothing.
//
// Two candidates that both exist resolve to nothing rather than to a guess: an
// import this reader cannot place is an import it says nothing about.
func (b *bindings) pythonModulePath(from, dots, module string) string {
	segments := strings.Split(strings.Trim(module, "."), ".")
	if module == "" {
		segments = nil
	}
	var bases []string
	if dots != "" {
		// A relative import counts upward from the importing file's own
		// package: one dot is the package it is in, two is the one above.
		here := filepath.ToSlash(filepath.Dir(from))
		for level := 1; level < len(dots); level++ {
			here = filepath.ToSlash(filepath.Dir(here))
		}
		if here == "." {
			here = ""
		}
		bases = []string{here}
	} else {
		bases = b.sourceRoots
	}
	var resolved []string
	for _, base := range bases {
		stem := strings.Join(append(strings.Split(strings.Trim(base, "/"), "/"), segments...), "/")
		stem = strings.TrimPrefix(strings.TrimPrefix(stem, "/"), "./")
		for _, candidate := range []string{stem + ".py", stem + "/__init__.py"} {
			if candidate == ".py" || candidate == "/__init__.py" {
				continue
			}
			if info, err := os.Stat(filepath.Join(b.root, filepath.FromSlash(candidate))); err == nil &&
				!info.IsDir() {
				resolved = append(resolved, candidate)
			}
		}
	}
	if len(sortedUnique(resolved)) != 1 {
		return ""
	}
	return resolved[0]
}

// pythonSubmodule says the name an import asks a PACKAGE for is one of its own
// modules, which python binds on import and no line of the package's
// `__init__.py` has to spell.
func (b *bindings) pythonSubmodule(pkg, name string) bool {
	if !strings.HasSuffix(pkg, "/__init__.py") {
		return false
	}
	dir := strings.TrimSuffix(pkg, "/__init__.py")
	for _, candidate := range []string{dir + "/" + name + ".py", dir + "/" + name + "/__init__.py"} {
		if _, err := os.Stat(filepath.Join(b.root, filepath.FromSlash(candidate))); err == nil {
			return true
		}
	}
	return false
}

// pythonModuleNames is what a module binds at its TOP LEVEL, as an importer
// reaches it.
//
// Module level here means "not inside a def or a class", and not "at indent
// zero", because the `try: from x import y / except ImportError: y = None`
// shape binds a real name four spaces in. Reading indent alone would call that
// name missing, which is the one direction this reader may not be wrong in.
func pythonModuleNames(body string, into *moduleBindings) {
	lines := pythonSource(body)
	// `__all__` names its exports as string literals, which pythonSource blanks;
	// they are read off the file as it is written, for the reason __slots__ is.
	raw := strings.Split(body, "\n")
	nested := -1
	slots := 0
	all := 0
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		indent := indentOf(line)
		if nested >= 0 && indent <= nested {
			nested = -1
		}
		for _, construct := range []string{"import *", "__getattr__", "globals()", "exec(", "eval("} {
			if strings.Contains(line, construct) {
				into.opaque = true
			}
		}
		// `__all__` is a module's own statement of what it hands out, and a name
		// in it is re-exported from wherever the module got it.
		if all > 0 || strings.Contains(line, "__all__") {
			if all == 0 {
				all = openBrackets(line, 0)
			} else {
				all = openBrackets(line, all)
			}
			for _, quoted := range unboundPythonQuoted.FindAllStringSubmatch(lineText(raw, index), -1) {
				into.names[quoted[1]] = true
			}
			if strings.Contains(line, "__all__") {
				continue
			}
		}
		if nested >= 0 {
			continue
		}
		if match := pythonDef.FindStringSubmatch(line); match != nil {
			into.names[match[2]] = true
			nested = indent
			continue
		}
		if match := pythonClass.FindStringSubmatch(line); match != nil {
			into.names[match[2]] = true
			nested = indent
			continue
		}
		if strings.Contains(line, "__slots__") || slots > 0 {
			slots = openBrackets(line, slots)
			continue
		}
		if match := unboundPythonFrom.FindStringSubmatch(line); match != nil {
			for _, name := range pythonImportedNames(lines, index) {
				into.names[name] = true
			}
			// And the local spelling, where the statement renamed what it took.
			for _, name := range importedAs(match[3]) {
				into.names[name] = true
			}
			continue
		}
		if match := unboundPythonImport.FindStringSubmatch(line); match != nil &&
			!strings.HasPrefix(trimmed, "import *") {
			for _, part := range strings.Split(match[1], ",") {
				fields := strings.Fields(part)
				switch {
				case len(fields) >= 3 && fields[1] == "as":
					into.names[fields[2]] = true
				case len(fields) >= 1:
					into.names[strings.Split(fields[0], ".")[0]] = true
				}
			}
			continue
		}
		if left, assigns := pythonAssignedTo(line); assigns {
			for _, part := range strings.Split(left, ",") {
				if name := strings.TrimSpace(strings.Split(part, ":")[0]); name != "" &&
					!strings.ContainsAny(name, ".[]()") {
					into.names[name] = true
				}
			}
			continue
		}
		if match := unboundPythonAnnotation.FindStringSubmatch(line); match != nil &&
			!strings.HasSuffix(trimmed, ":") {
			into.names[match[1]] = true
		}
	}
}

// importedAs is the local names an import list binds where it renamed what it
// took: `from x import a as b` binds b here and asks x for a.
func importedAs(text string) []string {
	var names []string
	for _, part := range strings.Split(strings.NewReplacer("(", " ", ")", " ").Replace(text), ",") {
		if fields := strings.Fields(part); len(fields) >= 3 && fields[1] == "as" {
			names = append(names, fields[2])
		}
	}
	return names
}

// ── typescript and javascript ────────────────────────────────────────────────

var (
	// unboundScriptThis finds a `this.name` reference, with the same leading
	// class the python one has and for the same reason.
	unboundScriptThis = regexp.MustCompile(`(^|[^A-Za-z0-9_$.])this\.([A-Za-z_$][A-Za-z0-9_$]*)`)
	// unboundScriptMember is a class or interface member declaration in any of
	// the shapes typescript spells one: a field, a method, an accessor, an
	// optional, a readonly.
	unboundScriptMember = regexp.MustCompile(
		`^\s*(?:public\s+|private\s+|protected\s+|readonly\s+|static\s+|abstract\s+|` +
			`declare\s+|override\s+|async\s+|get\s+|set\s+)*` +
			`([A-Za-z_$][A-Za-z0-9_$]*)\s*[?!]?\s*[(:=;<]`)
	// unboundScriptImport is a named-import statement and the module it comes
	// from. Only the braced form is read: a default import binds whatever the
	// module's default is, whatever it is called here.
	unboundScriptImport = regexp.MustCompile(
		`^\s*import\s+(?:type\s+)?\{([^}]*)\}\s*from\s*['"]([^'"]+)['"]`)
	// unboundScriptClass opens a class body, exported or not — this reader is
	// about references and not about what a module publishes.
	unboundScriptClass = regexp.MustCompile(
		`^(\s*)(?:export\s+)?(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?class\s+` +
			`([A-Za-z_$][A-Za-z0-9_$]*)`)
)

// scriptOwned is what every javascript object carries whatever a class declares:
// the language's own members, reachable through the prototype chain and spelled
// by no repository. It is dunder's counterpart, and it is a list for the same
// reason pythonBuiltins is one — there is no tree to read it off.
var scriptOwned = map[string]bool{
	"constructor": true, "prototype": true, "__proto__": true, "toString": true,
	"toLocaleString": true, "valueOf": true, "hasOwnProperty": true,
	"isPrototypeOf": true, "propertyIsEnumerable": true,
}

// scriptDynamic is typescript's own set of ways to bind a member this reader
// cannot see: an index signature, a spread onto the instance, a cast that turns
// the type off, a computed member.
var scriptDynamic = []string{
	"Object.assign(this", "Object.defineProperty", "this as any", "this as unknown",
	"[key:", "[key :", "[index:", "this[", "declare module",
}

// scriptBindings records every member name a typescript or javascript file
// spells, with no export rule applied.
//
// It reuses scriptMember's own grammar for the class-body shapes and adds the
// two the surface reader has no use for: a member declared on an interface, and
// every `x.name =` target anywhere in the file.
func scriptBindings(body string, into map[string]bool) {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		if match := unboundScriptMember.FindStringSubmatch(line); match != nil &&
			!scriptKeyword(match[1]) {
			into[match[1]] = true
		}
		if left, assigns := pythonAssignedTo(line); assigns {
			for _, attribute := range unboundPythonAttrTarget.FindAllStringSubmatch(left, -1) {
				into[attribute[1]] = true
			}
		}
	}
}

// scriptUnbound is one changed typescript or javascript file's unbound
// references: members its classes read that nothing declares, and named imports
// a relative module does not export.
func scriptUnbound(file, body string, index *bindings) []UnboundName {
	lines := strings.Split(body, "\n")
	found := scriptUnboundImports(file, lines, index)
	if index.partial {
		return found
	}
	return append(found, scriptUnboundMembers(file, lines, index)...)
}

// scriptUnboundMembers is the `this.name` half.
func scriptUnboundMembers(file string, lines []string, index *bindings) []UnboundName {
	var found []UnboundName
	for at, line := range lines {
		match := unboundScriptClass.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		to := spanEnd(lines, at, indentOf(line))
		body := lines[at : to+1]
		if scriptScopeIsDynamic(body) {
			continue
		}
		seen := map[string]bool{}
		for number := at; number <= to && number < len(lines); number++ {
			text := lines[number]
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
				continue
			}
			read := text
			if left, assigns := pythonAssignedTo(text); assigns {
				read = strings.Repeat(" ", len(left)) + text[len(left):]
			}
			for _, reference := range unboundScriptThis.FindAllStringSubmatch(read, -1) {
				name := reference[2]
				if seen[name] || index.members[name] || scriptOwned[name] {
					continue
				}
				seen[name] = true
				found = append(found, UnboundName{
					File: file, Line: number + 1, Name: match[2] + "." + name,
					Scope:  "class " + match[2],
					Ground: "declared nowhere in class " + match[2] + ", tree-wide",
					Text:   strings.TrimSpace(text)})
			}
		}
	}
	return found
}

// scriptScopeIsDynamic says this class binds members in a way nothing here can
// enumerate, so nothing in it may be reported. A class that EXTENDS anything is
// one of those: its base may be a mixin, a generic, or a class expression, and
// the tree-wide index is what covers a base this reader can actually see.
func scriptScopeIsDynamic(lines []string) bool {
	if len(lines) > 0 && strings.Contains(lines[0], " extends ") {
		return true
	}
	for _, line := range lines {
		for _, construct := range scriptDynamic {
			if strings.Contains(line, construct) {
				return true
			}
		}
	}
	return false
}

// scriptUnboundImports is the named-import half: a binding taken from a RELATIVE
// module in the tree that the module does not export.
//
// Relative only. A bare specifier is a package, this reader has no
// `node_modules` and no resolver, and an import it cannot place is one it says
// nothing about.
func scriptUnboundImports(file string, lines []string, index *bindings) []UnboundName {
	var found []UnboundName
	for number, line := range lines {
		match := unboundScriptImport.FindStringSubmatch(line)
		if match == nil || !strings.HasPrefix(match[2], ".") {
			continue
		}
		path := index.scriptModulePath(file, match[2])
		if path == "" {
			continue
		}
		exported, readable := index.scriptExports(path)
		if !readable {
			continue
		}
		for _, part := range strings.Split(match[1], ",") {
			fields := strings.Fields(strings.TrimSpace(part))
			if len(fields) == 0 || fields[0] == "type" && len(fields) == 1 {
				continue
			}
			name := fields[0]
			if name == "type" && len(fields) > 1 {
				name = fields[1]
			}
			if name == "default" || name == "*" || exported[name] {
				continue
			}
			found = append(found, UnboundName{
				File: file, Line: number + 1, Name: name, Scope: path,
				Ground: path + " exports no " + name,
				Text:   strings.TrimSpace(line)})
		}
	}
	return found
}

// scriptModulePath resolves a relative specifier to the ONE file in the tree it
// names, through the extensions and the index file typescript resolves through.
func (b *bindings) scriptModulePath(from, specifier string) string {
	stem := filepath.ToSlash(filepath.Join(filepath.Dir(from), specifier))
	if strings.HasPrefix(stem, "..") {
		return ""
	}
	var resolved []string
	for _, suffix := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		"/index.ts", "/index.tsx", "/index.js"} {
		candidate := stem + suffix
		if info, err := os.Stat(filepath.Join(b.root, filepath.FromSlash(candidate))); err == nil &&
			!info.IsDir() {
			resolved = append(resolved, candidate)
		}
	}
	if len(resolved) == 0 {
		return ""
	}
	// The first extension that exists is the one the resolver takes, which is
	// the order above and is why it is written out rather than sorted.
	return resolved[0]
}

// scriptExports is what one module publishes, read by the surface reader itself
// so an export is what the photograph says an export is.
//
// readable is false where the module re-exports a barrel (`export * from`) or
// spells an export this reader cannot name — a destructured `export const {a}`,
// a computed one — because a module whose surface is partly unreadable is a
// module nothing may be reported missing from.
func (b *bindings) scriptExports(path string) (map[string]bool, bool) {
	body, read := readSurfaceFile(filepath.Join(b.root, filepath.FromSlash(path)))
	if !read {
		return nil, false
	}
	names := map[string]bool{}
	for _, declaration := range DeclarationsIn(path, body) {
		if !strings.Contains(declaration.Name, ".") {
			names[declaration.Name] = true
		}
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "export") {
			continue
		}
		if strings.HasPrefix(trimmed, "export *") || strings.Contains(trimmed, "export {") &&
			strings.Contains(trimmed, "from") {
			return nil, false
		}
		// An export line the surface reader could not name a declaration out of
		// is an export this cannot enumerate.
		if !scriptExport.MatchString(line) && !scriptExportList.MatchString(line) &&
			!scriptClass.MatchString(line) && !strings.HasPrefix(trimmed, "export default") {
			return nil, false
		}
	}
	return names, true
}

// ── the third python shape: a bare name nothing in reach binds ───────────────

// pythonBuiltins is the language's own vocabulary, which is bound in every file
// by nobody. It is a list because there is no way to read it off a tree — it is
// the one place in this reader where a name is known rather than measured, and
// it is the interpreter's list rather than a project's.
var pythonBuiltins = map[string]bool{
	"abs": true, "aiter": true, "all": true, "anext": true, "any": true, "ascii": true,
	"bin": true, "bool": true, "breakpoint": true, "bytearray": true, "bytes": true,
	"callable": true, "chr": true, "classmethod": true, "compile": true, "complex": true,
	"dict": true, "dir": true, "divmod": true, "enumerate": true, "filter": true,
	"float": true, "format": true, "frozenset": true, "hash": true, "help": true,
	"hex": true, "id": true, "input": true, "int": true, "isinstance": true,
	"issubclass": true, "iter": true, "len": true, "list": true, "map": true,
	"max": true, "memoryview": true, "min": true, "next": true, "object": true,
	"oct": true, "open": true, "ord": true, "pow": true, "print": true, "property": true,
	"range": true, "repr": true, "reversed": true, "round": true, "set": true,
	"slice": true, "sorted": true, "staticmethod": true, "str": true, "sum": true,
	"super": true, "tuple": true, "type": true, "zip": true,
	"setattr": true, "getattr": true, "hasattr": true, "delattr": true,
	"globals": true, "locals": true, "vars": true, "exec": true, "eval": true,
	"__import__": true, "exit": true, "quit": true, "copyright": true,
	"credits": true, "license": true,
	"ArithmeticError": true, "AssertionError": true, "AttributeError": true,
	"BaseException": true, "BlockingIOError": true, "BrokenPipeError": true,
	"BufferError": true, "BytesWarning": true, "ConnectionError": true,
	"DeprecationWarning": true, "EOFError": true, "Ellipsis": true, "EnvironmentError": true,
	"Exception": true, "False": true, "FileExistsError": true, "FileNotFoundError": true,
	"FloatingPointError": true, "FutureWarning": true, "GeneratorExit": true,
	"IOError": true, "ImportError": true, "ImportWarning": true, "IndentationError": true,
	"IndexError": true, "InterruptedError": true, "IsADirectoryError": true,
	"KeyError": true, "KeyboardInterrupt": true, "LookupError": true, "MemoryError": true,
	"ModuleNotFoundError": true, "NameError": true, "None": true, "NotADirectoryError": true,
	"NotImplemented": true, "NotImplementedError": true, "OSError": true,
	"OverflowError": true, "PermissionError": true, "ProcessLookupError": true,
	"RecursionError": true, "ReferenceError": true, "RuntimeError": true,
	"RuntimeWarning": true, "StopAsyncIteration": true, "StopIteration": true,
	"SyntaxError": true, "SyntaxWarning": true, "SystemError": true, "SystemExit": true,
	"TabError": true, "TimeoutError": true, "True": true, "TypeError": true,
	"UnboundLocalError": true, "UnicodeDecodeError": true, "UnicodeEncodeError": true,
	"UnicodeError": true, "UnicodeWarning": true, "UserWarning": true, "ValueError": true,
	"Warning": true, "ZeroDivisionError": true, "self": true, "cls": true,
	// The keywords a statement opens with reach this reader as identifiers,
	// because it looks at shapes and not at grammar.
	"and": true, "as": true, "assert": true, "async": true, "await": true, "break": true,
	"case": true, "class": true, "continue": true, "def": true, "del": true, "elif": true,
	"else": true, "except": true, "finally": true, "for": true, "from": true,
	"global": true, "if": true, "import": true, "in": true, "is": true, "lambda": true,
	"match": true, "nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
}

// unboundPythonBinder is every shape that puts a name in reach somewhere in a
// file: a target, a parameter, a loop variable, an `as`, a `global`. It is read
// as a SUPERSET on purpose — a name bound anywhere in the file is treated as
// bound everywhere in it, because a scope analysis that got one comprehension
// wrong would invent a finding, and a scope analysis this reader skips only
// costs it one.
var unboundPythonBinder = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)

// pythonUnboundBare is a name used as a call or as the base of an attribute
// reach that nothing in the file binds, nothing imports, and the language does
// not own.
//
// IT IS ASKED ONLY OF A FILE WHOSE EVERY IMPORT RESOLVES INSIDE THE TREE. One
// import of `os`, of `pytest`, of anything with a `site-packages` behind it, and
// this reader has no way to know what that module put in reach — so it says
// nothing at all about the file. Most real files are in that state, and that is
// the intended shape of this check: it is the narrowest of the three and it
// fires on a self-contained module or not at all.
func pythonUnboundBare(file string, lines, raw []string, index *bindings) []UnboundName {
	if !pythonImportsAreInTree(file, lines, index) {
		return nil
	}
	inReach := map[string]bool{}
	for index, line := range lines {
		if match := unboundPythonFrom.FindStringSubmatch(line); match != nil {
			for _, name := range pythonImportedNames(lines, index) {
				inReach[name] = true
			}
			for _, name := range importedAs(match[3]) {
				inReach[name] = true
			}
			continue
		}
		if match := unboundPythonImport.FindStringSubmatch(line); match != nil {
			for _, part := range strings.Split(match[1], ",") {
				fields := strings.Fields(part)
				switch {
				case len(fields) >= 3 && fields[1] == "as":
					inReach[fields[2]] = true
				case len(fields) >= 1:
					inReach[strings.Split(fields[0], ".")[0]] = true
				}
			}
			continue
		}
		if match := pythonDef.FindStringSubmatch(line); match != nil {
			inReach[match[2]] = true
			// Every word of a signature is a parameter, a default or an
			// annotation, and all three are names in reach of the body.
			for _, word := range unboundPythonBinder.FindAllString(line, -1) {
				inReach[word] = true
			}
			continue
		}
		if match := pythonClass.FindStringSubmatch(line); match != nil {
			inReach[match[2]] = true
			continue
		}
		if left, assigns := pythonAssignedTo(line); assigns {
			for _, word := range unboundPythonBinder.FindAllString(left, -1) {
				inReach[word] = true
			}
			continue
		}
		// `for x in`, `with y as z`, `except E as err`, `global g` — the
		// binding is whatever word follows, and every word of the statement is
		// taken because over-binding costs a silence.
		trimmed := strings.TrimSpace(line)
		for _, opener := range []string{"for ", "with ", "except ", "global ", "nonlocal ", "lambda "} {
			if strings.HasPrefix(trimmed, opener) || strings.Contains(line, " as ") ||
				strings.Contains(line, "lambda ") {
				for _, word := range unboundPythonBinder.FindAllString(line, -1) {
					inReach[word] = true
				}
				break
			}
		}
	}
	var found []UnboundName
	seen := map[string]bool{}
	for number, line := range lines {
		// An import statement spells a module path, not a reference: the `igel`
		// of `from igel.configs import configs` is the name of a package this
		// file is reaching THROUGH, and nothing in this file binds it.
		if unboundPythonFrom.MatchString(line) || unboundPythonImport.MatchString(line) {
			continue
		}
		for _, match := range unboundPythonBare.FindAllStringSubmatch(line, -1) {
			name := match[2]
			if seen[name] || inReach[name] || pythonBuiltins[name] ||
				index.attributes[name] || dunder(name) {
				continue
			}
			seen[name] = true
			found = append(found, UnboundName{
				File: file, Line: number + 1, Name: name, Scope: file,
				Ground: "neither imported nor defined in " + file + ", and this file's " +
					"every import resolves inside the tree",
				Text: lineText(raw, number)})
		}
	}
	return found
}

// pythonImportsAreInTree says every module this file imports is a file this
// reader can read. It is the gate on the bare-name check and the whole of its
// safety: a file that imports one thing from outside the tree has names in
// reach that nothing here can enumerate.
func pythonImportsAreInTree(file string, lines []string, index *bindings) bool {
	imports := 0
	for _, line := range lines {
		if match := unboundPythonFrom.FindStringSubmatch(line); match != nil {
			imports++
			if index.pythonModulePath(file, match[1], match[2]) == "" {
				return false
			}
			continue
		}
		if match := unboundPythonImport.FindStringSubmatch(line); match != nil {
			for _, part := range strings.Split(match[1], ",") {
				fields := strings.Fields(part)
				if len(fields) == 0 {
					continue
				}
				imports++
				if index.pythonModulePath(file, "", fields[0]) == "" {
					return false
				}
			}
		}
	}
	// Reaching the end is every import resolved. A file with no imports at all
	// is self-contained by definition, and it is the one this check was written
	// for.
	_ = imports
	return true
}

// pythonBases is the names a class declaration inherits from, over as many
// lines as the header runs.
//
// Keyword arguments in the header — `class RichLog(ScrollView, can_focus=True)`
// — are not bases and are dropped; a subscripted base is read as the thing being
// subscripted, and a dotted one as its last segment, which is the name a class
// declaration spells.
func pythonBases(lines []string, at int) []string {
	header := lines[at]
	for depth, number := openBrackets(header, 0), at+1; depth > 0 && number < len(lines); number++ {
		header += " " + lines[number]
		depth = openBrackets(lines[number], depth)
	}
	open := strings.Index(header, "(")
	if open < 0 {
		return nil
	}
	inner := header[open+1:]
	if close := strings.LastIndex(inner, ")"); close >= 0 {
		inner = inner[:close]
	}
	var bases []string
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		if part == "" || strings.Contains(part, "=") {
			continue
		}
		if bracket := strings.Index(part, "["); bracket >= 0 {
			part = part[:bracket]
		}
		if segments := strings.Split(strings.TrimSpace(part), "."); len(segments) > 0 {
			if name := strings.TrimSpace(segments[len(segments)-1]); name != "" {
				bases = append(bases, name)
			}
		}
	}
	return bases
}

// inherits says this class stands on a base THIS TREE DOES NOT DECLARE, and so
// may carry attributes bound in code no reader here has seen.
//
// It is the largest silence in this file and it is the one that keeps the reader
// honest. `class WidgetPlacement(NamedTuple)` gets `_replace` from the standard
// library, `class TextualHandler(Handler)` gets `format` from `logging`, and a
// reader that did not know this reported both as names nothing assigns —
// measured on textual's own tree, 2026-08-29. A base declared in the tree is
// read like any other class and its bindings are already in the index.
func (b *bindings) inherits(class pythonScope) bool {
	for _, base := range class.bases {
		if base == "object" || b.classes[base] {
			continue
		}
		return true
	}
	return false
}
