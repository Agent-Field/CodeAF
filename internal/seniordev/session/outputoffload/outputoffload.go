//go:build !windows

// Package outputoffload keeps large tool outputs out of the context window:
// the full text is written to a file under .senior-dev/tool-output and the model
// sees a bounded extract (head, tail and diagnostic-looking lines) plus the
// path.
package outputoffload

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/seniordev/jsonutil"
)

const (
	HEAD_LINES              = 20
	TAIL_LINES              = 40
	EXTRACT_MAX_CHARS       = 6_000
	OFFLOAD_THRESHOLD_CHARS = 12_000
)

var unsafeCallIDRE = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type DistillHook func(fullOutput string) (string, error)

type ExtractRelevantOptions struct {
	FullOutputPath *string  `json:"fullOutputPath"`
	Path           *string  `json:"path"`
	MaxChars       *float64 `json:"maxChars"`
}

type OutputOffloadInput struct {
	Output           string `json:"output"`
	Workspace        string `json:"workspace"`
	ToolName         string `json:"toolName"`
	CallID           string `json:"callId"`
	SessionID        string `json:"sessionId,omitempty"`
	EscalationWanted bool   `json:"escalationWanted"`
}

type OutputOffloadOptions struct {
	Hook             DistillHook `json:"-"`
	EscalationWanted bool        `json:"escalationWanted"`
	Force            bool        `json:"-"`
}

type OutputOffloadResult struct {
	Inline           string
	OffloadPath      *string
	EscalationWanted bool
}

// MarshalJSON emits inline first and the optional offloadPath and
// escalationWanted only when they are set.
func (r OutputOffloadResult) MarshalJSON() ([]byte, error) {
	inline, err := jsonutil.Marshal(r.Inline)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(`{"inline":`)
	b.Write(inline)
	if r.OffloadPath != nil {
		path, err := jsonutil.Marshal(*r.OffloadPath)
		if err != nil {
			return nil, err
		}
		b.WriteString(`,"offloadPath":`)
		b.Write(path)
	}
	if r.EscalationWanted {
		b.WriteString(`,"escalationWanted":true`)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// OutputSink is the filesystem side effect behind offloadLargeOutput.
type OutputSink interface {
	WriteOutput(path string, output string) (string, error)
}

type DiskSink struct{}

func (DiskSink) WriteOutput(path string, output string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	// Bounded so a pathological collision space cannot livelock a
	// synchronous tool call past the run deadline.
	const maxCollisions = 100
	for collision := 1; collision <= maxCollisions; collision++ {
		candidate := path
		if collision > 1 {
			candidate = stem + "-" + strconv.Itoa(collision) + ext
		}
		file, err := os.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, writeErr := file.WriteString(output)
		closeErr := file.Close()
		if writeErr != nil {
			_ = os.Remove(candidate)
			return "", writeErr
		}
		if closeErr != nil {
			_ = os.Remove(candidate)
			return "", closeErr
		}
		return candidate, nil
	}
	return "", fmt.Errorf("outputoffload: exhausted %d collision candidates for %s", maxCollisions, path)
}

type Offloader struct {
	Sink OutputSink
}

var DefaultOffloader = Offloader{Sink: DiskSink{}}

// ExtractRelevant keeps the head, tail, and diagnostic-looking lines.
func ExtractRelevant(output string, opts *ExtractRelevantOptions) string {
	fullOutputPath := "<path>"
	maxChars := float64(EXTRACT_MAX_CHARS)
	if opts != nil {
		if opts.FullOutputPath != nil {
			fullOutputPath = *opts.FullOutputPath
		} else if opts.Path != nil {
			fullOutputPath = *opts.Path
		}
		if opts.MaxChars != nil {
			maxChars = *opts.MaxChars
		}
	}
	lines := splitCRLF(output)
	selected := make([]bool, len(lines))
	for i := 0; i < min(HEAD_LINES, len(lines)); i++ {
		selected[i] = true
	}
	for i := max(0, len(lines)-TAIL_LINES); i < len(lines); i++ {
		selected[i] = true
	}
	for i, line := range lines {
		if hasErrorText(line) {
			selected[i] = true
		}
	}

	chunks := []string{}
	seenLines := make(map[string]bool)
	cursor := 0
	for index, keep := range selected {
		if !keep {
			continue
		}
		if index > cursor {
			chunks = append(chunks, omission(index-cursor, fullOutputPath))
		}
		line := lines[index]
		if !seenLines[line] {
			chunks = append(chunks, line)
			seenLines[line] = true
		}
		cursor = index + 1
	}
	if cursor < len(lines) {
		chunks = append(chunks, omission(len(lines)-cursor, fullOutputPath))
	}
	return capExtract(strings.Join(chunks, "\n"), maxChars, fullOutputPath)
}

// OffloadLargeOutput uses the production filesystem sink. options accepts
// nil, OutputOffloadOptions (or a pointer), or a DistillHook.
func OffloadLargeOutput(input OutputOffloadInput, options any) OutputOffloadResult {
	return DefaultOffloader.OffloadLargeOutput(input, options)
}

// OffloadLargeOutput applies the offload policy with an injected sink.
func (o Offloader) OffloadLargeOutput(input OutputOffloadInput, options any) OutputOffloadResult {
	escalationRequested := input.EscalationWanted
	force := false
	switch value := options.(type) {
	case DistillHook:
		escalationRequested = true
	case func(string) (string, error):
		escalationRequested = true
	case OutputOffloadOptions:
		escalationRequested = escalationRequested || value.EscalationWanted
		force = value.Force
	case *OutputOffloadOptions:
		if value != nil {
			escalationRequested = escalationRequested || value.EscalationWanted
			force = value.Force
		}
	}

	if !force && charCount(input.Output) <= OFFLOAD_THRESHOLD_CHARS {
		return resultWithEscalation(input.Output, nil, escalationRequested, ExtractRelevant(input.Output, nil))
	}

	offloadPath := outputPathFor(input)
	sink := o.Sink
	if sink == nil {
		sink = DiskSink{}
	}
	actualPath, err := sink.WriteOutput(offloadPath, input.Output)
	if err != nil {
		return resultWithEscalation(plainTruncation(input.Output), nil, escalationRequested, ExtractRelevant(input.Output, nil))
	}
	offloadPath = actualPath

	extract := ExtractRelevant(input.Output, &ExtractRelevantOptions{FullOutputPath: &offloadPath})
	handle := "Full output saved to " + offloadPath + " — read it only if the extract is insufficient."
	return resultWithEscalation(extract+"\n\n"+handle, &offloadPath, escalationRequested, extract)
}

func outputPathFor(input OutputOffloadInput) string {
	safeCallID := unsafeCallIDRE.ReplaceAllString(input.CallID, "_")
	if safeCallID == "" {
		safeCallID = "unknown"
	}
	dir := filepath.Join(input.Workspace, ".senior-dev", "tool-output")
	if input.SessionID != "" {
		safeSessionID := unsafeCallIDRE.ReplaceAllString(input.SessionID, "_")
		if safeSessionID == "" {
			safeSessionID = "unknown"
		}
		dir = filepath.Join(dir, safeSessionID)
	}
	return filepath.Join(dir, safeCallID+".log")
}

func resultWithEscalation(inline string, path *string, requested bool, relevant string) OutputOffloadResult {
	return OutputOffloadResult{
		Inline:           inline,
		OffloadPath:      path,
		EscalationWanted: requested && !hasErrorLine(relevant),
	}
}

func hasErrorLine(output string) bool {
	for _, line := range splitCRLF(output) {
		if hasErrorText(line) {
			return true
		}
	}
	return false
}

func hasErrorText(line string) bool {
	lower := asciiLower(line)
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "fail") ||
		strings.Contains(lower, "assert") ||
		strings.Contains(lower, "exception") ||
		strings.Contains(lower, "panic") ||
		strings.Contains(line, "✗")
}

func omission(count int, fullOutputPath string) string {
	return "[... " + strconv.Itoa(count) + " lines omitted — full output at " + fullOutputPath + "]"
}

func capExtract(text string, maxChars float64, fullOutputPath string) string {
	limit := int(maxChars)
	if charCount(text) <= limit {
		return text
	}
	marker := "\n[... extract capped at " + strconv.Itoa(limit) + " chars — full output at " + fullOutputPath + "]"
	if charCount(marker) >= limit {
		return firstChars(marker, limit)
	}
	return firstChars(text, limit-charCount(marker)) + marker
}

func plainTruncation(output string) string {
	note := "[... full output could not be saved; showing a plain truncation]"
	if charCount(output) <= EXTRACT_MAX_CHARS {
		return output
	}
	if charCount(note) >= EXTRACT_MAX_CHARS {
		return firstChars(note, EXTRACT_MAX_CHARS)
	}
	return firstChars(output, EXTRACT_MAX_CHARS-charCount(note)-1) + "\n" + note
}

func splitCRLF(s string) []string {
	lines := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '\n' {
			continue
		}
		end := i
		if end > start && s[end-1] == '\r' {
			end--
		}
		lines = append(lines, s[start:end])
		start = i + 1
	}
	return append(lines, s[start:])
}

// charCount is the length of s in characters (runes).
func charCount(s string) int { return utf8.RuneCountInString(s) }

// firstChars returns the first n characters of s without splitting a
// multi-byte character.
func firstChars(s string, n int) string {
	if n <= 0 {
		return ""
	}
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

func asciiLower(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b.WriteByte(c)
	}
	return b.String()
}
