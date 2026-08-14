// Package tool ports the workspace-bound registry from swe-pro/src/tool/registry.ts.
package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/baked"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/core"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/orclient"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/permission"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/plandb"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/project"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/question"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/instruction"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/observer"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/testmemo"
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
	planRun           PlanRunFunc
	planActive        PlanDBActiveStore
	config            *config.Service
	formatters        *formatterServices
	npm               *core.Npm
	instructionConfig instruction.Config
	allowExternal     bool
	hardConfineShell  bool
	testMemo          *testmemo.TestCommandMemo
	testMemoMu        *sync.Mutex
	taskDispatcher    *TaskDispatcher
	taskSessionID     func() string
	guardInFlight     *guardInFlightState
	// aforge-embed: D11 — readHistory tracks file reads so a re-read of an
	// unchanged file returns a one-line notice instead of the full content.
	// It is a pointer so per-context clones share one history, matching the
	// testMemo pattern.
	readHistory     *readHistoryState
	question        *question.Service
	questionEnabled bool
	questionRejects *questionRejectionState
}

type questionRejectionState struct {
	mu     sync.Mutex
	counts map[string]int
}

// guardInFlightState is shared by every forContext clone of a registry so an
// in-flight reservation made through a per-leaf clone is visible wherever the
// drain consults the root registry.
type guardInFlightState struct {
	mu  sync.Mutex
	ids map[string]int
}

// BeginGuardTask records that a tool call bound to the given PlanDB task is
// executing right now. Scheduler pumps run concurrently with tool execution,
// so a guard-bound task with no live dispatch is not evidence of a leak while
// the tool call is still in flight — the root drain must treat it as live
// work rather than sweep-close or release it (run N showed the drain closing
// a bookkeeping package 17s before its command finished). The returned func
// removes the reservation and must be called exactly when the tool returns,
// after any tool-level package close.
func (r *Registry) BeginGuardTask(taskID string) func() {
	state := r.guardInFlight
	if taskID == "" || state == nil {
		return func() {}
	}
	state.mu.Lock()
	if state.ids == nil {
		state.ids = map[string]int{}
	}
	state.ids[taskID]++
	state.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			state.mu.Lock()
			if state.ids[taskID] <= 1 {
				delete(state.ids, taskID)
			} else {
				state.ids[taskID]--
			}
			state.mu.Unlock()
		})
	}
}

// SetTaskObserver wires the process-wide observer into the task-tool
// dispatcher after the run-level registry has been started.
func (r *Registry) SetTaskObserver(tracker observer.Tracker, now func() time.Time) {
	if r == nil || r.taskDispatcher == nil {
		return
	}
	r.taskDispatcher.Observer = tracker
	r.taskDispatcher.Now = now
}

// GuardInFlightTaskIDs lists tasks currently backing an executing tool call.
func (r *Registry) GuardInFlightTaskIDs() []string {
	state := r.guardInFlight
	if state == nil {
		return nil
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	ids := make([]string, 0, len(state.ids))
	for id := range state.ids {
		ids = append(ids, id)
	}
	return ids
}

type instructionRegistry struct {
	mu       sync.Mutex
	services map[string]*instruction.Service
}

// PermissionRules resolves the agent and instance rules for one tool call.
type PermissionRules func(context.Context, steploop.ToolCall) permission.Ruleset

// PermissionEvaluator receives the TS-shaped request, including mutation diff metadata.
type PermissionEvaluator interface {
	Evaluate(permission.AskInput) error
}

// RegistryOptions supplies the host services used by live tool execution.
type RegistryOptions struct {
	Permission               PermissionEvaluator
	PermissionRules          PermissionRules
	PlanRun                  PlanRunFunc
	PlanDBActive             PlanDBActiveStore
	Instructions             []string
	Config                   *config.Service
	AllowExternalDirectories bool
	// HardConfineShellPaths rejects parsed external shell operands instead of
	// asking permission. It is the swedog boundary; codeaf leaves it disabled.
	HardConfineShellPaths bool
	TaskSpawner           TaskSessionSpawner
	TaskSessionID         func() string
	// ClientIdentity is Flag.CODEAF_CLIENT at the registry boundary. The codeaf
	// binary supplies "cli" when the environment does not override it; embedders
	// that omit it are not assumed to have an interactive client.
	ClientIdentity string
	Question       *question.Service
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

// FilterInput is the coder-visible projection of registry.ts tools() input.
type FilterInput struct {
	ProviderID string
	ModelID    string
	AgentName  string
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
	active := options.PlanDBActive
	if active == nil {
		active = &MemoryPlanDBActiveStore{}
	}
	run := options.PlanRun
	if run == nil {
		run = plandb.RunPlanDB
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
		if configured, ok := env.Get("CODEAF_CLIENT"); ok {
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
		guardInFlight:    &guardInFlightState{},
		planActive:       active,
		planRun:          run,
		readHistory:      &readHistoryState{reads: map[string]readFingerprint{}},
		config:           configService,
		allowExternal:    options.AllowExternalDirectories,
		hardConfineShell: options.HardConfineShellPaths,
		testMemo:         testmemo.NewTestCommandMemo(),
		testMemoMu:       &sync.Mutex{},
		taskDispatcher:   &TaskDispatcher{Spawner: options.TaskSpawner},
		taskSessionID:    options.TaskSessionID,
		question:         questionService,
		questionEnabled:  clientIdentity == "app" || clientIdentity == "cli" || clientIdentity == "desktop" || env.Enabled("CODEAF_ENABLE_QUESTION_TOOL"),
		questionRejects:  &questionRejectionState{counts: map[string]int{}},
		formatters:       newFormatterServices(),
		npm:              core.NewNpm(filepath.Join(cacheDir, "codeaf"), nil),
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
	// TS registry.ts:239-240 enables question for app/cli/desktop or the force
	// flag. Go has no ambient client service, so cmd/codeaf supplies its CLI
	// identity explicitly; CODEAF_ENABLE_QUESTION_TOOL still enables embedders.
	if r.questionEnabled {
		question := definition("question", questionDescription, questionSchema, validateQuestion)
		// AI SDK holds the TS result stream open while question.execute awaits the
		// user, so processor cleanup (and its 250ms abort settlement) is not reached.
		question.WaitForResult = true
		definitions = append(definitions, question)
	}
	return append(definitions,
		definition("bash", "Run a Bash command in the workspace. Output is capped at 30000 bytes and execution at 600000ms.", bashSchema, validateBash),
		definition("read", readDescription, readSchema, validateRead),
		definition("glob", globDescription, globSchema, validateGlob),
		definition("grep", grepDescription, grepSchema, validateGrep),
		definition("edit", editDescription, editSchema, validateEdit),
		definition("write", writeDescription, writeSchema, validateWrite),
		definition("task", taskDescription, taskSchema, validateTask),
		definition("webfetch", webFetchDescription, webFetchSchema, validateWebFetch),
		definition("plandb", planDBDescription, planDBSchema, validatePlanDB),
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

// DefinitionsFor applies the registry's provider, specialist-mode, and
// model-family visibility rules.
func (r *Registry) DefinitionsFor(input FilterInput) []steploop.ToolDefinition {
	return FilterDefinitions(r.Definitions(), input)
}

// WebSearchEnabled ports registry.ts:78-83.
func WebSearchEnabled(providerID string, flags WebSearchFlags) bool {
	return providerID == "codeaf" || flags.Exa || flags.Parallel
}

// FilterDefinitions applies the registry's provider, specialist-mode, and
// model-family visibility rules. It is separate from Registry so plugin/custom
// definitions can pass through the same isolation seam.
func FilterDefinitions(
	definitions []steploop.ToolDefinition,
	input FilterInput,
) []steploop.ToolDefinition {
	allExclusive := baked.AllExclusiveToolIDs()
	mode, hasMode := baked.ModeForAgent(input.AgentName)
	myExclusive := map[string]struct{}{}
	forbidden := map[string]struct{}{}
	if hasMode {
		for _, id := range mode.ExclusiveTools {
			myExclusive[id] = struct{}{}
		}
		for _, id := range mode.ForbiddenTools {
			forbidden[id] = struct{}{}
		}
	}
	// aforge-embed: D9 — apply_patch is ungated. Upstream (registry.ts:339-393)
	// gated apply_patch to gpt-family models via usePatch and hid edit/write for
	// the same models. apply_patch is a harness-side unified-diff editor that
	// needs no provider support, so the gate served only to deny non-gpt coders
	// (notably deepseek) the multi-file edit tool. Every coder now gets edit,
	// write, and apply_patch together and picks whichever fits the change.
	out := make([]steploop.ToolDefinition, 0, len(definitions))
	for _, item := range definitions {
		id := item.Provider.Name
		// coder.md explicitly disables delegation and task-graph mutation.
		if input.AgentName == "coder" && (id == "task" || id == "plandb") {
			continue
		}
		if id == "websearch" && !WebSearchEnabled(input.ProviderID, input.Flags) {
			continue
		}
		if allExclusive.Has(id) {
			if _, ok := myExclusive[id]; !ok {
				continue
			}
		}
		if _, ok := forbidden[id]; ok {
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
	case "task":
		result, err = r.executeTask(ctx, call)
	case "webfetch":
		result, err = r.executeWebFetch(ctx, call)
	case "plandb":
		result, err = r.executePlanDB(ctx, call)
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
		Global: instruction.Global{Config: filepath.Join(config, "codeaf"), Home: home},
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
