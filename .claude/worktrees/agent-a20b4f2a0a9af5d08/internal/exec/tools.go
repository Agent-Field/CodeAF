package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The tool set is four tools, and the count is the design.
//
// Every definition is re-sent on every turn, and every result stays in context
// for every turn after it arrives, so the question is not "what would be
// convenient" but "what earns its place in a prompt paid for repeatedly".
//
//	sh     one definition buys read, list, search, find, move, curl, and
//	       everything nobody has thought of yet. The shell already composes, so
//	       batching needs no schema help.
//	write  because content must not pass through a shell. Emitting a document
//	       through a heredoc means quoting prose, which corrupts it and burns
//	       tokens on escaping.
//	edit   because changing a paragraph should cost a paragraph, not a whole
//	       file re-typed.
//	web    the only capability a shell genuinely lacks. urls is plural because
//	       research is search-once-then-read-several, and batching that turns
//	       five round-trips into one.
const (
	maxToolResultBytes = 12 << 10
	maxCommandSeconds  = 120

	// spillBytes is where a result stops being worth carrying. Anything larger
	// is written to a file and represented in context by a preview and a path.
	//
	// This is the single highest-leverage bound in the system. A turn resends
	// every earlier observation, so a k-turn loop pays for its results k times
	// over — one 25-turn leaf was 54% of a whole run's input tokens. Spilling
	// caps what any observation can cost, and the model loses nothing it cannot
	// get back: reading part of a file is one `sh` call away.
	//
	// The threshold sits where a typical single document — a source file, a
	// fetched page's article body — still fits whole. Set at 4KB it truncated
	// most of the very files an agent was asked to change, and the agent spent
	// its run re-fetching slices of things it had already read. Aging is the
	// decay pass's job; spill only has to stop the genuinely huge result.
	spillBytes   = 10 << 10
	previewBytes = 4 << 10
)

// Result is one tool's answer. A failure is a Result, never a Go error: the
// model has to see what went wrong to fix it, and aborting the loop over a
// mistyped path throws away every turn that came before.
type Result struct {
	Content string
	IsError bool
}

func errorf(format string, args ...any) Result {
	return Result{Content: fmt.Sprintf(format, args...), IsError: true}
}

// Toolbox executes tool calls against one workspace on behalf of one node.
type Toolbox struct {
	workspace *Workspace
	nodeID    int
	web       *Web
	spills    int
}

func NewToolbox(workspace *Workspace, nodeID int, web *Web) *Toolbox {
	return &Toolbox{workspace: workspace, nodeID: nodeID, web: web}
}

// Definitions are what the model sees. Descriptions are terse because they are
// resent every turn, but each one states the thing an agent gets wrong without
// being told.
func (t *Toolbox) Definitions() []ai.ToolDefinition {
	return []ai.ToolDefinition{
		define("sh", "Run a shell command in the workspace. Use it to read, list, search, and inspect. Chain with && and pipes to do several things in one call.", map[string]any{
			"cmd": prop("string", "shell command"),
			"t":   prop("integer", "timeout seconds, default 60"),
		}, "cmd"),
		define("write", "Write a file with exact content. Use this for any deliverable prose; never emit documents through sh.", map[string]any{
			"path": prop("string", "workspace-relative path"),
			"text": prop("string", "full file content"),
		}, "path", "text"),
		define("edit", "Replace an exact string in a file. Far cheaper than rewriting a long file to change part of it.", map[string]any{
			"path": prop("string", "workspace-relative path"),
			"old":  prop("string", "exact text to replace, must appear once"),
			"new":  prop("string", "replacement text"),
		}, "path", "old", "new"),
		define("web", "Search the web, or fetch pages as text. Pass q to search. Pass urls to fetch several pages in one call, which is much faster than one at a time.", map[string]any{
			"q":    prop("string", "search query"),
			"urls": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "page urls to fetch"},
			"n":    prop("integer", "max search results, default 6"),
		}),
	}
}

// Execute dispatches one call. An unknown name is answered with the valid list
// rather than refused, because a model that guessed a tool name can recover
// from being told the real ones and cannot recover from a dead loop.
func (t *Toolbox) Execute(ctx context.Context, name string, arguments string) Result {
	var args map[string]any
	if strings.TrimSpace(arguments) != "" {
		if err := json.Unmarshal([]byte(arguments), &args); err != nil {
			return errorf("arguments were not valid JSON: %v", err)
		}
	}
	var result Result
	switch name {
	case "sh":
		result = t.sh(ctx, args)
	case "write":
		result = t.write(args)
	case "edit":
		result = t.edit(args)
	case "web":
		result = t.webCall(ctx, args)
	default:
		return errorf("no tool named %q. Available: sh, write, edit, web", name)
	}
	return t.spill(result)
}

// spill moves a large result out of context and leaves a pointer to it. Errors
// are never spilled: they are usually short, and the whole value of an error is
// that the model reads it immediately rather than going to fetch it.
func (t *Toolbox) spill(result Result) Result {
	if result.IsError || len(result.Content) <= spillBytes {
		return result
	}
	t.spills++
	relative := filepath.Join(obsDir, fmt.Sprintf("%d-%d.txt", t.nodeID, t.spills))
	full, err := t.workspace.Resolve(relative)
	if err != nil {
		return result
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return result
	}
	if err := os.WriteFile(full, []byte(result.Content), 0o644); err != nil {
		return result
	}
	return Result{Content: fmt.Sprintf(
		"%s\n\n... [%d of %d bytes shown. Full output saved to %s — read the part you need with sh, for example: sed -n '1,80p' %s]",
		result.Content[:previewBytes], previewBytes, len(result.Content), relative, relative)}
}

func (t *Toolbox) sh(ctx context.Context, args map[string]any) Result {
	command := stringArg(args, "cmd")
	if command == "" {
		return errorf("sh needs cmd")
	}
	seconds := intArg(args, "t", 60)
	if seconds <= 0 || seconds > maxCommandSeconds {
		seconds = maxCommandSeconds
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = t.workspace.Root()
	output, err := cmd.CombinedOutput()
	body := clamp(string(output))
	if runCtx.Err() == context.DeadlineExceeded {
		return errorf("command timed out after %ds. Partial output:\n%s", seconds, body)
	}
	if err != nil {
		// The exit status matters less than the output; a build failure's value
		// is entirely in what it printed.
		return Result{Content: fmt.Sprintf("exit: %v\n%s", err, body), IsError: true}
	}
	if strings.TrimSpace(body) == "" {
		return Result{Content: "(no output)"}
	}
	return Result{Content: body}
}

func (t *Toolbox) write(args map[string]any) Result {
	path, text := stringArg(args, "path"), stringArg(args, "text")
	if path == "" {
		return errorf("write needs path")
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return errorf("could not create directory: %v", err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		return errorf("could not write %s: %v", path, err)
	}
	t.workspace.Record(t.nodeID, full)
	return Result{Content: fmt.Sprintf("wrote %s (%d bytes)", path, len(text))}
}

func (t *Toolbox) edit(args map[string]any) Result {
	path, old, replacement := stringArg(args, "path"), stringArg(args, "old"), stringArg(args, "new")
	if path == "" || old == "" {
		return errorf("edit needs path and old")
	}
	full, err := t.workspace.Resolve(path)
	if err != nil {
		return errorf("%v", err)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return errorf("could not read %s: %v", path, err)
	}
	body := string(data)
	// Ambiguity is reported rather than resolved. Replacing the first of three
	// matches silently is the kind of edit that looks like it worked.
	switch strings.Count(body, old) {
	case 0:
		return errorf("that exact text is not in %s. Read the file and match it byte for byte.", path)
	case 1:
	default:
		return errorf("that text appears %d times in %s. Include more surrounding context so it matches once.", strings.Count(body, old), path)
	}
	updated := strings.Replace(body, old, replacement, 1)
	if err := os.WriteFile(full, []byte(updated), 0o644); err != nil {
		return errorf("could not write %s: %v", path, err)
	}
	t.workspace.Record(t.nodeID, full)
	return Result{Content: fmt.Sprintf("edited %s (%d bytes)", path, len(updated))}
}

func (t *Toolbox) webCall(ctx context.Context, args map[string]any) Result {
	if t.web == nil {
		return errorf("web is not configured (set EXA_API_KEY)")
	}
	query := stringArg(args, "q")
	urls := stringsArg(args, "urls")
	if query == "" && len(urls) == 0 {
		return errorf("web needs q or urls")
	}
	var sections []string
	if query != "" {
		found, err := t.web.Search(ctx, query, intArg(args, "n", 6))
		if err != nil {
			return errorf("search failed: %v", err)
		}
		sections = append(sections, found)
	}
	if len(urls) > 0 {
		sections = append(sections, t.web.Fetch(ctx, urls))
	}
	return Result{Content: clamp(strings.Join(sections, "\n\n"))}
}

// clamp bounds a result at both ends.
//
// Keeping only the head is the obvious implementation and the wrong one: a
// command's most valuable line is usually its last, because that is where the
// error is. Cutting the middle keeps the shape of the output and the verdict at
// the end, and says plainly how much went missing so the model can go looking
// for it if it matters.
func clamp(text string) string {
	if len(text) <= maxToolResultBytes {
		return text
	}
	head := maxToolResultBytes * 2 / 3
	tail := maxToolResultBytes - head
	return text[:head] +
		fmt.Sprintf("\n\n... [%d bytes elided] ...\n\n", len(text)-maxToolResultBytes) +
		text[len(text)-tail:]
}

func define(name, description string, properties map[string]any, required ...string) ai.ToolDefinition {
	parameters := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return ai.ToolDefinition{Type: "function", Function: ai.ToolFunction{
		Name: name, Description: description, Parameters: parameters,
	}}
}

func prop(kind, description string) map[string]any {
	return map[string]any{"type": kind, "description": description}
}

func stringArg(args map[string]any, key string) string {
	if value, ok := args[key].(string); ok {
		return value
	}
	return ""
}

func intArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	}
	return fallback
}

func stringsArg(args map[string]any, key string) []string {
	raw, ok := args[key].([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			values = append(values, text)
		}
	}
	return values
}
