package verify

// The other half of the photograph: the public NAMES a tree spells, on either
// side of the work.
//
// The check-level half reads what a project's own verification says, and it is
// blind in one direction that turns out to matter more than any other. igel s11
// deleted eight public class attributes off `Igel` — `results_path` and its
// seven siblings — and moved them onto instances set in `__init__`. No check the
// project owns touches any of them, so the reading of the finished tree was an
// IMPROVEMENT: named 2 → 14, red 2 → 0. All twenty-four hidden tests failed at
// setup on `Igel.results_path`, and nothing in the run said a word.
//
// A CHECK IS EVIDENCE THAT SOMETHING IS EXERCISED; IT IS NOT EVIDENCE THAT
// NOTHING ELSE EXISTS. The public surface is the half a suite cannot see: a name
// that was there before the work and is gone after it is a fact about the world,
// measured twice, with no model in the loop and no citation to weigh.
//
// It is read BY LANGUAGE SHAPE and never by a list of names — FAILSAFE.md clause
// 1 — and it is deliberately CONSERVATIVE. Everything here reports only what it
// can read with certainty; what it cannot parse it does not guess at, because
// the cost of a name invented here is a false blocker on real work, and the cost
// of a name missed is the state this file was written in.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"

	"strings"
)

const (
	// surfaceFileLimit bounds how many source files one baseline reads.
	//
	// The whole tree has to be read, because at the moment the baseline is taken
	// there is no diff to say which files will matter. Two thousand is the size
	// of a large single-language repository — textual is about seven hundred
	// python files, ofetch about ninety typescript ones — and past it the walk
	// stops and the surface is partial, which degrades in the safe direction: a
	// file with no baseline entry can never be reported as having lost a name.
	surfaceFileLimit = 2000
	// surfaceReadBudget bounds the bytes, because a file count says nothing
	// about a repository full of generated code. Eight megabytes is four times
	// the scope walk's own budget, spent once per JOB rather than once per
	// reading.
	surfaceReadBudget = 8 << 20
	// surfaceFileBytes bounds one file, so a single generated module cannot
	// spend the budget by itself.
	surfaceFileBytes = 512 << 10
	// surfaceNamesReported bounds what one finding says out loud. It is
	// store.VerificationSample's sibling and the same size, for the same reason:
	// a list of names is read to learn what SHAPE the loss has.
	surfaceNamesReported = 8
)

// Surface is the public names a tree spells, keyed by the file that spells them.
//
// Per FILE rather than per project, because that is what makes the comparison
// affordable: the finished tree is re-read only where the run's own record says
// it changed something, and a name that moved from one file to another is a
// removal from the first and an addition to the second — which is what a rename
// is, and what it should read as.
type Surface map[string][]string

// PublicSurface reads the public names of the source files under root that this
// program can parse with certainty, walking the whole tree inside one budget.
//
// It is the BASELINE half, and it is taken with the check-level reading, before
// the job's first change.
func PublicSurface(root string) Surface {
	surface := Surface{}
	files, budget := 0, surfaceReadBudget
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
		if surfaceLanguage(entry.Name()) == "" || testFileName(entry.Name()) {
			return nil
		}
		if files++; files > surfaceFileLimit || budget <= 0 {
			return filepath.SkipAll
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		slashed := filepath.ToSlash(relative)
		body, read := readSurfaceFile(path)
		budget -= len(body)
		if !read {
			return nil
		}
		if names := publicNames(slashed, body); len(names) > 0 {
			surface[slashed] = names
		}
		return nil
	})
	return surface
}

// SurfaceOf reads the public names of NAMED files only, which is what the
// finished tree is read for: the run's own record of what it changed.
//
// A file the record names and the tree no longer holds contributes nothing here,
// so every public name its baseline entry held is reported removed — which is
// what deleting a module does.
func SurfaceOf(root string, files Focus) Surface {
	surface := Surface{}
	budget := surfaceReadBudget
	for _, file := range files {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))
		if clean == "" || clean == "." || strings.HasPrefix(clean, "..") ||
			filepath.IsAbs(clean) || surfaceLanguage(lastSegment(clean)) == "" || budget <= 0 {
			continue
		}
		body, read := readSurfaceFile(filepath.Join(root, filepath.FromSlash(clean)))
		budget -= len(body)
		if !read {
			continue
		}
		if names := publicNames(clean, body); len(names) > 0 {
			surface[clean] = names
		}
	}
	return surface
}

// Removed names every public name the baseline held that the finished tree does
// not, looking ONLY at the files named — the run's own record of what it
// changed.
//
// Scoping to the record is what keeps this a measurement of the WORK rather than
// of the repository. A name that vanished from a file nobody touched vanished
// some other way, and reporting it would hand a leaf a finding about something
// it never did.
//
// A rename reads as a removal, and correctly: the old name is gone, every caller
// of it is broken, and whether something similar was added in its place is a
// judgement this makes no attempt at. The added name is visible in the same two
// readings for whoever wants it.
func (baseline Surface) Removed(now Surface, files Focus) []string {
	if len(baseline) == 0 {
		return nil
	}
	var gone []string
	for _, file := range files {
		clean := filepath.ToSlash(filepath.Clean(strings.TrimSpace(file)))
		held, known := baseline[clean]
		if !known {
			continue
		}
		standing := make(map[string]bool, len(now[clean]))
		for _, name := range now[clean] {
			standing[name] = true
		}
		for _, name := range held {
			if !standing[name] {
				gone = append(gone, name)
			}
		}
	}
	return sortedUnique(gone)
}

// readSurfaceFile is one file's text, bounded, or read=false when it is bigger
// than one file's share or cannot be read at all.
func readSurfaceFile(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Size() > surfaceFileBytes {
		return "", false
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(body), true
}

// surfaceLanguage is which reader this file gets, by the shape of its name. An
// extension this program has no reader for is not a file it guesses at.
func surfaceLanguage(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs":
		return "script"
	case ".rs":
		return "rust"
	}
	return ""
}

// publicNames is the file's public surface, read by whichever reader its
// language shape calls for.
func publicNames(file, body string) []string {
	switch surfaceLanguage(lastSegment(file)) {
	case "go":
		return goPublicNames(file, body)
	case "python":
		return pythonPublicNames(body)
	case "script":
		return scriptPublicNames(body)
	case "rust":
		return rustPublicNames(body)
	}
	return nil
}

// ── Go: the standard library reads it, so nothing here guesses ───────────────

// goPublicNames is every exported identifier the file declares at package level,
// plus exported methods and exported struct fields.
//
// It is the one language in this file with a real parser, because the standard
// library ships one. A file that does not parse contributes NOTHING rather than
// a partial reading: a syntax error mid-edit would otherwise read as half the
// package's surface disappearing.
func goPublicNames(file, body string) []string {
	parsed, err := parser.ParseFile(token.NewFileSet(), file, body, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var names []string
	for _, declaration := range parsed.Decls {
		switch node := declaration.(type) {
		case *ast.FuncDecl:
			if !node.Name.IsExported() {
				continue
			}
			if node.Recv == nil || len(node.Recv.List) == 0 {
				names = append(names, node.Name.Name)
				continue
			}
			if receiver := goReceiver(node.Recv.List[0].Type); receiver != "" {
				names = append(names, receiver+"."+node.Name.Name)
			}
		case *ast.GenDecl:
			for _, spec := range node.Specs {
				names = append(names, goSpecNames(spec)...)
			}
		}
	}
	return sortedUnique(names)
}

// goSpecNames is one declaration's exported names: a type and its exported
// fields, or the exported names of a var or const block.
func goSpecNames(spec ast.Spec) []string {
	var names []string
	switch node := spec.(type) {
	case *ast.TypeSpec:
		if !node.Name.IsExported() {
			return nil
		}
		names = append(names, node.Name.Name)
		structure, ok := node.Type.(*ast.StructType)
		if !ok || structure.Fields == nil {
			return names
		}
		for _, field := range structure.Fields.List {
			for _, ident := range field.Names {
				if ident.IsExported() {
					names = append(names, node.Name.Name+"."+ident.Name)
				}
			}
		}
	case *ast.ValueSpec:
		for _, ident := range node.Names {
			if ident.IsExported() {
				names = append(names, ident.Name)
			}
		}
	}
	return names
}

// goReceiver is the type a method hangs off, with the pointer star taken off.
func goReceiver(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if index, ok := expr.(*ast.IndexExpr); ok {
		expr = index.X
	}
	if index, ok := expr.(*ast.IndexListExpr); ok {
		expr = index.X
	}
	if ident, ok := expr.(*ast.Ident); ok && ident.IsExported() {
		return ident.Name
	}
	return ""
}

// ── The line-and-indent readers ──────────────────────────────────────────────
//
// No external parser is worth a dependency for this, and none of these needs
// one: what they read is a DECLARATION AT THE START OF A LINE, which every
// language here spells the same way every time because that is what its own
// formatter enforces. Everything else — a name assigned inside a conditional, a
// class built by a decorator, an export re-exported through a barrel — is left
// alone on purpose. A reading that guessed at those would raise findings about
// work nobody did.

// pythonDef and its siblings read the shapes python spells at the start of a
// line. The indent is captured because it is what says whether a name belongs to
// the module or to the class above it.
var (
	pythonDef    = regexp.MustCompile(`^(\s*)(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	pythonClass  = regexp.MustCompile(`^(\s*)class\s+([A-Za-z_][A-Za-z0-9_]*)\s*[(:]`)
	pythonAssign = regexp.MustCompile(
		`^(\s*)([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*[^=]+)?=[^=]`)
	pythonSelfAssign = regexp.MustCompile(
		`^\s*self\.([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*[^=]+)?=[^=]`)
)

// pythonPublicNames is a module's public surface: what it defines at module
// level, and what its public classes carry.
//
// THE TWO WAYS A CLASS CARRIES A NAME ARE DIFFERENT NAMES, and igel s11 is the
// whole reason. That run moved eight attributes off the class body and onto
// instances in `__init__` — `results_path = configs.get(...)` became
// `self.results_path = _cfg[...]` — which is exactly the change that reads as
// "still there" to anything matching on the bare word, and exactly the change
// that broke twenty-four tests at setup on `Igel.results_path`. So a class-body
// name is spelled `Igel.results_path`, the way it is reached, and an instance
// name is spelled `Igel().results_path`, the way THAT is reached. They are two
// facts and this keeps them two names.
func pythonPublicNames(body string) []string {
	var names []string
	class, classIndent, bodyIndent := "", -1, -1
	inMethod := false
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		// A class ends where the indentation comes back to its own level.
		if class != "" && indent <= classIndent {
			class, classIndent, bodyIndent, inMethod = "", -1, -1, false
		}
		switch {
		case pythonClass.MatchString(line):
			match := pythonClass.FindStringSubmatch(line)
			if len(match[1]) == 0 && public(match[2]) {
				names = append(names, match[2])
				class, classIndent, bodyIndent, inMethod = match[2], 0, -1, false
			}
			continue
		case pythonDef.MatchString(line):
			match := pythonDef.FindStringSubmatch(line)
			switch {
			case len(match[1]) == 0:
				if public(match[2]) {
					names = append(names, match[2])
				}
				class, classIndent, bodyIndent, inMethod = "", -1, -1, false
			case class != "":
				if bodyIndent < 0 {
					bodyIndent = len(match[1])
				}
				if len(match[1]) == bodyIndent {
					inMethod = true
					if public(match[2]) {
						names = append(names, class+"."+match[2])
					}
				}
			}
			continue
		}
		if class == "" {
			continue
		}
		if inMethod && pythonSelfAssign.MatchString(line) {
			// An instance attribute, wherever in the class it is set. Which
			// method sets it is not read, because a name reachable on an
			// instance is public whether __init__ or a setter put it there.
			if name := pythonSelfAssign.FindStringSubmatch(line)[1]; public(name) {
				names = append(names, class+"()."+name)
			}
			continue
		}
		if match := pythonAssign.FindStringSubmatch(line); match != nil {
			if bodyIndent < 0 {
				bodyIndent = len(match[1])
			}
			if len(match[1]) == bodyIndent && public(match[2]) {
				names = append(names, class+"."+match[2])
			}
		}
	}
	return sortedUnique(names)
}

// scriptExport and its siblings read the shapes typescript and javascript spell
// an export in. Only the forms whose name is on the same line are read.
var (
	scriptExport = regexp.MustCompile(
		`^\s*export\s+(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?` +
			`(?:async\s+)?(?:function\*?|class|interface|type|enum|const|let|var)\s+` +
			`([A-Za-z_$][A-Za-z0-9_$]*)`)
	scriptExportList = regexp.MustCompile(`^\s*export\s*\{([^}]*)\}`)
	scriptClass      = regexp.MustCompile(
		`^(\s*)export\s+(?:default\s+)?(?:declare\s+)?(?:abstract\s+)?class\s+` +
			`([A-Za-z_$][A-Za-z0-9_$]*)`)
	scriptMember = regexp.MustCompile(
		`^(\s+)(?:public\s+|readonly\s+|static\s+|async\s+|get\s+|set\s+)*` +
			`([A-Za-z_$][A-Za-z0-9_$]*)\s*[(:=]`)
	scriptPrivate = regexp.MustCompile(`^\s*(?:private|protected)\s|^\s*#`)
)

// scriptPublicNames is what a module exports, and what its exported classes
// expose.
//
// A member is public unless it says otherwise: typescript's default is public,
// which is why the private marker is what is looked for rather than the public
// one. `#name` is javascript's own private field and is skipped for the same
// reason a leading underscore is in python.
func scriptPublicNames(body string) []string {
	var names []string
	class, classIndent, bodyIndent := "", -1, -1
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if class != "" && indent <= classIndent && !strings.HasPrefix(trimmed, "}") {
			class, classIndent, bodyIndent = "", -1, -1
		}
		if match := scriptClass.FindStringSubmatch(line); match != nil {
			names = append(names, match[2])
			class, classIndent, bodyIndent = match[2], len(match[1]), -1
			continue
		}
		if match := scriptExport.FindStringSubmatch(line); match != nil {
			names = append(names, match[1])
			class, classIndent, bodyIndent = "", -1, -1
			continue
		}
		if match := scriptExportList.FindStringSubmatch(line); match != nil {
			for _, part := range strings.Split(match[1], ",") {
				// `export { a as b }` exports b, which is the name a caller
				// writes; that is the one this is about.
				fields := strings.Fields(strings.TrimSpace(part))
				if len(fields) == 0 {
					continue
				}
				names = append(names, strings.TrimSpace(fields[len(fields)-1]))
			}
			continue
		}
		if class == "" || scriptPrivate.MatchString(line) || strings.HasPrefix(trimmed, "}") {
			continue
		}
		if match := scriptMember.FindStringSubmatch(line); match != nil {
			if bodyIndent < 0 {
				bodyIndent = len(match[1])
			}
			if len(match[1]) == bodyIndent && !scriptKeyword(match[2]) {
				names = append(names, class+"."+match[2])
			}
		}
	}
	return sortedUnique(names)
}

// scriptKeyword keeps the member reader off the statements that look like one.
// A conservative reader would rather miss `if` as a member name than report the
// body of every method as part of a class's surface.
func scriptKeyword(name string) bool {
	switch name {
	case "if", "for", "while", "switch", "return", "case", "catch", "do", "else",
		"try", "throw", "new", "await", "typeof", "constructor":
		return true
	}
	return false
}

// rustPub and its siblings read the shapes rust spells a public item in.
var (
	rustPub = regexp.MustCompile(
		`^(\s*)pub(?:\s*\([^)]*\))?\s+(?:async\s+|unsafe\s+|extern\s+"[^"]*"\s+)*` +
			`(?:fn|struct|enum|trait|type|const|static|mod|union)\s+([A-Za-z_][A-Za-z0-9_]*)`)
	rustImpl  = regexp.MustCompile(`^(\s*)impl(?:\s*<[^>]*>)?\s+(?:([A-Za-z_][A-Za-z0-9_]*)\s+for\s+)?([A-Za-z_][A-Za-z0-9_]*)`)
	rustField = regexp.MustCompile(`^(\s+)pub(?:\s*\([^)]*\))?\s+([A-Za-z_][A-Za-z0-9_]*)\s*:`)
	rustBlock = regexp.MustCompile(`^(\s*)pub(?:\s*\([^)]*\))?\s+struct\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)
)

// rustPublicNames is a module's public items, and the public members of the
// types it declares.
//
// A `pub fn` inside an `impl Type` is spelled `Type::name`, the way it is
// reached. An impl block for a trait is read as the type's surface too, because
// removing it removes a name callers use.
func rustPublicNames(body string) []string {
	var names []string
	scope, scopeIndent := "", -1
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if scope != "" && indent <= scopeIndent && !strings.HasPrefix(trimmed, "}") {
			scope, scopeIndent = "", -1
		}
		if match := rustImpl.FindStringSubmatch(line); match != nil {
			scope, scopeIndent = match[3], len(match[1])
			continue
		}
		if match := rustBlock.FindStringSubmatch(line); match != nil {
			names = append(names, match[2])
			scope, scopeIndent = match[2], len(match[1])
			continue
		}
		if match := rustPub.FindStringSubmatch(line); match != nil {
			if scope != "" && len(match[1]) > scopeIndent {
				names = append(names, scope+"::"+match[2])
			} else {
				names = append(names, match[2])
				scope, scopeIndent = "", -1
			}
			continue
		}
		if match := rustField.FindStringSubmatch(line); match != nil && scope != "" {
			names = append(names, scope+"."+match[2])
		}
	}
	return sortedUnique(names)
}

// public is python's own privacy rule and javascript's convention both: a
// leading underscore says the author called it internal, and this program takes
// them at their word. It is a rule about SHAPE and not a list of names.
func public(name string) bool {
	return name != "" && !strings.HasPrefix(name, "_")
}

// SurfaceNamed is what a finding says out loud: the first few names, and a count
// for the rest.
func SurfaceNamed(names []string) []string {
	if len(names) > surfaceNamesReported {
		return names[:surfaceNamesReported]
	}
	return names
}

// MissingFrom names the paths in a record that the tree no longer holds.
//
// It is the deletion case, and it belongs beside ChangedSources rather than
// inside it: that reader keeps only what is still there, because a scoped test
// command handed a path that has gone collects nothing at all. The symbol-level
// half wants the opposite — a public module that was deleted has lost every
// public name it had, and there is no file left to read that off.
func MissingFrom(root string, record []string) Focus {
	gone := make(Focus, 0, len(record))
	for _, entry := range record {
		clean := strings.TrimSpace(entry)
		if clean == "" {
			continue
		}
		if filepath.IsAbs(clean) {
			relative, err := filepath.Rel(root, clean)
			if err != nil || strings.HasPrefix(relative, "..") {
				continue
			}
			clean = relative
		}
		clean = filepath.ToSlash(filepath.Clean(clean))
		if clean == "." || strings.HasPrefix(clean, "..") {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(clean))); err == nil {
			continue
		}
		gone = append(gone, clean)
	}
	return Focus(sortedUnique(gone))
}
