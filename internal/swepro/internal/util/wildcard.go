// Wildcard matching — port of src/util/wildcard.ts:3-57
// (swe-pro 3b25a1a). Matching operates on UTF-16 code units because the TS
// regular expressions do not use the Unicode flag.
package util

import (
	"runtime"
	"sort"
	"strings"
	"unicode/utf16"
)

// StructuredInput is the head/tail form consumed by AllStructured.
type StructuredInput struct {
	Head string   `json:"head"`
	Tail []string `json:"tail"`
}

// Match applies the source's two wildcard tokens: * is any sequence and ? is
// one UTF-16 code unit. Backslashes normalize to slashes first.
func Match(value, pattern string) bool {
	return matchForPlatform(value, pattern, runtime.GOOS)
}

func matchForPlatform(value, pattern, goos string) bool {
	value = strings.ReplaceAll(value, `\`, "/")
	pattern = strings.ReplaceAll(pattern, `\`, "/")
	optionalTail := strings.HasSuffix(pattern, " *")
	if optionalTail {
		pattern = strings.TrimSuffix(pattern, " *")
		if wildcardMatch(value, pattern, goos == "windows") {
			return true
		}
		pattern += " *"
	}
	return wildcardMatch(value, pattern, goos == "windows")
}

func wildcardMatch(value, pattern string, fold bool) bool {
	v := utf16.Encode([]rune(value))
	p := utf16.Encode([]rune(pattern))
	type position struct{ pi, vi int }
	memo := map[position]bool{}
	seen := map[position]bool{}
	var visit func(int, int) bool
	visit = func(pi, vi int) bool {
		key := position{pi, vi}
		if seen[key] {
			return memo[key]
		}
		seen[key] = true
		var result bool
		switch {
		case pi == len(p):
			result = vi == len(v)
		case p[pi] == '*':
			result = visit(pi+1, vi) || vi < len(v) && visit(pi, vi+1)
		case p[pi] == '?':
			result = vi < len(v) && visit(pi+1, vi+1)
		default:
			result = vi < len(v) && equalUnit(p[pi], v[vi], fold) && visit(pi+1, vi+1)
		}
		memo[key] = result
		return result
	}
	return visit(0, 0)
}

func equalUnit(a, b uint16, fold bool) bool {
	if !fold || a == b {
		return a == b
	}
	// The wildcard call sites use ASCII command/path patterns. JS /i without
	// /u has ASCII canonicalization here; avoiding Unicode simple-fold prevents
	// ſ from matching s.
	if a >= 'a' && a <= 'z' {
		a -= 'a' - 'A'
	}
	if b >= 'a' && b <= 'z' {
		b -= 'a' - 'A'
	}
	return a == b
}

// All returns the value belonging to the last matching pattern after sorting
// patterns by length then UTF-16 lexical order.
func All[T any](input string, patterns map[string]T) (T, bool) {
	keys := sortedPatterns(patterns)
	var result T
	found := false
	for _, pattern := range keys {
		if Match(input, pattern) {
			result = patterns[pattern]
			found = true
		}
	}
	return result, found
}

// AllStructured matches a head plus an ordered subsequence of tail arguments.
func AllStructured[T any](input StructuredInput, patterns map[string]T) (T, bool) {
	keys := sortedPatterns(patterns)
	var result T
	found := false
	for _, pattern := range keys {
		parts := splitJSWhitespace(pattern)
		if len(parts) == 0 || !Match(input.Head, parts[0]) {
			continue
		}
		if len(parts) == 1 || matchSequence(input.Tail, parts[1:]) {
			result = patterns[pattern]
			found = true
		}
	}
	return result, found
}

func sortedPatterns[T any](patterns map[string]T) []string {
	keys := make([]string, 0, len(patterns))
	for key := range patterns {
		keys = append(keys, key)
	}
	sort.SliceStable(keys, func(i, j int) bool {
		iu := utf16.Encode([]rune(keys[i]))
		ju := utf16.Encode([]rune(keys[j]))
		if len(iu) != len(ju) {
			return len(iu) < len(ju)
		}
		return lessUTF16Units(iu, ju)
	})
	return keys
}

func lessUTF16Units(a, b []uint16) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func matchSequence(items, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if patterns[0] == "*" {
		return matchSequence(items, patterns[1:])
	}
	for i, item := range items {
		if Match(item, patterns[0]) && matchSequence(items[i+1:], patterns[1:]) {
			return true
		}
	}
	return false
}

func splitJSWhitespace(value string) []string {
	// String.split(/\s+/) retains a leading/trailing empty element.
	out := []string{}
	start := 0
	runes := []rune(value)
	for i := 0; i < len(runes); {
		if !jsSpace(runes[i]) {
			i++
			continue
		}
		out = append(out, string(runes[start:i]))
		for i < len(runes) && jsSpace(runes[i]) {
			i++
		}
		start = i
	}
	out = append(out, string(runes[start:]))
	return out
}

func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0x00a0, 0x1680, 0x2000, 0x2001,
		0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009,
		0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	default:
		return false
	}
}
