// Static import/dependency graph for context expansion — port of
// src/session/import-graph.ts (W5d).
//
// Pure by design: callers supply `[]SourceFile`; no filesystem I/O, no clock,
// no RNG (so no injectable now()/random() hooks are needed here). Regex-based
// extractors cover TypeScript/JavaScript, Python and Go; an injectable
// extractor map extends coverage to other languages.
//
// Fidelity notes (deliberate, do not "fix"):
//
//   - REGEX WHITESPACE. JS `\s` is a much larger class than RE2 `\s`
//     (RE2: [\t\n\f\r ]). Every ported pattern spells the JS class out as
//     jsWS below, so a NBSP / U+2028 / ZWNBSP between `import` and its
//     specifier is consumed exactly like V8 consumes it.
//
//   - MULTILINE `^`. Four of the five patterns are /…/gm. RE2's (?m)^ only
//     matches after U+000A, while JS also restarts a line after U+000D,
//     U+2028 and U+2029. Instead of (?m) the ported patterns are \A-anchored
//     and driven by execGlobalMultiline, which walks the JS line-start
//     positions and resumes each iteration from the previous match end —
//     the same state machine as `while ((m = re.exec(text)) !== null)` with
//     a `g` regex. A file with classic-Mac CR line endings therefore yields
//     the same imports in Go as in Bun.
//
//   - `[\s\S]` becomes `(?s:.)`. Both mean "any character"; JS operates on
//     UTF-16 code units and Go on runes, which is only observable for lone
//     surrogates (unrepresentable in a Go string anyway).
//
//   - STRING ORDER. asc() is the TS `a < b ? -1 : a > b ? 1 : 0`, i.e. JS
//     relational comparison, which is UTF-16 CODE-UNIT lexicographic — not
//     UTF-8 byte order. They disagree whenever a non-BMP character meets a
//     U+E000..U+FFFF one (surrogates 0xD800.. sort BELOW 0xE000 in UTF-16 but
//     the astral char's UTF-8 sorts ABOVE), so asc() compares code units.
//     Sorting still goes through sort.SliceStable per the port rules even
//     though the inputs are Sets and can never tie.
//
//   - capContent scans BYTES for the 5000th U+000A even though the TS scans
//     UTF-16 code units. `\n` is ASCII, so the byte offset of the Nth newline
//     and the UTF-16 offset of the Nth newline denote the same prefix; the
//     resulting substring is identical for any input.
//
//   - SUSPECTED TS BUGS, KEPT (see the divergence list in the port report):
//     indexFiles' doc-comment claims a "lowercase-keyed index" but
//     normalizePath does not lowercase; buildImportGraph guards a resolved
//     edge with `known.has(resolved)` where `resolved` is the CANONICAL
//     (un-normalized) path while `known` is keyed by the NORMALIZED path, so
//     a file list containing "./src/a.ts" resolves the edge and then throws
//     it away as external; dirname("/x.ts") returns "" and so drops the
//     leading slash of root-level absolute paths; joinRelative silently
//     swallows ".." that escapes the root; and extractPythonImports' `as`
//     splitting plus its `if (mod)` guard are unreachable because the capture
//     group can only ever contain [.\w] runs separated by commas.
package importgraph

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Agent-Field/swe-pro-go/internal/jscompat"
)

// MAX_IMPORT_SCAN_LINES is the max number of import lines scanned per file
// (pathological files). Mirrors the TS export name verbatim.
const MAX_IMPORT_SCAN_LINES float64 = 5000 // W5-TODO(knobs)

// JS_RESOLVE_EXTENSIONS lists the default top-level extensions tried when
// resolving a bare relative import. A package-level var, not a const slice,
// because the TS export is a mutable array read at resolve time.
var JS_RESOLVE_EXTENSIONS = []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} // W5-TODO(knobs)

// ImportExtractor is `(path: string, content: string) => string[]`.
type ImportExtractor func(path string, content string) []string

// ImportExtractorMap is `Readonly<Record<string, ImportExtractor>>`. Only ever
// probed by key, never iterated, so a bare Go map is order-safe here.
type ImportExtractorMap map[string]ImportExtractor

// SourceFile is the TS anonymous `{ path: string; content: string }`.
type SourceFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// ImportGraph mirrors the TS interface. The three members are JS Maps, so they
// are insertion-ordered OrderedMaps here; iteration order is observable
// (buildImportGraph re-sorts `dependents` in place while iterating it).
type ImportGraph struct {
	// Deps maps path → sorted unique internal dependency paths (resolved).
	Deps *jscompat.OrderedMap[string, []string] `json:"deps"`
	// Dependents maps path → sorted unique internal dependent paths (reverse edges).
	Dependents *jscompat.OrderedMap[string, []string] `json:"dependents"`
	// External maps path → sorted unique unresolved/external import specifiers.
	External *jscompat.OrderedMap[string, []string] `json:"external"`
}

// BuildImportGraphOptions mirrors the TS options bag. A nil/zero Extractors
// field is TS `undefined` (falsy → defaults used untouched); a non-nil empty
// map is TS `{}` (truthy → the spread branch, same effective table).
type BuildImportGraphOptions struct {
	// Extractors overrides/extends language extractors (keyed by file
	// extension, e.g. ".rs").
	Extractors ImportExtractorMap `json:"-"`
}

// ExtractImportsOptions mirrors extractImports' inline options type.
type ExtractImportsOptions struct {
	// KnownFiles nil means the TS `undefined` (no known set). A non-nil empty
	// slice means `[]`, which is TRUTHY in JS and therefore builds an empty
	// index rather than skipping resolution.
	KnownFiles []string           `json:"knownFiles"`
	Extractors ImportExtractorMap `json:"-"`
}

// ---------------------------------------------------------------------------
// JS regex whitespace class + multiline `^` emulation
// ---------------------------------------------------------------------------

// jsWS is the ECMAScript `\s`: WhiteSpace (TAB VT FF SP NBSP ZWNBSP + Zs)
// plus LineTerminator (LF CR LS PS).
const jsWS = `[\t\n\v\f\r \x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}]`

const (
	jsWSStar = jsWS + `*`
	jsWSPlus = jsWS + `+`
)

// jsLineStarts returns every index at which a JS multiline `^` asserts: 0 and
// any index immediately following LF, CR, U+2028 or U+2029. Note CRLF yields
// two starts (after the CR and after the LF), exactly like V8.
func jsLineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); {
		r, sz := utf8.DecodeRuneInString(text[i:])
		if r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 {
			starts = append(starts, i+sz)
		}
		i += sz
	}
	return starts
}

// execGlobalMultiline replays `while ((m = re.exec(text)) !== null)` for a
// /^…/gm pattern: re must be \A-anchored (its leading `^` stripped into the
// anchor), match starts are restricted to JS line-start positions, and each
// iteration resumes at the previous match end. Every ported /gm pattern is
// incapable of an empty match, so the JS "bump lastIndex on empty match" rule
// is not needed.
func execGlobalMultiline(re *regexp.Regexp, text string) [][]string {
	var out [][]string
	last := 0
	for _, p := range jsLineStarts(text) {
		if p < last {
			continue
		}
		loc := re.FindStringSubmatchIndex(text[p:])
		if loc == nil {
			continue
		}
		groups := make([]string, len(loc)/2)
		for g := range groups {
			if loc[2*g] < 0 {
				groups[g] = ""
				continue
			}
			groups[g] = text[p+loc[2*g] : p+loc[2*g+1]]
		}
		out = append(out, groups)
		last = p + loc[1]
	}
	return out
}

// ---------------------------------------------------------------------------
// Language extractors — return raw import specifiers (not yet resolved).
// ---------------------------------------------------------------------------

var tsJsImportRE = regexp.MustCompile(
	`(?:import` + jsWSPlus + `(?:type` + jsWSPlus + `)?(?:(?s:.)*?` + jsWSPlus + `from` + jsWSPlus + `)?` +
		`|export` + jsWSPlus + `(?:type` + jsWSPlus + `)?(?s:.)*?` + jsWSPlus + `from` + jsWSPlus +
		`|require` + jsWSStar + `\(` + jsWSStar +
		`|import` + jsWSStar + `\(` + jsWSStar +
		`)['"]([^'"]+)['"]`)

// ExtractTsJsImports is extractTsJsImports.
func ExtractTsJsImports(_path string, content string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	// Reset lastIndex in case of reuse; work on a line-capped slice.
	text := capContent(content)
	for _, m := range tsJsImportRE.FindAllStringSubmatch(text, -1) {
		spec := m[1]
		if _, ok := seen[spec]; !ok {
			seen[spec] = struct{}{}
			out = append(out, spec)
		}
	}
	return out
}

var (
	pyFromRE = regexp.MustCompile(
		`\A` + jsWSStar + `from` + jsWSPlus + `([.\w]+)` + jsWSPlus + `import` + jsWSPlus)
	pyImportRE = regexp.MustCompile(
		`\A` + jsWSStar + `import` + jsWSPlus + `([.\w]+(?:` + jsWSStar + `,` + jsWSStar + `[.\w]+)*)`)
	pyAsSplitRE = regexp.MustCompile(jsWSPlus + `as` + jsWSPlus)
)

// ExtractPythonImports is extractPythonImports.
func ExtractPythonImports(_path string, content string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	text := capContent(content)
	for _, m := range execGlobalMultiline(pyFromRE, text) {
		// relative from .x / ..x kept as-is for later path heuristic
		pushUnique(seen, &out, m[1])
	}
	for _, m := range execGlobalMultiline(pyImportRE, text) {
		for _, part := range strings.Split(m[1], ",") {
			// `.split(/\s+as\s+/)[0]` is unreachable in practice (see the
			// package doc) but is ported literally.
			mod := jscompat.Trim(splitFirstJS(pyAsSplitRE, jscompat.Trim(part)))
			if mod != "" {
				pushUnique(seen, &out, mod)
			}
		}
	}
	return out
}

var (
	goImportSingleRE = regexp.MustCompile(
		`\A` + jsWSStar + `import` + jsWSPlus + `"([^"]+)"`)
	goImportBlockRE = regexp.MustCompile(
		`\A` + jsWSStar + `import` + jsWSStar + `\(((?s:.)*?)\)`)
	goImportLineRE = regexp.MustCompile(
		`\A` + jsWSStar + `(?:[_\w.]\w*` + jsWSPlus + `)?"([^"]+)"`)
)

// ExtractGoImports is extractGoImports.
func ExtractGoImports(_path string, content string) []string {
	out := []string{}
	seen := map[string]struct{}{}
	text := capContent(content)
	for _, m := range execGlobalMultiline(goImportSingleRE, text) {
		pushUnique(seen, &out, m[1])
	}
	for _, m := range execGlobalMultiline(goImportBlockRE, text) {
		block := m[1]
		for _, lm := range execGlobalMultiline(goImportLineRE, block) {
			pushUnique(seen, &out, lm[1])
		}
	}
	return out
}

// defaultExtractors is DEFAULT_EXTRACTORS. Rebuilt per call site through
// mergedExtractors so a caller-supplied map can never mutate it.
func defaultExtractors() ImportExtractorMap {
	return ImportExtractorMap{
		".ts":  ExtractTsJsImports,
		".tsx": ExtractTsJsImports,
		".js":  ExtractTsJsImports,
		".jsx": ExtractTsJsImports,
		".mjs": ExtractTsJsImports,
		".cjs": ExtractTsJsImports,
		".py":  ExtractPythonImports,
		".go":  ExtractGoImports,
	}
}

// mergedExtractors is `opts?.extractors ? {...DEFAULT, ...opts.extractors} : DEFAULT`.
// A nil map is TS `undefined`; a non-nil empty map is TS `{}` and takes the
// spread branch (observationally identical, kept for shape fidelity).
func mergedExtractors(override ImportExtractorMap) ImportExtractorMap {
	base := defaultExtractors()
	if override == nil {
		return base
	}
	for k, v := range override {
		base[k] = v
	}
	return base
}

// ---------------------------------------------------------------------------
// ExtractImports — dispatch by extension, then resolve relative/module paths
// against the optional known-file set. Without knownFiles, relative JS paths
// are normalized to a best-effort joined path (no extension probing).
// ---------------------------------------------------------------------------

// ExtractImports is extractImports. opts nil is TS `undefined`.
func ExtractImports(path string, content string, opts *ExtractImportsOptions) []string {
	var override ImportExtractorMap
	var knownFiles []string
	if opts != nil {
		override = opts.Extractors
		knownFiles = opts.KnownFiles
	}
	extractors := mergedExtractors(override)
	ext := extensionOf(path)
	extractor, ok := extractors[ext]
	if !ok || extractor == nil {
		return []string{}
	}
	raw := extractor(path, content)
	var known *jscompat.OrderedMap[string, string]
	if knownFiles != nil {
		known = indexFiles(knownFiles)
	}
	out := []string{}
	seen := map[string]struct{}{}
	for _, spec := range raw {
		resolved, okRes := resolveSpecifier(path, spec, known)
		// JS truthiness: a resolved value of "" counts as UNRESOLVED.
		truthy := okRes && resolved != ""
		if truthy {
			if _, dup := seen[resolved]; !dup {
				seen[resolved] = struct{}{}
				out = append(out, resolved)
			}
			continue
		}
		if known != nil {
			// Keep unresolved specifier so callers/tests can inspect externals
			// via the graph; extractImports itself returns only
			// resolved-or-normalized. When knownFiles is set and resolve
			// fails, skip (graph records external).
			continue
		}
		// No known set: return the specifier as normalized relative path when
		// relative, else the raw specifier.
		fallback, isRel := normalizeRelative(path, spec)
		if !isRel {
			fallback = spec
		}
		if _, dup := seen[fallback]; !dup {
			seen[fallback] = struct{}{}
			out = append(out, fallback)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// BuildImportGraph
// ---------------------------------------------------------------------------

// BuildImportGraph is buildImportGraph. opts nil is the TS default `{}`.
func BuildImportGraph(files []SourceFile, opts *BuildImportGraphOptions) ImportGraph {
	knownList := make([]string, len(files))
	for i, f := range files {
		knownList[i] = f.Path
	}
	known := indexFiles(knownList)
	var override ImportExtractorMap
	if opts != nil {
		override = opts.Extractors
	}
	extractors := mergedExtractors(override)

	deps := jscompat.NewOrderedMap[string, []string]()
	dependents := jscompat.NewOrderedMap[string, []string]()
	external := jscompat.NewOrderedMap[string, []string]()

	for _, f := range files {
		deps.Set(f.Path, []string{})
		dependents.Set(f.Path, []string{})
		external.Set(f.Path, []string{})
	}

	for _, f := range files {
		ext := extensionOf(f.Path)
		extractor, ok := extractors[ext]
		if !ok || extractor == nil {
			continue
		}
		raw := extractor(f.Path, f.Content)
		depSet := newOrderedSet()
		extSet := newOrderedSet()
		for _, spec := range raw {
			resolved, okRes := resolveSpecifier(f.Path, spec, known)
			if okRes && resolved != "" && known.Has(resolved) {
				depSet.add(resolved)
			} else {
				extSet.add(spec)
			}
		}
		depList := sortAsc(depSet.values())
		deps.Set(f.Path, depList)
		external.Set(f.Path, sortAsc(extSet.values()))
		for _, d := range depList {
			if rev, okRev := dependents.Get(d); okRev {
				dependents.Set(d, append(rev, f.Path))
			}
		}
	}

	// Sort reverse edges deterministically.
	for _, e := range dependents.Entries() {
		uniq := newOrderedSet()
		for _, r := range e.Val {
			uniq.add(r)
		}
		dependents.Set(e.Key, sortAsc(uniq.values()))
	}

	return ImportGraph{Deps: deps, Dependents: dependents, External: external}
}

// DependenciesOf is dependenciesOf.
func DependenciesOf(graph ImportGraph, path string) []string {
	out := []string{}
	if graph.Deps != nil {
		if v, ok := graph.Deps.Get(path); ok {
			out = append(out, v...)
		}
	}
	return out
}

// DependentsOf is dependentsOf.
func DependentsOf(graph ImportGraph, path string) []string {
	out := []string{}
	if graph.Dependents != nil {
		if v, ok := graph.Dependents.Get(path); ok {
			out = append(out, v...)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Resolution helpers
// ---------------------------------------------------------------------------

// resolveSpecifier returns (value, true) for a TS string result and ("", false)
// for TS `null`. Note the value can legitimately be "" with ok==true, which is
// FALSY at every TS call site — callers check both.
func resolveSpecifier(
	fromPath string,
	spec string,
	known *jscompat.OrderedMap[string, string],
) (string, bool) {
	if spec == "" {
		return "", false
	}
	ext := extensionOf(fromPath)

	// TypeScript / JavaScript relative
	if isJsLike(ext) && (strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")) {
		joined := joinRelative(dirname(fromPath), spec)
		if known == nil {
			return joined, true
		}
		return resolveJsAgainstKnown(joined, known)
	}

	// Python: module path heuristic → a/b/c.py or a/b/c/__init__.py
	if ext == ".py" {
		return resolvePython(fromPath, spec, known)
	}

	// Go: import path — only resolve when it matches a known file path suffix
	// or exact path (local packages). Package-style imports stay external.
	if ext == ".go" && known != nil {
		if known.Has(spec) {
			v, _ := known.Get(spec)
			return v, true
		}
		// Try spec.go / spec/doc.go style under repo-relative paths.
		if asFile, ok := resolveJsAgainstKnown(spec, known); ok && asFile != "" {
			return asFile, true
		}
		withGoBase := spec
		if !strings.HasSuffix(withGoBase, ".go") {
			withGoBase = spec + ".go"
		}
		if withGo, ok := resolveJsAgainstKnown(withGoBase, known); ok && withGo != "" {
			return withGo, true
		}
	}

	// Absolute / package imports for JS: only hit if exact path is known.
	if known != nil && known.Has(spec) {
		v, _ := known.Get(spec)
		return v, true
	}
	return "", false
}

func resolveJsAgainstKnown(
	base string,
	known *jscompat.OrderedMap[string, string],
) (string, bool) {
	candidates := make([]string, 0, 1+2*len(JS_RESOLVE_EXTENSIONS))
	candidates = append(candidates, base)
	for _, e := range JS_RESOLVE_EXTENSIONS {
		candidates = append(candidates, base+e)
	}
	for _, e := range JS_RESOLVE_EXTENSIONS {
		candidates = append(candidates, base+"/index"+e)
	}
	for _, c := range candidates {
		hit, ok := known.Get(normalizePath(c))
		// TS `if (hit)`: a canonical path of "" is falsy and skipped.
		if ok && hit != "" {
			return hit, true
		}
	}
	return "", false
}

func resolvePython(
	fromPath string,
	spec string,
	known *jscompat.OrderedMap[string, string],
) (string, bool) {
	// Relative: from .foo / ..pkg.mod
	if strings.HasPrefix(spec, ".") {
		dots := 0
		for dots < len(spec) && spec[dots] == '.' {
			dots++
		}
		rest := strings.ReplaceAll(spec[dots:], ".", "/")
		dir := dirname(fromPath)
		for i := 1; i < dots; i++ {
			dir = dirname(dir)
		}
		base := dir
		if rest != "" {
			if dir != "" {
				base = dir + "/" + rest
			} else {
				base = rest
			}
		}
		if known == nil {
			return base, true
		}
		if v, ok := resolveExact(known, base+".py"); ok {
			return v, true
		}
		if v, ok := resolveExact(known, base+"/__init__.py"); ok {
			return v, true
		}
		return "", false
	}
	// Absolute module: foo.bar → foo/bar.py or foo/bar/__init__.py
	base := strings.ReplaceAll(spec, ".", "/")
	if known == nil {
		return base, true
	}
	if v, ok := resolveExact(known, base+".py"); ok {
		return v, true
	}
	if v, ok := resolveExact(known, base+"/__init__.py"); ok {
		return v, true
	}
	return "", false
}

// resolveExact is `known.get(normalizePath(path)) ?? null`: a stored "" is
// NOT nullish, so it round-trips as ("", true) — falsy at the call sites but
// still short-circuiting the `??` chain in resolvePython.
func resolveExact(known *jscompat.OrderedMap[string, string], path string) (string, bool) {
	v, ok := known.Get(normalizePath(path))
	if !ok {
		return "", false
	}
	return v, true
}

// normalizeRelative returns (joined, true) for a relative specifier and
// ("", false) for the TS `null`.
func normalizeRelative(fromPath string, spec string) (string, bool) {
	if !(strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")) {
		return "", false
	}
	return joinRelative(dirname(fromPath), spec), true
}

func joinRelative(dir string, rel string) string {
	var parts []string
	if dir != "" {
		parts = strings.Split(dir, "/")
	}
	parts = append(parts, strings.Split(rel, "/")...)
	stack := []string{}
	for _, p := range parts {
		if p == "" || p == "." {
			continue
		}
		if p == ".." {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		stack = append(stack, p)
	}
	return strings.Join(stack, "/")
}

func dirname(path string) string {
	i := strings.LastIndex(path, "/")
	if i <= 0 {
		return ""
	}
	return path[:i]
}

func extensionOf(path string) string {
	base := path
	if strings.Contains(path, "/") {
		base = path[strings.LastIndex(path, "/")+1:]
	}
	i := strings.LastIndex(base, ".")
	if i <= 0 {
		return ""
	}
	// JS String.prototype.toLowerCase is full Unicode case mapping while
	// strings.ToLower is simple mapping; the pair only disagree on characters
	// (U+0130 &c.) that can never produce one of the eight default extension
	// keys, so the dispatch is unaffected.
	return strings.ToLower(base[i:])
}

func isJsLike(ext string) bool {
	return ext == ".ts" ||
		ext == ".tsx" ||
		ext == ".js" ||
		ext == ".jsx" ||
		ext == ".mjs" ||
		ext == ".cjs"
}

// indexFiles builds a "lowercase-keyed" index → canonical path (first seen
// wins). The TS doc-comment says lowercase; normalizePath does not lowercase.
// Kept as-is.
func indexFiles(files []string) *jscompat.OrderedMap[string, string] {
	m := jscompat.NewOrderedMap[string, string]()
	for _, f := range files {
		key := normalizePath(f)
		if !m.Has(key) {
			m.Set(key, f)
		}
	}
	return m
}

var multiSlashRE = regexp.MustCompile(`/+`)

func normalizePath(p string) string {
	// Collapse duplicate slashes; strip leading ./ (only the first one, like
	// the TS's non-global /^\.\//).
	s := strings.ReplaceAll(p, `\`, "/")
	s = multiSlashRE.ReplaceAllString(s, "/")
	return strings.TrimPrefix(s, "./")
}

func capContent(content string) string {
	// Cheap line cap so pathological files don't dominate.
	lines := 0.0
	end := len(content)
	for i := 0; i < len(content); i++ {
		if content[i] == 10 /* \n */ {
			lines++
			if lines >= MAX_IMPORT_SCAN_LINES {
				end = i
				break
			}
		}
	}
	return content[:end]
}

func pushUnique(seen map[string]struct{}, out *[]string, v string) {
	if _, ok := seen[v]; !ok {
		seen[v] = struct{}{}
		*out = append(*out, v)
	}
}

// splitFirstJS is `s.split(re)[0]` for a regex that cannot match empty.
func splitFirstJS(re *regexp.Regexp, s string) string {
	loc := re.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]]
}

// orderedSet is a JS Set: membership plus insertion-ordered iteration.
type orderedSet struct {
	seen map[string]struct{}
	list []string
}

func newOrderedSet() *orderedSet {
	return &orderedSet{seen: map[string]struct{}{}}
}

func (s *orderedSet) add(v string) {
	if _, ok := s.seen[v]; ok {
		return
	}
	s.seen[v] = struct{}{}
	s.list = append(s.list, v)
}

// values mirrors `Array.from(set)` — always a fresh, non-nil slice so an empty
// result marshals as `[]`, never `null`.
func (s *orderedSet) values() []string {
	out := make([]string, len(s.list))
	copy(out, s.list)
	return out
}

// sortAsc is `.sort(asc)`. SliceStable per the port rules even though a Set's
// contents can never compare equal.
func sortAsc(v []string) []string {
	sort.SliceStable(v, func(i, j int) bool { return asc(v[i], v[j]) < 0 })
	return v
}

// asc is the TS `(a, b) => a < b ? -1 : a > b ? 1 : 0`, i.e. JS string
// relational comparison: UTF-16 code-unit lexicographic order.
func asc(a string, b string) int {
	if a == b {
		return 0
	}
	ua := utf16.Encode([]rune(a))
	ub := utf16.Encode([]rune(b))
	n := len(ua)
	if len(ub) < n {
		n = len(ub)
	}
	for i := 0; i < n; i++ {
		if ua[i] != ub[i] {
			if ua[i] < ub[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(ua) < len(ub):
		return -1
	case len(ua) > len(ub):
		return 1
	default:
		return 0
	}
}
