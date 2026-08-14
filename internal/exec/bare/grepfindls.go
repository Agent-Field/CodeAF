// Package bare — the three read-only tools (grep, find, ls) that pi registers
// but leaves inactive by default. They shell out to ripgrep and fd rather than
// reimplementing search in Go, matching pi's behavior exactly — including the
// error strings when the tools are missing.
package bare

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
)

// grepMaxLineLength mirrors pi's truncate.js:GREP_MAX_LINE_LENGTH.
const grepMaxLineLength = 500

// truncateLine mirrors pi's truncate.js:truncateLine. If the line exceeds
// maxChars, it is sliced to maxChars and suffixed with "... [truncated]".
// pi uses JS string.slice (UTF-16 code units); Go's string slice is bytes.
// For grep output, lines are overwhelmingly ASCII, so byte-slicing matches.
func truncateLine(line string) (string, bool) {
	if len(line) <= grepMaxLineLength {
		return line, false
	}
	return line[:grepMaxLineLength] + "... [truncated]", true
}

// ── grep tool ──────────────────────────────────────────────────────────────

func newGrepTool(cwd string) Tool {
	return Tool{
		Name:        "grep",
		Description: grepDescription,
		Schema:      json.RawMessage(grepSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Pattern    string  `json:"pattern"`
				Path       *string `json:"path"`
				Glob       *string `json:"glob"`
				IgnoreCase *bool   `json:"ignoreCase"`
				Literal    *bool   `json:"literal"`
				Context    *int    `json:"context"`
				Limit      *int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}

			// Resolve ripgrep. pi uses ensureTool("rg", true) which tries to
			// download it; we just look it up on PATH and produce the same
			// error string if missing.
			rgPath, err := exec.LookPath("rg")
			if err != nil {
				return "ripgrep (rg) is not available and could not be downloaded", true, nil
			}

			searchDir := "."
			if p.Path != nil {
				searchDir = *p.Path
			}
			searchPath := resolveToCwd(searchDir, cwd)

			// Check if path exists and is a directory.
			info, err := os.Stat(searchPath)
			if err != nil {
				return fmt.Sprintf("Path not found: %s", searchPath), true, nil
			}
			isDirectory := info.IsDir()

			contextValue := 0
			if p.Context != nil && *p.Context > 0 {
				contextValue = *p.Context
			}
			effectiveLimit := 100
			if p.Limit != nil && *p.Limit >= 1 {
				effectiveLimit = *p.Limit
			}

			// Build rg args.
			rgArgs := []string{"--json", "--line-number", "--color=never", "--hidden"}
			if p.IgnoreCase != nil && *p.IgnoreCase {
				rgArgs = append(rgArgs, "--ignore-case")
			}
			if p.Literal != nil && *p.Literal {
				rgArgs = append(rgArgs, "--fixed-strings")
			}
			if p.Glob != nil && *p.Glob != "" {
				rgArgs = append(rgArgs, "--glob", *p.Glob)
			}
			rgArgs = append(rgArgs, "--", p.Pattern, searchPath)

			cmd := exec.CommandContext(ctx, rgPath, rgArgs...)
			cmd.Stderr = nil
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				return fmt.Sprintf("Failed to run ripgrep: %s", err.Error()), true, nil
			}
			stderrPipe, _ := cmd.StderrPipe()

			if err := cmd.Start(); err != nil {
				return fmt.Sprintf("Failed to run ripgrep: %s", err.Error()), true, nil
			}

			// Collect stderr.
			var stderrStr strings.Builder
			if stderrPipe != nil {
				go func() {
					buf := make([]byte, 4096)
					for {
						n, err := stderrPipe.Read(buf)
						if n > 0 {
							stderrStr.Write(buf[:n])
						}
						if err != nil {
							break
						}
					}
				}()
			}

			// Parse rg --json output: collect match events.
			type match struct {
				filePath   string
				lineNumber int
				lineText   string
			}
			var matches []match
			matchCount := 0
			matchLimitReached := false
			linesTruncated := false

			scanner := bufio.NewScanner(stdout)
			scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
			for scanner.Scan() {
				line := scanner.Text()
				if strings.TrimSpace(line) == "" || matchCount >= effectiveLimit {
					continue
				}
				var event map[string]any
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					continue
				}
				if event["type"] != "match" {
					continue
				}
				matchCount++
				data, ok := event["data"].(map[string]any)
				if !ok {
					continue
				}
				filePath := ""
				if path, ok := data["path"].(map[string]any); ok {
					if text, ok := path["text"].(string); ok {
						filePath = text
					}
				}
				lineNumber := 0
				if ln, ok := data["line_number"].(float64); ok {
					lineNumber = int(ln)
				}
				lineText := ""
				if lines, ok := data["lines"].(map[string]any); ok {
					if text, ok := lines["text"].(string); ok {
						lineText = text
					}
				}
				if filePath != "" && lineNumber > 0 {
					matches = append(matches, match{filePath, lineNumber, lineText})
				}
				if matchCount >= effectiveLimit {
					matchLimitReached = true
					// Kill the child process to stop it.
					_ = cmd.Process.Signal(syscall.SIGTERM)
					break
				}
			}

			cmd.Wait()

			if ctx.Err() != nil {
				return "Operation aborted", true, nil
			}

			if matchCount == 0 {
				return "No matches found", false, nil
			}

			// Format matches.
			var outputLines []string
			for _, m := range matches {
				if contextValue == 0 && m.lineText != "" {
					relativePath := grepFormatPath(m.filePath, searchPath, isDirectory)
					sanitized := m.lineText
					sanitized = strings.ReplaceAll(sanitized, "\r\n", "\n")
					sanitized = strings.ReplaceAll(sanitized, "\r", "")
					sanitized = strings.TrimRight(sanitized, "\n")
					truncatedText, wasTruncated := truncateLine(sanitized)
					if wasTruncated {
						linesTruncated = true
					}
					outputLines = append(outputLines, fmt.Sprintf("%s:%d: %s", relativePath, m.lineNumber, truncatedText))
				} else {
					// Context mode: read the file and format a block.
					block := grepFormatBlock(m.filePath, m.lineNumber, contextValue, searchPath, isDirectory)
					outputLines = append(outputLines, block...)
				}
			}

			rawOutput := strings.Join(outputLines, "\n")
			truncation := truncateHeadNoLineLimit(rawOutput)
			output := truncation.content

			var notices []string
			if matchLimitReached {
				notices = append(notices, fmt.Sprintf("%d matches limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
			}
			if truncation.truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", formatSize(defaultMaxBytes)))
			}
			if linesTruncated {
				notices = append(notices, fmt.Sprintf("Some lines truncated to %d chars. Use read tool to see full lines", grepMaxLineLength))
			}
			if len(notices) > 0 {
				output += "\n\n[" + strings.Join(notices, ". ") + "]"
			}

			return output, false, nil
		},
	}
}

// grepFormatPath mirrors pi's formatPath: if searching a directory, relativize;
// otherwise use basename.
func grepFormatPath(filePath, searchPath string, isDirectory bool) string {
	if isDirectory {
		rel, err := filepath.Rel(searchPath, filePath)
		if err == nil && rel != "" && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.Base(filePath)
}

// grepFormatBlock reads a file and formats a context block around a match
// line, mirroring pi's formatBlock.
func grepFormatBlock(filePath string, lineNumber, contextValue int, searchPath string, isDirectory bool) []string {
	relativePath := grepFormatPath(filePath, searchPath, isDirectory)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return []string{fmt.Sprintf("%s:%d: (unable to read file)", relativePath, lineNumber)}
	}
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n"), "\n")
	if len(lines) == 0 {
		return []string{fmt.Sprintf("%s:%d: (unable to read file)", relativePath, lineNumber)}
	}
	var block []string
	start := lineNumber
	end := lineNumber
	if contextValue > 0 {
		start = lineNumber - contextValue
		if start < 1 {
			start = 1
		}
		end = lineNumber + contextValue
		if end > len(lines) {
			end = len(lines)
		}
	}
	for current := start; current <= end; current++ {
		lineText := ""
		if current-1 < len(lines) {
			lineText = strings.ReplaceAll(lines[current-1], "\r", "")
		}
		isMatchLine := current == lineNumber
		truncatedText, _ := truncateLine(lineText)
		if isMatchLine {
			block = append(block, fmt.Sprintf("%s:%d: %s", relativePath, current, truncatedText))
		} else {
			block = append(block, fmt.Sprintf("%s-%d- %s", relativePath, current, truncatedText))
		}
	}
	return block
}

// truncateHeadNoLineLimit applies truncateHead with effectively no line limit
// (only byte limit), matching pi's `{ maxLines: Number.MAX_SAFE_INTEGER }`.
func truncateHeadNoLineLimit(content string) truncateHeadResult {
	// Reuse truncateHead with a very large line limit. The byte limit still
	// applies. This is used by grep/find/ls where the match/entry count
	// already caps rows.
	totalBytes := byteLength(content)
	if totalBytes <= defaultMaxBytes {
		return truncateHeadResult{
			content:     content,
			totalLines:  len(splitLinesForCounting(content)),
			totalBytes:  totalBytes,
			outputLines: len(splitLinesForCounting(content)),
			outputBytes: totalBytes,
		}
	}
	// Only byte truncation matters; truncate to lines that fit under bytes.
	lines := splitLinesForCounting(content)
	var out []string
	outputBytesCount := 0
	for _, line := range lines {
		lineBytes := byteLength(line)
		if len(out) > 0 {
			lineBytes++ // newline separator
		}
		if outputBytesCount+lineBytes > defaultMaxBytes {
			break
		}
		out = append(out, line)
		outputBytesCount += lineBytes
	}
	outputContent := strings.Join(out, "\n")
	return truncateHeadResult{
		content:     outputContent,
		truncated:   true,
		truncatedBy: "bytes",
		totalLines:  len(lines),
		totalBytes:  totalBytes,
		outputLines: len(out),
		outputBytes: byteLength(outputContent),
	}
}

// ── find tool ──────────────────────────────────────────────────────────────

func newFindTool(cwd string) Tool {
	return Tool{
		Name:        "find",
		Description: findDescription,
		Schema:      json.RawMessage(findSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Pattern string  `json:"pattern"`
				Path    *string `json:"path"`
				Limit   *int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}

			fdPath, err := exec.LookPath("fd")
			if err != nil {
				return "fd is not available and could not be downloaded", true, nil
			}

			searchDir := "."
			if p.Path != nil {
				searchDir = *p.Path
			}
			searchPath := resolveToCwd(searchDir, cwd)

			effectiveLimit := 1000
			if p.Limit != nil {
				effectiveLimit = *p.Limit
			}

			// Build fd args.
			fdArgs := []string{"--glob", "--color=never", "--hidden"}

			// Check if inside a git repo (walk up for .git).
			insideGitRepo := false
			for current := searchPath; ; {
				if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
					insideGitRepo = true
					break
				}
				parent := filepath.Dir(current)
				if parent == current {
					break
				}
				current = parent
			}
			if !insideGitRepo {
				fdArgs = append(fdArgs, "--no-require-git")
			}
			fdArgs = append(fdArgs, "--max-results", fmt.Sprintf("%d", effectiveLimit))

			// Pattern with "/" needs --full-path and a leading "**/".
			effectivePattern := p.Pattern
			if strings.Contains(p.Pattern, "/") {
				fdArgs = append(fdArgs, "--full-path")
				if !strings.HasPrefix(p.Pattern, "/") && !strings.HasPrefix(p.Pattern, "**/") && p.Pattern != "**" {
					effectivePattern = "**/" + p.Pattern
				}
			}
			fdArgs = append(fdArgs, "--", effectivePattern, searchPath)

			cmd := exec.CommandContext(ctx, fdPath, fdArgs...)
			var stdout, stderr strings.Builder
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr

			runErr := cmd.Run()

			if ctx.Err() != nil {
				return "Operation aborted", true, nil
			}

			output := strings.TrimSpace(stdout.String())
			if output == "" {
				return "No files found matching pattern", false, nil
			}

			// Relativize paths.
			var relativized []string
			for _, rawLine := range strings.Split(output, "\n") {
				line := strings.TrimRight(rawLine, "\r")
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				hadTrailingSlash := strings.HasSuffix(line, "/") || strings.HasSuffix(line, "\\")
				relativePath := line
				if strings.HasPrefix(line, searchPath) {
					relativePath = line[len(searchPath)+1:]
				} else {
					rel, err := filepath.Rel(searchPath, line)
					if err == nil {
						relativePath = rel
					}
				}
				if hadTrailingSlash && !strings.HasSuffix(relativePath, "/") {
					relativePath += "/"
				}
				relativized = append(relativized, filepath.ToSlash(relativePath))
			}

			resultLimitReached := len(relativized) >= effectiveLimit
			rawOutput := strings.Join(relativized, "\n")
			truncation := truncateHeadNoLineLimit(rawOutput)
			resultOutput := truncation.content

			var notices []string
			if resultLimitReached {
				notices = append(notices, fmt.Sprintf("%d results limit reached. Use limit=%d for more, or refine pattern", effectiveLimit, effectiveLimit*2))
			}
			if truncation.truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", formatSize(defaultMaxBytes)))
			}
			if len(notices) > 0 {
				resultOutput += "\n\n[" + strings.Join(notices, ". ") + "]"
			}

			// Check for fd error with non-zero exit and no output.
			if runErr != nil {
				errMsg := strings.TrimSpace(stderr.String())
				if errMsg != "" && resultOutput == "" {
					return errMsg, true, nil
				}
			}

			return resultOutput, false, nil
		},
	}
}

// ── ls tool ────────────────────────────────────────────────────────────────

func newLsTool(cwd string) Tool {
	return Tool{
		Name:        "ls",
		Description: lsDescription,
		Schema:      json.RawMessage(lsSchemaJSON),
		Execute: func(ctx context.Context, args json.RawMessage) (string, bool, error) {
			var p struct {
				Path  *string `json:"path"`
				Limit *int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &p); err != nil {
				return "Invalid arguments: " + err.Error(), true, nil
			}

			dirPath := "."
			if p.Path != nil {
				dirPath = *p.Path
			}
			dirPath = resolveToCwd(dirPath, cwd)

			effectiveLimit := 500
			if p.Limit != nil {
				effectiveLimit = *p.Limit
			}

			// Check if path exists.
			info, err := os.Stat(dirPath)
			if err != nil {
				if os.IsNotExist(err) {
					return fmt.Sprintf("Path not found: %s", dirPath), true, nil
				}
				return fmt.Sprintf("Path not found: %s", dirPath), true, nil
			}

			// Check if directory.
			if !info.IsDir() {
				return fmt.Sprintf("Not a directory: %s", dirPath), true, nil
			}

			// Read directory entries.
			entries, err := os.ReadDir(dirPath)
			if err != nil {
				return fmt.Sprintf("Cannot read directory: %s", err.Error()), true, nil
			}

			// Sort alphabetically, case-insensitive.
			sort.Slice(entries, func(i, j int) bool {
				return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
			})

			// Format entries with directory indicators.
			var results []string
			entryLimitReached := false
			for _, entry := range entries {
				if len(results) >= effectiveLimit {
					entryLimitReached = true
					break
				}
				suffix := ""
				if entry.IsDir() {
					suffix = "/"
				}
				results = append(results, entry.Name()+suffix)
			}

			if len(results) == 0 {
				return "(empty directory)", false, nil
			}

			rawOutput := strings.Join(results, "\n")
			truncation := truncateHeadNoLineLimit(rawOutput)
			output := truncation.content

			var notices []string
			if entryLimitReached {
				notices = append(notices, fmt.Sprintf("%d entries limit reached. Use limit=%d for more", effectiveLimit, effectiveLimit*2))
			}
			if truncation.truncated {
				notices = append(notices, fmt.Sprintf("%s limit reached", formatSize(defaultMaxBytes)))
			}
			if len(notices) > 0 {
				output += "\n\n[" + strings.Join(notices, ". ") + "]"
			}

			return output, false, nil
		},
	}
}
