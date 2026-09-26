//go:build !windows

// The grep tool: content search through `rg --json`, grouped by file and
// ordered newest first.
package tool

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

const maxGrepLineLength = 2000

type ripgrepJSONLine struct {
	Type string `json:"type"`
	Data struct {
		Path struct {
			Text string `json:"text"`
		} `json:"path"`
		Lines struct {
			Text string `json:"text"`
		} `json:"lines"`
		LineNumber int `json:"line_number"`
	} `json:"data"`
}

type grepInput struct {
	Pattern string  `json:"pattern"`
	Path    *string `json:"path,omitempty"`
	Include *string `json:"include,omitempty"`
}

type grepMatch struct {
	path  string
	line  int
	text  string
	mtime int64
}

type grepMetadata struct {
	Matches   int  `json:"matches"`
	Truncated bool `json:"truncated"`
}

func (r *Registry) executeGrep(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input grepInput
	if err := decodeInput(call.Input, &input, "pattern"); err != nil {
		return steploop.ToolResult{}, err
	}
	if input.Pattern == "" {
		return steploop.ToolResult{}, errors.New("pattern is required")
	}
	if err := r.ask(ctx, call, "grep", []string{input.Pattern}, map[string]any{
		"pattern": input.Pattern, "path": input.Path, "include": input.Include,
	}); err != nil {
		return steploop.ToolResult{}, err
	}
	empty := func() steploop.ToolResult {
		return steploop.ToolResult{
			Title:  input.Pattern,
			Output: "No files found",
			Metadata: rawMetadata(grepMetadata{
				Matches:   0,
				Truncated: false,
			}),
		}
	}

	search := r.workDir
	if input.Path != nil {
		search = *input.Path
		if !filepath.IsAbs(search) {
			search = filepath.Join(r.workDir, search)
		}
	}
	search = filepath.Clean(search)
	resolved, err := r.resolvePath(search)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	info, statErr := os.Stat(resolved)
	isDirectory := statErr == nil && info.IsDir()
	kind := "file"
	if isDirectory {
		kind = "directory"
	}
	if err := r.askExternalDirectory(ctx, call, resolved, kind); err != nil {
		return steploop.ToolResult{}, err
	}
	cwd := filepath.Dir(resolved)
	files := []string{filepath.Base(resolved)}
	if isDirectory {
		cwd = resolved
		files = []string{"."}
	}

	args := []string{"--no-config", "--json", "--hidden", "--glob=!.git/*", "--no-messages"}
	if input.Include != nil {
		args = append(args, "--glob="+*input.Include)
	}
	args = append(args, "--", input.Pattern)
	args = append(args, files...)
	result, err := r.rg.Run(ctx, cwd, args)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if result.code != 0 && result.code != 1 && result.code != 2 {
		return steploop.ToolResult{}, ripgrepError(result)
	}
	if result.code == 1 {
		return empty(), nil
	}

	rows := []grepMatch{}
	scanner := bufio.NewScanner(strings.NewReader(string(result.stdout)))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		var event ripgrepJSONLine
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return steploop.ToolResult{}, errors.New("invalid ripgrep output")
		}
		if event.Type != "match" {
			continue
		}
		matchPath := cleanRipgrepPath(event.Data.Path.Text)
		if !filepath.IsAbs(matchPath) {
			matchPath = filepath.Join(cwd, matchPath)
		}
		matchPath = filepath.Clean(matchPath)
		info, statErr := os.Stat(matchPath)
		if statErr != nil || info.IsDir() {
			continue
		}
		rows = append(rows, grepMatch{
			path:  matchPath,
			line:  event.Data.LineNumber,
			text:  event.Data.Lines.Text,
			mtime: info.ModTime().UnixMilli(),
		})
	}
	if err := scanner.Err(); err != nil {
		return steploop.ToolResult{}, err
	}
	if len(rows) == 0 {
		return empty(), nil
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].mtime > rows[j].mtime
	})

	const limit = 100
	total := len(rows)
	truncated := total > limit
	final := rows
	if truncated {
		final = rows[:limit]
	}
	output := []string{"Found " + itoa(total) + " matches"}
	if truncated {
		output[0] += " (showing first 100)"
	}
	current := ""
	for _, match := range final {
		if current != match.path {
			if current != "" {
				output = append(output, "")
			}
			current = match.path
			output = append(output, match.path+":")
		}
		text := match.text
		if len(utf16.Encode([]rune(text))) > maxGrepLineLength {
			text = truncateUTF16Units(text, maxGrepLineLength) + "..."
		}
		output = append(output, "  Line "+itoa(match.line)+": "+text)
	}
	if truncated {
		output = append(output, "")
		output = append(
			output,
			"(Results truncated: showing 100 of "+itoa(total)+" matches ("+itoa(total-limit)+
				" hidden). Consider using a more specific path or pattern.)",
		)
	}
	if result.code == 2 {
		output = append(output, "")
		output = append(output, "(Some paths were inaccessible and skipped)")
	}
	return steploop.ToolResult{
		Title:  input.Pattern,
		Output: strings.Join(output, "\n"),
		Metadata: rawMetadata(grepMetadata{
			Matches:   total,
			Truncated: truncated,
		}),
	}, nil
}

// truncateUTF16Units cuts a line at limit UTF-16 code units, the unit
// maxGrepLineLength is expressed in.
func truncateUTF16Units(value string, limit int) string {
	units := utf16.Encode([]rune(value))
	if len(units) <= limit {
		return value
	}
	return string(utf16.Decode(units[:limit]))
}
