// Package leafdigest ports src/session/leaf-digest.ts:1-120 from swe-pro at
// commit 3b25a1a.
//
// The digest is model-visible, so every heading, fallback, newline, and
// truncation marker is kept byte-for-byte. JavaScript measures strings in
// UTF-16 code units. The helpers below therefore preserve a lone surrogate
// produced when String.prototype.slice cuts through an astral character by
// storing it in a Go string as WTF-8.
//
// Fidelity note: the TS comment says Outcome is never truncated, but its final
// fallback slices the entire Outcome-only rendering to 2,000 code units. That
// behavior is preserved and registered in BUGS-KEPT.md.
package leafdigest

import (
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

// DigestMaxChars is DIGEST_MAX_CHARS.
const DigestMaxChars = 2000 // W7-TODO(knobs)

// LeafDigestInput mirrors the TS type of the same name. Nil optional slices
// stand in for undefined; a non-nil empty slice stands in for [].
type LeafDigestInput struct {
	TaskID         string   `json:"taskID"`
	Verdict        string   `json:"verdict"`
	ChangedFiles   []string `json:"changedFiles"`
	TestSummary    *string  `json:"testSummary"`
	KeyDecisions   []string `json:"keyDecisions"`
	FailureSignals []string `json:"failureSignals"`
}

func bulletList(items []string, empty string) string {
	if len(items) == 0 {
		return empty
	}
	lines := make([]string, len(items))
	for i, item := range items {
		lines[i] = "- " + item
	}
	return strings.Join(lines, "\n")
}

func notesSection(input LeafDigestInput) string {
	lines := []string{}
	for _, decision := range input.KeyDecisions {
		if trimmed := jscompat.Trim(decision); trimmed != "" {
			lines = append(lines, "- decision: "+trimmed)
		}
	}
	for _, signal := range input.FailureSignals {
		if trimmed := jscompat.Trim(signal); trimmed != "" {
			lines = append(lines, "- failure: "+trimmed)
		}
	}
	if len(lines) == 0 {
		return "(none)"
	}
	return strings.Join(lines, "\n")
}

func renderSections(outcome, files, tests, notes string) string {
	return strings.Join([]string{
		"## Outcome",
		outcome,
		"",
		"## Files",
		files,
		"",
		"## Tests",
		tests,
		"",
		"## Notes",
		notes,
	}, "\n")
}

// BuildLeafDigest builds the bounded structured handoff. Its truncation order
// is Notes, Tests, Files, then (despite the TS comment) the whole Outcome-only
// rendering.
func BuildLeafDigest(input LeafDigestInput) string {
	outcome := "task: " + input.TaskID + "\nverdict: " + input.Verdict

	cleanFiles := []string{}
	for _, file := range input.ChangedFiles {
		if trimmed := jscompat.Trim(file); trimmed != "" {
			cleanFiles = append(cleanFiles, trimmed)
		}
	}
	files := bulletList(cleanFiles, "(none)")

	tests := "(none)"
	if input.TestSummary != nil {
		if trimmed := jscompat.Trim(*input.TestSummary); trimmed != "" {
			tests = trimmed
		}
	}
	notes := notesSection(input)

	digest := renderSections(outcome, files, tests, notes)
	if utf16Length(digest) <= DigestMaxChars {
		return digest
	}

	// Truncate Notes first (shrink, then drop).
	overheadWithoutNotes := utf16Length(renderSections(outcome, files, tests, ""))
	notesBudget := DigestMaxChars - overheadWithoutNotes
	switch {
	case notesBudget <= 0:
		notes = ""
	case utf16Length(notes) > notesBudget:
		notes = utf16SliceTo(notes, max(0, notesBudget-1)) + "…"
	}
	digest = renderSections(outcome, files, tests, notes)
	if utf16Length(digest) <= DigestMaxChars {
		return digest
	}

	// Then Tests.
	overheadWithoutTests := utf16Length(renderSections(outcome, files, "", notes))
	testsBudget := DigestMaxChars - overheadWithoutTests
	switch {
	case testsBudget <= 0:
		tests = ""
	case utf16Length(tests) > testsBudget:
		tests = utf16SliceTo(tests, max(0, testsBudget-1)) + "…"
	}
	digest = renderSections(outcome, files, tests, notes)
	if utf16Length(digest) <= DigestMaxChars {
		return digest
	}

	// Then Files.
	overheadWithoutFiles := utf16Length(renderSections(outcome, "", tests, notes))
	filesBudget := DigestMaxChars - overheadWithoutFiles
	switch {
	case filesBudget <= 0:
		files = ""
	case utf16Length(files) > filesBudget:
		files = utf16SliceTo(files, max(0, filesBudget-1)) + "…"
	}
	digest = renderSections(outcome, files, tests, notes)
	if utf16Length(digest) <= DigestMaxChars {
		return digest
	}

	// Last resort: this slices Outcome too, contrary to the source comment.
	return utf16SliceTo(renderSections(outcome, "", "", ""), DigestMaxChars)
}

// decodeWTF8 decodes one code point, accepting surrogate code points encoded
// in the three-byte WTF-8 form. Ordinary callers supply valid UTF-8; the
// surrogate case is created only by utf16SliceTo.
func decodeWTF8(s string, i int) (rune, int) {
	b := s[i]
	switch {
	case b < 0x80:
		return rune(b), 1
	case b&0xe0 == 0xc0:
		if i+1 < len(s) && s[i+1]&0xc0 == 0x80 {
			cp := rune(b&0x1f)<<6 | rune(s[i+1]&0x3f)
			if cp >= 0x80 {
				return cp, 2
			}
		}
	case b&0xf0 == 0xe0:
		if i+2 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 {
			cp := rune(b&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f)
			if cp >= 0x800 {
				return cp, 3
			}
		}
	case b&0xf8 == 0xf0:
		if i+3 < len(s) && s[i+1]&0xc0 == 0x80 && s[i+2]&0xc0 == 0x80 && s[i+3]&0xc0 == 0x80 {
			cp := rune(b&0x07)<<18 | rune(s[i+1]&0x3f)<<12 | rune(s[i+2]&0x3f)<<6 | rune(s[i+3]&0x3f)
			if cp >= 0x10000 && cp <= 0x10ffff {
				return cp, 4
			}
		}
	}
	return utf8.RuneError, 1
}

func appendWTF8(dst []byte, cp rune) []byte {
	if cp >= 0xd800 && cp <= 0xdfff {
		return append(dst,
			byte(0xe0|cp>>12),
			byte(0x80|(cp>>6)&0x3f),
			byte(0x80|cp&0x3f),
		)
	}
	return utf8.AppendRune(dst, cp)
}

func utf16Length(s string) int {
	n := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		i += size
		if cp >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

func utf16SliceTo(s string, n int) string {
	if n <= 0 {
		return ""
	}
	units := 0
	for i := 0; i < len(s); {
		cp, size := decodeWTF8(s, i)
		width := 1
		if cp >= 0x10000 {
			width = 2
		}
		if units+width > n {
			high := rune(0xd800 + ((cp - 0x10000) >> 10))
			return s[:i] + string(appendWTF8(nil, high))
		}
		units += width
		i += size
		if units == n {
			return s[:i]
		}
	}
	return s
}
