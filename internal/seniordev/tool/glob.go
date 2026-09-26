//go:build !windows

// The glob tool: file-name matching through `rg --files`, newest first, capped
// at globResultLimit entries.
package tool

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
)

const globResultLimit = 100

type globFile struct {
	path  string
	mtime int64
}

type globMetadata struct {
	Count     int  `json:"count"`
	Truncated bool `json:"truncated"`
}

func (r *Registry) executeGlob(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	var input globInput
	if err := decodeInput(call.Input, &input, "pattern"); err != nil {
		return steploop.ToolResult{}, err
	}
	if err := r.ask(ctx, call, "glob", []string{input.Pattern}, map[string]any{
		"pattern": input.Pattern, "path": input.Path,
	}); err != nil {
		return steploop.ToolResult{}, err
	}
	search := r.workDir
	if input.Path != nil {
		search = *input.Path
		if !filepath.IsAbs(search) {
			search = filepath.Join(r.workDir, search)
		}
		search = filepath.Clean(search)
	}
	resolved, err := r.resolvePath(search)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if err := r.askExternalDirectory(ctx, call, resolved, "directory"); err != nil {
		return steploop.ToolResult{}, err
	}
	if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() {
		return steploop.ToolResult{}, fmt.Errorf("glob path must be a directory: %s", resolved)
	}

	args := []string{
		"--no-config",
		"--files",
		"--glob=!.git/*",
		"--hidden",
		"--glob=" + input.Pattern,
		".",
	}
	result, err := r.rg.Run(ctx, resolved, args)
	if err != nil {
		return steploop.ToolResult{}, err
	}
	if result.code != 0 && result.code != 1 {
		return steploop.ToolResult{}, ripgrepError(result)
	}

	files := make([]globFile, 0, globResultLimit+1)
	scanner := bufio.NewScanner(strings.NewReader(string(result.stdout)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		file := cleanRipgrepPath(scanner.Text())
		full := filepath.Clean(filepath.Join(resolved, file))
		mtime := int64(0)
		if info, statErr := os.Stat(full); statErr == nil {
			mtime = info.ModTime().UnixMilli()
		}
		files = append(files, globFile{path: full, mtime: mtime})
		if len(files) == globResultLimit+1 {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return steploop.ToolResult{}, err
	}
	truncated := len(files) > globResultLimit
	if truncated {
		files = files[:globResultLimit]
	}
	sort.SliceStable(files, func(i, j int) bool {
		return files[i].mtime > files[j].mtime
	})

	output := []string{}
	if len(files) == 0 {
		output = append(output, "No files found")
	} else {
		for _, file := range files {
			output = append(output, file.path)
		}
		if truncated {
			output = append(output, "")
			output = append(output,
				"(Results are truncated: showing first 100 results. Consider using a more specific path or pattern.)",
			)
		}
	}
	title, err := filepath.Rel(r.workDir, resolved)
	if err != nil {
		title = resolved
	}
	return steploop.ToolResult{
		Title:  title,
		Output: strings.Join(output, "\n"),
		Metadata: rawMetadata(globMetadata{
			Count:     len(files),
			Truncated: truncated,
		}),
	}, nil
}

func cleanRipgrepPath(path string) string {
	if strings.HasPrefix(path, "./") || strings.HasPrefix(path, `.\`) {
		path = path[2:]
	}
	return filepath.Clean(path)
}
