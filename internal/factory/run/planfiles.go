package run

import (
	"regexp"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// filesHead is a plan's file list: "files:", "## Files", "files to touch:".
// Anything after the colon on that line is the list itself.
var filesHead = regexp.MustCompile(`(?i)^(?:#{1,6}\s+)?(?:planned\s+)?files(?:\s+to\s+(?:touch|change|edit))?\s*(?::\s*(.*))?$`)

// listMark is a markdown bullet or a numbered item.
var listMark = regexp.MustCompile(`^(?:[-*+]|\d+[.)])\s+`)

// pathToken is a repository path, or a bare filename with an extension.
// A match that sits against "://" is a URL and is dropped by the caller.
var pathToken = regexp.MustCompile(`[A-Za-z0-9_.-]+(?:/[A-Za-z0-9_.*?-]+)+|[A-Za-z0-9_.-]+\.[A-Za-z0-9]+`)

// lineSuffix is a trailing :line a claim puts on a path.
var lineSuffix = regexp.MustCompile(`:\d+$`)

// planFiles is the paths a plan said it will touch, in the order it named
// them. A file list in the result is that list. When the result names none,
// the plan's claims are read for paths. Nothing named answers nil.
func planFiles(res factory.StageResult) []string {
	raw := filesFromOutput(res.Output)
	if len(raw) == 0 {
		raw = filesFromClaims(res.Claims)
	}
	return dedupePaths(raw)
}

func filesFromOutput(output string) []string {
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		m := filesHead.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		var out []string
		if len(m) > 1 {
			out = append(out, splitPaths(m[1])...)
		}
		for _, next := range lines[i+1:] {
			trim := strings.TrimSpace(next)
			if trim == "" {
				if len(out) > 0 {
					break
				}
				continue
			}
			if strings.HasPrefix(trim, "#") {
				break
			}
			p := listPath(trim)
			if p == "" {
				break
			}
			out = append(out, p)
		}
		if len(out) > 0 {
			return out
		}
	}
	var out []string
	for _, line := range lines {
		if p := listPath(strings.TrimSpace(line)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func filesFromClaims(claims []factory.Claim) []string {
	var out []string
	for _, cl := range claims {
		out = append(out, pathTokens(cl.Text)...)
		out = append(out, pathTokens(cl.Evidence)...)
	}
	return out
}

func splitPaths(s string) []string {
	var out []string
	for _, part := range strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		if p := cleanPath(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func listPath(line string) string {
	line = listMark.ReplaceAllString(line, "")
	if strings.ContainsAny(line, " \t") {
		return ""
	}
	return cleanPath(line)
}

func pathTokens(s string) []string {
	var out []string
	for _, loc := range pathToken.FindAllStringIndex(s, -1) {
		if loc[0] > 0 {
			prev := s[loc[0]-1]
			if prev == '/' || prev == ':' {
				continue
			}
		}
		if p := cleanPath(s[loc[0]:loc[1]]); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func cleanPath(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "`\"'")
	s = strings.TrimRight(s, ",.;")
	s = lineSuffix.ReplaceAllString(s, "")
	s = strings.TrimPrefix(s, "./")
	if !pathOK(s) {
		return ""
	}
	return s
}

func pathOK(s string) bool {
	if s == "" || s == "." || s == ".." || strings.Contains(s, "://") || strings.ContainsAny(s, " \t") {
		return false
	}
	if strings.Contains(s, "/") {
		return strings.Trim(s, "/") != ""
	}
	dot := strings.LastIndex(s, ".")
	if dot <= 0 || dot == len(s)-1 {
		return false
	}
	ext := s[dot+1:]
	if ext[0] < 'A' || (ext[0] > 'Z' && ext[0] < 'a') || ext[0] > 'z' {
		return false
	}
	return true
}

func dedupePaths(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
