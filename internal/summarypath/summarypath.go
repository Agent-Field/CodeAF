// Package summarypath recognizes native absolute artifact names in summaries.
package summarypath

import (
	"path/filepath"
	"strings"
)

// Parse accepts an absolute host path surrounded by prose punctuation.
func Parse(field string) string {
	path := strings.Trim(strings.TrimSpace(field), `"'(),;:.`)
	if len(path) < 2 || !filepath.IsAbs(path) {
		return ""
	}
	return path
}

// Key compares native Windows spellings, including drive-letter case and
// separators. POSIX summaries keep their existing bytewise path identity.
func Key(path string) string {
	if filepath.Separator == '\\' && filepath.IsAbs(path) {
		return strings.ToLower(filepath.Clean(path))
	}
	return path
}
