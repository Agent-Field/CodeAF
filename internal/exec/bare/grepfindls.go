// Package bare — the three read-only tools (grep, find, ls) that pi registers
// but leaves inactive by default. They shell out to ripgrep and fd rather than
// reimplementing search in Go, matching pi's behavior exactly — including the
// error strings when the tools are missing.
package bare

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/Agent-Field/aforge-v2/internal/guard"
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

// ripgrepPath is the ONE PROBE for ripgrep, taken once per process.
//
// Once, because the answer cannot change under a running session — a binary
// does not appear on PATH between two tool calls — and because the description
// the model reads is chosen from it at belt construction. A second, later answer
// would mean the tool said one thing about itself and did another.
var ripgrepPath = sync.OnceValues(func() (string, bool) {
	path, err := exec.LookPath("rg")
	if err != nil {
		return "", false
	}
	return path, true
})

// grepFallbackDescription is what `grep` says about itself on a machine with no
// ripgrep. It is the same tool with the same arguments and the same caps; the
// two sentences that differ are the two facts that differ, and they are stated
// rather than left for the model to discover by being surprised.
const grepFallbackDescription = "Search file contents for a pattern. Returns matching lines with file paths and line numbers. Walks the tree itself (ripgrep is not on this machine), so it does NOT read .gitignore — it skips .git, node_modules, vendor and files that look binary. Output is truncated to 100 matches or 50KB (whichever is hit first). Long lines are truncated to 500 chars."

// grepToolDescription is the description this machine's `grep` actually carries.
//
// A CAPABILITY THAT CANNOT WORK IS ABSENT, AND THIS ONE CAN ALWAYS WORK. The
// verb `grep` used to be on every belt and fail on every machine without
// ripgrep, answering `ripgrep (rg) is not available and could not be
// downloaded` — eleven times in one measured run, on a machine whose egress was
// blocked so the download the sentence promised could never happen. The model
// learned it by failing, twice. So the tool keeps its name, its arguments and
// its output shape everywhere, and only its ENGINE and this one sentence change.
func grepToolDescription() string {
	if _, present := ripgrepPath(); present {
		return grepDescription
	}
	return grepFallbackDescription
}

// grepMatch is one hit, whichever engine found it.
type grepMatch struct {
	filePath   string
	lineNumber int
	lineText   string
}

func newGrepTool(cwd string) Tool {
	return Tool{
		Name:        "grep",
		Description: grepToolDescription(),
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

			rgPath, haveRipgrep := ripgrepPath()

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

			var (
				matches           []grepMatch
				matchCount        int
				matchLimitReached bool
				linesTruncated    bool
			)
			if !haveRipgrep {
				// THE FALLBACK ANSWERS IN THE SAME SHAPE, which is the whole
				// point of it: the same matches, formatted by the same code
				// below, so nothing downstream — the model, the person's screen,
				// the fix-recall lane — can tell which engine ran.
				found, limitHit, walkErr := grepByWalking(ctx, p.Pattern, searchPath, globOr(p.Glob), boolOr(p.IgnoreCase), boolOr(p.Literal), effectiveLimit)
				if walkErr != nil {
					return walkErr.Error(), true, nil
				}
				if ctx.Err() != nil {
					return "Operation aborted", true, nil
				}
				matches, matchCount, matchLimitReached = found, len(found), limitHit
				return grepRender(matches, matchCount, matchLimitReached, linesTruncated, contextValue, searchPath, isDirectory, effectiveLimit)
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
				guard.Go("exec/bare grep stderr", func() {
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
				})
			}

			// Parse rg --json output: collect match events.
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
					matches = append(matches, grepMatch{filePath, lineNumber, lineText})
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

			return grepRender(matches, matchCount, matchLimitReached, linesTruncated, contextValue, searchPath, isDirectory, effectiveLimit)
		},
	}
}

// grepRender turns matches into the answer, and it is the ONE renderer both
// engines go through. Splitting it out is what makes the fallback a fallback
// rather than a second grep: every cap, every notice and every path rule below
// is written once and neither engine can drift from it.
func grepRender(matches []grepMatch, matchCount int, matchLimitReached, linesTruncated bool, contextValue int, searchPath string, isDirectory bool, effectiveLimit int) (string, bool, error) {
	if matchCount == 0 {
		return "No matches found", false, nil
	}

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
}

// ── grep without ripgrep ───────────────────────────────────────────────────

// grepSkippedDirs are the directories the walking engine does not descend into.
//
// It is a SHORT, NAMED LIST and not a .gitignore reader. Reading .gitignore
// properly is ripgrep's whole difficulty — nested files, negations, precedence
// — and a half-implementation of it would be a search that silently missed
// files for reasons nobody could predict. Three names cover the cases that
// otherwise turn one search into a minute of walking somebody else's code, and
// the description says out loud that this is what the fallback does instead.
var grepSkippedDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
}

// grepByWalking is the engine `grep` uses on a machine with no ripgrep: the
// same arguments, the same caps, the same match shape, walked in Go.
//
// IT SHELLS OUT TO NOTHING. `grep -rn` was the obvious fallback and is the
// wrong one — its regex dialect is not ripgrep's (BRE, no \d, no non-greedy),
// its -r walks differently, and a machine without ripgrep is exactly the kind of
// machine that might not have GNU grep either. Go's own regexp is the same
// dialect ripgrep uses for the patterns models actually write, and it is
// guaranteed present because it is compiled in.
func grepByWalking(ctx context.Context, pattern, searchPath, glob string, ignoreCase, literal bool, limit int) ([]grepMatch, bool, error) {
	expression := pattern
	if literal {
		expression = regexp.QuoteMeta(pattern)
	}
	if ignoreCase {
		expression = "(?i)" + expression
	}
	compiled, err := regexp.Compile(expression)
	if err != nil {
		return nil, false, fmt.Errorf("Invalid pattern: %s", err.Error())
	}

	var (
		matches    []grepMatch
		limitHit   bool
		stopWalk   = errors.New("enough")
		globMatch  = grepGlobMatcher(glob)
		searchInfo os.FileInfo
	)
	if searchInfo, err = os.Stat(searchPath); err != nil {
		return nil, false, fmt.Errorf("Path not found: %s", searchPath)
	}

	scan := func(path string) error {
		data, readErr := os.ReadFile(path)
		if readErr != nil || looksBinary(data) {
			return nil
		}
		for index, line := range strings.Split(string(data), "\n") {
			if !compiled.MatchString(line) {
				continue
			}
			matches = append(matches, grepMatch{filePath: path, lineNumber: index + 1, lineText: line})
			if len(matches) >= limit {
				limitHit = true
				return stopWalk
			}
		}
		return nil
	}

	if !searchInfo.IsDir() {
		if err := scan(searchPath); err != nil && err != stopWalk {
			return nil, false, err
		}
		return matches, limitHit, nil
	}

	err = filepath.WalkDir(searchPath, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unreadable directory is skipped rather than fatal: a search
			// that dies on one permission-denied folder answers nothing about
			// the thousand folders it could have read.
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry.IsDir() {
			if path != searchPath && grepSkippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if globMatch != nil && !globMatch(path, searchPath) {
			return nil
		}
		return scan(path)
	})
	if err != nil && err != stopWalk {
		if ctx.Err() != nil {
			return nil, false, nil
		}
		return nil, false, err
	}
	return matches, limitHit, nil
}

// grepGlobMatcher turns the tool's `glob` argument into a per-file test, or nil
// when there is no filter.
//
// A glob with no separator is matched against the BASE NAME (`*.ts` means "any
// TypeScript file anywhere", which is what everybody means by it) and one with a
// separator against the path relative to the search root, with a leading `**/`
// tolerated the way ripgrep tolerates it. Go's filepath.Match has no `**`, so a
// leading one is stripped and the rest matched against the tail of the path —
// which is the only form of `**` that appears in practice.
func grepGlobMatcher(glob string) func(path, root string) bool {
	if glob == "" {
		return nil
	}
	if !strings.Contains(glob, "/") {
		return func(path, _ string) bool {
			ok, _ := filepath.Match(glob, filepath.Base(path))
			return ok
		}
	}
	trimmed := strings.TrimPrefix(glob, "**/")
	return func(path, root string) bool {
		relative, err := filepath.Rel(root, path)
		if err != nil {
			relative = path
		}
		relative = filepath.ToSlash(relative)
		if ok, _ := filepath.Match(trimmed, relative); ok {
			return true
		}
		// `**/` at the front means "at any depth", so the pattern is also tried
		// against every suffix of the path that starts at a separator.
		for index := 0; index < len(relative); index++ {
			if relative[index] != '/' {
				continue
			}
			if ok, _ := filepath.Match(trimmed, relative[index+1:]); ok {
				return true
			}
		}
		return false
	}
}

// looksBinary reports whether a file's bytes are not worth showing a person.
// A NUL byte in the first 8KB is the same test `grep` itself uses, and it is
// what keeps a search of a repository from pasting a compiled binary into the
// model's context one "line" at a time.
func looksBinary(data []byte) bool {
	head := data
	if len(head) > 8<<10 {
		head = head[:8<<10]
	}
	return bytes.IndexByte(head, 0) >= 0
}

// globOr, boolOr read the grep tool's optional arguments, where absent and zero
// mean the same thing.
func globOr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func boolOr(value *bool) bool { return value != nil && *value }

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
