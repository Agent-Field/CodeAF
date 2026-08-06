package exec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/aforge-v2/internal/store"
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
	// maxRecallResultBytes bounds persistent memory more tightly than ordinary
	// observations: recall is a map used to choose what to read, not a second
	// copy of the territory.
	maxRecallResultBytes = 8 << 10
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
	history   *store.Store
	// spills is atomic because a turn's tool calls execute concurrently, and
	// two large results spilling at once must not race the counter into the
	// same file name.
	spills atomic.Int64
}

func NewToolbox(workspace *Workspace, nodeID int, web *Web) *Toolbox {
	return &Toolbox{workspace: workspace, nodeID: nodeID, web: web}
}

// NewToolboxWithStore adds persistent recall to the generic toolbox. A nil
// store deliberately collapses to NewToolbox so one-shot leaves retain the
// original four-definition prompt.
func NewToolboxWithStore(workspace *Workspace, nodeID int, web *Web, history *store.Store) *Toolbox {
	return &Toolbox{workspace: workspace, nodeID: nodeID, web: web, history: history}
}

// Definitions are what the model sees. Descriptions are terse because they are
// resent every turn, but each one states the thing an agent gets wrong without
// being told.
func (t *Toolbox) Definitions() []ai.ToolDefinition {
	definitions := []ai.ToolDefinition{
		define("sh", "Run a shell command in the workspace. Use it to read, list, search, and inspect. cmd is one command string (chain with && and pipes), or an array of commands run in order, stopping at the first failure. For INDEPENDENT commands, prefer separate sh calls in the same turn — they run at the same time.", map[string]any{
			"cmd": prop("string", "shell command, or an array of commands run serially"),
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
	if t.history != nil {
		definitions = append(definitions, define("recall", "Search folded work and the notebook. Recall gives the map, not the territory: use the returned digest to choose what matters, then read the returned pointer paths with sh for the verbatim details. terms are free text; scope_cues are optional workspace or file paths.", map[string]any{
			"terms":      prop("string", "words describing the prior work or lesson"),
			"scope_cues": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "optional workspace or file paths"},
			"limit":      prop("integer", "maximum fold and notebook hits, default 5, maximum 10"),
		}, "terms"))
	}
	return definitions
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
	case "recall":
		result = t.recall(args)
	default:
		available := "sh, write, edit, web"
		if t.history != nil {
			available += ", recall"
		}
		return errorf("no tool named %q. Available: %s", name, available)
	}
	return t.spill(result)
}

type recallToolFact struct {
	NodeID   string         `json:"node_id,omitempty"`
	Scope    string         `json:"scope"`
	Kind     store.FactKind `json:"kind"`
	Body     string         `json:"body"`
	Pointers []string       `json:"pointers"`
	Age      string         `json:"age"`
}

type recallToolResponse struct {
	Folds    []store.RecallHit `json:"folds"`
	Notebook []recallToolFact  `json:"notebook"`
}

func (t *Toolbox) recall(args map[string]any) Result {
	if t.history == nil {
		return errorf("recall is not available without an attached store")
	}
	terms := strings.TrimSpace(stringArg(args, "terms"))
	cues := stringsArg(args, "scope_cues")
	if terms == "" && len(cues) == 0 {
		return errorf("recall needs terms or scope_cues")
	}
	limit := intArg(args, "limit", 5)
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	folds, err := t.history.Recall(terms, cues, limit)
	if err != nil {
		return errorf("recall folds: %v", err)
	}
	facts, err := t.history.SearchFacts(store.FactQuery{Cues: recallFactCues(cues), Terms: terms, Limit: limit})
	if err != nil {
		return errorf("recall notebook: %v", err)
	}

	response := recallToolResponse{Folds: make([]store.RecallHit, 0), Notebook: make([]recallToolFact, 0)}
	for _, hit := range folds {
		hit.Intent = recallClip(hit.Intent, 512)
		hit.Digest = recallClip(hit.Digest, 2<<10)
		if len(hit.Pointers) > 8 {
			hit.Pointers = hit.Pointers[:8]
		}
		for index := range hit.Pointers {
			hit.Pointers[index] = recallClip(hit.Pointers[index], 512)
		}
		candidate := response
		candidate.Folds = append(append([]store.RecallHit(nil), response.Folds...), hit)
		if recallJSONFits(candidate) {
			response = candidate
		}
	}
	now := time.Now().UTC()
	for _, fact := range facts {
		item := recallToolFact{NodeID: fact.NodeID, Scope: recallClip(fact.Scope, 256), Kind: fact.Kind,
			Body: recallClip(fact.Body, store.MaxFactBytes), Pointers: t.foldPointers(fact.NodeID),
			Age: store.AgeLabel(fact.Time, now)}
		candidate := response
		candidate.Notebook = append(append([]recallToolFact(nil), response.Notebook...), item)
		if recallJSONFits(candidate) {
			response = candidate
		}
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		return errorf("encode recall: %v", err)
	}
	return Result{Content: string(encoded)}
}

func (t *Toolbox) foldPointers(nodeID string) []string {
	for nodeID != "" && nodeID != store.RootID {
		node, ok, err := t.history.Node(nodeID)
		if err != nil || !ok {
			return nil
		}
		if node.FoldRoot {
			pointers := append([]string(nil), node.FoldPointers...)
			if len(pointers) > 8 {
				pointers = pointers[:8]
			}
			for index := range pointers {
				pointers[index] = recallClip(pointers[index], 512)
			}
			return pointers
		}
		nodeID = node.Parent
	}
	return nil
}

func recallFactCues(cues []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(cues)*3)
	add := func(cue string) {
		cue = strings.TrimSpace(cue)
		if cue == "" || seen[cue] {
			return
		}
		seen[cue] = true
		result = append(result, cue)
	}
	for _, cue := range cues {
		add(cue)
		if strings.Contains(cue, ":") {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(cue))
		add("file:" + cleaned)
		add("repo:" + cleaned)
		if parent := filepath.ToSlash(filepath.Dir(cleaned)); parent != "." && parent != cleaned {
			add("repo:" + parent)
		}
	}
	return result
}

func recallJSONFits(response recallToolResponse) bool {
	encoded, err := json.Marshal(response)
	return err == nil && len(encoded) <= maxRecallResultBytes
}

func recallClip(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	cut := limit - len("...")
	for cut > 0 && !utf8.ValidString(value[:cut]) {
		cut--
	}
	return value[:cut] + "..."
}

// spill moves a large result out of context and leaves a pointer to it. Errors
// are never spilled: they are usually short, and the whole value of an error is
// that the model reads it immediately rather than going to fetch it.
func (t *Toolbox) spill(result Result) Result {
	if result.IsError || len(result.Content) <= spillBytes {
		return result
	}
	relative, ok := t.writeObs(fmt.Sprintf("%d-%d.txt", t.nodeID, t.spills.Add(1)), result.Content)
	if !ok {
		return result
	}
	return Result{Content: fmt.Sprintf(
		"%s\n\n... [%d of %d bytes shown. Full output saved to %s — read the part you need with sh, for example: sed -n '1,80p' %s]",
		result.Content[:previewBytes], previewBytes, len(result.Content), relative, relative)}
}

// decaySpill preserves a decaying observation's full body under the
// workspace's observation directory. The file is named by tool call id, so
// writing is naturally idempotent — the decayer additionally guarantees it is
// invoked at most once per id.
func (t *Toolbox) decaySpill(toolCallID, body string) (string, bool) {
	return t.writeObs(fmt.Sprintf("%d-decay-%s.txt", t.nodeID, safeName(toolCallID)), body)
}

// writeObs writes one observation file and returns its workspace-relative
// path. It is the one place spilled bytes land, shared by the size-triggered
// spill and the decay pass.
func (t *Toolbox) writeObs(name, content string) (string, bool) {
	relative := filepath.Join(obsDir, name)
	full, err := t.workspace.Resolve(relative)
	if err != nil {
		return "", false
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return "", false
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", false
	}
	return relative, true
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// safeName makes a tool call id usable as a file name. Ids differ at the tail,
// so that is the part kept when one is too long.
func safeName(id string) string {
	cleaned := unsafeName.ReplaceAllString(id, "")
	if len(cleaned) > 40 {
		cleaned = cleaned[len(cleaned)-40:]
	}
	if cleaned == "" {
		cleaned = "x"
	}
	return cleaned
}

func (t *Toolbox) sh(ctx context.Context, args map[string]any) Result {
	command := stringArg(args, "cmd")
	if command == "" {
		// An array is an explicit serial script: each step runs only when
		// the one before it succeeded, exactly like hand-written a && b.
		if list, ok := args["cmd"].([]any); ok {
			steps := make([]string, 0, len(list))
			for _, step := range list {
				if text, ok := step.(string); ok && strings.TrimSpace(text) != "" {
					steps = append(steps, strings.TrimSpace(text))
				}
			}
			command = strings.Join(steps, " && ")
		}
	}
	if command == "" {
		return errorf("sh needs cmd")
	}
	seconds := intArg(args, "t", 60)
	if seconds <= 0 || seconds > maxCommandSeconds {
		seconds = maxCommandSeconds
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	var environment []string
	if t.history != nil {
		if bin, err := store.SkillsBinDir(); err == nil {
			environment = os.Environ()
			// The login shell may rewrite inherited PATH while reading its
			// profile. Export inside that shell so a store adds the learned
			// shelf without changing the benchmarked no-store command path.
			environment = replaceEnv(environment, "AFORGE_SKILLS_BIN", bin)
			command = "export PATH=\"${AFORGE_SKILLS_BIN:?}:$PATH\"\n" + command
		}
	}

	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = t.workspace.Root()
	if environment != nil {
		cmd.Env = environment
	}
	// A command that leaves a background child sharing its stdout used to hang
	// the whole run: killing bash at the timeout is not enough, because Wait
	// blocks until every inherited pipe writer exits, and a scheduler goroutine
	// stuck there wedges the graph silently and forever. The process group
	// makes the timeout kill reach grandchildren, and WaitDelay force-closes
	// the pipes shortly after bash itself is gone for anything that survives —
	// a stuck tool call must cost its timeout, never the run.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 3 * time.Second
	output, err := cmd.CombinedOutput()
	body := clamp(string(output))
	if runCtx.Err() == context.DeadlineExceeded {
		return errorf("command timed out after %ds. Partial output:\n%s", seconds, body)
	}
	if errors.Is(err, exec.ErrWaitDelay) {
		// The command itself finished; something it started in the background
		// kept the output pipe open until the grace ran out. That is a
		// completed command with a detached child, not a failure.
		return Result{Content: body + "\n(a background process the command started was left running detached)"}
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

func replaceEnv(environment []string, key, value string) []string {
	prefix := key + "="
	replaced := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			replaced = append(replaced, entry)
		}
	}
	return append(replaced, prefix+value)
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
