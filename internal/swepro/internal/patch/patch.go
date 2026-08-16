// Package patch ports swe-pro/src/patch/index.ts:1-684 at commit 3b25a1a.
// It implements the stripped-down patch parser, fuzzy chunk matching, direct
// filesystem application, and shell-command detection used by apply_patch.
package patch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/jscompat"
)

const (
	MaybeBody            = "Body"
	MaybeShellParseError = "ShellParseError"
	MaybePatchParseError = "PatchParseError"
	MaybeNotApplyPatch   = "NotApplyPatch"

	VerifiedBody             = "Body"
	VerifiedShellParseError  = "ShellParseError"
	VerifiedCorrectnessError = "CorrectnessError"
	VerifiedNotApplyPatch    = "NotApplyPatch"

	ErrorParseError          = "ParseError"
	ErrorIOError             = "IoError"
	ErrorComputeReplacements = "ComputeReplacements"
	ErrorImplicitInvocation  = "ImplicitInvocation"
)

// UpdateFileChunk is the parser's line-oriented update block.
type UpdateFileChunk struct {
	OldLines      []string `json:"old_lines"`
	NewLines      []string `json:"new_lines"`
	ChangeContext string   `json:"change_context,omitempty"`
	IsEndOfFile   bool     `json:"is_end_of_file,omitempty"`
}

// Hunk is one add, delete, or update section.
type Hunk struct {
	Type     string
	Path     string
	Contents string
	MovePath string
	Chunks   []UpdateFileChunk
}

func (h Hunk) MarshalJSON() ([]byte, error) {
	switch h.Type {
	case "add":
		return jscompat.Stringify(struct {
			Type     string `json:"type"`
			Path     string `json:"path"`
			Contents string `json:"contents"`
		}{h.Type, h.Path, h.Contents})
	case "delete":
		return jscompat.Stringify(struct {
			Type string `json:"type"`
			Path string `json:"path"`
		}{h.Type, h.Path})
	case "update":
		if h.MovePath != "" {
			return jscompat.Stringify(struct {
				Type     string            `json:"type"`
				Path     string            `json:"path"`
				MovePath string            `json:"move_path"`
				Chunks   []UpdateFileChunk `json:"chunks"`
			}{h.Type, h.Path, h.MovePath, nonNilChunks(h.Chunks)})
		}
		return jscompat.Stringify(struct {
			Type   string            `json:"type"`
			Path   string            `json:"path"`
			Chunks []UpdateFileChunk `json:"chunks"`
		}{h.Type, h.Path, nonNilChunks(h.Chunks)})
	default:
		return nil, fmt.Errorf("unknown hunk type %q", h.Type)
	}
}

func nonNilChunks(chunks []UpdateFileChunk) []UpdateFileChunk {
	if chunks == nil {
		return []UpdateFileChunk{}
	}
	return chunks
}

// ParseResult is parsePatch's object envelope.
type ParseResult struct {
	Hunks []Hunk `json:"hunks"`
}

// ApplyPatchArgs is maybeParseApplyPatch's parsed body.
type ApplyPatchArgs struct {
	Patch   string `json:"patch"`
	Hunks   []Hunk `json:"hunks"`
	Workdir string `json:"workdir,omitempty"`
}

// MaybeResult is the discriminated result of MaybeParseApplyPatch.
type MaybeResult struct {
	Type string
	Args *ApplyPatchArgs
	Err  error
}

// ApplyPatchFileUpdate is deriveNewContentsFromChunks' result.
type ApplyPatchFileUpdate struct {
	UnifiedDiff string `json:"unified_diff"`
	Content     string `json:"content"`
	BOM         bool   `json:"bom"`
}

// AffectedPaths records filesystem application results.
type AffectedPaths struct {
	Added    []string `json:"added"`
	Modified []string `json:"modified"`
	Deleted  []string `json:"deleted"`
}

// ApplyPatchFileChange mirrors the maybe-verified change union.
type ApplyPatchFileChange struct {
	Type        string `json:"type"`
	Content     string `json:"content,omitempty"`
	UnifiedDiff string `json:"unified_diff,omitempty"`
	MovePath    string `json:"move_path,omitempty"`
	NewContent  string `json:"new_content,omitempty"`
}

// ApplyPatchAction is the verified patch preview.
type ApplyPatchAction struct {
	Changes *jscompat.OrderedMap[string, ApplyPatchFileChange]
	Patch   string
	CWD     string
}

// VerifiedResult is the discriminated result of MaybeParseApplyPatchVerified.
type VerifiedResult struct {
	Type   string
	Action *ApplyPatchAction
	Err    error
}

// ParsePatch parses the stripped patch envelope.
func ParsePatch(patchText string) (ParseResult, error) {
	cleaned := stripHeredoc(jscompat.Trim(patchText))
	lines := strings.Split(cleaned, "\n")
	beginIndex := -1
	endIndex := -1
	for i, line := range lines {
		if beginIndex == -1 && jscompat.Trim(line) == "*** Begin Patch" {
			beginIndex = i
		}
		if endIndex == -1 && jscompat.Trim(line) == "*** End Patch" {
			endIndex = i
		}
	}
	if beginIndex == -1 || endIndex == -1 || beginIndex >= endIndex {
		return ParseResult{}, errors.New("Invalid patch format: missing Begin/End markers")
	}

	hunks := []Hunk{}
	for i := beginIndex + 1; i < endIndex; {
		path, movePath, next, kind, ok := parsePatchHeader(lines, i)
		if !ok {
			i++
			continue
		}
		switch kind {
		case "add":
			content, after := parseAddFileContent(lines, next)
			hunks = append(hunks, Hunk{Type: "add", Path: path, Contents: content})
			i = after
		case "delete":
			hunks = append(hunks, Hunk{Type: "delete", Path: path})
			i = next
		case "update":
			chunks, after := parseUpdateFileChunks(lines, next)
			hunks = append(hunks, Hunk{
				Type:     "update",
				Path:     path,
				MovePath: movePath,
				Chunks:   chunks,
			})
			i = after
		default:
			i++
		}
	}
	return ParseResult{Hunks: hunks}, nil
}

func parsePatchHeader(lines []string, index int) (path string, movePath string, next int, kind string, ok bool) {
	line := lines[index]
	for _, item := range []struct {
		prefix string
		kind   string
	}{
		{"*** Add File:", "add"},
		{"*** Delete File:", "delete"},
		{"*** Update File:", "update"},
	} {
		if !strings.HasPrefix(line, item.prefix) {
			continue
		}
		path = jscompat.Trim(line[len(item.prefix):])
		if path == "" {
			return "", "", 0, "", false
		}
		next = index + 1
		if item.kind == "update" && next < len(lines) && strings.HasPrefix(lines[next], "*** Move to:") {
			movePath = jscompat.Trim(lines[next][len("*** Move to:"):])
			next++
		}
		return path, movePath, next, item.kind, true
	}
	return "", "", 0, "", false
}

func parseUpdateFileChunks(lines []string, start int) ([]UpdateFileChunk, int) {
	chunks := []UpdateFileChunk{}
	i := start
	for i < len(lines) && !strings.HasPrefix(lines[i], "***") {
		if !strings.HasPrefix(lines[i], "@@") {
			i++
			continue
		}
		contextLine := jscompat.Trim(lines[i][2:])
		i++
		oldLines := []string{}
		newLines := []string{}
		isEndOfFile := false
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@") && !strings.HasPrefix(lines[i], "***") {
			changeLine := lines[i]
			// This is unreachable because the loop condition excludes every
			// "***" line. It remains to mirror the TS branch exactly.
			if changeLine == "*** End of File" {
				isEndOfFile = true
				i++
				break
			}
			switch {
			case strings.HasPrefix(changeLine, " "):
				oldLines = append(oldLines, changeLine[1:])
				newLines = append(newLines, changeLine[1:])
			case strings.HasPrefix(changeLine, "-"):
				oldLines = append(oldLines, changeLine[1:])
			case strings.HasPrefix(changeLine, "+"):
				newLines = append(newLines, changeLine[1:])
			}
			i++
		}
		chunks = append(chunks, UpdateFileChunk{
			OldLines:      oldLines,
			NewLines:      newLines,
			ChangeContext: contextLine,
			IsEndOfFile:   isEndOfFile,
		})
	}
	return chunks, i
}

func parseAddFileContent(lines []string, start int) (string, int) {
	var content strings.Builder
	i := start
	for i < len(lines) && !strings.HasPrefix(lines[i], "***") {
		if strings.HasPrefix(lines[i], "+") {
			content.WriteString(lines[i][1:])
			content.WriteByte('\n')
		}
		i++
	}
	out := content.String()
	out = strings.TrimSuffix(out, "\n")
	return out, i
}

func stripHeredoc(input string) string {
	headerEnd := strings.IndexByte(input, '\n')
	if headerEnd < 0 {
		return input
	}
	header := input[:headerEnd]
	rest := input[headerEnd+1:]
	if strings.HasPrefix(header, "cat") {
		after := header[len("cat"):]
		if after == "" || !isJSWhitespaceRune(firstRune(after)) {
			return input
		}
		header = strings.TrimLeftFunc(after, isJSWhitespaceRune)
	}
	if !strings.HasPrefix(header, "<<") {
		return input
	}
	header = header[2:]
	if len(header) > 0 && (header[0] == '\'' || header[0] == '"') {
		header = header[1:]
	}
	end := 0
	for end < len(header) && isASCIIWord(header[end]) {
		end++
	}
	if end == 0 {
		return input
	}
	delimiter := header[:end]
	header = header[end:]
	if len(header) > 0 && (header[0] == '\'' || header[0] == '"') {
		header = header[1:]
	}
	if jscompat.Trim(header) != "" {
		return input
	}
	suffix := "\n" + delimiter
	position := strings.Index(rest, suffix)
	for position >= 0 {
		after := rest[position+len(suffix):]
		if jscompat.Trim(after) == "" {
			return rest[:position]
		}
		next := strings.Index(rest[position+1:], suffix)
		if next < 0 {
			break
		}
		position += next + 1
	}
	return input
}

func firstRune(value string) rune {
	for _, r := range value {
		return r
	}
	return 0
}

func isASCIIWord(value byte) bool {
	return value >= 'a' && value <= 'z' ||
		value >= 'A' && value <= 'Z' ||
		value >= '0' && value <= '9' ||
		value == '_'
}

// MaybeParseApplyPatch detects direct and bash-heredoc invocations.
func MaybeParseApplyPatch(argv []string) MaybeResult {
	if len(argv) == 2 && (argv[0] == "apply_patch" || argv[0] == "applypatch") {
		parsed, err := ParsePatch(argv[1])
		if err != nil {
			return MaybeResult{Type: MaybePatchParseError, Err: err}
		}
		return MaybeResult{
			Type: MaybeBody,
			Args: &ApplyPatchArgs{Patch: argv[1], Hunks: parsed.Hunks},
		}
	}
	if len(argv) == 3 && argv[0] == "bash" && argv[1] == "-lc" {
		if content, ok := extractApplyPatchHeredoc(argv[2]); ok {
			parsed, err := ParsePatch(content)
			if err != nil {
				return MaybeResult{Type: MaybePatchParseError, Err: err}
			}
			return MaybeResult{
				Type: MaybeBody,
				Args: &ApplyPatchArgs{Patch: content, Hunks: parsed.Hunks},
			}
		}
	}
	return MaybeResult{Type: MaybeNotApplyPatch}
}

func extractApplyPatchHeredoc(script string) (string, bool) {
	for start := 0; start < len(script); {
		index := strings.Index(script[start:], "apply_patch")
		if index < 0 {
			return "", false
		}
		index += start + len("apply_patch")
		for index < len(script) && isJSWhitespaceRune(firstRune(script[index:])) {
			_, size := runeAt(script[index:])
			index += size
		}
		if !strings.HasPrefix(script[index:], "<<") {
			start = index
			continue
		}
		index += 2
		if index >= len(script) || (script[index] != '\'' && script[index] != '"') {
			return "", false
		}
		index++
		delimiterStart := index
		for index < len(script) && isASCIIWord(script[index]) {
			index++
		}
		if delimiterStart == index {
			return "", false
		}
		delimiter := script[delimiterStart:index]
		if index >= len(script) || (script[index] != '\'' && script[index] != '"') {
			return "", false
		}
		index++
		for index < len(script) && script[index] != '\n' {
			if !isJSWhitespaceRune(firstRune(script[index:])) {
				return "", false
			}
			_, size := runeAt(script[index:])
			index += size
		}
		if index >= len(script) {
			return "", false
		}
		bodyStart := index + 1
		endMarker := "\n" + delimiter
		bodyEnd := strings.Index(script[bodyStart:], endMarker)
		if bodyEnd < 0 {
			return "", false
		}
		return script[bodyStart : bodyStart+bodyEnd], true
	}
	return "", false
}

func runeAt(value string) (rune, int) {
	for _, r := range value {
		return r, len(string(r))
	}
	return 0, 0
}

// DeriveNewContentsFromChunks reads filePath and applies update chunks.
func DeriveNewContentsFromChunks(filePath string, chunks []UpdateFileChunk) (ApplyPatchFileUpdate, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ApplyPatchFileUpdate{}, fmt.Errorf("Failed to read file %s: %v", filePath, err)
	}
	bom, originalText := splitBOM(strings.ToValidUTF8(string(data), "\uFFFD"))
	originalLines := strings.Split(originalText, "\n")
	if len(originalLines) > 0 && originalLines[len(originalLines)-1] == "" {
		originalLines = originalLines[:len(originalLines)-1]
	}
	replacements, err := computeReplacements(originalLines, filePath, chunks)
	if err != nil {
		return ApplyPatchFileUpdate{}, err
	}
	newLines := applyReplacements(originalLines, replacements)
	if len(newLines) == 0 || newLines[len(newLines)-1] != "" {
		newLines = append(newLines, "")
	}
	nextBOM, newContent := splitBOM(strings.Join(newLines, "\n"))
	return ApplyPatchFileUpdate{
		UnifiedDiff: generateUnifiedDiff(originalText, newContent),
		Content:     newContent,
		BOM:         bom || nextBOM,
	}, nil
}

type replacement struct {
	start      int
	oldLength  int
	newSegment []string
}

func computeReplacements(originalLines []string, filePath string, chunks []UpdateFileChunk) ([]replacement, error) {
	replacements := []replacement{}
	lineIndex := 0
	for _, chunk := range chunks {
		if chunk.ChangeContext != "" {
			contextIndex := seekSequence(originalLines, []string{chunk.ChangeContext}, lineIndex, false)
			if contextIndex == -1 {
				return nil, fmt.Errorf("Failed to find context '%s' in %s", chunk.ChangeContext, filePath)
			}
			lineIndex = contextIndex + 1
		}
		if len(chunk.OldLines) == 0 {
			insertionIndex := len(originalLines)
			if len(originalLines) > 0 && originalLines[len(originalLines)-1] == "" {
				insertionIndex--
			}
			replacements = append(replacements, replacement{
				start: insertionIndex, oldLength: 0, newSegment: append([]string(nil), chunk.NewLines...),
			})
			continue
		}
		pattern := append([]string(nil), chunk.OldLines...)
		newSlice := append([]string(nil), chunk.NewLines...)
		found := seekSequence(originalLines, pattern, lineIndex, chunk.IsEndOfFile)
		if found == -1 && len(pattern) > 0 && pattern[len(pattern)-1] == "" {
			pattern = pattern[:len(pattern)-1]
			if len(newSlice) > 0 && newSlice[len(newSlice)-1] == "" {
				newSlice = newSlice[:len(newSlice)-1]
			}
			found = seekSequence(originalLines, pattern, lineIndex, chunk.IsEndOfFile)
		}
		if found == -1 {
			return nil, fmt.Errorf(
				"Failed to find expected lines in %s:\n%s",
				filePath,
				strings.Join(chunk.OldLines, "\n"),
			)
		}
		replacements = append(replacements, replacement{
			start: found, oldLength: len(pattern), newSegment: newSlice,
		})
		lineIndex = found + len(pattern)
	}
	sort.SliceStable(replacements, func(i, j int) bool {
		return replacements[i].start < replacements[j].start
	})
	return replacements, nil
}

func applyReplacements(lines []string, replacements []replacement) []string {
	result := append([]string(nil), lines...)
	for i := len(replacements) - 1; i >= 0; i-- {
		item := replacements[i]
		before := append([]string(nil), result[:item.start]...)
		after := append([]string(nil), result[item.start+item.oldLength:]...)
		result = append(before, item.newSegment...)
		result = append(result, after...)
	}
	return result
}

type comparator func(string, string) bool

func tryMatch(lines []string, pattern []string, start int, compare comparator, eof bool) int {
	if eof {
		fromEnd := len(lines) - len(pattern)
		if fromEnd >= start && sequenceMatches(lines, pattern, fromEnd, compare) {
			return fromEnd
		}
	}
	for i := start; i <= len(lines)-len(pattern); i++ {
		if sequenceMatches(lines, pattern, i, compare) {
			return i
		}
	}
	return -1
}

func sequenceMatches(lines []string, pattern []string, start int, compare comparator) bool {
	for j := range pattern {
		if !compare(lines[start+j], pattern[j]) {
			return false
		}
	}
	return true
}

func seekSequence(lines []string, pattern []string, start int, eof bool) int {
	if len(pattern) == 0 {
		return -1
	}
	if found := tryMatch(lines, pattern, start, func(a, b string) bool { return a == b }, eof); found != -1 {
		return found
	}
	if found := tryMatch(lines, pattern, start, func(a, b string) bool {
		return trimEnd(a) == trimEnd(b)
	}, eof); found != -1 {
		return found
	}
	if found := tryMatch(lines, pattern, start, func(a, b string) bool {
		return jscompat.Trim(a) == jscompat.Trim(b)
	}, eof); found != -1 {
		return found
	}
	return tryMatch(lines, pattern, start, func(a, b string) bool {
		return normalizeUnicode(jscompat.Trim(a)) == normalizeUnicode(jscompat.Trim(b))
	}, eof)
}

func trimEnd(value string) string {
	return strings.TrimRightFunc(value, isJSWhitespaceRune)
}

func normalizeUnicode(value string) string {
	var out strings.Builder
	for _, r := range value {
		switch {
		case r >= 0x2018 && r <= 0x201b:
			out.WriteByte('\'')
		case r >= 0x201c && r <= 0x201f:
			out.WriteByte('"')
		case r >= 0x2010 && r <= 0x2015:
			out.WriteByte('-')
		case r == 0x2026:
			out.WriteString("...")
		case r == 0x00a0:
			out.WriteByte(' ')
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

type diffLine struct {
	text    string
	newline bool
}

type diffOperation struct {
	kind byte
	line diffLine
}

type lineMatch struct {
	old int
	new int
}

const maxLineDiffCells int64 = 8_000_000

func contentLines(content string) []diffLine {
	if content == "" {
		return nil
	}
	parts := strings.SplitAfter(content, "\n")
	lines := make([]diffLine, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		newline := strings.HasSuffix(part, "\n")
		lines = append(lines, diffLine{text: strings.TrimSuffix(part, "\n"), newline: newline})
	}
	return lines
}

func equalDiffLine(left, right diffLine) bool {
	return left.text == right.text && left.newline == right.newline
}

func lineOperations(oldContent, newContent string) []diffOperation {
	oldLines, newLines := contentLines(oldContent), contentLines(newContent)
	prefix := 0
	for prefix < len(oldLines) && prefix < len(newLines) &&
		equalDiffLine(oldLines[prefix], newLines[prefix]) {
		prefix++
	}
	suffix := 0
	for suffix < len(oldLines)-prefix && suffix < len(newLines)-prefix &&
		equalDiffLine(oldLines[len(oldLines)-suffix-1], newLines[len(newLines)-suffix-1]) {
		suffix++
	}
	operations := make([]diffOperation, 0, len(oldLines)+len(newLines))
	for _, line := range oldLines[:prefix] {
		operations = append(operations, diffOperation{kind: ' ', line: line})
	}
	oldMiddle := oldLines[prefix : len(oldLines)-suffix]
	newMiddle := newLines[prefix : len(newLines)-suffix]
	matches, ok := linearLCSMatches(oldMiddle, newMiddle)
	if !ok {
		for _, line := range oldMiddle {
			operations = append(operations, diffOperation{kind: '-', line: line})
		}
		for _, line := range newMiddle {
			operations = append(operations, diffOperation{kind: '+', line: line})
		}
	} else {
		oldCursor, newCursor := 0, 0
		for _, match := range matches {
			for oldCursor < match.old {
				operations = append(operations, diffOperation{kind: '-', line: oldMiddle[oldCursor]})
				oldCursor++
			}
			for newCursor < match.new {
				operations = append(operations, diffOperation{kind: '+', line: newMiddle[newCursor]})
				newCursor++
			}
			operations = append(operations, diffOperation{kind: ' ', line: oldMiddle[match.old]})
			oldCursor, newCursor = match.old+1, match.new+1
		}
		for oldCursor < len(oldMiddle) {
			operations = append(operations, diffOperation{kind: '-', line: oldMiddle[oldCursor]})
			oldCursor++
		}
		for newCursor < len(newMiddle) {
			operations = append(operations, diffOperation{kind: '+', line: newMiddle[newCursor]})
			newCursor++
		}
	}
	for _, line := range oldLines[len(oldLines)-suffix:] {
		operations = append(operations, diffOperation{kind: ' ', line: line})
	}
	return operations
}

func linearLCSMatches(oldLines, newLines []diffLine) ([]lineMatch, bool) {
	budget := maxLineDiffCells
	matches := make([]lineMatch, 0)
	if len(newLines) <= len(oldLines) {
		if !appendLCSMatches(oldLines, newLines, 0, 0, false, &budget, &matches) {
			return nil, false
		}
	} else if !appendLCSMatches(newLines, oldLines, 0, 0, true, &budget, &matches) {
		return nil, false
	}
	return matches, true
}

// appendLCSMatches is Hirschberg's linear-space LCS. The work budget mirrors
// diff's abortable max-edit/timeout behavior by falling back to a whole-range
// replacement before pathological inputs consume unbounded CPU.
func appendLCSMatches(
	left, right []diffLine,
	leftOffset, rightOffset int,
	swapped bool,
	budget *int64,
	matches *[]lineMatch,
) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	if len(left) == 1 {
		if int64(len(right)) > *budget {
			return false
		}
		*budget -= int64(len(right))
		for index := range right {
			if !equalDiffLine(left[0], right[index]) {
				continue
			}
			if swapped {
				*matches = append(*matches, lineMatch{old: rightOffset + index, new: leftOffset})
			} else {
				*matches = append(*matches, lineMatch{old: leftOffset, new: rightOffset + index})
			}
			return true
		}
		return true
	}
	cells := int64(len(left)) * int64(len(right))
	if cells > *budget/2 {
		return false
	}
	middle := len(left) / 2
	forward := lcsPrefixLengths(left[:middle], right)
	backward := lcsSuffixLengths(left[middle:], right)
	*budget -= cells * 2
	split := 0
	best := -1
	for index := 0; index <= len(right); index++ {
		value := forward[index] + backward[index]
		if value > best {
			best = value
			split = index
		}
	}
	return appendLCSMatches(
		left[:middle], right[:split], leftOffset, rightOffset, swapped, budget, matches,
	) && appendLCSMatches(
		left[middle:], right[split:], leftOffset+middle, rightOffset+split, swapped, budget, matches,
	)
}

func lcsPrefixLengths(left, right []diffLine) []int {
	previous, current := make([]int, len(right)+1), make([]int, len(right)+1)
	for _, leftLine := range left {
		for index, rightLine := range right {
			if equalDiffLine(leftLine, rightLine) {
				current[index+1] = previous[index] + 1
			} else if previous[index+1] >= current[index] {
				current[index+1] = previous[index+1]
			} else {
				current[index+1] = current[index]
			}
		}
		previous, current = current, previous
		clear(current)
	}
	return previous
}

func lcsSuffixLengths(left, right []diffLine) []int {
	previous, current := make([]int, len(right)+1), make([]int, len(right)+1)
	for leftIndex := len(left) - 1; leftIndex >= 0; leftIndex-- {
		for rightIndex := len(right) - 1; rightIndex >= 0; rightIndex-- {
			if equalDiffLine(left[leftIndex], right[rightIndex]) {
				current[rightIndex] = previous[rightIndex+1] + 1
			} else if previous[rightIndex] >= current[rightIndex+1] {
				current[rightIndex] = previous[rightIndex]
			} else {
				current[rightIndex] = current[rightIndex+1]
			}
		}
		previous, current = current, previous
		clear(current)
	}
	return previous
}

func generateUnifiedDiff(oldContent string, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	operations := lineOperations(oldContent, newContent)
	oldBefore, newBefore := make([]int, len(operations)+1), make([]int, len(operations)+1)
	for i, operation := range operations {
		oldBefore[i+1], newBefore[i+1] = oldBefore[i], newBefore[i]
		if operation.kind != '+' {
			oldBefore[i+1]++
		}
		if operation.kind != '-' {
			newBefore[i+1]++
		}
	}
	var diff strings.Builder
	for cursor := 0; cursor < len(operations); {
		first := cursor
		for first < len(operations) && operations[first].kind == ' ' {
			first++
		}
		if first == len(operations) {
			break
		}
		start := first
		for count := 0; start > 0 && count < 4; count++ {
			start--
		}
		lastChange := first
		for scan := first + 1; scan < len(operations); {
			next := scan
			for next < len(operations) && operations[next].kind == ' ' {
				next++
			}
			if next == len(operations) || next-lastChange-1 > 8 {
				break
			}
			lastChange = next
			scan = next + 1
		}
		end := lastChange + 1
		for count := 0; end < len(operations) && operations[end].kind == ' ' && count < 4; count++ {
			end++
		}
		oldCount, newCount := oldBefore[end]-oldBefore[start], newBefore[end]-newBefore[start]
		oldStart, newStart := oldBefore[start]+1, newBefore[start]+1
		if oldCount == 0 {
			oldStart = oldBefore[start]
		}
		if newCount == 0 {
			newStart = newBefore[start]
		}
		fmt.Fprintf(&diff, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
		for _, operation := range operations[start:end] {
			diff.WriteByte(operation.kind)
			diff.WriteString(operation.line.text)
			diff.WriteByte('\n')
			if !operation.line.newline {
				diff.WriteString("\\ No newline at end of file\n")
			}
		}
		cursor = end
	}
	return diff.String()
}

// GenerateTwoFilesPatch formats the line diff like the TypeScript diff package.
func GenerateTwoFilesPatch(filePath, oldContent, newContent string) string {
	diff := generateUnifiedDiff(oldContent, newContent)
	return "Index: " + filePath + "\n" +
		"===================================================================\n" +
		"--- " + filePath + "\n" +
		"+++ " + filePath + "\n" + diff
}

// GenerateUnifiedDiff exposes the patch package's mutation preview to live
// edit/write tools.
func GenerateUnifiedDiff(oldContent string, newContent string) string {
	return generateUnifiedDiff(oldContent, newContent)
}

// ApplyHunksToFiles applies already-parsed hunks to their literal paths.
func ApplyHunksToFiles(hunks []Hunk) (AffectedPaths, error) {
	if len(hunks) == 0 {
		return AffectedPaths{}, errors.New("No files were modified.")
	}
	out := AffectedPaths{Added: []string{}, Modified: []string{}, Deleted: []string{}}
	for _, hunk := range hunks {
		switch hunk.Type {
		case "add":
			dir := filepath.Dir(hunk.Path)
			if dir != "." && dir != string(filepath.Separator) {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return AffectedPaths{}, err
				}
			}
			if err := os.WriteFile(hunk.Path, []byte(hunk.Contents), 0o644); err != nil {
				return AffectedPaths{}, err
			}
			out.Added = append(out.Added, hunk.Path)
		case "delete":
			if err := os.Remove(hunk.Path); err != nil {
				return AffectedPaths{}, err
			}
			out.Deleted = append(out.Deleted, hunk.Path)
		case "update":
			update, err := DeriveNewContentsFromChunks(hunk.Path, hunk.Chunks)
			if err != nil {
				return AffectedPaths{}, err
			}
			target := hunk.Path
			if hunk.MovePath != "" {
				target = hunk.MovePath
				dir := filepath.Dir(target)
				if dir != "." && dir != string(filepath.Separator) {
					if err := os.MkdirAll(dir, 0o755); err != nil {
						return AffectedPaths{}, err
					}
				}
			}
			if err := os.WriteFile(target, []byte(joinBOM(update.Content, update.BOM)), 0o644); err != nil {
				return AffectedPaths{}, err
			}
			if hunk.MovePath != "" {
				if err := os.Remove(hunk.Path); err != nil {
					return AffectedPaths{}, err
				}
			}
			out.Modified = append(out.Modified, target)
		}
	}
	return out, nil
}

// ApplyPatch parses and applies patchText.
func ApplyPatch(patchText string) (AffectedPaths, error) {
	parsed, err := ParsePatch(patchText)
	if err != nil {
		return AffectedPaths{}, err
	}
	return ApplyHunksToFiles(parsed.Hunks)
}

// MaybeParseApplyPatchVerified previews a detected patch against cwd.
func MaybeParseApplyPatchVerified(argv []string, cwd string) VerifiedResult {
	if len(argv) == 1 {
		if _, err := ParsePatch(argv[0]); err == nil {
			return VerifiedResult{Type: VerifiedCorrectnessError, Err: errors.New(ErrorImplicitInvocation)}
		}
	}
	result := MaybeParseApplyPatch(argv)
	switch result.Type {
	case MaybeBody:
		effectiveCWD := cwd
		if result.Args.Workdir != "" {
			effectiveCWD = filepath.Join(cwd, result.Args.Workdir)
		}
		effectiveCWD = filepath.Clean(effectiveCWD)
		changes := jscompat.NewOrderedMap[string, ApplyPatchFileChange]()
		for _, hunk := range result.Args.Hunks {
			targetPath := hunk.Path
			if hunk.Type == "update" && hunk.MovePath != "" {
				targetPath = hunk.MovePath
			}
			resolvedPath := resolve(effectiveCWD, targetPath)
			switch hunk.Type {
			case "add":
				changes.Set(resolvedPath, ApplyPatchFileChange{Type: "add", Content: hunk.Contents})
			case "delete":
				deletePath := resolve(effectiveCWD, hunk.Path)
				content, err := os.ReadFile(deletePath)
				if err != nil {
					return VerifiedResult{
						Type: VerifiedCorrectnessError,
						Err:  fmt.Errorf("Failed to read file for deletion: %s", deletePath),
					}
				}
				changes.Set(resolvedPath, ApplyPatchFileChange{Type: "delete", Content: string(content)})
			case "update":
				updatePath := resolve(effectiveCWD, hunk.Path)
				update, err := DeriveNewContentsFromChunks(updatePath, hunk.Chunks)
				if err != nil {
					return VerifiedResult{Type: VerifiedCorrectnessError, Err: err}
				}
				changes.Set(resolvedPath, ApplyPatchFileChange{
					Type:        "update",
					UnifiedDiff: update.UnifiedDiff,
					MovePath:    optionalResolved(effectiveCWD, hunk.MovePath),
					NewContent:  update.Content,
				})
			}
		}
		return VerifiedResult{
			Type: VerifiedBody,
			Action: &ApplyPatchAction{
				Changes: changes,
				Patch:   result.Args.Patch,
				CWD:     effectiveCWD,
			},
		}
	case MaybePatchParseError:
		return VerifiedResult{Type: VerifiedCorrectnessError, Err: result.Err}
	default:
		return VerifiedResult{Type: VerifiedNotApplyPatch}
	}
}

func resolve(cwd string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(cwd, path))
}

func optionalResolved(cwd string, path string) string {
	if path == "" {
		return ""
	}
	return resolve(cwd, path)
}

func splitBOM(value string) (bool, string) {
	if strings.HasPrefix(value, "\ufeff") {
		return true, value[len("\ufeff"):]
	}
	return false, value
}

func joinBOM(value string, bom bool) string {
	_, value = splitBOM(value)
	if bom {
		return "\ufeff" + value
	}
	return value
}

func isJSWhitespaceRune(r rune) bool {
	if r >= 0x0009 && r <= 0x000d {
		return true
	}
	if r >= 0x2000 && r <= 0x200a {
		return true
	}
	switch r {
	case 0x0020, 0x00a0, 0x1680, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	default:
		return unicode.IsSpace(r) && false
	}
}

// Ensure the custom Hunk marshaler remains compatible with encoding/json
// callers in addition to jscompat.Stringify.
var _ json.Marshaler = Hunk{}
