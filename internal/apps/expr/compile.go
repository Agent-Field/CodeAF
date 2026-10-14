package expr

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/google/cel-go/cel"
)

const maxSourceBytes = 4096

// Compile rewrites duration tokens while retaining positions in the original source.
func Compile(env *cel.Env, source string) (*cel.Ast, error) {
	if len(source) > maxSourceBytes {
		return nil, Diagnostic(source, fmt.Errorf("expression exceeds %d bytes", maxSourceBytes))
	}
	rewritten, positions := durations(source)
	ast, issues := env.Compile(rewritten)
	if issues.Err() == nil {
		return ast, nil
	}
	issue := issues.Errors()[0]
	offset := 0
	for line := 1; line < issue.Location.Line() && offset < len(rewritten); offset++ {
		if rewritten[offset] == '\n' {
			line++
		}
	}
	offset += issue.Location.Column()
	offset = min(offset, len(positions)-1)
	original := positions[max(0, offset)]
	line := 1 + strings.Count(source[:original], "\n")
	column := len([]rune(source[strings.LastIndex(source[:original], "\n")+1:original])) + 1
	return nil, fmt.Errorf("expression %q at %d:%d: %s; corrected example: timestamp(\"2026-10-13T00:00:00Z\") >= now - days(7)", source, line, column, issue.Message)
}

// Diagnostic keeps evaluation failures actionable when CEL supplies no source location.
func Diagnostic(source string, err error) error {
	return fmt.Errorf("expression %q at 1:1: %w; corrected example: timestamp(\"2026-10-13T00:00:00Z\") >= now - days(7)", source, err)
}

func durations(source string) (string, []int) {
	var out strings.Builder
	positions := []int{}
	write := func(text string, start int, literal bool) {
		out.WriteString(text)
		for j := range len(text) {
			at := start
			if literal {
				at += j
			}
			positions = append(positions, at)
		}
	}
	for i := 0; i < len(source); {
		end := literalEnd(source, i)
		if end > i {
			write(source[i:end], i, true)
			i = end
			continue
		}

		if text, end := referenceToken(source, i); end > i {
			write(text, i, false)
			i = end
			continue
		}
		if text, end := durationToken(source, i); end > i {
			write(text, i, false)
			i = end
			continue
		}

		write(source[i:i+1], i, true)
		i++
	}
	positions = append(positions, len(source))
	return out.String(), positions
}
func referenceToken(source string, start int) (string, int) {
	if source[start] != '$' {
		return "", start
	}
	end := start + 1
	for end < len(source) && (identifier(source[end]) || source[end] == '.') {
		end++
	}
	reference := source[start:end]
	switch {
	case reference == "$now":
		return "now()", end
	case reference == "$me":
		return "me()", end
	case strings.HasPrefix(reference, "$record."):
		return strings.TrimPrefix(strings.TrimPrefix(reference, "$record."), "fields."), end
	default:
		return "", start
	}
}
func durationToken(source string, start int) (string, int) {
	if source[start] < '0' || source[start] > '9' || start > 0 && (identifier(source[start-1]) || source[start-1] == '.') {
		return "", start
	}
	end := start
	for end < len(source) && source[end] >= '0' && source[end] <= '9' {
		end++
	}
	if end == len(source) || end+1 < len(source) && (identifier(source[end+1]) || source[end+1] == '.') {
		return "", start
	}
	unit, multiplier := durationUnit(source[end])
	if multiplier == 0 {
		return "", start
	}
	n, err := strconv.ParseUint(source[start:end], 10, 64)
	if err != nil || n > ^uint64(0)/multiplier {
		return "", start
	}
	return fmt.Sprintf("duration(%q)", strconv.FormatUint(n*multiplier, 10)+string(unit)), end + 1
}
func durationUnit(unit byte) (byte, uint64) {
	switch unit {
	case 'd':
		return 'h', 24
	case 'w':
		return 'h', 168
	case 'h', 'm':
		return unit, 1
	default:
		return 0, 0
	}
}
func identifier(b byte) bool {
	return b >= 128 || unicode.IsLetter(rune(b)) || b >= '0' && b <= '9' || b == '_'
}

// CEL permits escaped, raw and triple-quoted strings; none contains expression tokens.
func literalEnd(source string, start int) int {
	if strings.HasPrefix(source[start:], "//") {
		if end := strings.IndexByte(source[start:], '\n'); end >= 0 {
			return start + end
		}
		return len(source)
	}
	i := start
	raw := false
	if (source[i] == 'r' || source[i] == 'R') && i+1 < len(source) && (source[i+1] == '\'' || source[i+1] == '"') {
		raw = true
		i++
	}
	if source[i] != '\'' && source[i] != '"' {
		return start
	}
	delimiter := source[i : i+1]
	if strings.HasPrefix(source[i:], strings.Repeat(delimiter, 3)) {
		delimiter = strings.Repeat(delimiter, 3)
	}
	i += len(delimiter)
	for i < len(source) {
		if strings.HasPrefix(source[i:], delimiter) {
			return i + len(delimiter)
		}
		if !raw && source[i] == '\\' {
			i = min(i+2, len(source))
		} else {
			i++
		}
	}
	return len(source)
}
