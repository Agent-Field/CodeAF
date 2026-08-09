// Repository-reference parsing — port of src/util/repository.ts:5-142
// (swe-pro 3b25a1a).
package util

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

type RepositoryReference struct {
	Host     string   `json:"host"`
	Path     string   `json:"path"`
	Segments []string `json:"segments"`
	Owner    *string  `json:"owner,omitempty"`
	Repo     string   `json:"repo"`
	Remote   string   `json:"remote"`
	Label    string   `json:"label"`
	Protocol *string  `json:"protocol,omitempty"`
}

var (
	githubPrefixPattern = regexp.MustCompile(`^github:([^/\s]+)/([^/\s]+)$`)
	scpPattern          = regexp.MustCompile(`^(?:[^@/\s]+@)?([^:/\s]+):(.+)$`)
)

func normalizeRepositoryInput(input string) string {
	input = jscompat.Trim(input)
	input = strings.TrimPrefix(input, "git+")
	if index := strings.IndexByte(input, '#'); index >= 0 {
		input = input[:index]
	}
	return strings.TrimRight(input, "/")
}

func trimGitSuffix(input string) string { return strings.TrimSuffix(input, ".git") }

func repositoryParts(input string) []string {
	out := []string{}
	for _, item := range strings.Split(input, "/") {
		item = trimGitSuffix(jscompat.Trim(item))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func safeRepositoryHost(input string) bool {
	if input == "" || strings.HasPrefix(input, "-") {
		return false
	}
	for _, r := range input {
		if jsSpace(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func safeRepositorySegment(input string) bool {
	if input == "." || input == ".." || strings.ContainsRune(input, ':') {
		return false
	}
	for _, r := range input {
		if jsSpace(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func hostLike(input string) bool {
	return strings.ContainsAny(input, ".:") || input == "localhost"
}

func githubRemote(pathname string) string {
	base := os.Getenv("CODEAF_REPO_CLONE_GITHUB_BASE_URL")
	if base == "" {
		return "https://github.com/" + pathname + ".git"
	}
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return pathname + ".git"
	}
	relative, _ := url.Parse(pathname + ".git")
	return parsed.ResolveReference(relative).String()
}

func buildRepository(host string, segments []string, remote, protocol string) *RepositoryReference {
	clean := []string{}
	for _, segment := range segments {
		segment = trimGitSuffix(segment)
		if segment != "" {
			clean = append(clean, segment)
		}
	}
	if !safeRepositoryHost(host) || len(clean) == 0 {
		return nil
	}
	for _, segment := range clean {
		if !safeRepositorySegment(segment) {
			return nil
		}
	}
	host = strings.ToLower(host)
	pathname := strings.Join(clean, "/")
	repo := clean[len(clean)-1]
	var owner *string
	if len(clean) == 2 {
		value := clean[0]
		owner = &value
	}
	if remote == "" {
		if host == "github.com" {
			remote = githubRemote(pathname)
		} else {
			remote = "https://" + host + "/" + pathname + ".git"
		}
	}
	label := host + "/" + pathname
	if host == "github.com" && len(clean) == 2 {
		label = pathname
	}
	var protocolValue *string
	if protocol != "" {
		protocolValue = &protocol
	}
	return &RepositoryReference{
		Host: host, Path: pathname, Segments: clean, Owner: owner, Repo: repo,
		Remote: remote, Label: label, Protocol: protocolValue,
	}
}

func buildFileRepository(parsed *url.URL, remote string) *RepositoryReference {
	pathname, err := url.PathUnescape(parsed.Path)
	if err != nil {
		return nil
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		pathname = "//" + parsed.Host + pathname
	}
	pathname = filepath.Clean(filepath.FromSlash(pathname))
	rawSegments := strings.FieldsFunc(pathname, func(r rune) bool { return r == '/' || r == '\\' })
	if len(rawSegments) == 0 {
		return nil
	}
	segments := make([]string, len(rawSegments))
	for i, segment := range rawSegments {
		segments[i] = strings.TrimSuffix(segment, ":")
	}
	protocol := "file:"
	return &RepositoryReference{
		Host:     "file",
		Path:     pathname,
		Segments: segments,
		Repo:     trimGitSuffix(rawSegments[len(rawSegments)-1]),
		Remote:   remote,
		Label:    pathname,
		Protocol: &protocol,
	}
}

func ParseRepositoryReference(input string) *RepositoryReference {
	cleaned := normalizeRepositoryInput(input)
	if cleaned == "" {
		return nil
	}
	if match := githubPrefixPattern.FindStringSubmatch(cleaned); match != nil {
		return buildRepository("github.com", []string{match[1], match[2]}, "", "")
	}
	if !strings.Contains(cleaned, "://") {
		if match := scpPattern.FindStringSubmatch(cleaned); match != nil {
			return buildRepository(match[1], repositoryParts(match[2]), cleaned, "")
		}
		direct := repositoryParts(cleaned)
		if len(direct) >= 2 && hostLike(direct[0]) {
			return buildRepository(direct[0], direct[1:], "", "")
		}
		if len(direct) == 2 {
			return buildRepository("github.com", direct, "", "")
		}
	}
	parsed, err := url.Parse(cleaned)
	if err != nil || parsed.Scheme == "" {
		return nil
	}
	if parsed.Scheme == "file" {
		return buildFileRepository(parsed, cleaned)
	}
	host := strings.ToLower(parsed.Host)
	if at := strings.LastIndexByte(host, '@'); at >= 0 {
		host = host[at+1:]
	}
	segments := repositoryParts(parsed.EscapedPath())
	for i, segment := range segments {
		if decoded, err := url.PathUnescape(segment); err == nil {
			segments[i] = decoded
		}
	}
	remote := cleaned
	if host == "github.com" {
		remote = githubRemote(strings.Join(segments, "/"))
	}
	return buildRepository(host, segments, remote, parsed.Scheme+":")
}

type GitHubRemote struct {
	Owner string `json:"owner"`
	Repo  string `json:"repo"`
}

func ParseGitHubRemote(input string) *GitHubRemote {
	cleaned := normalizeRepositoryInput(input)
	if !strings.Contains(cleaned, "://") &&
		!regexp.MustCompile(`^(?:[^@/\s]+@)?github\.com:`).MatchString(cleaned) {
		return nil
	}
	parsed := ParseRepositoryReference(cleaned)
	if parsed == nil || parsed.Host != "github.com" || parsed.Owner == nil || len(parsed.Segments) != 2 {
		return nil
	}
	return &GitHubRemote{Owner: *parsed.Owner, Repo: parsed.Repo}
}

// RepositoryCachePath joins a caller-provided Global.Path.repos equivalent.
func RepositoryCachePath(reposDir string, input RepositoryReference) string {
	parts := []string{reposDir}
	parts = append(parts, strings.Split(input.Host, ":")...)
	parts = append(parts, input.Segments...)
	return filepath.Join(parts...)
}

func SameRepositoryReference(left, right RepositoryReference) bool {
	return left.Host == right.Host && left.Path == right.Path
}
