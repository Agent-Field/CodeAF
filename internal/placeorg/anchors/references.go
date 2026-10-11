package anchors

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	issueReference = regexp.MustCompile(`(?:([A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+))?#([0-9]+)`)
	issueURL       = regexp.MustCompile(`(?i)https?://[^\s<>()]+`)
	issuePath      = regexp.MustCompile(`(?i)(?:^|/)(?:issues|pull)/([0-9]+)(?:/|$)`)
)

// references gives issue and pull URLs the same identity as a qualified
// reference. The bare number remains available for the harness's overlap
// rule; the qualified form also supplies repository-specific evidence.
func references(text string) map[string]bool {
	out := map[string]bool{}
	for _, match := range issueReference.FindAllStringSubmatchIndex(text, -1) {
		if !referenceBoundary(text, match[0], match[1]) {
			continue
		}
		repo := ""
		if match[2] >= 0 {
			repo = text[match[2]:match[3]]
		}
		addReference(out, repo, text[match[4]:match[5]])
	}
	for _, raw := range issueURL.FindAllString(text, -1) {
		link, err := url.Parse(strings.TrimRight(raw, ".,;:'\"}]"))
		if err != nil || link.Host == "" {
			continue
		}
		match := issuePath.FindStringSubmatchIndex(link.Path)
		if match == nil {
			continue
		}
		addReference(out, pathRepository(link.Path[:match[0]]), link.Path[match[2]:match[3]])
	}
	return out
}

// Some forges put a routing separator before /issues/. It is not part of
// the repository identity. A link without a repository still anchors its id.
func pathRepository(prefix string) string {
	parts := strings.Split(strings.Trim(prefix, "/"), "/")
	if parts[len(parts)-1] == "-" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

func addReference(out map[string]bool, repo, digits string) {
	// Removing leading zeroes makes links and handwritten references agree.
	number := strings.TrimLeft(digits, "0")
	if number == "" {
		return
	}
	key := "#" + number
	out[key] = true
	if repo != "" {
		out[strings.ToLower(repo)+key] = true
	}
}

func referenceBoundary(text string, start, end int) bool {
	return (start == 0 || !referenceWord(text[start-1])) && (end == len(text) || !referenceWord(text[end]))
}

func referenceWord(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '#'
}

// numericSuffix recognises decimal lane ids and long hexadecimal ids without
// stripping ordinary alphabetic project names.
func numericSuffix(s string) bool {
	if s == "" {
		return false
	}
	if _, err := strconv.ParseUint(s, 10, 64); err == nil {
		return true
	}
	if len(s) < 7 {
		return false
	}
	_, err := strconv.ParseUint(s, 16, 64)
	return err == nil
}
