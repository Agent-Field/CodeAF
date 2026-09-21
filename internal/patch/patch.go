// Package patch is the exact-match text editor behind `codeaf patch`: one old
// text in, the file with its single occurrence replaced out.
//
// The law it keeps is the edit hand's (internal/exec/bare), which cannot be
// imported from here: an exact match is counted in line-ending-normalized
// space, so a caller who typed their old text with the endings their terminal
// shows still edits a CRLF file, and a replacement is refused rather than
// guessed at when the text matches nothing or matches several places at once.
// A shell has no model to ask for more context, so the refusals name the
// count — "found 3 matches" is something a person can act on, and a silent
// first-match replacement is not.
package patch

import (
	"fmt"
	"strings"
)

// Apply replaces the ONE occurrence of oldText in content with newText.
//
// Matching is exact after three normalizations the edit hand also makes: a
// leading byte-order mark is set aside (a shell will not include one in the
// text it passes), every line ending is folded to LF (so LF text edits a CRLF
// file), and the replacement is written back with the file's own ending. The
// match is a plain substring count after that folding — no fuzzy fallback,
// because a person typing a patch into a shell wants either the edit they
// described or a refusal, and a "close enough" match would apply the wrong
// one.
//
// The returned error carries the count when the match is not exactly one:
// none, or two or more. Content is only returned when the edit was made.
func Apply(content, oldText, newText string) (string, error) {
	if oldText == "" {
		return "", fmt.Errorf("the old text is empty — name the lines to replace with --old or --old-file")
	}

	bom, body := stripBOM(content)
	ending := detectLineEnding(body)
	normalized := normalizeToLF(body)
	old := normalizeToLF(oldText)

	matches := strings.Count(normalized, old)
	switch {
	case matches == 0:
		return "", fmt.Errorf("found 0 matches of the old text — it must appear exactly once, matching including whitespace and line endings")
	case matches > 1:
		return "", fmt.Errorf("found %d matches of the old text — it must match exactly one region; add surrounding lines to make it unique", matches)
	}

	replaced := strings.Replace(normalized, old, normalizeToLF(newText), 1)
	return bom + restoreLineEndings(replaced, ending), nil
}

// stripBOM separates a leading byte-order mark from the body, so text a shell
// pastes without one still matches the file's first line. It is restored on
// the way out for the same reason: the mark is the file's, not the edit's to
// remove.
func stripBOM(content string) (bom, body string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return content[:3], content[3:]
	}
	return "", content
}

// detectLineEnding returns CRLF when the file's first CRLF precedes its first
// LF, and LF otherwise — including when the file has no line endings at all,
// where LF is the ending a single-line replacement needs to restore nothing.
func detectLineEnding(content string) string {
	crlf := strings.Index(content, "\r\n")
	lf := strings.Index(content, "\n")
	if crlf != -1 && (lf == -1 || crlf < lf) {
		return "\r\n"
	}
	return "\n"
}

// normalizeToLF folds every CR and CRLF to LF, the form both the file and the
// old text are counted in.
func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.ReplaceAll(text, "\r", "\n")
}

// restoreLineEndings puts the file's own ending back on every line, the
// replaced block included, so a patch never changes how a file ends its lines
// beyond the lines it was asked to change.
func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}
