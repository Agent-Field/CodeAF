// Package orientation builds a compact repo digest that a leaf's first prompt
// carries so orientation costs zero turns: a bounded directory tree, code-file
// declaration/signature outlines (NOT bodies), test-file names, and — when the
// run already knows it — the failing-test summary.
//
// The digest is assembled ONCE per run and shared between the linear worker
// (internal/exec) and the swe coder (internal/swepro/codeaf). Both sides call
// BuildDigest with the leaf workspace (known before dispatch) and an optional
// failing-test list. The result is a stable Markdown string cached per
// workspace path so repeated calls return byte-identical output — this keeps
// the provider prefix cache intact when the system prompt is rebuilt every
// turn.
//
// aforge-embed: D10 — this package is aforge-owned, not vendored from
// swe-pro-go. It lives under internal/swepro/ (not internal/swepro/internal/)
// so Go's internal rule lets both the aforge tree (internal/exec) and the
// swepro tree (internal/swepro/codeaf) import it. It has no upstream
// counterpart; a harvest has nothing to re-apply here.
package orientation

import (
	"bufio"
	"fmt"

	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// MaxBytes bounds the rendered digest. A repo that exceeds this degrades
// gracefully: the directory tree is truncated first, then code outlines are
// dropped, leaving the top-level tree and the README head. The bound is in
// bytes (not UTF-16 code units) because the digest is Markdown read by the
// model, and a model's token count tracks bytes more closely than JS string
// length. 8000 bytes ≈ 2000 tokens, which is the budget a pi-harness leaf
// spends on orientation total.
const MaxBytes = 8000

// MaxTreeEntries bounds the directory tree listing. Past this the tree stops
// and a truncation note names how many entries were elided. 300 entries is
// enough for a mid-size monorepo's top packages without flooding the digest
// with node_modules-scale noise.
const MaxTreeEntries = 300

// MaxOutlineFiles bounds how many code files get declaration outlines. Files
// are prioritised by path depth (shallower first) then alphabetically, so a
// huge repo keeps the outlines for its entry points and drops the leaf files.
const MaxOutlineFiles = 40

// MaxOutlineLinesPerFile bounds the declarations shown for one file. A file
// with more declarations than this is truncated with a count of the elided
// ones. 25 lines covers the public surface of most files.
const MaxOutlineLinesPerFile = 25

// MaxReadmeBytes bounds the README excerpt shown when the digest degrades.
const MaxReadmeBytes = 1500

// codeExtensions are file extensions whose declarations we outline. Bodies are
// never read — only the declaration/signature line is extracted, by a
// per-language rule below. A file not in this set is still listed in the tree
// but gets no outline section.
var codeExtensions = map[string]bool{
	".go":    true,
	".ts":    true,
	".tsx":   true,
	".js":    true,
	".jsx":   true,
	".py":    true,
	".rs":    true,
	".rb":    true,
	".java":  true,
	".kt":    true,
	".swift": true,
	".c":     true,
	".h":     true,
	".cpp":   true,
	".cc":    true,
	".hpp":   true,
}

// testPatterns recognises test files by name convention. The convention is
// language-agnostic: a file whose stem ends in _test (Go), .test (JS/TS), or
// starts with test_ (Python) is a test file. This is the same heuristic a
// developer uses, and it needs no project-specific configuration.
func isTestFile(name string) bool {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	lower := strings.ToLower(stem)
	return strings.HasSuffix(lower, "_test") ||
		strings.HasSuffix(lower, ".test") ||
		strings.HasSuffix(lower, ".spec") ||
		strings.HasPrefix(lower, "test_") ||
		strings.HasPrefix(lower, "test")
}

// skipDirs are directories never walked into. They are either VCS metadata,
// dependency caches, build output, or harness machinery — none of which
// orient a coder toward the task.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "__pycache__": true,
	".codeaf": true, ".plandb": true, ".aforge": true, ".obs": true,
	"dist": true, "build": true, "target": true, ".next": true,
	"bin": true, "obj": true, ".gradle": true, ".idea": true, ".vscode": true,
}

// digestCache memoises digests per workspace path so repeated calls (the swe
// coder rebuilds its system prompt every turn) return byte-identical output
// and do not break the provider prefix cache. The cache is process-scoped: a
// workspace's tree is read once and reused for the run. A fresh process reads
// it again, which is correct — the tree may have changed between runs.
var digestCache sync.Map

// cachedDigest holds a pre-built digest and the failing-test summary it was
// built with. A second call with a different failing-test list rebuilds, but
// the common case (same workspace, same baseline) hits the cache.
type cachedDigest struct {
	digest     string
	failingKey string
}

// BuildDigest returns a bounded Markdown orientation digest for the workspace
// at root. failingTests, when non-empty, is rendered as a pre-run baseline so
// the leaf knows which tests were already red. The result is cached per root
// path + failing-test signature.
func BuildDigest(root string, failingTests []string) string {
	failingKey := strings.Join(failingTests, "\n")
	key := root + "\x00" + failingKey
	if entry, ok := digestCache.Load(key); ok {
		return entry.(cachedDigest).digest
	}
	digest := assembleDigest(root, failingTests)
	digestCache.Store(key, cachedDigest{digest: digest, failingKey: failingKey})
	return digest
}

// ClearCache invalidates cached digests. Tests call this between scenarios.
func ClearCache() {
	digestCache = sync.Map{}
}

func assembleDigest(root string, failingTests []string) string {
	entries, treeText, truncated := walkTree(root)
	outlines, outlineFiles := buildOutlines(root, entries)
	testFiles := collectTestFiles(entries)
	readmeText := readReadme(root)

	var b strings.Builder
	b.WriteString("## Repository orientation (precomputed — do not re-derive)\n\n")

	// Directory tree
	b.WriteString("### Directory tree\n")
	b.WriteString(treeText)
	if truncated > 0 {
		fmt.Fprintf(&b, "\n… (%d more entries elided)\n", truncated)
	}
	b.WriteString("\n")

	// Code outlines — dropped first when the bound is tight
	if len(outlineFiles) > 0 && b.Len()+estimateOutlines(outlines) <= MaxBytes {
		b.WriteString("### Code outlines (declarations only, not bodies)\n")
		for _, out := range outlines {
			b.WriteString(out)
			b.WriteString("\n")
		}
	}

	// Test files
	if len(testFiles) > 0 {
		b.WriteString("### Test files\n")
		for _, f := range testFiles {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	// Failing tests
	if len(failingTests) > 0 {
		b.WriteString("### Failing tests (pre-run baseline — already red before your work)\n")
		for _, f := range failingTests {
			fmt.Fprintf(&b, "- %s\n", f)
		}
		b.WriteString("\n")
	}

	// If the digest is still huge, degrade: keep the tree head and README
	if b.Len() > MaxBytes {
		return degradeDigest(root, treeText, readmeText)
	}

	return b.String()
}

// fileEntry is one file in the walked tree, relative to root.
type fileEntry struct {
	relPath string
	depth   int
	isDir   bool
}

// walkTree does a breadth-first walk of the directory tree, skipping noise
// directories and bounding the entry count. It returns the full entry list
// (for outline/test extraction), the rendered tree text, and the count of
// elided entries.
func walkTree(root string) ([]fileEntry, string, int) {
	var entries []fileEntry
	var tree strings.Builder
	count := 0
	elided := 0

	// BFS so shallow files appear before deep ones; this keeps entry points
	// visible when the bound truncates.
	queue := []struct {
		path  string
		depth int
	}{{root, 0}}

	for len(queue) > 0 {
		front := queue[0]
		queue = queue[1:]
		dirPath := front.path
		depth := front.depth

		dirEntries, err := os.ReadDir(dirPath)
		if err != nil {
			continue
		}
		sort.Slice(dirEntries, func(i, j int) bool {
			return dirEntries[i].Name() < dirEntries[j].Name()
		})
		for _, de := range dirEntries {
			name := de.Name()
			if de.IsDir() && skipDirs[name] {
				continue
			}
			fullPath := filepath.Join(dirPath, name)
			rel, err := filepath.Rel(root, fullPath)
			if err != nil {
				rel = name
			}
			if count >= MaxTreeEntries {
				elided++
				entries = append(entries, fileEntry{relPath: rel, depth: depth, isDir: de.IsDir()})
				continue
			}
			count++
			entries = append(entries, fileEntry{relPath: rel, depth: depth, isDir: de.IsDir()})
			indent := strings.Repeat("  ", depth)
			if de.IsDir() {
				tree.WriteString(indent + name + "/\n")
				queue = append(queue, struct {
					path  string
					depth int
				}{fullPath, depth + 1})
			} else {
				tree.WriteString(indent + name + "\n")
			}
		}
	}
	return entries, tree.String(), elided
}

// buildOutlines extracts declaration lines from code files. Files are
// prioritised by depth (shallower first) then alphabetically, and the count
// is bounded by MaxOutlineFiles.
func buildOutlines(root string, entries []fileEntry) ([]string, []fileEntry) {
	var codeFiles []fileEntry
	for _, e := range entries {
		if e.isDir {
			continue
		}
		ext := filepath.Ext(e.relPath)
		if !codeExtensions[ext] {
			continue
		}
		codeFiles = append(codeFiles, e)
	}
	// Shallower files first, then alphabetical — entry points before leaves.
	sort.Slice(codeFiles, func(i, j int) bool {
		if codeFiles[i].depth != codeFiles[j].depth {
			return codeFiles[i].depth < codeFiles[j].depth
		}
		return codeFiles[i].relPath < codeFiles[j].relPath
	})
	if len(codeFiles) > MaxOutlineFiles {
		codeFiles = codeFiles[:MaxOutlineFiles]
	}

	var outlines []string
	for _, e := range codeFiles {
		full := filepath.Join(root, e.relPath)
		lines := extractDeclarations(full, filepath.Ext(e.relPath))
		if len(lines) == 0 {
			continue
		}
		var sb strings.Builder
		sb.WriteString(e.relPath + ":\n")
		shown := lines
		if len(shown) > MaxOutlineLinesPerFile {
			shown = shown[:MaxOutlineLinesPerFile]
		}
		for _, line := range shown {
			sb.WriteString("  " + line + "\n")
		}
		if len(lines) > MaxOutlineLinesPerFile {
			fmt.Fprintf(&sb, "  … (%d more declarations)\n", len(lines)-MaxOutlineLinesPerFile)
		}
		outlines = append(outlines, sb.String())
	}
	return outlines, codeFiles
}

func estimateOutlines(outlines []string) int {
	total := 0
	for _, o := range outlines {
		total += len(o)
	}
	return total
}

func collectTestFiles(entries []fileEntry) []string {
	var tests []string
	for _, e := range entries {
		if e.isDir {
			continue
		}
		if isTestFile(filepath.Base(e.relPath)) {
			tests = append(tests, e.relPath)
		}
	}
	sort.Strings(tests)
	return tests
}

func readReadme(root string) string {
	for _, name := range []string{"README.md", "README", "README.rst", "README.txt", "readme.md"} {
		path := filepath.Join(root, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		text := string(data)
		if len(text) > MaxReadmeBytes {
			text = text[:MaxReadmeBytes] + "\n…"
		}
		return text
	}
	return ""
}

// degradeDigest is the graceful fallback for huge repos: top-level tree +
// README head, nothing else. It keeps the leaf oriented toward the project's
// front door even when the full digest would blow the budget.
func degradeDigest(root, treeText, readmeText string) string {
	var b strings.Builder
	b.WriteString("## Repository orientation (truncated — large repo)\n\n")
	// Keep only the top-level entries (depth 0) from the tree.
	for _, line := range strings.Split(treeText, "\n") {
		if line == "" || strings.HasPrefix(line, "  ") {
			continue
		}
		b.WriteString(line + "\n")
	}
	if readmeText != "" {
		b.WriteString("\n### README (head)\n")
		b.WriteString(readmeText)
		b.WriteString("\n")
	}
	return b.String()
}

// extractDeclarations reads a code file and returns the declaration/signature
// lines — never the bodies. The rule is per-extension and intentionally
// simple: a line that starts a top-level declaration (func, type, const, var
// for Go; export/function/class/interface/const for JS/TS; def/class for
// Python; etc.) is a declaration line. The rest is body and is skipped.
func extractDeclarations(path, ext string) []string {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var decls []string
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), " \t\r")
		if decl := declarationLine(ext, line); decl != "" {
			decls = append(decls, decl)
		}
	}
	return decls
}

// declarationLine returns the trimmed declaration line if the given line
// starts a top-level declaration for the given language, or "" otherwise.
// Bodies, comments, and blank lines return "". The match is on the line
// prefix after leading whitespace; a declaration's signature is kept, its
// opening brace and everything after it are dropped.
func declarationLine(ext, line string) string {
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") || strings.HasPrefix(trimmed, "*") {
		return ""
	}
	switch ext {
	case ".go":
		return goDeclaration(trimmed)
	case ".ts", ".tsx", ".js", ".jsx":
		return jsDeclaration(trimmed)
	case ".py":
		return pyDeclaration(trimmed)
	case ".rs":
		return rsDeclaration(trimmed)
	case ".rb":
		return rbDeclaration(trimmed)
	case ".java", ".kt", ".swift", ".c", ".h", ".cpp", ".cc", ".hpp":
		return cLikeDeclaration(trimmed)
	default:
		return ""
	}
}

// goDeclaration matches Go top-level declarations: func, type, const, var,
// struct, interface. The signature up to the opening brace or paren-count
// balanced close is kept; the body is dropped.
func goDeclaration(line string) string {
	for _, kw := range []string{"func ", "type ", "const ", "var "} {
		if strings.HasPrefix(line, kw) {
			return stripBody(line, '{')
		}
	}
	return ""
}

// jsDeclaration matches JS/TS top-level declarations: export, function,
// class, interface, const, let, type, enum. The signature up to the opening
// brace is kept.
func jsDeclaration(line string) string {
	stripped := line
	if strings.HasPrefix(stripped, "export ") {
		stripped = strings.TrimPrefix(stripped, "export ")
	}
	for _, kw := range []string{"function ", "class ", "interface ", "type ", "enum ", "const ", "let "} {
		if strings.HasPrefix(stripped, kw) {
			return stripBody(line, '{')
		}
	}
	return ""
}

func pyDeclaration(line string) string {
	for _, kw := range []string{"def ", "class ", "async def "} {
		if strings.HasPrefix(line, kw) {
			return stripBody(line, ':')
		}
	}
	return ""
}

func rsDeclaration(line string) string {
	for _, kw := range []string{"fn ", "struct ", "enum ", "trait ", "impl ", "const ", "static ", "mod ", "pub fn ", "pub struct ", "pub enum ", "pub trait ", "pub const ", "pub static ", "pub mod "} {
		if strings.HasPrefix(line, kw) {
			return stripBody(line, '{')
		}
	}
	return ""
}

func rbDeclaration(line string) string {
	for _, kw := range []string{"def ", "class ", "module "} {
		if strings.HasPrefix(line, kw) {
			return line
		}
	}
	return ""
}

// cLikeDeclaration matches C/C++/Java/Kotlin/Swift top-level declarations:
// function signatures, class/struct/enum/interface, and top-level const.
// The signature up to the opening brace is kept.
func cLikeDeclaration(line string) string {
	for _, kw := range []string{"func ", "fun ", "function ", "class ", "struct ", "enum ", "interface ", "typedef ", "public ", "private ", "protected ", "internal ", "static "} {
		if strings.HasPrefix(line, kw) {
			return stripBody(line, '{')
		}
	}
	// A line ending in ); or ) at top level is a function declaration.
	if strings.HasSuffix(strings.TrimRight(line, " "), ");") || (strings.Contains(line, "(") && strings.HasSuffix(strings.TrimRight(line, " "), ")")) {
		return line
	}
	return ""
}

// stripBody keeps the declaration signature and drops everything from the
// opening delimiter onward. For Go/JS/Rust the opener is '{'; for Python it
// is ':'. A line like "func Parse(s string) (Node, error) {" becomes
// "func Parse(s string) (Node, error)".
func stripBody(line string, opener byte) string {
	idx := strings.IndexByte(line, opener)
	if idx < 0 {
		return strings.TrimRight(line, " \t")
	}
	return strings.TrimRight(line[:idx], " \t")
}
