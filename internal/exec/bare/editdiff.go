// Package bare — edit matching logic, pinned to pi's edit-diff.js.
package bare

import (
	"strings"
	"unicode"
)

// detectLineEnding mirrors pi's edit-diff.js:detectLineEnding: returns "\r\n"
// when the first CRLF precedes the first LF, otherwise "\n".
func detectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if lfIdx == -1 {
		return "\n"
	}
	if crlfIdx == -1 {
		return "\n"
	}
	if crlfIdx < lfIdx {
		return "\r\n"
	}
	return "\n"
}

// normalizeToLF mirrors pi's edit-diff.js:normalizeToLF.
func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// restoreLineEndings mirrors pi's edit-diff.js:restoreLineEndings.
func restoreLineEndings(text, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// stripBom mirrors pi's edit-diff.js:stripBom. The model will not include an
// invisible BOM in oldText, so it is stripped before matching and restored
// after.
func stripBom(content string) (bom, text string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[3:] // \uFEFF is 3 UTF-8 bytes
	}
	return "", content
}

// normalizeForFuzzyMatch mirrors pi's edit-diff.js:normalizeForFuzzyMatch:
// NFKC, per-line trailing whitespace trim, smart quotes/dashes to ASCII,
// unicode spaces to regular space. The exact set of characters replaced is
// pinned to the source at edit-diff.js:30-49.
func normalizeForFuzzyMatch(text string) string {
	// NFKC normalization. Go's unicode/norm was retired in favor of
	// x/text/unicode/norm, but we avoid the external dependency by doing
	// the same character-level replacements pi actually depends on. The
	// NFKC step in JS folds compatibility characters; the replacements that
	// follow (quotes, dashes, spaces) are the ones that actually change
	// match outcomes in practice. We do a pragmatic NFKC by applying
	// unicode normalization for the common cases.
	text = nfkc(text)

	// Strip trailing whitespace per line.
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t\r")
	}
	text = strings.Join(lines, "\n")

	// Smart single quotes → '
	text = strings.NewReplacer(
		"\u2018", "'",
		"\u2019", "'",
		"\u201A", "'",
		"\u201B", "'",
	).Replace(text)

	// Smart double quotes → "
	text = strings.NewReplacer(
		"\u201C", "\"",
		"\u201D", "\"",
		"\u201E", "\"",
		"\u201F", "\"",
	).Replace(text)

	// Various dashes/hyphens → -
	text = strings.NewReplacer(
		"\u2010", "-",
		"\u2011", "-",
		"\u2012", "-",
		"\u2013", "-",
		"\u2014", "-",
		"\u2015", "-",
		"\u2212", "-",
	).Replace(text)

	// Special spaces → regular space.
	text = strings.NewReplacer(
		"\u00A0", " ", // NBSP
		"\u2002", " ", // EN SPACE
		"\u2003", " ", // EM SPACE
		"\u2004", " ", // THREE-PER-EM SPACE
		"\u2005", " ", // FOUR-PER-EM SPACE
		"\u2006", " ", // SIX-PER-EM SPACE
		"\u2007", " ", // FIGURE SPACE
		"\u2008", " ", // PUNCTUATION SPACE
		"\u2009", " ", // THIN SPACE
		"\u200A", " ", // HAIR SPACE
		"\u202F", " ", // NARROW NO-BREAK SPACE
		"\u205F", " ", // MEDIUM MATHEMATICAL SPACE
		"\u3000", " ", // IDEOGRAPHIC SPACE
	).Replace(text)

	return text
}

// nfkc applies a pragmatic NFKC normalization. JS's String.normalize("NFKC")
// folds compatibility forms and composes/decomposes. The Go standard library
// does not ship NFKC; rather than pull in x/text we apply the compatibility
// decomposition for the cases that matter for source-code matching (the only
// reason pi normalizes is to make fuzzy matching lenient for code). This
// covers fullwidth ASCII variants, superscript digits, and other compatibility
// forms that appear in code pasted from rich-text sources.
func nfkc(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		b.WriteRune(nfkcRune(r))
	}
	return b.String()
}

// nfkcRune maps a single rune to its NFKC compatibility equivalent for the
// cases that affect code matching. This is not a complete NFKC table — it
// covers the compatibility decompositions that actually appear in source files
// (fullwidth ASCII, superscripts, ring-above, etc.) and falls through to the
// original rune otherwise. Composing canonical combining marks is left to the
// rune iterator, which already yields precomposed runes for common cases.
func nfkcRune(r rune) rune {
	// Fullwidth ASCII variants (U+FF01–U+FF5E) → ASCII.
	if r >= 0xFF01 && r <= 0xFF5E {
		return r - 0xFEE0
	}
	// Superscript digits.
	switch r {
	case 0x2070:
		return '0'
	case 0x00B9:
		return '1'
	case 0x00B2:
		return '2'
	case 0x00B3:
		return '3'
	case 0x2074:
		return '4'
	case 0x2075:
		return '5'
	case 0x2076:
		return '6'
	case 0x2077:
		return '7'
	case 0x2078:
		return '8'
	case 0x2079:
		return '9'
	case 0x00AA:
		return 'a'
	case 0x00BA:
		return 'o'
	}
	return r
}

// matchResult mirrors pi's fuzzyFindText return shape.
type matchResult struct {
	found       bool
	index       int
	matchLength int
	usedFuzzy   bool
}

// fuzzyFindText mirrors pi's edit-diff.js:fuzzyFindText: exact substring
// first, then fuzzy match in normalized space. Returns the match index and
// length in the (possibly normalized) content space.
func fuzzyFindText(content, oldText string) matchResult {
	// Exact match first.
	idx := strings.Index(content, oldText)
	if idx != -1 {
		return matchResult{
			found:       true,
			index:       idx,
			matchLength: len(oldText),
			usedFuzzy:   false,
		}
	}
	// Fuzzy match in normalized space.
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	fuzzyIdx := strings.Index(fuzzyContent, fuzzyOldText)
	if fuzzyIdx == -1 {
		return matchResult{found: false}
	}
	return matchResult{
		found:       true,
		index:       fuzzyIdx,
		matchLength: len(fuzzyOldText),
		usedFuzzy:   true,
	}
}

// matchedEdit is one resolved edit, sorted by matchIndex.
type matchedEdit struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

// applyEditsResult is the output of applyEditsToNormalizedContent.
type applyEditsResult struct {
	baseContent string
	newContent  string
}

// applyEditsToNormalizedContent mirrors pi's edit-diff.js function of the same
// name. All edits are matched against the same original (normalized, LF)
// content. If any edit needs fuzzy matching, the operation runs in
// fuzzy-normalized space and then overlays those line-level changes onto the
// original content so unchanged line blocks keep their original bytes.
// Replacements are applied in reverse order so offsets remain stable.
//
// Error strings are the exact pi strings — the tests pin them.
func applyEditsToNormalizedContent(normalizedContent string, edits []editPair, path string) (applyEditsResult, error) {
	normalizedEdits := make([]editPair, len(edits))
	for i, e := range edits {
		normalizedEdits[i] = editPair{
			oldText: normalizeToLF(e.oldText),
			newText: normalizeToLF(e.newText),
		}
	}

	// Check empty oldText.
	for i, e := range normalizedEdits {
		if len(e.oldText) == 0 {
			return applyEditsResult{}, errorEmpty(path, i, len(normalizedEdits))
		}
	}

	// Determine whether any edit uses fuzzy matching.
	usedFuzzyMatch := false
	for _, e := range normalizedEdits {
		if !strings.Contains(normalizedContent, e.oldText) {
			// Exact not found; will need fuzzy.
			usedFuzzyMatch = true
			break
		}
	}

	replacementBaseContent := normalizedContent
	if usedFuzzyMatch {
		replacementBaseContent = normalizeForFuzzyMatch(normalizedContent)
	}

	var matchedEdits []matchedEdit
	for i, e := range normalizedEdits {
		mr := fuzzyFindText(replacementBaseContent, e.oldText)
		if !mr.found {
			return applyEditsResult{}, errorNotFound(path, i, len(normalizedEdits))
		}
		occurrences := strings.Count(replacementBaseContent, e.oldText)
		if occurrences > 1 {
			return applyEditsResult{}, errorDuplicate(path, i, len(normalizedEdits), occurrences)
		}
		matchedEdits = append(matchedEdits, matchedEdit{
			editIndex:   i,
			matchIndex:  mr.index,
			matchLength: mr.matchLength,
			newText:     e.newText,
		})
	}

	// Sort by matchIndex for overlap detection.
	sortMatchedEdits(matchedEdits)

	for i := 1; i < len(matchedEdits); i++ {
		prev := matchedEdits[i-1]
		cur := matchedEdits[i]
		if prev.matchIndex+prev.matchLength > cur.matchIndex {
			return applyEditsResult{}, errorOverlap(path, prev.editIndex, cur.editIndex)
		}
	}

	baseContent := normalizedContent
	var newContent string
	if usedFuzzyMatch {
		newContent = applyReplacementsPreservingUnchangedLines(normalizedContent, replacementBaseContent, matchedEdits)
	} else {
		newContent = applyReplacements(replacementBaseContent, matchedEdits)
	}

	if baseContent == newContent {
		return applyEditsResult{}, errorNoChange(path, len(normalizedEdits))
	}

	return applyEditsResult{
		baseContent: baseContent,
		newContent:  newContent,
	}, nil
}

// applyReplacements applies matched edits in reverse order so offsets stay
// stable. This is the non-fuzzy path where baseContent == normalizedContent.
func applyReplacements(content string, replacements []matchedEdit) string {
	result := content
	// Sort descending by matchIndex to apply from end to start.
	sorted := make([]matchedEdit, len(replacements))
	copy(sorted, replacements)
	for i, j := 0, len(sorted)-1; i < j; i, j = i+1, j-1 {
		sorted[i], sorted[j] = sorted[j], sorted[i]
	}
	for _, r := range sorted {
		result = result[:r.matchIndex] + r.newText + result[r.matchIndex+r.matchLength:]
	}
	return result
}

// applyReplacementsPreservingUnchangedLines overlays fuzzy-matched replacements
// onto the original content so unchanged line blocks keep their original bytes.
// This mirrors pi's edit-diff.js:104-133 exactly: group replacements by line
// ranges, copy unchanged original lines between groups, and apply replacements
// within each group against the fuzzy-normalized content slice.
func applyReplacementsPreservingUnchangedLines(originalContent, baseContent string, replacements []matchedEdit) string {
	originalLines := splitLinesWithEndings(originalContent)
	baseLines := getLineSpans(baseContent)
	if len(originalLines) != len(baseLines) {
		// This should not happen when fuzzy matching, because both are
		// derived from the same content with the same line structure.
		// Fall back to plain replacement to avoid a panic.
		return applyReplacements(baseContent, replacements)
	}

	sorted := make([]matchedEdit, len(replacements))
	copy(sorted, replacements)
	sortMatchedEdits(sorted)

	type replacementGroup struct {
		startLine    int
		endLine      int // exclusive
		replacements []matchedEdit
	}
	var groups []replacementGroup
	for _, r := range sorted {
		startLine, endLine := getReplacementLineRange(baseLines, r)
		if len(groups) > 0 && startLine < groups[len(groups)-1].endLine {
			g := &groups[len(groups)-1]
			if endLine > g.endLine {
				g.endLine = endLine
			}
			g.replacements = append(g.replacements, r)
			continue
		}
		groups = append(groups, replacementGroup{
			startLine:    startLine,
			endLine:      endLine,
			replacements: []matchedEdit{r},
		})
	}

	var result strings.Builder
	originalLineIndex := 0
	for _, g := range groups {
		result.WriteString(strings.Join(originalLines[originalLineIndex:g.startLine], ""))
		groupStartOffset := baseLines[g.startLine].start
		groupEndOffset := baseLines[g.endLine-1].end
		result.WriteString(applyReplacements(baseContent[groupStartOffset:groupEndOffset], g.replacements))
		originalLineIndex = g.endLine
	}
	result.WriteString(strings.Join(originalLines[originalLineIndex:], ""))
	return result.String()
}

// splitLinesWithEndings mirrors pi's edit-diff.js:splitLinesWithEndings.
// Splits content into lines that retain their line ending (including the
// trailing "\n" if present).
func splitLinesWithEndings(content string) []string {
	if content == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := range content {
		if content[i] == '\n' {
			lines = append(lines, content[start:i+1])
			start = i + 1
		}
	}
	if start < len(content) {
		lines = append(lines, content[start:])
	}
	return lines
}

// lineSpan is the byte offset range of one line.
type lineSpan struct {
	start int
	end   int
}

// getLineSpans mirrors pi's edit-diff.js:getLineSpans.
func getLineSpans(content string) []lineSpan {
	lines := splitLinesWithEndings(content)
	spans := make([]lineSpan, len(lines))
	offset := 0
	for i, line := range lines {
		spans[i] = lineSpan{start: offset, end: offset + len(line)}
		offset = spans[i].end
	}
	return spans
}

// getReplacementLineRange finds the line range [startLine, endLine) that the
// replacement spans in the base content. Mirrors pi's edit-diff.js:61-83.
func getReplacementLineRange(lines []lineSpan, r matchedEdit) (int, int) {
	replacementStart := r.matchIndex
	replacementEnd := r.matchIndex + r.matchLength
	startLine := -1
	for i, line := range lines {
		if replacementStart >= line.start && replacementStart < line.end {
			startLine = i
			break
		}
	}
	if startLine == -1 {
		// Replacement at the very end of content — clamp to last line.
		if len(lines) > 0 && replacementStart == lines[len(lines)-1].end {
			return len(lines) - 1, len(lines)
		}
		return 0, 1
	}
	endLine := startLine
	for endLine < len(lines) && lines[endLine].end < replacementEnd {
		endLine++
	}
	if endLine >= len(lines) {
		endLine = len(lines) - 1
	}
	return startLine, endLine + 1
}

// sortMatchedEdits sorts by matchIndex ascending (stable not required —
// overlap detection would catch duplicates, and all matchIndices are unique
// since each oldText is unique in the base content).
func sortMatchedEdits(edits []matchedEdit) {
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && edits[j-1].matchIndex > edits[j].matchIndex; j-- {
			edits[j-1], edits[j] = edits[j], edits[j-1]
		}
	}
}

// ── exact error strings (pinned to edit-diff.js:185-208) ──────────────────

func errorNotFound(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return errString("Could not find the exact text in " + path + ". The old text must match exactly including all whitespace and newlines.")
	}
	return errString("Could not find edits[" + itoa(editIndex) + "] in " + path + ". The oldText must match exactly including all whitespace and newlines.")
}

func errorDuplicate(path string, editIndex, totalEdits, occurrences int) error {
	if totalEdits == 1 {
		return errString("Found " + itoa(occurrences) + " occurrences of the text in " + path + ". The text must be unique. Please provide more context to make it unique.")
	}
	return errString("Found " + itoa(occurrences) + " occurrences of edits[" + itoa(editIndex) + "] in " + path + ". Each oldText must be unique. Please provide more context to make it unique.")
}

func errorEmpty(path string, editIndex, totalEdits int) error {
	if totalEdits == 1 {
		return errString("oldText must not be empty in " + path + ".")
	}
	return errString("edits[" + itoa(editIndex) + "].oldText must not be empty in " + path + ".")
}

func errorNoChange(path string, totalEdits int) error {
	if totalEdits == 1 {
		return errString("No changes made to " + path + ". The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.")
	}
	return errString("No changes made to " + path + ". The replacements produced identical content.")
}

func errorOverlap(path string, prevIndex, curIndex int) error {
	return errString("edits[" + itoa(prevIndex) + "] and edits[" + itoa(curIndex) + "] overlap in " + path + ". Merge them into one edit or target disjoint regions.")
}

// errString is a simple error type that carries an exact string.
type errString string

func (e errString) Error() string { return string(e) }

// editPair is one {oldText, newText} replacement.
type editPair struct {
	oldText string
	newText string
}

// _ keeps unicode imported (used by nfkc indirectly via rune iteration).
var _ = unicode.IsLetter
