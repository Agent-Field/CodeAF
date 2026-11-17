package factory

import (
	"fmt"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxNameChars bounds the display name in both the host config and its command.
const MaxNameChars = 40

// NormalizeName trims surrounding spaces while refusing control characters,
// including those at the edges that trimming would otherwise hide.
func NormalizeName(text string) (string, error) {
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("factory name must be valid UTF-8")
	}
	for _, r := range text {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("factory name must contain no control characters")
		}
	}
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > MaxNameChars {
		return "", fmt.Errorf("factory name must be at most %d characters", MaxNameChars)
	}
	return text, nil
}

// DisplayName uses the first watched repository's owner when no name was set.
// A factory without repositories uses this machine's short hostname.
func DisplayName(name string, repos []string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	if len(repos) > 0 {
		if owner, _, ok := strings.Cut(repos[0], "/"); ok && owner != "" {
			return owner
		}
	}
	host, _ := os.Hostname()
	host, _, _ = strings.Cut(host, ".")
	if host == "" {
		return "codeaf"
	}
	return host
}
