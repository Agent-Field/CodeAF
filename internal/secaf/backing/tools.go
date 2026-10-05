package backing

// The tools an agent session is handed: four ways to read the repository and
// none to change it.
//
// EVERY PATH IS THE REPOSITORY'S. A path is read relative to the folder under
// audit, and one that leads out of it — `..`, an absolute path elsewhere, a
// link whose target is elsewhere — is refused in a sentence the model can act
// on. sec-af's hunters chase data flow wherever it goes, and the person's home
// folder is not where it goes.
//
// EVERY ANSWER IS BOUNDED. A session runs for up to fifty turns, and a tool
// that hands back a whole minified bundle or every line matching `e` spends
// the session's context on noise; each tool says how much it left out and how
// to ask for the rest.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

const (
	// readLines is the most lines one read answers.
	readLines = 400
	// readBytes is the most bytes one read answers, for files with long lines.
	readBytes = 48 << 10
	// listEntries is the most entries a listing answers.
	listEntries = 400
	// globMatches is the most paths a glob answers.
	globMatches = 300
	// grepMatches is the most lines a search answers.
	grepMatches = 200
	// grepFileBytes is the largest file a search reads; past it a file is a
	// bundle, a dump or a dataset, and its lines are not code to trace.
	grepFileBytes = 2 << 20
	// grepLineRunes is how much of one matching line is quoted.
	grepLineRunes = 240
)

// skippedDirs are folders a search walks past: version control, dependency
// trees and build output. A read or a listing still reaches them by name,
// because a hunter that has a reason to open a vendored file may.
var skippedDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, "dist": true, "build": true, "target": true, ".next": true,
	".tox": true, ".mypy_cache": true, ".pytest_cache": true, ".gradle": true, ".idea": true,
}

// toolbox is the four tools over one repository.
type toolbox struct {
	root string
}

// definitions is what the model is offered.
func (t toolbox) definitions() []ai.ToolDefinition {
	tool := func(name, description string, properties map[string]any, required ...string) ai.ToolDefinition {
		return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
			Name: name, Description: description,
			Parameters: map[string]any{"type": "object", "properties": properties, "required": required},
		}}
	}
	text := func(description string) map[string]any {
		return map[string]any{"type": "string", "description": description}
	}
	number := func(description string) map[string]any {
		return map[string]any{"type": "integer", "description": description}
	}
	return []ai.ToolDefinition{
		tool("read_file", fmt.Sprintf("Read a file of the repository, with line numbers. At most %d lines per call; pass offset to read further.", readLines),
			map[string]any{"path": text("path relative to the repository root"), "offset": number("first line to read, 1-based (default 1)"), "limit": number(fmt.Sprintf("how many lines (default and most %d)", readLines))}, "path"),
		tool("list_dir", "List one folder of the repository: files, and folders with a trailing slash.",
			map[string]any{"path": text("folder relative to the repository root (default: the root)")}),
		tool("glob", "Find files whose path matches a pattern such as **/*.py or src/**/routes*.ts. Dependency and build folders are skipped.",
			map[string]any{"pattern": text("the pattern, relative to the repository root")}, "pattern"),
		tool("grep", "Search file contents with a regular expression (RE2 syntax). Answers path:line: text. Dependency and build folders are skipped.",
			map[string]any{"pattern": text("the regular expression"), "path": text("folder or file to search (default: the whole repository)"),
				"glob": text("only files whose path matches this pattern, such as *.go"), "ignore_case": map[string]any{"type": "boolean"}}, "pattern"),
	}
}

// run answers one tool call in the words the model reads: the result, or what
// was wrong with the call.
func (t toolbox) run(call ai.ToolCall) string {
	var args struct {
		Path       string `json:"path"`
		Offset     int    `json:"offset"`
		Limit      int    `json:"limit"`
		Pattern    string `json:"pattern"`
		Glob       string `json:"glob"`
		IgnoreCase bool   `json:"ignore_case"`
	}
	if raw := strings.TrimSpace(call.Function.Arguments); raw != "" {
		if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return "error: the arguments are not a JSON object: " + err.Error()
		}
	}
	var (
		out string
		err error
	)
	switch call.Function.Name {
	case "read_file":
		out, err = t.read(args.Path, args.Offset, args.Limit)
	case "list_dir":
		out, err = t.list(args.Path)
	case "glob":
		out, err = t.glob(args.Pattern)
	case "grep":
		out, err = t.grep(args.Pattern, args.Path, args.Glob, args.IgnoreCase)
	default:
		return fmt.Sprintf("error: there is no tool %q; the tools are read_file, list_dir, glob and grep", call.Function.Name)
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return out
}

// resolve is a path the model named, as an absolute path inside the
// repository, or the refusal.
func (t toolbox) resolve(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == "/" {
		return t.root, nil
	}
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(t.root, path)
	}
	path = filepath.Clean(path)
	if !within(t.root, path) {
		return "", fmt.Errorf("%s is outside the repository; paths are relative to its root", name)
	}
	// A LINK IS FOLLOWED ONLY TO WHERE IT STAYS INSIDE. The name is checked
	// above and the target here, so neither road leads out.
	if real, err := filepath.EvalSymlinks(path); err == nil && !within(t.root, real) {
		return "", fmt.Errorf("%s links outside the repository", name)
	}
	return path, nil
}

// within says path is root or below it.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// rel is a path as the model reads it: relative to the root, slash-separated.
func (t toolbox) rel(path string) string {
	if rel, err := filepath.Rel(t.root, path); err == nil {
		return filepath.ToSlash(rel)
	}
	return path
}

func (t toolbox) read(name string, offset, limit int) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", errors.New("name the file to read")
	}
	path, err := t.resolve(name)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("%s: %s", name, plainFSError(err))
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a folder; list it with list_dir", name)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("%s: %s", name, plainFSError(err))
	}
	defer file.Close()
	if offset < 1 {
		offset = 1
	}
	if limit <= 0 || limit > readLines {
		limit = readLines
	}
	var b strings.Builder
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	line, shown, more := 0, 0, false
	for scanner.Scan() {
		line++
		if line < offset {
			continue
		}
		if shown >= limit || b.Len() >= readBytes {
			more = true
			break
		}
		text := scanner.Text()
		if line == 1 && bytes.IndexByte([]byte(text), 0) >= 0 {
			return "", fmt.Errorf("%s is a binary file", name)
		}
		if len(text) > 2000 {
			text = text[:2000] + " …[line cut]"
		}
		fmt.Fprintf(&b, "%6d\t%s\n", line, text)
		shown++
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("%s: %v", name, err)
	}
	if shown == 0 {
		if line == 0 {
			return name + " is empty", nil
		}
		return fmt.Sprintf("%s has %d lines; there is nothing from line %d", name, line, offset), nil
	}
	if more {
		fmt.Fprintf(&b, "[more lines follow; read again with offset %d]\n", offset+shown)
	}
	return b.String(), nil
}

func (t toolbox) list(name string) (string, error) {
	path, err := t.resolve(name)
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("%s: %s", displayName(name), plainFSError(err))
	}
	var b strings.Builder
	for index, entry := range entries {
		if index == listEntries {
			fmt.Fprintf(&b, "[%d more entries not shown]\n", len(entries)-index)
			break
		}
		b.WriteString(entry.Name())
		if entry.IsDir() {
			b.WriteString("/")
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return displayName(name) + " is empty", nil
	}
	return b.String(), nil
}

func (t toolbox) glob(pattern string) (string, error) {
	pattern = strings.TrimPrefix(strings.TrimSpace(filepath.ToSlash(pattern)), "./")
	if pattern == "" {
		return "", errors.New("give a pattern, such as **/*.py")
	}
	match, err := globPattern(pattern)
	if err != nil {
		return "", err
	}
	var found []string
	more := false
	walkErr := t.walk(t.root, func(path string) bool {
		rel := t.rel(path)
		if match.MatchString(rel) {
			if len(found) == globMatches {
				more = true
				return false
			}
			found = append(found, rel)
		}
		return true
	})
	if walkErr != nil {
		return "", walkErr
	}
	if len(found) == 0 {
		return "no file matches " + pattern, nil
	}
	sort.Strings(found)
	out := strings.Join(found, "\n") + "\n"
	if more {
		out += fmt.Sprintf("[stopped at %d paths; narrow the pattern]\n", globMatches)
	}
	return out, nil
}

func (t toolbox) grep(expression, name, filter string, ignoreCase bool) (string, error) {
	if strings.TrimSpace(expression) == "" {
		return "", errors.New("give a pattern to search for")
	}
	if ignoreCase {
		expression = "(?i)" + expression
	}
	search, err := regexp.Compile(expression)
	if err != nil {
		return "", fmt.Errorf("the pattern is not a regular expression this search reads (RE2): %v", err)
	}
	var only *regexp.Regexp
	if filter = strings.TrimSpace(filepath.ToSlash(filter)); filter != "" {
		if !strings.Contains(filter, "/") {
			filter = "**/" + filter
		}
		if only, err = globPattern(filter); err != nil {
			return "", err
		}
	}
	start, err := t.resolve(name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	count, more := 0, false
	walkErr := t.walk(start, func(path string) bool {
		if only != nil && !only.MatchString(t.rel(path)) {
			return true
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() > grepFileBytes {
			return true
		}
		data, err := os.ReadFile(path)
		if err != nil || bytes.IndexByte(data[:min(len(data), 8000)], 0) >= 0 {
			return true
		}
		for number, line := range strings.Split(string(data), "\n") {
			if !search.MatchString(line) {
				continue
			}
			if count == grepMatches {
				more = true
				return false
			}
			if runes := []rune(line); len(runes) > grepLineRunes {
				line = string(runes[:grepLineRunes]) + " …"
			}
			fmt.Fprintf(&b, "%s:%d: %s\n", t.rel(path), number+1, strings.TrimRight(line, "\r"))
			count++
		}
		return true
	})
	if walkErr != nil {
		return "", walkErr
	}
	if count == 0 {
		return "no line matches", nil
	}
	if more {
		fmt.Fprintf(&b, "[stopped at %d lines; narrow the pattern or the path]\n", grepMatches)
	}
	return b.String(), nil
}

// walk visits every regular file under start, skipping the folders a search
// walks past and every link, until visit answers false.
func (t toolbox) walk(start string, visit func(path string) bool) error {
	info, err := os.Stat(start)
	if err != nil {
		return fmt.Errorf("%s: %s", t.rel(start), plainFSError(err))
	}
	if !info.IsDir() {
		visit(start)
		return nil
	}
	stop := errors.New("stop")
	err = filepath.WalkDir(start, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if path != start && skippedDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if !visit(path) {
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		return nil
	}
	return err
}

// globPattern turns a glob into a regular expression over slash paths: `**`
// crosses folders, `*` and `?` do not.
func globPattern(pattern string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for index := 0; index < len(pattern); index++ {
		switch c := pattern[index]; c {
		case '*':
			if index+1 < len(pattern) && pattern[index+1] == '*' {
				index++
				if index+1 < len(pattern) && pattern[index+1] == '/' {
					index++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	compiled, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("the pattern %q does not read as a glob: %v", pattern, err)
	}
	return compiled, nil
}

// plainFSError is a file error without the path the model already named.
func plainFSError(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "no such file or folder"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	}
	var path *fs.PathError
	if errors.As(err, &path) {
		return path.Err.Error()
	}
	return err.Error()
}

func displayName(name string) string {
	if strings.TrimSpace(name) == "" {
		return "the repository root"
	}
	return name
}
