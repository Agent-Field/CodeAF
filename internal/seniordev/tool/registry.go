//go:build !windows

// Package tool is the workspace-bound tool registry: the tools the model can
// call, each confined to a single workspace and gated by the permission rules.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/seniordev/baked"
	"github.com/Agent-Field/codeaf/internal/seniordev/config"
	"github.com/Agent-Field/codeaf/internal/seniordev/core"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/orclient"
	"github.com/Agent-Field/codeaf/internal/seniordev/engine/steploop"
	"github.com/Agent-Field/codeaf/internal/seniordev/permission"
	"github.com/Agent-Field/codeaf/internal/seniordev/project"
	"github.com/Agent-Field/codeaf/internal/seniordev/question"
	"github.com/Agent-Field/codeaf/internal/seniordev/session/instruction"
)

const (
	bashSchema = `{
		"type": "object",
		"properties": {
			"command": {"type": "string"},
			"workdir": {"type": "string"},
			"timeout_ms": {"type": "integer", "minimum": 1, "maximum": 600000}
		},
		"required": ["command"],
		"additionalProperties": false
	}`
	readSchema = `{
		"type": "object",
		"properties": {
			"filePath": {"type": "string", "description": "The absolute path to the file or directory to read"},
			"offset": {"type": "integer", "minimum": 0, "description": "The line number to start reading from (1-indexed)"},
			"limit": {"type": "integer", "minimum": 0, "description": "The maximum number of lines to read (defaults to 2000)"}
		},
		"required": ["filePath"],
		"additionalProperties": false
	}`
	writeSchema = `{
		"type": "object",
		"properties": {
			"content": {"type": "string", "description": "The content to write to the file"},
			"filePath": {"type": "string", "description": "The absolute path to the file to write (must be absolute, not relative)"}
		},
		"required": ["content", "filePath"],
		"additionalProperties": false
	}`
	editSchema = `{
		"type": "object",
		"properties": {
			"filePath": {"type": "string", "description": "The absolute path to the file to modify"},
			"oldString": {"type": "string", "description": "The text to replace"},
			"newString": {"type": "string", "description": "The text to replace it with (must be different from oldString)"},
			"replaceAll": {"type": "boolean", "description": "Replace all occurrences of oldString (default false)"}
		},
		"required": ["filePath", "oldString", "newString"],
		"additionalProperties": false
	}`
	globSchema = `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "The glob pattern to match files against"},
			"path": {"type": "string", "description": "The directory to search in. If not specified, the current working directory will be used. IMPORTANT: Omit this field to use the default directory. DO NOT enter \"undefined\" or \"null\" - simply omit it for the default behavior. Must be a valid directory path if provided."}
		},
		"required": ["pattern"],
		"additionalProperties": false
	}`
	grepSchema = `{
		"type": "object",
		"properties": {
			"pattern": {"type": "string", "description": "The regex pattern to search for in file contents"},
			"path": {"type": "string", "description": "The directory to search in. Defaults to the current working directory."},
			"include": {"type": "string", "description": "File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")"}
		},
		"required": ["pattern"],
		"additionalProperties": false
	}`
	applyPatchSchema = `{
		"type": "object",
		"properties": {
			"patchText": {"type": "string", "description": "The full patch text that describes all changes to be made"}
		},
		"required": ["patchText"],
		"additionalProperties": false
	}`
)

type bashInput struct {
	Command   string `json:"command"`
	Workdir   string `json:"workdir,omitempty"`
	TimeoutMS *int   `json:"timeout_ms,omitempty"`
}

type readInput struct {
	FilePath string `json:"filePath"`
	Offset   *int   `json:"offset,omitempty"`
	Limit    *int   `json:"limit,omitempty"`
}

type writeInput struct {
	Content  string `json:"content"`
	FilePath string `json:"filePath"`
}

type editInput struct {
	FilePath   string `json:"filePath"`
	OldString  string `json:"oldString"`
	NewString  string `json:"newString"`
	ReplaceAll bool   `json:"replaceAll,omitempty"`
}

type globInput struct {
	Pattern string  `json:"pattern"`
	Path    *string `json:"path,omitempty"`
}

type applyPatchInput struct {
	PatchText string `json:"patchText"`
}

// Registry is a collection of tools whose file operations are confined to a
// single workspace.
type Registry struct {
	workDir           string
	instance          *project.InstanceContext
	rg                ripgrepRunner
	instructions      *instructionRegistry
	permission        PermissionEvaluator
	rules             PermissionRules
	config            *config.Service
	formatters        *formatterServices
	npm               *core.Npm
	instructionConfig instruction.Config
	allowExternal     bool
	confineWrites     bool
	hardConfineShell  bool
	shellProcesses    *shellProcessRegistry
	question          *question.Service
	questionEnabled   bool
	questionRejects   *questionRejectionState
	// submitFreeze captures the candidate when the model calls submit. Nil in
	// embedders that do not run a submission protocol; the tool is then not
	// advertised at all rather than advertised and refused.
	submitFreeze SubmitFreezer
}

type questionRejectionState struct {
	mu     sync.Mutex
	counts map[string]int
}

// All shallow workspace views of a run's registry share its shell groups, so
// closing the run reaches commands started through any of those views.
type shellProcessRegistry struct {
	mu     sync.Mutex
	groups map[int]struct{}
	closed bool
}

type instructionRegistry struct {
	mu       sync.Mutex
	services map[string]*instruction.Service
}

// PermissionRules resolves the agent and instance rules for one tool call.
type PermissionRules func(context.Context, steploop.ToolCall) permission.Ruleset

// PermissionEvaluator decides one permission request; mutation requests carry
// the proposed diff in their metadata.
type PermissionEvaluator interface {
	Evaluate(permission.AskInput) error
}

// RegistryOptions supplies the host services used by live tool execution.
type RegistryOptions struct {
	Permission               PermissionEvaluator
	PermissionRules          PermissionRules
	Instructions             []string
	Config                   *config.Service
	AllowExternalDirectories bool
	// ConfineWrites refuses every file write outside the workspace, whatever
	// AllowExternalDirectories says of reads: codeaf keeps only what a program
	// changes in the folder it is handed, so a write anywhere else is work that
	// no run owns and a change made to somebody else's folder (path.go).
	ConfineWrites bool
	// HardConfineShellPaths rejects parsed external shell operands instead of
	// asking permission, for an embedder that must not prompt. senior-dev leaves it
	// disabled and asks.
	HardConfineShellPaths bool
	// ClientIdentity names the kind of client driving the registry (app, cli,
	// desktop) and decides whether the question tool is advertised. The senior-dev
	// binary supplies "cli" unless SENIOR_DEV_CLIENT overrides it; embedders that
	// omit it are not assumed to have an interactive client.
	ClientIdentity string
	Question       *question.Service
	// SubmitFreeze installs the submit tool and receives the candidate at the
	// moment the model submits. See submit.go.
	SubmitFreeze SubmitFreezer
}

var bakedPermissionCache sync.Map

func bakedPermissionRules(_ context.Context, call steploop.ToolCall) permission.Ruleset {
	if call.Agent == "" {
		return nil
	}
	if cached, ok := bakedPermissionCache.Load(call.Agent); ok {
		return cached.(permission.Ruleset)
	}
	markdown, ok := baked.GetBakedAgentMarkdown(call.Agent)
	if !ok {
		return nil
	}
	rules, err := permission.RulesetFromFrontmatter(markdown)
	if err != nil {
		return nil
	}
	bakedPermissionCache.Store(call.Agent, rules)
	return rules
}

// WebSearchFlags are the two feature flags consulted by webSearchEnabled.
type WebSearchFlags struct {
	Exa      bool
	Parallel bool
}

// FilterInput carries the provider, model and search flags that decide which
// tools are advertised for a turn.
type FilterInput struct {
	ProviderID string
	ModelID    string
	Flags      WebSearchFlags
}

// New returns a registry bound to workDir.
func New(workDir string) *Registry {
	return NewWithOptions(workDir, RegistryOptions{})
}

// NewWithOptions returns a configured registry bound to workDir.
func NewWithOptions(workDir string, options RegistryOptions) *Registry {
	absolute, err := filepath.Abs(workDir)
	if err != nil {
		absolute = workDir
	}
	service := options.Permission
	if service == nil {
		service = &permission.Service{}
	}
	rules := options.PermissionRules
	if rules == nil {
		rules = bakedPermissionRules
	}
	cacheDir, _ := os.UserCacheDir()
	configService := options.Config
	if configService == nil {
		configService = config.NewService(config.Loader{Env: config.NewEnv(nil)})
	}
	env := config.NewEnv(nil)
	clientIdentity := options.ClientIdentity
	if clientIdentity == "" {
		if configured, ok := env.Get("SENIOR_DEV_CLIENT"); ok {
			clientIdentity = configured
		}
	}
	questionService := options.Question
	if questionService == nil {
		questionService = question.Default
	}
	return &Registry{
		workDir:          filepath.Clean(absolute),
		rg:               pickRipgrepRunner(),
		permission:       service,
		rules:            rules,
		config:           configService,
		allowExternal:    options.AllowExternalDirectories,
		confineWrites:    options.ConfineWrites,
		hardConfineShell: options.HardConfineShellPaths,
		question:         questionService,
		shellProcesses:   &shellProcessRegistry{groups: make(map[int]struct{})},
		questionEnabled:  clientIdentity != "hosted" && (clientIdentity == "app" || clientIdentity == "cli" || clientIdentity == "desktop" || env.Enabled("SENIOR_DEV_ENABLE_QUESTION_TOOL")),
		questionRejects:  &questionRejectionState{counts: map[string]int{}},
		submitFreeze:     options.SubmitFreeze,
		formatters:       newFormatterServices(),
		npm:              core.NewNpm(filepath.Join(cacheDir, "senior-dev"), nil),
		instructionConfig: instruction.Config{
			Instructions: append([]string(nil), options.Instructions...),
		},
		instructions: &instructionRegistry{
			services: map[string]*instruction.Service{},
		},
	}
}

// Definitions returns the provider declarations for all workspace tools.
func (r *Registry) Definitions() []steploop.ToolDefinition {
	definitions := make([]steploop.ToolDefinition, 0, 10)
	// The question tool is advertised for interactive client identities (app,
	// cli, desktop) or when SENIOR_DEV_ENABLE_QUESTION_TOOL forces it on for an
	// embedder.
	if r.questionEnabled {
		question := definition("question", questionDescription, questionSchema, validateQuestion)
		// A question blocks until it is answered or rejected, so the result stream
		// is held open for it rather than settled on the usual abort timer.
		question.WaitForResult = true
		definitions = append(definitions, question)
	}
	if r.submitFreeze != nil {
		definitions = append(definitions,
			definition("submit", submitDescription, submitSchema, validateSubmit))
	}
	return append(definitions,
		definition("bash", "Run a Bash command in the workspace. Output is capped at 30000 bytes and execution at 600000ms.", bashSchema, validateBash),
		definition("read", readDescription, readSchema, validateRead),
		definition("glob", globDescription, globSchema, validateGlob),
		definition("grep", grepDescription, grepSchema, validateGrep),
		definition("edit", editDescription, editSchema, validateEdit),
		definition("write", writeDescription, writeSchema, validateWrite),
		definition("webfetch", webFetchDescription, webFetchSchema, validateWebFetch),
		definition("websearch", webSearchDescription(), webSearchSchema, validateWebSearch),
		definition("apply_patch", applyPatchDescription, applyPatchSchema, validateApplyPatch),
	)
}

// IDs returns builtin tool IDs in registry insertion order.
func (r *Registry) IDs() []string {
	definitions := r.Definitions()
	out := make([]string, 0, len(definitions))
	for _, item := range definitions {
		out = append(out, item.Provider.Name)
	}
	return out
}

// DefinitionsFor applies the registry's provider and model-family visibility
// rules.
func (r *Registry) DefinitionsFor(input FilterInput) []steploop.ToolDefinition {
	return FilterDefinitions(r.Definitions(), input)
}

// WebSearchEnabled reports whether a search backend is available: the senior-dev
// provider, or an Exa or Parallel flag.
func WebSearchEnabled(providerID string, flags WebSearchFlags) bool {
	return providerID == "senior-dev" || flags.Exa || flags.Parallel
}

// FilterDefinitions narrows the advertised tool list to what the provider and
// model can use: websearch needs a search backend, and GPT-family models get
// apply_patch in place of edit/write. It is separate from Registry so
// plugin/custom definitions can pass through the same seam.
func FilterDefinitions(
	definitions []steploop.ToolDefinition,
	input FilterInput,
) []steploop.ToolDefinition {
	usePatch := strings.Contains(input.ModelID, "gpt-") &&
		!strings.Contains(input.ModelID, "oss") &&
		!strings.Contains(input.ModelID, "gpt-4")
	out := make([]steploop.ToolDefinition, 0, len(definitions))
	for _, item := range definitions {
		id := item.Provider.Name
		if id == "websearch" && !WebSearchEnabled(input.ProviderID, input.Flags) {
			continue
		}
		if id == "apply_patch" && !usePatch {
			continue
		}
		if (id == "edit" || id == "write") && usePatch {
			continue
		}
		out = append(out, item)
	}
	return out
}

func definition(name, description, schema string, validate func(json.RawMessage) error) steploop.ToolDefinition {
	return steploop.ToolDefinition{
		Provider: orclient.Tool{
			Type:        "function",
			Name:        name,
			Description: description,
			InputSchema: json.RawMessage(schema),
		},
		Validate: validate,
	}
}

// Execute dispatches an already-validated call to its named tool.
func (r *Registry) Execute(ctx context.Context, call steploop.ToolCall) (steploop.ToolResult, error) {
	r = r.forContext(ctx)
	var result steploop.ToolResult
	var err error
	switch call.Name {
	case "question":
		return r.executeQuestion(ctx, call)
	case "submit":
		result, err = r.executeSubmit(ctx, call)
	case "bash":
		result, err = r.executeBash(ctx, call)
	case "read":
		result, err = r.executeRead(ctx, call)
	case "glob":
		result, err = r.executeGlob(ctx, call)
	case "grep":
		result, err = r.executeGrep(ctx, call)
	case "write":
		result, err = r.executeWrite(ctx, call)
	case "edit":
		result, err = r.executeEdit(ctx, call)
	case "apply_patch":
		result, err = r.executeApplyPatch(ctx, call)
	case "webfetch":
		result, err = r.executeWebFetch(ctx, call)
	case "websearch":
		result, err = r.executeWebSearch(ctx, call)
	default:
		return steploop.ToolResult{}, fmt.Errorf("unknown tool: %s", call.Name)
	}
	if err == nil {
		r.resetQuestionRejections(call.SessionID)
	}
	return result, err
}

func (r *Registry) resetQuestionRejections(sessionID string) {
	if r.questionRejects == nil {
		return
	}
	r.questionRejects.mu.Lock()
	delete(r.questionRejects.counts, sessionID)
	r.questionRejects.mu.Unlock()
}

func (r *Registry) recordQuestionRejection(sessionID string) int {
	if r.questionRejects == nil {
		return 1
	}
	r.questionRejects.mu.Lock()
	defer r.questionRejects.mu.Unlock()
	r.questionRejects.counts[sessionID]++
	return r.questionRejects.counts[sessionID]
}

func (r *Registry) instructionService() *instruction.Service {
	worktree := r.workDir
	if r.instance != nil && r.instance.Worktree != "" {
		worktree = filepath.Clean(r.instance.Worktree)
	}
	key := r.workDir + "\x00" + worktree
	r.instructions.mu.Lock()
	defer r.instructions.mu.Unlock()
	if service := r.instructions.services[key]; service != nil {
		return service
	}
	home, _ := os.UserHomeDir()
	config, _ := os.UserConfigDir()
	service := instruction.New(instruction.Options{
		Config: r.instructionConfig,
		Global: instruction.Global{Config: filepath.Join(config, "senior-dev"), Home: home},
		Instance: instruction.Instance{
			Directory: r.workDir,
			Worktree:  worktree,
		},
	})
	r.instructions.services[key] = service
	return service
}

func (r *Registry) ask(
	ctx context.Context,
	call steploop.ToolCall,
	name string,
	patterns []string,
	metadata map[string]any,
) error {
	return r.askWithAlways(ctx, call, name, patterns, []string{"*"}, metadata)
}

func (r *Registry) askWithAlways(
	ctx context.Context,
	call steploop.ToolCall,
	name string,
	patterns []string,
	always []string,
	metadata map[string]any,
) error {
	rules := permission.Ruleset(nil)
	if r.rules != nil {
		rules = r.rules(ctx, call)
	}
	return r.permission.Evaluate(permission.AskInput{
		Request: permission.Request{
			SessionID: call.SessionID, Permission: name, Patterns: patterns,
			Metadata: metadata, Always: always,
		},
		Ruleset: rules,
	})
}

// SystemInstructions returns the root/global instruction blocks used by the
// engine system prompt for the same workspace-bound service as read tools.
func (r *Registry) SystemInstructions(ctx context.Context) []string {
	return r.forContext(ctx).instructionService().System(ctx)
}

// ClearInstructionClaims releases one assistant turn's in-flight nested-path
// claims. Persisted read metadata remains the cross-turn loaded-path memory.
func (r *Registry) ClearInstructionClaims(ctx context.Context, messageID string) {
	r.forContext(ctx).instructionService().Clear(messageID)
}

// forContext resolves the per-leaf cwd at call time. A shallow clone makes a
// single registry safe for concurrent leaf contexts.
func (r *Registry) forContext(ctx context.Context) *Registry {
	instance, ok := project.FromContext(ctx)
	if !ok || instance.Directory == "" {
		return r
	}
	copy := *r
	copy.workDir = filepath.Clean(instance.Directory)
	copy.instance = &instance
	return &copy
}

func validateBash(raw json.RawMessage) error {
	var input bashInput
	if err := decodeInput(raw, &input, "command"); err != nil {
		return err
	}
	if input.TimeoutMS != nil && (*input.TimeoutMS < 1 || *input.TimeoutMS > 600000) {
		return fmt.Errorf("timeout_ms must be between 1 and 600000")
	}
	return nil
}

func validateRead(raw json.RawMessage) error {
	var input readInput
	if err := decodeInput(raw, &input, "filePath"); err != nil {
		return err
	}
	if input.Offset != nil && *input.Offset < 0 {
		return fmt.Errorf("offset must be at least 0")
	}
	if input.Limit != nil && *input.Limit < 0 {
		return fmt.Errorf("limit must be at least 0")
	}
	return nil
}

func validateWrite(raw json.RawMessage) error {
	var input writeInput
	return decodeInput(raw, &input, "content", "filePath")
}

func validateGlob(raw json.RawMessage) error {
	var input globInput
	return decodeInput(raw, &input, "pattern")
}

func validateGrep(raw json.RawMessage) error {
	var input grepInput
	return decodeInput(raw, &input, "pattern")
}

func validateEdit(raw json.RawMessage) error {
	var input editInput
	return decodeInput(raw, &input, "filePath", "oldString", "newString")
}

func validateApplyPatch(raw json.RawMessage) error {
	var input applyPatchInput
	return decodeInput(raw, &input, "patchText")
}

func decodeInput(raw json.RawMessage, destination any, required ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return fmt.Errorf("input must be a JSON object: %w", err)
	}
	if fields == nil {
		return fmt.Errorf("input must be a JSON object")
	}
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			return fmt.Errorf("missing required field %q", name)
		}
	}
	for name, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("field %q must not be null", name)
		}
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return err
	}
	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return fmt.Errorf("invalid input: %w", err)
	}
	return fmt.Errorf("input must contain one JSON object")
}
