// Package bare is the wire tool library: pi 0.82.1's seven tools (read, bash,
// edit, write, grep, find, ls) with the matching, truncation and streaming
// logic behind them. Chat's tool surface is built from it. It is aforge-owned
// code — not a vendored copy of pi — but the schemas, result strings and
// truncation footers are pinned to pi's source so the wire bytes a model sees
// are identical to the ones these tools were measured on.
//
// THE DESCRIPTIONS THAT QUOTE A LIMIT ARE THE EXCEPTION, and they have to be.
// pi's are literals because pi's caps are literals; here the caps follow the
// model's window ([Caps]), so read, bash, grep, find and ls render their
// figures from the pair they are actually applying. read and bash were cut to
// their contract in the same pass — what the tool does and what it costs, with
// the question of which work belongs on which door left to the page that owns
// it. edit and write are pi's, verbatim.
package bare

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/ctxbudget"
)

// Truncation constants mirror pi's truncate.js. They are the literal values pi
// interpolates into tool descriptions and footers, so they are exported names
// only within this package — the descriptions carry the rendered numbers.
//
// THEY ARE NOW THE CEILING RATHER THAN THE LAW. A result is cut to whatever
// [Caps] the belt was built with, and these two are what a belt gets when
// nothing can say how much room the model has. See [CapsFor].
const (
	defaultMaxLines = 2000
	defaultMaxBytes = 50 * 1024 // 50KB
)

// ResultByteCap is [defaultMaxBytes] for tools that live outside this package.
// A tool result is a tool result whatever it read, so anything else on the belt
// that has to bound one binds itself to the number the read tool's own
// description quotes rather than to a second copy of it that can drift away.
const ResultByteCap = defaultMaxBytes

// Caps are the two bounds every result in this package is cut to, and they
// travel with the belt rather than sitting in a constant.
//
// ONE READ MAY NOT BE MOST OF WHAT THE MODEL CAN HOLD. Cutting every result at
// pi's flat 50KB is right for the window pi's numbers were measured against and
// wrong below it: on a 16k model one `read` of one file was 78% of everything
// the model could carry, so the file arrived and there was no room left to
// think about it. The caps therefore follow the window (see [CapsFor]), and the
// descriptions the model reads are rendered from the pair actually in force —
// a tool that quotes a limit it is not applying is a tool the model plans
// wrongly around.
type Caps struct {
	MaxLines int
	MaxBytes int
}

// DefaultCaps is pi's own pair: 2000 lines or 50KB, whichever binds first. It
// is what a belt built without a window gets, so a caller that knows nothing
// about the model behaves exactly as this package did before caps existed.
func DefaultCaps() Caps { return Caps{MaxLines: defaultMaxLines, MaxBytes: defaultMaxBytes} }

// CapsFor scales the pair to a model's context window, through the one law
// that owns the share ([ctxbudget.ToolResultBytes]) rather than a second
// formula of this package's own.
//
// The line cap follows the byte cap in pi's own proportion, because the two
// bind together and moving one alone would change which of them a given output
// is cut by. A window of 128,000 tokens or more lands exactly 2000 lines and
// 50KB, which is what keeps every frontier conversation byte-identical to what
// it was.
func CapsFor(contextTokens int) Caps {
	return capsAt(ctxbudget.ToolResultBytes(contextTokens, defaultMaxBytes))
}

// capsAt derives the pair from a byte budget alone. A budget outside the
// ordinary range takes pi's own, so an unusable number can never widen a cap:
// the caps only ever fall away from pi's, never past them.
func capsAt(maxBytes int) Caps {
	if maxBytes <= 0 || maxBytes > defaultMaxBytes {
		return DefaultCaps()
	}
	lines := defaultMaxLines * maxBytes / defaultMaxBytes
	if lines < 1 {
		lines = 1
	}
	return Caps{MaxLines: lines, MaxBytes: maxBytes}
}

// resolve fills in pi's defaults for a zero Caps, so that a caller which has
// not been taught about caps yet cuts results exactly where it always did.
func (c Caps) resolve() Caps {
	if c.MaxLines <= 0 {
		c.MaxLines = defaultMaxLines
	}
	if c.MaxBytes <= 0 {
		c.MaxBytes = defaultMaxBytes
	}
	return c
}

// sizeWord renders a cap the way a SENTENCE says it — "50KB" where the footers
// say "50.0KB" — so a description interpolating the number in force reads the
// way the hand-typed one it replaces did.
func sizeWord(bytes int) string {
	rendered := formatSize(bytes)
	for _, unit := range []string{"KB", "MB"} {
		if trimmed, whole := strings.CutSuffix(rendered, ".0"+unit); whole {
			return trimmed + unit
		}
	}
	return rendered
}

// formatSize mirrors pi's truncate.js:formatSize. The boundary tests pin the
// exact rendered strings, which the read/bash footers interpolate.
func formatSize(bytes int) string {
	if bytes < 1024 {
		return itoa(bytes) + "B"
	}
	if bytes < 1024*1024 {
		return trimFloat(float64(bytes)/1024) + "KB"
	}
	return trimFloat(float64(bytes)/(1024*1024)) + "MB"
}

// byteLength returns the UTF-8 byte length of a string, matching JS
// Buffer.byteLength(str, "utf-8"). Every truncation boundary in pi is a byte
// boundary, not a rune boundary.
func byteLength(s string) int { return len(s) }

// truncateHeadResult is the output of truncateHead. Fields that read/bash
// footers depend on are exported by field name; the struct mirrors pi's
// return shape closely enough that the footer logic reads the same way.
type truncateHeadResult struct {
	content               string
	truncated             bool
	truncatedBy           string // "lines", "bytes", or ""
	totalLines            int
	totalBytes            int
	outputLines           int
	outputBytes           int
	lastLinePartial       bool
	firstLineExceedsLimit bool
}

// splitLinesForCounting mirrors pi's splitLinesForCounting: split on "\n",
// then drop a trailing empty element if the content ended with a newline.
// A trailing newline terminates the last line rather than starting an empty
// one — this is what makes "wc -l", sed, and the footers all agree.
func splitLinesForCounting(content string) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(content, "\n")
	if content[len(content)-1] == '\n' {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
	}
	return lines
}

// truncateHeadAt is truncateHead with a caller-owned byte budget. The ordinary
// tools always use pi's defaults; a composed belt may reserve room for the
// continuation footer while keeping the same line and offset semantics.
func truncateHeadAt(content string, maxLines, maxBytes int) truncateHeadResult {
	totalBytes := byteLength(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncateHeadResult{
			content:     content,
			totalLines:  totalLines,
			totalBytes:  totalBytes,
			outputLines: totalLines,
			outputBytes: totalBytes,
		}
	}

	// First line alone exceeds the byte limit.
	if len(lines) > 0 && byteLength(lines[0]) > maxBytes {
		return truncateHeadResult{
			content:               "",
			truncated:             true,
			truncatedBy:           "bytes",
			totalLines:            totalLines,
			totalBytes:            totalBytes,
			firstLineExceedsLimit: true,
		}
	}

	var out []string
	outputBytesCount := 0
	truncatedBy := "lines"
	for i := range min(len(lines), maxLines) {
		line := lines[i]
		lineBytes := byteLength(line)
		if i > 0 {
			lineBytes++ // +1 for the newline separator
		}
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			break
		}
		out = append(out, line)
		outputBytesCount += lineBytes
	}
	// Exited due to line limit.
	if len(out) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}
	outputContent := strings.Join(out, "\n")
	return truncateHeadResult{
		content:               outputContent,
		truncated:             true,
		truncatedBy:           truncatedBy,
		totalLines:            totalLines,
		totalBytes:            totalBytes,
		outputLines:           len(out),
		outputBytes:           byteLength(outputContent),
		firstLineExceedsLimit: false,
	}
}

// truncateTailResult mirrors truncateHeadResult plus lastLinePartial, which
// only the bash tail path sets.
type truncateTailResult struct {
	content               string
	truncated             bool
	truncatedBy           string
	totalLines            int
	totalBytes            int
	outputLines           int
	outputBytes           int
	lastLinePartial       bool
	firstLineExceedsLimit bool
}

// truncateTail keeps the last whole lines that fit both caps — the bash rule,
// for output whose verdict is at the end. When the final line alone exceeds
// the byte cap it keeps that line's last bytes (partial), because on a
// single-line output the end is still where the answer is.
//
// The byte accounting adds +1 for the newline separator when the line is not
// the first one added (outputLinesArr.length > 0 in pi), matching pi's
// backwards walk.
func truncateTail(content string, caps Caps) truncateTailResult {
	caps = caps.resolve()
	maxLines := caps.MaxLines
	maxBytes := caps.MaxBytes
	totalBytes := byteLength(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	if totalLines <= maxLines && totalBytes <= maxBytes {
		return truncateTailResult{
			content:     content,
			totalLines:  totalLines,
			totalBytes:  totalBytes,
			outputLines: totalLines,
			outputBytes: totalBytes,
		}
	}

	var out []string
	outputBytesCount := 0
	truncatedBy := "lines"
	lastLinePartial := false
	for i := len(lines) - 1; i >= 0 && len(out) < maxLines; i-- {
		line := lines[i]
		lineBytes := byteLength(line)
		if len(out) > 0 {
			lineBytes++ // +1 for the newline separator
		}
		if outputBytesCount+lineBytes > maxBytes {
			truncatedBy = "bytes"
			if len(out) == 0 {
				// This single line exceeds maxBytes: take the end of it.
				truncatedLine := truncateStringToBytesFromEnd(line, maxBytes)
				out = append([]string{truncatedLine}, out...)
				outputBytesCount = byteLength(truncatedLine)
				lastLinePartial = true
			}
			break
		}
		out = append([]string{line}, out...)
		outputBytesCount += lineBytes
	}
	if len(out) >= maxLines && outputBytesCount <= maxBytes {
		truncatedBy = "lines"
	}
	outputContent := strings.Join(out, "\n")
	return truncateTailResult{
		content:         outputContent,
		truncated:       true,
		truncatedBy:     truncatedBy,
		totalLines:      totalLines,
		totalBytes:      totalBytes,
		outputLines:     len(out),
		outputBytes:     byteLength(outputContent),
		lastLinePartial: lastLinePartial,
	}
}

// truncateStringToBytesFromEnd keeps the last maxBytes bytes of a string,
// advancing past any continuation UTF-8 bytes to land on a character
// boundary. Mirrors pi's truncateStringToBytesFromEnd.
func truncateStringToBytesFromEnd(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	start := len(s) - maxBytes
	for start < len(s) && (s[start]&0xc0) == 0x80 {
		start++
	}
	return s[start:]
}

// itoa is a minimal int→string for formatSize, avoiding strconv in the hot
// path. Only used for byte counts under 1024.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// trimFloat formats a float to one decimal place, matching JS toFixed(1).
// pi always calls toFixed(1), and JS always produces exactly one decimal
// digit, so we match that rather than using Go's default float formatting
// which would differ for values like 1.0 ("1" in Go, "1.0" in JS).
func trimFloat(f float64) string {
	// toFixed(1): round to one decimal, always one digit.
	// JS toFixed uses round-half-to-even in some engines and round-half-up
	// in others; pi's formatSize uses values like 51200/1024=50.0 exactly,
	// so the rounding mode rarely matters. We use round-half-up.
	scaled := f * 10
	rounded := int(scaled + 0.5)
	if f < 0 {
		rounded = int(scaled - 0.5)
	}
	whole := rounded / 10
	frac := rounded % 10
	if frac < 0 {
		frac = -frac
		whole = -whole // adjust for negative
	}
	return itoa(whole) + "." + string(rune('0'+frac))
}
