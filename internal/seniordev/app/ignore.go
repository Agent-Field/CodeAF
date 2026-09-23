//go:build !windows

package app

import (
	"bufio"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// A .gitignore reader for the snapshot recorder. Under the git recorder this
// file is dead weight: git answers "what belongs to the tree" itself. Without
// git something has to, and answering it wrongly is not a cosmetic bug -- an
// over-broad ignore silently drops the model's work out of the answer, and an
// under-broad one sweeps build output into it.
//
// WHAT IT IMPLEMENTS: blank lines and #comments; a leading ! negation; a
// trailing / restricting a pattern to directories; a leading or embedded /
// anchoring a pattern to the file it came from; *, ? and ** globs; per-
// directory .gitignore files, where a deeper file's rules win over a shallower
// one's, and a later rule in one file wins over an earlier one.
//
// WHAT IT DOES NOT: .git/info/exclude, core.excludesFile, .gitattributes,
// nested repositories, or character classes. Those are real gitignore features
// this deliberately skips. `senior-dev run` without --in-place uses git and is
// unaffected; the limits are documented in ARCHITECTURE.md so an in-place run
// on a repository that leans on them is a known, visible gap rather than a
// surprise.
type ignoreRules struct {
	// byDir maps a directory (slash-separated, relative to the workspace, ""
	// for the root) to the rules its own .gitignore declared.
	byDir map[string][]ignoreRule
}

type ignoreRule struct {
	pattern  *regexp.Regexp
	negate   bool
	dirOnly  bool
	anchored bool
	source   string // the directory the rule came from
}

func newIgnoreRules() *ignoreRules {
	return &ignoreRules{byDir: map[string][]ignoreRule{}}
}

// load reads the .gitignore in one directory, if it has one. dir is relative
// to the workspace, slash-separated, "" at the root.
func (rules *ignoreRules) load(workspace, dir string) {
	name := filepath.Join(workspace, filepath.FromSlash(dir), ".gitignore")
	file, err := os.Open(name)
	if err != nil {
		return
	}
	defer file.Close()
	var parsed []ignoreRule
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if rule, ok := parseIgnoreLine(scanner.Text(), dir); ok {
			parsed = append(parsed, rule)
		}
	}
	if len(parsed) > 0 {
		rules.byDir[dir] = parsed
	}
}

func parseIgnoreLine(line, dir string) (ignoreRule, bool) {
	trimmed := strings.TrimRight(line, " \t")
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return ignoreRule{}, false
	}
	rule := ignoreRule{source: dir}
	if strings.HasPrefix(trimmed, "!") {
		rule.negate = true
		trimmed = trimmed[1:]
	}
	if strings.HasSuffix(trimmed, "/") {
		rule.dirOnly = true
		trimmed = strings.TrimSuffix(trimmed, "/")
	}
	if trimmed == "" {
		return ignoreRule{}, false
	}
	// A pattern containing a slash anywhere but at its end is anchored to the
	// directory its .gitignore sits in; one without is matched against every
	// path component below that directory.
	rule.anchored = strings.Contains(trimmed, "/")
	trimmed = strings.TrimPrefix(trimmed, "/")
	rule.pattern = compileIgnoreGlob(trimmed)
	return rule, rule.pattern != nil
}

// compileIgnoreGlob turns a gitignore glob into an anchored regexp. ** spans
// separators, * and ? do not.
func compileIgnoreGlob(glob string) *regexp.Regexp {
	var builder strings.Builder
	builder.WriteString("^")
	for index := 0; index < len(glob); index++ {
		switch glob[index] {
		case '*':
			if index+1 < len(glob) && glob[index+1] == '*' {
				builder.WriteString(".*")
				index++
				// A trailing separator after ** is optional, so "a/**" matches
				// "a" as well as "a/b".
				if index+1 < len(glob) && glob[index+1] == '/' {
					index++
				}
				continue
			}
			builder.WriteString("[^/]*")
		case '?':
			builder.WriteString("[^/]")
		default:
			builder.WriteString(regexp.QuoteMeta(string(glob[index])))
		}
	}
	builder.WriteString("$")
	compiled, err := regexp.Compile(builder.String())
	if err != nil {
		return nil
	}
	return compiled
}

// ignored reports whether a path is excluded. relative is slash-separated and
// relative to the workspace. The deepest .gitignore that has an opinion wins,
// and within one file the last matching rule wins -- which is what makes a
// negation able to rescue a path an earlier rule excluded.
func (rules *ignoreRules) ignored(relative string, isDir bool) bool {
	decided, excluded := false, false
	// Shallowest first, so a deeper directory's rules overwrite the decision.
	for _, dir := range ancestorDirs(relative) {
		for _, rule := range rules.byDir[dir] {
			if rule.dirOnly && !isDir {
				continue
			}
			if rule.matches(relative, dir) {
				decided, excluded = true, !rule.negate
			}
		}
	}
	if !decided {
		return false
	}
	return excluded
}

func (rule ignoreRule) matches(relative, dir string) bool {
	within := relative
	if dir != "" {
		within = strings.TrimPrefix(relative, dir+"/")
		if within == relative {
			return false
		}
	}
	if rule.anchored {
		return rule.pattern.MatchString(within)
	}
	// Unanchored: the pattern applies to any component, and to any directory
	// prefix, so "build" excludes "build" and everything under it.
	for {
		if rule.pattern.MatchString(within) {
			return true
		}
		parent := path.Dir(within)
		if parent == "." || parent == within {
			return false
		}
		within = parent
	}
}

// ancestorDirs lists the directories whose .gitignore can speak about a path,
// shallowest first: "", then each parent, excluding the path itself.
func ancestorDirs(relative string) []string {
	dirs := []string{""}
	parent := path.Dir(relative)
	if parent == "." || parent == "/" {
		return dirs
	}
	parts := strings.Split(parent, "/")
	current := ""
	for _, part := range parts {
		if current == "" {
			current = part
		} else {
			current += "/" + part
		}
		dirs = append(dirs, current)
	}
	return dirs
}
