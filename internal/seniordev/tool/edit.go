//go:build !windows

// The edit tool: exact string replacement backed by a ladder of progressively
// more lenient matchers, tried in order until exactly one match is found.
package tool

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf16"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	patchpkg "github.com/Agent-Field/codeaf/internal/seniordev/patch"
	"github.com/Agent-Field/codeaf/internal/seniordev/util"
)

const (
	singleCandidateSimilarityThreshold    = 0.0
	multipleCandidatesSimilarityThreshold = 0.3
)

var editLocks sync.Map

type editFileDiff struct {
	File      string `json:"file"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type editMetadata struct {
	Diagnostics map[string]any `json:"diagnostics"`
	Diff        string         `json:"diff"`
	FileDiff    editFileDiff   `json:"filediff"`
}

type blockCandidate struct {
	startLine int
	endLine   int
}

type replacer func(content string, find string) []string

func (r *Registry) executeEdit(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input editInput
	if err := decodeInput(call.Input, &input, "filePath", "oldString", "newString"); err != nil {
		return steploop.ToolResult{}, err
	}
	if input.FilePath == "" {
		return steploop.ToolResult{}, errors.New("filePath is required")
	}
	if input.OldString == input.NewString {
		return steploop.ToolResult{}, errors.New("No changes to apply: oldString and newString are identical.")
	}
	if err := ctx.Err(); err != nil {
		return steploop.ToolResult{}, err
	}

	resolved, err := r.resolvePath(input.FilePath)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if err := r.askExternalDirectory(ctx, call, resolved, "file"); err != nil {
		return steploop.ToolResult{}, err
	}
	formatter, err := r.formatterService()
	if err != nil {
		return steploop.ToolResult{}, err
	}
	mutexValue, _ := editLocks.LoadOrStore(resolved, &sync.Mutex{})
	mutex := mutexValue.(*sync.Mutex)
	mutex.Lock()
	defer mutex.Unlock()

	var contentOld string
	var contentNew string
	var desiredBOM bool
	if input.OldString == "" {
		source, readErr := os.ReadFile(resolved)
		if readErr != nil && !os.IsNotExist(readErr) {
			return steploop.ToolResult{}, readErr
		}
		sourceBOM, sourceText := splitBOM(strings.ToValidUTF8(string(source), "\uFFFD"))
		nextBOM, nextText := splitBOM(input.NewString)
		contentOld = sourceText
		contentNew = nextText
		desiredBOM = sourceBOM || nextBOM
	} else {
		info, statErr := os.Stat(resolved)
		if os.IsNotExist(statErr) {
			return steploop.ToolResult{}, fmt.Errorf("File %s not found", resolved)
		}
		if statErr != nil {
			return steploop.ToolResult{}, statErr
		}
		if info.IsDir() {
			return steploop.ToolResult{}, fmt.Errorf("Path is a directory, not a file: %s", resolved)
		}
		source, readErr := os.ReadFile(resolved)
		if readErr != nil {
			return steploop.ToolResult{}, readErr
		}
		sourceBOM, sourceText := splitBOM(strings.ToValidUTF8(string(source), "\uFFFD"))
		contentOld = sourceText
		ending := detectLineEnding(contentOld)
		oldString := convertToLineEnding(normalizeLineEndings(input.OldString), ending)
		newString := convertToLineEnding(normalizeLineEndings(input.NewString), ending)
		replaced, replaceErr := Replace(contentOld, oldString, newString, input.ReplaceAll)
		if replaceErr != nil {
			return steploop.ToolResult{}, replaceErr
		}
		nextBOM, nextText := splitBOM(replaced)
		contentNew = nextText
		desiredBOM = sourceBOM || nextBOM
	}

	proposedOld, proposedNew := contentOld, contentNew
	if input.OldString != "" {
		proposedOld = normalizeLineEndings(proposedOld)
		proposedNew = normalizeLineEndings(proposedNew)
	}
	proposedDiff := proposedFileDiff(resolved, proposedOld, proposedNew)
	pattern, relErr := filepath.Rel(r.worktree(), resolved)
	if relErr != nil {
		pattern = resolved
	}
	metadata := map[string]any{"filepath": resolved, "diff": proposedDiff}
	if err := r.ask(ctx, call, "edit", []string{filepath.ToSlash(pattern)}, metadata); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := os.WriteFile(resolved, []byte(joinBOM(contentNew, desiredBOM)), 0o644); err != nil {
		return steploop.ToolResult{}, err
	}
	contentNew, err = formatMutationFile(ctx, formatter, resolved, desiredBOM)
	if err != nil {
		return steploop.ToolResult{}, err
	}

	util.EagerCommit(ctx, util.EagerCommitOptions{Cwd: r.workDir, FilePath: resolved, Label: "edit"})

	title, err := filepath.Rel(r.workDir, resolved)
	if err != nil {
		title = resolved
	}
	additions, deletions := lineChangeCounts(contentOld, contentNew)
	diff := TrimDiff(patchpkg.GenerateTwoFilesPatch(resolved,
		normalizeLineEndings(contentOld),
		normalizeLineEndings(contentNew),
	))
	fileDiff := editFileDiff{
		File:      resolved,
		Patch:     diff,
		Additions: additions,
		Deletions: deletions,
	}
	return steploop.ToolResult{
		Title:  title,
		Output: "Edit applied successfully.",
		Metadata: rawMetadata(editMetadata{
			Diagnostics: map[string]any{},
			Diff:        diff,
			FileDiff:    fileDiff,
		}),
	}, nil
}

func normalizeLineEndings(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}

func detectLineEnding(text string) string {
	if strings.Contains(text, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func convertToLineEnding(text string, ending string) string {
	if ending == "\n" {
		return text
	}
	return strings.ReplaceAll(text, "\n", "\r\n")
}

func proposedFileDiff(filePath, oldContent, newContent string) string {
	return TrimDiff(patchpkg.GenerateTwoFilesPatch(filePath, oldContent, newContent))
}

func levenshtein(a string, b string) int {
	aa := utf16.Encode([]rune(a))
	bb := utf16.Encode([]rune(b))
	if len(aa) == 0 || len(bb) == 0 {
		if len(aa) > len(bb) {
			return len(aa)
		}
		return len(bb)
	}
	previous := make([]int, len(bb)+1)
	current := make([]int, len(bb)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(aa); i++ {
		current[0] = i
		for j := 1; j <= len(bb); j++ {
			cost := 1
			if aa[i-1] == bb[j-1] {
				cost = 0
			}
			current[j] = min3(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(bb)]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// SimpleReplacer matches the search text exactly.
func SimpleReplacer(_ string, find string) []string {
	return []string{find}
}

// LineTrimmedReplacer matches line by line, ignoring leading and trailing
// whitespace on each line.
func LineTrimmedReplacer(content string, find string) []string {
	originalLines := strings.Split(content, "\n")
	searchLines := strings.Split(find, "\n")
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	out := []string{}
	for i := 0; i <= len(originalLines)-len(searchLines); i++ {
		matches := true
		for j := range searchLines {
			if strings.TrimSpace(originalLines[i+j]) != strings.TrimSpace(searchLines[j]) {
				matches = false
				break
			}
		}
		if matches {
			out = append(out, strings.Join(originalLines[i:i+len(searchLines)], "\n"))
		}
	}
	return out
}

// BlockAnchorReplacer matches a block by its first and last lines and scores
// the lines between them by similarity; a lone candidate is accepted outright,
// competing candidates must clear multipleCandidatesSimilarityThreshold.
func BlockAnchorReplacer(content string, find string) []string {
	originalLines := strings.Split(content, "\n")
	searchLines := strings.Split(find, "\n")
	if len(searchLines) < 3 {
		return []string{}
	}
	if searchLines[len(searchLines)-1] == "" {
		searchLines = searchLines[:len(searchLines)-1]
	}
	firstLineSearch := strings.TrimSpace(searchLines[0])
	lastLineSearch := strings.TrimSpace(searchLines[len(searchLines)-1])
	searchBlockSize := len(searchLines)
	candidates := []blockCandidate{}
	for i := 0; i < len(originalLines); i++ {
		if strings.TrimSpace(originalLines[i]) != firstLineSearch {
			continue
		}
		for j := i + 2; j < len(originalLines); j++ {
			if strings.TrimSpace(originalLines[j]) == lastLineSearch {
				candidates = append(candidates, blockCandidate{i, j})
				break
			}
		}
	}
	if len(candidates) == 0 {
		return []string{}
	}
	if len(candidates) == 1 {
		candidate := candidates[0]
		actualBlockSize := candidate.endLine - candidate.startLine + 1
		similarity := 0.0
		linesToCheck := searchBlockSize - 2
		if actualBlockSize-2 < linesToCheck {
			linesToCheck = actualBlockSize - 2
		}
		if linesToCheck > 0 {
			for j := 1; j < searchBlockSize-1 && j < actualBlockSize-1; j++ {
				originalLine := strings.TrimSpace(originalLines[candidate.startLine+j])
				searchLine := strings.TrimSpace(searchLines[j])
				maxLen := utf16Length(originalLine)
				if value := utf16Length(searchLine); value > maxLen {
					maxLen = value
				}
				if maxLen == 0 {
					continue
				}
				distance := levenshtein(originalLine, searchLine)
				similarity += (1 - float64(distance)/float64(maxLen)) / float64(linesToCheck)
				if similarity >= singleCandidateSimilarityThreshold {
					break
				}
			}
		} else {
			similarity = 1.0
		}
		if similarity >= singleCandidateSimilarityThreshold {
			return []string{strings.Join(originalLines[candidate.startLine:candidate.endLine+1], "\n")}
		}
		return []string{}
	}

	var best *blockCandidate
	maxSimilarity := -1.0
	for i := range candidates {
		candidate := candidates[i]
		actualBlockSize := candidate.endLine - candidate.startLine + 1
		similarity := 0.0
		linesToCheck := searchBlockSize - 2
		if actualBlockSize-2 < linesToCheck {
			linesToCheck = actualBlockSize - 2
		}
		if linesToCheck > 0 {
			for j := 1; j < searchBlockSize-1 && j < actualBlockSize-1; j++ {
				originalLine := strings.TrimSpace(originalLines[candidate.startLine+j])
				searchLine := strings.TrimSpace(searchLines[j])
				maxLen := utf16Length(originalLine)
				if value := utf16Length(searchLine); value > maxLen {
					maxLen = value
				}
				if maxLen == 0 {
					continue
				}
				similarity += 1 - float64(levenshtein(originalLine, searchLine))/float64(maxLen)
			}
			similarity /= float64(linesToCheck)
		} else {
			similarity = 1.0
		}
		if similarity > maxSimilarity {
			maxSimilarity = similarity
			copy := candidate
			best = &copy
		}
	}
	if maxSimilarity >= multipleCandidatesSimilarityThreshold && best != nil {
		return []string{strings.Join(originalLines[best.startLine:best.endLine+1], "\n")}
	}
	return []string{}
}

// WhitespaceNormalizedReplacer matches after collapsing every run of
// whitespace to a single space.
func WhitespaceNormalizedReplacer(content string, find string) []string {
	normalizedFind := normalizeWhitespace(find)
	lines := strings.Split(content, "\n")
	out := []string{}
	for _, line := range lines {
		if normalizeWhitespace(line) == normalizedFind {
			out = append(out, line)
			continue
		}
		normalizedLine := normalizeWhitespace(line)
		if strings.Contains(normalizedLine, normalizedFind) {
			words := splitWhitespace(strings.TrimSpace(find))
			if len(words) > 0 {
				if match, ok := findWordsMatch(line, words); ok {
					out = append(out, match)
				}
			}
		}
	}
	findLines := strings.Split(find, "\n")
	if len(findLines) > 1 {
		for i := 0; i <= len(lines)-len(findLines); i++ {
			block := strings.Join(lines[i:i+len(findLines)], "\n")
			if normalizeWhitespace(block) == normalizedFind {
				out = append(out, block)
			}
		}
	}
	return out
}

// IndentationFlexibleReplacer matches after removing the common indentation
// from both the search text and the candidate block.
func IndentationFlexibleReplacer(content string, find string) []string {
	normalizedFind := removeIndentation(find)
	contentLines := strings.Split(content, "\n")
	findLines := strings.Split(find, "\n")
	out := []string{}
	for i := 0; i <= len(contentLines)-len(findLines); i++ {
		block := strings.Join(contentLines[i:i+len(findLines)], "\n")
		if removeIndentation(block) == normalizedFind {
			out = append(out, block)
		}
	}
	return out
}

// EscapeNormalizedReplacer matches after unescaping backslash sequences in the
// search text, for a model that sent an escaped string.
func EscapeNormalizedReplacer(content string, find string) []string {
	unescapedFind := unescapeEditString(find)
	out := []string{}
	if strings.Contains(content, unescapedFind) {
		out = append(out, unescapedFind)
	}
	lines := strings.Split(content, "\n")
	findLines := strings.Split(unescapedFind, "\n")
	for i := 0; i <= len(lines)-len(findLines); i++ {
		block := strings.Join(lines[i:i+len(findLines)], "\n")
		if unescapeEditString(block) == unescapedFind {
			out = append(out, block)
		}
	}
	return out
}

// MultiOccurrenceReplacer returns one candidate per exact occurrence, which
// is what lets replaceAll act on every one. It assumes find != "": the edit
// tool rejects an empty search before the ladder runs.
func MultiOccurrenceReplacer(content string, find string) []string {
	out := []string{}
	start := 0
	for {
		index := strings.Index(content[start:], find)
		if index < 0 {
			break
		}
		out = append(out, find)
		start += index + len(find)
	}
	return out
}

// TrimmedBoundaryReplacer matches the search text with its surrounding
// whitespace trimmed away.
func TrimmedBoundaryReplacer(content string, find string) []string {
	trimmedFind := strings.TrimSpace(find)
	if trimmedFind == find {
		return []string{}
	}
	out := []string{}
	if strings.Contains(content, trimmedFind) {
		out = append(out, trimmedFind)
	}
	lines := strings.Split(content, "\n")
	findLines := strings.Split(find, "\n")
	for i := 0; i <= len(lines)-len(findLines); i++ {
		block := strings.Join(lines[i:i+len(findLines)], "\n")
		if strings.TrimSpace(block) == trimmedFind {
			out = append(out, block)
		}
	}
	return out
}

// ContextAwareReplacer matches a block of the same length by its first and
// last lines when at least half of the inner lines agree.
func ContextAwareReplacer(content string, find string) []string {
	findLines := strings.Split(find, "\n")
	if len(findLines) < 3 {
		return []string{}
	}
	if findLines[len(findLines)-1] == "" {
		findLines = findLines[:len(findLines)-1]
	}
	contentLines := strings.Split(content, "\n")
	firstLine := strings.TrimSpace(findLines[0])
	lastLine := strings.TrimSpace(findLines[len(findLines)-1])
	out := []string{}
	for i := 0; i < len(contentLines); i++ {
		if strings.TrimSpace(contentLines[i]) != firstLine {
			continue
		}
		for j := i + 2; j < len(contentLines); j++ {
			if strings.TrimSpace(contentLines[j]) != lastLine {
				continue
			}
			blockLines := contentLines[i : j+1]
			if len(blockLines) == len(findLines) {
				matchingLines := 0
				totalNonEmptyLines := 0
				for k := 1; k < len(blockLines)-1; k++ {
					blockLine := strings.TrimSpace(blockLines[k])
					findLine := strings.TrimSpace(findLines[k])
					if len(blockLine) > 0 || len(findLine) > 0 {
						totalNonEmptyLines++
						if blockLine == findLine {
							matchingLines++
						}
					}
				}
				if totalNonEmptyLines == 0 || float64(matchingLines)/float64(totalNonEmptyLines) >= 0.5 {
					out = append(out, strings.Join(blockLines, "\n"))
					break
				}
			}
			break
		}
	}
	return out
}

// TrimDiff removes the common leading indentation from a unified diff's
// content lines so the model-visible diff is not dominated by nesting.
func TrimDiff(diff string) string {
	lines := strings.Split(diff, "\n")
	contentLines := []string{}
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if (line[0] == '+' || line[0] == '-' || line[0] == ' ') &&
			!strings.HasPrefix(line, "---") &&
			!strings.HasPrefix(line, "+++") {
			contentLines = append(contentLines, line)
		}
	}
	if len(contentLines) == 0 {
		return diff
	}
	minIndent := int(^uint(0) >> 1)
	for _, line := range contentLines {
		content := line[1:]
		if strings.TrimSpace(content) != "" {
			indent := leadingWhitespaceUnits(content)
			if indent < minIndent {
				minIndent = indent
			}
		}
	}
	if minIndent == int(^uint(0)>>1) || minIndent == 0 {
		return diff
	}
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if (line[0] == '+' || line[0] == '-' || line[0] == ' ') &&
			!strings.HasPrefix(line, "---") &&
			!strings.HasPrefix(line, "+++") {
			lines[i] = line[:1] + sliceUTF16Units(line[1:], minIndent)
		}
	}
	return strings.Join(lines, "\n")
}

// Replace runs the replacer ladder in order and substitutes the first unique
// match, or every match of the first successful replacer when replaceAll is
// set.
func Replace(content string, oldString string, newString string, replaceAll bool) (string, error) {
	if oldString == newString {
		return "", errors.New("No changes to apply: oldString and newString are identical.")
	}
	notFound := true
	replacers := []replacer{
		SimpleReplacer,
		LineTrimmedReplacer,
		BlockAnchorReplacer,
		WhitespaceNormalizedReplacer,
		IndentationFlexibleReplacer,
		EscapeNormalizedReplacer,
		TrimmedBoundaryReplacer,
		ContextAwareReplacer,
		MultiOccurrenceReplacer,
	}
	for _, candidateReplacer := range replacers {
		for _, search := range candidateReplacer(content, oldString) {
			index := strings.Index(content, search)
			if index < 0 {
				continue
			}
			notFound = false
			if replaceAll {
				return replaceAllExpanding(content, search, newString), nil
			}
			lastIndex := strings.LastIndex(content, search)
			if index != lastIndex {
				continue
			}
			return content[:index] + newString + content[index+len(search):], nil
		}
	}
	if notFound {
		return "", errors.New(
			"Could not find oldString in the file. It must match exactly, including whitespace, indentation, and line endings.",
		)
	}
	return "", errors.New(
		"Found multiple matches for oldString. Provide more surrounding context to make the match unique.",
	)
}

func utf16Length(value string) int {
	return len(utf16.Encode([]rune(value)))
}

// isWhitespaceRune is the whitespace set the lenient matchers normalize: the
// ASCII controls, space, no-break space, the Unicode space separators, the
// line and paragraph separators and the BOM.
func isWhitespaceRune(r rune) bool {
	switch {
	case r >= 0x0009 && r <= 0x000d:
		return true
	case r == 0x0020, r == 0x00a0, r == 0x1680, r == 0x2028, r == 0x2029,
		r == 0x202f, r == 0x205f, r == 0x3000, r == 0xfeff:
		return true
	case r >= 0x2000 && r <= 0x200a:
		return true
	default:
		return false
	}
}

func normalizeWhitespace(value string) string {
	var out strings.Builder
	inWhitespace := false
	for _, r := range value {
		if isWhitespaceRune(r) {
			if !inWhitespace {
				out.WriteByte(' ')
				inWhitespace = true
			}
			continue
		}
		out.WriteRune(r)
		inWhitespace = false
	}
	return strings.TrimSpace(out.String())
}

func splitWhitespace(value string) []string {
	if value == "" {
		return []string{""}
	}
	out := []string{}
	start := 0
	for i, r := range value {
		if !isWhitespaceRune(r) {
			continue
		}
		if start < i {
			out = append(out, value[start:i])
		}
		start = i + len(string(r))
	}
	out = append(out, value[start:])
	return out
}

func findWordsMatch(line string, words []string) (string, bool) {
	if len(words) == 1 && words[0] == "" {
		return "", true
	}
	for start := 0; start <= len(line); {
		if !strings.HasPrefix(line[start:], words[0]) {
			if start == len(line) {
				break
			}
			_, size := nextRune(line[start:])
			start += size
			continue
		}
		pos := start + len(words[0])
		ok := true
		for _, word := range words[1:] {
			before := pos
			for pos < len(line) {
				r, size := nextRune(line[pos:])
				if !isWhitespaceRune(r) {
					break
				}
				pos += size
			}
			if pos == before || !strings.HasPrefix(line[pos:], word) {
				ok = false
				break
			}
			pos += len(word)
		}
		if ok {
			return line[start:pos], true
		}
		if start == len(line) {
			break
		}
		_, size := nextRune(line[start:])
		start += size
	}
	return "", false
}

func nextRune(value string) (rune, int) {
	for _, r := range value {
		return r, len(string(r))
	}
	return 0, 0
}

func removeIndentation(value string) string {
	lines := strings.Split(value, "\n")
	minIndent := int(^uint(0) >> 1)
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := leadingWhitespaceUnits(line)
		if indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent == int(^uint(0)>>1) {
		return value
	}
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = sliceUTF16Units(line, minIndent)
		}
	}
	return strings.Join(lines, "\n")
}

// leadingWhitespaceUnits counts a line's indentation in UTF-16 code units,
// the unit sliceUTF16Units removes it in.
func leadingWhitespaceUnits(value string) int {
	count := 0
	for _, r := range value {
		if !isWhitespaceRune(r) {
			break
		}
		count += utf16Length(string(r))
	}
	return count
}

func sliceUTF16Units(value string, start int) string {
	units := utf16.Encode([]rune(value))
	if start < 0 {
		start = 0
	}
	if start > len(units) {
		start = len(units)
	}
	return string(utf16.Decode(units[start:]))
}

func unescapeEditString(value string) string {
	var out strings.Builder
	for i := 0; i < len(value); {
		if value[i] != '\\' || i+1 >= len(value) {
			r, size := nextRune(value[i:])
			out.WriteRune(r)
			i += size
			continue
		}
		next := value[i+1]
		switch next {
		case 'n':
			out.WriteByte('\n')
		case 't':
			out.WriteByte('\t')
		case 'r':
			out.WriteByte('\r')
		case '\'', '"', '`', '\\', '$':
			out.WriteByte(next)
		case '\n':
			out.WriteByte('\n')
		default:
			out.WriteByte('\\')
			out.WriteByte(next)
			i += 2
			continue
		}
		i += 2
	}
	return out.String()
}

// replaceAllExpanding replaces every occurrence of search, expanding the $&,
// $`, $' and $$ patterns in the replacement (the match, the text before it, the
// text after it, and a literal dollar).
func replaceAllExpanding(content string, search string, replacement string) string {
	if search == "" {
		return content
	}
	var out strings.Builder
	start := 0
	for {
		index := strings.Index(content[start:], search)
		if index < 0 {
			out.WriteString(content[start:])
			break
		}
		index += start
		out.WriteString(content[start:index])
		out.WriteString(expandReplacement(replacement, search, content[:index], content[index+len(search):]))
		start = index + len(search)
	}
	return out.String()
}

func expandReplacement(replacement string, match string, before string, after string) string {
	var out strings.Builder
	for i := 0; i < len(replacement); i++ {
		if replacement[i] != '$' || i+1 >= len(replacement) {
			out.WriteByte(replacement[i])
			continue
		}
		switch replacement[i+1] {
		case '$':
			out.WriteByte('$')
			i++
		case '&':
			out.WriteString(match)
			i++
		case '`':
			out.WriteString(before)
			i++
		case '\'':
			out.WriteString(after)
			i++
		default:
			out.WriteByte('$')
		}
	}
	return out.String()
}

func lineChangeCounts(oldContent string, newContent string) (int, int) {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	table := make([][]int, len(oldLines)+1)
	for i := range table {
		table[i] = make([]int, len(newLines)+1)
	}
	for i := len(oldLines) - 1; i >= 0; i-- {
		for j := len(newLines) - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	common := table[0][0]
	return len(newLines) - common, len(oldLines) - common
}
