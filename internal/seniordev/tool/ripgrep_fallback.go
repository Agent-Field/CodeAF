//go:build !windows

// Ripgrep is the one external binary the search tools depend on, and it is
// never fetched at run time: a sealed or offline deployment could not download
// it anyway.
//
// A missing rg is not a cosmetic loss. grep and glob are how the agent reads a
// codebase, so without them a run does not degrade gracefully — it fails tool
// call after tool call and never gets to the work. This file keeps the
// existing ripgrepRunner seam and answers in process instead, so the engine is
// self-contained. rg stays authoritative whenever it is installed: the
// fallback is selected only when exec.LookPath("rg") misses, which means an
// existing deployment's behaviour is untouched.
//
// Fidelity notes (the deliberate gaps, so nobody has to rediscover them):
//   - Inside a git work tree the file list comes from `git ls-files --cached
//     --others --exclude-standard`, which reproduces rg's default .gitignore
//     behaviour exactly. Outside one, every regular file is walked: rg would
//     also honour .ignore/.rgignore files there, and this does not.
//   - Patterns are compiled with Go's regexp (RE2), the same family as rg's
//     default engine, so ordinary patterns behave the same. Anything relying on
//     Rust-regex-only syntax will not compile here.
//   - Results are emitted in lexicographic order. rg emits in traversal order;
//     both are arbitrary as far as the callers are concerned, and a stable
//     order makes the 100-result cap deterministic instead of filesystem
//     dependent.
package tool

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// binarySniffBytes mirrors rg's habit of skipping binary files: a NUL byte in
// the first chunk is the signal.
const binarySniffBytes = 8192

var warnMissingRipgrep sync.Once

// pickRipgrepRunner returns the real rg whenever it is on PATH and the
// in-process searcher otherwise. Resolution happens per Registry rather than
// once per process so a deployment that installs rg mid-flight picks it up.
func pickRipgrepRunner() ripgrepRunner {
	if _, err := exec.LookPath("rg"); err == nil {
		return execRipgrepRunner{}
	}
	warnMissingRipgrep.Do(func() {
		log.Printf("search: ripgrep (rg) is not on PATH — using the built-in searcher. " +
			"Install ripgrep for faster search and exact ignore-file semantics.")
	})
	return builtinSearchRunner{}
}

// builtinSearchRunner answers the two invocation shapes the tools construct:
// `--files` (glob.go) and `--json` (grep.go).
type builtinSearchRunner struct{}

type searchRequest struct {
	listFiles bool
	includes  []string
	excludes  []string
	pattern   string
	roots     []string
}

func (builtinSearchRunner) Run(ctx context.Context, cwd string, args []string) (ripgrepResult, error) {
	request := parseSearchArgs(args)
	filter, err := newGlobFilter(request.includes, request.excludes)
	if err != nil {
		return ripgrepResult{}, err
	}
	files, degraded := collectSearchFiles(ctx, cwd, request.roots, filter)
	if ctx.Err() != nil {
		return ripgrepResult{}, ctx.Err()
	}
	if request.listFiles {
		return listFilesOutput(files), nil
	}
	return grepOutput(ctx, cwd, request.pattern, files, degraded)
}

// parseSearchArgs reads the flags the callers actually pass. Unmodelled flags
// are ignored rather than rejected: every flag in play (--no-config, --hidden,
// --no-messages) only widens or quiets the search, so ignoring one can never
// turn a narrow search into a broad one.
func parseSearchArgs(args []string) searchRequest {
	var request searchRequest
	positional := []string{}
	index := 0
	for ; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			index++
			break
		}
		switch {
		case argument == "--files":
			request.listFiles = true
		case strings.HasPrefix(argument, "--glob="):
			glob := strings.TrimPrefix(argument, "--glob=")
			if strings.HasPrefix(glob, "!") {
				request.excludes = append(request.excludes, strings.TrimPrefix(glob, "!"))
				continue
			}
			request.includes = append(request.includes, glob)
		case strings.HasPrefix(argument, "-"):
		default:
			positional = append(positional, argument)
		}
	}
	positional = append(positional, args[index:]...)
	if !request.listFiles && len(positional) > 0 {
		request.pattern = positional[0]
		positional = positional[1:]
	}
	request.roots = positional
	if len(request.roots) == 0 {
		request.roots = []string{"."}
	}
	return request
}

// collectSearchFiles resolves every root to a de-duplicated, filtered, sorted
// list of paths relative to cwd. degraded reports whether anything was skipped
// because it could not be read — the caller turns that into rg's exit code 2.
func collectSearchFiles(
	ctx context.Context,
	cwd string,
	roots []string,
	filter *globFilter,
) (files []string, degraded bool) {
	seen := map[string]bool{}
	add := func(relative string) {
		relative = path.Clean(filepath.ToSlash(relative))
		if relative == "." || relative == "" || relative == "/" || seen[relative] {
			return
		}
		// .git is skipped unconditionally, not just via the caller's
		// `--glob=!.git/*`: leaking object files into a code search is never
		// what the caller wanted, whatever globs they passed.
		if hasGitSegment(relative) || !filter.match(relative) {
			return
		}
		seen[relative] = true
		files = append(files, relative)
	}

	for _, root := range roots {
		if ctx.Err() != nil {
			break
		}
		cleaned := filepath.Clean(root)
		absolute := cleaned
		if !filepath.IsAbs(absolute) {
			absolute = filepath.Join(cwd, cleaned)
		}
		info, err := os.Stat(absolute)
		if err != nil {
			degraded = true
			continue
		}
		if !info.IsDir() {
			add(cleaned)
			continue
		}
		listed, ok := gitTrackedFiles(ctx, absolute)
		if !ok {
			var walkFailed bool
			listed, walkFailed = walkRegularFiles(ctx, absolute)
			degraded = degraded || walkFailed
		}
		for _, relative := range listed {
			joined := relative
			if cleaned != "." {
				joined = path.Join(filepath.ToSlash(cleaned), relative)
			}
			add(joined)
		}
	}
	sort.Strings(files)
	return files, degraded
}

// gitTrackedFiles asks git for the working-tree file list, which is what makes
// the fallback honour .gitignore for free. ok is false whenever dir is not a
// work tree (or git is unavailable), leaving the caller to walk instead.
func gitTrackedFiles(ctx context.Context, dir string) (files []string, ok bool) {
	command := exec.CommandContext(
		ctx, "git", "-C", dir, "ls-files", "-z", "--cached", "--others", "--exclude-standard",
	)
	var stdout bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = nil
	if err := command.Run(); err != nil {
		return nil, false
	}
	for _, entry := range strings.Split(stdout.String(), "\x00") {
		if entry == "" {
			continue
		}
		// `--cached` also reports files deleted from the working tree, and it
		// reports symlinks that rg would not follow. Lstat screens out both:
		// only a regular file that is actually present is searchable.
		if info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(entry))); err != nil ||
			!info.Mode().IsRegular() {
			continue
		}
		files = append(files, entry)
	}
	return files, true
}

// walkRegularFiles is the non-git path. Symlinks are left out to match rg,
// which does not follow them by default, and the walk aborts on cancellation
// so a huge tree cannot outlive the request that asked for it.
func walkRegularFiles(ctx context.Context, dir string) (files []string, failed bool) {
	_ = filepath.WalkDir(dir, func(current string, entry fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			failed = true
			if entry != nil && entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		relative, relErr := filepath.Rel(dir, current)
		if relErr != nil {
			failed = true
			return nil
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	return files, failed
}

func hasGitSegment(relative string) bool {
	for _, segment := range strings.Split(relative, "/") {
		if segment == ".git" {
			return true
		}
	}
	return false
}

// listFilesOutput reproduces `rg --files`: one path per line, exit 1 when
// nothing matched. It never reports exit 2 — glob.go treats anything other
// than 0 or 1 as a hard error.
func listFilesOutput(files []string) ripgrepResult {
	if len(files) == 0 {
		return ripgrepResult{code: 1}
	}
	var stdout bytes.Buffer
	for _, file := range files {
		stdout.WriteString(file)
		stdout.WriteByte('\n')
	}
	return ripgrepResult{stdout: stdout.Bytes(), code: 0}
}

// grepOutput reproduces `rg --json`, emitting only the "match" events grep.go
// consumes. Exit codes follow rg: 0 matched, 1 matched nothing, 2 finished but
// skipped something unreadable.
func grepOutput(
	ctx context.Context,
	cwd string,
	pattern string,
	files []string,
	degraded bool,
) (ripgrepResult, error) {
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return ripgrepResult{}, err
	}
	var stdout bytes.Buffer
	encoder := json.NewEncoder(&stdout)
	matched := false
	for _, relative := range files {
		if ctx.Err() != nil {
			return ripgrepResult{}, ctx.Err()
		}
		fileMatched, readable := grepFile(
			filepath.Join(cwd, filepath.FromSlash(relative)), relative, expression, encoder,
		)
		if !readable {
			degraded = true
			continue
		}
		matched = matched || fileMatched
	}
	code := 1
	switch {
	case degraded:
		code = 2
	case matched:
		code = 0
	}
	return ripgrepResult{stdout: stdout.Bytes(), code: code}, nil
}

func grepFile(
	absolute string,
	relative string,
	expression *regexp.Regexp,
	encoder *json.Encoder,
) (matched bool, readable bool) {
	file, err := os.Open(absolute)
	if err != nil {
		return false, false
	}
	defer func() { _ = file.Close() }()

	reader := bufio.NewReaderSize(file, 64*1024)
	if head, _ := reader.Peek(binarySniffBytes); bytes.IndexByte(head, 0) >= 0 {
		// Binary: readable, just not searched — same as rg, and not a reason
		// to report the run as degraded.
		return false, true
	}

	for number := 1; ; number++ {
		line, readErr := reader.ReadString('\n')
		if line == "" && readErr != nil {
			return matched, true
		}
		if expression.MatchString(strings.TrimSuffix(line, "\n")) {
			matched = true
			event := ripgrepJSONLine{Type: "match"}
			event.Data.Path.Text = relative
			event.Data.Lines.Text = line
			event.Data.LineNumber = number
			if encodeErr := encoder.Encode(event); encodeErr != nil {
				return matched, true
			}
		}
		if readErr != nil {
			return matched, true
		}
	}
}

// globFilter applies rg's --glob rules: an exclude wins outright, and when any
// include is present a path must match at least one of them.
type globFilter struct {
	includes []compiledGlob
	excludes []compiledGlob
}

type compiledGlob struct {
	expression *regexp.Regexp
	baseOnly   bool
}

func newGlobFilter(includes, excludes []string) (*globFilter, error) {
	filter := &globFilter{}
	for _, pattern := range includes {
		compiled, err := compileGlob(pattern)
		if err != nil {
			return nil, err
		}
		filter.includes = append(filter.includes, compiled)
	}
	for _, pattern := range excludes {
		compiled, err := compileGlob(pattern)
		if err != nil {
			return nil, err
		}
		filter.excludes = append(filter.excludes, compiled)
	}
	return filter, nil
}

func (f *globFilter) match(relative string) bool {
	for _, exclude := range f.excludes {
		if exclude.matches(relative) {
			return false
		}
	}
	if len(f.includes) == 0 {
		return true
	}
	for _, include := range f.includes {
		if include.matches(relative) {
			return true
		}
	}
	return false
}

func (g compiledGlob) matches(relative string) bool {
	if g.baseOnly {
		return g.expression.MatchString(path.Base(relative))
	}
	if g.expression.MatchString(relative) {
		return true
	}
	// A path-bearing pattern also covers everything beneath a directory it
	// matches, which is what makes `!.git/*` exclude .git/refs/heads/main and
	// not merely .git/config.
	for parent := path.Dir(relative); parent != "." && parent != "/" && parent != ""; parent = path.Dir(parent) {
		if g.expression.MatchString(parent) {
			return true
		}
	}
	return false
}

// compileGlob translates a gitignore-style glob into a regexp. A pattern with
// no separator matches the basename at any depth ("*.go"); one with a
// separator is anchored at the search root (".git/*").
func compileGlob(pattern string) (compiledGlob, error) {
	trimmed := strings.TrimSuffix(pattern, "/")
	expression, err := globToRegexp(trimmed)
	if err != nil {
		return compiledGlob{}, err
	}
	return compiledGlob{expression: expression, baseOnly: !strings.Contains(trimmed, "/")}, nil
}

func globToRegexp(pattern string) (*regexp.Regexp, error) {
	runes := []rune(pattern)
	var builder strings.Builder
	builder.WriteString(`\A`)
	for index := 0; index < len(runes); index++ {
		switch character := runes[index]; character {
		case '*':
			if index+1 < len(runes) && runes[index+1] == '*' {
				index++
				if index+1 < len(runes) && runes[index+1] == '/' {
					index++
					builder.WriteString(`(?:[^/]*/)*`)
					continue
				}
				builder.WriteString(`.*`)
				continue
			}
			builder.WriteString(`[^/]*`)
		case '?':
			builder.WriteString(`[^/]`)
		case '[':
			closing := indexRune(runes[index:], ']')
			if closing < 0 {
				builder.WriteString(regexp.QuoteMeta("["))
				continue
			}
			builder.WriteString(string(runes[index : index+closing+1]))
			index += closing
		default:
			builder.WriteString(regexp.QuoteMeta(string(character)))
		}
	}
	builder.WriteString(`\z`)
	return regexp.Compile(builder.String())
}

func indexRune(runes []rune, target rune) int {
	for index, current := range runes {
		if current == target {
			return index
		}
	}
	return -1
}
