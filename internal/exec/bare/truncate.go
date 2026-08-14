// Package bare replicates pi 0.82.1's four default wire tools (read, bash,
// edit, write) so an atomic leaf runs at pi's own cost and wall time. It is
// aforge-owned code — not a vendored copy of pi — but the tool descriptions,
// schemas, result strings, and truncation footers are pinned to pi's source so
// the wire bytes are identical.
package bare

import "strings"

// Truncation constants mirror pi's truncate.js. They are the literal values pi
// interpolates into tool descriptions and footers, so they are exported names
// only within this package — the descriptions carry the rendered numbers.
const (
	defaultMaxLines = 2000
	defaultMaxBytes = 50 * 1024 // 50KB
)

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

// truncateHead keeps the first whole lines that fit both the line and byte
// caps. It is the read/grep/find/ls rule. When the first line alone exceeds
// the byte cap, it returns empty content with firstLineExceedsLimit set so
// the caller can emit the sed fallback hint.
//
// The byte accounting adds +1 for the newline that separates each line from
// the previous one (i>0), exactly as pi does — the newline is real output
// the model sees and counts against the budget.
func truncateHead(content string) truncateHeadResult {
	maxLines := defaultMaxLines
	maxBytes := defaultMaxBytes
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
func truncateTail(content string) truncateTailResult {
	maxLines := defaultMaxLines
	maxBytes := defaultMaxBytes
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
